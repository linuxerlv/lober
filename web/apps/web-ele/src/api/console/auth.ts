// Authentication shares the current session contract and authenticated client.
export {
  loginApi as consoleLoginApi,
  logoutApi as consoleLogoutApi,
} from '../core/auth';
export { getUserInfoApi as getConsoleUserInfoApi } from '../core/user';
export type {
  ConsoleLoginResult as AuthLoginResult,
  ConsoleSessionUser as ConsoleUserInfo,
} from './session';
