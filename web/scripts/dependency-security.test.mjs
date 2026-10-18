import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath, pathToFileURL } from 'node:url';

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
