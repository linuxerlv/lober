import { createApp, defineComponent, h, nextTick } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';

import { useAccessStore } from '@vben/stores';

import { createPinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';
const io = vi.hoisted(() => ({
  getRoleMenusApi: vi.fn(),
  saveRoleMenusApi: vi.fn(),
}));
vi.mock('#/api/console/role-menus', () => io);
vi.mock('#/api', () => ({
  loginApi: vi.fn(),
  logoutApi: vi.fn(),
  getUserInfoApi: vi.fn(),
}));
import { useAuthStore } from '#/store';

import RoleMenus from './role-menus.vue';
const roles = ['viewer', 'operator', 'hook-author', 'hook-approver', 'admin'];
const fixture = {
  menus: [
    {
      id: 'hosts',
      title: '主机清单',
      permission: 'host:read',
      required: false,
    },
    {
      id: 'templates',
      title: '模板',
      permission: 'template:read',
      required: false,
    },
    {
      id: 'reports',
      title: '报表与审计',
      permission: 'audit:read',
      required: false,
    },
    {
      id: 'role-menus',
      title: '角色与菜单',
      permission: 'user:manage',
      required: true,
    },
  ],
  roles: roles.map((role) => ({
    role,
    availableMenuIds:
      role === 'admin'
        ? ['hosts', 'templates', 'reports', 'role-menus']
        : ['hosts', 'templates'],
    menuIds:
      role === 'admin'
        ? ['hosts', 'templates', 'reports', 'role-menus']
        : ['hosts'],
    rev: 0,
  })),
};
const cleanup: Array<() => void> = [];
afterEach(() => {
  cleanup.splice(0).forEach((fn) => fn());
  vi.resetAllMocks();
});
async function setup() {
  io.getRoleMenusApi.mockResolvedValue(structuredClone(fixture));
  const pinia = createPinia();
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    });
  await router.push('/');
  const app = createApp(
    defineComponent({
      setup() {
        useAccessStore().setAccessToken('admin');
        useAuthStore().applySessionUser({
          username: 'admin',
          displayName: '管理员',
          role: 'admin',
          permissions: ['user:manage'],
          menuIds: ['role-menus'],
          menuRev: 0,
          rev: 1,
          createdAt: 1,
          disabled: false,
          passwordChangeRequired: false,
        });
        return () => h(RoleMenus);
      },
    }),
  );
  const el = document.createElement('div');
  document.body.append(el);
  app.use(pinia);
  app.use(router);
  app.mount(el);
  cleanup.push(() => {
    app.unmount();
    el.remove();
  });
  await vi.waitFor(() =>
    expect(el.querySelectorAll('select option')).toHaveLength(5),
  );
  return el;
}
async function check(el: HTMLElement, id: string, checked: boolean) {
  const input = el.querySelector<HTMLInputElement>(`input[value="${id}"]`)!;
  input.checked = checked;
  input.dispatchEvent(new Event('change', { bubbles: true }));
  await nextTick();
}
it('offers only eligible menus and can save an explicit empty viewer menu', async () => {
  const el = await setup();
  expect(el.querySelector('input[value="reports"]')).toBeNull();
  await check(el, 'hosts', false);
  io.saveRoleMenusApi.mockResolvedValue({
    ...fixture.roles[0],
    menuIds: [],
    rev: 1,
  });
  (el.querySelector('[data-action="save"]') as HTMLButtonElement).click();
  await vi.waitFor(() =>
    expect(io.saveRoleMenusApi).toHaveBeenCalledWith('viewer', {
      menuIds: [],
      rev: 0,
    }),
  );
});
it('preserves edits after a conflict and prevents duplicate submits', async () => {
  const el = await setup();
  await check(el, 'templates', true);
  let reject!: (error: unknown) => void;
  io.saveRoleMenusApi.mockReturnValue(
    new Promise((_, fail) => {
      reject = fail;
    }),
  );
  const save = el.querySelector<HTMLButtonElement>('[data-action="save"]')!;
  save.click();
  await nextTick();
  expect(save.disabled).toBe(true);
  save.click();
  expect(io.saveRoleMenusApi).toHaveBeenCalledTimes(1);
  reject({ response: { status: 409 } });
  await vi.waitFor(() =>
    expect(el.textContent).toContain('配置已被其他管理员修改'),
  );
  expect(
    el.querySelector<HTMLInputElement>('input[value="templates"]')!.checked,
  ).toBe(true);
});
it('keeps the administrator permission-management menu checked', async () => {
  const el = await setup();
  const select = el.querySelector('select')!;
  select.value = 'admin';
  select.dispatchEvent(new Event('change', { bubbles: true }));
  await nextTick();
  const required = el.querySelector<HTMLInputElement>(
    'input[value="role-menus"]',
  )!;
  expect(required.checked).toBe(true);
  expect(required.disabled).toBe(true);
});
