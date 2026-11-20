import type { TaskEvent } from '#/api/console/task';

type LabeledEvent = Pick<TaskEvent, 'fields' | 'kind'> & { key?: string };

function logText(encoded: string): string {
  try {
    const bytes = Uint8Array.from(atob(encoded), (char) => char.charCodeAt(0));
    return Array.from(new TextDecoder().decode(bytes), (char) => {
      const code = char.charCodeAt(0);
      return code === 127 || (code < 32 && ![9, 10, 13].includes(code))
        ? `\\x${code.toString(16).padStart(2, '0')}`
        : char;
    }).join('');
  } catch {
    return '[无法解码]';
  }
}

export function eventDetails(fields?: null | Record<string, string>): string {
  return Object.entries(fields ?? {})
    .map(([key, value]) =>
      key === 'log_base64' ? `日志=${logText(value)}` : `${key}=${value}`,
    )
    .join('；');
}

const eventLabels: Record<string, string> = {
  'task.created': '任务已创建',
  'machine.boot': '主机开始引导',
  'machine.cancel': '任务已取消',
  'machine.cancelled': '任务已取消',
  'machine.retry': '任务已请求重试',
  'machine.installing': '开始安装',
  'machine.installed': '安装器已完成，准备重启',
  'machine.reboot': '主机重启',
  'machine.booted': '新系统已启动',
  'machine.init': '开始初始化',
  'machine.init_start': '首次开机初始化开始',
  'machine.init_done': '首次开机初始化完成',
  'machine.ready': '安装已完成',
  'machine.done': '安装已完成',
  'machine.failed': '任务失败',
  'machine.timeout': '任务执行超时',
  'machine.slow': '安装耗时较长',
  'slot.start': '脚本插槽开始执行',
  'slot.end': '脚本插槽执行完成',
  'machine.slot-end': '脚本插槽执行完成',
  'hook.start': '装机脚本开始执行',
  'hook.report': '装机脚本执行回报',
  'installer.syslog': '安装程序日志',
};

function bootFailed(event: LabeledEvent): boolean {
  return (
    event.kind === 'machine.boot' &&
    (event.fields?.state === 'failed' ||
      (typeof event.key === 'string' && event.key.endsWith('-failed')))
  );
}

function hasExitError(event: LabeledEvent): boolean {
  const rc = event.fields?.rc ?? '';
  return /^-?\d+$/.test(rc) && Number(rc) !== 0;
}

export function eventLabel(event: LabeledEvent): string {
  if (bootFailed(event)) return '主机引导失败';
  if (
    (event.kind === 'slot.end' || event.kind === 'machine.slot-end') &&
    hasExitError(event)
  ) {
    return '脚本插槽执行失败';
  }
  return eventLabels[event.kind] || event.kind;
}

const timingStageLabels: Record<string, string> = {
  pre: '装前脚本',
  'post-installer': '装后脚本',
  'post-target': '装后脚本',
  post: '装后脚本',
  firstboot: '首次开机初始化脚本',
};

const stateStageLabels: Record<string, string> = {
  pending: '等待引导（等待目标机从网络启动）',
  booting: '引导中（正在下发引导菜单与安装环境）',
  installing: '安装中',
  rebooting: '重启中（等待进入新系统）',
  initializing: '初始化中（正在执行首次开机初始化）',
  done: '已完成',
  failed: '失败',
  cancelled: '已取消',
};

const MAX_SUMMARY_LEN = 160;

function truncateSummary(text: string): string {
  const clean = text.replace(/\s+/g, ' ').trim();
  if (clean.length <= MAX_SUMMARY_LEN) return clean;
  return `${clean.slice(0, MAX_SUMMARY_LEN)}…`;
}

function summaryDetails(fields?: null | Record<string, string>): string {
  const parts: string[] = [];
  for (const [key, raw] of Object.entries(fields ?? {})) {
    const value = key === 'log_base64' ? logText(raw).trim() : (raw ?? '').trim();
    if (!value) continue;
    parts.push(key === 'log_base64' ? `日志=${value}` : `${key}=${value}`);
  }
  return truncateSummary(parts.join('；'));
}

function currentAttemptEvents<T extends Pick<TaskEvent, 'kind'>>(
  events: T[],
): T[] {
  for (let i = events.length - 1; i >= 0; i--) {
    if (events[i]?.kind === 'machine.retry') {
      return events.slice(i);
    }
  }
  return events;
}

export function taskProgressStage(
  state: string | undefined,
  events: Pick<TaskEvent, 'fields' | 'kind' | 'seq'>[],
): string {
  const base = stateStageLabels[state ?? ''] || state || '未知阶段';
  if (state === 'done') return '已完成';
  if (state === 'cancelled') return '已取消';
  if (state === 'pending') return stateStageLabels.pending!;

  const attempt = currentAttemptEvents(events);
  if (state === 'failed') {
    for (let i = attempt.length - 1; i >= 0; i--) {
      const ev = attempt[i]!;
      const stageField = ev.fields?.stage?.trim();
      if (ev.kind === 'machine.timeout') {
        const label = stageField ? timingStageLabels[stageField] || stageField : '';
        return label ? `失败（${label}超时）` : '失败（执行超时）';
      }
    }
    return '失败';
  }

  for (let i = attempt.length - 1; i >= 0; i--) {
    const ev = attempt[i]!;
    if (ev.kind === 'machine.retry') break;
    const timing = ev.fields?.timing?.trim();
    const timingLabel = timing ? timingStageLabels[timing] || timing : '';

    if (state === 'booting') {
      if (ev.kind === 'machine.boot') {
        return '引导中（主机已请求网络引导）';
      }
      continue;
    }

    if (state === 'installing') {
      if (
        ev.kind === 'slot.end' ||
        ev.kind === 'machine.slot-end' ||
        (ev.kind === 'machine.slow' && ev.fields?.stage)
      ) {
        const stage = ev.fields?.stage;
        if (stage === 'packages') return '安装中（安装软件包）';
        if (stage === 'pre' || stage === 'post') return `安装中（${timingStageLabels[stage]}）`;
        return base;
      }
      if (
        (ev.kind === 'slot.start' ||
          ev.kind === 'hook.start' ||
          ev.kind === 'hook.report' ||
          ev.kind === 'machine.hook-report') &&
        timing &&
        timing !== 'firstboot'
      ) {
        return `安装中（${timingLabel}）`;
      }
      if (ev.kind === 'machine.installing') {
        return '安装中（安装器已拉取应答配置）';
      }
      continue;
    }

    if (state === 'rebooting') {
      if (ev.kind === 'machine.installed' || ev.kind === 'machine.reboot') {
        return '重启中（安装器已完成，等待重启进入新系统）';
      }
      continue;
    }

    if (state === 'initializing') {
      if (ev.kind === 'machine.init_done') {
        return '初始化中（首次开机脚本已完成）';
      }
      if (
        (ev.kind === 'slot.start' ||
          ev.kind === 'slot.end' ||
          ev.kind === 'hook.start' ||
          ev.kind === 'hook.report' ||
          ev.kind === 'machine.slot-end' ||
          ev.kind === 'machine.hook-report') &&
        timing === 'firstboot'
      ) {
        return `初始化中（${timingLabel}）`;
      }
      if (ev.kind === 'machine.init_start' || ev.kind === 'machine.init') {
        return '初始化中（首次开机脚本执行中）';
      }
      if (ev.kind === 'machine.booted') {
        return '初始化中（新系统已启动）';
      }
    }
  }
  return base;
}

export function latestEventReport(
  events: (LabeledEvent & Pick<TaskEvent, 'at' | 'seq'>)[],
  state?: string,
):
  | undefined
  | {
      at: number;
      content: string;
      kind: string;
      seq: number;
      title: string;
    } {
  if (!events.length) return undefined;
  const attempt = currentAttemptEvents(events);
  let last = attempt.at(-1);
  if (state === 'pending') {
    last = [...attempt]
      .reverse()
      .find((ev) => ev.kind === 'machine.retry' || ev.kind === 'task.created');
  }
  if (!last) return undefined;
  const reason = last.fields?.reason?.trim();
  const message = last.fields?.message?.trim();
  const details = summaryDetails(last.fields);
  const rawContent = reason
    ? truncateSummary(reason)
    : message
      ? truncateSummary(message)
      : details;
  return {
    seq: last.seq,
    at: last.at,
    kind: last.kind,
    title: eventLabel(last),
    content: rawContent || '无附加内容',
  };
}

export function findFailureEvent<
  T extends LabeledEvent & Pick<TaskEvent, 'seq'>,
>(events: T[], requiredHook: (event: T) => boolean = () => false) {
  for (let index = events.length - 1; index >= 0; index--) {
    const event = events[index]!;
    if (event.kind === 'machine.retry') break;
    const fields = event.fields;
    const reason = fields?.reason?.trim();
    if (event.kind === 'machine.failed')
      return { event, summary: reason || '任务报告失败，未提供具体原因。' };
    if (event.kind === 'machine.timeout')
      return { event, summary: reason || '任务在执行阶段超时。' };
    if (bootFailed(event)) {
      return { event, summary: reason || '主机引导失败。' };
    }
    if (
      (event.kind === 'slot.end' ||
        event.kind === 'machine.slot-end' ||
        ((event.kind === 'hook.report' || event.kind === 'machine.hook-report') &&
          requiredHook(event))) &&
      hasExitError(event)
    ) {
      return {
        event,
        summary: reason || `装机脚本执行失败，退出码 ${fields?.rc}。`,
      };
    }
  }
}
