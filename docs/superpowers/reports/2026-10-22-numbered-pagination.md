# 数据列表分页交付记录

开发目录：`E:/src/lober/.worktree/rbac-integration`（现有 dev 工作区）。所有源代码修改在本地完成，同步归档到云端后执行测试。保留此前角色二级菜单树、用户名和角色分开展示、账号创建/编辑弹窗的修改。

## 已实现

- 主机、任务、制品集、hook、模板、审计、账号管理及旧用户入口使用统一 `ListPagination`（Element Plus `ElPagination`），默认 20 条，可选 10/20/50/100，显示真实总数、页码与跳页。
- 七个后端主列表支持 `page/page_size`，页码模式使用数据库过滤后 COUNT + 稳定排序 LIMIT/OFFSET。拒绝零/负数、非法字符串、重复参数、偏移溢出及游标参数混用。保留旧 `cursor/limit` 与全量审计 CSV 导出；不需要数据库迁移。
- `NumberedPage` 统一处理过期响应、保留失败前的数据/页码、每页条数变化和末页删除后的自动回退。翻页使用已经提交的筛选条件。
- 创建任务向导的主机、ISO、模板及 hook 候选也使用服务端分页。主机跨页勾选并恢复可见勾选状态；ISO 和模板选择名称跨页保留。hook 候选按具体版本分页。
- 模板候选在 SQL 中应用当前审批版本、内置版本清单、引用 hook 状态和 ISO offer 集合条件后计数，不在 HTTP 层取全列表切片。
- 任务事件保留增量读取、重放去重和流连接，按已接收事件的总数分页展示。
- 七个主列表表格使用自然高度和 520px 最大高度，更多行在表格内部滚动。真实浏览器截图复核曾发现 Vben `h-full` 包装与 Vxe `height: auto` 的尺寸反馈导致容器膨胀到 6579px，已去掉这组依赖；分页条不会被不断撑远。

## 云端验证

主集成目录：`/home/dev/role-menu-tree-20261022-qzeMd7`，独立 QA schema 与监听地址 `127.0.0.1:18089`。私有连接与账号配置仅留在云端受限文件中，未写入源代码归档。

- 前端完整测试：30 个文件，116 项通过（`evidence/pagination-ui-tests.log`）。
- 后端完整测试：`go test ./internal/store/... ./internal/console -count=1` 通过，pgstore 91.278s，console 239.107s。日志：`/home/dev/pagination-backend-20261022-FB8t9G/backend-selection-full.log`。
- `go vet ./internal/store/... ./internal/console` 通过；分页及候选资格专项测试分别验证了实现前失败、实现后通过。
- 公共分页、主列表、向导与事件列表回归覆盖跨页勾选、失败恢复、过期响应、筛选总数、稳定排序、空页和末页回退。
- 两次只读代码审查未发现需要修复的问题，包含 SQL 候选筛选与业务判定的一致性。

- `pnpm lint:console`、`pnpm check:console-types`、`pnpm --filter @vben/web-ele typecheck` 均通过。事件测试适配器的三个 prop 默认值警告已改为 required 后同步云端，无剩余警告。
- `pnpm build:ele` 通过（11 个构建任务成功），`go build -o evidence/lober-pagination ./cmd/lober` 通过，真实前端已嵌入独立 QA 程序。

- 表格高度修正后，前端 116 项完整测试、完整 lint、类型检查、接口生成一致性、生产构建及 QA 二进制构建再次通过。
- 真实浏览器 12 个场景通过，页面 JS 错误数 0：七个主列表的页码请求与总数；主机页替换/每页条数/已提交筛选；空结果；删除末页回退；账号弹窗保存后保留第二页；真实 Vxe 向导跨页勾选、返回原页恢复勾选及提交两台主机。
- 高度回归检查通过：1500×980 窗口中，主机第二页每页 10 条的首行 y=300.39、分页条 y=707.39，外层没有额外滚动膨胀。截图已人工查看。日志 `evidence/pagination-browser.log`、`pagination-visual.log`。
- 本地与云端 72 个代码文件 SHA-256 一致，日志 `evidence/pagination-finish.log`。测试结束后已停止独立 QA 进程，现有 `lober.service` 仍为 active；此次只同步和运行测试，未替换现有运行服务。

截图：

- [主机列表第二页](assets/pagination/pagination-hosts.png)
- [账号列表第二页保存后](assets/pagination/pagination-users.png)
- [创建任务跨页选择](assets/pagination/pagination-wizard.png)

云端浏览器脚本：`evidence/pagination-browser.mjs`、`pagination-visual.mjs`。本地日志副本：`E:/src/lober/.tmp/pagination-evidence/`。Git 状态以 dev 分支提交记录为准。

## 后续运行服务更新

用户随后明确要求更新云端运行服务。已将验证过的构建替换至 `/home/dev/bin/lober` 并重启 `lober.service`，配置保持原样。更新前保存旧二进制、配置和 PostgreSQL 自定义格式备份：`/home/dev/bin/backups/lober-20261022T013620Z/`。

运行二进制 SHA-256：`3ffe428795f19462af2c1e6fd905605582cd822ec6fdefc7e0bf1f38ab69858f`。公网 `http://110.42.44.11:8080/` 返回 HTTP 200，首页 2825 字节、包含真实前端模块且没有占位提示，11 个静态资源通过。现有管理员账号登录成功，主机/任务/制品集/hook/模板/审计/账号七类真实列表 `page=1&page_size=10` 均返回有效总数和不超过 10 条数据。部署记录已保存到本地日志副本的 `deployment.json`。

## 后续版本错配修复

用户报告 `Numbered pagination requires a valid total` 后，检查发现运行服务已被替换为 `f54c8d355ae75f9de0a2f97c8c74912767eb33994c24b31386bccb357452d11a`。该构建使用装机修复分支 `c935d0c` 的旧控制台后端和新前端：七类列表均忽略 `page/page_size`，响应缺少 `total`，例如审计请求每页 10 条却返回 100 条。这是部署来源错配，不能通过给前端缺失总数补零来掩盖。

已在当前独立 dev 工作区合入 `c935d0c` 相对 `f5c8ee9` 的 `cmd/internal` 修复，保留正在运行的 Debian/AlmaLinux/AutoYaST 修复及分页/菜单改动。其他聊天正在编辑的 ISO/hook 参数规则工作区未修改。合并后仓储、console、builtin、execution、machine、cmd/lober 测试均通过；装配测试的 kickstart 固定版本检查更新为当前发行的 v3，原生脚本测试按约定配置 Bash/BusyBox。只读整合审查未发现问题。

重新构建并部署的程序 SHA-256 为 `9e49c49d69397dcc49a65dabaf1cceff3b8f02fabc5be35147f87ad6f5476d3c`。更新前备份在 `/home/dev/bin/backups/lober-20261022T040608Z/`。部署脚本新增旧程序哈希复核、七类列表和三类向导分页契约检查，失败自动恢复旧程序；十项接口检查通过。

实际 8080 服务浏览器只读检查通过：主机、任务、制品集、hook、模板、报表/审计、账号七页均显示正确分页总数，不再有该提示，页面脚本错误为 0，未修改业务数据。云端日志 `evidence/pagination-live-readonly.log`、`pagination-installer-combined-tests.log`、`pagination-installer-followup-tests.log`；本地副本仍在上述日志目录。
