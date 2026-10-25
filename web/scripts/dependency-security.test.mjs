import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { runInNewContext } from 'node:vm';

// Resolve through the real consumers: a root-only test dependency could hide an
// unpatched transitive copy used by the build or lint tools.
const lintRequire = createRequire(
  new URL('../internal/lint-configs/stylelint-config/package.json', import.meta.url),
);
const stylelintPath = lintRequire.resolve('stylelint');
const toolRequire = createRequire(stylelintPath);
const micromatch = toolRequire('micromatch');
const braces = createRequire(toolRequire.resolve('micromatch'))('braces');
const glob = toolRequire('fast-glob');

test('the changesets editor dependency rejects comment-following line terminators', () => {
  const rootRequire = createRequire(new URL('../package.json', import.meta.url));
  const changesetsRequire = createRequire(rootRequire.resolve('@changesets/cli'));
  const editorRequire = createRequire(changesetsRequire.resolve('launch-editor'));
  const shellQuote = editorRequire('shell-quote');
  for (const separator of ['\n', '\r', '\u2028', '\u2029']) {
    assert.throws(() => shellQuote.quote(['echo', 'ok', { comment: 'x' }, `a${separator}id;#`]), TypeError);
  }
  const tokens = ['echo', 'two words', "single'quote", '$literal'];
  assert.deepEqual(shellQuote.parse(shellQuote.quote(tokens)), tokens);
});

const depthError = (error) =>
  error instanceof SyntaxError && /max depth/i.test(error.message);

for (const [name, pattern] of [
  ['brace nesting', '{'.repeat(4000) + 'a' + '}'.repeat(4000)],
  ['parenthesis nesting', '('.repeat(4000) + 'a' + ')'.repeat(4000)],
]) {
  for (const method of ['compile', 'expand', 'stringify']) {
    test(`${method} rejects excessive ${name} with a controlled depth error`, () => {
      assert.throws(() => braces[method](pattern), depthError);
    });
  }
}

function deepAst() {
  let node = { type: 'text', value: 'a' };
  for (let i = 0; i < 4000; i++) {
    node = { type: 'paren', nodes: [node] };
  }
  return { type: 'root', nodes: [node] };
}

for (const method of ['compile', 'expand', 'stringify']) {
  test(`${method} guards a supplied AST that bypasses the parser`, () => {
    assert.throws(() => braces[method](deepAst()), depthError);
  });
}

test('depth options cannot disable the security bound', () => {
  const pattern = '{'.repeat(4000) + 'a' + '}'.repeat(4000);
  assert.throws(() => braces(pattern, { maxDepth: 10000 }), depthError);
  for (const maxDepth of [Number.NaN, Infinity, 0, -1]) {
    assert.throws(() => braces('{a,b}', { maxDepth }), TypeError);
  }
});

test('the glob consumer retains nested sets, ranges, escaping and deduplication', () => {
  assert.deepEqual(micromatch.braces('src/{a,{b,c}}/{01..03}.ts', {
    expand: true, nodupes: true, keepEscaping: true,
  }), [
    'src/a/01.ts', 'src/a/02.ts', 'src/a/03.ts',
    'src/b/01.ts', 'src/b/02.ts', 'src/b/03.ts',
    'src/c/01.ts', 'src/c/02.ts', 'src/c/03.ts',
  ]);
  assert.deepEqual(micromatch.braces('{a,a,b}', { expand: true, nodupes: true }), ['a', 'b']);
  assert.deepEqual(micromatch.braces(String.raw`src/\{a,b\}.ts`, {
    expand: true, keepEscaping: true,
  }), [String.raw`src/\{a,b\}.ts`]);
});

test('real fast-glob discovers CSS and Vue files and respects exclusions', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lober-glob-'));
  try {
    await mkdir(join(dir, 'nested'));
    for (const path of ['one.vue', 'two.css', 'nested/three.scss', 'nested/skip.less', 'other.ts']) {
      await writeFile(join(dir, path), '');
    }
    const found = await glob(['**/*.{vue,css,less,scss}', '!**/skip.*'], { cwd: dir });
    assert.deepEqual(found.sort(), ['nested/three.scss', 'one.vue', 'two.css']);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('real stylelint applies a brace-pattern override and reports invalid CSS', async () => {
  const { default: stylelint } = await import(pathToFileURL(stylelintPath).href);
  const config = {
    rules: {},
    overrides: [{ files: ['**/*.{css,scss}'], rules: { 'color-no-invalid-hex': true } }],
  };
  const good = await stylelint.lint({ code: 'a { color: #fff; }', codeFilename: 'valid.css', config });
  const bad = await stylelint.lint({ code: 'a { color: #fffff; }', codeFilename: 'invalid.scss', config });
  assert.equal(good.errored, false);
  assert.equal(bad.errored, true);
  assert.equal(bad.results[0].warnings[0].rule, 'color-no-invalid-hex');
});

test('real CSpell reads TOML config and preserves spelling failure exits', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lober-cspell-'));
  const cli = fileURLToPath(new URL('../node_modules/cspell/bin.mjs', import.meta.url));
  try {
    await writeFile(join(dir, 'cspell.toml'), 'version = "0.2"\nlanguage = "en"\nwords = ["loberfixtureword"]\n');
    await writeFile(join(dir, 'good.txt'), 'loberfixtureword\n');
    await writeFile(join(dir, 'bad.txt'), 'zzzloberinvalidwordzzz\n');
    const run = (path) => spawnSync(process.execPath, [cli, 'lint', '--config', 'cspell.toml', '--no-progress', path], {
      cwd: dir, encoding: 'utf8', timeout: 30000,
    });
    const good = run('good.txt');
    const bad = run('bad.txt');
    assert.equal(good.status, 0, good.stderr || good.error?.message);
    assert.equal(bad.status, 1, bad.stderr || bad.error?.message);
    assert.match(bad.stdout + bad.stderr, /zzzloberinvalidwordzzz/);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('real Knip still detects an unused manifest dependency', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lober-knip-'));
  const vshRequire = createRequire(new URL('./vsh/package.json', import.meta.url));
  const cli = join(dirname(vshRequire.resolve('knip')), '..', 'bin', 'knip.js');
  try {
    await writeFile(join(dir, 'package.json'), JSON.stringify({
      name: 'lober-knip-fixture', private: true, type: 'module',
      dependencies: { 'lober-unused-fixture': '1.0.0' },
    }));
    await writeFile(join(dir, 'index.js'), 'console.log("fixture");\n');
    await writeFile(join(dir, 'knip.json'), JSON.stringify({ entry: ['index.js'], project: ['**/*.js'] }));
    const result = spawnSync(process.execPath, [cli, '--include', 'dependencies', '--reporter', 'json', '--no-config-hints'], {
      cwd: dir, encoding: 'utf8', timeout: 30000,
    });
    assert.equal(result.status, 1, result.stderr || result.error?.message);
    const report = JSON.parse(result.stdout);
    assert.ok(report.issues.some((issue) => issue.dependencies.some((dep) => dep.name === 'lober-unused-fixture')));
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

// 从实际使用者找包，避免测到没有被产品使用的另一份依赖。
const requestRequire = createRequire(new URL('../packages/effects/request/package.json', import.meta.url));
const axiosRequire = createRequire(requestRequire.resolve('axios'));
const redirects = axiosRequire('follow-redirects');
const formRequire = createRequire(new URL('../packages/@core/ui-kit/form-ui/package.json', import.meta.url));
const z = formRequire('zod');
const viteConfigRequire = createRequire(new URL('../internal/vite-config/package.json', import.meta.url));
const pwaRequire = createRequire(viteConfigRequire.resolve('vite-plugin-pwa'));
const workboxRequire = createRequire(pwaRequire.resolve('workbox-build'));
const terserPath = workboxRequire.resolve('@rollup/plugin-terser');
const serialize = createRequire(terserPath)('serialize-javascript');
const Ajv = workboxRequire('ajv');
const uri = createRequire(workboxRequire.resolve('ajv'))('fast-uri');

async function withRedirectServers(run) {
  const collector = createServer((req, res) => res.end(JSON.stringify(req.headers)));
  let target;
  const origin = createServer((req, res) => {
    if (req.url === '/same') {
      res.writeHead(302, { location: '/received' });
    } else if (req.url === '/cross') {
      res.writeHead(302, { location: target });
    } else {
      res.end(JSON.stringify(req.headers));
      return;
    }
    res.end();
  });
  try {
    collector.listen(0, '127.0.0.1');
    await once(collector, 'listening');
    target = `http://127.0.0.1:${collector.address().port}/received`;
    origin.listen(0, '127.0.0.1');
    await once(origin, 'listening');
    await run(`http://127.0.0.1:${origin.address().port}`);
  } finally {
    await Promise.all([origin, collector].map((server) => new Promise((resolve) => {
      server.closeAllConnections();
      server.close(resolve);
    })));
  }
}

function redirectHeaders(url, headers, sensitiveHeaders = []) {
  // Node数组请求头需要显式Host，否则服务端会先拒绝请求。
  if (Array.isArray(headers)) headers = ['Host', new URL(url).host, ...headers];
  return new Promise((resolve, reject) => {
    const req = redirects.http.get(url, { headers, sensitiveHeaders }, (res) => {
      let body = '';
      res.setEncoding('utf8');
      res.on('data', (chunk) => { body += chunk; });
      res.on('error', reject);
      res.on('end', () => {
        try { resolve(JSON.parse(body)); } catch (error) { reject(error); }
      });
    });
    req.on('error', reject);
    req.setTimeout(5000, () => req.destroy(new Error('本地跳转请求超时')));
  });
}

test('跨站跳转移除数组中的认证、Cookie和自定义敏感头', async () => {
  await withRedirectServers(async (base) => {
    const headers = await redirectHeaders(`${base}/cross`, [
      'Authorization', 'Bearer fixture', 'Cookie', 'session=fixture',
      'Proxy-Authorization', 'Basic fixture', 'X-Api-Key', 'fixture', 'X-Public', 'keep',
    ], ['X-Api-Key']);
    for (const name of ['authorization', 'cookie', 'proxy-authorization', 'x-api-key']) {
      assert.equal(headers[name], undefined, `${name} 被带到了其他站点`);
    }
    assert.equal(headers['x-public'], 'keep');
  });
});

test('跨站跳转按字面名称移除带特殊字符的敏感头', async () => {
  await withRedirectServers(async (base) => {
    const headers = await redirectHeaders(`${base}/cross`, {
      'X-Api^Key': 'fixture', 'X-Token|Secret': 'fixture', 'X-Control-Key': 'fixture',
      'X-Token': 'keep', Secret: 'keep', Authorization: 'Bearer fixture',
    }, ['X-Api^Key', 'X-Token|Secret', 'X-Control-Key']);
    for (const name of ['x-api^key', 'x-token|secret', 'x-control-key', 'authorization']) {
      assert.equal(headers[name], undefined, `${name} 被带到了其他站点`);
    }
    assert.equal(headers['x-token'], 'keep');
    assert.equal(headers.secret, 'keep');
  });
});

test('同站跳转保留认证头，Axios真实HTTP请求也能正常跳转', async () => {
  await withRedirectServers(async (base) => {
    const same = await redirectHeaders(`${base}/same`, ['Authorization', 'Bearer fixture']);
    assert.equal(same.authorization, 'Bearer fixture');
    const axios = requestRequire('axios');
    const response = await axios.get(`${base}/cross`, {
      adapter: 'http', proxy: false, timeout: 5000,
      headers: { Authorization: 'Bearer fixture', 'X-Public': 'keep' },
    });
    assert.equal(response.data.authorization, undefined);
    assert.equal(response.data['x-public'], 'keep');
  });
});

test('表单使用的Zod保留邮箱、默认值和转换规则', () => {
  const form = z.object({
    email: z.email(), port: z.coerce.number().int().min(1).max(65535),
    enabled: z.boolean().default(true), tags: z.array(z.string()).max(3),
  });
  assert.deepEqual(form.parse({ email: 'user@example.test', port: '443', tags: ['a'] }), {
    email: 'user@example.test', port: 443, enabled: true, tags: ['a'],
  });
  assert.equal(form.safeParse({ email: 'bad', port: '0', tags: [] }).success, false);
});

test('Zod提前停止选项阻止大数组累积每一项错误', () => {
  const result = z.array(z.string()).safeParse(Array(100000).fill(0), { abortEarly: true });
  assert.equal(result.success, false);
  assert.equal(result.error.issues.length, 1);
  assert.deepEqual(result.error.issues[0].path, [0]);
});

test('Zod先拒绝超过集合上限的输入', () => {
  const result = z.array(z.string()).max(3).safeParse(Array(100000).fill(0));
  assert.equal(result.success, false);
  assert.equal(result.error.issues.length, 1);
  assert.equal(result.error.issues[0].code, 'too_big');
});

test('Ajv真实引用解析保持相对路径和有效无效数据的区别', () => {
  const ajv = new Ajv({ allErrors: true });
  ajv.addSchema({ $id: 'https://example.test/schemas/common.json', type: 'integer', minimum: 1 });
  const validate = ajv.compile({
    $id: 'https://example.test/schemas/forms/main.json', type: 'object',
    properties: { count: { $ref: '../common.json' } }, required: ['count'], additionalProperties: false,
  });
  assert.equal(validate({ count: 2 }), true);
  assert.equal(validate({ count: 0 }), false);
  assert.equal(validate.errors[0].keyword, 'minimum');
  assert.equal(validate({ count: '2' }), false);
});

test('Ajv使用的地址解析器不把编码协议变成可执行协议', () => {
  for (const input of ['%6Aavascript:alert(1)', '%u006Aavascript:alert(1)']) {
    assert.notEqual(uri.parse(input).scheme, 'javascript');
    assert.doesNotMatch(uri.normalize(input), /^javascript:/i);
    assert.throws(() => uri.resolve('https://example.test/', input), /scheme.*malformed/i);
  }
  assert.equal(uri.equal('%68ttp://a/', 'http://a/'), false);
});

test('Ajv使用的地址解析器不改变相对引用目标', () => {
  assert.equal(uri.normalize('../x'), '../x');
  assert.equal(uri.resolve('https://example.test/area/forms/', uri.normalize('../x')), 'https://example.test/area/x');
  assert.equal(uri.equal('../x', 'x'), false);
});

test('Ajv使用的地址解析器拒绝主机名混淆', () => {
  for (const input of ['http://a@b@evil.example/', 'http://exa\tmple.test/', 'http://exa\nmple.test/', 'http://exa\rmple.test/']) {
    assert.ok(uri.parse(input).error, `应拒绝地址：${JSON.stringify(input)}`);
  }
});

test('Ajv使用的地址解析器为suffix相对引用转义首段冒号', () => {
  for (const scheme of ['javascript', 'data', 'vbscript']) {
    const input = `${scheme}:alert(1)`;
    assert.doesNotMatch(uri.serialize({ scheme, path: input }, { reference: 'suffix' }), new RegExp(`^${scheme}:`, 'i'));
    assert.doesNotMatch(uri.resolve('', input, { reference: 'suffix' }), new RegExp(`^${scheme}:`, 'i'));
  }
});

test('压缩工具使用的序列化器阻止函数末尾闭合脚本标签', () => {
  function f() {}
  function g() {}
  f.toString = () => 'function f(){ return 1 }//</script';
  g.toString = () => 'function g(x){ return x } > <img src=x onerror=alert(1)>';
  const output = serialize({ a: { f }, g }, { space: 2 });
  assert.doesNotMatch(output, /<\/script(?:[\t\n\f\r />]|$)/i);
  assert.match(output, /function f/);
});

test('压缩工具使用的序列化器保留正常函数和特殊字符串', () => {
  const output = serialize({ value: '</script>', twice: (value) => value * 2 });
  assert.doesNotMatch(output, /<\/script/i);
  const restored = runInNewContext(`(${output})`);
  assert.equal(restored.value, '</script>');
  assert.equal(restored.twice(3), 6);
});

test('PWA使用的真实压缩插件能把函数选项传给工作线程', async () => {
  const terser = workboxRequire('@rollup/plugin-terser');
  const plugin = terser({
    maxWorkers: 1, mangle: false,
    format: { comments: (_node, comment) => /keep-fixture/.test(comment.value) },
  });
  const result = await plugin.renderChunk('/*! keep-fixture */\nfunction twice(value) { return value * 2; } globalThis.fixture = twice(3);', {}, { format: 'iife', sourcemap: false });
  const code = typeof result === 'string' ? result : result.code;
  assert.match(code, /keep-fixture/);
  const context = {};
  runInNewContext(code, context);
  assert.equal(context.fixture, 6);
});

async function withViteFixture(run) {
  const dir = await mkdtemp(join(tmpdir(), 'lober-vite-'));
  const rootRequire = createRequire(new URL('../package.json', import.meta.url));
  const { createServer: createViteServer } = await import(pathToFileURL(rootRequire.resolve('vite')).href);
  let server;
  try {
    await writeFile(join(dir, 'package.json'), '{"type":"module"}');
    server = await createViteServer({
      root: dir, configFile: false, logLevel: 'silent',
      optimizeDeps: { noDiscovery: true },
      server: { host: '127.0.0.1', port: 0, hmr: false, watch: null, fs: { strict: true, allow: [dir] } },
    });
    await server.listen();
    await run(dir, server, `http://127.0.0.1:${server.httpServer.address().port}`);
  } finally {
    await server?.close();
    await rm(dir, { recursive: true, force: true });
  }
}

test('Vite不把项目内导入路径误当成项目外的文件许可', { skip: process.platform === 'win32' }, async () => {
  const outside = await mkdtemp(join(tmpdir(), 'lober-vite-outside-'));
  const secret = join(outside, 'secret.js');
  try {
    await writeFile(secret, 'globalThis.fixtureSecret = "outside-fixture";');
    await withViteFixture(async (dir, _server, base) => {
      const relative = secret.slice(1);
      await mkdir(dirname(join(dir, relative)), { recursive: true });
      await writeFile(join(dir, relative), 'export const decoy = true;');
      await writeFile(join(dir, 'main.js'), `import "./${relative}";`);
      assert.equal((await fetch(`${base}/main.js`)).status, 200);
      const denied = await fetch(`${base}/@fs${secret}`);
      assert.equal(denied.status, 403);
      assert.doesNotMatch(await denied.text(), /outside-fixture/);
    });
  } finally {
    await rm(outside, { recursive: true, force: true });
  }
});

test('Vite的Wasm请求不泄露禁止读取文件的前四字节', async () => {
  await withViteFixture(async (dir, _server, base) => {
    await writeFile(join(dir, '.env'), 'ABCD_fixture_only=1');
    const response = await fetch(`${base}/.env?vite-wasm-instance`);
    assert.ok(response.status >= 400);
    assert.doesNotMatch(await response.text(), /ABCD|41 42 43 44/);
  });
});

test('Vite转换HTML时不把请求网址变成外站脚本地址', async () => {
  await withViteFixture(async (_dir, server) => {
    const html = await server.transformIndexHtml('//evil.example/payload.js?/../../package.json', '<script type="module">globalThis.fixture = true</script>');
    assert.doesNotMatch(html, /src=["']\/\/evil\.example\//);
    assert.match(html, /html-proxy/);
  });
});
