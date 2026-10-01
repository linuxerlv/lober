package activation

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/scan"
)

// fragmentBody 是正例里 rocky 与 almalinux 共用的 kickstart 片段。它把片段里允许的写法
// 都用上一次：标量参数、cidr 的子值、逐行重复的列表、|join、该格式可用的系统值，以及
// 不归 lober 管的 {{ … }}。
const fragmentBody = "ignoredisk --only-use={{param:disk}}\n" +
	"part /boot --fstype=xfs --size=1024 --ondisk={{param:disk}}\n" +
	"part pv.01 --size=1 --grow --ondisk={{param:disk}}\n" +
	"volgroup vg0 pv.01\n" +
	"logvol / --vgname=vg0 --name=root --size={{param:root_mb}}\n" +
	"network --bootproto=static --ip={{param:addr.addr}} --netmask={{param:addr.netmask}}\n" +
	"# NTP：{{param:ntp|join}}；任务 {{sys:task}}，SN {{sys:sn}}，{{ 原样保留 }}\n" +
	"%packages\n" +
	"{{param:pkgs}}\n" +
	"%end\n"

// scriptBody 是正例里 debian 与 ubuntu 共用的装后脚本，只经环境变量拿参数，text 也能用。
const scriptBody = "#!/bin/sh\n" +
	"printf '%s\\n' \"$LOBER_PARAM_MOTD\" >/etc/motd\n" +
	"for p in $LOBER_PARAM_PKGS; do echo \"$p\"; done\n"

// goodHook 是能通过激活判定的完整 hook。每个反例都在它上面只改一处。
func goodHook() model.Hook {
	return model.Hook{
		ID:      "lvm",
		Version: 3,
		Name:    "LVM 分区",
		State:   model.VersionReview,
		Author:  "alice",
		Params: []model.ParamDecl{
			{Name: "disk", Label: "系统盘", Type: "diskpath", Default: "/dev/sda"},
			{Name: "root_mb", Label: "根分区（MB）", Type: "int", Default: "20480"},
			{Name: "addr", Label: "地址与前缀", Type: "cidr"},
			{Name: "pkgs", Label: "软件包", Type: "word", List: true},
			{Name: "ntp", Label: "NTP 服务器", Type: "hostname", List: true},
			{Name: "motd", Label: "登录提示", Type: "text"},
		},
		TimeoutSec: 600,
		Required:   true,
		Impls: []model.Impl{
			{
				Family: "linux",
				Series: []catalog.Series{"rocky", "almalinux"},
				Kind:   model.ImplFragment,
				Body:   fragmentBody,
			},
			{
				Family:      "linux",
				Series:      []catalog.Series{"debian", "ubuntu"},
				Kind:        model.ImplScript,
				Body:        scriptBody,
				Interpreter: "sh",
				Timings:     []catalog.Timing{"post-target"},
			},
		},
	}
}

func codesOf(ps []model.Problem) []model.Code {
	out := make([]model.Code, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Code)
	}
	return out
}

// onlyCode 断言报出了问题，而且每一条都是 want：反例只改了一处，就不该连带报出别的代码。
func onlyCode(t *testing.T, ps []model.Problem, want model.Code) {
	t.Helper()
	if len(ps) == 0 {
		t.Fatalf("应报 %s，却通过了", want)
	}
	for _, p := range ps {
		if p.Code != want {
			t.Fatalf("应只报 %s，得到 %v", want, codesOf(ps))
		}
	}
}

func Test正例通过激活判定(t *testing.T) {
	h := goodHook()
	if ps := Hook(h); len(ps) != 0 {
		t.Fatalf("正例应通过，得到：%v", ps)
	}
	if !reflect.DeepEqual(h, goodHook()) {
		t.Error("判定改动了入参")
	}
}

// 只含片段的 hook 不跑脚本，超时与它无关（C11 只约束脚本）。
func Test只有片段时不查超时(t *testing.T) {
	h := goodHook()
	h.Impls = h.Impls[:1]
	h.TimeoutSec = 0
	if ps := Hook(h); len(ps) != 0 {
		t.Fatalf("只有片段的 hook 应通过，得到：%v", ps)
	}
}

// edit 是在正例上只改一处的反例。
type edit struct {
	name string
	mod  func(h *model.Hook)
}

func frag(h *model.Hook) *model.Impl   { return &h.Impls[0] }
func script(h *model.Hook) *model.Impl { return &h.Impls[1] }

// fragLine 在片段末尾追加一行。
func fragLine(line string) func(h *model.Hook) {
	return func(h *model.Hook) { frag(h).Body += line + "\n" }
}

// runEdits 对每个反例断言它恰好报出 want。
func runEdits(t *testing.T, want model.Code, edits []edit) {
	t.Helper()
	for _, e := range edits {
		t.Run(e.name, func(t *testing.T) {
			h := goodHook()
			e.mod(&h)
			onlyCode(t, Hook(h), want)
		})
	}
}

func Test名称不合法(t *testing.T) {
	runEdits(t, "name_invalid", []edit{
		{"空名称", func(h *model.Hook) { h.Name = "" }},
		{"只有空白", func(h *model.Hook) { h.Name = " \t " }},
		{"含控制字符", func(h *model.Hook) { h.Name = "LVM\x00分区" }},
		{"超过 64 个字", func(h *model.Hook) { h.Name = strings.Repeat("分", 65) }},
	})
}

// 参数声明由全部实现共用（C9），逐个校验，名字在 hook 内不能重复。
func Test参数声明不合法或重名(t *testing.T) {
	runEdits(t, "param_decl_invalid", []edit{
		{"text 声明成列表", func(h *model.Hook) {
			h.Params = append(h.Params, model.ParamDecl{Name: "notes", Label: "备注", Type: "text", List: true})
		}},
		{"类型不认识", func(h *model.Hook) {
			h.Params = append(h.Params, model.ParamDecl{Name: "size", Label: "大小", Type: "float"})
		}},
		{"名字不是标识符", func(h *model.Hook) {
			h.Params = append(h.Params, model.ParamDecl{Name: "Swap", Label: "交换分区", Type: "int"})
		}},
		{"默认值通不过校验", func(h *model.Hook) { h.Params[0].Default = "sda" }},
		{"名字重复", func(h *model.Hook) {
			h.Params = append(h.Params, model.ParamDecl{Name: "disk", Label: "数据盘", Type: "diskpath"})
		}},
	})
}

func Test没有实现(t *testing.T) {
	runEdits(t, "hook_no_impl", []edit{
		{"nil", func(h *model.Hook) { h.Impls = nil }},
		{"空切片", func(h *model.Hook) { h.Impls = []model.Impl{} }},
	})
}

// 一份实现只属于一个族，覆盖的系非空、合法且都属于该族（C8）。
func Test实现的族或系不对(t *testing.T) {
	runEdits(t, "impl_series_invalid", []edit{
		{"族不认识", func(h *model.Hook) { frag(h).Family = "Linux" }},
		{"族为空", func(h *model.Hook) { frag(h).Family = "" }},
		{"没勾系", func(h *model.Hook) { frag(h).Series = nil }},
		{"系不认识", func(h *model.Hook) { frag(h).Series = append(frag(h).Series, "centos") }},
		{"系大小写不同", func(h *model.Hook) { frag(h).Series[1] = "AlmaLinux" }},
		{"系属于别的族", func(h *model.Hook) { frag(h).Series = append(frag(h).Series, "esxi") }},
		{"同一个系勾了两次", func(h *model.Hook) { frag(h).Series = append(frag(h).Series, "rocky") }},
		{"脚本的系属于别的族", func(h *model.Hook) { script(h).Family = "windows" }},
		// 推不出格式，就不再按格式查时机，不会因同一个错误再报 script_timing_unsupported。
		{"脚本勾了不认识的系", func(h *model.Hook) { script(h).Series = append(script(h).Series, "centos") }},
	})
}

// 写错的声明已经报过，引用它的地方不再按类型报一遍。
func Test引用不合法的声明时只报声明(t *testing.T) {
	h := goodHook()
	h.Params = append(h.Params, model.ParamDecl{Name: "size", Label: "大小", Type: "float"})
	frag(&h).Body += "part /data --size={{param:size}}\n"
	onlyCode(t, Hook(h), "param_decl_invalid")
}

// 一个系最多出现在一份实现里，否则渲染时不知道取哪一份（C8）。
func Test同一个系出现在两份实现里(t *testing.T) {
	runEdits(t, "impl_series_overlap", []edit{
		{"脚本也覆盖 rocky", func(h *model.Hook) { script(h).Series = append(script(h).Series, "rocky") }},
		{"第三份实现与片段重叠", func(h *model.Hook) {
			h.Impls = append(h.Impls, model.Impl{Family: "linux", Series: []catalog.Series{"almalinux"},
				Kind: model.ImplFragment, Body: "{{param:disk}}\n"})
		}},
	})
}

// 片段按一种格式的语法写，覆盖的系必须同格式（C8）。
func Test片段覆盖的系不同格式(t *testing.T) {
	runEdits(t, "fragment_mixed_formats", []edit{
		{"kickstart 与 autoyast", func(h *model.Hook) { frag(h).Series = append(frag(h).Series, "sles15") }},
		{"autoyast 与 agama", func(h *model.Hook) { frag(h).Series = []catalog.Series{"sles15", "sles16"} }},
	})
}

// 扫描的问题原样带出：代码、说明与行列都不变。
func Test片段的扫描错误原样带出(t *testing.T) {
	runEdits(t, "placeholder_unclosed", []edit{{"未闭合", fragLine("part swap --size={{param:disk")}})
	runEdits(t, "placeholder_unknown_kind", []edit{{"种类不认识", fragLine("url --url={{http://repo/}}")}})
	runEdits(t, "placeholder_malformed", []edit{{"写法不对", fragLine("{{param:Disk}}")}})
	runEdits(t, "sysvalue_unknown", []edit{{"系统值不认识", fragLine("# {{sys:mirror}}")}})
}

// hook 只有一层：片段里不能引用插槽或系统部件（C8）。
func Test片段里有插槽或系统部件(t *testing.T) {
	runEdits(t, "fragment_forbidden_placeholder", []edit{
		{"片段插槽", fragLine("{{slot:分区}}")},
		{"执行插槽", fragLine("{{slot:装后@post-target}}")},
		{"系统部件", fragLine("{{lober:syslog}}")},
	})
}

func Test片段引用的系统值在该格式不可用(t *testing.T) {
	runEdits(t, "sysvalue_not_allowed", []edit{
		{"kickstart 没有 smb", fragLine("# {{sys:smb}}")},
		{"kickstart 没有 iso", fragLine("# {{sys:iso}}")},
	})
}

// 片段引用的参数必须都声明过（C9），而且不能是 text（C19）。
func Test片段引用的参数没有声明(t *testing.T) {
	runEdits(t, "param_undeclared", []edit{
		{"没声明", fragLine("rootpw --iscrypted {{param:rootpw}}")},
		{"声明改了名，片段里的旧名都落空", func(h *model.Hook) { h.Params[0].Name = "sysdisk" }},
	})
	runEdits(t, "param_text_in_fragment", []edit{{"text 进片段", fragLine("# {{param:motd}}")}})
}

func Test片段的列表规则与子值(t *testing.T) {
	runEdits(t, "list_repeat_conflict", []edit{
		{"一行两个逐行重复的列表", fragLine("# {{param:pkgs}} {{param:ntp}}")},
	})
	runEdits(t, "param_accessor_invalid", []edit{
		{"非 cidr 取子值", fragLine("# {{param:disk.addr}}")},
		{"cidr 没有这个子值", fragLine("# {{param:addr.mask}}")},
		{"标量用 |join", fragLine("# {{param:disk|join}}")},
		{"cidr 子值后跟 |join", fragLine("# {{param:addr.prefix|join}}")},
	})
}

// 同一个列表参数在一行里出现多次、或取它的子值，逐行重复时每次代入同一项，没有歧义。
func Test同一列表参数在一行里多次出现不冲突(t *testing.T) {
	h := goodHook()
	h.Params = append(h.Params, model.ParamDecl{Name: "nets", Label: "业务网", Type: "cidr", List: true})
	frag(&h).Body += "network --ip={{param:nets.addr}} --netmask={{param:nets.netmask}} # {{param:nets}}\n"
	if ps := Hook(h); len(ps) != 0 {
		t.Fatalf("应通过，得到：%v", ps)
	}
}

// 脚本要有正文，并声明解释器和至少一个时机（C10）。
func Test脚本不完整(t *testing.T) {
	runEdits(t, "script_incomplete", []edit{
		{"正文为空", func(h *model.Hook) { script(h).Body = "" }},
		{"正文只有空白", func(h *model.Hook) { script(h).Body = " \n\t\r\n" }},
		{"没声明解释器", func(h *model.Hook) { script(h).Interpreter = "" }},
		{"没声明时机", func(h *model.Hook) { script(h).Timings = nil }},
		{"种类既不是片段也不是脚本", func(h *model.Hook) { script(h).Kind = "macro" }},
	})
}

// 脚本可以跨格式，但声明的每个时机在它覆盖的每个系里都要支持（C8）。
func Test脚本的时机该格式不支持(t *testing.T) {
	runEdits(t, "script_timing_unsupported", []edit{
		{"时机不认识", func(h *model.Hook) { script(h).Timings = []catalog.Timing{"post"} }},
	})
	// unattend 没有两个 post 时机。
	h := model.Hook{ID: "win", Version: 1, Name: "Windows 装后", TimeoutSec: 60, Impls: []model.Impl{{
		Family: "windows", Series: []catalog.Series{"windows-server"}, Kind: model.ImplScript,
		Body: "echo ok\r\n", Interpreter: "cmd", Timings: []catalog.Timing{"post-target"},
	}}}
	onlyCode(t, Hook(h), "script_timing_unsupported")
}

// 解释器在声明的每个时机、覆盖的每个系里都要可用（C10）：Debian 装前只有 busybox sh。
func Test脚本的解释器在该时机不可用(t *testing.T) {
	runEdits(t, "script_interpreter_unavailable", []edit{
		{"bash 在 pre 覆盖 debian", func(h *model.Hook) {
			script(h).Interpreter = "bash"
			script(h).Timings = []catalog.Timing{"pre"}
		}},
		{"解释器不认识", func(h *model.Hook) { script(h).Interpreter = "python" }},
		{"cmd 不在 Linux 上", func(h *model.Hook) { script(h).Interpreter = "cmd" }},
	})
}

// 脚本只经 EnvName 的环境变量拿参数；只认字面出现的变量名。
func Test脚本引用的环境变量没有声明(t *testing.T) {
	runEdits(t, "param_undeclared", []edit{
		{"没声明的参数", func(h *model.Hook) { script(h).Body += "mkfs.xfs \"$LOBER_PARAM_DATA_DISK\"\n" }},
		{"花括号写法", func(h *model.Hook) { script(h).Body += "echo ${LOBER_PARAM_HOST}\n" }},
		{"大小写不同是另一个变量", func(h *model.Hook) { script(h).Body += "echo $LOBER_PARAM_motd\n" }},
	})
}

// 前缀之前还有标识符字符时是另一个变量，后面没有名字时不是变量，都不算引用。
func Test脚本里不是参数变量的写法不报(t *testing.T) {
	h := goodHook()
	script(&h).Body += "echo $MY_LOBER_PARAM_X $LOBER_PARAM_ \"$LOBER_PARAM_DISK\"\n"
	if ps := Hook(h); len(ps) != 0 {
		t.Fatalf("应通过，得到：%v", ps)
	}
}

// 有脚本实现时超时要在 1–21600 秒之间（C11），两端都算合法。
func Test超时不在范围内(t *testing.T) {
	runEdits(t, "timeout_invalid", []edit{
		{"0 秒", func(h *model.Hook) { h.TimeoutSec = 0 }},
		{"负数", func(h *model.Hook) { h.TimeoutSec = -1 }},
		{"超过 6 小时", func(h *model.Hook) { h.TimeoutSec = 21601 }},
	})
	for _, sec := range []int{1, 21600} {
		h := goodHook()
		h.TimeoutSec = sec
		if ps := Hook(h); len(ps) != 0 {
			t.Errorf("超时 %d 秒应通过，得到：%v", sec, ps)
		}
	}
}

// hook 整体的问题指向 hook，实现的问题指向那份实现（Path 为它覆盖的系），带正文位置的
// 带上行列：前端靠 Subject 把错误标到具体的地方。
func TestSubject指向出问题的位置(t *testing.T) {
	cases := []struct {
		name string
		mod  func(h *model.Hook)
		want model.Subject
	}{
		{"名称", func(h *model.Hook) { h.Name = "" }, model.Subject{Kind: "hook", ID: "lvm@3"}},
		{"参数声明", func(h *model.Hook) { h.Params[1].Min = new(int64(99999)) },
			model.Subject{Kind: "hook", ID: "lvm@3", Path: "root_mb"}},
		{"没有实现", func(h *model.Hook) { h.Impls = nil }, model.Subject{Kind: "hook", ID: "lvm@3"}},
		{"超时", func(h *model.Hook) { h.TimeoutSec = 0 }, model.Subject{Kind: "hook", ID: "lvm@3"}},
		{"系不对", func(h *model.Hook) { frag(h).Series = nil }, model.Subject{Kind: "impl", ID: "lvm@3"}},
		{"系重叠指向后一份实现", func(h *model.Hook) { script(h).Series = append(script(h).Series, "rocky") },
			model.Subject{Kind: "impl", ID: "lvm@3", Path: "debian,ubuntu,rocky"}},
		{"片段的正文位置按 rune 计", func(h *model.Hook) { frag(h).Body = "# 中文{{param:motd}}\n" },
			model.Subject{Kind: "impl", ID: "lvm@3", Path: "rocky,almalinux", Line: 1, Col: 5}},
		{"扫描问题改指向实现、保留行列", func(h *model.Hook) { frag(h).Body = "a\n  {{slot:x\n" },
			model.Subject{Kind: "impl", ID: "lvm@3", Path: "rocky,almalinux", Line: 2, Col: 3}},
		{"脚本里的变量位置按 rune 计", func(h *model.Hook) { script(h).Body = "#!/bin/sh\n# 说明 $LOBER_PARAM_X\n" },
			model.Subject{Kind: "impl", ID: "lvm@3", Path: "debian,ubuntu", Line: 2, Col: 7}},
		{"脚本的时机", func(h *model.Hook) { script(h).Timings = []catalog.Timing{"post"} },
			model.Subject{Kind: "impl", ID: "lvm@3", Path: "debian,ubuntu"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := goodHook()
			c.mod(&h)
			ps := Hook(h)
			if len(ps) == 0 {
				t.Fatal("应报出问题，却通过了")
			}
			if ps[0].Subject != c.want {
				t.Errorf("Subject = %+v，应为 %+v", ps[0].Subject, c.want)
			}
		})
	}
}

// 扫描的问题只改 Subject 指向实现，代码、说明和下一步与扫描给的逐字相同。
func Test扫描问题的说明不变(t *testing.T) {
	body := "{{param:disk}}\n{{lober:nope}}\n"
	_, want := scan.Scan(body)
	h := goodHook()
	frag(&h).Body = body
	got := Hook(h)
	if len(want) != 1 || len(got) != 1 {
		t.Fatalf("扫描应报一条，判定也应只报这一条：扫描 %v，判定 %v", want, got)
	}
	if got[0].Code != want[0].Code || got[0].Message != want[0].Message || got[0].Next != want[0].Next {
		t.Errorf("判定报出 %+v，扫描给的是 %+v", got[0], want[0])
	}
}

// 同样的输入永远得到同样顺序的输出：先 hook 整体，再按实现的顺序，超时最后；
// 一份实现里正文的问题按位置排。
func Test问题的顺序固定(t *testing.T) {
	h := goodHook()
	h.Name = ""
	h.Params = append(h.Params, model.ParamDecl{Name: "disk", Label: "重名", Type: "word"})
	frag(&h).Body = "# {{param:motd}} {{sys:smb}}\n{{slot:x\n"
	script(&h).Interpreter = "bash"
	script(&h).Timings = []catalog.Timing{"pre"}
	h.TimeoutSec = 0
	want := []model.Code{
		"name_invalid", "param_decl_invalid",
		"param_text_in_fragment", "sysvalue_not_allowed", "placeholder_unclosed",
		"script_interpreter_unavailable",
		"timeout_invalid",
	}
	for range 3 {
		if got := codesOf(Hook(h)); !slices.Equal(got, want) {
			t.Fatalf("问题顺序是 %v，应为 %v", got, want)
		}
	}
}
