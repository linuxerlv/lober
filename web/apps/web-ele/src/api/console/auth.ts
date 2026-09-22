import { requestClient } from '#/api/request';

/**
 * 认证相关端点（docs/contracts/http-console.md「1. 认证」节，端点 1-4）。
 *
 * 不做 refresh token（enableRefreshToken 默认 false），单 access token 即可
 * （SPEC §12.5）。
 */
/**
 * 本文件与 src/api/core/auth.ts + src/api/core/user.ts（框架默认认证 API）
 * 在契约层面等价——两者对应同一组端点，字段与路径完全一致。当前
 * src/store/auth.ts 已经在用 core 版本且已满足契约，因此本文件不接入任何
 * 消费方，只作为「按 SPEC §12.5 表格逐条对齐契约」的独立参照实现存在，
 * 避免与 core 版本产生重复导出冲突（两者函数名相同）。
 */

/** POST /auth/login 入参 */
export interface AuthLoginParams {
  password: string;
  username: string;
}

/** POST /auth/login 返回 */
export interface AuthLoginResult {
  accessToken: string;
}

/** GET /user/info 返回。roles 取值域见 SPEC §9.4 五个角色之一 */
export interface ConsoleUserInfo {
  realName: string;
  roles: string[];
  username: string;
}

/** POST /auth/login */
export async function consoleLoginApi(data: AuthLoginParams) {
  return requestClient.post<AuthLoginResult>('/auth/login', data);
}

/** GET /user/info */
export async function getConsoleUserInfoApi() {
  return requestClient.get<ConsoleUserInfo>('/user/info');
}

/** GET /auth/codes，权限码集合 */
export async function getConsoleAccessCodesApi() {
  return requestClient.get<string[]>('/auth/codes');
}

/** POST /auth/logout */
export async function consoleLogoutApi() {
  return requestClient.post<null>('/auth/logout');
}
