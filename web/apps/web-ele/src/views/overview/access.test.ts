import { createApp, nextTick, ref } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';

import { createPinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';

import Analytics from './analytics.vue';
import Workspace from './workspace.vue';

const io = vi.hoisted(() => ({
  audit: vi.fn(),
  discoveries: vi.fn(),
  hooks: vi.fn(),
  hosts: vi.fn(),
  isos: vi.fn(),
  tasks: vi.fn(),
  templates: vi.fn(),
}));
vi.mock('#/api/console/audit', () => ({ listAudit: io.audit }));
vi.mock('#/api/console/discovery', () => ({
  getDiscoveryListApi: io.discoveries,
}));
vi.mock('#/api/console/hook', () => ({ getHookListApi: io.hooks }));
vi.mock('#/api/console/host', () => ({ getHostListApi: io.hosts }));
vi.mock('#/api/console/artifact', () => ({ getArtifactSetListApi: io.isos }));
vi.mock('#/api/console/task', () => ({ taskAPI: { list: io.tasks } }));
vi.mock('#/api/console/template', () => ({ getTemplateListApi: io.templates }));
vi.mock('#/store', () => ({
  useAuthStore: () => ({ sessionUser: { role: 'viewer' } }),
}));
vi.mock('@vben/plugins/echarts', () => ({
  EchartsUI: { render: () => null },
  useEcharts: () => ({ renderEcharts: vi.fn() }),
}));
const menus = ref<string[]>([]);
const permissions = ref<string[]>([]);
vi.mock('#/composables/use-console-access', () => ({
  useConsoleAccess: () => ({
    hasMenu: (id: string) => menus.value.includes(id),
    can: (permission: string) => permissions.value.includes(permission),
  }),
}));
const cleanup: Array<() => void> = [];
afterEach(() => {
  cleanup.splice(0).forEach((fn) => fn());
  vi.resetAllMocks();
});
async function mount(page: typeof Analytics | typeof Workspace) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: page },
      { path: '/hooks', component: { render: () => null } },
    ],
  });
  await router.push('/');
  for (const fn of Object.values(io))
    fn.mockResolvedValue({ items: [], total: 0 });
  const app = createApp(page);
  app.use(createPinia());
  app.use(router);
  const el = document.createElement('div');
  app.mount(el);
  cleanup.push(() => app.unmount());
  await nextTick();
  return el;
}
it('shows only granted workspace links and skips restricted audit requests', async () => {
  menus.value = ['overview-workspace', 'hosts'];
  permissions.value = ['task:read', 'host:read'];
  const el = await mount(Workspace);
  expect(el.textContent).toContain('主机资产与发现');
  expect(el.textContent).not.toContain('装机任务监控');
  expect(el.textContent).not.toContain('最近操作动态');
  expect(io.audit).not.toHaveBeenCalled();
  expect(io.templates).not.toHaveBeenCalled();
  expect(io.discoveries).toHaveBeenCalled();
});
it('offers only authorized destinations on analytics cards', async () => {
  menus.value = ['overview-analytics', 'hooks'];
  permissions.value = ['task:read', 'hook:read'];
  const el = await mount(Analytics);
  const links = [...el.querySelectorAll('a')].map((link) =>
    link.getAttribute('href'),
  );
  expect(links).toEqual(['/hooks']);
  expect(io.hooks).toHaveBeenCalled();
  expect(io.templates).not.toHaveBeenCalled();
  expect(io.hosts).not.toHaveBeenCalled();
});

it('discards an audit response after the reports grant is revoked and restored', async () => {
  menus.value = ['reports', 'overview-workspace'];
  permissions.value = ['task:read', 'audit:read'];
  let finish!: (value: unknown) => void;
  io.audit.mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  const el = await mount(Workspace);
  expect(el.textContent).toContain('最近操作动态');
  menus.value = ['overview-workspace'];
  await nextTick();
  expect(el.textContent).not.toContain('最近操作动态');
  menus.value = ['reports', 'overview-workspace'];
  await nextTick();
  finish({
    items: [{ id: 'old', detail: '上次授权的记录', at: 1, actor: 'old' }],
  });
  await nextTick();
  await nextTick();
  expect(io.audit).toHaveBeenCalledTimes(2);
  expect(el.textContent).not.toContain('上次授权的记录');
});
