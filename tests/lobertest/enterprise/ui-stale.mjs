import assert from 'node:assert/strict';
import { closeDialog, completed, go, input, run, signIn } from './browser.mjs';

await run(async () => {
  const session = await signIn('enterprise_hook_author');
  const { page } = session;
  let release;
  const held = new Promise(resolve => { release = resolve; });
  let arrived;
  const waiting = new Promise(resolve => { arrived = resolve; });
  const routePattern = '**/console/v1/templates/' + input.firstID + '/versions/1';
  await page.route(routePattern, async route => {
    // Fetch from the real service, delay delivery, preserve its response unchanged.
    const response = await route.fetch();
    arrived();
    await held;
    await route.fulfill({ response });
  });
  await go(page, '/templates');
  async function open(name) {
    await page.getByPlaceholder('名称前缀', { exact: true }).fill(name);
    await page.getByRole('button', { name: '查询', exact: true }).click();
    await page.locator('.vxe-body--row').filter({ hasText: name }).first()
      .getByRole('button', { name: '查看 / 编辑', exact: true }).click();
  }
  await open(input.firstName);
  await Promise.race([waiting, new Promise((_, reject) => setTimeout(() => reject(new Error('Real request was not held')), 20000))]);
  await closeDialog(page.getByRole('dialog'));
  await open(input.secondName);
  const editor = page.getByRole('dialog');
  await editor.getByLabel('名称', { exact: true }).waitFor();
  assert.equal(await editor.getByLabel('名称', { exact: true }).inputValue(), input.secondName);
  const delivered = page.waitForResponse(response => new URL(response.url()).pathname === '/console/v1/templates/' + input.firstID + '/versions/1');
  release();
  await delivered;
  // A server round trip and rendering checkpoint ensure the released callback ran.
  await page.getByRole('button', { name: '查询', exact: true }).count();
  await page.waitForTimeout(200);
  assert.equal(await editor.getByLabel('名称', { exact: true }).inputValue(), input.secondName);
  assert.match(await editor.getByLabel('模板正文', { exact: true }).inputValue(), /SECOND-RESOURCE/);
  completed.push('late-real-template-response-cannot-overwrite-new-editor');
});
