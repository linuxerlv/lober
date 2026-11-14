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
import type { TaskEvent } from '#/api/console/task';

export function findFailureEvent<
  T extends Pick<TaskEvent, 'fields' | 'kind' | 'seq'>,
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
    if (
      event.kind === 'machine.boot' &&
      (event.fields?.state === 'failed' ||
        ('key' in event &&
          typeof event.key === 'string' &&
          event.key.endsWith('-failed')))
    ) {
      return { event, summary: reason || '主机引导失败。' };
    }
    if (
      (event.kind === 'machine.slot-end' ||
        (event.kind === 'machine.hook-report' && requiredHook(event))) &&
      /^-?\d+$/.test(fields?.rc ?? '') &&
      Number(fields?.rc) !== 0
    ) {
      return {
        event,
        summary: reason || `装机脚本执行失败，退出码 ${fields?.rc}。`,
      };
    }
  }
}
