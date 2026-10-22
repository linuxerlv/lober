# Lober 控制台

源码在 `apps/web-ele`，使用 Vue 3、Vben 和 Element Plus；依赖由 `pnpm-lock.yaml` 锁定，上游来源见 `UPSTREAM_PIN.md`。

## 运行和构建

在 `web` 目录执行：

```sh
pnpm install --frozen-lockfile
pnpm --filter @vben/web-ele dev
pnpm exec vitest run --config apps/web-ele/vitest.config.ts
pnpm --filter @vben/web-ele typecheck
pnpm --filter @vben/web-ele build
```

开发服务通过同源 `/console` 和 `/openapi.json` 转发 Go API，默认目标为 `http://127.0.0.1:8080`。API 在其他地址时，在启动 Vite 的进程环境中设置 `VITE_CONSOLE_PROXY_TARGET`。例如 PowerShell：

```powershell
$env:VITE_CONSOLE_PROXY_TARGET='http://192.168.137.21:18080'
pnpm --filter @vben/web-ele dev
```

生产构建输出到仓库根 `internal/console/ui/dist/`。先构建前端，再构建 Go 程序，`console.WithFrontend` 将真实制品嵌入二进制并提供 SPA 回退。仓库中的占位文件仅保证未构建时 Go 可编译，不是发布页面。也可用同源服务器托管完整 dist 并转发 API；Nginx 示例：

```nginx
server {
    listen 8088;
    root /opt/lober/ui;
    location /console/ {
        proxy_pass http://127.0.0.1:18080;
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_read_timeout 1h;
        client_max_body_size 8m;
    }
    location = /openapi.json {
        proxy_pass http://127.0.0.1:18080;
    }
    location / { try_files $uri $uri/ /index.html; }
}
```

在有依赖的开发环境构建，将 dist、Go 程序及配置作为发布包带到内网。图标、Logo、默认头像使用本地资源；个人头像为内嵌 PNG/JPEG。发布包不包含账号口令或数据库连接串。

## 账号和权限

- 初始 admin 登录后必须先修改密码；成功后所有旧会话失效，需要重新登录。所有账号可从头像下拉的“个人设置”修改自己的显示名、邮箱、头像、简介，并进入修改密码页。邮箱仅为联系资料。
- “权限管理 → 账号管理”仅 admin 可访问。支持创建账号，给账号分配 viewer、operator、hook-author、hook-approver、admin 中的一个角色，重置密码，禁用/启用账号。后端禁止禁用本人及停用/降级最后一个管理员。
- “权限管理 → 角色与菜单”仅 admin 可修改五个角色的可见菜单。使用 Element Plus 树展示一级目录和二级页面，可独立勾选二级页面，父目录支持全选与半选。只能选择该角色已有读取权限的页面；设置菜单不会增加 API 写入或审批权限。admin 的“角色与菜单”页面必须保留，“账号管理”可单独分配。
- 菜单目录通过 `parentId` 描述层级，角色配置的 `menuIds` 只保存二级页面 ID；父目录不作为授权项持久化，至少有一个可见子菜单时才显示。原有业务菜单 ID 保持兼容，新增 `users` 对应账号管理；已有显式配置不会自动获得新增页面，管理员可在树中勾选保存。
- 用户名、显示名称与角色分别展示；头像菜单中的角色使用独立标签，默认水印只显示用户名。
- 从未配置的角色使用默认可见菜单；保存空列表表示没有业务菜单，进入“暂无可访问菜单”。个人设置和退出仍可使用。
- 当前用户信息在导航、窗口重新聚焦及每 30 秒不重叠检查时刷新。角色或菜单变更后旧路由、标签和缓存被移除；直接输入隐藏页面 URL 会返回可访问首页。
- 页面按当前权限隐藏操作按钮，提交前再次检查；后端每个请求仍执行认证和授权。viewer 只有读取入口；作者可提交，审批人可审核，禁止自我审批等状态限制保留。
- 表单使用 rev 防并发覆盖。409 时保留编辑，应重新读取并核对后保存。网络错误明确提示，登出失败可重试。

## 当前接口与能力边界

认证使用 `/console/v1/auth/login` 的 `{token,expiresAt,user}`；当前资料 `/user/info`；本人资料 `PUT /user/profile`；本人改密 `POST /auth/password`。权限来自当前用户的 `permissions`，不调用不存在的 `/auth/codes` 或 refresh 接口。

业务视图使用主机键 `sn`、ISO `/isos`、脚本/模板版本路径及 rev。报表页提供真实审计查询和 CSV 导出。独立报表统计、磁盘 dry-run 及第 7 层真实任务控制尚未提供；页面说明边界，后端错误不会被当作成功。

### 数据列表分页

主机、任务、制品集、hook、模板、审计和账号列表使用统一的 Element Plus 分页条，默认每页 20 条，可选 10/20/50/100，支持总数、页码和跳页。查询和修改每页条数回到第一页；刷新、保存保留当前页，删除末页最后一条后自动回到有效页。查询框内容在点击“查询”时提交，翻页使用已提交的条件。请求失败保留原有行和页码。

HTTP 列表传入 `page` 或 `page_size` 即使用页码模式，缺省值为 1/20，返回 `{items,next,total}`，`total` 为数据库过滤后的匹配总数。`page` 必须为正整数，`page_size` 范围为 1..500，不能与 `cursor`、`limit` 混用。未传页码参数的旧游标调用继续兼容；审计 CSV 导出仍包含全部匹配记录。

前后端须使用同一份整合代码构建。不要将新版 `dist` 复制到缺少分页实现的旧后端目录后单独编译；旧后端会忽略页码且不返回 `total`，前端因此报 `Numbered pagination requires a valid total`。部署后应携带登录会话检查七类列表 `?page=1&page_size=1` 的 `total` 为非负整数、记录数不超过 1，再将发布判为成功。需要并行发布其他修复时，应先整合源代码并验证，而不是相互覆盖服务二进制。

创建任务向导中的主机、ISO、模板和 hook 候选也按服务端页码加载，主机支持跨页勾选。任务事件维持增量流及去重协议，对已经接收的事件分页显示；分页总数表示已接收事件数量。
