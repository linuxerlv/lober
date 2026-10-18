import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import net from 'node:net';
import { completed, go, input, run, signIn } from './browser.mjs';

await run(async () => {
  const destination = new URL(input.url);
  const sockets = new Set();
  let disconnected = false;
  const proxy = net.createServer(client => {
    if (disconnected) { client.destroy(); return; }
    const upstream = net.connect(Number(destination.port), destination.hostname);
    for (const socket of [client, upstream]) {
      sockets.add(socket);
      socket.on('close', () => sockets.delete(socket));
      socket.on('error', () => { client.destroy(); upstream.destroy(); });
    }
    client.pipe(upstream).pipe(client);
  });
  await new Promise(resolve => proxy.listen(0, '127.0.0.1', resolve));
  input.url = 'http://127.0.0.1:' + proxy.address().port;
  try {
  const viewer = await signIn('enterprise_viewer');
  const { page, context } = viewer;
  const append = async (start, count) => {
    const result = await promisify(execFile)(input.producer,
      [input.config, input.taskID, String(start), String(count), 'browser'], { timeout: 40000 });
    return JSON.parse(result.stdout);
  };
  await append(1, 1);
  await go(page, '/tasks/' + input.taskID);
  await page.getByText('实时连接已建立', { exact: true }).waitFor();
  const rows = page.locator('.vxe-table--body .vxe-body--row').filter({ hasText: 'test.transport' });
  await rows.nth(0).waitFor();
  assert.equal(await rows.count(), 1);
  await context.setOffline(true);
  // Browser offline emulation alone does not close already-open SSE sockets.
  disconnected = true;
  for (const socket of sockets) socket.destroy();
  await page.getByText('正在重连，定时补读事件', { exact: true }).waitFor({ timeout: 15000 });
  await append(2, 3);
  assert.ok((await append(2, 3)).every(item => item.Duplicate));
  assert.equal(await rows.count(), 1, 'Disconnected browser received events');
  disconnected = false;
  await context.setOffline(false);
  await page.getByText('实时连接已建立', { exact: true }).waitFor({ timeout: 20000 });
  await rows.nth(3).waitFor();
  assert.equal(await rows.count(), 4);
  await page.getByRole('button', { name: '补读后续事件', exact: true }).click();
  await page.getByRole('button', { name: '刷新', exact: true }).click();
  assert.equal(await rows.count(), 4);
  const seqs = await rows.locator('td:first-child .vxe-cell').allTextContents();
  assert.deepEqual(seqs.map(value => Number(value.trim())), [1, 2, 3, 4]);
  completed.push('real-browser-offline-reconnect-replay-and-deduplication');
  } finally {
    for (const socket of sockets) socket.destroy();
    await new Promise(resolve => proxy.close(resolve));
  }
});
