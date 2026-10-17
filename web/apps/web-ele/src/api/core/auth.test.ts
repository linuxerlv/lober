import { beforeEach, describe, expect, it, vi } from 'vitest';
const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}));
vi.mock('#/api/request', () => ({ requestClient: client }));
import { exportAudit } from '../console/audit';
import { updateUser } from '../console/user';
import { loginApi, logoutApi } from './auth';
import { getUserInfoApi, toUserInfo } from './user';

describe('real authentication and administration contracts', () => {
  beforeEach(() => vi.clearAllMocks());
  it('returns the server session token and requests current-user permissions', async () => {
    const session = {
      token: 'bearer',
      expiresAt: 10,
      user: { role: 'viewer' },
    };
    client.post.mockResolvedValueOnce(session);
    expect(await loginApi({ username: 'admin', password: 'secret' })).toBe(
      session,
    );
    await getUserInfoApi();
    await logoutApi();
    expect(client.post.mock.calls.map((call) => call[0])).toEqual([
      '/auth/login',
      '/auth/logout',
    ]);
    expect(client.get).toHaveBeenCalledWith('/user/info');
  });
  it('maps real role/displayName and forces initial password route', () => {
    const info = toUserInfo({
      username: 'admin',
      displayName: 'Operator',
      role: 'admin',
      permissions: ['user:manage'],
      disabled: false,
      passwordChangeRequired: true,
      createdAt: 1,
      rev: 1,
    });
    expect(info).toMatchObject({
      realName: 'Operator',
      roles: ['admin'],
      permissions: ['user:manage'],
      homePath: '/account/password',
    });
  });
  it('encodes user paths and exports through the complete server CSV endpoint', async () => {
    await updateUser('a/b #', {
      role: 'viewer',
      displayName: '',
      disabled: false,
      rev: 2,
    });
    expect(client.put).toHaveBeenCalledWith(
      '/users/a%2Fb%20%23',
      expect.objectContaining({ rev: 2 }),
    );
    await exportAudit({ actor: 'a' });
    expect(client.get).toHaveBeenCalledWith('/audit/export', {
      params: { actor: 'a' },
      responseType: 'blob',
      responseReturn: 'body',
    });
  });
});
