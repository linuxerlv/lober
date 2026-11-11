package tasks

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/snapshot"
)

func (w *wizard) Submit(ctx context.Context, actor ports.Actor, req SubmitRequest) ([]TaskChoice, error) {
	if err := authorize(actor); err != nil {
		return nil, err
	}
	if !req.Selection.valid() || len(req.Hosts) == 0 {
		return nil, repository.ErrInvalid
	}
	release := w.deps.SelectionGuard.ReadLock()
	defer release()
	t, iso, hooks, err := w.selectionFacts(ctx, req.Selection)
	if err != nil {
		return nil, err
	}
	raw := make([]string, len(req.Hosts))
	for i, h := range req.Hosts {
		raw[i] = h.SN
	}
	sns, err := normalizeSNs(raw)
	if err != nil {
		return nil, err
	}
	hosts, missing, err := w.hostRecords(ctx, sns)
	if err != nil {
		return nil, err
	}
	result, err := w.batch(ctx, t, iso, hooks, hosts, missing)
	if err != nil {
		return nil, err
	}
	absent := map[string]bool{}
	for _, sn := range missing {
		absent[sn] = true
	}
	tasks := make([]model.Task, 0, len(hosts))
	seen := map[string]bool{}
	for i, h := range hosts {
		if seen[h.SN] || absent[h.SN] || len(result.Common) > 0 {
			continue
		}
		seen[h.SN] = true
		frozen, ps := snapshot.Build(params.Input{Template: t, Hooks: hooks, ISO: iso, Host: h, Entered: enteredOf(req.Hosts[i].Entered)})
		result.ByHost[h.SN] = append(result.ByHost[h.SN], ps...)
		if len(ps) == 0 {
			tasks = append(tasks, model.Task{HostSN: h.SN, ISOID: iso.ID, TemplateID: t.ID, TemplateVersion: t.Version, Snapshot: frozen, CreatedBy: actor.Username, CreatedAt: w.deps.Clock()})
		}
	}
	if err := batchError(result); err != nil {
		return nil, err
	}
	created, err := w.deps.Tasks.CreateCurrentTasks(ctx, tasks, repository.Audit{Actor: actor.Username, Action: "task.create", At: w.deps.Clock()})
	if err != nil {
		return nil, err
	}
	out := make([]TaskChoice, len(created))
	for i, task := range created {
		out[i] = TaskChoice{Task: task, CanCancel: []model.Problem{}, CanRetry: []model.Problem{}}
		if w.deps.Diagnostics != nil {
			out[i].CanCancel = w.deps.Diagnostics.CanCancel(task)
			out[i].CanRetry = w.deps.Diagnostics.CanRetry(task)
		}
		w.deps.Notifier.Publish(task.ID)
	}
	return out, nil
}
