// 本文件测试制品集四个端点。对应实现计划任务 4「测试要锁住的行为」制品集
// 部分。
package console

import (
	"net/http"
	"testing"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/model"
)

// TestArtifact_CreateListVerify 覆盖登记、列表、校验三个端点的基本流程。
func TestArtifact_CreateListVerify(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/artifact-sets", tok, map[string]string{
		"sourcePath": "/data/isos/rhel9.iso", "name": "rhel9-x86_64",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("登记失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data artifactSetItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)
	if created.Data.State != string(model.ArtifactSetImporting) {
		t.Errorf("登记后 state 应为 importing，实际 %q", created.Data.State)
	}

	rr = ts.doJSON("GET", "/console/v1/artifact-sets", tok, nil)
	var listed struct {
		Data []artifactSetItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &listed)
	if len(listed.Data) != 1 {
		t.Fatalf("列表应有 1 条，实际 %d", len(listed.Data))
	}

	rr = ts.doJSON("POST", "/console/v1/artifact-sets/"+created.Data.ID+"/verify", tok, nil)
	if rr.Code != http.StatusOK {
		t.Errorf("校验完整性应返回 HTTP 200，实际 %d", rr.Code)
	}
}

// TestArtifact_DeleteRejectedWhenReferenced 锁住「制品集删除在有 active
// 模板引用时回对应错误码」。
func TestArtifact_DeleteRejectedWhenReferenced(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	set, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "as-1", Name: "x", State: model.ArtifactSetDeprecated,
	})
	if err != nil {
		t.Fatalf("准备制品集失败: %v", err)
	}
	if _, err := ts.Store.CreateTemplate(ctx, model.Template{
		ID: "tpl-1", Name: "y", ArtifactSetID: set.ID, Version: 1, State: model.TemplateStateActive,
	}); err != nil {
		t.Fatalf("准备模板失败: %v", err)
	}

	tok := ts.login("eve-admin")
	rr := ts.doJSON("DELETE", "/console/v1/artifact-sets/"+set.ID, tok, nil)
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeStillReferenced) {
		t.Errorf("被引用时删除应回 CodeStillReferenced(%d)，实际 code=%d", apierr.CodeStillReferenced, env.Code)
	}
}

// TestArtifact_DeleteRejectedWhenNotDeprecated 验证非 deprecated 状态
// 删除被拒（§2.7 前置条件）。
func TestArtifact_DeleteRejectedWhenNotDeprecated(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	set, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "as-2", Name: "x", State: model.ArtifactSetReady,
	})
	if err != nil {
		t.Fatalf("准备制品集失败: %v", err)
	}

	tok := ts.login("eve-admin")
	rr := ts.doJSON("DELETE", "/console/v1/artifact-sets/"+set.ID, tok, nil)
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeConflict) {
		t.Errorf("非 deprecated 状态删除应回 CodeConflict(%d)，实际 code=%d", apierr.CodeConflict, env.Code)
	}
}

// TestArtifact_DeleteSucceedsWhenDeprecatedAndUnreferenced 验证正常删除
// 路径。
func TestArtifact_DeleteSucceedsWhenDeprecatedAndUnreferenced(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	set, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "as-3", Name: "x", State: model.ArtifactSetDeprecated,
	})
	if err != nil {
		t.Fatalf("准备制品集失败: %v", err)
	}

	tok := ts.login("eve-admin")
	rr := ts.doJSON("DELETE", "/console/v1/artifact-sets/"+set.ID, tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("正常删除应返回 HTTP 200，实际 %d，body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeOK) {
		t.Errorf("正常删除应成功，实际 code=%d", env.Code)
	}
}
