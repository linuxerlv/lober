// 本文件渲染 %pre 阶段的硬件采集脚本（对应实现计划 P2-02 任务 1，依据
// SPEC §5.6、§6.2、§10 失败模式 5、6）。
//
// 核心原则（SPEC §5.6）：不要在模板里硬编码 /dev/sda 或 eth0。正确顺序是
// 先采集、再由服务端决策、再把具体设备标识写进应答文件。本文件只负责
// "采集"这一步——它产出的脚本在 installer ramdisk 里跑，采集四类事实
// （磁盘、网卡、主板与内存、CPU）后把结果以 KEY=value 形式 POST 到
// facts 端点，不做任何决策（决策在服务端，由 provider 包的
// hwfacts.go/nicplanner.go/diskplanner.go 完成）。
//
// 工具可用性约束（installer ramdisk 里没有完整工具集，计划任务 1 明确
// 列出）：
//   - lsblk 与 ip 在三个家族的 ramdisk 里都有，不需要降级路径
//   - ethtool 在 Debian 的 ramdisk 里可能缺失，缺失时降级为只读
//     /sys/class/net/<if>/ 下的 operstate 文件
//   - dmidecode 在部分精简 ramdisk 里缺失，缺失时降级为读
//     /sys/class/dmi/id/ 下的文件
//
// 输出约束（SPEC §6.2）：ramdisk 里没有 python、没有 jq，curl 可能只是
// busybox 版——脚本只能用 shell 内建与 coreutils，输出形态是纯文本
// KEY=value（与 facts 端点的格式一致），不产出 JSON。
//
// 契约缺口（本文件与 provider/hwfacts.go 共享的私有编码约定，写在此处
// 因为两个文件是这个约定的唯一使用方）：磁盘与网卡列表用"带序号的键"
// 表达（LOBER_INSPECT_DISK_<n>_<field>、LOBER_INSPECT_NIC_<n>_<field>），
// 前缀刻意不落进 model.FactKeysV1——那 24 个键是 M1 冻结的对外契约，而
// 这里的键是采集脚本与 hwfacts.go 解析逻辑之间的中间表示，不需要走契约
// 变更流程（真正对外承诺的是 LOBER_DISKS/LOBER_NICS 两个契约键本身，是
// 决策完成后写什么格式，不是采集阶段怎么编码）。两个文件必须保持这个
// 前缀与字段名一致，任何一边改动都要同步改另一边。
//
// 已知的编排缺口（本文件不负责，写清楚交给 contractGaps）：计划任务 4
// 要求 %pre 阶段"采集上报后同步等待服务端决策结果再分区"，那一步需要
// gateway 在收到 facts POST 的响应路径上就跑完决策并让机器紧接着读一次
// facts 拿到结果，涉及 internal/gateway/facts.go——不在本文件（也不在
// 本 agent 的文件归属）范围内。RenderInspectScript 只产出"采集并上报"
// 这一段独立脚本片段，是否在它之后追加"同步读决策结果"逻辑、以及决策
// 失败时如何让机器中止安装，都取决于持有 kickstart.go 装配点与
// gateway/facts.go 的实现者如何拼接。
package render

import (
	"fmt"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// factsPostPath 按 docs/contracts/http-machine.md 冻结的约定拼出 facts
// 端点路径：POST /v1/facts/{task}/{token}。token 走路径段而不是 query
// string（该文档"约定 3"）。
func factsPostPath(taskID, token string) string {
	return "/v1/facts/" + taskID + "/" + token
}

// RenderInspectScript 渲染硬件采集脚本文本（计划任务 1）。
//
// 产出的文本是一段独立、自成一体的 shell 脚本片段：定义好网关/任务/token
// 三个变量后，依次执行四类采集函数（各自内部处理工具缺失的降级路径），
// 把结果汇总进一个临时文件，最后用 curl POST 到 facts 端点。不注入
// signal/run_hook（那是 ClientLib 的职责，若调用方需要在同一段落里用到
// 它们，由调用方自己决定是否额外拼接 ClientLib(ctx)——本函数职责单一，
// 只做"采集并上报"）。
//
// 三条行为要求（计划任务 1）：
//  1. 单项采集失败不中断整体——每类采集各自的失败只影响该类别对应的键，
//     不 set -e，不用 && 链式依赖前一步成功。
//  2. 输出格式是纯文本键值对。
//  3. 磁盘列表用 by-id 路径，优先 wwn- 形式，选择规则确定。
func RenderInspectScript(ctx model.RenderContext) string {
	gw := ctx.Bases.GatewayURL()
	postPath := factsPostPath(ctx.Task.ID, ctx.InstallToken)

	var b strings.Builder

	fmt.Fprintf(&b, "GW='%s'\n", gw)
	b.WriteString("LOBER_INSPECT_OUT=\"$(mktemp)\"\n")
	b.WriteString("\n")

	b.WriteString(inspectDiskFunction())
	b.WriteString("\n")
	b.WriteString(inspectNICFunction())
	b.WriteString("\n")
	b.WriteString(inspectBoardMemoryFunction())
	b.WriteString("\n")
	b.WriteString(inspectCPUFunction())
	b.WriteString("\n")

	// 依次调用四类采集函数，各自把结果追加进 LOBER_INSPECT_OUT。任一函数
	// 内部出错不影响其它函数被调用——这里刻意逐行调用而不是用 && 串联，
	// 单纯的顺序执行加上每个函数自身"只处理自己的事、错误只影响自己的
	// 输出"就足够满足"单项失败不中断整体"（函数级隔离，不需要脚本层面
	// 额外的 trap 或 set +e/-e 切换）。
	b.WriteString("lober_inspect_disks\n")
	b.WriteString("lober_inspect_nics\n")
	b.WriteString("lober_inspect_board_memory\n")
	b.WriteString("lober_inspect_cpu\n")
	b.WriteString("\n")

	// 上报：用 curl --data-binary 发纯文本 body（与 clientlib.go 的
	// signal() 同一手法），只尝试一次——采集上报不是关键信号通道，reuse
	// signal() 的三次重试逻辑需要 ClientLib 已经注入，而本函数刻意不
	// 依赖 ClientLib（职责单一）；调用失败时仍不 exit 非零，遥测失败不
	// 应该杀死后续的 %pre 逻辑（与 signal() 第 7 条行为要求同一方向）。
	fmt.Fprintf(&b, "curl -fsS -m 10 -X POST \"$GW%s\" -H 'Content-Type: text/plain' "+
		"--data-binary \"@$LOBER_INSPECT_OUT\" || true\n", postPath)
	b.WriteString("rm -f \"$LOBER_INSPECT_OUT\"\n")

	return b.String()
}

// inspectDiskFunction 渲染磁盘采集函数：lsblk 列出块设备，对每个磁盘
// （TYPE=disk）反查 /dev/disk/by-id/ 下指向它的链接，按"优先 wwn- 前缀，
// 否则按链接名字典序取第一个"的确定规则选出持久化标识，随容量、型号、
// rotational 一并输出为带序号的键（LOBER_INSPECT_DISK_<n>_<field>）。
//
// by-id 选择规则确定的理由（计划任务 1）：同一块盘往往有多个 by-id
// 链接（wwn-/ata-/scsi- 等前缀），若不定规则、依赖 ls 的目录遍历顺序，
// 两次采集可能选出不同的链接名，服务端决策也就不确定。wwn- 优先是因为
// World Wide Name 是存储行业标准的全局唯一标识，比厂商私有的 ata-/scsi-
// 序列号形式更稳定（不随控制器/接口类型变化）。
func inspectDiskFunction() string {
	return `lober_inspect_disks() {
  local idx=0
  local name size rot model_ byid
  while read -r name size rot model_; do
    [ -z "$name" ] && continue
    byid=""
    if [ -d /dev/disk/by-id ]; then
      byid="$(
        for link in /dev/disk/by-id/*; do
          [ -e "$link" ] || continue
          target="$(readlink -f "$link" 2>/dev/null)" || continue
          [ "$target" = "/dev/$name" ] || continue
          echo "$link"
        done | sort | { grep -m1 '/wwn-' || true; }
      )"
      if [ -z "$byid" ]; then
        byid="$(
          for link in /dev/disk/by-id/*; do
            [ -e "$link" ] || continue
            target="$(readlink -f "$link" 2>/dev/null)" || continue
            [ "$target" = "/dev/$name" ] || continue
            echo "$link"
          done | sort | head -n1
        )"
      fi
    fi
    [ -z "$byid" ] && byid="/dev/$name"
    echo "LOBER_INSPECT_DISK_${idx}_BYID=$byid" >> "$LOBER_INSPECT_OUT"
    echo "LOBER_INSPECT_DISK_${idx}_SIZE=$size" >> "$LOBER_INSPECT_OUT"
    echo "LOBER_INSPECT_DISK_${idx}_ROTATIONAL=$rot" >> "$LOBER_INSPECT_OUT"
    echo "LOBER_INSPECT_DISK_${idx}_MODEL=$model_" >> "$LOBER_INSPECT_OUT"
    idx=$((idx+1))
  done < <(lsblk -dnb -o NAME,SIZE,ROTA,MODEL -e 7,11 2>/dev/null || true)
  return 0
}
`
}

// inspectNICFunction 渲染网卡采集函数：接口枚举优先用 `ip -o link show`
// 解析名字与 MAC（计划任务 1 表格点名的工具），`ip` 缺失（command not
// found）时降级为遍历 /sys/class/net/*/address 反查；link 状态优先用
// ethtool 探测，ethtool 缺失时降级读 /sys/class/net/<if>/operstate；
// PCI 路径从 /sys/class/net/<if>/device 的符号链接目标中提取（这一步
// 本身就是 sysfs 读取，没有对应的"工具"可降级，三个家族的 ramdisk 里
// sysfs 必然挂载）。
//
// ethtool 缺失时的降级路径是计划任务 1 明确要求的（Debian ramdisk 可能
// 没有这个工具）——不降级的后果是 Debian 的采集整体失败，表现为"磁盘
// 决策拿不到数据"（磁盘与网卡共用同一个采集脚本执行环境，网卡函数若
// 因缺工具直接退出非零，理论上不该级联影响磁盘函数，但为了不让机器
// 侧运维在不同发行版上看到不一致的采集完整度，这里显式做了降级而不是
// 放任报错）。
func inspectNICFunction() string {
	return `lober_inspect_nics() {
  local idx=0
  local iface mac state pci
  local ifaces=""
  if command -v ip >/dev/null 2>&1; then
    ifaces="$(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | awk '{print $1}')"
  fi
  if [ -z "$ifaces" ]; then
    for ifpath in /sys/class/net/*; do
      [ -d "$ifpath" ] || continue
      ifaces="$ifaces $(basename "$ifpath")"
    done
  fi
  for iface in $ifaces; do
    [ "$iface" = "lo" ] && continue
    ifpath="/sys/class/net/$iface"
    [ -d "$ifpath" ] || continue
    mac="$(cat "$ifpath/address" 2>/dev/null || true)"
    [ -z "$mac" ] && continue
    state=""
    if command -v ethtool >/dev/null 2>&1; then
      state="$(ethtool "$iface" 2>/dev/null | { grep -m1 'Link detected' || true; })"
      case "$state" in
        *yes*) state="up" ;;
        *no*)  state="down" ;;
        *)     state="" ;;
      esac
    fi
    if [ -z "$state" ]; then
      state="$(cat "$ifpath/operstate" 2>/dev/null || true)"
      case "$state" in
        up)   state="up" ;;
        down) state="down" ;;
        *)    state="down" ;;
      esac
    fi
    pci=""
    if [ -L "$ifpath/device" ]; then
      pci="$(basename "$(readlink -f "$ifpath/device" 2>/dev/null || true)")"
    fi
    echo "LOBER_INSPECT_NIC_${idx}_NAME=$iface" >> "$LOBER_INSPECT_OUT"
    echo "LOBER_INSPECT_NIC_${idx}_MAC=$mac" >> "$LOBER_INSPECT_OUT"
    echo "LOBER_INSPECT_NIC_${idx}_LINK=$state" >> "$LOBER_INSPECT_OUT"
    [ -n "$pci" ] && echo "LOBER_INSPECT_NIC_${idx}_PCI=$pci" >> "$LOBER_INSPECT_OUT"
    idx=$((idx+1))
  done
  return 0
}
`
}

// inspectBoardMemoryFunction 渲染主板与内存采集函数：优先用 dmidecode
// 取厂商/型号，缺失（command not found）时降级读 /sys/class/dmi/id/ 下
// 对应的文件（sys_vendor、product_name）。内存总量从 /proc/meminfo 的
// MemTotal 行取（该文件是 procfs 标准内容，三个家族的 ramdisk 里都有，
// 不需要单独的降级路径）。
func inspectBoardMemoryFunction() string {
	return `lober_inspect_board_memory() {
  local vendor product ram_kb ram_mb
  vendor=""
  product=""
  if command -v dmidecode >/dev/null 2>&1; then
    vendor="$(dmidecode -s system-manufacturer 2>/dev/null || true)"
    product="$(dmidecode -s system-product-name 2>/dev/null || true)"
  fi
  if [ -z "$vendor" ] && [ -r /sys/class/dmi/id/sys_vendor ]; then
    vendor="$(cat /sys/class/dmi/id/sys_vendor 2>/dev/null || true)"
  fi
  if [ -z "$product" ] && [ -r /sys/class/dmi/id/product_name ]; then
    product="$(cat /sys/class/dmi/id/product_name 2>/dev/null || true)"
  fi
  [ -n "$vendor" ] && echo "LOBER_INSPECT_VENDOR=$vendor" >> "$LOBER_INSPECT_OUT"
  [ -n "$product" ] && echo "LOBER_INSPECT_PRODUCT=$product" >> "$LOBER_INSPECT_OUT"

  ram_kb="$(awk '/^MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || true)"
  if [ -n "$ram_kb" ]; then
    ram_mb=$((ram_kb / 1024))
    echo "LOBER_INSPECT_RAM_MB=$ram_mb" >> "$LOBER_INSPECT_OUT"
  fi
  return 0
}
`
}

// inspectCPUFunction 渲染 CPU 采集函数：优先用 nproc，缺失时降级为数
// /proc/cpuinfo 里 "^processor" 开头的行数（两者语义一致，都是逻辑核心
// 数）。
func inspectCPUFunction() string {
	return `lober_inspect_cpu() {
  local cores
  cores=""
  if command -v nproc >/dev/null 2>&1; then
    cores="$(nproc 2>/dev/null || true)"
  fi
  if [ -z "$cores" ] && [ -r /proc/cpuinfo ]; then
    cores="$(grep -c '^processor' /proc/cpuinfo 2>/dev/null || true)"
  fi
  [ -n "$cores" ] && echo "LOBER_INSPECT_CPU_CORES=$cores" >> "$LOBER_INSPECT_OUT"
  return 0
}
`
}
