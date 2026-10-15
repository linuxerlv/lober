package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

func responseData[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var e struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	return e.Data
}
func csvHTTP(s *Server, path, data, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(data))
	req.Header.Set("Content-Type", "text/csv")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, req)
	return w
}
func Test主机HTTP登记替换版本删除与审计(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	h := responseData[hostResponse](t, httpCall(s, "POST", "/console/v1/hosts", `{"sn":" sn-1 ","arch":"x86_64","firmware":"uefi","attrs":{"rack":" A01 "}}`, token))
	if h.SN != "SN-1" || h.Rev != 1 || h.Attrs["rack"] != " A01 " {
		t.Fatal(h)
	}
	responseData[hostResponse](t, httpCall(s, "GET", "/console/v1/hosts/sn-1", "", token))
	if w := httpCall(s, "POST", "/console/v1/hosts", `{"sn":"SN-1"}`, token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{"rev":0}`, `{"rev":1,"sn":"SN-2"}`, `{`} {
		if w := httpCall(s, "PUT", "/console/v1/hosts/sn-1", body, token); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	h = responseData[hostResponse](t, httpCall(s, "PUT", "/console/v1/hosts/sn-1", `{"rev":1}`, token))
	if h.SN != "SN-1" || h.Rev != 2 || h.Arch != "" || h.Firmware != "" || h.Attrs == nil || len(h.Attrs) != 0 {
		t.Fatal(h)
	}
	if w := httpCall(s, "PUT", "/console/v1/hosts/SN-1", `{"rev":1}`, token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, query := range []string{"", "?rev=bad", "?rev=0", "?rev=-1"} {
		if w := httpCall(s, "DELETE", "/console/v1/hosts/SN-1"+query, "", token); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := httpCall(s, "DELETE", "/console/v1/hosts/SN-1?rev=1", "", token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	responseData[any](t, httpCall(s, "DELETE", "/console/v1/hosts/sn-1?rev=2", "", token))
	if _, err := st.GetHost(t.Context(), "SN-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if w := httpCall(s, "GET", "/console/v1/hosts/SN-1", "", token); w.Code != 404 {
		t.Fatal(w.Code)
	}
	audit, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, a := range audit.Items {
		if a.TargetKind == "host" {
			if a.Actor != "admin" || a.At != fixedAt || a.TargetID != "SN-1" {
				t.Fatal(a)
			}
			counts[a.Action]++
		}
	}
	if counts["host.create"] != 1 || counts["host.update"] != 1 || counts["host.delete"] != 1 {
		t.Fatal(counts)
	}
}
func Test主机HTTP校验列表过滤分页(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	for _, body := range []string{`{"sn":""}`, `{"sn":"SN-1","arch":"amd64","firmware":"UEFI","attrs":{"sn":"x"}}`, `{"sn":"SN-1","attrs":{"rack":"\u0000"}}`} {
		w := httpCall(s, "POST", "/console/v1/hosts", body, token)
		if w.Code != 422 || !strings.Contains(w.Body.String(), `"problems"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, sn := range []string{"SN-2", "SN-10", "SN-1"} {
		responseData[hostResponse](t, httpCall(s, "POST", "/console/v1/hosts", fmt.Sprintf(`{"sn":%q,"attrs":{"rack":"A01"}}`, sn), token))
	}
	page := responseData[hostPage](t, httpCall(s, "GET", "/console/v1/hosts?limit=2", "", token))
	if len(page.Items) != 2 || page.Items[0].SN != "SN-1" || page.Items[1].SN != "SN-10" || page.Next == "" {
		t.Fatal(page)
	}
	last := responseData[hostPage](t, httpCall(s, "GET", "/console/v1/hosts?cursor="+page.Next+"&limit=2", "", token))
	if len(last.Items) != 1 || last.Items[0].SN != "SN-2" {
		t.Fatal(last)
	}
	page = responseData[hostPage](t, httpCall(s, "GET", "/console/v1/hosts?prefix=SN-1&attr_key=rack&attr_value=A01", "", token))
	if len(page.Items) != 2 {
		t.Fatal(page)
	}
	page = responseData[hostPage](t, httpCall(s, "GET", "/console/v1/hosts?prefix=NONE", "", token))
	if page.Items == nil || len(page.Items) != 0 {
		t.Fatal(page)
	}
	for _, q := range []string{"limit=no", "limit=-1", "limit=501", "attr_key=rack"} {
		if w := httpCall(s, "GET", "/console/v1/hosts?"+q, "", token); w.Code != 400 {
			t.Fatal(q, w.Code, w.Body.String())
		}
	}
}
func Test主机HTTP全部路由权限矩阵(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	for i, role := range store.Roles() {
		name := fmt.Sprintf("u%d", i)
		seedAuthUser(t, st, name, role, "pw")
		token := loginHTTP(t, s, name, "pw").Token
		count := 0
		for _, rt := range s.Routes() {
			if rt.Perm != PermHostRead && rt.Perm != PermHostWrite {
				continue
			}
			count++
			path := strings.ReplaceAll(rt.Path, ":sn", "SN-1")
			w := httpCall(s, rt.Method, path, `{}`, token)
			allowed := rt.Perm == PermHostRead || role == store.RoleAdmin || role == store.RoleOperator
			if allowed && (w.Code == 401 || w.Code == 403) || !allowed && w.Code != 403 {
				t.Fatal(role, rt, w.Code)
			}
		}
		if count != 6 {
			t.Fatal("host routes missing", count)
		}
	}
}
func TestCSV导入预演与原子提交(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	responseData[hostResponse](t, httpCall(s, "POST", "/console/v1/hosts", `{"sn":"SN-1","arch":"x86_64"}`, token))
	before, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	data := "sn,arch,rack\r\nSN-1,aarch64, A02 \r\nSN-2,x86_64,A01\r\n"
	p := responseData[csvPlan](t, csvHTTP(s, "/console/v1/hosts/import?dryRun=true", data, token))
	if p.Rows[0].Action != "update" || p.Rows[1].Action != "create" {
		t.Fatal(p)
	}
	after, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if len(before.Items) != len(after.Items) {
		t.Fatal("preview audited")
	}
	if _, err := st.GetHost(t.Context(), "SN-2"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	for _, bad := range []string{"sn,arch\nSN-1,x86_64\nSN-3,amd64\nSN-4,x86_64\n", "sn,arch\nSN-3,x86_64\nsn-3,x86_64\n"} {
		p := responseData[csvPlan](t, csvHTTP(s, "/console/v1/hosts/import?dryRun=true", bad, token))
		if p.Rows[1].Action != "error" {
			t.Fatal(p)
		}
		w := csvHTTP(s, "/console/v1/hosts/import", bad, token)
		if w.Code != 422 || !strings.Contains(w.Body.String(), `"rows"`) {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, sn := range []string{"SN-3", "SN-4"} {
			if _, err := st.GetHost(t.Context(), sn); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("partial import", sn, err)
			}
		}
	}
	h, _ := st.GetHost(t.Context(), "SN-1")
	after, _ = st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if h.Rev != 1 || len(before.Items) != len(after.Items) {
		t.Fatal("rejected batch changed records/audit")
	}
	page := responseData[hostPage](t, csvHTTP(s, "/console/v1/hosts/import", data, token))
	if len(page.Items) != 2 {
		t.Fatal(page)
	}
	h, err := st.GetHost(t.Context(), "SN-1")
	if err != nil || h.Arch != "aarch64" || h.Attrs["rack"] != " A02 " || h.Rev != 2 {
		t.Fatal(h, err)
	}
	after, _ = st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	count := 0
	for _, a := range after.Items {
		if a.Action == "host.import" {
			if a.Actor != "admin" || a.TargetKind != "host" {
				t.Fatal(a)
			}
			count++
		}
	}
	if count != 2 {
		t.Fatal(count)
	}
	for _, bad := range []string{"sn\n", "arch\nx86_64\n", "sn,arch\nSN-3\n"} {
		if w := csvHTTP(s, "/console/v1/hosts/import", bad, token); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := csvHTTP(s, "/console/v1/hosts/import?dryRun=no", data, token); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func Test主机OpenAPI导入媒体与两种结果(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	paths := s.OpenAPI()["paths"].(map[string]any)
	op := paths["/console/v1/hosts/import"].(map[string]any)["post"].(map[string]any)
	req := op["requestBody"].(map[string]any)["content"].(map[string]any)
	if req["text/csv"] == nil {
		t.Fatal(req)
	}
	encoded, _ := json.Marshal(op)
	for _, part := range []string{"CsvPlan", "HostPage", "oneOf", "dryRun"} {
		if !strings.Contains(string(encoded), part) {
			t.Fatal(string(encoded))
		}
	}
}

func TestCSV预演数据库失败不伪装新建(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	closed := pgtest.OpenStore(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	s.server.deps.Hosts = closed
	w := csvHTTP(s, "/console/v1/hosts/import?dryRun=true", "sn\nSN-1\n", token)
	if w.Code != 500 || strings.Contains(w.Body.String(), "backend unavailable") || strings.Contains(w.Body.String(), `"create"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
