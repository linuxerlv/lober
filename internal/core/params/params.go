// Package params 是参数求值（spec §4.1）：给定模板、它引用的 hook、选定的 ISO 和一台主机，
// 给出这台主机在向导里的参数表单；带上操作员填的值时，给出最终取值或逐格的问题（C19–C21）。
//
// 向导的表单、建任务时的校验和快照构建都经这一个函数取值，所以向导里标红的格子和建任务被拒
// 的原因一致，渲染拿到的值也就是向导里看到的值。
package params

import (
	"cmp"
	"errors"
	"maps"
	"slices"
	"strconv"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/scan"
)

// FieldKey 是表单字段的键，前后端和错误定位共用这一种写法：模板参数为 tpl/<参数名>，
// 实例参数为 <插槽名>/<序号>/<参数名>，序号是实例在插槽里的位置，从 1 起。参数名是标识符、
// 插槽名不含 /，所以两种键不会撞上。
type FieldKey string

// Input 是求值的全部入参，由调用方凑齐：Hooks 要包含装配表引用的每个 hook 版本。
type Input struct {
	Template model.Template
	Hooks    map[model.HookKey]model.Hook
	ISO      model.ISO
	Host     model.Host
	// Entered 是操作员填的值。列表每行一项，与 ParamDecl.Parse 一致；空串算没填。
	Entered map[FieldKey]string
}

// Field 是表单里的一格：向导里每台主机一行，每个字段一列。
type Field struct {
	Key   FieldKey
	Label string // 参数的显示名；声明里没写时用参数名，列头和问题的说明里才不会是空的
	Type  model.ParamType
	List  bool
	// Required：被模板正文或所选片段引用的一律必填（C20），其余看声明的 Required。
	Required bool
	Locked   bool   // 取值来源不许在向导里改，只用预填值
	Prefill  string // 预填值，原样给出；取值来源是主机没登记的属性时为空
	// Options 只对 enum 有：声明的静态选项，或 ISO 提供的同名集合（C21）。
	Options []model.Option
}

// Values 是各字段的最终取值，键是字段键；没有值的可选字段不在其中。
type Values map[FieldKey]model.Value

// Result 是求值结果。Fields 先列模板参数，再按插槽在正文里的顺序、实例在插槽里的顺序列各实例
// 的参数，同样的输入顺序不变。Values 只在没有问题时非空。Problems 按表单的顺序排，结构性缺失
// 的问题排在那个实例的位置上；表单里没有的字段键排在最后，按字典序。
type Result struct {
	Fields   []Field
	Values   Values
	Problems []model.Problem
}

// Evaluate 求一台主机的参数表单与最终取值。每个字段依次：
//
//  1. 选实现：每个实例用 ISO 的系调 ImplFor；
//  2. 必填：被模板正文或所选片段引用的必填，只被脚本用到的看声明的 Required；
//  3. 预填：fixed 取它的值，attr 取主机属性，default 或没有 Binding 取声明的默认值；
//  4. 锁定：Binding 的 Editable 为 false 时锁定，没有 Binding 时可改；
//  5. 选项：enum 取声明的 Options，或 ISO 的 Offers 里同名的集合；
//  6. 最终值：可改的用操作员填的值，没填就用预填值；锁定的用预填值。然后按类型解析；正文或
//     所选片段对它取了子值的，子值还要取得出（IPv6 的 cidr 取不出 netmask）。
//
// 本函数假定模板已通过激活判定，不重做那些检查。引用的 hook 不在 Hooks 里、或实例没有覆盖
// ISO 的系的实现时，报 hook_ref_missing 或 impl_missing_for_series，跳过那个实例。
func Evaluate(in Input) Result {
	ev := evaluator{in: in, inForm: map[FieldKey]bool{}, values: Values{}}
	// 正文有扫描错误时照样给出插槽与引用，那些错误归激活判定报。
	body, _ := scan.Scan(in.Template.Body)
	ev.addParams("tpl/", in.Template.Params, in.Template.Bindings, refSubs(body.Params))
	seen := map[string]bool{}
	for _, slot := range body.Slots {
		if seen[slot.Name] {
			continue // 插槽重名由激活判定报（slot_duplicate），这里只按第一次出现的算
		}
		seen[slot.Name] = true
		for i, inst := range in.Template.Assembly[slot.Name] {
			ev.addInstance(slot.Name+"/"+strconv.Itoa(i+1), inst)
		}
	}
	ev.checkEntered()
	res := Result{Fields: ev.fields, Problems: ev.problems}
	if len(ev.problems) == 0 {
		res.Values = ev.values
	}
	return res
}

// evaluator 收集一次求值的结果。inForm 记下已进表单的字段键：重名的参数只算第一次出现的，
// 操作员填的值也按它核对。
type evaluator struct {
	in       Input
	inForm   map[FieldKey]bool
	fields   []Field
	values   Values
	problems []model.Problem
}

// addInstance 取实例所用 hook 在 ISO 的系上的实现，再把 hook 的参数列进表单。path 是
// 「插槽名/序号」，结构性缺失的问题落在这个实例上。
func (ev *evaluator) addInstance(path string, inst model.Instance) {
	t := ev.in.Template
	where := model.Subject{Kind: "slot", ID: t.ID + "@" + strconv.Itoa(t.Version), Path: path}
	h, ok := ev.in.Hooks[model.HookKey{ID: inst.HookID, Version: inst.HookVersion}]
	if !ok {
		ev.report(model.NewProblem("hook_ref_missing", where, inst.HookID, inst.HookVersion))
		return
	}
	impl, ok := h.ImplFor(ev.in.ISO.Series)
	if !ok {
		ev.report(model.NewProblem("impl_missing_for_series", where, inst.HookID, inst.HookVersion, ev.in.ISO.Series))
		return
	}
	// 片段的正文进应答文件，它引用的参数必填；脚本的参数经环境变量传入，看声明。
	// 种类不认识的也按片段算：多要几个必填，好过把空值渲染进应答文件。
	var refs map[string][]string
	if impl.Kind != model.ImplScript {
		frag, _ := scan.Scan(impl.Body)
		refs = refSubs(frag.Params)
	}
	ev.addParams(path+"/", h.Params, inst.Bindings, refs)
}

// addParams 把一组参数声明列进表单，字段键为 prefix 加参数名；refs 是正文或片段里引用到的
// 参数名，各带上对它取过的子值（见 refSubs）。
func (ev *evaluator) addParams(prefix string, decls []model.ParamDecl, bindings map[string]model.Binding, refs map[string][]string) {
	for _, d := range decls {
		key := FieldKey(prefix + d.Name)
		if ev.inForm[key] {
			continue // 参数重名由激活判定报（param_decl_invalid），这里只按第一次出现的算
		}
		ev.inForm[key] = true
		b, bound := bindings[d.Name]
		subs, referenced := refs[d.Name]
		ev.addField(key, d, b, bound, referenced, subs)
	}
}

// addField 按 Evaluate 文档的第 2–6 条求一个字段，subs 是正文或片段对它取过的子值。一格的问题
// 按发现的顺序报：填了锁定的字段，与取值本身有没有问题互不相干，两样都报；ISO 没提供选项集合
// 时，取值无从核对，就此打住。
func (ev *evaluator) addField(key FieldKey, d model.ParamDecl, b model.Binding, bound, referenced bool, subs []string) {
	f := Field{
		Key:      key,
		Label:    cmp.Or(d.Label, d.Name),
		Type:     d.Type,
		List:     d.List,
		Required: referenced || d.Required,
		Locked:   bound && !b.Editable,
		Prefill:  prefill(d, b, bound, ev.in.Host),
	}
	fromISO := d.Type == "enum" && d.Offer != ""
	var offered []model.Option
	switch {
	case fromISO:
		offered = ev.in.ISO.Offers[d.Offer]
		f.Options = slices.Clone(offered)
	case d.Type == "enum":
		f.Options = slices.Clone(d.Options)
	}
	ev.fields = append(ev.fields, f)

	at := ev.at(key)
	entered := ev.in.Entered[key]
	// 填的与预填值相同也算填：锁定的格子向导只显示、不提交，提交上来就是表单出了错。
	if f.Locked && entered != "" {
		ev.report(model.NewProblem("param_locked", at, f.Label))
	}
	// 空集合也算没提供：一个选项都没有，向导里无从选起。
	if fromISO && len(offered) == 0 {
		ev.report(model.NewProblem("iso_offer_missing", at, f.Label, d.Offer))
		return
	}
	raw := f.Prefill
	if !f.Locked && entered != "" {
		raw = entered
	}
	if raw == "" {
		switch {
		case !f.Required: // 可选的没有值就不进 Values
		case f.Locked && b.Source == model.BindingAttr:
			ev.report(model.NewProblem("param_attr_missing", at, f.Label, b.Attr))
		default:
			ev.report(model.NewProblem("param_required", at, f.Label))
		}
		return
	}
	v, err := d.Parse(raw)
	if err != nil {
		p, _ := errors.AsType[model.Problem](err) // Parse 的 error 一律是 Problem，Subject 由这里填
		p.Subject = at
		ev.report(p)
		return
	}
	// Parse 只查得出 enum 取值是不是安全词，在不在这个 ISO 的集合里要对照 ISO 查（C21）。
	if fromISO {
		for _, item := range v.Items() {
			if !slices.ContainsFunc(offered, func(o model.Option) bool { return o.Value == item }) {
				ev.report(model.NewProblem("param_not_offered", at, f.Label, item, d.Offer))
				return
			}
		}
	}
	// 子值适不适用于类型，激活判定按声明查过；取决于取值的那部分（IPv6 的 cidr 取不出 netmask）
	// 只能在这里查，漏了要到渲染应答文件时才失败。列表逐项取，有一项取不出就报。
	for _, sub := range subs {
		if _, err := v.Sub(sub); err != nil {
			p, _ := errors.AsType[model.Problem](err) // Sub 的 error 一律是 Problem，Subject 由这里填
			p.Subject = at
			ev.report(p)
			return
		}
	}
	ev.values[key] = v
}

// prefill 按取值来源给出预填值。attr 取主机属性，主机没登记它就没有值，不回落到声明的默认值：
// 回落会让「按主机指定系统盘」的模板，在漏登记的主机上悄悄用默认盘。来源不认识的也没有值。
func prefill(d model.ParamDecl, b model.Binding, bound bool, h model.Host) string {
	switch {
	case !bound, b.Source == model.BindingDefault:
		return d.Default
	case b.Source == model.BindingFixed:
		return b.Value
	case b.Source == model.BindingAttr:
		return h.Attrs[b.Attr]
	}
	return ""
}

// checkEntered 按字典序报 Entered 里表单中没有的字段键：多半是表单旧了，模板或 ISO 已经换过。
func (ev *evaluator) checkEntered() {
	for _, key := range slices.Sorted(maps.Keys(ev.in.Entered)) {
		if !ev.inForm[key] {
			ev.report(model.NewProblem("param_unknown_field", ev.at(key), string(key)))
		}
	}
}

// at 是落在字段上的问题的 Subject：Kind 为 param，ID 为主机 SN，Path 为字段键。
func (ev *evaluator) at(key FieldKey) model.Subject {
	return model.Subject{Kind: "param", ID: ev.in.Host.SN, Path: string(key)}
}

func (ev *evaluator) report(p model.Problem) {
	ev.problems = append(ev.problems, p)
}

// refSubs 整理一组参数引用：键是引用到的参数名，值是对它取过的子值，按第一次出现的顺序、
// 不重复。只按名字引用的参数也有键，值为空。
func refSubs(refs []scan.ParamRef) map[string][]string {
	subs := make(map[string][]string, len(refs))
	for _, r := range refs {
		s := subs[r.Name]
		if r.Sub != "" && !slices.Contains(s, r.Sub) {
			s = append(s, r.Sub)
		}
		subs[r.Name] = s
	}
	return subs
}
