import assert from 'node:assert/strict';

import { chromium } from 'playwright';

const [origin, batchID, failedID] = process.argv.slice(2);
const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  const batchURL = `${origin}/#/tasks/batches/${batchID}`;
  const batchResponse = (response) =>
    new URL(response.url()).pathname === `/console/v1/task-batches/${batchID}` &&
    response.ok();
  await page.goto(origin);
  await page.locator('[name=username]').fill('admin');
  await page.locator('[name=password]').fill('admin password');
  const login = page.waitForResponse((response) =>
    new URL(response.url()).pathname === '/console/v1/auth/login' && response.ok());
  await page.getByRole('button', { name: 'login', exact: true }).click();
  await login;
  await page.waitForURL((url) => !url.hash.includes('login'));
  const initial = page.waitForResponse(batchResponse);
  await page.goto(batchURL);
  await initial;

  const count = (label) => page.locator('dl > div').filter({
    has: page.getByText(label, { exact: true }),
  }).locator('dd');
  await count('任务总数').waitFor();
  assert.equal(await count('任务总数').innerText(), '2');
  assert.equal(await count('失败').innerText(), '1');

  const refreshed = page.waitForResponse(batchResponse);
  await page.reload();
  await refreshed;
  await count('任务总数').waitFor();
  assert.equal(await count('任务总数').innerText(), '2');

  const filtered = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === '/console/v1/tasks' &&
      url.searchParams.get('batch_id') === batchID &&
      url.searchParams.get('state') === 'failed' && response.ok();
  });
  await page.getByRole('button', { name: '失败任务', exact: true }).click();
  await filtered;
  await page.getByRole('cell', { name: failedID, exact: true }).waitFor();
  assert.equal(await page.getByRole('link', { name: '查看任务', exact: true }).count(), 0);
  assert.equal(await count('任务总数').innerText(), '2');
  assert.equal(await count('失败').innerText(), '1');
  console.log('PASS batch link, browser refresh, failed filter and full-batch counts');
} finally {
  await browser.close();
}
