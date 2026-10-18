# 当前重写版企业测试

本目录面向 dev 重写版；外层旧 lobertest 的客户端和部署 fixture 尚未整体迁移。
这里使用 Python 标准库 unittest，显式收集本目录，避免旧 pytest 的 needs_vm/slow
默认过滤或自动部署。不要用外层 `uv run pytest` 的全绿替代本目录结果。

release 同时要求 Go 漏洞扫描与前端依赖审计结果。前端审计使用
`pnpm audit --prod --json --registry=https://registry.npmjs.org`；镜像站不支持审计
接口时必须记录失败，不能当作零漏洞。workspace 的生产依赖分类也包含构建工具，
告警需另行确认实际交付范围和可利用性。

所有执行和被测服务均在指定测试 VM 上。产品需要 Linux、真实 PostgreSQL、psql、
pg_dump；浏览器用锁文件对应 Playwright 和 Linux Chromium headless shell。
fixturegen.sh 复用 isotest.Image 生成明确标注的合成介质，经过真实上传/识别/导入，
用于接口集成，不证明任何真实发行版可安装。

运行前从 test 分支冻结源码并构建含真实前端资源的二进制。以下环境变量必须显式设置：

- LOBER_TEST_PG_DSN：能创建/删除独立测试 schema 的连接串，通过受限文件加载。
- LOBER_ENTERPRISE_BINARY：本轮候选二进制绝对路径。
- LOBER_ENTERPRISE_FIXTURE：fixturegen.sh 输出的合成 ISO 路径。
- LOBER_ENTERPRISE_NODE、LOBER_ENTERPRISE_UI_SCRIPT、LOBER_ENTERPRISE_PLAYWRIGHT：VM 上的 Node、ui.mjs 及 Playwright index.mjs 路径。
- LOBER_ENTERPRISE_VM_IP：指定测试 VM 自身的 IPv4；recovery 的来源服务只绑定该地址的临时端口。

在仓库根运行（替换结果目录与完整 SHA；不得将秘密写入命令历史）：

```bash
bash tests/lobertest/enterprise/fixturegen.sh /private/run/synthetic-rocky.iso
python3 tests/lobertest/enterprise/run.py gate --output /private/run/logs --product-sha PRODUCT_SHA --test-sha TEST_SHA --manifest /private/run/candidate.json > /private/run/logs/gate.log 2>&1
python3 tests/lobertest/enterprise/run.py control --output /private/run/logs --product-sha PRODUCT_SHA --test-sha TEST_SHA --manifest /private/run/candidate.json > /private/run/logs/control.log 2>&1
python3 tests/lobertest/enterprise/run.py flows --output /private/run/logs --product-sha PRODUCT_SHA --test-sha TEST_SHA --manifest /private/run/candidate.json > /private/run/logs/flows.log 2>&1
python3 tests/lobertest/enterprise/run.py events --output /private/run/logs --product-sha PRODUCT_SHA --test-sha TEST_SHA --manifest /private/run/candidate.json > /private/run/logs/events.log 2>&1
python3 tests/lobertest/enterprise/run.py recovery --output /private/run/logs --product-sha PRODUCT_SHA --test-sha TEST_SHA --manifest /private/run/candidate.json > /private/run/logs/recovery.log 2>&1
python3 tests/lobertest/enterprise/gate.py --profile control-smoke --results /private/run/logs --product-sha PRODUCT_SHA --manifest /private/run/candidate.json
python3 tests/lobertest/enterprise/gate.py --profile control-complete --results /private/run/logs --product-sha PRODUCT_SHA --manifest /private/run/candidate.json
python3 tests/lobertest/enterprise/gate.py --profile release --results /private/run/logs --product-sha PRODUCT_SHA --manifest /private/run/candidate.json
```

每条命令独立记录退出码；目录预先创建。backend/frontend 结果须由相应实际门禁退出码和
用例/包数量生成，证据为同目录原始日志。run.py 自己记录当前套件结果、零收集、失败和 skip。
每轮使用新的 run_id。candidate.json 由独立构建步骤冻结，包含 product_sha、test_sha、
run_id、binary（VM 上的路径）、binary_sha256、source_archive、source_archive_sha256，
以及 files（从已提交 Git archive 得出的逐文件 SHA256 映射）。运行器验证实际二进制、
归档和解压源码；开始时先把旧结果原子替换成 RUNNING。所有结果还必须匹配 run_id、
manifest_sha256，以及实际日志的 evidence_sha256，故中断、换包或日志被覆盖无法复用旧 PASS。
release profile 缺少真实介质、PXE/状态机、真实安装、容量长稳或受支持升级证据时返回非零。
control-smoke 通过只代表该明确子集通过；它不是计划中的完整控制面或发布验收。
control-complete 在 smoke 基础上要求真实浏览器业务、事件传输和控制面故障恢复三套结果。
名称只指 profiles.json 列出的集合；导入发布/删除的精确崩溃时刻、全部安全/容量/升级场景仍需单独资格证据。

flows 使用当前构建的真实表单、独立登录会话和后端；旧请求隔离只延迟实际服务响应，不伪造业务响应。
events 的 eventgen.sh 仅在 enterprise 随机 schema 中向 pending 的 ENTERPRISE 主机任务追加 test.transport
探针事件，验证存储及 SSE/浏览器回放，不改变任务状态，不代表机器安装信号或真实执行链。
浏览器还会延迟真实 REST 事件响应，等待同一 seq 经 SSE 显示后再放行 REST，验证两通道重叠不增加重复行。
recovery 需要免密 sudo mount/umount/iptables；使用独立 8 MiB tmpfs、本实例 DB 代理和自身随机来源端口。
TCP 超时用只匹配该来源地址/端口的 OUTPUT DROP 规则制造；finally 删除原规则，不改共享服务或默认策略。
该超时依赖真实内核连接超时，单例预算 170 秒；来源失败后按实际契约显式 retry，崩溃中的 fetching 则重启续传。
不得用 HTTP 408 响应代替 TCP 超时验收。临时配置为 0600，不应复制到公共报告。

控制面覆盖五角色真实 HTTP、认证与权限撤销、CSV 原子性、CAS、并发唯一性、模板审批、
真实导入合成 ISO 后批量建任务、快照固化、浏览器登录/路由权限/深链接、服务进程重启、
以及独立 schema 加配套制品/来源目录的备份恢复。测试账号口令每轮随机生成，不使用 VM
登录口令；运行配置为 0600，结束删除，原始运行目录为 0700，不提交秘密和数据库备份。
