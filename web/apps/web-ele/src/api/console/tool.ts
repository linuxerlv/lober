import { requestClient } from '#/api/request';

/**
 * 磁盘 dry-run 端点（docs/contracts/http-console.md「7. 磁盘 dry-run」节，
 * 端点 27）。
 *
 * 与 templates/{id}/preview 同精神：把装机模板调试周期从「上电等十分钟」
 * 压到秒级（SPEC §8.2），不落地，纯计算。preview 与 dryrun 要给显眼入口，
 * 做成一等公民，别埋进二级菜单（SPEC §12.6）。
 */
/**
 * POST /disk-policy/dryrun 入参。facts 是一份模拟的硬件采集结果，
 * 字段形如 LOBER_DISKS 的实际取值。
 */
export interface DiskDryrunParams {
  diskPolicy: string;
  facts: Record<string, string>;
}

/** POST /disk-policy/dryrun 返回 */
export interface DiskPlanResult {
  chosenDisk: string;
  reasoning: string;
}

/** POST /disk-policy/dryrun */
export async function diskPolicyDryrunApi(data: DiskDryrunParams) {
  return requestClient.post<DiskPlanResult>('/disk-policy/dryrun', data);
}
