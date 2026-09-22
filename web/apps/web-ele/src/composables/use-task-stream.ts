/**
 * 任务 SSE 实时事件流 composable。
 *
 * 契约依据：docs/contracts/http-console.md 约定 4、SPEC §12.8。
 *
 * 为什么不用浏览器内置的 EventSource：EventSource 不能设请求头，只能把
 * token 塞进 query string，而 token 进 query 会落进 nginx access log
 * （SPEC §9.5 刚要求清理这类记录）。因此改用 fetch + ReadableStream 手动解析
 * SSE 帧，Authorization 头照常带在请求头里。
 *
 * 四个实现要点（对应 SPEC §12.8 / 契约约定 4）：
 * 1. 断线用指数退避重连，重连带 Last-Event-ID，服务端按 task_event.seq 续传，
 *    不从头重放。
 * 2. 组件卸载必须 AbortController.abort()，否则列表与详情来回切会攒住一堆
 *    连接。
 * 3. 服务端已回 X-Accel-Buffering: no、nginx 已关 proxy_buffering，前端无需
 *    处理，但要知道日志不会被中间层缓冲冻住。
 * 4. 任务进终态（done / failed）由服务端主动关流，前端收到终态事件即停止
 *    重连，不做这条会无限重连一个已结束的任务。
 */
import type { Ref } from 'vue';

import { onScopeDispose, readonly, ref, shallowRef } from 'vue';

/** 与 model.TaskEvent 的 JSON 序列化对应（docs/contracts/http-console.md 约定 4）。 */
export interface TaskStreamEvent {
  taskId?: string;
  seq: number;
  ts: number;
  phase: string;
  level: string;
  message: string;
  rc: number;
  hookDigest: string;
  source: string;
}

/** 任务状态机的终态取值。非终态（pending/booting/... 等）一律触发重连。 */
export type TaskStreamState = 'closed' | 'connecting' | 'error' | 'open';

/** 与 model.TaskState 对应的终态集合——done / failed 之外都视为非终态。 */
const TERMINAL_TASK_STATES = new Set(['done', 'failed']);

/**
 * 判定一个任务状态是否为终态。
 *
 * 只依据事件里显式携带的状态字段判断，不做启发式猜测——契约里 SSE 事件本身
 * 不直接带 task.state 字段，终态判定实际落在 `isTerminalPhase` 上（服务端
 * 在进入终态时发送 phase="done" 的信号事件后主动关流）。此函数保留给未来
 * 契约扩展直接携带 state 字段时复用，当前实现以 isTerminalPhase 为准。
 */
export function isTerminalTaskState(state: string): boolean {
  return TERMINAL_TASK_STATES.has(state);
}

/**
 * 判定一条任务事件是否代表任务进入终态。
 *
 * 依据：契约约定 4——服务端在任务进入 done/failed 后发送该终态事件即主动
 * 关闭连接。前端收到 phase 为 done 或 failed 的事件即视为终态，停止重连。
 */
export function isTerminalPhase(phase: string): boolean {
  return TERMINAL_TASK_STATES.has(phase);
}

/**
 * 指数退避重连延迟计算（纯函数，便于单测）。
 *
 * attempt 从 0 开始计数（第一次重连前的等待）。基础延迟 1s，每次翻倍，
 * 上限封顶 30s，避免长时间断网后恢复时退避时间过长导致日志显示滞后过久。
 */
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
 *   event: task-event
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

export interface UseTaskStreamOptions {
  /** 鉴权头取值，形如 `Bearer <token>`；由调用方从 access store 取，不在此处依赖具体 store。 */
  authorizationHeader: () => string | undefined;
  /** SSE 端点完整 URL（含 baseURL），例如 `/api/console/v1/tasks/{id}/stream`。 */
  url: string;
  /** 每收到一条事件的回调（用于追加日志、更新时间线等）。 */
  onEvent?: (event: TaskStreamEvent) => void;
  /** 任务进入终态时的回调。 */
  onTerminal?: (event: TaskStreamEvent) => void;
  /** 重连退避参数，主要供测试覆盖默认值。 */
  backoff?: { baseMs?: number; maxMs?: number };
}

export interface UseTaskStreamReturn {
  /** 连接状态，供页面展示“连接中/已连接/已断开”等提示。 */
  state: Readonly<Ref<TaskStreamState>>;
  /** 最近一次错误信息（非终态原因导致的断线不算错误，会静默重连）。 */
  lastError: Readonly<Ref<string | undefined>>;
  /** 主动停止（组件卸载会自动调用，也可手动调用以切走页面时提前中止）。 */
  stop: () => void;
}

/**
 * 订阅单个任务的 SSE 事件流。
 *
 * 用法：
 *   const { state, stop } = useTaskStream({
 *     url: `${apiURL}/console/v1/tasks/${taskId}/stream`,
 *     authorizationHeader: () => `Bearer ${accessStore.accessToken}`,
 *     onEvent: (e) => events.value.push(e),
 *     onTerminal: () => { ... },
 *   });
 *
 * 组件卸载时通过 onScopeDispose 自动 abort，调用方无需手动清理，但页面
 * 切换场景（如详情页内部 taskId 变化）应显式调用 stop() 后再重新订阅。
 */
export function useTaskStream(
  options: UseTaskStreamOptions,
): UseTaskStreamReturn {
  const state = ref<TaskStreamState>('connecting');
  const lastError = shallowRef<string | undefined>(undefined);

  let abortController: AbortController | undefined;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let lastEventId: string | undefined;
  let attempt = 0;
  let stopped = false;

  function clearReconnectTimer() {
    if (reconnectTimer !== undefined) {
      clearTimeout(reconnectTimer);
      reconnectTimer = undefined;
    }
  }

  function scheduleReconnect() {
    if (stopped) {
      return;
    }
    clearReconnectTimer();
    const delay = computeBackoffDelayMs(attempt, options.backoff);
    attempt += 1;
    reconnectTimer = setTimeout(() => {
      void connect();
    }, delay);
  }

  async function connect() {
    if (stopped) {
      return;
    }
    state.value = 'connecting';
    abortController = new AbortController();

    const headers: Record<string, string> = {};
    const authHeader = options.authorizationHeader();
    if (authHeader) {
      headers.Authorization = authHeader;
    }
    if (lastEventId !== undefined) {
      headers['Last-Event-ID'] = lastEventId;
    }

    try {
      const response = await fetch(options.url, {
        headers,
        signal: abortController.signal,
      });

      if (!response.ok || !response.body) {
        throw new Error(`SSE 连接失败：HTTP ${response.status}`);
      }

      state.value = 'open';
      attempt = 0; // 连接成功后重置退避计数
      lastError.value = undefined;

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) {
          break;
        }
        buffer += decoder.decode(value, { stream: true });

        // SSE 帧以空行（\n\n 或 \r\n\r\n）分隔。
        let frameEnd = buffer.indexOf('\n\n');
        while (frameEnd !== -1) {
          const frameText = buffer.slice(0, frameEnd);
          buffer = buffer.slice(frameEnd + 2);

          const frame = parseSseFrame(frameText);
          if (frame) {
            if (frame.id !== undefined) {
              lastEventId = frame.id;
            }
            try {
              const parsed = JSON.parse(frame.data) as TaskStreamEvent;
              options.onEvent?.(parsed);
              if (isTerminalPhase(parsed.phase)) {
                options.onTerminal?.(parsed);
                stopped = true;
                state.value = 'closed';
                abortController?.abort();
                return;
              }
            } catch {
              // 单条事件解析失败不应打断整条连接，跳过该帧继续读。
            }
          }
          frameEnd = buffer.indexOf('\n\n');
        }
      }

      // 服务端正常结束响应体（未显式发终态事件也会走到这里，例如网络中间层
      // 主动断开）——视为需要重连的断线，不视为错误。
      if (!stopped) {
        state.value = 'connecting';
        scheduleReconnect();
      }
    } catch (error) {
      if (abortController?.signal.aborted && stopped) {
        // 主动 stop() 触发的 abort，不是错误。
        return;
      }
      state.value = 'error';
      lastError.value = error instanceof Error ? error.message : String(error);
      scheduleReconnect();
    }
  }

  function stop() {
    stopped = true;
    clearReconnectTimer();
    abortController?.abort();
    state.value = 'closed';
  }

  void connect();

  onScopeDispose(() => {
    stop();
  });

  return {
    state: readonly(state),
    lastError: readonly(lastError),
    stop,
  };
}
