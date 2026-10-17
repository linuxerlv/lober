# 当前重写版企业测试

本目录面向 dev 重写版；外层旧 lobertest 的客户端和部署 fixture 尚未整体迁移。
这里使用 Python 标准库 unittest，显式收集本目录，避免旧 pytest 的 needs_vm/slow
默认过滤或自动部署。不要用外层 `uv run pytest` 的全绿替代本目录结果。

所有执行和被测服务均在指定测试 VM 上。产品需要 Linux、真实 PostgreSQL、psql、
pg_dump；浏览器用锁文件对应 Playwright 和 Linux Chromium headless shell。
fixturegen.sh 复用 isotest.Image 生成明确标注的合成介质，经过真实上传/识别/导入，
用于接口集成，不证明任何真实发行版可安装。

运行前从 test 分支冻结源码并构建含真实前端资源的二进制。以下环境变量必须显式设置：

- LOBER_TEST_PG_DSN：能创建/删除独立测试 schema 的连接串，通过受限文件加载。
- LOBER_ENTERPRISE_BINARY：本轮候选二进制绝对路径。
- LOBER_ENTERPRISE_FIXTURE：fixturegen.sh 输出的合成 ISO 路径。
- LOBER_ENTERPRISE_NODE、LOBER_ENTERPRISE_UI_SCRIPT、LOBER_ENTERPRISE_PLAYWRIGHT：VM 上的 Node、ui.mjs 及 Playwright index.mjs 路径。

在仓库根运行（替换结果目录与完整 SHA；不得将秘密写入命令历史）：

```bash
bash tests/lobertest/enterprise/fixturegen.sh /private/run/synthetic-rocky.iso
python3 tests/lobertest/enterprise/run.py gate --output /private/run/logs --product-sha PRODUCT_SHA --test-sha TEST_SHA > /private/run/logs/gate.log 2>&1
python3 tests/lobertest/enterprise/run.py control --output /private/run/logs --product-sha PRODUCT_SHA --test-sha TEST_SHA > /private/run/logs/control.log 2>&1
python3 tests/lobertest/enterprise/gate.py --profile control-smoke --results /private/run/logs --product-sha PRODUCT_SHA
python3 tests/lobertest/enterprise/gate.py --profile release --results /private/run/logs --product-sha PRODUCT_SHA
```

每条命令独立记录退出码；目录预先创建。backend/frontend 结果须由相应实际门禁退出码和
用例/包数量生成，证据为同目录原始日志。run.py 自己记录当前套件结果、零收集、失败和 skip。
release profile 缺少真实介质、PXE/状态机、真实安装、容量长稳或受支持升级证据时返回非零。
control-smoke 通过只代表该明确子集通过；它不是计划中的完整控制面或发布验收。

控制面覆盖五角色真实 HTTP、认证与权限撤销、CSV 原子性、CAS、并发唯一性、模板审批、
真实导入合成 ISO 后批量建任务、快照固化、浏览器登录/路由权限/深链接、服务进程重启、
以及独立 schema 加配套制品/来源目录的备份恢复。测试账号口令每轮随机生成，不使用 VM
登录口令；运行配置为 0600，结束删除，原始运行目录为 0700，不提交秘密和数据库备份。
