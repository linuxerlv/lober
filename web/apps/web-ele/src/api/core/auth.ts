import type { Schemas } from '#/api/console/generated';

import { requestClient } from '#/api/request';
export namespace AuthApi {
  export type LoginParams = Schemas['LoginRequest'];
  export type LoginResult = Schemas['SessionResponse'];
}
export function loginApi(data: AuthApi.LoginParams) {
  return requestClient.post<AuthApi.LoginResult>('/auth/login', data);
}
export function logoutApi() {
  return requestClient.post<null>('/auth/logout');
}
