import assert from 'node:assert/strict';
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

export const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const { chromium } = await import(pathToFileURL(process.argv[2]).href);
export const browser = await chromium.launch({ headless: true });
export const completed = [];
const trace = [];
let currentPage;

export function watch(page) {
  currentPage = page;
  page.setDefaultTimeout(20000);
  page.on('response', response => {
    trace.push({ method: response.request().method(), path: new URL(response.url()).pathname,
      status: response.status() });
    if (trace.length > 500) trace.shift();
  });
  return page;
}

export async function signIn(username, existingPage) {
  const context = existingPage?.context() ?? await browser.newContext({ locale: 'zh-CN' });
  const page = watch(existingPage ?? await context.newPage());
  await page.goto(input.url + '/#/auth/login');
  await page.locator('input:not([type="password"]):not([type="checkbox"])').first().fill(username);
  await page.locator('input[type="password"]').fill(input.password);
  const loggedIn = page.waitForResponse(response => new URL(response.url()).pathname === '/console/v1/auth/login' && response.request().method() === 'POST');
  await page.getByRole('button', { name: 'login', exact: true }).click();
  const result = await (await loggedIn).json();
  assert.equal(result.code, 0, 'UI login failed');
  await page.waitForURL(url => !url.hash.startsWith('#/auth'));
  return { page, context, token: result.data.token };
}

export async function api(session, method, path, body, expected = 200) {
  const response = await session.context.request.fetch(input.url + '/console/v1' + path,
    { method, headers: { Authorization: 'Bearer ' + session.token }, data: body });
  assert.equal(response.status(), expected, method + ' ' + path + ' returned unexpected HTTP status');
  const envelope = await response.json();
  assert.equal(envelope.code, expected === 200 ? 0 : expected, 'Unexpected response envelope');
  return envelope.data;
}

export async function go(page, route) {
  watch(page);
  // A distinct query causes a real navigation, avoiding hidden cached route views.
  await page.goto(input.url + '/?enterprise-route=' + encodeURIComponent(route) + '#' + route);
}

export async function choose(page, locator, text) {
  await locator.locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")][1]').click();
  await page.getByRole('option', { name: text, exact: true }).last().click();
}

export async function closeDialog(dialog) {
  await dialog.locator('.el-dialog__headerbtn').click();
  await dialog.waitFor({ state: 'hidden' });
}

export async function run(action) {
  try {
    await action();
    console.log(JSON.stringify({ status: 'PASS', count: completed.length, cases: completed }));
  } catch (error) {
    console.error(String(error).replace(/access_token=[^&\s"']+/g, 'access_token=[redacted]'));
    console.error('Completed checks: ' + JSON.stringify(completed));
    if (currentPage && !currentPage.isClosed()) {
      console.error('Current route: ' + new URL(currentPage.url()).hash);
      console.error('Rendered text: ' + (await currentPage.locator('body').innerText()).slice(0, 2500));
      if (input.failureScreenshot) await currentPage.screenshot({ path: input.failureScreenshot, fullPage: true });
    }
    if (input.tracePath) fs.writeFileSync(input.tracePath, JSON.stringify(trace, null, 2));
    process.exitCode = 1;
  } finally {
    await browser.close();
  }
}
