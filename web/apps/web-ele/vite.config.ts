import process from 'node:process';

import { defineConfig, viteCssLayerPlugin } from '@vben/vite-config';

import ElementPlus from 'unplugin-element-plus/vite';

export default defineConfig(async () => {
  return {
    application: {},
    vite: {
      build: {
        // 构建产物直接落到 internal/console/ui 的 go:embed 目录，不是构建完
        // 再 cp —— go:embed 不接受 ".." 路径，产物必须原生落在 embed 所在
        // 包的目录树内（SPEC §12.3）。
        outDir: '../../../internal/console/ui/dist',
        // outDir 在项目根目录之外时 Vite 默认不清空（避免误删无关目录），
        // 需要显式打开。没有这条，历史构建的哈希文件名资源会在 dist 里
        // 无限堆积（内容寻址文件名不同名，旧文件不会被同名新文件覆盖），
        // 且仓库里当前的占位文件（index.html 占位内容、_app/
        // config-placeholder.js 等）也不会被真实产物替换/清理。
        emptyOutDir: true,
      },
      plugins: [
        // element-plus 的 css 包进 @layer el，使 Tailwind 工具类可覆盖组件样式
        viteCssLayerPlugin({ layerName: 'el', packageName: 'element-plus' }),
        ElementPlus({ format: 'esm' }),
      ],
      server: {
        proxy: {
          '/console/v1': {
            changeOrigin: true,
            target: process.env.LOBER_API_TARGET || 'http://127.0.0.1:58080',
          },
          '/openapi.json': {
            target: process.env.LOBER_API_TARGET || 'http://127.0.0.1:58080',
          },
        },
      },
    },
  };
});
