/* eslint-disable vue/one-component-per-file -- 只替换外部网络和显示适配器，运行真实详情页。 */
import type { Task, TaskEvent } from '#/api/console/task';

import { createApp, nextTick } from 'vue';

import { afterEach, beforeEach, expect, it, vi } from 'vitest';

const io = vi.hoisted(() => ({
  events: vi.fn(),
  get: vi.fn(),
  grid: vi.fn(),
  streamURL: vi.fn(),
}));
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { id: 'task-1' } }) }));
vi.mock('@vben/stores', () => ({
  useAccessStore: () => ({ accessToken: 'test-token', accessCodes: [] }),
}));
vi.mock('@vben/hooks', () => ({ useAppConfig: () => ({ apiURL: '/console/v1' }) }));
vi.mock('@vben/common-ui', async () => {
  const { defineComponent, h } = await import('vue');
  return { Page: defineComponent({ setup: (_, ctx) => () => h('main', ctx.slots.default?.()) }) };
});
vi.mock('element-plus', async () => {
  const { defineComponent, h } = await import('vue');
  const slot = (tag: string) => defineComponent({
    inheritAttrs: false,
    setup: (_, ctx) => () => h(tag, ctx.attrs, ctx.slots.default?.()),
  });
  return {
    ElAlert: slot('aside'),
    ElButton: slot('button'),
    ElCard: slot('section'),
    ElDialog: slot('dialog'),
    ElMessageBox: { confirm: vi.fn() },
    ElTabPane: slot('div'),
    ElTabs: slot('div'),
    ElTag: slot('span'),
  };
});
vi.mock('#/api/console/task', () => ({
  taskAPI: { events: io.events, get: io.get },
  taskStreamURL: io.streamURL,
}));
vi.mock('#/adapter/vxe-table', async () => {
  const { defineComponent, h, ref } = await import('vue');
  return {
    useVbenVxeGrid: () => {
      const rows = ref<TaskEvent[]>([]);
      io.grid.mockImplementation(({ data }: { data: TaskEvent[] }) => { rows.value = data; });
      return [
        defineComponent({
          setup: () => () => h('div', { class: 'event-rows' }, rows.value.map((row) =>
            h('span', { 'data-seq': row.seq }, JSON.stringify(row)))),
        }),
        { setGridOptions: io.grid },
      ];
    },
  };
});
vi.mock('#/components/console/ListPagination.vue', async () => {
  const { defineComponent, h } = await import('vue');
  return { default: defineComponent({
    props: {
      currentPage: { type: Number, required: true },
      pageSize: { type: Number, required: true },
      total: { type: Number, required: true },
    },
    emits: ['page-change', 'size-change'],
    setup: (props, { emit }) => () => h('div', [
      h('output', `${props.currentPage}/${props.pageSize}/${props.total}`),
      h('input', {
        'aria-label': '事件页码',
        onChange: (event: Event) => emit('page-change', Number((event.target as HTMLInputElement).value)),
        type: 'number',
        value: props.currentPage,
      }),
      h('button', { onClick: () => emit('size-change', 10) }, '每页10条'),
    ]),
  }) };
});

import Detail from './detail.vue';

class Stream extends EventTarget {
  static instances: Stream[] = [];
  close = vi.fn();
  onerror: (() => void) | null = null;
  onopen: (() => void) | null = null;

  constructor(readonly url: string) {
    super();
    Stream.instances.push(this);
  }

  send(event: unknown) {
    // 关闭后仍可主动投递，用来检查已排队的旧回调不会更新离开的页面。
    this.dispatchEvent(new MessageEvent('task', { data: JSON.stringify(event) }));
  }
}

function event(seq: number, message = `日志 ${seq}`): TaskEvent {
  return { at: 1_730_000_000 + seq, fields: { message }, key: `log-${seq}`, kind: 'log', seq, taskID: 'task-1' };
}

function task(state = 'installing'): Task {
  return {
    canCancel: [], canRetry: [], createdAt: 1_730_000_000, createdBy: 'tester',
    hostSN: 'SN-1', id: 'task-1', isoID: 'iso-1', rev: 1, state,
    snapshot: {
      family: 'linux', format: 'kickstart', schema: 1, series: 'almalinux', slots: [],
      host: { arch: 'x86_64', attrs: {}, firmware: 'uefi', sn: 'SN-1' },
      iso: { arch: 'x86_64', artifacts: {}, id: 'iso-1', name: 'Alma minimal', version: '10' },
      template: { body: '', id: 'template-1', name: '安装模板', params: {}, version: 1 },
    },
    templateID: 'template-1', templateVersion: 1,
  };
}

let unmount: (() => void) | undefined;
beforeEach(() => {
  vi.useFakeTimers();
  vi.resetAllMocks();
  vi.stubGlobal('EventSource', Stream);
  Stream.instances = [];
  io.get.mockResolvedValue(task());
  io.events.mockResolvedValue({ items: Array.from({ length: 200 }, (_, index) => event(index + 1)) });
  io.streamURL.mockImplementation((base: string, id: string, token: string, after: number) =>
    `${base}/tasks/${id}/stream?access_token=${token}&after_seq=${after}`);
});
afterEach(() => {
  unmount?.();
  unmount = undefined;
  document.body.innerHTML = '';
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function flushPage() {
  for (let tick = 0; tick < 6; tick++) await nextTick();
}

async function mountDetail() {
  const node = document.createElement('div');
  document.body.append(node);
  const app = createApp(Detail);
  app.mount(node);
  unmount = () => app.unmount();
  await flushPage();
  expect(node.querySelector('output')?.textContent).toBe('1/20/200');
  expect(io.events).toHaveBeenCalledWith('task-1', 0);
  expect(io.streamURL).toHaveBeenCalledWith('/console/v1', 'task-1', 'test-token', 200);
  const source = Stream.instances[0]!;
  expect(source).toBeDefined();
  source.onopen?.();
  await nextTick();
  expect(node.textContent).toContain('实时连接已建立');
  io.get.mockClear();
  io.grid.mockClear();
  return { node, source };
}

async function goToPage(node: HTMLElement, page: number) {
  const input = node.querySelector<HTMLInputElement>('input[aria-label="事件页码"]')!;
  input.value = String(page);
  input.dispatchEvent(new Event('change'));
  await nextTick();
}

function visibleEvents(node: HTMLElement): TaskEvent[] {
  return [...node.querySelectorAll('.event-rows [data-seq]')].map((row) => JSON.parse(row.textContent!) as TaskEvent);
}

function click(node: HTMLElement, text: string) {
  const button = [...node.querySelectorAll<HTMLButtonElement>('button')].find((item) => item.textContent === text);
  expect(button).toBeDefined();
  button!.click();
}

it('batches 5000 separately delivered task messages while preserving every page, replacements and replay cursor', async () => {
  const { node, source } = await mountDetail();
  await goToPage(node, 2);
  io.grid.mockClear();
  io.get.mockResolvedValue(task('done'));

  // 每条消息后都让 Vue 处理更新，防止同步循环自动合并，掩盖原来的逐条刷新。
  // 倒序到达也必须展示完整的升序历史。
  for (let seq = 5200; seq >= 201; seq--) {
    source.send(event(seq));
    await nextTick();
  }
  source.send(event(25, '当前页的替换日志'));
  await nextTick();
  source.send(event(5200, '最后一条的替换日志'));
  await nextTick();

  // 允许首条立即处理；同一时间到达的其余消息必须合并。
  expect.soft(io.get.mock.calls.length).toBeLessThanOrEqual(1);
  expect.soft(io.grid.mock.calls.length).toBeLessThanOrEqual(1);
  await vi.advanceTimersByTimeAsync(120);
  await flushPage();
  expect.soft(io.get.mock.calls.length).toBeLessThanOrEqual(2);
  expect.soft(io.grid.mock.calls.length).toBeLessThanOrEqual(2);
  expect(node.querySelector('output')?.textContent).toBe('2/20/5200');
  expect(visibleEvents(node).map((row) => row.seq)).toEqual(Array.from({ length: 20 }, (_, index) => index + 21));
  expect(visibleEvents(node).find((row) => row.seq === 25)?.fields?.message).toBe('当前页的替换日志');
  expect(node.textContent).toContain('已完成');

  const all: TaskEvent[] = [];
  for (let page = 1; page <= 260; page++) {
    await goToPage(node, page);
    all.push(...visibleEvents(node));
  }
  expect(all.map((row) => row.seq)).toEqual(Array.from({ length: 5200 }, (_, index) => index + 1));
  expect(all.at(-1)).toEqual(event(5200, '最后一条的替换日志'));

  io.events.mockResolvedValueOnce({ items: [event(5200, '补读覆盖'), event(5201)] });
  click(node, '补读后续事件');
  await flushPage();
  expect(io.events).toHaveBeenLastCalledWith('task-1', 5200);
  expect(node.querySelector('output')?.textContent).toBe('260/20/5201');
  await goToPage(node, 261);
  expect(visibleEvents(node)).toEqual([event(5201)]);
  await goToPage(node, 260);
  expect(visibleEvents(node).at(-1)).toEqual(event(5200, '补读覆盖'));
  click(node, '每页10条');
  await nextTick();
  expect(node.querySelector('output')?.textContent).toBe('1/10/5201');
}, 30_000);

it('flushes a continuing stream within the batch budget instead of waiting for silence', async () => {
  const { node, source } = await mountDetail();
  for (let seq = 201; seq <= 204; seq++) {
    source.send(event(seq));
    await nextTick();
    await vi.advanceTimersByTimeAsync(25);
  }
  await vi.advanceTimersByTimeAsync(20);
  await flushPage();
  expect(node.querySelector('output')?.textContent).toBe('1/20/204');
  expect(io.get.mock.calls.length).toBeLessThanOrEqual(2);
  expect(io.grid.mock.calls.length).toBeLessThanOrEqual(2);
  source.send(event(205));
  await vi.advanceTimersByTimeAsync(120);
  await flushPage();
  await goToPage(node, 11);
  expect(visibleEvents(node).map((row) => row.seq)).toEqual([201, 202, 203, 204, 205]);
});

it('ignores another task and invalid sequence numbers without reading or refreshing', async () => {
  const { node, source } = await mountDetail();
  source.send({ ...event(201), taskID: 'another-task' });
  for (const seq of [-1, 0, 1.5, Number.MAX_SAFE_INTEGER + 1, '201', null]) {
    source.send({ ...event(201), seq });
    await nextTick();
  }
  await vi.advanceTimersByTimeAsync(120);
  await flushPage();
  expect(node.querySelector('output')?.textContent).toBe('1/20/200');
  expect(io.get).not.toHaveBeenCalled();
  expect(io.grid).not.toHaveBeenCalled();
});

it('closes the source and cancels pending updates on unmount, including late message callbacks', async () => {
  const { source } = await mountDetail();
  source.send(event(201));
  await nextTick();
  unmount!();
  unmount = undefined;
  expect(source.close).toHaveBeenCalledTimes(1);
  expect(vi.getTimerCount()).toBe(0);
  const reads = io.get.mock.calls.length;
  const grids = io.grid.mock.calls.length;
  const eventReads = io.events.mock.calls.length;
  source.send(event(202));
  await vi.advanceTimersByTimeAsync(10_000);
  await flushPage();
  expect(io.get).toHaveBeenCalledTimes(reads);
  expect(io.events).toHaveBeenCalledTimes(eventReads);
  expect(io.grid).toHaveBeenCalledTimes(grids);
});

it('keeps one slow task read in flight, follows it with the latest state, and stops queued reads on unmount', async () => {
  const { node, source } = await mountDetail();
  const pending: Array<(value: Task) => void> = [];
  let inFlight = 0;
  let maxInFlight = 0;
  io.get.mockImplementation(() => {
    inFlight += 1;
    maxInFlight = Math.max(maxInFlight, inFlight);
    return new Promise<Task>((resolve) => {
      pending.push((value) => {
        inFlight -= 1;
        resolve(value);
      });
    });
  });
  const finishRead = (state: string) => {
    const resolve = pending.shift();
    expect(resolve).toBeDefined();
    resolve!(task(state));
  };

  source.send(event(201));
  await vi.advanceTimersByTimeAsync(120);
  await flushPage();
  expect(io.get).toHaveBeenCalledTimes(1);
  // 故意让首个读取一直不返回，跨越多个批次再交付消息。
  for (let seq = 202; seq <= 205; seq++) {
    source.send(event(seq));
    await nextTick();
    await vi.advanceTimersByTimeAsync(120);
  }
  expect(node.querySelector('output')?.textContent).toBe('1/20/205');
  expect(maxInFlight).toBe(1);
  expect(io.get).toHaveBeenCalledTimes(1);

  finishRead('installing');
  await flushPage();
  await vi.advanceTimersByTimeAsync(120);
  await flushPage();
  // 慢请求期间的全部更新只需要一次尾部补读，不能丢失最新状态。
  expect(io.get).toHaveBeenCalledTimes(2);
  expect(io.get).toHaveBeenLastCalledWith('task-1');
  expect(maxInFlight).toBe(1);
  finishRead('done');
  await flushPage();
  expect(node.textContent).toContain('已完成');

  source.send(event(206));
  await vi.advanceTimersByTimeAsync(120);
  source.send(event(207));
  await vi.advanceTimersByTimeAsync(120);
  await flushPage();
  expect(io.get).toHaveBeenCalledTimes(3);
  expect(maxInFlight).toBe(1);
  unmount!();
  unmount = undefined;
  expect(source.close).toHaveBeenCalledTimes(1);
  expect(vi.getTimerCount()).toBe(0);
  const grids = io.grid.mock.calls.length;
  finishRead('failed');
  await vi.advanceTimersByTimeAsync(10_000);
  await flushPage();
  expect(io.get).toHaveBeenCalledTimes(3);
  expect(io.grid).toHaveBeenCalledTimes(grids);
  expect(node.textContent).toBe('');
});
