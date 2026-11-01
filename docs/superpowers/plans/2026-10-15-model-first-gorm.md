# Model-first GORM Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 推荐当前会话由主执行者顺序实施；未经用户选择，不启动子代理。

**Goal:** 仓库通过 GORM 模型实例、关联及受限 ORM 扩展访问数据库，消除业务手写 SQL，并补齐 Host 基本字段、独立管理状态和任务关联。

**Architecture:** 保留 core/model → store 契约 → pgstore → Console 的分层。GORM 不足的 JSON/排序/系统目录及迁移能力由官方扩展或独立 ormext 补齐；业务写入先读取并锁定模型、修改实例再保存。保留现有 schema 隔离、JSONB 数据和分页语义，不引入重复查询投影或排序键。

**Tech Stack:** Go 1.27.1，GORM v1.31.2，gorm.io/driver/postgres v1.6.3，PostgreSQL，候选新增 gorm.io/datatypes v1.2.7，现有 Gin/Vue 3/Element Plus。

**Spec:** `docs/superpowers/specs/2026-10-15-model-first-gorm-design.md`

## Global Constraints

- 本次覆盖仓库自有的生产代码、测试和测试夹具。
- 业务代码不使用 Raw/Exec、原生驱动查询、SQL 条件/联表/排序字符串或 gorm.Expr。
- 更新先读取并锁定模型实例，修改字段后显式保存；不使用 map 更新或 Save 的更新失败转插入回退。
- GORM 能力不足时可以使用第三方库或扩展 ORM；不开放任意 SQL 字符串入口，不将整条业务 SQL 原样搬进 helper。
- ORM 扩展必须引用标识符、绑定参数，不能引入第二套原生连接池；业务实体不得泄漏到 ormext。
- Host 管理状态 enabled/disabled 与 Task.State 独立；停用只阻止新任务，不取消已有任务。
- 保留现有 READ COMMITTED、Rev、锁顺序、数据库约束、审计原子性、旧快照渲染及游标语义。
- 不改变现有数据库表/索引/约束名，不为消除表达式增加投影表或排序字段。
- 旧数据升级通过 ORM，支持重入；不能删库重建，不能伪造历史创建信息。
- 不整体升级 Go/GORM/驱动；新增依赖锁定版本并检查模块差异。
- 保留工作区已有的渲染、ISO、测试及文档改动；提交只包含本任务文件。

## Review Focus

1. 特殊属性键、引号、点号、百分号、下划线和反斜杠必须作为参数值；别名、列名和 schema 必须正确引用（任务 1、7）。
2. 同 SN 删除再建、重复启动迁移、部分回填失败不得错认创建者、改变 Rev 或产生升级审计（任务 3）。
3. 并发创建/导入/更新同一主机、交叉停用管理员、短分片重传不得丢写或破坏锁顺序（任务 5）。
4. 模板继续读取提升为固定字段的 ip/hostname；旧快照不重写，nil/空属性仍能区分（任务 2、3、8）。
5. 无任务、只有已结束任务、已有未结束任务、主机停用期间已有任务完成，详情与分页均返回正确状态（任务 6、8、9）。

## 执行准备

- [ ] 按 using-git-worktrees 技能检查已有工件和当前 Git 状态，确定隔离目录；如果需要新 worktree，必须从包含当前必要修改的正确状态开始，不假定原生 create_worktree 会复制未提交内容。先记录已有差异，禁止覆盖其他工作。
- [ ] 记录基线：Go 版本、当前依赖、`go test -count=1 -run '^Test全模块依赖守卫$' ./internal/archtest` 和相关存储契约结果。DSN 只能通过现有 pgtest 规则读取，日志不得输出口令。
- [ ] 阅读 spec、`internal/store/doc.go`、`internal/store/pgstore/doc.go`、`Makefile`；每项任务执行红→绿验证后再提交，遇到偏离约束的能力缺口先修正设计。

## Task 1: 建立受限 GORM 表达式与迁移扩展

**Files:** 新建 `internal/store/pgstore/ormext/{expression.go,migrator.go,expression_test.go,migrator_test.go}`；修改 `go.mod`、`go.sum`、`internal/archtest/deps_test.go`。

**Interfaces:**
- `Collated(column clause.Column) clause.Expression`：固定 C 排序规则。
- `OrderC(columns ...clause.OrderByColumn) clause.OrderBy`；`Prefix(column clause.Column, value string) clause.Expression`。
- `JSONTextEquals(column clause.Column, key, value string) clause.Expression`；`JSONArrayContains(column clause.Column, value string) clause.Expression`。
- `InSubquery(columns []clause.Column, query *gorm.DB) clause.Expression`：包含标量/复合列，不把单个子查询误生成等值比较。
- `MigratorFor(db *gorm.DB) Migrator`，`Migrator.CreateSchema(name string) error`、`DropSchema(name string, cascade bool) error`。内部复用当前 GORM Dialector/连接；只生成通用结构化迁移动作。

- [ ] 编写 `TestExpressionBindsValuesAndQuotesIdentifiers`、`TestJSONNullAndEmptyAndLiteralKeys`、`TestSubqueryScalarAndCompositeMembership`、`TestSchemaIsolationAndCleanup`。断言恶意引号/点号不变成 SQL，JSON 字面键保真，两个并行 schema 互不可见，清理只影响本次 schema。
- [ ] 运行 `go test ./internal/store/pgstore/ormext -count=1`，确认因接口缺失失败。
- [ ] 用 GORM 内置 clause 与 `datatypes v1.2.7` 实现表达式；先核对锁定版本源码并用真实 PostgreSQL 验证 JSON 空值语义。表达式拒绝 clause.Column.Raw 等绕过引用的输入。迁移扩展以结构化动作和 Dialector 引用生成 DDL，不能接收语句模板；扩展外不暴露执行句柄。调整架构依赖规则精确放行 ormext 和该依赖。
- [ ] 同时运行扩展测试及全模块依赖守卫，确认 PASS；检查 go.mod 未升级现有版本，确认新增传递依赖不是新增数据库运行路径。
- [ ] 仅提交本任务文件：`feat(store): add typed GORM PostgreSQL extensions`。

## Task 2: 扩展 Host 领域模型与兼容字段读取

**Files:** 修改 `internal/core/model/{registry.go,registry_test.go,reserved_attrs_test.go,codes.go,codes_align_test.go}`、`internal/core/params/{params.go,params_test.go}`、`internal/core/activation/{template.go,template_test.go}`、`internal/core/snapshot/{build.go,build_test.go}`；新增 `internal/core/model/{host.go,host_test.go}`。

**Interfaces:** `HostStatus`、常量 `HostEnabled="enabled"`/`HostDisabled="disabled"`；Host 新增 `IP, Hostname, MACAddress string`、`Status HostStatus`、`CreatedBy string`、`CreatedAt int64`。增加 `Host.Attribute(name string) string`、`Host.TemplateAttrs() map[string]string`、`ValidHostBindingKey(name string) bool`；持久化自定义属性键校验与模板可读字段校验分开。

- [ ] 编写 `TestHostFieldsAndStatusValidation`、`TestHostBuiltinBindingCompatibility`、`TestSnapshotRetainsPromotedFieldsWithoutChangingOldSchema`：覆盖 IPv4/IPv6/前缀、非合法网络值、MAC 格式、主机名、保留属性冲突、旧 ip/hostname 绑定和快照 map 的深拷贝。
- [ ] 运行 `go test ./internal/core/model ./internal/core/params ./internal/core/activation ./internal/core/snapshot -count=1`，确认新断言失败。
- [ ] 实现基本字段及校验；IP 接受裸地址或带前缀地址且不丢前缀，MAC 规范化为 6 字节小写冒号格式，空网络字段允许；用现有允许的 encoding/hex 验证 MAC，不为此扩大 core 标准库白名单。不增加 MAC/IP 全局唯一约束。统一只读字段解析保留 ip/hostname/mac_address 的历史绑定能力，拒绝其再次作为可编辑 Attrs 键。新快照用 TemplateAttrs 固化兼容值，保留 SnapshotSchema=1 与旧快照格式；创建信息不进入参数绑定。
- [ ] 运行本任务 core 测试和 `go test ./internal/render -count=1` 验证旧快照/渲染；不得重写已有任务快照或无理由更新 golden。
- [ ] 提交 `feat(model): add host identity fields and compatible bindings`。

## Task 3: Host 持久化与可重入升级

**Files:** 修改 `internal/store/{check_registry.go,check_registry_test.go}`、`internal/store/pgstore/{rows_registry.go,hosts.go,open.go,schema.go,contract_hosts_test.go}`；新增 `internal/store/pgstore/{migrate_hosts.go,migrate_hosts_test.go}`。

**Interfaces:** `migrateHosts(ctx context.Context, db *gorm.DB) error`；hostRow 增加与领域模型对应的基本字段及状态、创建信息。创建信息空字符串/0 表示可靠来源缺失的历史未知值；新记录必须从 Audit 赋值。

- [ ] 编写 `TestHostFieldsRoundTripAndCreationProvenance`、`TestHostMigrationIsRepeatable`、`TestHostMigrationAfterDeleteAndRecreate`、`TestHostMigrationRejectsConflictingLegacyAttrs`。断言新建使用 Audit.Actor/At，更新/重导入保留创建信息，历史 CIDR 不丢，重跑不改 Rev/不增审计，同 SN 前次生命周期的创建者不被继承。
- [ ] 运行 `go test ./internal/store/pgstore -run 'TestHost(Fields|Migration)' -count=1` 确认失败。
- [ ] 用模型声明做加法迁移：新增字段先允许历史未知值，分批读取旧模型并修改实例；从当前登记生命周期的审计恢复创建信息。迁移 `ip`、`hostname`、`mac`/`mac_address`，等价 MAC 统一格式，不等价别名冲突报告 SN 并拒绝启动。只移除提升的属性，保留其他属性及 nil/空集合差异。迁移完成后启用状态模型约束并运行完整结构检查。
- [ ] 新建 Status 空值规范化为 enabled；更新/重新导入 Status 未提供时保留当前状态，不能意外重新启用 disabled 主机。服务器创建信息不可由输入覆盖。运行本任务测试与 `Test契约主机`，确认 PASS。
- [ ] 提交 `feat(store): persist and migrate expanded host models`。

## Task 4: 全部业务读取改为模型关联与结构化条件

**Files:** 修改 `internal/store/pgstore/{hosts.go,isos.go,sources.go,hooks.go,templates.go,tasks.go,events.go,audit.go,users.go,references.go,rows_authoring.go}`；按职责新增 `internal/store/pgstore/{query.go,hook_queries.go,template_queries.go,query_test.go}`。

**Interfaces:** `taskReference(tx *gorm.DB, condition clause.Expression) error` 替换 SQL 字符串入参；内部共用条件构造接受 Column/Expression，不接受 SQL。Hook/模板分别以明确模型类型查询，关联字段 Hook/Template 提供身份信息。

- [ ] 编写 `TestStructuredQueryPreservesByteOrderAndCursors`、`TestVersionQueriesSelectLatestAndApprovedWithoutFallback`，补充现有 Hook/模板契约的重名、删除最新草稿、废弃当前版本、复合键、多页、特殊前缀和 JSON 属性筛选断言。
- [ ] 运行 `go test ./internal/store/pgstore -run 'Test(StructuredQuery|VersionQueries)' -count=1`，确认新断言失败。
- [ ] 普通过滤改为模型/map 条件或内置 clause；排序和相关游标比较统一用任务 1 的 C 表达式；JSON 与前缀用任务 1 的 API。最新版本子查询用模型+结构化所属列比较+version 倒序 Limit(1)，不写 SELECT max；连表用模型关联，不拼表名/SELECT 别名。保持分页仍在数据库完成。
- [ ] 运行 `go test ./internal/store/pgstore -run 'Test(契约|StructuredQuery|VersionQueries|ISOSources)' -count=1`，对照查询语义和锁定的表名；只读校验记录 ORM 生成语句不得含敏感参数。
- [ ] 提交 `refactor(store): build reads through typed GORM models`。

## Task 5: 全部写入基于实例并保留并发保证

**Files:** 修改 `internal/store/pgstore/{hosts.go,isos.go,sources.go,hooks.go,templates.go,tasks.go,events.go,users.go,open.go,schema.go,admin_guard_test.go,hostlock_test.go,contract_concurrency_test.go,contract_isosources_test.go}`；新增 `internal/store/pgstore/{rows_guards.go,model_write_test.go}`。

**Interfaces:** `guardRow{Name string}` 模型映射 `store_guards`，固定记录名 `users`；`lockGuard(tx *gorm.DB, name string) error`。更新帮助函数只接受已读取的模型指针和明确字段列表，不接受 map 或更新表达式。

- [ ] 编写 `TestModelUpdatesPersistZeroValuesAndProtectCreationFields`、`TestConcurrentHostImportReadsLatestRevision`、`TestConcurrentRangeRetryKeepsLongestLength`；保留并扩展管理员交叉移除、导入与建任务锁顺序、事件 Seq 和审计失败回滚测试。
- [ ] 运行 `go test ./internal/store/pgstore -run 'Test(ModelUpdates|ConcurrentHostImport|ConcurrentRangeRetry)' -count=1` 确认失败。
- [ ] 所有 Updates/Update 调整为修改已读取实例后显式字段保存；删除依赖已加载实例。ImportHosts 按 SN 顺序对单模型尝试 Create + OnConflict DoNothing，立即根据 RowsAffected 区分创建与已有，再锁定读取并按实际分支更新 Rev；全部在既有事务内，不做 GREATEST/rev+1。SaveRange 在来源锁内读取范围并在 Go 中保留最长值。用户创建和更新统一先锁 users guard，再锁用户记录；保留 READ COMMITTED。自增身份版本和任务事件序号也在已锁定模型上修改。
- [ ] 运行存储层全量测试及相关并发 race；确认 Rev、创建信息、零值和审计原子性符合 spec。
- [ ] 提交 `refactor(store): persist locked model instances consistently`。

## Task 6: Host 任务关联、当前任务与停用准入

**Files:** 修改 `internal/store/pgstore/{rows_registry.go,rows_tasks.go,hosts.go,tasks.go,contract_tasks_test.go}`、`internal/core/admission/{admission.go,admission_test.go}`、`internal/core/model/codes.go`；新增 `internal/store/pgstore/host_tasks_test.go`。

**Interfaces:** hostRow 增加 `Tasks []taskRow` 关联（外键 HostSN 引用 SN），只查询加载，普通 Host 保存 Omit 该关联。复用既有 `Tasks.FindOpenTask` / `Tasks.ListTasks(HostSN)` 查询，不增加任务 ID 列。领域 `admission.HostSelectable` 对 disabled 返回 `host_disabled` 问题。

- [ ] 编写 `TestHostDisableOnlyBlocksNewTasks`、`TestHostTaskAssociationDoesNotCascadeWrites`、`TestHostCurrentTaskSelection`。覆盖停用与建任务并发、已有任务继续推进、没有任务/仅结束任务/存在未结束任务，以及保存主机不修改任务 Rev/状态。
- [ ] 运行 `go test ./internal/core/admission ./internal/store/pgstore -run 'Test(HostDisable|HostTaskAssociation|HostCurrentTask)' -count=1` 确认失败。
- [ ] 准入和向导共用 HostSelectable；仓储在锁住最新 Host 后重跑准入。当前任务选择优先未结束，否则 ListTasks 的第一页第一项；同秒按现有 ID 字节序规则，空为 nil。关联加载不改变 taskRow 中的外键名、历史快照和引用删除保护。
- [ ] 运行任务契约、Host 关联测试与准入测试，确认 PASS。
- [ ] 提交 `feat(store): expose host tasks and independent management state`。

## Task 7: 结构元数据、测试夹具与异常注入统一走 ORM

**Files:** 新增 `internal/store/pgstore/ormext/{catalog.go,catalog_test.go}`；修改 `internal/store/pgstore/{schema.go,schema_test.go,open.go,export_test.go,pgstore_test.go,task_indexes_test.go,contract_hooks_test.go,contract_templates_test.go,contract_tasks_test.go,contract_concurrency_test.go}`、`internal/store/pgstore/pgtest/{pgtest.go,pgtest_test.go}`、`cmd/lober/queue_atomicity_test.go`、`internal/console/wizard_test.go`。

**Interfaces:** `ormext.ReadCatalog(ctx, db, tableNames) (Catalog, error)`，Catalog 内包含索引/主键/约束的结构化描述；`SessionInfo(ctx, db) (Session, error)`、`LockWaits(ctx, db, pid uint32) ([]Wait, error)` 供并发测试。`pgstore.WithPlugins(plugins ...gorm.Plugin) Option` 提供标准插件安装，不暴露任意 SQL；pgtest 内实现测试故障回调插件。

- [ ] 编写 `TestCatalogRetainsFullSchemaAssertions`、`TestCatalogScopesToCurrentSchema`、`TestFixtureFaultRollsBackRealTransaction`；保留所有同名错定义、多余约束、列顺序、外 schema 引用和 schema 并行清理测试。
- [ ] 运行新测试确认旧代码缺少接口而失败。
- [ ] 系统目录映射只读模型，通过 GORM 模型查询和受限函数表达式得到 pg_get_expr/pg_get_indexdef/pg_get_constraintdef 等元数据，替换整条 Raw SELECT；聚合列映射可在 Go 完成，限制为当前 schema 和目标表。测试 DDL 通过 Migrator 和独立测试模型构造。schema 创建/清理用任务 1 扩展；DSN 继续携带 search_path。白盒数据读取/改写、持有事务和等待观测都改用模型。
- [ ] 将审计/队列失败触发器改为仅测试插件回调；确认失败发生于真实事务中的目标写入，保留数据库约束失败覆盖。pgtest 不持有 pgx 连接。运行所有结构、夹具、并发和 queue 原子性测试，确认无丢失覆盖。
- [ ] 提交 `refactor(store): use ORM metadata and integration fixtures`。

## Task 8: Console、CSV 与 OpenAPI 同步 Host 契约

**Files:** 修改 `internal/console/{hosts.go,hosts_test.go,csv.go,csv_test.go,wizard.go,wizard_test.go,openapi_test.go,server.go}`；按需要新增 `internal/console/host_tasks_test.go`，更新 `internal/core/model/golden_test.go` 中明确受新字段影响的固定结构断言。

**Interfaces:** Host 请求增加 `ip`、`hostname`、`macAddress`、可选 `status`；响应增加这些字段及只读 `createdBy/createdAt`。详情 `currentTask` 仅有 `id/state/createdAt`，无任务为 null；`GET /console/v1/hosts/:sn/tasks` 返回既有 Task DTO 的分页结果，权限沿用任务读取规则，同时验证 Host 可读。

- [ ] 编写 HTTP 测试验证创建者取登录身份、不能覆盖创建信息、status 缺省不会启用已停用主机、currentTask 选择和任务分页，CSV 预演不写库；测试旧模板仍能通过 ip/hostname 绑定得到值。
- [ ] 运行新测试确认失败。
- [ ] 同步 DTO/显式转换/路由/OpenAPI；CSV 固定列新增 ip、hostname、mac_address、status，创建信息不接受 CSV 写入，任务不可通过 Host 请求写入。普通列表不包含全量 Tasks；字段排序与序列化不依赖位置式 struct 字面量。HTTP Host 详情通过现有 Tasks 依赖聚合只读摘要。
- [ ] 运行 `go test ./internal/console ./internal/builtin ./cmd/lober -count=1`，确认权限、原子导入、向导与初始化没有回归。
- [ ] 提交 `feat(console): expose expanded hosts and task history`。

## Task 9: 前端 Host 类型和页面对齐后端

**Files:** 修改 `web/apps/web-ele/src/api/console/host.ts`、`web/apps/web-ele/src/views/hosts/index.vue`、`web/apps/web-ele/src/components/host-select.vue`；按现有项目方式新增 `web/apps/web-ele/src/api/console/host.test.ts`。

**Interfaces:** 类型明确匹配 `/console/v1/hosts` 的 cursor page 和 SN 主键，不继续沿用遗留 id/uuid/offset 协议；提供按 SN 查询详情和分页任务 API，Arch 使用后端 aarch64/x86_64，固定字段名称与任务 DTO 一致。

- [ ] 编写 API 契约测试：查询参数使用 prefix/cursor/limit；分页响应 items/next，主机详情 currentTask 可空，创建 payload 不包含 createdBy/createdAt/task IDs。在 web 目录用现有 Vitest 运行 `pnpm exec vitest run apps/web-ele/src/api/console/host.test.ts` 并确认失败；复用 web/vitest.config.ts，不引入第二套测试框架。
- [ ] 调整 API 类型和页面模型，展示 IP/Hostname/MAC、管理状态、创建信息及分页任务列表；管理状态和任务状态分别显示。主机编辑不暴露服务端只读字段。主机选择器使用新分页契约，不因仅加载第一页丢失候选。
- [ ] 在 web 目录运行 `pnpm --filter @vben/web-ele typecheck` 和 `pnpm --filter @vben/web-ele build`；通过现有浏览器/组件工具检查无任务、已停用、任务失败、历史多页和无写权限场景。
- [ ] 提交 `feat(web): align host inventory with model-based API`。

## Task 10: 收紧门禁、全量验收和交付

**Files:** 新增 `internal/archtest/{orm_rules_test.go,orm_test.go}`；修改 `internal/archtest/deps_test.go`、`internal/store/pgstore/doc.go`、`internal/store/doc.go`；新增 `docs/superpowers/reports/2026-10-15-model-first-gorm.md`。

**Interfaces:** `checkORMFile(fset *token.FileSet, pkg string, f *ast.File, info *types.Info) []violation`，针对实际 GORM/驱动方法类型检查；将 ORM 文件规则接入既有全模块守卫，并检查测试和夹具。ormext 的表达式构造及迁移执行单独受限，不给其他包豁免。

- [ ] 编写门禁反例：导入别名 Raw/Exec、SQL 字符串变量/常量、条件字符串、map Updates、原生驱动查询、业务自定义 SQL builder；正例为模型、关联名、列名选择、第三方 JSON 表达式、pgconn 错误解码和受限 ormext。`go test ./internal/archtest -run 'TestORM' -count=1` 应先失败。
- [ ] 实现类型检查规则，必要时复用现有 golang.org/x/tools/go/packages 加载类型；检查扩展公开签名，不允许 ExecSQL(string)、Raw(string) 或任意 SQL 模板。移除已迁移的旧业务入口，确认类型信息未把模型字段选择误判为 SQL。
- [ ] 运行格式检查、`go vet ./...`、`go test -count=1 ./internal/archtest`、`go test -count=1 ./...`、`go test -race -timeout=20m ./...`、Linux amd64 交叉构建及现有 Wire diff。Windows 下设置进程级 GOOS/GOARCH 后恢复原值，不覆盖用户系统变量；可用 Git Bash 执行 Makefile 既有目标。
- [ ] 运行前端验证和真实 PostgreSQL 旧数据升级演练；记录实际命令、退出码、数据库版本、残余风险和能力清单。不得在报告中暴露 DSN/密码，不得把未执行的检查写为通过。
- [ ] 审查所有变更和扩展：业务没有手写 SQL；更新确实修改实例；未新增无必要的投影/排序列；约束、并发、旧快照、权限和已存在的工作区修改均保留。按所选执行方式完成最终代码审查，再记录交付结果。
- [ ] 提交 `test(store): enforce model-based database access`；不自行合并或发布。

## 计划自审记录

- spec 的模型写入、Host 扩展、ORM 表达式、元数据、夹具、迁移、并发、前端和门禁分别由任务 1–10 承接。
- Review Focus 的五项均有上述具体测试断言，旧数据生命周期、特殊 JSON 键和未结束任务优先级明确。
- 第三方 JSON 功能先验证锁定版本，不把 MySQL/SQLite 的能力直接当作 PostgreSQL 可用。
- ORM 扩展内部生成 SQL 是用户明确允许的边界；它不授权业务使用 Raw/Exec，也不改变模型实例写入要求。
- 所有步骤均为待执行；本计划中的测试与构建结果不是已经获得的验证证据。
