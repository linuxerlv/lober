import { createApp, defineComponent, h, nextTick } from 'vue';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';
import { useAccessStore } from '@vben/stores';
import { afterEach, expect, it, vi } from 'vitest';
vi.mock('#/api', () => ({
  loginApi: vi.fn(),
  logoutApi: vi.fn(),
  getUserInfoApi: vi.fn(),
}));
import AccessButton from '#/components/access-button.vue';
import { useAuthStore } from '#/store';
import { useConsoleAccess } from '#/composables/use-console-access';
const cleanup: Array<() => void> = [];
afterEach(() => cleanup.splice(0).forEach((fn) => fn()));
it('hides unauthorized buttons and rechecks permission before a deferred action runs', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  });
  await router.push('/');
  const pinia = createPinia();
  let auth!: ReturnType<typeof useAuthStore>,
    access!: ReturnType<typeof useConsoleAccess>;
  const clicked = vi.fn();
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp(
    defineComponent({
      setup() {
        useAccessStore().setAccessToken('token');
        auth = useAuthStore();
        access = useConsoleAccess();
        auth.applySessionUser({
          username: 'author',
          displayName: 'Author',
          role: 'hook-author',
          permissions: ['hook:write', 'template:write'],
          menuIds: ['hooks', 'templates'],
          menuRev: 0,
          rev: 1,
          createdAt: 1,
          disabled: false,
          passwordChangeRequired: false,
        });
        return () =>
          h('div', [
            h(
              AccessButton,
              { permission: 'hook:write', onClick: clicked },
              () => '新建 hook',
            ),
            h(AccessButton, { permission: 'hook:approve' }, () => '审批'),
          ]);
      },
    }),
  );
  app.use(pinia);
  app.use(router);
  app.mount(el);
  cleanup.push(() => {
    app.unmount();
    el.remove();
  });
  expect(el.textContent).toContain('新建 hook');
  expect(el.textContent).not.toContain('审批');
  el.querySelector('button')!.click();
  expect(clicked).toHaveBeenCalledTimes(1);
  auth.applySessionUser({
    ...auth.sessionUser!,
    role: 'viewer',
    permissions: ['hook:read'],
  });
  await nextTick();
  expect(el.querySelector('button')).toBeNull();
  const write = vi.fn(async () => 42);
  await access.run('hook:write', write);
  expect(write).not.toHaveBeenCalled();
});
