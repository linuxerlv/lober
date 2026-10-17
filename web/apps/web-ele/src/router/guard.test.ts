import {
  createMemoryHistory,
  createRouter,
  type RouteRecordRaw,
} from 'vue-router';
import { describe, expect, it, vi } from 'vitest';
import { createConsoleGuard } from './console-access';
import type { ConsoleSessionUser } from '#/api/console/session';
const component = { template: '<div />' };
const base: RouteRecordRaw[] = [
  '/auth/login',
  '/account/password',
  '/account/profile',
  '/no-access',
].map((path) => ({ path, name: path, component }));
base.push(
  { name: 'Root', path: '/', component, children: [] },
  { name: 'FallbackNotFound', path: '/:path(.*)*', component },
);
const business: RouteRecordRaw[] = [
  {
    name: 'Hosts',
    path: '/hosts',
    component,
    meta: { title: 'Test', menuId: 'hosts' },
  },
  {
    name: 'Templates',
    path: '/templates',
    component,
    meta: { title: 'Test', menuId: 'templates' },
  },
];
function setup() {
  let token: string | null = 'token';
  let user: ConsoleSessionUser = {
    username: 'alice',
    displayName: 'Alice',
    role: 'viewer',
    permissions: ['host:read', 'template:read'],
    menuIds: ['hosts'],
    menuRev: 1,
    createdAt: 1,
    rev: 1,
    disabled: false,
    passwordChangeRequired: false,
  };
  const router = createRouter({ history: createMemoryHistory(), routes: base });
  const error = vi.fn(),
    clearCaches = vi.fn();
  const refresh = vi.fn(async () => user);
  router.beforeEach(
    createConsoleGuard({
      router,
      baseRoutes: base,
      routes: business,
      getToken: () => token,
      getUser: () => user,
      refreshUser: refresh,
      onError: error,
      clearCaches,
    }),
  );
  return {
    router,
    refresh,
    error,
    clearCaches,
    setUser: (value: Partial<ConsoleSessionUser>) => {
      user = { ...user, ...value };
    },
    setToken: (value: string | null) => {
      token = value;
    },
  };
}
describe('console navigation', () => {
  it('keeps personal settings accessible with no business menus', async () => {
    const s = setup();
    s.setUser({ menuIds: [] });
    await s.router.push('/account/profile');
    expect(s.router.currentRoute.value.path).toBe('/account/profile');
  });
  it('blocks direct hidden URLs and redirects an empty menu without looping', async () => {
    const s = setup();
    await s.router.push('/templates');
    expect(s.router.currentRoute.value.path).toBe('/hosts');
    s.setUser({ menuIds: [], menuRev: 2 });
    await s.router.push('/templates');
    expect(s.router.currentRoute.value.path).toBe('/no-access');
    expect(s.router.hasRoute('Hosts')).toBe(false);
    expect(s.clearCaches).toHaveBeenCalled();
  });
  it('requires password change before all business navigation, while unauthenticated visits go to login', async () => {
    const s = setup();
    s.setUser({ passwordChangeRequired: true });
    await s.router.push('/hosts');
    expect(s.router.currentRoute.value.path).toBe('/account/password');
    s.setToken(null);
    await s.router.push('/hosts');
    expect(s.router.currentRoute.value.path).toBe('/auth/login');
  });
  it('does not grant a page if refreshing the current user fails', async () => {
    const s = setup();
    await s.router.push('/hosts');
    s.refresh.mockRejectedValue(new Error('offline'));
    await s.router.push('/templates');
    expect(s.router.currentRoute.value.path).toBe('/hosts');
    expect(s.error).toHaveBeenCalledTimes(1);
  });
});
