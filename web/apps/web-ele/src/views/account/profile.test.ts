import { createApp, defineComponent, h, nextTick } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';

import { createPinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';
const io = vi.hoisted(() => ({ profile: vi.fn(), getUserInfoApi: vi.fn() }));
vi.mock('#/api/console/accounts', () => ({
  accountsApi: io,
  roleLabels: { viewer: '只读用户' },
}));
vi.mock('#/api', () => ({
  getUserInfoApi: io.getUserInfoApi,
  loginApi: vi.fn(),
  logoutApi: vi.fn(),
}));
import { useAccessStore } from '@vben/stores';

import { useAuthStore } from '#/store';

import Profile from './profile.vue';
const cleanup: Array<() => void> = [];
afterEach(() => {
  cleanup.splice(0).forEach((fn) => fn());
  vi.resetAllMocks();
});
it('keeps security fields read-only and retains the personal draft on a conflict', async () => {
  const user = {
    username: 'lisi',
    displayName: '李四',
    email: '',
    bio: '',
    avatar: '',
    role: 'viewer',
    permissions: [],
    menuIds: [],
    menuRev: 0,
    rev: 5,
    createdAt: 1,
    disabled: false,
    passwordChangeRequired: false,
  };
  io.getUserInfoApi.mockResolvedValue(user);
  const pinia = createPinia();
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    });
  await router.push('/');
  const app = createApp(
    defineComponent({
      setup() {
        useAccessStore().setAccessToken('viewer');
        useAuthStore().applySessionUser(user as any);
        return () => h(Profile);
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
  await vi.waitFor(() =>
    expect(
      el.querySelector<HTMLInputElement>('input[name=displayName]')!.value,
    ).toBe('李四'),
  );
  expect(el.querySelectorAll('input[readonly]')).toHaveLength(2);
  expect(el.querySelector('select')).toBeNull();
  const input = el.querySelector<HTMLInputElement>('input[name=displayName]')!;
  input.value = '新名字';
  input.dispatchEvent(new Event('input', { bubbles: true }));
  await nextTick();
  io.profile.mockRejectedValue({ response: { status: 409 } });
  el.querySelector('form')!.dispatchEvent(
    new Event('submit', { bubbles: true, cancelable: true }),
  );
  await vi.waitFor(() => expect(el.textContent).toContain('当前编辑已保留'));
  expect(io.profile).toHaveBeenCalledWith({
    displayName: '新名字',
    email: '',
    avatar: '',
    bio: '',
    rev: 5,
  });
  expect(input.value).toBe('新名字');
});
