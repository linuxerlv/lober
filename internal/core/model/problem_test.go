package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// appendixCodes 照抄计划附录「原因代码总表」。代码表必须与它加上 extraCodes 逐项一致：
// 改代码表就得同步改这里，测试就是为了逼出这一步。
var appendixCodes = map[Category][]Code{
	CategoryInvalid: {
		"sn_invalid", "sn_placeholder", "name_invalid",
		"param_decl_invalid", "param_value_invalid", "param_accessor_invalid",
		"attr_key_invalid",
		"placeholder_unknown_kind", "placeholder_malformed", "placeholder_unclosed",
		"timing_unknown", "part_unknown", "sysvalue_unknown",
		"approver_missing",
		"param_required", "param_locked", "param_not_offered", "param_unknown_field",
		"host_duplicate_in_batch",
	},
	CategoryIncomplete: {
		"hook_no_impl", "impl_series_invalid", "impl_series_overlap", "fragment_mixed_formats",
		"fragment_forbidden_placeholder", "sysvalue_not_allowed", "param_undeclared",
		"param_text_in_fragment", "list_repeat_conflict", "script_incomplete",
		"script_timing_unsupported", "script_interpreter_unavailable", "timeout_invalid",
		"template_series_invalid", "template_mixed_formats", "slot_duplicate",
		"slot_timing_unsupported", "fragment_slot_not_alone", "part_missing", "part_duplicate",
		"part_not_in_format", "binding_param_unknown", "assembly_slot_unknown",
		"hook_ref_missing", "hook_ref_unapproved", "hook_ref_deprecated",
		"impl_missing_for_series", "impl_kind_mismatch", "impl_timing_mismatch",
	},
	CategoryIncompatible: {
		"param_attr_missing", "iso_offer_missing",
		"arch_mismatch", "firmware_mismatch", "template_blocked_by_deprecated", "series_not_ticked",
	},
	CategoryState: {
		"approver_is_author",
		"host_has_open_task", "iso_not_ready", "template_not_approved",
	},
}

// extraCodes 是附录之外、按计划「并行执行」一节追加在代码表末尾的代码，各写明来由。
var extraCodes = map[Code]Category{
	"user_field_invalid":       CategoryInvalid, // 第 5 层用户字段规则，不能误报为 UTF-8/NUL 问题。
	"iso_media_unrecognized":   CategoryIncompatible,
	"iso_series_unrecognized":  CategoryIncompatible,
	"iso_series_mismatch":      CategoryIncompatible,
	"iso_content_mismatch":     CategoryIncompatible,
	"iso_version_missing":      CategoryIncompatible,
	"iso_extra_source_invalid": CategoryIncompatible,
	"iso_repo_incomplete":      CategoryIncompatible,
	"iso_artifact_missing":     CategoryIncompatible,
	"iso_tool_missing":         CategoryIncompatible,
	"iso_wim_compressed":       CategoryIncompatible,
	"iso_source_rejected":      CategoryInvalid,
	// 整层验收：{{sys:sn}} 要代入的主机 SN 不是安全词，由 snapshot.Build 报。
	"sn_unsafe_in_body": CategoryIncompatible,

	// 第 1 层修补轮（plans/2026-10-02-layer1-fixups.md「新增代码」），括号里是清单条目。
	"text_not_storable":               CategoryInvalid,      // 自由文本不是合法的 UTF-8 或含 NUL（T13-10）
	"iso_incomplete":                  CategoryState,        // ISO.Validate 不通过（L-6）
	"host_platform_invalid":           CategoryInvalid,      // 主机登记的架构或固件不合法（T4-1）
	"impl_kind_unknown":               CategoryIncomplete,   // 实现的种类不认识（T7-2）
	"binding_source_unknown":          CategoryIncomplete,   // 取值来源不认识（T7-2）
	"param_locked_without_value":      CategoryIncomplete,   // 锁在默认值上却没有默认值（L-4）
	"script_placeholder_not_expanded": CategoryIncomplete,   // 脚本里写了 lober 的占位符（L-1）
	"param_attr_unusable":             CategoryIncompatible, // 锁定取自主机属性的格子取值用不了（T11-8）
	"param_sub_unavailable":           CategoryInvalid,      // 填的值取不出模板要的子值（T11-8）
	"iso_import_failed":               CategoryState,        // ISO 导入失败（T5-1）

	// 第 2 层（plans/2026-10-03-rewrite-layer2-store.md 任务 1）。
	"host_not_registered": CategoryState, // 建任务时批里的 SN 没有登记，由仓储在事务里重跑准入时报

	// 第 2 层修补轮（plans/2026-10-08-layer2-fixups.md 任务 U5，S10-2）。
	"name_reserved": CategoryInvalid, // 用户新建、派生或改名时用了保留前缀 lober:

	// 第 3 层渲染骨架（任务 4）：渲染入参和单行替换不满足前提。
	"render_system_value_missing":  CategoryInvalid,
	"render_system_value_reserved": CategoryInvalid,
	"render_artifact_missing":      CategoryInvalid,
	"render_slot_invalid":          CategoryInvalid,
	"render_replacement_missing":   CategoryInvalid,
	"render_line_invalid":          CategoryInvalid,

	// Task 7: actual firmware, frozen boot snapshot compatibility, original ESXi media.
	"render_firmware_invalid":      CategoryInvalid,
	"render_boot_snapshot_invalid": CategoryIncompatible,
	"render_esxi_bootcfg_invalid":  CategoryInvalid,
	"render_windows_env_limit":     CategoryInvalid,
}

func TestCode表与附录总表逐项一致(t *testing.T) {
	want := map[Code]Category{}
	for cat, codes := range appendixCodes {
		for _, c := range codes {
			if prev, dup := want[c]; dup {
				t.Fatalf("测试自己抄的附录里 %s 出现了两次（%s、%s）", c, prev, cat)
			}
			want[c] = cat
		}
	}
	if len(want) != 58 {
		t.Fatalf("附录应有 58 个代码，测试里抄了 %d 个", len(want))
	}
	for c, cat := range extraCodes {
		if _, dup := want[c]; dup {
			t.Fatalf("追加的 %s 已在附录里", c)
		}
		want[c] = cat
	}
	// 58 appendix + 1 acceptance + 10 fixups + 1 layer2 + 1 layer2 fixup + 10 layer3 + 11 layer4 + 1 layer5.
	if len(want) != 93 {
		t.Fatalf("附录加上追加的代码应共 93 个，测试里有 %d 个", len(want))
	}
	for c, cat := range want {
		if got := c.Category(); got != cat {
			t.Errorf("%s 的类别是 %q，应为 %q（空表示没登记）", c, got, cat)
		}
	}
	for _, c := range registeredCodes() {
		if _, ok := want[c]; !ok {
			t.Errorf("代码表多出附录与 extraCodes 里都没有的 %s", c)
		}
	}
}

// 按声明的参数个数各构造一次。参数分别用字符串和整数：调用方会传行号、秒数这类整数，
// 模板若写成 %s，整数进去就成了 %!s(int=…)，会原样出现在界面上。
func TestCode逐个构造说明与下一步完整(t *testing.T) {
	for _, c := range registeredCodes() {
		n := codeTable[c].args
		for _, asInt := range []bool{false, true} {
			args := make([]any, n)
			for i := range args {
				if asInt {
					args[i] = 90001 + i
				} else {
					args[i] = fmt.Sprintf("‹参数%d›", i+1)
				}
			}
			s := Subject{Kind: "host", ID: "SN-1", Path: "disk", Line: 3, Col: 7}
			p := NewProblem(c, s, args...)
			if p.Code != c || p.Subject != s {
				t.Errorf("%s：代码或对象没有原样带出：%+v", c, p)
			}
			if strings.TrimSpace(p.Message) == "" || strings.TrimSpace(p.Next) == "" {
				t.Errorf("%s：说明或下一步为空：%+v", c, p)
			}
			for _, text := range []string{p.Message, p.Next} {
				if strings.Contains(text, "%!") {
					t.Errorf("%s：模板与参数对不上：%s", c, text)
				}
			}
			for i, a := range args {
				if !strings.Contains(p.Message+p.Next, fmt.Sprint(a)) {
					t.Errorf("%s：声明了 %d 个参数，第 %d 个在说明和下一步里都没用上", c, n, i+1)
				}
			}
		}
	}
}

// param_value_invalid 的「期望的格式」按参数类型登记在 codes.go。键要与计划任务 6
// 类型表里的十种类型一字不差：键写错了，Parse 取到的就是空串，说明里会缺一截。
func TestCode参数取值的格式按十种类型登记(t *testing.T) {
	types := []string{"word", "int", "enum", "ip", "cidr", "hostname", "diskpath", "sshkey", "pwhash", "text"}
	if len(paramValueFormats) != len(types) {
		t.Errorf("paramValueFormats 登记了 %d 种类型，应为 %d 种", len(paramValueFormats), len(types))
	}
	for _, typ := range types {
		if strings.TrimSpace(paramValueFormats[typ]) == "" {
			t.Errorf("类型 %s 没有登记期望的格式", typ)
		}
	}
}

// 修补轮新增与改写的代码，参数个数照修补计划。别的包按这些个数调用 NewProblem，个数一变，
// 并入时就会 panic；改写说明时不许顺手改个数，所以改写的几条也钉在这里。
func TestCode修补轮登记的参数个数(t *testing.T) {
	cases := []struct {
		code Code
		args int
	}{
		// 新增
		{"text_not_storable", 0},
		{"iso_incomplete", 1},
		{"host_platform_invalid", 2},
		{"impl_kind_unknown", 1},
		{"binding_source_unknown", 2},
		{"param_locked_without_value", 1},
		{"script_placeholder_not_expanded", 1},
		{"param_attr_unusable", 2},
		{"param_sub_unavailable", 2},
		{"iso_import_failed", 0},
		// 只改说明，个数不变
		{"name_invalid", 1},
		{"param_decl_invalid", 1},
		{"param_value_invalid", 2},
		{"impl_series_invalid", 0},
		{"template_series_invalid", 0},
		{"placeholder_unclosed", 0},
		{"iso_not_ready", 1},
		{"template_not_approved", 1},
	}
	for _, c := range cases {
		spec, ok := codeTable[c.code]
		switch {
		case !ok:
			t.Errorf("%s 没有登记", c.code)
		case spec.args != c.args:
			t.Errorf("%s 登记了 %d 个参数，修补计划定的是 %d 个", c.code, spec.args, c.args)
		}
	}
}

// 第 2 层登记的代码不带参数，SN 在 Subject 里（第 2 层计划任务 1）。仓储按这个个数调用 NewProblem，
// 个数一变，建任务时就会 panic。
func TestCode第2层登记的参数个数(t *testing.T) {
	if spec, ok := codeTable["host_not_registered"]; !ok || spec.args != 0 {
		t.Errorf("host_not_registered 应登记为不带参数：登记了 %v，参数 %d 个", ok, spec.args)
	}
	// name_reserved 带一个参数：规范化之后的名字（store.NormalizeName 按这个个数调用）。
	if spec, ok := codeTable["name_reserved"]; !ok || spec.args != 1 {
		t.Errorf("name_reserved 应登记为带一个参数：登记了 %v，参数 %d 个", ok, spec.args)
	}
}

// int 声明了上下限时，Parse 把 intRange* 之一填好数字、接在 int 的格式后面（T5-4①），操作员
// 看到的就是下面这几句。钉住拼法：措辞自己带着连接用的逗号，接上去不会多一个或少一个。
func TestCode的int范围措辞接在格式后面(t *testing.T) {
	cases := []struct {
		wording string
		args    []any
		want    string
	}{
		{intRangeMin, []any{int64(1)}, "十进制整数，不小于 1"},
		{intRangeMax, []any{int64(-1)}, "十进制整数，不大于 -1"},
		{intRangeBoth, []any{int64(1), int64(4094)}, "十进制整数，在 1 到 4094 之间"},
	}
	for _, c := range cases {
		if got := paramValueFormats["int"] + fmt.Sprintf(c.wording, c.args...); got != c.want {
			t.Errorf("拼出来是 %q，应为 %q", got, c.want)
		}
	}
}

// iso_incomplete、host_platform_invalid 的参数是字段的中文称呼，按 Subject.Path 里的字段名查
// fieldLabels。ISO.Validate 与 Host.Validate 用到的字段都要有称呼：查不到就是空串，说明里会缺一截。
func TestCode字段称呼覆盖ISO与主机校验的字段(t *testing.T) {
	for _, field := range []string{"family", "series", "arch", "version", "name", "offers", "firmware"} {
		if strings.TrimSpace(fieldLabels[field]) == "" {
			t.Errorf("字段 %s 没有登记中文称呼", field)
		}
	}
}

func TestCode未登记或参数个数不符时panic(t *testing.T) {
	cases := map[string]func(){
		"未登记的代码": func() { NewProblem("no_such_code", Subject{}) },
		"参数少了":   func() { NewProblem("sn_placeholder", Subject{}) },
		"参数多了":   func() { NewProblem("sn_placeholder", Subject{}, "A", "B") },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("应当 panic：这是编程错误，测试第一次走到就要暴露")
				}
			}()
			call()
		})
	}
	if got := Code("no_such_code").Category(); got != "" {
		t.Errorf("未登记代码的类别应为空，得到 %q", got)
	}
}

func TestProblem能经errorsAs取出(t *testing.T) {
	want := NewProblem("sn_placeholder", Subject{Kind: "host", ID: "N/A"}, "N/A")
	err := fmt.Errorf("登记主机：%w", want)
	var got Problem
	if !errors.As(err, &got) {
		t.Fatalf("errors.As 取不出 Problem：%v", err)
	}
	if got != want {
		t.Errorf("取出的 Problem 变了：%+v，应为 %+v", got, want)
	}
}

func TestProblem的Error带代码与说明(t *testing.T) {
	p := NewProblem("placeholder_unclosed", Subject{Kind: "body", ID: "tpl@3", Line: 5, Col: 9})
	msg := p.Error()
	for _, part := range []string{"placeholder_unclosed", p.Message, "tpl@3", "5", "9"} {
		if !strings.Contains(msg, part) {
			t.Errorf("Error() 缺少 %q：%s", part, msg)
		}
	}
}

// 字段键与实例的位置只由这三个函数构造（C-3），写法照修补计划 U6 的表：激活判定、参数求值与快照构建
// 都调它们。插槽名不含 /，实例参数的键比模板参数的多一段，名叫 tpl 的插槽也撞不上模板参数。
func Test字段键与实例位置的写法(t *testing.T) {
	cases := []struct{ got, want string }{
		{TemplateFieldKey("disk"), "tpl/disk"},
		{InstancePath("分区", 1), "分区/1"},
		{InstancePath("装后配置", 12), "装后配置/12"},
		{InstanceFieldKey("分区", 2, "disk"), "分区/2/disk"},
		{InstanceFieldKey("tpl", 1, "disk"), "tpl/1/disk"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("得到 %q，应为 %q", c.got, c.want)
		}
	}
	if got, want := InstanceFieldKey("装后配置", 3, "mtu"), InstancePath("装后配置", 3)+"/mtu"; got != want {
		t.Errorf("实例参数的键 %q 应以实例的位置 %q 开头", got, want)
	}
}

// registeredCodes 按字典序列出代码表，失败信息的顺序因此固定。
func registeredCodes() []Code {
	codes := make([]Code, 0, len(codeTable))
	for c := range codeTable {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}
