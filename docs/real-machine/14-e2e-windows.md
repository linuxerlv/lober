# Windows 家族端到端验证报告

> **数据层迁移说明**：本文记录的是数据层仍为 SQLite 时的情况，数据层已于
> 2026-09-24 迁移到 GORM + PostgreSQL（见
> `docs/superpowers/specs/2026-09-24-gorm-postgres-migration-design.md`），
> 正文保留当时原貌，未作修改。

验证方式：`scripts/e2e-windows.sh`（照 `scripts/e2e.sh` RHEL 基线的九阶段
结构编写，本报告只是把该脚本的运行结果与发现整理成可读文档，不重复脚本内
已有的详细注释）。本轮**不是真机 PXE 测试**（不同于
`docs/real-machine/03-PXE-引导实测.md` 那种 QEMU/真机验证），任务范围是
"分派刚接上，代码从没跑到过"这一层——用 curl 模拟机器，验证
`internal/render/{unattend,winpe}.go` 这两个此前只有单元测试覆盖、从未被
任何非测试代码路径调用过的文件，接入分派后端到端跑起来是什么样。

## 一句话结论

**Windows 分派实际上没有接上。** `internal/render/menu.go` 与
`internal/render/renderer.go` 的两个分派 switch 仍然把
`model.OSFamilyWindows` 与 `model.OSFamilySUSE`、`model.OSFamilyESXi` 分在
同一个"未实现"分支，从未调用 `winpe.go` 的 `RenderWindowsMenu` 或
`unattend.go` 的 `RenderUnattend`——这两个函数各自的单元测试与 golden file
全部通过，但在真实的 HTTP 请求路径上一行都没跑到过。本轮任务说明预告的
"分派刚接上"与仓库当前代码状态不符，本报告的价值正是把这个不一致用可复现
的脚本固定下来。

更值得记录的是**这个缺口造成的失败模式不是崩溃或明显错误，而是静默的资源
浪费**：`/v1/boot` 端点有"始终返回 HTTP 200"的硬约束
（`internal/gateway/boot.go` 第 47-49 行注释），`StartBoot`
（消耗 attempt_count、签发装机 token、把任务状态 CAS 到 `booting`）先于
`RenderMenu` 调用完成并提交，`RenderMenu` 报错后网关直接回退到与"完全陌生
的机器"逐字符相同的 `sanboot` 载荷。机器侧观察不到任何区别，但服务端已经
消耗了一次 `attempt_count`——`SPEC §4.6`"重试必然同样失败的场景不消耗
attempt_count"这条设计意图专门针对 precheck 失败（arch/固件不符）场景，
但"分派未接线"这种配置/代码缺口却落进了"precheck 通过、正常消耗 attempt"
的路径，等于运维重试满 `model.MaxBootAttempts`（3）次后，Windows 任务会被
打成 `attempt_exceeded` 永久失败，而根因只是渲染层没接线，与机器或网络
毫无关系。`/v1/answer` 端点有相同性质但更明显的表现（HTTP 500，而不是
200 掩盖），且同样存在"状态先提交、渲染后失败"的顺序问题：
`MarkAnswerFetched`（`booting → installing`）在
`internal/gateway/answer.go` 第 87 行执行，`RenderAnswerFile` 调用在第 92
行——任务会被静默卡进 `installing`，即使渲染最终失败。

## 已确认能正常工作的部分

以下机制与"分派是否接线"无关（它们发生在 `RenderMenu`/`RenderAnswerFile`
调用之前，或者是完全独立的模块），本轮脚本验证全部通过：

1. **四条通用准入负面用例**（阶段 6a-6d，与 RHEL 基线同构）：陌生机器回
   `sanboot`；SN 对 MAC 不对回 `sanboot` 且任务置 `failed`
   （`failureReason=precheck`）；架构不符同样回 `sanboot` 且置 `failed`；
   precheck 失败的任务允许 `retry` 回到 `pending`。
2. **Windows 特有的双固件负面用例**（阶段 6e）：同一 Windows 版本按固件
   （`uefi`/`bios`，取具体值而非 `any`，`SPEC §0.3/§2.1` 硬约束）各导入
   一个制品集后，BIOS 任务遇到机器实际上报 `plat=efi` 时被硬拒绝
   （`internal/statemachine/precheck.go` 的 `checkFirmware`），且**验证
   了不消耗 attempt_count**（`precheck` 在 `StartBoot` 内部排在
   `attempt++` 之前，`internal/statemachine/boot.go` 第 43-66 行）。这一
   条在 RHEL 基线里没有对应用例（RHEL 走 `firmware=any`，不存在固件不匹配
   的制品集选择问题），是本报告新增覆盖的场景。
3. **十一个信号的打点、去重、越权拒绝**（阶段 8b/8c）：Windows 家族的
   `winPESignalPoints`（`winpe.go`：`pre-start`/`pre-done`/
   `packages-start`/`packages-done`/`post-start`/`post-done`/
   `rebooting`，七个）与 `windowsInitScript` 对应的
   `booted`/`init-start`/`init-done`/`ready`（四个），信号名与状态机内部
   的 `model.LookupSignal` 表完全复用 Linux 侧同一套枚举与序号
   （`Seq: 10/20/.../110`，与传输协议家族无关）——`ApplySignal` 的分支
   逻辑本身不区分家族，验证结果与 RHEL 基线一致。finalize token 越权发
   装机期信号被正确拒绝（403）。
4. **终态收敛与状态门控**（阶段 9a-9d）：任务正确到达 `done`；装完的机器
   再次引导拿到 `sanboot` 而不是装机菜单（本报告额外用状态查询确认这一次
   `sanboot` 的原因是任务已终态，不是与阶段 7 同一个"分派未接线"缺口的
   混淆——两种不同原因在机器侧会产生同一种表面响应，必须靠查库区分）；
   终态任务的应答文件请求回 403；事件表留下 23 条完整时间线。

## Windows 的导入路径确认仍被 `mvpUnsupportedFamilies` 挡着

`internal/imports/identify.go` 的 `mvpUnsupportedFamilies` 表仍把
`model.OSFamilyWindows` 判定为"识别出但 MVP 不支持"
（`ErrUnsupportedOSFamily`），`internal/imports/job.go` 只有 `RunRHEL` 一个
真正执行抽取的入口，没有 `RunWindows`——`RunUnsupported` 是 Windows 目前
唯一会走到的分支，效果是把制品集直接置为 `failed`，不会调用
`internal/imports/extract_windows.go` 的 `ExtractWindows`（该文件自己的
契约缺口说明已经完整记录了这一点，本报告在此确认它在本轮仍然成立，不是
过时信息）。

本脚本对此的绕法与 `scripts/e2e.sh` 完全一致：用真实的
`POST /artifact-sets` 端点落一条 `state=importing` 记录取得真实分配的 ID，
再用 `sqlite3` CLI 直接把该记录推进到 `ready` 并写回
`paths_json`/`os_family`/`arch`/`firmware`——磁盘上的文件仍是
`scripts/e2e/mkfixture-windows.sh` 造出的真实文件，绕过的只是"谁调用
`ExtractWindows`"这个缺失的调用点，不是伪造磁盘内容本身。

## 验证方式的必要调整（因分派缺口而绕行，非脚本图省事）

由于阶段 7/8a 确认的分派缺口，本脚本从阶段 7 开始无法像 RHEL 基线那样
"从真实菜单文本里解析出 install token 再继续"——机器根本拿不到菜单。改为
在确认过"任务状态与响应内容的不一致"这个发现本身之后，直接从数据库读出
`StartBoot`/`IssueFinalizeToken` 签发的 token 继续验证信号通道、状态机、
终态收敛这几个与渲染层解耦的机制。这不是伪造凭据——是绕过"机器要从菜单/
应答文件正文解析 token"这一步，去验证"信号端点本身能否用一个真实签发的
token 认证"，机制上与 `scripts/e2e.sh` 阶段 8 读取 `finalize_token`
列的做法完全同一类。

## 运行方式与环境注意事项

```
bash scripts/e2e-windows.sh
```

依赖 `curl`、`sqlite3`（本机 MSYS UCRT64 环境自带）、`go`（`go build`
构建二进制，不依赖已安装的产物）。九个阶段全部打印 `[PASS]`，退出码 0。

**已知的环境敏感点**：脚本使用固定端口（网关 28180/控制台 28190/syslog
28614），与 `scripts/e2e.sh`（RHEL 基线，28080/28090/28514）不冲突，但若
连续两次快速重跑本脚本，Windows 的 TCP `TIME_WAIT`（默认约 120 秒）可能
让第二次的 `bind` 失败——这不是脚本逻辑错误，本机实测两次运行间隔 5 秒会
撞上、间隔 30 秒不会。`scripts/e2e.sh` 用固定端口的设计选择理论上有
同样的敏感性，本报告记录下来供后续如果要在 CI 里连续跑两个 e2e 脚本时
注意，不建议为此改动基线脚本的端口分配方式（那超出本轮任务"只写自己的
文件"的授权范围）。

## 本报告未覆盖、留给未来轮次的部分

1. **Windows 分派接线之后的重新验证**：一旦 `t08-dispatch`
   （或后续轮次）在 `menu.go`/`renderer.go` 补上 `case
   model.OSFamilyWindows` 分支，本脚本阶段 5/7/8a 的断言需要整体改写——
   当前断言的是"确认失败"，接线后这些断言会变红，这是**预期且需要的**
   信号，提醒维护者更新脚本去验证真实的菜单文本（四行 wimboot/BCD/
   boot.sdi/boot.wim）与真实的 `unattend.xml` 内容，而不是继续断言失败。
2. **`unattend.xml` 的 XML 解析与转义验证**：任务说明要求"用
   `encoding/xml` 验证取到的应答文件能被解析，恶意输入的转义是否正确"。
   本轮由于 `/v1/answer` 端点对 Windows 返回 500（渲染从未真正执行到
   `RenderUnattend`），没有真实的 `unattend.xml` 响应体可供解析验证——
   `internal/render/unattend_test.go`/`golden_test.go` 已经在单元测试层
   覆盖了这部分（模板渲染、XML 转义、guard 校验），但"通过真实 HTTP 请求
   拿到的应答文件内容"这条端到端链路要等分派接线后才能真正跑通。
3. **WimArchDetector 的真实实现**：`extract_windows.go` 的固件位宽探测
   目前只有测试用假实现，真实 WIM 格式解析需要额外依赖（不在本模块允许的
   依赖白名单内），本轮沿用 sqlite3 绕过导入 job 的方式，未涉及这部分。
4. **真机/QEMU 层面的 WinPE 引导**（wimboot 加载、DISM 应用镜像、
   PowerShell 遥测通道的真实网络行为）不在本轮范围——本轮验证的是"服务端
   HTTP API 与状态机是否正确"，不是"WinPE 客户端是否真的能装完 Windows"。

## Go 侧发现汇总（供 t08-dispatch 与相关轨参考，本轨不修复）

| # | 位置 | 发现 |
|---|------|------|
| 1 | `internal/render/menu.go:28`、`internal/render/renderer.go:60` | `model.OSFamilyWindows` 仍与 SUSE/ESXi 一起路由到"未实现"分支，`RenderWindowsMenu`/`RenderUnattend` 从未被调用 |
| 2 | `internal/gateway/boot.go:150-190` | `StartBoot`（消耗 attempt、签 token、CAS 到 booting）先于 `RenderMenu` 提交；渲染失败时机器收到与 no_task 相同的响应，但 attempt_count 已被消耗——重试满 3 次会被打成 `attempt_exceeded`，根因是渲染层缺口 |
| 3 | `internal/gateway/answer.go:87-92` | `MarkAnswerFetched`（booting→installing）先于 `RenderAnswerFile` 提交；渲染失败时任务已被静默卡进 installing |
| 4 | `internal/imports/identify.go`、`internal/imports/job.go` | `mvpUnsupportedFamilies` 仍挡着 Windows；`RunWindows` 不存在，`ExtractWindows` 零调用点（`extract_windows.go` 自身已记录此缺口，本报告确认在本轮仍成立） |

以上第 2/3 两条是本轮运行**新确认**的具体行为（不只是"渲染未接线"这一句
话，而是精确到"状态在渲染失败前已提交"这个顺序问题及其后果），第 1/4 两条
是对已知缺口在当前代码状态下的复核确认。
