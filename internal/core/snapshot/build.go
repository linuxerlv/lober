// Package snapshot 是快照构建（spec §4.1）：把选定的主机、ISO、模板版本、hook 版本和参数取值，
// 整理成渲染要用的完整输入。建任务时结果随任务固化（C29），预览时现算现用。全系统只有这一份
// 构建，预览与真实下发的输入才一致，C35 靠的就是它。
package snapshot

import (
	"maps"
	"slices"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/params"
	"github.com/lober/lober/internal/core/scan"
)

// Build 构建一台主机的快照，入参与参数求值的相同。
//
// 取值不另算：自己调用 params.Evaluate(in)，有问题就原样返回、不产出快照，所以建任务、预览与
// 这里用的是同一套取值规则，不会有两份结果对不上。插槽的顺序也不靠调用方给，自己重新扫描正文去取。
//
// 草稿没过激活判定也要能预览（C22），所以渲染依赖的前提由这里自己守住（L-7）。依次查下面几样，
// 有问题就返回这一样的问题，不产出快照，也不再往下查。前四样在正常流程里已被激活判定、准入判定与
// 参数求值挡住，这里是最后一道：错的快照要到机器装到一半才会暴露。
//   - ISO 的系不在模板所勾的系里，或不是目录里的系（推不出族与格式）：报 series_not_ticked，只报
//     这一条。模板本不为这个系而写，接着求值只会多出一串因它而起的 impl_missing_for_series。
//   - 正文里的插槽（见 slotProblems），每处都查、问题都报：重名报 slot_duplicate，执行插槽的时机该格式
//     不支持报 slot_timing_unsupported，与激活判定报的相同。正文本身不成立，参数的问题等它改好再报。
//   - 参数求值有问题：原样返回。
//   - 插槽里的实例（见 instance），每个都查、问题都报：引用的 hook 不在 in.Hooks 里报
//     hook_ref_missing，没有覆盖该系的实现报 impl_missing_for_series，与参数求值报的相同；所选实现
//     放不进这个插槽（model.Impl.FitsSlot）报 impl_kind_mismatch 或 impl_timing_mismatch。
//   - {{sys:sn}}（见 unsafeSN）：正文或所选片段引用了它、而主机 SN 不是安全词时报 sn_unsafe_in_body。
//     准入判定与参数求值都不查这一条；建任务与预览都要经这里构建快照，所以都在装机之前被拒。
//
// 渲染因此可以依赖五条不变式：执行插槽里只有带摘要的脚本，片段插槽里只有片段，Family 与 Format
// 非空，执行插槽的时机该格式支持，正文里的插槽名与 Slots 一一对应、没有重名。
//
// 同样的输入得到深度相等的快照，用 encoding/json 编码两次字节相同：列表按正文与装配表的顺序，
// map 由 encoding/json 按键排序。快照里的 map 与切片都是新建的，不与入参共用；没有内容的也给
// 空值而不是 nil，JSON 里统一是 {} 与 []。
func Build(in params.Input) (model.Snapshot, []model.Problem) {
	t, series := in.Template, in.ISO.Series
	// 目录里的系都推得出族与格式，catalog 的测试守着这一点。不认识的系，模板也不可能合法地勾上，
	// 所以与没勾同报。
	if !slices.Contains(t.Series, series) || !series.Valid() {
		subject := model.Subject{Kind: model.KindTemplate, ID: templateRef(t)}
		return model.Snapshot{}, []model.Problem{model.NewProblem("series_not_ticked", subject, string(series))}
	}
	if ps := slotProblems(t, series.Format()); len(ps) > 0 {
		return model.Snapshot{}, ps
	}
	res := params.Evaluate(in)
	if len(res.Problems) > 0 {
		return model.Snapshot{}, res.Problems
	}
	return buildFrom(in, res)
}

// slotProblems 按正文里的顺序查模板 t 的每个插槽：名字不重复，执行插槽的时机格式 format 支持。两条与
// 激活判定查插槽的相同（C14），Subject 也相同，落在插槽上、带行列；重名的报在后出现的那一处，同一处两条
// 都犯就两条都报。扫描错误归激活判定报，写错的插槽占位符不算插槽。
//
// 激活判定已拦下这两样，构建还要再查，是因为渲染依赖它们：快照的 Slots 不记正文里的插槽段对应哪一个，
// 重名时渲染可能拿片段插槽的实例去展开同名的执行插槽段；时机该格式不支持的执行插槽，安装器里没有地方跑。
func slotProblems(t model.Template, format catalog.Format) []model.Problem {
	body, _ := scan.Scan(t.Body)
	var out []model.Problem
	seen := make(map[string]bool, len(body.Slots))
	for _, seg := range body.Segments {
		if seg.Kind != scan.KindSlot {
			continue
		}
		slot := seg.Slot
		at := model.Subject{Kind: model.KindSlot, ID: templateRef(t), Path: slot.Name, Line: seg.Line, Col: seg.Col}
		if seen[slot.Name] {
			out = append(out, model.NewProblem("slot_duplicate", at, slot.Name))
		}
		seen[slot.Name] = true
		if slot.Exec && !slices.Contains(format.Timings(), slot.Timing) {
			out = append(out, model.NewProblem("slot_timing_unsupported", at, slot.Timing, format))
		}
	}
	return out
}

// buildFrom 是 Build 在参数求值通过之后的部分，res 是 in 的求值结果。单独拆出来，是为了测得到
// 参数求值与这里走岔的情形（T13-8），正常的入参走不到那里。
func buildFrom(in params.Input, res params.Result) (model.Snapshot, []model.Problem) {
	b := builder{in: in, values: res.Values, fields: make(map[params.FieldKey]params.Field, len(res.Fields))}
	for _, f := range res.Fields {
		b.fields[f.Key] = f
	}
	slots, ps := b.slots()
	if len(ps) > 0 {
		return model.Snapshot{}, ps
	}
	t, series := in.Template, in.ISO.Series
	snap := model.Snapshot{
		Schema: model.SnapshotSchema,
		// 族与格式由系推出：系的格式发布后不会变，三者在快照里不会互相矛盾。
		Family: series.Family(),
		Series: series,
		Format: series.Format(),
		Host: model.SnapshotHost{
			SN: in.Host.SN, Arch: in.Host.Arch, Firmware: in.Host.Firmware, Attrs: cloneStrings(in.Host.Attrs),
		},
		ISO: model.SnapshotISO{
			ID: in.ISO.ID, Name: in.ISO.Name, Version: in.ISO.Version, Arch: in.ISO.Arch,
			Artifacts: cloneStrings(in.ISO.Artifacts),
		},
		Template: model.SnapshotTemplate{
			ID: t.ID, Version: t.Version, Name: t.Name, Body: t.Body, Params: b.valuesOf(model.TemplateFieldKey, t.Params),
		},
		Slots: slots,
	}
	if ps := unsafeSN(snap); len(ps) > 0 {
		return model.Snapshot{}, ps
	}
	return snap, nil
}

// sysSN 是代入主机 SN 的系统值。
const sysSN catalog.SystemValue = "sn"

// unsafeSN 查 {{sys:sn}}：快照的模板正文或片段实例引用了它、而主机 SN 不是安全词时，报一条
// sn_unsafe_in_body，落在这台主机上；引用了几处都只报这一条。
//
// SN 的登记规则（model.NormalizeSN）收空格和任意可打印 ASCII，渲染又不解析语法、不知道该按什么
// 位置转义（C18），只能原样代入：空格会把指令和 shell 参数拆开，引号、<>&、#、「冒号加空格」会
// 破坏 JSON、XML、YAML 的结构，$( 与反引号落在 shell 里还会被执行。进正文与片段的参数只能是取值
// 不改变结构的受限类型（C19），SN 来自用户输入，进正文与片段时也守这条规矩，按安全词查；不引用
// 它的模板不受影响，登记规则也不因此收紧。
//
// 查的就是渲染要展开的那几份：快照里的模板正文，以及按 ISO 的系选中的片段。脚本不查：它的正文
// 原样下发、不展开占位符，网关按摘要放行，机器侧按摘要校验。
func unsafeSN(snap model.Snapshot) []model.Problem {
	if model.IsSafeWord(snap.Host.SN) || !refersSN(snap) {
		return nil
	}
	subject := model.Subject{Kind: model.KindHost, ID: snap.Host.SN}
	return []model.Problem{model.NewProblem("sn_unsafe_in_body", subject, snap.Host.SN)}
}

// refersSN 报告快照里要展开的正文有没有 {{sys:sn}}：模板正文，以及片段插槽里的实例。实例放不放得进
// 插槽在 slots 里查过（Build 的不变式），所以片段插槽里只有片段、执行插槽里只有脚本，后者原样下发、
// 不展开，不用再看种类。写错的占位符扫描时留在原文段里，不算引用。
func refersSN(snap model.Snapshot) bool {
	bodies := []string{snap.Template.Body}
	for _, s := range snap.Slots {
		if s.Exec {
			continue
		}
		for _, inst := range s.Instances {
			bodies = append(bodies, inst.Body)
		}
	}
	for _, body := range bodies {
		if res, _ := scan.Scan(body); slices.Contains(res.Sys, sysSN) {
			return true
		}
	}
	return false
}

// builder 在参数求值通过之后整理快照：values 是求出的最终取值，fields 是表单的各格，都按字段键查。
type builder struct {
	in     params.Input
	values params.Values
	fields map[params.FieldKey]params.Field
}

// slots 按正文里出现的顺序列出插槽，连同各实例的问题（见 instance）；有问题时这些插槽不完整，
// 由调用方丢掉。插槽重名在 Build 里已经拒了（slotProblems），这里每个插槽名只出现一次，序号就是它在
// 正文里的位置。扫描错误归激活判定报，写错的占位符原样留在正文里。
func (b builder) slots() ([]model.SnapshotSlot, []model.Problem) {
	body, _ := scan.Scan(b.in.Template.Body)
	out := make([]model.SnapshotSlot, 0, len(body.Slots))
	var problems []model.Problem
	for _, s := range body.Slots {
		insts := b.in.Template.Assembly[s.Name]
		slot := model.SnapshotSlot{
			Seq: len(out) + 1, Name: s.Name, Exec: s.Exec, Timing: s.Timing,
			Instances: make([]model.SnapshotInstance, 0, len(insts)),
		}
		for i, inst := range insts {
			// 序号从 1 起，与参数求值给实例定位、拼字段键用的一致。
			got, ps := b.instance(s, i+1, inst)
			slot.Instances = append(slot.Instances, got)
			problems = append(problems, ps...)
		}
		out = append(out, slot)
	}
	return out, problems
}

// instance 整理放进插槽 slot 的第 seq 个实例。它的位置与参数的字段键由 model.InstancePath、
// model.InstanceFieldKey 给出，与参数求值的是同一份（C-3）。
//
// hook 与实现自己再查一遍，不丢掉查找结果（T13-8）：参数求值通过了本不会查不到，可两边一旦走岔，
// 静默产出种类与正文全空的实例，就违反了「不做静默降级」。所选实现还要放得进这个插槽（L-7），
// Build 文档里插槽的那两条不变式靠的就是这一步。有问题时返回一条落在这个实例上的问题，Subject
// 与参数求值的一致，不给实例。
func (b builder) instance(slot scan.Slot, seq int, inst model.Instance) (model.SnapshotInstance, []model.Problem) {
	where := model.Subject{Kind: model.KindSlot, ID: templateRef(b.in.Template), Path: model.InstancePath(slot.Name, seq)}
	fail := func(code model.Code, args ...any) (model.SnapshotInstance, []model.Problem) {
		return model.SnapshotInstance{}, []model.Problem{model.NewProblem(code, where, args...)}
	}
	series := b.in.ISO.Series
	h, ok := b.in.Hooks[model.HookKey{ID: inst.HookID, Version: inst.HookVersion}]
	if !ok {
		return fail("hook_ref_missing", inst.HookID, inst.HookVersion)
	}
	impl, ok := h.ImplFor(series)
	if !ok {
		return fail("impl_missing_for_series", inst.HookID, inst.HookVersion, series)
	}
	switch code := impl.FitsSlot(slot.Exec, slot.Timing); code {
	case "impl_kind_mismatch":
		return fail(code, inst.HookID, inst.HookVersion, series, impl.Kind)
	case "impl_timing_mismatch":
		return fail(code, inst.HookID, inst.HookVersion, series, slot.Timing)
	}
	out := model.SnapshotInstance{
		HookID: inst.HookID, HookVersion: inst.HookVersion, HookName: h.Name,
		Kind: impl.Kind, Body: impl.Body,
		Params: b.valuesOf(func(param string) string { return model.InstanceFieldKey(slot.Name, seq, param) }, h.Params),
	}
	// 解释器、超时、是否必需和摘要只对正文不展开的实现有意义（model.Impl.Expanded）：走到这里种类已经
	// 查过，那就是脚本。片段在渲染时就写进了应答文件，没有执行这一步，网关也只该对脚本的摘要放行。
	if !impl.Expanded() {
		out.Interpreter = impl.Interpreter
		out.TimeoutSec, out.Required = h.TimeoutSec, h.Required
		if inst.TimeoutSec != nil {
			out.TimeoutSec = *inst.TimeoutSec
		}
		if inst.Required != nil {
			out.Required = *inst.Required
		}
		out.Digest = impl.Digest()
	}
	return out, nil
}

// valuesOf 取一组参数的最终取值，键为参数名，没有值的可选参数不在其中；fieldKey 按参数名给出它在表单
// 里的键（model.TemplateFieldKey 或 model.InstanceFieldKey）。类型与是否列表取自表单里的那一格：取值
// 就是按它解析的，参数重名时进了表单的也只有第一个声明。
func (b builder) valuesOf(fieldKey func(param string) string, decls []model.ParamDecl) map[string]model.SnapshotValue {
	out := make(map[string]model.SnapshotValue, len(decls))
	for _, d := range decls {
		key := params.FieldKey(fieldKey(d.Name))
		if v, ok := b.values[key]; ok {
			f := b.fields[key]
			out[d.Name] = model.SnapshotValue{Type: f.Type, List: f.List, Items: v.Items()}
		}
	}
	return out
}

// templateRef 是落在模板与实例上的问题的 Subject.ID，写成「模板 ID@版本」，与参数求值的一致。
func templateRef(t model.Template) string {
	return (model.TemplateKey{ID: t.ID, Version: t.Version}).String()
}

// cloneStrings 复制一份映射；nil 也给出空映射。
func cloneStrings(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	maps.Copy(out, m)
	return out
}
