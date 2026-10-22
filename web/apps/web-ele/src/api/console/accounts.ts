import type { ConsoleRole, ConsoleSessionUser } from './session';

import type { NumberedPageParams } from '#/composables/numbered-page';

import { requestClient } from '#/api/request';
export interface ProfileInput {
  displayName: string;
  email: string;
  avatar: string;
  bio: string;
  rev: number;
}
export interface AccountInput {
  username: string;
  displayName: string;
  role: ConsoleRole;
  password: string;
}
export interface AccountUpdate {
  displayName: string;
  role: ConsoleRole;
  disabled: boolean;
  rev: number;
  password?: string;
}
interface AccountHttp {
  get<T>(path: string): Promise<T>;
  post<T>(path: string, data: unknown): Promise<T>;
  put<T>(path: string, data: unknown): Promise<T>;
}
export function createAccountsApi(client: AccountHttp) {
  return {
    list: ({ page = 1, page_size = 20 }: Partial<NumberedPageParams> = {}) =>
      client.get<{ items: ConsoleSessionUser[]; next: string; total: number }>(
        `/users?page=${page}&page_size=${page_size}`,
      ),
    profile: (data: ProfileInput) =>
      client.put<ConsoleSessionUser>('/user/profile', data),
    create: (data: AccountInput) =>
      client.post<ConsoleSessionUser>('/users', data),
    update: (username: string, data: AccountUpdate) =>
      client.put<ConsoleSessionUser>(
        `/users/${encodeURIComponent(username)}`,
        data,
      ),
  };
}
export const accountsApi = createAccountsApi(requestClient);
export const roleLabels: Record<ConsoleRole, string> = {
  viewer: '只读用户',
  operator: '运维操作员',
  'hook-author': '脚本与模板作者',
  'hook-approver': '脚本与模板审批人',
  admin: '管理员',
};
