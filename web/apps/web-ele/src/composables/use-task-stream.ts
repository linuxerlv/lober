import type { TaskEvent } from '#/api/console/task';

import { readonly, ref } from 'vue';

type TaskStreamState = 'closed' | 'connecting' | 'open';

export function computeBackoffDelayMs(
  attempt: number,
  options: { baseMs?: number; maxMs?: number } = {},
): number {
  const baseMs = options.baseMs ?? 1000;
  const maxMs = options.maxMs ?? 30_000;
  if (attempt < 0) {
    return baseMs;
  }
  const delay = baseMs * 2 ** attempt;
  return Math.min(delay, maxMs);
}

/**
 * 解析一段 SSE 帧文本（不含结尾空行）为 { id, event, data }。
 *
 * SSE 帧形如：
 *   id: 110
 *   event: task
 *   data: {"seq":110,...}
 *
 * 纯函数，便于单测覆盖“缺 id 字段”“多行 data”等边界情况。
 */
export function parseSseFrame(
  frameText: string,
): null | { data: string; event?: string; id?: string } {
  const lines = frameText.split('\n');
  let id: string | undefined;
  let event: string | undefined;
  const dataLines: string[] = [];

  for (const rawLine of lines) {
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine;
    if (line.length === 0 || line.startsWith(':')) {
      continue;
    }
    const sepIndex = line.indexOf(':');
    const field = sepIndex === -1 ? line : line.slice(0, sepIndex);
    let value = sepIndex === -1 ? '' : line.slice(sepIndex + 1);
    if (value.startsWith(' ')) {
      value = value.slice(1);
    }
    switch (field) {
      case 'id': {
        id = value;
        break;
      }
      case 'event': {
        event = value;
        break;
      }
      case 'data': {
        dataLines.push(value);
        break;
      }
      default: {
        // 忽略 retry 等未使用的字段。
        break;
      }
    }
  }

  if (dataLines.length === 0) {
    return null;
  }
  return { data: dataLines.join('\n'), event, id };
}

interface UseTaskStreamOptions {
  after: () => number;
  onEvent: (event: TaskEvent) => void;
  onUnauthorized: () => void;
  taskId: string;
  token: string;
  url: string;
}

export function useTaskStream(options: UseTaskStreamOptions) {
  const state = ref<TaskStreamState>('connecting');
  const abort = new AbortController();
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let lastSeq = 0;
  let attempt = 0;

  function stop() {
    clearTimeout(reconnectTimer);
    abort.abort();
    state.value = 'closed';
  }

  async function connect() {
    try {
      const response = await fetch(options.url, {
        headers: {
          Accept: 'text/event-stream',
          Authorization: `Bearer ${options.token}`,
          'Last-Event-ID': String(Math.max(lastSeq, options.after())),
        },
        signal: abort.signal,
      });
      if (abort.signal.aborted) return;
      if (response.status === 401) {
        stop();
        options.onUnauthorized();
        return;
      }
      if (response.ok && response.body) {
        state.value = 'open';
        await read(response.body);
      } else {
        await response.body?.cancel();
      }
    } catch {
      // 断线后按最后收到的序号继续读。
    }
    if (abort.signal.aborted) return;
    state.value = 'connecting';
    reconnectTimer = setTimeout(() => void connect(), computeBackoffDelayMs(attempt++));
  }

  async function read(body: ReadableStream<Uint8Array>) {
    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    try {
      while (!abort.signal.aborted) {
        const { done, value } = await reader.read();
        if (done || abort.signal.aborted) return;
        buffer += decoder.decode(value, { stream: true });
        let separator = /\r?\n\r?\n/.exec(buffer);
        while (separator && !abort.signal.aborted) {
          receive(buffer.slice(0, separator.index));
          buffer = buffer.slice(separator.index + separator[0].length);
          separator = /\r?\n\r?\n/.exec(buffer);
        }
      }
    } finally {
      reader.releaseLock();
    }
  }

  function receive(text: string) {
    const frame = parseSseFrame(text);
    if (frame?.event !== 'task') return;
    let event: null | TaskEvent;
    try {
      event = JSON.parse(frame.data) as null | TaskEvent;
    } catch {
      return;
    }
    if (event?.taskID !== options.taskId || !Number.isSafeInteger(event.seq) || event.seq <= 0) return;
    options.onEvent(event);
    lastSeq = Math.max(lastSeq, event.seq);
    attempt = 0;
  }

  void connect();
  return { state: readonly(state), stop };
}
