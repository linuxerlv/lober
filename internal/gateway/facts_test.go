// 本文件测试 facts 读写端点：POST /v1/facts/{task}/{token} 与
// GET /v1/facts/{task}/{token}。对应实现计划任务 8，依据
// docs/contracts/http-machine.md 端点 5、6、SPEC §5.5。
package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// factsFixture 复用 signalFixture 的形态：一个持有装机 token 与
// finalize token 的任务。facts 端点的认证与信号端点相同（同支持两种
// token），因此直接复用 newSignalFixture。

// doFactsWrite 请求 POST /v1/facts/{task}/{token}，body 为纯文本。
func doFactsWrite(t *testing.T, deps Deps, taskID, token, body string) (respBody string, status int) {
	t.Helper()
	srv := NewServer(deps)

	req := httptest.NewRequest(http.MethodPost, "/v1/facts/"+taskID+"/"+token, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	return string(raw), resp.StatusCode
}

// doFactsRead 请求 GET /v1/facts/{task}/{token}。
func doFactsRead(t *testing.T, deps Deps, taskID, token string) (respBody string, status int) {
	t.Helper()
	srv := NewServer(deps)

	req := httptest.NewRequest(http.MethodGet, "/v1/facts/"+taskID+"/"+token, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	return string(raw), resp.StatusCode
}

// TestFacts_WriteThenRead_RoundTrips 锁住：facts 写入后能读出，格式是每行
// 一个键值对。
func TestFacts_WriteThenRead_RoundTrips(t *testing.T) {
	fx := newSignalFixture(t)

	_, writeStatus := doFactsWrite(t, fx.deps, fx.task.ID, signalFixtureInstallToken, "LOBER_HOSTNAME=host-1\nLOBER_RACK=A-01\n")
	if writeStatus != http.StatusOK {
		t.Fatalf("写入状态码 = %d, want %d", writeStatus, http.StatusOK)
	}

	body, readStatus := doFactsRead(t, fx.deps, fx.task.ID, signalFixtureInstallToken)
	if readStatus != http.StatusOK {
		t.Fatalf("读取状态码 = %d, want %d", readStatus, http.StatusOK)
	}
	if !strings.Contains(body, "LOBER_HOSTNAME=host-1\n") {
		t.Errorf("响应体缺少 LOBER_HOSTNAME=host-1，got:\n%s", body)
	}
	if !strings.Contains(body, "LOBER_RACK=A-01\n") {
		t.Errorf("响应体缺少 LOBER_RACK=A-01，got:\n%s", body)
	}
}

// TestFacts_BlankLinesAndComments_Ignored 锁住：空行与注释行被忽略。
func TestFacts_BlankLinesAndComments_Ignored(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doFactsWrite(t, fx.deps, fx.task.ID, signalFixtureInstallToken,
		"# 这是注释\n\nLOBER_HOSTNAME=host-2\n\n# 另一条注释\n")
	if status != http.StatusOK {
		t.Fatalf("写入状态码 = %d, want %d", status, http.StatusOK)
	}

	body, _ := doFactsRead(t, fx.deps, fx.task.ID, signalFixtureInstallToken)
	if strings.Contains(body, "#") {
		t.Errorf("响应体应不含注释行，got:\n%s", body)
	}
	if !strings.Contains(body, "LOBER_HOSTNAME=host-2\n") {
		t.Errorf("响应体缺少 LOBER_HOSTNAME=host-2，got:\n%s", body)
	}
}

// TestFacts_LineWithoutEquals_IgnoredNotRejected 锁住契约文档端点 5
// 「错误行为」：body 中出现不含 = 的行时忽略该行，不拒绝整个请求。
func TestFacts_LineWithoutEquals_IgnoredNotRejected(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doFactsWrite(t, fx.deps, fx.task.ID, signalFixtureInstallToken,
		"this line has no equals sign\nLOBER_HOSTNAME=host-3\n")
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d —— 一行格式错不该让整批 fact 都写不进去", status, http.StatusOK)
	}

	body, _ := doFactsRead(t, fx.deps, fx.task.ID, signalFixtureInstallToken)
	if !strings.Contains(body, "LOBER_HOSTNAME=host-3\n") {
		t.Errorf("格式错误行之外的合法行也应该写入成功，got:\n%s", body)
	}
}

// TestFacts_ValueMayContainEquals 锁住契约文档端点 5：= 后面到行尾都算
// value（value 本身可含 =）。
func TestFacts_ValueMayContainEquals(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doFactsWrite(t, fx.deps, fx.task.ID, signalFixtureInstallToken, "LOBER_CUSTOM=a=b=c\n")
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d", status, http.StatusOK)
	}

	body, _ := doFactsRead(t, fx.deps, fx.task.ID, signalFixtureInstallToken)
	if !strings.Contains(body, "LOBER_CUSTOM=a=b=c\n") {
		t.Errorf("value 中的 = 应完整保留，got:\n%s", body)
	}
}

// TestFacts_MergeOverwritesSameKeyKeepsOthers 锁住 SPEC §5.5：写入是合并
// 而非替换，同 key 后写覆盖先写，未出现的既有键保持不变。
func TestFacts_MergeOverwritesSameKeyKeepsOthers(t *testing.T) {
	fx := newSignalFixture(t)

	doFactsWrite(t, fx.deps, fx.task.ID, signalFixtureInstallToken, "LOBER_HOSTNAME=first\nLOBER_RACK=A-01\n")
	doFactsWrite(t, fx.deps, fx.task.ID, signalFixtureInstallToken, "LOBER_HOSTNAME=second\n")

	body, _ := doFactsRead(t, fx.deps, fx.task.ID, signalFixtureInstallToken)
	if !strings.Contains(body, "LOBER_HOSTNAME=second\n") {
		t.Errorf("同 key 后写应覆盖先写，got:\n%s", body)
	}
	if !strings.Contains(body, "LOBER_RACK=A-01\n") {
		t.Errorf("未出现在第二次写入中的既有键应保持不变，got:\n%s", body)
	}
}

// TestFacts_AcceptsBothTokenPurposes 锁住：facts 端点接受两种 token。
func TestFacts_AcceptsBothTokenPurposes(t *testing.T) {
	fx := newSignalFixture(t)

	_, installStatus := doFactsWrite(t, fx.deps, fx.task.ID, signalFixtureInstallToken, "LOBER_A=1\n")
	if installStatus != http.StatusOK {
		t.Errorf("装机 token 写入状态码 = %d, want %d", installStatus, http.StatusOK)
	}

	_, finalizeStatus := doFactsWrite(t, fx.deps, fx.task.ID, signalFixtureFinalizeToken, "LOBER_B=2\n")
	if finalizeStatus != http.StatusOK {
		t.Errorf("finalize token 写入状态码 = %d, want %d", finalizeStatus, http.StatusOK)
	}

	_, readStatus := doFactsRead(t, fx.deps, fx.task.ID, signalFixtureFinalizeToken)
	if readStatus != http.StatusOK {
		t.Errorf("finalize token 读取状态码 = %d, want %d", readStatus, http.StatusOK)
	}
}

// TestFacts_InvalidToken_Returns403 补充覆盖：无效 token 时读写均回 403。
func TestFacts_InvalidToken_Returns403(t *testing.T) {
	fx := newSignalFixture(t)

	_, writeStatus := doFactsWrite(t, fx.deps, fx.task.ID, "wrong-token", "LOBER_A=1\n")
	if writeStatus != http.StatusForbidden {
		t.Errorf("写入状态码 = %d, want %d", writeStatus, http.StatusForbidden)
	}

	_, readStatus := doFactsRead(t, fx.deps, fx.task.ID, "wrong-token")
	if readStatus != http.StatusForbidden {
		t.Errorf("读取状态码 = %d, want %d", readStatus, http.StatusForbidden)
	}
}

// TestFacts_ReadContentTypeIsPlainText 锁住读端点 Content-Type。
func TestFacts_ReadContentTypeIsPlainText(t *testing.T) {
	fx := newSignalFixture(t)
	doFactsWrite(t, fx.deps, fx.task.ID, signalFixtureInstallToken, "LOBER_A=1\n")

	srv := NewServer(fx.deps)
	req := httptest.NewRequest(http.MethodGet, "/v1/facts/"+fx.task.ID+"/"+signalFixtureInstallToken, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	ct := rec.Result().Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want 以 text/plain 开头", ct)
	}
}
