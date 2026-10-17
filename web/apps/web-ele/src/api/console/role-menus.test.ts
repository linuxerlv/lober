import { expect, it } from 'vitest';
import { createRoleMenusApi } from './role-menus';
it('uses the revision and target role when saving menus', async () => {
  const requests: unknown[] = [];
  const api = createRoleMenusApi({
    async get<T>(path: string) {
      requests.push(path);
      return { menus: [], roles: [] } as T;
    },
    async put<T>(path: string, data: unknown) {
      requests.push([path, data]);
      return {
        role: 'viewer',
        menuIds: [],
        availableMenuIds: ['hosts'],
        rev: 2,
      } as T;
    },
  });
  await api.list();
  expect((await api.save('viewer', { menuIds: [], rev: 1 })).rev).toBe(2);
  expect(requests).toEqual([
    '/roles/menus',
    ['/roles/viewer/menus', { menuIds: [], rev: 1 }],
  ]);
});
