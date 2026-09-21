// 本文件测试模板四个端点。对应实现计划任务 5「测试要锁住的行为」。
package console

import (
	"encoding/json"
	"net/http"
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
