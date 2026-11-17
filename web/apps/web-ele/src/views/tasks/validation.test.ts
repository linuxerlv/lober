/* eslint-disable vue/one-component-per-file -- Test adapters exercise the real page's async flow. */
import { createApp, nextTick } from 'vue';

import { afterEach, expect, it, vi } from 'vitest';

import { wizardAPI } from '#/api/console/wizard';

import Wizard from './new.vue';
const flow = vi.hoisted(() => ({
  pending: undefined as ((x: unknown) => void) | undefined,
  calls: 0,
}));
vi.mock('vue-router', () => ({
  onBeforeRouteLeave: vi.fn(),
  useRoute: () => ({ query: {} }),
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
          getCheckboxRecords: () => [{ sn: 'H1', attrs: {}, selectable: true }],
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
    ElMessage: { warning: vi.fn() },
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
vi.mock('#/api/console/wizard', () => ({
  wizardAPI: {
    hosts: async () => ({
      items: [{ sn: 'H1', attrs: {}, selectable: true }],
      next: '',
      total: 1,
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
