/* eslint-disable vue/one-component-per-file -- Mount wrapper exercises HostDiscovery component. */
import { createApp, h, nextTick } from 'vue';

import { afterEach, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({
  getDiscoveryApi: vi.fn(),
  getDiscoveryListApi: vi.fn(),
  registerDiscoveryApi: vi.fn(),
}));
vi.mock('#/api/console/discovery', () => api);

import HostDiscovery from './HostDiscovery.vue';

const cleanup: Array<() => void> = [];
afterEach(() => {
  cleanup.splice(0).forEach((dispose) => dispose());
  vi.resetAllMocks();
});

const summary = {
  arch: 'x86_64',
  firmware: 'uefi',
  first_seen_at: 1_750_000_000,
  last_seen_at: 1_750_000_001,
  mac: '52:54:00:00:11:22',
  report_at: 1_750_000_000,
  status: 'timed_out',
  system_model: '示例服务器',
};
const discovery = {
  ...summary,
  report: {
    sections: [
      {
        items: [{ fields: { 容量: '16 GB' }, name: 'DIMM A1' }],
        key: 'memory',
        reason: '无法读取序列号',
        status: 'partial',
        title: '内存',
      },
      {
        items: [],
        key: 'power',
        status: 'unread',
        title: '电源',
      },
    ],
  },
};

function mount(canWrite: boolean) {
  const el = document.createElement('div');
  const registered = vi.fn();
  const app = createApp({
    render: () =>
      h(HostDiscovery, { canWrite, modelValue: true, onRegistered: registered }),
  });
  document.body.append(el);
  app.mount(el);
  cleanup.push(() => {
    app.unmount();
    el.remove();
  });
  return { el, registered };
}

function button(el: HTMLElement, label: string) {
  const match = [...el.querySelectorAll('button')].find(
    (element) => element.textContent?.trim() === label,
  );
  expect(match).toBeDefined();
  return match!;
}

it('shows a timed-out boot and identifies incomplete sections in its earlier report', async () => {
  api.getDiscoveryListApi.mockResolvedValue({ items: [summary] });
  api.getDiscoveryApi.mockResolvedValue(discovery);
  const { el } = mount(false);
  await vi.waitFor(() => expect(el.textContent).toContain('硬件报告未收到'));
  expect(el.textContent).toContain('示例服务器');
  expect(api.getDiscoveryApi).not.toHaveBeenCalled();
  button(el, '查看详情').click();
  await vi.waitFor(() => expect(el.textContent).toContain('DIMM A1'));

  expect(el.textContent).toContain('保留的上次报告');
  expect(el.textContent).toContain('硬件报告未收到');
  expect(el.textContent).toContain('无法读取序列号');
  expect(el.textContent).toContain('部分读取');
  expect(el.textContent).toContain('未读取');
  expect(el.querySelector('form')).toBeNull();
  expect(api.registerDiscoveryApi).not.toHaveBeenCalled();
});

it('requires an administrator to confirm a serial number and uses it to register', async () => {
  api.getDiscoveryListApi.mockResolvedValue({ items: [summary] });
  api.getDiscoveryApi.mockResolvedValue(discovery);
  api.registerDiscoveryApi.mockResolvedValue({ sn: 'LAB-42' });
  const { el, registered } = mount(true);
  await vi.waitFor(() => expect(el.textContent).toContain(discovery.mac));
  button(el, '查看详情').click();
  await nextTick();

  expect(button(el, '确认登记').disabled).toBe(true);
  const input = el.querySelector<HTMLInputElement>('input')!;
  input.value = '  LAB-42  ';
  input.dispatchEvent(new Event('input', { bubbles: true }));
  await nextTick();
  button(el, '确认登记').click();

  await vi.waitFor(() =>
    expect(api.registerDiscoveryApi).toHaveBeenCalledExactlyOnceWith(
      discovery.mac,
      'LAB-42',
    ),
  );
  expect(registered).toHaveBeenCalledExactlyOnceWith('LAB-42');
  await vi.waitFor(() => expect(el.textContent).toContain('已登记为 LAB-42'));
});

it('shows create install task button after registration and navigates to wizard with preselected host when permitted', async () => {
  api.getDiscoveryListApi.mockResolvedValue({ items: [summary] });
  api.getDiscoveryApi.mockResolvedValue(discovery);
  api.registerDiscoveryApi.mockResolvedValue({ sn: 'LAB-42' });
  const createTask = vi.fn();
  const el = document.createElement('div');
  const app = createApp({
    render: () =>
      h(HostDiscovery, {
        canCreateTask: true,
        canWrite: true,
        modelValue: true,
        onCreateTask: createTask,
        onRegistered: vi.fn(),
      }),
  });
  document.body.append(el);
  app.mount(el);
  cleanup.push(() => {
    app.unmount();
    el.remove();
  });

  await vi.waitFor(() => expect(el.textContent).toContain(discovery.mac));
  button(el, '查看详情').click();
  await nextTick();
  expect(
    [...el.querySelectorAll('button')].some(
      (b) => b.textContent?.trim() === '为这台主机创建装机任务',
    ),
  ).toBe(false);

  const input = el.querySelector<HTMLInputElement>('input')!;
  input.value = 'LAB-42';
  input.dispatchEvent(new Event('input', { bubbles: true }));
  await nextTick();
  button(el, '确认登记').click();

  await vi.waitFor(() =>
    expect(el.textContent).toContain('为这台主机创建装机任务'),
  );
  button(el, '为这台主机创建装机任务').click();
  expect(createTask).toHaveBeenCalledExactlyOnceWith('LAB-42');
});

it('keeps the serial number available for correction if the host already exists', async () => {
  api.getDiscoveryListApi.mockResolvedValue({
    items: [{ ...summary, sn: 'LAB-42' }],
  });
  api.getDiscoveryApi.mockResolvedValue({ ...discovery, sn: 'LAB-42' });
  api.registerDiscoveryApi.mockRejectedValue({
    response: { data: { message: '主机序列号已登记' } },
  });
  const { el, registered } = mount(true);
  await vi.waitFor(() => expect(el.textContent).toContain(discovery.mac));
  button(el, '查看详情').click();
  await vi.waitFor(() => expect(button(el, '确认登记').disabled).toBe(false));
  button(el, '确认登记').click();

  await vi.waitFor(() => expect(el.textContent).toContain('主机序列号已登记'));
  expect(el.querySelector<HTMLInputElement>('input')?.value).toBe('LAB-42');
  expect(registered).not.toHaveBeenCalled();
});
