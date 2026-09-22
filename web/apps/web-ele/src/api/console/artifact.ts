import { requestClient } from '#/api/request';

/**
 * 制品集端点（docs/contracts/http-console.md「3. 制品集」节，端点 7-10）。
 *
 * 字段名与 model.ArtifactSet 对应。
 */

/** model.ArtifactSetState 取值域（inventory.go） */
export type ArtifactSetState = 'deprecated' | 'failed' | 'importing' | 'ready';

export interface ArtifactSetPaths {
  extra: Record<string, string>;
  initrd: string[];
  kernel: string;
  repo: string;
}

/** 制品集条目，与 model.ArtifactSet 对应 */
export interface ArtifactSetItem {
  arch: string;
  firmware: string;
  id: string;
  manifestSha256: string;
  name: string;
  osFamily: string;
  osVersion: string;
  paths: ArtifactSetPaths;
  sourceIsoSha256: string;
  state: ArtifactSetState;
}

/**
 * POST /artifact-sets 入参。气隙环境以登记服务器本地路径为主（SPEC §2.1），
 * MVP 不实现 WebUI 直传大文件。
 */
export interface ArtifactSetCreateParams {
  name: string;
  sourcePath: string;
}

/** GET /artifact-sets，返回数组（同约定 2） */
export async function getArtifactSetListApi() {
  return requestClient.get<ArtifactSetItem[]>('/artifact-sets');
}

/**
 * POST /artifact-sets
 * 导入是异步 job，本端点立即返回 state=importing，进度靠前端轮询
 * GET /artifact-sets 观察。
 */
export async function createArtifactSetApi(data: ArtifactSetCreateParams) {
  return requestClient.post<ArtifactSetItem>('/artifact-sets', data);
}

/**
 * POST /artifact-sets/{id}/verify
 * 重算 manifestSha256，异步执行，结果体现在下次 GET /artifact-sets 的 state。
 */
export async function verifyArtifactSetApi(id: string) {
  return requestClient.post<null>(`/artifact-sets/${id}/verify`);
}

/**
 * DELETE /artifact-sets/{id}
 * 前置条件：仅 state=deprecated 且未被任何 active template 引用时可删除
 * （SPEC §2.7）。失败：state 不对 → CodeConflict；仍被引用 → CodeReferencedResource。
 */
export async function deleteArtifactSetApi(id: string) {
  return requestClient.delete<null>(`/artifact-sets/${id}`);
}
