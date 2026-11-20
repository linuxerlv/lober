import { createApp, h, ref } from 'vue';

import { expect, it, vi } from 'vitest';

import { getHostHardwareApi } from '#/api/console/discovery';

import HostHardware from './HostHardware.vue';

vi.mock('#/api/console/discovery', () => ({ getHostHardwareApi: vi.fn() }));

it('shows the selected host, field reasons, old reports, and retry after failure', async () => {
  const api = vi.mocked(getHostHardwareApi);
  const old = Promise.withResolvers<Awaited<ReturnType<typeof getHostHardwareApi>>>();
  api.mockReturnValueOnce(old.promise).mockRejectedValueOnce({ message: '网络中断' });
  const sn = ref('A');
  const app = createApp({ render: () => h(HostHardware, { sn: sn.value }) });
  const el = document.createElement('div');
  app.mount(el);
  try {
    sn.value = 'B';
    await vi.waitFor(() => expect(el.textContent).toContain('网络中断'));
    old.resolve({ report_at: 0 });
    await Promise.resolve();
    expect(el.textContent).toContain('网络中断');
    api.mockResolvedValueOnce({ report_at: 100, report: { sections: [{
      key: 'storage', title: '磁盘', status: 'partial', reason: '部分读取',
      items: [{ name: '盘 2', fields: { '已用寿命（%）': '0', '未读取·盘位': '机箱未提供槽位映射' } }],
    }] } });
    el.querySelector('button')!.click();
    await vi.waitFor(() => expect(el.textContent).toContain('未读取（机箱未提供槽位映射）'));
    expect(el.textContent).toContain('旧采集格式');
    expect(el.textContent).toContain('最近部件报告');
    expect(el.textContent).toContain('已用寿命（%）0');
    api.mockResolvedValueOnce({ report_at: 0 });
    sn.value = 'C';
    await vi.waitFor(() => expect(el.textContent).toContain('暂无详细部件报告'));
    expect(el.textContent).not.toContain('盘 2');
  } finally {
    app.unmount();
  }
});
