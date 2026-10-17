import { createApp, defineComponent, h, nextTick } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';

import { useAccessStore } from '@vben/stores';

import { createPinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';
const io = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  profile: vi.fn(),
}));
vi.mock('#/api/console/accounts', () => ({
  accountsApi: io,
  roleLabels: {
    viewer: '只读用户',
    operator: '运维操作员',
    'hook-author': '作者',
    'hook-approver': '审批人',
    admin: '管理员',
  },
}));
vi.mock('#/api', () => ({
  loginApi: vi.fn(),
  logoutApi: vi.fn(),
  getUserInfoApi: vi.fn(),
}));
import { useAuthStore } from '#/store';

import Users from './users.vue';
const cleanup: Array<() => void> = [];
afterEach(() => {
  cleanup.splice(0).forEach((fn) => fn());
  vi.resetAllMocks();
});
async function setup(admin = true) {
  io.list.mockResolvedValue({
    items: [
      {
        username: 'lisi',
        displayName: '李四',
        role: 'viewer',
        permissions: ['host:read'],
        disabled: false,
        rev: 4,
      },
    ],
    next: '',
  });
  const pinia = createPinia();
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    });
  await router.push('/');
  const app = createApp(
    defineComponent({
      setup() {
        useAccessStore().setAccessToken('session');
        useAuthStore().applySessionUser({
          username: 'admin',
          displayName: 'Admin',
          role: admin ? 'admin' : 'viewer',
          permissions: admin ? ['user:manage'] : [],
          menuIds: [],
          menuRev: 0,
          rev: 1,
          createdAt: 1,
          disabled: false,
          passwordChangeRequired: false,
        });
        return () => h(Users);
      },
    }),
  );
  app.use(pinia);
  app.use(router);
  const el = document.createElement('div');
  document.body.append(el);
  app.mount(el);
  cleanup.push(() => {
    app.unmount();
    el.remove();
  });
  await nextTick();
  return el;
}
it('lets admin assign a fixed role without resetting a password and retains conflicting edits', async () => {
  const el = await setup();
  await vi.waitFor(() => expect(el.textContent).toContain('李四'));
  const edit = Array.from(el.querySelectorAll('button')).find((b) =>
    b.textContent?.includes('编辑'),
  );
  edit!.click();
  await nextTick();
  const role = el.querySelector<HTMLSelectElement>('select[name=role]')!;
  role.value = 'operator';
  role.dispatchEvent(new Event('change', { bubbles: true }));
  await nextTick();
  io.update.mockRejectedValue({ response: { status: 409 } });
  el.querySelector('form')!.dispatchEvent(
    new Event('submit', { bubbles: true, cancelable: true }),
  );
  await vi.waitFor(() =>
    expect(io.update).toHaveBeenCalledWith('lisi', {
      displayName: '李四',
      role: 'operator',
      disabled: false,
      rev: 4,
    }),
  );
  await vi.waitFor(() => expect(el.textContent).toContain('当前编辑已保留'));
  expect(role.value).toBe('operator');
});
it('does not load accounts or render a management form for non-admin', async () => {
  const el = await setup(false);
  expect(io.list).not.toHaveBeenCalled();
  expect(el.querySelector('form')).toBeNull();
  expect(el.textContent).toContain('仅管理员');
});
