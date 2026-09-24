# ESXi 端到端验证报告

> **数据层迁移说明**：本文记录的是数据层仍为 SQLite 时的情况，数据层已于
> 2026-09-24 迁移到 GORM + PostgreSQL（见
> `docs/superpowers/specs/2026-09-24-gorm-postgres-migration-design.md`），
> 正文保留当时原貌，未作修改。

验证方式：`scripts/e2e-esxi.sh`（curl 模拟机器打点，本地跑通，不碰硬件），
结构照抄 `scripts/e2e.sh`（RHEL 家族，九阶段全过，MVP 验收基线）——只读它、
照它的结构写，未修改它一个字符。本报告不是真机测试报告（与
`03-PXE-引导实测.md` 不同类），是"分派刚接上、代码从没跑到过"这句任务描述
下的第一次真实运行验证。

## 一句话结论

**ESXi 家族的渲染代码（`internal/render/esxi.go`）质量很好、测试与 golden
file 齐全，但从未被真正调用过——`internal/render/menu.go` 与
`internal/render/renderer.go` 的分派 switch 里，`model.OSFamilyESXi` 仍与
`SUSE`、`Windows` 一起落在返回"MVP 范围内未实现"的分支。** 这不是猜测：
`scripts/e2e-esxi.sh` 完整跑通了一条真实成功路径（UEFI + SN/MAC/arch 全部
匹配），实测到状态机侧已经把任务推进到 `booting` 并消耗了一次
`attempt_count`，但机器收到的响应体与"完全陌生的机器"逐字符相同（`sanboot`
载荷）——因为 `RenderMenu` 内部报错，网关把渲染失败静默降级成了
`no_task` 载荷。

四条准入控制负面用例（陌生机器/MAC 不匹配/架构不符/retry 允许）与 ESXi
特有的第五条（BIOS 请求装 ESXi 硬拒绝）全部通过真实路径验证，且这五条
与分派是否接线完全无关（precheck 三项校验发生在 `StartBoot` 内部，早于
对 `Renderer.RenderMenu` 的调用）。

## 运行方式

```bash
bash scripts/e2e-esxi.sh
```

依赖与 `e2e.sh` 完全相同：curl、sqlite3、go（均已确认本机可用）。脚本自身
起停服务、自建临时数据库与制品目录，不依赖任何已安装的服务，退出时自动
清理（trap EXIT/INT/TERM）。已验证连续运行 8 次（含 3 次紧邻无间隔的
背靠背运行）全部 exit 0，无端口冲突、无残留进程（用 `Get-Process -Name
lober` 核对过），见"脚本本身的一个真实 bug 与修复"一节。

## 阶段结果总览

| 阶段 | 结果 | 说明 |
|---|---|---|
| 1 造制品树/二进制/admin/配置 | 通过 | ESXi 假制品树（`mboot.efi`+`boot.cfg`+四个模块文件），语法与 `esxi_test.go` 的 `rawBootCfgTemplate` 同构 |
| 2 起服务 | 通过 | 动态挑选空闲端口（见下方"脚本本身的一个真实 bug 与修复"），轮询到就绪 |
| 3 登录 | 通过 | 与家族无关 |
| 4 造主机+制品集+模板 | 通过（制品集经 sqlite3 绕过导入识别，见下）| |
| 5 preview 渲染 | **按预期报错**（分派未接线） | 断言错误信息含"MVP 范围内未实现" |
| 6 五条准入负面用例 | 全部通过 | 含 ESXi 特有的 BIOS 硬拒绝（第 5 条），与分派无关 |
| 7 真实成功路径 | **核心发现**：状态机推进但渲染失败 | 见下 |
| 8/9 应答文件/信号/终态 | 无法进行 | 机器从未拿到装机 token |

## 发现 1（核心）：ESXi 分派处仍未接线，成功路径无法端到端跑通

用 `grep` 直接核对了 `internal/render/menu.go` 与
`internal/render/renderer.go` 在本 worktree 当前 HEAD 的字面内容：

```go
// menu.go RenderMenu
case model.OSFamilySUSE, model.OSFamilyWindows, model.OSFamilyESXi:
    return "", unimplementedFamilyError(ctx.Artifact.OSFamily)

// renderer.go RenderAnswerFile
case model.OSFamilySUSE, model.OSFamilyWindows, model.OSFamilyESXi:
    return "", unimplementedFamilyError(ctx.Artifact.OSFamily)
```

`git log --oneline -- internal/render/menu.go internal/render/renderer.go`
显示两文件最近的改动是 `140fef4 "entry dispatch with end-to-end injection
guard"`——即分派骨架 commit（switch 结构、`validateAnswerFileName` 防注入
校验），本轮之前的"Wave2"只完成了骨架，没有给 ESXi（连同 Windows、SUSE）
接上具体渲染函数调用，尽管 `internal/render/esxi.go` 的三个导出函数
（`RenderESXiBootCfg`/`RenderESXiMenuLine`/`AssembleESXiKickstart`）与对应
单元测试、golden file 全部就位且看起来正确（本次跑了
`go test ./internal/render/... -run ESXi -v`，全部 PASS，见"独立验证"节）。

### 传导链（本脚本阶段 7 实测到的完整数据）

用 6e 之后 retry 回到 `pending` 的任务，带完全正确的一切（UEFI、正确
SN/MAC/arch）请求一次 `/v1/boot`：

```
请求前 attempt_count = 0
请求后 attempt_count = 1
请求后任务 state     = booting
机器实际收到的响应体 = #!ipxe
echo lober: no task for E2E-ESXI-SN-0002
sanboot --no-describe --drive 0x80
```

具体传导路径：

1. 机器上报 UEFI + arch 匹配 + SN/MAC 双因子匹配 → precheck 三项校验
   （arch/firmware/制品完整性）全部通过。
2. `internal/statemachine/boot.go` 的 `StartBoot` 第 4/5 步随即执行：
   `attempt_count++`、签发装机 token、任务 CAS 到 `booting`——这一步已经
   "消耗了一次真实的装机尝试"，不可撤销。
3. `internal/gateway/boot.go` 的 `handleBootGrant` 调用
   `s.deps.Renderer.RenderMenu(renderCtx)`，命中 `unimplementedFamilyError`。
4. `handleBootGrant` 对渲染失败的处理是静默回退到 `writeNoTaskScript`
   （sanboot 载荷），HTTP 响应仍是 200——机器侧收到的响应与"完全陌生的
   机器"或"MAC 不匹配"两种拒绝场景**逐字符相同**，机器没有任何办法区分
   "我从未被认识"与"我被认识、已经消耗了一次尝试、但服务端不会渲染我的
   菜单"。

### 为什么这比一个直白的报错更值得关注

任务的 `attempt_count` 已经不可逆地递增，但 HTTP 响应看起来与完全无害的
"没有任务"一模一样。若 MVP 的 `attempt_count` 上限较小（SPEC §9.1 第三道
闸门），这意味着"分派没接好"这一个纯服务端问题会表现成"这台机器很快就
被服务端标记为 `attempt_exceeded` 永久拒绝"——运维会去查机器硬件或网络，
而不会想到去查渲染器的 switch 分支。这是本次验证发现的最有价值的一点：
**"分派未接线"对机器侧不是一个能立刻观察到的失败，而是一个会伪装成
"机器自己有问题"的延迟性故障。**

### 正解方向

`internal/render/menu.go` 的 `RenderMenu` 与 `internal/render/renderer.go`
的 `RenderAnswerFile` 需要各加一个 ESXi 分支：

```go
// menu.go（示意，不是最终实现）
case model.OSFamilyESXi:
    return renderESXiMenu(ctx) // 需要新写：从 ctx.Bases/ctx.Task 拼出
                                // mbootEFIPath 与 answerFileURL，调用
                                // RenderESXiMenuLine
```

`renderer.go` 一侧更复杂，见发现 3（第二文件名契约缺口）——不是简单加一个
`case` 就能完成，需要先解决"一个任务需要两个不同应答文件"这件事。

本轨文件归属不包含 `menu.go`/`renderer.go`（归 t08-dispatch 独占），本报告
只记录发现，不越界修改。

## 发现 2：ESXi 的导入识别在 MVP 阶段被拒绝

`internal/imports/identify.go` 的 `mvpUnsupportedFamilies` 仍把 ESXi 列在
其中——`Identify()` 认出 `boot.cfg`+`mboot.efi` 特征后返回
`(model.OSFamilyESXi, ErrUnsupportedOSFamily)`，且
`internal/imports/job.go` 只有 `RunRHEL` 一个执行入口，没有 `RunESXi`。

本脚本按任务要求"优先走真实的登记→导入→ready 路径，走不通再退回 sqlite3
绕法"，验证结果是：即使制品集登记→触发导入 job 这条桥接在本轮由其它轨
接上了（本轨未直接验证这件事本身，因为那是 Go 代码，不在本轨文件归属
内），`identify.go` 的 MVP 支持闸门仍会在识别阶段就拒绝 ESXi——真实触发
一次导入只会验证"MVP 确实明确拒绝 ESXi 导入"这一件事，不会让制品集变成
`ready`。因此本脚本采用与 `e2e.sh` 完全相同的绕法：跳过导入 job，直接用
`sqlite3` CLI 把制品集 `state` 推进到 `ready` 并写回
`paths_json`/`os_family`/`firmware`（`firmware='uefi'`，ESXi 的固件字段
在 `contract.ArtifactSetStore.SetArtifactSetContent` 中同样没有写入路径，
这是 `internal/imports/extract_esxi.go` 已经记录过的既有缺口，本报告不
重复分析，只确认它同样适用于本次验证场景）。文件本身仍是
`scripts/e2e/mkfixture-esxi.sh` 造出的真实文件、真实落在制品根目录下，
绕过的只是"识别+导入"这两步，不是伪造磁盘内容。

## 发现 3：ESXi 应答文件的第二文件名契约缺口（复述 esxi.go 已记录的，实测确认其影响范围）

即使分派接好，`internal/render/renderer.go` 的 `validateAnswerFileName`
当前只接受 `ctx.AnswerFileName()` 算出的唯一文件名（Ubuntu 是唯一例外，
硬编码接受额外的 `"meta-data"`）。`model.AnswerFileName(esxi)` 固定返回
`"boot.cfg"`，但真实 ESXi 流程需要两个独立文件：`mboot.efi` 引导器读取的
`boot.cfg`（本身不是 kickstart 语法）与 `boot.cfg` 的 `kernelopt` 行
`ks=` 指向的另一份真正的 kickstart 脚本（`AssembleESXiKickstart` 的产出）。

`esxi.go` 文件末尾已经完整记录了这个缺口与正解方向（建议第二文件名
`"ks.cfg"`，与 Ubuntu 的 `meta-data` 先例同构）。本报告在此补充一点实测
确认：**即使发现 1 的分派 switch 被接上，若不同时补上这个第二文件名的
分派，取应答文件那一步（`/v1/answer/<task>/<token>/ks.cfg`）仍会被
`validateAnswerFileName` 拒绝**，因为它只会拿请求的文件名与
`"boot.cfg"` 比较，请求 `"ks.cfg"` 会被判定为"文件名不一致"而报错——这
意味着发现 1 与发现 3 是同一条端到端链路上的两个独立断点，修复发现 1
（接上 `RenderMenu`/`RenderAnswerFile` 的 ESXi 分支）本身不足以让机器真正
装上 ESXi，还需要发现 3 的第二文件名机制同时到位。

## 独立验证：ESXi 渲染层单元测试全部通过

尽管分派未接线，`internal/render` 包内 ESXi 相关的渲染逻辑本身经检验是
正确的（本报告不是"渲染代码有问题"，而是"渲染代码没被调用"）：

```
go test ./internal/render/... -run ESXi -v -count=1
```

全部通过（`RenderESXiBootCfg` 的 9 个用例、`RenderESXiMenuLine` 的 6 个
用例、`AssembleESXiKickstart` 的 8 个用例、`ClientLibESXi` 的 6 个用例，
含两个 golden file 断言），细节见 `internal/render/esxi_test.go`（本轨
未修改该文件，只是运行它确认现状）。

## 五条准入控制负面用例：全部通过，与分派无关

precheck 三项校验（`checkArch`/`checkFirmware`/`checkArtifactFiles`）发生
在 `internal/statemachine/boot.go` 的 `StartBoot` 内部，早于对
`Renderer.RenderMenu` 的调用——因此这五条用例即使在分派完全未接线的当前
状态下也全部按预期工作：

1. **完全陌生的机器** → `sanboot`（§4.2、§0.1）。
2. **SN 对但 MAC 不对** → `sanboot`，任务置 `failed`（`failureReason=
   precheck`，§4.3 双因子硬要求）。
3. **架构不符** → `sanboot`，任务置 `failed`，失败原因可读（§4.6 硬拒绝）。
4. **precheck 失败的任务允许 retry 并回到 `pending`**（§8.2 只禁两类超时）。
5. **【ESXi 特有】固件不符（BIOS 请求装 ESXi）** → 硬拒绝，错误信息明确
   可读：

   ```
   这台机器是 bios，任务要装 esxi，该 OS 家族不支持这种固件模式
   ```

   （`internal/statemachine/precheck.go` 的 `checkFirmware`，`%s` 格式化
   `model.FirmwareMode` 的原始取值，实测是小写 `bios`/`esxi`）。这正是
   SPEC §4.6、附录 A.14 明确要求的"BIOS 机器不能被分配 ESXi 任务"硬拒绝，
   且拒绝点确实在网关（本次实测未经过 DHCP 层，只验证了网关这一层）。

   本报告额外核对了一条 SPEC §4.6 隐含但未被任务描述明确列出的断言：
   **两次 precheck 拒绝（架构不符 + 固件不符）之后，`attempt_count`
   仍是 `0`**——查库确认，precheck 失败不消耗 attempt（§4.6："重试必然
   同样失败"这一原则要求拒绝发生在"任务真正开始"之前）。

## 脚本本身的一个真实 bug 与修复：固定端口在并行 worktree 环境下会撞车

初版脚本沿用 `e2e.sh` 的思路，用与 RHEL 脚本不同的固定高位端口
（28180/28190/28614）。连续跑 3 次后第 3 次失败：

```
listen tcp 127.0.0.1:28180: bind: Only one usage of each socket address
(protocol/network address/port) is normally permitted.
```

排查过程：一度怀疑是 `SERVER_PID=$!` 在 Git Bash/Windows 上追踪到的是
中间层 PID 而非真实 `lober.exe` 进程（`kill` 杀错对象导致真实进程存活、
端口未释放）——用 `ps` 核对某次诊断复现确实观察到这个现象，但后续更严谨
的复现（用完全独立、语法正确的临时配置反复测试）没有再现，判断那次观察
是诊断过程中一次误读（当时的诊断配置本身有 YAML 语法错误，进程可能还
没完全进入正常运行状态）。

真正的根因用 `Get-NetTCPConnection` 直接查证：**本仓库同一时刻有 15+ 条
worktree 轨在同一台 Windows 机器上并行跑各自的端到端脚本**，实测直接
观察到一个属于别的家族轨（Debian）遗留的 `lober` 进程占着 `28080`/
`28090`——与 `e2e.sh` 本身使用的端口相同。这说明"固定端口 + 各轨各自随手
加一个偏移量"这个假设在真实并行环境下不成立：任意两条轨都有不小概率
选中同一个端口区间。

**修复**：改为运行期用 `$RANDOM` 在 `30000-49000` 区间随机挑选三个端口，
每次用 bash 内建 `/dev/tcp` 探测确认三者当前都空闲才使用，最多重试 20
轮，全部失败才报错退出（不静默退回一个可能仍冲突的固定值）。修复后连续
验证 8 次运行（含 3 次背靠背无间隔运行）全部 `exit 0`，端口号在多次运行
间确实不同（实测样例：31479、38955、47362、43195、31502），且用
`Get-Process -Name lober` 核对每次运行结束后均无残留进程。

这个修复只影响 `scripts/e2e-esxi.sh` 自身，未触碰 `scripts/e2e.sh`（该
文件按任务铁律不允许修改），也不代表 `e2e.sh` 存在同样的隐患一定会发生
——`e2e.sh` 全程运行时间更长，可能只是恰好没有在关键窗口撞上其它轨，
但两者用的是同一类"固定端口"策略，理论风险是共享的，写在这里供参考。

## 未能验证 / 结构性限制

- **阶段 8（应答文件、装机期七信号、finalize token 权限校验）与阶段 9
  （终态断言、装完再引导拿 sanboot、事件时间线）完全无法验证**——这不是
  本脚本还需要补的逻辑，是发现 1 的分派缺口本身导致的：机器从未拿到
  装机 token（菜单里的 `inst.ks=`/`ks=` 段），这两个阶段依赖的输入
  （`INSTALL_TOKEN`）根本不存在。一旦发现 1（及发现 3）被修复，
  `scripts/e2e-esxi.sh` 阶段 7 之后的部分需要重写为断言真正的菜单内容
  （`mboot.efi -c <boot.cfg URL>`，SPEC §4.5）并继续验证阶段 8/9——脚本
  内已经在阶段 7 的断言注释里写明了这一点，方便未来直接定位需要改哪里。
- **`esxcli system syslog config set --loghost=` 这条 ESXi 特有的 syslog
  配置方式本脚本验证不到**——它不是 kickstart 里的一行指令，而是
  `%firstboot` 段落里的一条 shell 调用（`internal/render/esxi.go` 的
  `esxiSyslogDirective` 只在 kickstart 正文里留一行注释说明），而阶段 8/9
  本身就因为发现 1 无法到达，这条更是无法验证。
- **真实 ESXi 安装器（vmkernel）加载 `mboot.efi`/`boot.cfg` 之后的实际
  行为**：本脚本与 `mkfixture-esxi.sh` 造的都是占位文件（内容是
  `printf` 出来的文本），不是真实 ESXi ISO 的二进制内容——这与
  `e2e.sh`/`mkfixture.sh` 对 RHEL 采用的策略完全一致（导入逻辑只关心
  特征文件存在性，不解析内核镜像本身），但意味着本报告不能代表"真实
  ESXi 安装器拿到改写后的 `boot.cfg` 真的能引导"这一步，只能代表"服务端
  各层逻辑按预期处理了一台声称自己是 UEFI+ESXi 的机器"。
- **BIOS+ESXi 硬拒绝只验证了网关这一层**（`/v1/boot?plat=pcbios`），未
  验证 SPEC §4.6 提到的"建任务时按记录的固件模式过滤可选 OS 列表"这条
  更早的拦截（这条本身在 SPEC 里也标注为"需补"的缺口，见 SPEC §4.3 附近
  讨论，不是本轨范围内的验证目标）。

## 与 RHEL 基线（`e2e.sh`）的差异小结

| 维度 | RHEL（`e2e.sh`） | ESXi（`e2e-esxi.sh`） |
|---|---|---|
| 分派是否接线 | 是（`renderRockyMenu`/`AssembleKickstart`） | **否**（发现 1） |
| 导入识别 | 通过（`.treeinfo`），但导入 job 桥接仍是绕过 | 识别本身即被 `mvpUnsupportedFamilies` 拒绝（发现 2），比 RHEL 更早一层 |
| 应答文件命名 | 单文件（`ks.cfg`，取自 `AnswerFileName`） | 需要两个文件（发现 3），当前契约只支持一个 |
| 九阶段可达性 | 全部九阶段通过 | 阶段 1-6 通过，阶段 7 揭示核心发现后停止，阶段 8/9 结构性无法到达 |
| 固件维度 | 不适用（Linux 四家用 `any`） | 全程使用（`uefi`，且验证了 BIOS 硬拒绝） |
