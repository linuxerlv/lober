package scan

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// sample 是 testdata 里的一份样例，以及它应扫出的四个列表：按出现顺序，重复的照列。
type sample struct {
	format catalog.Format
	file   string
	slots  []Slot
	parts  []catalog.SystemPart
	sys    []catalog.SystemValue
	params []ParamRef
}

// 七种格式各一份，按该格式的真实写法排版：YAML 的缩进与列表项、XML 的元素、preseed
// 只能写一行的指令、Jsonnet 的引号与尾逗号。扫描不看格式，同一套规则要在七种语法里
// 都切对。样例照各格式的能力表写（必需部件各一次，只用支持的时机和可用的系统值），
// 免得测的是现实里不会出现的正文。
var samples = []sample{
	{
		format: "kickstart", file: "kickstart.cfg",
		slots: []Slot{
			{Name: "分区"},
			{Name: "软件包"},
			{Name: "装前检查", Exec: true, Timing: "pre"},
			{Name: "收集日志", Exec: true, Timing: "post-installer"},
			{Name: "装后配置", Exec: true, Timing: "post-target"},
		},
		parts:  []catalog.SystemPart{"syslog", "firstboot", "cleanup"},
		sys:    []catalog.SystemValue{"repo", "lbrace"},
		params: []ParamRef{{Name: "hostname"}, {Name: "pkgs"}},
	},
	{
		format: "preseed", file: "preseed.cfg",
		slots: []Slot{
			{Name: "分区"},
			{Name: "装前", Exec: true, Timing: "pre"},
			{Name: "装后", Exec: true, Timing: "post-installer"},
		},
		parts:  []catalog.SystemPart{"firstboot", "cleanup"},
		sys:    []catalog.SystemValue{"repo"},
		params: []ParamRef{{Name: "hostname"}, {Name: "pkgs", Join: true}},
	},
	{
		format: "autoinstall", file: "autoinstall.yaml",
		slots: []Slot{
			{Name: "分区"},
			{Name: "装前", Exec: true, Timing: "pre"},
			{Name: "装后", Exec: true, Timing: "post-target"},
		},
		parts:  []catalog.SystemPart{"firstboot", "cleanup"},
		sys:    []catalog.SystemValue{"repo"},
		params: []ParamRef{{Name: "hostname"}, {Name: "pkgs"}},
	},
	{
		format: "autoyast", file: "autoyast.xml",
		slots: []Slot{
			{Name: "分区"},
			{Name: "装前", Exec: true, Timing: "pre"},
			{Name: "装后", Exec: true, Timing: "post-target"},
		},
		parts:  []catalog.SystemPart{"firstboot", "cleanup"},
		sys:    []catalog.SystemValue{"repo"},
		params: []ParamRef{{Name: "hostname"}, {Name: "pkgs"}},
	},
	{
		format: "agama", file: "agama.jsonnet",
		slots: []Slot{
			{Name: "分区"},
			{Name: "装前", Exec: true, Timing: "pre"},
			{Name: "装后", Exec: true, Timing: "post-target"},
		},
		parts:  []catalog.SystemPart{"firstboot", "cleanup"},
		sys:    []catalog.SystemValue{"repo"},
		params: []ParamRef{{Name: "hostname"}, {Name: "pkgs"}},
	},
	{
		format: "unattend", file: "unattend.xml",
		slots: []Slot{
			{Name: "分区"},
			{Name: "装前", Exec: true, Timing: "pre"},
			{Name: "首次开机", Exec: true, Timing: "firstboot"},
		},
		parts:  []catalog.SystemPart{"firstboot", "cleanup"},
		sys:    []catalog.SystemValue{"smb"},
		params: []ParamRef{{Name: "edition"}, {Name: "hostname"}},
	},
	{
		format: "esxi-ks", file: "esxi-ks.cfg",
		slots: []Slot{
			{Name: "附加配置"},
			{Name: "装前", Exec: true, Timing: "pre"},
			{Name: "装后", Exec: true, Timing: "post-installer"},
		},
		parts: []catalog.SystemPart{"firstboot", "cleanup"},
		sys:   []catalog.SystemValue{"task", "sn", "gateway"},
		params: []ParamRef{
			{Name: "disk"}, {Name: "addr", Sub: "addr"}, {Name: "addr", Sub: "netmask"},
			{Name: "hostname"}, {Name: "rootpw"},
		},
	},
}

func Test七种格式的样例扫出预期的占位符(t *testing.T) {
	var covered []catalog.Format
	for _, s := range samples {
		covered = append(covered, s.format)
		t.Run(string(s.format), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", s.file))
			if err != nil {
				t.Fatalf("读样例失败：%v", err)
			}
			src := string(raw)
			res, problems := Scan(src)
			if len(problems) != 0 {
				t.Errorf("样例应扫描无错，得到 %v", problems)
			}
			if !slices.Equal(res.Slots, s.slots) {
				t.Errorf("Slots = %+v，想要 %+v", res.Slots, s.slots)
			}
			if !slices.Equal(res.Parts, s.parts) {
				t.Errorf("Parts = %q，想要 %q", res.Parts, s.parts)
			}
			if !slices.Equal(res.Sys, s.sys) {
				t.Errorf("Sys = %q，想要 %q", res.Sys, s.sys)
			}
			if !slices.Equal(res.Params, s.params) {
				t.Errorf("Params = %+v，想要 %+v", res.Params, s.params)
			}
			// T8-8：手写期望也可能随样例一起偏离能力表，另按格式核对真实扫描结果。
			parts := slices.Sorted(slices.Values(res.Parts))
			if want := slices.Sorted(slices.Values(s.format.RequiredParts())); !slices.Equal(parts, want) {
				t.Errorf("Parts = %q，格式 RequiredParts = %q，想要一致", parts, want)
			}
			for _, value := range res.Sys {
				if !slices.Contains(s.format.SystemValues(), value) {
					t.Errorf("系统值 %q 不在格式 %q 的 SystemValues 中", value, s.format)
				}
			}
			for _, slot := range res.Slots {
				if slot.Exec && !slices.Contains(s.format.Timings(), slot.Timing) {
					t.Errorf("执行插槽 %q 的时机 %q 不在格式 %q 的 Timings 中", slot.Name, slot.Timing, s.format)
				}
			}
			checkSegments(t, src, res.Segments)
		})
	}
	// 新加一种格式时，先在这里补一份样例。
	if got, want := slices.Sorted(slices.Values(covered)), catalogFormats(); !slices.Equal(got, want) {
		t.Errorf("样例覆盖的格式 = %q，目录里的格式 = %q，想要一致", got, want)
	}
}

// catalogFormats 经族与系反查出目录里的全部格式，按字典序：目录没有直接列出格式的函数。
func catalogFormats() []catalog.Format {
	var out []catalog.Format
	for _, f := range catalog.Families() {
		for _, s := range catalog.SeriesOf(f) {
			if !slices.Contains(out, s.Format()) {
				out = append(out, s.Format())
			}
		}
	}
	slices.Sort(out)
	return out
}

// position 是 src 里字节偏移 off 处的行列：行按 \n 分，列按 rune 数，都从 1 起。
// 与实现分开写一遍，用来核对每一段的行列。
func position(src string, off int) (line, col int) {
	before := src[:off]
	lineStart := strings.LastIndexByte(before, '\n') + 1
	return strings.Count(before, "\n") + 1, utf8.RuneCountInString(before[lineStart:]) + 1
}

// checkSegments 核对扫描结果的形状：各段原文拼起来与输入逐字节相同（C18），没有空段，
// 相邻两段不都是原文，每段的行列与它在输入里的偏移相符，占位符以 {{ 开头、}} 结尾且不跨行。
func checkSegments(t *testing.T, src string, segs []Segment) {
	t.Helper()
	var joined strings.Builder
	for _, s := range segs {
		joined.WriteString(s.Raw)
	}
	if joined.String() != src {
		t.Fatalf("各段原文拼起来是 %q，输入是 %q", joined.String(), src)
	}
	off := 0
	for i, s := range segs {
		if s.Raw == "" {
			t.Errorf("第 %d 段是空段", i)
		}
		if i > 0 && s.Kind == KindText && segs[i-1].Kind == KindText {
			t.Errorf("第 %d、%d 段都是原文，应合成一段", i-1, i)
		}
		if line, col := position(src, off); s.Line != line || s.Col != col {
			t.Errorf("第 %d 段 %q 的位置是 %d:%d，应为 %d:%d", i, s.Raw, s.Line, s.Col, line, col)
		}
		if s.Kind != KindText && (!strings.HasPrefix(s.Raw, "{{") || !strings.HasSuffix(s.Raw, "}}") || strings.Contains(s.Raw, "\n")) {
			t.Errorf("第 %d 段是占位符，原文 %q 应以 {{ 开头、}} 结尾且不跨行", i, s.Raw)
		}
		off += len(s.Raw)
	}
}

func Test各段原文拼回输入逐字节相同(t *testing.T) {
	// C18：占位符以外的字节原样输出。渲染照段拼回，差一个字节（CRLF 的 \r、制表符缩进、
	// 文件末尾有没有换行）都可能改掉应答文件，所以写错的占位符和非法 UTF-8 也原样留下。
	cases := map[string]string{
		"空正文":      "",
		"只有原文":     "text\nurl --url=http://mirror/\n",
		"只有换行":     "\n\r\n\n",
		"CRLF":     "%post\r\n{{slot:装后@post-target}}\r\n\t{{param:pkgs}}\r\n%end\r\n",
		"制表符缩进":    "<a>\n\t\t{{slot:分区}}\n\t- {{param:pkgs}}\t\n</a>",
		"首尾带空格":    "{{  sys:repo  }}",
		"占位符紧挨着":   "{{sys:repo}}{{sys:sn}}{{slot:x}}",
		"写错的混在中间":  "a {{foo:x}} b {{slot:a b}} c {{param:x\n{{sys:nope}}\n",
		"Jinja":    "{{ ds.meta_data.local_hostname }}{%- if x %}{{- y -}}{% endif %}",
		"连续的花括号":   "{{{slot:a}}}{{{{sys:lbrace}}}}",
		"非法 UTF-8": "a\xff{{slot:分区}}\xe4\xb8\n{{sys:sn}}",
		"末尾不换行":    "x\n{{lober:cleanup}}",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			res, _ := Scan(src)
			checkSegments(t, src, res.Segments)
		})
	}
}

func Test片段带种类原文行列缩进与解析结果(t *testing.T) {
	// 渲染照这些字段展开：Indent 套到片段插槽展开出的每一行上，Alone 决定片段插槽放不放得下，
	// 行列给编辑器标位置。中文与制表符各算一列。
	src := "url --url={{sys:repo}}\n  {{slot:分区}}\r\n\t- {{param:addr.netmask}} {{lober:syslog}}\n{{slot:装后@post-target}}"
	want := []Segment{
		{Kind: KindText, Raw: "url --url=", Line: 1, Col: 1},
		{Kind: KindSys, Raw: "{{sys:repo}}", Line: 1, Col: 11, Sys: "repo"},
		{Kind: KindText, Raw: "\n  ", Line: 1, Col: 23},
		{Kind: KindSlot, Raw: "{{slot:分区}}", Line: 2, Col: 3, Indent: "  ", Alone: true, Slot: Slot{Name: "分区"}},
		{Kind: KindText, Raw: "\r\n\t- ", Line: 2, Col: 14},
		{Kind: KindParam, Raw: "{{param:addr.netmask}}", Line: 3, Col: 4, Indent: "\t", Param: ParamRef{Name: "addr", Sub: "netmask"}},
		{Kind: KindText, Raw: " ", Line: 3, Col: 26},
		{Kind: KindPart, Raw: "{{lober:syslog}}", Line: 3, Col: 27, Indent: "\t", Part: "syslog"},
		{Kind: KindText, Raw: "\n", Line: 3, Col: 43},
		{Kind: KindSlot, Raw: "{{slot:装后@post-target}}", Line: 4, Col: 1, Alone: true, Slot: Slot{Name: "装后", Exec: true, Timing: "post-target"}},
	}
	res, problems := Scan(src)
	if len(problems) != 0 {
		t.Errorf("应扫描无错，得到 %v", problems)
	}
	if !slices.Equal(res.Segments, want) {
		t.Errorf("Segments =\n%+v\n想要\n%+v", res.Segments, want)
	}
	wantSlots := []Slot{{Name: "分区"}, {Name: "装后", Exec: true, Timing: "post-target"}}
	if !slices.Equal(res.Slots, wantSlots) || !slices.Equal(res.Parts, []catalog.SystemPart{"syslog"}) ||
		!slices.Equal(res.Sys, []catalog.SystemValue{"repo"}) || !slices.Equal(res.Params, []ParamRef{{Name: "addr", Sub: "netmask"}}) {
		t.Errorf("四个列表 = %+v、%q、%q、%+v，与各段的解析结果对不上", res.Slots, res.Parts, res.Sys, res.Params)
	}
}

func Test写错的占位符报对代码与行列(t *testing.T) {
	// 行列指向占位符的 {{，给编辑器标红用：列按 rune 数，前面有中文或制表符时也不会错开。
	// 一个占位符只报一个问题：先查写法，写法对了再查目录里有没有。
	long := "{{slot:" + strings.Repeat("区", 33) + "}}"
	cases := []struct {
		name      string
		src       string
		code      model.Code
		line, col int
		args      []any
	}{
		{"种类不认识", "第一行\n  {{foo:bar}}\n", "placeholder_unknown_kind", 2, 3, []any{"foo"}},
		{"把网址写进了双花括号", "url --url={{http://mirror/}}", "placeholder_unknown_kind", 1, 11, []any{"http"}},
		// 种类在冒号前就定了，先于闭合检查：没打算写占位符的人，看到「种类不认识」才知道要转义。
		{"种类不认识也没闭合", "{{https://mirror/", "placeholder_unknown_kind", 1, 1, []any{"https"}},
		{"插槽名为空", "{{slot:}}", "placeholder_malformed", 1, 1, []any{"{{slot:}}"}},
		{"插槽名含空格", "分区：{{slot:系统 盘}}", "placeholder_malformed", 1, 4, []any{"{{slot:系统 盘}}"}},
		{"插槽名含中文标点", "{{slot:分区，主}}", "placeholder_malformed", 1, 1, []any{"{{slot:分区，主}}"}},
		{"插槽名含冒号", "{{slot:a:b}}", "placeholder_malformed", 1, 1, []any{"{{slot:a:b}}"}},
		{"插槽名超过 32 个字", long, "placeholder_malformed", 1, 1, []any{long}},
		{"时机为空", "{{slot:装后@}}", "placeholder_malformed", 1, 1, []any{"{{slot:装后@}}"}},
		{"写了两个时机", "{{slot:装后@pre@post-target}}", "placeholder_malformed", 1, 1, []any{"{{slot:装后@pre@post-target}}"}},
		{"冒号后有空格", "{{sys: repo}}", "placeholder_malformed", 1, 1, []any{"{{sys: repo}}"}},
		{"部件为空", "{{lober:}}", "placeholder_malformed", 1, 1, []any{"{{lober:}}"}},
		{"系统值带子值", "{{sys:repo.addr}}", "placeholder_malformed", 1, 1, []any{"{{sys:repo.addr}}"}},
		{"系统值带 join", "{{sys:repo|join}}", "placeholder_malformed", 1, 1, []any{"{{sys:repo|join}}"}},
		{"参数名有大写", "{{param:Disk}}", "placeholder_malformed", 1, 1, []any{"{{param:Disk}}"}},
		{"参数名是中文", "{{param:磁盘}}", "placeholder_malformed", 1, 1, []any{"{{param:磁盘}}"}},
		{"子值为空", "{{param:addr.}}", "placeholder_malformed", 1, 1, []any{"{{param:addr.}}"}},
		{"子值有大写", "{{param:addr.Netmask}}", "placeholder_malformed", 1, 1, []any{"{{param:addr.Netmask}}"}},
		{"两级子值", "{{param:addr.a.b}}", "placeholder_malformed", 1, 1, []any{"{{param:addr.a.b}}"}},
		{"join 大写", "{{param:pkgs|Join}}", "placeholder_malformed", 1, 1, []any{"{{param:pkgs|Join}}"}},
		{"join 写在子值前", "{{param:addrs|join.addr}}", "placeholder_malformed", 1, 1, []any{"{{param:addrs|join.addr}}"}},
		{"两个 join", "{{param:pkgs|join|join}}", "placeholder_malformed", 1, 1, []any{"{{param:pkgs|join|join}}"}},
		{"竖线后为空", "{{param:pkgs|}}", "placeholder_malformed", 1, 1, []any{"{{param:pkgs|}}"}},
		{"竖线两边有空格", "{{param:pkgs | join}}", "placeholder_malformed", 1, 1, []any{"{{param:pkgs | join}}"}},
		{"未闭合", "%pre\n\t{{slot:装前@pre\n}}\n", "placeholder_unclosed", 2, 2, nil},
		{"时机不认识", "{{slot:装后@post}}", "timing_unknown", 1, 1, []any{"post"}},
		{"时机大小写不对", "{{slot:装前@Pre}}", "timing_unknown", 1, 1, []any{"Pre"}},
		{"时机写成显示名", "{{slot:开机@首次开机}}", "timing_unknown", 1, 1, []any{"首次开机"}},
		{"部件不认识", "\t\t{{lober:rsyslog}}", "part_unknown", 1, 3, []any{"rsyslog"}},
		{"部件大小写不对", "{{lober:Syslog}}", "part_unknown", 1, 1, []any{"Syslog"}},
		{"系统值不认识", "源：{{sys:url}}", "sysvalue_unknown", 1, 3, []any{"url"}},
		{"把部件写成系统值", "{{sys:firstboot}}", "sysvalue_unknown", 1, 1, []any{"firstboot"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, problems := Scan(c.src)
			want := model.NewProblem(c.code, model.Subject{Kind: "body", Line: c.line, Col: c.col}, c.args...)
			if len(problems) != 1 || problems[0] != want {
				t.Errorf("Scan(%q) 的问题 = %v，想要恰好 %v", c.src, problems, want)
			}
			if got := c.code.Category(); got != model.CategoryInvalid {
				t.Errorf("%s 的类别是 %q，正文扫描产出的代码都应是 invalid", c.code, got)
			}
			// 写错的占位符当作原文：不单成一段，也不进任何列表。
			for _, s := range res.Segments {
				if s.Kind != KindText {
					t.Errorf("写错的占位符不该扫出占位符段，得到 %+v", s)
				}
			}
			if n := len(res.Slots) + len(res.Parts) + len(res.Sys) + len(res.Params); n != 0 {
				t.Errorf("写错的占位符不该进列表，得到 %+v、%q、%q、%+v", res.Slots, res.Parts, res.Sys, res.Params)
			}
			checkSegments(t, c.src, res.Segments)
		})
	}
}

func Test首尾带空格的占位符(t *testing.T) {
	// {{ 与 }} 之间允许首尾有空格，原文保留空格，解析结果与不带空格的相同；中间的空格不行，
	// 见「写错的占位符」里的「冒号后有空格」「竖线两边有空格」。
	cases := []struct {
		src  string
		want Segment
	}{
		{"{{ slot:分区 }}", Segment{Kind: KindSlot, Slot: Slot{Name: "分区"}}},
		{"{{   slot:装后@post-target}}", Segment{Kind: KindSlot, Slot: Slot{Name: "装后", Exec: true, Timing: "post-target"}}},
		{"{{lober:cleanup   }}", Segment{Kind: KindPart, Part: "cleanup"}},
		{"{{ sys:repo }}", Segment{Kind: KindSys, Sys: "repo"}},
		{"{{ param:addrs.addr|join }}", Segment{Kind: KindParam, Param: ParamRef{Name: "addrs", Sub: "addr", Join: true}}},
	}
	for _, c := range cases {
		res, problems := Scan(c.src)
		want := c.want
		want.Raw, want.Line, want.Col, want.Alone = c.src, 1, 1, true
		if len(problems) != 0 || len(res.Segments) != 1 || res.Segments[0] != want {
			t.Errorf("Scan(%q) = %+v、%v，想要恰好一段 %+v 且无错", c.src, res.Segments, problems, want)
		}
	}
}

func Test未闭合的占位符不跨行(t *testing.T) {
	// 同一行里没有 }} 就报未闭合，下一行开头的 }} 不会把它接上，下一行照常扫描。同一行里
	// 没闭合的 {{ 各报一次：内层的 {{ 截断外层后另起一个占位符，它自己也没有 }}，作者两处
	// 都得补，一次全标出来。
	src := "{{slot:a\n}}{{sys:repo}}\n{{slot:b {{slot:c\n  {{slot:d}}"
	res, problems := Scan(src)
	want := []model.Problem{
		model.NewProblem("placeholder_unclosed", model.Subject{Kind: "body", Line: 1, Col: 1}),
		model.NewProblem("placeholder_unclosed", model.Subject{Kind: "body", Line: 3, Col: 1}),
		model.NewProblem("placeholder_unclosed", model.Subject{Kind: "body", Line: 3, Col: 10}),
	}
	if !slices.Equal(problems, want) {
		t.Errorf("问题 = %v，想要 %v", problems, want)
	}
	if !slices.Equal(res.Slots, []Slot{{Name: "d"}}) || !slices.Equal(res.Sys, []catalog.SystemValue{"repo"}) {
		t.Errorf("Slots = %+v、Sys = %q，想要只有下面几行的 d 与 repo", res.Slots, res.Sys)
	}
	checkSegments(t, src, res.Segments)
}

func Test内容里遇到双左花括号就截断外层(t *testing.T) {
	// 插槽名、标识、子值里都没有花括号，占位符的内容不可能含 {{：内层的 {{ 另起一个占位符，
	// 外层截断在那里报未闭合。否则作者少写一个 }}，同一行后面的插槽就从编辑器的插槽列表里
	// 消失，渲染也找不到它（T8-1）。
	unknownKind := func(col int, word string) model.Problem {
		return model.NewProblem("placeholder_unknown_kind", model.Subject{Kind: "body", Line: 1, Col: col}, word)
	}
	unclosed := func(col int) model.Problem {
		return model.NewProblem("placeholder_unclosed", model.Subject{Kind: "body", Line: 1, Col: col})
	}
	cases := []struct {
		name     string
		src      string
		problems []model.Problem
		slots    []Slot
	}{
		// 种类不认识的占位符闭合了，就从它的 }} 之后接着扫，不丢掉这一行余下的部分。
		{"种类不认识之后接着扫", "{{foo:x}} {{slot:a}}", []model.Problem{unknownKind(1, "foo")}, []Slot{{Name: "a"}}},
		{"插槽里套插槽", "{{slot:a {{slot:b}}", []model.Problem{unclosed(1)}, []Slot{{Name: "b"}}},
		// 截断的外层不论种类都报未闭合，同一行后面多半还是作者要写的占位符。
		{"种类不认识的套插槽", "{{foo:x {{slot:b}}", []model.Problem{unclosed(1)}, []Slot{{Name: "b"}}},
		// 内层不归 lober 管也照样截断：外层的问题是没收住，不是内容写法不对。
		{"内层是 Jinja", "{{slot:a {{ x }}", []model.Problem{unclosed(1)}, nil},
		{"一行套了几层", "{{param:x {{sys:repo {{slot:c}}", []model.Problem{unclosed(1), unclosed(11)}, []Slot{{Name: "c"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, problems := Scan(c.src)
			if !slices.Equal(problems, c.problems) {
				t.Errorf("Scan(%q) 的问题 = %v，想要恰好 %v", c.src, problems, c.problems)
			}
			if !slices.Equal(res.Slots, c.slots) {
				t.Errorf("Scan(%q) 的 Slots = %+v，想要 %+v", c.src, res.Slots, c.slots)
			}
			checkSegments(t, c.src, res.Segments)
		})
	}
}

func Test用lbrace转义左花括号对(t *testing.T) {
	// {{sys:lbrace}} 渲染成字面量 {{。它自己是一个系统值占位符，紧跟的文字只是原文：
	// 「{{sys:lbrace}}slot:a}}」渲染出 {{slot:a}} 的字面量，扫描时没有插槽。正文里要写
	// {{http://…}} 这类字面量，也靠它绕开「种类不认识」。
	cases := []struct {
		src  string
		want []Segment
	}{
		{"{{sys:lbrace}}", []Segment{
			{Kind: KindSys, Raw: "{{sys:lbrace}}", Line: 1, Col: 1, Alone: true, Sys: "lbrace"},
		}},
		{"{{sys:lbrace}}slot:a}}", []Segment{
			{Kind: KindSys, Raw: "{{sys:lbrace}}", Line: 1, Col: 1, Sys: "lbrace"},
			{Kind: KindText, Raw: "slot:a}}", Line: 1, Col: 15},
		}},
		{"url={{sys:lbrace}}http://mirror/}}", []Segment{
			{Kind: KindText, Raw: "url=", Line: 1, Col: 1},
			{Kind: KindSys, Raw: "{{sys:lbrace}}", Line: 1, Col: 5, Sys: "lbrace"},
			{Kind: KindText, Raw: "http://mirror/}}", Line: 1, Col: 19},
		}},
	}
	for _, c := range cases {
		res, problems := Scan(c.src)
		if len(problems) != 0 || !slices.Equal(res.Segments, c.want) || len(res.Slots) != 0 {
			t.Errorf("Scan(%q) = %+v、%v，想要 %+v 且无错、无插槽", c.src, res.Segments, problems, c.want)
		}
		if !slices.Equal(res.Sys, []catalog.SystemValue{"lbrace"}) {
			t.Errorf("Scan(%q) 的 Sys = %q，lbrace 也是系统值引用，应列出", c.src, res.Sys)
		}
	}
}

func Test中文插槽名(t *testing.T) {
	// 插槽名给人看，可以用中文（全局约定）。1–32 个字按 rune 数，一个汉字算一个；
	// 超长与中文标点见「写错的占位符」。
	longest := strings.Repeat("区", 32)
	src := "说明：{{slot:分区}}\n{{slot:装后配置@post-target}}\n{{slot:网卡_2-业务}}\n{{slot:" + longest + "}}\n{{slot:Disk1}}"
	res, problems := Scan(src)
	if len(problems) != 0 {
		t.Errorf("应扫描无错，得到 %v", problems)
	}
	want := []Slot{
		{Name: "分区"},
		{Name: "装后配置", Exec: true, Timing: "post-target"},
		{Name: "网卡_2-业务"},
		{Name: longest},
		{Name: "Disk1"},
	}
	if !slices.Equal(res.Slots, want) {
		t.Errorf("Slots = %+v，想要 %+v", res.Slots, want)
	}
	// 「说明：」是三个 rune、九个字节：按字节数的话，插槽会被标在第 10 列。
	if len(res.Segments) < 2 || res.Segments[1].Kind != KindSlot || res.Segments[1].Col != 4 {
		t.Errorf("第一个插槽应在第 1 行第 4 列，Segments = %+v", res.Segments)
	}
	checkSegments(t, src, res.Segments)
}

func Test插槽名超长时说明里的上限与常量一致(t *testing.T) {
	// 插槽名的上限在 codes.go 的说明里另写了一份：改了 slotNameMaxRunes 却没改说明，
	// 作者就会照着错的上限去改（T5-5）。
	limit := strings.Repeat("区", slotNameMaxRunes)
	if _, problems := Scan("{{slot:" + limit + "}}"); len(problems) != 0 {
		t.Errorf("%d 个字的插槽名应扫描无错，得到 %v", slotNameMaxRunes, problems)
	}
	_, problems := Scan("{{slot:" + limit + "区}}")
	if len(problems) != 1 || problems[0].Code != "placeholder_malformed" {
		t.Fatalf("%d 个字的插槽名应恰好报一条 placeholder_malformed，得到 %v", slotNameMaxRunes+1, problems)
	}
	if p, want := problems[0], "1–"+strconv.Itoa(slotNameMaxRunes); !strings.Contains(p.Message+p.Next, want) {
		t.Errorf("placeholder_malformed 的说明 = %q，下一步 = %q，想要写明插槽名是 %s 个字", p.Message, p.Next, want)
	}
}

func Test独占一行的判定(t *testing.T) {
	// 片段插槽必须独占一行（任务 10），展开出的每一行都套用 Indent。独占指这一行除了前导
	// 空白（空格与制表符）、它自己和行尾空白（含 CRLF 的 \r），没有别的字符。
	cases := []struct {
		name   string
		src    string
		indent string
		alone  bool
	}{
		{"整个正文就是它", "{{slot:a}}", "", true},
		{"空格缩进", "x\n    {{slot:a}}\ny", "    ", true},
		{"制表符与空格混合缩进", "<a>\n\t \t{{slot:a}}\n</a>", "\t \t", true},
		{"行尾空白与 CRLF", "  {{slot:a}} \t\r\n", "  ", true},
		{"前面有非空白", "  - {{slot:a}}", "  ", false},
		{"后面有非空白", "\t{{slot:a}} #", "\t", false},
		{"同一行两个占位符", "{{slot:a}}{{sys:sn}}", "", false},
		{"后面有写错的占位符", "{{slot:a}} {{foo:x}}", "", false},
		{"后面有 Jinja", "{{slot:a}}{{ x }}", "", false},
		{"全角空格不算前导空白", string(rune(0x3000)) + "{{slot:a}}", "", false},
	}
	for _, c := range cases {
		res, _ := Scan(c.src)
		n := 0
		for _, s := range res.Segments {
			if s.Kind == KindText {
				continue
			}
			n++
			if s.Indent != c.indent || s.Alone != c.alone {
				t.Errorf("%s：%q 的 Indent = %q、Alone = %v，想要 %q、%v", c.name, s.Raw, s.Indent, s.Alone, c.indent, c.alone)
			}
		}
		if n == 0 {
			t.Errorf("%s：没有扫出占位符", c.name)
		}
	}
}

func TestJinja与其他双花括号写法原样保留(t *testing.T) {
	// {{ 后跳过空格，紧跟的不是「小写字母加冒号」就不归 lober 管：整段原样保留，不报错。
	// cloud-init 的 Jinja、Go 模板因此能直接写进正文。
	check := func(t *testing.T, src string) {
		t.Helper()
		res, problems := Scan(src)
		want := []Segment{{Kind: KindText, Raw: src, Line: 1, Col: 1}}
		if len(problems) != 0 || !slices.Equal(res.Segments, want) {
			t.Errorf("Scan(%q) = %+v、%v，想要原样一段原文且无错", src, res.Segments, problems)
		}
	}
	t.Run("Jinja", func(t *testing.T) {
		for _, src := range []string{
			"## template: jinja\n#cloud-config\nhostname: {{ ds.meta_data.local_hostname }}\n",
			"{{ v1.local_hostname }}",
			"{{- ds.meta_data.instance_id -}}",
			"{{ ds.meta_data.get('region', 'cn') }}",
			"{{ x | default('a:b') }}",
			"{{ 'slot:a' }}",
			"{%- if v1.distro == 'ubuntu' %}ok{% endif %}",
			"{# 注释 #}",
			"{{ {'a': 1} }}",
			"{{ .Values.image }}",
			"{{ ds.meta_data.local_hostname", // 不归 lober 管的，没闭合也不报
		} {
			check(t, src)
		}
	})
	t.Run("不是小写字母加冒号", func(t *testing.T) {
		// 这些看着像占位符，却不合「{{、可选的空格、小写字母、冒号」这个开头，计划规定原样保留。
		// 写错大小写的代价是渲染出字面量，预览里就能看到。只跳过空格，制表符不算。
		for _, src := range []string{
			"{{Slot:a}}", "{{SYS:repo}}", "{{slot :a}}", "{{\tslot:a}}", "{{_slot:a}}",
			"{{1:x}}", "{{:x}}", "{{slot}}", "{{}}", "{{ }}", "{{", "}}",
		} {
			check(t, src)
		}
	})
}

func Test多出来的花括号留在原文里(t *testing.T) {
	// 每个位置都试一次 {{：多出来的 { 和 } 留在原文段里，紧挨着的占位符照样识别，
	// 不会被吞掉、原样漏进应答文件。
	cases := []struct {
		src  string
		want []Segment
	}{
		{"{{{slot:a}}}", []Segment{
			{Kind: KindText, Raw: "{", Line: 1, Col: 1},
			{Kind: KindSlot, Raw: "{{slot:a}}", Line: 1, Col: 2, Slot: Slot{Name: "a"}},
			{Kind: KindText, Raw: "}", Line: 1, Col: 12},
		}},
		{"{{{{sys:lbrace}}", []Segment{
			{Kind: KindText, Raw: "{{", Line: 1, Col: 1},
			{Kind: KindSys, Raw: "{{sys:lbrace}}", Line: 1, Col: 3, Sys: "lbrace"},
		}},
		{"{{slot:a}}}}", []Segment{
			{Kind: KindSlot, Raw: "{{slot:a}}", Line: 1, Col: 1, Slot: Slot{Name: "a"}},
			{Kind: KindText, Raw: "}}", Line: 1, Col: 11},
		}},
	}
	for _, c := range cases {
		res, problems := Scan(c.src)
		if len(problems) != 0 || !slices.Equal(res.Segments, c.want) {
			t.Errorf("Scan(%q) = %+v、%v，想要 %+v 且无错", c.src, res.Segments, problems, c.want)
		}
	}
}

func Test参数引用的子值与join(t *testing.T) {
	// 子值适不适用于参数的类型、|join 是不是用在列表上，要看参数声明，扫描时不知道，
	// 由激活判定报 param_accessor_invalid；扫描只管写法，所以 disk.foo 也照样解析出来。
	src := "{{param:disk}} {{param:addr.netmask}} {{param:pkgs|join}} {{param:addrs.addr|join}} {{param:disk.foo}}"
	res, problems := Scan(src)
	want := []ParamRef{
		{Name: "disk"},
		{Name: "addr", Sub: "netmask"},
		{Name: "pkgs", Join: true},
		{Name: "addrs", Sub: "addr", Join: true},
		{Name: "disk", Sub: "foo"},
	}
	if len(problems) != 0 || !slices.Equal(res.Params, want) {
		t.Errorf("Params = %+v、问题 = %v，想要 %+v 且无错", res.Params, problems, want)
	}
}

func Test重复的插槽部件与引用如实列出(t *testing.T) {
	// 插槽名重复、部件出现两次由模板的激活判定报（slot_duplicate、part_duplicate），
	// 它靠这里如实列出每一次出现来数，所以扫描既不去重也不报错。
	src := "{{slot:分区}}\n{{lober:cleanup}}\n{{slot:分区@pre}}\n{{lober:cleanup}}\n{{sys:sn}}{{sys:sn}}{{param:disk}}{{param:disk}}"
	res, problems := Scan(src)
	if len(problems) != 0 {
		t.Errorf("重复不该报错，得到 %v", problems)
	}
	if want := []Slot{{Name: "分区"}, {Name: "分区", Exec: true, Timing: "pre"}}; !slices.Equal(res.Slots, want) {
		t.Errorf("Slots = %+v，想要 %+v", res.Slots, want)
	}
	if want := []catalog.SystemPart{"cleanup", "cleanup"}; !slices.Equal(res.Parts, want) {
		t.Errorf("Parts = %q，想要 %q", res.Parts, want)
	}
	if want := []catalog.SystemValue{"sn", "sn"}; !slices.Equal(res.Sys, want) {
		t.Errorf("Sys = %q，想要 %q", res.Sys, want)
	}
	if want := []ParamRef{{Name: "disk"}, {Name: "disk"}}; !slices.Equal(res.Params, want) {
		t.Errorf("Params = %+v，想要 %+v", res.Params, want)
	}
}

func Test有问题时仍返回其余占位符(t *testing.T) {
	// 草稿有错也照样保存，编辑器靠这份结果继续显示插槽列表（spec §5.2）。问题按出现位置排列。
	src := "{{slot:分区}}\n{{slot:a b}} {{slot:软件包}}\n{{foo:x}}\n{{slot:装后@post}}\n{{slot:装前@pre\n{{lober:firstboot}}"
	res, problems := Scan(src)
	at := func(line int) model.Subject { return model.Subject{Kind: "body", Line: line, Col: 1} }
	want := []model.Problem{
		model.NewProblem("placeholder_malformed", at(2), "{{slot:a b}}"),
		model.NewProblem("placeholder_unknown_kind", at(3), "foo"),
		model.NewProblem("timing_unknown", at(4), "post"),
		model.NewProblem("placeholder_unclosed", at(5)),
	}
	if !slices.Equal(problems, want) {
		t.Errorf("问题 =\n%v\n想要\n%v", problems, want)
	}
	if want := []Slot{{Name: "分区"}, {Name: "软件包"}}; !slices.Equal(res.Slots, want) {
		t.Errorf("Slots = %+v，想要 %+v", res.Slots, want)
	}
	if want := []catalog.SystemPart{"firstboot"}; !slices.Equal(res.Parts, want) {
		t.Errorf("Parts = %q，想要 %q", res.Parts, want)
	}
	checkSegments(t, src, res.Segments)
}

// placeholder_unknown_kind 的下一步列出了 lober 占位符的全部种类，还写了个数（C-9）：kindOf 加了一种，
// 说明得跟着补，否则作者照着说明找不到新种类。种类只登记在本包，model 的 codes_align_test 引不到 scan
// （会成环），所以在这里对齐，与插槽名的上限是同一个做法。
func Test种类不认识的说明列出的种类与扫描认的一致(t *testing.T) {
	_, problems := Scan("{{foo:x}}")
	if len(problems) != 1 || problems[0].Code != "placeholder_unknown_kind" {
		t.Fatalf("应恰好报一条 placeholder_unknown_kind，得到 %v", problems)
	}
	text := problems[0].Message + "\n" + problems[0].Next
	m := regexp.MustCompile(`占位符只有 (\S+) (\p{Han})种`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("说明里按「占位符只有 … 几种」找不到种类清单；措辞改了的话，先改测试里的写法再核对：\n%s", text)
	}
	listed := strings.Split(m[1], "、")
	if got, want := slices.Sorted(slices.Values(listed)), slices.Sorted(maps.Keys(kindOf)); !slices.Equal(got, want) {
		t.Errorf("说明列出的种类是 %q，扫描认的是 %q", got, want)
	}
	counts := []string{"零", "一", "两", "三", "四", "五", "六", "七", "八", "九"}
	if n := len(kindOf); n >= len(counts) || m[2] != counts[n] {
		t.Errorf("说明写的是「%s种」，扫描认 %d 种", m[2], n)
	}
}

// Refs 整理一份正文的参数引用（C-2）：每个参数一个键，子值按第一次出现的顺序、不重复；只按名字引用的
// 也有键。写错的占位符不在 Params 里，不算引用。激活判定与参数求值都从这里取，规则只有这一份。
func TestResult的Refs整理参数引用(t *testing.T) {
	res, _ := Scan("{{param:addr.netmask}} {{param:disk}}\n" +
		"{{param:addr.addr}} {{param:addr.netmask|join}} {{param:pkgs|join}} {{param:Bad}} {{slot:x}}\n")
	want := Refs{"addr": {"netmask", "addr"}, "disk": nil, "pkgs": nil}
	if got := res.Refs(); !maps.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Refs() = %q，想要 %q", got, want)
	}
	if got := (Result{}).Refs(); len(got) != 0 {
		t.Errorf("没有引用时 Refs() 应为空，得到 %q", got)
	}
}

// Merge 把几份正文的引用并起来（C-2）：一个 hook 的几份片段、一个实例在所勾各系里选中的片段，激活判定要
// 并起来查。已有的子值在前，新来的按出现顺序接在后面，不重复；并进来的切片不与 more 共用。
func TestRefs的Merge(t *testing.T) {
	refs := Refs{"addr": {"netmask"}, "disk": nil}
	more := Refs{"addr": {"addr", "netmask"}, "pkgs": nil, "disk": {"x"}, "dns": {"addr"}}
	refs.Merge(more)
	want := Refs{"addr": {"netmask", "addr"}, "disk": {"x"}, "pkgs": nil, "dns": {"addr"}}
	if !maps.EqualFunc(refs, want, slices.Equal) {
		t.Errorf("并起来是 %q，想要 %q", refs, want)
	}
	refs["dns"][0] = "prefix"
	if more["dns"][0] != "addr" {
		t.Error("改了并进来的子值，more 跟着变了：两者共用了切片")
	}
}

// Required 就是「被引用即必填」（C20、C-2）：被正文或所选片段引用的一律必填，没被引用的看声明的
// Required。没有引用（nil，比如实例只用到脚本）时只看声明。
func TestRefs的Required(t *testing.T) {
	refs := Refs{"disk": nil, "addr": {"netmask"}}
	cases := []struct {
		refs Refs
		decl model.ParamDecl
		want bool
	}{
		{refs, model.ParamDecl{Name: "disk"}, true},
		{refs, model.ParamDecl{Name: "addr"}, true},
		{refs, model.ParamDecl{Name: "note"}, false},
		{refs, model.ParamDecl{Name: "note", Required: true}, true},
		{nil, model.ParamDecl{Name: "disk"}, false},
		{nil, model.ParamDecl{Name: "disk", Required: true}, true},
	}
	for _, c := range cases {
		if got := c.refs.Required(c.decl); got != c.want {
			t.Errorf("引用 %q、声明 %+v：Required = %v，想要 %v", c.refs, c.decl, got, c.want)
		}
	}
}
