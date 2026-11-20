/* eslint-disable vue/one-component-per-file -- Small grid adapter exercises the actual list views. */
import type { Component } from 'vue';

import { createApp, defineComponent, h, onMounted, ref } from 'vue';

import { useAccessStore } from '@vben/stores';

import { createPinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';

const io = vi.hoisted(() => ({ commit: vi.fn(), hosts: vi.fn(), isos: vi.fn(), preview: vi.fn(), requestInventory: vi.fn(), update: vi.fn() }));
vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/hosts', query: {} }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));
vi.mock('#/api/console/host', () => ({
  commitHostCSVApi: io.commit,
  getHostListApi: io.hosts,
  previewHostCSVApi: io.preview,
  requestHostInventoryApi: io.requestInventory,
  updateHostApi: io.update,
}));
vi.mock('#/api/console/artifact', () => ({ getArtifactSetListApi: io.isos }));
vi.mock('@vben/common-ui', () => ({
  Page: defineComponent({
    setup(_, { slots }) {
      return () => h('div', [slots.extra?.(), slots.default?.()]);
    },
  }),
}));
vi.mock('#/adapter/vxe-table', () => ({
  useVbenVxeGrid: (options: {
    gridOptions: {
      proxyConfig: { ajax: { query: () => Promise<{ items: unknown[] }> } };
    };
  }) => {
    const rows = ref<unknown[]>([]);
    const reload = async () => {
      rows.value = (await options.gridOptions.proxyConfig.ajax.query()).items;
    };
    return [
      defineComponent({
        setup(_, { slots }) {
          onMounted(reload);
          return () =>
            h('div', [
              h('button', { onClick: reload }, '刷新表格'),
              h('pre', JSON.stringify(rows.value)),
              ...rows.value.flatMap((row) =>
                row && typeof row === 'object' && 'sn' in row ? slots.action?.({ row }) ?? [] : [],
              ),
            ]);
        },
      }),
      { reload },
    ];
  },
}));

import ISOs from '../artifact-sets/index.vue';
import Hosts from './index.vue';

const cleanup: Array<() => void> = [];
afterEach(() => {
  cleanup.splice(0).forEach((fn) => fn());
  vi.resetAllMocks();
});
function mount(component: Component, canWrite = false) {
  const el = document.createElement('div');
  document.body.append(el);
  const pinia = createPinia();
  const app = createApp(component).use(pinia);
  if (canWrite) useAccessStore(pinia).setAccessCodes(['host:write']);
  app.mount(el);
  cleanup.push(() => {
    app.unmount();
    el.remove();
  });
  return el;
}

it('navigates host pages, keeps the page on refresh, resets filters and restores failed navigation', async () => {
  io.hosts.mockResolvedValue({
    items: [{ sn: 'host-first' }],
    total: 41,
    next: '',
  });
  const el = mount(Hosts);
  await vi.waitFor(() => expect(el.textContent).toContain('host-first'));
  expect(io.hosts).toHaveBeenLastCalledWith({
    prefix: '',
    attr_key: '',
    attr_value: '',
    page: 1,
    page_size: 20,
  });
  io.hosts.mockResolvedValue({
    items: [{ sn: 'host-second' }],
    total: 41,
    next: '',
  });
  const draftFilter = el.querySelector<HTMLInputElement>('form input')!;
  draftFilter.value = 'unsubmitted';
  draftFilter.dispatchEvent(new Event('input', { bubbles: true }));
  el.querySelector<HTMLButtonElement>('.btn-next')!.click();
  await vi.waitFor(() => expect(el.textContent).toContain('host-second'));
  expect(io.hosts).toHaveBeenLastCalledWith(
    expect.objectContaining({ page: 2, prefix: '' }),
  );
  expect(el.textContent).not.toContain('host-first');
  Array.from(el.querySelectorAll('button'))
    .find((button) => button.textContent === '刷新表格')!
    .click();
  await vi.waitFor(() => expect(io.hosts).toHaveBeenCalledTimes(3));
  expect(io.hosts).toHaveBeenLastCalledWith(
    expect.objectContaining({ page: 2 }),
  );
  io.hosts.mockResolvedValue({
    items: [{ sn: 'filtered' }],
    total: 21,
    next: '',
  });
  const input = el.querySelector<HTMLInputElement>('form input')!;
  input.value = 'filtered';
  input.dispatchEvent(new Event('input', { bubbles: true }));
  el.querySelector('form')!.dispatchEvent(
    new Event('submit', { cancelable: true }),
  );
  await vi.waitFor(() => expect(el.textContent).toContain('filtered'));
  expect(io.hosts).toHaveBeenLastCalledWith(
    expect.objectContaining({ page: 1, prefix: 'filtered' }),
  );
  io.hosts.mockRejectedValue(new Error('navigation unavailable'));
  el.querySelector<HTMLButtonElement>('.btn-next')!.click();
  await vi.waitFor(() =>
    expect(el.textContent).toContain('navigation unavailable'),
  );
  expect(el.querySelector('.el-pager .is-active')?.textContent?.trim()).toBe(
    '1',
  );
  expect(el.querySelector('pre')?.textContent).toContain('filtered');
});

it('replaces ISO pages and automatically returns to the valid last page after rows disappear', async () => {
  io.isos.mockResolvedValueOnce({
    items: [{ id: 'iso-first' }],
    total: 21,
    next: '',
  });
  const el = mount(ISOs);
  await vi.waitFor(() => expect(el.textContent).toContain('iso-first'));
  io.isos
    .mockResolvedValueOnce({ items: [], total: 20, next: '' })
    .mockResolvedValueOnce({
      items: [{ id: 'iso-last' }],
      total: 20,
      next: '',
    });
  el.querySelector<HTMLButtonElement>('.btn-next')!.click();
  await vi.waitFor(() => expect(el.textContent).toContain('iso-last'));
  expect(io.isos.mock.calls).toEqual([
    [{ state: '', page: 1, page_size: 20 }],
    [{ state: '', page: 2, page_size: 20 }],
    [{ state: '', page: 1, page_size: 20 }],
  ]);
  expect(el.querySelector('pre')?.textContent).not.toContain('iso-first');
  expect(el.querySelector('.el-pager .is-active')?.textContent?.trim()).toBe(
    '1',
  );
});


it('invalidates a confirmed preview as soon as another file is chosen', async () => {
  io.hosts.mockResolvedValue({ items: [], total: 0, next: '' });
  io.preview.mockResolvedValue({ total: 1, rows: [] });
  io.commit.mockResolvedValue({});
  const el = mount(Hosts, true);
  const button = (label: string) =>
    [...document.querySelectorAll('button')].find((item) => item.textContent?.trim() === label);
  const bytes = (sn: string) => new TextEncoder().encode(`sn\n${sn}\n`);
  button('CSV 导入')!.click();
  const input = await vi.waitFor(() => {
    const file = document.querySelector<HTMLInputElement>('[aria-label="CSV 文件"]');
    expect(file).toBeTruthy();
    return file!;
  });
  const choose = (name: string, arrayBuffer: () => Promise<ArrayBuffer>) => {
    Object.defineProperty(input, 'files', { configurable: true, value: [{ name, arrayBuffer }] });
    input.dispatchEvent(new Event('change'));
  };
  choose('A.csv', async () => bytes('A').buffer);
  await vi.waitFor(() => expect(el.textContent).toContain('A.csv'));
  button('预览 CSV（不写入）')!.click();
  const confirmation = await vi.waitFor(() => {
    const checkbox = document.querySelector<HTMLInputElement>('input[type="checkbox"]');
    expect(checkbox).toBeTruthy();
    return checkbox!;
  });
  confirmation.click();
  await vi.waitFor(() => expect(button('确认导入')?.disabled).toBe(false));

  let resolveB!: (value: ArrayBuffer) => void;
  choose('B.csv', () => new Promise((resolve) => { resolveB = resolve; }));
  await vi.waitFor(() => expect(el.textContent).toContain('B.csv'));
  expect(button('确认导入')?.disabled).toBe(true);
  button('确认导入')!.click();
  expect(io.commit).not.toHaveBeenCalled();

  resolveB(bytes('B').buffer);
  await vi.waitFor(() => expect(button('预览 CSV（不写入）')?.disabled).toBe(false));
  button('预览 CSV（不写入）')!.click();
  const newConfirmation = await vi.waitFor(() => {
    const checkbox = document.querySelector<HTMLInputElement>('input[type="checkbox"]');
    expect(checkbox).toBeTruthy();
    return checkbox!;
  });
  expect(newConfirmation.checked).toBe(false);
  newConfirmation.click();
  await vi.waitFor(() => expect(button('确认导入')?.disabled).toBe(false));
  button('确认导入')!.click();
  await vi.waitFor(() => expect(io.commit).toHaveBeenCalledExactlyOnceWith(bytes('B')));
});


it('requests host inventory only with write access and refreshes after success', async () => {
  const row = { sn: 'inventory-host', rev: 1, attrs: {} };
  io.hosts.mockResolvedValue({ items: [row], total: 1, next: '' });
  const button = (el: HTMLElement) => [...el.querySelectorAll('button')]
    .find((item) => item.textContent?.trim() === '请求硬件采集');
  const viewer = mount(Hosts);
  await vi.waitFor(() => expect(viewer.textContent).toContain('inventory-host'));
  expect(button(viewer)).toBeUndefined();

  const editor = mount(Hosts, true);
  await vi.waitFor(() => expect(button(editor)).toBeDefined());
  io.requestInventory.mockRejectedValueOnce(new Error('主机有未结束任务'));
  button(editor)!.click();
  await vi.waitFor(() => expect(editor.textContent).toContain('主机有未结束任务'));
  expect(io.hosts).toHaveBeenCalledTimes(2);

  io.requestInventory.mockResolvedValueOnce(row);
  button(editor)!.click();
  button(editor)!.click();
  await vi.waitFor(() => expect(io.hosts).toHaveBeenCalledTimes(3));
  expect(io.requestInventory.mock.calls).toEqual([['inventory-host'], ['inventory-host']]);
  expect(editor.textContent).not.toContain('主机有未结束任务');
});

it('keeps hardware data editable but leaves inventory request markers out of host updates', async () => {
  io.hosts.mockResolvedValue({
    items: [{
      sn: 'editable-host',
      rev: 1,
      attrs: {
        inv_request_id: 'request',
        inv_requested_at: '1',
        inv_in_flight_at: '2',
        inv_collected_at: '3',
        inv_cpu: 'sample cpu',
      },
    }],
    total: 1,
    next: '',
  });
  io.update.mockResolvedValue({});
  const el = mount(Hosts, true);
  const edit = await vi.waitFor(() => {
    const button = [...el.querySelectorAll('button')].find((item) => item.textContent?.trim() === '编辑');
    expect(button).toBeDefined();
    return button!;
  });
  edit.click();
  const name = await vi.waitFor(() => {
    const input = el.querySelector<HTMLInputElement>('[aria-label="属性名 1"]');
    expect(input).toBeTruthy();
    return input!;
  });
  expect(name.value).toBe('inv_cpu');
  expect(el.querySelector('[aria-label="属性名 2"]')).toBeNull();
  const save = [...el.querySelectorAll('button')].find((button) => button.textContent?.trim() === '保存主机')!;
  save.click();
  await vi.waitFor(() => expect(io.update).toHaveBeenCalledExactlyOnceWith('editable-host', {
    sn: 'editable-host', rev: 1, attrs: { inv_cpu: 'sample cpu' },
  }));
});
