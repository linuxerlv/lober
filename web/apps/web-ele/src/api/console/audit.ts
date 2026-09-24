import { requestClient } from '#/api/request';

/**
 * 审计查询端点（docs/contracts/http-console.md「4. hook」节，端点 29：
 * `GET /hooks/{id}/audit?limit=`，本轮契约变更新增）。
 *
 * 背景：`POST /hooks/{id}/reject` 的驳回理由此前只在驳回当次的响应体里
 * 可见（`HookRejectResult.reason`），作者若当时不在场就永远查不到——这是
 * `docs/contracts/http-console.md`「变更记录 2」记录的契约缺口。本端点
 * 补上事后查询路径，与当场回显并存，不是取代它。
 */

/** 单条审计记录，字段名与 contract.AuditRecord 对应 */
export interface AuditRecordItem {
  /** 写入时刻，Unix 秒（与 HookItem.approvedAt 同单位，*1000 转 Date） */
  ts: number;
  /** 操作人用户名 */
  actor: string;
  /** 动作标识，如 "hook.reject" */
  action: string;
  /** 操作目标，本端点场景下恒等于路径里的 hook id */
  target: string;
  /** 详情原文（驳回场景下含理由原文） */
  detail: string;
}

/** GET /hooks/{id}/audit 查询参数，limit 可选，服务端默认 1000 */
export interface AuditListParams {
  limit?: number;
}

/**
 * GET /hooks/{id}/audit
 * 按时间倒序返回（最新的在前，服务端已排好序，调用方不需要再排）。
 * 没有审计记录时返回空数组，不是错误。
 *
 * 权限：hook-author（hook:write）或 hook-approver（hook:approve）之一，
 * admin 通过聚合权限码天然满足。无权限时服务端返回 HTTP 403、
 * `{code: 1003, data: null}`（状态码与 code 是真实值，不是固定 200，
 * 见 docs/superpowers/specs/2026-09-24-gin-migration-contract.md §0），
 * 会被 requestClient 的响应拦截器识别为失败并 throw——调用方需要用
 * err.response?.data?.code === 1003（或等价地 err.response?.status === 403，
 * 但 403 下只有这一种业务分支，不需要额外区分）区分「无权限」与其它
 * 失败，不要笼统展示成「加载失败」。
 */
export async function getHookAuditApi(id: string, params?: AuditListParams) {
  return requestClient.get<AuditRecordItem[]>(`/hooks/${id}/audit`, {
    params,
  });
}
