import { describe, expect, it, vi } from 'vitest';

import { missingChunks, verifyReselectedFile } from '../artifact-sets/upload';
import { attributesOf, CsvImportSession, CursorPage } from './inventory-state';

describe('host maintenance state', () => {
  it('keeps all string attributes, rejects duplicate keys without silently losing values', () => {
    expect(
      attributesOf([
        { key: 'rack', value: 'R1' },
        { key: 'ip', value: '192.0.2.1' },
      ]),
    ).toEqual({ rack: 'R1', ip: '192.0.2.1' });
    expect(() =>
      attributesOf([
        { key: 'rack', value: 'R1' },
        { key: 'rack', value: 'R2' },
      ]),
    ).toThrow('重复');
  });
  it('requires a successful dry-run and explicit confirmation of the same bytes', async () => {
    const session = new CsvImportSession();
    const commit = vi.fn();
    const bytes = new TextEncoder().encode('sn,rack\r\nSN1,R2\r\n');
    session.select(bytes);
    await expect(session.commit(commit, true)).rejects.toThrow('预览');
    await session.preview(async () => ({
      total: 1,
      rows: [{ row: 2, sn: 'SN1', action: 'create', problems: [] }],
    }));
    await expect(session.commit(commit, false)).rejects.toThrow('确认');
    await session.commit(commit, true);
    expect(commit).toHaveBeenCalledWith(bytes);
    session.select(new TextEncoder().encode('sn\nSN2\n'));
    await expect(session.commit(commit, true)).rejects.toThrow('预览');
  });
  it('blocks the whole import when any dry-run row has server diagnostics', async () => {
    const session = new CsvImportSession();
    session.select(new Uint8Array([1]));
    await session.preview(async () => ({
      total: 1,
      rows: [{ row: 2, sn: 'SN1', action: 'error', problems: [] }],
    }));
    await expect(session.commit(vi.fn(), true)).rejects.toThrow('错误');
  });
  it('does not attach an old preview to CSV changed during the request', async () => {
    const session = new CsvImportSession();
    session.select(new TextEncoder().encode('sn\nold\n'));
    let resolve!: (value: { total: number; rows: [] }) => void;
    const pending = session.preview(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    session.select(new TextEncoder().encode('sn\nnew\n'));
    resolve({ total: 0, rows: [] });
    await pending;
    await expect(session.commit(vi.fn(), true)).rejects.toThrow('预览');
  });
  it('resets cursor on a new search and discards late responses', async () => {
    const page = new CursorPage<{ sn: string }>();
    let resolve!: (value: { items: { sn: string }[]; next: string }) => void;
    const late = page.load(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    page.reset();
    await page.load(async () => ({ items: [{ sn: 'new' }], next: 'two' }));
    resolve({ items: [{ sn: 'old' }], next: 'old' });
    await late;
    expect(page.items).toEqual([{ sn: 'new' }]);
    page.advance();
    expect(page.cursor).toBe('two');
    page.reset();
    expect(page.cursor).toBe('');
  });
  it('ignores a failed request from an obsolete search', async () => {
    const page = new CursorPage<{ sn: string }>();
    let reject!: (reason: unknown) => void;
    const pending = page.load(
      () =>
        new Promise((_resolve, fail) => {
          reject = fail;
        }),
    );
    page.reset();
    await page.load(async () => ({ items: [{ sn: 'current' }], next: '' }));
    reject(new Error('obsolete network failure'));
    await expect(pending).resolves.toEqual({ items: [{ sn: 'current' }] });
  });
});

describe('persisted ISO upload ranges', () => {
  it('uploads only gaps in sorted or overlapping persisted ranges', () => {
    expect(
      missingChunks(
        12,
        [
          { start: 8, length: 2 },
          { start: 0, length: 4 },
          { start: 2, length: 4 },
        ],
        3,
      ),
    ).toEqual([
      { start: 6, end: 8 },
      { start: 10, end: 12 },
    ]);
  });
  it('refuses a different reselected file before it can corrupt resumed content', async () => {
    const original = new File(['original'], 'install.iso', {
      lastModified: 123,
    });
    const changed = new File(['modified'], 'install.iso', {
      lastModified: 123,
    });
    const fingerprint = await verifyReselectedFile(original, 8);
    await expect(verifyReselectedFile(changed, 8, fingerprint)).rejects.toThrow(
      '不一致',
    );
    await expect(verifyReselectedFile(original, 9)).rejects.toThrow('大小');
  });
});
