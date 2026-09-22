import { requestClient } from '#/api/request';

/**
 * hook 端点（docs/contracts/http-console.md「4. hook」节，端点 11-16、28）。
 *
 * 字段名与 model.Hook 对应。POST /hooks/{id}/reject 是本轮新增端点
 * （契约文档变更记录），与 approve 对称：review → draft 单步回退。
 */
/** model.HookStage 取值域（authoring.go） */
export type HookStage = 'firstboot' | 'post' | 'post-nochroot' | 'pre';

/** model.HookState 取值域，四个取值是冻结契约 */
export type HookState = 'active' | 'deprecated' | 'draft' | 'review';

export interface HookRequires {
  capabilities: string[];
  contractVersion: string;
  facts: string[];
}

/** hook 条目，与 model.Hook 对应 */
export interface HookItem {
  approvedAt: number;
  approver: string;
  author: string;
  body: string;
  bodySha256: string;
  description: string;
  id: string;
  interpreter: string;
  name: string;
  osFamilies: string[];
  paramsSchema: string;
  required: boolean;
  requires: HookRequires;
  stage: HookStage;
  state: HookState;
  timeoutSec: number;
  version: number;
}

/** POST /hooks 与 PUT /hooks/{id} 共用入参：除服务端计算字段外的全部字段 */
export interface HookUpsertParams {
  body: string;
  description: string;
  interpreter: string;
  name: string;
  osFamilies: string[];
  paramsSchema: string;
  required: boolean;
  requires: HookRequires;
  stage: HookStage;
  timeoutSec: number;
}

/** GET /hooks 查询参数，均为精确匹配过滤（不做子串搜索） */
export interface HookListParams {
  os_family?: string;
  stage?: HookStage;
}

export interface HookValidateIssue {
  level: 'error' | 'warning';
  message: string;
}

/** POST /hooks/{id}/validate 返回。passed=false 时仍是成功响应，不是请求失败 */
export interface HookValidateResult {
  issues: HookValidateIssue[];
  passed: boolean;
}

/** POST /hooks/{id}/reject 入参，reason 必填且不能是空白字符串 */
export interface HookRejectParams {
  reason: string;
}

/** POST /hooks/{id}/reject 返回，HookItem 附加回显的 reason */
export interface HookRejectResult extends HookItem {
  reason: string;
}

/** POST /hooks，返回 state=draft 的新 HookItem */
export async function createHookApi(data: HookUpsertParams) {
  return requestClient.post<HookItem>('/hooks', data);
}

/** GET /hooks?stage=&os_family=，返回数组（同约定 2） */
export async function getHookListApi(params?: HookListParams) {
  return requestClient.get<HookItem[]>('/hooks', { params });
}

/**
 * PUT /hooks/{id}
 * 建新版本，不改旧版本（hook 正文不可变）。新记录 version = 旧版本 + 1，
 * bodySha256 按新 body 重算，state 重置为 draft。
 */
export async function updateHookApi(id: string, data: HookUpsertParams) {
  return requestClient.put<HookItem>(`/hooks/${id}`, data);
}

/** POST /hooks/{id}/validate */
export async function validateHookApi(id: string) {
  return requestClient.post<HookValidateResult>(`/hooks/${id}/validate`);
}

/** GET /hooks/{id}/versions，该 id 对应 hook 的全部历史版本，按 version 升序 */
export async function getHookVersionsApi(id: string) {
  return requestClient.get<HookItem[]>(`/hooks/${id}/versions`);
}

/**
 * POST /hooks/{id}/approve
 * draft → review → active 单向流转，每次调用推进一步。
 * review → active 要求审批人不能等于作者（SPEC §9.4）。
 */
export async function approveHookApi(id: string) {
  return requestClient.post<HookItem>(`/hooks/${id}/approve`);
}

/**
 * POST /hooks/{id}/reject（本轮新增，见契约文档变更记录）
 * review → draft 单步回退，仅当前 state=review 时可用；驳回人不能等于作者。
 * 不改动 body/bodySha256。
 */
export async function rejectHookApi(id: string, data: HookRejectParams) {
  return requestClient.post<HookRejectResult>(`/hooks/${id}/reject`, data);
}
