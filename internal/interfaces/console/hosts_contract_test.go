package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/lober/lober/internal/application/audit"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/application/hosts"
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
	s.server.deps.HostRegistry = hosts.NewRegistry(hosts.Deps{Hosts: closed, Clock: s.server.deps.Clock})
	for _, query := range []string{"dryRun=true", "dryRun=false"} {
		hostContractError(t, csvHTTP(s, "/console/v1/hosts/import?"+query, data, token), 500, "")
	}
	s.server.deps.HostRegistry = hosts.NewRegistry(hosts.Deps{Hosts: st, Clock: s.server.deps.Clock})
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

// 与改密测试相同，只在仓储边界把审计操作者换成夹具的失败标记。
type failingHostAudit struct{ repository.Hosts }

func (p failingHostAudit) CreateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error) {
	a.Actor = pgtest.FaultActor
	return p.Hosts.CreateHost(ctx, h, a)
}
func (p failingHostAudit) UpdateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error) {
	a.Actor = pgtest.FaultActor
	return p.Hosts.UpdateHost(ctx, h, a)
}
func (p failingHostAudit) DeleteHost(ctx context.Context, sn string, rev int64, a repository.Audit) error {
	a.Actor = pgtest.FaultActor
	return p.Hosts.DeleteHost(ctx, sn, rev, a)
}

// 经真实网页与应用调用，让测试库的审计失败注入验证同一事务的回滚。
func Test主机应用审计失败与删除引用保护(t *testing.T) {
	t.Run("审计失败回滚整条主机", func(t *testing.T) {
		s, st, _ := newHTTPServer(t)
		token, _ := seedHTTPUser(t, s, st, "host_rollback", model.RoleAdmin)
		s.server.deps.HostRegistry = hosts.NewRegistry(hosts.Deps{Hosts: failingHostAudit{st}, Clock: s.server.deps.Clock})
		beforeHost, err := st.CreateHost(t.Context(), model.Host{SN: "ROLLBACK-1", Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{"rack": " A01 ", "owner": "old"}}, repository.Audit{Actor: "test", Action: "host.create", At: fixedAt})
		if err != nil {
			t.Fatal(err)
		}
		beforeAudit, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range []struct{ method, path, body string }{
			{"POST", "/console/v1/hosts", `{"sn":"ROLLBACK-NEW","arch":"aarch64","firmware":"uefi","attrs":{"rack":"new"}}`},
			{"PUT", "/console/v1/hosts/ROLLBACK-1", `{"rev":1,"attrs":{"rack":"changed"}}`},
			{"DELETE", "/console/v1/hosts/ROLLBACK-1?rev=1", ""},
		} {
			hostContractError(t, httpCall(s, r.method, r.path, r.body, token), 500, "")
			afterHost, err := st.GetHost(t.Context(), "ROLLBACK-1")
			if err != nil || !reflect.DeepEqual(beforeHost, afterHost) {
				t.Fatal("审计失败后主机或版本改变", afterHost, err)
			}
			if _, err := st.GetHost(t.Context(), "ROLLBACK-NEW"); !errors.Is(err, repository.ErrNotFound) {
				t.Fatal("失败创建留下记录", err)
			}
			afterAudit, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
			if err != nil || !reflect.DeepEqual(beforeAudit, afterAudit) {
				t.Fatal("失败操作留下审计", err)
			}
			got := responseData[hostResponse](t, httpCall(s, "GET", "/console/v1/hosts/ROLLBACK-1", "", token))
			if !reflect.DeepEqual(got, toHostResponse(beforeHost)) {
				t.Fatal(got)
			}
		}
	})
	t.Run("所有任务状态均阻止删除", func(t *testing.T) {
		s, st, token, task, _ := taskFixture(t)
		beforeHost, err := st.GetHost(t.Context(), task.HostSN)
		if err != nil {
			t.Fatal(err)
		}
		beforeAudit, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		for _, state := range model.TaskStates() {
			if state != task.State {
				task, _, err = st.TransitionTask(t.Context(), task.ID, repository.TaskExpect{State: task.State, Rev: task.Rev}, state, nil)
				if err != nil {
					t.Fatal(state, err)
				}
			}
			w := httpCall(s, "DELETE", fmt.Sprintf("/console/v1/hosts/%s?rev=%d", task.HostSN, beforeHost.Rev), "", token)
			var out struct {
				Code    int             `json:"code"`
				Data    referenceDetail `json:"data"`
				Message string          `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if w.Code != 409 || out.Code != 409 || out.Message != model.ConsoleReferenced || len(out.Data.References) != 1 || out.Data.References[0].Kind != "task" || out.Data.References[0].ID != task.ID {
				t.Fatal(state, w.Code, w.Body.String())
			}
			afterHost, err := st.GetHost(t.Context(), task.HostSN)
			if err != nil || !reflect.DeepEqual(beforeHost, afterHost) {
				t.Fatal(state, afterHost, err)
			}
			afterAudit, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
			if err != nil || !reflect.DeepEqual(beforeAudit, afterAudit) {
				t.Fatal("删除拒绝后留下审计", state, err)
			}
		}
	})
}

func hostContractServer(st *pgstore.Store) *Server {
	return New(wizardDeps(Deps{Tasks: st, Events: st, AuditReader: audit.NewReader(st), Clock: func() int64 { return fixedAt }}, st))
}
func hostContractConnection(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal("无法打开测试数据库连接")
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}
func hostContractSQL(t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.Exec(t.Context(), sql); err != nil {
		t.Fatal(err)
	}
}

func Test主机导入预演后覆盖最新版本(t *testing.T) {
	dsn := pgtest.Schema(t)
	st, other := pgtest.OpenAt(t, dsn), pgtest.OpenAt(t, dsn)
	s := hostContractServer(st)
	token, _ := seedHTTPUser(t, s, st, "host_preview", model.RoleAdmin)
	data := "sn,arch,rack\nSN-1,aarch64, A01 \n"
	p := responseData[csvPlan](t, csvHTTP(s, "/console/v1/hosts/import?dryRun=true", data, token))
	if p.Total != 1 || p.Rows[0].Action != "create" {
		t.Fatal(p)
	}
	if _, err := other.CreateHost(t.Context(), model.Host{SN: "SN-1", Arch: "x86_64", Attrs: map[string]string{"old": "value"}}, repository.Audit{Actor: "other", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	for _, rev := range []int64{2, 3} {
		p := responseData[hostPage](t, csvHTTP(s, "/console/v1/hosts/import", data, token))
		if len(p.Items) != 1 || p.Items[0].Rev != rev || p.Items[0].Arch != "aarch64" || !reflect.DeepEqual(p.Items[0].Attrs, map[string]string{"rack": " A01 "}) || p.Next != "" || p.Total != nil {
			t.Fatal(p)
		}
		actual, err := other.GetHost(t.Context(), "SN-1")
		if err != nil || !reflect.DeepEqual(p.Items[0], toHostResponse(actual)) {
			t.Fatal(actual, err)
		}
	}
}

// 审计触发器在真实事务内碰到数据库门闩；观察连接必须看到阻塞关系才放行。
func hostContractAuditGate(t *testing.T, conn *pgx.Conn, action string) pgx.Tx {
	t.Helper()
	hostContractSQL(t, conn, `CREATE TABLE host_contract_gate (id integer PRIMARY KEY); INSERT INTO host_contract_gate VALUES (1)`)
	hostContractSQL(t, conn, fmt.Sprintf(`CREATE FUNCTION host_contract_pause() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action = '%s' THEN PERFORM id FROM host_contract_gate FOR UPDATE; END IF; RETURN NEW; END $$`, action))
	hostContractSQL(t, conn, `CREATE TRIGGER host_contract_pause BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION host_contract_pause()`)
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback(context.Background()) })
	if _, err = tx.Exec(t.Context(), `LOCK TABLE host_contract_gate IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	return tx
}
func hostContractBlocked(t *testing.T, observer *pgx.Conn, blocker int, relation string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		var pid int
		err := observer.QueryRow(ctx, `SELECT COALESCE((SELECT a.pid FROM pg_stat_activity a WHERE a.wait_event_type='Lock' AND $1=ANY(pg_blocking_pids(a.pid)) AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND NOT l.granted) AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.relation=to_regclass($2)) LIMIT 1),0)`, blocker, relation).Scan(&pid)
		if err != nil {
			t.Fatalf("未观察到 %s 的数据库等待：%v", relation, err)
		}
		if pid != 0 {
			t.Logf("连接 %d 等待连接 %d，关系 %s", pid, blocker, relation)
			return pid
		}
	}
}
func hostContractAwait(t *testing.T, done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case w := <-done:
		return w
	case <-time.After(10 * time.Second):
		t.Fatal("请求未完成")
		return nil
	}
}
func hostContractEntry(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("请求没有进入真实写入入口")
	}
}

type hostContractImportEntry struct {
	repository.Hosts
	entered chan<- struct{}
}

func (p hostContractImportEntry) ImportHosts(ctx context.Context, hs []model.Host, a repository.Audit) ([]model.Host, error) {
	close(p.entered)
	return p.Hosts.ImportHosts(ctx, hs, a)
}

type hostContractTaskEntry struct {
	repository.CurrentTaskCreator
	entered chan<- struct{}
}

func (p hostContractTaskEntry) CreateCurrentTasks(ctx context.Context, ts []model.Task, a repository.Audit) ([]model.Task, error) {
	close(p.entered)
	return p.CurrentTaskCreator.CreateCurrentTasks(ctx, ts, a)
}

func Test主机变更与向导提交按数据库事实交错(t *testing.T) {
	for _, hostFirst := range []bool{true, false} {
		name := "任务先锁主机并保留旧快照"
		action := "task.create"
		if hostFirst {
			name = "主机先提交后任务重查拒绝"
			action = "host.import"
		}
		t.Run(name, func(t *testing.T) {
			dsn := pgtest.Schema(t)
			st, other := pgtest.OpenAt(t, dsn), pgtest.OpenAt(t, dsn)
			s, second := hostContractServer(st), hostContractServer(other)
			_, _, token, iso, tpl := seedWizardFixture(t, s, st)
			secondToken, _ := seedHTTPUser(t, second, other, "host_concurrent", model.RoleAdmin)
			gateConn, observer := hostContractConnection(t, dsn), hostContractConnection(t, dsn)
			gate := hostContractAuditGate(t, gateConn, action)
			taskEntered, importEntered := make(chan struct{}), make(chan struct{})
			assembled := wizardDeps(s.server.deps, st, hostContractTaskEntry{st, taskEntered})
			s.server.deps.Wizard, s.server.deps.TemplateAuthoring = assembled.Wizard, assembled.TemplateAuthoring
			second.server.deps.HostRegistry = hosts.NewRegistry(hosts.Deps{Hosts: hostContractImportEntry{other, importEntered}, Clock: second.server.deps.Clock})
			submitted, imported := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			submit := func() {
				submitted <- httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1"}]`), token)
			}
			importCSV := func() {
				imported <- csvHTTP(second, "/console/v1/hosts/import", "sn,arch,firmware\nSN-1,aarch64,uefi\n", secondToken)
			}
			if hostFirst {
				go importCSV()
				hostContractEntry(t, importEntered)
			} else {
				go submit()
				hostContractEntry(t, taskEntered)
			}
			writer := hostContractBlocked(t, observer, int(gateConn.PgConn().PID()), "host_contract_gate")
			if hostFirst {
				go submit()
				hostContractEntry(t, taskEntered)
			} else {
				go importCSV()
				hostContractEntry(t, importEntered)
			}
			waiter := hostContractBlocked(t, observer, writer, "hosts")
			if waiter == writer || waiter == int(gateConn.PgConn().PID()) {
				t.Fatal("并发写入没有使用独立连接")
			}
			if err := gate.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			importedPage := responseData[hostPage](t, hostContractAwait(t, imported))
			if len(importedPage.Items) != 1 || importedPage.Items[0].Arch != "aarch64" || importedPage.Items[0].Rev != 2 {
				t.Fatal(importedPage)
			}
			w := hostContractAwait(t, submitted)
			if hostFirst {
				var out struct {
					Code int         `json:"code"`
					Data batchDetail `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				ps := out.Data.ByHost["SN-1"]
				if w.Code != 422 || out.Code != 422 || len(ps) == 0 || ps[0].Code != "arch_mismatch" {
					t.Fatal(w.Code, w.Body.String())
				}
			} else {
				out := responseData[wizardSubmitResult](t, w)
				if len(out.Tasks) != 1 || out.Tasks[0].Snapshot.Host.Arch != "x86_64" {
					t.Fatal(out)
				}
			}
			page, err := st.ListTasks(t.Context(), repository.TaskQuery{})
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if hostFirst {
				want = 0
			}
			if len(page.Items) != want {
				t.Fatal(page)
			}
			if !hostFirst && page.Items[0].Snapshot.Host.Arch != "x86_64" {
				t.Fatal(page.Items)
			}
			h, err := st.GetHost(t.Context(), "SN-1")
			if err != nil || h.Arch != "aarch64" || h.Rev != 2 {
				t.Fatal(h, err)
			}
			audits, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, a := range audits.Items {
				if a.Action == "task.create" {
					count++
				}
			}
			if count != want {
				t.Fatal("任务审计数", count, want)
			}
		})
	}
}

func Test主机导入审计取消整批回滚(t *testing.T) {
	for _, failure := range []string{"中间主机写入失败", "中间审计失败", "锁等待取消"} {
		t.Run(failure, func(t *testing.T) {
			dsn := pgtest.Schema(t)
			st := pgtest.OpenAt(t, dsn)
			s := hostContractServer(st)
			token, _ := seedHTTPUser(t, s, st, "host_batch", model.RoleAdmin)
			for _, sn := range []string{"SN-1", "SN-2"} {
				if _, err := st.CreateHost(t.Context(), model.Host{SN: sn, Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{"rack": "old"}}, repository.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := st.GetHosts(t.Context(), []string{"SN-1", "SN-2", "SN-3"})
			if err != nil {
				t.Fatal(err)
			}
			beforeAudit, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			control := hostContractConnection(t, dsn)
			csv := "sn,arch,rack\nSN-2,aarch64,new2\nSN-1,aarch64,new1\nSN-3,aarch64,new3\n"
			switch failure {
			case "中间主机写入失败":
				hostContractSQL(t, control, `CREATE FUNCTION host_contract_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.sn='SN-2' THEN RAISE EXCEPTION 'host write fault'; END IF; RETURN NEW; END $$`)
				hostContractSQL(t, control, `CREATE TRIGGER host_contract_fail BEFORE INSERT OR UPDATE ON hosts FOR EACH ROW EXECUTE FUNCTION host_contract_fail()`)
				hostContractError(t, csvHTTP(s, "/console/v1/hosts/import", csv, token), 500, "")
				hostContractSQL(t, control, `DROP TRIGGER host_contract_fail ON hosts`)
			case "中间审计失败":
				hostContractSQL(t, control, `CREATE FUNCTION host_contract_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='host.import' AND NEW.target_id='SN-1' THEN RAISE EXCEPTION 'audit write fault'; END IF; RETURN NEW; END $$`)
				hostContractSQL(t, control, `CREATE TRIGGER host_contract_fail BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION host_contract_fail()`)
				hostContractError(t, csvHTTP(s, "/console/v1/hosts/import", csv, token), 500, "")
				hostContractSQL(t, control, `DROP TRIGGER host_contract_fail ON audit_log`)
			case "锁等待取消":
				tx, err := control.Begin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(t.Context(), `SELECT sn FROM hosts WHERE sn='SN-2' FOR UPDATE`); err != nil {
					t.Fatal(err)
				}
				observer := hostContractConnection(t, dsn)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() {
					_, err := s.server.deps.HostRegistry.Import(ctx, ports.Actor{Username: "host_batch", Role: model.RoleAdmin}, hosts.ImportRequest{CSV: []byte(csv)})
					done <- err
				}()
				hostContractBlocked(t, observer, int(control.PgConn().PID()), "hosts")
				cancel()
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("取消后仍成功")
					}
				case <-time.After(10 * time.Second):
					t.Fatal("取消没有结束导入")
				}
				if err := tx.Rollback(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			after, err := st.GetHosts(t.Context(), []string{"SN-1", "SN-2", "SN-3"})
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("整批记录或版本改变", after, err)
			}
			afterAudit, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
			if err != nil || !reflect.DeepEqual(beforeAudit, afterAudit) {
				t.Fatal("失败留下审计", err)
			}
			success := responseData[hostPage](t, csvHTTP(s, "/console/v1/hosts/import", "sn,arch,rack\nSN-2,aarch64,new2\nSN-1,aarch64,new1\n", token))
			if len(success.Items) != 2 || success.Items[0].SN != "SN-2" || success.Items[1].SN != "SN-1" || success.Items[0].Rev != 2 || success.Items[1].Rev != 2 {
				t.Fatal(success)
			}
			// 直接按审计自增编号检查写入顺序，避免列表的倒序展示改变含义。
			rows, err := control.Query(t.Context(), `SELECT target_id,actor,at,detail FROM audit_log WHERE action='host.import' ORDER BY id`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var sns []string
			for rows.Next() {
				var sn, actor, detail string
				var at int64
				if err := rows.Scan(&sn, &actor, &at, &detail); err != nil {
					t.Fatal(err)
				}
				if actor != "host_batch" || at != fixedAt || detail != "" {
					t.Fatal(actor, at, detail)
				}
				sns = append(sns, sn)
			}
			if err := rows.Err(); err != nil || !reflect.DeepEqual(sns, []string{"SN-2", "SN-1"}) {
				t.Fatal(sns, err)
			}
		})
	}
}
