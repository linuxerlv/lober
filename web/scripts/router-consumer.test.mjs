import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { copyFile, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import test from 'node:test';
import { fileURLToPath, pathToFileURL } from 'node:url';

const webRoot = fileURLToPath(new URL('../', import.meta.url));
const appRoot = join(webRoot, 'apps/web-ele');
const rootRequire = createRequire(new URL('../package.json', import.meta.url));

test('应用安装的路由能供真实布局读取，并在导航和卸载时正确工作', async () => {
  const { build, loadConfigFromFile } = await import(pathToFileURL(rootRequire.resolve('vite')).href);
  const { Window } = await import(pathToFileURL(rootRequire.resolve('happy-dom')).href);
  const fixture = await mkdtemp(join(appRoot, '.router-consumer-'));
  const entry = join(fixture, 'entry.ts');
  const output = join(fixture, 'dist');
  const originalCwd = process.cwd();
  const modules = new Set();
  let window;
  const globals = new Map();

  try {
    // 入口位于应用目录；布局仍从自己的真实目录导入依赖。
    await writeFile(entry, `
import { createApp, defineComponent, h, nextTick } from 'vue';
import { createMemoryHistory, createRouter, useRouter } from 'vue-router';
import { useLayoutScroll } from '../../../packages/effects/layouts/src/basic/use-layout-scroll';

export async function exerciseLayoutRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: ['/', '/next', '/after-unmount'].map((path) => ({ path, component: { render: () => null } })),
  });
  const errors = [];
  const scrollCalls = [];
  const scroll = document.createElement('div');
  scroll.id = '__vben_layout_scroll';
  scroll.scrollTo = (options) => scrollCalls.push(options);
  document.body.append(scroll);
  const root = document.createElement('div');
  document.body.append(root);
  let sameProvidedRouter = false;
  let layoutSetupCompleted = false;
  const view = defineComponent({
    setup() {
      sameProvidedRouter = useRouter() === router;
      useLayoutScroll();
      layoutSetupCompleted = true;
      return () => h('main', router.currentRoute.value.path);
    },
  });
  const app = createApp(view);
  app.config.errorHandler = (error) => errors.push(error.message);
  app.use(router);
  await router.push('/');
  await router.isReady();
  app.mount(root);
  await router.push('/next');
  await nextTick();
  const renderedRoute = root.textContent;
  const callsBeforeUnmount = scrollCalls.length;
  app.unmount();
  await router.push('/after-unmount');
  await nextTick();
  const callsAfterUnmount = scrollCalls.length;
  root.remove();
  scroll.remove();
  return { errors, sameProvidedRouter, layoutSetupCompleted, renderedRoute, scrollCalls, callsBeforeUnmount, callsAfterUnmount };
}
`);

    process.chdir(appRoot);
    const loaded = await loadConfigFromFile(
      { command: 'build', mode: 'production' },
      join(appRoot, 'vite.config.ts'),
    );
    assert.ok(loaded, '应用的真实构建配置必须能够读取');

    // 沿用应用全部插件及解析规则，只把入口和产物位置换成这个小用例。
    await build({
      ...loaded.config,
      configFile: false,
      root: appRoot,
      publicDir: false,
      logLevel: 'warn',
      plugins: [
        ...(loaded.config.plugins || []),
        {
          name: 'lober-router-consumer-evidence',
          generateBundle(_options, bundle) {
            for (const chunk of Object.values(bundle)) {
              if (chunk.type === 'chunk') {
                for (const id of Object.keys(chunk.modules)) modules.add(id);
              }
            }
          },
        },
      ],
      build: {
        ...loaded.config.build,
        outDir: output,
        emptyOutDir: true,
        lib: { entry, formats: ['es'], fileName: () => 'router-consumer.mjs' },
        rolldownOptions: {
          ...loaded.config.build?.rolldownOptions,
          input: entry,
          output: { format: 'es', entryFileNames: 'router-consumer.mjs', codeSplitting: false, minify: false },
        },
      },
    });

    window = new Window({ url: 'http://localhost/' });
    for (const name of ['window', 'document', 'navigator', 'HTMLElement', 'SVGElement', 'Element', 'Node', 'ShadowRoot', 'history', 'location', 'CSS']) {
      globals.set(name, Object.getOwnPropertyDescriptor(globalThis, name));
      Object.defineProperty(globalThis, name, { configurable: true, writable: true, value: window[name] });
    }
    const { exerciseLayoutRouter } = await import(pathToFileURL(join(output, 'router-consumer.mjs')).href);
    const result = await exerciseLayoutRouter();
    const routerModules = [...modules].filter((id) => id.includes('/vue-router/')).sort();
    const proof = { result, routerModules, emittedFiles: [] };
    for (const name of await readdir(output)) {
      const body = await readFile(join(output, name));
      proof.emittedFiles.push({ path: name, bytes: body.length, sha256: createHash('sha256').update(body).digest('hex') });
    }
    const evidenceDir = process.env.LOBER_ROUTER_CONSUMER_EVIDENCE_DIR;
    if (evidenceDir) {
      await mkdir(evidenceDir);
      await copyFile(entry, join(evidenceDir, 'entry.ts'));
      for (const file of proof.emittedFiles) await copyFile(join(output, file.path), join(evidenceDir, file.path));
      await writeFile(join(evidenceDir, 'proof.json'), JSON.stringify(proof, null, 2));
    }
    console.log(JSON.stringify({ routerModules, result }));
    assert.deepEqual(result.errors, [], '布局挂载不能因缺少路由对象而抛错');
    assert.equal(result.sameProvidedRouter, true);
    assert.equal(result.layoutSetupCompleted, true);
    assert.equal(result.renderedRoute, '/next');
    assert.ok(result.callsBeforeUnmount > 0, '实际导航应触发布局的滚动处理');
    assert.deepEqual(result.scrollCalls.at(-1), { top: 0 });
    assert.equal(result.callsAfterUnmount, result.callsBeforeUnmount, '卸载后必须移除布局的导航钩子');
  } finally {
    window?.close();
    for (const [name, descriptor] of globals) {
      if (descriptor) Object.defineProperty(globalThis, name, descriptor);
      else delete globalThis[name];
    }
    process.chdir(originalCwd);
    await rm(fixture, { recursive: true, force: true });
  }
});
