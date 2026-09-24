// 本文件测试模板四个端点。对应实现计划任务 5「测试要锁住的行为」。
package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// seedActiveArtifactSet 与 seedActiveHook 是本文件多个测试共用的最小
// 种子数据构造 helper。
func seedActiveArtifactSet(t *testing.T, ts *testServer, id string, family model.OSFamily) model.ArtifactSet {
	t.Helper()
	set, err := ts.Store.CreateArtifactSet(t.Context(), model.ArtifactSet{
		ID: id, Name: id, OSFamily: family, State: model.ArtifactSetReady,
	})
	if err != nil {
		t.Fatalf("准备制品集失败: %v", err)
	}
	return set
}

func seedActiveHook(t *testing.T, ts *testServer, id string, stage model.HookStage, families []model.OSFamily, paramsSchema string) model.Hook {
	t.Helper()
	body := "#!/bin/bash\necho seed\n"
	hook, err := ts.Store.CreateHook(t.Context(), model.Hook{
		ID: id, Name: id, Stage: stage, OSFamilies: families, Interpreter: "bash",
		Body: body, BodySHA256: model.HookDigest(body), ParamsSchema: paramsSchema,
		Version: 1, State: model.HookStateActive, Author: "seed",
	})
	if err != nil {
		t.Fatalf("准备 hook 失败: %v", err)
	}
	return hook
}

// TestTemplate_UpdateCreatesNewVersion 锁住「更新产生新版本」。
func TestTemplate_UpdateCreatesNewVersion(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-1", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-1", "artifactSetId": set.ID, "answerTemplate": "# ks\n",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("创建失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("PUT", "/console/v1/templates/"+created.Data.ID, tok, map[string]any{
		"name": "tpl-1-v2", "artifactSetId": set.ID, "answerTemplate": "# ks v2\n",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("更新失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var updated struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &updated)
	if updated.Data.Version != 2 {
		t.Errorf("更新后 version 应为 2，实际 %d", updated.Data.Version)
	}
}

// TestTemplate_CreateRejectsWhitespaceOnlyName 锁住「name 全是空白字符时
// 仍被拒」。这条区分手写校验（strings.TrimSpace 判空）与 gin
// binding:"required" 标签：后者只判断零值，"   " 不是空字符串、不会触发
// required 标签，若迁移时误以为标签能完全替代这处手写校验，这条会先失败
// （迁移契约 §7：标签表达不了的校验条件必须留在 handler 里，不能因为
// 「换成标签」而悄悄弱化）。
func TestTemplate_CreateRejectsWhitespaceOnlyName(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-name-ws", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "   ", "artifactSetId": set.ID, "answerTemplate": "# ks\n",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("name 全空白时 HTTP 状态码 = %d, want %d（迁移契约 §2：CodeBadRequest 对应 400）",
			rr.Code, http.StatusBadRequest)
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code == int(apierr.CodeOK) {
		t.Errorf("name 全是空白字符时应被拒，实际成功——说明校验被 gin binding:\"required\" 标签" +
			"静默弱化了（该标签不认为 \"   \" 是零值）")
	}
}

// TestTemplate_CreateRejectsMalformedJSONBody 锁住「请求体不是合法 JSON
// 时返回 400 + CodeBadRequest，且不是 500」——迁移前用手写
// json.NewDecoder().Decode 判断解析错误，迁移后换成 gin 的
// c.ShouldBindJSON，这条测试确保换绑定方式后这个分支还在同一个错误码上，
// 不会因为 gin 把解析错误和 panic 混在一起处理而退化成未识别的内部错误。
func TestTemplate_CreateRejectsMalformedJSONBody(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	// 不用 ts.doJSON——它总是先 json.Marshal 一个合法的 Go 值，构造不出
	// 「请求体本身就不是合法 JSON」这种输入，这里手写请求体是恰当的做法，
	// 不是绕开共享 helper。
	req := httptest.NewRequest("POST", "/console/v1/templates", strings.NewReader("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = "203.0.113.1:12345"
	rr := httptest.NewRecorder()
	ts.Server.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("请求体不是合法 JSON 时 HTTP 状态码 = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeBadRequest) {
		t.Errorf("请求体不是合法 JSON 时业务码 = %d, want %d（CodeBadRequest）", env.Code, int(apierr.CodeBadRequest))
	}
}

// TestTemplate_FormSchemaAggregatesThreeSections 锁住「表单 schema 含三段
// 的字段，且序列号字段标记为主机选择控件」。
func TestTemplate_FormSchemaAggregatesThreeSections(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-2", model.OSFamilyRHEL)
	hook := seedActiveHook(t, ts, "hook-1", model.HookStagePost, nil,
		`{"vlan_id": {"type": "integer", "title": "VLAN ID"}}`)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-2", "artifactSetId": set.ID,
		"requiredFormFields": []string{"role"},
		"hookRefs": []map[string]any{
			{"hookId": hook.ID, "stage": "post", "order": 10},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("创建失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("GET", "/console/v1/templates/"+created.Data.ID+"/form-schema", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("取表单 schema 失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)

	for _, field := range []string{"sn", "os", "version", "hostname", "bizIp", "role", "vlan_id"} {
		if _, ok := env.Data[field]; !ok {
			t.Errorf("聚合后的表单 schema 缺少字段 %q，实际字段=%v", field, mapKeys(env.Data))
		}
	}

	var snField struct {
		XWidget string `json:"x-widget"`
	}
	decodeJSON(t, env.Data["sn"], &snField)
	if snField.XWidget != "host-select" {
		t.Errorf("sn 字段应标记 x-widget=host-select，实际 %q", snField.XWidget)
	}
}

// TestTemplate_FormSchemaRejectsHookOutsideSubset 锁住「聚合结果在子集内
// （用一个超出子集的 hook schema 验证被拒）」。
//
// 校验点在 hook 注册期（本模块的 registration-time validation 会拒绝
// 超出子集的 params_schema），因此本测试直接验证「无法注册出一个超出
// 子集的 active hook」这条前置条件本身成立——如果 hook 注册期校验被
// 绕过（例如测试直接写入 fake.Store），form-schema 端点的聚合校验作为
// 第二道防线也必须拦下。这里覆盖第二道防线：直接在 Store 里塞入一个
// 绕过了注册校验的非法 hook（模拟「校验点被绕过」的情形），验证
// form-schema 端点本身在聚合时也会拒绝。
func TestTemplate_FormSchemaRejectsHookOutsideSubset(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-3", model.OSFamilyRHEL)
	hook := seedActiveHook(t, ts, "hook-bad", model.HookStagePost, nil,
		`{"bad_field": {"type": "object"}}`) // object 不在 §12.7 子集内
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-3", "artifactSetId": set.ID,
		"hookRefs": []map[string]any{{"hookId": hook.ID, "stage": "post", "order": 10}},
	})
	var created struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("GET", "/console/v1/templates/"+created.Data.ID+"/form-schema", tok, nil)
	// HTTP 状态码必须与 CodeInternal 对应的 500 一致（迁移契约 §2 冻结的
	// 错误码表）：聚合失败是服务端内部状态异常（active hook 的 params_schema
	// 本不该出现子集外字段，走到这里说明注册期校验被绕过），不是客户端
	// 输入错误，用 500 而不是 400/404 才与「谁的责任」对应。
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("聚合结果超出子集时 HTTP 状态码 = %d, want %d（迁移契约 §2：CodeInternal 对应 500）",
			rr.Code, http.StatusInternalServerError)
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code == int(apierr.CodeOK) {
		t.Errorf("聚合结果包含超出子集的字段时应被拒，实际成功")
	}
}

// TestTemplate_PreviewReturnsTwoSectionsWithoutSideEffects 锁住「preview
// 返回两段文本且不产生任何任务或 token」。
func TestTemplate_PreviewReturnsTwoSectionsWithoutSideEffects(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-4", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-4", "artifactSetId": set.ID, "answerTemplate": "# ks\n",
	})
	var created struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("POST", "/console/v1/templates/"+created.Data.ID+"/preview", tok, map[string]any{
		"formData": map[string]string{"hostname": "web-01"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("preview 失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data previewResult `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Data.AnswerFile == "" || env.Data.BootMenu == "" {
		t.Errorf("preview 应返回非空的 answerFile 与 bootMenu，实际 %+v", env.Data)
	}

	tasks, err := ts.Store.ListTasks(t.Context(), contract.TaskFilter{})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("preview 不应产生任何任务，实际 %d 条", len(tasks))
	}
}

// TestTemplate_PreviewWithRealHostIDUsesThatHostNotFakeDefault 证伪一个
// 隐藏假设：handleTemplatePreview 传入真实 hostId 时是否真的用了那台主机。
//
// host.go 的 findHostByExposedID 与 task.go 的 handleCreateTask 都刻意
// 避免直接调用 HostStore.GetHost——因为「暴露给人面 API 的 id」（hostID()
// 算出的裸 SN/MAC/UUID）与 fake.Store 内部 hostKey() 用的带前缀 key
// （"sn:xxx"）不是同一个字符串，直接拿裸值查 GetHost 必然查不到。但
// handleTemplatePreview 的实现（template.go）在有 req.HostID 时确实直接
// 调用了 s.deps.Hosts.GetHost(ctx, req.HostID)，且失败时静默回退到
// fakePreviewHost、不报错、不警告——如果这个假设错了（换个 Store 实现，
// 或者就是当前的 fake.Store），前端传一个从 GET /hosts 拿到的真实 hostId
// 进来，preview 展示的会一直是内置假主机的数据，运维完全看不出来。
func TestTemplate_PreviewWithRealHostIDUsesThatHostNotFakeDefault(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-preview-realhost", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")

	const realSN = "PREVIEW-REAL-HOST-SN-42"
	realHost := model.Host{
		SN: realSN, MAC: "11:22:33:44:55:66", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI,
	}
	if _, err := ts.Store.UpsertHost(t.Context(), realHost); err != nil {
		t.Fatalf("准备主机失败: %v", err)
	}
	// 与 host.go/task.go 一致的取法：暴露给人面 API 的 id 是 hostID()
	// 算出的值（这里就是 SN 本身），前端从 GET /hosts 拿到的正是这个值。
	exposedHostID := hostID(realHost)

	// 用一个能回显 ctx.Task.HostSN 的 Renderer，观察 preview 实际用了
	// 哪台主机的数据。
	var capturedHostSN string
	ts.Server.deps.Renderer = &stubRenderer{
		answerFunc: func(ctx model.RenderContext, fileName string) (string, error) {
			capturedHostSN = ctx.Task.HostSN
			return "# answer for " + ctx.Task.HostSN + "\n", nil
		},
	}

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-preview-realhost", "artifactSetId": set.ID, "answerTemplate": "# ks\n",
	})
	var created struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("POST", "/console/v1/templates/"+created.Data.ID+"/preview", tok, map[string]any{
		"hostId": exposedHostID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("preview 失败: %d, body=%s", rr.Code, rr.Body.String())
	}

	if capturedHostSN != realSN {
		t.Errorf("preview 传入真实 hostId=%q（来自 GET /hosts 会暴露的同一个值）时，"+
			"渲染上下文里的 HostSN 应为真实主机的 %q，实际却是 %q——"+
			"说明 handleTemplatePreview 用 HostStore.GetHost(裸id) 查不到真实主机，"+
			"静默回退成了内置假主机 fakePreviewHost（SN=%q），前端无法察觉",
			exposedHostID, realSN, capturedHostSN, fakePreviewHost.SN)
	}
}

// TestTemplate_SaveRejectsUnknownHookDigest 锁住「保存模板时引用了不存在
// 的 digest 被拒」（本实现按 hookId 解析出 active 版本自动填 digest，
// 因此「不存在的 digest」体现为「引用了不存在或没有 active 版本的
// hookId」）。
func TestTemplate_SaveRejectsUnknownHookDigest(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-5", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-5", "artifactSetId": set.ID,
		"hookRefs": []map[string]any{{"hookId": "no-such-hook", "stage": "post", "order": 10}},
	})
	// HTTP 状态码必须是 404（迁移契约 §2：CodeNotFound 对应 404）——引用的
	// hookId 没有 active 版本，本质是「引用了不存在的资源」，不是 200。
	if rr.Code != http.StatusNotFound {
		t.Errorf("引用不存在的 hook 时 HTTP 状态码 = %d, want %d（迁移契约 §2：CodeNotFound 对应 404）",
			rr.Code, http.StatusNotFound)
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code == int(apierr.CodeOK) {
		t.Errorf("引用不存在的 hook 应被拒，实际成功")
	}
}

// TestTemplate_SaveRejectsIncompatibleFamily 锁住「保存模板时 hook 家族
// 与制品集不兼容被拒」。
func TestTemplate_SaveRejectsIncompatibleFamily(t *testing.T) {
	ts := newTestServer(t)
	set := seedActiveArtifactSet(t, ts, "as-6", model.OSFamilyESXi)
	hook := seedActiveHook(t, ts, "hook-rhel-only", model.HookStagePost,
		[]model.OSFamily{model.OSFamilyRHEL}, "")
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-6", "artifactSetId": set.ID,
		"hookRefs": []map[string]any{{"hookId": hook.ID, "stage": "post", "order": 10}},
	})
	// HTTP 状态码必须是 400（迁移契约 §2：CodeBadRequest 对应 400）——家族
	// 不兼容是请求本身的语义错误（客户端传的 hookRefs 与 artifactSetId
	// 组合不合法），不是服务端内部错误也不是「资源不存在」。
	if rr.Code != http.StatusBadRequest {
		t.Errorf("hook 家族不兼容时 HTTP 状态码 = %d, want %d（迁移契约 §2：CodeBadRequest 对应 400）",
			rr.Code, http.StatusBadRequest)
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code == int(apierr.CodeOK) {
		t.Errorf("hook 家族限定为 rhel、制品集是 esxi 时应被拒，实际成功")
	}
}

func mapKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestTemplate_PreviewPassesRootSSHPublicKeys 锁住「preview 端点把
// Deps.RootSSHPublicKeys 传进它自己构造的 RenderContext」。
//
// 为什么这条重要：preview 是少数自己拼 model.RenderContext 字面量的地方
// （gateway 走 buildRenderContext，两条路径独立）。而渲染器对空公钥列表
// 是硬拒绝——SPEC §1.5 要求 root 不设可复用密码、公钥是唯一登录通路，
// 写占位公钥会静默产出一台没人能登录的机器，所以渲染器宁可报错。
//
// 漏传的后果是 preview 对任何模板都返回渲染失败，而 §8.2 说明 preview 与
// dryrun 是开发期性价比最高的两个端点（把模板调试周期从「上电等十分钟」
// 压到秒级）。这个缺口实际发生过一次：新增公钥字段时 gateway 那条路径
// 改了、preview 这条漏了，而单元测试用的 stubRenderer 不做校验、测不出来，
// 只有 e2e 的阶段 5 才暴露。这条测试把它拉回单元测试层。
func TestTemplate_PreviewPassesRootSSHPublicKeys(t *testing.T) {
	ts := newTestServer(t)
	wantKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItestkeyfordeps admin@ops"
	ts.Server.deps.RootSSHPublicKeys = []string{wantKey}

	var gotKeys []string
	ts.Server.deps.Renderer = &stubRenderer{
		answerFunc: func(ctx model.RenderContext, _ string) (string, error) {
			gotKeys = ctx.RootSSHPublicKeys
			return "# fake answer", nil
		},
	}

	set := seedActiveArtifactSet(t, ts, "as-sshkey", model.OSFamilyRHEL)
	tok := ts.login("eve-admin")
	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-sshkey", "artifactSetId": set.ID, "answerTemplate": "# ks\n",
	})
	var created struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("POST", "/console/v1/templates/"+created.Data.ID+"/preview", tok, map[string]any{
		"formData": map[string]string{"hostname": "web-01"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("preview 失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	if len(gotKeys) != 1 || gotKeys[0] != wantKey {
		t.Errorf("RenderContext.RootSSHPublicKeys = %v, want [%s] —— preview 漏传这一项会让它对"+
			"任何模板都渲染失败（渲染器对空公钥硬拒绝，SPEC §1.5）", gotKeys, wantKey)
	}
}
