import { expect, it } from 'vitest';

import { createAccountsApi } from './accounts';
it('requests a numbered account page without cursor or limit', async () => {
  const calls: string[] = [];
  const client = {
    get: async <T>(path: string) => {
      calls.push(path);
      return { items: [], total: 67, next: '' } as T;
    },
    post: async <T>() => null as T,
    put: async <T>() => null as T,
  };
  const api = createAccountsApi(client);
  expect(await api.list({ page: 3, page_size: 20 })).toEqual({
    items: [],
    total: 67,
    next: '',
  });
  expect(calls).toEqual(['/users?page=3&page_size=20']);
});
it('uses own-profile endpoint and carries account revisions and fixed roles', async () => {
  const calls: unknown[] = [];
  const client = {
    get: async <T>(path: string) => {
      calls.push(['GET', path]);
      return { items: [], next: '' } as T;
    },
    put: async <T>(path: string, data: unknown) => {
      calls.push(['PUT', path, data]);
      return data as T;
    },
    post: async <T>(path: string, data: unknown) => {
      calls.push(['POST', path, data]);
      return data as T;
    },
  };
  const api = createAccountsApi(client);
  await api.profile({
    displayName: '李四',
    email: '',
    avatar: '',
    bio: '',
    rev: 3,
  });
  await api.update('zhangsan', {
    displayName: '张三',
    role: 'admin',
    disabled: false,
    rev: 7,
  });
  await api.create({
    username: 'lisi',
    displayName: '李四',
    role: 'viewer',
    password: 'temporary',
  });
  expect(calls).toEqual([
    [
      'PUT',
      '/user/profile',
      { displayName: '李四', email: '', avatar: '', bio: '', rev: 3 },
    ],
    [
      'PUT',
      '/users/zhangsan',
      { displayName: '张三', role: 'admin', disabled: false, rev: 7 },
    ],
    [
      'POST',
      '/users',
      {
        username: 'lisi',
        displayName: '李四',
        role: 'viewer',
        password: 'temporary',
      },
    ],
  ]);
});
