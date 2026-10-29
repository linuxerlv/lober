# Minimal 安装盘支持实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** AlmaLinux、Rocky Linux 官方 minimal 安装盘通过正常导入和真实装机。

**Architecture:** 领域层纯解析真实分组；应用层读取并形成明确计划；文件实现消费计划并复核。复用既有仓库根和必需路径字段，不增加公开接口或数据库字段。

**Tech Stack:** Go、现有 PostgreSQL 测试夹具、原生安装器、浏览器页面、云端客户机。

**Spec:** `docs/superpowers/specs/2026-10-29-rhel-minimal-media-design.md`。用户已明确批准并要求直接实施，沿用分任务、独立审查和 oc2 委派方式。

## Global Constraints

- 只在 `codex/rhel-minimal-media` 改动；根代理独占 Git，实施者独占本计划产品文件。
- 所有测试在 lober VM；唯一远端执行者统一持原锁。其他代理不得自行操作 VM 或云宿主。
- 当前候选与已完成 Debian、Ubuntu 客户保留；新来源、测试和部署目录不覆盖旧证据。
- 只支持明确的 Alma/Rocky Minimal 布局。完整盘要求、安全路径、来源复核及发布所有权不放宽。
- 不下载完整 DVD 做研究，不伪造目录、ready 或完成回调，不关闭签名检查。

## Review Focus

- 文件名和注释含 minimal 不产生能力；必须有一致的真实分组声明。
- full 缺 AppStream，即使存在 Minimal 目录也失败。
- Alma 来源为10.2但盘内版本10，不能按文件名改元数据。
- 危险路径、冲突/重复声明、空或缺失元数据不被清洗后接受。
- Minimal 的仓库根含 `.treeinfo`，不能误指向缺少安装树元数据的子目录。

## Task 1：先证明现有正常导入缺少此能力

**Files:** 新建 `internal/application/media/import_minimal_test.go` 和 `internal/application/media/testdata/{almalinux-10-minimal,rocky-10.2-minimal}.treeinfo`。

- [ ] 原样保存两份真实小元数据，记录公开来源和摘要；整张 minimal 镜像验签后再核实际正文一致。
- [ ] 写 `TestImportMinimalKickstartRepository`，复用 `importSetup/importUpload/isotest.Image`。两系分别建立内核、initrd、stage2、Minimal仓库和包的小夹具；不得加入伪造的BaseOS/AppStream。
- [ ] 断言真实 `Sources.Import` 成功、状态ready、保留原版本10/10.2、四角色齐全；repo指向带原`.treeinfo`和Minimal元数据的安装树，stage2仍正确。
- [ ] 唯一执行者使用原安全测试库，在 VM 跑 `go test -count=1 ./internal/application/media -run '^TestImportMinimalKickstartRepository$'`，保存实际失败。缺测试环境或编译失败不当作预期失败。
- [ ] 收到有效失败证据前，不写产品实现。

## Task 2：最小实现与保护性回归

**Files:** 新建 `internal/domain/media/kickstart_repository.go`、`kickstart_repository_test.go`、`internal/application/media/kickstart_repository.go`；修改 `internal/application/media/import.go`、`internal/infrastructure/media/extract.go`、应用回归及 `docs/contracts/media.md`。

**Interfaces:** 领域新增 `RepositoryLayout{Root string; RepoPaths []string}` 和 `MinimalKickstartRepository(series catalog.Series, treeinfo string) (RepositoryLayout, bool, error)`。布尔值表示明确匹配Minimal；未匹配沿用原完整盘计划。应用新增 `planKickstartRepository(context.Context, port.MediaView, port.ExtractionPlan) (port.ExtractionPlan, error)`。

- [ ] 领域纯解析分组，要求Minimal清单、分组ID、repository、packages及身份/架构事实一致；冲突重复、混合或危险声明返回现有 `iso_repo_incomplete` 问题，不新增错误文字协议。
- [ ] 应用只为两系kickstart通过现有有界Text读取，调用领域规则；匹配时设置`Identified.RepoRoot="."`和`RepoPaths={"Minimal/repodata/repomd.xml"}`。同一个明确计划传给BeginPrepare和Prepare。
- [ ] 文件层优先使用明确RepoRoot并复核安全；只有未指定布局才沿原`importRepoRoot`。保留原复制、链接、非空元数据、Verify/Publish及rollback规则。
- [ ] 增加纯规则表测试和真实导入负例：文件名/注释不算声明，完整盘缺AppStream拒绝，Minimal仓库缺/空拒绝，重复冲突/混合声明/路径穿越拒绝；保留原嵌套完整盘成功、原来源变化和发布冲突回归。
- [ ] VM 跑原红测试、领域包和应用/文件实现包焦点测试，必须实际通过；再跑原 `make check`，不得过滤或改时限。
- [ ] 独立审查源码、原命令、隔离数据库与原始结果。根代理核实后提交产品修订。

## Task 3：真实介质、部署和两系装机

- [ ] 官方/中科大固定来源，可信公钥完整指纹与清单签名通过，整ISO大小和摘要通过；再核真实treeinfo与夹具相同。
- [ ] 从真实提交构建新程序，保原前端和数据库。若为真实日志入口补充现有测试配置字段，单列准确差异和新摘要，保留原配置；不改主服务或用户HTTPS。新实例记录、精确切换及启动窗回退，旧实例不可覆盖。独审后使用新基准。
- [ ] 每系各建云端专属客户和新磁盘，实际CPU满足v3要求；先配置真实日志入口，再正常页面导入、预览和一次创建任务。
- [ ] Minimal包列表只用已提供能力。受控串口登录固定主机公钥，严格SSH及真实管理员权限核验全部设计要求；缺少客户管理助手单列未验证，不能跳过业务检查。
- [ ] 每系一次正常重启：启动编号变化，持久系统与任务事件不变，不能重新安装；再页面刷新和重登录。
- [ ] 每系完整实际链独立审查。失败保留，修订后新实例/新任务区分，不能把原失败改为通过。
- [ ] 根依仓库流程合入dev再test；合并源与被测源一致，继续真实验收。未完成两系前不扩大结论。

## Task 4：修正原生 Kickstart 仓库地址

首轮真实失败记录见设计的 2026-10-30 补充。领域渲染中的 Kickstart repo 系统值去掉末尾分隔符，保留仓库根和原静态文件安全约束。

- [ ] 实施者仅先添加真实渲染回归，使用带末尾斜杠的真实目录角色；验证原生答案的仓库地址、stage2 和其他格式不受影响。由唯一执行者在 VM 取得断言失败，不能把编译错误当失败。
- [ ] 原实施者做最小实现，更新相关渲染契约和必要的预期内容；不改媒体分类、发布结构或静态路由。
- [ ] VM 焦点检查通过，独立核对失败与修后原件及源码。

## Task 5：接收原生 TCP 安装日志

保持原地址及 UDP 接收，增加同地址的 TCP 换行日志。连接数、消息大小、空闲时间及取消清理须有明确上限。网络实现归基础设施，cmd 只负责装配；沿用原来源租约和应用收录入口。

- [ ] 实施者先补经过现有服务入口的真实 TCP 回归，绑定正常凭证流后发送日志；在 VM 取得连接拒绝或事件缺失的真实失败。保留 UDP 用例，不用不存在的新接口制造编译失败。
- [ ] 最小实现 TCP 传输与启动/取消清理；无租约、过期、终态、断开和超长消息不绕过原归属及边界。更新执行契约和必要配置说明，不新增配置字段。
- [ ] VM 焦点检查及原 UDP 回归通过；两项修正合并只跑一次原完整 `make check`，独立审查后根提交。
- [ ] 从修后真实提交部署新候选，精确旧实例切换与回退。保留旧失败；按新基准重新进行 Task 3 Alma→Rocky 真实安装、日志、严格登录、管理员检查、一次正常重启及页面验收。

## 执行分工

实施者负责上述Go、测试夹具及契约文档，可调用oc2完成明确的小任务；不运行本地测试，不操作Git。唯一远端执行者负责下载验签、VM检查及云端客户；独立审者只读源码和真实证据；根代理负责范围、验收、Git与用户沟通。
