/* eslint-disable vue/one-component-per-file -- Small grid adapter exercises the actual list views. */
import type { Component } from 'vue';

import { createApp, defineComponent, h, onMounted, ref } from 'vue';

import { createPinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';

const io = vi.hoisted(() => ({ hosts: vi.fn(), isos: vi.fn() }));
vi.mock('#/api/console/host', () => ({ getHostListApi: io.hosts }));
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
        setup() {
          onMounted(reload);
          return () =>
            h('div', [
              h('button', { onClick: reload }, '刷新表格'),
              h('pre', JSON.stringify(rows.value)),
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
function mount(component: Component) {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp(component).use(createPinia());
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
