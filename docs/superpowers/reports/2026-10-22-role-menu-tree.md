# 角色与二级菜单配置验证

日期：2026-10-22（Asia/Shanghai）

本地开发工作区：`E:/src/lober/.worktree/rbac-integration`，分支 `dev`，基线 `f5c8ee9`。该工作区包含截图对应的权限管理功能；主工作目录的旧分支尚无此页面。

## 交付行为

- 角色选择使用 Element Plus `ElSelect`，菜单配置使用 `ElTree`，按侧边栏展示一级目录与二级页面，支持父节点批量勾选与半选。
- 菜单目录通过 `parentId` 描述层级。角色设置只存二级页面 ID，目录不作为授权项保存。后端拒绝未知、重复、越权和目录 ID；修订号冲突保留原有保护。
- 二级页面独立过滤路由；没有可访问子页面的目录消失，输入隐藏页面 URL 也不能进入。账号管理新增独立 ID `users`，不再与 `role-menus` 共用授权。
- 管理员的 `role-menus` 入口必留；其他角色可保存空菜单。旧业务菜单 ID 保持兼容，已有显式配置不会自动获得新增的 `users` 页面。
- 账号管理使用 Element Plus 表格与表单，用户名、显示名称、角色分别展示。头像菜单用独立角色徽标，默认水印只显示用户名。

## 云端验证

按用户要求，本地修改后同步到云端执行检查。云端独立目录为 `/home/dev/role-menu-tree-20261022-qzeMd7`。后端测试使用专用测试库及每例隔离 schema；浏览器使用独立 schema `qa_menu_tree_20261022` 和仅监听回环地址的临时实例，未替换现有产品服务。

验证结束后临时实例已停止，现有 `lober.service` 仍为 active。已核对本地与云端菜单页面、用户布局和后端菜单目录的 SHA-256 一致。

| 检查 | 结果 |
| --- | --- |
| `pnpm test:console` | 26 个文件、94 项测试通过 |
| `pnpm --filter @vben/web-ele typecheck` | 通过 |
| `pnpm lint:console`，收尾文件补检 | 通过 |
| `pnpm check:console-types` | 通过，OpenAPI 类型由生成器更新 |
| `pnpm build:ele` | 通过 |
| `go test ./internal/console ./internal/store/pgstore -count=1` | 两个包均通过，包含真实 PostgreSQL 保存与读取 |
| `go vet ./internal/console` | 通过 |
| `go build ./cmd/lober` | 通过，独立实例运行实际嵌入前端 |
| Chromium 浏览器联调 | 6 个场景通过，页面运行错误为 0 |
| 只读代码审查 | 无实质问题 |

浏览器场景覆盖：用户名称与角色独立展示、二级树及必留入口、同级页面独立保存与刷新回读、隐藏 URL 拦截、不同角色的新会话权限，以及深色主题选择器。收尾角色徽标调整后重新构建并复验浏览器。

本地脱敏日志与截图：`E:/src/lober/.tmp/role-menu-tree-evidence/`。云端日志位于上述独立目录的 `evidence/`。私密环境说明、连接串和测试账号口令不包含在代码归档或本报告中。

## 后续调整：创建与编辑账号模态框

按用户截图反馈，将列表下方的行内表单移入 Element Plus `ElDialog`。创建和编辑共用弹窗；保存成功后关闭并刷新列表，失败在弹窗内提示并保留输入。取消、关闭或 Escape 会清空密码草稿；提交期间禁用保存、取消、关闭和 Escape，避免重复请求及误关。

本次仍为本地开发、云端测试。云端全量前端测试增加为 26 个文件、96 项，全部通过；ESLint、Vue 类型检查、生产构建和嵌入前端的服务构建通过。Chromium 的 4 个真实联调场景通过，覆盖必填校验、取消/关闭清空密码、提交期间禁止关闭及重复请求、真实创建与编辑后刷新列表，页面运行错误为 0。未修改后端代码。

截图为本地证据目录内的 `account-create-dialog.png`，相关日志以 `account-dialog-` 开头。独立临时实例在验证后停止，现有产品服务仍为 active，未替换其版本。
