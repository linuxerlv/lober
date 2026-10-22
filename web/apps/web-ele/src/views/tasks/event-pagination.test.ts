/* eslint-disable vue/one-component-per-file -- Small adapters exercise the real event list. */
import { createApp, nextTick } from 'vue';

import { afterEach, expect, it, vi } from 'vitest';

const io = vi.hoisted(() => ({ events: vi.fn() }));
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { id: 'task-1' } }) }));
vi.mock('@vben/stores', () => ({ useAccessStore: () => ({ accessToken: '', accessCodes: [] }) }));
vi.mock('@vben/hooks', () => ({ useAppConfig: () => ({ apiURL: '' }) }));
vi.mock('@vben/common-ui', async () => {
  const { defineComponent, h } = await import('vue');
  return { Page: defineComponent({ setup: (_, ctx) => () => h('main', ctx.slots.default?.()) }) };
});
vi.mock('#/api/console/task', () => ({
  taskAPI: { get: async () => undefined, events: io.events },
  taskStreamURL: vi.fn(),
}));
vi.mock('#/adapter/vxe-table', async () => {
  const { defineComponent, h, ref } = await import('vue');
  return {
    useVbenVxeGrid: () => {
      const rows = ref<any[]>([]);
      return [
        defineComponent({ setup: () => () => h('div', { class: 'event-rows' }, rows.value.map((row) => h('span', `${row.seq},`))) }),
        { setGridOptions: ({ data }: any) => { rows.value = data; } },
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
      h('button', { onClick: () => emit('page-change', 2) }, 'page2'),
      h('button', { onClick: () => emit('size-change', 10) }, 'size10'),
    ]),
  }) };
});
import Detail from './detail.vue';

let stop: (() => void) | undefined;
afterEach(() => { stop?.(); document.body.innerHTML = ''; vi.resetAllMocks(); });
it('pages buffered events without losing the server replay cursor or jumping on new events', async () => {
  io.events.mockResolvedValueOnce({ items: Array.from({ length: 45 }, (_, n) => ({ seq: n + 1, kind: 'boot' })) });
  const node = document.createElement('div');
  document.body.append(node);
  const app = createApp(Detail);
  app.mount(node);
  stop = () => app.unmount();
  await vi.waitFor(() => expect(node.querySelector('.event-rows')?.textContent).toBe(Array.from({ length: 20 }, (_, n) => `${n + 1},`).join('')));
  node.querySelector<HTMLButtonElement>('button')!.click();
  await nextTick();
  expect(node.querySelector('.event-rows')?.textContent).toBe(Array.from({ length: 20 }, (_, n) => `${n + 21},`).join(''));
  io.events.mockResolvedValueOnce({ items: [{ seq: 45, kind: 'replay' }, { seq: 46, kind: 'new' }] });
  [...node.querySelectorAll<HTMLButtonElement>('button')].find((button) => button.textContent?.includes('补读后续事件'))!.click();
  await vi.waitFor(() => expect(node.querySelector('output')?.textContent).toBe('2/20/46'));
  expect(io.events).toHaveBeenLastCalledWith('task-1', 45);
  expect(node.querySelector('.event-rows')?.textContent).toMatch(/^21,22,/);
  [...node.querySelectorAll<HTMLButtonElement>('button')].find((button) => button.textContent === 'size10')!.click();
  await nextTick();
  expect(node.querySelector('output')?.textContent).toBe('1/10/46');
  expect(node.querySelector('.event-rows')?.textContent).toBe('1,2,3,4,5,6,7,8,9,10,');
});
