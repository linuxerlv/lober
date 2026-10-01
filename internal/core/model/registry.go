package model

import (
	"maps"
	"slices"

	"github.com/lober/lober/internal/core/catalog"
)

// Host 是一台登记过的主机。SN 是它唯一的身份（C6），其余都是可选属性（C7）。
// 第 2 层原样存它，字段名就是最终的名字。
type Host struct {
	SN       string           // 已经过 NormalizeSN
	Arch     catalog.Arch     // 可空：登记了才参与兼容校验（C28）
	Firmware catalog.Firmware // 可空，同上
	// Attrs 是自定义属性，如 IP 与前缀、主机名、机架位。键是标识符；值原样存、系统不解释，
	// 被参数引用时才按那个参数的类型校验。
	Attrs map[string]string
}

// hostReservedAttrs 不能当属性键：它们是主机自己的字段，CSV 导入按列名把这几列填进字段、
// 其余列才成为属性。属性键再用这几个名字，同一列就有了两种去处。
var hostReservedAttrs = []string{"sn", "arch", "firmware"}

// Validate 只查属性键：要是标识符，且不是保留键。SN 在登记时已经过 NormalizeSN；属性值要等
// 参数引用时才知道该按什么类型查。问题按键的字典序排列，代码为 attr_key_invalid，
// Subject 的 Path 是那个键。
func (h Host) Validate() []Problem {
	var out []Problem
	for _, key := range slices.Sorted(maps.Keys(h.Attrs)) {
		if !ValidIdent(key) || slices.Contains(hostReservedAttrs, key) {
			out = append(out, NewProblem("attr_key_invalid", Subject{Kind: "host", ID: h.SN, Path: key}, key))
		}
	}
	return out
}

// ISOState 是 ISO 的状态。字面量会存进数据库，发布后不能改名。
type ISOState string

const (
	ISOAcquiring  ISOState = "acquiring"  // 获取中：字节还在下载或上传
	ISOImporting  ISOState = "importing"  // 导入中
	ISOReady      ISOState = "ready"      // 可用
	ISOFailed     ISOState = "failed"     // 失败
	ISODeprecated ISOState = "deprecated" // 已废弃：已建的任务不受影响，新任务不能再用（C26）
)

// ISO 是一份登记过的安装介质。族和系在创建时就声明（C2），这时字节可能还没到齐，
// 所以版本、架构、制品和提供的集合都要等导入之后才有。
type ISO struct {
	ID      string
	Name    string
	Family  catalog.Family
	Series  catalog.Series
	Version string // 导入时按内容识别，[0-9A-Za-z._-]{1,32}
	Arch    catalog.Arch
	State   ISOState
	SHA256  string // 内容的 SHA256，相同的只导入一次（C4）
	// Artifacts 是角色到制品目录下相对路径的映射，角色由第 4 层定。只存相对路径：
	// 完整地址由渲染按制品目录布局拼出。
	Artifacts map[string]string
	// Offers 是 ISO 提供的取值集合，集合名到选项，如 Windows 的 editions。声明了 Offer 的
	// enum 参数，取值必须在同名的集合里（C21）。
	Offers map[string][]Option
}

// Usable 报告 ISO 能不能用来建任务：只有 ready 算（C5），已废弃的不能再被新引用（C26）。
func (iso ISO) Usable() bool {
	return iso.State == ISOReady
}
