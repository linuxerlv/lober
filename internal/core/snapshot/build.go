// Package snapshot 是快照构建（spec §4.1）：把选定的主机、ISO、模板版本、hook 版本和参数取值，
// 整理成渲染要用的完整输入。建任务时结果随任务固化（C29），预览时现算现用。全系统只有这一份
// 构建，预览与真实下发的输入才一致，C35 靠的就是它。
package snapshot

import (
	"maps"
	"slices"
	"strconv"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/params"
	"github.com/lober/lober/internal/core/scan"
)

// Build 构建一台主机的快照，入参与参数求值的相同。
//
// 取值不另算：自己调用 params.Evaluate(in)，有问题就原样返回、不产出快照，所以建任务、预览与
// 这里用的是同一套取值规则，不会有两份结果对不上。插槽的顺序也不靠调用方给，自己重新扫描正文去取。
//
// 下面两种前提，正常流程里准入判定已经挡住了，这里是最后一道：错的快照要到机器装到一半才会暴露。
//   - ISO 的系不在模板所勾的系里：报 series_not_ticked，只报这一条。模板本不为这个系而写，
//     接着求值只会多出一串因它而起的 impl_missing_for_series。
//   - 某个实例没有覆盖该系的实现，或引用的 hook 不在 in.Hooks 里：参数求值会报
//     impl_missing_for_series 或 hook_ref_missing。
//
// 同样的输入得到深度相等的快照，用 encoding/json 编码两次字节相同：列表按正文与装配表的顺序，
// map 由 encoding/json 按键排序。快照里的 map 与切片都是新建的，不与入参共用；没有内容的也给
// 空值而不是 nil，JSON 里统一是 {} 与 []。
func Build(in params.Input) (model.Snapshot, []model.Problem) {
	t, series := in.Template, in.ISO.Series
	if !slices.Contains(t.Series, series) {
		subject := model.Subject{Kind: "template", ID: t.ID + "@" + strconv.Itoa(t.Version)}
		return model.Snapshot{}, []model.Problem{model.NewProblem("series_not_ticked", subject, string(series))}
	}
	res := params.Evaluate(in)
	if len(res.Problems) > 0 {
		return model.Snapshot{}, res.Problems
	}
	b := builder{in: in, values: res.Values, fields: make(map[params.FieldKey]params.Field, len(res.Fields))}
	for _, f := range res.Fields {
		b.fields[f.Key] = f
	}
	return model.Snapshot{
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
			// 模板参数的字段键是 tpl/<参数名>，写法见 params.FieldKey。
			ID: t.ID, Version: t.Version, Name: t.Name, Body: t.Body, Params: b.valuesOf("tpl/", t.Params),
		},
		Slots: b.slots(),
	}, nil
}

// builder 在参数求值通过之后整理快照：values 是求出的最终取值，fields 是表单的各格，都按字段键查。
type builder struct {
	in     params.Input
	values params.Values
	fields map[params.FieldKey]params.Field
}

// slots 按正文里出现的顺序列出插槽。重名的只按第一次出现的算，与参数求值一致：插槽重名由激活
// 判定报（slot_duplicate），草稿照样能预览。扫描错误也归激活判定报，写错的占位符原样留在正文里。
func (b builder) slots() []model.SnapshotSlot {
	body, _ := scan.Scan(b.in.Template.Body)
	out := make([]model.SnapshotSlot, 0, len(body.Slots))
	seen := make(map[string]bool, len(body.Slots))
	for _, s := range body.Slots {
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		insts := b.in.Template.Assembly[s.Name]
		slot := model.SnapshotSlot{
			Seq: len(out) + 1, Name: s.Name, Exec: s.Exec, Timing: s.Timing,
			Instances: make([]model.SnapshotInstance, 0, len(insts)),
		}
		for i, inst := range insts {
			// 实例参数的字段键是 <插槽名>/<序号>/<参数名>，序号从 1 起，写法见 params.FieldKey。
			slot.Instances = append(slot.Instances, b.instance(s.Name+"/"+strconv.Itoa(i+1)+"/", inst))
		}
		out = append(out, slot)
	}
	return out
}

// instance 整理一个实例，prefix 是它的字段键前缀。参数求值通过了，就说明引用的 hook 在 in.Hooks
// 里，也有覆盖 ISO 的系的实现。
func (b builder) instance(prefix string, inst model.Instance) model.SnapshotInstance {
	h := b.in.Hooks[model.HookKey{ID: inst.HookID, Version: inst.HookVersion}]
	impl, _ := h.ImplFor(b.in.ISO.Series)
	out := model.SnapshotInstance{
		HookID: inst.HookID, HookVersion: inst.HookVersion, HookName: h.Name,
		Kind: impl.Kind, Body: impl.Body,
		Params: b.valuesOf(prefix, h.Params),
	}
	// 解释器、超时、是否必需和摘要只对脚本有意义：片段在渲染时就写进了应答文件，没有执行这一步，
	// 网关也只该对脚本的摘要放行。种类不认识的同样不算脚本，与参数求值把它当片段一致。
	if impl.Kind == model.ImplScript {
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
	return out
}

// valuesOf 取一组参数的最终取值，键为参数名，没有值的可选参数不在其中。类型与是否列表取自表单里
// 的那一格：取值就是按它解析的，参数重名时进了表单的也只有第一个声明。
func (b builder) valuesOf(prefix string, decls []model.ParamDecl) map[string]model.SnapshotValue {
	out := make(map[string]model.SnapshotValue, len(decls))
	for _, d := range decls {
		key := params.FieldKey(prefix + d.Name)
		if v, ok := b.values[key]; ok {
			f := b.fields[key]
			out[d.Name] = model.SnapshotValue{Type: f.Type, List: f.List, Items: v.Items()}
		}
	}
	return out
}

// cloneStrings 复制一份映射；nil 也给出空映射。
func cloneStrings(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	maps.Copy(out, m)
	return out
}
