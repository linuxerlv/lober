import { describe, expect, it } from 'vitest';
import { toFrameworkUser, createSessionApi } from './session';

export const viewer = {
  username: 'alice', displayName: 'Alice', role: 'viewer' as const,
  permissions: ['host:read', 'template:read'], disabled: false, createdAt: 1,
  rev: 1, passwordChangeRequired: false, menuIds: ['hosts', 'templates'], menuRev: 0,
};

describe('console session contract', () => {
  it('maps a single backend role and picks a reachable home', () => {
    expect(toFrameworkUser(viewer)).toMatchObject({
      userId: 'alice', username: 'alice', realName: 'Alice', roles: ['viewer'], homePath: '/hosts',
    });
    expect(toFrameworkUser({ ...viewer, menuIds: [] }).homePath).toBe('/no-access');
    expect(toFrameworkUser({ ...viewer, passwordChangeRequired: true }).homePath).toBe('/account/password');
  });
  it('uses the current endpoints and payloads through one authenticated client', async () => {
    const requests: Array<{method: string; path: string; data?: unknown}> = [];
    const api = createSessionApi({
      async get<T>(path: string) { requests.push({method: 'GET', path}); return viewer as T; },
      async post<T>(path: string, data?: unknown) { requests.push({method: 'POST', path, data}); return (path.endsWith('/login') ? { token: 'token', expiresAt: 10, user: viewer } : null) as T; },
    });
    expect(await api.login({ username: 'alice', password: 'old' })).toMatchObject({ token: 'token', user: viewer });
    await api.info();
    await api.password({currentPassword: 'old', newPassword: 'new'});
    await api.logout();
    expect(requests).toEqual([
      {method: 'POST', path: '/auth/login', data: {username: 'alice', password: 'old'}},
      {method: 'GET', path: '/user/info'},
      {method: 'POST', path: '/auth/password', data: {currentPassword: 'old', newPassword: 'new'}},
      {method: 'POST', path: '/auth/logout', data: undefined},
    ]);
  });
});
