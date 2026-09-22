import { requestClient } from '#/api/request';

/**
 * 主机清单端点（docs/contracts/http-console.md「2. 主机清单」节，端点 5-6）。
 *
 * 字段名与 model.Host 逐一对应，不另起名字。GET /hosts 是建任务页 SN 字段的
 * 唯一数据源（host-select 组件从本端点取值，不是 input），q 参数用于主机数
 * 超过一页时仍能找到目标机器，是删掉 armed 状态后唯一人工确认点的必要条件
 * （SPEC §9.1、§10 失败模式 2b），不能省略。
 */

/** model.Arch 取值域（inventory.go） */
export type Arch = 'arm64' | 'x86_64';

/** 机器可上报的固件子集，不含 model.FirmwareMode 的 any（那只在 ArtifactSet 上出现） */
export type Firmware = 'bios' | 'uefi';

/** 主机清单条目，与 model.Host 对应 */
export interface HostListItem {
  arch: Arch;
  firmware: Firmware;
  id: string;
  lastSeenAt: number;
  mac: string;
  rack: string;
  sn: string;
  uuid: string;
}

/** GET /hosts 查询参数，均可选 */
export interface HostListParams {
  /** 默认 0 */
  offset?: number;
  /** 文本搜索，匹配 sn/mac/rack 子串（不区分大小写） */
  q?: string;
  /** 默认 1000，防止一次拉出几万行，不是给分页器用（约定 2） */
  limit?: number;
}

/** POST /hosts 入参，手工录入兜底（SPEC §7.4） */
export interface HostCreateParams {
  arch: Arch;
  firmware: Firmware;
  mac: string;
  sn: string;
  rack?: string;
  uuid?: string;
}

/**
 * GET /hosts?q=
 * 返回数组，不带总数，前端做客户端分页（约定 2）。不传 params 时取全量
 * （受 limit 默认上限 1000 约束），host-select 组件的搜索框用 q 参数过滤。
 */
export async function getHostListApi(params?: HostListParams) {
  return requestClient.get<HostListItem[]>('/hosts', { params });
}

/**
 * POST /hosts
 * MAC 入库前经 model.NormalizeMAC 归一化，格式不合法 → CodeMACInvalid。
 */
export async function createHostApi(data: HostCreateParams) {
  return requestClient.post<HostListItem>('/hosts', data);
}
