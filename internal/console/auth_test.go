// 本文件测试认证四端点与会话过期行为。对应实现计划任务 1「测试要锁住的
// 行为」前四条与「无 token 访问受保护端点回未认证」。
package console

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lober/lober/internal/apierr"
)

// TestAuth_LoginSuccessAndFailure 锁住「正确密码登录成功并返回 token；
// 错误密码失败」，以及「失败响应不区分用户不存在与密码错误」。
func TestAuth_LoginSuccessAndFailure(t *testing.T) {
	ts := newTestServer(t)

	// 正确密码：成功并拿到非空 token。
	rr := ts.doJSON("POST", "/console/v1/auth/login", "", map[string]string{
		"username": "bob-operator",
		"password": "password123",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("正确密码登录应返回 HTTP 200，实际 %d，body=%s", rr.Code, rr.Body.String())
	}

	cases := []struct {
		name     string
		username string
		password string
	}{
		{"密码错误", "bob-operator", "wrong-password"},
		{"用户不存在", "no-such-user", "whatever"},
	}
	var messages []string
	for _, c := range cases {
		rr := ts.doJSON("POST", "/console/v1/auth/login", "", map[string]string{
			"username": c.username,
			"password": c.password,
		})
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s：期望 HTTP 401，实际 %d —— 未登录/认证失败必须是 401（契约唯一的 HTTP 状态码例外）", c.name, rr.Code)
		}
		var env struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		decodeJSON(t, rr.Body.Bytes(), &env)
		if env.Code == 0 {
			t.Errorf("%s：code 不应为 0（0 表示成功）", c.name)
		}
		messages = append(messages, env.Message)
	}
	if messages[0] != messages[1] {
		t.Errorf("失败响应不应区分「用户不存在」与「密码错误」，但两条消息不同："+
			"密码错误=%q，用户不存在=%q —— 区分会帮攻击者做用户名枚举", messages[0], messages[1])
	}
}

// TestAuth_LoginRejectsEmptyCredentials 锁住「用户名或密码为空时登录请求
// 被 gin binding 挡在 handler 之前，回 422 + CodeValidationFailed」——这是
// gin 迁移新增的传输层校验（迁移契约 §7：auth.go 原本 0 个手写校验点，本次
// 用 binding:"required" 标签补上「非空」这条校验，不是从手写代码搬运过来
// 的既有行为）。
func TestAuth_LoginRejectsEmptyCredentials(t *testing.T) {
	cases := []struct {
		name string
		body map[string]string
	}{
		{"用户名为空", map[string]string{"username": "", "password": "password123"}},
		{"密码为空", map[string]string{"username": "bob-operator", "password": ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := newTestServer(t)
			rr := ts.doJSON("POST", "/console/v1/auth/login", "", c.body)
			if rr.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s：期望 HTTP 422，实际 %d，body=%s", c.name, rr.Code, rr.Body.String())
			}
			var env struct {
				Code int `json:"code"`
			}
			decodeJSON(t, rr.Body.Bytes(), &env)
			if env.Code != int(apierr.CodeValidationFailed) {
				t.Errorf("%s：期望 code=CodeValidationFailed(%d)，实际 %d", c.name, apierr.CodeValidationFailed, env.Code)
			}
		})
	}
}

// TestAuth_LoginRejectsMalformedJSON 锁住「请求体不是合法 JSON」这条分支
// ——本轮迁移把解析载体从 json.NewDecoder 换成 c.ShouldBindJSON，语法错误
// 的 error 类型不是 validator.ValidationErrors，必须单独判断分流到
// CodeBadRequest，否则会被 apierr.Fail 的兜底分支误判成 CodeInternal（同
// artifact.go 的 TestArtifact_CreateRejectsMalformedJSON，两处是同一类
// 问题的独立修正）。用 httptest.NewRequest 直接发裸 body，不能用
// doJSON——它总是先把 body 序列化成合法 JSON。/auth/login 是公开端点，
// 不需要携带 token。
func TestAuth_LoginRejectsMalformedJSON(t *testing.T) {
	ts := newTestServer(t)

	req := httptest.NewRequest("POST", "/console/v1/auth/login", bytes.NewReader([]byte("{not valid json")))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.1:12345"
	rr := httptest.NewRecorder()
	ts.Server.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("请求体不是合法 JSON 应回 HTTP 400，实际 %d，body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeBadRequest) {
		t.Errorf("请求体不是合法 JSON 应回 CodeBadRequest(%d)，实际 code=%d，body=%s",
			apierr.CodeBadRequest, env.Code, rr.Body.String())
	}
}

// TestAuth_TokenExpiry 锁住「token 过期后被拒」。
func TestAuth_TokenExpiry(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("bob-operator")

	// 过期前能访问。
	rr := ts.doJSON("GET", "/console/v1/user/info", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("过期前访问 /user/info 应成功，实际 %d", rr.Code)
	}

	// 推进时钟超过默认 TTL（12 小时）。
	ts.Clock.Advance(13 * time.Hour)

	rr = ts.doJSON("GET", "/console/v1/user/info", tok, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("token 过期后应回 401，实际 %d，body=%s", rr.Code, rr.Body.String())
	}
}

// TestAuth_LogoutInvalidatesTokenImmediately 锁住「登出后 token 立即
// 失效」。
func TestAuth_LogoutInvalidatesTokenImmediately(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/auth/logout", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("登出应返回 HTTP 200，实际 %d", rr.Code)
	}

	rr = ts.doJSON("GET", "/console/v1/user/info", tok, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("登出后同一 token 访问受保护端点应回 401，实际 %d", rr.Code)
	}
}

// TestAuth_NoTokenIsUnauthenticated 锁住「无 token 访问受保护端点回未
// 认证」。
func TestAuth_NoTokenIsUnauthenticated(t *testing.T) {
	ts := newTestServer(t)

	rr := ts.doJSON("GET", "/console/v1/user/info", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("无 token 访问受保护端点应回 401，实际 %d", rr.Code)
	}
}

// TestRBAC_RoleCodes 锁住「五个角色各自的权限码正确」。
func TestRBAC_RoleCodes(t *testing.T) {
	cases := []struct {
		username string
		want     []string
	}{
		{"alice-viewer", []string{}},
		{"bob-operator", []string{codeTaskCreate, codeTaskRetry, codeTaskCancel}},
		{"carol-author", []string{codeHookWrite}},
		{"dave-approver", []string{codeHookApprove}},
		{"eve-admin", []string{
			codeProviderWrite, codeArtifactImport,
			codeTaskCreate, codeTaskRetry, codeTaskCancel,
			codeHookWrite, codeHookApprove,
		}},
	}

	for _, c := range cases {
		t.Run(c.username, func(t *testing.T) {
			ts := newTestServer(t)
			tok := ts.login(c.username)
			rr := ts.doJSON("GET", "/console/v1/auth/codes", tok, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("/auth/codes 应返回 HTTP 200，实际 %d", rr.Code)
			}
			var env struct {
				Data []string `json:"data"`
			}
			decodeJSON(t, rr.Body.Bytes(), &env)
			if !sameStringSet(env.Data, c.want) {
				t.Errorf("角色 %s 的权限码 = %v, want %v（集合比较，不看顺序）", c.username, env.Data, c.want)
			}
		})
	}
}

// TestRBAC_OperatorCannotWriteHook 锁住「operator 调写 hook 的端点被拒
// 403」。
//
// HTTP 状态码断言在 gin 迁移中从 200 改为 403：迁移契约 §2 冻结的错误码表
// 明确 CodeForbidden → HTTP 403，且 §0 指出「HTTP 状态码固定 200」是过期
// 文档认知，本次迁移要让状态码开始反映真实语义，不是本测试自行变更判定
// 标准。requireCode/requireRole（rbac.go）已改为传 http.StatusForbidden，
// 见迁移契约 §5 骨架 agent 的交付范围。
func TestRBAC_OperatorCannotWriteHook(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/hooks", tok, map[string]any{
		"name": "x", "stage": "pre", "interpreter": "bash", "body": "#!/bin/bash\n",
	})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("授权失败应回 HTTP 403（迁移契约 §2 冻结的状态码表），实际 %d", rr.Code)
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeForbidden) {
		t.Errorf("operator 调写 hook 端点应回 CodeForbidden(%d)，实际 code=%d", apierr.CodeForbidden, env.Code)
	}
}

// TestRBAC_ViewerCannotCallWriteEndpoints 锁住「viewer 调任何写端点被拒」，
// 用建任务端点代表写端点。HTTP 状态码断言同上一测试的说明，改为 403。
func TestRBAC_ViewerCannotCallWriteEndpoints(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("alice-viewer")

	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": "whatever", "templateId": "whatever",
	})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("授权失败应回 HTTP 403（迁移契约 §2 冻结的状态码表），实际 %d", rr.Code)
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeForbidden) {
		t.Errorf("viewer 调建任务端点应回 CodeForbidden(%d)，实际 code=%d", apierr.CodeForbidden, env.Code)
	}
}

// sameStringSet 比较两个字符串切片是否含有相同的元素集合（不看顺序、不看
// 重复次数是否一致，只看「出现过的元素集合是否相等」——本测试文件的用例
// 里不存在重复元素，因此这个简化判据足够）。
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		if !set[x] {
			return false
		}
	}
	return true
}
