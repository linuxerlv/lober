/* eslint-disable vue/one-component-per-file -- 用轻量组件替换界面控件，运行真实批次页。 */
import { createApp, defineComponent, h, nextTick } from 'vue';

import { expect, it, vi } from 'vitest';

const io = vi.hoisted(() => ({
  get: vi.fn(),
  list: vi.fn(),
}));

vi.mock('vue-router', () => ({
  RouterLink: 'a',
  useRoute: () => ({ params: { id: 'batch-one' } }),
}));
vi.mock('@vben/common-ui', () => ({
  Page: defineComponent({ setup: (_, { slots }) => () => h('main', slots.default?.()) }),
}));
vi.mock('element-plus', () => {
  const slot = (tag: string) => defineComponent({
    setup: (_, { slots }) => () => h(tag, slots.default?.()),
  });
  return {
    ElAlert: slot('aside'),
    ElButton: slot('button'),
    ElCard: slot('section'),
    ElTable: defineComponent({
      props: { data: { type: Array, default: () => [] } },
      setup: (props) => () => h('output', JSON.stringify(props.data)),
    }),
    ElTableColumn: slot('div'),
    ElTag: slot('span'),
  };
});
vi.mock('#/api/request', () => ({ requestClient: {} }));
vi.mock('#/api/console/task', () => ({
  taskAPI: { batch: io.get, list: io.list },
}));
vi.mock('#/components/console/ListPagination.vue', () => ({
  default: { render: () => null },
}));

import Batch from './batch.vue';

it('按网址回查整批，筛选失败时只请求该页失败任务', async () => {
  io.get.mockResolvedValue({
    total: 42,
    pending: 40,
    active: 0,
    done: 0,
    failed: 2,
    cancelled: 0,
    createdBy: 'operator',
    createdAt: 1790000000,
    isoName: 'Rocky',
    templateName: '标准',
    templateVersion: 1,
  });
  io.list.mockImplementation(async ({ state }) => ({
    items: [{ id: state ? 'failed-task' : 'pending-task', batchID: 'batch-one', snapshot: { iso: { name: 'Rocky' }, template: { name: '标准' } } }],
    total: state ? 2 : 42,
  }));
  const root = document.createElement('div');
  const app = createApp(Batch);
  app.directive('loading', {});
  app.mount(root);
  try {
    await vi.waitFor(() => expect(root.textContent).toContain('任务总数42'));
    expect(io.get).toHaveBeenCalledWith('batch-one');
    expect(root.querySelector('output')?.textContent).toContain('pending-task');
    Array.from(root.querySelectorAll('button'))
      .find((button) => button.textContent?.trim() === '失败任务')!
      .click();
    await nextTick();
    await vi.waitFor(() => expect(root.querySelector('output')?.textContent).toContain('failed-task'));
    expect(io.list).toHaveBeenLastCalledWith({ batch_id: 'batch-one', page: 1, page_size: 20, state: 'failed' });
    expect(root.textContent).toContain('任务总数42');
  } finally {
    app.unmount();
  }
});
