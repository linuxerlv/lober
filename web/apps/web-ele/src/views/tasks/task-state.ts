import type { Schemas } from '#/api/console/generated';

export const taskStateLabels: Record<Schemas['ModelTaskState'], string> = {
  pending: '等待机器引导，随时可能开始装机',
  booting: '引导中',
  installing: '安装中',
  rebooting: '重启中',
  initializing: '初始化中',
  done: '已完成',
  failed: '失败',
  cancelled: '已取消',
};
