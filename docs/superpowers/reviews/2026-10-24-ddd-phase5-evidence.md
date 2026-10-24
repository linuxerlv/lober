# 第五阶段：身份权限与审计验证证据

第五阶段可交付。七项任务独立审查、整分支终审与唯一补强的局部复审全部通过。指针方法守卫缺口已关闭，仅保留既有日志噪声后续项。最新代码对应与审查结论见[最终交付记录](2026-10-24-ddd-phase5-final-review.md)。

本阶段起点为已验收的 `dev` 提交 `9c457799133f64a268f0c3607233feaaa78bc863`。模块分支为 `codex/ddd-identity-audit`，工作区为 `E:/src/lober/.worktree/ddd-identity-audit`。未合并、推送或推进后续环境。

## 实现与审查范围

| 任务 | 实现提交 | 本项结果 |
| --- | --- | --- |
| 1，兼容契约与删除清单 | `2db3aab` | [报告](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-1-report.md)、[独立审查](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-1-review.md)已关闭 |
| 2，凭据、会话与认证 | `3bdfeeb` | [报告](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-2-report.md)、[独立审查](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-2-review.md)已关闭 |
| 3，账号、本人改密与资料 | `1eabf35` | [报告](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-3-report.md)、[独立审查](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-3-review.md)已关闭 |
| 4，角色菜单与启动初始化 | `3b4634f` | [报告](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-4-report.md)、[独立审查](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-4-review.md)已关闭 |
| 5，一次接通身份与任务接缝 | `7c6baae` | [报告](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-5-report.md)、[独立审查](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-5-review.md)已关闭；第七项另补用户分页迁移遗漏 |
| 6，审计查询与完整导出 | `4457db6` | [报告](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-6-report.md)、[独立审查](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-6-review.md)已关闭 |
| 7，精确边界与完整验收 | `f65b48c` | [报告](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-7-report.md)、[独立审查](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task-7-review.md)已关闭 |
| 整分支审查 | `9c45779..f65b48c` | [独立终审](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/final-review.md)通过；两项轻微问题按补强复审关闭或递延 |
| 唯一补强与局部复审 | `f65b48c..7a5704d` | [补强报告](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/final-fix-report.md)、[局部复审](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/final-rereview.md)通过；指针漏检已关闭，日志噪声明确递延 |

身份生产入口使用同一 Registry/Bootstrapper 实例。会话表与凭据协调锁留在应用内；密码和令牌实现独立。审计 Reader 只读，导出完整缓冲后才交给网页。共同 AuditIntent 只有四个普通字段，任务控制只作类型转换后委托原实现。

第七项新增类型检查跟踪真实对象，覆盖包别名、方法值、点导入、本地小接口、匿名字段与新文件；身份入口、审计入口和组装许可均精确列出。公共方法签名、身份私有字段、普通结果字段、窄存储能力也有固定检查。既有全包发现、排除文件、类型装载失败和前四阶段反例保持。

## 本项失败与修复记录

| 轮次 | 结果与用途 | 安全日志 |
| --- | --- | --- |
| `task7-red` | 退出1。测试装载真实依赖时漏取包名，32项在类型检查前失败；不作为这些边界的行为证据 | [日志](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-red.safe.log) |
| `task7-red2` | 退出1。35项全部先通过真实 Go 类型检查；19个漏检点得到 `allowed=false findings=[]`，16项既有拒绝或正例通过 | [有效失败证据](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-red2.safe.log) |
| `task7-green` | 35项类型例全部通过；全模块发现用户列表仍经旧分页存储检查，另误报原机器签名随机源；退出1 | [原始全模块失败](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-green.safe.log) |
| `task7-green2` | 临时给用户分页转发加许可后通过；控制器否决此做法，许可已删除。不能把此轮当成最终修复 | [保留过程日志](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-green2.safe.log) |
| `task7-focused` | 正式修复后退出0。49项类型例、公共契约及全模块通过；用户数字分页与游标分页两个测试通过。11个顶层、66个子测试明确通过 | [局部验证](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-focused.safe.log) |
| `task7-format` | VM 格式、Wire diff、最终边界/契约检查退出0。49项类型例；9个顶层、69个子测试通过。只生成一个格式变化文件，已取回后冻结 | [格式与边界验证](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-format.safe.log) |
| `task7-final` | 首轮原完整命令退出2。普通43包结果为34通过、8无测试文件、archtest失败1包，缓存0。旧身份正例仍允许本地GetUser；竞态及后续步骤未开始 | [首轮完整日志](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-final.safe.log) |
| `task7-stale-replay` | 不改失败样例，点名原测试重跑；同一个旧身份正例失败，8个其他子项通过，退出1 | [原样复现](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-stale-replay.safe.log) |
| `task7-stale-fix` | 原GetUser正文保留为明确拒绝反例，另加真实Registry正常调用。格式无变化，9个顶层、79个子测试通过，退出0 | [样例同步后的验证](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-stale-fix.safe.log) |

控制器批准的唯一产品改动是 `console/users.go` 的 `listUsers` 改用 `parseQueryPagination`。网页只解析数字，`identity.Service.ListUsers` 继续执行原分页规则；未修改测试断言、39项剩余许可、错误映射或业务规则。

机器签名密钥仍只在 `cmd/lober/machine.go / machineSigningKey / crypto/rand.Reader` 使用原随机源。这个文件字节未变；新增正例验证原位置，反例验证其他函数、其他文件和其他随机符号都不能借用。

首轮完整验收在UTC `2026-10-24T05:47:00Z` 至 `05:54:37Z` 运行457秒，树为 `4d60a3f29f7bae5dd43360d66364c07701a8f221`。唯一失败为 `Test镜像新网页文件不能调用内部能力/后续身份接口正常调用`：第四阶段预留的“后续身份”正例声明了本地 `reader.GetUser`，第五阶段完成后必须拒绝。控制器批准仅同步该样例，新增正例通过真实仓库类型检查后调用 Registry.Authenticate/CurrentUser，保留原直接调用和方法值拒绝。原154项历史区域至文件尾逐字不变，没有削弱新守卫。

各轮完整命令、目录树、UTC起止时间、退出码、缓存、明确跳过与无匹配统计见[逐轮证明](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task7-iteration-proofs.json)。各轮详细日志无明确跳过、无匹配或缓存结果；这不推断未详细输出部分的实际跳过总数。

## 精确清理与历史接续

| 清单 | 起点 | 删除 | 剩余 |
| --- | ---: | ---: | ---: |
| migrationExceptions | 64 | 37 | 27 |
| mediaMigrationExceptions | 0 | 0 | 0 |
| consoleRuleMigrationExceptions | 42 | 31 | 11 |
| notificationMigrationExceptions | 1 | 0 | 1 |
| 合计 | 107 | 68 | 39 |

删除集合为第五阶段35项存储、30项规则，以及已批准的三个第六阶段接缝。剩余39项逐字段、逐顺序与起点相同，均为第六阶段；零新增、零改阶段、零换名挪表。`check-transitions.py --final` 和 Go 精确摘要测试均验证该集合。

历史重建使用当前39项加不可执行的68项历史记录，还原上轮107项，再加原154项还原261项。原154条记录、原四个摘要和六处随机编号许可均保留；历史清单不能放行已经删除的调用。[保护性证明](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task7-preservation-proof.json)记录逐字检查结果。

## 最终冻结与环境

最终测试树为 `839257c56c3280f2002db454e949b19c0cb8a0b7`，基于 `4457db6da0d09c22974e9266befb47d9193bcf82` 加本项源码。完整候选逐路径归档上传，VM重建同一Git树后才执行；未把其他工作区内容纳入。

最终开始前再次执行 `vm.py prepare`：[环境记录](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task7-final2-environment.txt)。主机 `lober`；无竞争Go作业；真实数据库 `lober_test` 非恢复状态；可用磁盘28GiB、内存2111MiB；Go 1.27.1 linux/amd64、Node 24.16.0、pnpm 11.16.0。数据库连接材料保持忽略并为600权限，未打印凭据。

所有 Go、格式、Wire、Node、构建及测试仅在归属目录 `/home/dev/lober-ddd-phase5-identity-audit-20261024` 运行。一次一个作业，上传和拉取均等待完成。固定 `GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off`，保留原 Bash/Busybox 路径、密码强度、测试准备缓存和20分钟竞态超时。

最终作业直接使用首次执行前已保存的[原命令文件](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/final-acceptance-original.commands)，字节摘要为 `948265128bd7802fd6b367ce6f272ef5492739459b0cca49bfb86e68d0cbf9f9`。上传快照中的命令与原文件字节一致，未删除包或改变筛选：

```sh
make check
pnpm --dir web check:console-types
go test ./internal/application/identity ./internal/application/audit ./internal/application/execution ./internal/domain/access ./internal/domain/audit ./internal/infrastructure/credentials ./internal/interfaces/console ./internal/infrastructure/persistence/pgstore ./cmd/lober -run 'Test.*(身份|登录|口令|改密|会话|用户|资料|菜单|审计|管理员|凭据|SSE|控制委托|装配初始)' -v -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
git diff --check
git diff --exit-code
```

最终重跑 `task7-final2` 退出0，UTC `2026-10-24T05:58:14Z` 至 `2026-10-24T06:25:40Z`，共1646秒。执行树 `839257c56c3280f2002db454e949b19c0cb8a0b7`，VM生成差异和未跟踪文件均为空。

| 阶段 | 包结果数 | 通过 | 无测试文件 | 缓存结果 |
| --- | ---: | ---: | ---: | ---: |
| 强制全模块发现 | 1 | 1 | 0 | 0 |
| 普通测试 | 43 | 35 | 8 | 30 |
| 竞态测试 | 43 | 35 | 8 | 0 |
| 九包点名重跑 | 9 | 9 | 0 | 0 |

普通测试中的缓存复用了首轮已执行的未改动包；不将缓存称为重新执行。全模块强制发现始终带`-count=1`，原增量缓存反例也在完整archtest中运行。最终九包点名命令带`-v -count=1`，实际命中如下：

| 包 | 明确通过的顶层测试 | 包运行时间 |
| --- | ---: | --- |
| `internal/application/identity` | 29 | 44.205s |
| `internal/application/audit` | 4 | 0.008s |
| `internal/application/execution` | 2 | 0.507s |
| `internal/domain/access` | 5 | 0.005s |
| `internal/domain/audit` | 2 | 0.006s |
| `internal/infrastructure/credentials` | 3 | 3.395s |
| `internal/interfaces/console` | 53 | 105.123s |
| `internal/infrastructure/persistence/pgstore` | 8 | 17.261s |
| `cmd/lober` | 5 | 11.998s |

点名阶段明确通过111个顶层测试、260个子测试。明确跳过0项：[]；无匹配记录0项：[]。非详细普通/竞态输出中的隐式跳过总数未知，不写成0。

原`make check`的格式、vet、全模块普通测试、竞态测试、Linux amd64构建和Wire diff全部完成；网页契约类型检查、九包点名测试、最后一次Wire diff与两次Git差异检查均成功。点名输出明确覆盖真实数据库、十三入口及五项重点风险。

完整安全输出见[最终日志](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/vm-logs/task7-final2.safe.log)，逐包时间、缓存和全部点名测试见[最终统计](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task7-final2-proof.json)，原命令/树见[冻结快照](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task7-final2.snapshot.json)。


## 十三入口证据对应

下表列出最终点名命令覆盖的真实行为测试。共同入口描述、请求/响应字段、路由权限和强制改密矩阵由 `Test身份十三入口契约与强制改密矩阵` 固定。证据定位为包名加测试名，不把一次200响应视为全部兼容证明。

| 入口 | 应用委托 | 重点测试（console包，另注明者除外） |
| --- | --- | --- |
| POST `/console/v1/auth/login` | Access.Login | `Test登录失败说明一致与请求前提`、`Test并发改密不能让旧口令登录补发会话` |
| POST `/console/v1/auth/logout` | Access.Logout | `Test登录退出与到期走真实HTTP`、`Test会话期限与退出登录` |
| POST `/console/v1/auth/password` | Accounts.ChangePassword | `Test当前用户改密及全部会话撤销`、`Test改密审计失败不改变凭据与会话` |
| GET `/console/v1/user/info` | Access.CurrentUser | `Test角色菜单修改进入当前会话`；identity包 `Test身份快照归属与菜单副本` |
| PUT `/console/v1/user/profile` | Accounts.UpdateProfile | `Test个人资料仅更新本人且不接受权限字段`、`Test资料已提交后菜单读取失败保留事实` |
| GET `/console/v1/users` | Accounts.ListUsers | `Test用户列表不泄露口令并能按游标翻页`、`Test用户响应不泄露口令并保留字段`；本项另点名数字分页测试 |
| POST `/console/v1/users` | Accounts.CreateUser | `Test用户HTTP创建修改禁用恢复与审计`、`Test用户管理全部路由权限矩阵` |
| PUT `/console/v1/users/:username` | Accounts.UpdateUser | `Test用户HTTP拒绝坏请求陈旧版本与自己禁用`、`Test并发本人改密不能覆盖管理员重置` |
| DELETE `/console/v1/users/:username` | Accounts.DisableUser | `Test用户HTTP创建修改禁用恢复与审计`；pgstore包 `Test最后启用管理员不能禁用或降级` |
| GET `/console/v1/roles/menus` | Menus.ListRoleMenus | `Test菜单目录与五角色默认配置`、`Test角色菜单HTTP权限与验证` |
| PUT `/console/v1/roles/:role/menus` | Menus.SaveRoleMenus | `Test二级菜单独立存储且目录不能授权`；pgstore包 `Test角色菜单并发首次保存与更新`、`Test角色菜单审计失败回滚` |
| GET `/console/v1/audit` | Reader.List | `Test审计接口五角色权限`、`Test审计旧游标不倒灌并发新增及精确筛选` |
| GET `/console/v1/audit/export` | Reader.ExportCSV | `Test审计分页过滤完整CSV及公式作为文本`、`Test审计导出后页失败不发送残缺下载` |

## 五项重点检查对应

| 重点 | 验证证据与实际边界 |
| --- | --- |
| 旧密码读取与重置/本人改密交错 | identity与console包同名并发改密测试用通道控制时序；`Test身份发布会话全程持有凭据读锁`；`Test身份写入审计失败不撤销会话`。成功写入后撤销，失败保留原会话 |
| 慢资料、禁用/降级与最后管理员 | identity与console包 `Test慢资料请求不能用猜测新修订号恢复旧权限或凭据`；pgstore包 `Test并发管理员移除必须保留一位`；identity包跨实例管理员移除测试。保留原版本，数据库保证至少一位启用管理员 |
| 默认、显式空、必需页面和旧版本 | access包 `Test菜单默认显式空必需页面与副本`；identity包 `Test菜单配置不提升接口权限`、`Test身份菜单首次竞争及审计回滚`；pgstore首次/更新并发测试。页面可见性不扩大接口权限 |
| 长连接退出、到期、禁用、强制改密 | console包 `Test任务SSE连接期间复查禁用改密及会话过期`、`Test任务SSE退出与到期只检查原令牌`、`Test查询令牌只允许SSE且Authorization优先`。实际网络连接继续检查原令牌与最新账号 |
| 超500条导出、晚失败、取消、公式 | console真实501条和完整字段/顺序验证；audit应用后页失败/取消返回nil字节；domain/audit公式前缀测试。失败不提前发下载头，原文不裁剪，成功读取全部页 |

启动初始化另由cmd/lober `Test装配初始管理员强制改密及内置内容`、`Test身份接线共用会话与凭据协调`验证；任务桥由execution `Test审计意图桥接不改变任务控制`及console任务控制委托测试验证。此前75条正式路由字段保留证明见[第五项路由证明](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task5-route-proof.json)。

## 交付边界

第七项提交 `f65b48c` 与当时被测树的精确差异只含本证据、第五阶段计划和总路线三份文档；当时所有执行代码、测试和生成文件相同。最新补强与交付源码关系见文末。提交前后分别用 `git diff --name-only 839257c56c3280f2002db454e949b19c0cb8a0b7 <提交树或HEAD>` 检查；详见[提交与被测树证明](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/task7-commit-proof.json)。纯文档收尾不重复绿灯套件。工作区、模块分支、原日志、失败证据和用户文件全部保留。

七项独立审查、整分支审查和补强复审均已关闭。未覆盖实物镜像、实机安装、企业测试套件、浏览器人工验收、完整前端构建或合并后的集成测试；不声称这些范围通过。测试日志仍含既有Gin调试输出与预期故障注入消息，未为减少日志修改测试准备或断言。

## 终审后的公共契约守卫补强

上文两轮VM验收对应 `f65b48c29fcb838ece864765d3e54011b56adf17` 候选。其后整分支终审通过并记录两项轻微问题；以下为唯一补强。针对本次差异的局部复审也已通过：指针方法漏检已关闭；既有日志噪声单独递延，没有新增问题。

- `Principal` 与 `AuditIntent` 的公开方法现在同时检查值类型和指针类型。`Principal.Actor` 仍必须是值方法。真实 Go 类型检查通过的两个“仅指针新增公开方法”反例，先在旧守卫下失败，再在补强后通过。没有修改产品代码或原字段、签名及历史清单检查。
- `finalfix-acceptance` 在 lober 虚拟机对冻结树 `136b8f1db89960218f5a66798c88d0ed2a0f5946` 原样运行完整验收命令。命令文件摘要 `948265128bd7802fd6b367ce6f272ef5492739459b0cca49bfb86e68d0cbf9f9`，UTC `2026-10-24T06:49:15Z–06:59:19Z`，退出 0，虚拟机没有生成差异或未跟踪文件。强制发现通过；普通及竞态测试各 35 包通过、8 包无测试文件，其中各 30 包使用缓存。九包点名重跑 9 包通过，明确通过 111 个顶层和 260 个子测试；明确跳过、失败、无匹配均为 0，简略输出中的隐式跳过总数未知。
- 终审第二项是既有 Gin 调试及预期故障日志过多，继续单独递延。此次没有修改日志、测试准备缓存、密码强度、竞态超时或断言。

本轮红灯、绿灯、安全日志、原命令验收统计和源码树核对见[补强报告](../../../.superpowers/sdd/2026-10-24-backend-ddd-phase5-identity-audit/final-fix-report.md)。本段写入后的纯文档差异以该报告的提交证明为准；不为纯文档收尾重复套件。

## 最终交付状态

七项任务、整分支终审、唯一补强局部复审均已关闭，没有严重或重要问题。十三入口和五项重点逐项结论、全部实施判断及后续范围见[最终交付记录](2026-10-24-ddd-phase5-final-review.md)。仅保留既有调试/预期故障日志噪声一项非阻断后续整理事项。

最新通过树为 `136b8f1db89960218f5a66798c88d0ed2a0f5946`，实现补强提交为 `7a5704d`。交付文档收尾相对该被测树只包含本证据、第五阶段计划、总路线、最终交付记录四份Markdown；其他所有路径逐一相同。原始日志和工作区保留，不为文档变化重复测试。
