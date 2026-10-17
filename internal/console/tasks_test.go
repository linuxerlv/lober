package console

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

func taskFixture(t *testing.T) (*Server, *pgstore.Store, string, model.Task, *atomic.Int64) {
	t.Helper()
	s, st, clock := newHTTPServer(t)
	_, _, token, iso, tpl := seedWizardFixture(t, s, st)
	iso.Artifacts = map[string]string{"kernel": "boot/vmlinuz", "initrd": "boot/initrd", "repo": "."}
	for _, role := range catalog.RequiredRoles(iso.Series) {
		iso.Artifacts[role] = "media/" + role
	}
	iso, err := st.UpdateISO(t.Context(), iso)
	if err != nil {
		t.Fatal(err)
	}
	result := responseData[wizardSubmitResult](t, httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1"},{"sn":"SN-2"}]`), token))
	task, err := st.GetTask(t.Context(), result.Tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, st, token, task, clock
}

func Test任务列表详情事件及查询边界(t *testing.T) {
	s, st, token, task, _ := taskFixture(t)
	page := responseData[taskPage](t, httpCall(s, "GET", "/console/v1/tasks?limit=1", "", token))
	if len(page.Items) != 1 || page.Next == "" {
		t.Fatal(page)
	}
	next := responseData[taskPage](t, httpCall(s, "GET", "/console/v1/tasks?limit=1&cursor="+page.Next, "", token))
	if len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatal(next)
	}
	page = responseData[taskPage](t, httpCall(s, "GET", "/console/v1/tasks?host_sn=sn-1&state=pending", "", token))
	if len(page.Items) != 1 || page.Items[0].ID != task.ID {
		t.Fatal(page)
	}
	detail := responseData[taskItem](t, httpCall(s, "GET", "/console/v1/tasks/"+task.ID, "", token))
	if detail.Snapshot.Template.Params["hostname"].Items[0] != "node" || detail.CanCancel == nil || detail.CanRetry == nil {
		t.Fatal(detail)
	}
	_, err := st.AppendEvents(t.Context(), task.ID, []store.NewEvent{{At: fixedAt, Kind: "slot.start", Key: "one"}, {At: fixedAt, Kind: "slot.exit", Key: "two", Fields: map[string]string{"code": "0"}}})
	if err != nil {
		t.Fatal(err)
	}
	events := responseData[eventPage](t, httpCall(s, "GET", "/console/v1/tasks/"+task.ID+"/events?after_seq=1", "", token))
	if len(events.Items) != 1 || events.Items[0].Seq != 2 || events.Items[0].Fields["code"] != "0" {
		t.Fatal(events)
	}
	for _, path := range []string{"/tasks?limit=bad", "/tasks?state=made-up", "/tasks?created_from=1x", "/tasks?created_to=-1", "/tasks?created_from=3&created_to=2", "/tasks/" + task.ID + "/events?after_seq=bad", "/tasks/" + task.ID + "/events?after_seq=-1", "/tasks/" + task.ID + "/stream?after_seq=bad"} {
		w := httpCall(s, "GET", "/console/v1"+path, "", token)
		if w.Code != 400 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/tasks/missing", "/tasks/missing/events", "/tasks/missing/stream"} {
		w := httpCall(s, "GET", "/console/v1"+path, "", token)
		if w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
}

type taskControlFixture struct {
	st     store.Tasks
	calls  []store.Audit
	reject error
	block  []model.Problem
}

func (f *taskControlFixture) Cancel(ctx context.Context, task model.Task, a store.Audit) error {
	f.calls = append(f.calls, a)
	if f.reject != nil {
		return f.reject
	}
	_, _, err := f.st.TransitionTaskByUser(ctx, task.ID, store.TaskExpect{State: task.State, Rev: task.Rev}, model.TaskCancelled, nil, a)
	return err
}
func (f *taskControlFixture) Retry(ctx context.Context, task model.Task, a store.Audit) error {
	f.calls = append(f.calls, a)
	if f.reject != nil {
		return f.reject
	}
	_, _, err := f.st.TransitionTaskByUser(ctx, task.ID, store.TaskExpect{State: task.State, Rev: task.Rev}, model.TaskPending, nil, a)
	return err
}
func (f *taskControlFixture) CanCancel(model.Task) []model.Problem { return f.block }
func (f *taskControlFixture) CanRetry(model.Task) []model.Problem  { return f.block }

func Test任务控制委托审计及错误隔离(t *testing.T) {
	s, st, token, task, _ := taskFixture(t)
	ctl := &taskControlFixture{st: st}
	s.server.deps.TaskCtl = ctl
	before, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	ch, unsub := s.server.hub.Subscribe(task.ID)
	defer unsub()
	for _, step := range []struct {
		action string
		state  model.TaskState
	}{{"cancel", model.TaskCancelled}, {"retry", model.TaskPending}} {
		back := responseData[taskItem](t, httpCall(s, "POST", "/console/v1/tasks/"+task.ID+"/"+step.action, "", token))
		if back.State != step.state {
			t.Fatal(back)
		}
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("committed task notification missing")
		}
	}
	if len(ctl.calls) != 2 || ctl.calls[0].Actor != "admin" || ctl.calls[0].Action != "task.cancel" || ctl.calls[1].Action != "task.retry" || ctl.calls[1].At != fixedAt {
		t.Fatal(ctl.calls)
	}
	after, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil || len(after.Items) != len(before.Items)+2 {
		t.Fatal(after, err)
	}
	for _, test := range []struct {
		err    error
		status int
	}{{store.ErrConflict, 409}, {Problems([]model.Problem{model.NewProblem("host_has_open_task", model.Subject{Kind: model.KindHost, ID: "SN-1"})}), 422}, {errors.New("private backend credential"), 500}, {ErrTaskControlUnavailable, 422}} {
		ctl.reject = test.err
		w := httpCall(s, "POST", "/console/v1/tasks/"+task.ID+"/cancel", "", token)
		if w.Code != test.status || strings.Contains(w.Body.String(), "private backend credential") {
			t.Fatal(w.Code, w.Body.String())
		}
		select {
		case <-ch:
			t.Fatal("failed control published")
		default:
		}
	}
	final, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if len(final.Items) != len(after.Items) {
		t.Fatal("failed control audited")
	}
	ctl.block = []model.Problem{model.NewProblem("host_has_open_task", model.Subject{Kind: model.KindHost, ID: "SN-1"})}
	detail := responseData[taskItem](t, httpCall(s, "GET", "/console/v1/tasks/"+task.ID, "", token))
	if len(detail.CanRetry) != 1 || detail.CanRetry[0].Code != "host_has_open_task" {
		t.Fatal(detail)
	}
}

func Test任务预览只使用固化快照及引用删除保护(t *testing.T) {
	s, st, token, task, _ := taskFixture(t)
	path := "/console/v1/tasks/" + task.ID + "/preview"
	first := responseData[previewResponse](t, httpCall(s, "POST", path, `{"firmware":"uefi"}`, token))
	for _, file := range first.Files {
		if !file.Sample {
			t.Fatal("preview file omitted sample-credential marker", file.Name)
		}
	}
	host, err := st.GetHost(t.Context(), "SN-1")
	if err != nil {
		t.Fatal(err)
	}
	host.Attrs = map[string]string{"rack": "changed"}
	host.Firmware = "bios"
	if host, err = st.UpdateHost(t.Context(), host, store.Audit{Actor: "seed", Action: "host.update", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err = st.DeprecateTemplate(t.Context(), model.TemplateKey{ID: task.TemplateID, Version: task.TemplateVersion}, 3, store.Audit{Actor: "seed", Action: "template.deprecate", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	second := responseData[previewResponse](t, httpCall(s, "POST", path, fmt.Sprintf(`{"firmware":"uefi","hostSN":"MISSING","isoID":"MISSING","entered":{"tpl/hostname":"spoof"}}`), token))
	if !reflect.DeepEqual(first, second) || len(first.Files) < 2 || !strings.Contains(first.Files[1].Body, "node") {
		t.Fatal(first, second)
	}
	after, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	back, _ := st.GetTask(t.Context(), task.ID)
	if len(before.Items) != len(after.Items) || back.Rev != task.Rev || !reflect.DeepEqual(back.Snapshot, task.Snapshot) {
		t.Fatal("preview changed state")
	}
	w := httpCall(s, "DELETE", fmt.Sprintf("/console/v1/hosts/SN-1?rev=%d", host.Rev), "", token)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	iso, err := st.GetISO(t.Context(), task.ISOID)
	if err != nil {
		t.Fatal(err)
	}
	s.server.deps.ISODeletion = &mockISODeletion{st: st}
	w = httpCall(s, "DELETE", fmt.Sprintf("/console/v1/isos/%s?rev=%d", iso.ID, iso.Rev), "", token)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httpCall(s, "POST", path, `{"firmware":"bad"}`, token)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	closed := pgtest.OpenStore(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	s.server.deps.Hosts, s.server.deps.ISOs, s.server.deps.Hooks, s.server.deps.Templates = closed, closed, closed, closed
	frozen := responseData[previewResponse](t, httpCall(s, "POST", path, `{"firmware":"uefi"}`, token))
	if !reflect.DeepEqual(first, frozen) {
		t.Fatal("preview depended on live records", frozen)
	}
}

type streamRecord struct {
	id    string
	event eventItem
	err   error
}

func readTaskStream(r io.Reader) <-chan streamRecord {
	out := make(chan streamRecord, 512)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		var id string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "id:") {
				id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			}
			if strings.HasPrefix(line, "data:") {
				var e eventItem
				err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &e)
				out <- streamRecord{id: id, event: e, err: err}
			}
		}
		if err := scanner.Err(); err != nil {
			out <- streamRecord{err: err}
		}
	}()
	return out
}
func awaitStreamRecord(t *testing.T, ch <-chan streamRecord, timeout time.Duration) streamRecord {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok || e.err != nil {
			t.Fatal("stream closed", e.err)
		}
		return e
	case <-time.After(timeout):
		t.Fatal("stream read timed out")
		return streamRecord{}
	}
}
func Test任务SSE补读分页丢通知续读及断开退订(t *testing.T) {
	s, st, token, task, _ := taskFixture(t)
	batch := make([]store.NewEvent, 205)
	for i := range batch {
		batch[i] = store.NewEvent{At: fixedAt, Kind: "stream.test", Key: fmt.Sprintf("event-%d", i)}
	}
	if _, err := st.AppendEvents(t.Context(), task.ID, batch); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Engine)
	defer srv.Close()
	req, _ := http.NewRequestWithContext(t.Context(), "GET", srv.URL+"/console/v1/tasks/"+task.ID+"/stream?access_token="+token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(resp.StatusCode)
	}
	ch := readTaskStream(resp.Body)
	for i := 1; i <= 205; i++ {
		e := awaitStreamRecord(t, ch, 2*time.Second)
		if e.event.Seq != int64(i) || e.id != fmt.Sprint(i) || e.event.Fields == nil {
			t.Fatal(e)
		}
	}
	if _, err := st.AppendEvents(t.Context(), task.ID, []store.NewEvent{{At: fixedAt, Kind: "stream.hint", Key: "hint"}}); err != nil {
		t.Fatal(err)
	}
	s.server.hub.Publish(task.ID)
	e := awaitStreamRecord(t, ch, 2*time.Second)
	if e.event.Seq != 206 {
		t.Fatal(e)
	}
	if _, err := st.AppendEvents(t.Context(), task.ID, []store.NewEvent{{At: fixedAt, Kind: "stream.lost", Key: "lost"}}); err != nil {
		t.Fatal(err)
	}
	e = awaitStreamRecord(t, ch, 7*time.Second)
	if e.event.Seq != 207 {
		t.Fatal(e)
	}
	resp.Body.Close()
	req, _ = http.NewRequestWithContext(t.Context(), "GET", srv.URL+"/console/v1/tasks/"+task.ID+"/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Last-Event-ID", "206")
	resumed, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Body.Close()
	resumedCh := readTaskStream(resumed.Body)
	e = awaitStreamRecord(t, resumedCh, 2*time.Second)
	if e.event.Seq != 207 {
		t.Fatal(e)
	}
	resumed.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	h := s.server.hub.(*hub)
	for {
		h.mu.Lock()
		n := len(h.subs[task.ID])
		h.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscriber leaked", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func Test任务接口五角色权限(t *testing.T) {
	s, st, _, _, _ := taskFixture(t)
	for i, role := range []store.Role{store.RoleAdmin, store.RoleOperator, store.RoleHookAuthor, store.RoleHookApprover, store.RoleViewer} {
		actor := fmt.Sprintf("taskrole%d", i)
		seedAuthUser(t, st, actor, role, "pw")
		actor = loginHTTP(t, s, actor, "pw").Token
		count := 0
		for _, r := range s.Routes() {
			if r.Tag != "tasks" {
				continue
			}
			count++
			path := strings.ReplaceAll(r.Path, ":id", "missing")
			w := httpCall(s, r.Method, path, `{}`, actor)
			allowed := r.Perm == PermTaskRead || role == store.RoleAdmin || role == store.RoleOperator
			if allowed && (w.Code == 401 || w.Code == 403) || !allowed && w.Code != 403 {
				t.Fatal(role, r, w.Code)
			}
		}
		if count != 7 {
			t.Fatal(count)
		}
	}
}

func Test任务SSE连接期间复查禁用改密及会话过期(t *testing.T) {
	s, st, token, task, clock := taskFixture(t)
	seedHTTPUser(t, s, st, "backup", store.RoleAdmin)
	srv := httptest.NewServer(s.Engine)
	defer srv.Close()
	for _, reason := range []string{"disabled", "password", "expired"} {
		t.Run(reason, func(t *testing.T) {
			// Resume beyond the tail, so only a newly appended event could leak.
			req, _ := http.NewRequestWithContext(t.Context(), "GET", srv.URL+"/console/v1/tasks/"+task.ID+"/stream?after_seq=100&access_token="+token, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatal(resp.StatusCode)
			}
			ch := readTaskStream(resp.Body)
			u, err := st.GetUser(t.Context(), "admin")
			if err != nil {
				t.Fatal(err)
			}
			if reason == "expired" {
				clock.Add(12 * 3600)
			} else {
				u.Disabled = reason == "disabled"
				u.MustChangePassword = reason == "password"
				if _, err := st.UpdateUser(t.Context(), u, store.Audit{Actor: "seed", Action: "user.update", At: fixedAt}); err != nil {
					t.Fatal(err)
				}
			}
			s.server.hub.Publish(task.ID)
			select {
			case got, ok := <-ch:
				if ok {
					t.Fatal("unauthorized stream remained open", got)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("authorization change did not close stream")
			}
			if reason != "expired" {
				u, err = st.GetUser(t.Context(), "admin")
				if err != nil {
					t.Fatal(err)
				}
				u.Disabled = false
				u.MustChangePassword = false
				if _, err = st.UpdateUser(t.Context(), u, store.Audit{Actor: "seed", Action: "user.update", At: fixedAt}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func Test任务SSE断线续读OpenAPI请求头(t *testing.T) {
	s := New(Deps{Clock: func() int64 { return fixedAt }})
	op := s.OpenAPI()["paths"].(map[string]any)["/console/v1/tasks/{id}/stream"].(map[string]any)["get"].(map[string]any)
	found := false
	for _, raw := range op["parameters"].([]any) {
		p := raw.(map[string]any)
		if p["in"] == "header" && p["name"] == "Last-Event-ID" {
			found = true
		}
	}
	if !found {
		t.Fatal("Last-Event-ID header absent", op["parameters"])
	}
}
