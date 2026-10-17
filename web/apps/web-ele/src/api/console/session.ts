import type { UserInfo } from '@vben/types';
import { sessionHome } from '#/access/policy';
export type ConsoleRole =
  | 'viewer'
  | 'operator'
  | 'hook-author'
  | 'hook-approver'
  | 'admin';
export interface ConsoleSessionUser {
  username: string;
  displayName: string;
  email?: string;
  avatar?: string;
  bio?: string;
  role: ConsoleRole;
  permissions: string[];
  menuIds: string[];
  menuRev: number;
  rev: number;
  createdAt: number;
  disabled: boolean;
  passwordChangeRequired: boolean;
}
export interface ConsoleLoginResult {
  token: string;
  expiresAt: number;
  user: ConsoleSessionUser;
}
export interface PasswordInput {
  currentPassword: string;
  newPassword: string;
}
export interface SessionHttp {
  get<T>(path: string): Promise<T>;
  post<T>(path: string, data?: unknown): Promise<T>;
}
export function createSessionApi(client: SessionHttp) {
  return {
    login: (data: { username?: string; password?: string }) =>
      client.post<ConsoleLoginResult>('/auth/login', data),
    info: () => client.get<ConsoleSessionUser>('/user/info'),
    logout: () => client.post<null>('/auth/logout'),
    password: (data: PasswordInput) =>
      client.post<null>('/auth/password', data),
  };
}
export function toFrameworkUser(user: ConsoleSessionUser): UserInfo {
  return {
    ...user,
    avatar: user.avatar || '/avatar.svg',
    userId: user.username,
    realName: user.displayName || user.username,
    roles: [user.role],
    desc: '',
    token: '',
    homePath: sessionHome(user),
  };
}
