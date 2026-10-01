// Package model 放层 1 共用的领域类型：判定结果与原因代码、参数类型、
// 主机与 ISO、hook 与模板、任务与快照。scan、activation、params、admission、
// snapshot 都产出或消费这些类型，放进其中任何一个都会让判定包互相依赖。
package model

import (
	"fmt"
	"strconv"
	"strings"
)

// Code 是原因代码，全部登记在 codes.go 的代码表里。
type Code string

// Category 是原因的类别，第 5 层据此映射 HTTP 状态码。
type Category string

const (
	CategoryInvalid      Category = "invalid"      // 输入不合法
	CategoryIncomplete   Category = "incomplete"   // 未完备，激活不过
	CategoryIncompatible Category = "incompatible" // 组合不兼容，准入不过
	CategoryState        Category = "state"        // 对象状态不允许
)

// Subject 指出问题落在哪个对象的哪个位置，前端据此把错误标到具体的行或单元格。
//
// Kind 取 host、iso、hook、impl、template、slot、param、body 之一；ID 是 SN、ISO ID
// 或「hook ID@版本」这类标识；Path 是对象内的位置（参数的字段键、插槽名），可空；
// Line、Col 是正文位置，从 1 起，不适用时为 0。
type Subject struct {
	Kind, ID, Path string
	Line, Col      int
}

// Problem 是一条判定结果。说明与下一步只在代码表里写一次，所以向导里灰显的原因
// 和建任务被拒的原因字字相同。
type Problem struct {
	Code          Code
	Subject       Subject
	Message, Next string // 中文说明与下一步
}

// Error 给日志和开发者看：代码、对象位置和说明。界面用 Message 与 Next。
func (p Problem) Error() string {
	var where []string
	for _, s := range []string{p.Subject.Kind, p.Subject.ID, p.Subject.Path} {
		if s != "" {
			where = append(where, s)
		}
	}
	if p.Subject.Line > 0 {
		where = append(where, "第 "+strconv.Itoa(p.Subject.Line)+" 行第 "+strconv.Itoa(p.Subject.Col)+" 列")
	}
	if len(where) == 0 {
		return string(p.Code) + "：" + p.Message
	}
	return string(p.Code) + "（" + strings.Join(where, " ") + "）：" + p.Message
}

// Category 返回代码登记的类别；未登记的代码返回空。
func (c Code) Category() Category {
	return codeTable[c].category
}

// NewProblem 从代码表取说明与下一步的模板，用 args 填入。
//
// 代码未登记或参数个数与登记的不符时 panic：这是调用方的编程错误，测试第一次
// 走到就会暴露。判定函数按登记的个数调用时不会触发，所以不违反「判定函数不 panic」。
func NewProblem(c Code, s Subject, args ...any) Problem {
	spec, ok := codeTable[c]
	if !ok {
		panic(fmt.Sprintf("model.NewProblem：原因代码 %q 没有登记", c))
	}
	if len(args) != spec.args {
		panic(fmt.Sprintf("model.NewProblem：原因代码 %s 登记了 %d 个参数，调用时传了 %d 个", c, spec.args, len(args)))
	}
	return Problem{
		Code:    c,
		Subject: s,
		Message: fill(spec.message, args),
		Next:    fill(spec.next, args),
	}
}

// fill 填模板。说明和下一步共用同一组参数，各自只取用得到的那几个（写成 %[n]v），
// 不含动词的模板原样返回：交给 fmt 的话，用不上的参数会被印成 %!(EXTRA …)。
func fill(tmpl string, args []any) string {
	if !strings.Contains(tmpl, "%") {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}
