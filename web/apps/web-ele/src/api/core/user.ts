import { requestClient } from '#/api/request';
import { createSessionApi } from '#/api/console/session';
export function getUserInfoApi() {
  return createSessionApi(requestClient).info();
}
