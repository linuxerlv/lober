import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

// Credentials arrive on stdin and are never written to screenshots or reports.
const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const { chromium } = await import(pathToFileURL(process.argv[2]).href);
const browser = await chromium.launch({ headless: true });
const results = [];
let currentPage;
async function signIn(username) {
  const context = await browser.newContext({ locale: 'zh-CN' });
  const page = await context.newPage();
  currentPage = page;
  await page.goto(input.url + '/#/auth/login');
  await page.locator('input:not([type="password"]):not([type="checkbox"])').first().fill(username);
  await page.locator('input[type="password"]').fill(input.password);
  await page.getByRole('button', { name: 'login', exact: true }).click();
  await page.waitForURL(url => !url.hash.startsWith('#/auth'), { timeout: 30000 });
  return { page, context };
}
try {
  const admin = await signIn('admin');
  await admin.page.goto(input.url + '/#/access/role-menus');
  await admin.page.getByLabel('配置角色', { exact: true }).selectOption('admin');
  await admin.page.locator('input[type="checkbox"][value="role-menus"]').waitFor();
  results.push('administrator-role-menu-page');
  await admin.page.goto(input.url + '/#/hosts');
  await admin.page.locator('.vxe-grid').first().waitFor();
  await admin.page.reload();
  await admin.page.locator('.vxe-grid').first().waitFor();
  results.push('production-spa-deep-link-refresh');
  const viewer = await signIn('enterprise_viewer');
  await viewer.page.goto(input.url + '/#/hosts');
  await viewer.page.locator('.vxe-grid').first().waitFor();
  if (await viewer.page.getByRole('button', { name: /新建|登记主机|导入.*CSV|CSV.*导入/ }).count()) {
    throw new Error('Viewer page exposes a host write button');
  }
  results.push('viewer-read-only-host-page');
  await viewer.page.goto(input.url + '/#/access/users');
  await viewer.page.waitForURL(url => url.hash.split('?')[0] !== '#/access/users', { timeout: 30000 });
  results.push('viewer-admin-route-denied');
  console.log(JSON.stringify({ status: 'PASS', count: results.length, cases: results }));
} catch (error) {
  console.error(String(error));
  console.error('Completed browser checks: ' + JSON.stringify(results));
  if (currentPage) {
    console.error('Current route: ' + new URL(currentPage.url()).hash);
    // Rendered page text excludes input values; do not log network responses,
    // storage or form values, which can contain credentials.
    console.error('Rendered text: ' + (await currentPage.locator('body').innerText()).slice(0, 2500));
    if (input.failureScreenshot) {
      await currentPage.screenshot({ path: input.failureScreenshot, fullPage: true });
    }
  }
  process.exitCode = 1;
} finally {
  await browser.close();
}
