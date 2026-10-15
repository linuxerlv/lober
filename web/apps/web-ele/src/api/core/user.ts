import type { UserInfo } from '@vben/types';

import type { Schemas } from '#/api/console/generated';

import { requestClient } from '#/api/request';
export function getUserInfoApi() {
  return requestClient.get<Schemas['UserResponse']>('/user/info');
}
export function toUserInfo(user: Schemas['UserResponse']): UserInfo {
  return {
    ...user,
    avatar: '/avatar.svg',
    desc: '',
    token: '',
    userId: user.username,
    realName: user.displayName || user.username,
    roles: [user.role],
    homePath: user.passwordChangeRequired ? '/account/password' : '/tasks',
  };
}
