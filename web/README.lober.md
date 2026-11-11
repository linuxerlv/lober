# Lober 控制台前端

第 6 层基于已有 `apps/web-ele` 改造，保留 Vben 布局、Element Plus、VxeTable 适配与上游 `packages/`。主机、ISO、hook、模板、任务、用户和审计页面对接新版控制台。

## 开发与验证

在 `web/` 使用项目声明的 Node 与 pnpm 版本执行：

```sh
pnpm install --frozen-lockfile
pnpm dev:ele
pnpm check:console
```

开发服务器代理 `/console/v1` 与 `/openapi.json` 到 `http://127.0.0.1:58080`；可用环境变量 `LOBER_API_TARGET` 指定后端地址。后端运行所需 PostgreSQL 与 YAML 配置见 `cmd/lober`，实际 ISO 来源仍由后端处理。

`check:console` 依次检查生成契约漂移、Vue 类型、ESLint、前端单测和生产构建。仓库根目录的 `make webcheck` 调同一入口；`make check` 独立验证后端六步，保持后端验证不依赖 Node。

## 类型与接口

`src/api/console/generated.ts` 由真实控制台路由和 DTO 的 OpenAPI 生成，不能手改。修改后端接口后执行：

```sh
pnpm generate:console-types
pnpm check:console-types
```

生成器会运行 Go 导出入口 `cmd/console-openapi`，不连接数据库、不需要启动服务。需要可用的 Go 1.27.1 或更新版本；`GO` 可指定可执行文件路径。

前端展示服务端的 `problems`、逐台诊断和字段位置；模板插槽、hook 候选及参数字段使用服务端返回值。用户权限来自 `/user/info`，不调用历史 `/auth/codes` 或刷新令牌接口。

## 交付

```sh
pnpm build:ele
# 回到仓库根目录，再构建含实际前端资源的 Linux 服务
GOOS=linux GOARCH=amd64 go build -o dist/lober ./cmd/lober
```

应用构建关闭 Turbo 结果缓存，确保每次实际生成外部嵌入目录的资源；依赖包仍可复用缓存。Vite 直接输出到 `internal/console/ui/dist/`，服务编译时嵌入这些资源；非 API 页面支持 Vue Router 刷新回退。缺失资源与未知 API 保留 404。构建输出不提交 Git，仓库只带能编译服务的占位页；发布时必须先构建前端再编译 Go。避免将带哈希的实际构建首页单独提交而漏掉配套资源。

第 7 层落地前，任务取消/重试可能返回服务端“执行链尚未就绪”的限制；前端显示该诊断，不模拟装机进度。
