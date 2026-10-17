package console

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/lober/lober/internal/builtin"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

func seedBuiltinContent(t *testing.T, st store.Store) {
	t.Helper()
	for _, h := range builtin.Hooks() {
		if err := st.EnsureBuiltinHook(t.Context(), h); err != nil {
			t.Fatal(err)
		}
	}
	for _, tpl := range builtin.Templates() {
		if err := st.EnsureBuiltinTemplate(t.Context(), tpl); err != nil {
			t.Fatal(err)
		}
	}
}
func approvedTestHook(t *testing.T, st store.Store, name string) model.Hook {
	t.Helper()
	h, err := st.CreateHook(t.Context(), model.Hook{Name: name, Author: "alice", Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment, Body: "# example\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SubmitHook(t.Context(), h.Key(), h.Rev, store.Audit{Actor: "alice", Action: "hook.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveHook(t.Context(), h.Key(), h.Rev+1, "reviewer", store.Audit{Actor: "reviewer", Action: "hook.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	h, err = st.GetHook(t.Context(), h.Key())
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func TestHook草稿可保存不完备内容并保留元数据(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	before, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	w := httpCall(s, "POST", "/console/v1/hooks", `{"name":" draft ","author":"spoof","state":"approved","builtin":true,"timeoutSec":0,"params":[{"name":"disk","type":"diskpath","label":"系统盘"}],"impls":[]}`, token)
	h := responseData[hookResponse](t, w)
	if h.Name != "draft" || h.State != model.VersionDraft || h.Author != "admin" || h.Builtin || h.Rev != 1 || h.Version != 1 || len(h.Problems) == 0 || h.Impls == nil {
		t.Fatal(h)
	}
	if strings.Contains(w.Body.String(), `"Params"`) || strings.Contains(w.Body.String(), `"Code"`) || !strings.Contains(w.Body.String(), `"type":"diskpath"`) {
		t.Fatal(w.Body.String())
	}
	path := fmt.Sprintf("/console/v1/hooks/%s/versions/1", h.ID)
	h = responseData[hookResponse](t, httpCall(s, "PUT", path, `{"rev":1,"description":"changed","author":"other","approver":"spoof"}`, token))
	if h.Author != "admin" || h.Approver != "" || h.Name != "draft" || h.Description != "changed" || h.Rev != 2 || h.Params == nil || len(h.Params) != 0 {
		t.Fatal(h)
	}
	if w := httpCall(s, "PUT", path, `{"rev":1,"description":"stale"}`, token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{}`, `{"rev":0}`, `{"rev":2,"name":"renamed"}`} {
		if w := httpCall(s, "PUT", path, body, token); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	responseData[hookResponse](t, httpCall(s, "GET", path, "", token))
	vs := responseData[versionPage](t, httpCall(s, "GET", "/console/v1/hooks/"+h.ID+"/versions", "", token))
	if len(vs.Items) != 1 || vs.Items[0].Version != 1 {
		t.Fatal(vs)
	}
	after, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if len(before.Items) != len(after.Items) {
		t.Fatal("draft edit audited")
	}
}
func TestHook名称文本校验列表分页改名与删除(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	for _, body := range []string{`{"name":"lober:mine"}`, `{"name":"有空格 x"}`, `{"name":""}`, `{"name":"x","impls":[{"body":"a\u0000b"}]}`} {
		if w := httpCall(s, "POST", "/console/v1/hooks", body, token); w.Code != 422 || !strings.Contains(w.Body.String(), `"problems"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	var last hookResponse
	for _, name := range []string{"network", "lvm", "storage"} {
		last = responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks", fmt.Sprintf(`{"name":%q}`, name), token))
	}
	if w := httpCall(s, "POST", "/console/v1/hooks", `{"name":"LVM"}`, token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	p := responseData[hookPage](t, httpCall(s, "GET", "/console/v1/hooks?limit=2", "", token))
	if len(p.Items) != 2 || p.Items[0].Name != "lvm" || p.Items[1].Name != "network" || p.Next == "" {
		t.Fatal(p)
	}
	p = responseData[hookPage](t, httpCall(s, "GET", "/console/v1/hooks?cursor="+p.Next+"&limit=2", "", token))
	if len(p.Items) != 1 || p.Items[0].Name != "storage" {
		t.Fatal(p)
	}
	p = responseData[hookPage](t, httpCall(s, "GET", "/console/v1/hooks?prefix=NET&state=draft", "", token))
	if len(p.Items) != 1 {
		t.Fatal(p)
	}
	p = responseData[hookPage](t, httpCall(s, "GET", "/console/v1/hooks?approved=true", "", token))
	if p.Items == nil || len(p.Items) != 0 {
		t.Fatal(p)
	}
	for _, q := range []string{"limit=bad", "state=bad", "approved=bad"} {
		if w := httpCall(s, "GET", "/console/v1/hooks?"+q, "", token); w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
	responseData[any](t, httpCall(s, "PUT", "/console/v1/hooks/"+last.ID+"/name", `{"name":"disks"}`, token))
	responseData[any](t, httpCall(s, "DELETE", "/console/v1/hooks/"+last.ID+"/versions/1?rev=1", "", token))
	if _, err := st.GetHook(t.Context(), model.HookKey{ID: last.ID, Version: 1}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	audit, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	count := 0
	for _, a := range audit.Items {
		if a.TargetKind == "hook" {
			if a.Action != "hook.rename" || a.Actor != "admin" {
				t.Fatal(a)
			}
			count++
		}
	}
	if count != 1 {
		t.Fatal(count)
	}
}
func TestHook新版本约束序号与并发修订(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	original := approvedTestHook(t, st, "example")
	base := "/console/v1/hooks/" + original.ID
	for _, q := range []string{"", "?from=no", "?from=0"} {
		if w := httpCall(s, "POST", base+"/versions"+q, "", token); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	next := responseData[hookResponse](t, httpCall(s, "POST", base+"/versions?from=1", "", token))
	if next.Version != 2 || next.Author != "admin" || next.Approver != "" || next.State != model.VersionDraft || next.Impls[0].Body != "# example\n" {
		t.Fatal(next)
	}
	if w := httpCall(s, "POST", base+"/versions?from=1", "", token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes <- httpCall(s, "PUT", base+"/versions/2", fmt.Sprintf(`{"rev":1,"description":"edit%d"}`, i), token).Code
		}(i)
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	responseData[any](t, httpCall(s, "DELETE", base+"/versions/2?rev=2", "", token))
	next = responseData[hookResponse](t, httpCall(s, "POST", base+"/versions?from=1", "", token))
	if next.Version != 3 {
		t.Fatal(next)
	}
	for _, version := range []string{"no", "0", "-1"} {
		if w := httpCall(s, "GET", base+"/versions/"+version, "", token); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
func Test内置Hook与模板只读且草稿操作不声明审计(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	seedBuiltinContent(t, st)
	for _, kind := range []string{"hooks", "templates"} {
		id := builtin.Hooks()[0].ID
		version := builtin.Hooks()[0].Version
		if kind == "templates" {
			id = builtin.Templates()[0].ID
			version = builtin.Templates()[0].Version
		}
		base := "/console/v1/" + kind + "/" + id
		for _, item := range [][3]string{{"PUT", base + "/name", `{"name":"changed"}`}, {"POST", fmt.Sprintf("%s/versions?from=%d", base, version), ""}, {"PUT", fmt.Sprintf("%s/versions/%d", base, version), `{"rev":1}`}, {"DELETE", fmt.Sprintf("%s/versions/%d?rev=1", base, version), ""}} {
			if w := httpCall(s, item[0], item[1], item[2], token); w.Code != 409 {
				t.Fatal(kind, item, w.Code, w.Body.String())
			}
		}
	}
	for _, r := range s.Routes() {
		if r.Perm != PermHookWrite && r.Perm != PermTemplateWrite {
			continue
		}
		draft := r.Path == "/console/v1/hooks" || r.Path == "/console/v1/templates" || strings.HasSuffix(r.Path, "/versions") || strings.HasSuffix(r.Path, "/:version") || strings.HasSuffix(r.Path, "/derive")
		if draft && r.Audit != "" {
			t.Fatal("draft route advertises audit", r)
		}
	}
}
func Test作者对象全部路由权限矩阵(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	for i, role := range store.Roles() {
		name := fmt.Sprintf("author%d", i)
		seedAuthUser(t, st, name, role, "pw")
		token := loginHTTP(t, s, name, "pw").Token
		count := 0
		for _, rt := range s.Routes() {
			write := rt.Perm == PermHookWrite || rt.Perm == PermTemplateWrite
			read := rt.Perm == PermHookRead || rt.Perm == PermTemplateRead
			if !write && !read {
				continue
			}
			count++
			path := strings.ReplaceAll(strings.ReplaceAll(rt.Path, ":id", "missing"), ":version", "1")
			w := httpCall(s, rt.Method, path, `{}`, token)
			allowed := read || role == store.RoleAdmin || role == store.RoleHookAuthor
			if allowed && (w.Code == 401 || w.Code == 403) || !allowed && w.Code != 403 {
				t.Fatal(role, rt, w.Code)
			}
		}
		if count == 0 {
			t.Fatal(count)
		}
	}
}
