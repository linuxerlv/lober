import type { User } from '#/api/console/accounts';

import { accountsApi } from '#/api/console/accounts';

export async function readAccountDirectory(username?: string) {
  const accounts = new Map<string, User>();
  const size = 100;
  for (let page = 1; ; page++) {
    const response = await accountsApi.list({ page, page_size: size });
    const rows = response.items ?? [];
    if (
      response.total == null ||
      (rows.length === 0 && (page - 1) * size < response.total)
    ) {
      throw new Error('账号列表不完整，请重新读取');
    }
    for (const row of rows) accounts.set(row.username, row);
    if (username && accounts.has(username)) return [accounts.get(username)!];
    if (page * size >= response.total) return [...accounts.values()];
  }
}
