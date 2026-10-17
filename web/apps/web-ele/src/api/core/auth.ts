import type { PasswordInput } from '#/api/console/session';

import { createSessionApi } from '#/api/console/session';
import { requestClient } from '#/api/request';
export namespace AuthApi {
  export interface LoginParams {
    password?: string;
    username?: string;
  }
}
export function loginApi(data: AuthApi.LoginParams) {
  return createSessionApi(requestClient).login(data);
}
export function logoutApi() {
  return createSessionApi(requestClient).logout();
}
export function changePasswordApi(data: PasswordInput) {
  return createSessionApi(requestClient).password(data);
}
