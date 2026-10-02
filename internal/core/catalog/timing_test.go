package catalog

import (
	"regexp"
	"slices"
	"testing"
)

// 下面几张表抄录计划「任务 3」的标识表与能力表，计划没给的显示名在表上注明了出处。
// 时机、系统部件、系统值的标识要写进正文的占位符（{{slot:名字@时机}}、{{lober:部件}}、
// {{sys:名字}}），解释器的标识随 hook 存进数据库，发布后都不能改名，所以期望值写死在
// 测试里，不从实现推出来。

// planTimings 的顺序就是计划里的顺序，也是 Rank 的顺序。
var planTimings = []struct {
	id    Timing
	rank  int
	label string
}{
	{"pre", 1, "装前"},
	{"post-installer", 2, "装后（安装环境）"},
	{"post-target", 2, "装后（目标系统内）"},
	{"firstboot", 3, "首次开机"},
}

// 计划没给解释器的显示名：前三个就用标识，PowerShell 照 spec 里的写法。
var planInterpreters = []struct {
	id    Interpreter
	label string
}{
	{"sh", "sh"},
	{"bash", "bash"},
	{"cmd", "cmd"},
	{"powershell", "PowerShell"},
}

// syslog 的显示名计划没给，取 spec §2.5 的叫法；另两个是计划括号里的说明。
var planSystemParts = []struct {
	id    SystemPart
	label string
}{
	{"syslog", "syslog 指向"},
	{"firstboot", "首次开机脚手架"},
	{"cleanup", "凭据清理"},
}

var planSystemValues = []struct {
	id    SystemValue
	label string
}{
	{"repo", "安装源地址"},
	{"iso", "原始 ISO 地址"},
	{"smb", "Windows 安装源共享"},
	{"gateway", "网关基地址"},
	{"task", "任务号"},
	{"sn", "主机 SN"},
	{"lbrace", "字面量 {{"},
}

// planTimingCap 是能力表里的一格：格式支持的一个时机，以及这个时机可用的解释器。
type planTimingCap struct {
	timing       Timing
	interpreters []Interpreter
}

// planCapability 是能力表的一行。timings 先按 Rank、同级再按 planTimings 的顺序写，
// 也就是 Format.Timings 应有的输出；没写出来的时机就是不支持。
type planCapability struct {
	format  Format
	timings []planTimingCap
	parts   []SystemPart
	values  []SystemValue
}

var planCapabilities = []planCapability{
	{
		format: "kickstart",
		timings: []planTimingCap{
			{"pre", []Interpreter{"sh", "bash"}},
			{"post-installer", []Interpreter{"sh", "bash"}},
			{"post-target", []Interpreter{"sh", "bash"}},
			{"firstboot", []Interpreter{"sh", "bash"}},
		},
		parts:  []SystemPart{"syslog", "firstboot", "cleanup"},
		values: []SystemValue{"repo", "gateway", "task", "sn", "lbrace"},
	},
	{
		// Debian 的安装环境只有 busybox sh（C10），装前和装后（安装环境）没有 bash。
		format: "preseed",
		timings: []planTimingCap{
			{"pre", []Interpreter{"sh"}},
			{"post-installer", []Interpreter{"sh"}},
			{"post-target", []Interpreter{"sh", "bash"}},
			{"firstboot", []Interpreter{"sh", "bash"}},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"repo", "gateway", "task", "sn", "lbrace"},
	},
	{
		// 只有 autoinstall 有 iso：Ubuntu 的安装器要经 url= 把整张 ISO 下进内存（spec §2.2）。
		format: "autoinstall",
		timings: []planTimingCap{
			{"pre", []Interpreter{"sh", "bash"}},
			{"post-installer", []Interpreter{"sh", "bash"}},
			{"post-target", []Interpreter{"sh", "bash"}},
			{"firstboot", []Interpreter{"sh", "bash"}},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"repo", "iso", "gateway", "task", "sn", "lbrace"},
	},
	{
		format: "autoyast",
		timings: []planTimingCap{
			{"pre", []Interpreter{"sh", "bash"}},
			{"post-installer", []Interpreter{"sh", "bash"}},
			{"post-target", []Interpreter{"sh", "bash"}},
			{"firstboot", []Interpreter{"sh", "bash"}},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"repo", "gateway", "task", "sn", "lbrace"},
	},
	{
		format: "agama",
		timings: []planTimingCap{
			{"pre", []Interpreter{"sh", "bash"}},
			{"post-installer", []Interpreter{"sh", "bash"}},
			{"post-target", []Interpreter{"sh", "bash"}},
			{"firstboot", []Interpreter{"sh", "bash"}},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"repo", "gateway", "task", "sn", "lbrace"},
	},
	{
		// 装前跑在安装 ISO 自带的 WinPE 里，没有 PowerShell（spec §8）。这一格照「只有 cmd」
		// 登记，不因为「以后能解决」就先写上 powershell（计划任务 3 的风险）。
		format: "unattend",
		timings: []planTimingCap{
			{"pre", []Interpreter{"cmd"}},
			{"firstboot", []Interpreter{"cmd", "powershell"}},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"smb", "gateway", "task", "sn", "lbrace"},
	},
	{
		format: "esxi-ks",
		timings: []planTimingCap{
			{"pre", []Interpreter{"sh"}},
			{"post-installer", []Interpreter{"sh"}},
			{"firstboot", []Interpreter{"sh"}},
		},
		parts:  []SystemPart{"firstboot", "cleanup"},
		values: []SystemValue{"gateway", "task", "sn", "lbrace"},
	},
}

// 公开接口里没有列出全部时机、解释器、部件或系统值的函数，「一个不多」只能直接遍历
// 实现里的表，和 series_test.go 的 formatTableIDs 同理。
func timingTableIDs() []Timing {
	var out []Timing
	for _, r := range timingTable {
		out = append(out, r.id)
	}
	return out
}

func interpreterTableIDs() []Interpreter {
	var out []Interpreter
	for _, r := range interpreterTable {
		out = append(out, r.id)
	}
	return out
}

func systemPartTableIDs() []SystemPart {
	var out []SystemPart
	for _, r := range systemPartTable {
		out = append(out, r.id)
	}
	return out
}

func systemValueTableIDs() []SystemValue {
	var out []SystemValue
	for _, r := range systemValueTable {
		out = append(out, r.id)
	}
	return out
}

func Test时机解释器部件与系统值的标识与计划一致(t *testing.T) {
	// 「一个不多、一个不少」比较排序后的标识：词表内部的顺序不对外，时机的对外顺序由
	// Format.Timings 的测试管。
	t.Run("时机", func(t *testing.T) {
		var planned []Timing
		for _, p := range planTimings {
			planned = append(planned, p.id)
			if v := p.id; !v.Valid() || v.Label() != p.label || v.Rank() != p.rank {
				t.Errorf("时机 %q：Valid() = %v、Label() = %q、Rank() = %d，想要 true、%q、%d",
					v, v.Valid(), v.Label(), v.Rank(), p.label, p.rank)
			}
		}
		if got, want := slices.Sorted(slices.Values(timingTableIDs())), slices.Sorted(slices.Values(planned)); !slices.Equal(got, want) {
			t.Errorf("目录里的时机 = %q，想要恰好 %q", got, want)
		}
	})
	t.Run("解释器", func(t *testing.T) {
		var planned []Interpreter
		for _, p := range planInterpreters {
			planned = append(planned, p.id)
			if v := p.id; !v.Valid() || v.Label() != p.label {
				t.Errorf("解释器 %q：Valid() = %v、Label() = %q，想要 true、%q", v, v.Valid(), v.Label(), p.label)
			}
		}
		if got, want := slices.Sorted(slices.Values(interpreterTableIDs())), slices.Sorted(slices.Values(planned)); !slices.Equal(got, want) {
			t.Errorf("目录里的解释器 = %q，想要恰好 %q", got, want)
		}
	})
	t.Run("系统部件", func(t *testing.T) {
		var planned []SystemPart
		for _, p := range planSystemParts {
			planned = append(planned, p.id)
			if v := p.id; !v.Valid() || v.Label() != p.label {
				t.Errorf("系统部件 %q：Valid() = %v、Label() = %q，想要 true、%q", v, v.Valid(), v.Label(), p.label)
			}
		}
		if got, want := slices.Sorted(slices.Values(systemPartTableIDs())), slices.Sorted(slices.Values(planned)); !slices.Equal(got, want) {
			t.Errorf("目录里的系统部件 = %q，想要恰好 %q", got, want)
		}
	})
	t.Run("系统值", func(t *testing.T) {
		var planned []SystemValue
		for _, p := range planSystemValues {
			planned = append(planned, p.id)
			if v := p.id; !v.Valid() || v.Label() != p.label {
				t.Errorf("系统值 %q：Valid() = %v、Label() = %q，想要 true、%q", v, v.Valid(), v.Label(), p.label)
			}
		}
		if got, want := slices.Sorted(slices.Values(systemValueTableIDs())), slices.Sorted(slices.Values(planned)); !slices.Equal(got, want) {
			t.Errorf("目录里的系统值 = %q，想要恰好 %q", got, want)
		}
	})
}

func Test时机解释器部件与系统值的标识只用小写字母数字与连字符(t *testing.T) {
	// 这些标识原样写进占位符、存进数据库（全局约定：标识一律小写 ASCII）。占位符的种类、
	// 名字、时机之间靠冒号和 @ 分隔，标识里混进它们、空白或花括号，正文扫描就会切错。
	var ids []string
	for _, v := range timingTableIDs() {
		ids = append(ids, string(v))
	}
	for _, v := range interpreterTableIDs() {
		ids = append(ids, string(v))
	}
	for _, v := range systemPartTableIDs() {
		ids = append(ids, string(v))
	}
	for _, v := range systemValueTableIDs() {
		ids = append(ids, string(v))
	}
	ident := regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	for _, id := range ids {
		if !ident.MatchString(id) {
			t.Errorf("标识 %q 不匹配 %s", id, ident)
		}
	}
}

func Test未知或大小写不同的时机解释器部件与系统值无效(t *testing.T) {
	// 规则同族、系、格式：只认表里的原样写法，不做规范化。显示名和安装器里的叫法
	// （%post、pwsh、url）都不是标识。
	for _, v := range []Timing{"", "Pre", "PRE", " pre", "pre ", "post", "post_target", "postinstaller", "%post", "first-boot", "装前", "sh"} {
		if v.Valid() || v.Label() != "" || v.Rank() != 0 {
			t.Errorf("时机 %q：Valid() = %v、Label() = %q、Rank() = %d，想要 false、空、0", v, v.Valid(), v.Label(), v.Rank())
		}
	}
	for _, v := range []Interpreter{"", "SH", "Bash", "PowerShell", "pwsh", "ash", "busybox", "/bin/sh", "cmd.exe", "python", "pre"} {
		if v.Valid() || v.Label() != "" {
			t.Errorf("解释器 %q：Valid() = %v、Label() = %q，想要 false、空", v, v.Valid(), v.Label())
		}
	}
	for _, v := range []SystemPart{"", "Syslog", "SYSLOG", " syslog", "cleanup\n", "first-boot", "rsyslog", "首次开机脚手架", "repo"} {
		if v.Valid() || v.Label() != "" {
			t.Errorf("系统部件 %q：Valid() = %v、Label() = %q，想要 false、空", v, v.Valid(), v.Label())
		}
	}
	for _, v := range []SystemValue{"", "Repo", "REPO", " repo", "lbrace ", "rbrace", "{{", "url", "gw", "syslog", "安装源地址"} {
		if v.Valid() || v.Label() != "" {
			t.Errorf("系统值 %q：Valid() = %v、Label() = %q，想要 false、空", v, v.Valid(), v.Label())
		}
	}
}

func Test能力表逐格与计划一致(t *testing.T) {
	var planned []Format
	for _, pc := range planCapabilities {
		f := pc.format
		planned = append(planned, f)
		var timings []Timing
		interpreters := map[Timing][]Interpreter{}
		for _, c := range pc.timings {
			timings = append(timings, c.timing)
			interpreters[c.timing] = c.interpreters
		}
		if got := f.Timings(); !slices.Equal(got, timings) {
			t.Errorf("%s.Timings() = %q，想要 %q", f, got, timings)
		}
		// 四个时机逐个问，计划里没写的时机要得到空。
		for _, p := range planTimings {
			if got, want := f.Interpreters(p.id), interpreters[p.id]; !slices.Equal(got, want) {
				t.Errorf("%s.Interpreters(%s) = %q，想要 %q", f, p.id, got, want)
			}
		}
		if got := f.RequiredParts(); !slices.Equal(got, pc.parts) {
			t.Errorf("%s.RequiredParts() = %q，想要 %q", f, got, pc.parts)
		}
		if got := f.SystemValues(); !slices.Equal(got, pc.values) {
			t.Errorf("%s.SystemValues() = %q，想要 %q", f, got, pc.values)
		}
	}
	// 新加了格式却忘了登记能力，查询不会报错，只会对它一律返回空：什么时机都不支持，
	// 也不要求任何部件。所以抄录的能力表要覆盖目录里的每种格式，新格式先在这里写下期望。
	if got, want := slices.Sorted(slices.Values(planned)), slices.Sorted(slices.Values(formatTableIDs())); !slices.Equal(got, want) {
		t.Errorf("抄录的能力表覆盖的格式 = %q，目录里的格式 = %q，想要一致", got, want)
	}
}

// catalogValue 是能力表格子里的取值：解释器、系统部件、系统值都是这样的词表标识。
type catalogValue interface {
	~string
	Valid() bool
}

// checkCell 断言能力表一格里的每一项都是词表里的值，且同一格里不重复。where 写成对应的
// 查询，报错时一眼看出是哪一格。
func checkCell[T catalogValue](t *testing.T, where string, items []T) {
	t.Helper()
	seen := map[T]bool{}
	for _, v := range items {
		if !v.Valid() {
			t.Errorf("%s 里有未知的值 %q", where, v)
		}
		if seen[v] {
			t.Errorf("%s 里重复登记了 %q", where, v)
		}
		seen[v] = true
	}
}

func Test能力表只登记目录里的值且每格不重复(t *testing.T) {
	// 能力按格式、再按时机查。键拼错了查询不会报错，那一行或那一格只是永远查不到；
	// Timings 又只列时机表里的时机，拼错的时机会悄无声息地消失。格子里的值同样没人兜：
	// 误加的 python 能让 hook 通过激活，Label() 却是空串；gateway 拼成 gatway，引用它的
	// 片段和模板全被判不可用。「逐格与计划一致」比对的是一起改的抄录，两边一起写错时
	// 只有这里报红，所以直接查实现里的表。
	for f, r := range capabilityTable {
		if !f.Valid() {
			t.Errorf("能力表登记了未知的格式 %q", f)
		}
		for tm, interpreters := range r.interpreters {
			if !tm.Valid() {
				t.Errorf("格式 %q 的能力登记了未知的时机 %q", f, tm)
			}
			checkCell(t, string(f)+".Interpreters("+string(tm)+")", interpreters)
		}
		checkCell(t, string(f)+".RequiredParts()", r.parts)
		checkCell(t, string(f)+".SystemValues()", r.values)
	}
}

func Test每种格式都支持首次开机时机并要求首次开机与凭据清理部件(t *testing.T) {
	// 任务要等首次开机的脚本跑完才进入「完成」，最后还要清理目标机上的凭据（spec §5.4 第 5 步）。
	// 哪种格式缺了首次开机的时机或部件，用它装的机器就永远到不了「完成」；缺了凭据清理，
	// 装出的机器会把应答文件副本里的密码哈希和首次开机 token 留在盘上。
	for _, f := range formatTableIDs() {
		if !slices.Contains(f.Timings(), "firstboot") {
			t.Errorf("%s.Timings() = %q，想要含 firstboot", f, f.Timings())
		}
		for _, p := range []SystemPart{"firstboot", "cleanup"} {
			if !slices.Contains(f.RequiredParts(), p) {
				t.Errorf("%s.RequiredParts() = %q，想要含 %s", f, f.RequiredParts(), p)
			}
		}
	}
}

func Test受支持的时机都至少有一个解释器(t *testing.T) {
	// 支持一个时机，就是说那里能跑脚本。一个解释器都没有的话，正文里能写这个时机的
	// 执行插槽、模板也能激活，槽里却放不进任何脚本（C10、C16）。
	for _, f := range formatTableIDs() {
		for _, tm := range f.Timings() {
			if len(f.Interpreters(tm)) == 0 {
				t.Errorf("%s 支持时机 %s，Interpreters 却为空", f, tm)
			}
		}
	}
}

func Test时机次序与两个post同级(t *testing.T) {
	// 任务状态只按时机的先后推进（C33）。两个 post 的先后由正文的书写位置决定，不由
	// 安装器决定，所以同级，推进时算同一步。
	pre, installer, target, firstboot := Timing("pre").Rank(), Timing("post-installer").Rank(), Timing("post-target").Rank(), Timing("firstboot").Rank()
	if !(0 < pre && pre < installer && installer == target && target < firstboot) {
		t.Errorf("Rank：pre = %d、post-installer = %d、post-target = %d、firstboot = %d，想要 0 < pre < post-installer = post-target < firstboot",
			pre, installer, target, firstboot)
	}
	// Timings 先按 Rank 排是对外的契约：调用方按它的顺序列出可选的时机，不再自己排序。
	for _, f := range formatTableIDs() {
		got := f.Timings()
		for i := 1; i < len(got); i++ {
			if got[i-1].Rank() > got[i].Rank() {
				t.Errorf("%s.Timings() = %q：%s 排在 %s 前面，Rank 却更大", f, got, got[i-1], got[i])
			}
		}
	}
}

func Test未知格式与不支持的时机查询为空(t *testing.T) {
	t.Run("未知格式", func(t *testing.T) {
		// 显示名、族、系的标识都不是格式。
		for _, f := range []Format{"", "Kickstart", "KICKSTART", " preseed", "ESXi ks", "esxi", "ks", "linux", "rocky"} {
			if got := f.Timings(); len(got) != 0 {
				t.Errorf("%q.Timings() = %q，想要空", f, got)
			}
			for _, p := range planTimings {
				if got := f.Interpreters(p.id); len(got) != 0 {
					t.Errorf("%q.Interpreters(%s) = %q，想要空", f, p.id, got)
				}
			}
			if got := f.RequiredParts(); len(got) != 0 {
				t.Errorf("%q.RequiredParts() = %q，想要空", f, got)
			}
			if got := f.SystemValues(); len(got) != 0 {
				t.Errorf("%q.SystemValues() = %q，想要空", f, got)
			}
		}
	})
	t.Run("格式不支持的时机", func(t *testing.T) {
		// 模板激活与脚本激活都靠这里返回空，挡住格式没有的时机（C10、C14）。
		cases := []struct {
			f  Format
			tm Timing
		}{
			{"unattend", "post-installer"},
			{"unattend", "post-target"},
			{"esxi-ks", "post-target"},
		}
		for _, c := range cases {
			if got := c.f.Interpreters(c.tm); len(got) != 0 {
				t.Errorf("%s.Interpreters(%s) = %q，想要空", c.f, c.tm, got)
			}
			if slices.Contains(c.f.Timings(), c.tm) {
				t.Errorf("%s.Timings() = %q，不该含 %s", c.f, c.f.Timings(), c.tm)
			}
		}
	})
	t.Run("未知时机", func(t *testing.T) {
		for _, f := range formatTableIDs() {
			for _, tm := range []Timing{"", "Pre", "PRE", " pre", "post", "post_target", "%post", "首次开机"} {
				if got := f.Interpreters(tm); len(got) != 0 {
					t.Errorf("%s.Interpreters(%q) = %q，想要空", f, tm, got)
				}
			}
		}
	})
}

func Test改动能力查询返回的切片不影响下一次查询(t *testing.T) {
	// 能力表是内置的静态事实（C1）：调用方拿去排序、改写的切片，不能连带改掉目录。
	t.Run("Timings", func(t *testing.T) {
		got := Format("kickstart").Timings()
		for i := range got {
			got[i] = "pre"
		}
		want := []Timing{"pre", "post-installer", "post-target", "firstboot"}
		if again := Format("kickstart").Timings(); !slices.Equal(again, want) {
			t.Errorf("改过上一次的结果后 kickstart.Timings() = %q，想要 %q", again, want)
		}
	})
	t.Run("Interpreters", func(t *testing.T) {
		got := Format("unattend").Interpreters("firstboot")
		for i := range got {
			got[i] = "bash"
		}
		want := []Interpreter{"cmd", "powershell"}
		if again := Format("unattend").Interpreters("firstboot"); !slices.Equal(again, want) {
			t.Errorf("改过上一次的结果后 unattend.Interpreters(firstboot) = %q，想要 %q", again, want)
		}
	})
	t.Run("RequiredParts", func(t *testing.T) {
		got := Format("kickstart").RequiredParts()
		for i := range got {
			got[i] = "cleanup"
		}
		want := []SystemPart{"syslog", "firstboot", "cleanup"}
		if again := Format("kickstart").RequiredParts(); !slices.Equal(again, want) {
			t.Errorf("改过上一次的结果后 kickstart.RequiredParts() = %q，想要 %q", again, want)
		}
	})
	t.Run("SystemValues", func(t *testing.T) {
		got := Format("autoinstall").SystemValues()
		for i := range got {
			got[i] = "smb"
		}
		want := []SystemValue{"repo", "iso", "gateway", "task", "sn", "lbrace"}
		if again := Format("autoinstall").SystemValues(); !slices.Equal(again, want) {
			t.Errorf("改过上一次的结果后 autoinstall.SystemValues() = %q，想要 %q", again, want)
		}
	})
}
