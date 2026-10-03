package activation

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// tplBody 是正例的 kickstart 正文：一个片段插槽、一个 post-target 执行插槽，三个必需部件
// 各一次，另引用一个系统值和一个逐行重复的列表参数。
const tplBody = "text\n" +
	"url --url={{sys:repo}}\n" +
	"{{lober:syslog}}\n" +
	"{{slot:分区}}\n" +
	"%packages\n" +
	"@^minimal-environment\n" +
	"{{param:pkgs}}\n" +
	"%end\n" +
	"%post\n" +
	"{{slot:装后配置@post-target}}\n" +
	"{{lober:firstboot}}\n" +
	"{{lober:cleanup}}\n" +
	"%end\n"

// tplID 是正例在 Subject 里的 ID。
const tplID = "rhel-base@2"

// hookSet 是调用方凑齐的 hook 版本。
type hookSet = map[model.HookKey]model.Hook

// partHook 是正例分区插槽里的片段 hook：一份 kickstart 片段覆盖模板勾的两个系。
func partHook() model.Hook {
	return model.Hook{
		ID:       "lvm",
		Version:  3,
		Name:     "LVM-Partition",
		State:    model.VersionApproved,
		Author:   "alice",
		Approver: "bob",
		Params:   []model.ParamDecl{{Name: "disk", Label: "系统盘", Type: "diskpath", Default: "/dev/sda"}},
		Impls: []model.Impl{{
			Family: "linux",
			Series: []catalog.Series{"rocky", "almalinux"},
			Kind:   model.ImplFragment,
			Body:   "ignoredisk --only-use={{param:disk}}\nautopart --type=lvm\n",
		}},
	}
}

// motdHook 是正例装后插槽里的脚本 hook：post-target 时机用 sh 跑，参数是 text，只进脚本。
func motdHook() model.Hook {
	return model.Hook{
		ID:         "motd",
		Version:    1,
		Name:       "Login-Banner",
		State:      model.VersionApproved,
		Author:     "alice",
		Approver:   "bob",
		Params:     []model.ParamDecl{{Name: "msg", Label: "提示语", Type: "text", Required: true}},
		TimeoutSec: 60,
		Impls: []model.Impl{{
			Family:      "linux",
			Series:      []catalog.Series{"rocky", "almalinux"},
			Kind:        model.ImplScript,
			Body:        "#!/bin/sh\nprintf '%s\\n' \"$LOBER_PARAM_MSG\" >/etc/motd\n",
			Interpreter: "sh",
			Timings:     []catalog.Timing{"post-target"},
		}},
	}
}

func goodHooks() hookSet {
	return hookSet{
		{ID: "lvm", Version: 3}:  partHook(),
		{ID: "motd", Version: 1}: motdHook(),
	}
}

// goodTemplate 是能通过激活判定的完整模板，每个反例都在它上面只改一处：勾 rocky 与 almalinux，
// 分区插槽放一个片段 hook，装后插槽放同一个脚本 hook 两次、参数不同（C17），一个模板参数。
func goodTemplate() model.Template {
	return model.Template{
		ID:      "rhel-base",
		Version: 2,
		Name:    "RHEL-Baseline",
		State:   model.VersionReview,
		Author:  "alice",
		Family:  "linux",
		Series:  []catalog.Series{"rocky", "almalinux"},
		Body:    tplBody,
		Params:  []model.ParamDecl{{Name: "pkgs", Label: "软件包", Type: "word", List: true}},
		Bindings: map[string]model.Binding{
			"pkgs": {Source: model.BindingFixed, Value: "vim\ntmux", Editable: true},
		},
		Assembly: map[string][]model.Instance{
			"分区": {
				{HookID: "lvm", HookVersion: 3, Bindings: map[string]model.Binding{
					"disk": {Source: model.BindingAttr, Attr: "sysdisk"},
				}},
			},
			"装后配置": {
				{HookID: "motd", HookVersion: 1, Bindings: map[string]model.Binding{
					"msg": {Source: model.BindingFixed, Value: "欢迎使用"},
				}},
				{HookID: "motd", HookVersion: 1, TimeoutSec: new(120), Bindings: map[string]model.Binding{
					"msg": {Source: model.BindingFixed, Value: "维护中，请勿改动", Editable: true},
				}},
			},
		},
	}
}

// tplMod 改正例的一处：可以改模板，也可以改调用方凑齐的 hook。
type tplMod = func(tpl *model.Template, hooks hookSet)

// tplEdit 是在正例上只改一处的反例。
type tplEdit struct {
	name string
	mod  tplMod
}

// runTplEdits 对每个反例断言它恰好报出一条 want。
func runTplEdits(t *testing.T, want model.Code, edits []tplEdit) {
	t.Helper()
	runTplEditsN(t, want, 1, edits)
}

// runTplEditsN 对每个反例断言它恰好报出 n 条 want：有的问题按系、按实例各报一条。
func runTplEditsN(t *testing.T, want model.Code, n int, edits []tplEdit) {
	t.Helper()
	for _, e := range edits {
		t.Run(e.name, func(t *testing.T) {
			tpl, hooks := goodTemplate(), goodHooks()
			e.mod(&tpl, hooks)
			onlyCode(t, Template(tpl, hooks), want, n)
		})
	}
}

// all 依次做几处修改：有的反例要同时声明参数、写引用、定取值来源。
func all(mods ...tplMod) tplMod {
	return func(tpl *model.Template, hooks hookSet) {
		for _, m := range mods {
			m(tpl, hooks)
		}
	}
}

// declare 只声明一个模板参数，正文里不引用它。
func declare(d model.ParamDecl) tplMod {
	return func(tpl *model.Template, _ hookSet) { tpl.Params = append(tpl.Params, d) }
}

// replaceWith 整个换成另一个模板：有的格式规则在正例的 kickstart 上反不出来。
func replaceWith(other model.Template) tplMod {
	return func(tpl *model.Template, _ hookSet) { *tpl = other }
}

// passes 断言正例改过一处后仍然通过。
func passes(t *testing.T, name string, mod tplMod) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		tpl, hooks := goodTemplate(), goodHooks()
		mod(&tpl, hooks)
		if ps := Template(tpl, hooks); len(ps) != 0 {
			t.Fatalf("应通过，得到：%v", ps)
		}
	})
}

// tplLine 在正文末尾追加一行。
func tplLine(line string) tplMod {
	return func(tpl *model.Template, _ hookSet) { tpl.Body += line + "\n" }
}

// tplReplace 把正文里的 old 换成 repl。old 不在正文里时 panic：反例没改到正文，测的就成了正例。
func tplReplace(old, repl string) tplMod {
	return func(tpl *model.Template, _ hookSet) {
		if !strings.Contains(tpl.Body, old) {
			panic("正文里没有「" + old + "」")
		}
		tpl.Body = strings.Replace(tpl.Body, old, repl, 1)
	}
}

// withParam 声明一个模板参数，再在正文末尾追加一行引用它：有的规则要用正例里没有的参数类型。
func withParam(d model.ParamDecl, line string) tplMod {
	return func(tpl *model.Template, _ hookSet) {
		tpl.Params = append(tpl.Params, d)
		tpl.Body += line + "\n"
	}
}

// setBinding 换掉模板参数 name 的取值来源。
func setBinding(name string, b model.Binding) tplMod {
	return func(tpl *model.Template, _ hookSet) { tpl.Bindings[name] = b }
}

// setInstBinding 换掉插槽 slot 里第 i 个实例（从 0 起）对参数 name 的取值来源。
func setInstBinding(slot string, i int, name string, b model.Binding) tplMod {
	return func(tpl *model.Template, _ hookSet) { tpl.Assembly[slot][i].Bindings[name] = b }
}

// setInstTimeout 让插槽 slot 里第 i 个实例（从 0 起）覆盖 hook 给的超时。
func setInstTimeout(slot string, i, sec int) tplMod {
	return func(tpl *model.Template, _ hookSet) { tpl.Assembly[slot][i].TimeoutSec = &sec }
}

// editHook 改调用方凑齐的一个 hook 版本：map 的值不可寻址，取出来改完再放回去。
func editHook(id string, version int, mod func(h *model.Hook)) tplMod {
	return func(_ *model.Template, hooks hookSet) {
		k := model.HookKey{ID: id, Version: version}
		h := hooks[k]
		mod(&h)
		hooks[k] = h
	}
}

// splitPart 把 partHook 覆盖 rocky 与 almalinux 的那份片段拆成每个系一份，正文照旧（都引用 disk），再往
// rocky 的那份末尾追加一行 rocky、almalinux 的那份追加一行 alma，空串不追加。模板勾的两个系于是各选中
// 一份不同的片段，实例对参数的引用要把两份并起来才全。
func splitPart(h *model.Hook, rocky, alma string) {
	other := h.Impls[0]
	other.Series = []catalog.Series{"almalinux"}
	h.Impls[0].Series = []catalog.Series{"rocky"}
	if rocky != "" {
		h.Impls[0].Body += rocky + "\n"
	}
	if alma != "" {
		other.Body += alma + "\n"
	}
	h.Impls = append(h.Impls, other)
}

// bareTemplate 是只写了必需部件、另加一行 extra 的模板。有的格式规则在正例的 kickstart 上
// 反不出来（它四个时机都支持，三个部件也都必需），就换一种格式从头写。
func bareTemplate(s catalog.Series, extra string) model.Template {
	var body strings.Builder
	for _, p := range s.Format().RequiredParts() {
		body.WriteString("{{lober:" + string(p) + "}}\n")
	}
	body.WriteString(extra)
	return model.Template{
		ID:      "bare",
		Version: 1,
		Name:    "Minimal",
		Family:  s.Family(),
		Series:  []catalog.Series{s},
		Body:    body.String(),
	}
}

func Test模板正例通过激活判定(t *testing.T) {
	for _, h := range []model.Hook{partHook(), motdHook()} {
		if ps := Hook(h); len(ps) != 0 {
			t.Fatalf("正例引用的 hook %s 自己就过不了激活判定：%v", h.ID, ps)
		}
	}
	tpl, hooks := goodTemplate(), goodHooks()
	if ps := Template(tpl, hooks); len(ps) != 0 {
		t.Fatalf("正例应通过，得到：%v", ps)
	}
	if !reflect.DeepEqual(tpl, goodTemplate()) || !reflect.DeepEqual(hooks, goodHooks()) {
		t.Error("判定改动了入参")
	}
}

// 每种格式只写必需部件、装配表空着，也能激活：没有实例的插槽和空的装配表都是允许的。
func Test各格式只写必需部件的模板能通过(t *testing.T) {
	for _, f := range catalog.Families() {
		for _, s := range catalog.SeriesOf(f) {
			if ps := Template(bareTemplate(s, ""), nil); len(ps) != 0 {
				t.Errorf("%s：应通过，得到：%v", s, ps)
			}
		}
	}
}

func Test模板名称不合法(t *testing.T) {
	runTplEdits(t, "name_invalid", []tplEdit{
		{"空名称", func(tpl *model.Template, _ hookSet) { tpl.Name = "" }},
		{"只有空白", func(tpl *model.Template, _ hookSet) { tpl.Name = " \t " }},
		{"含控制字符", func(tpl *model.Template, _ hookSet) { tpl.Name = "RHEL\x07基线" }},
		{"超过 64 个字", func(tpl *model.Template, _ hookSet) { tpl.Name = strings.Repeat("基", 65) }},
	})
}

// 模板正文要存得下（T13-10），理由同 hook 的正文：快照按 JSON 存，非法的字节存进去再取出来就变了。
func Test模板正文存不下(t *testing.T) {
	runTplEdits(t, "text_not_storable", []tplEdit{
		{"含 NUL", tplLine("# \x00")},
		{"不是 UTF-8", tplLine("# \xe4\xb8")},
	})
}

// 模板属于一个族，勾该族的系，没勾系的不能激活（C13）。系不对时推不出格式，按格式、按系的
// 检查都跳过：正例的 hook 不覆盖 centos 和 esxi，照查就会连带报出 impl_missing_for_series。
func Test模板的族或系不对(t *testing.T) {
	runTplEdits(t, "template_series_invalid", []tplEdit{
		{"没选族", func(tpl *model.Template, _ hookSet) { tpl.Family = "" }},
		{"族不认识", func(tpl *model.Template, _ hookSet) { tpl.Family = "Linux" }},
		{"族与系不符", func(tpl *model.Template, _ hookSet) { tpl.Family = "windows" }},
		{"没勾系", func(tpl *model.Template, _ hookSet) { tpl.Series = nil }},
		{"系是空切片", func(tpl *model.Template, _ hookSet) { tpl.Series = []catalog.Series{} }},
		{"系不认识", func(tpl *model.Template, _ hookSet) { tpl.Series = append(tpl.Series, "centos") }},
		{"系大小写不同", func(tpl *model.Template, _ hookSet) { tpl.Series[1] = "AlmaLinux" }},
		{"系属于别的族", func(tpl *model.Template, _ hookSet) { tpl.Series = append(tpl.Series, "esxi") }},
		{"同一个系勾了两次", func(tpl *model.Template, _ hookSet) { tpl.Series = append(tpl.Series, "rocky") }},
		{"推不出格式时不查系统值", func(tpl *model.Template, _ hookSet) {
			tpl.Series = nil
			tpl.Body += "# {{sys:smb}}\n"
		}},
	})
}

// 族或系不对时只跳过按格式、按系的检查，不依赖格式的照查（T10-6②）。
func Test模板的族或系不对时照查不依赖格式的问题(t *testing.T) {
	tpl, hooks := goodTemplate(), goodHooks()
	tpl.Series = nil
	tpl.Body += "# {{param:nope}}\n"
	wantCodes(t, Template(tpl, hooks), "template_series_invalid", "param_undeclared")
}

// 所勾的系要同格式，正文按这种格式写（C13）；推不出格式，按格式、按系的检查同样跳过。
func Test模板勾的系不同格式(t *testing.T) {
	runTplEdits(t, "template_mixed_formats", []tplEdit{
		{"kickstart 与 autoyast", func(tpl *model.Template, _ hookSet) { tpl.Series = append(tpl.Series, "sles15") }},
		{"preseed 与 autoinstall", func(tpl *model.Template, _ hookSet) {
			tpl.Series = []catalog.Series{"debian", "ubuntu"}
		}},
	})
}

// 正文扫描的问题原样带出，指向改成模板。
func Test模板正文的扫描错误原样带出(t *testing.T) {
	runTplEdits(t, "placeholder_unclosed", []tplEdit{{"未闭合", tplLine("# {{param:pkgs")}})
	runTplEdits(t, "placeholder_unknown_kind", []tplEdit{{"种类不认识", tplLine("url --url={{http://repo/}}")}})
	runTplEdits(t, "placeholder_malformed", []tplEdit{{"写法不对", tplLine("{{param:Pkgs}}")}})
	runTplEdits(t, "timing_unknown", []tplEdit{{"时机不认识", tplLine("{{slot:收尾@post}}")}})
	runTplEdits(t, "part_unknown", []tplEdit{{"部件不认识", tplLine("{{lober:motd}}")}})
	runTplEdits(t, "sysvalue_unknown", []tplEdit{{"系统值不认识", tplLine("# {{sys:mirror}}")}})
}

// 写坏的插槽占位符不进插槽列表；这时再报 assembly_slot_unknown，作者照「从装配表里删掉它」去做，
// 会丢掉已经配好的实例（T9-6②）。正文改好之后，真不存在的插槽照样会报。
func Test正文扫描有问题时不报装配表里的插槽(t *testing.T) {
	runTplEdits(t, "placeholder_unclosed", []tplEdit{{"片段插槽没闭合", tplReplace("{{slot:分区}}\n", "{{slot:分区\n")}})
	runTplEdits(t, "timing_unknown", []tplEdit{{"执行插槽的时机写错", tplReplace("@post-target}}", "@post}}")}})
	orphan := func(tpl *model.Template, _ hookSet) {
		tpl.Assembly["软件包"] = []model.Instance{{HookID: "lvm", HookVersion: 3}}
	}
	runTplEdits(t, "sysvalue_unknown", []tplEdit{{"别处的扫描错误也算", all(tplLine("# {{sys:mirror}}"), orphan)}})
}

// 插槽名在一个模板里唯一，不论类型（C14）：装配表按名字挂实例，重名就不知道挂在哪一个。
func Test模板插槽名重复(t *testing.T) {
	runTplEdits(t, "slot_duplicate", []tplEdit{
		{"片段插槽写了两次", tplLine("{{slot:分区}}")},
		{"执行插槽写了两次", tplLine("{{slot:装后配置@post-target}}")},
		{"执行插槽与片段插槽同名", tplLine("{{slot:分区@firstboot}}")},
	})
}

// 执行插槽的时机要在该格式的时机表里（C14）。kickstart 四个时机都支持，换别的格式反。
func Test执行插槽的时机该格式不支持(t *testing.T) {
	cases := []struct {
		name   string
		series catalog.Series
		line   string
	}{
		{"esxi-ks 没有 post-target", "esxi", "{{slot:装后@post-target}}\n"},
		{"unattend 没有 post-installer", "windows-server", "{{slot:装后@post-installer}}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			onlyCode(t, Template(bareTemplate(c.series, c.line), nil), "slot_timing_unsupported", 1)
		})
	}
}

// 执行插槽的时机该格式不支持时，插槽已报 slot_timing_unsupported；再拿它去比实例的时机，下一步会让
// 作者给 hook 加一个该格式根本没有的时机（T9-6③）。种类与时机无关，照比。
func Test执行插槽的时机该格式不支持时不比实例的时机(t *testing.T) {
	win := func(id string, kind model.ImplKind) model.Hook {
		impl := model.Impl{Family: "windows", Series: []catalog.Series{"windows-server"}, Kind: kind, Body: "echo ok\r\n"}
		if kind == model.ImplScript {
			impl.Interpreter, impl.Timings = "cmd", []catalog.Timing{"firstboot"}
		}
		return model.Hook{ID: id, Version: 1, Name: "Windows-" + id, State: model.VersionApproved,
			Author: "alice", Approver: "bob", TimeoutSec: 60, Impls: []model.Impl{impl}}
	}
	hooks := hookSet{{ID: "post", Version: 1}: win("post", model.ImplScript), {ID: "frag", Version: 1}: win("frag", model.ImplFragment)}
	tpl := bareTemplate("windows-server", "{{slot:装后@post-target}}\n")
	tpl.Assembly = map[string][]model.Instance{"装后": {{HookID: "post", HookVersion: 1}}}
	wantCodes(t, Template(tpl, hooks), "slot_timing_unsupported")
	tpl.Assembly["装后"] = append(tpl.Assembly["装后"], model.Instance{HookID: "frag", HookVersion: 1})
	wantCodes(t, Template(tpl, hooks), "slot_timing_unsupported", "impl_kind_mismatch")
}

// 片段插槽展开成多行、每行套用所在行的缩进，所以要独占一行；执行插槽只展开成一行，可以写在行内。
func Test片段插槽没有独占一行(t *testing.T) {
	runTplEdits(t, "fragment_slot_not_alone", []tplEdit{
		{"前面有非空白", tplReplace("{{slot:分区}}\n", "part {{slot:分区}}\n")},
		{"后面有非空白", tplReplace("{{slot:分区}}\n", "{{slot:分区}} # 分区\n")},
	})
	passes(t, "缩进与行尾空白不算", tplReplace("{{slot:分区}}\n", "\t  {{slot:分区}} \t\r\n"))
	passes(t, "执行插槽写在行内", tplReplace("{{slot:装后配置@post-target}}\n", "true; {{slot:装后配置@post-target}}\n"))
}

// 该格式的必需部件各恰好出现一次，别的部件不能出现（C15）。
func Test系统部件缺少重复或不属于该格式(t *testing.T) {
	runTplEdits(t, "part_missing", []tplEdit{
		{"缺 syslog", tplReplace("{{lober:syslog}}\n", "")},
		{"缺 cleanup", tplReplace("{{lober:cleanup}}\n", "")},
	})
	runTplEdits(t, "part_duplicate", []tplEdit{
		{"cleanup 写了两次", tplLine("{{lober:cleanup}}")},
		{"同一行里写了两次", tplReplace("{{lober:firstboot}}\n", "{{lober:firstboot}} {{lober:firstboot}}\n")},
	})
	// kickstart 的三个部件都必需，换 preseed 反：它没有 syslog。
	onlyCode(t, Template(bareTemplate("debian", "{{lober:syslog}}\n"), nil), "part_not_in_format", 1)
}

func Test模板正文引用的系统值在该格式不可用(t *testing.T) {
	runTplEdits(t, "sysvalue_not_allowed", []tplEdit{
		{"kickstart 没有 smb", tplLine("# {{sys:smb}}")},
		{"kickstart 没有 iso", tplLine("url --url={{sys:iso}}")},
	})
}

// 正文引用的参数要在模板参数里声明过，不能是 text（C19）；子值、|join 与逐行重复的规则同片段。
// 另外逐行重复的行里不能有插槽或系统部件：它们只能展开一次，不能随列表项抄成几份。
func Test模板正文的参数引用(t *testing.T) {
	runTplEdits(t, "param_undeclared", []tplEdit{{"没声明", tplLine("network --hostname={{param:hostname}}")}})
	runTplEdits(t, "param_text_in_fragment", []tplEdit{
		{"text 进正文", withParam(model.ParamDecl{Name: "motd", Label: "登录提示", Type: "text"}, "# {{param:motd}}")},
	})
	runTplEdits(t, "param_accessor_invalid", []tplEdit{
		{"非 cidr 取子值", tplLine("# {{param:pkgs.addr}}")},
		{"标量用 |join", withParam(model.ParamDecl{Name: "host", Label: "主机名", Type: "hostname"},
			"network --hostname={{param:host|join}}")},
	})
	runTplEdits(t, "list_repeat_conflict", []tplEdit{
		{"一行两个逐行重复的列表", withParam(model.ParamDecl{Name: "ntp", Label: "NTP 服务器", Type: "hostname", List: true},
			"# {{param:pkgs}} {{param:ntp}}")},
		{"执行插槽之后有列表", tplReplace("{{slot:装后配置@post-target}}\n", "{{slot:装后配置@post-target}} {{param:pkgs}}\n")},
		{"列表之后有系统部件", tplReplace("{{lober:syslog}}\n", "{{param:pkgs}} {{lober:syslog}}\n")},
		// 每行每个参数只报一次（T9-6④）。
		{"系统部件之后同一个列表写了两次", tplReplace("{{lober:syslog}}\n", "{{lober:syslog}} {{param:pkgs}} {{param:pkgs}}\n")},
		{"列表之后有系统部件又有执行插槽", tplReplace("{{lober:firstboot}}\n",
			"{{param:pkgs}} {{lober:firstboot}} {{slot:收尾@post-target}}\n")},
	})
	passes(t, "同一个列表在一行里写两次", tplLine("# {{param:pkgs}} {{param:pkgs}}"))
	passes(t, "|join 的列表与执行插槽同行",
		tplReplace("{{slot:装后配置@post-target}}\n", "{{slot:装后配置@post-target}} {{param:pkgs|join}}\n"))
}

// 模板参数的声明逐个合法、不重名；取值来源只指向声明过的参数，fixed 的值通过类型校验，
// attr 的键是合法的主机属性键（C20）。
func Test模板参数的声明与取值来源(t *testing.T) {
	runTplEdits(t, "param_decl_invalid", []tplEdit{
		{"默认值通不过校验", func(tpl *model.Template, _ hookSet) { tpl.Params[0].Default = "vim tmux" }},
		{"类型不认识", func(tpl *model.Template, _ hookSet) { tpl.Params[0].Type = "float" }},
		{"名字重复", func(tpl *model.Template, _ hookSet) {
			tpl.Params = append(tpl.Params, model.ParamDecl{Name: "pkgs", Label: "重名", Type: "word"})
		}},
	})
	runTplEdits(t, "binding_param_unknown", []tplEdit{
		{"指向没声明的参数", setBinding("hostname", model.Binding{Source: model.BindingDefault, Editable: true})},
	})
	runTplEdits(t, "param_value_invalid", []tplEdit{
		{"fixed 的值通不过类型校验", setBinding("pkgs", model.Binding{Source: model.BindingFixed, Value: "vim tmux"})},
		{"fixed 没有值", setBinding("pkgs", model.Binding{Source: model.BindingFixed})},
	})
	runTplEdits(t, "attr_key_invalid", []tplEdit{
		{"属性键不是标识符", setBinding("pkgs", model.Binding{Source: model.BindingAttr, Attr: "Pkgs"})},
		{"属性键为空", setBinding("pkgs", model.Binding{Source: model.BindingAttr})},
		{"用了保留键", setBinding("pkgs", model.Binding{Source: model.BindingAttr, Attr: "arch"})},
	})
	passes(t, "取 default、向导里可改", setBinding("pkgs", model.Binding{Source: model.BindingDefault, Editable: true}))
	passes(t, "取主机属性", setBinding("pkgs", model.Binding{Source: model.BindingAttr, Attr: "pkg_list"}))
	passes(t, "没有取值来源", func(tpl *model.Template, _ hookSet) { tpl.Bindings = nil })
}

// 取值来源只有 fixed、attr、default 三种，零值也不算（T7-2）：JSON 里漏了 Source 的取值来源，不能被
// 当成其中哪一种，也不再借 param_value_invalid 说「应为某种格式」。
func Test取值来源不认识(t *testing.T) {
	runTplEdits(t, "binding_source_unknown", []tplEdit{
		{"模板参数的来源不认识", setBinding("pkgs", model.Binding{Source: "host", Value: "vim"})},
		{"模板参数漏了来源", setBinding("pkgs", model.Binding{Value: "vim", Editable: true})},
		{"实例参数的来源不认识", setInstBinding("分区", 0, "disk", model.Binding{Source: "host"})},
		{"实例参数的来源大小写不同", setInstBinding("装后配置", 1, "msg", model.Binding{Source: "Fixed", Value: "你好"})},
	})
	tpl, hooks := goodTemplate(), goodHooks()
	setBinding("pkgs", model.Binding{Source: "host"})(&tpl, hooks)
	if ps := Template(tpl, hooks); len(ps) != 1 || !strings.Contains(ps[0].Message, "「pkgs」的取值来源「host」") {
		t.Errorf("说明里应写出参数名与来源，得到：%v", ps)
	}
}

// fixed 值和默认值与主机无关，激活时就判定得了取不取得出正文或所选片段要的子值（L-4）：IPv6 的 cidr
// 取不出 netmask，模板一经批准，每台主机建任务都会报错，格子锁着的话永远建不了。问题落在取值来源上，
// Path 与参数求值的字段键相同。取自主机属性的要到求值时才知道。
func Test取值来源取不出正文或片段要的子值(t *testing.T) {
	const ipv4, ipv6 = "192.0.2.1/24", "2001:db8::1/64"
	gw := model.ParamDecl{Name: "gw", Label: "网关", Type: "cidr"}
	gwDefault := gw
	gwDefault.Default = ipv6
	netmask := "network --ip={{param:gw.addr}} --netmask={{param:gw.netmask}}"
	fixed := func(v string) model.Binding { return model.Binding{Source: model.BindingFixed, Value: v} }
	// gwHook 给分区插槽里的 hook 加上参数 d，line 写进它覆盖 rocky 与 almalinux 的片段。
	gwHook := func(d model.ParamDecl, line string) tplMod {
		return editHook("lvm", 3, func(h *model.Hook) {
			h.Params = append(h.Params, d)
			h.Impls[0].Body += line + "\n"
		})
	}
	// gwSplit 给分区插槽里的 hook 加上参数 gw，再把片段拆成每个系一份：rocky 的那份取 netmask，almalinux
	// 的只取 addr。
	gwSplit := editHook("lvm", 3, func(h *model.Hook) {
		h.Params = append(h.Params, gw)
		splitPart(h, netmask, "network --ip={{param:gw.addr}}")
	})
	cases := []struct {
		name string
		mod  tplMod
		want model.Subject
	}{
		{"模板参数写死了 IPv6", all(withParam(gw, netmask), setBinding("gw", fixed(ipv6))),
			model.Subject{Kind: "template", ID: tplID, Path: "tpl/gw"}},
		{"模板参数锁在 IPv6 的默认值上", all(withParam(gwDefault, netmask), setBinding("gw", model.Binding{Source: model.BindingDefault})),
			model.Subject{Kind: "template", ID: tplID, Path: "tpl/gw"}},
		{"模板参数没有取值来源，默认值是 IPv6", withParam(gwDefault, netmask),
			model.Subject{Kind: "template", ID: tplID, Path: "tpl/gw"}},
		{"实例参数写死了 IPv6", all(gwHook(gw, netmask), setInstBinding("分区", 0, "gw", fixed(ipv6))),
			model.Subject{Kind: "slot", ID: tplID, Path: "分区/1/gw"}},
		// hook 自己的激活判定拦得住这样的默认值，这里给的却是已审批的版本：在这条规则之前批准的照样能被引用。
		{"实例参数沿用 hook 的 IPv6 默认值", gwHook(gwDefault, netmask),
			model.Subject{Kind: "slot", ID: tplID, Path: "分区/1/gw"}},
		// 所勾各系选中的片段不同，引用要并起来查：只看其中一个系，另一个系的主机建任务时照样报错。
		{"实例参数写死了 IPv6，只有一个系的片段取了 netmask", all(gwSplit, setInstBinding("分区", 0, "gw", fixed(ipv6))),
			model.Subject{Kind: "slot", ID: tplID, Path: "分区/1/gw"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tpl, hooks := goodTemplate(), goodHooks()
			c.mod(&tpl, hooks)
			ps := Template(tpl, hooks)
			onlyCode(t, ps, "param_accessor_invalid", 1)
			if ps[0].Subject != c.want {
				t.Errorf("Subject = %+v，应为 %+v", ps[0].Subject, c.want)
			}
		})
	}
	addrPrefix := "network --ipv6={{param:gw.addr}}/{{param:gw.prefix}}"
	ipv4Default := gw
	ipv4Default.Default = ipv4
	passes(t, "写死的 IPv4 取得出 netmask", all(withParam(gw, netmask), setBinding("gw", fixed(ipv4))))
	passes(t, "写死的 IPv6 取 addr 与 prefix", all(withParam(gw, addrPrefix), setBinding("gw", fixed(ipv6))))
	passes(t, "IPv4 的默认值取得出 netmask", withParam(ipv4Default, netmask))
	passes(t, "IPv6 的默认值取 addr 与 prefix", withParam(gwDefault, addrPrefix))
	passes(t, "取自主机属性的不查", all(withParam(gw, netmask), setBinding("gw", model.Binding{Source: model.BindingAttr, Attr: "gw"})))
	passes(t, "只有没勾的系的片段取了 netmask", all(
		editHook("lvm", 3, func(h *model.Hook) {
			h.Params = append(h.Params, gw)
			h.Impls = append(h.Impls, model.Impl{Family: "linux", Series: []catalog.Series{"debian"}, Kind: model.ImplFragment,
				Body: "d-i netcfg/get_netmask string {{param:gw.netmask}}\n"})
		}),
		setInstBinding("分区", 0, "gw", fixed(ipv6)),
	))
}

// 参数按参数求值的规则必填（被正文或所选片段引用，或者声明了 Required），取值来源却锁在默认值上，
// 声明里又没有默认值：向导里这一格锁着填不了，每台主机都会报 param_required（L-4）。
func Test锁在默认值上却没有默认值(t *testing.T) {
	locked := model.Binding{Source: model.BindingDefault}
	note := model.ParamDecl{Name: "note", Label: "备注", Type: "word"}
	requiredNote := note
	requiredNote.Required = true
	vg := model.ParamDecl{Name: "vg", Label: "卷组", Type: "word"}
	// vgHook 给分区插槽里的 hook 加上参数 vg，再由 mod 决定哪份实现引用它。
	vgHook := func(mod func(h *model.Hook)) tplMod {
		return editHook("lvm", 3, func(h *model.Hook) {
			h.Params = append(h.Params, vg)
			mod(h)
		})
	}
	// 覆盖 rocky 与 almalinux 的片段引用它，模板勾的两个系都选中这一份。
	inTicked := vgHook(func(h *model.Hook) { h.Impls[0].Body += "volgroup {{param:vg}} pv.01\n" })
	// 只有覆盖 debian 的片段引用它，模板没勾 debian。
	inUnticked := vgHook(func(h *model.Hook) {
		h.Impls = append(h.Impls, model.Impl{Family: "linux", Series: []catalog.Series{"debian"}, Kind: model.ImplFragment,
			Body: "d-i partman-auto-lvm/new_vg_name string {{param:vg}}\n"})
	})
	// 片段拆成每个系一份，只有 rocky 的那份引用它：所勾各系选中的片段要并起来看。
	inOneTicked := vgHook(func(h *model.Hook) { splitPart(h, "volgroup {{param:vg}} pv.01", "") })
	cases := []struct {
		name string
		mod  tplMod
		want model.Subject
	}{
		{"正文引用的模板参数", setBinding("pkgs", locked), model.Subject{Kind: "template", ID: tplID, Path: "tpl/pkgs"}},
		{"声明了必填的模板参数", all(declare(requiredNote), setBinding("note", locked)),
			model.Subject{Kind: "template", ID: tplID, Path: "tpl/note"}},
		{"脚本声明了必填的实例参数", setInstBinding("装后配置", 0, "msg", locked),
			model.Subject{Kind: "slot", ID: tplID, Path: "装后配置/1/msg"}},
		{"所选片段引用的实例参数", all(inTicked, setInstBinding("分区", 0, "vg", locked)),
			model.Subject{Kind: "slot", ID: tplID, Path: "分区/1/vg"}},
		{"只有一个系所选的片段引用的实例参数", all(inOneTicked, setInstBinding("分区", 0, "vg", locked)),
			model.Subject{Kind: "slot", ID: tplID, Path: "分区/1/vg"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tpl, hooks := goodTemplate(), goodHooks()
			c.mod(&tpl, hooks)
			ps := Template(tpl, hooks)
			onlyCode(t, ps, "param_locked_without_value", 1)
			if ps[0].Subject != c.want {
				t.Errorf("Subject = %+v，应为 %+v", ps[0].Subject, c.want)
			}
		})
	}
	passes(t, "有默认值", setInstBinding("分区", 0, "disk", locked))
	passes(t, "向导里可改", setBinding("pkgs", model.Binding{Source: model.BindingDefault, Editable: true}))
	passes(t, "不必填", all(declare(note), setBinding("note", locked)))
	passes(t, "只有没勾的系的片段引用了它", all(inUnticked, setInstBinding("分区", 0, "vg", locked)))
}

// 装配表只能引用正文里存在的插槽（C16）。插槽类型无从知道，里面的实例不再往下查，
// 所以引用了不存在的 hook 也不连带报 hook_ref_missing。
func Test装配表里的插槽正文里没有(t *testing.T) {
	runTplEdits(t, "assembly_slot_unknown", []tplEdit{
		{"有实例", func(tpl *model.Template, _ hookSet) {
			tpl.Assembly["软件包"] = []model.Instance{{HookID: "nope", HookVersion: 1}}
		}},
		{"实例为空", func(tpl *model.Template, _ hookSet) { tpl.Assembly["软件包"] = nil }},
	})
}

// 装配表里没有实例的插槽展开为空，不报错。
func Test装配表里没有实例的插槽不报(t *testing.T) {
	passes(t, "正文里另有一个空插槽", tplLine("{{slot:软件包}}"))
	passes(t, "插槽的实例列表为空", func(tpl *model.Template, _ hookSet) { tpl.Assembly["分区"] = nil })
	passes(t, "没有装配表", func(tpl *model.Template, _ hookSet) { tpl.Assembly = nil })
}

// 引用的 hook 版本要由调用方给齐，不去别处查；还要已审批（C22）、未废弃（C26）。
func Test引用的hook版本不存在未审批或已废弃(t *testing.T) {
	runTplEdits(t, "hook_ref_missing", []tplEdit{
		{"版本不存在", func(tpl *model.Template, _ hookSet) { tpl.Assembly["分区"][0].HookVersion = 4 }},
		{"ID 不存在", func(tpl *model.Template, _ hookSet) { tpl.Assembly["分区"][0].HookID = "lvm2" }},
	})
	// 装后插槽里放了 motd 两次，每个实例各报一条。
	runTplEditsN(t, "hook_ref_missing", 2, []tplEdit{
		{"调用方没给", func(_ *model.Template, hooks hookSet) { delete(hooks, model.HookKey{ID: "motd", Version: 1}) }},
	})
	runTplEdits(t, "hook_ref_unapproved", []tplEdit{
		{"草稿", editHook("lvm", 3, func(h *model.Hook) { h.State = model.VersionDraft })},
		{"状态为空", editHook("lvm", 3, func(h *model.Hook) { h.State = "" })},
	})
	runTplEditsN(t, "hook_ref_unapproved", 2, []tplEdit{
		{"待审批", editHook("motd", 1, func(h *model.Hook) { h.State = model.VersionReview })},
	})
	runTplEdits(t, "hook_ref_deprecated", []tplEdit{
		{"已废弃", editHook("lvm", 3, func(h *model.Hook) { h.State = model.VersionDeprecated })},
	})
}

// 对所勾的每个系，每个实例的 hook 都要有覆盖它的实现（C16）。
func Test实例的hook没有覆盖所勾的系(t *testing.T) {
	runTplEdits(t, "impl_missing_for_series", []tplEdit{
		{"片段只覆盖 rocky", editHook("lvm", 3, func(h *model.Hook) { h.Impls[0].Series = []catalog.Series{"rocky"} })},
	})
	// 三个实例都没有覆盖 rhel 的实现，各报一条。
	runTplEditsN(t, "impl_missing_for_series", 3, []tplEdit{
		{"模板多勾了 rhel", func(tpl *model.Template, _ hookSet) { tpl.Series = append(tpl.Series, "rhel") }},
	})
}

// 实现种类要与插槽类型一致（C16）：片段进片段插槽，脚本进执行插槽。同一个 hook 在不同系
// 可以是不同种类（spec §2.4），所以按系逐个对。
func Test实现种类与插槽类型不一致(t *testing.T) {
	// 模板勾了两个系，每个系各报一条。
	runTplEditsN(t, "impl_kind_mismatch", 2, []tplEdit{
		{"片段放进执行插槽", func(tpl *model.Template, _ hookSet) {
			tpl.Assembly["装后配置"] = []model.Instance{{HookID: "lvm", HookVersion: 3}}
		}},
		{"脚本放进片段插槽", func(tpl *model.Template, _ hookSet) {
			tpl.Assembly["分区"] = []model.Instance{{HookID: "motd", HookVersion: 1}}
		}},
	})
	runTplEdits(t, "impl_kind_mismatch", []tplEdit{
		{"almalinux 上是脚本", editHook("lvm", 3, func(h *model.Hook) {
			h.Impls[0].Series = []catalog.Series{"rocky"}
			h.Impls = append(h.Impls, model.Impl{Family: "linux", Series: []catalog.Series{"almalinux"},
				Kind: model.ImplScript, Body: "#!/bin/sh\ntrue\n", Interpreter: "sh", Timings: []catalog.Timing{"pre"}})
		})},
	})
}

// 放进执行插槽的脚本，声明的时机要包含插槽的时机（C16）。
func Test执行插槽的时机不在脚本声明的时机里(t *testing.T) {
	// 装后插槽里的两个实例、模板勾的两个系，共四条。
	runTplEditsN(t, "impl_timing_mismatch", 4, []tplEdit{
		{"插槽改成 post-installer", tplReplace("@post-target}}", "@post-installer}}")},
		{"脚本只声明了 firstboot", editHook("motd", 1, func(h *model.Hook) { h.Impls[0].Timings = []catalog.Timing{"firstboot"} })},
	})
	passes(t, "脚本声明了多个时机", editHook("motd", 1, func(h *model.Hook) {
		h.Impls[0].Timings = []catalog.Timing{"post-installer", "post-target"}
	}))
}

// 实例的取值来源按该 hook 的参数声明校验，规则同模板参数（C20）。
func Test实例的取值来源按hook的参数声明校验(t *testing.T) {
	runTplEdits(t, "binding_param_unknown", []tplEdit{
		{"指向 hook 没声明的参数", setInstBinding("分区", 0, "size", model.Binding{Source: model.BindingDefault})},
		{"指向模板参数", setInstBinding("分区", 0, "pkgs", model.Binding{Source: model.BindingDefault})},
	})
	runTplEdits(t, "param_value_invalid", []tplEdit{
		{"fixed 的值通不过类型校验", setInstBinding("分区", 0, "disk", model.Binding{Source: model.BindingFixed, Value: "sda"})},
		{"text 含 NUL", setInstBinding("装后配置", 1, "msg", model.Binding{Source: model.BindingFixed, Value: "a\x00b"})},
	})
	runTplEdits(t, "attr_key_invalid", []tplEdit{
		{"属性键不是标识符", setInstBinding("分区", 0, "disk", model.Binding{Source: model.BindingAttr, Attr: "Sys-Disk"})},
		{"用了保留键", setInstBinding("分区", 0, "disk", model.Binding{Source: model.BindingAttr, Attr: "sn"})},
	})
}

// 实例可以覆盖 hook 给的超时（C11），覆盖的值同样要在 1–21600 秒之间，两端都算合法。
func Test实例覆盖的超时不在范围内(t *testing.T) {
	runTplEdits(t, "timeout_invalid", []tplEdit{
		{"0 秒", setInstTimeout("装后配置", 1, 0)},
		{"负数", setInstTimeout("装后配置", 1, -1)},
		{"超过 6 小时", setInstTimeout("装后配置", 0, 21601)},
	})
	passes(t, "1 秒", setInstTimeout("装后配置", 1, 1))
	passes(t, "21600 秒", setInstTimeout("装后配置", 1, 21600))
	// 片段在渲染时就写进了应答文件，没有执行这一步，覆盖的超时用不上。
	passes(t, "片段插槽里的实例不查", setInstTimeout("分区", 0, 0))
}

// 同一个 hook 可以在一个模板里出现多次，各带一组参数（C17），放进不同的插槽也一样。
func Test同一个hook在模板里出现多次不报错(t *testing.T) {
	passes(t, "片段 hook 放两次", func(tpl *model.Template, _ hookSet) {
		tpl.Assembly["分区"] = append(tpl.Assembly["分区"], model.Instance{HookID: "lvm", HookVersion: 3})
	})
	passes(t, "脚本 hook 放进两个插槽", func(tpl *model.Template, _ hookSet) {
		tpl.Body += "{{slot:收尾@post-target}}\n"
		tpl.Assembly["收尾"] = []model.Instance{{HookID: "motd", HookVersion: 1}}
	})
}

// Subject 按位置指向出问题的地方（T9-1）：模板整体与正文的问题指向模板，参数声明是 param/<序号>，取值
// 来源是 tpl/<参数名>；插槽的问题 Path 为插槽名，实例的为「插槽名/序号」，实例参数的取值来源为「插槽名/
// 序号/参数名」，后两种与参数求值的字段键一致；涉及正文位置的带上行列。前端靠它把错误标到具体的地方。
func Test模板问题的Subject(t *testing.T) {
	whole := model.Subject{Kind: "template", ID: tplID}
	field := func(path string) model.Subject { return model.Subject{Kind: "template", ID: tplID, Path: path} }
	body := func(line, col int) model.Subject {
		return model.Subject{Kind: "template", ID: tplID, Line: line, Col: col}
	}
	slot := func(path string, line, col int) model.Subject {
		return model.Subject{Kind: "slot", ID: tplID, Path: path, Line: line, Col: col}
	}
	cases := []struct {
		name string
		mod  tplMod
		code model.Code
		want model.Subject
	}{
		{"名称", func(tpl *model.Template, _ hookSet) { tpl.Name = "" }, "name_invalid", whole},
		{"族与系", func(tpl *model.Template, _ hookSet) { tpl.Series = nil }, "template_series_invalid", whole},
		{"参数声明", func(tpl *model.Template, _ hookSet) { tpl.Params[0].Default = "vim tmux" }, "param_decl_invalid", field("param/1")},
		{"重名的参数声明指向后一个", declare(model.ParamDecl{Name: "pkgs", Label: "重名", Type: "word"}),
			"param_decl_invalid", field("param/2")},
		{"参数的取值来源", setBinding("pkgs", model.Binding{Source: model.BindingFixed, Value: "vim tmux"}),
			"param_value_invalid", field("tpl/pkgs")},
		{"取值来源指向没声明的参数", setBinding("nope", model.Binding{Source: model.BindingDefault}),
			"binding_param_unknown", field("tpl/nope")},
		{"取值来源不认识", setBinding("pkgs", model.Binding{Source: "host"}), "binding_source_unknown", field("tpl/pkgs")},
		{"锁在默认值上却没有默认值", setBinding("pkgs", model.Binding{Source: model.BindingDefault}),
			"param_locked_without_value", field("tpl/pkgs")},
		{"正文存不下不带行列", tplLine("\x00"), "text_not_storable", whole},
		{"正文位置按 rune 计", tplLine("# 中文{{sys:smb}}"), "sysvalue_not_allowed", body(14, 5)},
		{"扫描问题改指向模板、保留行列", tplLine("  {{slot:x"), "placeholder_unclosed", body(14, 3)},
		{"插槽的问题", tplReplace("{{slot:分区}}\n", "# {{slot:分区}}\n"), "fragment_slot_not_alone", slot("分区", 4, 3)},
		{"重名的插槽指向后一处", tplLine("{{slot:分区}}"), "slot_duplicate", slot("分区", 14, 1)},
		{"执行插槽的时机该格式不支持", replaceWith(bareTemplate("esxi", "{{slot:装后@post-target}}\n")),
			"slot_timing_unsupported", model.Subject{Kind: "slot", ID: "bare@1", Path: "装后", Line: 3, Col: 1}},
		{"部件不属于该格式", replaceWith(bareTemplate("debian", "{{lober:syslog}}\n")),
			"part_not_in_format", model.Subject{Kind: "template", ID: "bare@1", Line: 3, Col: 1}},
		{"缺少的部件没有位置", tplReplace("{{lober:syslog}}\n", ""), "part_missing", whole},
		{"重复的部件指向后一处", tplLine("{{lober:cleanup}}"), "part_duplicate", body(14, 1)},
		{"装配表里不存在的插槽", func(tpl *model.Template, _ hookSet) { tpl.Assembly["收尾"] = nil },
			"assembly_slot_unknown", slot("收尾", 0, 0)},
		{"实例的 hook 引用", func(tpl *model.Template, _ hookSet) { tpl.Assembly["分区"][0].HookVersion = 9 },
			"hook_ref_missing", slot("分区/1", 0, 0)},
		{"实例覆盖的超时", setInstTimeout("装后配置", 1, 0), "timeout_invalid", slot("装后配置/2", 0, 0)},
		{"实例参数的取值来源", setInstBinding("装后配置", 1, "msg", model.Binding{Source: model.BindingFixed}),
			"param_value_invalid", slot("装后配置/2/msg", 0, 0)},
		{"实例参数的来源不认识", setInstBinding("分区", 0, "disk", model.Binding{Source: "host"}),
			"binding_source_unknown", slot("分区/1/disk", 0, 0)},
		{"实例参数的属性键", setInstBinding("分区", 0, "disk", model.Binding{Source: model.BindingAttr, Attr: "sn"}),
			"attr_key_invalid", slot("分区/1/disk", 0, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tpl, hooks := goodTemplate(), goodHooks()
			c.mod(&tpl, hooks)
			ps := Template(tpl, hooks)
			if len(ps) == 0 {
				t.Fatal("应报出问题，却通过了")
			}
			if ps[0].Code != c.code || ps[0].Subject != c.want {
				t.Errorf("第一条是 %s %+v，应为 %s %+v", ps[0].Code, ps[0].Subject, c.code, c.want)
			}
		})
	}
}

// 同一个实例里两个参数写了同一个坏属性键，问题按参数分开（T9-1）：说明里只有属性键，Path 不带参数名的
// 话两条逐字节相同，前端分不清是哪一格。
func Test实例取值来源的问题按参数分开(t *testing.T) {
	tpl, hooks := goodTemplate(), goodHooks()
	editHook("lvm", 3, func(h *model.Hook) {
		h.Params = append(h.Params, model.ParamDecl{Name: "data", Label: "数据盘", Type: "diskpath"})
	})(&tpl, hooks)
	bad := model.Binding{Source: model.BindingAttr, Attr: "Sys-Disk"}
	tpl.Assembly["分区"][0].Bindings = map[string]model.Binding{"disk": bad, "data": bad}
	var got []string
	for _, p := range Template(tpl, hooks) {
		got = append(got, string(p.Code)+" "+p.Subject.Path)
	}
	if want := []string{"attr_key_invalid 分区/1/data", "attr_key_invalid 分区/1/disk"}; !slices.Equal(got, want) {
		t.Errorf("得到 %q，应为 %q", got, want)
	}
}

// 同样的输入永远得到同样顺序的输出：先模板整体（名称、族与系、参数声明按位置、取值来源按参数名），
// 再正文（存不下的在前，其余按位置，缺少的部件随后），最后装配表（正文里没有的插槽按名字排，再按插槽
// 在正文里的顺序、实例在插槽里的顺序，实例的取值来源按参数名）。取值来源与装配表都是 map，多跑几次
// 才看得出顺序是否固定。
func Test模板问题的顺序固定(t *testing.T) {
	tpl, hooks := goodTemplate(), goodHooks()
	tpl.Name = ""
	tpl.Params = append(tpl.Params,
		model.ParamDecl{Name: "pkgs", Label: "重名", Type: "word"},
		model.ParamDecl{Name: "gw", Label: "网关", Type: "cidr"},
	)
	tpl.Bindings["zz"] = model.Binding{Source: model.BindingDefault}
	tpl.Bindings["aa"] = model.Binding{Source: model.BindingDefault}
	tpl.Bindings["gw"] = model.Binding{Source: model.BindingFixed, Value: "2001:db8::1/64"}
	// 正文里写坏的占位符会让装配表里的未知插槽不报（见 Test正文扫描有问题时不报装配表里的插槽），这里只放
	// 扫描之后才查出的问题。
	tpl.Body = strings.Replace(tpl.Body, "{{lober:syslog}}\n", "", 1) +
		"# {{sys:smb}} {{param:nope}} {{param:gw.netmask}}\n{{slot:分区}}\n\x00\n"
	tpl.Assembly["分区"][0].Bindings["size"] = model.Binding{Source: model.BindingDefault}
	tpl.Assembly["分区"][0].Bindings["disk"] = model.Binding{Source: "host"}
	tpl.Assembly["装后配置"][1].HookVersion = 9
	tpl.Assembly["不存在"] = nil
	tpl.Assembly["also"] = nil
	want := []string{
		"name_invalid ",
		"param_decl_invalid param/2",
		"binding_param_unknown tpl/aa",
		"param_accessor_invalid tpl/gw",
		"binding_param_unknown tpl/zz",
		"text_not_storable ",
		"sysvalue_not_allowed ",
		"param_undeclared ",
		"slot_duplicate 分区",
		"part_missing ",
		"assembly_slot_unknown also",
		"assembly_slot_unknown 不存在",
		"binding_source_unknown 分区/1/disk",
		"binding_param_unknown 分区/1/size",
		"hook_ref_missing 装后配置/2",
	}
	for range 5 {
		var got []string
		for _, p := range Template(tpl, hooks) {
			got = append(got, string(p.Code)+" "+p.Subject.Path)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("问题顺序是 %q，应为 %q", got, want)
		}
	}
}

// 审批人不能为空，也不能是作者（C22）。首尾空白和大小写不同不算换了人：账号名在多数认证源里
// 不分大小写，宁可多拦一次，也不让作者换个写法自己批。
func Test审批人(t *testing.T) {
	cases := []struct {
		name, author, approver string
		want                   model.Code
		category               model.Category
	}{
		{"没有审批人", "alice", "", "approver_missing", model.CategoryInvalid},
		{"审批人只有空白", "alice", " \t", "approver_missing", model.CategoryInvalid},
		{"审批人是作者", "alice", "alice", "approver_is_author", model.CategoryState},
		{"换了大小写和空白还是作者", "alice", " Alice ", "approver_is_author", model.CategoryState},
		{"作者以外的人", "alice", "bob", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ps := Approver(c.author, c.approver)
			if c.want == "" {
				if len(ps) != 0 {
					t.Fatalf("应通过，得到：%v", ps)
				}
				return
			}
			if len(ps) != 1 || ps[0].Code != c.want || ps[0].Code.Category() != c.category {
				t.Fatalf("应只报 %s（%s），得到：%v", c.want, c.category, ps)
			}
		})
	}
}
