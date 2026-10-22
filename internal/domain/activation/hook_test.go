package activation

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/scan"
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
		ID:          "lvm",
		Version:     3,
		Name:        "LVM-Partition",
		Description: "按参数分区，并写入登录提示。\n说明可以换行，也可以用中文。",
		State:       model.VersionReview,
		Author:      "alice",
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

// onlyCode 断言恰好报出 n 条问题，每一条都是 want：反例只改了一处，就不该连带报出别的代码，
// 同一处也不该重复报（T10-6①）。
func onlyCode(t *testing.T, ps []model.Problem, want model.Code, n int) {
	t.Helper()
	if len(ps) != n || slices.ContainsFunc(ps, func(p model.Problem) bool { return p.Code != want }) {
		t.Fatalf("应恰好报 %d 条 %s，得到 %d 条：%v", n, want, len(ps), ps)
	}
}

// wantCodes 断言报出的代码依次就是 want。
func wantCodes(t *testing.T, ps []model.Problem, want ...model.Code) {
	t.Helper()
	if got := codesOf(ps); !slices.Equal(got, want) {
		t.Fatalf("报出 %v，应为 %v：%v", got, want, ps)
	}
}

// hookPasses 断言正例改过一处后仍然通过。
func hookPasses(t *testing.T, name string, mod func(h *model.Hook)) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		h := goodHook()
		mod(&h)
		if ps := Hook(h); len(ps) != 0 {
			t.Fatalf("应通过，得到：%v", ps)
		}
	})
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

// runEdits 对每个反例断言它恰好报出一条 want。
func runEdits(t *testing.T, want model.Code, edits []edit) {
	t.Helper()
	runEditsN(t, want, 1, edits)
}

// runEditsN 对每个反例断言它恰好报出 n 条 want：有的问题按系、按引用各报一条。
func runEditsN(t *testing.T, want model.Code, n int, edits []edit) {
	t.Helper()
	for _, e := range edits {
		t.Run(e.name, func(t *testing.T) {
			h := goodHook()
			e.mod(&h)
			onlyCode(t, Hook(h), want, n)
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

// 说明和每份实现的正文都要存得下（T13-10）：PostgreSQL 不收 NUL，encoding/json 会把非法的 UTF-8
// 换成 U+FFFD，存进去再取出来就不是原来的字节，脚本的摘要也对不上。
func Test说明或正文存不下(t *testing.T) {
	runEdits(t, "text_not_storable", []edit{
		{"说明含 NUL", func(h *model.Hook) { h.Description = "分区\x00" }},
		{"说明不是 UTF-8", func(h *model.Hook) { h.Description = "\xc0\xaf" }},
		{"片段含 NUL", fragLine("# \x00")},
		{"脚本存成了 UTF-16", func(h *model.Hook) { script(h).Body = "\xff\xfe#\x00!\x00/\x00b\x00i\x00n\x00/\x00s\x00h\x00" }},
	})
	// 种类不认识的实现，正文照样要存得下：两样毛病互不相干。
	h := goodHook()
	h.Impls = append(h.Impls, model.Impl{Family: "linux", Series: []catalog.Series{"rhel"}, Kind: "macro", Body: "\x00"})
	wantCodes(t, Hook(h), "impl_kind_unknown", "text_not_storable")
	hookPasses(t, "说明为空", func(h *model.Hook) { h.Description = "" })
	hookPasses(t, "NUL 以外的控制字符照存", func(h *model.Hook) { h.Description = "第一行\t制表符\r\n第二行\x1b" })
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

// 族或系不对时推不出格式，只跳过按格式、按系的检查；不依赖格式的照查，作者一次能看到更多要改的
// 地方（T10-6②）。
func Test族或系不对时照查不依赖格式的问题(t *testing.T) {
	h := goodHook()
	frag(&h).Family = "Linux"
	frag(&h).Body += "# {{param:nope}}\n"
	script(&h).Family = "windows"
	script(&h).Body += "echo $LOBER_PARAM_NOPE\n"
	wantCodes(t, Hook(h), "impl_series_invalid", "param_undeclared", "impl_series_invalid", "param_undeclared")
}

// 写错的声明已经报过，引用它的地方不再按类型报一遍，它的默认值也不再拿去取子值。
func Test引用不合法的声明时只报声明(t *testing.T) {
	runEdits(t, "param_decl_invalid", []edit{
		{"类型不认识", func(h *model.Hook) {
			h.Params = append(h.Params, model.ParamDecl{Name: "size", Label: "大小", Type: "float"})
			frag(h).Body += "part /data --size={{param:size}}\n"
		}},
		{"默认值通不过校验，片段还取了它的子值", func(h *model.Hook) {
			h.Params = append(h.Params, model.ParamDecl{Name: "gw", Label: "网关", Type: "cidr", Default: "gateway"})
			frag(h).Body += "network --gateway={{param:gw.addr}}\n"
		}},
	})
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

// 重叠只比本实现族的系（T9-6①）：不属于这个族的系已经算作族或系不对，另一份实现照样覆盖它是对的，
// 再报重叠，作者会照「从其余的实现里去掉它」去改那份本来正确的实现。
func Test重叠检查不比不属于本族的系(t *testing.T) {
	esxi := model.Impl{Family: "esxi", Series: []catalog.Series{"esxi"}, Kind: model.ImplFragment, Body: "# {{param:disk}}\n"}
	cases := []struct {
		name string
		mod  func(h *model.Hook)
		path string // 报 impl_series_invalid 的那份实现
	}{
		{"误勾的实现在前", func(h *model.Hook) {
			frag(h).Series = append(frag(h).Series, "esxi")
			h.Impls = append(h.Impls, esxi)
		}, "impl/1"},
		{"误勾的实现在后", func(h *model.Hook) {
			frag(h).Series = append(frag(h).Series, "esxi")
			h.Impls = append([]model.Impl{esxi}, h.Impls...)
		}, "impl/2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := goodHook()
			c.mod(&h)
			ps := Hook(h)
			wantCodes(t, ps, "impl_series_invalid")
			if ps[0].Subject.Path != c.path {
				t.Errorf("Path = %q，应为 %q", ps[0].Subject.Path, c.path)
			}
		})
	}
}

// 片段按一种格式的语法写，覆盖的系必须同格式（C8）。
func Test片段覆盖的系不同格式(t *testing.T) {
	runEdits(t, "fragment_mixed_formats", []edit{
		{"kickstart 与 autoyast", func(h *model.Hook) { frag(h).Series = append(frag(h).Series, "sles15") }},
		{"autoyast 与 agama", func(h *model.Hook) { frag(h).Series = []catalog.Series{"sles15", "sles16"} }},
		// 推不出格式就不按格式查系统值（T10-6③）：smb 在 kickstart 与 autoyast 里都不可用，照查就会连带报出
		// sysvalue_not_allowed。
		{"混格式时不查系统值", func(h *model.Hook) {
			frag(h).Series = append(frag(h).Series, "sles15")
			frag(h).Body += "# {{sys:smb}}\n"
		}},
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
	runEdits(t, "param_undeclared", []edit{{"没声明", fragLine("rootpw --iscrypted {{param:rootpw}}")}})
	// 片段里引用了三处 disk，每处各报一条。
	runEditsN(t, "param_undeclared", 3, []edit{
		{"声明改了名，片段里的旧名都落空", func(h *model.Hook) { h.Params[0].Name = "sysdisk" }},
	})
	runEdits(t, "param_text_in_fragment", []edit{{"text 进片段", fragLine("# {{param:motd}}")}})
}

func Test片段的列表规则与子值(t *testing.T) {
	runEdits(t, "list_repeat_conflict", []edit{
		{"一行两个逐行重复的列表", fragLine("# {{param:pkgs}} {{param:ntp}}")},
		// 每行每个参数只报一次（T9-6④）。
		{"后一个列表在这一行里写了两次", fragLine("# {{param:pkgs}} {{param:ntp}} {{param:ntp}}")},
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

// |join 的列表内联成一行、不逐行重复，与一个逐行重复的列表同行不冲突（T10-6④）。
func Test用join的列表与逐行重复的列表同行不冲突(t *testing.T) {
	hookPasses(t, "join 在前", fragLine("# {{param:ntp|join}} {{param:pkgs}}"))
	hookPasses(t, "join 在后", fragLine("# {{param:pkgs}} {{param:ntp|join}} {{param:ntp|join}}"))
}

// 片段的正文可以为空（T9-4②）：给不需要做任何事的系当空操作实现，模板勾了这个系，才不会报
// impl_missing_for_series。脚本不行，见 Test脚本不完整。
func Test空片段能通过(t *testing.T) {
	for _, body := range []string{"", "\n", " \t\r\n"} {
		hookPasses(t, strconv.Quote(body), func(h *model.Hook) { frag(h).Body = body })
	}
}

// 脚本要有正文，并声明解释器和至少一个时机（C10）。
func Test脚本不完整(t *testing.T) {
	runEdits(t, "script_incomplete", []edit{
		{"正文为空", func(h *model.Hook) { script(h).Body = "" }},
		{"正文只有空白", func(h *model.Hook) { script(h).Body = " \n\t\r\n" }},
		{"没声明解释器", func(h *model.Hook) { script(h).Interpreter = "" }},
		{"没声明时机", func(h *model.Hook) { script(h).Timings = nil }},
	})
}

// 实现只有片段与脚本两种（C8）。种类不认识的只会来自直接调接口或库里的坏数据，按哪种查正文都
// 无从说起，只报这一条，不再借 script_incomplete（T7-2）。
func Test实现的种类不认识(t *testing.T) {
	runEdits(t, "impl_kind_unknown", []edit{
		{"macro", func(h *model.Hook) { script(h).Kind = "macro" }},
		{"空串", func(h *model.Hook) { script(h).Kind = "" }},
		{"大小写不同", func(h *model.Hook) { frag(h).Kind = "Fragment" }},
	})
	h := goodHook()
	script(&h).Kind = "macro"
	if ps := Hook(h); len(ps) != 1 || !strings.Contains(ps[0].Message, "「macro」") {
		t.Errorf("说明里应写出不认识的种类，得到：%v", ps)
	}
}

// 脚本可以跨格式，但声明的每个时机在它覆盖的每个系里都要支持（C8）。
func Test脚本的时机该格式不支持(t *testing.T) {
	// 脚本覆盖 debian 与 ubuntu，各报一条。
	runEditsN(t, "script_timing_unsupported", 2, []edit{
		{"时机不认识", func(h *model.Hook) { script(h).Timings = []catalog.Timing{"post"} }},
	})
	// unattend 没有两个 post 时机。
	h := model.Hook{ID: "win", Version: 1, Name: "Windows-Post", TimeoutSec: 60, Impls: []model.Impl{{
		Family: "windows", Series: []catalog.Series{"windows-server"}, Kind: model.ImplScript,
		Body: "echo ok\r\n", Interpreter: "cmd", Timings: []catalog.Timing{"post-target"},
	}}}
	onlyCode(t, Hook(h), "script_timing_unsupported", 1)
}

// 同一个时机写了两次只查一次（T9-4①）：不像重复勾的系那样拦下，第 5 层可以在接口边界去重。
func Test重复声明的时机只查一次(t *testing.T) {
	hookPasses(t, "合法的时机写了两次", func(h *model.Hook) {
		script(h).Timings = []catalog.Timing{"post-target", "post-target"}
	})
	runEditsN(t, "script_timing_unsupported", 2, []edit{
		{"不认识的时机写了两次，每个系仍只报一条", func(h *model.Hook) { script(h).Timings = []catalog.Timing{"post", "post"} }},
	})
}

// 解释器在声明的每个时机、覆盖的每个系里都要可用（C10）：Debian 装前只有 busybox sh。
func Test脚本的解释器在该时机不可用(t *testing.T) {
	runEdits(t, "script_interpreter_unavailable", []edit{
		{"bash 在 pre 覆盖 debian", func(h *model.Hook) {
			script(h).Interpreter = "bash"
			script(h).Timings = []catalog.Timing{"pre"}
		}},
	})
	// debian 与 ubuntu 都没有，各报一条。
	runEditsN(t, "script_interpreter_unavailable", 2, []edit{
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

// 脚本按原样下发，不展开 lober 的占位符（L-1）：写在里面的永远不会被替换，作者以为拿到了参数，
// 到装机时才出错。四种都报，写错的同种类也报，各一条，参数是占位符原文。
func Test脚本里写了lober的占位符(t *testing.T) {
	cases := []struct {
		name, line string
		col        int    // {{ 所在的列
		raw        string // 说明里的占位符原文
	}{
		{"参数", "mkfs.xfs {{param:disk}}", 10, "{{param:disk}}"},
		{"参数的子值", "ip addr add {{param:addr.addr}}", 13, "{{param:addr.addr}}"},
		{"系统值", "curl {{sys:gateway}}/done", 6, "{{sys:gateway}}"},
		{"转义在脚本里也不展开", "echo '{{sys:lbrace}}'", 7, "{{sys:lbrace}}"},
		{"插槽", "{{slot:分区}}", 1, "{{slot:分区}}"},
		{"系统部件", "  {{lober:syslog}}", 3, "{{lober:syslog}}"},
		{"首尾带空格", "echo {{ param:disk }}", 6, "{{ param:disk }}"},
		{"写法不对", "echo {{param:Disk}}", 6, "{{param:Disk}}"},
		{"时机不认识", "# {{slot:收尾@post}}", 3, "{{slot:收尾@post}}"},
		{"部件不认识", "# {{lober:motd}}", 3, "{{lober:motd}}"},
		{"系统值不认识", "echo {{sys:mirror}}", 6, "{{sys:mirror}}"},
		{"没闭合", "echo {{param:disk ", 6, "{{param:disk"},
		{"被下一个 {{ 截断", "echo {{param:disk {{ x }}", 6, "{{param:disk"},
		{"中文之后的列按 rune 计", "# 中文 {{param:disk}}", 6, "{{param:disk}}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := goodHook()
			script(&h).Body += c.line + "\n"
			ps := Hook(h)
			onlyCode(t, ps, "script_placeholder_not_expanded", 1)
			if want := (model.Subject{Kind: "impl", ID: "lvm@3", Path: "impl/2", Line: 4, Col: c.col}); ps[0].Subject != want {
				t.Errorf("Subject = %+v，应为 %+v", ps[0].Subject, want)
			}
			if !strings.Contains(ps[0].Message, "「"+c.raw+"」") {
				t.Errorf("说明里应有占位符原文 %q：%s", c.raw, ps[0].Message)
			}
		})
	}
	// 种类不是那四种的不归 lober 管，扫描报的问题也不带出：Jinja、Go 模板、网址在脚本里照写。
	for _, line := range []string{
		"curl {{http://repo/}}",
		"echo {{foo:x}} {{Param:disk}}",
		"echo {{ ds.meta_data.local_hostname }}",
		"echo {{foo:x {{ y }}",
		"printf '{{%s}}' {{ .Name }}",
	} {
		hookPasses(t, line, func(h *model.Hook) { script(h).Body += line + "\n" })
	}
}

// 一份脚本里有几处报几处，与环境变量的问题一起按位置排。
func Test脚本里的占位符与环境变量按位置排(t *testing.T) {
	h := goodHook()
	script(&h).Body = "#!/bin/sh\necho $LOBER_PARAM_NOPE {{param:disk}}{{sys:sn}}\n{{slot:x\n"
	ps := Hook(h)
	wantCodes(t, ps, "param_undeclared",
		"script_placeholder_not_expanded", "script_placeholder_not_expanded", "script_placeholder_not_expanded")
	for i, want := range [][2]int{{2, 7}, {2, 24}, {2, 38}, {3, 1}} {
		if got := [2]int{ps[i].Subject.Line, ps[i].Subject.Col}; got != want {
			t.Errorf("第 %d 条在 %v，应在 %v", i+1, got, want)
		}
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

// timeout_invalid 的说明里写死了 1–21600 秒（6 小时），要与这里的上下限一致：改了范围，说明也得跟着改（T5-5）。
func Test超时的说明与范围一致(t *testing.T) {
	p := model.NewProblem("timeout_invalid", model.Subject{}, 0)
	lo, hi := strconv.Itoa(timeoutMinSec), strconv.Itoa(timeoutMaxSec)
	if !strings.Contains(p.Message, lo+"–"+hi+" 秒") {
		t.Errorf("说明 %q 里应有 %s–%s 秒", p.Message, lo, hi)
	}
	if hours := strconv.Itoa(timeoutMaxSec / 3600); !strings.Contains(p.Next, lo+" 到 "+hi+" 秒（"+hours+" 小时）") {
		t.Errorf("下一步 %q 里应有 %s 到 %s 秒（%s 小时）", p.Next, lo, hi, hours)
	}
}

// hook 参数的默认值要经得起它自己的片段（L-4）：IPv6 的 cidr 取不出 netmask，模板沿用这个默认值时，
// 每台主机建任务都会报错。问题落在参数声明上：引用本身没错，换成 IPv4 的默认值就取得出。
func Test参数的默认值取不出片段要的子值(t *testing.T) {
	gw := func(def string, list bool, line string) func(h *model.Hook) {
		return func(h *model.Hook) {
			h.Params = append(h.Params, model.ParamDecl{Name: "gw", Label: "网关", Type: "cidr", List: list, Default: def})
			frag(h).Body += line + "\n"
		}
	}
	netmask := "network --gateway={{param:gw.addr}} --netmask={{param:gw.netmask}}"
	runEdits(t, "param_accessor_invalid", []edit{
		{"IPv6 取 netmask", gw("2001:db8::1/64", false, netmask)},
		{"列表里有一项是 IPv6", gw("192.0.2.1/24\n2001:db8::1/64", true, "# {{param:gw.netmask}}")},
	})
	h := goodHook()
	gw("2001:db8::1/64", false, netmask)(&h)
	if ps := Hook(h); len(ps) != 1 || ps[0].Subject != (model.Subject{Kind: "hook", ID: "lvm@3", Path: "param/7"}) {
		t.Errorf("应指向第 7 个参数声明，得到：%v", ps)
	}
	hookPasses(t, "IPv4 取 netmask", gw("192.0.2.1/24", false, netmask))
	hookPasses(t, "IPv6 取 addr 与 prefix", gw("2001:db8::1/64", false, "network --ipv6={{param:gw.addr}}/{{param:gw.prefix}}"))
	hookPasses(t, "列表各项都是 IPv4", gw("192.0.2.1/24\n198.51.100.1/24", true, "# {{param:gw.netmask}}"))
	hookPasses(t, "没有默认值的不查", gw("", false, netmask))
	// 脚本经环境变量拿整个值，不取子值。
	hookPasses(t, "只有脚本用到它", func(h *model.Hook) {
		h.Params = append(h.Params, model.ParamDecl{Name: "gw", Label: "网关", Type: "cidr", Default: "2001:db8::1/64"})
		script(h).Body += "ip -6 route add default via \"$LOBER_PARAM_GW\"\n"
	})
	// 脚本里写的 {{param:gw.netmask}} 不会展开，只报这一处，不当作对默认值取子值。
	h = goodHook()
	h.Params = append(h.Params, model.ParamDecl{Name: "gw", Label: "网关", Type: "cidr", Default: "2001:db8::1/64"})
	script(&h).Body += "echo {{param:gw.netmask}}\n"
	wantCodes(t, Hook(h), "script_placeholder_not_expanded")
}

// Subject 按位置指向出问题的地方（T9-1）：hook 整体的问题指向 hook；参数声明是 param/<序号>，实现是
// impl/<序号>，都从 1 起，带正文位置的另带行列。重名的声明、勾了同一组系的两份实现也分得开，前端靠它
// 把错误标到具体的行。
func TestSubject指向出问题的位置(t *testing.T) {
	hook := model.Subject{Kind: "hook", ID: "lvm@3"}
	param := func(n int) model.Subject {
		return model.Subject{Kind: "hook", ID: "lvm@3", Path: "param/" + strconv.Itoa(n)}
	}
	impl := func(n, line, col int) model.Subject {
		return model.Subject{Kind: "impl", ID: "lvm@3", Path: "impl/" + strconv.Itoa(n), Line: line, Col: col}
	}
	cases := []struct {
		name string
		mod  func(h *model.Hook)
		code model.Code
		want model.Subject
	}{
		{"名称", func(h *model.Hook) { h.Name = "" }, "name_invalid", hook},
		{"说明", func(h *model.Hook) { h.Description = "\x00" }, "text_not_storable", hook},
		{"参数声明", func(h *model.Hook) { h.Params[1].Min = new(int64(99999)) }, "param_decl_invalid", param(2)},
		{"重名的声明指向后一个", func(h *model.Hook) {
			h.Params = append(h.Params, model.ParamDecl{Name: "disk", Label: "数据盘", Type: "diskpath"})
		}, "param_decl_invalid", param(7)},
		{"参数的默认值", func(h *model.Hook) {
			h.Params = append(h.Params, model.ParamDecl{Name: "gw", Label: "网关", Type: "cidr", Default: "2001:db8::1/64"})
			frag(h).Body += "# {{param:gw.netmask}}\n"
		}, "param_accessor_invalid", param(7)},
		{"没有实现", func(h *model.Hook) { h.Impls = nil }, "hook_no_impl", hook},
		{"超时", func(h *model.Hook) { h.TimeoutSec = 0 }, "timeout_invalid", hook},
		{"系不对", func(h *model.Hook) { frag(h).Series = nil }, "impl_series_invalid", impl(1, 0, 0)},
		{"系重叠指向后一份实现", func(h *model.Hook) { script(h).Series = append(script(h).Series, "rocky") },
			"impl_series_overlap", impl(2, 0, 0)},
		{"勾了同一组系的两份实现分得开", func(h *model.Hook) {
			h.Impls = append(h.Impls, model.Impl{Family: "linux", Series: []catalog.Series{"rocky", "almalinux"},
				Kind: model.ImplFragment, Body: "# {{param:disk}}\n"})
		}, "impl_series_overlap", impl(3, 0, 0)},
		{"种类不认识", func(h *model.Hook) { script(h).Kind = "macro" }, "impl_kind_unknown", impl(2, 0, 0)},
		{"正文存不下不带行列", fragLine("\x00"), "text_not_storable", impl(1, 0, 0)},
		{"片段的正文位置按 rune 计", func(h *model.Hook) { frag(h).Body = "# 中文{{param:motd}}\n" },
			"param_text_in_fragment", impl(1, 1, 5)},
		{"扫描问题改指向实现、保留行列", func(h *model.Hook) { frag(h).Body = "a\n  {{slot:x\n" },
			"placeholder_unclosed", impl(1, 2, 3)},
		{"脚本里的变量位置按 rune 计", func(h *model.Hook) { script(h).Body = "#!/bin/sh\n# 说明 $LOBER_PARAM_X\n" },
			"param_undeclared", impl(2, 2, 7)},
		{"脚本里的占位符", func(h *model.Hook) { script(h).Body = "#!/bin/sh\n# 中文 {{sys:sn}}\n" },
			"script_placeholder_not_expanded", impl(2, 2, 6)},
		{"脚本的时机", func(h *model.Hook) { script(h).Timings = []catalog.Timing{"post"} },
			"script_timing_unsupported", impl(2, 0, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := goodHook()
			c.mod(&h)
			ps := Hook(h)
			if len(ps) == 0 {
				t.Fatal("应报出问题，却通过了")
			}
			if ps[0].Code != c.code || ps[0].Subject != c.want {
				t.Errorf("第一条是 %s %+v，应为 %s %+v", ps[0].Code, ps[0].Subject, c.code, c.want)
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

// 同样的输入永远得到同样顺序的输出：先 hook 整体（名称、说明，参数声明按位置），再按实现的顺序，
// 超时最后；一份实现里先是不涉及位置的，正文的问题按位置排。
func Test问题的顺序固定(t *testing.T) {
	h := goodHook()
	h.Name = ""
	h.Description = "\x00"
	h.Params = append(h.Params,
		model.ParamDecl{Name: "disk", Label: "重名", Type: "word"},
		model.ParamDecl{Name: "gw", Label: "网关", Type: "cidr", Default: "2001:db8::1/64"},
	)
	frag(&h).Body = "# {{param:motd}} {{sys:smb}} {{param:gw.netmask}}\n{{slot:x\n"
	script(&h).Interpreter = "bash"
	script(&h).Timings = []catalog.Timing{"pre"}
	script(&h).Body += "echo {{param:disk}}\n"
	h.Impls = append(h.Impls, model.Impl{Family: "linux", Series: []catalog.Series{"rhel"}, Kind: "macro"})
	h.TimeoutSec = 0
	want := []model.Code{
		"name_invalid", "text_not_storable", "param_decl_invalid", "param_accessor_invalid",
		"param_text_in_fragment", "sysvalue_not_allowed", "placeholder_unclosed",
		"script_interpreter_unavailable", "script_placeholder_not_expanded",
		"impl_kind_unknown",
		"timeout_invalid",
	}
	for range 3 {
		if got := codesOf(Hook(h)); !slices.Equal(got, want) {
			t.Fatalf("问题顺序是 %v，应为 %v", got, want)
		}
	}
}
