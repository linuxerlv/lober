import { createSessionApi } from '#/api/console/session';
import { type ConsoleSessionUser, toFrameworkUser } from '#/api/console/session';
import { requestClient } from '#/api/request';
export function toUserInfo(user:Omit<ConsoleSessionUser,'menuIds'|'menuRev'> & Partial<Pick<ConsoleSessionUser,'menuIds'|'menuRev'>>) {
 return toFrameworkUser({...user,menuIds:user.menuIds||[],menuRev:user.menuRev||0});
}
export function getUserInfoApi() {
  return createSessionApi(requestClient).info();
}
