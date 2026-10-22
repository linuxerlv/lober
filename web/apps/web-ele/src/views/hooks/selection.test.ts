/* eslint-disable vue/one-component-per-file -- Lightweight UI adapters isolate the real page request race. */
import type { Schemas } from '#/api/console/generated';

import { createApp, h, nextTick, ref } from 'vue';

import { ElMessage } from 'element-plus';
import { afterEach, expect, it, vi } from 'vitest';

import AuditPage from '../audit/index.vue';
import TasksPage from '../tasks/index.vue';
import TemplatesPage from '../templates/index.vue';
import PreviewFields from '../templates/PreviewFields.vue';
import UsersPage from '../users/index.vue';
import HooksPage from './index.vue';

const fixtures = vi.hoisted(() => ({
  pending: new Map<string, (value: unknown) => void>(),
  previewRequests: [] as Schemas['PreviewRequest'][],
  hookQueries: [] as Record<string, unknown>[],
  templateQueries: [] as Record<string, unknown>[],
  otherQueries: [] as Record<string, unknown>[],
  candidateQueries: [] as Record<string, unknown>[],
  slots: [] as Schemas['SlotInfo'][],
}));
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock('#/api/console/task', () => ({
  taskAPI: {
    list: async (query: Record<string, unknown>) => {
      fixtures.otherQueries.push(query);
      return { items: [{ id: `task-${query.page}`, state: 'done' }], total: 42 };
    },
  },
}));
vi.mock('#/api/console/audit', () => ({
  listAudit: async (query: Record<string, unknown>) => {
    fixtures.otherQueries.push(query);
    return { items: [{ id: `audit-${query.page}` }], total: 42 };
  },
  exportAudit: vi.fn(),
}));
vi.mock('#/api/console/user', () => ({
  listUsers: async (query: Record<string, unknown>) => {
    fixtures.otherQueries.push(query);
    return { items: [{ username: `user-${query.page}`, rev: 1, role: 'viewer', disabled: false }], total: 42 };
  },
  createUser: vi.fn(),
  updateUser: vi.fn(),
  disableUser: vi.fn(),
}));
vi.mock('#/components/console/ListPagination.vue', async () => {
  const { defineComponent, h } = await import('vue');
  return {
    default: defineComponent({
      props: {
        currentPage: { type: Number, default: 1 },
        pageSize: { type: Number, default: 20 },
        total: { type: Number, default: 0 },
        disabled: Boolean,
      },
      emits: ['page-change', 'size-change'],
      setup: (props, { emit }) => () => h('nav', [
        h('span', `第 ${props.currentPage} 页 / 共 ${props.total} 条`),
        h('button', { disabled: props.disabled, onClick: () => emit('page-change', 2) }, '跳到第 2 页'),
        h('button', { disabled: props.disabled, onClick: () => emit('size-change', 50) }, '每页 50 条'),
      ]),
    }),
  };
});
vi.mock('@vben/stores', () => ({
  useAccessStore: () => ({
    accessCodes: [
      'hook:read',
      'hook:write',
      'hook:approve',
      'template:read',
      'template:write',
      'template:approve',
      'user:manage',
    ],
  }),
  useUserStore: () => ({ userInfo: { username: 'reviewer' } }),
}));
vi.mock('@vben/common-ui', async () => {
  const { defineComponent, h } = await import('vue');
  return {
    Page: defineComponent({
      setup:
        (_, { slots }) =>
        () =>
          h('main', [slots.extra?.(), slots.default?.()]),
    }),
  };
});
vi.mock('#/adapter/vxe-table', async () => {
  const { defineComponent, h, ref } = await import('vue');
  return {
    useVbenVxeGrid: () => {
      const rows = ref<unknown[]>([]);
      return [
        defineComponent({
          setup:
            (_, { slots }) =>
            () =>
              h(
                'div',
                rows.value.map((row) => h('div', [
                  h('span', { 'data-testid': 'row-id' }, (row as { id?: string; username?: string }).id ?? (row as { username?: string }).username),
                  slots.action?.({ row }),
                ])),
              ),
        }),
        {
          setGridOptions: ({ data }: { data: unknown[] }) => {
            rows.value = data;
          },
        },
      ];
    },
  };
});
vi.mock('element-plus', async () => {
  const { defineComponent, h } = await import('vue');
  const Container = defineComponent({
    setup:
      (_, { slots }) =>
      () =>
        h('div', slots.default?.()),
  });
  const Button = defineComponent({
    setup:
      (_, { slots, attrs }) =>
      () =>
        h('button', attrs, slots.default?.()),
  });
  const Input = defineComponent({
    props: {
      modelValue: { type: [String, Number], default: '' },
      type: { type: String, default: 'text' },
    },
    emits: ['update:modelValue'],
    setup:
      (props, { emit }) =>
      () =>
        h(props.type === 'textarea' ? 'textarea' : 'input', {
          value: props.modelValue,
          onInput: (event: Event) =>
            emit('update:modelValue', (event.target as HTMLInputElement).value),
        }),
  });
  const Dialog = defineComponent({
    props: { modelValue: Boolean, title: { type: String, default: '' } },
    setup:
      (props, { slots }) =>
      () =>
        props.modelValue
          ? h('section', [
              h('h2', String(props.title)),
              slots.default?.(),
              slots.footer?.(),
            ])
          : null,
  });
  return {
    ElAlert: Container,
    ElButton: Button,
    ElCheckbox: Container,
    ElDialog: Dialog,
    ElForm: Container,
    ElFormItem: Container,
    ElInput: Input,
    ElInputNumber: Input,
    ElOption: defineComponent({
      props: {
        value: { type: String, default: '' },
        label: { type: String, default: '' },
      },
      setup: (props) => () => h('option', { value: props.value }, props.label),
    }),
    ElSelect: defineComponent({
      props: {
        modelValue: { type: [String, Array], default: '' },
        multiple: Boolean,
        disabled: Boolean,
      },
      emits: ['update:modelValue'],
      setup:
        (props, { slots, emit }) =>
        () =>
          h(
            'select',
            {
              value: props.modelValue,
              multiple: props.multiple,
              disabled: props.disabled,
              onChange: (event: Event) => {
                const target = event.target as HTMLSelectElement;
                emit(
                  'update:modelValue',
                  props.multiple
                    ? [...target.selectedOptions].map((option) => option.value)
                    : target.value,
                );
              },
            },
            slots.default?.(),
          ),
    }),
    ElTag: Container,
    ElMessage: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
    ElMessageBox: { confirm: vi.fn() },
  };
});
vi.mock('#/api/console/hook', () => ({
  getHookListApi: async (query: Record<string, unknown>) => {
    fixtures.hookQueries.push(query);
    return ({
    items: [
      { id: query.page === 2 ? 'page2' : 'first', version: 1, name: 'first' },
      { id: 'second', version: 1, name: 'second' },
    ],
    next: '',
    total: 42,
  }); },
  getHookApi: (id: string) =>
    new Promise((resolve) => fixtures.pending.set(id, resolve)),
  getHookVersionsApi: async () => ({ items: [] }),
  updateHookApi: () =>
    new Promise((resolve) => fixtures.pending.set('mutation', resolve)),
  hookLifecycleApi: () =>
    new Promise((resolve) => fixtures.pending.set('mutation', resolve)),
}));
let unmount: (() => void) | undefined;
afterEach(() => {
  unmount?.();
  document.body.innerHTML = '';
  fixtures.pending.clear();
  fixtures.previewRequests.length = 0;
  fixtures.hookQueries.length = 0;
  fixtures.templateQueries.length = 0;
  fixtures.otherQueries.length = 0;
  fixtures.candidateQueries.length = 0;
  fixtures.slots.length = 0;
  vi.clearAllMocks();
});
async function settle() {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await nextTick();
}
function hook(id: string): Schemas['HookResponse'] {
  return {
    id,
    version: 1,
    name: id,
    author: 'author',
    approver: '',
    description: '',
    state: 'review',
    builtin: false,
    rev: 4,
    timeoutSec: 30,
    required: true,
    params: [],
    impls: [],
    problems: [],
  };
}
it('keeps the selected hook when an older detail request arrives last', async () => {
  const root = document.createElement('div');
  document.body.append(root);
  const app = createApp(HooksPage);
  app.mount(root);
  unmount = () => app.unmount();
  await settle();
  const buttons = [...root.querySelectorAll('button')].filter((button) =>
    button.textContent?.includes('查看 / 编辑'),
  );
  buttons[0]!.click();
  await nextTick();
  buttons[1]!.click();
  fixtures.pending.get('second')!(hook('second'));
  await settle();
  fixtures.pending.get('first')!(hook('first'));
  await settle();
  expect(root.querySelector('h2')?.textContent).toContain('second');
  expect(root.querySelector('h2')?.textContent).not.toContain('first');
});

it.each(['save', 'approve'] as const)(
  'does not apply a late hook %s after closing and creating another editor',
  async (action) => {
    const root = document.createElement('div');
    document.body.append(root);
    const app = createApp(HooksPage);
    app.mount(root);
    unmount = () => app.unmount();
    await settle();
    [...root.querySelectorAll('button')]
      .find((button) => button.textContent?.includes('查看 / 编辑'))!
      .click();
    fixtures.pending.get('first')!({
      ...hook('first'),
      state: action === 'save' ? 'draft' : 'review',
    });
    await settle();
    [...root.querySelectorAll('button')]
      .find((button) =>
        button.textContent?.includes(action === 'save' ? '保存草稿' : '批准'),
      )!
      .click();
    await nextTick();
    [...root.querySelectorAll('button')]
      .find((button) => button.textContent?.trim() === '关闭')!
      .click();
    await nextTick();
    [...root.querySelectorAll('button')]
      .find((button) => button.textContent?.trim() === '新建 hook')!
      .click();
    await nextTick();
    fixtures.pending.get('mutation')!(hook('first'));
    await settle();
    expect(root.querySelector('h2')?.textContent).toBe('新建 hook');
  },
);

vi.mock('#/api/console/template', () => ({
  getTemplateListApi: async (query: Record<string, unknown>) => {
    fixtures.templateQueries.push(query);
    return ({
    items: [{ id: 'first', version: 1 }],
    next: '',
    total: 42,
  }); },
  getTemplateApi: () =>
    new Promise((resolve) => fixtures.pending.set('template:get', resolve)),
  getTemplateVersionsApi: async () => ({ items: [] }),
  getTemplateSlotsApi: async () => ({ slots: fixtures.slots, problems: [] }),
  getHookCandidatesApi: async (_id: string, _version: number, query: Record<string, unknown>) => {
    fixtures.candidateQueries.push(query);
    const name = query.page === 2 ? 'candidate-second-page' : 'candidate-first-page';
    return { items: [{ hook: { ...hook(name), description: name }, usable: true, problems: [] }], total: 42 };
  },
  getSourceDiffApi: async () => ({
    original: null,
    current: null,
    changed: false,
  }),
  updateTemplateApi: () =>
    new Promise((resolve) => fixtures.pending.set('mutation', resolve)),
  templateLifecycleApi: () =>
    new Promise((resolve) => fixtures.pending.set('mutation', resolve)),
  deriveTemplateApi: () =>
    new Promise((resolve) => fixtures.pending.set('mutation', resolve)),
  getPreviewParamsApi: (
    _id: string,
    _version: number,
    data: Schemas['PreviewRequest'],
  ) =>
    new Promise((resolve) =>
      fixtures.pending.set('preview:' + (data.isoID || 'sample'), resolve),
    ),
  previewTemplateApi: async (
    _id: string,
    _version: number,
    data: Schemas['PreviewRequest'],
  ) => {
    fixtures.previewRequests.push(data);
    return { files: [], sample: [], problems: [] };
  },
}));
function template(): Schemas['TemplateResponse'] {
  return {
    id: 'first',
    name: 'first',
    version: 1,
    rev: 4,
    author: 'author',
    approver: '',
    builtin: false,
    state: 'draft',
    family: 'linux',
    series: ['rocky'],
    body: '',
    params: [],
    bindings: {},
    assembly: {},
    derivedFrom: null,
    problems: [],
  };
}
it.each(['hook', 'template'] as const)(
  'requests numbered %s pages, resets search/page size, and displays the server total',
  async (kind) => {
    const root = document.createElement('div');
    document.body.append(root);
    const app = createApp(kind === 'hook' ? HooksPage : TemplatesPage);
    app.mount(root);
    unmount = () => app.unmount();
    const queries = kind === 'hook' ? fixtures.hookQueries : fixtures.templateQueries;
    const click = (text: string) => [...root.querySelectorAll('button')]
      .find((button) => button.textContent?.trim() === text)!.click();
    await settle();
    expect(queries.at(-1)).toMatchObject({ page: 1, page_size: 20 });
    expect(queries.at(-1)).not.toHaveProperty('cursor');
    expect(root.querySelector('nav')?.textContent).toContain('共 42 条');
    const prefixInput = root.querySelector<HTMLInputElement>('input')!;
    prefixInput.value = 'filtered';
    prefixInput.dispatchEvent(new Event('input'));
    await nextTick();
    click('跳到第 2 页');
    await settle();
    expect(queries.at(-1)).toMatchObject({ page: 2, page_size: 20, prefix: '' });
    click('查询');
    await settle();
    expect(queries.at(-1)).toMatchObject({ page: 1, page_size: 20, prefix: 'filtered' });
    click('跳到第 2 页');
    await settle();
    click('每页 50 条');
    await settle();
    expect(queries.at(-1)).toMatchObject({ page: 1, page_size: 50 });
  },
);
it.each([
  ['tasks', TasksPage, 'task'],
  ['audit', AuditPage, 'audit'],
  ['users', UsersPage, 'user'],
] as const)('loads numbered %s pages and replaces the visible rows', async (_kind, component, id) => {
  const root = document.createElement('div');
  document.body.append(root);
  const app = createApp(component);
  app.mount(root);
  unmount = () => app.unmount();
  await settle();
  expect(fixtures.otherQueries.at(-1)).toMatchObject({ page: 1, page_size: 20 });
  expect(fixtures.otherQueries.at(-1)).not.toHaveProperty('cursor');
  expect(root.querySelector('nav')?.textContent).toContain('共 42 条');
  [...root.querySelectorAll('button')].find((button) => button.textContent === '跳到第 2 页')!.click();
  await settle();
  expect(fixtures.otherQueries.at(-1)).toMatchObject({ page: 2, page_size: 20 });
  expect([...root.querySelectorAll('[data-testid="row-id"]')].map((row) => row.textContent)).toEqual([`${id}-2`]);
  if (id === 'user') {
    [...root.querySelectorAll('button')].find((button) => button.textContent === '刷新')!.click();
    await settle();
    expect(fixtures.otherQueries.at(-1)).toMatchObject({ page: 2, page_size: 20 });
  }
  [...root.querySelectorAll('button')].find((button) => button.textContent === '每页 50 条')!.click();
  await settle();
  expect(fixtures.otherQueries.at(-1)).toMatchObject({ page: 1, page_size: 50 });
});
it('pages hook candidates without accumulating older pages and resets candidate search', async () => {
  fixtures.slots.push({ name: 'post', line: 1, col: 1, exec: false, timing: 'post-target' });
  const root = document.createElement('div');
  document.body.append(root);
  const app = createApp(TemplatesPage);
  app.mount(root);
  unmount = () => app.unmount();
  await settle();
  const click = (text: string, container: ParentNode = root) => [...container.querySelectorAll('button')]
    .find((button) => button.textContent?.trim() === text)!.click();
  click('查看 / 编辑');
  fixtures.pending.get('template:get')!(template());
  await settle();
  click('添加 hook');
  fixtures.pending.get('mutation')!(template());
  await settle();
  const dialog = [...root.querySelectorAll('section')].find((section) => section.querySelector('h2')?.textContent?.startsWith('选择 hook'))!;
  expect(fixtures.candidateQueries.at(-1)).toMatchObject({ slot: 'post', index: 1, page: 1, page_size: 20 });
  expect(dialog.textContent).toContain('candidate-first-page');
  click('跳到第 2 页', dialog);
  await settle();
  expect(fixtures.candidateQueries.at(-1)).toMatchObject({ page: 2, page_size: 20 });
  expect(dialog.textContent).toContain('candidate-second-page');
  expect(dialog.textContent).not.toContain('candidate-first-page');
  click('查询', dialog);
  await settle();
  expect(fixtures.candidateQueries.at(-1)).toMatchObject({ page: 1, page_size: 20 });
  click('选用', dialog);
  await settle();
  expect(root.textContent).toContain('candidate-first-page v1');
});
it.each(['save', 'approve', 'derive'] as const)(
  'does not apply a late template %s to a newly created editor',
  async (action) => {
    const root = document.createElement('div');
    document.body.append(root);
    const app = createApp(TemplatesPage);
    app.mount(root);
    unmount = () => app.unmount();
    await settle();
    [...root.querySelectorAll('button')]
      .find((button) => button.textContent?.includes('查看 / 编辑'))!
      .click();
    fixtures.pending.get('template:get')!({
      ...template(),
      state: action === 'approve' ? 'review' : 'draft',
    });
    await settle();
    if (action === 'derive') {
      const input = root.querySelector<HTMLInputElement>(
        'input[placeholder^="派生模板名称"]',
      )!;
      input.value = 'derived';
      input.dispatchEvent(new Event('input'));
      await nextTick();
    }
    [...root.querySelectorAll('button')]
      .find(
        (button) =>
          button.textContent?.trim() ===
          { save: '保存草稿', approve: '批准', derive: '派生模板' }[action],
      )!
      .click();
    await nextTick();
    [...root.querySelectorAll('button')]
      .find((button) => button.textContent?.trim() === '关闭')!
      .click();
    await nextTick();
    [...root.querySelectorAll('button')]
      .find((button) => button.textContent?.trim() === '新建模板')!
      .click();
    await nextTick();
    fixtures.pending.get('mutation')!(template());
    await settle();
    expect(root.querySelector('h2')?.textContent).toBe('新建模板');
  },
);
it('does not show completion notifications for an editor mutation after unmount', async () => {
  const root = document.createElement('div');
  document.body.append(root);
  const app = createApp(HooksPage);
  app.mount(root);
  unmount = () => app.unmount();
  await settle();
  [...root.querySelectorAll('button')]
    .find((button) => button.textContent?.includes('查看 / 编辑'))!
    .click();
  fixtures.pending.get('first')!({ ...hook('first'), state: 'draft' });
  await settle();
  [...root.querySelectorAll('button')]
    .find((button) => button.textContent?.trim() === '保存草稿')!
    .click();
  await nextTick();
  app.unmount();
  unmount = undefined;
  fixtures.pending.get('mutation')!(hook('first'));
  await settle();
  expect(ElMessage.success).not.toHaveBeenCalled();
});

it('renders only server-evaluated preview controls with options, multiline lists, locked prefills and canonical edits', async () => {
  const root = document.createElement('div');
  document.body.append(root);
  const fields: Schemas['ParamFieldDTO'][] = [
    {
      key: 'tpl/disks',
      label: '目标盘',
      type: 'diskpath',
      list: true,
      required: true,
      locked: false,
      prefill: '/dev/sda\n/dev/sdb',
      options: [],
    },
    {
      key: 'tpl/host',
      label: '主机名',
      type: 'hostname',
      list: false,
      required: true,
      locked: true,
      prefill: 'host-from-attribute',
      options: [],
    },
    {
      key: 'tpl/edition',
      label: '版本',
      type: 'enum',
      list: false,
      required: true,
      locked: false,
      prefill: 'server',
      options: [
        { value: 'server', label: '服务器' },
        { value: 'minimal', label: '最小' },
      ],
    },
    {
      key: 'tpl/packages',
      label: '软件包',
      type: 'enum',
      list: true,
      required: false,
      locked: false,
      prefill: 'a\nb',
      options: [
        { value: 'a', label: 'A' },
        { value: 'b', label: 'B' },
      ],
    },
  ];
  const entered = ref<Record<string, string>>({});
  const app = createApp({
    setup: () => () =>
      h(PreviewFields, {
        fields,
        modelValue: entered.value,
        'onUpdate:modelValue': (value) => {
          entered.value = value;
        },
      }),
  });
  app.mount(root);
  unmount = () => app.unmount();
  await nextTick();
  const textarea = root.querySelector('textarea');
  expect(textarea).not.toBeNull();
  expect(textarea!.value).toBe('/dev/sda\n/dev/sdb');
  const locked = root.querySelector<HTMLInputElement>('input[disabled]');
  expect(locked?.value).toBe('host-from-attribute');
  const selects = root.querySelectorAll('select');
  expect(selects).toHaveLength(2);
  expect(selects[0]!.textContent).toContain('服务器');
  expect(selects[1]!.multiple).toBe(true);
  textarea!.value = '/dev/nvme0n1\n/dev/nvme1n1';
  textarea!.dispatchEvent(new Event('input'));
  selects[0]!.value = 'minimal';
  selects[0]!.dispatchEvent(new Event('change'));
  selects[1]!.options[0]!.selected = false;
  selects[1]!.options[1]!.selected = true;
  selects[1]!.dispatchEvent(new Event('change'));
  await nextTick();
  expect(entered.value).toEqual({
    'tpl/disks': '/dev/nvme0n1\n/dev/nvme1n1',
    'tpl/edition': 'minimal',
    'tpl/packages': 'b',
  });
});

it('refreshes preview fields from server context and ignores older context results, submitting only editable canonical values', async () => {
  const root = document.createElement('div');
  document.body.append(root);
  const app = createApp(TemplatesPage);
  app.mount(root);
  unmount = () => app.unmount();
  await settle();
  [...root.querySelectorAll('button')]
    .find((button) => button.textContent?.includes('查看 / 编辑'))!
    .click();
  fixtures.pending.get('template:get')!({ ...template(), state: 'approved' });
  await settle();
  [...root.querySelectorAll('button')]
    .find((button) => button.textContent?.trim() === '预览全部文件')!
    .click();
  await settle();
  await vi.waitFor(() =>
    expect(fixtures.pending.has('preview:sample')).toBe(true),
  );
  const iso = root.querySelector<HTMLInputElement>(
    'input[placeholder^="留空使用示例 ISO"]',
  )!;
  iso.value = 'iso2';
  iso.dispatchEvent(new Event('input'));
  await nextTick();
  await vi.waitFor(() =>
    expect(fixtures.pending.has('preview:iso2')).toBe(true),
  );
  const fields: Schemas['ParamFieldDTO'][] = [
    {
      key: 'tpl/edition',
      label: '服务端版本',
      type: 'enum',
      list: false,
      required: true,
      locked: false,
      prefill: 'server',
      options: [
        { value: 'server', label: '服务器限定版本' },
        { value: 'minimal', label: '最小版本' },
      ],
    },
    {
      key: 'tpl/hostname',
      label: '主机名',
      type: 'hostname',
      list: false,
      required: true,
      locked: true,
      prefill: 'locked-host',
      options: [],
    },
    {
      key: 'tpl/disks',
      label: '盘列表',
      type: 'diskpath',
      list: true,
      required: true,
      locked: false,
      prefill: '/dev/sda',
      options: [],
    },
  ];
  fixtures.pending.get('preview:iso2')!({ fields, problems: [], sample: [] });
  await settle();
  fixtures.pending.get('preview:sample')!({
    fields: [
      { ...fields[0], options: [{ value: 'stale', label: '旧上下文' }] },
    ],
    problems: [],
    sample: [],
  });
  await settle();
  expect(root.textContent).toContain('服务器限定版本');
  expect(root.textContent).not.toContain('旧上下文');
  const edition = [...root.querySelectorAll('select')].find((select) =>
    select.textContent?.includes('服务器限定版本'),
  )!;
  edition.value = 'minimal';
  edition.dispatchEvent(new Event('change'));
  await nextTick();
  const disks = root.querySelector<HTMLTextAreaElement>(
    'textarea[placeholder="tpl/disks"]',
  )!;
  disks.value = '/dev/sda\n/dev/sdb';
  disks.dispatchEvent(new Event('input'));
  await nextTick();
  expect(
    root.querySelector<HTMLInputElement>('input[placeholder="tpl/hostname"]')
      ?.disabled,
  ).toBe(true);
  [...root.querySelectorAll('button')]
    .find((button) => button.textContent?.trim() === '生成预览')!
    .click();
  await settle();
  expect(fixtures.previewRequests).toEqual([
    {
      hostSN: '',
      isoID: 'iso2',
      entered: { 'tpl/edition': 'minimal', 'tpl/disks': '/dev/sda\n/dev/sdb' },
    },
  ]);
});
