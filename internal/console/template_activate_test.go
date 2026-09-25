// 本文件测试模板的状态流转端点（POST /templates/{id}/state）。
//
// **这个端点存在的理由是一条断掉的链路。** 在它出现之前，模板只能被创建成
// draft（handleCreateTemplate 硬编码 TemplateStateDraft），而
// handleCreateTask 第 2 步硬性要求 tpl.State == TemplateStateActive，
// 契约冻结的四个模板端点（create/update/form-schema/preview）里没有任何
// 一个能改状态——于是**任何模板都永远无法用于建任务**。
//
// store 层的 TemplateStore.UpdateTemplateState 一直是完备的，只是没有任何
// HTTP 调用点。scripts/e2e.sh 的头部注释（第 24-29 行）早就记下了这件事，
// 并用 db_template_active 直接写库绕过——与 artifact_sets 的 arch/firmware
// 是同一个模式：缺的是调用点，而 e2e 用 psql 替它补上了，于是缺口在真实
// API 路径上一直没暴露。
package console

import (
	"net/http"
	"testing"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/model"
)

// seedDraftTemplate 造一个 draft 模板并返回它的 ID 与 version。
func seedDraftTemplate(t *testing.T, ts *testServer, tok string, setID string) (string, int) {
	t.Helper()
	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-act", "artifactSetId": setID, "answerTemplate": "# ks\n",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("创建模板失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)
	if created.Data.State != string(model.TemplateStateDraft) {
		t.Fatalf("新建模板应为 draft，实际 %q", created.Data.State)
	}
	return created.Data.ID, created.Data.Version
}

// TestTemplateState_DraftToActive 锁住主路径：draft → active，之后该模板
// 能用于建任务。
func TestTemplateState_DraftToActive(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-1", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")
	id, ver := seedDraftTemplate(t, ts, tok, set.ID)

	rr := ts.doJSON("POST", "/console/v1/templates/"+id+"/state", tok, map[string]any{
		"state": "active", "version": ver,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("draft → active 应成功，实际 %d, body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &got)
	if got.Data.State != string(model.TemplateStateActive) {
		t.Errorf("流转后 state = %q，想要 active", got.Data.State)
	}
}

// TestTemplateState_ActiveToDeprecated 锁住停用路径：active → deprecated。
// 这是删制品集的前提（DeleteArtifactSet 对被 active 模板引用的制品集返回
// ErrStillReferenced，§2.7：只能 deprecate 不能删）。
func TestTemplateState_ActiveToDeprecated(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-1", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")
	id, ver := seedDraftTemplate(t, ts, tok, set.ID)

	rr := ts.doJSON("POST", "/console/v1/templates/"+id+"/state", tok, map[string]any{
		"state": "active", "version": ver,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("先激活失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var active struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &active)

	rr = ts.doJSON("POST", "/console/v1/templates/"+id+"/state", tok, map[string]any{
		"state": "deprecated", "version": active.Data.Version,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("active → deprecated 应成功，实际 %d, body=%s", rr.Code, rr.Body.String())
	}
	var dep struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &dep)
	if dep.Data.State != string(model.TemplateStateDeprecated) {
		t.Errorf("流转后 state = %q，想要 deprecated", dep.Data.State)
	}
}

// TestTemplateState_RejectsUnknownState 锁住「只认三个已定义的状态值」。
//
// 不做白名单的后果：任意字符串都能写进 state 列，而 handleCreateTask 用
// `!= active` 判定，于是一个拼错的状态值（"activ"）会让模板永久不可用，
// 且没有任何地方报错。
func TestTemplateState_RejectsUnknownState(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-1", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")
	id, ver := seedDraftTemplate(t, ts, tok, set.ID)

	for _, bad := range []string{"activ", "ACTIVE", "", "pending", "draft "} {
		rr := ts.doJSON("POST", "/console/v1/templates/"+id+"/state", tok, map[string]any{
			"state": bad, "version": ver,
		})
		if rr.Code == http.StatusOK {
			t.Errorf("state=%q 应被拒，实际 200", bad)
		}
	}
}

// TestTemplateState_RequiresAdmin 锁住权限：与 POST/PUT /templates 一致，
// 改模板状态需要 admin。
//
// 理由与建任务同级：模板决定这台机器装什么、跑哪些 hook，把一个模板激活
// 等于让它可被用于擦盘装机（§9.4 把 provider 与模板并列为需代码评审级别
// 的写权限）。
func TestTemplateState_RequiresAdmin(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-1", model.OSFamilyRHEL)
	adminTok := ts.login("eve-admin")
	id, ver := seedDraftTemplate(t, ts, adminTok, set.ID)

	rr := ts.doJSON("POST", "/console/v1/templates/"+id+"/state", ts.login("bob-operator"),
		map[string]any{"state": "active", "version": ver})
	if rr.Code != http.StatusForbidden {
		t.Errorf("operator 改模板状态应 403，实际 %d, body=%s", rr.Code, rr.Body.String())
	}
}

// TestTemplateState_VersionMismatchConflicts 锁住乐观锁：version 不匹配时
// 返回冲突，不静默覆盖。
//
// 场景：两个人同时看着同一个 draft 模板，一个人先改了内容（version 变成
// 2），另一个人拿着 version=1 点激活。静默放行会让「被激活的那一版」不是
// 点击者看到的那一版——而模板内容决定分区与 hook，这不是可以将错就错的
// 东西。
func TestTemplateState_VersionMismatchConflicts(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-1", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")
	id, ver := seedDraftTemplate(t, ts, tok, set.ID)

	// 改一次内容，version 前进。
	rr := ts.doJSON("PUT", "/console/v1/templates/"+id, tok, map[string]any{
		"name": "tpl-act-v2", "artifactSetId": set.ID, "answerTemplate": "# ks v2\n",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("更新模板失败: %d, body=%s", rr.Code, rr.Body.String())
	}

	rr = ts.doJSON("POST", "/console/v1/templates/"+id+"/state", tok, map[string]any{
		"state": "active", "version": ver, // 旧 version
	})
	if rr.Code != http.StatusConflict {
		t.Errorf("过期 version 应返回 409，实际 %d, body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Code apierr.Code `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &body)
	if body.Code != apierr.CodeConflict {
		t.Errorf("错误码应为 CodeConflict(%d)，实际 %d", apierr.CodeConflict, body.Code)
	}
}

// TestTemplateState_NotFound 锁住不存在的模板返回 404。
func TestTemplateState_NotFound(t *testing.T) {
	ts := newTestServer(t)
	rr := ts.doJSON("POST", "/console/v1/templates/no-such-tpl/state", ts.login("eve-admin"),
		map[string]any{"state": "active", "version": 1})
	if rr.Code != http.StatusNotFound {
		t.Errorf("不存在的模板应 404，实际 %d, body=%s", rr.Code, rr.Body.String())
	}
}
