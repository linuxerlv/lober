/* eslint-disable vue/one-component-per-file -- Lightweight table adapters exercise the actual view permissions. */
import { createApp, defineComponent, h } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';

import { useAccessStore } from '@vben/stores';

import { createPinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';
const io = vi.hoisted(() => ({
  hosts: vi.fn(),
  tasks: vi.fn(),
  isos: vi.fn(),
  listAuthoring: vi.fn(),
}));

vi.mock('#/api/console/host',()=>({getHostListApi:io.hosts}));
vi.mock('#/api/console/task',()=>({taskAPI:{list:io.tasks}}));
vi.mock('#/api/console/artifact',()=>({getArtifactSetListApi:io.isos}));
vi.mock('#/adapter/vxe-table',async()=>{
 const {defineComponent,h,ref,onMounted}=await import('vue');
 return {useVbenVxeGrid:(options:any)=>{
  const data=ref<any[]>([]);
  return [defineComponent({setup(_,{slots}){onMounted(async()=>{const query=options.gridOptions.proxyConfig?.ajax?.query;if(query)data.value=(await query()).items||[];});return()=>h('div',data.value.map(row=>h('div',[JSON.stringify(row),slots.action?.({row})])));}}),{setGridOptions:({data:items}:any)=>{data.value=items;},reload:async()=>{},loadData:async()=>{}}];
 }};
});
vi.mock('#/api', () => ({
  loginApi: vi.fn(),
  logoutApi: vi.fn(),
  getUserInfoApi: vi.fn(),
}));
import type { ConsoleRole } from '#/api/console/session';

import { useAuthStore } from '#/store';
import ISOs from '#/views/artifact-sets/index.vue';
import Hosts from '#/views/hosts/index.vue';
import Tasks from '#/views/tasks/index.vue';
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
    total: 1,
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
    total: 1,
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
    total: 1,
  });
  const iso = await mount(ISOs, 'viewer', ['iso:read']);
  await vi.waitFor(() => expect(iso.textContent).toContain('Rocky ISO'));
  expect(iso.textContent).not.toContain('登记制品集');
  expect(iso.textContent).not.toContain('废弃');
});
