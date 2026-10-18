import assert from 'node:assert/strict';
import { api, choose, closeDialog, completed, go, input, run, signIn } from './browser.mjs';

await run(async () => {
  const admin = await signIn('admin');
  const { page } = admin;
  const first = 'B2-' + input.suffix.toUpperCase() + '-ONE';
  const second = 'B2-' + input.suffix.toUpperCase() + '-TWO';
  await go(page, '/hosts');
  await page.getByRole('button', { name: '登记主机', exact: true }).click();
  const hostForm = page.getByRole('dialog', { name: '登记主机', exact: true });
  await hostForm.getByLabel('主机 SN', { exact: true }).fill(first);
  await choose(page, hostForm.getByLabel('主机架构', { exact: true }), 'x86_64');
  await choose(page, hostForm.getByLabel('主机固件', { exact: true }), 'UEFI');
  await hostForm.getByRole('button', { name: '保存主机', exact: true }).click();
  await hostForm.waitFor({ state: 'hidden' });
  assert.equal((await api(admin, 'GET', '/hosts/' + first)).firmware, 'uefi');
  await page.getByRole('button', { name: 'CSV 导入', exact: true }).click();
  const csv = page.getByRole('dialog', { name: 'CSV 导入：预览后确认', exact: true });
  await csv.getByLabel('CSV 文本', { exact: true }).fill(`sn,arch,firmware,rack\n${second},x86_64,uefi,B2\n`);
  await csv.getByRole('button', { name: '预览 CSV（不写入）', exact: true }).click();
  await csv.getByText(second, { exact: true }).waitFor();
  assert.equal((await api(admin, 'GET', '/hosts?prefix=' + second)).items.length, 0, 'CSV preview wrote a host');
  await csv.locator('label.el-checkbox').click();
  assert.equal(await csv.getByRole('checkbox').isChecked(), true);
  await csv.getByRole('button', { name: '确认导入', exact: true }).click();
  await csv.waitFor({ state: 'hidden' });
  assert.equal((await api(admin, 'GET', '/hosts/' + second)).attrs.rack, 'B2');
  completed.push('host-registration-and-csv-preview-confirmation');

  const isoName = 'B2-ISO-' + input.suffix;
  await go(page, '/artifact-sets');
  await page.getByRole('button', { name: '新建 ISO', exact: true }).click();
  const isoForm = page.getByRole('dialog', { name: '新建 ISO', exact: true });
  await isoForm.getByLabel('ISO 名字', { exact: true }).fill(isoName);
  await choose(page, isoForm.getByLabel('主 ISO来源方式', { exact: true }), '浏览器上传（支持续传）');
  await isoForm.getByLabel('主 ISO上传文件', { exact: true }).setInputFiles(input.fixture);
  await isoForm.getByRole('button', { name: '登记并获取', exact: true }).click();
  await isoForm.waitFor({ state: 'hidden' });
  // Successful creation opens its progress dialog automatically.
  const isoDetails = page.getByRole('dialog', { name: 'ISO 详情与来源进度', exact: true });
  await isoDetails.getByText('状态：可用', { exact: true }).waitFor({ timeout: 30000 });
  const iso = (await api(admin, 'GET', '/isos?limit=100')).items.find(item => item.name === isoName);
  assert.ok(iso && iso.state === 'ready' && iso.sha256, 'UI upload did not produce a ready ISO');
  await closeDialog(isoDetails);
  completed.push('browser-file-upload-and-server-import-progress');

  const author = await signIn('enterprise_hook_author');
  const derivedName = 'B2-template-' + input.suffix;
  await go(author.page, '/templates');
  await author.page.getByPlaceholder('名称前缀', { exact: true }).fill(input.templateName);
  await author.page.getByRole('button', { name: '查询', exact: true }).click();
  await author.page.locator('.vxe-body--row').filter({ hasText: input.templateName }).first()
    .getByRole('button', { name: '查看 / 编辑', exact: true }).click();
  const editor = author.page.getByRole('dialog').filter({ has: author.page.getByPlaceholder('派生模板名称（独立副本）') }).first();
  await editor.getByPlaceholder('派生模板名称（独立副本）').fill(derivedName);
  await editor.getByRole('button', { name: '派生模板', exact: true }).click();
  await editor.getByLabel('模板正文', { exact: true }).fill('# {{param:hostname}}\n{{slot:enterprise}}\n{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n');
  await editor.getByRole('button', { name: '保存并刷新插槽', exact: true }).click();
  const slot = editor.locator('section').filter({ hasText: 'enterprise' }).first();
  await slot.getByRole('button', { name: '添加 hook', exact: true }).click();
  async function pickHook(name) {
    const picker = author.page.getByRole('dialog', { name: /^选择 hook/ });
    await picker.getByPlaceholder('hook 名称前缀').fill(name);
    await picker.getByRole('button', { name: '查询', exact: true }).click();
    await picker.locator('article').filter({ hasText: name }).first()
      .getByRole('button', { name: '选用', exact: true }).click();
    await picker.waitFor({ state: 'hidden' });
  }
  await pickHook(input.hookA);
  await slot.getByRole('button', { name: '替换 hook（保留同名绑定）', exact: true }).click();
  await pickHook(input.hookB);
  await editor.getByRole('button', { name: '保存草稿', exact: true }).click();
  const submitted = author.page.waitForResponse(response => response.request().method() === 'POST' && /\/templates\/[^/]+\/versions\/1\/submit/.test(response.url()));
  await editor.getByRole('button', { name: '提交审批', exact: true }).click();
  assert.equal((await submitted).status(), 200, 'UI template submission failed');
  await editor.getByText('审批人不能是作者，请由其他审批人审批。', { exact: true }).waitFor();
  assert.equal(await editor.getByRole('button', { name: '批准', exact: true }).count(), 0, 'Author can approve own template');
  const approver = await signIn('enterprise_hook_approver');
  await go(approver.page, '/templates');
  await approver.page.getByPlaceholder('名称前缀', { exact: true }).fill(derivedName);
  await approver.page.getByRole('button', { name: '查询', exact: true }).click();
  await approver.page.locator('.vxe-body--row').filter({ hasText: derivedName }).first()
    .getByRole('button', { name: '查看 / 编辑', exact: true }).click();
  const approved = approver.page.waitForResponse(response => response.request().method() === 'POST' && /\/templates\/[^/]+\/versions\/1\/approve/.test(response.url()));
  await approver.page.getByRole('dialog').getByRole('button', { name: '批准', exact: true }).click();
  assert.equal((await approved).status(), 200, 'Independent UI approval failed');
  completed.push('derive-template-replace-hook-and-independent-approval');

  await go(page, '/tasks/new');
  for (const sn of [first, second]) {
    await page.locator('.vxe-body--row').filter({ hasText: sn }).first().locator('.vxe-cell--checkbox').click();
  }
  await page.getByText('已选 2 台', { exact: true }).waitFor();
  await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.locator('label.el-radio').filter({ hasText: isoName }).click();
  assert.equal(await page.getByRole('radio', { name: new RegExp(isoName) }).isChecked(), true);
  await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.locator('label.el-radio').filter({ hasText: derivedName }).click();
  assert.equal(await page.getByRole('radio', { name: new RegExp(derivedName) }).isChecked(), true);
  await page.getByRole('button', { name: '下一步', exact: true }).click();
  const hostnames = page.locator('input[id$=":tpl/hostname"]');
  await hostnames.nth(0).fill('b2-one');
  await hostnames.nth(1).fill('b2-two');
  await page.getByRole('button', { name: '校验并确认', exact: true }).click();
  for (const sn of [first, second]) {
    const confirmation = page.locator('strong').filter({ hasText: sn }).locator('..');
    await confirmation.getByRole('button', { name: '预览该主机全部文件', exact: true }).click();
    const preview = page.getByRole('dialog', { name: '机器下发文件预览', exact: true });
    await preview.getByRole('tab', { name: 'ks.cfg', exact: true }).click();
    const body = await preview.locator('.el-tab-pane:visible pre').innerText();
    assert.match(body, /UI-HOOK-B/);
    assert.doesNotMatch(body, /UI-HOOK-A/);
    assert.match(body, sn === first ? /b2-one/ : /b2-two/);
    await closeDialog(preview);
  }
  const batchResponse = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/console/v1/wizard/tasks');
  await page.getByRole('button', { name: '创建整批任务', exact: true }).click();
  assert.equal((await batchResponse).status(), 200, 'UI batch creation failed');
  await page.getByText(/已原子创建\s*2\s*个任务/).waitFor();
  for (const sn of [first, second]) {
    const tasks = (await api(admin, 'GET', '/tasks?host_sn=' + sn)).items;
    assert.equal(tasks.length, 1);
    assert.equal(tasks[0].state, 'pending');
  }
  completed.push('per-host-render-preview-and-atomic-two-host-task-batch');
});
