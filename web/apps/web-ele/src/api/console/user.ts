import type { Paths, Schemas } from './generated';

import { requestClient } from '#/api/request';
export type User = Schemas['UserResponse'];
export type UserCreate = Schemas['UserCreateRequest'];
export type UserUpdate = Schemas['UserUpdateRequest'];
export function listUsers(params?: Paths['/console/v1/users']['get']['query']) {
  return requestClient.get<Schemas['UserPage']>('/users', { params });
}
export function createUser(data: UserCreate) {
  return requestClient.post<User>('/users', data);
}
export function updateUser(username: string, data: UserUpdate) {
  return requestClient.put<User>(
    `/users/${encodeURIComponent(username)}`,
    data,
  );
}
export function disableUser(username: string) {
  return requestClient.delete<null>(`/users/${encodeURIComponent(username)}`);
}
