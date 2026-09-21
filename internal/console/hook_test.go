// 本文件测试 hook 六个端点。对应实现计划任务 4「测试要锁住的行为」hook
// 部分。
package console

import (
	"net/http"
	"testing"

	"github.com/lober/lober/internal/apierr"
)

func validHookBody() map[string]any {
	return map[string]any{
		"name": "install-agent", "stage": "post", "interpreter": "bash",
		"body":       "#!/bin/bash\necho hello\n",
		"osFamilies": []string{"rhel"},
		"requires":   map[string]any{"contractVersion": ">=1", "facts": []string{"LOBER_SN"}},
		"timeoutSec": 60,
	}
}

// TestHook_UpdateCreatesNewVersionKeepingOld 锁住「hook 更新产生新版本且
// 旧版本仍可取」。
func TestHook_UpdateCreatesNewVersionKeepingOld(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("carol-author")

	rr := ts.doJSON("POST", "/console/v1/hooks", tok, validHookBody())
	if rr.Code != http.StatusOK {
		t.Fatalf("创建失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)
	if created.Data.Version != 1 {
		t.Fatalf("首次创建 version 应为 1，实际 %d", created.Data.Version)
	}

	updateBody := validHookBody()
	updateBody["body"] = "#!/bin/bash\necho updated\n"
	rr = ts.doJSON("PUT", "/console/v1/hooks/"+created.Data.ID, tok, updateBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("更新失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var updated struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &updated)
	if updated.Data.Version != 2 {
		t.Errorf("更新后 version 应为 2，实际 %d", updated.Data.Version)
	}
	if updated.Data.BodySHA256 == created.Data.BodySHA256 {
		t.Errorf("正文变了，digest 应该不同")
	}

	rr = ts.doJSON("GET", "/console/v1/hooks/"+created.Data.ID+"/versions", tok, nil)
	var versions struct {
		Data []hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &versions)
	if len(versions.Data) != 2 {
		t.Fatalf("版本列表应有 2 条，实际 %d", len(versions.Data))
	}
	if versions.Data[0].Version != 1 || versions.Data[1].Version != 2 {
		t.Errorf("版本列表应按 version 升序: %+v", versions.Data)
	}
}

// TestHook_RegistrationValidation 锁住「四项注册期校验各自拦下对应的
// 非法输入」。
func TestHook_RegistrationValidation(t *testing.T) {
	cases := []struct {
		name   string
		modify func(m map[string]any)
	}{
		{"非法 stage", func(m map[string]any) { m["stage"] = "not-a-stage" }},
		{"不存在的 fact 键", func(m map[string]any) {
			m["requires"] = map[string]any{"facts": []string{"LOBER_NOT_REAL"}}
		}},
		{"不满足的 contract 版本", func(m map[string]any) {
			m["requires"] = map[string]any{"contractVersion": ">=999"}
		}},
		{"params_schema 超出子集", func(m map[string]any) {
			m["paramsSchema"] = `{"vlan_id": {"type": "object"}}`
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := newTestServer(t)
			tok := ts.login("carol-author")
			body := validHookBody()
			c.modify(body)
			rr := ts.doJSON("POST", "/console/v1/hooks", tok, body)
			var env struct {
				Code int `json:"code"`
			}
			decodeJSON(t, rr.Body.Bytes(), &env)
			if env.Code == int(apierr.CodeOK) {
				t.Errorf("%s 应被拒绝，实际成功", c.name)
			}
		})
	}
}

// TestHook_ValidateWarnsOnTypoVar 锁住「静态检查对拼错的变量名产出警告
// （但不阻止注册）」。
func TestHook_ValidateWarnsOnTypoVar(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("carol-author")

	body := validHookBody()
	body["body"] = "#!/bin/bash\necho $LOBER_TYPO_NAME\n"
	rr := ts.doJSON("POST", "/console/v1/hooks", tok, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("创建应成功（拼写警告不阻止注册），实际 %d, body=%s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/validate", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("校验端点应返回 HTTP 200，实际 %d", rr.Code)
	}
	var result struct {
		Data hookValidateResult `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &result)

	foundWarn := false
	for _, iss := range result.Data.Issues {
		if iss.Level == "warn" {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Errorf("应对拼错的变量名产出警告，实际 issues=%+v", result.Data.Issues)
	}
}

// TestHook_ValidateSkipsPowerShellWithExplanation 锁住「跳过语法检查的
// 解释器在响应里明确说明」。
func TestHook_ValidateSkipsPowerShellWithExplanation(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("carol-author")

	body := validHookBody()
	body["interpreter"] = "powershell"
	body["body"] = "Write-Host 'hello'"
	rr := ts.doJSON("POST", "/console/v1/hooks", tok, body)
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/validate", tok, nil)
	var result struct {
		Data hookValidateResult `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &result)

	found := false
	for _, iss := range result.Data.Issues {
		if iss.Level == "warn" && containsSubstr(iss.Message, "跳过语法检查") {
			found = true
		}
	}
	if !found {
		t.Errorf("应有一条说明「跳过语法检查」的 issue，实际 issues=%+v", result.Data.Issues)
	}
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// TestHook_ApproveFlow 覆盖 draft→review→active 流转，锁住权限细分与
// 「审批人不能等于作者」。
func TestHook_ApproveFlow(t *testing.T) {
	ts := newTestServer(t)
	authorTok := ts.login("carol-author")

	rr := ts.doJSON("POST", "/console/v1/hooks", authorTok, validHookBody())
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	// draft → review：作者本人可发起。
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", authorTok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("draft→review 应成功: %d, body=%s", rr.Code, rr.Body.String())
	}
	var afterReview struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &afterReview)
	if afterReview.Data.State != "review" {
		t.Fatalf("期望 state=review，实际 %q", afterReview.Data.State)
	}

	// review → active：author 本人尝试审批自己的 hook，应被拒。
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", authorTok, nil)
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeForbidden) {
		t.Errorf("author 本人不应能审批（审批人不能等于作者），实际 code=%d", env.Code)
	}

	// review → active：hook-approver 审批通过。
	approverTok := ts.login("dave-approver")
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", approverTok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("approver 审批应成功: %d, body=%s", rr.Code, rr.Body.String())
	}
	var afterActive struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &afterActive)
	if afterActive.Data.State != "active" {
		t.Errorf("期望 state=active，实际 %q", afterActive.Data.State)
	}
	if afterActive.Data.Approver != "dave-approver" {
		t.Errorf("approver 字段应记为 dave-approver，实际 %q", afterActive.Data.Approver)
	}

	// 再次审批：已是终态，应回 Conflict。
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", approverTok, nil)
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeConflict) {
		t.Errorf("对 active 状态再次审批应回 CodeConflict，实际 code=%d", env.Code)
	}
}
