import { describe, expect, it, vi } from 'vitest';

import { NumberedPage } from './numbered-page';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

describe('NumberedPage', () => {
  it('requests numbered pages, replaces rows and preserves real zero totals', async () => {
    const page = new NumberedPage<string>();
    const fetch = vi
      .fn()
      .mockResolvedValueOnce({ items: ['a'], total: 41 })
      .mockResolvedValueOnce({ items: ['b'], total: 41 })
      .mockResolvedValueOnce({ items: [], total: 0 });
    expect(await page.load(fetch)).toEqual({ items: ['a'], total: 41 });
    page.setPage(2);
    await page.load(fetch);
    expect(fetch).toHaveBeenLastCalledWith({ page: 2, page_size: 20 });
    expect(page.items).toEqual(['b']);
    page.setPageSize(50);
    expect(page.page).toBe(1);
    await page.load(fetch);
    expect(page.total).toBe(0);
    expect(page.items).toEqual([]);
  });

  it('retains rows and restores the last successful page and size on failure', async () => {
    const page = new NumberedPage<string>();
    page.setPage(2);
    await page.load(async () => ({ items: ['kept'], total: 41 }));
    page.setPageSize(100);
    await expect(
      page.load(async () => {
        throw new Error('offline');
      }),
    ).rejects.toThrow('offline');
    expect({
      page: page.page,
      size: page.pageSize,
      rows: page.items,
      total: page.total,
      loading: page.loading,
    }).toEqual({
      page: 2,
      size: 20,
      rows: ['kept'],
      total: 41,
      loading: false,
    });
  });

  it('ignores stale successes and failures, including after filters reset', async () => {
    const page = new NumberedPage<string>();
    const stale = deferred<{ items: string[]; total: number }>();
    const first = page.load(() => stale.promise);
    page.reset();
    const newest = deferred<{ items: string[]; total: number }>();
    const latest = page.load(() => newest.promise);
    stale.resolve({ items: ['old'], total: 100 });
    await first;
    expect(page.loading).toBe(true);
    expect(page.items).toEqual([]);
    newest.resolve({ items: ['new'], total: 1 });
    await latest;
    const failure = deferred<{ items: string[]; total: number }>();
    const older = page.load(() => failure.promise);
    await page.load(async () => ({ items: ['newest'], total: 1 }));
    failure.reject(new Error('stale failure'));
    expect(await older).toEqual({ items: ['newest'], total: 1 });
    expect(page.items).toEqual(['newest']);
  });

  it('refetches the valid last page once after deletion', async () => {
    const page = new NumberedPage<string>();
    page.setPage(3);
    const fetch = vi
      .fn()
      .mockResolvedValueOnce({ items: [], total: 40 })
      .mockResolvedValueOnce({ items: ['last'], total: 40 });
    expect(await page.load(fetch)).toEqual({ items: ['last'], total: 40 });
    expect(fetch.mock.calls).toEqual([
      [{ page: 3, page_size: 20 }],
      [{ page: 2, page_size: 20 }],
    ]);
    expect(page.page).toBe(2);
  });

  it('does not invent totals when the backend omits them', async () => {
    const page = new NumberedPage<string>();
    await expect(
      page.load(async () => ({ items: ['a'], next: '' })),
    ).rejects.toThrow('total');
    expect(page.total).toBe(0);
    expect(page.items).toEqual([]);
  });
});
