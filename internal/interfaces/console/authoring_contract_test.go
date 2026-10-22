package console

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

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
