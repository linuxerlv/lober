// 本文件测试认证四端点与会话过期行为。对应实现计划任务 1「测试要锁住的
// 行为」前四条与「无 token 访问受保护端点回未认证」。
package console

import (
	"net/http"
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
func TestRBAC_OperatorCannotWriteHook(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/hooks", tok, map[string]any{
		"name": "x", "stage": "pre", "interpreter": "bash", "body": "#!/bin/bash\n",
	})
	if rr.Code != http.StatusOK {
		// 契约要求 HTTP 状态码固定 200（认证失败是唯一例外），授权失败
		// 靠 body 里的 code 字段表达。
		t.Fatalf("授权失败应仍是 HTTP 200（契约的 HTTP 状态码约定），实际 %d", rr.Code)
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
// 用建任务端点代表写端点。
func TestRBAC_ViewerCannotCallWriteEndpoints(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("alice-viewer")

	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": "whatever", "templateId": "whatever",
	})
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
