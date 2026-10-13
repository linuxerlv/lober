# 第 4 层（ISO）设计

日期：2026-10-11
状态：已确认

**2026-10-14 实现现状：** 来源登记、下载/上传、本地原地读取、取消重试、恢复清单、识别、提取、Windows SMB 发布与删除后的文件清理接口已落地。**ESXi 首版明确不能导入为 ready**，因为没有经过验证的版本出处。第 3 层尚未合入此分支，render 的角色表改造留到合入之后；本次不把那部分记作完成。

合成介质覆盖 kickstart、preseed、autoinstall、AutoYaST、Agama、unattend 的导入流程。真实 ISO9660 读取已验证 Kali 安装镜像（6580 个条目）与 VMware Tools 镜像（30 个条目）；它们不是六种受支持发行版的安装验收证据。Debian/OpenCloudOS/Windows 等实际识别，以及第 7 层真装仍待验证。

已落实的边界：

- 一个暂存目录由一个装配点创建的 `Sources` 实例管理；并发进程不得共享它。服务自身不启动后台协程，`Register` 只登记，装配点调用阻塞的 `FetchOne`/`Import`。
- 本地来源遵循总规格：原地读取，运维管理，lober 不复制、不删除原文件。路径通过允许根和 `os.OpenInRoot` 校验；导入前后校验打开的文件。
- Windows WIM 的架构码 0 是 32 位 x86，当前目录不支持，因此明确拒绝；9 是 x86_64，12 是 aarch64。[微软平台说明](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/windows-setup-supported-platforms-and-cross-platform-deployments?view=windows-10)。
- Debian 附加 ISO 必须带可验证的版本、同架构和 netboot 标记；辅助 mini.iso 不要求 dists，不把光驱 NETINST 镜像用作 netboot。tar.gz 尚未支持，因为不能跳过同版本校验。[Debian 网络安装文件](https://www.debian.org/releases/stable/amd64/ch04s04.en.html)。
- 制品与 SMB 各在目标文件系统的临时兄弟目录内构建后 rename；只有读回确认数据库未提交 ready 时才撤回本次发布，提交结果不确定时保留目录待恢复。现有其他 ISO 的正式目录不覆盖；同一 ISO 重启时仅复用内容一致的完整目录。所有来源在包括 SMB 的最后一次复制后复核；符号链接按实际文件系统语义检查完整链，不能越过发布根目录。
- SHA256 去重保留仓储的 `DuplicateError` 和已有 ISO ID。导入失败说明保存在已取齐主来源的 `Error` 中，成功重导时清空。缺少必需辅助来源的 failed ISO 允许补登记该来源、获取并重导；主来源不能因此被替换。辅助获取期间 ISO 保持 failed，进度按来源状态显示，取消/重试也可作用于这些来源。
- HTTP 续传仅在给定期望 SHA256 时复用旧字节，发布前必须匹配；没有内容摘要或持久化实体验证器时重新完整下载，不能拼接不同远程实体。
- 删除由第 5 层审计编排：删除前保留 `DeletionSnapshot{ISO, Sources}`（Sources 必须来自 SourcesOf，空集合也非 nil），成功执行 Store.DeleteISO 后调用 `Sources.CleanupDeleted(ctx,snapshot)`。本层拒绝清理仍存在的数据库记录，协调在途获取/导入，仅清理所属制品、SMB、上传/下载原件及本 ISO 的残留临时文件，本地原件保留。装配点须持久化快照直至幂等清理成功；删除后的崩溃重试队列属于第 5 层，Recover 不承担该队列。

存储接口以代码和实现计划的完整签名为准：`SaveSource(ctx, Source, Audit) (Source,error)`、`DeleteSource(ctx,isoID,role,Audit) error`，Rev 防止取消与旧进度相互覆盖；`SaveRange` 原子更新合并进度并递增 Rev。
依据：`specs/2026-09-30-lober-rewrite-design.md`（下称总 spec）§2.1–§2.2、§4.4、§5.1、§6.1、§6.2 第 6 条、§7.2 第 4 条、§8、附录 A
第 3 层交接：`internal/render/testdata/menu-aux-contract.md`、`internal/render/testdata/runtime-contract.md`

## 1. 目标与范围

把一份安装介质从「登记的来源」变成「可用来建任务的 ISO 记录」：取到字节、认出它是哪个系的哪个版本、解出装机要用的制品、写好清单。

不在本层：

| 事项 | 归谁 |
|---|---|
| 建 ISO 前的磁盘余量检查、进度界面、登记入口的 HTTP 接口 | 第 5 层（调本层导出的能力） |
| 用户、角色、会话、审计的展示 | 第 5 层 |
| 真装机验证（含 Windows 固件分套、ESXi 原生 firstboot、Agama 产品 ID） | 第 7 层 |
| 装配点注入目录、启动时调恢复 | 第 5 层装配点首次出现时 |

推迟是安全的：本层是纯执行，判定规则在第 1 层，持久化在第 2 层，本层只负责把字节变成制品。

## 2. 总体形状

```
前置（第 1 层）：catalog 补四份静态事实
  ├─ 识别线索（格式级 + 系级）
  ├─ 仓库树完整性判据
  ├─ 附加来源要求
  └─ 每种格式必需的制品角色        ← render 也改成读它

子系统 A：来源获取                 子系统 B：导入
  远程链接 / 浏览器上传 /            ISO 9660 解析
  服务器本地路径                     → 识别系/版本/架构/editions
  SSRF 校验 · 断点续传 · 分片        → SHA256 去重（C4）
  进度写仓储                         → 解出制品 · 写清单
                                    → 置可用（C5）

对外：internal/iso 实现 console.ISOSource（接口由控制台声明），另给装配点 Recover 与 Import
第 7 层不直接调本层——它只读仓储里的 ISO 记录
```

**不依赖第 3 层。** 总 spec §6.1 的依赖方向是「ISO 依赖层 1、存储契约里自己用到的能力，以及自己声明的接口」。第 1、2 层已合入，本层从不调用渲染，因此没有需要 mock 的对象。唯一的前置是把第 1 层缺的静态事实补上。

**与第 5 层的接缝按总 spec §6.1 定：接口在调用方一侧。** 早先本文写的「控制台经 `iso.Sources` 调本层」会让 console import iso，与「实现模块之间互不引用」冲突，已按第 5 层规格 `specs/2026-10-11-layer5-console-design.md` §6.1 改过来：`internal/iso` 导出一个具体类型 `Sources`，方法签名在结构上满足 `console.ISOSource`，由 `cmd/lober` 一行 `var _ console.ISOSource = (*iso.Sources)(nil)` 兜住。本层比那个接口多两处：`Register` 与 `Upload` 带 `role`（一个 ISO 可以有多条来源，Debian 的主介质加 netboot），四个写方法带 `a store.Audit`（登记来源要审计，总 spec §6.2 第 8 条）。

## 3. 前置：catalog 补四份数据（第 1 层）

### 3.1 核心原则

判定只有一份。catalog 沿用 `doc.go` 的「只读不判定」，登记静态事实；导入执行它。附录 A 的 `65883a5` 是「仓库判据用错且有三份副本」，本层不许再犯。

### 3.2 识别线索分两级

格式级线索定格式，系级线索在格式内定系：

| 格式 | 格式级线索 |
|---|---|
| kickstart | `.treeinfo` |
| preseed | `.disk/info` + `install.amd/` |
| autoinstall | `.disk/info` + `casper/` |
| autoyast / agama | `content` + `boot/x86_64/loader/` |
| esxi-ks | `boot.cfg` + `mboot.efi` |
| unattend | `sources/install.wim` |

系级线索在格式内区分：kickstart 下按 `.treeinfo` 的 `name=` 分 RHEL、Rocky Linux、AlmaLinux、openEuler、OpenCloudOS；SUSE 下按 `content` 与 `LiveOS/` 的有无分 15 与 16；unattend 下按 install.wim 的 editions 分 Windows Server 与桌面版。

分两级的理由是 **C3 要能区分两种失败**：「认不出任何格式」与「认出了 kickstart 家族，但 `.treeinfo` 里的名字对不上任何内置系」。后者该提示运维这是清单外的重编译版（总 spec §8 接受这个代价），而不是让他反复重导。

### 3.3 仓库树完整性判据

每种格式的仓库根必须包含哪些路径（RHEL 系的 `BaseOS/repodata/repomd.xml`、Debian 的 `dists/<codename>/Release` 等）。

**RHEL 系的仓库路径按 `.treeinfo` 的声明取，不硬编码成 ISO 根**（附录 A `3468d64`：inst.repo 指向包索引目录导致取 stage2 时 404）。取到树根还是 `packagedir` 的上级，用真 ISO 定。

**逐条判据都要有出处。** 计划的任务 0 把七种格式的判据逐条列清，每条指到一个依据（安装器文档、旧系统、或总 spec）；指不出来的标「待真机核实」并记进总 spec §8。没有依据的一律不凭想象编——装机系统里编出来的判据，错了要到真装机才发现。

### 3.4 附加来源

只有一项：Debian 要同版本的官方 netboot 包（`ce9cd56`：netinst ISO 里的 initrd 是光驱版，不能网络装机）。判据是版本与 ISO 一致。

**因此一个 ISO 可以有多条来源**：主介质一条，附加来源各一条（Debian 是两条）。`kernel` 与 `initrd` 两个角色对 Debian 取自 netboot 包，对其余格式取自主介质。netboot 包自己也是 ISO 9660 镜像，复用同一个解析器。每条来源各有自己的期望摘要与进度，ISO 记录的 `SHA256` 始终是主介质的。

### 3.5 必需制品角色

15 个角色按系列一张表（见 §7.1）。**render 也改成读这张表**——现在 `internal/render/menu.go:44-56`、`aux.go:221`、`script.go:198` 各写了一份，正是 `65883a5` 说的三份副本。改动是机械的：三处硬编码切片换成 `catalog.RequiredRoles(series)`，测试跟着改。

### 3.6 依据不足的怎么记

照第 3 层任务 2 处理 Agama 的办法：查得出处的登记，查不出的标「待真机核实」并记进总 spec §8。同时有一条测试盯着——`catalog.seriesTable` 里每个系都必须在介质表里有对应行，缺一个就报红，否则忘了补也没人知道。

## 4. 子系统 A：来源获取

### 4.1 状态机

```
来源状态（存 iso_sources）
  pending → fetching → fetched
                ↘ failed / cancelled

ISO 记录状态（已有，model.ISOState）
  acquiring ────────────→ importing → ready / failed
```

**取消不把 ISO 置为 failed。** `ISOFailed` 的语义是「字节到齐后的校验或导入没通过，可以直接重新导入」，取消不是导入失败。取消后来源停在 `cancelled`、ISO 仍是 `acquiring`，重试时清掉重来。不新增 `ISOState`——它的字面量会存进数据库，发布后不能改名。

### 4.2 三种来源

| 来源 | 落盘 | 续传 |
|---|---|---|
| 远程链接 | `{staging}/{isoID}.part`，边下边算 SHA256 | HTTP `Range: bytes=N-`；服务端回 200 或 416 就从头再来 |
| 浏览器上传 | 按调用方给的偏移写 | 库里记已确认的字节区间，只补缺的；同一段传两次按区间覆盖，不重复计 |
| 服务器本地路径 | 校验在允许目录内后原地读，复制进暂存区 | 无，一次性 |

本地路径必须在装配点注入的允许目录内。文件原地读取，归运维管理，本层不删它（总 spec §5.1 第 2 步）。

分片大小由客户端自己定，服务端按「偏移加长度」记区间，不要求固定大小——客户端换一个分片大小续传也照样成立。`Register` 时只声明总大小。

### 4.3 SSRF 四条规则

1. 只放行 `http` 与 `https`
2. 拒回环（`127.0.0.0/8`、`::1`）
3. 拒链路本地（`169.254.0.0/16`、`fe80::/10`）
4. 按解析出的 IP 直连，每一跳重定向重新过 1–3

**私有网段刻意放行。** 气隙数据中心的 ISO 镜像就在内网，拒了私有段这条主要路径就废了；总 spec 的字面也只禁回环与链路本地。

「按解析出的 IP 直连」要自己控 DNS：解析后逐个 IP 过规则 2–3，全部通过才拨号；`https` 用解析出的 IP 建连但把 SNI 留给原主机名（`crypto/tls` 的自定义 `DialTLSContext`）。

### 4.4 断点续传的两个用例

总 spec §7.2 第 4 条点名：

- 中途杀进程：重启后按库里记的偏移接着下
- **库里记的进度比盘上多**：以盘为准——文件比记的偏移短就截断到实际大小，再按这个偏移续

### 4.5 磁盘余量

总 spec §5.1 第 1 步「能事先知道大小的，先查磁盘余量，不够就拒绝，并说明还差多少」归第 5 层，但它要知道暂存区与制品目录的余量。本层导出 `FreeSpace(ctx) (staging, artifacts int64, err error)`，第 5 层调它。

## 5. 子系统 B：导入

### 5.1 流程

```
字节到齐（一个 ISO 的全部来源都 fetched）
 → 算主介质的 SHA256
 → 填了期望值却对不上 ────────────────→ failed（C3）
 → 已有未失败的 ISO 占着这个摘要 ─────→ failed，指向对方 ID（C4）
 → ISO 9660 解析成 fs.FS 视图
 → 识别：格式 → 系 → 版本 → 架构 →（Windows）editions
 → 与声明的系不符 ───────────────────→ failed，同时给声明值和识别值（C3）
 → 附加来源不齐，或它的版本与识别出的版本不一致 ──→ failed（C3）
 → 解出制品到 {artifacts}/{series}/{version}/{arch}/.tmp-{isoID}/
 → 按 §3.3 的判据验仓库树完整
 → 写 Artifacts（角色 → 相对路径），逐个核对文件真实存在
 → rename .tmp → 正式目录        （原子；本层唯一改制品目录的地方）
 → UpdateISO(State=ready)，记录必须过 ISO.Validate，由仓储兜底（C5）
```

附加来源的「版本一致」要等主介质识别出版本才能判，所以排在第 5 步之后；「不齐」（没登记、没取到）在取字节阶段就已拦下，到这一步只会是版本不符。

导入期间用户在控制台废弃了这个 ISO：`UpdateISO` 对已废弃的记录报 `ErrImmutable`，导入作废并清掉临时目录，不报成导入失败——废弃是要审计的决定，不是内容问题。

### 5.2 原子性与重启恢复

解到 `.tmp-{isoID}`，完成后整体 `rename`。重启后扫暂存区，残留的 `.tmp-*` 一律删掉重来。

**导入进度不落库，恢复就是重跑。** 解压是本地盘操作，重来的代价远小于维护一套导入进度状态机，也不会出现「进度记着但盘上对不上」这类问题。

### 5.3 识别出的版本与架构

版本合 `^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$`（`model.ISO.Validate` 已限，它会成为制品目录里的一段路径）。架构按格式取：`.treeinfo` 的 `arch=`、SUSE 的 `boot/<arch>/`、Windows 的 install.wim XML。**架构必须写**——附录 A `81339e0` 是导入不写架构、第一次引导就被拒。

### 5.4 Windows editions

自己解析 `install.wim`：WIM 头部 → 定位 XML 资源 → 解压 → 读 `<WIM><IMAGE INDEX><NAME>/<FLAGS>`。

只支持未压缩与 XPRESS 的 XML 资源，**遇 LZX 明确失败并说明**，不引第三方库。editions 的取值用 `FLAGS` 里的安全词（`ServerDatacenter` 这类），全名进 `Label`——`model.ISO.Offers` 的规则要求值原样进正文、必须是安全词（T7-5、C-8）。

### 5.5 Windows 制品

装配点注入工具目录；导入时把 `wimboot`、`winpe-busybox`、`winpe-curl` 复制进制品集。**缺哪个就导入失败并指明缺哪个**，不静默跳过——否则要到菜单渲染时才发现，那是装机现场。

`winpeshl` 由本层写一份真的 `[LaunchApps]` ini 指向 `startnet.cmd`（第 3 层契约要求它是真文件，不是虚构端点）。安装源另复制进注入的 SMB 目录。

### 5.6 ESXi

解出完整树（`esxi-tree`，含原始 `boot.cfg`）与 `mboot`。**保留原始 `boot.cfg` 的字节**——第 3 层的 `FileAux` 要调用方提供原始内容，本层是它的出处。纯 ISO 9660 文件名要小写化，否则引导器与内核抽取出错（附录 A `41fe0ee`）。

## 6. 存储契约扩展（第 2 层）

```go
// internal/store，新增
type ISOSources interface {
    SaveSource(context.Context, Source, Audit) (Source, error)
    GetSource(context.Context, string, string) (Source, bool, error)
    SourcesOf(context.Context, string) ([]Source, error)
    ListUnfinishedSources(context.Context) ([]Source, error) // pending/fetching
    DeleteSource(context.Context, string, string, Audit) error
    SaveRange(context.Context, string, string, int64, int64) error
    RangesOf(context.Context, string, string) ([]Range, error)
}
```

`Source` 由 `(isoID, role)` 定键：主介质的 role 是空串，附加来源用 catalog 的附加来源要求给的名字（Debian 是 netboot）。每条来源各自记 `Kind`、地址、期望摘要、已确认字节数与状态，互不影响。

`Source` 的条件更新要防下载协程与控制台的取消互相覆盖（如取消与进度写入撞车）：更新时带上期望的状态，对不上就报冲突，由调用方重读后再决断。

两张新表进 `schema.go` 的表结构断言——第 2 层的规矩是索引和约束一次定对，由每次启动时的断言守着。

进度双写：字节进度在 `iso_sources`，ISO 记录的 `State` 镜像粗状态（全部来源 fetched 才算字节到齐）。第 5 层两个都读，契约里都要有读法。

## 7. 制品目录布局与 15 个角色

布局：`{artifacts}/{series}/{version}/{arch}/…`。三段都是已验证安全的标识（系是目录里的固定标识、版本过 `ISO.Validate`、架构是固定取值），不会目录穿越。

目录角色以 `/` 结尾。相对路径的合规（非空、无前导 `/`、无 `.` 与 `..` 段）由本层写入时保证——`catalog.ArtifactURL` 只拼不查（转义挡不住 `%2E%2E`），这一层漏了查，渲染出的地址就能穿出制品目录。

### 7.1 每个系必需的制品角色

下表就是 §3.5 要登记的那张表，逐格照第 3 层现有代码抄出，不是新设计的：

| 系 | 必需角色 |
|---|---|
| rhel、rocky、almalinux、openeuler、opencloudos | `kernel`、`initrd`、`stage2`、`repo` |
| debian | `kernel`、`initrd` |
| ubuntu | `kernel`、`initrd`、`iso` |
| sles15、leap15 | `kernel`、`initrd`、`repo` |
| sles16、leap16 | `kernel`、`initrd`、`live-root`、`repo` |
| esxi | `mboot`、`esxi-tree` |
| windows-server、windows-desktop | `wimboot`、`bcd`、`boot-sdi`、`boot-wim`、`winpeshl`、`winpe-busybox`、`winpe-curl` |

导入解出制品后，按这张表逐个核对文件真实存在（第 3 层契约：「The importer must validate real existence/completeness」）；缺一个就导入失败并指明缺哪个。render 那三处硬编码切片换成读这张表。

### 7.2 角色从哪来

| 角色 | 来源 | 消费方 |
|---|---|---|
| `kernel` `initrd` | ISO 解出；Debian 取自 netboot 包 | 五种 Linux 菜单 |
| `stage2` | RHEL 系解出的完整安装树 | kickstart `inst.stage2` |
| `repo` | 仓库根，路径按 `.treeinfo` 声明（`3468d64`） | kickstart / autoyast / agama |
| `iso` | Ubuntu 原始 ISO 整文件复制 | Ubuntu `url=` |
| `live-root` | Agama 的 `LiveOS/squashfs.img` | Agama 菜单 |
| `mboot` `esxi-tree` | ESXi 的 `efi/boot/bootx64.efi` 与完整解出树（含原始 `boot.cfg`） | ESXi 菜单与 boot.cfg |
| `wimboot` `bcd` `boot-sdi` `boot-wim` | 工具目录 + ISO 的 `boot/`、`sources/` | Windows 菜单 |
| `winpeshl` | 本层生成的真 ini | Windows 菜单 |
| `winpe-busybox` `winpe-curl` | 工具目录 | Windows 菜单、附属文件、运行脚本 |

## 8. 包与文件

| 包 | 职责 | 依赖 |
|---|---|---|
| `internal/core/catalog` | 补 `media.go`：识别线索、仓库判据、附加来源、必需角色 | 只读不判定，层 1 叶子 |
| `internal/render` | 三处硬编码角色表改读 catalog | 层 1 |
| `internal/store` | 契约加 `ISOSources` | 层 1 |
| `internal/store/pgstore` | 实现 `ISOSources`，加两张表 | 层 1、store、gorm、pgx |
| `internal/iso` | 来源获取 + 导入编排 | 层 1、store 契约、标准库 |
| `internal/iso/iso9660` | ISO 9660 只读解析：`io.ReaderAt` → `fs.FS` | 仅标准库 |
| `internal/archtest` | 加「ISO」层规则 | — |

`internal/iso/iso9660` 单独成包：它是唯一要在真镜像上活下来的部件（附录 A `dab857b`：旧系统的解析器在真实镜像上必然失败），隔离出来才能用真 ISO 单测它，也不用把 lober 的概念带进解析器。

**新包必须同时给守卫加规则**（第 3 层定下的，见第 3 层计划「留给后续层的约定」）：守卫全模块列包，归不到任何层的包一律报违规。常量与引用它的用例必须在同一次改动里一起加。

## 9. 错误处理

内容问题一律走 `model.Problem`，用第 1 层的原因代码体系，需要新代码时追加到 `extraCodes` 末尾并同步 `codes_align_test`；`error` 只用于调用方用错接口。每个失败的说明都要能指到「缺什么、下一步怎么办」（总 spec §3 总则），不做静默降级。

## 10. 测试策略

| 层 | 手段 |
|---|---|
| `iso9660` | 合成 ISO 9660 镜像：Rock Ridge 与 Joliet、目录记录跨扇区、纯 ISO 9660 小写化（`41fe0ee`）。**真 ISO 用例由环境变量开关，未设则跳过** |
| 识别 | 合成目录树七种格式各一套；真 Debian netinst 不被认成 Ubuntu（`131d659`）由真 ISO 用例钉 |
| 来源获取 | SSRF 每条规则一个用例；续传两个用例（杀进程、库里比盘上多）；同一段重复上传只算一次、只补缺的区间 |
| 导入 | 七种格式各一套合成镜像，外加边界：缺特征文件、版本与声明系不符、附加来源版本不符、制品缺角色、摘要撞车（C4） |
| 存储 | 契约测试连测试环境 PostgreSQL，找不到连接串就失败不跳过（总 spec §6.4） |
| 真机 | 归第 7 层。本机 QEMU 在 WHPX 下只能 `-cpu qemu64`（总 spec §8） |

真 ISO 的开关照旧系统的 `LOBER_TEST_REAL_ISO`：设了变量就跑，没设就跳过。与 PostgreSQL 的 DSN 不同——那边没有库就等于存储层零覆盖，必须失败；这边只是缺一份几 GB 的本地镜像，其余导入测试仍覆盖着解析与抽取的逻辑分支。本机当前只有 `Downloads/kali-linux-2026.2-installer-amd64.iso` 与 `/d/VMware/windows.iso`，且 Kali 不在系的清单里（C1），只能验 ISO 9660 解析，验不了系识别。

## 11. 风险与待定项（要同步进总 spec §8）

| 风险 | 处置 |
|---|---|
| Windows 固件分套与 render 角色不分固件冲突：旧 SPEC §2.1 要求「同一 Windows 版本导两个制品集（uefi/bios），boot.wim 位宽不同」，但新 render 的 Windows 角色没有固件分支，`model.ISO` 也没有 Firmware 字段。本层只能出一套 | 记「待真机核实」，第 7 层验证；真机确认需要两套时，回到第 1 层给 ISO 加固件维度 |
| 识别规则的确切依据：七种格式的特征文件有旧系统与安装器文档可依，但 Rocky/Alma/openEuler/OpenCloudOS 之间靠 `.treeinfo` 的 `name=` 区分这一点，要真 ISO 才能坐实 | 标「待真机核实」，由 §10 的真 ISO 用例坐实 |
| **ESXi 的版本在 ISO 里取不到**：`boot.cfg` 只有 `title=Loading ESXi installer` 这类字样，不带版本号；旧系统也从来没从内容里取到过它。而 `model.ISO.Validate` 要求 ready 的 ISO 必须带合法版本（C5） | **首版 ESXi 导不出 ready 的 ISO**：识别之后明确报 `iso_version_missing`，不猜取法——猜错的版本会成为制品目录里的一段路径，错了要到真装机才发现。候选的坐实路径：`imgpayld.tgz` 里的版本文件，或 `s.v00` 模块里的构建串，第 7 层真机时定 |
| WIM XML 资源的 LZX 压缩不在首版支持内 | 遇 LZX 明确失败并说明，不引第三方库 |
| Agama 的 `live-root` 与仓库取法依据 Agama 文档，未真装验证 | 记「待真机核实」，第 7 层验证 |
| ESXi 的原生 firstboot 与 Secure Boot（总 spec §8 已有条目） | 沿用，第 7 层验证 |

## 12. 留给后续层的约定

- **第 5 层**：接口由控制台声明、本层实现（总 spec §6.1），`internal/iso` 不 import console。控制台经它登记来源、上传分片、查进度、取消、重试、重新导入，自己不碰字节；建 ISO 前调 `FreeSpace` 查余量（总 spec §5.1 第 1 步）。进度只读，不自己写。
- **第 5 层装配点**：注入暂存目录、制品目录、SMB 目录、Windows 工具目录、允许登记的本地目录；启动时调 `Recover` 拿到要恢复的清单，把每件事分别放进看护协程（总 spec §6.2 第 6 条、§6.3）。`Recover` 只遍历并返回清单，不起协程——起协程归装配点。
- **第 7 层**：网关只读仓储里的 ISO 记录与 `ISO.Artifacts`，不调本层。真装后确认 Windows 是否需要按固件分两套制品，需要就回到第 1 层。
- **任何一层要改识别规则或仓库判据**：只改 catalog 一处。它们是随程序发布的静态事实（C1），改了要发新版本。
