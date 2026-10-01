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
	// 整层验收：{{sys:sn}} 要代入的主机 SN 不是安全词，由 snapshot.Build 报。
	"sn_unsafe_in_body": CategoryIncompatible,
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

// registeredCodes 按字典序列出代码表，失败信息的顺序因此固定。
func registeredCodes() []Code {
	codes := make([]Code, 0, len(codeTable))
	for c := range codeTable {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}
