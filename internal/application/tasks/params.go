package tasks

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
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
		ps := append([]model.Problem{}, result.ByHost[h.SN]...)
		if !absent[h.SN] && len(result.Common) == 0 {
			res := params.Evaluate(params.Input{Template: t, Hooks: hooks, ISO: iso, Host: h, Entered: enteredOf(entered[h.SN])})
			fields = res.Fields
			ps = append(ps, res.Problems...)
		}
		out[i] = HostParams{SN: h.SN, Fields: fields, Problems: ps}
	}
	return ParamsResult{Hosts: out, Common: result.Common}, nil
}
