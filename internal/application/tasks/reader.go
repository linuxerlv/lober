package tasks

import (
	"context"
	"strconv"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks/port"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type reader struct{ deps ReaderDeps }

// NewReader 只使用显式注入的读口；诊断缺省时保持原空结果。
func NewReader(d ReaderDeps) Reader {
	if d.Tasks == nil || d.Renderer == nil {
		panic("任务读取缺少必要能力")
	}
	return &reader{deps: d}
}
func readTime(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, ErrBadRequest
	}
	return value, nil
}
func taskChoice(t model.Task, diagnostics port.TaskDiagnostics) TaskChoice {
	out := TaskChoice{Task: t, CanCancel: []model.Problem{}, CanRetry: []model.Problem{}}
	if diagnostics != nil {
		out.CanCancel = diagnostics.CanCancel(t)
		out.CanRetry = diagnostics.CanRetry(t)
	}
	return out
}
func (r *reader) List(ctx context.Context, actor ports.Actor, req ListRequest) (TaskPage, error) {
	if err := actor.Authorize(access.PermTaskRead); err != nil {
		return TaskPage{}, err
	}
	if _, err := repository.CheckPageQuery(req.Cursor, req.Limit, req.Page); err != nil {
		return TaskPage{}, err
	}
	from, err := readTime(req.CreatedFrom)
	if err != nil {
		return TaskPage{}, err
	}
	to, err := readTime(req.CreatedTo)
	if err != nil {
		return TaskPage{}, err
	}
	if to != 0 && from > to {
		return TaskPage{}, ErrBadRequest
	}
	q := repository.TaskQuery{HostSN: req.HostSN, CreatedFrom: from, CreatedTo: to, Cursor: req.Cursor, Limit: req.Limit, Page: req.Page}
	if q.HostSN != "" {
		sn, err := model.NormalizeSN(q.HostSN)
		if err != nil {
			return TaskPage{}, err
		}
		q.HostSN = sn
	}
	if req.State != "" {
		state := model.TaskState(req.State)
		if !state.Valid() {
			return TaskPage{}, ErrBadRequest
		}
		q.States = []model.TaskState{state}
	}
	page, err := r.deps.Tasks.ListTasks(ctx, q)
	if err != nil {
		return TaskPage{}, err
	}
	items := make([]TaskChoice, len(page.Items))
	for i, t := range page.Items {
		items[i] = taskChoice(t, r.deps.Diagnostics)
	}
	return TaskPage{Items: items, Next: page.Next, Total: page.Total}, nil
}
func (r *reader) Get(ctx context.Context, actor ports.Actor, id string) (TaskChoice, error) {
	if err := actor.Authorize(access.PermTaskRead); err != nil {
		return TaskChoice{}, err
	}
	t, err := r.deps.Tasks.GetTask(ctx, id)
	if err != nil {
		return TaskChoice{}, err
	}
	return taskChoice(t, r.deps.Diagnostics), nil
}
func (r *reader) Preview(ctx context.Context, actor ports.Actor, id string, req PreviewRequest) (PreviewResult, error) {
	if err := actor.Authorize(access.PermTaskRead); err != nil {
		return PreviewResult{}, err
	}
	if req.Firmware != "" && !req.Firmware.Valid() {
		return PreviewResult{}, ErrBadRequest
	}
	t, err := r.deps.Tasks.GetTask(ctx, id)
	if err != nil {
		return PreviewResult{}, err
	}
	firmware := req.Firmware
	var labels []string
	if firmware == "" {
		firmware = t.Snapshot.Host.Firmware
		if firmware == "" {
			firmware = catalog.FirmwareUEFI
			labels = append(labels, "firmware")
		}
	}
	files, sample, ps, err := r.deps.Renderer.RenderFrozen(ctx, t.Snapshot, firmware, t.ID)
	if err != nil {
		return PreviewResult{}, err
	}
	// 原网页先判问题；有问题时不追加样例标签或修改文件标记。
	if len(ps) == 0 {
		sample = append(sample, labels...)
		for i := range files {
			files[i].Sample = len(sample) > 0
		}
	}
	return PreviewResult{Files: files, Sample: sample, Problems: ps}, nil
}
