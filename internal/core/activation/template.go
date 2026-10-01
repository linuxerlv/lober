package activation

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/scan"
)

// Template 判断模板版本 t 能不能批准（C13–C17、C19，以及 C22、C26 在模板上的部分）。
//
// hooks 由调用方凑齐，包含装配表引用的每个 hook 版本；缺的当作引用不存在，不去别处查。被引用的
// hook 版本自己完不完备，在它批准时已经查过（Hook），这里只查它能不能被引用、放进这个插槽对不对。
//
// 问题的顺序固定：先模板整体（名称、族与系、参数声明、按参数名排的取值来源），再正文（按位置，
// 缺少的部件随后），最后装配表（正文里没有的插槽按名字排，再按插槽在正文里的顺序、实例在插槽
// 里的顺序）。模板整体与正文的问题 Subject 的 Kind 为 template，ID 为「模板 ID@版本」，参数声明
// 与取值来源的 Path 为参数名，正文的问题带上行列；插槽的问题 Kind 为 slot，Path 为插槽名，出自
// 正文的带上行列；实例的问题 Kind 为 slot，Path 为「插槽名/序号」，序号从 1 起。
func Template(t model.Template, hooks map[model.HookKey]model.Hook) []model.Problem {
	id := versionID(t.ID, t.Version)
	whole := model.Subject{Kind: "template", ID: id}
	var out []model.Problem
	if _, _, err := model.NormalizeName(t.Name); err != nil {
		out = append(out, withSubject(err, whole))
	}
	format, ps := templateFormat(t, whole)
	out = append(out, ps...)
	params, ps := checkDecls(t.Params, whole)
	out = append(out, ps...)
	out = append(out, checkBindings(t.Bindings, params, func(name string) model.Subject {
		s := whole
		s.Path = name
		return s
	})...)
	slots, ps := checkTemplateBody(t.Body, format, params, whole)
	out = append(out, ps...)
	var series []catalog.Series // 推不出格式时为空，不查实现的覆盖
	if format != "" {
		series = t.Series
	}
	return append(out, checkAssembly(t.Assembly, slots, series, hooks, id)...)
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

// checkTemplateBody 查正文（C14、C15，以及与片段共有的规则），返回正文里的插槽：按出现顺序，
// 重名的照列。
func checkTemplateBody(body string, format catalog.Format, params map[string]paramInfo, whole model.Subject) ([]scan.Slot, []model.Problem) {
	res, out := scanBody(body, whole)
	rules := newBodyRules(format, params)
	seen := map[string]bool{}             // 前面出现过的插槽名
	parts := map[catalog.SystemPart]int{} // 必需部件出现的次数
	for _, seg := range res.Segments {
		s := at(whole, seg.Line, seg.Col)
		switch seg.Kind {
		case scan.KindSlot:
			slotSubj := model.Subject{Kind: "slot", ID: whole.ID, Path: seg.Slot.Name, Line: seg.Line, Col: seg.Col}
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
	return res.Slots, out
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
// 顺序、重名的照列，重名时按第一次出现的类型查；series 是要逐个对实现的系，推不出格式时为空。
// 装配表里没有实例的插槽展开为空，不报错；同一个 hook 出现几次都不报错（C17）。
func checkAssembly(assembly map[string][]model.Instance, slots []scan.Slot, series []catalog.Series, hooks map[model.HookKey]model.Hook, id string) []model.Problem {
	known := make(map[string]bool, len(slots))
	var order []scan.Slot // 去掉重名后的插槽，按正文里的顺序
	for _, s := range slots {
		if !known[s.Name] {
			known[s.Name] = true
			order = append(order, s)
		}
	}
	var out []model.Problem
	// 正文里没有的插槽，类型无从知道，里面的实例对不对也无从说起，不再往下查。
	for _, name := range slices.Sorted(maps.Keys(assembly)) {
		if !known[name] {
			out = append(out, model.NewProblem("assembly_slot_unknown", model.Subject{Kind: "slot", ID: id, Path: name}, name))
		}
	}
	for _, slot := range order {
		for i, inst := range assembly[slot.Name] {
			s := model.Subject{Kind: "slot", ID: id, Path: slot.Name + "/" + strconv.Itoa(i+1)}
			out = append(out, checkInstance(inst, slot, series, hooks, s)...)
		}
	}
	return out
}

// checkInstance 查放进插槽 slot 的一个实例：引用的 hook 版本能被引用，覆盖的超时合法，对 series
// 里的每个系都有种类、时机对得上的实现，取值来源按该 hook 的参数声明合法。引用的 hook 版本不在
// hooks 里时只报这一条：别的检查都要看它。
func checkInstance(inst model.Instance, slot scan.Slot, series []catalog.Series, hooks map[model.HookKey]model.Hook, s model.Subject) []model.Problem {
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
	want := model.ImplFragment
	if slot.Exec {
		want = model.ImplScript
	}
	// 渲染时每个 hook 取覆盖 ISO 的系的那份实现（C27），同一个 hook 在不同系可以是不同种类，
	// 所以对所勾的每个系逐个查。
	for _, sr := range series {
		impl, ok := h.ImplFor(sr)
		switch {
		case !ok:
			out = append(out, model.NewProblem("impl_missing_for_series", s, k.ID, k.Version, sr))
		case impl.Kind != want:
			out = append(out, model.NewProblem("impl_kind_mismatch", s, k.ID, k.Version, sr, impl.Kind))
		case slot.Exec && !slices.Contains(impl.Timings, slot.Timing):
			out = append(out, model.NewProblem("impl_timing_mismatch", s, k.ID, k.Version, sr, slot.Timing))
		}
	}
	// hook 的参数声明在它批准时已经查过，这里只要它们的类型，不再报声明的问题。
	params, _ := checkDecls(h.Params, model.Subject{})
	return append(out, checkBindings(inst.Bindings, params, func(string) model.Subject { return s })...)
}

// checkBindings 查取值来源（C20）：只指向声明过的参数；fixed 的值通过该参数的类型校验；attr 的键
// 是合法的主机属性键。按参数名的字典序报：Bindings 是 map，遍历顺序不固定。subject 给出每条问题
// 的指向。
func checkBindings(bindings map[string]model.Binding, params map[string]paramInfo, subject func(name string) model.Subject) []model.Problem {
	var out []model.Problem
	for _, name := range slices.Sorted(maps.Keys(bindings)) {
		b, s := bindings[name], subject(name)
		info, declared := params[name]
		if !declared {
			out = append(out, model.NewProblem("binding_param_unknown", s, name))
			continue
		}
		switch b.Source {
		case model.BindingDefault: // 用声明的默认值，没有要查的
		case model.BindingAttr:
			// 属性键的规则（标识符、不是保留键）只在 model 里登记一处，借 Host.Validate 来查，
			// 不另抄一份：主机上登记不了的键，参数也取不到。
			for _, p := range (model.Host{Attrs: map[string]string{b.Attr: ""}}).Validate() {
				p.Subject = s
				out = append(out, p)
			}
		case model.BindingFixed:
			out = append(out, valueProblems(info, b.Value, s)...)
		default:
			// 来源只有上面三种。不认识的说不清值从哪来，当作没有值：空串一律通不过 Parse
			// （ParamDecl.Parse 的约定），报出的 param_value_invalid 写着该参数期望的格式。
			out = append(out, valueProblems(info, "", s)...)
		}
	}
	return out
}

// valueProblems 查 raw 能不能通过参数的类型校验。写错的声明已经报过，不再按它查值。
func valueProblems(info paramInfo, raw string, s model.Subject) []model.Problem {
	if !info.ok {
		return nil
	}
	if _, err := info.decl.Parse(raw); err != nil {
		return []model.Problem{withSubject(err, s)}
	}
	return nil
}
