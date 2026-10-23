package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

func hostContractError(t *testing.T, w *httptest.ResponseRecorder, status int, problem string) {
	t.Helper()
	var out struct {
		Code    int             `json:"code"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != status || out.Code != status {
		t.Fatalf("want %d: %d %s", status, w.Code, w.Body.String())
	}
	if problem != "" {
		var detail problemList
		if err := json.Unmarshal(out.Data, &detail); err != nil {
			t.Fatal(err)
		}
		if len(detail.Problems) != 1 || string(detail.Problems[0].Code) != problem || detail.Problems[0].Subject.Kind != "host" || detail.Problems[0].Message == "" || detail.Problems[0].Next == "" || out.Message != detail.Problems[0].Message {
			t.Fatal(w.Body.String())
		}
		return
	}
	wantMessage := map[int]string{400: model.ConsoleBadRequest, 403: model.ConsoleForbidden, 404: model.ConsoleNotFound, 500: model.ConsoleInternal}[status]
	if string(out.Data) != "null" || out.Message != wantMessage {
		t.Fatal(w.Body.String())
	}
}

// 固定手写的入口集合，防止迁移时漏路由、改权限或错记审计动作。
func Test主机六入口契约(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	want := map[string][2]string{
		"GET /console/v1/hosts":         {"host:read", ""},
		"GET /console/v1/hosts/:sn":     {"host:read", ""},
		"POST /console/v1/hosts":        {"host:write", "host.create"},
		"PUT /console/v1/hosts/:sn":     {"host:write", "host.update"},
		"DELETE /console/v1/hosts/:sn":  {"host:write", "host.delete"},
		"POST /console/v1/hosts/import": {"host:write", "host.import"},
	}
	got := map[string][2]string{}
	for _, rt := range s.Routes() {
		if rt.Path == "/console/v1/hosts" || strings.HasPrefix(rt.Path, "/console/v1/hosts/") {
			got[rt.Method+" "+rt.Path] = [2]string{rt.Perm, rt.Audit}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes: got %#v, want %#v", got, want)
	}
	roles := []model.Role{model.RoleAdmin, model.RoleOperator, model.RoleViewer, model.RoleHookAuthor, model.RoleHookApprover}
	for i, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			name := fmt.Sprintf("host_role_%d", i)
			token, _ := seedHTTPUser(t, s, st, name, role)
			sn := fmt.Sprintf("ROLE-%d", i)
			if _, err := st.CreateHost(t.Context(), model.Host{SN: sn}, repository.Audit{Actor: "test", Action: "host.create", At: fixedAt}); err != nil {
				t.Fatal(err)
			}
			page := responseData[hostPage](t, httpCall(s, "GET", "/console/v1/hosts?prefix="+sn, "", token))
			if len(page.Items) != 1 || page.Items[0].SN != sn || page.Items[0].Rev != 1 {
				t.Fatal(page)
			}
			h := responseData[hostResponse](t, httpCall(s, "GET", "/console/v1/hosts/"+sn, "", token))
			if h.SN != sn || h.Rev != 1 || h.Attrs == nil {
				t.Fatal(h)
			}
			allowed := role == model.RoleAdmin || role == model.RoleOperator
			for _, input := range []struct{ method, path, body string }{
				{"POST", "/console/v1/hosts", fmt.Sprintf(`{"sn":%q}`, sn+"-NEW")},
				{"PUT", "/console/v1/hosts/" + sn, `{"rev":1}`},
				{"POST", "/console/v1/hosts/import?dryRun=true", "sn\n" + sn + "-IMPORT\n"},
				{"DELETE", "/console/v1/hosts/" + sn + "?rev=2", ""},
			} {
				w := httpCall(s, input.method, input.path, input.body, token)
				if !allowed {
					hostContractError(t, w, 403, "")
					continue
				}
				if strings.Contains(input.path, "/import?") {
					plan := responseData[csvPlan](t, w)
					if plan.Total != 1 || len(plan.Rows) != 1 || plan.Rows[0].SN != sn+"-IMPORT" || plan.Rows[0].Action != "create" || plan.Rows[0].Problems == nil || len(plan.Rows[0].Problems) != 0 {
						t.Fatal(plan)
					}
				} else if input.method == "DELETE" {
					if responseData[any](t, w) != nil {
						t.Fatal(w.Body.String())
					}
				} else {
					h := responseData[hostResponse](t, w)
					wantSN, wantRev := sn+"-NEW", int64(1)
					if input.method == "PUT" {
						wantSN, wantRev = sn, 2
					}
					if h.SN != wantSN || h.Rev != wantRev || h.Attrs == nil {
						t.Fatal(h)
					}
				}
			}
			if !allowed {
				h, err := st.GetHost(t.Context(), sn)
				if err != nil || h.Rev != 1 {
					t.Fatal(h, err)
				}
				if _, err := st.GetHost(t.Context(), sn+"-NEW"); !errors.Is(err, repository.ErrNotFound) {
					t.Fatal(err)
				}
			}
		})
	}
	audits, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, a := range audits.Items {
		if strings.HasPrefix(a.Actor, "host_role_") {
			if a.Actor != "host_role_0" && a.Actor != "host_role_1" || a.At != fixedAt || a.TargetKind != "host" || a.Detail != "" {
				t.Fatal(a)
			}
			counts[a.Action]++
		}
	}
	if !reflect.DeepEqual(counts, map[string]int{"host.create": 2, "host.update": 2, "host.delete": 2}) {
		t.Fatal(counts)
	}
}

// 同一请求有多个错误时，先后顺序也是网页约定。
func Test主机输入多错保持原顺序(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "host_input", model.RoleAdmin)
	cases := []struct {
		rev         int64
		path        string
		wantHTTP    int
		wantProblem string
	}{
		{0, "ſn-1", 400, ""},
		{1, "ſn-1", 422, "sn_invalid"},
	}
	for _, tc := range cases {
		w := httpCall(s, "PUT", "/console/v1/hosts/"+tc.path, fmt.Sprintf(`{"rev":%d,"sn":"OTHER"}`, tc.rev), token)
		hostContractError(t, w, tc.wantHTTP, tc.wantProblem)
	}
	hostContractError(t, httpCall(s, "DELETE", "/console/v1/hosts/ſn-1?rev=9223372036854775808", "", token), 400, "")
	hostContractError(t, httpCall(s, "DELETE", "/console/v1/hosts/ſn-1?rev=1", "", token), 422, "sn_invalid")
	hostContractError(t, httpCall(s, "PUT", "/console/v1/hosts/SN-1", `{"rev":1,"sn":"SN-2"}`, token), 400, "")
	hostContractError(t, httpCall(s, "POST", "/console/v1/hosts", `{"sn":"","rev":-9}`, token), 422, "sn_invalid")
	for i, rev := range []int64{-9, 0, 77} {
		h := responseData[hostResponse](t, httpCall(s, "POST", "/console/v1/hosts", fmt.Sprintf(`{"sn":"REV-%d","rev":%d}`, i, rev), token))
		if h.Rev != 1 || h.SN != fmt.Sprintf("REV-%d", i) {
			t.Fatal(h)
		}
	}
}

func Test主机列表保留两种分页与原文过滤(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "host_list", model.RoleViewer)
	for _, sn := range []string{"SN-2", "SN-10", "SN-1"} {
		attrs := map[string]string{"rack": " A01 "}
		if sn == "SN-2" {
			attrs["rack"] = "A01"
		}
		if _, err := st.CreateHost(t.Context(), model.Host{SN: sn, Attrs: attrs}, repository.Audit{Actor: "test", Action: "host.create", At: fixedAt}); err != nil {
			t.Fatal(err)
		}
	}
	first := responseData[hostPage](t, httpCall(s, "GET", "/console/v1/hosts?limit=2", "", token))
	if len(first.Items) != 2 || first.Items[0].SN != "SN-1" || first.Items[1].SN != "SN-10" || first.Next == "" || first.Total != nil {
		t.Fatal(first)
	}
	last := responseData[hostPage](t, httpCall(s, "GET", "/console/v1/hosts?limit=2&cursor="+first.Next, "", token))
	if len(last.Items) != 1 || last.Items[0].SN != "SN-2" || last.Next != "" || last.Total != nil {
		t.Fatal(last)
	}
	page := responseData[hostPage](t, httpCall(s, "GET", "/console/v1/hosts?page=2&page_size=2", "", token))
	if page.Total == nil || *page.Total != 3 || len(page.Items) != 1 || page.Items[0].SN != "SN-2" || page.Next != "" {
		t.Fatal(page)
	}
	for _, q := range []string{"prefix=sn-1", "prefix=NONE", "attr_key=rack&attr_value=%20A01%20", "attr_key=rack&attr_value=A01", "attr_key=invalid-key&attr_value=anything"} {
		w := httpCall(s, "GET", "/console/v1/hosts?"+q, "", token)
		page := responseData[hostPage](t, w)
		want := []string{}
		if strings.Contains(q, "%20A01%20") {
			want = []string{"SN-1", "SN-10"}
		}
		if q == "attr_key=rack&attr_value=A01" {
			want = []string{"SN-2"}
		}
		got := make([]string, len(page.Items))
		for i, h := range page.Items {
			got[i] = h.SN
		}
		if !reflect.DeepEqual(got, want) || page.Items == nil || strings.Contains(w.Body.String(), `"total"`) {
			t.Fatal(q, w.Body.String())
		}
	}
	for _, q := range []string{"page=1&cursor=", "page=1&limit=", "page_size=2&cursor=", "page=", "page=1&page=2", "page=9223372036854775808", "page=9223372036854775807&page_size=2", "page_size=9223372036854775808", "limit=9223372036854775808"} {
		hostContractError(t, httpCall(s, "GET", "/console/v1/hosts?"+q, "", token), 400, "")
	}
}

func Test主机CSV预演与失败优先级(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "host_csv", model.RoleAdmin)
	responseData[hostResponse](t, httpCall(s, "POST", "/console/v1/hosts", `{"sn":"SN-1","attrs":{"rack":"old"}}`, token))
	badCSV := "sn,arch\nSN-2,x86_64\nSN-3\n"
	for _, query := range []string{"dryRun=no", "dryRun=", "dryRun=TRUE", "dryRun=1", "dryRun=no&dryRun=true"} {
		hostContractError(t, csvHTTP(s, "/console/v1/hosts/import?"+query, badCSV, token), 400, "")
	}
	w := csvHTTP(s, "/console/v1/hosts/import?dryRun=true", badCSV, token)
	var fileError struct {
		Code    int             `json:"code"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &fileError); err != nil {
		t.Fatal(err)
	}
	if w.Code != 400 || fileError.Code != 400 || string(fileError.Data) != "null" || !strings.HasPrefix(fileError.Message, "CSV 读不成：") {
		t.Fatal(w.Code, w.Body.String())
	}
	data := "sn,arch,rack\nSN-1,,new\nSN-2,x86_64, A01 \nSN-3,amd64,bad\n"
	closed := pgtest.OpenStore(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	s.server.deps.Hosts = closed
	for _, query := range []string{"dryRun=true", "dryRun=false"} {
		hostContractError(t, csvHTTP(s, "/console/v1/hosts/import?"+query, data, token), 500, "")
	}
	s.server.deps.Hosts = st
	before, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	plan := responseData[csvPlan](t, csvHTTP(s, "/console/v1/hosts/import?dryRun=true&dryRun=no", data, token))
	if plan.Total != 3 || len(plan.Rows) != 3 || plan.Rows[0].Row != 2 || plan.Rows[0].SN != "SN-1" || plan.Rows[0].Action != "update" || plan.Rows[1].Row != 3 || plan.Rows[1].SN != "SN-2" || plan.Rows[1].Action != "create" || plan.Rows[2].Row != 4 || plan.Rows[2].Action != "error" || len(plan.Rows[2].Problems) != 1 || plan.Rows[2].Problems[0].Code != "host_platform_invalid" {
		t.Fatal(plan)
	}
	for _, query := range []string{"", "?dryRun=false&dryRun=true"} {
		w := csvHTTP(s, "/console/v1/hosts/import"+query, data, token)
		var rejected struct {
			Code    int     `json:"code"`
			Data    csvPlan `json:"data"`
			Message string  `json:"message"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &rejected); err != nil {
			t.Fatal(err)
		}
		if w.Code != 422 || rejected.Code != 422 || !reflect.DeepEqual(rejected.Data, plan) || rejected.Message != plan.Rows[2].Problems[0].Message {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	h, err := st.GetHost(t.Context(), "SN-1")
	if err != nil || h.Rev != 1 || h.Attrs["rack"] != "old" {
		t.Fatal(h, err)
	}
	for _, sn := range []string{"SN-2", "SN-3"} {
		if _, err := st.GetHost(t.Context(), sn); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal(sn, err)
		}
	}
	after, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil || !reflect.DeepEqual(before.Items, after.Items) {
		t.Fatal("预演或拒绝修改了审计", err)
	}
}
