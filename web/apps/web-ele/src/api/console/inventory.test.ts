import { beforeEach, describe, expect, it, vi } from 'vitest';

const http = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}));
vi.mock('#/api/request', () => ({ requestClient: http }));
import * as isos from './artifact';
import * as hosts from './host';

describe('inventory HTTP contracts', () => {
  beforeEach(() => vi.clearAllMocks());

  it('preserves the identity of a partially registered ISO for recovery', () => {
    expect(isos.createdISOIDOf({ response: { headers: { location: '/console/v1/isos/id%3Fx' } } })).toBe('id?x');
    expect(isos.createdISOIDOf({ response: { headers: {} } })).toBeUndefined();
  });

  it('registers ISO with sources at the actual ISO endpoint', async () => {
    const request = {
      name: 'Debian',
      family: 'linux',
      series: 'debian',
      sources: [
        { kind: 'url', addr: 'https://example.test/main.iso', role: '' },
        {
          kind: 'url',
          addr: 'https://example.test/netboot.iso',
          role: 'netboot',
        },
      ],
    };
    await isos.createArtifactSetApi(request as never);
    expect(http.post).toHaveBeenCalledWith('/isos', request);
  });

  it('updates a host by encoded SN and preserves revision and attributes', async () => {
    const data = {
      sn: 'A?B#C%',
      attrs: { rack: 'R2', ip: '192.0.2.2' },
      rev: 7,
    };
    await hosts.updateHostApi(data.sn, data);
    expect(http.put).toHaveBeenCalledWith('/hosts/A%3FB%23C%25', data);
    await hosts.deleteHostApi(data.sn, data.rev);
    expect(http.delete).toHaveBeenCalledWith('/hosts/A%3FB%23C%25', {
      params: { rev: 7 },
    });
  });

  it('sends CSV bytes unchanged and always identifies dry-run versus commit', async () => {
    const bytes = new Uint8Array([239, 187, 191, 115, 110, 10, 65, 10]);
    await hosts.previewHostCSVApi(bytes);
    expect(http.post).toHaveBeenCalledWith('/hosts/import', bytes, {
      headers: { 'Content-Type': 'text/csv' },
      params: { dryRun: true },
    });
    await hosts.commitHostCSVApi(bytes);
    expect(http.post).toHaveBeenLastCalledWith('/hosts/import', bytes, {
      headers: { 'Content-Type': 'text/csv' },
      params: { dryRun: false },
    });
  });

  it('uploads raw bytes for the selected role with the exact content range', async () => {
    const bytes = new Blob([new Uint8Array([7, 8, 9])]);
    await isos.uploadISOChunkApi('id?x', 'netboot', bytes, 3, 9);
    expect(http.put).toHaveBeenCalledWith('/isos/id%3Fx/content', bytes, {
      headers: {
        'Content-Type': 'application/octet-stream',
        'Content-Range': 'bytes 3-5/9',
      },
      params: { role: 'netboot' },
      timeout: 15 * 60_000,
    });
  });

  it('passes cursor and host attribute search to the server', async () => {
    const page = { items: [], next: 'opaque-next' };
    http.get.mockResolvedValueOnce(page);
    const params = {
      prefix: 'SN',
      attr_key: 'rack',
      attr_value: 'R2',
      cursor: 'opaque',
      limit: 50,
    };
    expect(await hosts.getHostListApi(params)).toBe(page);
    expect(http.get).toHaveBeenCalledWith('/hosts', { params });
  });
});
