# 主机硬件属性、装机地址与业务地址约定

状态：草稿，待确认。尚未修改产品代码。

本文只约定名词、数据归属和执行顺序，不引入新的持久化对象。落地时按文末的改动清单逐项做。

## 要解决的问题

三件事容易混在一起，混了就会装错机器、或者让 lober 误判任务失败：

1. 主机的硬件信息（几块盘、多大内存、什么 CPU）从哪来；
2. 装机过程中机器用的是什么地址；
3. 装好以后机器长期用的是什么地址。

第 2 和第 3 必须是两个东西。用同一个字段会导致：装机网络里的 DHCP 地址被当成业务地址写进系统，
装完就断网；或者反过来，业务地址在装机阶段被拿去访问 lober，装到一半失联。

## 三个名词

### 硬件属性（`inv_` 前缀）

主机采集到的硬件事实：磁盘清单、内存大小、CPU 型号。

- **只由采集程序写**，人和控制台都不能改。
- 键一律以 `inv_` 开头，不带前缀的属性键不属于这一类。
- 采集**不采网络地址**，理由见下。

### 装机地址

机器在装机过程中从 DHCP 拿到的临时地址。

- 只活在一次任务里，任务结束或 4 小时后失效。
- **只在内存里**，进程重启就丢，不写数据库。
- 唯一用途是让 syslog 认出「这条日志来自哪个任务」，因为 syslog 报文只带源地址、不带任务号。
- 代码位置：`internal/infrastructure/events/syslog.go` 的 `Collector.leases`，以及
  `internal/application/execution/observation.go` 的 `NewIPObserver`。

### 业务地址

机器装好以后长期使用的地址，包括地址/前缀、网关、DNS。

- 随主机永久保存，存在主机属性里，键为 `biz_ip`、`biz_gateway`、`biz_dns`。
- 来源二选一：人工填写，或从网络对象里选一个。
- 装机前就定好，冻进任务快照，装完由 firstboot 写进系统。

## 为什么采集不采网络地址

采集程序跑的时候机器用的是装机网络的地址，这个地址：

- 装完就还给 DHCP 了；
- 和业务地址不在一个网段；
- 对运维没有任何意义。

采了它只会让人误以为那是主机的地址。

两道防线：

| 防线 | 做法 |
|---|---|
| 采集脚本 | 只读 `/proc/cpuinfo`、`/proc/meminfo`、`/sys/block/*`、`/sys/class/dmi/id/*`，脚本里根本没有网卡相关代码 |
| 上报接口 | 请求里出现 `ip`、`mac`、`netmask`、`gateway`、`dns` 这类键，整个请求 400 |

第二条是关键。就算采集脚本被改过，服务端也不会把 `eth0` 的地址写成主机属性。

## 采集用什么系统

**Alpine 的内核 + 自制 initramfs，不启动 Alpine 自己的安装器。**

```
iPXE → vmlinuz-lts + lober-inventory.cpio.gz
           ↓
      我们的 /init 直接跑采集，然后 reboot
```

| 元件 | 来源 | 体积 |
|---|---|---|
| `vmlinuz-lts` | Alpine 官方 netboot | 约 10 MB |
| `busybox` 静态版 | Alpine 的 `busybox-static` 包，单文件不依赖 musl | 约 1 MB |
| 内核模块 | 从 `linux-lts` apk 里提取 | 约 15 MB |
| `/init` 与采集脚本 | 自己写 | 约 5 KB |

**合计约 26 MB**，构建一次放进制品库，所有主机共用。

内核模块只带这些，虚拟机和物理机都够：

```
virtio_blk virtio_pci virtio_scsi
ahci libahci sd_mod nvme
megaraid_sas mpt3sas mptspi aacraid hpsa
e1000 e1000e igb ixgbe virtio_net
```

## 采集用什么工具：一个都不装

Alpine 的 busybox **没有** `lsblk`、`lscpu`、`dmidecode`（实测确认）。但这三个都不需要，
数据本来就在 `/sys` 和 `/proc` 里：

| 要什么 | 怎么取 | 替代了什么 |
|---|---|---|
| CPU 型号 | `grep 'model name' /proc/cpuinfo` | `lscpu` |
| CPU 核数 | 数 `processor` 行 | `lscpu` |
| 内存大小 | `MemTotal`（`/proc/meminfo`） | `free` |
| 磁盘容量 | `/sys/block/*/size` × 512 | `lsblk` |
| 磁盘型号 | `/sys/block/*/device/model` | `lsblk -o MODEL` |
| 是否机械盘 | `/sys/block/*/queue/rotational` | `lsblk -d` |
| 厂商与机型 | `/sys/class/dmi/id/product_*` | `dmidecode` |
| 固件类型 | 看 `/sys/firmware/efi` 在不在 | `dmidecode` |
| 上传 | busybox 自带的 `wget --post-file` | `curl` |

**零外部依赖**，镜像小，也不会因为缺某个包在物理机上失败。

磁盘过滤：跳过 `loop*`、`ram*`、`sr*`、`dm-*`、`zram*`，只留真实块设备。

## 采集上传什么

接口：`POST /v1/inventory/<sn>`，`Content-Type: application/json`

```json
{
  "collected_at": "2026-11-01T10:00:00Z",
  "firmware": "uefi",
  "cpu": { "model": "Intel Xeon Gold 6248R", "cores": 48 },
  "memory_mib": 32768,
  "system": { "manufacturer": "Dell Inc.", "product": "PowerEdge R750" },
  "disks": [
    { "name": "sda", "bytes": 10737418240, "model": "PERC H755", "rotational": false },
    { "name": "sdb", "bytes": 8589934592,  "model": "QEMU HARDDISK", "rotational": false }
  ]
}
```

服务端收到后：

| 做什么 | 结果 |
|---|---|
| 查白名单 | 有 `ip`、`mac` 这类键 → 400 |
| 算最小容量盘 | 写 `inv_system_disk`。**服务端自己算**，不信任采集端给的 |
| 存原始清单 | 写 `inv_disks`、`inv_cpu_model`、`inv_cpu_cores`、`inv_mem_mib`、`inv_system_model` |
| 打标记 | 记「已采集」，以后不再进采集镜像 |

写入的主机属性：

| 键 | 类型 | 例子 |
|---|---|---|
| `inv_cpu_model` | 文本 | `Intel Xeon Gold 6248R` |
| `inv_cpu_cores` | 整数 | `48` |
| `inv_mem_mib` | 整数 | `32768` |
| `inv_system_model` | 文本 | `Dell Inc. PowerEdge R750` |
| `inv_disks` | 文本，列表 | `sda:10737418240:PERC H755` |
| `inv_system_disk` | 磁盘名 | `sda` |

## 采集怎么认证

采集时机器上还没有任务，拿不到现有的安装令牌。所以 `/v1/boot` 决定给采集菜单时，
在返回的 iPXE 引导行里塞一个一次性令牌：

```
kernel <artifacts>/inventory/vmlinuz \
  lober.gateway=<gateway> lober.sn=<sn> \
  lober.inventory_token=<64 位十六进制>
initrd <artifacts>/inventory/inventory.cpio.gz
```

令牌和 SN 一一对应，**存在内存里，一小时作废**，跟装机地址租约是同一类东西，不新增数据库字段。

| 场景 | 结果 |
|---|---|
| 正常采集 | 令牌对得上，数据被接受 |
| 拿别人的 SN 伪造上报 | 没有令牌 → 401 |
| 采集中途断网，一小时后重来 | 令牌已作废，要在控制台重新点「更新配置」 |
| lober 重启 | 内存清空，同上 |

## 什么时候给采集菜单

`/v1/boot` 现在的规则是「找不到打开的任务就给本地盘引导」。加一层判断：

```
有打开的任务                        → 安装菜单（不变）
没有任务，且满足采集条件             → 采集菜单（新增）
其余                                → 回本地盘（不变）
```

采集条件，四个都要满足：

1. 主机在 `hosts` 表里登记过；
2. 从没采集过，或者控制台点过「更新配置」；
3. 没有打开的任务；
4. 采集标记没超过 1 小时（超时自动作废，避免网络不通时无限重启）。

## 装机地址与业务地址的分界

```
① 采集（可选）     主机开机 → 采集程序 → 报硬件 → 写 inv_* 属性
                                             ↑ 不报任何网络地址
② 定业务地址       人工填，或从网络对象选 → 写 biz_* 属性
③ 建任务           求值 → 冻进快照（inv_* 和 biz_* 都在快照里）
④ 装机             全程 DHCP，拿装机地址；lober 只在内存记租约给 syslog 用
⑤ 装完重启         进入新系统，lober-firstboot.service 启动
                   排序靠前的槽位仍然用 DHCP，持续往 lober 发信号
⑥ 最后一步         把网卡切成 biz_ip / biz_gateway / biz_dns
                   从这一刻起这台机器用业务地址，lober 联系不上它了
```

⑥ 之后 lober 收不到任何东西，所以 `init-done` 和 `ready` 必须在 ⑥ 之前发出去。这是
`internal/domain/render/answer.go` 里 firstboot 运行器的既有顺序，不需要改。

## 静态地址的槽位必须排最后

现在槽位按 `Seq` 顺序串行执行，但没有任何约束保证「切地址」那个槽排在最后。作者把它排在中间，
它后面的槽全部连不上 lober，任务卡在「初始化中」，4 小时后假失败。

**约定**：切地址的槽必须命名为 `network-static`。渲染 firstboot 运行器时，把这个槽排到所有
`firstboot` 槽的最后，不按 `Seq`。槽名对不上就不动顺序，保持现在的行为。

只改渲染层一处，不动状态机、不动 catalog。

## 静态地址要的参数

四个，都走主机属性绑定：

| 参数 | 类型 | 例子 | 说明 |
|---|---|---|---|
| `biz_ip` | `cidr` | `10.0.0.5/24` | 保留主机位，就是这个机器的地址 |
| `biz_gateway` | `ip` | `10.0.0.1` | |
| `biz_dns` | `ip`，列表 | `223.5.5.5` | 可多个 |
| `biz_iface` | `word` | `eth0` | 网卡名 |

**掩码不用单独开参数**，`cidr` 类型已经能取子值，模板里写 `{{param:biz_ip.netmask}}` 即可
（`internal/domain/model/param.go` 的 `cidrAccessors`）。

## 五家的落点

每家一份 `firstboot` 时机的脚本，和现在 `sshkey` hook 的做法一样。

| 系统 | 落点 |
|---|---|
| AlmaLinux、Rocky | NetworkManager：`nmcli con mod`，或写 `/etc/sysconfig/network-scripts/ifcfg-<iface>` |
| Debian | `/etc/network/interfaces` |
| Ubuntu 24.04 | netplan，`/etc/netplan/99-lober.yaml` |
| openSUSE 15、16 | wicked：`/etc/sysconfig/network/ifcfg-<iface>` |

这些脚本要写出「装完能远程登录」的完整配置，包括 `auto` 开机启用。写错的结果是机器装好就失联，
只能进带外或者重装。

## 业务地址的两个来源

| 方式 | 谁定 IP | 系统多做的一步 |
|---|---|---|
| 人工填写 | 人 | 只校验格式和「没有别的主机登记过同一个地址」 |
| 从网络对象选 | 人 | 多校验一条：地址必须落在该网段的地址池范围内 |

两种方式产出的结果都是主机属性 `biz_*`，渲染时没有区别。第二种只是帮人检查，不是新机制。

**不做自动分配**（IPAM）。理由：地址回收、重装时地址是否保持、与 DHCP 池重叠、网段里有没有
非 lober 管理的设备在用同一个地址，这四个问题没有可信答案。自动分配会把这些问题变成静默故障。

## 与现有机制的关系

以下都是现成的，本文不新增：

| 能力 | 现状 |
|---|---|
| 主机属性存取 | `model.Host.Attrs`，pgstore 的 `hosts.attrs` 列 |
| 参数从主机属性取值 | `model.BindingAttr`，求值时走 `params.go` 的 `prefill` |
| 快照留一份主机属性 | `model.SnapshotHost.Attrs` |
| 装机地址临时租约 | `events.Collector.leases`，纯内存 |
| firstboot 槽位串行执行 | `render/answer.go` 的 firstboot 运行器 |

## 要改的地方

按依赖顺序：

1. **渲染层**：firstboot 运行器把名为 `network-static` 的槽排到最后。
2. **内置内容**：五家 network hook 加 `firstboot` 时机的静态地址实现和对应参数声明。
3. **采集镜像**：构建 Alpine 内核 + 自制 initramfs，放进制品库，角色名 `inventory`。
4. **引导分支**：`/v1/boot` 加「给采集菜单」的判断，返回带一次性令牌的 iPXE 引导行。
5. **上报接口**：`POST /v1/inventory/<sn>`，查令牌、查白名单、算最小盘、写 `inv_*` 属性。
6. **控制台**：主机详情页展示 `inv_*`；业务地址可编辑；「更新配置」按钮置重采标记。

第 4 步要改 C32（引导只按 SN 匹配任务，找不到就让机器从本地盘启动）——加一个例外：主机登记了
「需要采集」且从未采集过时，给采集镜像。这条要同步改 `docs/contracts/machine-execution.md`。

第 3 步怎么构建、`/init` 干什么、上传什么、令牌怎么发，见上面「采集」几节。

## 明确不做

| 不做 | 理由 |
|---|---|
| 采集时采网络地址 | 装机的地址对业务没有意义，采了只会误导 |
| 自动分配业务地址（IPAM） | 见上一节，四个问题没有可信答案 |
| 独立的硬件配置对象/表 | 主机属性已能存任意键值，先用手头的，不够用再建表 |
| 用 `{{host:xxx}}` 新占位符直接引用属性 | 会绕过类型校验和缺值检查，错误从「建任务时」推迟到「装机时」 |

## 风险

**`inv_` 前缀靠约定，不靠强制。** 属性和人工属性在同一张 map 里，只是键前缀不同。要真正分清，
得给 `hosts` 表加一列记录哪些键是采集来的——那是一个字段，不是一套对象，真需要时再加。

**槽名约定也可能写错。** 作者写 `network_static` 而不是 `network-static`，排序就不生效，
且不会报错。建议在激活判定里加一条警告：模板里有静态地址参数却没有 `network-static` 槽时提示。

**采集镜像的驱动集决定它能采到什么。** 只带上面列的模块，遇到没覆盖的 RAID 卡时，采集会看到
零块磁盘，`inv_system_disk` 就是空的，装机时盘名参数取不到值。这种情况要在上报时明确报错
「未发现任何可安装磁盘」，而不是悄悄写个空值——空值要到建任务时才会暴露，那时人已经离开机柜了。

**采集令牌存在内存里，lober 重启就丢。** 采集中途重启 lober，机器会一直重启但拿不到新令牌，
直到管理员重新点「更新配置」。这个行为要写进控制台的提示文字，不然运维会以为机器坏了。
