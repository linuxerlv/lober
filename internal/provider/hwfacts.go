// 本文件把机器侧 inspect 采集脚本上报的原始 facts KV（扁平
// map[string]string，来自 GET /v1/facts 端点）解析成结构化的硬件事实
// （磁盘列表与网卡列表），供 DiskPlanner 与网卡决策消费。对应实现计划
// P2-02 任务 2、任务 3，依据 SPEC §5.4、§5.5、§5.6。
//
// 契约缺口（写在此处，因为本文件是唯一的解析现场）：contract.HWFacts 只有
// OSFamily/Vendor/Product/Arch 四个字段，不携带磁盘/网卡这类结构化硬件
// 事实；TaskParamsResolver.Resolve 的 facts 参数类型是扁平的
// map[string]string，不是结构化类型。本文件因此在 provider 包内部定义
// HWDisk/HWNIC 两个结构化类型，只在跨接口边界处（resolve.go 调用
// contract.DiskPlanner.Plan 时）转换为 contract.Disk；HWNIC 没有对应的
// contract 类型（网卡决策不是七个 provider 扩展点之一），故 HWNIC 是纯
// provider 包内部类型，不需要跨边界转换。
//
// 正解：contract.HWFacts 补充结构化磁盘/网卡字段，或者
// TaskParamsResolver.Resolve 的签名改为接收一个结构化的硬件事实参数（不再
// 从扁平字符串 map 里二次解析）。届时本文件的解析逻辑可以整体保留（服务端
// 仍需要"从 facts KV 文本还原结构"这一步，只是调用点从 provider 内部搬到
// gateway 或装配点），只是转换到 contract 类型的那一小段代码需要跟着新
// 字段调整。
package provider

import (
	"fmt"
	"strconv"
	"strings"
)

// inspectDiskKeyPrefix / inspectNICKeyPrefix 是本文件定义的「采集专用键」
// 命名空间前缀（计划任务 2 编码约定：磁盘/网卡列表用「带序号的键」表达，
// 不用分隔符拼进一个值——型号字符串本身含空格，容量是数字，混在一起解析
// 容易出错）。
//
// 刻意不落进 model.FactKeysV1（M1 冻结的 24 个契约键）：那 24 个键是对外
// 承诺、只增不删，而这里的键是 inspect 采集脚本与本文件解析逻辑之间的私有
// 编码细节，改动它不需要走契约变更流程。真正对外承诺的是 LOBER_DISKS /
// LOBER_NICS 两个契约键本身（决策完成后由谁写、写什么格式），不是采集
// 阶段的中间表示——本文件与 render/inspect.go 共享这个前缀约定即可，无需
// 惊动 model 包。
const (
	inspectDiskKeyPrefix = "LOBER_INSPECT_DISK_"
	inspectNICKeyPrefix  = "LOBER_INSPECT_NIC_"
)

// HWDisk 是解析后的结构化磁盘事实：由 ParseHWDisks 从 facts KV 还原，供
// resolve.go 转换为 contract.Disk 后交给 DiskPlanner 使用。
//
// 字段集合与 contract.Disk 一致（ByIDPath/SizeBytes/Rotational/Model），
// 刻意不直接复用 contract.Disk 类型本身：HWDisk 是"采集解析"这一步的产物，
// contract.Disk 是"磁盘规划器输入"这一步的契约类型，两者语义不同的阶段
// 恰好字段相同是巧合，不应该因为当前字段相同就耦合成同一个类型——磁盘
// 决策未来可能需要 contract.Disk 之外的采集专属字段（如采集时间戳），
// 那时两个类型会分叉，现在就分开定义不需要未来再拆。
type HWDisk struct {
	ByIDPath   string
	SizeBytes  int64
	Rotational bool
	Model      string
}

// HWNIC 是解析后的结构化网卡事实：由 ParseHWNICs 从 facts KV 还原，供
// nicplanner.go 消费。没有对应的 contract 类型——网卡决策不是七个 provider
// 扩展点之一，不经过 contract 层。
type HWNIC struct {
	Name    string
	MAC     string
	LinkUp  bool
	PCIPath string
}

// diskFieldKeys 是磁盘每条记录必须齐备的四个字段后缀。ParseHWDisks 按序号
// 分组后逐条检查这四个键是否全部存在，任一缺失即跳过整条记录（计划任务 2：
// "缺字段的条目跳过而不是用零值补齐"）。
var diskFieldKeys = []string{"BYID", "SIZE", "ROTATIONAL", "MODEL"}

// nicFieldKeys 是网卡每条记录必须齐备的字段后缀。PCI 允许缺失（同型号单卡
// 机器上 PCI 路径不是区分依据，采集脚本在探测不到时可以不报这个字段），
// 但 NAME/MAC/LINK 三者缺一不可——MAC 是决策最重要的判据，缺失时决不能用
// 空字符串补齐（会被误判为"匹配"到另一块同样缺 MAC 的记录）。
var nicFieldKeys = []string{"NAME", "MAC", "LINK"}

// groupBySerial 把形如 "<prefix><index>_<field>" 的键按 index 分组，
// 返回 index（原始字符串形式，不要求是纯数字——序号解析失败的分组会在
// 调用方逐条转换时被判定为畸形并跳过，不在这一步就报错）到「字段名→值」
// 映射的表。
//
// 分组顺序不保证与输出顺序一致（map 遍历本身无序）；调用方需要对 index
// 做数值排序以获得稳定输出（ParseHWDisks/ParseHWNICs 都这样做，保证同一
// 输入两次解析得到同一顺序的切片，这是决策纯函数确定性的前提之一）。
func groupBySerial(facts map[string]string, prefix string) map[string]map[string]string {
	groups := make(map[string]map[string]string)
	for key, value := range facts {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := key[len(prefix):]
		sep := strings.IndexByte(rest, '_')
		if sep <= 0 || sep == len(rest)-1 {
			// 序号部分为空（sep<=0），或字段名部分为空（sep 是最后一个字符），
			// 都是畸形键——跳过，不纳入任何分组。sep<=0 时 rest[sep:] 会切出
			// 空字符串或包含负索引，进一步处理只会产出误导性的分组名。
			continue
		}
		index := rest[:sep]
		field := rest[sep+1:]

		group, ok := groups[index]
		if !ok {
			group = make(map[string]string)
			groups[index] = group
		}
		group[field] = value
	}
	return groups
}

// sortedNumericIndices 把 groupBySerial 返回的字符串索引集合按数值升序
// 排列，非数值索引整体丢弃（视为畸形分组）。返回排好序的整数索引列表。
func sortedNumericIndices(groups map[string]map[string]string) []int {
	indices := make([]int, 0, len(groups))
	for idxStr := range groups {
		n, err := strconv.Atoi(idxStr)
		if err != nil {
			continue // 非数值序号——畸形键，静默丢弃（groupBySerial 已经做过一次
			// 基本形态过滤，这里是第二道防线，防止 "DISK_abc_BYID" 这类序号
			// 本身非数字的键混进分组）
		}
		indices = append(indices, n)
	}
	sortInts(indices)
	return indices
}

// sortInts 是 sort.Ints 的直接替身；本文件已经在用简单排序，单独包一层
// 只是让调用点不必额外 import "sort" 却仍能表达"升序排列"这个语义。
func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// hasAllFields 检查 group 是否包含 required 列出的全部字段键，且值非空。
// 值为空字符串等同于字段缺失——一个"存在但是空"的容量字段与"完全没有这个
// 键"一样危险（都不能被当作合法的容量 0 来用）。
func hasAllFields(group map[string]string, required []string) bool {
	for _, f := range required {
		v, ok := group[f]
		if !ok || v == "" {
			return false
		}
	}
	return true
}

// ParseHWDisks 从 facts KV 中还原磁盘列表（计划任务 2）。
//
// 行为要点：
//   - facts 中没有任何 LOBER_INSPECT_DISK_ 键时返回空切片、不报错——上游
//     （render/inspect.go 生成的采集脚本尚未上报，或本次任务不涉及磁盘
//     采集）是正常的中间状态。
//   - 每条记录缺任一字段（BYID/SIZE/ROTATIONAL/MODEL）时整条跳过，不用
//     零值补齐——容量为 0 的盘参与 largest 策略会选错（计划任务 2 明确
//     要求）。
//   - SIZE 非合法整数、ROTATIONAL 非合法布尔值时同样视为该条记录畸形，
//     整条跳过（不 panic，也不让一条畸形记录拖累其它记录的解析）。
//   - 序号不连续（某块盘采集失败）时仍能正确还原其余记录。
//   - 不消费任何不属于本前缀的键——它们应原样留在传入的 facts map 里，
//     本函数不做任何删除操作。
//
// 返回顺序按序号数值升序，保证同一输入两次解析得到同一顺序的切片（决策
// 纯函数确定性的前提，与 diskplanner.go 的 diskByIDPathAsc 精神一致，但
// 这里排序依据是采集序号而不是 by-id 路径——两者服务于不同目的：这里的
// 排序保证解析步骤本身确定，diskplanner.go 的排序保证"同容量并列时选谁"
// 确定，互不冲突）。
func ParseHWDisks(facts map[string]string) ([]HWDisk, error) {
	groups := groupBySerial(facts, inspectDiskKeyPrefix)
	indices := sortedNumericIndices(groups)

	disks := make([]HWDisk, 0, len(indices))
	for _, idx := range indices {
		group := groups[strconv.Itoa(idx)]
		if !hasAllFields(group, diskFieldKeys) {
			continue // 缺字段：跳过整条记录，不用零值补齐（计划任务 2）
		}

		size, err := strconv.ParseInt(group["SIZE"], 10, 64)
		if err != nil {
			continue // SIZE 畸形：跳过整条记录，不让它污染其它记录的解析
		}
		rotational, err := parseHWBool(group["ROTATIONAL"])
		if err != nil {
			continue // ROTATIONAL 畸形：同上
		}

		disks = append(disks, HWDisk{
			ByIDPath:   group["BYID"],
			SizeBytes:  size,
			Rotational: rotational,
			Model:      group["MODEL"],
		})
	}
	return disks, nil
}

// parseHWBool 把采集脚本可能产出的布尔文本形式解析为 bool。busybox shell
// 环境下最自然的写法是 "0"/"1"（lsblk --json 的 ROTA 字段本身就是这种
// 形式），但也接受 "true"/"false" 以兼容脚本里可能出现的显式文本——两种
// 写法都不合法时返回错误，调用方据此跳过该条记录。
func parseHWBool(s string) (bool, error) {
	switch s {
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	default:
		return false, fmt.Errorf("provider: 布尔字段值 %q 既不是 0/1 也不是 true/false", s)
	}
}

// ParseHWNICs 从 facts KV 中还原网卡列表（计划任务 3 的输入）。
//
// 行为要点与 ParseHWDisks 对称：
//   - 没有任何 LOBER_INSPECT_NIC_ 键时返回空切片、不报错。
//   - NAME/MAC/LINK 三者缺一即跳过整条记录——MAC 是网卡决策最重要的判据，
//     缺失时不能用空字符串补齐（空字符串会被误判为"匹配"到另一块同样
//     缺 MAC 的记录，两块不同的物理网卡因此被认为是同一块）。
//   - LINK 字段只接受 "up"/"down" 两个字面值，其它任何值（含空、大小写
//     不同、拼写错误）都视为该条记录畸形并跳过——网卡链路状态不明时，
//     猜测为 up 或 down 都可能导致装机网卡选错。
//   - PCI 路径允许缺失（保留在 HWNIC.PCIPath 里为空字符串），因为它只在
//     "同型号多网卡"场景下才需要作为区分依据，单卡或型号不同的机器上
//     采集脚本探测不到时不应该因此整条记录报废。
func ParseHWNICs(facts map[string]string) ([]HWNIC, error) {
	groups := groupBySerial(facts, inspectNICKeyPrefix)
	indices := sortedNumericIndices(groups)

	nics := make([]HWNIC, 0, len(indices))
	for _, idx := range indices {
		group := groups[strconv.Itoa(idx)]
		if !hasAllFields(group, nicFieldKeys) {
			continue
		}

		linkUp, ok := parseLinkState(group["LINK"])
		if !ok {
			continue // LINK 畸形：跳过整条记录，不猜测默认状态
		}

		nics = append(nics, HWNIC{
			Name:    group["NAME"],
			MAC:     group["MAC"],
			LinkUp:  linkUp,
			PCIPath: group["PCI"], // 缺失时 map 零值即空字符串，符合"允许缺失"的要求
		})
	}
	return nics, nil
}

// parseLinkState 把 "up"/"down" 解析为 bool，其它任何值返回 ok=false。
func parseLinkState(s string) (up bool, ok bool) {
	switch s {
	case "up":
		return true, true
	case "down":
		return false, true
	default:
		return false, false
	}
}
