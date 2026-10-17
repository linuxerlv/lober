import { describe, expect, it } from 'vitest';
import {
  createMemoryHistory,
  createRouter,
  type RouteRecordRaw,
} from 'vue-router';
import {
  filterConsoleRoutes,
  replaceConsoleRoutes,
  consoleAccessKey,
  firstConsolePath,
} from './console-access';
import type { ConsoleSessionUser } from '#/api/console/session';
const user: ConsoleSessionUser = {
  username: 'alice',
  displayName: 'Alice',
  role: 'hook-author',
  permissions: ['task:read', 'template:read', 'host:read'],
  menuIds: ['tasks', 'templates'],
  menuRev: 0,
  disabled: false,
  createdAt: 1,
  rev: 1,
  passwordChangeRequired: false,
};
const view = { template: '<div />' };
const base: RouteRecordRaw[] = [
  { name: 'Root', path: '/', component: view, children: [] },
  { name: 'NoAccess', path: '/no-access', component: view },
];
const routes: RouteRecordRaw[] = [
  {
    name: 'Tasks',
    path: '/tasks',
    meta: { title: 'Test', menuId: 'tasks' },
    children: [
      { name: 'TasksIndex', path: '', component: view },
      {
        name: 'TasksNew',
        path: 'new',
        meta: { title: 'Test', permission: 'task:create' },
        component: view,
      },
    ],
  },
  {
    name: 'Templates',
    path: '/templates',
    meta: {
      title: 'Test',
      menuId: 'templates',
      authority: ['admin', 'viewer'],
    },
    component: view,
  },
  {
    name: 'Hosts',
    path: '/hosts',
    meta: { title: 'Test', menuId: 'hosts' },
    component: view,
  },
];
describe('current console routes', () => {
  it('inherits menu restrictions, checks operation permission and ignores obsolete role lists', () => {
    const out = filterConsoleRoutes(routes, user);
    expect(out.map((r) => r.name)).toEqual(['Tasks', 'Templates']);
    expect(out[0]!.children!.map((r) => r.name)).toEqual(['TasksIndex']);
    expect(firstConsolePath(out)).toBe('/tasks');
    expect(
      firstConsolePath(filterConsoleRoutes(routes, { ...user, menuIds: [] })),
    ).toBe('/no-access');
    expect(routes[0]!.children).toHaveLength(2);
  });
  it('removes formerly granted routes on account or menu changes', () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: base,
    });
    replaceConsoleRoutes(router, base, routes, user);
    expect(router.hasRoute('Templates')).toBe(true);
    replaceConsoleRoutes(router, base, routes, { ...user, menuIds: ['hosts'] });
    expect(router.hasRoute('Templates')).toBe(false);
    expect(router.hasRoute('TasksIndex')).toBe(false);
    expect(router.hasRoute('Hosts')).toBe(true);
    expect(router.hasRoute('Root')).toBe(true);
    expect(consoleAccessKey(user)).not.toBe(
      consoleAccessKey({ ...user, username: 'bob' }),
    );
  });
});
