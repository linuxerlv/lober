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

生产构建输出到仓库根 `internal/console/ui/dist/`，这是忽略的交付制品。当前 Go 服务提供 API，不负责静态页面；用同源服务器托管完整 dist 并转发 API。SPA 路由需要回退到 `index.html`。Nginx 示例：

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
- “权限管理 → 角色与菜单”仅 admin 可修改五个角色的可见菜单。只能选择该角色已有读取权限的菜单；设置菜单不会增加 API 写入或审批权限。admin 的权限管理入口必须保留。
- 从未配置的角色使用默认可见菜单；保存空列表表示没有业务菜单，进入“暂无可访问菜单”。个人设置和退出仍可使用。
- 当前用户信息在导航、窗口重新聚焦及每 30 秒不重叠检查时刷新。角色或菜单变更后旧路由、标签和缓存被移除；直接输入隐藏页面 URL 会返回可访问首页。
- 页面按当前权限隐藏操作按钮，提交前再次检查；后端每个请求仍执行认证和授权。viewer 只有读取入口；作者可提交，审批人可审核，禁止自我审批等状态限制保留。
- 表单使用 rev 防并发覆盖。409 时保留编辑，应重新读取并核对后保存。网络错误明确提示，登出失败可重试。

## 当前接口与能力边界

认证使用 `/console/v1/auth/login` 的 `{token,expiresAt,user}`；当前资料 `/user/info`；本人资料 `PUT /user/profile`；本人改密 `POST /auth/password`。权限来自当前用户的 `permissions`，不调用不存在的 `/auth/codes` 或 refresh 接口。

业务视图使用当前分页 `{items,next}`、主机键 `sn`、ISO `/isos`、脚本/模板版本路径及 rev。报表页提供真实审计查询和 CSV 导出。独立报表统计、磁盘 dry-run 及第 7 层真实任务控制尚未提供；页面说明边界，后端错误不会被当作成功。
