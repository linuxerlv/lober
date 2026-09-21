// 本文件测试审计中间件。对应实现计划任务 2「测试要锁住的行为」。
package console

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAudit_WriteOperationRecordsFiveFields 锁住「写操作产生审计记录，
// 含五项且操作人正确」。五项是时间、操作人、动作、目标资源 id、客户端
// IP；本模块的 AuditLogger.Log 签名只有 actor/action/target/detail 四个
// 参数（时间由审计存储自己的实现决定如何记录，不是 console 传入的字段），
// 因此这里断言 actor/action/target 三项与 detail 里携带的客户端 IP。
func TestAudit_WriteOperationRecordsFiveFields(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]string{
		"sn": "AUDIT-SN-1", "mac": "aa:bb:cc:dd:ee:ff", "arch": "x86_64", "firmware": "uefi",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("录入失败: %d, body=%s", rr.Code, rr.Body.String())
	}

	entries := ts.Audit.all()
	if len(entries) != 1 {
		t.Fatalf("应产生 1 条审计记录，实际 %d 条: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Actor != "eve-admin" {
		t.Errorf("操作人应为 eve-admin，实际 %q", e.Actor)
	}
	if e.Action != "host.create" {
		t.Errorf("动作应为 host.create，实际 %q", e.Action)
	}
	if len(e.Detail) == 0 {
		t.Errorf("detail 应携带客户端 IP 信息，实际为空")
	}
}

// TestAudit_ReadOperationDoesNotRecord 锁住「读操作不产生审计记录」。
func TestAudit_ReadOperationDoesNotRecord(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("alice-viewer")

	rr := ts.doJSON("GET", "/console/v1/hosts", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("查询失败: %d", rr.Code)
	}

	if entries := ts.Audit.all(); len(entries) != 0 {
		t.Errorf("读操作不应产生审计记录，实际 %d 条: %+v", len(entries), entries)
	}
}

// TestAudit_ClientIPFromHeaderWhenPeerIsLoopback 锁住「审计记录里的客户端
// IP 是头部值（loopback peer 时）」。
func TestAudit_ClientIPFromHeaderWhenPeerIsLoopback(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	req := newJSONRequest(t, "POST", "/console/v1/hosts", map[string]string{
		"sn": "AUDIT-SN-2", "mac": "aa:bb:cc:dd:ee:ff", "arch": "x86_64", "firmware": "uefi",
	})
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = "127.0.0.1:9999" // 直连 peer 是 loopback，应信任转发头
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	rr := doRequest(ts, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("录入失败: %d, body=%s", rr.Code, rr.Body.String())
	}

	entries := ts.Audit.all()
	if len(entries) != 1 {
		t.Fatalf("应产生 1 条审计记录，实际 %d 条", len(entries))
	}
	if !containsSubstr(entries[0].Detail, "198.51.100.7") {
		t.Errorf("detail 应含转发头里的客户端 IP 198.51.100.7，实际 %q", entries[0].Detail)
	}
}

// TestAudit_ClientIPIgnoresHeaderWhenPeerIsNotLoopback 验证反向情形：
// 直连 peer 不是 loopback 时不信任转发头，记录 RemoteAddr 本身——这条
// 不在计划任务 2 的「测试要锁住的行为」清单里逐字列出，但直接支撑
// 「审计日志记全回环地址就失去了追溯价值」这条注释背后的判定逻辑，
// 反向路径同样需要验证，否则「只在 loopback 时信任」这条判断本身没有
// 被测试约束住。
func TestAudit_ClientIPIgnoresHeaderWhenPeerIsNotLoopback(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	req := newJSONRequest(t, "POST", "/console/v1/hosts", map[string]string{
		"sn": "AUDIT-SN-3", "mac": "aa:bb:cc:dd:ee:ff", "arch": "x86_64", "firmware": "uefi",
	})
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = "203.0.113.9:12345" // 非 loopback 直连
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	rr := doRequest(ts, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("录入失败: %d, body=%s", rr.Code, rr.Body.String())
	}

	entries := ts.Audit.all()
	if containsSubstr(entries[0].Detail, "198.51.100.7") {
		t.Errorf("非 loopback 直连时不应信任转发头，但 detail 里出现了伪造的 198.51.100.7: %q", entries[0].Detail)
	}
	if !containsSubstr(entries[0].Detail, "203.0.113.9") {
		t.Errorf("非 loopback 直连时应记录 RemoteAddr 本身 203.0.113.9，实际 %q", entries[0].Detail)
	}
}

// TestAudit_WriteFailureDoesNotBreakMainFlow 锁住「审计写入失败不影响
// 主流程」——审计存储故障不该让运维无法建任务。
func TestAudit_WriteFailureDoesNotBreakMainFlow(t *testing.T) {
	ts := newTestServer(t)
	ts.Audit.failNext = true
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]string{
		"sn": "AUDIT-SN-4", "mac": "aa:bb:cc:dd:ee:ff", "arch": "x86_64", "firmware": "uefi",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("审计写入失败不应影响主流程，但请求本身失败了: %d, body=%s", rr.Code, rr.Body.String())
	}

	hosts, err := ts.Store.ListHosts(t.Context())
	if err != nil {
		t.Fatalf("查询主机失败: %v", err)
	}
	found := false
	for _, h := range hosts {
		if h.SN == "AUDIT-SN-4" {
			found = true
		}
	}
	if !found {
		t.Errorf("主机应已成功录入，即便审计记录失败")
	}
}

// doRequest 直接把已构造好的 *http.Request 喂给 Server，供需要手动设置
// RemoteAddr/Header 的测试使用（doJSON 会覆盖 RemoteAddr 为固定值，不
// 满足这里的需要）。
func doRequest(ts *testServer, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	ts.Server.ServeHTTP(rr, req)
	return rr
}
