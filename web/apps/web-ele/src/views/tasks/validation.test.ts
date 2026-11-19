/* eslint-disable vue/one-component-per-file -- Test adapters exercise the real page's async flow. */
import { createApp, nextTick } from 'vue';

import { afterEach, expect, it, vi } from 'vitest';

import { wizardAPI } from '#/api/console/wizard';

import Wizard from './new.vue';
const flow = vi.hoisted(() => ({
  pending: undefined as ((x: unknown) => void) | undefined,
  calls: 0,
  routeQuery: {} as Record<string, string>,
  checkedHosts: [{ sn: 'H1', attrs: {}, selectable: true }] as Array<{
    attrs?: Record<string, string>;
    selectable: boolean;
    sn: string;
  }>,
  warning: vi.fn(),
  hostDetail: vi.fn(),
  discoveryList: vi.fn(),
  discoveryDetail: vi.fn(),
}));
vi.mock('vue-router', () => ({
  onBeforeRouteLeave: vi.fn(),
  useRoute: () => ({ query: flow.routeQuery }),
  useRouter: () => ({ push: vi.fn() }),
}));
vi.mock('@vben/stores', () => ({
  useAccessStore: () => ({ accessCodes: ['task:create'] }),
}));
vi.mock('@vben/common-ui', async () => {
  const { defineComponent, h } = await import('vue');
  return {
    Page: defineComponent({
      setup: (_, ctx) => () => h('main', [ctx.slots.default?.(), ctx.slots.footer?.()]),
    }),
  };
});
vi.mock('#/adapter/vxe-table', async () => {
  const { defineComponent, h } = await import('vue');
  return {
    useVbenVxeGrid: (options: {
      gridEvents: { checkboxChange: () => void };
    }) => [
      defineComponent({
        setup: () => () =>
          h(
            'button',
            { onClick: () => options.gridEvents.checkboxChange() },
            '选择测试主机',
          ),
      }),
      {
        setGridOptions: vi.fn(),
        grid: {
          getCheckboxRecords: () => flow.checkedHosts,
          clearCheckboxRow: vi.fn(),
          setCheckboxRow: vi.fn(),
        },
      },
    ],
  };
});
vi.mock('element-plus', async () => {
  const { defineComponent, h, provide, inject } = await import('vue');
  const Container = defineComponent({
    setup: (_, ctx) => () => h('div', ctx.slots.default?.()),
  });
  const Input = defineComponent({
    props: { modelValue: { type: String, default: '' }, disabled: Boolean },
    emits: ['update:modelValue'],
    setup: (props, ctx) => () =>
      h('input', {
        ...ctx.attrs,
        value: props.modelValue,
        disabled: props.disabled,
        onInput: (e: Event) =>
          ctx.emit('update:modelValue', (e.target as HTMLInputElement).value),
      }),
  });
  const RadioGroup = defineComponent({
    props: { modelValue: { type: String, default: '' } },
    emits: ['update:modelValue', 'change'],
    setup: (_, ctx) => {
      provide('choose', (value: string) => {
        ctx.emit('update:modelValue', value);
        ctx.emit('change', value);
      });
      return () => h('div', ctx.slots.default?.());
    },
  });
  const Radio = defineComponent({
    props: { value: { type: String, default: '' }, disabled: Boolean },
    setup: (props, ctx) => {
      const choose = inject<(x: string) => void>('choose')!;
      return () =>
        h(
          'button',
          {
            disabled: props.disabled,
            onClick: () => choose(props.value ?? ''),
          },
          ctx.slots.default?.(),
        );
    },
  });
  return {
    ElAlert: defineComponent({
      props: { title: { type: String, default: '' } },
      setup: (props) => () => h('aside', props.title),
    }),
    ElButton: Container,
    ElCard: Container,
    ElDialog: Container,
    ElInput: Input,
    ElMessage: { warning: flow.warning, success: vi.fn() },
    ElOption: Container,
    ElPagination: Container,
    ElRadio: Radio,
    ElRadioGroup: RadioGroup,
    ElSelect: Input,
    ElStep: Container,
    ElSteps: Container,
    ElTabPane: Container,
    ElTabs: Container,
    ElTag: Container,
  };
});
vi.mock('#/api/console/host', () => ({
  getHostApi: flow.hostDetail,
}));
vi.mock('#/api/console/discovery', () => ({
  getDiscoveryListApi: flow.discoveryList,
  getDiscoveryApi: flow.discoveryDetail,
}));
vi.mock('#/api/console/wizard', () => ({
  wizardAPI: {
    hosts: async () => ({
      items: flow.checkedHosts,
      next: '',
      total: flow.checkedHosts.length,
    }),
    isos: async () => ({
      items: [{ id: 'iso1', name: '选择测试 ISO', usable: true, problems: [] }],
      next: '',
      total: 1,
    }),
    templates: async () => ({
      items: [
        {
          id: 'tpl1',
          version: 1,
          name: '选择测试模板',
          usable: true,
          problems: [],
        },
      ],
      next: '',
      total: 1,
    }),
    params: () => {
      flow.calls++;
      const result = {
        common: [],
        hosts: [
          {
            sn: 'H1',
            fields: [
              {
                key: 'tpl/name',
                label: '主机名',
                locked: false,
                prefill: 'original',
                list: false,
                options: [],
              },
            ],
            problems: [],
          },
        ],
      };
      return flow.calls === 1
        ? Promise.resolve(result)
        : new Promise((resolve) => {
            flow.pending = resolve;
          });
    },
    preview: vi.fn(),
    submit: vi.fn(),
  },
}));
let stop: (() => void) | undefined;
afterEach(() => {
  stop?.();
  vi.restoreAllMocks();
  document.body.innerHTML = '';
  flow.calls = 0;
  flow.pending = undefined;
  flow.routeQuery = {};
  flow.checkedHosts = [{ sn: 'H1', attrs: {}, selectable: true }];
  flow.warning.mockReset();
  flow.hostDetail.mockReset();
  flow.discoveryList.mockReset();
  flow.discoveryDetail.mockReset();
});
async function settle() {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await nextTick();
}
async function click(text: string) {
  const element = [
    ...document.querySelectorAll<HTMLElement>('button,div'),
  ].find(
    (el) =>
      el.textContent?.trim() === text ||
      (el.tagName === 'BUTTON' && !!el.textContent?.trim().startsWith(text)),
  );
  expect(element, `control ${text}`).toBeTruthy();
  element!.click();
  await settle();
}
it('freezes parameter and column controls during server validation', async () => {
  const mount = document.createElement('div');
  document.body.append(mount);
  const app = createApp(Wizard);
  app.mount(mount);
  stop = () => app.unmount();
  await settle();
  await click('选择测试主机');
  await click('下一步');
  await click('选择测试 ISO');
  await click('选择测试模板');
  await click('下一步');
  const field = document.getElementById('H1:tpl/name') as HTMLInputElement;
  expect(field.value).toBe('original');
  await click('校验并确认');
  expect(flow.pending).toBeTypeOf('function');
  expect(field.disabled).toBe(true);
  expect(
    (
      document.querySelector(
        'input[placeholder="整列值；列表每行一项"]',
      ) as HTMLInputElement
    ).disabled,
  ).toBe(true);
  flow.pending!({
    common: [],
    hosts: [
      {
        sn: 'H1',
        fields: [
          {
            key: 'tpl/name',
            label: '主机名',
            locked: false,
            prefill: 'original',
          },
        ],
        problems: [],
      },
    ],
  });
  await settle();
  expect(document.body.textContent).toContain('创建整批任务');
});

it('clears a failed preview diagnostic when retry succeeds', async () => {
  const mount = document.createElement('div');
  document.body.append(mount);
  const app = createApp(Wizard);
  app.mount(mount);
  stop = () => app.unmount();
  await settle();
  await click('选择测试主机');
  await click('下一步');
  await click('选择测试 ISO');
  await click('选择测试模板');
  await click('下一步');
  await click('校验并确认');
  flow.pending!({
    common: [],
    hosts: [{ sn: 'H1', fields: [], problems: [] }],
  });
  await settle();
  vi.mocked(wizardAPI.preview).mockRejectedValueOnce({
    message: 'fixture unavailable',
  });
  await click('预览该主机全部文件');
  expect(document.body.textContent).toContain('fixture unavailable');
  vi.mocked(wizardAPI.preview).mockResolvedValueOnce({
    files: [],
    problems: [],
    sample: [],
  });
  await click('预览该主机全部文件');
  expect(document.body.textContent).not.toContain('fixture unavailable');
});

it('updates parameter errors after editing and ignores an older response', async () => {
  const result = (
    message?: string,
  ): Awaited<ReturnType<typeof wizardAPI.params>> => ({
    common: [],
    hosts: [
      {
        sn: 'H1',
        fields: [
          {
            key: 'tpl/name',
            label: '主机名',
            list: false,
            locked: false,
            options: [],
            prefill: '',
            required: true,
            type: 'hostname',
          },
        ],
        problems: message
          ? [
              {
                code: 'invalid',
                message,
                next: '',
                subject: { col: 0, id: 'H1', kind: 'param', line: 0, path: 'tpl/name' },
              },
            ]
          : [],
      },
    ],
  });
  const replies: Array<(value: ReturnType<typeof result>) => void> = [];
  vi.spyOn(wizardAPI, 'params')
    .mockResolvedValueOnce(result('请填写主机名'))
    .mockImplementation(() => new Promise((resolve) => replies.push(resolve)));

  const mount = document.createElement('div');
  document.body.append(mount);
  const app = createApp(Wizard);
  app.mount(mount);
  stop = () => app.unmount();
  await settle();
  await click('选择测试主机');
  await click('下一步');
  await click('选择测试 ISO');
  await click('选择测试模板');
  await click('下一步');
  expect(mount.textContent).toContain('请填写主机名');
  expect(mount.textContent).toContain('1 台主机有参数需要修改');

  function edit(value: string) {
    const input = document.getElementById('H1:tpl/name') as HTMLInputElement;
    input.value = value;
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }
  edit('host-01');
  await vi.waitFor(() => expect(replies).toHaveLength(1));
  replies[0]!(result());
  await settle();
  expect(mount.textContent).not.toContain('请填写主机名');
  expect(mount.textContent).not.toContain('台主机有参数需要修改');
  expect(mount.textContent).toContain('校验并确认');

  edit('invalid name');
  await vi.waitFor(() => expect(replies).toHaveLength(2));
  replies[1]!(result('主机名不能包含空格'));
  await settle();
  expect(mount.textContent).toContain('主机名不能包含空格');
  expect(mount.textContent).toContain('1 台主机有参数需要修改');

  edit('older edit');
  await vi.waitFor(() => expect(replies).toHaveLength(3));
  edit('host-02');
  await settle();
  replies[2]!(result('旧输入的校验结果'));
  await settle();
  expect(mount.textContent).not.toContain('旧输入的校验结果');
  await vi.waitFor(() => expect(replies).toHaveLength(4));
  replies[3]!(result());
  await settle();
  expect(mount.textContent).not.toContain('台主机有参数需要修改');
  expect(
    (document.getElementById('H1:tpl/name') as HTMLInputElement).value,
  ).toBe('host-02');
});

it('shows disk candidates from hardware report, warns on default disk mismatch without auto-selecting, updates target disk, and shows it on confirmation', async () => {
  flow.hostDetail.mockResolvedValue({
    sn: 'H1',
    arch: 'x86_64',
    firmware: 'uefi',
    attrs: {
      inv_disks: 'nvme0n1:480000000000:Intel NVMe\nnvme1n1:960000000000:Samsung NVMe',
    },
    rev: 1,
  });
  flow.discoveryList.mockResolvedValue({
    items: [{ mac: '52:54:00:11:22:33', registered_sn: 'H1', status: 'received' }],
  });
  flow.discoveryDetail.mockResolvedValue({
    mac: '52:54:00:11:22:33',
    registered_sn: 'H1',
    status: 'received',
    report: {
      sections: [
        {
          key: 'storage',
          title: '磁盘和阵列',
          status: 'read',
          items: [
            {
              name: 'nvme0n1',
              fields: {
                类型: '磁盘',
                '容量（字节）': '480000000000',
                序列号: 'SN-NVME-01',
              },
            },
            {
              name: 'nvme1n1',
              fields: {
                类型: '磁盘',
                '容量（字节）': '960000000000',
                序列号: 'SN-NVME-02',
              },
            },
          ],
        },
      ],
    },
  });
  vi.spyOn(wizardAPI, 'params').mockResolvedValue({
    common: [],
    hosts: [
      {
        sn: 'H1',
        fields: [
          {
            key: 'partition/1/disk',
            label: '安装目标盘',
            list: false,
            locked: false,
            options: [],
            prefill: 'sda',
            required: true,
            type: 'word',
          },
        ],
        problems: [],
      },
    ],
  });

  const mount = document.createElement('div');
  document.body.append(mount);
  const app = createApp(Wizard);
  app.mount(mount);
  stop = () => app.unmount();
  await settle();
  await click('选择测试主机');
  await click('下一步');
  await click('选择测试 ISO');
  await click('选择测试模板');
  await click('下一步');
  await settle();

  const diskInput = document.getElementById('H1:partition/1/disk') as HTMLInputElement;
  // Must not silently replace 'sda' with nvme0n1
  expect(diskInput.value).toBe('sda');
  expect(mount.textContent).toContain('默认目标盘');
  expect(mount.textContent).toContain('nvme0n1');
  expect(mount.textContent).toContain('480 GB');
  expect(mount.textContent).toContain('SN-NVME-01');

  // Explicitly select nvme1n1 from candidate options
  await click('nvme1n1 · 960 GB · 序列号 SN-NVME-02');
  expect(diskInput.value).toBe('nvme1n1');

  await click('校验并确认');
  await settle();
  expect(mount.textContent).toContain('创建整批任务');
  expect(mount.textContent).toContain('目标盘：nvme1n1');
  expect(mount.textContent).toContain('960 GB');
  expect(mount.textContent).toContain('SN-NVME-02');
});

it('safely preselects a requested registered host and warns without selecting when requested host is unregistered or not selectable', async () => {
  // 1. Valid registered and selectable host is preselected
  flow.routeQuery = { host: 'H1' };
  vi.spyOn(wizardAPI, 'hosts').mockResolvedValueOnce({
    items: [
      { sn: 'H1', attrs: {}, selectable: true } as never,
      { sn: 'H1-OTHER', attrs: {}, selectable: true } as never,
    ],
    next: '',
    total: 2,
  });

  const mount1 = document.createElement('div');
  document.body.append(mount1);
  const app1 = createApp(Wizard);
  app1.mount(mount1);
  await settle();
  expect(mount1.textContent).toContain('已选 1 台');
  expect(flow.warning).not.toHaveBeenCalled();
  app1.unmount();
  mount1.remove();

  // 2. Registered but unselectable host (e.g. open task) is not preselected and shows warning
  flow.routeQuery = { host: 'H-BUSY' };
  vi.spyOn(wizardAPI, 'hosts').mockResolvedValueOnce({
    items: [
      {
        sn: 'H-BUSY',
        attrs: {},
        selectable: false,
        problems: [{ message: '该主机存在未结束的任务' }],
      } as never,
    ],
    next: '',
    total: 1,
  });

  const mount2 = document.createElement('div');
  document.body.append(mount2);
  const app2 = createApp(Wizard);
  app2.mount(mount2);
  await settle();
  expect(flow.warning).toHaveBeenCalledWith('该主机存在未结束的任务');
  expect(mount2.textContent).toContain('已选 0 台');
  app2.unmount();
  mount2.remove();
  flow.warning.mockReset();

  // 3. Unregistered host is not preselected even if prefix matches another host
  flow.routeQuery = { host: 'UNREGISTERED-99' };
  vi.spyOn(wizardAPI, 'hosts').mockResolvedValueOnce({
    items: [{ sn: 'UNREGISTERED-99-OTHER', attrs: {}, selectable: true } as never],
    next: '',
    total: 1,
  });

  const mount3 = document.createElement('div');
  document.body.append(mount3);
  const app3 = createApp(Wizard);
  app3.mount(mount3);
  stop = () => app3.unmount();
  await settle();

  expect(flow.warning).toHaveBeenCalledWith('未找到已登记的可选主机 UNREGISTERED-99');
  expect(mount3.textContent).toContain('已选 0 台');
});

it('does not overwrite manual edits or cross-contaminate hosts when hardware reports arrive asynchronously, and preserves manual input when no report exists', async () => {
  let resolveH1Host!: (val: unknown) => void;
  flow.hostDetail.mockImplementation((sn: string) => {
    if (sn === 'H1') {
      return new Promise((resolve) => {
        resolveH1Host = resolve;
      });
    }
    return Promise.resolve({
      sn: 'H2',
      arch: 'x86_64',
      firmware: 'uefi',
      attrs: {},
      rev: 1,
    });
  });
  flow.discoveryList.mockResolvedValue({ items: [] });

  vi.spyOn(wizardAPI, 'params').mockResolvedValue({
    common: [],
    hosts: [
      {
        sn: 'H1',
        fields: [
          {
            key: 'partition/1/disk',
            label: '安装目标盘',
            list: false,
            locked: false,
            options: [],
            prefill: 'sda',
            required: true,
            type: 'word',
          },
        ],
        problems: [],
      },
      {
        sn: 'H2',
        fields: [
          {
            key: 'partition/1/disk',
            label: '安装目标盘',
            list: false,
            locked: false,
            options: [],
            prefill: 'vda',
            required: true,
            type: 'word',
          },
        ],
        problems: [],
      },
    ],
  });

  const mount = document.createElement('div');
  document.body.append(mount);
  const app = createApp(Wizard);
  app.mount(mount);
  stop = () => app.unmount();
  await settle();
  await click('选择测试主机');
  await click('下一步');
  await click('选择测试 ISO');
  await click('选择测试模板');
  await click('下一步');
  await settle();

  // Operator manually edits H1's target disk before H1 hardware report finishes loading
  const h1Input = document.getElementById('H1:partition/1/disk') as HTMLInputElement;
  h1Input.value = 'manual-disk0';
  h1Input.dispatchEvent(new Event('input', { bubbles: true }));
  await settle();

  // Now H1 hardware report arrives asynchronously
  resolveH1Host({
    sn: 'H1',
    arch: 'x86_64',
    firmware: 'uefi',
    attrs: {
      inv_disks: 'nvme0n1:480000000000:Intel NVMe',
    },
    rev: 1,
  });
  await settle();

  // Manual edit on H1 must not be overwritten by async report
  expect(h1Input.value).toBe('manual-disk0');

  // H2 has no report, must keep manual path and not see H1's disks
  const h2Input = document.getElementById('H2:partition/1/disk') as HTMLInputElement;
  expect(h2Input.value).toBe('vda');
});

it('blocks confirmation and submission when disk report is loading or current disk value mismatches installable disks (including locked, batch-filled, or late async report), and focuses the offending host', async () => {
  flow.checkedHosts = [
    { sn: 'H1', attrs: {}, selectable: true },
    { sn: 'H2', attrs: {}, selectable: true },
  ];
  let resolveDiscoveryList!: (val: unknown) => void;
  flow.discoveryList.mockImplementation(
    () =>
      new Promise((resolve) => {
        resolveDiscoveryList = resolve;
      }),
  );
  flow.discoveryDetail.mockImplementation(async (mac: string) => {
    if (mac === '52:54:00:00:00:02') {
      return {
        mac,
        registered_sn: 'H2',
        report_at: 2000,
        status: 'received',
        report: {
          sections: [
            {
              key: 'storage',
              title: '磁盘和阵列',
              status: 'read',
              items: [
                {
                  name: 'nvme0n1',
                  fields: {
                    类型: '磁盘',
                    '容量（字节）': '480000000000',
                    序列号: 'H2-NVME-1',
                  },
                },
              ],
            },
          ],
        },
      };
    }
    return { mac, status: 'waiting' };
  });

  vi.spyOn(wizardAPI, 'params').mockResolvedValue({
    common: [],
    hosts: [
      {
        sn: 'H1',
        fields: [
          {
            key: 'partition/1/disk',
            label: '安装目标盘',
            list: false,
            locked: false,
            options: [],
            prefill: 'sda',
            required: true,
            type: 'word',
          },
        ],
        problems: [],
      },
      {
        sn: 'H2',
        fields: [
          {
            key: 'partition/1/disk',
            label: '安装目标盘',
            list: false,
            locked: false,
            options: [],
            prefill: 'sda',
            required: true,
            type: 'word',
          },
        ],
        problems: [],
      },
    ],
  });

  const mount = document.createElement('div');
  document.body.append(mount);
  const app = createApp(Wizard);
  app.mount(mount);
  stop = () => app.unmount();
  await settle();
  await click('选择测试主机');
  await click('下一步');
  await click('选择测试 ISO');
  await click('选择测试模板');
  await click('下一步');
  await settle();

  // While hardware report is still loading, attempting to confirm must not advance to step 4
  await click('校验并确认');
  expect(mount.textContent).not.toContain('创建整批任务');

  // Resolve discovery list: H1 has no report (manual allowed), H2 has nvme0n1 (mismatch with 'sda')
  resolveDiscoveryList({
    items: [{ mac: '52:54:00:00:00:02', registered_sn: 'H2', report_at: 2000, status: 'received' }],
  });
  await settle();
  await settle();

  // Now try to confirm: H2 mismatches ('sda' not in ['nvme0n1']), must block confirmation and focus H2
  await click('校验并确认');
  expect(mount.textContent).not.toContain('创建整批任务');
  expect(mount.textContent).toContain('H2');

  // Select H2's valid disk nvme0n1
  await click('nvme0n1 · 480 GB · 序列号 H2-NVME-1');
  await click('校验并确认');
  await settle();
  // Now both H1 (no report -> manual 'sda' allowed) and H2 ('nvme0n1' matched) can reach step 4
  expect(mount.textContent).toContain('创建整批任务');

  // Also verify locked target disk mismatch blocks confirmation
  await click('上一步');
  vi.spyOn(wizardAPI, 'params').mockResolvedValueOnce({
    common: [],
    hosts: [
      {
        sn: 'H1',
        fields: [
          {
            key: 'partition/1/disk',
            label: '安装目标盘',
            list: false,
            locked: true,
            options: [],
            prefill: 'sda',
            required: true,
            type: 'word',
          },
        ],
        problems: [],
      },
      {
        sn: 'H2',
        fields: [
          {
            key: 'partition/1/disk',
            label: '安装目标盘',
            list: false,
            locked: true,
            options: [],
            prefill: 'sda',
            required: true,
            type: 'word',
          },
        ],
        problems: [],
      },
    ],
  });
  await click('校验并确认');
  await settle();
  expect(mount.textContent).not.toContain('创建整批任务');
});

it('paginates discovery pages past 50 records until registered hosts are found, stops on repeated cursor, ignores spoofed unregistered sn, and reuses wizard host attrs', async () => {
  const h1Row = {
    sn: 'H1',
    attrs: { inv_collected_at: '1000', inv_disks: 'sda:240000000000:Old Disk' },
    selectable: true,
  };
  flow.checkedHosts = [h1Row];
  vi.spyOn(wizardAPI, 'hosts').mockResolvedValueOnce({
    items: [h1Row as never],
    next: '',
    total: 1,
  });

  // Page 1 has 50 items including a spoofed unregistered record with sn: 'H1'
  // Page 2 has the real registered_sn: 'H1' with a newer report_at: 2000
  flow.discoveryList
    .mockResolvedValueOnce({
      items: [
        {
          mac: '52:54:00:aa:bb:01',
          sn: 'H1',
          report_at: 3000,
          status: 'received',
        },
      ],
      next_cursor: 'cursor-page-2',
    })
    .mockResolvedValueOnce({
      items: [
        {
          mac: '52:54:00:aa:bb:02',
          registered_sn: 'H1',
          report_at: 1500,
          status: 'received',
        },
        {
          mac: '52:54:00:aa:bb:03',
          registered_sn: 'H1',
          report_at: 2500,
          status: 'received',
        },
      ],
      next_cursor: 'cursor-page-2', // repeated cursor safety
    });

  flow.discoveryDetail.mockImplementation(async (mac: string) => ({
    mac,
    registered_sn: mac === '52:54:00:aa:bb:01' ? undefined : 'H1',
    sn: 'H1',
    report_at: mac === '52:54:00:aa:bb:03' ? 2500 : 1500,
    status: 'received',
    report: {
      sections: [
        {
          key: 'storage',
          title: '磁盘和阵列',
          status: 'read',
          items: [
            {
              name: mac === '52:54:00:aa:bb:03' ? 'nvme0n1' : 'spoofed0',
              fields: {
                类型: '磁盘',
                '容量（字节）': '960000000000',
                序列号: mac === '52:54:00:aa:bb:03' ? 'REAL-NEWEST-SN' : 'WRONG-SN',
              },
            },
          ],
        },
      ],
    },
  }));

  vi.spyOn(wizardAPI, 'params').mockResolvedValue({
    common: [],
    hosts: [
      {
        sn: 'H1',
        fields: [
          {
            key: 'partition/1/disk',
            label: '安装目标盘',
            list: false,
            locked: false,
            options: [],
            prefill: 'nvme0n1',
            required: true,
            type: 'word',
          },
        ],
        problems: [],
      },
    ],
  });

  const mount = document.createElement('div');
  document.body.append(mount);
  const app = createApp(Wizard);
  app.mount(mount);
  stop = () => app.unmount();
  await settle();
  await click('选择测试主机');
  await click('下一步');
  await click('选择测试 ISO');
  await click('选择测试模板');
  await click('下一步');
  await settle();
  await settle();

  // Reused wizard host attrs without redundant N+1 getHostApi call when attrs already present
  expect(flow.hostDetail).not.toHaveBeenCalled();
  // Fetched both page 1 and page 2
  expect(flow.discoveryList).toHaveBeenCalledTimes(2);
  // Only queried the newest registered discovery record (52:54:00:aa:bb:03), never the spoofed one
  expect(flow.discoveryDetail).toHaveBeenCalledWith('52:54:00:aa:bb:03');
  expect(flow.discoveryDetail).not.toHaveBeenCalledWith('52:54:00:aa:bb:01');
  expect(mount.textContent).toContain('REAL-NEWEST-SN');
  expect(mount.textContent).not.toContain('WRONG-SN');
  expect(mount.textContent).not.toContain('Old Disk');
});
