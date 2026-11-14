import { expect, it } from 'vitest';

import { summarizeBatch } from './batch-data';

it('按装机状态汇总同一批任务，等待和取消单独计数', () => {
  expect(
    summarizeBatch([
      { state: 'pending' },
      { state: 'booting' },
      { state: 'installing' },
      { state: 'rebooting' },
      { state: 'initializing' },
      { state: 'done' },
      { state: 'failed' },
      { state: 'failed' },
      { state: 'cancelled' },
    ]),
  ).toEqual({ pending: 1, active: 4, done: 1, failed: 2, cancelled: 1 });
  expect(summarizeBatch([])).toEqual({
    pending: 0,
    active: 0,
    done: 0,
    failed: 0,
    cancelled: 0,
  });
});
