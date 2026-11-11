import { createApp, nextTick } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';

import { useAccessStore } from '@vben/stores';

import { createPinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';
const io = vi.hoisted(() => ({
  changePasswordApi: vi.fn(),
  loginApi: vi.fn(),
  logoutApi: vi.fn(),
  getUserInfoApi: vi.fn(),
}));
vi.mock('#/api', () => io);
import ChangePassword from './change-password.vue';
const cleanup: Array<() => void> = [];
afterEach(() => {
  cleanup.splice(0).forEach((fn) => fn());
  vi.resetAllMocks();
});
async function setup() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/account/password', component: ChangePassword },
      { path: '/auth/login', component: { template: '<div />' } },
    ],
  });
  await router.push('/account/password');
  const el = document.createElement('div');
  document.body.append(el);
  const pinia = createPinia();
  const app = createApp(ChangePassword);
  app.use(pinia);
  app.use(router);
  app.mount(el);
  cleanup.push(() => {
    app.unmount();
    el.remove();
  });
  const access = useAccessStore(pinia);
  access.setAccessToken('old-session');
  for (const [name, value] of Object.entries({
    currentPassword: 'old',
    newPassword: 'new',
    confirmPassword: 'new',
  })) {
    const input = el.querySelector<HTMLInputElement>(`input[name="${name}"]`)!;
    input.value = value;
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }
  await nextTick();
  return { el, access, router };
}
it('changes the current password and returns to login with the old local session cleared', async () => {
  const { el, access, router } = await setup();
  io.changePasswordApi.mockResolvedValue(null);
  el.querySelector('form')!.dispatchEvent(
    new Event('submit', { bubbles: true, cancelable: true }),
  );
  await vi.waitFor(() =>
    expect(router.currentRoute.value.path).toBe('/auth/login'),
  );
  expect(access.accessToken).toBeNull();
  expect(io.changePasswordApi).toHaveBeenCalledWith({
    currentPassword: 'old',
    newPassword: 'new',
  });
});
it('retains inputs and session after a failed password change', async () => {
  const { el, access } = await setup();
  io.changePasswordApi.mockRejectedValue(new Error('无法连接服务器'));
  el.querySelector('form')!.dispatchEvent(
    new Event('submit', { bubbles: true, cancelable: true }),
  );
  await vi.waitFor(() => expect(el.textContent).toContain('无法连接服务器'));
  expect(access.accessToken).toBe('old-session');
  expect(
    el.querySelector<HTMLInputElement>('input[name="newPassword"]')!.value,
  ).toBe('new');
});
