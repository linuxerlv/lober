import { requestClient } from '#/api/request';

/**
 * 任务端点（docs/contracts/http-console.md「6. 任务」节，端点 21-26）。
 *
 * 字段名与 model.Task 对应，省略 token/tokenExpiresAt/finalizeToken/
 * finalizeExpiresAt 四个敏感字段——人面 API 不回传任务的装机凭据，这是安全
 * 边界，不是字段名对齐问题。
 *
 * 用命名空间导出（而不是像 host/artifact/template 那样拍平）：
 * src/views/tasks/detail.vue（另一位 agent 的产出）已经按
 * `ConsoleTaskApi.TaskItem` / `ConsoleTaskApi.Phase` 等命名空间引用写好并
 * 大量使用，此文件是它的唯一数据来源，保持这个形状是避免破坏已完成页面的
 * 唯一选择。
 */
export namespace ConsoleTaskApi {
  /** model.TaskState 取值域（task.go），七个取值，终态是 done 与 failed */
  export type TaskState =
    | 'booting'
    | 'done'
    | 'failed'
    | 'initializing'
    | 'installing'
    | 'pending'
    | 'rebooting';

  /** model.Phase 取值域（task.go） */
  export type Phase =
    | 'booting'
    | 'done'
    | 'initializing'
    | 'packages'
    | 'pending'
    | 'post'
    | 'pre'
    | 'rebooting';

  /**
   * model.FailureReason 取值域（task.go），空串表示未失败。
   *
   * 后三个（reboot_timeout/init_timeout/init_failed）对应 Reinstallable()
   * === false——机器可能已经装好、盘上已有数据，重装会擦掉，服务端拒绝这些
   * 原因的 retry（CodeConflict）。前端据 NON_REINSTALLABLE_REASONS 预先禁用
   * retry 按钮只是体验优化，服务端判定才是真正的防线（不在前端重新实现这份
   * 白名单，只是抄一份用于禁用态展示）。
   */
  export type FailureReason =
    | ''
    | 'attempt_exceeded'
    | 'boot_timeout'
    | 'canceled'
    | 'hook_required'
    | 'init_failed'
    | 'init_timeout'
    | 'install_phase'
    | 'precheck'
    | 'reboot_timeout';

  export interface FailureDetail {
    exitCode: number;
    hookDigest: string;
    logTail: string;
    phase: Phase;
    sinceLastSig: string;
    syslogTail: string;
  }

  /** 任务条目，与 model.Task 对应（不含敏感凭据字段） */
  export interface TaskItem {
    artifactSetId: string;
    attemptCount: number;
    createdAt: number;
    createdBy: string;
    currentPhase: Phase;
    failureDetail: FailureDetail | null;
    failureReason: FailureReason;
    formData: Record<string, string>;
    hookDigests: string[];
    hostMac: string;
    hostSn: string;
    hostUuid: string;
    id: string;
    lastSeq: number;
    lastSignalAt: number;
    phaseEnteredAt: number;
    resolvedParams: Record<string, string>;
    state: TaskState;
    stateUpdatedAt: number;
    templateId: string;
    templateVersion: number;
    version: number;
  }

  /**
   * POST /tasks 入参。约定 3：入参是主机清单 id，不是 SN 字符串——服务端据
   * hostId 从主机清单查出 sn/mac 写进任务，两者构成引导时的双因子。
   * 本端点不接受直接传入 hostSn/hostMac 字段，因此这里刻意不声明它们，
   * 避免调用方以为可以绕过主机清单选择。
   */
  export interface CreateParams {
    formData: Record<string, string>;
    hostId: string;
    templateId: string;
  }

  /** GET /tasks 查询参数，均为精确匹配（sn 不做子串搜索） */
  export interface ListParams {
    sn?: string;
    state?: TaskState;
  }

  /** GET /tasks/{id}/events 返回条目，与 model.TaskEvent 对应 */
  export interface TaskEventItem {
    hookDigest: string;
    level: 'error' | 'info' | 'warn';
    message: string;
    phase: Phase;
    rc: number;
    seq: number;
    source: 'heartbeat' | 'nginx' | 'provider' | 'signal' | 'syslog';
    taskId: string;
    ts: number;
  }
}

/** 不可重装的失败原因集合（Reinstallable() 的白名单取反），供 UI 禁用 retry 按钮 */
export const NON_REINSTALLABLE_REASONS: readonly ConsoleTaskApi.FailureReason[] =
  ['reboot_timeout', 'init_timeout', 'init_failed'];

/**
 * POST /tasks
 * 建任务即授权装机，无 arm/disarm（SPEC §3.2、§12.5）。
 * 失败：hostId 查不到 → CodeNotFound；templateId 非 active → CodeConflict。
 */
export async function createTaskApi(data: ConsoleTaskApi.CreateParams) {
  return requestClient.post<ConsoleTaskApi.TaskItem>('/tasks', data);
}

/** GET /tasks?state=&sn=，返回数组（同约定 2，limit 默认上限 1000） */
export async function getTasksApi(params?: ConsoleTaskApi.ListParams) {
  return requestClient.get<ConsoleTaskApi.TaskItem[]>('/tasks', { params });
}

/** GET /tasks/{id}/events，按 seq 升序返回全量（不分页） */
export async function getTaskEventsApi(id: string) {
  return requestClient.get<ConsoleTaskApi.TaskEventItem[]>(
    `/tasks/${id}/events`,
  );
}

/**
 * POST /tasks/{id}/retry
 * 重置 attemptCount 为 0，签发新 token，状态置回 pending。
 * 拒绝条件：failureReason 对应 Reinstallable() === false 时必须拒绝
 * （CodeConflict）——这类任务的机器可能已经装好，重装会擦掉数据。
 */
export async function retryTaskApi(id: string) {
  return requestClient.post<ConsoleTaskApi.TaskItem>(`/tasks/${id}/retry`);
}

/**
 * POST /tasks/{id}/cancel
 * 状态置为终态 failed，failureReason=canceled。失败：任务已处于终态 →
 * CodeConflict（终态不可二次取消）。
 */
export async function cancelTaskApi(id: string) {
  return requestClient.post<ConsoleTaskApi.TaskItem>(`/tasks/${id}/cancel`);
}

/**
 * GET /tasks/{id}/stream 的相对路径（挂在 /console/v1 下）。
 *
 * 这个端点不套 {code,data,message} 响应包裹，是 SSE 事件流（约定 4），
 * 因此不能用 requestClient 的默认拦截器——本函数只返回拼好的相对路径，
 * 调用方（如任务详情页）需要自行拼上 baseURL 传给
 * src/composables/use-task-stream.ts 的 fetch + ReadableStream 实现，
 * Authorization 头照常带（不接受 token 进 query string）。
 */
export function taskStreamUrl(id: string) {
  return `/tasks/${id}/stream`;
}
