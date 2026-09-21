# 上游 pin 记录

> 本文档记录 M8 任务 1 对 vue-vben-admin 上游仓库的实机核对结果。
> 全部数据来自本次核对时（见「核对时间」）对 `raw.githubusercontent.com` 与
> `api.github.com` 的实际请求输出，不是抄自 SPEC.md §12。
>
> **核对环境限制：** 本机没有 Node 与 pnpm（`node -v`、`pnpm -v` 均
> command not found），因此本文档只能核实"文件内容与目录是否存在"，
> 不能核实"装上依赖后是否真的能 build/typecheck 过"——那需要任务 2 起
> 在有 Node/pnpm 的机器上验证。

## 核对时间与方法

- 核对日期：2026-09-21
- 方法：`curl -H "User-Agent: ..." https://raw.githubusercontent.com/vbenjs/vue-vben-admin/main/<path>`
  取文件内容；`curl https://api.github.com/repos/vbenjs/vue-vben-admin/...`
  取仓库元数据（commit、目录列表）。WebFetch 对 `github.com` 域名被挡
  （"Unable to verify if domain github.com is safe to fetch"），改用
  raw 与 api 子域直连，均可用。
- `api.github.com` 未认证请求限额较低（每小时 60 次），核对过程中触发过一次
  `403 rate limit exceeded`，等待窗口重置后继续，不影响结论。

## Pin 目标

| 项 | 值 |
|---|---|
| pin commit sha | `8f2295700eae45fc6d07dbabc5ca6ab586b12855` |
| commit 日期 | 2026-09-19T12:00:37Z |
| commit message | `docs: fix demo-preview and  code copy btn (#8411)` |
| 取值命令 | `curl -H "User-Agent: lober-m8-agent" -H "Accept: application/vnd.github+json" https://api.github.com/repos/vbenjs/vue-vben-admin/commits/main` |

**按此 commit sha clone，不跟 main 头**（SPEC §12.9：升级当独立任务做，装机功能
开发中途不跟版）。

## 五项核对结果

### 1. 根 package.json 与目标应用的实际版本号

**命令：** `curl https://raw.githubusercontent.com/vbenjs/vue-vben-admin/main/package.json`
与 `.../apps/web-ele/package.json`

**实际输出（关键字段）：**

```json
// 根 package.json
{
  "name": "vben-admin-monorepo",
  "version": "5.7.0",
  "engines": { "node": "^22.18.0 || ^24.12.0", "pnpm": ">=11.0.0" },
  "packageManager": "pnpm@11.16.0"
}

// apps/web-ele/package.json
{ "name": "@vben/web-ele", "version": "5.8.0" }
```

**结论：与 SPEC §12.1 的记录完全一致**（monorepo `5.7.0` / `@vben/web-ele` `5.8.0`，
Node `^22.18.0 || ^24.12.0`，`packageManager: pnpm@11.16.0`）。SPEC 写作时点
（2026-09-20）与本次核对（2026-09-21）间隔一天，上游未发生影响这些字段的变化。

**依赖版本不在 package.json 直接体内，走 `catalog:` 协议**（这点 SPEC 未提及，
是本次核对新发现的事实）——需要看 `pnpm-workspace.yaml` 的 `catalog:` 表：

**命令：** `curl https://raw.githubusercontent.com/vbenjs/vue-vben-admin/main/pnpm-workspace.yaml`

**实际输出（与 web-ele 相关的条目）：**

```yaml
catalog:
  vue: 3.5.41
  vue-router: ^5.2.0
  pinia: ^4.0.2
  element-plus: ^2.14.3
  vxe-table: ^4.21.2
  axios: ^1.18.1
  vite: ^8.3.0
  typescript: ^6.0.3
  tailwindcss: ^4.3.3
  vue-i18n: ^11.4.10
  unplugin-element-plus: ^0.11.2
```

**结论：与 SPEC §12.1 框架栈表逐项一致**（Vue 3.5.41、Vite 8.3、TypeScript 6.0、
Tailwind CSS 4.3、vue-router 5.2、pinia 4.0、Element Plus 2.14.3、vxe-table 4.21、
axios 1.18、vue-i18n 11.4）。**任务 2 起 clone 后要读 `pnpm-workspace.yaml` 的
`catalog:` 表而不是各 `package.json`**，否则版本号对不上。

### 2. §12.2 精简清单里涉及的目录是否都还在原处

**命令：** 对以下 18 个路径逐一 `curl -o /dev/null -w "%{http_code}"` 探测：

| 路径 | HTTP 状态 |
|---|---|
| `apps/web-antd/package.json` | 200 |
| `apps/web-antdv-next/package.json` | 200 |
| `apps/web-naive/package.json` | 200 |
| `apps/web-tdesign/package.json` | 200 |
| `apps/backend-mock/package.json` | 200 |
| `playground/package.json` | 200 |
| `docs/package.json` | 200 |
| `.changeset/config.json` | 200 |
| `apps/web-ele/src/router/routes/core.ts` | 200 |
| `apps/web-ele/src/router/routes/modules/dashboard.ts` | 200 |
| `apps/web-ele/src/router/routes/modules/demos.ts` | 200 |
| `apps/web-ele/src/router/routes/modules/vben.ts` | 200 |
| `apps/web-ele/src/views/_core/authentication/code-login.vue` | 200 |
| `apps/web-ele/src/views/_core/authentication/qrcode-login.vue` | 200 |
| `apps/web-ele/src/views/_core/authentication/forget-password.vue` | 200 |
| `apps/web-ele/src/views/_core/authentication/register.vue` | 200 |
| `apps/web-ele/src/views/_core/fallback/coming-soon.vue` | 200 |
| `apps/web-ele/src/views/_core/fallback/offline.vue` | 200 |

**结论：18/18 全部存在（200）。SPEC §12.2 的精简清单未漂移**，可以照抄执行删除。
`packages/**`（框架本体）与 `internal/**`（构建配置）也确认存在，按 SPEC 要求
全留。

**未核实项：** `packages/effects/common-ui/src/ui/{about,dashboard,profile}` 三个
子目录的存在性本次未逐一探测（SPEC §12.2 提到但优先级低于应用级目录），接手人
clone 后需自行确认。

### 3. 请求封装里响应拦截器的字段名

**命令：** `curl https://raw.githubusercontent.com/vbenjs/vue-vben-admin/main/apps/web-ele/src/api/request.ts`

**实际输出（关键片段）：**

```ts
client.addResponseInterceptor(
  defaultResponseInterceptor({
    codeField: 'code',
    dataField: 'data',
    successCode: 0,
  }),
);
// ...
export const requestClient = createRequestClient(apiURL, {
  responseReturn: 'data',
});
```

**结论：与 SPEC §12.5 记录完全一致**（`codeField: 'code'` / `dataField: 'data'` /
`successCode: 0` / `responseReturn: 'data'`），也与 `docs/contracts/http-console.md`
定稿的响应包裹格式（`{"code":0,"data":...,"message":""}`）互相印证。**M6/M8 均
不需要改这段框架代码。**

### 4. 偏好设置文件里各覆盖项的字段路径

**命令：** `curl https://raw.githubusercontent.com/vbenjs/vue-vben-admin/main/packages/@core/preferences/src/config.ts`
（默认值定义处，字段路径的权威来源）；另 `.../src/types.ts` 核对
`widget.languageToggle`。

**实际输出核对结果：**

| SPEC §12.4 提到的字段路径 | 本次核对结果 |
|---|---|
| `app.accessMode` | 存在，默认值 `'frontend'` |
| `app.defaultHomePath` | 存在，默认值 `'/dashboard'` |
| `app.locale` | 存在，默认值 `'zh-CN'` |
| `widget.languageToggle` | 存在（`WidgetPreferences` 接口字段） |
| `app.enableCheckUpdates` | 存在，默认值 `true` |
| `app.defaultAvatar` | 存在，默认值仍指向 `https://unpkg.com/@vbenjs/static-source@0.1.7/...`（未换成本地） |
| `logo.source` | 存在，默认值仍指向 `https://unpkg.com/@vbenjs/static-source@0.1.7/...`（未换成本地） |
| `app.watermark` | 存在，默认值 `false` |
| `copyright.enable` | 存在，默认值 `true` |
| `tabbar`（整节） | 存在 |

**结论：SPEC §12.4 表里全部字段路径仍然有效，未漂移。** `unpkg.com` 的两处外网
依赖（logo、头像）确认仍是上游默认值，任务 6/7 的"换成本地文件"仍然必要且
路径不变。

`apps/web-ele/src/preferences.ts`（应用侧覆盖文件本体）实际内容比 SPEC §12.4 表
列出的八项少得多——当前只覆盖 `app.name` 与 `copyright`：

```ts
export const overridesPreferences = defineOverridesPreferences({
  app: { name: import.meta.env.VITE_APP_TITLE },
  copyright: appCopyrightPreferences,
});
```

这不代表其余字段不能覆盖（`defineOverridesPreferences` 接受任意深度合并的
`Preferences` 子集），只是上游默认工程只演示了两项。任务 6 落地时按 SPEC §12.4
表的八项直接加进这个对象即可，字段路径已确认全部存在。

### 5. Node 与 pnpm 的版本要求

见「1」中根 `package.json` 的 `engines`/`packageManager` 字段：
`node: "^22.18.0 || ^24.12.0"`、`pnpm: ">=11.0.0"`、`packageManager: "pnpm@11.16.0"`。
与 SPEC §12.1 一致。

## 额外发现（SPEC 未提及，本次核对新增的事实）

### A. 百度统计脚本的具体触发条件（任务 7 第 1 处）

**命令：** `curl https://raw.githubusercontent.com/vbenjs/vue-vben-admin/main/apps/web-ele/index.html`

**实际输出：** 脚本位于 `<head>` 内，**被 `if (window._VBEN_ADMIN_PRO_APP_CONF_)`
条件门控**：

```html
<script>
  // 生产环境下注入百度统计
  if (window._VBEN_ADMIN_PRO_APP_CONF_) {
    var _hmt = _hmt || [];
    (function () {
      var hm = document.createElement('script');
      hm.src = 'https://hm.baidu.com/hm.js?97352b16ed2df8c3860cf5a1a65fb4dd';
      var s = document.getElementsByTagName('script')[0];
      s.parentNode.insertBefore(hm, s);
    })();
  }
</script>
```

由于 `_VBEN_ADMIN_PRO_APP_CONF_` 是上游 Pro 版本才会注入的全局变量，本项目
（开源版）构建产物中该条件天然为假，脚本不会执行。**但任务 7 仍要求物理删除
这段代码**（不能依赖"条件恰好不满足"这种脆弱保证——上游任何时候都可能改变
判断条件或去掉门控），删除方式是直接删掉这段 `<script>` 标签。

### B. 图标在线解析的具体机制（任务 7 第 3 处，对应 SPEC 附录 B 待核实项）

**命令：** 列出并核对 `packages/@core/base/icons/src/` 下的源文件（`create-icon.ts`、
`index.ts`、`lucide.ts`）。

**关键发现：`@vben/icons` 包不再有 `iconify.ts` 文件**（若存在应导出一个统一的
iconify 图标解析入口，但当前源码树里没有这个文件名）。实际结构是：

- `lucide.ts`：从 `@lucide/vue`（本地依赖，构建时静态打包）具名导出约 90 个
  图标组件，如 `export { Search, Settings, ... } from '@lucide/vue'`——**这些是
  本地组件，不走网络**。
- `create-icon.ts`：导出 `createIconifyIcon(icon: string)`，内部用
  `h(Icon, { icon, ...props })` 渲染 `@iconify/vue` 的 `Icon` 组件——**这个函数
  接受一个图标名字符串，运行时未命中本地注册表就会向 Iconify 在线 API 请求**。
- `index.ts`：同时 re-export 上面两者，以及 `@iconify/vue` 的 `addCollection`/
  `addIcon`/`Icon`（别名 `IconifyIcon`）/`listIcons`。

**实际验证了路由 `meta.icon` 字段的具体写法**（`apps/web-ele/src/router/routes/modules/vben.ts`）：

```ts
icon: 'lucide:book-open-text',   // 字符串图标名——会走 iconify 在线解析
icon: 'mdi:github',              // 同上，且 mdi 集合更不可能被本地预注册
icon: 'logos:naiveui',           // 同上
icon: SvgAntdvLogoIcon,          // 本地 svg 组件对象——不走网络
```

**结论（解答 SPEC 附录 B"控制台 UI 离线图标"待核实项）：**
`@iconify/vue` 对未本地注册的图标名**确实会回落到在线 Iconify API**（`Icon`
组件与 `createIconifyIcon()` 均基于同一渲染路径，运行时按 `icon` 字符串向外
请求 SVG 数据）。规避方式有两种：

1. **只使用 `lucide.ts` 导出的本地组件对象**赋给 `meta.icon`（对应 SPEC 任务 7
   要求的"引用本地图标组件"）——本次核对确认 `lucide.ts` 导出足够覆盖常见场景
   （箭头、菜单、设置、用户、锁、任务列表等语义相关的图标都有）。
2. 若确实需要用字符串图标名（比如美观性要求某个 lucide 未覆盖的图形），必须
   在应用启动时调用 `addCollection()` **预注册**该图标数据到本地，使 `Icon`
   组件命中缓存而不外发请求——**这条路径本次未做断网实测**（本机无 Node/pnpm
   无法跑起应用），SPEC 任务 7 要求的"断网后验证图标是否正常显示"这一步
   留给有构建环境的后续任务执行。

**本模块的路由文件（若涉及 icon 字段）已按第 1 种方式处理**——但受任务范围
限制，本轮不产出实际路由代码，此结论供任务 8/9 的实现者参照。

## 未能核实的项

- **`pnpm install`/`pnpm build:ele` 是否真的能在这份精简后跑通**——本机无
  Node/pnpm，无法实测。SPEC §12.3 附录 B 的"前端离线构建"待核实项（`pnpm fetch`
  + `pnpm install --offline` 能否装上这套含原生二进制依赖的依赖树）同样
  未能核实，需要有 Node/pnpm 环境的任务执行者验证。
- **`packages/effects/common-ui/src/ui/{about,dashboard,profile}` 三个子目录**
  的存在性未逐一探测（见「2」）。
- **`vue-tsc`/`typescript` 6.0 与表单底层 TanStack Form + Zod 4 的实际集成状态**——
  SPEC §12.9 提到"表单底层也换过"，本次核对确认 `catalog:` 里有
  `@tanstack/vue-form: ^1.33.2` 与 `zod: ^4.4.3`，但未深入核对 `apps/web-ele` 的
  表单组件是否已切换到这套底层（任务 10 的表单 schema 转换层实现者需要自行
  核对 `src/adapter/form-schema.ts` 或对应文件的实际写法，不要按 SPEC §12.7
  的旧描述硬套）。
