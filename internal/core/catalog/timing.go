package catalog

import "slices"

// Timing 是执行时机：装机过程中能跑 hook 脚本的位置，执行插槽在正文里就声明它
// （{{slot:名字@时机}}）。四个时机是一张全局词表，每种格式只支持其中一部分。
type Timing string

// Interpreter 是脚本声明的解释器。sh 指 POSIX sh，含 busybox ash：有的安装环境只有它，
// 声明 sh 的脚本就不能用 bash 的扩展语法。
type Interpreter string

// SystemPart 是系统部件：格式要求的系统代码，如首次开机脚手架。正文只决定它摆在哪
// （{{lober:部件}}），内容由系统给出，作者换不掉。
type SystemPart string

// SystemValue 是系统值：系统按任务填入的值（{{sys:名字}}），如安装源地址、网关地址，
// 模板因此不用知道 ISO 和网关在哪。
type SystemValue string

// 下面四张词表和 series.go 的表一样：标识会写进正文的占位符或数据库，发布后不能改名；
// 显示名只给界面看。

type timingRow struct {
	id    Timing
	rank  int
	label string
}

type interpreterRow struct {
	id    Interpreter
	label string
}

type systemPartRow struct {
	id    SystemPart
	label string
}

type systemValueRow struct {
	id    SystemValue
	label string
}

// timingTable 按 Rank 排列，同级的两个 post 先安装环境、后目标系统。Format.Timings
// 照这张表的顺序输出，所以它的顺序就是对外的顺序。
var timingTable = []timingRow{
	{"pre", 1, "装前"},
	{"post-installer", 2, "装后（安装环境）"},
	{"post-target", 2, "装后（目标系统内）"},
	{"firstboot", 3, "首次开机"},
}

var interpreterTable = []interpreterRow{
	{"sh", "sh"},
	{"bash", "bash"},
	{"cmd", "cmd"},
	{"powershell", "PowerShell"},
}

var systemPartTable = []systemPartRow{
	{"syslog", "syslog 指向"},
	{"firstboot", "首次开机脚手架"},
	{"cleanup", "凭据清理"},
}

// 最后三个只给系统部件与机器面地址用（spec §6.2 第 2 条），不进任何格式的能力表：作者在正文里写
// {{sys:token}}，激活判定报 sysvalue_not_allowed。
var systemValueTable = []systemValueRow{
	{"repo", "安装源地址"},
	{"iso", "原始 ISO 地址"}, // Ubuntu 的 url=：安装器要经 HTTP 把整张 ISO 下进内存
	{"smb", "Windows 安装源共享"},
	{"gateway", "网关基地址"},
	{"task", "任务号"},
	{"sn", "主机 SN"},
	{"lbrace", "字面量 {{"},         // 正文里要写字面量 {{ 时用它转义
	{"token", "token"},           // 网关签发的 token，放在 AnswerURL 的路径段里
	{"syslog-addr", "syslog 地址"}, // host:port；syslog 是系统部件的标识，两者不混
	{"artifacts", "制品基地址"},       // ArtifactURL 的 base
}

// capabilityRow 是一种格式的能力。三个列表都按这里写的顺序对外返回。
type capabilityRow struct {
	interpreters map[Timing][]Interpreter // 键就是该格式支持的时机，值是这个时机可用的解释器
	parts        []SystemPart             // 正文里必须各恰好出现一次（C15）
	values       []SystemValue            // 正文与片段里能引用
}

// capabilityTable 是每种格式的能力表（C1、C10、C14、C15）。kickstart、preseed 两行取自
// 旧系统跑过的渲染器，其余是初值，第 3、7 层真装之后改这张表要附带用例。用格式和时机
// 做 map 的键，是为了让重复登记的格式或时机在编译时就报错，而不是后一行悄悄不生效。
var capabilityTable = map[Format]capabilityRow{
	"kickstart": {
		interpreters: map[Timing][]Interpreter{
			"pre":            {"sh", "bash"},
			"post-installer": {"sh", "bash"},
			"post-target":    {"sh", "bash"},
			"firstboot":      {"sh", "bash"},
		},
		parts:  []SystemPart{"syslog", "firstboot", "cleanup"},
		values: []SystemValue{"repo", "gateway", "task", "sn", "lbrace"},
	},
	// Debian 的安装环境只有 busybox sh（C10），bash 要到目标系统里才有。
	"preseed": {
		interpreters: map[Timing][]Interpreter{
			"pre":            {"sh"},
			"post-installer": {"sh"},
			"post-target":    {"sh", "bash"},
			"firstboot":      {"sh", "bash"},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"repo", "gateway", "task", "sn", "lbrace"},
	},
	"autoinstall": {
		interpreters: map[Timing][]Interpreter{
			"pre":            {"sh", "bash"},
			"post-installer": {"sh", "bash"},
			"post-target":    {"sh", "bash"},
			"firstboot":      {"sh", "bash"},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"repo", "iso", "gateway", "task", "sn", "lbrace"},
	},
	"autoyast": {
		interpreters: map[Timing][]Interpreter{
			"pre":            {"sh", "bash"},
			"post-installer": {"sh", "bash"},
			"post-target":    {"sh", "bash"},
			"firstboot":      {"sh", "bash"},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"repo", "gateway", "task", "sn", "lbrace"},
	},
	"agama": {
		interpreters: map[Timing][]Interpreter{
			"pre":            {"sh", "bash"},
			"post-installer": {"sh", "bash"},
			"post-target":    {"sh", "bash"},
			"firstboot":      {"sh", "bash"},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"repo", "gateway", "task", "sn", "lbrace"},
	},
	// 装前跑在安装 ISO 自带的 WinPE 里，既没有 PowerShell，也没有 HTTP 客户端（spec §8）。
	// 不要因为「以后能解决」就先写上 powershell：激活判定照这张表放行，脚本要到 WinPE 里才失败。
	"unattend": {
		interpreters: map[Timing][]Interpreter{
			"pre":       {"cmd"},
			"firstboot": {"cmd", "powershell"},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"smb", "gateway", "task", "sn", "lbrace"},
	},
	"esxi-ks": {
		interpreters: map[Timing][]Interpreter{
			"pre":            {"sh"},
			"post-installer": {"sh"},
			"firstboot":      {"sh"},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"gateway", "task", "sn", "lbrace"},
	},
}

// Valid 报告 t 是不是内置的时机，规则同 Family.Valid：只认表里的原样写法。
func (t Timing) Valid() bool {
	_, ok := timingRowOf(t)
	return ok
}

// Label 是时机的界面显示名；未知的时机返回空串。
func (t Timing) Label() string {
	r, _ := timingRowOf(t)
	return r.label
}

// Rank 是时机的先后：pre 为 1，两个 post 同为 2，firstboot 为 3；未知的时机为 0。
// 两个 post 谁先谁后由正文的书写位置决定、不由安装器决定（kickstart 的 %post --nochroot
// 与 %post 按出现顺序执行），所以同级；任务状态只按 Rank 推进，同级的算同一步（C33）。
func (t Timing) Rank() int {
	r, _ := timingRowOf(t)
	return r.rank
}

// Valid 报告 i 是不是内置的解释器，规则同 Family.Valid。
func (i Interpreter) Valid() bool {
	_, ok := interpreterRowOf(i)
	return ok
}

// Label 是解释器的界面显示名；未知的解释器返回空串。
func (i Interpreter) Label() string {
	r, _ := interpreterRowOf(i)
	return r.label
}

// Valid 报告 p 是不是内置的系统部件，规则同 Family.Valid。
func (p SystemPart) Valid() bool {
	_, ok := systemPartRowOf(p)
	return ok
}

// Label 是系统部件的界面显示名；未知的部件返回空串。
func (p SystemPart) Label() string {
	r, _ := systemPartRowOf(p)
	return r.label
}

// Valid 报告 v 是不是内置的系统值，规则同 Family.Valid。
func (v SystemValue) Valid() bool {
	_, ok := systemValueRowOf(v)
	return ok
}

// Label 是系统值的界面显示名；未知的系统值返回空串。
func (v SystemValue) Label() string {
	r, _ := systemValueRowOf(v)
	return r.label
}

// Timings 是格式 f 支持的时机，先按 Rank、同级再按 timingTable 的顺序；未知的格式返回空。
// 它和 Interpreters、RequiredParts、SystemValues 每次都返回新切片，调用方改了不影响目录。
func (f Format) Timings() []Timing {
	supported := capabilityTable[f].interpreters
	var out []Timing
	for _, r := range timingTable {
		if _, ok := supported[r.id]; ok {
			out = append(out, r.id)
		}
	}
	return out
}

// Interpreters 是格式 f 在时机 t 可用的解释器（C10）；f 不支持 t、或两者有一个未知时返回空。
func (f Format) Interpreters(t Timing) []Interpreter {
	return slices.Clone(capabilityTable[f].interpreters[t])
}

// RequiredParts 是格式 f 的正文里必须各恰好出现一次的系统部件（C15）；未知的格式返回空。
func (f Format) RequiredParts() []SystemPart {
	return slices.Clone(capabilityTable[f].parts)
}

// SystemValues 是格式 f 的正文与片段能引用的系统值；未知的格式返回空。
func (f Format) SystemValues() []SystemValue {
	return slices.Clone(capabilityTable[f].values)
}

func timingRowOf(t Timing) (timingRow, bool) {
	for _, r := range timingTable {
		if r.id == t {
			return r, true
		}
	}
	return timingRow{}, false
}

func interpreterRowOf(i Interpreter) (interpreterRow, bool) {
	for _, r := range interpreterTable {
		if r.id == i {
			return r, true
		}
	}
	return interpreterRow{}, false
}

func systemPartRowOf(p SystemPart) (systemPartRow, bool) {
	for _, r := range systemPartTable {
		if r.id == p {
			return r, true
		}
	}
	return systemPartRow{}, false
}

func systemValueRowOf(v SystemValue) (systemValueRow, bool) {
	for _, r := range systemValueTable {
		if r.id == v {
			return r, true
		}
	}
	return systemValueRow{}, false
}
