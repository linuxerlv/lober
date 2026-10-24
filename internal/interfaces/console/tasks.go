package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type taskPage struct {
	Items []taskItem `json:"items"`
	Next  string     `json:"next"`
	Total *int64     `json:"total,omitempty"`
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
	Page        int             `form:"page"`
	PageSize    int             `form:"page_size"`
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
	out := tasks.TaskChoice{Task: t}
	if s.deps.TaskCtl != nil {
		out.CanCancel = s.deps.TaskCtl.CanCancel(t)
		out.CanRetry = s.deps.TaskCtl.CanRetry(t)
	}
	return toTaskChoiceItem(out)
}
func (s *server) listTasks(c *gin.Context) {
	number, limit, ok := parseQueryPagination(c)
	if !ok {
		return
	}
	page, ok := Get(c, func() (tasks.TaskPage, error) {
		return s.deps.TaskReader.List(c.Request.Context(), actorOf(c), tasks.ListRequest{PageRequest: tasks.PageRequest{Page: number, Limit: limit, Cursor: c.Query("cursor")}, HostSN: c.Query("host_sn"), State: c.Query("state"), CreatedFrom: c.Query("created_from"), CreatedTo: c.Query("created_to")})
	})
	if !ok {
		return
	}
	items := make([]taskItem, len(page.Items))
	for i, t := range page.Items {
		items[i] = toTaskChoiceItem(t)
	}
	Ok(c, taskPage{items, page.Next, page.Total})
}
func (s *server) getTask(c *gin.Context) {
	t, ok := Get(c, func() (tasks.TaskChoice, error) {
		return s.deps.TaskReader.Get(c.Request.Context(), actorOf(c), c.Param("id"))
	})
	if ok {
		Ok(c, toTaskChoiceItem(t))
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
	return s.deps.Identity.StreamAuthorized(c.Request.Context(), token, PermTaskRead)
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
			err = ctl.Retry(c.Request.Context(), t, s.auditIntentFrom(c, ""))
		} else {
			err = ctl.Cancel(c.Request.Context(), t, s.auditIntentFrom(c, ""))
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
	result, ok := Get(c, func() (tasks.PreviewResult, error) {
		return s.deps.TaskReader.Preview(c.Request.Context(), actorOf(c), c.Param("id"), tasks.PreviewRequest{Firmware: req.Firmware})
	})
	if !ok || !Judge(c, result.Problems) {
		return
	}
	Ok(c, previewResponse{Files: previewFiles(result.Files), Problems: jsonProblems(result.Problems), Sample: result.Sample})
}
