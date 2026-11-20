package tasks

import (
	"context"
	"slices"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
)

func enteredOf(values map[string]string) map[params.FieldKey]string {
	out := make(map[params.FieldKey]string, len(values))
	for key, value := range values {
		out[params.FieldKey(key)] = value
	}
	return out
}
func normalizeEntered(sns []string, raw map[string]map[string]string) (map[string]map[string]string, error) {
	selected := make(map[string]bool, len(sns))
	for _, sn := range sns {
		selected[sn] = true
	}
	out := make(map[string]map[string]string, len(raw))
	for key, values := range raw {
		sn, err := model.NormalizeSN(key)
		_, duplicate := out[sn]
		if err != nil || !selected[sn] || duplicate {
			return nil, repository.ErrInvalid
		}
		out[sn] = values
	}
	return out, nil
}
func (w *wizard) Params(ctx context.Context, actor ports.Actor, req ParamsRequest) (ParamsResult, error) {
	if err := authorize(actor); err != nil {
		return ParamsResult{}, err
	}
	if !req.Selection.valid() || len(req.HostSNs) == 0 {
		return ParamsResult{}, repository.ErrInvalid
	}
	release := w.deps.SelectionGuard.ReadLock()
	defer release()
	t, iso, hooks, err := w.selectionFacts(ctx, req.Selection)
	if err != nil {
		return ParamsResult{}, err
	}
	sns, err := normalizeSNs(req.HostSNs)
	if err != nil {
		return ParamsResult{}, err
	}
	entered, err := normalizeEntered(sns, req.Entered)
	if err != nil {
		return ParamsResult{}, err
	}
	hosts, missing, err := w.hostRecords(ctx, sns)
	if err != nil {
		return ParamsResult{}, err
	}
	result, err := w.batch(ctx, t, iso, hooks, hosts, missing)
	if err != nil {
		return ParamsResult{}, err
	}
	absent := map[string]bool{}
	for _, sn := range missing {
		absent[sn] = true
	}
	out := make([]HostParams, len(hosts))
	for i, h := range hosts {
		fields := []params.Field{}
		var disk *TargetDisk
		ps := append([]model.Problem{}, result.ByHost[h.SN]...)
		if !absent[h.SN] && len(result.Common) == 0 {
			res := params.Evaluate(params.Input{Template: t, Hooks: hooks, ISO: iso, Host: h, Entered: enteredOf(entered[h.SN])})
			fields, disk = wizardDiskFields(t, iso, hooks, res)
			ps = append(ps, res.Problems...)
		}
		out[i] = HostParams{SN: h.SN, Fields: fields, TargetDisk: disk, Problems: ps}
	}
	return ParamsResult{Hosts: out, Common: result.Common}, nil
}

// 只有随程序发布的模板才能保证分区步骤与最终安装位置一致。
// 自定义模板仍可使用任何参数，向导不能替它推断整盘的用途。
func wizardDiskFields(t model.Template, iso model.ISO, hooks map[model.HookKey]model.Hook, res params.Result) ([]params.Field, *TargetDisk) {
	const slot = "partition"
	instances := t.Assembly[slot]
	if !t.Builtin || len(instances) != 1 {
		return res.Fields, nil
	}
	inst := instances[0]
	hook := hooks[model.HookKey{ID: inst.HookID, Version: inst.HookVersion}]
	if !hook.Builtin || hook.ID != "builtin-partition" {
		return res.Fields, nil
	}
	name, mode := "disk", ""
	switch iso.Series.Format() {
	case catalog.FormatKickstart:
		mode = "device-or-path"
	case catalog.FormatPreseed, catalog.FormatAutoinstall:
		mode = "device"
	case catalog.FormatESXiKS:
		mode = "first-disk-rule"
	case catalog.FormatUnattend:
		if t.ID != "builtin-unattend" {
			return res.Fields, nil
		}
		name, mode = "disk_index", "windows-index"
	default:
		return res.Fields, nil
	}
	key := params.FieldKey(model.InstanceFieldKey(slot, 1, name))
	pos := slices.IndexFunc(res.Fields, func(f params.Field) bool { return f.Key == key })
	if pos < 0 {
		return res.Fields, nil
	}
	fields := res.Fields
	if mode == "windows-index" {
		// 内置应答的 InstallTo 固定为磁盘 0；分区脚本必须锁定同一编号。
		if f := fields[pos]; !f.Locked || f.Prefill != "0" {
			return fields, nil
		}
		unused := params.FieldKey(model.InstanceFieldKey(slot, 1, "disk"))
		fields = slices.DeleteFunc(fields, func(f params.Field) bool { return f.Key == unused })
	}
	return fields, &TargetDisk{Key: key, Mode: mode, Value: res.Values[key].Canonical()}
}
