package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type taskPage struct {
	Items []taskItem `json:"items"`
	Next  string     `json:"next"`
}
type eventItem struct {
	TaskID string            `json:"taskID"`
	Seq    int64             `json:"seq"`
	At     int64             `json:"at"`
	Kind   string            `json:"kind"`
	Key    string            `json:"key"`
	Fields map[string]string `json:"fields"`
}
type eventPage struct {
	Items []eventItem `json:"items"`
}
type taskQuery struct {
	HostSN      string          `form:"host_sn"`
	State       model.TaskState `form:"state"`
	CreatedFrom int64           `form:"created_from"`
	CreatedTo   int64           `form:"created_to"`
	Cursor      string          `form:"cursor"`
	Limit       int             `form:"limit"`
}
type eventQuery struct {
	AfterSeq int64 `form:"after_seq"`
	Limit    int   `form:"limit"`
}
type streamQuery struct {
	AfterSeq int64 `form:"after_seq"`
}
type streamHeaders struct {
	LastEventID int64 `form:"Last-Event-ID"`
}
type taskPreviewRequest struct {
	Firmware catalog.Firmware `json:"firmware,omitempty"`
}

func (s *server) taskRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/tasks", perm: PermTaskRead, tag: "tasks", summary: "List tasks and frozen snapshots", query: taskQuery{}, resp: taskPage{}, handle: s.listTasks},
		{method: "GET", path: "/console/v1/tasks/:id", perm: PermTaskRead, tag: "tasks", summary: "Read frozen task and control diagnostics", resp: taskItem{}, handle: s.getTask},
		{method: "GET", path: "/console/v1/tasks/:id/events", perm: PermTaskRead, tag: "tasks", summary: "Read committed events after a sequence", query: eventQuery{}, resp: eventPage{}, handle: s.listEvents},
		{method: "GET", path: "/console/v1/tasks/:id/stream", perm: PermTaskRead, tag: "tasks", summary: "Stream committed events with resume and polling recovery", query: streamQuery{}, headers: streamHeaders{}, stream: true, handle: s.taskStream},
		{method: "POST", path: "/console/v1/tasks/:id/cancel", perm: PermTaskCancel, audit: "task.cancel", tag: "tasks", summary: "Delegate cancellation to task state machine", resp: taskItem{}, handle: s.cancelTask},
		{method: "POST", path: "/console/v1/tasks/:id/retry", perm: PermTaskRetry, audit: "task.retry", tag: "tasks", summary: "Delegate retry to task state machine", resp: taskItem{}, handle: s.retryTask},
		{method: "POST", path: "/console/v1/tasks/:id/preview", perm: PermTaskRead, tag: "tasks", summary: "Render frozen task snapshot with sample credentials", req: taskPreviewRequest{}, resp: previewResponse{}, handle: s.previewTask},
	}
}
func queryNonnegative(c *gin.Context, key string) (int64, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return 0, false
	}
	return value, true
}
func (s *server) taskResponse(t model.Task) taskItem {
	out := toTaskItem(t)
	if s.deps.TaskCtl != nil {
		out.CanCancel = jsonProblems(s.deps.TaskCtl.CanCancel(t))
		out.CanRetry = jsonProblems(s.deps.TaskCtl.CanRetry(t))
	}
	return out
}
func (s *server) listTasks(c *gin.Context) {
	limit, ok := queryLimit(c)
	if !ok {
		return
	}
	from, ok := queryNonnegative(c, "created_from")
	if !ok {
		return
	}
	to, ok := queryNonnegative(c, "created_to")
	if !ok {
		return
	}
	if to != 0 && from > to {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	q := repository.TaskQuery{HostSN: c.Query("host_sn"), CreatedFrom: from, CreatedTo: to, Cursor: c.Query("cursor"), Limit: limit}
	if q.HostSN != "" {
		sn, err := model.NormalizeSN(q.HostSN)
		if err != nil {
			RespondError(c, err)
			return
		}
		q.HostSN = sn
	}
	if raw := c.Query("state"); raw != "" {
		state := model.TaskState(raw)
		if !state.Valid() {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
		q.States = []model.TaskState{state}
	}
	page, ok := Get(c, func() (repository.Page[model.Task], error) { return s.deps.Tasks.ListTasks(c.Request.Context(), q) })
	if !ok {
		return
	}
	items := make([]taskItem, len(page.Items))
	for i, t := range page.Items {
		items[i] = s.taskResponse(t)
	}
	Ok(c, taskPage{items, page.Next})
}
func (s *server) getTask(c *gin.Context) {
	t, ok := Get(c, func() (model.Task, error) { return s.deps.Tasks.GetTask(c.Request.Context(), c.Param("id")) })
	if ok {
		Ok(c, s.taskResponse(t))
	}
}
func toEventItem(e repository.Event) eventItem {
	return eventItem{e.TaskID, e.Seq, e.At, e.Kind, e.Key, stringMap(e.Fields)}
}
func (s *server) listEvents(c *gin.Context) {
	after, ok := queryNonnegative(c, "after_seq")
	if !ok {
		return
	}
	limit, ok := queryLimit(c)
	if !ok {
		return
	}
	events, ok := Get(c, func() ([]repository.Event, error) {
		return s.deps.Events.ListEvents(c.Request.Context(), c.Param("id"), after, limit)
	})
	if !ok {
		return
	}
	items := make([]eventItem, len(events))
	for i, e := range events {
		items[i] = toEventItem(e)
	}
	Ok(c, eventPage{items})
}
func (s *server) streamAuthorized(c *gin.Context, token string) bool {
	username, ok := s.sessions.lookup(token)
	if !ok {
		return false
	}
	u, err := s.deps.Users.GetUser(c.Request.Context(), username)
	return err == nil && !u.Disabled && !u.MustChangePassword && u.Role.Valid() && slices.Contains(PermsOf(u.Role), PermTaskRead)
}
func (s *server) taskStream(c *gin.Context) {
	last, ok := queryNonnegative(c, "after_seq")
	if !ok {
		return
	}
	if raw := c.GetHeader("Last-Event-ID"); raw != "" {
		seq, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || seq < 0 {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
		last = seq
	}
	id := c.Param("id")
	if _, ok := Get(c, func() (model.Task, error) { return s.deps.Tasks.GetTask(c.Request.Context(), id) }); !ok {
		return
	}
	hints, unsub := s.hub.Subscribe(id)
	defer unsub()
	events, ok := Get(c, func() ([]repository.Event, error) {
		return s.deps.Events.ListEvents(c.Request.Context(), id, last, 200)
	})
	if !ok {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	token := bearerToken(c)
	send := func(page []repository.Event) bool {
		for {
			if c.Request.Context().Err() != nil || !s.streamAuthorized(c, token) {
				return false
			}
			for _, e := range page {
				data, err := json.Marshal(toEventItem(e))
				if err != nil {
					return false
				}
				if _, err = fmt.Fprintf(c.Writer, "id: %d\nevent: task\ndata: %s\n\n", e.Seq, data); err != nil {
					return false
				}
				last = e.Seq
			}
			c.Writer.Flush()
			if len(page) < 200 {
				return true
			}
			var err error
			page, err = s.deps.Events.ListEvents(c.Request.Context(), id, last, 200)
			if err != nil {
				return false
			}
		}
	}
	if !send(events) {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case _, open := <-hints:
			if !open {
				return
			}
		case <-ticker.C:
		case <-c.Request.Context().Done():
			return
		}
		events, err := s.deps.Events.ListEvents(c.Request.Context(), id, last, 200)
		if err != nil || !send(events) {
			return
		}
	}
}
func (s *server) controlTask(c *gin.Context, retry bool) {
	t, ok := Get(c, func() (model.Task, error) { return s.deps.Tasks.GetTask(c.Request.Context(), c.Param("id")) })
	if !ok {
		return
	}
	err := ErrTaskControlUnavailable
	if ctl := s.deps.TaskCtl; ctl != nil {
		if retry {
			err = ctl.Retry(c.Request.Context(), t, s.auditFrom(c, ""))
		} else {
			err = ctl.Cancel(c.Request.Context(), t, s.auditFrom(c, ""))
		}
	}
	if err != nil {
		if errors.Is(err, ErrTaskControlUnavailable) {
			Fail(c, 422, nil, model.ConsoleTaskControlUnavailable)
		} else {
			RespondError(c, err)
		}
		return
	}
	back, ok := Get(c, func() (model.Task, error) { return s.deps.Tasks.GetTask(c.Request.Context(), t.ID) })
	if !ok {
		return
	}
	s.hub.Publish(back.ID)
	Ok(c, s.taskResponse(back))
}
func (s *server) cancelTask(c *gin.Context) { s.controlTask(c, false) }
func (s *server) retryTask(c *gin.Context)  { s.controlTask(c, true) }
func (s *server) previewTask(c *gin.Context) {
	var req taskPreviewRequest
	if !Bind(c, &req) {
		return
	}
	if req.Firmware != "" && !req.Firmware.Valid() {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	t, ok := Get(c, func() (model.Task, error) { return s.deps.Tasks.GetTask(c.Request.Context(), c.Param("id")) })
	if !ok {
		return
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
	files, sample, ps, err := s.renderPreview(c.Request.Context(), t.Snapshot, firmware, t.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !Judge(c, ps) {
		return
	}
	sample = append(sample, labels...)
	for i := range files {
		files[i].Sample = len(sample) > 0
	}
	Ok(c, previewResponse{Files: files, Problems: jsonProblems(ps), Sample: sample})
}
