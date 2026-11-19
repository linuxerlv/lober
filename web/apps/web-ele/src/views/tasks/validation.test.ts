/* eslint-disable vue/one-component-per-file -- Test adapters exercise the real page's async flow. */
import type { Schemas } from '#/api/console/generated';

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
  disks: vi.fn().mockResolvedValue({ status: 'none', disks: [] }),
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
vi.mock('#/api/console/discovery', () => ({
  getHostDisksApi: flow.disks,
}));
vi.mock('#/api/console/wizard', () => ({
  wizardAPI: {
    hosts: async () => ({
      items: flow.checkedHosts,
      next: '',
      total: flow.checkedHosts.length,
    }),
    isos: async () => ({
      items: [{ id: 'iso1', family: 'linux', name: '选择测试 ISO', usable: true, problems: [] }],
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
  flow.disks.mockReset().mockResolvedValue({ status: 'none', disks: [] });
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


const diskField: Schemas['ParamFieldDTO'] = {
  key: 'partition/1/disk',
  label: '已重命名',
  type: 'word',
  prefill: 'sda',
  locked: false,
  list: false,
  required: true,
  options: [],
};
const diskRow = (device: string, serial = '') => ({
  device,
  serial,
  model: '',
  bytes: '480000000000',
});
type Form = Schemas['WizardParamsHost'];

function mockForms(build: (sn: string) => Form) {
  vi.spyOn(wizardAPI, 'params').mockImplementation(
    async ({ hostSNs, entered }) => ({
      common: [],
      hosts: (hostSNs ?? []).map((sn) => {
        const form = build(sn);
        const target = form.targetDisk;
        if (target) {
          const field = form.fields?.find(
            (candidate) => candidate.key === target.key,
          );
          target.value =
            (field?.locked ? undefined : entered?.[sn]?.[target.key]) ||
            field?.prefill ||
            '';
        }
        return form;
      }),
    }),
  );
}

function diskForm(sn: string, field = diskField, mode = 'device') {
  return {
    sn,
    fields: [field],
    problems: [],
    targetDisk: { key: field.key, mode, value: '' },
  };
}

async function openDiskForm() {
  const mount = document.createElement('div');
  document.body.append(mount);
  const app = createApp(Wizard);
  app.mount(mount);
  stop = () => app.unmount();
  await settle();
  for (const label of [
    '选择测试主机',
    '下一步',
    '选择测试 ISO',
    '选择测试模板',
    '下一步',
  ])
    await click(label);
  return mount;
}

function editDisk(sn: string, value: string) {
  const input = document.getElementById(
    sn + ':' + diskField.key,
  ) as HTMLInputElement;
  input.value = value;
  input.dispatchEvent(new Event('input', { bubbles: true }));
  return input;
}

it('uses the declared field despite its label and waits for the report before confirming', async () => {
  let finish!: (v: unknown) => void;
  flow.disks.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  mockForms((sn) => diskForm(sn));
  const page = await openDiskForm();
  const input = editDisk('H1', 'manual');
  await click('校验并确认');
  expect(page.textContent).not.toContain('创建整批任务');
  finish({ status: 'ready', disks: [diskRow('nvme1n1', 'REAL-01')] });
  await settle();
  expect(input.value).toBe('manual');
  expect(page.textContent).toContain('默认目标盘');
  await click('校验并确认');
  expect(page.textContent).not.toContain('创建整批任务');
  await click('nvme1n1 · 480 GB · 序列号 REAL-01');
  await click('校验并确认');
  expect(page.textContent).toContain(
    '目标盘：nvme1n1（480 GB · 序列号 REAL-01）',
  );
  expect(flow.disks).toHaveBeenCalledExactlyOnceWith('H1');
});

it('blocks a locked target that is missing from the disk report', async () => {
  flow.disks.mockResolvedValue({ status: 'ready', disks: [diskRow('sdb')] });
  mockForms((sn) => diskForm(sn, { ...diskField, locked: true }));
  const page = await openDiskForm();
  await click('校验并确认');
  expect(page.textContent).toContain('默认目标盘');
  expect(page.textContent).not.toContain('创建整批任务');
});

it.each([
  ['windows-index', '0', '目标盘：Windows 磁盘 0', true],
  ['first-disk-rule', 'local', '选盘规则：local', false],
])(
  'shows %s as evaluated without loading a Linux disk list',
  async (mode, prefill, summary, locked) => {
    mockForms((sn) =>
      diskForm(
        sn,
        { ...diskField, prefill: String(prefill), locked: Boolean(locked) },
        String(mode),
      ),
    );
    const page = await openDiskForm();
    await click('校验并确认');
    expect(page.textContent).toContain(String(summary));
    expect(flow.disks).not.toHaveBeenCalled();
    vi.mocked(wizardAPI.submit).mockResolvedValueOnce({ tasks: [] });
    await click('创建整批任务');
    expect(vi.mocked(wizardAPI.submit).mock.lastCall?.[0].hosts).toEqual([
      { sn: 'H1', entered: locked ? {} : { [diskField.key]: prefill }, expectedTargetDisk: {key:diskField.key, mode, value:prefill} },
    ]);
  },
);

it('keeps an old response without a descriptor editable even if a label says target disk', async () => {
  mockForms((sn) => ({
    sn,
    fields: [{ ...diskField, label: '安装目标盘' }],
    problems: [],
  }));
  const page = await openDiskForm();
  const path = '/dev/disk/by-id/wwn-example';
  editDisk('H1', path);
  await click('校验并确认');
  expect(page.textContent).toContain('目标盘由自定义模板决定');
  expect(flow.disks).not.toHaveBeenCalled();
  vi.mocked(wizardAPI.submit).mockResolvedValueOnce({ tasks: [] });
  await click('创建整批任务');
  expect(
    vi.mocked(wizardAPI.submit).mock.lastCall?.[0].hosts?.[0]?.entered,
  ).toEqual({ [diskField.key]: path });
});

it('preserves an absolute persistent path when the installer accepts it', async () => {
  mockForms((sn) => diskForm(sn, diskField, 'device-or-path'));
  flow.disks.mockResolvedValue({
    status: 'partial',
    disks: [diskRow('sda')],
    notice: '未读取序列号',
  });
  const page = await openDiskForm();
  expect(page.textContent).toContain('未读取序列号');
  const path = '/dev/disk/by-id/wwn-1234';
  editDisk('H1', path);
  await click('校验并确认');
  expect(page.textContent).toContain(
    '目标盘：' + path + '（安装时解析此路径）',
  );
});

it('retries both failed hosts without rereading the successful one or changing manual input', async () => {
  flow.checkedHosts = ['H1', 'H2', 'H3'].map((sn) => ({
    sn,
    selectable: true,
    attrs: {},
  }));
  const retried = new Set<string>();
  flow.disks.mockImplementation(async (sn: string) => {
    if (sn !== 'H2' && !retried.has(sn)) {
      retried.add(sn);
      throw new Error('连接失败');
    }
    return { status: 'none', disks: [] };
  });
  mockForms((sn) => diskForm(sn));
  const page = await openDiskForm();
  expect(page.textContent).toContain('H1 的硬件报告读取失败');
  expect(page.textContent).toContain('H3 的硬件报告读取失败');
  const input = editDisk('H1', 'vda');
  await click('校验并确认');
  expect(page.textContent).not.toContain('创建整批任务');
  await click('重新读取硬件报告');
  expect(input.value).toBe('vda');
  await click('校验并确认');
  expect(page.textContent).toContain('目标盘：vda');
  expect(flow.disks.mock.calls.map(([sn]) => sn).sort()).toEqual([
    'H1',
    'H1',
    'H2',
    'H3',
    'H3',
  ]);
});

it('discards late reports and old input after the selection changes', async () => {
  const all = ['H1', 'H2'].map((sn) => ({ sn, selectable: true, attrs: {} }));
  vi.spyOn(wizardAPI, 'hosts').mockResolvedValue({
    items: all as never,
    total: 2,
    next: '',
  });
  let finish!: (v: unknown) => void;
  flow.disks
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValue({ status: 'ready', disks: [diskRow('sda', 'DISK-B')] });
  mockForms((sn) => diskForm(sn));
  const page = await openDiskForm();
  editDisk('H1', 'old');
  await click('上一步');
  await click('上一步');
  flow.checkedHosts = [all[1]!];
  for (const label of [
    '选择测试主机',
    '下一步',
    '选择测试 ISO',
    '选择测试模板',
    '下一步',
    '校验并确认',
  ])
    await click(label);
  finish({ status: 'ready', disks: [diskRow('sda', 'DISK-A')] });
  await settle();
  expect(page.textContent).toContain('目标盘：sda（480 GB · 序列号 DISK-B）');
  expect(page.textContent).not.toContain('DISK-A');
  expect(vi.mocked(wizardAPI.params).mock.lastCall?.[0].entered).toEqual({
    H2: { [diskField.key]: 'sda' },
  });
});

it('clears an entered value even when the replacement template uses the same field key', async () => {
  vi.spyOn(wizardAPI, 'templates').mockResolvedValue({
    items: [
      { id: 'one', version: 1, name: '选择测试模板', usable: true },
      { id: 'two', version: 3, name: '第二个模板', usable: true },
    ] as never,
    total: 2,
    next: '',
  });
  mockForms((sn) => diskForm(sn));
  await openDiskForm();
  editDisk('H1', 'old');
  await click('上一步');
  await click('第二个模板');
  await click('下一步');
  const last = vi.mocked(wizardAPI.params).mock.lastCall?.[0];
  expect(last).toMatchObject({
    templateID: 'two',
    templateVersion: 3,
    entered: {},
  });
  expect(
    (document.getElementById('H1:' + diskField.key) as HTMLInputElement).value,
  ).toBe('sda');
});

it('explains manual entry and an invalid template prefix without a report', async () => {
  mockForms((sn) => diskForm(sn));
  const page = await openDiskForm();
  expect(page.textContent).toContain('默认目标盘未经硬件核对');
  editDisk('H1', '/dev/vdb');
  await click('校验并确认');
  expect(page.textContent).toContain('此模板会补上 /dev/');
  expect(page.textContent).not.toContain('创建整批任务');
  editDisk('H1', 'vdb');
  await click('校验并确认');
  expect(page.textContent).toContain('目标盘：vdb');
});

it('submits the confirmed target and asks for confirmation again if it changed', async () => {
  let disk = 'sda';
  mockForms((sn) => diskForm(sn, { ...diskField, locked: true, prefill: disk }));
  const page = await openDiskForm();
  await click('校验并确认');
  expect(page.textContent).toContain('目标盘：sda');
  disk = 'sdb';
  vi.mocked(wizardAPI.submit).mockRejectedValueOnce({ response: { data: {
    code: 409, data: { problems: [{ code: 'target_disk_changed', message: '目标盘已变化，请重新确认。' }] },
  } } });
  await click('创建整批任务');
  expect(vi.mocked(wizardAPI.submit).mock.lastCall?.[0].hosts).toEqual([
    { sn: 'H1', entered: {}, expectedTargetDisk: { key: diskField.key, mode: 'device', value: 'sda' } },
  ]);
  expect(page.textContent).toContain('目标盘已变化');
  expect(page.textContent).toContain('校验并确认');
  await click('校验并确认');
  expect(page.textContent).toContain('目标盘：sdb');
});
