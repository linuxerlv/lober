import type { AuditRecordItem } from './audit';

import { requestClient } from '#/api/request';

/**
 * 审计查询与三张装机统计报表（`docs/contracts/http-console.md`「审计与报表」
 * 节，端点 30-37；实现计划 P4-04）。
 *
 * 八个端点**全部要求 admin 角色**。理由：审计记录含全体操作人的动作明细，
 * 报表含全局装机数据。P4-04 任务 1 讨论过是否引入「审计角色」——SPEC §9.4
 * 没有定义它，新增角色是契约变更，故先只给管理员。
 *
 * 时间范围参数在全部端点上统一为 `since` / `until`（Unix 秒，**闭区间**）。
 * 闭区间与后端 `AuditFilter` / `TaskFilter` 一致是刻意的：运维在同一页面的
 * 不同标签上看同一个时间窗口，包含性不同会让边界记录在一处出现、另一处消失。
 */

/** 时间范围查询参数，两端都是 Unix 秒、闭区间；省略即该端不过滤 */
export interface TimeRangeParams {
  since?: number;
  until?: number;
  limit?: number;
}

/** 端点 30 的查询参数：四项条件按「与」相交，全部可选 */
export interface AuditQueryParams extends TimeRangeParams {
  /** 操作人，精确匹配不做子串（查 bob 不会带出 bobby 的记录） */
  actor?: string;
  /** 动作类型，如 "task.create" */
  action?: string;
  /** 目标资源 id */
  target?: string;
}

/** 装机量报表的一行：某个状态的任务数 */
export interface ProvisioningStateRow {
  state: string;
  count: number;
}

/** 端点 31 返回：按状态分组的任务计数 */
export interface ProvisioningReport {
  total: number;
  byState: ProvisioningStateRow[];
}

/** 失败分布报表的一行 */
export interface FailureReasonRow {
  /** 失败原因，SPEC §3.4 的九种取值之一 */
  reason: string;
  count: number;
  /** 占比，分母是本次统计到的失败任务总数（不是全部任务数） */
  share: number;
  /**
   * 是否允许重装（SPEC §3.4 的白名单）。后端直接带上，前端**不要**自己再
   * 维护一份副本——副本迟早与 `model.FailureReason.Reinstallable()` 漂移，
   * 而漂移方向若是「把不可重装误标成可重装」，代价是运维照着报表点重试、
   * 擦掉一台已经装好的机器。
   */
  reinstallable: boolean;
}

/** 端点 32 返回：按失败原因分组，按计数降序 */
export interface FailureReport {
  totalFailed: number;
  byReason: FailureReasonRow[];
}

/** 阶段耗时报表的一行 */
export interface PhaseDurationRow {
  phase: string;
  /**
   * 真正算出了耗时的任务数，**不是**落在该阶段的任务数。中途失败的任务只有
   * 部分阶段有事件，算不出耗时的被跳过而非算成 0——所以这个数要显示出来，
   * 运维才知道中位数有多少可信度（3 个样本和 300 个样本不该看起来一样权威）。
   */
  sampleCount: number;
  /** 中位数。口径定死用中位数与高分位，不用平均值（装机耗时是长尾分布） */
  medianSec: number;
  p95Sec: number;
  maxSec: number;
}

/** 端点 33 返回 */
export interface PhaseDurationReport {
  byPhase: PhaseDurationRow[];
}

/** 溯源视图里解析后的模板信息 */
export interface ProvenanceTemplate {
  id: string;
  name: string;
  /** pin 住的版本号。对象已删除时仍然给出——它是从备份里找回那一版的唯一线索 */
  version: number;
  missing: boolean;
  hint: string;
}

/** 溯源视图里解析后的制品集信息 */
export interface ProvenanceArtifactSet {
  id: string;
  name: string;
  osFamily: string;
  osVersion: string;
  missing: boolean;
  hint: string;
}

/**
 * 溯源视图里解析后的 hook 信息。作者与审批人是追责链上最关键的一环——
 * hook 以 root 身份在分区之前运行，能力等同「擦掉这台机器所有盘」（SPEC §9.4）。
 */
export interface ProvenanceHook {
  digest: string;
  id: string;
  name: string;
  stage: string;
  version: number;
  author: string;
  approver: string;
  approvedAt: number;
  missing: boolean;
  hint: string;
}

/** 溯源视图里的一条后续操作（含 P4-01 的自动重试，其 actor 不是人） */
export interface ProvenanceOperation {
  ts: number;
  actor: string;
  action: string;
  detail: string;
}

/** 时间线上的一条事件，字段与 TaskEventItem 一致 */
export interface ProvenanceEvent {
  taskId: string;
  seq: number;
  ts: number;
  phase: string;
  level: string;
  message: string;
  rc: number;
  hookDigest: string;
  source: string;
}

/** SPEC §3.5 的六个失败诊断字段 */
export interface ProvenanceFailureDetail {
  phase: string;
  exitCode: number;
  hookDigest: string;
  logTail: string;
  syslogTail: string;
  sinceLastSig: string;
}

/**
 * 端点 34 返回：任务溯源的五块内容。
 *
 * 回答「误装事故的四个问题」（P4-04 任务 1）：谁在什么时候建了那个任务；当时
 * 选的是哪台机器、哪个模板、哪个制品集；用了哪些 hook、谁写的谁批的；从建任务
 * 到擦盘之间发生了什么。
 *
 * **`missing` 标记不是错误。** SPEC §2.7 只保证「被 active 模板引用的 hook
 * 不能删」，模板本身在任务终态后可以删；而事故往往发生在几个月后，那时相关
 * 对象可能已经清理过。UI 要把 `missing` 渲染成一条提示，不要当成加载失败。
 */
export interface TaskProvenance {
  taskId: string;
  createdAt: number;
  createdBy: string;
  /** 建任务时的客户端 IP。同一账号被多人共用时，只能靠它区分到底是谁 */
  createdFromIp: string;
  hostSn: string;
  hostMac: string;
  hostUuid: string;
  state: string;
  failureReason: string;
  template: ProvenanceTemplate;
  artifactSet: ProvenanceArtifactSet;
  hooks: ProvenanceHook[];
  timeline: ProvenanceEvent[];
  failureDetail: null | ProvenanceFailureDetail;
  operations: ProvenanceOperation[];
}

/** 端点 30：GET /audit，四项条件按「与」相交，按时间倒序返回 */
export async function getAuditApi(params?: AuditQueryParams) {
  return requestClient.get<AuditRecordItem[]>('/audit', { params });
}

/** 端点 31：GET /reports/provisioning */
export async function getProvisioningReportApi(params?: TimeRangeParams) {
  return requestClient.get<ProvisioningReport>('/reports/provisioning', {
    params,
  });
}

/** 端点 32：GET /reports/failures */
export async function getFailureReportApi(params?: TimeRangeParams) {
  return requestClient.get<FailureReport>('/reports/failures', { params });
}

/** 端点 33：GET /reports/phase-durations */
export async function getPhaseDurationReportApi(params?: TimeRangeParams) {
  return requestClient.get<PhaseDurationReport>('/reports/phase-durations', {
    params,
  });
}

/** 端点 34：GET /tasks/{id}/provenance */
export async function getTaskProvenanceApi(taskId: string) {
  return requestClient.get<TaskProvenance>(
    `/tasks/${encodeURIComponent(taskId)}/provenance`,
  );
}

/**
 * CSV 导出的通用取数（端点 35-37 共用）。
 *
 * 两个必须一起给的配置项：
 *
 * - `responseType: 'blob'` —— 响应体是「UTF-8 BOM + CSV 文本」，当文本读会
 *   让 BOM 参与字符串拼接，落盘后反而丢掉它的作用。
 * - `responseReturn: 'raw'` —— 拿到完整的 axios 响应而不是解包后的 `data`。
 *   两个原因：其一，`statusCodeResponseInterceptor` 对非 raw 的响应会去读
 *   `body.code` 做信封解包，而 CSV 没有信封，会被判成契约违反并抛错
 *   （见 `../response-interceptor.ts` 第 120 行的 raw 提前返回）；其二，
 *   文件名在 `Content-Disposition` 响应头里，只有 raw 才拿得到 headers。
 *
 * 为什么不直接拼一个 URL 交给浏览器原生下载（那样零内存占用）：浏览器导航
 * 不会带自定义请求头，而后端用的是 `Authorization: Bearer <token>`，那条路径
 * 会被判成未认证。这里牺牲「零内存」换「能带认证头」；后端的 10 万行上限
 * （约 10 MB 量级）让这个折中可以接受。
 */
async function fetchCSV(
  path: string,
  params?: AuditQueryParams | TimeRangeParams,
): Promise<{ blob: Blob; filename: string }> {
  const response = await requestClient.get<any>(path, {
    params,
    responseReturn: 'raw',
    responseType: 'blob',
  });

  return {
    blob: response.data as Blob,
    filename: filenameFromContentDisposition(
      response.headers?.['content-disposition'],
    ),
  };
}

/**
 * 从 `Content-Disposition` 解析文件名。后端给的形如
 * `attachment; filename="audit-1735689600.csv"`（英文加时间戳——文件名刻意
 * 不用中文，跨平台下载时中文文件名会遇到编码问题；列名才用中文）。
 *
 * 解析不出时退化成一个本地生成的同形状名字，不抛错：文件内容已经拿到了，
 * 为一个文件名让整次导出失败不成比例。
 */
function filenameFromContentDisposition(raw: unknown): string {
  const fallback = `export-${Math.floor(Date.now() / 1000)}.csv`;
  if (typeof raw !== 'string') {
    return fallback;
  }
  const matched = /filename="?([^";]+)"?/.exec(raw);
  return matched?.[1] ?? fallback;
}

/** 端点 35：GET /audit/export */
export async function exportAuditCSV(params?: AuditQueryParams) {
  return fetchCSV('/audit/export', params);
}

/** 端点 36：GET /reports/provisioning/export */
export async function exportProvisioningCSV(params?: TimeRangeParams) {
  return fetchCSV('/reports/provisioning/export', params);
}

/** 端点 37：GET /tasks/{id}/provenance/export */
export async function exportProvenanceCSV(taskId: string) {
  return fetchCSV(`/tasks/${encodeURIComponent(taskId)}/provenance/export`);
}

/**
 * 把取到的 blob 触发成一次浏览器下载。
 *
 * `URL.revokeObjectURL` 必须调用——不调用的话每次导出都会在页面生命周期内
 * 泄漏一个 blob（几十 MB 量级，运维连着导十几次就很显眼了）。
 */
export function triggerDownload(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}
