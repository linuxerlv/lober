package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	selectionguard "github.com/lober/lober/internal/infrastructure/selection"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test编写预览路由契约基线(t *testing.T) {
	s, st, token, _, tpl := wizardFixture(t)
	// 手写完整集合，新增、删除、改权限或审计都需要明确更新契约。
	want := map[string][2]string{
		"GET /console/v1/hooks":                                           {"hook:read", ""},
		"POST /console/v1/hooks":                                          {"hook:write", ""},
		"GET /console/v1/hooks/:id/versions":                              {"hook:read", ""},
		"POST /console/v1/hooks/:id/versions":                             {"hook:write", ""},
		"GET /console/v1/hooks/:id/versions/:version":                     {"hook:read", ""},
		"PUT /console/v1/hooks/:id/versions/:version":                     {"hook:write", ""},
		"DELETE /console/v1/hooks/:id/versions/:version":                  {"hook:write", ""},
		"PUT /console/v1/hooks/:id/name":                                  {"hook:write", "hook.rename"},
		"POST /console/v1/hooks/:id/versions/:version/submit":             {"hook:write", "hook.submit"},
		"POST /console/v1/hooks/:id/versions/:version/approve":            {"hook:approve", "hook.approve"},
		"POST /console/v1/hooks/:id/versions/:version/reject":             {"hook:approve", "hook.reject"},
		"POST /console/v1/hooks/:id/versions/:version/deprecate":          {"hook:approve", "hook.deprecate"},
		"POST /console/v1/hooks/:id/versions/:version/validate":           {"hook:write", ""},
		"GET /console/v1/templates":                                       {"template:read", ""},
		"POST /console/v1/templates":                                      {"template:write", ""},
		"POST /console/v1/templates/:id/derive":                           {"template:write", ""},
		"GET /console/v1/templates/:id/versions":                          {"template:read", ""},
		"POST /console/v1/templates/:id/versions":                         {"template:write", ""},
		"GET /console/v1/templates/:id/versions/:version":                 {"template:read", ""},
		"PUT /console/v1/templates/:id/versions/:version":                 {"template:write", ""},
		"DELETE /console/v1/templates/:id/versions/:version":              {"template:write", ""},
		"PUT /console/v1/templates/:id/name":                              {"template:write", "template.rename"},
		"POST /console/v1/templates/:id/versions/:version/submit":         {"template:write", "template.submit"},
		"POST /console/v1/templates/:id/versions/:version/approve":        {"template:approve", "template.approve"},
		"POST /console/v1/templates/:id/versions/:version/reject":         {"template:approve", "template.reject"},
		"POST /console/v1/templates/:id/versions/:version/deprecate":      {"template:approve", "template.deprecate"},
		"POST /console/v1/templates/:id/versions/:version/validate":       {"template:write", ""},
		"POST /console/v1/templates/:id/versions/:version/preview":        {"template:read", ""},
		"POST /console/v1/templates/:id/versions/:version/preview-params": {"template:read", ""},
		"GET /console/v1/templates/:id/versions/:version/slots":           {"template:read", ""},
		"GET /console/v1/templates/:id/versions/:version/hook-candidates": {"template:read", ""},
		"GET /console/v1/templates/:id/versions/:version/source-diff":     {"template:read", ""},
	}
	got := map[string][2]string{}
	for _, rt := range s.Routes() {
		if strings.HasPrefix(rt.Path, "/console/v1/hooks") || strings.HasPrefix(rt.Path, "/console/v1/templates") {
			key := rt.Method + " " + rt.Path
			if _, duplicate := got[key]; duplicate {
				t.Fatal("重复路由", key)
			}
			got[key] = [2]string{string(rt.Perm), rt.Audit}
		}
	}
	if len(want) != 32 || !reflect.DeepEqual(got, want) {
		t.Fatalf("路由契约变化：got=%v want=%v", got, want)
	}
	for i, role := range []model.Role{model.RoleAdmin, model.RoleHookAuthor, model.RoleHookApprover, model.RoleOperator, model.RoleViewer} {
		roleToken, _ := seedHTTPUser(t, s, st, fmt.Sprintf("contractrole%d", i), role)
		for key, meta := range want {
			parts := strings.SplitN(key, " ", 2)
			path := strings.ReplaceAll(strings.ReplaceAll(parts[1], ":id", "missing"), ":version", "1")
			allowed := strings.HasSuffix(meta[0], ":read") || role == model.RoleAdmin || (strings.HasSuffix(meta[0], ":write") && role == model.RoleHookAuthor) || (strings.HasSuffix(meta[0], ":approve") && role == model.RoleHookApprover)
			w := httpCall(s, parts[0], path, `{}`, roleToken)
			if allowed && (w.Code == 401 || w.Code == 403) || !allowed && w.Code != 403 {
				t.Fatal(role, key, w.Code, w.Body.String())
			}
		}
	}
	hook := approvedTestHook(t, st, "contract-hook")
	candidate, err := st.CreateTemplate(t.Context(), model.Template{Name: "contract-slots", Author: "alice", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "{{slot:disk}}\n"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	h := responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks", `{"name":"contract-draft-hook"}`, token))
	hookPath := fmt.Sprintf("/console/v1/hooks/%s/versions/1", h.ID)
	responseData[hookResponse](t, httpCall(s, "PUT", hookPath, `{"rev":1,"description":"saved"}`, token))
	responseData[any](t, httpCall(s, "DELETE", hookPath+"?rev=2", "", token))
	copyHook := responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks/"+hook.ID+"/versions?from=1", "", token))
	responseData[any](t, httpCall(s, "DELETE", fmt.Sprintf("/console/v1/hooks/%s/versions/%d?rev=1", hook.ID, copyHook.Version), "", token))
	draft := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates", `{"name":"contract-draft-template"}`, token))
	draftPath := fmt.Sprintf("/console/v1/templates/%s/versions/1", draft.ID)
	responseData[templateResponse](t, httpCall(s, "PUT", draftPath, `{"rev":1,"body":"saved"}`, token))
	responseData[any](t, httpCall(s, "DELETE", draftPath+"?rev=2", "", token))
	copyTemplate := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates/"+tpl.ID+"/versions?from=1", "", token))
	responseData[any](t, httpCall(s, "DELETE", fmt.Sprintf("/console/v1/templates/%s/versions/%d?rev=1", tpl.ID, copyTemplate.Version), "", token))
	derived := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates/"+tpl.ID+"/derive", `{"from":1,"name":"contract-derived"}`, token))
	for _, path := range []string{
		"/console/v1/hooks", "/console/v1/hooks/" + hook.ID + "/versions", fmt.Sprintf("/console/v1/hooks/%s/versions/1", hook.ID),
		"/console/v1/templates", "/console/v1/templates/" + tpl.ID + "/versions", fmt.Sprintf("/console/v1/templates/%s/versions/1", tpl.ID),
		fmt.Sprintf("/console/v1/templates/%s/versions/1/slots", tpl.ID),
		fmt.Sprintf("/console/v1/templates/%s/versions/1/hook-candidates?slot=disk", candidate.ID),
		fmt.Sprintf("/console/v1/templates/%s/versions/1/source-diff", derived.ID),
	} {
		responseData[any](t, httpCall(s, "GET", path, "", token))
	}
	for _, path := range []string{
		fmt.Sprintf("/console/v1/hooks/%s/versions/1/validate", hook.ID),
		fmt.Sprintf("/console/v1/templates/%s/versions/1/validate", tpl.ID),
		fmt.Sprintf("/console/v1/templates/%s/versions/1/preview-params", tpl.ID),
		fmt.Sprintf("/console/v1/templates/%s/versions/1/preview", tpl.ID),
	} {
		responseData[any](t, httpCall(s, "POST", path, `{}`, token))
	}
	after, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil || !reflect.DeepEqual(before.Items, after.Items) {
		t.Fatal("草稿及读取操作改变了审计", before.Items, after.Items, err)
	}
}

func authoringErrorEnvelope(t *testing.T, w *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"code": float64(status), "data": nil, "message": message}
	if w.Code != status || !reflect.DeepEqual(got, want) {
		t.Fatalf("got status=%d body=%s want=%v", w.Code, w.Body.String(), want)
	}
}

func Test编写输入多错时保留原先错误(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	tpl, _ := previewFixture(t, st)
	base := fmt.Sprintf("/console/v1/templates/%s/versions/1", tpl.ID)
	authoringErrorEnvelope(t, httpCall(s, "GET", "/console/v1/templates/aaaaaaaaaaaaaaaaaaaaaaaaaa/versions/1/hook-candidates?slot=missing&index=bad", "", token), 404, model.ConsoleNotFound)
	authoringErrorEnvelope(t, httpCall(s, "GET", base+"/hook-candidates?slot=missing&index=bad", "", token), 400, model.ConsoleBadRequest)
	for _, kind := range []string{"hooks", "templates"} {
		authoringErrorEnvelope(t, httpCall(s, "POST", "/console/v1/"+kind+"/missing/versions/bad/reject?rev=bad", `{"reason":" \t "}`, token), 400, model.ConsoleRejectReason)
	}
	authoringErrorEnvelope(t, httpCall(s, "PUT", base, `{"rev":1,"name":"different"}`, token), 400, model.ConsoleBadRequest)
	h := responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks", `{"name":"contract-name"}`, token))
	authoringErrorEnvelope(t, httpCall(s, "PUT", fmt.Sprintf("/console/v1/hooks/%s/versions/1", h.ID), `{"rev":1,"name":"different"}`, token), 400, model.ConsoleBadRequest)
	for _, endpoint := range []string{"preview", "preview-params"} {
		for _, body := range []string{`{"isoID":"aaaaaaaaaaaaaaaaaaaaaaaaaa"}`, `{"hostSN":"MISSING-HOST"}`} {
			authoringErrorEnvelope(t, httpCall(s, "POST", base+"/"+endpoint, body, token), 404, model.ConsoleNotFound)
		}
	}
}

func Test编写页型与来源选择互不替代(t *testing.T) {
	s, st, token, iso, source := wizardFixture(t)
	hook := approvedTestHook(t, st, "contract-page-a")
	if _, err := st.NewHookVersion(t.Context(), hook.ID, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	second := approvedTestHook(t, st, "contract-page-b")
	tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: "contract-page-template", Author: "alice", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "{{slot:disk}}\n"})
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("/console/v1/templates/%s/versions/1/hook-candidates?slot=disk&prefix=contract-page-", tpl.ID)
	cursor := responseData[hookCandidatePage](t, httpCall(s, "GET", base+"&limit=1", "", token))
	if len(cursor.Items) != 2 || cursor.Items[0].Hook.ID != hook.ID || cursor.Items[0].Hook.Version != 1 || cursor.Items[1].Hook.ID != hook.ID || cursor.Items[1].Hook.Version != 2 || cursor.Next == "" || cursor.Total != nil {
		t.Fatal(cursor)
	}
	tail := responseData[hookCandidatePage](t, httpCall(s, "GET", base+"&limit=1&cursor="+cursor.Next, "", token))
	if len(tail.Items) != 1 || tail.Items[0].Hook.ID != second.ID || tail.Next != "" {
		t.Fatal(tail)
	}
	for page := 1; page <= 3; page++ {
		got := responseData[hookCandidatePage](t, httpCall(s, "GET", fmt.Sprintf("%s&page=%d&page_size=1", base, page), "", token))
		wantID, wantVersion := hook.ID, page
		if page == 3 {
			wantID, wantVersion = second.ID, 1
		}
		if got.Total == nil || *got.Total != 3 || len(got.Items) != 1 || got.Items[0].Hook.ID != wantID || got.Items[0].Hook.Version != wantVersion || (got.Next != "") != (page < 3) {
			t.Fatal(page, got)
		}
	}
	sourceKey := model.TemplateKey{ID: source.ID, Version: source.Version}
	derived, err := st.DeriveTemplate(t.Context(), sourceKey, "contract-source-copy", "alice")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := st.NewTemplateVersion(t.Context(), source.ID, 1, "alice")
	if err != nil {
		t.Fatal(err)
	}
	newer.Body = "# latest draft\n"
	if _, err = st.SaveTemplateDraft(t.Context(), newer); err != nil {
		t.Fatal(err)
	}
	diff := responseData[sourceDiffResponse](t, httpCall(s, "GET", fmt.Sprintf("/console/v1/templates/%s/versions/1/source-diff", derived.ID), "", token))
	if !diff.Changed || diff.Original == nil || diff.Current == nil || diff.Original.ID != source.ID || diff.Original.Version != 1 || diff.Original.Body != source.Body || diff.Current.Version != 2 || diff.Current.State != model.VersionDraft || diff.Current.Body != "# latest draft\n" {
		t.Fatal(diff)
	}
	stored, err := st.GetTemplate(t.Context(), model.TemplateKey{ID: derived.ID, Version: derived.Version})
	if err != nil || stored.Body != source.Body || stored.DerivedFrom == nil || *stored.DerivedFrom != sourceKey || stored.Rev != derived.Rev {
		t.Fatal(stored, err)
	}
	choices := responseData[wizardTemplateResult](t, httpCall(s, "POST", "/console/v1/wizard/templates", fmt.Sprintf(`{"isoID":%q}`, iso.ID), token))
	if len(choices.Items) != 1 || choices.Items[0].ID != source.ID || choices.Items[0].Version != 1 || choices.Items[0].State != model.VersionApproved {
		t.Fatal(choices)
	}
	noSource := responseData[sourceDiffResponse](t, httpCall(s, "GET", fmt.Sprintf("/console/v1/templates/%s/versions/1/source-diff", source.ID), "", token))
	if noSource.Original != nil || noSource.Current != nil || noSource.Changed {
		t.Fatal(noSource)
	}
}

func Test编写审计失败状态与审批人均回滚(t *testing.T) {
	for _, kind := range []string{"hook", "template"} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel=%v", kind, cancelled), func(t *testing.T) {
				s, st, _, _, baseTemplate := wizardFixture(t)
				a := ports.Actor{Username: pgtest.FaultActor, Role: model.RoleHookApprover}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var beforeHook model.Hook
				var beforeTemplate model.Template
				if kind == "hook" {
					beforeHook = approvedTestHook(t, st, "rollback-hook")
					next, e := st.NewHookVersion(t.Context(), beforeHook.ID, 1, "alice")
					if e != nil {
						t.Fatal(e)
					}
					if e = st.SubmitHook(t.Context(), next.Key(), next.Rev, repository.Audit{Actor: "alice", Action: "hook.submit", At: fixedAt}); e != nil {
						t.Fatal(e)
					}
					beforeHook, e = st.GetHook(t.Context(), next.Key())
					if e != nil {
						t.Fatal(e)
					}
				} else {
					beforeTemplate = baseTemplate
					next, e := st.NewTemplateVersion(t.Context(), beforeTemplate.ID, 1, "alice")
					if e != nil {
						t.Fatal(e)
					}
					k := model.TemplateKey{ID: next.ID, Version: next.Version}
					if e = st.SubmitTemplate(t.Context(), k, next.Rev, repository.Audit{Actor: "alice", Action: "template.submit", At: fixedAt}); e != nil {
						t.Fatal(e)
					}
					beforeTemplate, e = st.GetTemplate(t.Context(), k)
					if e != nil {
						t.Fatal(e)
					}
				}
				before, e := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
				if e != nil {
					t.Fatal(e)
				}
				notified := &authoringNotificationProbe{Notifier: s.server.deps.Notifier}
				s.server.deps.Notifier = notified
				if cancelled {
					cancel()
				}
				var err error
				if kind == "hook" {
					_, err = s.server.deps.HookAuthoring.Approve(ctx, a, authoring.HookRevision{Key: beforeHook.Key(), Rev: beforeHook.Rev})
				} else {
					_, err = s.server.deps.TemplateAuthoring.Approve(ctx, a, authoring.TemplateRevision{Key: model.TemplateKey{ID: beforeTemplate.ID, Version: beforeTemplate.Version}, Rev: beforeTemplate.Rev})
				}
				if err == nil || cancelled && !errors.Is(err, context.Canceled) {
					t.Fatal("预期事务失败", err)
				}
				if !cancelled {
					for _, sentinel := range []error{repository.ErrInvalid, repository.ErrConflict, repository.ErrRejected, repository.ErrNotFound, ports.ErrForbidden} {
						if errors.Is(err, sentinel) {
							t.Fatal("未到审计失败注入", err)
						}
					}
				}
				if kind == "hook" {
					after, e := st.GetHook(t.Context(), beforeHook.Key())
					if e != nil || !reflect.DeepEqual(beforeHook, after) {
						t.Fatal("组件未回滚", after, e)
					}
				} else {
					after, e := st.GetTemplate(t.Context(), model.TemplateKey{ID: beforeTemplate.ID, Version: beforeTemplate.Version})
					if e != nil || !reflect.DeepEqual(beforeTemplate, after) {
						t.Fatal("模板未回滚", after, e)
					}
					done := make(chan error, 1)
					go func() {
						_, e := s.server.deps.TemplateAuthoring.Approve(t.Context(), ports.Actor{Username: "reviewer", Role: model.RoleHookApprover}, authoring.TemplateRevision{Key: model.TemplateKey{ID: beforeTemplate.ID, Version: beforeTemplate.Version}, Rev: beforeTemplate.Rev})
						done <- e
					}()
					select {
					case e := <-done:
						if e != nil {
							t.Fatal("失败后选择锁未释放", e)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("失败后选择锁未释放")
					}
				}
				// 模板重试成功新增一条；组件分支保持审计不变。
				after, e := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
				if e != nil {
					t.Fatal(e)
				}
				if kind == "hook" && !reflect.DeepEqual(before.Items, after.Items) || kind == "template" && (len(after.Items) != len(before.Items)+1 || !reflect.DeepEqual(after.Items[1:], before.Items)) {
					t.Fatal("失败事务留下审计", before.Items, after.Items)
				}
				if notified.count.Load() != 0 {
					t.Fatal("编写操作发送了通知")
				}
			})
		}
	}
}

type authoringNotificationProbe struct {
	ports.Notifier
	count atomic.Int64
}

func (n *authoringNotificationProbe) Publish(string) { n.count.Add(1) }

type authoringWaitingGuard struct {
	ports.SelectionGuard
	waiting chan struct{}
	entered chan struct{}
}

func (g *authoringWaitingGuard) WriteLock() func() {
	close(g.waiting)
	release := g.SelectionGuard.WriteLock()
	close(g.entered)
	return release
}
func Test模板取消等待选择锁后释放锁且不写入(t *testing.T) {
	for _, op := range []string{"approve", "deprecate"} {
		t.Run(op, func(t *testing.T) {
			s, st, _, _, baseTemplate := wizardFixture(t)
			tpl := baseTemplate
			if op == "approve" {
				next, e := st.NewTemplateVersion(t.Context(), tpl.ID, 1, "alice")
				if e != nil {
					t.Fatal(e)
				}
				k := model.TemplateKey{ID: next.ID, Version: next.Version}
				if e = st.SubmitTemplate(t.Context(), k, next.Rev, repository.Audit{Actor: "alice", Action: "template.submit", At: fixedAt}); e != nil {
					t.Fatal(e)
				}
				tpl, e = st.GetTemplate(t.Context(), k)
				if e != nil {
					t.Fatal(e)
				}
			}
			guard := &authoringWaitingGuard{SelectionGuard: selectionguard.NewGuard(), waiting: make(chan struct{}), entered: make(chan struct{})}
			d := wizardDepsGuard(s.server.deps, st, guard)
			before, e := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
			if e != nil {
				t.Fatal(e)
			}
			release := guard.ReadLock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				r := authoring.TemplateRevision{Key: model.TemplateKey{ID: tpl.ID, Version: tpl.Version}, Rev: tpl.Rev}
				a := ports.Actor{Username: "reviewer", Role: model.RoleHookApprover}
				var err error
				if op == "approve" {
					_, err = d.TemplateAuthoring.Approve(ctx, a, r)
				} else {
					_, err = d.TemplateAuthoring.Deprecate(ctx, a, r)
				}
				done <- err
			}()
			select {
			case <-guard.waiting:
			case <-time.After(5 * time.Second):
				release()
				t.Fatal("应用未等待选择锁")
			}
			cancel()
			select {
			case <-guard.entered:
				release()
				t.Fatal("读锁期间进入了写锁")
			case err := <-done:
				release()
				t.Fatal("等待锁新增了立即取消", err)
			default:
			}
			release()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("取消后未退出")
			}
			acquired := make(chan struct{})
			go func() { r := guard.SelectionGuard.WriteLock(); r(); close(acquired) }()
			select {
			case <-acquired:
			case <-time.After(5 * time.Second):
				t.Fatal("取消后未释放写锁")
			}
			after, e := st.GetTemplate(t.Context(), model.TemplateKey{ID: tpl.ID, Version: tpl.Version})
			if e != nil || !reflect.DeepEqual(tpl, after) {
				t.Fatal(after, e)
			}
			audit, e := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
			if e != nil || !reflect.DeepEqual(before.Items, audit.Items) {
				t.Fatal("取消后留下审计", audit, e)
			}
		})
	}
}
