# Lober 本地模拟服务

这个应用沿用 Vben 的 Nitro 文件路由。它模拟 Lober 的 `/console/v1` 接口，用于预览装机任务和制品。任务与事件保存在内存中，重启服务会恢复初始数据。

从 `web/` 安装依赖，然后在第一个终端启动模拟服务。

```powershell
pnpm install --frozen-lockfile
pnpm --filter @vben/backend-mock start --host 127.0.0.1 --port 5778
```

在第二个终端从 `web/` 启动前端。

```powershell
$env:VITE_CONSOLE_PROXY_TARGET='http://127.0.0.1:5778'
pnpm --filter @vben/web-ele dev --host 127.0.0.1 --port 5779 --strictPort
```

打开 [装机任务](http://127.0.0.1:5779/tasks)，用本地模拟账号 `operator` 和密码 `lober-preview` 登录。

权限管理可用 `admin` 登录。还可用 `viewer`、`author` 和 `approver` 查看不同角色的菜单，初始密码均为 `lober-preview`。账号和菜单改动保存在内存中。

- 在“任务状态”中选“失败”并查询，查看失败原因和日志。
- 在主机清单打开 `LAB-SRV-002`，从“最近装机任务”进入 `TSK-1047`。点“定位日志”可跳到第 24 条事件，再用“重试任务”查看状态更新。
- 点击“创建任务”，选择一台可用的 x86_64 主机，再选 Rocky Linux 和标准装机模板。
- 把系统磁盘改为 `bad`，校验时会显示字段错误。改为 `/dev/sdb` 后可以预览文件并创建任务。
- 在“制品管理 → 系统镜像”查看可用、获取中和失败的镜像。点“查看详情”可读到来源状态、进度和失败原因。模板与脚本也提供只读示例。
