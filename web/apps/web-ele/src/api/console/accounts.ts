import type { Paths, Schemas } from './generated';

import { requestClient } from '#/api/request';

export type User = Schemas['UserResponse'];
export type UserCreate = Schemas['UserCreateRequest'];

type UserQuery = Pick<Paths['/console/v1/users']['get']['query'], 'page' | 'page_size'>;
type AccountHttp = Pick<typeof requestClient, 'delete' | 'get' | 'post' | 'put'>;

function userPath(username: string) {
  return `/users/${encodeURIComponent(username)}`;
}

export function createAccountsApi(client: AccountHttp) {
  return {
    list: ({ page = 1, page_size = 20 }: UserQuery = {}) =>
      client.get<Schemas['UserPage']>('/users', { params: { page, page_size } }),
    profile: (data: Schemas['ProfileRequest']) =>
      client.put<User>('/user/profile', data),
    create: (data: UserCreate) => client.post<User>('/users', data),
    update: (username: string, data: Schemas['UserUpdateRequest']) =>
      client.put<User>(userPath(username), data),
    disable: (username: string) => client.delete<null>(userPath(username)),
  };
}
export const accountsApi = createAccountsApi(requestClient);
export const roleLabels: Record<Schemas['StoreRole'], string> = {
  viewer: '只读用户',
  operator: '运维操作员',
  'hook-author': '脚本与模板作者',
  'hook-approver': '脚本与模板审批人',
  admin: '管理员',
};
