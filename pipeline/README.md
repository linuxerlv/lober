# 镜像封装流水线（外网侧）

> 对应 phase3-03 计划任务 2。这是「两半计划」（SPEC §11 Phase 3）里跑在外网构建机上
> 的那一半；内网侧的镜像直写通道（任务 4）与这一半通过本文档定义的四件套交付物对接，
> 彼此不需要知道对方的实现细节——**只要遵守这份契约，两侧就能各自开发、各自验证**。
>
> **本文档不是可执行脚本，是接口定义 + 工具能力核实结论。** 本仓库当前 worktree
> 里没有安装 packer / mkosi / cosign（已用 `command -v` 核对，见第 6 节「交付前
> 必跑」），所以任务 2 交付的是构建定义文件（可静态核对语法）与本 README，
> 不是一次实际跑通的 CI 记录。`scripts/package.sh` 是例外——它不依赖 packer/mkosi，
> 本轮已用假数据实际跑通并验证其逻辑正确（见第 6 节）。**部署前必须在有对应
> 工具的机器上按第 6 节逐条实测剩余部分。**

## 目录里有什么

```
pipeline/
├── README.md                          本文档
├── linux/
│   └── mkosi.conf.d/
│       └── rocky9.conf                mkosi 封装定义（Rocky Linux 9，见下方核实结论）
├── windows/
│   └── windows-server.pkr.hcl         Packer 模板骨架（vsphere-iso builder，必须引导真 installer）
├── esxi/
│   └── esxi.pkr.hcl                   Packer 模板骨架（qemu builder，同样引导真 installer，见第 1 节）
├── manifest/
│   └── manifest.schema.json           清单文件的字段定义（交付物第 2 项的结构）
└── scripts/
    └── package.sh                     把 mkosi/Packer 的产物打成四件套交付物的封装脚本
```

**明确不在这里的东西：** `pipeline/sign.sh` 与 `internal/imports/signature.go` 是计划
任务 3 的产出，本任务不写、不占位。本 README 第 4 节描述签名接口是「任务 2 需要向任务 3
交代清楚的契约」，具体签名脚本的实现属于任务 3。

## 1. 工具边界：哪个工具对应哪类 OS（附录 B 待验证项，本轮核实结论）

计划任务 2 明确要求先核实工具能力，再写构建定义。以下每条结论都标注了核实方式与来源，
**核实不到的如实标注为「未核实」，不凭印象写。**

| OS 家族 | 工具 | 核实结论 | 核实方式 | 来源 |
|---|---|---|---|---|
| Linux（RHEL 系/Fedora 系） | osbuild-composer | **官方明确只支持 Fedora 与 RHEL**（README 原文："osbuild-composer is a service for building customized operating system images (currently only Fedora and RHEL)"） | WebFetch 官方仓库 README + WebSearch 交叉确认 | [osbuild/osbuild-composer README](https://github.com/osbuild/osbuild-composer) 、[osbuild.org 概览页](https://osbuild.org/docs/on-premises/overview/) |
| Linux（跨发行版族） | mkosi | **官方 `Distribution=` 选项支持 `fedora, debian, kali, ubuntu, arch, opensuse, mageia, centos, rhel, rhel-ubi, openmandriva, rocky, alma, azure` 十三种命名发行版加 `custom` 兜底**——覆盖面比 osbuild-composer 广，且同时覆盖 RHEL 系与 Debian 系。**约束：mkosi 生成的镜像是「legacy-free」——只支持 GPT 分区表，不支持 MBR/BIOS 传统引导；bootable 镜像只支持 EFI 系统** | curl 拉取 mkosi 官方 man page 源文件原文核对 `Distribution=` 选项的取值列表与 legacy-free 声明 | [systemd/mkosi mkosi.1.md](https://github.com/systemd/mkosi/blob/main/mkosi/resources/man/mkosi.1.md) 、[systemd/mkosi distribution-policy.md](https://github.com/systemd/mkosi/blob/main/docs/distribution-policy.md) |
| Windows | Packer（`vsphere-iso` builder） | **确认「必须引导真 installer」，没有替代路径**：Packer 的 vSphere ISO builder "starts from a guest operating system ISO file and builds a virtual machine image on a vSphere cluster or an ESX host using the vSphere API"——本质是自动化一次真实的图形/命令行安装过程（挂 ISO、发送按键或应答文件、等待安装完成、关机、导出）,不是解压/改写现成镜像。适用于「在 vCenter 里装一台 guest VM」的场景 | WebSearch 命中官方文档页 + curl 核对 VMware 官方 packer-examples-for-vsphere 仓库的真实构建定义 | [HashiCorp vSphere-ISO builder 文档](https://developer.hashicorp.com/packer/integrations/vmware/vsphere/latest/components/builder/vsphere-iso) 、[VMware packer-examples-for-vsphere](https://github.com/vmware/packer-examples-for-vsphere) |
| ESXi | Packer（`qemu` builder，**不是** `vsphere-iso`） | **ESXi 本身是 hypervisor，不是要装进 vCenter 的 guest VM**——`vsphere-iso` 面向的场景（在已有 vCenter 里装一台 guest）与「封装一份 ESXi 安装介质」的目标不匹配。改用 `qemu` builder 在虚拟机里跑一次真实的 ESXi kickstart 安装，直接产出 raw 格式的磁盘镜像文件。**这不是本任务自行设计的**：Canonical MAAS 项目的官方 `packer-maas` 仓库用完全相同的思路（qemu builder + USB 注入 kickstart + raw 输出）封装 ESXi 供 MAAS 装机用，与 Lober 的目标场景高度一致 | curl 直接拉取 canonical/packer-maas 仓库的 `vmware-esxi/vmware-esxi.pkr.hcl` 与 `KS.CFG` 源文件核对字段 | [canonical/packer-maas](https://github.com/canonical/packer-maas/blob/main/vmware-esxi/vmware-esxi.pkr.hcl) |
| 增量定制（已有镜像上改） | virt-customize（libguestfs 套件） | **确认是离线、原地修改已有磁盘镜像**（装包、写文件、跑脚本），不重新走安装流程。官方文档原话：`virt-customize` "modifies the guest or disk image in place without making a copy of it"——**这意味着调用前必须自己复制一份镜像**，否则改坏了没有回退 | WebSearch 命中官方 man page + Red Hat 文档交叉确认 | [libguestfs virt-customize(1)](https://www.libguestfs.org/virt-customize.1.html) 、[Red Hat 虚拟化文档](https://access.redhat.com/documentation/en-US/Red_Hat_Enterprise_Linux/7/html/Virtualization_Deployment_and_Administration_Guide/sect-Guest_virtual_machine_disk_access_with_offline_tools-Using_virt_customize.html) |

**本仓库选择：Linux 封装用 mkosi，不用 osbuild-composer。** 理由：osbuild-composer 官方
只覆盖 Fedora/RHEL 两家，而 SPEC §2.1 的 `os_family` 枚举含 `rhel | debian | ubuntu | suse`
四家，mkosi 的十三种命名发行版覆盖了其中三家（`rhel`/`debian`/`ubuntu`），只有 `suse`
系（`opensuse` 虽在列，但 SPEC 的 `suse` 具体指哪个变体未定）需要在实际接入时再核实一次。
`linux/mkosi.conf.d/rocky9.conf` 用 `rocky` 作为示例（对应 SPEC §11 Phase 1 已选定的
Rocky 9），验证了这条链路后，切到 `debian` / `ubuntu` 只需换 `Distribution=` 与
`Release=` 两个字段，构建定义结构不变。

**仍未核实、需在目标环境补做的项（进 openQuestions）：**
- SPEC 的 `suse` 家族与 mkosi 的 `opensuse` 命名是否指同一 OS、版本对应关系如何——
  本轮未找到权威交叉来源，需在有网络的环境用 `mkosi --distribution=opensuse --release=<版本>` 实测。
- mkosi 对 `windows` / `esxi` 完全不适用（这两家从未出现在其 `Distribution=` 选项里），
  与计划任务 2 的表格结论一致，此处不是新发现，只是确认没有反例。

## 2. 交付物格式：四件套

**镜像直写通道要的不是「一个文件」，是「一个自描述的目录」**——因为写盘端（任务 4）
需要在下载前就知道校验值与分区布局，不能靠约定文件名猜测，必须有清单显式声明。

```
<artifact_set_name>/                    对应 model.ArtifactSet 里镜像直写通道的一条记录
├── image.raw.zst                        1. 镜像文件（压缩后）
├── manifest.yaml                        2. 清单文件（sha256 + 分区布局描述）
├── manifest.yaml.sig                    3. 清单的签名（对清单签，不对镜像签，见第 4 节）
└── meta.yaml                            4. 元信息（家族/版本/架构/固件/封装时间/源 ISO sha256）
```

### 2.1 镜像文件：`image.raw.zst`

**格式：原始块设备镜像（raw disk image），用 zstd 压缩。** 不用 qcow2/vmdk 等带
元数据层的虚拟机镜像格式——写盘端要做的是「流式解压直接写到目标物理盘」（计划任务 4
的第 2 步），raw 格式的字节序列本身就是目标盘的最终内容，不需要额外一层格式解析；
qcow2 之类的格式在写盘前还要先转换（`qemu-img convert`），那一步本身就要落地临时
文件，与任务 4「不落地」的要求冲突。

**压缩用 zstd，不用 gzip/xz。** SPEC §11 Phase 3 明确写了 zstd，理由是解压速度：
镜像直写的瓶颈在写盘与解压（任务 4 第 2 步是流式解压边写盘），zstd 在同压缩率下
解压速度显著高于 gzip/xz——这条本轮未重新核实（zstd 官方的速度对比数据是长期公开
且广为引用的基准，SPEC 已经采信，本任务不重复验证同一个已有结论）。**本机已确认
装有 `zstd` 命令**（`/d/msys/ucrt64/bin/zstd`），`scripts/package.sh` 直接调用它。

### 2.2 清单文件：`manifest.yaml`

结构定义见 `manifest/manifest.schema.json`。核心字段对应计划任务 1 新增的镜像直写
制品集三项（镜像文件路径由目录结构隐含，不重复记在清单里）：

| 字段 | 对应 SPEC/计划 | 说明 |
|---|---|---|
| `image_sha256` | 计划任务 1 的「镜像的 sha256」 | 对 `image.raw.zst`（压缩后的文件）算的 sha256，不是解压后镜像的 sha256——因为任务 4 第 3 步的回读校验比对的是写到盘上的最终字节，那份校验值应该来自解压**之后**、写盘**过程中**动态算的 sha256，与本字段（压缩文件的 sha256，用于导入时完整性校验，即本 README 第 4 节的校验对象）是两个不同层次的校验，不要混用同一个字段 |
| `image_uncompressed_sha256` | 任务 4 第 3 步的回读校验值 | 解压后原始镜像的 sha256，写盘端流式解压时边算边比对，写完后与此值核对 |
| `partition_layout` | 计划任务 1 的「分区布局描述」 | 镜像内部的分区结构（哪个分区是根分区、用什么文件系统），供任务 4 第 4 步「按清单里的分区布局描述扩展分区」使用。**具体 schema 由任务 4 的实现者定义**（本任务只留字段位置，不预先设计扩展分区算法要用的具体数据形状——那是运行时消费者的关注点，提前设计容易设计错） |
| `package_list_sha256` | 计划任务 2 的可重现性验证 | 清单内软件包列表算出的 sha256（不是镜像文件本身的 sha256），两次独立封装比对这个值判断「可重现」——见第 5 节 |
| `image_size_bytes` | 供任务 4 预估下载/写盘时间、供任务 5 的耗时对比记录 | 压缩后文件大小 |

**为什么分成压缩态与解压态两个 sha256：** 任务 3 的导入时校验（README 第 4 节）
校验的是「传输到内网的文件没有被篡改」——对象是 `image.raw.zst` 本身；任务 4 的
写盘回读校验（计划任务 4 第 3 步）校验的是「写到盘上的字节没有出错」——对象是
解压后的原始镜像。**这是两个不同阶段、不同目的的校验，即使都叫 sha256 也不能共用
同一个值**，否则任务 3 的校验会因为「压缩算法版本不同导致压缩文件字节不同但解压
内容相同」这类场景产生误报，或者反过来在真正的传输损坏时因为比对了错误的值而漏检。

### 2.3 清单的签名：`manifest.yaml.sig`

见第 4 节。此处只确认接口：`sign.sh`（任务 3）读 `manifest.yaml`，输出
`manifest.yaml.sig`；导入时（任务 3 的 `signature.go`）读这两个文件加网关预置的
公钥做校验。**签名对象是清单，不是镜像**——理由见第 4 节。

### 2.4 元信息：`meta.yaml`

```yaml
os_family: rocky            # 对应 model.ArtifactSet.os_family
os_version: "9.4"           # 对应 model.ArtifactSet.os_version
arch: x86_64                # 对应 model.ArtifactSet.arch
firmware: any                # 对应 model.ArtifactSet.firmware（rhel 系通常是 any，见 SPEC §2.1）
packaged_at: "2026-09-21T00:00:00Z"   # 封装完成时间，ISO 8601
source_iso_sha256: "PLACEHOLDER_SHA256_OF_SOURCE_ISO"   # 见下方"源 ISO 校验值"
packaging_tool: "mkosi"     # 用哪个工具封装的，排查问题时第一个要看的字段
packaging_tool_version: "PLACEHOLDER_TOOL_VERSION"       # 工具版本，同上
```

**`source_iso_sha256` 是可追溯性的关键字段**（计划任务 2 原文：「这个镜像是从哪个
ISO 封装的」在排查时是第一个问题）——与 SPEC §2.1 的 `source_iso_sha256` 字段同名
对应，语义完全一致：都是「原始 ISO 校验值，留痕」。

## 3. 气隙约束：三条，逐条落实

**封装必须在有外网的机器上做，产物带进内网。** 气隙内网装不上构建工具的依赖
（与前端构建同理，SPEC §12.3）——mkosi/Packer 都需要联网拉取发行版官方仓库的包
或安装介质，这个假设不需要在内网重新验证一次，因为内网的定义就是「不能联网」。

### 3.1 cosign 的 keyless 模式在气隙下走不通

**这是本节最重要的一条，SPEC §9.6 的标题就是结论。** cosign 的 keyless 签名模式
依赖两个外部服务：
- **Fulcio**：签发短期证书，需要外网 OIDC（登录 Google/GitHub 账号换证书）
- **Rekor**：透明日志服务，签名时默认会把记录上传到这个公开日志

**气隙环境两者均不可达。** 必须用 **key-based** 签名：私钥对清单签名，公钥预置
在网关配置里（不从网络取）。**不要指望默认流程能跑**——这是 SPEC §9.6 的原话，
本轮核实进一步确认了这句话为什么现在比写 SPEC 时更真：

**本轮核实发现：cosign 当前版本（v3.0.x）的命令行示例已经全面转向 `--bundle`
这个单一 JSON 文件格式，而不是传统的「裸签名文件 + 公钥文件」两文件模式。**
`cosign verify-blob` 的官方示例列表里，**六个示例全部带 `--bundle`**，`--key`
只出现在 flag 说明里、不出现在任何一条示例命令中——照抄文档最上面的示例会先卡在
「以为必须先跑 keyless 流程生成 bundle」这一步。**实际上 `--key`（公钥文件路径）
与 `--insecure-ignore-tlog`（跳过透明日志校验）两个 flag 仍然存在且受支持**
（本轮从 cosign 官方 CHANGELOG 核实到 v3.0.4 版本的变更记录明确写了
"Don't require a trusted root to verify offline with a key"——即官方仍在积极
维护「离线 + key-based」这条路径，只是不再是文档首推的示例），组合使用即可绕开
keyless 流程，具体命令见第 4 节。

来源：[cosign sign-blob 文档](https://github.com/sigstore/cosign/blob/main/doc/cosign_sign-blob.md)、
[cosign verify-blob 文档](https://github.com/sigstore/cosign/blob/main/doc/cosign_verify-blob.md)、
[cosign CHANGELOG](https://github.com/sigstore/cosign/blob/main/CHANGELOG.md)（v3.0.4 条目）。

### 3.2 私钥的存放（任务 3 的操作性问题，本任务只记录接口影响）

私钥用于 `sign.sh`（任务 3），只需要在构建机上存在——**构建机本身就是有外网的机器，
且已经承担「产出被系统信任的镜像」这个信任根的角色，私钥放在这里不扩大攻击面**。
不放开发者机器（分散的私钥副本无法审计谁签过什么），不放内网（内网只做校验，
不需要签名能力）。具体的密钥管理（谁能访问构建机、私钥是否需要 HSM）是任务 3
与部署运维要回答的问题，本任务只确认：**签名发生在外网构建机上，是本流水线的
最后一步**，不是内网侧的动作。

### 3.3 交付流程（人工步骤，本任务不写自动化脚本，只描述顺序）

```
外网构建机：
  1. mkosi/Packer 封装出原始镜像
  2. scripts/package.sh 打包成四件套（image.raw.zst + manifest.yaml + meta.yaml）
  3. sign.sh（任务 3 产出）对 manifest.yaml 签名，产出 manifest.yaml.sig
  4. 整个目录拷出（U 盘/单向传输设备/审计过的文件传输通道——具体方式不在本任务范围）

内网：
  5. 导入（任务 3 的 signature.go）校验签名 + sha256，写入 /srv/lober/artifacts/<id>/
  6. 之后的写盘（任务 4）只信任已导入的本地文件，不再联网校验
```

## 4. 与已有通道的分工（SPEC §11 Phase 3）

**两条通道并存，不是替代关系：**

| 通道 | 定位 | 覆盖场景 | 机制 |
|---|---|---|---|
| **installer 驱动**（已有） | **兜底** | 新 OS 版本发布当天（镜像还没封装出来）、特殊机型的驱动问题（要在 installer 里现场调）、装机失败需要停在 shell 里诊断（§2.3 失败处置策略） | 引导真实 installer，installer 读应答文件（kickstart/preseed/autoinstall 等），全程可人工介入 |
| **镜像直写**（本任务定义外网侧） | **主力** | 批量装机、要求「装出来的系统逐台一致」 | 引导一个最小环境（不是 installer），流式写入预先封装好的镜像 |

**为什么兜底通道不能被镜像直写取代：**
1. 新 OS 版本发布时，镜像还没封装好——installer 驱动不需要等封装流水线跑完
2. 特殊机型的网卡/存储驱动问题，往往要在 installer 的 shell 里现场加驱动、试出
   正确参数——镜像直写的环境里没有 installer 的诊断工具，出问题时只能看着一个
   卡住的写盘进度条
3. 装机失败需要停下来看现场——installer 通常提供一个可交互的 shell，镜像直写
   环境是为了「跑一次流程就应该结束」设计的最小环境，不是为了停下来调试设计的

**这三个场景都是「少数但关键」**——它们出现时若只有镜像直写通道，运维就只能手工
装机。所以 SPEC 与计划都强调：**加镜像直写不是为了删掉 installer 驱动，是为了让
批量场景不必逐台走一次完整的 installer 流程。**

## 5. 可重现性：验证方法

**要求：** 同一份输入（ISO 加定制脚本）两次封装应产出「清单里的软件包列表相同」的
镜像。**不要求字节级相同**——时间戳、随机生成的机器 id（如 `/etc/machine-id`）在
两次封装之间必然不同，这是正常的，不是 bug。

**验证做法：** `manifest.yaml` 的 `package_list_sha256` 字段是对「软件包名+版本」
排序后拼接算出的 sha256（不是对整个清单文件算，因为清单里还含时间戳等预期会变化
的字段）。两次独立封装后比较这一个字段——相等即通过。

## 6. 交付前必跑（本轮因缺少工具无法执行，需在目标主机补做）

**本 worktree 环境是 Windows，且未安装 packer / mkosi / cosign**（已用
`command -v packer / mkosi / cosign` 核对，均未找到；只有 `zstd` 可用）。以下
每条在部署前必须在装有对应工具的 Linux 构建机上补做：

```bash
# 1. mkosi 语法与依赖检查（不需要实际跑完封装，--dry-run 或等价的语法检查即可）
cd pipeline/linux && mkosi --directory=. summary

# 2. 实际封装一次 Rocky 9，产出应在 mkosi.output/ 下
cd pipeline/linux && mkosi --directory=. build

# 3. 跑封装打包脚本，确认产出四件套（除签名外）且内容符合本文档第 2 节。
#    scripts/package.sh 的完整参数说明见 `bash pipeline/scripts/package.sh --help`——
#    本轮已在当前 Windows 环境用假数据（dd 生成的随机文件模拟镜像）实际跑通这一步，
#    确认脚本本身的逻辑正确（sha256 长度、包列表排序、参数校验），
#    但输入必须换成 mkosi/Packer 真实产出的镜像文件：
bash pipeline/scripts/package.sh \
  --image <mkosi/Packer 产出的 raw 镜像路径> \
  --os-family rocky --os-version 9.4 --arch x86_64 --firmware any \
  --source-iso <原始 ISO 路径> --tool mkosi --tool-version <mkosi --version 输出> \
  --packages-file <软件包清单文件> --output <输出目标目录>

# 4. 校验 zstd 压缩产物能正确解压且与原文件字节一致（本轮已用假数据验证此命令本身
#    有效：解压后 diff 原文件字节一致）
zstd -d --test <输出目标目录>/image.raw.zst

# 5. 两次独立封装（建议间隔跑，或用两台构建机），比对 package_list_sha256 字段。
#    本轮已用假数据验证「包列表顺序不同但两次的 sha256 相同」这一核心逻辑成立
#    （grep 手动比对即可，不依赖 yq——本环境未装 yq，若目标构建机装了 yq 也可以
#    用 `yq '.package_list_sha256' manifest.yaml` 代替下面的 grep）：
diff <(grep '^package_list_sha256:' 第一次产出/manifest.yaml) \
     <(grep '^package_list_sha256:' 第二次产出/manifest.yaml)

# 6. Packer 模板语法检查——两个模板用不同插件，需分别 packer init 拉插件：
#    windows-server.pkr.hcl 需要 packer-plugin-vsphere（对接已有 vCenter，
#    在其中装一台 Windows guest VM）；esxi.pkr.hcl 需要 packer-plugin-qemu
#    （本地 KVM 虚拟化，不依赖任何已有 vCenter/ESXi 环境——见本文档第 1 节
#    ESXi 行的核实结论：封装 ESXi 本身不能用面向「装 guest VM」的 vsphere-iso，
#    两个模板因此不共用同一个插件）。
cd pipeline/windows && packer init . && packer validate .
cd pipeline/esxi && packer init . && packer validate .
```

**如果第 5 步 diff 有输出（即两次结果不同），说明封装过程引入了非预期的随机性
（例如仓库镜像在两次封装之间更新了、或定制脚本本身含随机逻辑），需要先定位差异
来源再继续——这是计划任务 2 明确的验证要求，不能跳过。**

## openQuestions（需实测或需编排方决策）

1. SPEC 的 `os_family: suse` 与 mkosi 的 `Distribution=opensuse` 是否指同一 OS、
   版本号如何对应——本轮未找到权威交叉来源，需在有网络环境实测
   `mkosi --distribution=opensuse --release=<版本> summary` 确认可行。
2. `manifest.yaml` 里 `partition_layout` 字段的具体 schema（分区如何描述、扩展
   根分区的算法需要哪些输入）留给任务 4 的实现者定义——本任务只留字段位置，
   若任务 4 已经在其它分支上开始实现，需要和该实现对齐字段名。
3. `linux/mkosi.conf.d/rocky9.conf` 里 `Bootloader=none`：本任务假设写盘端
   （任务 4）会在写盘后自行处理引导修复/bootloader 安装，mkosi 侧不需要产出
   一个「开机即可自举」的完整镜像。**这个假设需要与任务 4 的实现者确认**——
   若任务 4 期望拿到的镜像本身已经可自举（不需要写盘端额外处理引导），这里
   应改为 `Bootloader=systemd-boot` 并重新验证 mkosi 产出的 ESP 布局与
   `partition_layout` 字段的分区描述是否匹配。
4. 本 README 第 3.1 节关于 cosign v3.x `--bundle` 与传统 `--key`/`--insecure-
   ignore-tlog` 组合的可行性，是基于官方文档 flag 说明推断的可行路径，**并非
   本轮实际跑通的命令**（环境缺少 cosign 二进制）。任务 3 实现 `sign.sh` 时
   必须先用 `cosign sign-blob --key ... --insecure-ignore-tlog` 之类的组合实测
   一遍，确认参数组合真的能在完全离线环境下跑通（例如是否仍会尝试联网检查
   Rekor 或做其它隐式网络调用），核实后再写入任务 3 自己的 README 段落。
5. Windows 侧的 Packer 模板（`windows/windows-server.pkr.hcl`，本任务写的是骨架）
   需要在装有 vSphere 环境与 `packer-plugin-vsphere` 插件的机器上实际跑一次，
   验证 `guest_os_type`（`windows9Server64Guest`）在目标虚拟硬件版本上是否
   引导正常——本任务的骨架只搭了框架结构，autounattend.xml 的具体渲染内容
   由 M4/M6 决定。
6. ESXi 侧的 Packer 模板（`esxi/esxi.pkr.hcl`）需要在**有 KVM 加速的 Linux
   构建机**（不是 vSphere 环境——本模板用 qemu builder，见第 1 节）上实际跑一次，
   确认 `<leftShift>O` 进入 installer 命令行编辑模式、USB 注入 kickstart 两个
   关键步骤在目标 ESXi 版本的安装器上行为一致（本任务的字段来自 Canonical
   packer-maas 仓库，该仓库的验证版本与 Lober 要覆盖的 ESXi 版本可能不同，
   需要重新确认一次这两个交互细节没有随 ESXi 版本变化）。
7. `windows-server.pkr.hcl` 里 `path.cwd` 与 `path.root` 两个 Packer 内置变量的
   精确语义边界（「模板所在目录」与「当前工作目录」具体如何不同、在什么场景下
   两者取值会分叉）本轮未能核实——`developer.hashicorp.com` 域名在本次核实环境
   下始终无法访问（WebFetch 与直接 curl 均被拒绝），只按 VMware 官方
   `packer-examples-for-vsphere` 仓库里的真实用法逐字对照抄录（`cd_files` 用
   `path.cwd`，`templatefile()` 里定位模板源用 `path.root`），未独立验证这个
   区分背后的官方语义说明。实测前建议先读一遍 Packer 官方 path-variables 文档
   确认理解无误，再决定是否需要调整。
