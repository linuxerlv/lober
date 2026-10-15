import { createPinia, setActivePinia } from 'pinia';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const state = vi.hoisted(() => {
  const access = {
    accessToken: null as null | string,
    loginExpired: false,
    setAccessToken: vi.fn(),
    setAccessCodes: vi.fn(),
    setLoginExpired: vi.fn(),
  };
  access.setAccessToken.mockImplementation((token) => {
    access.accessToken = token;
  });
  const user = { setUserInfo: vi.fn() };
  const router = {
    push: vi.fn(),
    replace: vi.fn(),
    currentRoute: { value: { fullPath: '/tasks' } },
  };
  return {
    access,
    user,
    router,
    login: vi.fn(),
    info: vi.fn(),
    logout: vi.fn(),
    reset: vi.fn(),
  };
});
vi.mock('@vben/stores', () => ({
  useAccessStore: () => state.access,
  useUserStore: () => state.user,
  resetAllStores: state.reset,
}));
vi.mock('vue-router', () => ({ useRouter: () => state.router }));
vi.mock('@vben/constants', () => ({ LOGIN_PATH: '/auth/login' }));
vi.mock('@vben/preferences', () => ({
  preferences: { app: { defaultHomePath: '/tasks' } },
}));
vi.mock('element-plus', () => ({ ElNotification: vi.fn() }));
vi.mock('#/locales', () => ({ $t: (key: string) => key }));
vi.mock('#/api/request', () => ({ requestClient: {} }));
vi.mock('#/api', async () => ({
  loginApi: state.login,
  getUserInfoApi: state.info,
  logoutApi: state.logout,
  toUserInfo: (await import('#/api/core/user')).toUserInfo,
}));
import { useAuthStore } from './auth';

const user = {
  username: 'admin',
  displayName: '管理员',
  role: 'admin',
  permissions: ['user:manage'],
  disabled: false,
  passwordChangeRequired: false,
  createdAt: 1,
  rev: 1,
};
describe('console authentication flow', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    state.access.accessToken = null;
    state.login.mockResolvedValue({
      token: 'server-session',
      expiresAt: 10,
      user,
    });
    state.info.mockResolvedValue(user);
  });
  it('stores the real token and current-user permissions', async () => {
    await useAuthStore().authLogin({ username: 'admin', password: 'secret' });
    expect(state.access.setAccessToken).toHaveBeenCalledWith('server-session');
    expect(state.access.setAccessCodes).toHaveBeenCalledWith(['user:manage']);
    expect(state.user.setUserInfo).toHaveBeenCalledWith(
      expect.objectContaining({ realName: '管理员', roles: ['admin'] }),
    );
    expect(state.router.push).toHaveBeenCalledWith('/tasks');
  });
  it('forces password change before executing a requested redirect', async () => {
    state.info.mockResolvedValue({ ...user, passwordChangeRequired: true });
    const onSuccess = vi.fn();
    await useAuthStore().authLogin(
      { username: 'admin', password: 'secret' },
      onSuccess,
    );
    expect(state.router.push).toHaveBeenCalledWith('/account/password');
    expect(onSuccess).not.toHaveBeenCalled();
  });
  it('sends logout before clearing local authentication', async () => {
    state.access.accessToken = 'server-session';
    await useAuthStore().logout(false);
    expect(state.logout).toHaveBeenCalledOnce();
    expect(state.reset).toHaveBeenCalledOnce();
    expect(state.router.replace).toHaveBeenCalledWith({
      path: '/auth/login',
      query: {},
    });
  });
});
