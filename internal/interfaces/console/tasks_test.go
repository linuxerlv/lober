package console

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/snapshot"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
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
	_, err := st.AppendEvents(t.Context(), task.ID, []repository.NewEvent{{At: fixedAt, Kind: "slot.start", Key: "one"}, {At: fixedAt, Kind: "slot.exit", Key: "two", Fields: map[string]string{"code": "0"}}})
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
	st     repository.Tasks
	calls  []ports.AuditIntent
	reject error
	block  []model.Problem
}

// 通过通知契约观察订阅生命周期，不读取搬迁后的通知实现私有字段。
type streamSubscriptionObserver struct {
	Notifier
	active  atomic.Int64
	stopped atomic.Int64
}

func (o *streamSubscriptionObserver) Subscribe(id string) (<-chan struct{}, func()) {
	ch, stop := o.Notifier.Subscribe(id)
	o.active.Add(1)
	var once sync.Once
	return ch, func() { once.Do(func() { stop(); o.stopped.Add(1); o.active.Add(-1) }) }
}

func (f *taskControlFixture) Cancel(ctx context.Context, task model.Task, a ports.AuditIntent) error {
	f.calls = append(f.calls, a)
	if f.reject != nil {
		return f.reject
	}
	_, _, err := f.st.TransitionTaskByUser(ctx, task.ID, repository.TaskExpect{State: task.State, Rev: task.Rev}, model.TaskCancelled, nil, repository.Audit{Actor: a.Actor, Action: a.Action, Detail: a.Detail, At: a.At})
	return err
}
func (f *taskControlFixture) Retry(ctx context.Context, task model.Task, a ports.AuditIntent) error {
	f.calls = append(f.calls, a)
	if f.reject != nil {
		return f.reject
	}
	_, _, err := f.st.TransitionTaskByUser(ctx, task.ID, repository.TaskExpect{State: task.State, Rev: task.Rev}, model.TaskPending, nil, repository.Audit{Actor: a.Actor, Action: a.Action, Detail: a.Detail, At: a.At})
	return err
}
func (f *taskControlFixture) CanCancel(model.Task) []model.Problem { return f.block }
func (f *taskControlFixture) CanRetry(model.Task) []model.Problem  { return f.block }

func Test任务控制委托审计及错误隔离(t *testing.T) {
	s, st, token, task, _ := taskFixture(t)
	ctl := &taskControlFixture{st: st}
	setTaskControl(s, ctl)
	before, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
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
	after, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil || len(after.Items) != len(before.Items)+2 {
		t.Fatal(after, err)
	}
	for _, test := range []struct {
		err    error
		status int
	}{{repository.ErrConflict, 409}, {&repository.ProblemsError{Kind: repository.ErrRejected, Problems: []model.Problem{model.NewProblem("host_has_open_task", model.Subject{Kind: model.KindHost, ID: "SN-1"})}}, 422}, {errors.New("private backend credential"), 500}, {tasks.ErrControlUnavailable, 422}} {
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
	final, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
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
	if host, err = st.UpdateHost(t.Context(), host, repository.Audit{Actor: "seed", Action: "host.update", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err = st.DeprecateTemplate(t.Context(), model.TemplateKey{ID: task.TemplateID, Version: task.TemplateVersion}, 3, repository.Audit{Actor: "seed", Action: "template.deprecate", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	second := responseData[previewResponse](t, httpCall(s, "POST", path, fmt.Sprintf(`{"firmware":"uefi","hostSN":"MISSING","isoID":"MISSING","entered":{"tpl/hostname":"spoof"}}`), token))
	if !reflect.DeepEqual(first, second) || len(first.Files) < 2 || !strings.Contains(first.Files[1].Body, "node") {
		t.Fatal(first, second)
	}
	after, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
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
	setMedia(s, st, st, &mockISOSource{calls: map[string][]sourceCall{}}, &mockISODeletion{st: st})
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
	setMedia(s, closed, closed, &mockISOSource{calls: map[string][]sourceCall{}}, &mockISODeletion{st: closed})
	// 所有编写读口也注入关闭的仓储；任务预览仍应只读取已保存快照。
	assembled := wizardDeps(s.server.deps, closed)
	s.server.deps.HostRegistry = assembled.HostRegistry
	s.server.deps.HookAuthoring, s.server.deps.TemplateAuthoring, s.server.deps.AuthoringEditor = assembled.HookAuthoring, assembled.TemplateAuthoring, assembled.AuthoringEditor
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
	subscriptions := &streamSubscriptionObserver{Notifier: s.server.hub}
	s.server.hub = subscriptions
	batch := make([]repository.NewEvent, 205)
	for i := range batch {
		batch[i] = repository.NewEvent{At: fixedAt, Kind: "stream.test", Key: fmt.Sprintf("event-%d", i)}
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
	if _, err := st.AppendEvents(t.Context(), task.ID, []repository.NewEvent{{At: fixedAt, Kind: "stream.hint", Key: "hint"}}); err != nil {
		t.Fatal(err)
	}
	s.server.hub.Publish(task.ID)
	e := awaitStreamRecord(t, ch, 2*time.Second)
	if e.event.Seq != 206 {
		t.Fatal(e)
	}
	if _, err := st.AppendEvents(t.Context(), task.ID, []repository.NewEvent{{At: fixedAt, Kind: "stream.lost", Key: "lost"}}); err != nil {
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
	for {
		n := subscriptions.active.Load()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscriber leaked", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if subscriptions.stopped.Load() != 2 {
		t.Fatal("两条真实事件流没有全部退订", subscriptions.stopped.Load())
	}
}

func Test任务接口五角色权限(t *testing.T) {
	s, st, _, _, _ := taskFixture(t)
	for i, role := range []model.Role{model.RoleAdmin, model.RoleOperator, model.RoleHookAuthor, model.RoleHookApprover, model.RoleViewer} {
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
			allowed := r.Perm == PermTaskRead || role == model.RoleAdmin || role == model.RoleOperator
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
	seedHTTPUser(t, s, st, "backup", model.RoleAdmin)
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
				if _, err := st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.update", At: fixedAt}); err != nil {
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
				if _, err = st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.update", At: fixedAt}); err != nil {
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

func Test任务预览只使用冻结快照(t *testing.T) {
	s, st, token, item, _ := wizardFixture(t)
	item.Artifacts = map[string]string{"kernel": "old/kernel", "initrd": "old/initrd", "stage2": "old/stage2", "repo": "old/repo"}
	var err error
	item, err = st.UpdateISO(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	tpl, h := previewFixture(t, st)
	if err := st.SubmitHook(t.Context(), h.Key(), h.Rev, repository.Audit{Actor: "author", Action: "hook.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveHook(t.Context(), h.Key(), h.Rev+1, "reviewer", repository.Audit{Actor: "reviewer", Action: "hook.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	tpl.ID, tpl.Name, tpl.Version = "builtin-preview-frozen", "lober:preview-frozen", 1
	tpl.Builtin, tpl.Author, tpl.Approver, tpl.Rev, tpl.State = true, "", "", 0, model.VersionApproved
	tpl.Body = "# frozen {{sys:task}} {{param:message}}\n" + tpl.Body
	if err := st.EnsureBuiltinTemplate(t.Context(), tpl); err != nil {
		t.Fatal(err)
	}
	host, err := st.GetHost(t.Context(), "SN-1")
	if err != nil {
		t.Fatal(err)
	}
	frozen, ps := snapshot.Build(params.Input{Template: tpl, Hooks: map[model.HookKey]model.Hook{h.Key(): h}, ISO: item, Host: host})
	if len(ps) > 0 {
		t.Fatal(ps)
	}
	created, err := st.CreateTasks(t.Context(), []model.Task{{HostSN: host.SN, ISOID: item.ID, TemplateID: tpl.ID, TemplateVersion: 1, Snapshot: frozen, CreatedBy: "admin", CreatedAt: fixedAt}}, repository.Audit{Actor: "admin", Action: "task.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	id := created[0].ID
	path := "/console/v1/tasks/" + id + "/preview"
	first := responseData[previewResponse](t, httpCall(s, "POST", path, `{}`, token))
	tpl.Version = 2
	tpl.Body = "# new release\n{{lober:firstboot}}\n{{lober:cleanup}}\n"
	tpl.Params = nil
	tpl.Assembly = nil
	if err := st.EnsureBuiltinTemplate(t.Context(), tpl); err != nil {
		t.Fatal(err)
	}
	draft, err := st.NewHookVersion(t.Context(), h.ID, 1, "author")
	if err != nil {
		t.Fatal(err)
	}
	draft.Impls[0].Body = "#!/bin/sh\necho changed\n"
	if _, err := st.SaveHookDraft(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	item.Artifacts["kernel"] = "new/kernel"
	if _, err := st.UpdateISO(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	again := responseData[previewResponse](t, httpCall(s, "POST", path, `{"entered":{"tpl/message":"changed"}}`, token))
	if !reflect.DeepEqual(first, again) || len(first.Files) != 4 || !strings.Contains(first.Files[0].Body, "old/kernel") || !strings.Contains(first.Files[1].Body, "# frozen "+id+" template-default") || first.Files[3].Body != h.Impls[0].Body || first.Files[3].Digest != h.Impls[0].Digest() || first.Files[3].Digest == "" {
		t.Fatal(first, again)
	}
}

func Test任务SSE退出与到期只检查原令牌(t *testing.T) {
	for _, reason := range []string{"logout", "expiry-boundary"} {
		t.Run(reason, func(t *testing.T) {
			s, st, token, task, clock := taskFixture(t)
			other := identityFixture(s).sessions.Put("admin")
			srv := httptest.NewServer(s.Engine)
			defer srv.Close()
			req, _ := http.NewRequestWithContext(t.Context(), "GET", srv.URL+"/console/v1/tasks/"+task.ID+"/stream?after_seq=100", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatal(resp.StatusCode)
			}
			ch := readTaskStream(resp.Body)
			if reason == "logout" {
				if w := httpCall(s, "POST", "/console/v1/auth/logout", "", token); w.Code != 200 {
					t.Fatal(w.Code)
				}
			} else {
				expiry := identityFixture(s).sessions.ExpiresAt(token)
				clock.Store(expiry - 1)
				if !s.server.deps.Identity.StreamAuthorized(t.Context(), token, PermTaskRead) {
					t.Fatal("到期前提前失效")
				}
				clock.Store(expiry)
				other = identityFixture(s).sessions.Put("admin")
			}
			if w := httpCall(s, "GET", "/console/v1/user/info", "", other); w.Code != 200 {
				t.Fatal("其他会话应继续有效", w.Code)
			}
			if _, err := st.AppendEvents(t.Context(), task.ID, []repository.NewEvent{{At: clock.Load(), Kind: "after.revoke", Key: reason}}); err != nil {
				t.Fatal(err)
			}
			s.server.hub.Publish(task.ID)
			select {
			case got, ok := <-ch:
				if ok {
					t.Fatal("原令牌失效后仍发送事件", got)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("原令牌失效后未关闭")
			}
		})
	}
}

// 网页只绑定原始筛选文本，分页语法不合法时不能调用应用。
type taskReaderHTTPFixture struct {
	tasks.Reader
	calls   int
	req     tasks.ListRequest
	choice  tasks.TaskChoice
	preview tasks.PreviewResult
	err     error
}

func (f *taskReaderHTTPFixture) List(_ context.Context, _ ports.Actor, q tasks.ListRequest) (tasks.TaskPage, error) {
	f.calls++
	f.req = q
	return tasks.TaskPage{Items: []tasks.TaskChoice{f.choice}, Next: "next"}, f.err
}
func (f *taskReaderHTTPFixture) Get(_ context.Context, _ ports.Actor, _ string) (tasks.TaskChoice, error) {
	f.calls++
	return f.choice, f.err
}
func (f *taskReaderHTTPFixture) Preview(_ context.Context, _ ports.Actor, _ string, _ tasks.PreviewRequest) (tasks.PreviewResult, error) {
	f.calls++
	return f.preview, f.err
}
func Test任务网页只转换读取应用结果与原始查询(t *testing.T) {
	s, _, token, task, _ := taskFixture(t)
	p := model.NewProblem("host_has_open_task", model.Subject{Kind: model.KindHost, ID: "SN-1"})
	f := &taskReaderHTTPFixture{choice: tasks.TaskChoice{Task: task, CanCancel: []model.Problem{p}}}
	s.server.deps.TaskReader = f
	for _, path := range []string{"?limit=bad&created_from=bad", "?page=1&cursor=x", "?page_size=501"} {
		w := httpCall(s, "GET", "/console/v1/tasks"+path, "", token)
		if w.Code != 400 || f.calls != 0 {
			t.Fatal(path, w.Code, f.calls)
		}
	}
	page := responseData[taskPage](t, httpCall(s, "GET", "/console/v1/tasks?page=2&page_size=3&created_from=bad&created_to=-1&host_sn=sn-1&state=bad", "", token))
	if page.Next != "next" || len(page.Items) != 1 || len(page.Items[0].CanCancel) != 1 || f.req.CreatedFrom != "bad" || f.req.CreatedTo != "-1" || f.req.HostSN != "sn-1" || f.req.State != "bad" || f.req.Page != 2 || f.req.Limit != 3 {
		t.Fatal(page, f.req)
	}
	detail := responseData[taskItem](t, httpCall(s, "GET", "/console/v1/tasks/lookup", "", token))
	if !reflect.DeepEqual(detail, toTaskChoiceItem(f.choice)) {
		t.Fatal(detail)
	}
	ps := []model.Problem{model.NewProblem("arch_mismatch", model.Subject{}, "x86_64", "aarch64"), p}
	f.preview = tasks.PreviewResult{Problems: ps}
	w := httpCall(s, "POST", "/console/v1/tasks/lookup/preview", `{}`, token)
	if w.Code != StatusOfProblems(ps) || !strings.Contains(w.Body.String(), "arch_mismatch") {
		t.Fatal(w.Code, w.Body.String())
	}
	f.err = errors.New("private renderer")
	w = httpCall(s, "POST", "/console/v1/tasks/lookup/preview", `{}`, token)
	if w.Code != 500 || strings.Contains(w.Body.String(), "arch_mismatch") || strings.Contains(w.Body.String(), "private renderer") {
		t.Fatal(w.Code, w.Body.String())
	}
	f.err = tasks.ErrBadRequest
	w = httpCall(s, "GET", "/console/v1/tasks", "", token)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}

type taskControllerResultFixture struct {
	calls  []string
	actor  ports.Actor
	choice tasks.TaskChoice
	err    error
}

func (f *taskControllerResultFixture) Cancel(_ context.Context, a ports.Actor, id string) (tasks.TaskChoice, error) {
	f.actor = a
	f.calls = append(f.calls, "cancel:"+id)
	return f.choice, f.err
}
func (f *taskControllerResultFixture) Retry(_ context.Context, a ports.Actor, id string) (tasks.TaskChoice, error) {
	f.actor = a
	f.calls = append(f.calls, "retry:"+id)
	return f.choice, f.err
}
func Test任务控制网页只转换应用结果(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "alice", model.RoleAdmin)
	ps := []model.Problem{model.NewProblem("host_has_open_task", model.Subject{})}
	f := &taskControllerResultFixture{choice: tasks.TaskChoice{Task: model.Task{ID: "app-result", Rev: 17, State: model.TaskCancelled}, CanCancel: ps, CanRetry: ps}}
	s.server.deps.TaskController = f
	for _, action := range []string{"cancel", "retry"} {
		got := responseData[taskItem](t, httpCall(s, "POST", "/console/v1/tasks/raw-id/"+action, `{"actor":"forged","action":"forged","detail":"forged","at":1}`, token))
		if got.ID != "app-result" || got.Rev != 17 || got.State != model.TaskCancelled || len(got.CanCancel) != 1 || len(got.CanRetry) != 1 || got.CanCancel[0].Code != ps[0].Code || f.actor.Username != "alice" || f.actor.Role != model.RoleAdmin {
			t.Fatal(got, f.actor)
		}
	}
	if !reflect.DeepEqual(f.calls, []string{"cancel:raw-id", "retry:raw-id"}) {
		t.Fatal(f.calls)
	}
	f.err = tasks.ErrControlUnavailable
	w := httpCall(s, "POST", "/console/v1/tasks/raw-id/cancel", "", token)
	if w.Code != 422 || !strings.Contains(w.Body.String(), model.ConsoleTaskControlUnavailable) {
		t.Fatal(w.Code, w.Body.String())
	}
}
