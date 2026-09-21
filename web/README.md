# Lober 控制台 WebUI

## 本仓库当前状态

**前端源码树尚未落地。** 原因：本轮实施环境没有 Node 与 pnpm（`node -v`、
`pnpm -v` 均返回 command not found），M8 计划里需要 clone 上游 monorepo 并
`pnpm install`/`pnpm build` 的任务（任务 2、3、5、6、7、8、9、10、11、12、13，
以及任务 4 的前端构建部分）在这个环境下无法执行，不属于"可以绕过但选择不做"，
是环境硬约束。

**已就位的部分：**

- `web/UPSTREAM_PIN.md`：对上游仓库实际状态的核对记录（版本号、目录结构、
  响应拦截器字段名、偏好设置字段路径、图标在线解析机制），含 pin 的 commit
  sha，供接手人直接按此 clone。
- `internal/console/ui/embed.go` + `embed_test.go`：Go 侧的 `go:embed` handler，
  已按 TDD 实现并测试通过，可以直接内嵌真实前端产物。
- `internal/console/ui/dist/`：**最小占位产物**（`index.html` 一个骨架页面，
  外加两个用来锁住 embed 模式与缓存策略测试的占位文件）。这不是真实前端，
  只是为了让 `go:embed all:dist` 有东西可嵌，使 `internal/console/ui` 包能
  编译通过，`cmd/lober`（M9）才能完成装配。真实前端构建产物会整体覆盖这个
  目录（vite build 的 outDir 直接指向这里，见任务 4 的设计，不是构建完再
  复制）。

**接手人需要做什么（按 M8 计划任务 2 起，在有 Node/pnpm 的机器上）：**

1. 按 `web/UPSTREAM_PIN.md` 记录的 pin commit sha clone 上游仓库（不要跟
   main 头，理由见该文档与 SPEC §12.9：装机功能开发中途跟版会把两类问题
   混在一起排查）。
2. 按 SPEC §12.2 与 `UPSTREAM_PIN.md` 的核对结果做仓库级与应用级精简。
3. 按本文档下面「构建流程」一节，把 `apps/web-ele/vite.config.ts` 的
   `build.outDir` 指向 `internal/console/ui/dist`（不是构建完再复制）。
4. 按 `docs/contracts/http-console.md` 实现 API 客户端与 P1 六个页面
   （登录、任务列表、任务详情、建任务、主机清单、模板），具体要求见
   M8 计划任务 5 起的各条。
5. 构建产物覆盖占位 dist 后，`go test ./internal/console/ui/` 应该仍然
   全部通过（handler 的行为不依赖 dist 内容是占位还是真实产物）。

## 交付方式

**构建必须在有外网的机器上做。** 气隙内网装不上 npm 依赖，这不是能绕的事——
`pnpm install` 需要连 registry 拉取整棵依赖树，包括含原生二进制的依赖
（如 esbuild、Rollup 的原生绑定），私有源镜像不解决"内网本身没有出网通道"
这个前提问题。

三种做法，按推荐顺序（SPEC §12.3）：

1. **开发机或 CI 构建，只把 `dist`（或含 dist 的发布包）带进内网 —— MVP 采用**
   优点：内网只需要拿到构建产物，不需要拿到 `node_modules` 或任何 npm 相关
   的东西，交付物最小、审计面最小。缺点：内网无法临时改前端调试，改动需要
   走"外网改 → 构建 → 带进内网"的完整流程。
2. **外网 `pnpm fetch` 灌满离线 store，整个 store 带进内网后
   `pnpm install --offline`**
   适用场景：内网确实需要在现场用 Node/pnpm 本地调试或重新构建（比如现场
   排查一个只在特定网络环境下复现的前端 bug）。代价是要把一个完整的 pnpm
   store（体量以百 MB 到 GB 计，取决于依赖树）带进内网，且 `pnpm fetch`
   在这套依赖树（Vite 8、Tailwind 4，含原生二进制依赖）下能否真的完整
   离线安装**本轮未实测**（见 `UPSTREAM_PIN.md`"未能核实的项"），接手人
   若选此方案需先在外网环境验证一遍完整流程。
3. **内网起私有 registry（verdaccio / Nexus）镜像上游 —— 只有当内网确实
   需要改前端时才值得**
   代价最高（要在内网维护一套 registry 服务及其镜像同步机制），仅在方案 2
   的"临时离线安装"不够用、需要长期在内网迭代前端代码时才考虑。

**本项目当前选择方案 1。** 原因：本系统的前端定制收敛在四处（`apps/web-ele/
src/**`、`.env*`、`index.html`、根 `package.json` 的 `scripts`，见下文
「与上游同步」一节），日常迭代在有外网的开发环境完成，气隙内网只是部署
目标，不是开发环境——不需要方案 2/3 提供的"内网本地构建"能力。

**`pnpm-lock.yaml` 必须提交。** 这是锁定依赖树具体版本的唯一依据，没有它，
"外网构建"这一步在不同时间点执行会拿到不同的依赖版本，version pin（见
`UPSTREAM_PIN.md`）的意义就打折扣。

**`dist` 是否入库取决于交付方式：** 方案 1（当前选择）下，如果内网部署时
只能拿到 git 仓库本身（没有独立的发布包分发渠道），`dist` 就必须提交进
仓库；如果部署走"发布包"（release tar，独立于 git 仓库分发），则不必提交，
`.gitignore` 里 `internal/console/ui/dist/` 的忽略规则对真实产物依然生效
（本轮加的例外只覆盖当前的占位文件路径，不是给真实产物开的口子——真实产物
落地后，接手人需要重新评估是否需要针对真实文件名再加例外，或者改用发布包
分发从而不需要例外）。

## 气隙验收

以下三条 grep 检查必须在**构建产物**（`internal/console/ui/dist/` 的真实
内容，不是占位文件）上跑，且必须无匹配才能视为气隙验收通过。本轮占位产物
不含真实前端资源，这三条检查暂不适用，留给接手人在真实构建产物就位后执行：

```bash
# 1. 产物里无第三方统计脚本的域名（SPEC §12.4 第 1 条：生产构建注入的百度统计）
grep -r "hm\.baidu\.com" internal/console/ui/dist/

# 2. 产物里无公共 CDN 的域名（SPEC §12.4 第 2 条：logo 与默认头像默认指向 unpkg.com）
grep -r "unpkg\.com" internal/console/ui/dist/

# 3. 路由定义源码里无在线图标名（SPEC §12.4 第 3 条，见下方"图标"一节）
grep -rn "icon:\s*['\"][a-z-]\+:[a-z-]\+['\"]" apps/web-ele/src/router/routes/
```

三条都应该无输出（无匹配）。

### SPEC §12.4 三处外网依赖的具体处置

本轮通过实机核对（见 `UPSTREAM_PIN.md`）确认了这三处依赖在当前上游版本中
的具体表现形式，接手人处置时可直接参照：

1. **百度统计脚本**：位于 `apps/web-ele/index.html` 的 `<head>` 内，被
   `if (window._VBEN_ADMIN_PRO_APP_CONF_)` 条件门控（开源版本该变量不存在，
   条件天然为假）。**仍需物理删除这段 `<script>`**，不能依赖"条件恰好不
   满足"这种脆弱保证。

2. **logo 与默认头像**：`packages/@core/preferences` 的默认配置里
   `logo.source` 与 `app.defaultAvatar` 两个字段默认值都指向
   `https://unpkg.com/@vbenjs/static-source@0.1.7/...`。在
   `apps/web-ele/src/preferences.ts` 的 `overridesPreferences` 里覆盖这两个
   字段，指向 `public/` 下的本地文件。

3. **图标在线解析**（SPEC 附录 B 待核实项，本轮已核实，见下）。

### 图标：为什么会有在线解析风险，怎么规避

`@vben/icons` 包同时提供两条路径：

- `lucide.ts` 具名导出约 90 个来自 `@lucide/vue`（本地依赖，构建时静态
  打包）的图标组件——**这条路径完全离线**，用组件对象而不是字符串引用。
- `create-icon.ts` 导出的 `createIconifyIcon(icon: string)`，以及包本身
  re-export 的 `@iconify/vue` 的 `Icon` 组件——**这条路径接受一个图标名
  字符串（如 `'lucide:search'`、`'mdi:github'`），运行时若该名字未被
  `addCollection()` 预注册到本地，会向 Iconify 在线 API 请求 SVG 数据**。
  上游示例路由（如 `router/routes/modules/vben.ts`，本身是 SPEC §12.2 的
  删除目标，但暴露了框架的常见写法）里 `meta.icon` 字段大量使用这种字符串
  形式。

**规避方式：路由 `meta.icon` 字段只赋值 `lucide.ts` 导出的组件对象，不写
图标名字符串。** 若确实需要某个 `lucide.ts` 未覆盖的图形，必须在应用启动
时调用 `addCollection()` 预注册该图标数据，使 `Icon` 组件命中本地缓存而不
外发请求。

**`grep -rn "icon:\s*['\"][a-z-]\+:[a-z-]\+['\"]"` 这条检查只能覆盖"直接
写字符串"的情形**（形如 `icon: 'lucide:search'`），无法检测"变量间接持有
一个字符串图标名"这种写法——若审查时发现这种间接写法，需要人工追踪变量
来源，静态 grep 不够用。

**断网验证仍是必要步骤（SPEC 任务 7 明确要求）：** 本轮受限于本机没有
Node/pnpm 无法运行应用，**没有做过实际断网测试**。接手人在真实构建产物
就位、且有能运行前端的环境后，必须实际断开外网连接打开控制台页面，确认
全部图标正常显示——静态 grep 检查不了图标名是否恰好命中了浏览器本地已有
的 DNS/HTTP 缓存这种运气因素，只有真实断网测试能排除这种假阳性。

## 构建流程（供有 Node/pnpm 环境时参照）

```bash
# 1. 按 UPSTREAM_PIN.md 记录的 pin commit clone
git clone https://github.com/vbenjs/vue-vben-admin.git /tmp/vben-upstream
cd /tmp/vben-upstream
git checkout 8f2295700eae45fc6d07dbabc5ca6ab586b12855

# 2. 按 SPEC §12.2 精简（删非目标应用、mock、演示、文档站源码等，
#    packages/** 与 internal/** 全留）

# 3. 把结果移到本仓库的 web/ 目录，vite.config.ts 的 build.outDir 改为
#    直接指向 ../../../internal/console/ui/dist（相对于
#    web/apps/web-ele/vite.config.ts 的路径），而不是构建完再复制——
#    go:embed 不接受 ".." 路径，产物必须原生落在 embed 所在包的目录树内，
#    这一步做对了后面就不需要额外的复制脚本。

# 4. 安装与构建（在有外网的机器上）
pnpm install
pnpm run build:ele

# 5. 确认产物落地
ls internal/console/ui/dist/index.html

# 6. 跑 Go 测试确认 embed 与产物兼容
go test ./internal/console/ui/ -v
```

## 与上游同步

**不做侵入式修改，定制只收敛在四处**（SPEC §12.9）：

- `apps/web-ele/src/**`
- `apps/web-ele/.env*`
- `apps/web-ele/index.html`
- 根 `package.json` 的 `scripts`

`packages/**` 与 `internal/**` 保持原样（删除文件不算"改"，只是不需要的
应用/文档/CI 相关内容不进本仓库）。升级上游版本时，直接拿新 pin commit 的
这两棵树整体覆盖本仓库对应位置，四处定制内容不动。

**pin 住一个上游 commit，升级当独立任务做。** 5.x 仍在快速迭代——`UPSTREAM_PIN.md`
记录的核对时点与 SPEC 写作时点只隔一天，但历史上这一带已经换过 Vite 大版本、
CSS 框架、TypeScript 大版本、表单底层实现。在装机功能开发中途跟版，会把
"装机逻辑坏了"和"前端框架升级坏了"两类问题混在一起排查，升级必须是一次
独立提交、独立验证的任务。

## 相关文档

- `web/UPSTREAM_PIN.md`：上游版本核对记录与 pin commit。
- `docs/contracts/http-console.md`：前后端对接的 HTTP 契约（冻结文档）。
- `docs/superpowers/plans/2026-09-20-lober-mvp-08-webui.md`：本模块完整
  实现计划，含全部 15 个任务的细节要求。
- SPEC.md §12：控制台 WebUI 的设计依据与已确定值。
