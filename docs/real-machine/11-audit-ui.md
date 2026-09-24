# 11 审计查询 UI：把「只在驳回当次响应里可见」的理由接上事后查询入口

> **数据层迁移说明**：本文记录的是数据层仍为 SQLite 时的情况，数据层已于
> 2026-09-24 迁移到 GORM + PostgreSQL（见
> `docs/superpowers/specs/2026-09-24-gorm-postgres-migration-design.md`），
> 正文保留当时原貌，未作修改。

> 本文档记录本轮变更：hook 页面接上端点 29（`GET /hooks/{id}/audit`），
> 让驳回理由与完整操作时间线随时可查，不再要求查看者必须在驳回发生的
> 那一刻在场。
> 依据：`docs/contracts/http-console.md`「4. hook」节端点 29 与「变更
> 记录 2」；`docs/real-machine/05-审计持久化.md`「UI 侧还需要做什么」；
> SPEC §9.4。

## 缺口回顾：为什么这一层 UI 缺口是真实的，不是「体验不够好」

hook 以 root 身份在 `pre` 阶段（分区之前）运行，能力等同「擦掉这台机器
所有盘」（SPEC §9.4）。审批人驳回一个有问题的 hook 时必须填理由，否则
作者不知道哪里出了问题——这是契约变更 1（`POST /hooks/{id}/reject`）
已经保证的。但契约变更 1 自己也记录了一层遗留缺口：理由只在驳回那一次
的 HTTP 响应体里可见，`internal/console/audit.go` 的 `AuditLogger` 当
时只写不读，作者若当时不在场（异步审批场景下大概率如此），这条理由就
永久丢失，只能靠人工翻 journald 原始文本日志。

契约变更 2（`docs/contracts/http-console.md` 文首「变更记录 2」）已经
在后端补上了查询路径：`contract.AuditStore`、`GET /hooks/{id}/audit`
（端点 29）、SQLite 落库实现，五条 conformance 断言在两个 Store 实现上
都绿。但补后端不等于缺口消失——`docs/real-machine/05-审计持久化.md`
自己在末尾写明「本轮明确不碰 web/**，UI 侧还需要做什么留给后续」。本轮
要做的就是接上这最后一段：**没有 UI 入口，后端能力等于不存在**，作者
仍然没有地方点进去查。

## 做了什么

三个改动点，全部落在本轮任务允许的四个文件里：

1. **`web/apps/web-ele/src/api/console/audit.ts`**（新建）：
   `getHookAuditApi(id, params?)`，对应端点 29，字段名与
   `contract.AuditRecord` 逐一对应（`ts`/`actor`/`action`/`target`/
   `detail`），照 `hook.ts` 已有的注释风格与命名习惯写（`XxxApi` 命名、
   契约端点编号引用）。
2. **`web/apps/web-ele/src/components/audit-timeline.vue`**（新建）：
   接收 `hookId` prop，自行拉取并展示 `ElTimeline`（与
   `views/tasks/detail.vue` 的「状态机时间线」面板同一视觉语言：
   `:timestamp` + `:type`），暴露 `reload()` 方法供父组件在需要时手动
   刷新。三种非正常状态分别处理，不合并成一句「加载失败」：
   - 加载中：文字提示。
   - 403（`code === 1003`，对应 `apierr.CodeForbidden`）：识别为「无
     权限查看」，展示服务端给出的具体中文说明。
   - 其它失败：展示服务端 `message`，仍不是空泛的「失败」。
   - 空数组：`ElEmpty` 明确提示「暂无审计记录」——一个从未被驳回过的
     hook 没有审计记录是正常状态，不是错误，契约文档已经这样约定。
3. **`web/apps/web-ele/src/views/hooks/index.vue`**（只加审计相关
   UI，未改既有审批流转逻辑）：
   - 操作列加一个「审计」按钮，点开一个只读的 `AuditModal`，内嵌
     `<AuditTimeline :hook-id="row.id" />`。
   - 按钮的显示与否用 `useAccessStore().accessCodes` 判定是否持有
     `hook:write` 或 `hook:approve`——与后端 403 判定的权限码字面值
     完全一致（`internal/console/rbac.go` 的 `codeHookWrite`/
     `codeHookApprove`）。这只是体验层的收敛（不该让无权限的人点进去
     才看到报错），真正的权限判定仍在服务端，前端隐藏入口不能替代
     后端校验。
   - 修正了驳回成功提示里一句现在已经是假话的文案：本轮变更之前那句
     话说「系统没有为作者提供事后查询驳回理由的入口，请通过线下沟通
     告知」——端点 29 上线后这句话不再成立，必须更新，否则会误导
     操作者以为理由真的会永久丢失。新文案改为「理由已写入审计记录，
     可通过审计入口自行查询」，同时**没有**改成暗示「会自动通知
     作者」——那是这个端点做不到的事（没有推送能力），本轮任务的
     要点明确要求不能给这个假象。

## 权限：为什么前端要做「双重判断」而不是只依赖后端 403

`handleHookAudit`（`internal/console/hook.go`）的权限判定是
「`hook:write` 或 `hook:approve` 之一即可」的 OR 语义，与
`handleApproveHook`/`handleRejectHook` 一致，不是路由层单一权限码能
表达的 AND 语义，因此判定放在 handler 内部。前端复刻同一个 OR 判断只是
为了不让无权限角色点进一个注定会 403 的入口——这是纯体验层的优化，
删掉它系统仍然安全（后端会兜底拒绝），但留着能避免用户白跑一趟弄不清
「为什么点了没反应/报错」。

如果后端权限码字面值将来变化（例如拆分出更细的审计专属权限码），这里
的前端判断需要同步更新，否则会出现「按钮显示但点开必 403」或「按钮
隐藏但其实有权限」的体验退化——但这不是安全缺口，只是体验退化，真正
的权限边界始终由服务端把关。

## 已知限制：驳回理由不会被自动推送给作者

这是本轮任务要点明确要求写清楚的边界，重复一遍以防遗漏：`GET
/hooks/{id}/audit` 是**拉取式查询**，不是推送。作者要看到驳回理由，
必须自己打开这个 hook 的「审计」面板去看——系统不会主动通知、不会发
邮件、不会有未读红点。UI 侧的驳回成功提示已经如实说明了这一点（「此
理由不会自动通知作者，系统没有推送能力」），没有暗示任何形式的自动
触达。如果未来需要主动通知，那是一个新的、独立的功能（消息推送/
webhook），不是本轮变更的范围，也不应该被误读成本轮已经做了。

## 验证

- `pnpm check:type`：通过（`vue-tsc --noEmit --skipLibCheck` 无错误）。
- `pnpm lint`：本轮新增/修改的三个文件本身无新增错误（修了一个
  `unicorn/catch-error-name` 提示后清零）；仓库里另有 15 个 pre-existing
  错误（`pnpm-workspace.yaml` 未使用的 catalog 条目）与 16 个
  pre-existing 警告（`packages/**/__tests__` 下的 `vue/one-component-
  per-file`），核对过 `git diff dev -- web/pnpm-workspace.yaml` 无差异，
  确认是本轮变更之前就存在、且落在本任务不允许触碰的文件范围之外的
  基线噪音，不是本次引入的回归。
- `pnpm build:ele`：成功，产物落进 `internal/console/ui/dist`
  （17.23s，213 个带 hash 文件）。**验证后已手动把该目录还原成
  `git show HEAD` 记录的两个占位文件原样**——产物按约定不入库，只是
  构建验证会临时覆盖占位文件，验证完必须还原，否则会把别的并行轨
  依赖的占位文件状态弄脏。
- 气隙 grep：构建产物里 `hm.baidu.com` 零命中；`unpkg.com`、
  `api.iconify.design` 各 2 处命中，与任务描述的既有基线（1-2 处，
  框架默认值里的死字符串）一致，未新增。
- `go test ./internal/console/ui/ -count=1`：全绿，确认 embed 在
  产物覆盖前后都能正常工作（占位文件与真实构建产物都能被
  `findUnderscorePrefixedFile` 正确处理）。
