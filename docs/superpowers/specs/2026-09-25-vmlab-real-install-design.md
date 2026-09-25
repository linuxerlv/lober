# VM 实验床：让后端真的把系统装上去

| | |
|---|---|
| 日期 | 2026-09-25 |
| 状态 | 设计定稿，待实现 |
| 范围 | 六个 OS 家族在本机 QEMU 上串行真实装机，验收到状态机 `done` |

## 1. 问题

渲染层、导入层、状态机的单元测试都是绿的，七个 `scripts/e2e*.sh` 也全通，
但**没有任何一次真实安装发生过**。两层原因：

- 七个 e2e 脚本一律用 `curl` 模拟机器打点，用 psql 直接把
  `artifact_sets` 推到 `ready`（见 `docs/real-machine/17-iso读取实现.md`
  的「现状」一节）。制品文件是真的落在盘上，但没有一次
  `imports.Job.Run` 跑过，也没有一次 anaconda 读过我们渲染的 kickstart。
- `docs/real-machine/03-PXE-引导实测.md` 走通了 DHCP → TFTP → HTTP →
  菜单渲染，终点是 `sanboot 失败回落`——引导链验证到菜单下发为止，
  之后的安装、重启、firstboot 回调、状态机终态全部未经真实验证。

因此「装机链路已接上」这个结论目前只在**单元测试层与引导链前半段**成立。
本设计要把它推到产品语义上的终点：**目标机真的被装上系统，任务真的走到
`done`，且人能 SSH 登进去。**

## 2. 已验证的前置事实

设计基于以下**实测结果**（非推断），2026-09-25 在本机取得：

```
LOBER_TEST_REAL_ISO=/d/OpenCloudOS-9.6-x86_64-minimal.iso \
  go test ./internal/imports/ -run TestRealISO -v
→ PASS (17.6s)
  ISO 根目录 8 项: [.discinfo .treeinfo BaseOS EFI Minimal boot images media.repo]
  识别家族=rhel  Kernel="vmlinuz" Initrd=[initrd.img] Repo="repo/BaseOS/"
```

纯 Go ISO 9660 解析器在真实 1.6G 镜像上可用，RHEL 家族识别与抽取正确。
这是整条链路的第一步，它成立才谈得上后面。

本机环境（实测）：内存 15.5G；D: 剩 78G，C: 34G，E: 20G；QEMU 11.0.1 在
`D:\msys\ucrt64\bin`；`edk2-x86_64-code.fd` 与 `bios.bin` 均在
`/d/msys/ucrt64/share/qemu/`；`/d/OpenCloudOS-9.6-x86_64-minimal.iso` 已在盘上。

## 3. 拓扑

三台 QEMU，落在 `D:\qemu-vm\lober-testbed\`。服务端磁盘用现有
`debian-cloud\base.qcow2` 做新 overlay，**不动现有那台 VM**。

```
                SLIRP (apt / ssh from Windows)
                            │
                    ┌───────┴────────┐
                    │  VM-S  服务端   │  Debian 13, 2.5G
                    │  dnsmasq   DHCP + TFTP
                    │  nginx     /artifacts/ + API 反代
                    │  postgres  lober 库
                    │  lober     GOOS=linux GOARCH=amd64
                    └──┬──────────┬──┘
              br0: 10.20.20.1/24  （桥接两块 socket 网卡）
                 │                │
    ┌────────────┴───┐    ┌───────┴────────┐
    │ VM-T-bios      │    │ VM-T-uefi      │  空盘，-boot n
    │ SeaBIOS + iPXE │    │ OVMF edk2-x86_64-code.fd
    └────────────────┘    └────────────────┘
          （同一时刻只有一台目标机在跑，见 §4）
```

三个约束需要单独说明：

**为什么要桥。** `-netdev socket,listen/connect` 是点对点的，服务端一块
网卡只能接一个目标。VM-S 挂两块 socket 网卡、在 guest 里用
systemd-networkd 桥成 `br0`，两种固件的目标机才能同处
`10.20.20.0/24`、共用一份 dnsmasq 配置。不经 Windows 宿主的任何桥接，
与 03 报告的做法一致。

**CPU model 是待验证的风险点。** `[[qemu-local-vm]]` 记录 WHPX 下
`-cpu max` / `-cpu host` 必炸（`Unexpected VP exit code 4`，串口零输出），
必须 `-cpu qemu64` + `-accel whpx,kernel-irqchip=off`。但 `qemu64` 只到
x86-64-v1（缺 SSE4.2、POPCNT），而 RHEL 9 系 glibc 要求 x86-64-v2
baseline，预期会 `Fatal glibc error: CPU does not support x86-64-v2`。
对策是 `-cpu Nehalem` 或 `qemu64,+ssse3,+sse4.1,+sse4.2,+popcnt`。
**这是推断，不是已验证事实**——实施第一步就实测确认，见 §8 里程碑 0。

**iPXE 二进制取 Debian 的 `ipxe` 包**（`undionly.kpxe` / `snponly.efi`）。
03 报告已验证这两个文件可用。dnsmasq 配置从 `deploy/dnsmasq/lober.conf`
填占位符生成，option 93 的 7/9 双匹配照搬不改——那是 03 报告用抓包证实过
的判据（真实 UEFI 固件报 7 不报 9）。

**ISO 以只读 virtio 盘挂给 VM-S**，`sourcePath=/dev/vdb`。
`iso9660.go:196` 的 `perCallOpenReaderAt` 每次 ReadAt 独立开关文件，
不校验「必须是普通文件」，块设备可直接用。零拷贝，不占 guest 磁盘。
scp 进去是备选路径。

## 4. 串行队列

### 4.1 为什么串行

15.5G 总内存，Windows 宿主自己占 4-5G。VM-S 2.5G + 一台目标机 3G
峰值 5.5G，宽裕；三台并行 8.5G 就开始换页，装机会慢到分不清是卡死
还是在挣扎。**时间换空间**：一次一台，装完一个再起下一个。

### 4.2 队列内容与预算

| # | 家族 | 目标机内存 | 目标机盘 | ISO | 已知阻塞 |
|---|---|---|---|---|---|
| 01 | RHEL / OpenCloudOS 9.6 **BIOS** | 3G | 12G | 本机已有 | 无（基线） |
| 02 | RHEL / OpenCloudOS 9.6 **UEFI** | 3G | 12G | 同上 | B1 + B2 |
| 03 | Debian 13 | 3G | 12G | 下 ~700MB | 无 |
| 04 | Ubuntu 24.04 | 3G | 12G | 下 ~2.6G | 无 |
| 05 | openSUSE Leap 16 | 3G | 12G | 下 ~250MB | e2e-suse 覆盖缺口 |
| 06 | Windows Server 2022 | 4G | 32G | 下 ~5G | B5 + B6，两处新代码 |
| 07 | ESXi | 8G | 32G | 获取存疑 | 镜像可得性 |

排序依据是**制品可得性 × 代码就绪度**，不是家族重要性。RHEL 占两格
是因为两种固件要各验一遍（02 依赖 B1/B2 的修复）。

**磁盘**：ISO 下载约 9G + VM-S overlay（含六套解开的制品）约 15G +
七块目标机 overlay 实际占用约 25G ≈ 50G，D: 的 78G 留 28G 余量。
overlay 虚拟大小按上表开，稀疏分配，实际只占已写入部分。

**内存峰值**：条目 01-06 是 5.5-6.5G，宽裕。条目 07 是 10.5G，加宿主
就贴到天花板，会换页。ESXi 8.0 硬性要求 8G 不给妥协，ESXi 7.0 只要 4G
——真跑到那步时优先找 7.0 镜像。

**时间**：每条目首次跑含调试 1-3 小时，全队列跨多个工作日。因此队列
**必须**能从任意条目中断和续跑，不能设计成一条跑到底的脚本。

### 4.3 Windows 与 ESXi 的实际卡点

Windows 的阻塞不只在制品，代码断了两处（见 §5 的 B5、B6）：
`cmd/lober/importrunner.go` 从未调用 `Job.ConfigureWindows`，生产路径上
`windowsArchDetector` 是 nil，`extract_windows.go:126` 会直接拒绝导入；
而 `WimArchDetector` 至今只有测试假实现（`extract_windows.go:228-234`
自己承认了这一点）。这两处不补，ISO 下来了也导不进去。

ESXi 的卡点在镜像可得性（Broadcom 已收回免费版下载），不在代码。
若拿不到镜像，条目 07 判定为**环境阻塞**而非后端缺陷，队列照常结束，
在最终报告里明确写出这一条未验证。

## 5. 后端要改的地方

### B1 —— kickstart 不看固件

`internal/render/kickstart.go:177-179` 的 `baseDirectives` 无条件发：

```
bootloader --location=mbr
clearpart --all --drives=<disk>
part / --fstype=xfs --grow --ondisk=<disk>
```

整个函数不读 `ctx.ActualFirmware`。UEFI 机器上 anaconda 会因为没有 EFI
系统分区而失败。SPEC §0.3 表格里「RHEL BIOS / UEFI 通用」这一行对渲染出
的 kickstart **不成立**——通用的是制品集（一套 kernel/initrd 通吃），
不是应答文件。

改成按 `ctx.ActualFirmware` 分派：UEFI 增加 `part /boot/efi --fstype=efi`
并让 `bootloader` 不带 `--location=mbr`；BIOS 保持现状字节不变。
既有 golden 文件分裂成 bios/uefi 两份。

### B2 —— `/v1/answer` 拿不到固件

`internal/gateway/answer.go:70` 硬传 `""` 给 `buildRenderContext`，注释
写的是「应答文件渲染不依赖机器本次上报的固件模式」。B1 一落地这句就
不成立。

不能退回用 `Host.Firmware`：`precheck.go:52` 的 `checkFirmware` 只把
上报值与**制品集**的固件比对，而 RHEL 制品集是 `FirmwareAny`，
`MatchesFirmware` 对它无条件返回 true——清单里那个 discovery 阶段记录的
`Host.Firmware` 从来没有被任何代码路径校验过，不可信。

做法：`StateMachine.StartBoot` 已经收到 `actualFirmware` 参数
（`statemachine/boot.go:47`），把它持久化到 task 上，`answer.go` 读回来。
涉及 `model.Task` 加字段 + AutoMigrate 加列。纯新增列、不碰索引，
避开 `[[gorm-automigrate-index-blind-spots]]` 记录的两个盲区。

该字段语义是「本次引导时机器**实测上报**的固件模式」，与
`Host.Firmware`（discovery 阶段记录、可能过期、从未被校验）是两件不同的
事，不复用后者也不覆写后者。字段在 `/v1/boot` 命中时写入，
`/v1/answer` 只读——应答文件必须与本次引导的真实固件一致，否则渲染出的
分区方案会与机器实际引导方式错配。

### B3 —— 单一 repo 路径是否够装（待实测）

真 ISO 抽出 `Repo="repo/BaseOS/"`，但该 ISO 根下有 `BaseOS` 与
`Minimal` 两个变体目录。`@core` 包组应当全在 BaseOS 内，anaconda 单靠它
能装完——**这是判断，不是已验证事实**。若装不完，需让 `ArtifactPaths`
支持多 repo，那是契约变更，会明显放大本轮范围；届时先记录证据再单独决策，
不在本轮夹带实现。

### B4 —— `importrunner.go` 注释已过期

文件头与 `newImportRunner` 都还写着 `MountExtractor.Open` 返回
「未实现真实挂载逻辑」，但 1480131 已接上纯 Go 解析器，§2 的实测也证明
它在真 ISO 上可用。顺手改掉——照着过期注释判断能力边界会误导下一个人。

同类过期注释：`internal/render/menu.go:17` 写「SUSE 仍留在未实现分支」，
但第 47 行已接 `RenderAutoYaSTMenu`（9702a0f 修的）。一并改。

### B5 —— `WimArchDetector` 没有生产实现（条目 06 前置）

需要新写一个读 WIM 头、判断 boot.wim 位宽的实现。`extract_windows.go`
的文档已经指出这是部署适配阶段留下的缺口。

### B6 —— 装配点未接 Windows 导入（条目 06 前置）

`cmd/lober/importrunner.go` 需要调 `Job.ConfigureWindows(resolver, detector)`。
`WindowsSourcesResolver` 要解析 wimboot / BCD / boot.sdi / boot.wim 四个
外部文件的本地路径（都不在 ISO 里，需单独获取）。

B1/B2/B5 **先写单测再改代码**（TDD），然后才进 VM 验证。不先测就改，
VM 里失败时分不清是新 bug 还是老 bug。

## 6. 队列运行器

新目录 `scripts/vmlab/`，与 `scripts/e2e*.sh` **分开**——那七个是 curl
模拟、进 CI 的；这套要起虚拟机、只在本机手跑。混在一起会让「e2e 全绿」
这句话再次失去意义（`[[lober-e2e-scripts]]` 记录过同类教训）。

```
scripts/vmlab/
  queue.sh              编排器：一次一个条目，可中断可续跑
  lib-vm.sh             QEMU 启动参数（-cpu / -accel / socket 网卡常量）
  lib-api.sh            控制台 API 客户端（curl + token）
  serverside/
    provision.sh        VM-S 一次性装机：dnsmasq nginx postgresql lober
    dnsmasq.conf.in     ← deploy/dnsmasq/lober.conf 填占位符
    nginx.conf.in       ← deploy/nginx/lober.conf
    lober.yaml.in       ← deploy/lober.yaml.example
  entries/
    01-rhel-bios.sh     每条目声明：ISO、家族、固件、内存、模板正文、期望阶段
    02-rhel-uefi.sh
    …  07-esxi.sh
```

**VM-S 只装一次，全队列复用。** `provision.sh` 幂等，装完打
`.provisioned` 标记。这是串行换来的最大好处：六个条目不必各装一遍
postgres。每个条目只做四件事：导制品集、建模板与 hook、建任务、起目标机。

**目标机每条目一块全新 overlay**（`rm` 后 `qemu-img create -b base`
重建），装完保留不删。条目间零污染，失败后那块盘还能挂起来看
anaconda 日志。

**续跑靠 `.tmp/vmlab/state/<entry>.json`。** 默认跳过已 `passed` 的条目；
`--only 03` / `--from 03` / `--redo 03` 覆盖手动重跑。这是硬要求不是便利
——单条目 1-3 小时、全队列跨多个工作日，不可能一口气跑完。

**装机任务全部经真实 API 建立**，不写库：
`POST /auth/login` → `POST /hosts` → `POST /artifact-sets`（触发真实
`imports.Job.Run`）→ `POST /templates` → `POST /hooks` +
`POST /hooks/:id/approve` → `POST /tasks`。admin 角色带全部权限码，
一个用户够。这一条同时验证了控制台侧的权限码与 hook 审批闸门。

## 7. 验收标准

两条**独立**证据，缺一条都不算通过。

**证据一：状态机终态与阶段序列。** `GET /tasks` 的 state 为 `done`；
`GET /tasks/:id/events` 按序含
`booting → installing → rebooting → initializing → done`，
以及 `pre-start` / `pre-done` / `packages-*` / `post-*` / `booted` /
`init-*` / `ready` 这串信号。

**证据二：SSH 登进新装的机器。** 用注入的公钥登录，跑
`cat /etc/os-release` 与 `systemctl is-system-running`。这条不能省——
那串信号是我们自己渲染的脚本发出来的，全到齐只证明脚本跑完了，
不证明系统是好的。同时它验证了 `kickstart.go:190-194` 的
`rootpw --lock` + 公钥注入是唯一登录通路这条设计（SPEC §1.5）。

**失败即停，不自动往下走。** 每条目 45 分钟墙钟预算。超时或失败时落全部
证据并停住队列：串口 log、qemu log、dnsmasq log、nginx access log、
lober journal、task events JSON、目标机 overlay。`--keep-going` 才继续。
默认停是因为失败大概率源自 §5 那类后端缺陷，往下跑只会拿到更多同源失败。
排查走 systematic-debugging，不靠猜。

## 8. 里程碑

| # | 内容 | 出口判据 |
|---|---|---|
| 0 | CPU model 实测 | 目标机在 WHPX 下能引导且不报 x86-64-v2 错误，参数组合写进 `lib-vm.sh` |
| 1 | VM-S provision | dnsmasq/nginx/postgres/lober 四件都起来，`lober -check` 通过 |
| 2 | B1/B2 修复（TDD） | 新单测先红后绿，golden 分裂成 bios/uefi 两份，`make check` 全绿 |
| 3 | 条目 01（RHEL BIOS） | §7 两条证据齐全 |
| 4 | 条目 02（RHEL UEFI） | 同上，且证明 B1/B2 的修复在真固件上成立 |
| 5 | 条目 03-05（Debian/Ubuntu/SUSE） | 每条目 §7 两条证据齐全 |
| 6 | B5/B6 + 条目 06（Windows） | 同上 |
| 7 | 条目 07（ESXi） | §7 两条证据齐全，或明确记录为环境阻塞 |

里程碑 3 是本设计的**最小成功线**：它一成立，「后端能在 VM 环境下真实
装上系统」这句话第一次有了证据。其余条目是覆盖面的扩展。

## 9. 明确不做

以下都在 SPEC Phase 4 或 §0.2 显式排除项里，与「能真的装上系统」无关：

- Secure Boot、TLS / mTLS、UEFI HTTP Boot
- `secure_link` 制品授权（Phase 2 加固开关，默认 false 即可）
- 真实硬件验证（本轮只在 QEMU 上跑）
- 分机架 HTTP cache、多播、desync 增量传输
- 带外管理（Redfish / IPMI）——SPEC §0.2 已排除
- 修 `e2e-suse.sh` 的阶段 6-9 覆盖缺口（既有问题，与本轮正交）

## 10. 与既有文档的关系

本设计是 `docs/real-machine/03-PXE-引导实测.md` 的续篇：那份报告的终点是
`sanboot 失败回落`，本设计从那里接着往下走到 `done`。

验证结果按既有惯例落进 `docs/real-machine/`，每条目一份报告，
编号从 18 起。
