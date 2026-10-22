package console

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/infrastructure/builtin"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

func reviewerHTTP(t *testing.T, s *Server, st repository.Users) string {
	t.Helper()
	seedAuthUser(t, st, "reviewer", model.RoleHookApprover, "reviewer password")
	return loginHTTP(t, s, "reviewer", "reviewer password").Token
}
func lifecyclePath(kind, id, operation string, rev int64) string {
	return fmt.Sprintf("/console/v1/%s/%s/versions/1/%s?rev=%d", kind, id, operation, rev)
}
func Test生命周期Hook提交审批废弃与审计(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	admin := adminHTTP(t, s, st)
	reviewer := reviewerHTTP(t, s, st)
	h := responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks", `{"name":"life","impls":[{"family":"linux","series":["rocky"],"kind":"fragment","body":"# example\n"}]}`, admin))
	h = responseData[hookResponse](t, httpCall(s, "POST", lifecyclePath("hooks", h.ID, "submit", 1), "", admin))
	if h.State != model.VersionReview || h.Rev != 2 {
		t.Fatal(h)
	}
	w := httpCall(s, "POST", lifecyclePath("hooks", h.ID, "approve", 2), `{"approver":"reviewer"}`, admin)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "approver_is_author") {
		t.Fatal(w.Code, w.Body.String())
	}
	h = responseData[hookResponse](t, httpCall(s, "POST", lifecyclePath("hooks", h.ID, "approve", 2), `{"approver":"spoof"}`, reviewer))
	if h.State != model.VersionApproved || h.Approver != "reviewer" || h.Rev != 3 {
		t.Fatal(h)
	}
	h = responseData[hookResponse](t, httpCall(s, "POST", lifecyclePath("hooks", h.ID, "deprecate", 3), "", reviewer))
	if h.State != model.VersionDeprecated || h.Rev != 4 {
		t.Fatal(h)
	}
	audit, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, a := range audit.Items {
		if a.TargetID == h.ID+"@1" {
			if a.TargetKind != "hook" || a.At != fixedAt || a.Action == "hook.submit" && a.Actor != "admin" || a.Action != "hook.submit" && a.Actor != "reviewer" {
				t.Fatal(a)
			}
			counts[a.Action]++
		}
	}
	if counts["hook.submit"] != 1 || counts["hook.approve"] != 1 || counts["hook.deprecate"] != 1 || len(counts) != 3 {
		t.Fatal(counts)
	}
}
func Test生命周期修订前提与失败不写审计(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	admin := adminHTTP(t, s, st)
	reviewer := reviewerHTTP(t, s, st)
	h := responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks", `{"name":"draft"}`, admin))
	base := fmt.Sprintf("/console/v1/hooks/%s/versions/1", h.ID)
	for _, op := range []string{"submit", "approve", "reject", "deprecate"} {
		for _, rev := range []string{"", "?rev=bad", "?rev=0", "?rev=-1"} {
			if w := httpCall(s, "POST", base+"/"+op+rev, `{"reason":"fix"}`, admin); w.Code != 400 {
				t.Fatal(op, rev, w.Code, w.Body.String())
			}
		}
	}
	before, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	for _, op := range []string{"approve", "reject", "deprecate"} {
		if w := httpCall(s, "POST", lifecyclePath("hooks", h.ID, op, 1), `{"reason":"fix"}`, reviewer); w.Code != 409 {
			t.Fatal(op, w.Code, w.Body.String())
		}
	}
	// Submitting is a review transition; incomplete content remains visible to the reviewer.
	h = responseData[hookResponse](t, httpCall(s, "POST", lifecyclePath("hooks", h.ID, "submit", 1), "", admin))
	if len(h.Problems) == 0 {
		t.Fatal("missing diagnostics")
	}
	if w := httpCall(s, "POST", lifecyclePath("hooks", h.ID, "approve", 1), "", reviewer); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := httpCall(s, "POST", lifecyclePath("hooks", h.ID, "approve", 2), "", reviewer)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "hook_no_impl") {
		t.Fatal(w.Code, w.Body.String())
	}
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1})
	if err != nil || back.State != model.VersionReview || back.Rev != 2 {
		t.Fatal(back, err)
	}
	after, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if len(after.Items) != len(before.Items)+1 {
		t.Fatal("failed operations audited")
	}
}
func Test生命周期驳回原样理由并允许再编辑(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	admin := adminHTTP(t, s, st)
	reviewer := reviewerHTTP(t, s, st)
	h := responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks", `{"name":"rejectable"}`, admin))
	responseData[hookResponse](t, httpCall(s, "POST", lifecyclePath("hooks", h.ID, "submit", 1), "", admin))
	for _, body := range []string{"", `{}`, `{"reason":""}`, `{"reason":" \n\t "}`, `{"reason":"\u0000"}`} {
		if w := httpCall(s, "POST", lifecyclePath("hooks", h.ID, "reject", 2), body, reviewer); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	h = responseData[hookResponse](t, httpCall(s, "POST", lifecyclePath("hooks", h.ID, "reject", 2), `{"reason":"  请补齐实现  "}`, reviewer))
	if h.State != model.VersionDraft || h.Rev != 3 {
		t.Fatal(h)
	}
	audit, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	found := false
	for _, a := range audit.Items {
		if a.Action == "hook.reject" {
			found = true
			if a.Detail != "  请补齐实现  " || a.Actor != "reviewer" {
				t.Fatal(a)
			}
		}
	}
	if !found {
		t.Fatal("missing rejection audit")
	}
	responseData[hookResponse](t, httpCall(s, "PUT", fmt.Sprintf("/console/v1/hooks/%s/versions/1", h.ID), `{"rev":3,"description":"updated"}`, admin))
}
func Test生命周期模板审批重跑引用并保留待审批状态(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	admin := adminHTTP(t, s, st)
	reviewer := reviewerHTTP(t, s, st)
	hook := approvedTestHook(t, st, "reference")
	body := fmt.Sprintf(`{"name":"reviewed","family":"linux","series":["rocky"],"body":"{{lober:syslog}}\n{{slot:x}}\n{{lober:cleanup}}\n{{lober:firstboot}}\n","assembly":{"x":[{"hookID":%q,"hookVersion":1}]}}`, hook.ID)
	tpl := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates", body, admin))
	tpl = responseData[templateResponse](t, httpCall(s, "POST", lifecyclePath("templates", tpl.ID, "submit", 1), "", admin))
	if tpl.State != model.VersionReview {
		t.Fatal(tpl)
	}
	responseData[hookResponse](t, httpCall(s, "POST", lifecyclePath("hooks", hook.ID, "deprecate", hook.Rev), "", reviewer))
	before, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	w := httpCall(s, "POST", lifecyclePath("templates", tpl.ID, "approve", 2), "", reviewer)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "hook_ref_deprecated") {
		t.Fatal(w.Code, w.Body.String())
	}
	back, err := st.GetTemplate(t.Context(), model.TemplateKey{ID: tpl.ID, Version: 1})
	if err != nil || back.State != model.VersionReview || back.Rev != 2 {
		t.Fatal(back, err)
	}
	after, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if len(after.Items) != len(before.Items) {
		t.Fatal("rejected approval audited")
	}
	tpl = responseData[templateResponse](t, httpCall(s, "POST", lifecyclePath("templates", tpl.ID, "reject", 2), `{"reason":"换一个仍可用的 hook"}`, reviewer))
	if tpl.State != model.VersionDraft || tpl.Rev != 3 {
		t.Fatal(tpl)
	}
	// Restore a valid body without that reference, then approve and deprecate through HTTP.
	tpl = responseData[templateResponse](t, httpCall(s, "PUT", fmt.Sprintf("/console/v1/templates/%s/versions/1", tpl.ID), `{"rev":3,"family":"linux","series":["rocky"],"body":"{{lober:syslog}}\n{{lober:cleanup}}\n{{lober:firstboot}}\n"}`, admin))
	responseData[templateResponse](t, httpCall(s, "POST", lifecyclePath("templates", tpl.ID, "submit", 4), "", admin))
	tpl = responseData[templateResponse](t, httpCall(s, "POST", lifecyclePath("templates", tpl.ID, "approve", 5), "", reviewer))
	if tpl.State != model.VersionApproved || tpl.Approver != "reviewer" || tpl.Rev != 6 {
		t.Fatal(tpl)
	}
	tpl = responseData[templateResponse](t, httpCall(s, "POST", lifecyclePath("templates", tpl.ID, "deprecate", 6), "", reviewer))
	if tpl.State != model.VersionDeprecated || tpl.Rev != 7 {
		t.Fatal(tpl)
	}
}
func Test生命周期校验只读并计算OK(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	admin := adminHTTP(t, s, st)
	invalid := responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks", `{"name":"invalid"}`, admin))
	valid := approvedTestHook(t, st, "valid")
	tpl := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates", fmt.Sprintf(`{"name":"diagnostic","family":"linux","series":["rocky"],"body":"{{lober:syslog}}\n{{slot:x}}\n{{lober:cleanup}}\n{{lober:firstboot}}\n","assembly":{"x":[{"hookID":%q,"hookVersion":1}]}}`, valid.ID), admin))
	before, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	for _, item := range []struct {
		kind, id string
		want     bool
	}{{"hooks", invalid.ID, false}, {"hooks", valid.ID, true}, {"templates", tpl.ID, true}} {
		p := responseData[validateResponse](t, httpCall(s, "POST", lifecyclePath(item.kind, item.id, "validate", 0), "", admin))
		if p.OK != item.want || p.Problems == nil || p.OK && len(p.Problems) != 0 || !p.OK && len(p.Problems) == 0 {
			t.Fatal(item, p)
		}
	}
	after, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if len(after.Items) != len(before.Items) {
		t.Fatal("validate audited")
	}
	back, _ := st.GetHook(t.Context(), model.HookKey{ID: invalid.ID, Version: 1})
	if back.State != model.VersionDraft || back.Rev != 1 {
		t.Fatal(back)
	}
	// A closed real backend must be reported as 500, not a fabricated missing-reference problem.
	closed := pgtest.OpenStore(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	s.server.deps.Hooks = closed
	// 编写应用在构造时接收引用读取器；失败注入也必须重新组装该能力。
	assembled := wizardDeps(s.server.deps, st)
	s.server.deps.Wizard, s.server.deps.TemplateAuthoring = assembled.Wizard, assembled.TemplateAuthoring
	w := httpCall(s, "POST", lifecyclePath("templates", tpl.ID, "validate", 0), "", admin)
	if w.Code != 500 || strings.Contains(w.Body.String(), "hook_ref_missing") || strings.Contains(w.Body.String(), "database is closed") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func Test生命周期十个端点权限与内置保护(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	seedBuiltinContent(t, st)
	for i, role := range model.Roles() {
		name := fmt.Sprintf("life%d", i)
		seedAuthUser(t, st, name, role, "pw")
		token := loginHTTP(t, s, name, "pw").Token
		count := 0
		for _, r := range s.Routes() {
			lifecycle := false
			op := ""
			for _, action := range []string{"submit", "approve", "reject", "deprecate", "validate"} {
				if strings.HasSuffix(r.Path, "/"+action) && strings.Contains(r.Path, "/versions/") {
					lifecycle = true
					op = action
				}
			}
			if !lifecycle {
				continue
			}
			count++
			path := strings.ReplaceAll(strings.ReplaceAll(r.Path, ":id", "missing"), ":version", "1") + "?rev=1"
			w := httpCall(s, r.Method, path, `{"reason":"fix"}`, token)
			allowed := role == model.RoleAdmin || (op == "submit" || op == "validate") && role == model.RoleHookAuthor || (op == "approve" || op == "reject" || op == "deprecate") && role == model.RoleHookApprover
			if allowed && (w.Code == 401 || w.Code == 403) || !allowed && w.Code != 403 {
				t.Fatal(role, r, w.Code)
			}
		}
		if count != 10 {
			t.Fatal(count)
		}
		if role == model.RoleAdmin {
			h := builtin.Hooks()[0]
			tpl := builtin.Templates()[0]
			for _, item := range []struct {
				kind, id string
				version  int
			}{{"hooks", h.ID, h.Version}, {"templates", tpl.ID, tpl.Version}} {
				for _, op := range []string{"submit", "approve", "reject", "deprecate"} {
					path := fmt.Sprintf("/console/v1/%s/%s/versions/%d/%s?rev=1", item.kind, item.id, item.version, op)
					if w := httpCall(s, "POST", path, `{"reason":"fix"}`, token); w.Code != 409 {
						t.Fatal(item, op, w.Code, w.Body.String())
					}
				}
			}
		}
	}
}

func Test组件驳回理由错误不被坏版本和修订盖掉(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	reviewer := reviewerHTTP(t, s, st)
	for _, version := range []string{"bad", "0", "999999999999999999999999999"} {
		for _, rev := range []string{"bad", "0", "999999999999999999999999999"} {
			path := "/console/v1/hooks/missing/versions/" + version + "/reject?rev=" + rev
			for _, body := range []string{`{"reason":"  "}`, `{"reason":"\u0000"}`} {
				w := httpCall(s, "POST", path, body, reviewer)
				var got struct {
					Message string `json:"message"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if w.Code != 400 || got.Message != model.ConsoleRejectReason {
					t.Fatal(version, rev, w.Code, w.Body.String())
				}
			}
			w := httpCall(s, "POST", path, `{"reason":"修正内容"}`, reviewer)
			if w.Code != 400 || strings.Contains(w.Body.String(), model.ConsoleRejectReason) {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
}

func Test生命周期模板数字溢出仍返回400(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: "overflow", Author: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	overflow := "9223372036854775808"
	for _, op := range []string{"submit", "approve", "reject", "deprecate"} {
		for _, number := range []struct{ name, version, rev string }{{"version", overflow, "1"}, {"rev", "1", overflow}} {
			t.Run(op+"/"+number.name, func(t *testing.T) {
				body := ""
				if op == "reject" {
					body = `{"reason":"需要修改"}`
				}
				path := fmt.Sprintf("/console/v1/templates/%s/versions/%s/%s?rev=%s", tpl.ID, number.version, op, number.rev)
				authoringErrorEnvelope(t, httpCall(s, "POST", path, body, token), 400, model.ConsoleBadRequest)
			})
		}
	}
	for _, number := range []struct{ name, version, rev string }{{"version", overflow, "1"}, {"rev", "1", overflow}, {"both", overflow, overflow}} {
		t.Run("blank-reason/"+number.name, func(t *testing.T) {
			path := fmt.Sprintf("/console/v1/templates/%s/versions/%s/reject?rev=%s", tpl.ID, number.version, number.rev)
			authoringErrorEnvelope(t, httpCall(s, "POST", path, `{"reason":" \t "}`, token), 400, model.ConsoleRejectReason)
		})
	}
	stored, err := st.GetTemplate(t.Context(), model.TemplateKey{ID: tpl.ID, Version: tpl.Version})
	if err != nil || !reflect.DeepEqual(stored, tpl) {
		t.Fatal("无效输入修改了模板", stored, err)
	}
	after, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil || !reflect.DeepEqual(before.Items, after.Items) {
		t.Fatal("无效输入写了审计", after, err)
	}
}
