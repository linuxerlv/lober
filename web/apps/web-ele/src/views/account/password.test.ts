import { createApp, nextTick } from 'vue';

import { afterEach, expect, it, vi } from 'vitest';

const io = vi.hoisted(() => ({
  clearSession: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}));
vi.mock('@vben/common-ui', async () => {
  const { defineComponent, h } = await import('vue');
  return { Page: defineComponent({ setup: (_, { slots }) => () => h('main', slots.default?.()) }) };
});
vi.mock('#/api/request', () => ({ requestClient: io }));
vi.mock('#/store', () => ({ useAuthStore: () => ({ clearSession: io.clearSession }) }));
vi.mock('@vben/stores', () => ({
  useUserStore: () => ({ userInfo: { passwordChangeRequired: true } }),
}));

import Password from './password.vue';

let unmount: (() => void) | undefined;
afterEach(() => {
  unmount?.();
  document.body.innerHTML = '';
  vi.resetAllMocks();
});

it('requires the current password, uses the personal endpoint and clears the session after success', async () => {
  const root = document.createElement('div');
  document.body.append(root);
  const app = createApp(Password);
  app.mount(root);
  unmount = () => app.unmount();

  const enter = (input: HTMLInputElement, value: string) => {
    input.value = value;
    input.dispatchEvent(new Event('input', { bubbles: true }));
  };
  const [password, confirmation] = root.querySelectorAll<HTMLInputElement>('[autocomplete="new-password"]');
  enter(password!, 'new-test-password');
  enter(confirmation!, 'new-test-password');
  await nextTick();
  const button = [...root.querySelectorAll('button')].find(
    (element) => element.textContent?.trim() === '保存并重新登录',
  )!;
  expect(button.disabled).toBe(true);
  expect(io.post).not.toHaveBeenCalled();

  const current = root.querySelector<HTMLInputElement>('[autocomplete="current-password"]')!;
  enter(current, 'old-test-password');
  await nextTick();
  button.click();
  await vi.waitFor(() => expect(io.clearSession).toHaveBeenCalledExactlyOnceWith(false));
  expect(io.post).toHaveBeenCalledExactlyOnceWith('/auth/password', {
    currentPassword: 'old-test-password', newPassword: 'new-test-password',
  });
  expect(io.get).not.toHaveBeenCalled();
  expect(io.put).not.toHaveBeenCalled();
});
