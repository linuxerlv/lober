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
      id: 'business',
      title: '业务',
      parentId: '',
      permission: '',
      required: false,
    },
    {
      id: 'access',
      title: '权限管理',
      parentId: '',
      permission: '',
      required: false,
    },
    {
      id: 'hosts',
      parentId: 'business',
      title: '主机清单',
      permission: 'host:read',
      required: false,
    },
    {
      id: 'templates',
      parentId: 'business',
      title: '模板',
      permission: 'template:read',
      required: false,
    },
    {
      id: 'reports',
      parentId: 'business',
      title: '报表与审计',
      permission: 'audit:read',
      required: false,
    },
    {
      id: 'users',
      parentId: 'access',
      title: '账号管理',
      permission: 'user:manage',
      required: false,
    },
    {
      id: 'role-menus',
      parentId: 'access',
      title: '角色与菜单',
      permission: 'user:manage',
      required: true,
    },
  ],
  roles: roles.map((role) => ({
    role,
    availableMenuIds:
      role === 'admin'
        ? ['hosts', 'templates', 'reports', 'users', 'role-menus']
        : ['hosts', 'templates'],
    menuIds:
      role === 'admin'
        ? ['hosts', 'templates', 'reports', 'users', 'role-menus']
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
    expect(el.querySelector('[role="tree"]')).not.toBeNull(),
  );
  return el;
}
async function selectRole(el: HTMLElement, label: string) {
  (el.querySelector('[role="combobox"]') as HTMLElement).click();
  await nextTick();
  const option = Array.from(document.querySelectorAll('[role="option"]')).find(
    (item) => item.textContent?.includes(label),
  ) as HTMLElement;
  expect(option).toBeDefined();
  option.click();
  await nextTick();
}
async function check(el: HTMLElement, id: string, checked: boolean) {
  const input = el.querySelector<HTMLInputElement>(
    `[data-key="${id}"] input[type="checkbox"]`,
  )!;
  input.checked = checked;
  input.dispatchEvent(new Event('change', { bubbles: true }));
  await nextTick();
}
it('offers only eligible menus and can save an explicit empty viewer menu', async () => {
  const el = await setup();
  expect(el.querySelector('[data-key="reports"]')).toBeNull();
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
    el.querySelector<HTMLInputElement>('[data-key="templates"] input')!.checked,
  ).toBe(true);
});
it('keeps the administrator permission-management menu checked', async () => {
  const el = await setup();
  await selectRole(el, '管理员');
  const required = el.querySelector<HTMLInputElement>(
    '[data-key="role-menus"] input',
  )!;
  expect(required.checked).toBe(true);
  expect(required.disabled).toBe(true);
});
it('keeps each role draft separate when switching the themed selector', async () => {
  const el = await setup();
  await check(el, 'templates', true);
  await selectRole(el, '运维操作员');
  expect(
    el.querySelector<HTMLInputElement>('[data-key="templates"] input')!.checked,
  ).toBe(false);
  await selectRole(el, '只读用户');
  expect(
    el.querySelector<HTMLInputElement>('[data-key="templates"] input')!.checked,
  ).toBe(true);
});
it('saves only selected children and keeps required access when a parent is unchecked', async () => {
  const el = await setup();
  await selectRole(el, '管理员');
  await check(el, 'access', false);
  expect(
    el.querySelector<HTMLInputElement>('[data-key="users"] input')!.checked,
  ).toBe(false);
  expect(
    el.querySelector<HTMLInputElement>('[data-key="role-menus"] input')!
      .checked,
  ).toBe(true);
  expect(
    el.querySelector<HTMLInputElement>(
      '[data-key="access"] > .el-tree-node__content input',
    )!.indeterminate,
  ).toBe(true);
  io.saveRoleMenusApi.mockResolvedValue({
    ...fixture.roles[4],
    menuIds: ['hosts', 'templates', 'reports', 'role-menus'],
    rev: 1,
  });
  (el.querySelector('[data-action="save"]') as HTMLButtonElement).click();
  await vi.waitFor(() =>
    expect(io.saveRoleMenusApi).toHaveBeenCalledWith('admin', {
      menuIds: ['hosts', 'templates', 'reports', 'role-menus'],
      rev: 0,
    }),
  );
});
