package model

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"

	"github.com/lober/lober/internal/core/catalog"
)

// VersionState 是 hook 或模板某个版本的状态。字面量会存进数据库，发布后不能改名。
// 审批后的版本不可修改，修改会产生新版本（C23），只有已审批的版本能用于建任务（C22）。
type VersionState string

const (
	VersionDraft      VersionState = "draft"      // 草稿：怎样都能存，可以预览
	VersionReview     VersionState = "review"     // 已提交，等待审批
	VersionApproved   VersionState = "approved"   // 已审批
	VersionDeprecated VersionState = "deprecated" // 已废弃：不能再被新引用，已建的任务不受影响（C26）
)

// HookKey 指一个 hook 版本。模板引用的是具体版本，hook 出了新版本，模板不会自动跟随（C23），
// 所以 ID 和版本合起来才是一个引用。
type HookKey struct {
	ID      string
	Version int
}

// ImplKind 是实现的种类。字面量会存进数据库，发布后不能改名。
type ImplKind string

const (
	ImplFragment ImplKind = "fragment" // 片段：渲染时放进片段插槽
	ImplScript   ImplKind = "script"   // 脚本：装机时在目标机上执行，放进执行插槽
)

// Hook 是一个 hook 版本，相当于一个组件：参数声明由它的全部实现共用（C9）。
// 第 2 层原样存它，字段名就是最终的名字。
type Hook struct {
	ID          string
	Version     int // 从 1 起
	Name        string
	Description string
	State       VersionState
	Author      string
	Approver    string
	Builtin     bool // 内置的只读，出厂即已审批（C25）
	Params      []ParamDecl
	// TimeoutSec 与 Required 是脚本实现的默认值：超时秒数，以及失败时任务是否随之失败。
	// 模板可以按实例改（C11）。
	TimeoutSec int
	Required   bool
	Impls      []Impl
}

// Impl 是 hook 的一份实现：属于一个族，可以覆盖该族的多个系（C8）。
type Impl struct {
	Family catalog.Family
	Series []catalog.Series // 非空，由激活判定查
	Kind   ImplKind
	Body   string
	// Interpreter 与 Timings 只对脚本有意义。
	Interpreter catalog.Interpreter
	Timings     []catalog.Timing
}

// TemplateKey 指一个模板版本，派生用它记下来源（C24）。
type TemplateKey struct {
	ID      string
	Version int
}

// Template 是一个模板版本：一份正文，可以勾同一种格式的多个系（C13），另带一张装配表。
// 第 2 层原样存它，字段名就是最终的名字。
type Template struct {
	ID       string
	Version  int // 从 1 起
	Name     string
	State    VersionState
	Author   string
	Approver string
	Builtin  bool // 内置的只读，出厂即已审批（C25）
	// DerivedFrom 是派生来源的模板 ID 与版本，新建的为 nil。它只记来源：派生时内容已整份
	// 复制过来，来源之后的修改不影响本模板（C24）。
	DerivedFrom *TemplateKey
	// Family 与 Series 在草稿里都可以留空，没勾系的模板不能激活（C13）。
	Family catalog.Family
	Series []catalog.Series
	Body   string
	Params []ParamDecl // 模板自己的参数声明
	// Bindings 是模板参数的取值来源，键为参数名。
	Bindings map[string]Binding
	// Assembly 是装配表：插槽名到按顺序放进该插槽的实例。插槽只来自正文（C14），这里只按名字引用。
	Assembly map[string][]Instance
}

// Instance 是装配表里的一个 hook 实例。同一个 hook 可以在一个模板里出现多次，各带一组取值来源（C17）。
type Instance struct {
	HookID      string
	HookVersion int
	// TimeoutSec 与 Required 覆盖 hook 给的默认值，nil 表示沿用（C11）。
	TimeoutSec *int
	Required   *bool
	Bindings   map[string]Binding // 键为该 hook 的参数名
}

// BindingSource 是参数取值的来源。字面量会存进数据库，发布后不能改名。
type BindingSource string

const (
	BindingFixed   BindingSource = "fixed"   // 模板写死的值
	BindingAttr    BindingSource = "attr"    // 主机属性
	BindingDefault BindingSource = "default" // 参数声明的默认值
)

// Binding 是一个参数的取值来源，以及向导里能不能改（C20）。参数没有 Binding 条目时，
// 等同于 {Source: BindingDefault, Editable: true}。
type Binding struct {
	Source   BindingSource
	Value    string // Source 为 fixed 时的取值，按参数声明校验；列表以换行分隔
	Attr     string // Source 为 attr 时的主机属性键
	Editable bool
}

// Key 是这个 hook 版本的引用键。
func (h Hook) Key() HookKey {
	return HookKey{ID: h.ID, Version: h.Version}
}

// ImplFor 返回覆盖系 s 的实现，没有时返回 false：渲染时每个 hook 取覆盖 ISO 的系的那一份（C27）。
// 同一个系出现在两份实现里是激活判定要拦的错误，这里不另报错，按实现的顺序取第一份。
// 返回的实现不与 hook 共用切片。
func (h Hook) ImplFor(s catalog.Series) (Impl, bool) {
	for _, impl := range h.Impls {
		if slices.Contains(impl.Series, s) {
			return impl.clone(), true
		}
	}
	return Impl{}, false
}

// Digest 是 Body 的 SHA256，小写十六进制。不加前缀、不规范化换行，与 coreutils sha256sum
// 对同样字节的输出一致：机器侧的运行脚本用 sha256sum 校验下载到的 hook 脚本，网关也只对
// 快照里钉住的摘要放行（spec §6.2 第 5 条）。
func (i Impl) Digest() string {
	sum := sha256.Sum256([]byte(i.Body))
	return hex.EncodeToString(sum[:])
}

// Format 是模板的格式：所勾的系非空且同格式时为 true，正文按这种格式写（C13）。
// 只看系推出的格式；族与系是否相符、系是否合法，由激活判定查。
func (t Template) Format() (catalog.Format, bool) {
	if len(t.Series) == 0 {
		return "", false
	}
	f := t.Series[0].Format()
	for _, s := range t.Series[1:] {
		if s.Format() != f {
			return "", false
		}
	}
	return f, f != "" // 系都不认识时，它们推出的格式同为空串
}

// CurrentApproved 从一个模板的各个版本里取当前的已审批版本：State 为 approved 的最高版本，
// 没有就返回 false。向导里每个模板只提供这个版本（C23）。废弃的不算，所以最新版被废弃后
// 回落到上一个已审批版本（C26）；新版本审批之前，旧版本照常可用。
// 版本可以按任意顺序传入；返回的模板不与入参共用切片、map 和指针。
func CurrentApproved(versions []Template) (Template, bool) {
	best := -1
	for i, v := range versions {
		if v.State == VersionApproved && (best < 0 || v.Version > versions[best].Version) {
			best = i
		}
	}
	if best < 0 {
		return Template{}, false
	}
	return versions[best].clone(), true
}

// 下面的 clone 都是深拷贝，并保留 nil 与空的区别：encoding/json 把两者分别编成 null 与 []，
// 副本与原对象要编码出同样的字节。

func (i Impl) clone() Impl {
	i.Series = slices.Clone(i.Series)
	i.Timings = slices.Clone(i.Timings)
	return i
}

func (t Template) clone() Template {
	t.DerivedFrom = clonePtr(t.DerivedFrom)
	t.Series = slices.Clone(t.Series)
	t.Params = cloneParamDecls(t.Params)
	t.Bindings = maps.Clone(t.Bindings) // Binding 只有值字段，复制 map 就够了
	if t.Assembly != nil {
		assembly := make(map[string][]Instance, len(t.Assembly))
		for slot, insts := range t.Assembly {
			insts = slices.Clone(insts)
			for i := range insts {
				insts[i] = insts[i].clone()
			}
			assembly[slot] = insts
		}
		t.Assembly = assembly
	}
	return t
}

func (in Instance) clone() Instance {
	in.TimeoutSec = clonePtr(in.TimeoutSec)
	in.Required = clonePtr(in.Required)
	in.Bindings = maps.Clone(in.Bindings)
	return in
}

func cloneParamDecls(decls []ParamDecl) []ParamDecl {
	decls = slices.Clone(decls)
	for i := range decls {
		decls[i].Options = slices.Clone(decls[i].Options)
		decls[i].Min = clonePtr(decls[i].Min)
		decls[i].Max = clonePtr(decls[i].Max)
	}
	return decls
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
