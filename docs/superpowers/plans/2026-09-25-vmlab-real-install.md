# VM 实验床实现计划 —— 六个 OS 家族串行真实装机

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 让 lober 后端在本机 QEMU 上真的把系统装上去，六个 OS 家族串行排队，验收到状态机 `done` 且能 SSH 登进新装的机器。

**架构：** 三台 QEMU（VM-S 服务端跑 dnsmasq/nginx/postgres/lober，两台目标机分别 BIOS 与 UEFI，桥接在 `10.20.20.0/24`）。受 15.5G 内存限制同时只跑一台目标机，靠 `scripts/vmlab/queue.sh` 串行调度、可中断续跑。装机任务全部经真实控制台 API 建立，不写库。

**技术栈：** QEMU 11.0.1（WHPX）、Debian 13 guest、dnsmasq 2.91、nginx、PostgreSQL、Go 1.27.1 交叉编译 linux/amd64。

**设计依据：** `docs/superpowers/specs/2026-09-25-vmlab-real-install-design.md`

**与设计 §8 里程碑的映射：** 里程碑 0 → 任务 1；1 → 任务 2；2 → 任务 3+4；3 → 任务 6（**最小成功线**）；4 → 任务 7；5 → 任务 8；6 → 任务 9+10；7 → 任务 11。

---

## 文件结构

### 后端改动（§5 的 B1-B6）

| 文件 | 职责 | 为什么这样切 |
|---|---|---|
| 修改 `internal/core/model/task.go` | `Task` 加 `ActualFirmware` 字段 | 领域模型是「本次引导实测固件」这个事实的唯一归属地。不复用 `Host.Firmware`——那是 discovery 阶段记录、从未被任何代码校验过的值 |
| 修改 `internal/store/postgres/task/row.go` | `taskRow` 加对应列与 `model.Task` 双向映射 | 持久化细节隔离在 store 层，领域层不感知列名 |
| 修改 `internal/core/model/task.go`（`TaskMutation`） | 加 `ActualFirmware *model.FirmwareMode` | 走既有 mutation 通道写入，不新增 Store 方法 |
| 修改 `internal/statemachine/boot.go:139-146` | `StartBoot` 把已收到的 `actualFirmware` 写进 mutation | 值已在手（形参），只是从未落库 |
| 修改 `internal/gateway/answer.go:70` | 从 task 读回固件，替掉硬传的 `""` | 应答文件必须与本次引导的真实固件一致 |
| 修改 `internal/render/kickstart.go`（`baseDirectives`） | 按 `ctx.ActualFirmware` 分派分区与 bootloader 指令 | 分区方案是固件的函数，这是 B1 的核心 |
| 新建 `internal/render/testdata/kickstart_full_uefi.golden` 等 | golden 分裂成 bios/uefi 两份 | 既有 golden 是 BIOS 形态，不能原地改——那会丢掉 BIOS 回归基线 |
| 新建 `internal/imports/wim.go` | `WimArchDetector` 的生产实现（读 WIM 头判位宽） | 独立文件：WIM 格式解析与 ISO 9660 解析同级，不该塞进 `extract_windows.go` |
| 修改 `cmd/lober/importrunner.go` | 调 `Job.ConfigureWindows`；清理过期注释 | 装配点是唯一能同时 import console 与 imports 的包 |
| 修改 `internal/render/menu.go:17` | 删掉「SUSE 未实现」的过期注释 | 第 47 行早已接上 `RenderAutoYaSTMenu` |

### VM 实验床（§6）

| 文件 | 职责 |
|---|---|
| 新建 `scripts/vmlab/lib-vm.sh` | QEMU 参数常量（`-cpu`/`-accel`/socket 网卡/OVMF 路径）、overlay 重建、串口日志落盘 |
| 新建 `scripts/vmlab/lib-api.sh` | 控制台 API 客户端：登录取 token，建 host/artifact-set/template/hook/task，轮询状态 |
| 新建 `scripts/vmlab/serverside/provision.sh` | VM-S 一次性装机，幂等，打 `.provisioned` 标记 |
| 新建 `scripts/vmlab/serverside/{dnsmasq.conf.in,nginx.conf.in,lober.yaml.in}` | 从 `deploy/` 三份配置填占位符 |
| 新建 `scripts/vmlab/queue.sh` | 编排器：读条目、跑一个、落状态、失败即停 |
| 新建 `scripts/vmlab/entries/0N-*.sh` | 每条目声明 ISO/家族/固件/内存/模板正文/期望阶段 |

**为什么与 `scripts/e2e*.sh` 分开：** 那七个是 curl 模拟、进 CI 的；这套起虚拟机、只在本机手跑。混在一起会让「e2e 全绿」再次失去意义（`docs/real-machine/17-iso读取实现.md` 记录过同类教训）。

---

## 任务 1：实测 WHPX 下能跑 RHEL 9 的 CPU model

**目标：** 确定一组 QEMU 参数，使目标机在 WHPX 加速下能引导且不触发 RHEL 9 的 x86-64-v2 检查，把结论固化成 `lib-vm.sh` 的常量。

**文件：**
- 创建：`scripts/vmlab/lib-vm.sh` —— 本任务只写 CPU/加速相关常量与一个自检函数

**背景约束（已知事实，不要重新试错）：**
- WHPX 下 `-cpu max` 与 `-cpu host` 必然 `WHPX: Unexpected VP exit code 4`，串口零输出
- 必须配 `-accel whpx,kernel-irqchip=off`，否则刷 `Ignoring request for interrupt vector 0`
- QEMU 在 `D:\msys\ucrt64\bin`，用绝对路径调用

**待验证的推断：** `-cpu qemu64` 只到 x86-64-v1（缺 SSE4.2/POPCNT），RHEL 9 系 glibc 要求 v2 baseline，预期报 `Fatal glibc error: CPU does not support x86-64-v2`。候选替代是 `-cpu Nehalem` 与 `-cpu qemu64,+ssse3,+sse4.1,+sse4.2,+popcnt`。

**接口契约：**

```bash
VMLAB_CPU_ARGS      # 数组：目标机 -cpu 与 -accel 参数
VMLAB_QEMU_BIN      # QEMU 可执行文件绝对路径
vmlab_check_cpu()   # 自检：直接引导 OpenCloudOS ISO 的 installer 内核，
                    # 成功=串口出现 installer 启动输出，失败=非零退出
```

- 用 `-cdrom` 直接引导 §2 那张 OpenCloudOS ISO 验证，**不经 PXE**——本任务只隔离 CPU 变量，引入网络引导会让失败原因无法归因
- 串口重定向到文件，自检读该文件判成败，不依赖交互

- [ ] **红：** 先用 `-cpu qemu64` 跑一次 `vmlab_check_cpu`，记录真实串口输出。这一步的目的是**取证**而非通过：若确实报 x86-64-v2 错误，把该输出片段抄进 `lib-vm.sh` 的注释作为「为什么不用 qemu64」的依据；若竟然通过了，则推断有误，直接采用 qemu64 并在注释中记录此事。

- [ ] **绿：** 依次试候选 CPU model 直到自检通过，把胜出的参数组合写成 `VMLAB_CPU_ARGS`。注释里写清试过哪些、各自的失败症状。

- [ ] **重构 + commit：** 确认自检可重复跑两次结果一致后 commit（`feat(vmlab): WHPX 下可跑 RHEL 9 的 QEMU CPU 参数`）。

**风险：** 若全部候选都在 WHPX 下失败，退路是改用 TCG 软件模拟（`-accel tcg`）。慢一个数量级（装机可能从 20 分钟变成数小时），但不阻塞正确性验证。真走到这一步要先停下来报告，不要默默切 TCG。

---

## 任务 2：VM-S 服务端一键 provision

**目标：** 一条命令把服务端 VM 从 Debian 13 base 镜像变成可用的 lober 部署（dnsmasq + nginx + postgres + lober 四件都起来），幂等可重跑。

**文件：**
- 创建：`scripts/vmlab/serverside/provision.sh`
- 创建：`scripts/vmlab/serverside/dnsmasq.conf.in` —— 源自 `deploy/dnsmasq/lober.conf`
- 创建：`scripts/vmlab/serverside/nginx.conf.in` —— 源自 `deploy/nginx/lober.conf`
- 创建：`scripts/vmlab/serverside/lober.yaml.in` —— 源自 `deploy/lober.yaml.example`
- 修改：`scripts/vmlab/lib-vm.sh` —— 加 VM-S 启动函数与双 socket 网卡桥接

**占位符替换表（三份 `.in` 文件的全部变量）：**

| 占位符 | 值 | 出处 |
|---|---|---|
| `<PXE_VLAN_IFACE>` | `br0` | 桥接两块 socket 网卡 |
| `<PXE_VLAN_SUBNET_START/END/NETMASK>` | `10.20.20.100` / `10.20.20.200` / `255.255.255.0` | |
| `<TFTP_SERVER_IP>` / `<GATEWAY_IP>` | `10.20.20.1` | VM-S 在 br0 上的地址 |
| `<GATEWAY_DOMAIN>` | `gw.vmlab.internal` | dnsmasq 自己解析（`address=/…/`） |
| `<NTP_SERVER_IP>` | `10.20.20.1` | |
| `<TFTP_ROOT_DIR>` | `/srv/tftp` | 放 Debian `ipxe` 包的两个二进制 |

**接口契约：**

```bash
provision.sh              # 幂等；已装则立即退出 0
provision.sh --force      # 忽略 .provisioned 标记重装
```

- `lober.yaml.in` 的 `root_ssh_public_keys` 填 `D:\qemu-vm\debian-cloud\id_ed25519.pub` 的内容——复用现有密钥对，验收阶段的 SSH 登录用它
- `gateway.listen` 与 `console.listen` 绑 `127.0.0.1`，nginx 做唯一对外入口（SPEC §1.4）
- `secure_link_enabled` 保持 `false`（设计 §9 明确不做）
- postgres 建 `lober` 库与同名角色，`sslmode: disable`（同机 loopback）
- 出口判据之一：`lober -config /etc/lober/lober.yaml -check` 退出码 0

**iPXE 二进制**：`apt-get install ipxe` 后把 `undionly.kpxe` 与 `snponly.efi` 复制进 `/srv/tftp`。03 报告已验证这两个文件可用。

- [ ] **红：** 在全新 overlay 上跑 `provision.sh`，预期首次失败（脚本尚不存在或步骤不全）。记录失败点。

- [ ] **绿：** 补齐脚本直到四项检查全过：`systemctl is-active dnsmasq nginx postgresql lober` 全为 active；`lober -check` 退出 0；从 Windows 侧 `curl http://127.0.0.1:<fwd>/console/v1/…` 能拿到 401（服务在跑且认证生效）；guest 内 `ip addr show br0` 显示 `10.20.20.1/24`。

- [ ] **重构 + commit：** 再跑一次 `provision.sh` 确认幂等（立即退出、不重复装），然后 commit（`feat(vmlab): 服务端 VM 一键 provision`）。

**风险：** 桥接会断开 guest 的 SSH。`[[qemu-local-vm]]` 记录 `netplan apply` 会踢掉连接、`netplan generate` 不会。桥接配置必须在 provision 脚本里一次写完再重启网络，不要交互式分步改。SLIRP 那块网卡（apt/ssh 用）不要加进桥。

---

## 任务 3：task 持久化本次引导实测的固件（B2）

**目标：** 让 `/v1/answer` 能拿到本次引导机器实测上报的固件模式，为任务 4 的分区分派提供输入。

**文件：**
- 修改：`internal/core/model/task.go` —— `Task` 与 `TaskMutation` 各加一个字段
- 修改：`internal/store/postgres/task/row.go` —— `taskRow` 加列与双向映射
- 修改：`internal/statemachine/boot.go:139-146` —— mutation 里带上形参 `actualFirmware`
- 修改：`internal/gateway/answer.go:70` —— 读 task 字段替掉硬传的 `""`
- 测试：`internal/statemachine/boot_test.go`、`internal/gateway/answer_test.go`、`internal/store/postgres/task/store_test.go`

**接口契约：**

```go
// model.Task 新增
ActualFirmware FirmwareMode  // 本次引导机器实测上报值；pending 阶段为空

// model.TaskMutation 新增
ActualFirmware *FirmwareMode
```

- 语义是「本次引导实测」，与 `Host.Firmware`（discovery 记录、可能过期、**从未被任何代码校验**）是两件不同的事：不读它、不写它
- `/v1/boot` 命中时由 `StartBoot` 写入；`/v1/answer` 只读
- 列为纯新增，`not null;default:''`，**不带任何 index 标签**——`[[gorm-automigrate-index-blind-spots]]` 记录 AutoMigrate 对已存在索引永不更新，新增列不碰索引就绕开了这个盲区
- `answer.go` 读到空值时：保持既有行为渲染（不报错）。空值只可能出现在「任务还没走过 `/v1/boot`」这种不该到达 answer 端点的路径上，此处不是校验点

**要覆盖的输入类别：**
1. `StartBoot(uefi)` 后取任务 → 字段为 `uefi`
2. `StartBoot(bios)` 后取任务 → 字段为 `bios`
3. store 层往返：写入后读出值不变（需 `LOBER_TEST_PG_DSN`，见 `[[lober-test-pg-dsn]]`）
4. `/v1/answer` 渲染上下文的 `ActualFirmware` 等于任务里存的值，不再是空串

- [ ] **红：** 为上述四类各写失败测试。运行 `go test ./internal/statemachine/ ./internal/gateway/ -run 'Firmware' -v`，预期 FAIL（字段未定义，编译错误即是红灯）。

- [ ] **绿：** 按契约实现直到四类全绿。不要在本任务改渲染逻辑（那是任务 4）。store 测试需先 `export LOBER_TEST_PG_DSN=...`。

- [ ] **重构 + 验证 + commit：** 跑全量 `go test ./...` 确认无回归，特别确认既有 `answer_test.go` 断言未被破坏。commit（`feat(gateway)!: task 持久化本次引导实测固件，应答文件渲染不再假设固件`）。

**风险：** `boot.go:139` 的 mutation 在 `casRetry` 回调内构造，回调可能因版本冲突重跑。`ActualFirmware` 对同一次引导请求是常量（来自形参，不依赖 task 当前值），重跑幂等，不需要额外处理。

---

## 任务 4：kickstart 按固件分派分区与 bootloader（B1）

**目标：** 让渲染出的 kickstart 在 UEFI 机器上包含 EFI 系统分区，BIOS 路径字节不变。

**文件：**
- 修改：`internal/render/kickstart.go`（`baseDirectives`，当前 163-195 行）
- 创建：`internal/render/testdata/kickstart_full_uefi.golden`、`kickstart_minimal_uefi.golden`
- 测试：`internal/render/kickstart_test.go`、`internal/render/golden_test.go`

**当前行为（问题所在）：** `baseDirectives` 无条件发 `bootloader --location=mbr` + `clearpart --all` + 单个 `part / --fstype=xfs --grow`，整个函数不读 `ctx.ActualFirmware`。UEFI 机器上 anaconda 因缺少 EFI 系统分区而失败。SPEC §0.3 的「RHEL BIOS/UEFI 通用」说的是**制品集**通用（一套 kernel/initrd 通吃），不是应答文件通用。

**接口契约：** `baseDirectives` 签名不变（已收 `ctx model.RenderContext`），行为按 `ctx.ActualFirmware` 分派：

| `ActualFirmware` | bootloader | 分区 |
|---|---|---|
| `bios` | `bootloader --location=mbr` | 现状不变：`clearpart --all --drives=X` + `part / --fstype=xfs --grow --ondisk=X` |
| `uefi` | `bootloader`（不带 `--location`） | `clearpart` 不变，加 `part /boot/efi --fstype=efi --size=600 --ondisk=X`，`part /` 保持 `--grow` |
| 空串 | 与 `bios` 相同 | 保守兜底：既有调用方（含未走过 `/v1/boot` 的 preview 路径）行为不变 |

- 磁盘标识仍取 `ctx.Facts[model.FactDiskPrimary]`，不硬编码 `/dev/sda`
- 公钥为空时的既有报错行为（`kickstart.go:164-169`）不动
- `--size=600`（MiB）：RHEL 9 推荐 ESP ≥ 200MiB，600 给未来内核留余量。这个字面量本身是决策

**要覆盖的输入类别：**
1. `ActualFirmware=bios` → 输出与修改前逐字节一致（回归基线）
2. `ActualFirmware=uefi` → 含 `part /boot/efi`，且 `bootloader` 行不含 `--location=mbr`
3. `ActualFirmware=""` → 输出与类别 1 一致
4. golden 对比：`kickstart_full.golden`（BIOS，不动）与新增的 `_uefi.golden` 两份都匹配

- [ ] **红：** 为四类写失败测试。类别 1、3 应当**立刻通过**（现状就是 BIOS 行为）——这正是回归基线的意义，先让它们绿着；类别 2、4 预期 FAIL。运行 `go test ./internal/render/ -run 'Kickstart|Golden' -v`。

- [ ] **绿：** 实现固件分派直到四类全绿。新 golden 文件**手写期望内容**再跑测试，不要用「跑一遍把实际输出存成 golden」的方式生成——那样 golden 只是实现的复印件，测不出任何东西。

- [ ] **重构 + commit：** 跑 `go test ./internal/render/` 全绿，确认 `kickstart_exec_test.go` 与 `injection_test.go` 未受影响。commit（`fix(render)!: kickstart 按固件分派分区，UEFI 不再缺 EFI 系统分区`）。

**风险：** `internal/console/template.go:462` 的 preview 路径传的是 `host.Firmware`（清单 discovery 值）。本任务**不改它**——preview 是给人看的近似预览，与真实装机路径的数据来源不同是可接受的；但要在 `baseDirectives` 的注释里写明这个差异，避免下一个人以为两处该统一。

---

## 任务 5：队列编排器与 API 客户端

**目标：** 一条命令跑完一个队列条目（导制品集→建模板 hook 任务→起目标机→判定），失败即停，可中断续跑。

**文件：**
- 创建：`scripts/vmlab/lib-api.sh`
- 创建：`scripts/vmlab/queue.sh`
- 修改：`scripts/vmlab/lib-vm.sh` —— 加目标机启动与 overlay 重建函数

**接口契约（`lib-vm.sh` 新增，供条目脚本调用）：**

```bash
vmlab_reset_overlay <entry_id> <size>        # rm 旧 overlay 后 qemu-img create -b base
vmlab_start_target <entry_id> <firmware> <mem>  # 起目标机，-boot n，串口落盘；uefi 时挂 pflash
vmlab_stop_target <entry_id>                 # 确认进程退出（内存约束，下一条目才能起）
```

**接口契约（`lib-api.sh`，全部走真实 HTTP，不碰数据库）：**

```bash
api_login <user> <pass>                      # 导出 VMLAB_TOKEN
api_create_host <sn> <mac> <arch> <firmware> # → host id
api_import_artifact <source_path> <name>     # → artifact_set id（触发真实 imports.Job.Run）
api_wait_artifact_ready <id> <timeout_sec>   # 轮询到 ready；failed 时打印 FailReason 后非零退出
api_create_template <name> <family> <body_file>  # → template id
api_create_hook <name> <phase> <script_file> # → hook id（state=draft）
api_approve_hook <id>                        # draft → active
api_create_task <host_sn> <template_id> <artifact_set_id> <form_json>  # → task id
api_wait_task_state <id> <want_state> <timeout_sec>
api_dump_events <id> <out_file>              # GET /tasks/:id/events 全量落盘
```

- 端点路径前缀 `/console/v1`，经 nginx 人面 server 块
- 建 hook 后**必须**调 `api_approve_hook`：`hook.go:39` 的审批闸门要求 active 才能被任务引用。这一步同时验证了 RBAC 权限码
- `api_import_artifact` 的 `source_path` 是 VM-S 内的路径。ISO 以只读 virtio 盘挂给 VM-S，传 `/dev/vdb`——`iso9660.go:196` 的 `perCallOpenReaderAt` 每次 ReadAt 独立开关文件、不校验「必须是普通文件」，块设备可直接用

**接口契约（`queue.sh`）：**

```bash
queue.sh                    # 跑全部未通过的条目，遇失败即停
queue.sh --only 03          # 只跑条目 03
queue.sh --from 03          # 从 03 开始往后
queue.sh --redo 03          # 忽略已 passed 状态重跑 03
queue.sh --keep-going       # 失败不停，继续下一条
```

- 状态落 `.tmp/vmlab/state/<entry>.json`，含 `state`（passed/failed/blocked）、时间戳、失败摘要
- 默认跳过已 `passed` 的条目。**这是硬要求不是便利**：单条目 1-3 小时、全队列跨多个工作日
- 每条目墙钟预算 45 分钟，超时按失败处理
- 同时只起一台目标机（内存约束），条目结束前必须确认目标机进程已退出

**失败时必须落的证据（缺一样都算 queue.sh 的 bug）：** 目标机串口 log、qemu stderr、VM-S 的 `journalctl -u lober`、dnsmasq log、nginx access log、`api_dump_events` 的 JSON、目标机 overlay（保留不删）。

- [ ] **红：** 用一个**故意错误**的条目（指向不存在的 ISO 路径）跑 `queue.sh --only 00`，预期：非零退出、状态文件写 `failed`、七样证据齐全。这一步测的是失败路径，它比成功路径更容易写漏。

- [ ] **绿：** 补齐编排器直到上述预期全部满足，且 `--only`/`--from`/`--redo` 三个开关行为正确（用桩条目验证，不必真装机）。

- [ ] **重构 + commit：** 确认状态文件在中断（Ctrl-C）后不残留半写状态。commit（`feat(vmlab): 串行队列编排器与控制台 API 客户端`）。

**风险：** `POST /artifact-sets` 立即返回 `state=importing`，真实导入异步跑（`artifact.go:200`）。真 ISO 抽取实测 16.8 秒，但 Ubuntu 2.6G 镜像会更久——`api_wait_artifact_ready` 的超时给足 10 分钟，且**必须**在 failed 时打印 `FailReason`，否则导入失败会表现成无信息的超时。

---

## 任务 6：条目 01 —— RHEL BIOS 真实装机

**目标：** 达成本计划的最小成功线：OpenCloudOS 9.6 在 BIOS 目标机上真的装完，任务走到 `done`，SSH 能登进去。

**文件：**
- 创建：`scripts/vmlab/entries/01-rhel-bios.sh`
- 创建：`docs/real-machine/18-vmlab-rhel-bios.md` —— 验证报告
- 修改：`scripts/vmlab/lib-api.sh` —— 按实跑暴露的问题补齐

**条目声明内容：** ISO `/dev/vdb`（OpenCloudOS 9.6）、家族 `rhel`、固件 `bios`、目标机 3G/12G、模板正文（kickstart 的 `%packages` 段，取 `@core`）、一个 pre hook 与一个 firstboot hook（各打一行日志即可——本条目验的是链路通不通，不是 hook 能干多复杂的事）、期望阶段序列。

**验收要两条独立证据（缺一条都不算通过）：**

1. **状态机终态与阶段序列**：`GET /tasks` 的 state 为 `done`；events 按序含 `booting → installing → rebooting → initializing → done`，以及 `pre-start`/`pre-done`/`packages-*`/`post-*`/`booted`/`init-*`/`ready`
2. **SSH 登进新装的机器**：用注入的公钥登录，`cat /etc/os-release` 显示 OpenCloudOS，`systemctl is-system-running` 为 `running` 或 `degraded`（degraded 可接受但报告里要写清哪个 unit 失败）

证据二不能省：那串信号是我们自己渲染的脚本发出来的，全到齐只证明脚本跑完了，不证明系统是好的。它同时验证了 `kickstart.go:190-194` 的 `rootpw --lock` + 公钥注入是唯一登录通路（SPEC §1.5）。

- [ ] **红：** 跑 `queue.sh --only 01`，预期失败。**这是本计划最重要的一次红灯**——它是第一次真实装机，几乎必然暴露设计 §5 未预见的缺口。按 systematic-debugging 定位根因，不要猜着改。

- [ ] **绿：** 修到两条证据齐全。每修一处**先判断它属于哪类**：(a) 实验床脚本问题 → 改 `scripts/vmlab/`；(b) 后端缺陷 → 先补单测再改代码，且单测要能在 VM 之外复现；(c) 环境限制 → 记进报告，不改代码。三类不要混着改。

- [ ] **重构 + commit：** 写 `docs/real-machine/18-vmlab-rhel-bios.md`：真实输出片段、踩到的坑、以及**诚实说明哪些部分不能代表真实硬件**（参照 03 报告对 QEMU BIOS ROM 本身就是 iPXE 构建这一局限的处理方式）。commit。

**风险（设计 §5 B3，必须在本任务判定）：** 真 ISO 抽出的是单个 `Repo="repo/BaseOS/"`，但该 ISO 根下有 `BaseOS` 与 `Minimal` 两个变体目录。`@core` 应当全在 BaseOS 内——**这是判断不是事实**。若 anaconda 在选包阶段报缺包，说明需要让 `ArtifactPaths` 支持多 repo，那是契约变更：**停下来，记录证据，报告后再决策**，不要在本任务里夹带实现。

---

## 任务 7：条目 02 —— RHEL UEFI，验证 B1/B2 在真固件上成立

**目标：** 同一套制品集与模板，换 OVMF 固件的目标机装完，证明任务 3、4 的修复在真实 UEFI 上成立。

**文件：**
- 创建：`scripts/vmlab/entries/02-rhel-uefi.sh`
- 创建：`docs/real-machine/19-vmlab-rhel-uefi.md`

**与条目 01 的差异（只有这些）：** 目标机加 `-drive if=pflash` 挂 `edk2-x86_64-code.fd` 与一份可写 vars、复用 01 的 artifact_set 与 template、目标机 SN/MAC 不同（避免撞上 `idx_tasks_sn_active` 那个「同一 SN 不能有两个活跃任务」的唯一索引）。

**额外验收（在任务 6 两条证据之外）：**
- 取一份实际下发的 kickstart（`GET /v1/answer/...` 或从串口日志抓），确认含 `part /boot/efi` 且 `bootloader` 不带 `--location=mbr`
- 目标机 `efibootmgr -v` 显示有 OpenCloudOS 的引导项——证明 ESP 真的被用上了，不只是分区存在
- dnsmasq log 显示这台机器走的是 option 93 = **7**（03 报告抓包证实真实 UEFI 报 7 不报 9），确认 `deploy/dnsmasq/lober.conf` 的双值配置在本路径上生效

- [ ] **红：** 跑 `queue.sh --only 02`，记录失败点。

- [ ] **绿：** 修到全部验收项通过。若失败在 B1/B2 的修复不完整，**补单测后再改**——UEFI 分区形态的期望值应当能在 `internal/render` 的单测里断言，不该只靠 VM 验。

- [ ] **重构 + commit：** 写报告并 commit。

**风险：** OVMF 的 pflash vars 必须每条目一份可写副本，不能多台 VM 共用一个文件——共用会让引导项互相污染，且 QEMU 可能直接因文件被占用而拒绝启动。

---

## 任务 8：条目 03-05 —— Debian / Ubuntu / openSUSE

**目标：** 证明换 OS 家族不需要改装机链路，三套应答文件机制（preseed / autoinstall / AutoYaST）在真实安装器上成立。

**文件：**
- 创建：`scripts/vmlab/entries/{03-debian.sh,04-ubuntu.sh,05-suse.sh}`
- 创建：`docs/real-machine/{20-vmlab-debian.md,21-vmlab-ubuntu.md,22-vmlab-suse.md}`

**每条目的 ISO（先下载，USTC 源，见 `[[qemu-local-vm]]` 第 3 条）：** Debian 13 netinst ~700MB、Ubuntu 24.04 live-server ~2.6G、openSUSE Leap 16 netinst ~250MB。全部 UEFI 固件（BIOS 路径已由条目 01 覆盖）。

**验收：** 每条目同任务 6 的两条证据。额外确认每个家族的仓库判据真的生效——`model.RepoIndexPattern` 按家族分流（65883a5 修的，Debian/Ubuntu 用 APT glob 而非 `repodata/repomd.xml`），装机能过 precheck 就是这条修复在真实制品上的证据。

- [ ] **红：** 逐条跑 `queue.sh --only 0N`，记录各自失败点。三个条目**分别**走红绿循环，不要合并——家族差异正是要暴露的东西，合并会让归因变难。

- [ ] **绿：** 每条目修到两条证据齐全。Debian 这条会顺带验证 131d659（真实 netinst 被误判成 Ubuntu）与 3468d64 的修复。

- [ ] **重构 + commit：** 三份报告，每条目独立 commit。

**风险：** `scripts/e2e-suse.sh` 只有 5 个阶段、其余家族有 9 个（`[[lober-e2e-scripts]]` 记录的既有缺口）。本任务**不修那个脚本**（设计 §9 明确排除），但条目 05 若失败，先确认不是那个缺口掩盖的问题——SUSE 的 6-9 阶段从未被任何测试覆盖过。

---

## 任务 9：WIM 位宽探测器与 Windows 导入接线（B5/B6）

**目标：** 补上 Windows 导入路径在生产装配上的两处断口，让 Windows 制品集能真的导进来。

**文件：**
- 创建：`internal/imports/wim.go` —— `WimArchDetector` 的生产实现
- 创建：`internal/imports/wim_test.go`
- 修改：`cmd/lober/importrunner.go` —— 调 `Job.ConfigureWindows`，并清理已过期的注释

**当前断口（两处，都在生产路径上）：**
1. `cmd/lober/importrunner.go` 从未调用 `Job.ConfigureWindows`，所以 `windowsArchDetector` 是 nil，`extract_windows.go:126` 直接拒绝导入
2. `WimArchDetector` 至今只有测试假实现（`extract_windows.go:228-234` 自己承认了这一点）

**接口契约：**

```go
// 实现 imports.WimArchDetector
type WIMHeaderDetector struct{}
func (WIMHeaderDetector) DetectFirmware(bootWIMPath string) (model.FirmwareMode, error)

// 实现 imports.WindowsSourcesResolver（在 importrunner.go 内）
func (r *importRunner) ResolveWindowsSources(ctx context.Context, artifactSetID string) (imports.WindowsExternalSources, error)
```

- `DetectFirmware` 读 WIM 头部的 magic 与架构标识判位宽，**不解压镜像内容**
- 文件不是合法 WIM、或架构字段是未知值时返回 error，**不猜一个默认固件**——`extract_windows.go:148` 的既有注释明确要求「不能假设一个默认固件继续导入」
- `ResolveWindowsSources` 从制品集目录下的约定路径取 wimboot/BCD/boot.sdi/boot.wim 四个文件（都不在 ISO 里，需运维预先放好）；任一缺失返回 error 并在消息里点明缺哪个

**要覆盖的输入类别：**
1. 合法 x64 WIM → `uefi`
2. 合法 x86 WIM → `bios`
3. 非 WIM 文件（如随便一段字节）→ error
4. 文件不存在 → error
5. 四个外部文件齐全 → `ResolveWindowsSources` 成功
6. 缺 `boot.wim` → error 消息含 `boot.wim`

类别 1、2 需要真实 WIM 样本。若本机拿不到，用 `LOBER_TEST_REAL_WIM` 环境变量开关跳过（参照 `realiso_test.go` 对 `LOBER_TEST_REAL_ISO` 的处理方式与 skip-vs-fail 的论证），类别 3-6 用构造字节与临时目录覆盖，不依赖真样本。

- [ ] **红：** 为六类写失败测试。运行 `go test ./internal/imports/ -run 'WIM|WindowsSources' -v`，预期 FAIL。

- [ ] **绿：** 实现到六类全绿（真样本相关的按上述开关跳过）。

- [ ] **重构 + commit：** 顺手改掉 `importrunner.go` 文件头与 `newImportRunner` 里「MountExtractor.Open 未实现真实挂载逻辑」的过期注释（1480131 已接上纯 Go 解析器，任务 6 的实跑也证明它可用），以及 `internal/render/menu.go:17` 的「SUSE 未实现」过期注释。commit（`feat(imports): WIM 位宽探测器与 Windows 导入装配接线`）。

**风险：** WIM 有两种容器形态（旧的复合文件二进制与新版 XML 清单），`extract_windows.go:229-234` 已指出这一点。只实现能覆盖目标样本的那种，另一种返回明确的「不支持这种 WIM 形态」错误，不要静默猜。

---

## 任务 10：条目 06 —— Windows Server 真实装机

**目标：** Windows Server 2022 经 wimboot + WinPE + unattend.xml 装完，任务走到 `done`。

**文件：**
- 创建：`scripts/vmlab/entries/06-windows.sh`
- 创建：`docs/real-machine/23-vmlab-windows.md`

**前置（不满足则本条目判为 blocked，不是 failed）：** 任务 9 完成；Windows Server 2022 评估版 ISO；wimboot 二进制（iPXE 项目发布）；从 ISO 或 Windows ADK 取得 BCD / boot.sdi / boot.wim 三个文件。目标机 4G/32G，UEFI。

**验收（证据二的形态与 Linux 不同）：**
1. 状态机终态 `done`，阶段序列齐全（Windows 的信号由 `startnet.cmd` 与 firstboot 脚本发出，见 `winpe.go:88`）
2. 能远程登进去：RDP 或 WinRM（Linux 那条 SSH 公钥路径对 Windows 不适用）。unattend.xml 里要配好对应服务与凭据

- [ ] **红：** 跑 `queue.sh --only 06`，记录失败点。

- [ ] **绿：** 修到两条证据齐全。按任务 6 绿灯步骤的三类归因法分开处理。

- [ ] **重构 + commit：** 写报告，明确列出哪些制品是手工准备的、下一个人要怎么复现。commit。

**风险：** `winpe.go:212` 写死 `bcdboot W:\Windows /s S: /f UEFI`，盘符与固件都是硬编码。若目标机上盘符不是 `W:`/`S:`，这行会失败——那是真实的后端缺陷，按 (b) 类处理（先补单测再改）。

---

## 任务 11：条目 07 —— ESXi，或明确判定环境阻塞

**目标：** ESXi 装完走到 `done`，或**明确记录为环境阻塞**并给出复现所需条件。

**文件：**
- 创建：`scripts/vmlab/entries/07-esxi.sh`
- 创建：`docs/real-machine/24-vmlab-esxi.md`

**前置：** ESXi ISO。Broadcom 已收回免费版下载，拿不到就判 blocked——这是环境问题不是后端缺陷。ESXi 8.0 硬性要求 8G 内存，7.0 只要 4G，**优先找 7.0**：8G 目标机 + 2.5G VM-S + 宿主 4-5G 会贴到 15.5G 天花板并开始换页。

**ESXi 特有约束：** 仅 UEFI（`esxi.go:218` 的 `ErrESXiRequiresUEFI` 会拒绝 BIOS）；真实 ISO 里**没有** `mboot.efi`，是 `EFI/BOOT/BOOTX64.EFI` 按 PXE 惯例改名而来（`extract_esxi.go:32-37` 已记录）；boot.cfg 需运行时改写引导参数（c2ee84e 接的）。

- [ ] **红：** 拿到 ISO 则跑 `queue.sh --only 07` 记录失败点；拿不到则直接写 blocked 报告，跳到重构步骤。

- [ ] **绿：** 修到两条证据齐全（证据二：ESXi Shell 或 vSphere 登入确认 hypervisor 起来了）。

- [ ] **重构 + commit：** 写报告。blocked 的情况下报告必须写清：缺什么、从哪能拿到、拿到后跑哪条命令。commit。

**风险：** 若最终 blocked，**不要**为了让队列全绿而降低验收标准或伪造结论。设计 §4.3 已明确 blocked 是可接受结局，最终报告里如实列出这一条未验证。

---

## 整体验收

全部任务完成后逐项确认：

- [ ] `go build ./...` 退出 0，无输出
- [ ] `go vet ./...` 退出 0
- [ ] `export LOBER_TEST_PG_DSN=...` 后 `go test ./...` 全绿（缺该变量时 store 测试会**刻意整批失败**而非 skip，见 `[[lober-test-pg-dsn]]`）
- [ ] `LOBER_TEST_REAL_ISO=/d/OpenCloudOS-9.6-x86_64-minimal.iso go test ./internal/imports/ -run TestRealISO -v` 仍 PASS（本计划开工前的基线，不能被弄坏）
- [ ] `internal/render/testdata/kickstart_full.golden` 与 `kickstart_minimal.golden` **逐字节未变**（BIOS 回归基线；`git diff --stat` 对这两个文件应为空）
- [ ] 七个 `scripts/e2e*.sh` 中原本能跑通的五个仍能跑通（`[[lober-e2e-scripts]]`：suse 只有 5 阶段、nginx 跑不了，这两个的既有状态不变即可）
- [ ] `queue.sh` 无参数跑一遍：全部 passed 的条目被跳过，输出一份汇总（每条目 passed/failed/blocked + 耗时）
- [ ] `docs/real-machine/` 下每个条目一份报告，编号 18 起，无缺号
- [ ] 每份报告都含真实命令输出片段，且明确写出**哪些部分不能代表真实硬件行为**
- [ ] 至少条目 01（最小成功线）达成两条证据：任务 `done` + SSH 登入成功
- [ ] 设计 §9「明确不做」的七项一件都没被夹带实现（`git log` 与 `git diff main` 复核：无 TLS/Secure Boot/secure_link/HTTP Boot/cache/多播/desync 相关改动）

---

## 备注：三类归因法

任务 6 起的每次绿灯修复，先判断属于哪类再动手，三类不要混着改：

| 类别 | 判据 | 处置 |
|---|---|---|
| (a) 实验床脚本问题 | 后端行为正确，是脚本传错参/等错状态/证据没落全 | 改 `scripts/vmlab/`，不动 `internal/` |
| (b) 后端缺陷 | 后端行为不符合 SPEC 或自身文档 | **先补能在 VM 之外复现的单测**，再改代码 |
| (c) 环境限制 | QEMU/WHPX/镜像可得性/内存所致 | 记进报告，不改代码 |

混着改的后果是：VM 里终于绿了，但分不清哪一处修复真正起了作用，下次换个家族又断。
