import type { Schemas } from '#/api/console/generated';

export const taskStateLabels: Record<Schemas['ModelTaskState'], string> = {
  pending: '等待引导',
  booting: '引导中',
  installing: '安装中',
  rebooting: '重启中',
  initializing: '初始化中',
  done: '已完成',
  failed: '失败',
  cancelled: '已取消',
};

export const activeTaskStates = ['booting', 'installing', 'rebooting', 'initializing'];

export function taskCount(states: Record<string, number> | null | undefined, filter = '') {
  if (!states) return undefined;
  if (filter === 'active') return activeTaskStates.reduce((sum, state) => sum + (states[state] ?? 0), 0);
  if (filter) return states[filter] ?? 0;
  return Object.values(states).reduce((sum, count) => sum + count, 0);
}
