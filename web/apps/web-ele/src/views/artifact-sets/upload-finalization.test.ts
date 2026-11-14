/* eslint-disable vue/one-component-per-file -- 只替换网络与显示组件，保留真实页面和文件校验。 */
import type { ISOItem, SourceProgress } from '#/api/console/artifact';

import { createApp, nextTick } from 'vue';

import { useAccessStore } from '@vben/stores';

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const io = vi.hoisted(() => ({
  get: vi.fn(),
  operate: vi.fn(),
  progress: vi.fn(),
  upload: vi.fn(),
  reload: vi.fn(),
  item: {} as ISOItem,
}));
vi.mock('@vben/stores', async () => {
  const { reactive } = await import('vue');
  const access = reactive({ accessCodes: ['iso:write'] });
  return { useAccessStore: () => access };
});
vi.mock('@vben/common-ui', async () => {
  const { defineComponent, h } = await import('vue');
  return { Page: defineComponent({ setup: (_, ctx) => () => h('main', [ctx.slots.extra?.(), ctx.slots.default?.()]) }) };
});
vi.mock('element-plus', async () => {
  const { defineComponent, h } = await import('vue');
  const slot = (tag: string) => defineComponent({
    inheritAttrs: false,
    setup: (_, ctx) => () => h(tag, ctx.attrs, ctx.slots.default?.()),
  });
  const overlay = defineComponent({
      props: { modelValue: Boolean },
      setup: (props, ctx) => () => props.modelValue ? h('section', [ctx.slots.default?.(), ctx.slots.footer?.()]) : null,
  });
  return {
    ElAlert: defineComponent({ props: { title: { type: String, default: '' } }, setup: (props) => () => h('aside', props.title) }),
    ElButton: slot('button'),
    ElCheckbox: slot('span'),
    ElDialog: overlay,
    ElDrawer: overlay,
    ElEmpty: slot('div'),
    ElInput: slot('input'),
    ElMessage: { success: vi.fn() },
    ElMessageBox: { confirm: vi.fn() },
    ElOption: slot('option'),
    ElProgress: slot('meter'),
    ElSelect: slot('select'),
    ElTag: slot('span'),
  };
});
vi.mock('#/adapter/vxe-table', async () => {
  const { defineComponent, h } = await import('vue');
  return { useVbenVxeGrid: () => [
    defineComponent({ setup: (_, ctx) => () => h('div', ctx.slots.action?.({ row: io.item })) }),
    { reload: io.reload },
  ] };
});
vi.mock('#/components/console/ListPagination.vue', () => ({ default: { render: () => null } }));
vi.mock('#/components/console/ProblemList.vue', () => ({ default: { render: () => null } }));
vi.mock('./SourceForm.vue', () => ({ default: { render: () => null } }));
vi.mock('#/api/console/artifact', () => ({
  createArtifactSetApi: vi.fn(),
  createdISOIDOf: vi.fn(),
  deleteArtifactSetApi: vi.fn(),
  deprecateISOApi: vi.fn(),
  getArtifactSetListApi: vi.fn(),
  getISOApi: io.get,
  getISOProgressApi: io.progress,
  operateISOApi: io.operate,
  registerISOSourceApi: vi.fn(),
  uploadISOChunkApi: io.upload,
}));

import Index from './index.vue';

const chunkSize = 4 * 1024 * 1024;
const access = useAccessStore();
let source: SourceProgress;
let unmount: (() => void) | undefined;

beforeEach(() => {
  vi.resetAllMocks();
  localStorage.clear();
  access.accessCodes = ['iso:write'];
  io.item = {
    id: 'iso-1', name: 'Rocky minimal', family: 'linux', series: 'rocky',
    state: 'acquiring', rev: 1,
  } as ISOItem;
  source = {
    error: '', fetched: 0, kind: 'upload', ranges: [], rev: 1,
    role: '', state: 'fetching', totalSize: 9,
  };
  io.get.mockResolvedValue(io.item);
  io.progress.mockImplementation(async () => ({
    id: 'iso-1', rev: 1, state: 'acquiring', sources: [{ ...source }],
  }));
  io.upload.mockResolvedValue(null);
});
afterEach(() => {
  unmount?.();
  unmount = undefined;
  document.body.innerHTML = '';
  localStorage.clear();
});

function click(node: HTMLElement, text: string) {
  const button = [...node.querySelectorAll<HTMLButtonElement>('button')]
    .find((item) => item.textContent?.trim() === text);
  expect(button).toBeDefined();
  button!.click();
}
async function mountPage() {
  const node = document.createElement('div');
  document.body.append(node);
  const app = createApp(Index);
  app.mount(node);
  unmount = () => app.unmount();
  await nextTick();
  click(node, '查看详情');
  await vi.waitFor(() => expect(node.textContent).toContain('已获取'));
  return node;
}
function select(node: HTMLElement, file: File) {
  const input = node.querySelector<HTMLInputElement>('input[type="file"]');
  expect(input).not.toBeNull();
  Object.defineProperty(input!, 'files', { configurable: true, value: [file] });
  input!.dispatchEvent(new Event('change'));
  return input!;
}
async function selectAndWait(node: HTMLElement, file: File) {
  const input = select(node, file);
  await nextTick();
  await vi.waitFor(() => expect(input.disabled).toBe(false));
  await nextTick();
}
async function beginOriginalUpload(node: HTMLElement, file: File) {
  source.totalSize = file.size;
  await selectAndWait(node, file);
  expect(io.upload).toHaveBeenCalled();
  expect(localStorage.getItem('lober:iso-upload:iso-1:')).toMatch(/^[\da-f]{64}$/);
  io.upload.mockClear();
}
async function assertChunk(file: File, start: number, end: number) {
  expect(io.upload).toHaveBeenCalledTimes(1);
  const call = io.upload.mock.calls[0]!;
  expect(call[0]).toBe('iso-1');
  expect(call[1]).toBe('');
  expect(call[3]).toBe(start);
  expect(call[4]).toBe(file.size);
  expect(await (call[2] as Blob).arrayBuffer()).toEqual(await file.slice(start, end).arrayBuffer());
}

describe('Linux 支持范围', () => {
  it('新建镜像只提供五种 Linux', async () => {
    const node = document.createElement('div');
    document.body.append(node);
    const app = createApp(Index);
    app.mount(node);
    unmount = () => app.unmount();
    await nextTick();
    click(node, '登记镜像');
    await nextTick();
    const options = [...node.querySelectorAll<HTMLOptionElement>('select[aria-label="ISO 系"] option')]
      .map((item) => item.value);
    expect(options).toEqual(['rhel', 'rocky', 'almalinux', 'debian', 'ubuntu']);
  });
});

describe('normal page upload finalization recovery', () => {
  it.each([1, chunkSize, chunkSize + 3, 2 * chunkSize])(
    'replays exactly the original last chunk for a verified complete fetching source of %i bytes',
    async (size) => {
      const file = new File([new Uint8Array(size).fill(7)], 'rocky.iso');
      const node = await mountPage();
      await beginOriginalUpload(node, file);
      source.fetched = size;
      source.ranges = [{ start: 0, length: size }];
      await selectAndWait(node, file);
      await assertChunk(file, Math.floor((size - 1) / chunkSize) * chunkSize, size);
    },
  );

  it.each(['fetched', 'failed', 'cancelled', 'pending'])('does not replay complete content in source state %s', async (state) => {
    const file = new File(['original!'], 'rocky.iso');
    const node = await mountPage();
    await beginOriginalUpload(node, file);
    source.fetched = file.size;
    source.ranges = [{ start: 0, length: file.size }];
    source.state = state;
    await selectAndWait(node, file);
    expect(io.upload).not.toHaveBeenCalled();
  });

  it('recognizes full coverage across unordered overlapping persisted ranges', async () => {
    const file = new File(['original!'], 'rocky.iso');
    const node = await mountPage();
    await beginOriginalUpload(node, file);
    source.fetched = file.size;
    source.ranges = [{ start: 5, length: 4 }, { start: 0, length: 3 }, { start: 2, length: 4 }];
    await selectAndWait(node, file);
    await assertChunk(file, 0, file.size);
  });

  it('keeps partial recovery limited to its actual gap', async () => {
    const file = new File(['original!'], 'rocky.iso');
    const node = await mountPage();
    await beginOriginalUpload(node, file);
    source.fetched = 6;
    source.ranges = [{ start: 6, length: 3 }, { start: 0, length: 3 }];
    await selectAndWait(node, file);
    await assertChunk(file, 3, 6);
  });

  it('重试清空失败上传后，可在同一浏览器上传同大小的正确文件', async () => {
    source.state = 'pending';
    const wrong = new File(['wrongfile'], 'rocky.iso');
    const correct = new File(['rightfile'], 'rocky.iso');
    const node = await mountPage();

    io.upload.mockImplementationOnce(async () => {
      source = {
        ...source,
        fetched: wrong.size,
        ranges: [{ start: 0, length: wrong.size }],
        state: 'failed',
      };
      throw new Error('完整文件校验失败');
    });
    await selectAndWait(node, wrong);
    expect(node.textContent).toContain('完整文件校验失败');
    expect(node.textContent).toContain('已确认 1 段');
    io.upload.mockClear();

    io.operate.mockImplementationOnce(async () => {
      source = { ...source, fetched: 0, ranges: [], state: 'pending' };
    });
    click(node, '重试获取');
    await vi.waitFor(() => expect(node.textContent).toContain('已确认 0 段'));
    expect(io.operate).toHaveBeenCalledWith('iso-1', 'retry');
    expect(node.textContent).toContain('选择文件上传');

    await selectAndWait(node, correct);
    await assertChunk(correct, 0, correct.size);
  });

  it.each([
    { state: 'fetching', fetched: 3, ranges: [{ start: 0, length: 3 }] },
    { state: 'fetching', fetched: 0, ranges: [] },
    { state: 'pending', fetched: 0, ranges: [{ start: 0, length: 3 }] },
    { state: 'pending', fetched: 3, ranges: [] },
  ])(
    '拒绝混入同大小的另一份文件（$state，已接收 $fetched 字节，区间 $ranges）',
    async (progress) => {
      const original = new File(['original!'], 'rocky.iso');
      const changed = new File(['modified!'], 'rocky.iso');
      const node = await mountPage();
      await beginOriginalUpload(node, original);
      source = { ...source, ...progress };

      await selectAndWait(node, changed);
      expect(io.upload).not.toHaveBeenCalled();
      expect(node.textContent).toContain('内容与原上传文件不一致');
    },
  );

  it.each(['changed bytes', 'changed size', 'missing browser identity'])(
    'refuses complete-range recovery with %s',
    async (change) => {
      const file = new File(['original!'], 'rocky.iso');
      const node = await mountPage();
      if (change !== 'missing browser identity') await beginOriginalUpload(node, file);
      source.fetched = file.size;
      source.ranges = [{ start: 0, length: file.size }];
      const selected = change === 'changed bytes' ? new File(['modified!'], 'rocky.iso')
        : change === 'changed size' ? new File(['short'], 'rocky.iso') : file;
      await selectAndWait(node, selected);
      expect(io.upload).not.toHaveBeenCalled();
      expect(node.textContent).toContain(change === 'missing browser identity'
        ? '此浏览器没有原上传文件的校验记录' : change === 'changed size' ? '大小' : '内容与原上传文件不一致');
    },
  );

  it('does not automatically retry a failed finalization replay', async () => {
    const file = new File(['original!'], 'rocky.iso');
    const node = await mountPage();
    await beginOriginalUpload(node, file);
    source.fetched = file.size;
    source.ranges = [{ start: 0, length: file.size }];
    io.upload.mockRejectedValue(new Error('本次校验失败'));
    await selectAndWait(node, file);
    expect(io.upload).toHaveBeenCalledTimes(1);
    expect(node.textContent).toContain('本次校验失败');
    click(node, '刷新进度');
    await nextTick();
    expect(io.upload).toHaveBeenCalledTimes(1);
  });

  it.each(['pause', 'permission', 'unmount'])('stops subsequent chunks after %s', async (stop) => {
    const file = new File([new Uint8Array(chunkSize + 3)], 'rocky.iso');
    source.totalSize = file.size;
    let finish!: (value: null) => void;
    io.upload.mockImplementationOnce(() => new Promise<null>((resolve) => { finish = resolve; }));
    const node = await mountPage();
    select(node, file);
    await vi.waitFor(() => expect(io.upload).toHaveBeenCalledTimes(1));
    if (stop === 'pause') click(node, '暂停上传');
    else if (stop === 'permission') access.accessCodes = [];
    else { unmount?.(); unmount = undefined; }
    finish(null);
    await vi.waitFor(() => {
      if (stop === 'permission') expect(node.textContent).toContain('上传权限已变更');
      else if (stop === 'pause') expect(node.textContent).toContain('本地上传已暂停');
    });
    await nextTick();
    expect(io.upload).toHaveBeenCalledTimes(1);
  });
});
