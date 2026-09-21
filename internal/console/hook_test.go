// 本文件测试 hook 六个端点。对应实现计划任务 4「测试要锁住的行为」hook
// 部分。
package console

import (
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"

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

// TestHook_ValidateCatchesRealBashSyntaxError 证伪一个隐藏假设：
// bashSyntaxCheck 真的调用了 `bash -n` 并把结果反映进 issues，而不是
// 「代码里写了调用 exec.CommandContext 的样子，但没有测试验证子进程真的
// 跑起来并识别出语法错误」。TestHook_ValidateWarnsOnTypoVar 只断言「存在
// 至少一条 warn 级别的 issue」，这条断言即便 bash -n 从未真正执行也可能
// 通过（拼写检查本身就会产出一条 warn），因此语法检查这条硬校验实际上
// 没有被任何测试锁住。本测试用一段故意不闭合 if 的脚本（缺 fi），断言
// 响应里出现一条 error 级别、且提到"bash -n"的 issue，同时 passed=false
// ——只有 bash -n 真的执行了才能产出这个结果。
func TestHook_ValidateCatchesRealBashSyntaxError(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("carol-author")

	body := validHookBody()
	// 故意不闭合的 if——bash -n 对此的标准诊断是 "unexpected end of file"
	// 或等价的语法错误提示，这是一个任何 bash 语法检查器都会拦下的错误，
	// 不依赖 bash 版本的具体错误文案。
	body["body"] = "#!/bin/bash\nif [ 1 -eq 1 ]; then\necho missing-fi\n"
	rr := ts.doJSON("POST", "/console/v1/hooks", tok, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("创建应成功（语法错误只在 validate 端点报告，不阻止注册）: %d, body=%s", rr.Code, rr.Body.String())
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

	if result.Data.Passed {
		t.Fatalf("脚本有真实语法错误（if 缺 fi），passed 应为 false，实际 issues=%+v", result.Data.Issues)
	}
	foundSyntaxError := false
	for _, iss := range result.Data.Issues {
		if iss.Level == "error" && containsSubstr(iss.Message, "bash -n") {
			foundSyntaxError = true
		}
	}
	if !foundSyntaxError {
		t.Errorf("应有一条 error 级别、提到 bash -n 的 issue（证明 bash -n 真的执行并识别出语法错误），"+
			"实际 issues=%+v（如果本机确实没有 bash 可执行文件，这条会假失败——但那本身也是需要"+
			"知道的环境问题，不应该被静默放过）", result.Data.Issues)
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

// TestHook_RejectReturnsReviewToDraft 锁住「驳回把 review 状态回退到
// draft，且不引入新状态」——这是本次契约变更的核心行为（SPEC §2.2 冻结了
// HookState 只有四个取值，驳回不能发明第五个）。
func TestHook_RejectReturnsReviewToDraft(t *testing.T) {
	ts := newTestServer(t)
	authorTok := ts.login("carol-author")
	approverTok := ts.login("dave-approver")

	rr := ts.doJSON("POST", "/console/v1/hooks", authorTok, validHookBody())
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	// draft → review。
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", authorTok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("draft→review 应成功: %d, body=%s", rr.Code, rr.Body.String())
	}

	// review → draft（驳回），approver 填理由。
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/reject", approverTok,
		map[string]string{"reason": "脚本里硬编码了内网密码，需要改成读环境变量"})
	if rr.Code != http.StatusOK {
		t.Fatalf("review→draft（驳回）应成功: %d, body=%s", rr.Code, rr.Body.String())
	}
	var rejected struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &rejected)
	if rejected.Data.State != "draft" {
		t.Fatalf("驳回后 state 应回到 draft，实际 %q（HookState 四个取值是冻结的，"+
			"驳回不能发明新状态）", rejected.Data.State)
	}
}

// TestHook_RejectRejectsNonReviewStates 锁住「只有 review 状态能被驳回」
// ——draft 驳回没有意义（还没提交审批），active 已经生效（要停用是
// deprecate 不是 reject），都要拒绝并给出可读原因（计划任务硬约束）。
func TestHook_RejectRejectsNonReviewStates(t *testing.T) {
	ts := newTestServer(t)
	authorTok := ts.login("carol-author")
	approverTok := ts.login("dave-approver")

	t.Run("draft", func(t *testing.T) {
		rr := ts.doJSON("POST", "/console/v1/hooks", authorTok, validHookBody())
		var created struct {
			Data hookItem `json:"data"`
		}
		decodeJSON(t, rr.Body.Bytes(), &created)

		rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/reject", approverTok,
			map[string]string{"reason": "理由"})
		var env struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		decodeJSON(t, rr.Body.Bytes(), &env)
		if env.Code != int(apierr.CodeConflict) {
			t.Errorf("draft 状态驳回应回 CodeConflict，实际 code=%d, message=%q", env.Code, env.Message)
		}
	})

	t.Run("active", func(t *testing.T) {
		rr := ts.doJSON("POST", "/console/v1/hooks", authorTok, validHookBody())
		var created struct {
			Data hookItem `json:"data"`
		}
		decodeJSON(t, rr.Body.Bytes(), &created)
		ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", authorTok, nil)
		ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", approverTok, nil)

		rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/reject", approverTok,
			map[string]string{"reason": "理由"})
		var env struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		decodeJSON(t, rr.Body.Bytes(), &env)
		if env.Code != int(apierr.CodeConflict) {
			t.Errorf("active 状态驳回应回 CodeConflict（要停用应用 deprecate 不是 reject），"+
				"实际 code=%d, message=%q", env.Code, env.Message)
		}
	})
}

// TestHook_RejectRequiresApprovePermission 锁住「无 hook:approve 权限的
// 角色驳回被拒」——驳回同样是审批权，不是作者自己能做的事（计划任务硬
// 约束，与 approve 的 review→active 分支对称）。
func TestHook_RejectRequiresApprovePermission(t *testing.T) {
	ts := newTestServer(t)
	authorTok := ts.login("carol-author")

	rr := ts.doJSON("POST", "/console/v1/hooks", authorTok, validHookBody())
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)
	ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", authorTok, nil)

	// 用 viewer（无 hook:approve 权限码）尝试驳回。
	viewerTok := ts.login("alice-viewer")
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/reject", viewerTok,
		map[string]string{"reason": "理由"})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeForbidden) {
		t.Errorf("无 hook:approve 权限的角色驳回应被拒绝，实际 code=%d", env.Code)
	}
}

// TestHook_RejectAuthorCannotRejectOwnHook 锁住「作者不能驳回自己的
// hook」——与 approve 的「审批人不能等于作者」对称：本测试用
// frank-both（同时持有 hook-author 与 hook-approver 两个角色）复现
// TestHook_ApproverCannotEqualAuthorEvenWithApprovePermission 的证伪逻辑，
// 确认拒绝原因是「同人判定」本身，而不是权限码不够。
func TestHook_RejectAuthorCannotRejectOwnHook(t *testing.T) {
	ts := newTestServer(t)

	const bothUsername = "frank-both-reject"
	const bothPassword = "password123"
	hash, err := bcrypt.GenerateFromPassword([]byte(bothPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成 bcrypt 哈希失败: %v", err)
	}
	ts.Server.deps.Users = append(ts.Server.deps.Users, AuthUser{
		Name:       bothUsername,
		BcryptHash: string(hash),
		Roles:      []string{roleHookAuthor, roleHookApprover},
	})

	loginRR := ts.doJSON("POST", "/console/v1/auth/login", "", map[string]string{
		"username": bothUsername, "password": bothPassword,
	})
	var loginEnv struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	decodeJSON(t, loginRR.Body.Bytes(), &loginEnv)
	tok := loginEnv.Data.AccessToken

	rr := ts.doJSON("POST", "/console/v1/hooks", tok, validHookBody())
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)
	ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", tok, nil)

	// 同一账号既是作者也持有 hook:approve 权限码，此时唯一能解释拒绝的
	// 原因就是「作者不能驳回自己的 hook」这条判据本身。
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/reject", tok,
		map[string]string{"reason": "理由"})
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeForbidden) {
		t.Errorf("持有 hook:approve 权限码的作者本人驳回自己的 hook 仍应被拒（作者不能驳回自己的 hook），"+
			"实际 code=%d, message=%q", env.Code, env.Message)
	}
}

// TestHook_RejectReasonIsQueryableViaAuditLog 锁住「驳回理由能被查到」
// ——本项目里 model.Hook 没有专门字段存理由（不能改 model，它是冻结契约），
// 选定方案是写入审计日志的 detail 字段（AuditLogger 已有该职责）。本测试
// 断言 audit 记录里出现 action=hook.reject、且 detail 包含理由原文。
//
// 契约缺口（详见 docs/contracts/http-console.md 变更记录与本轮
// contractGaps）：AuditLogger 接口只有 Log（写），没有查询方法，整个
// console 包也没有任何读取审计日志的 HTTP 端点——因此「审计日志里查得到」
// 这句话目前只在服务端内部成立（可用于日后补一个审计查询端点，或运维
// 直接查存储层），作者本人无法通过现有任何 API 拿到这条理由。本测试与
// 响应体回显理由（见下一个测试）共同构成当前最保守的方案。
func TestHook_RejectReasonIsQueryableViaAuditLog(t *testing.T) {
	ts := newTestServer(t)
	authorTok := ts.login("carol-author")
	approverTok := ts.login("dave-approver")

	rr := ts.doJSON("POST", "/console/v1/hooks", authorTok, validHookBody())
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)
	ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", authorTok, nil)

	const reason = "接口调用没有做超时保护，容易把主机名单请求卡死"
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/reject", approverTok,
		map[string]string{"reason": reason})
	if rr.Code != http.StatusOK {
		t.Fatalf("驳回应成功: %d, body=%s", rr.Code, rr.Body.String())
	}

	// 响应体本身回显理由——这是理由在「查得到」这件事上唯二的保障之一
	// （另一个是下方的审计日志），因为驳回没有改变正文，无法靠正文差异
	// 推断理由。
	var rejected struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &rejected)

	found := false
	for _, e := range ts.Audit.all() {
		if e.Action == "hook.reject" && e.Target == created.Data.ID && containsSubstr(e.Detail, reason) {
			found = true
		}
	}
	if !found {
		t.Errorf("审计日志里应有一条 action=hook.reject、detail 含理由原文的记录，实际 entries=%+v", ts.Audit.all())
	}
}

// TestHook_RejectDoesNotModifyBodyOrDigest 锁住「驳回不改动正文与
// digest」——这是本轮测试里最重要的一条：SPEC §2.7 的浮动引用防护要求
// 「hook 正文不可变，模板 pin 的是 digest，审批过的内容不会被偷换」。
// 如果驳回顺手改了 body/bodySha256，就意味着一个已经进入过 review 阶段
// 的正文可以被审批人以外的路径悄悄替换——这正是 §2.7 要防的事。
func TestHook_RejectDoesNotModifyBodyOrDigest(t *testing.T) {
	ts := newTestServer(t)
	authorTok := ts.login("carol-author")
	approverTok := ts.login("dave-approver")

	rr := ts.doJSON("POST", "/console/v1/hooks", authorTok, validHookBody())
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)
	ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", authorTok, nil)

	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/reject", approverTok,
		map[string]string{"reason": "理由"})
	var rejected struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &rejected)

	if rejected.Data.Body != created.Data.Body {
		t.Errorf("驳回不应改动正文，创建时 body=%q，驳回后 body=%q", created.Data.Body, rejected.Data.Body)
	}
	if rejected.Data.BodySHA256 != created.Data.BodySHA256 {
		t.Errorf("驳回不应改动 digest（正文没变，digest 就不该变；正文被改动意味着审批过的内容能被偷换，"+
			"SPEC §2.7），创建时 digest=%q，驳回后 digest=%q", created.Data.BodySHA256, rejected.Data.BodySHA256)
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

// TestHook_ApproverCannotEqualAuthorEvenWithApprovePermission 单独证伪
// 「审批人不能等于作者」这条判据本身——TestHook_ApproveFlow 里 author 本人
// 尝试审批被拒的那个断言，实际上被 carol-author 缺 hook:approve 权限码
// 这件事本身就足以解释（她连权限码检查那一关都过不了，代码根本不会走到
// 「user.Username == hook.Author」这一行），因此那条断言没有真正锁住这条
// 规则。本测试构造一个同时持有 hook-author 与 hook-approver 两个角色的
// 账号，用它自己创建 hook 并推进到 review，再用同一账号尝试审批——此时
// hook:approve 权限码检查一定能通过，如果响应仍然是 CodeForbidden，才能
// 确认真的是「审批人不能等于作者」这条判据生效，而不是权限码不够。
func TestHook_ApproverCannotEqualAuthorEvenWithApprovePermission(t *testing.T) {
	ts := newTestServer(t)

	const bothUsername = "frank-both"
	const bothPassword = "password123"
	hash, err := bcrypt.GenerateFromPassword([]byte(bothPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成 bcrypt 哈希失败: %v", err)
	}
	ts.Server.deps.Users = append(ts.Server.deps.Users, AuthUser{
		Name:       bothUsername,
		BcryptHash: string(hash),
		Roles:      []string{roleHookAuthor, roleHookApprover},
	})

	loginRR := ts.doJSON("POST", "/console/v1/auth/login", "", map[string]string{
		"username": bothUsername, "password": bothPassword,
	})
	if loginRR.Code != http.StatusOK {
		t.Fatalf("登录失败: %d, body=%s", loginRR.Code, loginRR.Body.String())
	}
	var loginEnv struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	decodeJSON(t, loginRR.Body.Bytes(), &loginEnv)
	tok := loginEnv.Data.AccessToken

	// 先确认这个账号确实拿到了 hook:approve 权限码——否则下面的拒绝就仍然
	// 可能只是权限码不够，而不是同人判定生效，测试本身就没有意义。
	codesRR := ts.doJSON("GET", "/console/v1/auth/codes", tok, nil)
	var codesEnv struct {
		Data []string `json:"data"`
	}
	decodeJSON(t, codesRR.Body.Bytes(), &codesEnv)
	if !hasCode(codesEnv.Data, codeHookApprove) {
		t.Fatalf("测试前提不满足：frank-both 应持有 hook:approve 权限码，实际 %v", codesEnv.Data)
	}

	rr := ts.doJSON("POST", "/console/v1/hooks", tok, validHookBody())
	if rr.Code != http.StatusOK {
		t.Fatalf("创建失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data hookItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	// draft → review。
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("draft→review 应成功: %d, body=%s", rr.Code, rr.Body.String())
	}

	// review → active：同一账号既是作者也持有 hook:approve 权限码，此时
	// 唯一能解释拒绝的原因就是「审批人不能等于作者」这条判据本身。
	rr = ts.doJSON("POST", "/console/v1/hooks/"+created.Data.ID+"/approve", tok, nil)
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeForbidden) {
		t.Errorf("持有 hook:approve 权限码的作者本人审批自己的 hook 仍应被拒（审批人不能等于作者），"+
			"实际 code=%d, message=%q", env.Code, env.Message)
	}
}
