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
// Kind 取 host、iso、hook、impl、template、slot、param、body 之一；ID 是对象的标识；Path 是对象内的
// 位置，可空；Line、Col 是正文位置，从 1 起，列按 rune 计，不适用时为 0。
//
// 各处的写法如下，与修补计划 U6 的表一致（T9-1、C-4）；第 5、6 层按它解析，以后不要随手改。n 都从 1 起，
// 是在列表里的位置。参数声明和实现按位置写，即 param/<n>、impl/<n>，不写参数名，也不写系的列表：重名的声明、
// 勾了同一组系的实现因此分得开。取值来源的字段键是例外，要带参数名（tpl/<参数名>、<插槽名>/<n>/<参数名>）。
//   - 主机：Kind 为 host，ID 为 SN。主机整体的问题（未结束的任务、批内重复、平台冲突、SN 不是安全词、
//     没登记）Path 为空；登记的架构、固件不合法时 Path 为 arch、firmware；属性的问题 Path 为属性键。
//   - ISO：Kind 为 iso，ID 为 ISO 的 ID。状态不可用时 Path 为空；记录不合法时 Path 为 family、series、
//     arch、version、name，提供的集合为 offers/<集合名>。
//   - hook：针对单个版本时 ID 为「hook ID@版本」。整体的问题 Kind 为 hook、Path 为空；第 n 个参数声明 Kind 为
//     hook、Path 为 param/<n>；第 n 份实现 Kind 为 impl、Path 为 impl/<n>，正文位置另带行列。
//   - 模板：针对单个版本时 ID 为「模板 ID@版本」。整体与正文的问题 Kind 为 template、Path 为空，正文位置另带行列；
//     第 n 个参数声明 Kind 为 template、Path 为 param/<n>；模板参数的取值来源 Kind 为 template、Path 为
//     tpl/<参数名>（TemplateFieldKey）。插槽的问题 Kind 为 slot、Path 为插槽名，出自正文的另带行列；插槽里第 n 个
//     实例 Path 为 <插槽名>/<n>（InstancePath），实例参数的取值来源为 <插槽名>/<n>/<参数名>（InstanceFieldKey）。
//   - hook 与模板的另外两种 ID，由仓储（internal/store）给出。针对整个身份的问题，ID 是身份 ID，不带 @版本：改名
//     （RenameHook、RenameTemplate）时名称的问题是 {hook 或 template, 身份 ID}。新建与派生（CheckNewHook、
//     CheckNewTemplate、DeriveTemplate）时身份还没有 ID，ID 为空，Kind 与 Path 照针对单个版本时的写法填：名称的
//     问题是 {hook 或 template, 空}，第 n 份实现存不下的文本是 {impl, 空, impl/<n>}。
//   - 参数求值的一格：Kind 为 param，ID 为主机 SN，Path 为字段键，写法同上面两种取值来源，同一格在激活与
//     建任务时标在同一处。param_unknown_field 是例外：Path 原样照搬提交上来的键，不一定合字段键的写法。
//   - 正文扫描：Kind 为 body，ID 与 Path 为空，只带行列；调用方换成正文所属的对象，保留行列。
//
// 只有调用方知道对象的，Subject 留空、由调用方填上：参数声明的校验、取值的解析与取子值、名称的规范化
// （NormalizeName 的 name_invalid）、审批人的校验。保留前缀的判定 NameReserved 只回答是与否，不产出问题；
// name_reserved 由仓储的用户入口（store.NormalizeName）产出，Subject 同上面 hook 与模板里名称的问题。内置内容
// 装载时名称的问题针对单个版本，ID 为「ID@版本」。NormalizeSN 报 sn_invalid 时只给 Kind；报 sn_placeholder 时
// 另给 ID，即规范化之后的 SN。
type Subject struct {
	Kind, ID, Path string
	Line, Col      int
}

// 下面三个函数构造 Path 里与参数求值共用的写法（C-3）：实例的位置，以及模板参数、实例参数的字段键。
// 字段键也是向导表单里每一格的键（params.FieldKey）。激活判定、参数求值与快照构建都调它们，同一格在
// 三处才不会走岔；依赖守卫不许 activation 依赖 params，所以放在这里。参数名是标识符、插槽名不含 /，
// 实例参数的键比模板参数的多一段，两种键不会撞上。seq 是实例在插槽里的位置，从 1 起。

// TemplateFieldKey 是模板参数 param 的字段键：tpl/<参数名>。
func TemplateFieldKey(param string) string {
	return "tpl/" + param
}

// InstancePath 是插槽 slot 里第 seq 个实例的位置：<插槽名>/<序号>。实例上的问题以它为 Path。
func InstancePath(slot string, seq int) string {
	return slot + "/" + strconv.Itoa(seq)
}

// InstanceFieldKey 是插槽 slot 里第 seq 个实例的参数 param 的字段键：<插槽名>/<序号>/<参数名>。
func InstanceFieldKey(slot string, seq int, param string) string {
	return InstancePath(slot, seq) + "/" + param
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
