import { expect, it, vi } from 'vitest';

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('#/api/request', () => ({ requestClient: http }));

import { getDiscoveryApi, getDiscoveryListApi, registerDiscoveryApi } from './discovery';

it('reads the next discoveries page and registers using the encoded network address', async () => {
  await getDiscoveryListApi('next');
  expect(http.get).toHaveBeenCalledWith('/discoveries', {
    params: { cursor: 'next', limit: 50 },
  });
  await getDiscoveryApi('52:54:00:00:11:22');
  expect(http.get).toHaveBeenLastCalledWith(
    '/discoveries/52%3A54%3A00%3A00%3A11%3A22',
  );
  await registerDiscoveryApi('52:54:00:00:11:22', 'LAB-42');
  expect(http.post).toHaveBeenCalledWith(
    '/discoveries/52%3A54%3A00%3A00%3A11%3A22/register',
    { sn: 'LAB-42' },
  );
});
