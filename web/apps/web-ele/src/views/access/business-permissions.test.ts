import { createApp, defineComponent, h } from 'vue';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';
import { useAccessStore } from '@vben/stores';
import { afterEach, expect, it, vi } from 'vitest';
const io = vi.hoisted(() => ({
  hosts: vi.fn(),
  tasks: vi.fn(),
  isos: vi.fn(),
  listAuthoring: vi.fn(),
}));
vi.mock('#/api/console/operations', () => ({ operations: io }));
vi.mock('#/api', () => ({
  loginApi: vi.fn(),
  logoutApi: vi.fn(),
  getUserInfoApi: vi.fn(),
}));
import { useAuthStore } from '#/store';
import Hosts from '#/views/hosts/index.vue';
import Tasks from '#/views/tasks/index.vue';
import ISOs from '#/views/artifact-sets/index.vue';
import AuthoringManager from '#/components/authoring-manager.vue';
import type { ConsoleRole } from '#/api/console/session';
const cleanup: Array<() => void> = [];
afterEach(() => {
  cleanup.splice(0).forEach((fn) => fn());
  vi.resetAllMocks();
});
async function mount(
  component: any,
  role: ConsoleRole,
  permissions: string[],
  props = {},
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  });
  await router.push('/');
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp(
    defineComponent({
      setup() {
        useAccessStore().setAccessToken('token');
        useAuthStore().applySessionUser({
          username: 'alice',
          displayName: 'Alice',
          role,
          permissions,
          menuIds: ['hosts', 'tasks', 'isos', 'hooks', 'templates'],
          menuRev: 0,
          rev: 1,
          createdAt: 1,
          disabled: false,
          passwordChangeRequired: false,
        });
        return () => h(component, props);
      },
    }),
  );
  app.use(createPinia());
  app.use(router);
  app.mount(el);
  cleanup.push(() => {
    app.unmount();
    el.remove();
  });
  return el;
}
it('renders current paged read responses for a viewer without any write entry points', async () => {
  io.hosts.mockResolvedValue({
    items: [
      {
        sn: 'SN-1',
        arch: 'x86_64',
        firmware: 'uefi',
        attrs: { rack: 'A-1' },
        rev: 1,
      },
    ],
    next: '',
  });
  const host = await mount(Hosts, 'viewer', ['host:read']);
  await vi.waitFor(() => expect(host.textContent).toContain('SN-1'));
  expect(host.textContent).not.toContain('手工录入主机');
  expect(host.textContent).not.toContain('删除');
  io.tasks.mockResolvedValue({
    items: [
      {
        id: 'task',
        hostSN: 'SN-1',
        templateVersion: 1,
        snapshot: { template: { name: 'Rocky' }, iso: { name: 'Rocky ISO' } },
        state: 'pending',
        createdBy: 'bob',
        createdAt: 1,
        canCancel: [],
        canRetry: [],
      },
    ],
    next: '',
  });
  const tasks = await mount(Tasks, 'viewer', ['task:read']);
  await vi.waitFor(() => expect(tasks.textContent).toContain('Rocky ISO'));
  expect(tasks.textContent).not.toContain('建任务');
  expect(tasks.textContent).not.toContain('取消任务');
  expect(tasks.textContent).not.toContain('重试');
  io.isos.mockResolvedValue({
    items: [
      {
        id: 'iso',
        name: 'Rocky ISO',
        state: 'ready',
        series: 'rocky',
        version: '9',
        arch: 'x86_64',
        rev: 1,
      },
    ],
    next: '',
  });
  const iso = await mount(ISOs, 'viewer', ['iso:read']);
  await vi.waitFor(() => expect(iso.textContent).toContain('Rocky ISO'));
  expect(iso.textContent).not.toContain('登记制品集');
  expect(iso.textContent).not.toContain('废弃');
});
it.each([
  ['hook-author', ['hook:read', 'hook:write'], true, false],
  ['hook-approver', ['hook:read', 'hook:approve'], false, true],
  ['viewer', ['hook:read'], false, false],
  ['admin', ['hook:read', 'hook:write', 'hook:approve'], true, true],
] as const)(
  'shows the correct author and approval controls for %s',
  async (role, permissions, write, approve) => {
    io.listAuthoring.mockResolvedValue({
      items: [
        {
          id: 'draft',
          version: 1,
          name: 'Draft',
          state: 'draft',
          author: 'bob',
          builtin: false,
          rev: 1,
          params: [],
          impls: [],
          problems: [],
        },
        {
          id: 'review',
          version: 1,
          name: 'Review',
          state: 'review',
          author: 'bob',
          builtin: false,
          rev: 1,
          params: [],
          impls: [],
          problems: [],
        },
      ],
      next: '',
    });
    const el = await mount(AuthoringManager, role, [...permissions], {
      kind: 'hooks',
    });
    await vi.waitFor(() => expect(el.textContent).toContain('Review'));
    expect(el.textContent!.includes('新建hook')).toBe(write);
    expect(el.textContent!.includes('提交审批')).toBe(write);
    expect(el.textContent!.includes('批准')).toBe(approve);
  },
);
