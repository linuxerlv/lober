import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { EventEmitter } from 'node:events';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

// 从实际使用者加载，避免另一份未被项目使用的包让测试误过。
const rootRequire = createRequire(new URL('../package.json', import.meta.url));
const changesetsRequire = createRequire(rootRequire.resolve('@changesets/cli'));
const editorPath = changesetsRequire.resolve('launch-editor');
const vitestRequire = createRequire(rootRequire.resolve('vitest/package.json'));
const { interceptorPlugin } = await import(pathToFileURL(vitestRequire.resolve('@vitest/mocker/node')).href);
const { MockerRegistry } = await import(pathToFileURL(vitestRequire.resolve('@vitest/mocker')).href);
const { createServer: createViteServer } = await import(pathToFileURL(rootRequire.resolve('vite')).href);
const { Window } = await import('happy-dom');

test('编辑器把像命令参数的文件名当作普通文件路径', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lober-editor-'));
  try {
    await writeFile(join(dir, '+fixture.js'), 'fixture');
    // 只替换启动外部进程的边界，文件检查和参数构造仍用真实依赖。
    const code = `
      const cp = require('node:child_process');
      cp.spawn = (editor, args) => {
        console.log(JSON.stringify(args));
        return new (require('node:events').EventEmitter)();
      };
      require(${JSON.stringify(editorPath)})('+fixture.js:3:2', 'vim');
    `;
    const result = spawnSync(process.execPath, ['-e', code], { cwd: dir, encoding: 'utf8', timeout: 5000 });
    assert.equal(result.status, 0, result.stderr);
    assert.deepEqual(JSON.parse(result.stdout), ['+call cursor(3, 2)', join(dir, '+fixture.js')]);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('模拟Windows启动分支拒绝百分号和换行文件名', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lober-editor-win-'));
  try {
    for (const name of ['fixture%NAME%.js', 'fixture\nnext.js']) {
      await writeFile(join(dir, name), 'fixture');
      const code = `
        Object.defineProperty(process, 'platform', { value: 'win32' });
        const cp = require('node:child_process');
        let launched = false, rejected = false;
        cp.exec = () => { launched = true; return new (require('node:events').EventEmitter)(); };
        require(${JSON.stringify(editorPath)})(${JSON.stringify(name)}, 'code', () => { rejected = true; });
        console.log(JSON.stringify({ launched, rejected }));
      `;
      const result = spawnSync(process.execPath, ['-e', code], { cwd: dir, encoding: 'utf8', timeout: 5000 });
      assert.equal(result.status, 0, result.stderr);
      const lastLine = result.stdout.trim().split('\n').at(-1);
      assert.deepEqual(JSON.parse(lastLine), { launched: false, rejected: true });
    }
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('模块模拟的特殊网址不会变成可执行语句', async () => {
  const registry = new MockerRegistry();
  const plugin = interceptorPlugin({ registry });
  const url = 'x"); globalThis.__loberMockInjection = true; void("';
  registry.register('manual', 'fixture', '/fixture.js', url, async () => ({ default: 'fixture' }));
  const source = await plugin.load.handler('/fixture.js');
  const previous = globalThis.__vitest_mocker__;
  try {
    globalThis.__vitest_mocker__ = { getFactoryModule: async () => ({ default: 'fixture' }) };
    const module = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
    assert.equal(globalThis.__loberMockInjection, undefined);
    assert.equal(module.default, 'fixture');
  } finally {
    globalThis.__vitest_mocker__ = previous;
    delete globalThis.__loberMockInjection;
  }
});

test('模块模拟拒绝反斜杠越界路径并保留正常重定向', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lober-mocker-'));
  const registry = new MockerRegistry();
  let server;
  try {
    await writeFile(join(dir, 'allowed.js'), 'export default "allowed-fixture";');
    server = await createViteServer({ root: dir, configFile: false, logLevel: 'silent',
      server: { watch: null, fs: { strict: true, allow: [dir] } },
      optimizeDeps: { noDiscovery: true },
    });
    // 只替换消息传输，保留插件与 Vite 的真实路径许可检查。
    const ws = new EventEmitter();
    ws.send = () => {};
    const plugin = interceptorPlugin({ registry });
    plugin.configureServer({ config: server.config, ws });
    ws.emit('vitest:interceptor:register', {
      type: 'redirect', raw: 'outside', id: '/outside.js', url: '/outside.js', redirect: 'mock:/..\\secret.js',
    });
    assert.equal(registry.getById('/outside.js'), undefined);
    ws.emit('vitest:interceptor:register', {
      type: 'redirect', raw: 'allowed', id: '/allowed.js', url: '/allowed.js', redirect: 'mock:/allowed.js',
    });
    assert.equal(await plugin.load.handler('/allowed.js'), 'export default "allowed-fixture";');
  } finally {
    await server?.close();
    await rm(dir, { recursive: true, force: true });
  }
});

test('HTML解析默认不加载框架和样式表中的网络资源', async () => {
  const hits = [];
  const server = createServer((req, res) => { hits.push(req.url); res.end('fixture'); });
  const windows = [];
  try {
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const base = `http://127.0.0.1:${server.address().port}`;
    const html = `<iframe src="${base}/frame"></iframe><link rel="stylesheet" href="${base}/style">`;
    const window = new Window({ url: base });
    windows.push(window);
    window.document.body.innerHTML = html;
    await window.happyDOM.waitUntilComplete();
    assert.deepEqual(hits, []);
    // 显式允许时仍能加载，证明本机服务和观测方法有效。
    const allowed = new Window({ url: base, settings: { enableIframePageLoading: true, enableCSSFileLoading: true } });
    windows.push(allowed);
    allowed.document.body.innerHTML = html;
    await allowed.happyDOM.waitUntilComplete();
    assert.deepEqual(hits.sort(), ['/frame', '/style']);
  } finally {
    await Promise.all(windows.map((window) => window.happyDOM.close()));
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});

test('Cookie域名必须匹配完整域名边界', async () => {
  const attacker = new Window({ url: 'https://notexample.test/' });
  const valid = new Window({ url: 'https://sub.example.test/' });
  try {
    attacker.document.cookie = 'session=forged; Domain=example.test; Path=/';
    assert.equal(attacker.document.cookie, '');
    valid.document.cookie = 'session=fixture; Domain=example.test; Path=/';
    assert.equal(valid.document.cookie, 'session=fixture');
  } finally {
    await Promise.all([attacker.happyDOM.close(), valid.happyDOM.close()]);
  }
});

test('非法模块导入别名不会执行表达式', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lober-dom-module-'));
  const window = new Window({ url: 'https://fixture.test/', settings: {
    enableJavaScriptEvaluation: true, suppressCodeGenerationFromStringsWarning: true,
    fetch: { virtualServers: [{ url: 'https://fixture.test/', directory: dir }] },
  } });
  try {
    await writeFile(join(dir, 'dep.json'), '{"value":7}');
    const invalid = window.document.createElement('script');
    invalid.type = 'module';
    invalid.textContent = 'import { missing as injected = globalThis.__loberDomInjection = 1 } from "./dep.json" with { type: "json" };';
    window.document.body.appendChild(invalid);
    await window.happyDOM.waitUntilComplete();
    assert.equal(window.__loberDomInjection, undefined);
    const valid = window.document.createElement('script');
    valid.type = 'module';
    valid.textContent = 'import data from "./dep.json" with { type: "json" }; globalThis.fixtureValue = data.value;';
    window.document.body.appendChild(valid);
    await window.happyDOM.waitUntilComplete();
    assert.equal(window.fixtureValue, 7);
  } finally {
    await window.happyDOM.close();
    await rm(dir, { recursive: true, force: true });
  }
});
