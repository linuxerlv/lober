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
    total: 41,
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
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
  expect(dialog).not.toBeNull();
  expect(dialog.textContent).toContain('编辑账号');
  const role = dialog.querySelector<HTMLElement>('[role="combobox"]')!;
  role.click();
  await nextTick();
  (
    Array.from(document.querySelectorAll('[role="option"]')).find((option) =>
      option.textContent?.includes('运维操作员'),
    ) as HTMLElement
  ).click();
  await nextTick();
  io.update.mockRejectedValue({ response: { status: 409 } });
  dialog
    .querySelector('form')!
    .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  await vi.waitFor(() =>
    expect(io.update).toHaveBeenCalledWith('lisi', {
      displayName: '李四',
      role: 'operator',
      disabled: false,
      rev: 4,
    }),
  );
  await vi.waitFor(() =>
    expect(dialog.textContent).toContain('当前编辑已保留'),
  );
  expect(dialog.querySelector('.el-select')!.textContent).toContain(
    '运维操作员',
  );
});
it('creates an account in a modal, blocks closing while saving and refreshes the list on success', async () => {
  const el = await setup();
  expect(el.querySelector('form')).toBeNull();
  Array.from(el.querySelectorAll('button'))
    .find((button) => button.textContent?.includes('创建账号'))!
    .click();
  await nextTick();
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
  expect(dialog).not.toBeNull();
  for (const [name, value] of [
    ['username', 'wangwu'],
    ['displayName', '王五'],
    ['password', 'initial-password'],
  ]) {
    const input = dialog.querySelector<HTMLInputElement>(
      `input[name="${name}"]`,
    )!;
    input.value = value!;
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }
  await nextTick();
  let complete!: () => void;
  io.create.mockReturnValue(
    new Promise<void>((resolve) => {
      complete = resolve;
    }),
  );
  const form = dialog.querySelector('form')!;
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  await nextTick();
  expect(io.create).toHaveBeenCalledWith({
    username: 'wangwu',
    displayName: '王五',
    role: 'viewer',
    password: 'initial-password',
  });
  const cancel = Array.from(dialog.querySelectorAll('button')).find((button) =>
    button.textContent?.includes('取消'),
  )!;
  expect(cancel.disabled).toBe(true);
  expect(dialog.querySelector('.el-dialog__headerbtn')).toBeNull();
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  expect(io.create).toHaveBeenCalledTimes(1);
  complete();
  await vi.waitFor(() => expect(el.textContent).toContain('账号已创建'));
  await vi.waitFor(() =>
    expect(document.querySelector('[role="dialog"] form')).toBeNull(),
  );
  expect(io.list).toHaveBeenCalledTimes(2);
});
it('cancels a modal without saving and clears the password before reopening', async () => {
  const el = await setup();
  const create = Array.from(el.querySelectorAll('button')).find((button) =>
    button.textContent?.includes('创建账号'),
  )!;
  create.click();
  await nextTick();
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
  expect(dialog).not.toBeNull();
  const password = dialog.querySelector<HTMLInputElement>(
    'input[name="password"]',
  )!;
  password.value = 'discard-this-password';
  password.dispatchEvent(new Event('input', { bubbles: true }));
  await nextTick();
  Array.from(dialog.querySelectorAll('button'))
    .find((button) => button.textContent?.includes('取消'))!
    .click();
  await vi.waitFor(() =>
    expect(document.querySelector('[role="dialog"] form')).toBeNull(),
  );
  expect(io.create).not.toHaveBeenCalled();
  create.click();
  await nextTick();
  expect(
    document.querySelector<HTMLInputElement>(
      '[role="dialog"] input[name="password"]',
    )!.value,
  ).toBe('');
});
it('does not load accounts or render a management form for non-admin', async () => {
  const el = await setup(false);
  expect(io.list).not.toHaveBeenCalled();
  expect(el.querySelector('form')).toBeNull();
  expect(el.textContent).toContain('仅管理员');
});
it('replaces account rows on numbered navigation and refreshes the selected page', async () => {
  const el = await setup();
  await vi.waitFor(() => expect(el.textContent).toContain('李四'));
  expect(io.list).toHaveBeenLastCalledWith({ page: 1, page_size: 20 });
  io.list.mockResolvedValue({
    items: [
      {
        username: 'other',
        displayName: '第二页账号',
        role: 'viewer',
        disabled: false,
        rev: 1,
      },
    ],
    next: '',
    total: 41,
  });
  el.querySelector<HTMLButtonElement>('.el-pagination .btn-next')!.click();
  await vi.waitFor(() =>
    expect(io.list).toHaveBeenLastCalledWith({ page: 2, page_size: 20 }),
  );
  await vi.waitFor(() => expect(el.textContent).toContain('第二页账号'));
  expect(el.textContent).not.toContain('李四');
  Array.from(el.querySelectorAll('button'))
    .find((button) => button.textContent?.includes('刷新列表'))!
    .click();
  await vi.waitFor(() => expect(io.list).toHaveBeenCalledTimes(3));
  expect(io.list).toHaveBeenLastCalledWith({ page: 2, page_size: 20 });
});
