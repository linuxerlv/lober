package activation

import (
	"maps"
	"slices"
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/scan"
)

// Template 判断模板版本 t 能不能批准（C13–C17、C19、C20，以及 C22、C26 在模板上的部分）。
//
// hooks 由调用方凑齐，包含装配表引用的每个 hook 版本；缺的当作引用不存在，不去别处查。被引用的
// hook 版本自己完不完备，在它批准时已经查过（Hook），这里只查它能不能被引用、放进这个插槽对不对，
// 以及模板给它的参数定的取值来源。
//
// 问题的顺序固定：先模板整体（名称、族与系，参数声明按位置，取值来源按参数名），再正文（存不下的
// 在前，其余按位置，缺少的部件随后），最后装配表（正文里没有的插槽按名字排，再按插槽在正文里的
// 顺序、实例在插槽里的顺序，实例的取值来源按参数名）。
//
// Subject 按位置指向（T9-1），ID 都是「模板 ID@版本」：模板整体与正文的问题 Kind 为 template，Path
// 为空，正文的问题另带行列；第 n 个参数声明的 Path 为 param/<n>；模板参数取值来源的 Path 为
// tpl/<参数名>。插槽的问题 Kind 为 slot，Path 为插槽名，出自正文的带上行列；插槽里第 n 个实例的问题
// Path 为「插槽名/n」，实例参数取值来源的为「插槽名/n/参数名」。n 都从 1 起；后三种写法由 model 的
// TemplateFieldKey、InstancePath、InstanceFieldKey 构造，与参数求值的字段键（params.FieldKey）是同一份，
// 同一格在激活与建任务时标在同一处（C-3）。
func Template(t model.Template, hooks map[model.HookKey]model.Hook) []model.Problem {
	id := versionID(t.ID, t.Version)
	whole := model.Subject{Kind: model.KindTemplate, ID: id}
	var out []model.Problem
	if _, _, err := model.NormalizeName(t.Name); err != nil {
		out = append(out, withSubject(err, whole))
	}
	format, ps := templateFormat(t, whole)
	out = append(out, ps...)
	params, ps := checkDecls(t.Params, whole, nil)
	out = append(out, ps...)
	// 正文先扫一遍：取值来源要按正文引用的参数与子值查，正文的检查也用这份结果。
	res, scanned := scanBody(t.Body, whole)
	out = append(out, checkBindings(t.Bindings, params, res.Refs(), func(name string) model.Subject {
		s := whole
		s.Path = model.TemplateFieldKey(name)
		return s
	})...)
	out = append(out, checkTemplateBody(t.Body, res, scanned, format, params, whole)...)
	var series []catalog.Series // 推不出格式时为空，不查实现的覆盖
	if format != "" {
		series = t.Series
	}
	return append(out, checkAssembly(t.Assembly, res.Slots, len(scanned) == 0, format, series, hooks, id)...)
}

// Approver 校验审批人（C22）：要有，且不是作者。hook 与模板的审批共用它；内置内容出厂即已审批，
// 不走这里。Subject 留空：审批的是哪个版本只有调用方知道，由调用方填上。
//
// 首尾空白和大小写不同的不算换了人：账号名在多数认证源里不分大小写，宁可多拦一次，也不让作者
// 换个写法自己批。
func Approver(author, approver string) []model.Problem {
	a := strings.TrimSpace(approver)
	switch {
	case a == "":
		return []model.Problem{model.NewProblem("approver_missing", model.Subject{})}
	case strings.EqualFold(a, strings.TrimSpace(author)):
		return []model.Problem{model.NewProblem("approver_is_author", model.Subject{}, a)}
	}
	return nil
}

// templateFormat 是模板所勾的系推出的格式（C13）。系不对或不同格式时推不出来，返回空并报出原因；
// 这时按格式（时机、部件、系统值）和按系（实现的覆盖）的检查都跳过，理由同 checkFragment。
func templateFormat(t model.Template, whole model.Subject) (catalog.Format, []model.Problem) {
	if !seriesValid(t.Family, t.Series) {
		return "", []model.Problem{model.NewProblem("template_series_invalid", whole)}
	}
	return commonFormat(t.Series, "template_mixed_formats", whole)
}

// checkTemplateBody 查正文（C14、C15，以及与片段共有的规则）。res 与 scanned 是正文的扫描结果和
// 扫描报的问题，后者原样带出。正文要原样存下（T13-10）：说的是整份正文，不带位置，排在最前。
func checkTemplateBody(body string, res scan.Result, scanned []model.Problem, format catalog.Format, params map[string]paramInfo, whole model.Subject) []model.Problem {
	var out []model.Problem
	if !model.StorableText(body) {
		out = append(out, model.NewProblem("text_not_storable", whole))
	}
	out = append(out, scanned...)
	rules := newBodyRules(format, params)
	seen := map[string]bool{}             // 前面出现过的插槽名
	parts := map[catalog.SystemPart]int{} // 必需部件出现的次数
	for _, seg := range res.Segments {
		s := at(whole, seg.Line, seg.Col)
		switch seg.Kind {
		case scan.KindSlot:
			slotSubj := model.Subject{Kind: model.KindSlot, ID: whole.ID, Path: seg.Slot.Name, Line: seg.Line, Col: seg.Col}
			out = append(out, checkSlot(seg, format, seen, slotSubj)...)
			out = append(out, rules.placeholder(seg, s)...)
		case scan.KindPart:
			out = append(out, checkPart(seg.Part, format, parts, s)...)
			out = append(out, rules.placeholder(seg, s)...)
		default:
			out = append(out, rules.check(seg, s)...)
		}
	}
	slices.SortStableFunc(out, byPosition)
	for _, p := range format.RequiredParts() { // 推不出格式时为空
		if parts[p] == 0 {
			out = append(out, model.NewProblem("part_missing", whole, p, format))
		}
	}
	return out
}

// checkSlot 查正文里的一个插槽：名字不重复（C14）；片段插槽要展开成多行、每行套用这一行的
// 缩进，所以独占一行；执行插槽的时机该格式支持（C14）。seen 记着前面出现过的插槽名。
func checkSlot(seg scan.Segment, format catalog.Format, seen map[string]bool, s model.Subject) []model.Problem {
	slot := seg.Slot
	var out []model.Problem
	if seen[slot.Name] {
		out = append(out, model.NewProblem("slot_duplicate", s, slot.Name))
	}
	seen[slot.Name] = true
	switch {
	case !slot.Exec && !seg.Alone:
		out = append(out, model.NewProblem("fragment_slot_not_alone", s, slot.Name))
	case slot.Exec && format != "" && !slices.Contains(format.Timings(), slot.Timing):
		out = append(out, model.NewProblem("slot_timing_unsupported", s, slot.Timing, format))
	}
	return out
}

// checkPart 查一个系统部件（C15）：只能是该格式的必需部件，每个只写一次。count 记着每个必需部件
// 出现的次数，缺了哪个由调用方最后看。推不出格式时不查。
func checkPart(p catalog.SystemPart, format catalog.Format, count map[catalog.SystemPart]int, s model.Subject) []model.Problem {
	switch {
	case format == "":
		return nil
	case !slices.Contains(format.RequiredParts(), p):
		return []model.Problem{model.NewProblem("part_not_in_format", s, p, format)}
	}
	count[p]++
	if count[p] > 1 {
		return []model.Problem{model.NewProblem("part_duplicate", s, p)}
	}
	return nil
}

// checkAssembly 查装配表（C16、C17，以及 C22、C26 在引用上的部分）。slots 是正文里的插槽，按出现
// 顺序、重名的照列，重名时按第一次出现的类型查；bodyOK 表示正文扫描没有报问题；format 与 series
// 是推出的格式和要逐个对实现的系，推不出格式时都为空。装配表里没有实例的插槽展开为空，不报错；
// 同一个 hook 出现几次都不报错（C17）。
func checkAssembly(assembly map[string][]model.Instance, slots []scan.Slot, bodyOK bool, format catalog.Format, series []catalog.Series, hooks map[model.HookKey]model.Hook, id string) []model.Problem {
	known := make(map[string]bool, len(slots))
	var order []scan.Slot // 去掉重名后的插槽，按正文里的顺序
	for _, s := range slots {
		if !known[s.Name] {
			known[s.Name] = true
			order = append(order, s)
		}
	}
	var out []model.Problem
	// 正文里没有的插槽，类型无从知道，里面的实例对不对也无从说起，不再往下查。正文扫描报了问题时
	// 先不报它（T9-6②）：写坏的插槽占位符不进插槽列表，装配表里同名的插槽就成了「不存在」，作者照
	// 「从装配表里删掉它」去做，会丢掉配好的实例。正文改好之后，真不存在的照样会报。
	if bodyOK {
		for _, name := range slices.Sorted(maps.Keys(assembly)) {
			if !known[name] {
				out = append(out, model.NewProblem("assembly_slot_unknown", model.Subject{Kind: model.KindSlot, ID: id, Path: name}, name))
			}
		}
	}
	for _, slot := range order {
		// 执行插槽的时机该格式不支持时，正文已报 slot_timing_unsupported；再比实例的时机，下一步会让
		// 作者给 hook 加一个该格式根本没有的时机（T9-6③）。
		timingOK := !slot.Exec || slices.Contains(format.Timings(), slot.Timing)
		for i, inst := range assembly[slot.Name] {
			out = append(out, checkInstance(inst, slot, i+1, timingOK, series, hooks, id)...)
		}
	}
	return out
}

// checkInstance 查放进插槽 slot 的第 seq 个实例：引用的 hook 版本能被引用，覆盖的超时合法，对 series
// 里的每个系都有种类、时机对得上的实现（timingOK 为假时不比时机），取值来源按该 hook 的参数声明
// 合法。问题落在 model.InstancePath 给的位置上。引用的 hook 版本不在 hooks 里时只报这一条：别的检查
// 都要看它。
func checkInstance(inst model.Instance, slot scan.Slot, seq int, timingOK bool, series []catalog.Series, hooks map[model.HookKey]model.Hook, id string) []model.Problem {
	s := model.Subject{Kind: model.KindSlot, ID: id, Path: model.InstancePath(slot.Name, seq)}
	k := model.HookKey{ID: inst.HookID, Version: inst.HookVersion}
	h, ok := hooks[k]
	if !ok {
		return []model.Problem{model.NewProblem("hook_ref_missing", s, k.ID, k.Version)}
	}
	var out []model.Problem
	switch h.State {
	case model.VersionApproved:
	case model.VersionDeprecated: // 废弃的不能再被新引用（C26）
		out = append(out, model.NewProblem("hook_ref_deprecated", s, k.ID, k.Version))
	default: // 已审批的模板只能引用已审批的 hook 版本（C22）
		out = append(out, model.NewProblem("hook_ref_unapproved", s, k.ID, k.Version))
	}
	// 实例可以覆盖 hook 给的超时（C11），范围与 hook 的一样；片段插槽里的实例不执行，用不上它。
	if sec := inst.TimeoutSec; slot.Exec && sec != nil && !timeoutOK(*sec) {
		out = append(out, model.NewProblem("timeout_invalid", s, *sec))
	}
	// 渲染时每个 hook 取覆盖 ISO 的系的那份实现（C27），同一个 hook 在不同系可以是不同种类，
	// 所以对所勾的每个系逐个查。refs 并起各个系选中的实现引用的参数，哪些实现算引用与参数求值是
	// 同一条规则（model.Impl.Expanded）：脚本的参数经环境变量传入，不算；种类不认识的按片段算。
	refs := scan.Refs{}
	for _, sr := range series {
		impl, ok := h.ImplFor(sr)
		if !ok {
			out = append(out, model.NewProblem("impl_missing_for_series", s, k.ID, k.Version, sr))
			continue
		}
		switch impl.FitsSlot(slot.Exec, slot.Timing) {
		case "impl_kind_mismatch":
			out = append(out, model.NewProblem("impl_kind_mismatch", s, k.ID, k.Version, sr, impl.Kind))
		case "impl_timing_mismatch":
			if timingOK {
				out = append(out, model.NewProblem("impl_timing_mismatch", s, k.ID, k.Version, sr, slot.Timing))
			}
		}
		if impl.Expanded() {
			res, _ := scan.Scan(impl.Body)
			refs.Merge(res.Refs())
		}
	}
	// hook 的参数声明在它批准时已经查过，这里只要它们的类型，不再报声明的问题。
	params, _ := checkDecls(h.Params, model.Subject{}, nil)
	return append(out, checkBindings(inst.Bindings, params, refs, func(name string) model.Subject {
		p := s
		p.Path = model.InstanceFieldKey(slot.Name, seq, name)
		return p
	})...)
}

// checkBindings 查一组参数的取值来源（C20），params 是这组参数的声明，refs 是正文或所选片段对它们的
// 引用（见 scan.Refs）。按参数名的字典序报：Bindings 是 map，遍历顺序不固定。subject 给出每条问题
// 的指向。逐个参数查：
//
//   - Binding 指向的参数要声明过；来源要是 fixed、attr、default 之一，零值也不算（T7-2）。
//   - attr 的键要是合法的主机属性键；取到的值要到求值时才知道，不在这里查。
//   - fixed 的值，以及 default 来源（没有 Binding 的也是）取到的默认值，要通过类型校验，并取得出
//     引用里对它取的每个子值（L-4）：它们与主机无关，查不过的话每台主机建任务都会报错。
//   - 参数按参数求值的规则必填（scan.Refs.Required：被引用，或声明了 Required），Binding 却锁定在默认
//     值上、声明里又没有默认值的，向导里这一格锁着填不了，报 param_locked_without_value（L-4）。
//
// 写错的声明已经报过，不再按它查值。
func checkBindings(bindings map[string]model.Binding, params map[string]paramInfo, refs scan.Refs, subject func(name string) model.Subject) []model.Problem {
	names := slices.Collect(maps.Keys(bindings))
	for name := range params {
		if _, bound := bindings[name]; !bound {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var out []model.Problem
	for _, name := range names {
		b, bound := bindings[name]
		info, declared := params[name]
		s := subject(name)
		subs := refs[name]
		switch {
		case !declared:
			out = append(out, model.NewProblem("binding_param_unknown", s, name))
		case bound && !b.Source.Valid():
			out = append(out, model.NewProblem("binding_source_unknown", s, name, b.Source))
		case bound && b.Source == model.BindingAttr:
			// 属性键的规则只在 model 里登记一处：主机上登记不了的键，参数也取不到（T4-1）。
			if !model.ValidAttrKey(b.Attr) {
				out = append(out, model.NewProblem("attr_key_invalid", s, b.Attr))
			}
		case !info.ok: // 写错的声明已经报过，不再按它查值
		case bound && b.Source == model.BindingFixed:
			out = append(out, valueProblems(info.decl, b.Value, subs, s)...)
		case info.decl.Default != "":
			out = append(out, valueProblems(info.decl, info.decl.Default, subs, s)...)
		case bound && !b.Editable && refs.Required(info.decl):
			out = append(out, model.NewProblem("param_locked_without_value", s, name))
		}
	}
	return out
}
