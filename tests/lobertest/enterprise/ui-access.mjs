import assert from 'node:assert/strict';
import { api, completed, go, input, run, signIn } from './browser.mjs';

await run(async () => {
  const admin = await signIn('admin');
  const username = 'ui_access_' + input.suffix;
  const user = await api(admin, 'POST', '/users', { username, role: 'operator', password: input.password });
  const operator = await signIn(username);
  const { page } = operator;
  await go(page, '/hosts');
  await page.getByRole('button', { name: '登记主机', exact: true }).waitFor();
  await api(admin, 'PUT', '/users/' + username, { role: 'viewer', displayName: 'demoted', rev: user.rev });
  // The application refreshes the real session on its normal 30-second interval.
  await page.getByRole('button', { name: '登记主机', exact: true }).waitFor({ state: 'hidden', timeout: 45000 });
  await api(operator, 'POST', '/hosts', { sn: 'FORBIDDEN-' + input.suffix, attrs: {} }, 403);
  completed.push('open-page-role-revocation-removes-write-controls-and-denies-api');

  const roles = await api(admin, 'GET', '/roles/menus');
  const viewer = roles.roles.find(role => role.role === 'viewer');
  let empty = await api(admin, 'PUT', '/roles/viewer/menus', { menuIds: [], rev: viewer.rev });
  try {
    await page.getByText('暂无可访问菜单，请联系管理员配置角色菜单', { exact: true }).waitFor({ timeout: 45000 });
    await api(operator, 'POST', '/hosts', { sn: 'DENIED-' + input.suffix, attrs: {} }, 403);
    const restored = await api(admin, 'PUT', '/roles/viewer/menus', { menuIds: viewer.menuIds, rev: empty.rev });
    await page.getByRole('button', { name: '重新读取权限', exact: true }).click();
    await page.getByText('暂无可访问菜单，请联系管理员配置角色菜单', { exact: true }).waitFor({ state: 'hidden' });
    await go(page, '/hosts');
    assert.equal(await page.getByRole('button', { name: '登记主机', exact: true }).count(), 0);
    await api(operator, 'POST', '/hosts', { sn: 'STILL-DENIED-' + input.suffix, attrs: {} }, 403);
    completed.push('empty-menu-restore-refresh-recovers-navigation-without-write-escalation');
    empty = await api(admin, 'PUT', '/roles/viewer/menus', { menuIds: [], rev: restored.rev });
    await page.reload();
    await page.getByText('暂无可访问菜单，请联系管理员配置角色菜单', { exact: true }).waitFor();
    // Log out through the real empty-menu UI, then use the same tab for another identity.
    await page.getByRole('button', { name: '退出登录', exact: true }).click();
    await page.waitForURL(url => url.hash.startsWith('#/auth/login'));
    await api(operator, 'GET', '/user/info', undefined, 401);
    const next = await signIn('admin', page);
    await go(next.page, '/hosts');
    await next.page.getByRole('button', { name: '登记主机', exact: true }).waitFor();
    assert.equal((await api(next, 'GET', '/user/info')).username, 'admin');
    completed.push('empty-menu-logout-and-same-tab-account-switch');
  } finally {
    await api(admin, 'PUT', '/roles/viewer/menus', { menuIds: viewer.menuIds, rev: empty.rev });
  }
});
