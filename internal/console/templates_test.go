package console

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lober/lober/internal/builtin"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

func Test模板草稿的嵌套内容与实际引用诊断(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	hook := approvedTestHook(t, st, "fragment")
	body := fmt.Sprintf(`{"name":"base","author":"spoof","family":"linux","series":["rocky"],"body":"{{slot:partition}}\n{{lober:syslog}}\n{{lober:cleanup}}\n","params":[{"name":"mode","type":"enum","options":[{"value":"yes","label":"启用"}]}],"bindings":{"mode":{"source":"fixed","value":"yes","editable":false}},"assembly":{"partition":[{"hookID":%q,"hookVersion":1,"timeoutSec":0,"required":false,"bindings":{}}]}}`, hook.ID)
	w := httpCall(s, "POST", "/console/v1/templates", body, token)
	tpl := responseData[templateResponse](t, w)
	if tpl.Author != "admin" || tpl.State != model.VersionDraft || tpl.DerivedFrom != nil || tpl.Rev != 1 {
		t.Fatal(tpl)
	}
	for _, p := range tpl.Problems {
		if p.Code == "hook_ref_missing" {
			t.Fatal("existing pinned hook falsely missing", p)
		}
	}
	for _, word := range []string{`"hookID"`, `"timeoutSec":0`, `"required":false`, `"options":[{"value":"yes","label":"启用"}]`} {
		if !strings.Contains(w.Body.String(), word) {
			t.Fatal(w.Body.String())
		}
	}
	back, err := st.GetTemplate(t.Context(), model.TemplateKey{ID: tpl.ID, Version: 1})
	if err != nil || back.Assembly["partition"][0].Required == nil || *back.Assembly["partition"][0].Required || back.Params[0].Options[0].Label != "启用" {
		t.Fatal(back, err)
	}
	path := "/console/v1/templates/" + tpl.ID + "/versions/1"
	tpl = responseData[templateResponse](t, httpCall(s, "PUT", path, `{"rev":1,"family":"","series":[],"body":"unfinished","assembly":{"missing":[{"hookID":"","hookVersion":0}]}}`, token))
	if tpl.Rev != 2 || tpl.Family != "" || tpl.Params == nil || tpl.Bindings == nil || len(tpl.Problems) == 0 {
		t.Fatal(tpl)
	}
	if w := httpCall(s, "PUT", path, `{"rev":1}`, token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := httpCall(s, "PUT", path, `{}`, token); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	responseData[templateResponse](t, httpCall(s, "GET", path, "", token))
}
func Test模板派生版本与副本独立(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	seedBuiltinContent(t, st)
	original := builtin.Templates()[0]
	originalKey := model.TemplateKey{ID: original.ID, Version: original.Version}
	tpl := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates/"+original.ID+"/derive", fmt.Sprintf(`{"from":%d,"name":"copy"}`, original.Version), token))
	if tpl.ID == original.ID || tpl.DerivedFrom == nil || tpl.DerivedFrom.ID != original.ID || tpl.DerivedFrom.Version != original.Version || tpl.Author != "admin" || tpl.State != model.VersionDraft || tpl.Builtin || tpl.Approver != "" {
		t.Fatal(tpl)
	}
	base := "/console/v1/templates/" + tpl.ID
	responseData[templateResponse](t, httpCall(s, "PUT", base+"/versions/1", `{"rev":1,"body":"changed","derivedFrom":{"id":"spoof","version":9}}`, token))
	derived, err := st.GetTemplate(t.Context(), model.TemplateKey{ID: tpl.ID, Version: 1})
	if err != nil || derived.DerivedFrom == nil || *derived.DerivedFrom != originalKey {
		t.Fatal(derived, err)
	}
	unchanged, err := st.GetTemplate(t.Context(), originalKey)
	if err != nil || unchanged.Body != original.Body {
		t.Fatal("source mutated", err)
	}
	// An unfinished draft prevents creating another; editing stays unaudited.
	if w := httpCall(s, "POST", base+"/versions?from=1", "", token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	responseData[any](t, httpCall(s, "PUT", base+"/name", `{"name":"renamed-copy"}`, token))
	responseData[any](t, httpCall(s, "DELETE", base+"/versions/1?rev=2", "", token))
	audit, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	count := 0
	for _, a := range audit.Items {
		if a.TargetKind == "template" {
			if a.Action != "template.rename" || a.Actor != "admin" {
				t.Fatal(a)
			}
			count++
		}
	}
	if count != 1 {
		t.Fatal(count)
	}
}
func Test模板列表过滤页型与只取当前已审批版本(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	seedBuiltinContent(t, st)
	a := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates", `{"name":"a","family":"linux","series":["rocky"]}`, token))
	responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates", `{"name":"b","family":"linux","series":["debian"]}`, token))
	p := responseData[templatePage](t, httpCall(s, "GET", "/console/v1/templates?prefix=a&series=rocky&state=draft", "", token))
	if len(p.Items) != 1 || p.Items[0].ID != a.ID {
		t.Fatal(p)
	}
	vs := responseData[templateVersionPage](t, httpCall(s, "GET", "/console/v1/templates/"+a.ID+"/versions", "", token))
	if len(vs.Items) != 1 || vs.Items[0].ID != a.ID {
		t.Fatal(vs)
	}
	p = responseData[templatePage](t, httpCall(s, "GET", "/console/v1/templates?approved=true&limit=1", "", token))
	if len(p.Items) != 1 || p.Items[0].State != model.VersionApproved || p.Next == "" {
		t.Fatal(p)
	}
	p = responseData[templatePage](t, httpCall(s, "GET", "/console/v1/templates?prefix=no-such-name", "", token))
	if p.Items == nil || len(p.Items) != 0 {
		t.Fatal(p)
	}
	for _, q := range []string{"state=bad", "series=bad", "limit=-1", "approved=no"} {
		if w := httpCall(s, "GET", "/console/v1/templates?"+q, "", token); w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
	for _, body := range []string{`{"name":"lober:mine"}`, `{"name":"x","body":"\u0000"}`} {
		if w := httpCall(s, "POST", "/console/v1/templates", body, token); w.Code != 422 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func Test模板新版本复制完整来源并不重发序号(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	seedBuiltinContent(t, st)
	original := builtin.Templates()[0]
	original.ID, original.Version, original.Rev = "", 0, 0
	original.Name, original.Author, original.Approver, original.State, original.Builtin = "versioned", "alice", "", "", false
	tpl, err := st.CreateTemplate(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	key := model.TemplateKey{ID: tpl.ID, Version: 1}
	if err := st.SubmitTemplate(t.Context(), key, 1, store.Audit{Actor: "alice", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", store.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	base := "/console/v1/templates/" + tpl.ID
	next := responseData[templateResponse](t, httpCall(s, "POST", base+"/versions?from=1", "", token))
	if next.Version != 2 || next.Author != "admin" || next.Approver != "" || next.State != model.VersionDraft || next.Body != original.Body || len(next.Assembly) != len(original.Assembly) {
		t.Fatal(next)
	}
	responseData[any](t, httpCall(s, "DELETE", base+"/versions/2?rev=1", "", token))
	next = responseData[templateResponse](t, httpCall(s, "POST", base+"/versions?from=1", "", token))
	if next.Version != 3 {
		t.Fatal(next)
	}
	versions := responseData[templateVersionPage](t, httpCall(s, "GET", base+"/versions", "", token))
	if len(versions.Items) != 2 || versions.Items[0].Version != 1 || versions.Items[1].Version != 3 {
		t.Fatal(versions)
	}
	after, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if len(before.Items) != len(after.Items) {
		t.Fatal("version/delete draft audited")
	}
}
func Test模板引用保护Hook草稿删除(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	h := responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks", `{"name":"used"}`, token))
	tpl := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates", fmt.Sprintf(`{"name":"uses-hook","body":"{{slot:x}}\n","assembly":{"x":[{"hookID":%q,"hookVersion":1}]}}`, h.ID), token))
	hookPath := "/console/v1/hooks/" + h.ID + "/versions/1?rev=1"
	if w := httpCall(s, "DELETE", hookPath, "", token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	responseData[any](t, httpCall(s, "DELETE", "/console/v1/templates/"+tpl.ID+"/versions/1?rev=1", "", token))
	responseData[any](t, httpCall(s, "DELETE", hookPath, "", token))
}
