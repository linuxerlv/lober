// 本文件测试制品集四个端点。对应实现计划任务 4「测试要锁住的行为」制品集
// 部分，本轮新增「登记触发异步导入」相关测试。
package console

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/model"
)

// fakeArtifactImporter 是 ArtifactImporter 的可控测试替身：记录每次
// TriggerImport 调用的参数，并按测试预设的行为（成功/失败/保持中间态）
// 驱动 store 的状态流转——不真的解压任何文件，只验证「登记端点确实触发
// 了导入，且导入完成后状态被正确写回」这条契约（任务要求「用可控的假
// 导入器测，不要真解压」）。
type fakeArtifactImporter struct {
	mu    sync.Mutex
	calls []fakeImportCall

	// onTrigger 是每次 TriggerImport 被调用时执行的行为，测试按需覆盖：
	// nil 时默认什么也不做（制品集停留在 importing，供测试断言「触发了
	// 但尚未完成」这一中间状态）。测试用闭包捕获自己的 ts.Store，不由本
	// 类型持有 store 引用——本替身只关心「TriggerImport 被调用时应该
	// 发生什么」，不关心具体是哪个 store 实现。
	onTrigger func(ctx context.Context, artifactSetID, isoPath string)
}

type fakeImportCall struct {
	ArtifactSetID string
	ISOPath       string
}

func (f *fakeArtifactImporter) TriggerImport(ctx context.Context, artifactSetID string, src ImportSource) {
	isoPath := src.ISOPath
	_ = isoPath
	f.mu.Lock()
	f.calls = append(f.calls, fakeImportCall{ArtifactSetID: artifactSetID, ISOPath: isoPath})
	onTrigger := f.onTrigger
	f.mu.Unlock()

	if onTrigger != nil {
		onTrigger(ctx, artifactSetID, isoPath)
	}
}

func (f *fakeArtifactImporter) triggeredCalls() []fakeImportCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeImportCall, len(f.calls))
	copy(out, f.calls)
	return out
}

var _ ArtifactImporter = (*fakeArtifactImporter)(nil)

// fakeArtifactVerifier 是 ArtifactVerifier 的可控测试替身：记录每次
// TriggerVerify 调用的 artifactSetID，并按测试预设的 onTrigger 行为驱动
// store——不真的重算任何 sha256，只验证「校验端点确实异步触发了校验，且
// 端点本身不等触发完成就返回」这条契约。
type fakeArtifactVerifier struct {
	mu    sync.Mutex
	calls []string

	// onTrigger 是每次 TriggerVerify 被调用时执行的行为，nil 时什么也
	// 不做（仅记录调用）。
	onTrigger func(ctx context.Context, artifactSetID string)
}

// TriggerVerify 记录调用后立即返回，onTrigger（若非 nil）在独立 goroutine
// 里执行——接口文档要求实现方自己负责异步（不能阻塞调用方），真实实现
// （cmd/lober/verifier.go）用 go func(){...}() 满足这条要求，本测试替身
// 必须模拟同样的义务：否则用阻塞 channel 断言「handler 不等触发完成」的
// 测试会在假替身这一层同步阻塞住，而不是在验证 handler 本身是否异步。
func (f *fakeArtifactVerifier) TriggerVerify(ctx context.Context, artifactSetID string) {
	f.mu.Lock()
	f.calls = append(f.calls, artifactSetID)
	onTrigger := f.onTrigger
	f.mu.Unlock()

	if onTrigger != nil {
		go onTrigger(ctx, artifactSetID)
	}
}

func (f *fakeArtifactVerifier) triggeredIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

var _ ArtifactVerifier = (*fakeArtifactVerifier)(nil)

// TestArtifact_CreateTriggersImportAndImportingUntilJobCompletes 锁住
// 「登记端点返回后制品集是 importing」这一条：导入器的 onTrigger 故意留空
// （不立即完成），断言登记响应与紧随其后的一次 GET 都看到 importing。
func TestArtifact_CreateTriggersImportAndImportingUntilJobCompletes(t *testing.T) {
	ts := newTestServer(t)
	importer := &fakeArtifactImporter{}
	ts.Server.deps.ArtifactImporter = importer

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
		t.Fatalf("登记响应的 state 应为 importing，实际 %q", created.Data.State)
	}

	calls := importer.triggeredCalls()
	if len(calls) != 1 {
		t.Fatalf("登记应触发恰好 1 次导入，实际 %d 次", len(calls))
	}
	if calls[0].ArtifactSetID != created.Data.ID {
		t.Errorf("触发导入的 artifactSetID = %q, want %q", calls[0].ArtifactSetID, created.Data.ID)
	}
	if calls[0].ISOPath != "/data/isos/rhel9.iso" {
		t.Errorf("触发导入的 isoPath = %q, want %q（应与登记请求的 sourcePath 一致）",
			calls[0].ISOPath, "/data/isos/rhel9.iso")
	}

	rr = ts.doJSON("GET", "/console/v1/artifact-sets", tok, nil)
	var listed struct {
		Data []artifactSetItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &listed)
	if len(listed.Data) != 1 || listed.Data[0].State != string(model.ArtifactSetImporting) {
		t.Fatalf("导入器尚未完成时，列表应仍显示 importing，实际 %+v", listed.Data)
	}
}

// TestArtifact_CreateThenImportCompletes_BecomesReady 锁住「导入完成后
// 变 ready」：导入器的 onTrigger 同步调用 store 把状态推进到 ready，断言
// 紧随其后的 GET 看到 ready。
func TestArtifact_CreateThenImportCompletes_BecomesReady(t *testing.T) {
	ts := newTestServer(t)
	importer := &fakeArtifactImporter{
		onTrigger: func(ctx context.Context, artifactSetID, isoPath string) {
			if _, err := ts.Store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetReady, "sha256-abc", ""); err != nil {
				t.Errorf("假导入器写回 ready 失败: %v", err)
			}
		},
	}
	ts.Server.deps.ArtifactImporter = importer

	tok := ts.login("eve-admin")
	rr := ts.doJSON("POST", "/console/v1/artifact-sets", tok, map[string]string{
		"sourcePath": "/data/isos/rhel9.iso", "name": "rhel9-x86_64",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("登记失败: %d, body=%s", rr.Code, rr.Body.String())
	}

	rr = ts.doJSON("GET", "/console/v1/artifact-sets", tok, nil)
	var listed struct {
		Data []artifactSetItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &listed)
	if len(listed.Data) != 1 {
		t.Fatalf("列表应有 1 条，实际 %d", len(listed.Data))
	}
	if listed.Data[0].State != string(model.ArtifactSetReady) {
		t.Errorf("导入完成后 state 应为 ready，实际 %q", listed.Data[0].State)
	}
	if listed.Data[0].ManifestSHA256 != "sha256-abc" {
		t.Errorf("导入完成后应记录 manifest 校验值，实际 %q", listed.Data[0].ManifestSHA256)
	}
}

// TestArtifact_CreateThenImportFails_BecomesFailed 锁住「导入失败时状态是
// failed 且带原因，不是卡在 importing」——本测试的假导入器模拟一次真实
// job 失败后的行为（写回 failed），验证的是「控制台端点确实观察到了这次
// 失败」，而不是重新验证 imports.Job 自己的失败写回逻辑（那部分已经在
// internal/imports/job_test.go 里被覆盖）。
//
// 本测试曾经断言「state 变为 failed」是现有字段能表达的最大程度，不断言
// 任何原因文本（model.ArtifactSet 当时没有专门的失败原因字段）——这一轮
// 契约变更给 ArtifactSet 补上了 FailReason 字段并接通了控制台响应，本测试
// 现在断言原因文本确实能被运维在列表响应里看到，这正是本次改动的核心
// 目的（t01 记录的契约缺口在这里被真正闭环）。
func TestArtifact_CreateThenImportFails_BecomesFailed(t *testing.T) {
	ts := newTestServer(t)
	const wantReason = "识别不出 OS 家族: 未知的 ISO 结构"
	importer := &fakeArtifactImporter{
		onTrigger: func(ctx context.Context, artifactSetID, isoPath string) {
			// 失败时只调 UpdateArtifactSetState，不调 SetArtifactSetContent
			// ——与 imports.Job.fail 的分工一致（manifestSHA256 传空字符串，
			// 不复用它承载错误文本；reason 走新增的第五个参数）。
			if _, err := ts.Store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetFailed, "", wantReason); err != nil {
				t.Errorf("假导入器写回 failed 失败: %v", err)
			}
		},
	}
	ts.Server.deps.ArtifactImporter = importer

	tok := ts.login("eve-admin")
	rr := ts.doJSON("POST", "/console/v1/artifact-sets", tok, map[string]string{
		"sourcePath": "/data/isos/broken.iso", "name": "broken",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("登记失败: %d, body=%s", rr.Code, rr.Body.String())
	}

	rr = ts.doJSON("GET", "/console/v1/artifact-sets", tok, nil)
	var listed struct {
		Data []artifactSetItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &listed)
	if len(listed.Data) != 1 {
		t.Fatalf("列表应有 1 条，实际 %d", len(listed.Data))
	}
	if listed.Data[0].State != string(model.ArtifactSetFailed) {
		t.Errorf("导入失败后 state 应为 failed，不应卡在 importing，实际 %q", listed.Data[0].State)
	}
	if listed.Data[0].FailReason != wantReason {
		t.Errorf("导入失败后列表响应应带上具体原因，want %q, got %q —— "+
			"运维在页面上只看到一个 failed 而不知道为什么，是本次改动要解决的核心问题",
			wantReason, listed.Data[0].FailReason)
	}
}

// TestArtifact_CreateThenImportSucceeds_FailReasonEmpty 锁住「导入成功后
// FailReason 为空」——即便这是一次失败重试后的成功（模拟一次先失败再
// 成功的场景），成功路径必须清空上一次遗留的失败原因，不能让运维看到一个
// state=ready 却还挂着旧错误文本的制品集。
func TestArtifact_CreateThenImportSucceeds_FailReasonEmpty(t *testing.T) {
	ts := newTestServer(t)
	importer := &fakeArtifactImporter{
		onTrigger: func(ctx context.Context, artifactSetID, isoPath string) {
			// 模拟先失败一次（写入一条原因），再重试成功——成功路径必须
			// 覆盖掉这条旧原因，不能让它残留。
			if _, err := ts.Store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetFailed, "", "第一次尝试失败"); err != nil {
				t.Fatalf("模拟首次失败写回失败: %v", err)
			}
			if _, err := ts.Store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetReady, "sha256-retry-ok", ""); err != nil {
				t.Errorf("假导入器写回 ready 失败: %v", err)
			}
		},
	}
	ts.Server.deps.ArtifactImporter = importer

	tok := ts.login("eve-admin")
	rr := ts.doJSON("POST", "/console/v1/artifact-sets", tok, map[string]string{
		"sourcePath": "/data/isos/retry.iso", "name": "retry",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("登记失败: %d, body=%s", rr.Code, rr.Body.String())
	}

	rr = ts.doJSON("GET", "/console/v1/artifact-sets", tok, nil)
	var listed struct {
		Data []artifactSetItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &listed)
	if len(listed.Data) != 1 {
		t.Fatalf("列表应有 1 条，实际 %d", len(listed.Data))
	}
	if listed.Data[0].State != string(model.ArtifactSetReady) {
		t.Errorf("重试成功后 state 应为 ready，实际 %q", listed.Data[0].State)
	}
	if listed.Data[0].FailReason != "" {
		t.Errorf("重试成功后 FailReason 应被清空，实际残留 %q —— "+
			"运维会看到一个已经 ready 但还挂着上一次失败原因的制品集，误以为它仍有问题",
			listed.Data[0].FailReason)
	}
}

// TestArtifact_CreateWithNilImporter_StaysImporting 锁住「Deps.ArtifactImporter
// 为 nil 时登记端点仍能正常工作，只是不触发导入」——这是本次改动引入
// 新字段时必须保持的向后兼容行为（既有测试/既有部署形态可能不注入这个
// 依赖）。
func TestArtifact_CreateWithNilImporter_StaysImporting(t *testing.T) {
	ts := newTestServer(t)
	// 有意不设置 ts.Server.deps.ArtifactImporter（保持零值 nil）。

	tok := ts.login("eve-admin")
	rr := ts.doJSON("POST", "/console/v1/artifact-sets", tok, map[string]string{
		"sourcePath": "/data/isos/rhel9.iso", "name": "rhel9-x86_64",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("登记失败（ArtifactImporter 为 nil 时也应正常登记): %d, body=%s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data artifactSetItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)
	if created.Data.State != string(model.ArtifactSetImporting) {
		t.Errorf("state 应为 importing，实际 %q", created.Data.State)
	}
}

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

// TestArtifact_VerifyTriggersVerifierAndReturnsImmediately 锁住「校验端点
// 确实调用了 Deps.ArtifactVerifier.TriggerVerify，且不等它跑完就返回」——
// 假验证器的 onTrigger 故意阻塞在一个 channel 上，若 handler 是同步等待
// 触发完成才写响应，本测试会超时挂起；若 handler 确实异步触发、立即
// 返回，doJSON 会在假验证器仍阻塞的情况下先拿到 HTTP 200。
func TestArtifact_VerifyTriggersVerifierAndReturnsImmediately(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	set, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "as-verify-1", Name: "rhel9-x86_64", State: model.ArtifactSetReady, ManifestSHA256: "sha256-orig",
	})
	if err != nil {
		t.Fatalf("准备制品集失败: %v", err)
	}

	block := make(chan struct{})
	verifier := &fakeArtifactVerifier{
		onTrigger: func(ctx context.Context, artifactSetID string) {
			<-block // 故意阻塞：只有测试主动关闭 block 才会解除。
		},
	}
	ts.Server.deps.ArtifactVerifier = verifier
	defer close(block) // 测试结束前释放，避免遗留 goroutine。

	tok := ts.login("eve-admin")
	rr := ts.doJSON("POST", "/console/v1/artifact-sets/"+set.ID+"/verify", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("校验端点应立即返回 HTTP 200（不等触发完成），实际 %d, body=%s", rr.Code, rr.Body.String())
	}

	calls := verifier.triggeredIDs()
	if len(calls) != 1 || calls[0] != set.ID {
		t.Errorf("应恰好触发 1 次校验，且 artifactSetID = %q，实际 %v", set.ID, calls)
	}
}

// TestArtifact_VerifyNotFound 锁住「校验不存在的制品集返回
// apierr.CodeNotFound」（任务要求「不存在的制品集返回 1004」）。
func TestArtifact_VerifyNotFound(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/artifact-sets/does-not-exist/verify", tok, nil)
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeNotFound) {
		t.Errorf("校验不存在的制品集应回 CodeNotFound(%d)，实际 code=%d", apierr.CodeNotFound, env.Code)
	}
}

// TestArtifact_VerifyNotFound_DoesNotTriggerVerifier 锁住「制品集不存在
// 时不应该触发校验器」——不存在的 ID 没有意义去触发一次「校验」，这与
// handleCreateArtifactSet 的「先落库成功才触发导入」是同一种顺序纪律。
func TestArtifact_VerifyNotFound_DoesNotTriggerVerifier(t *testing.T) {
	ts := newTestServer(t)
	verifier := &fakeArtifactVerifier{}
	ts.Server.deps.ArtifactVerifier = verifier

	tok := ts.login("eve-admin")
	ts.doJSON("POST", "/console/v1/artifact-sets/does-not-exist/verify", tok, nil)

	if calls := verifier.triggeredIDs(); len(calls) != 0 {
		t.Errorf("制品集不存在时不应触发校验，实际触发了 %v", calls)
	}
}

// TestArtifact_VerifyWithNilVerifier_StillReturnsOK 锁住「Deps.ArtifactVerifier
// 为 nil 时校验端点仍正常返回接受」——这是按需注入依赖缺省时的既有降级
// 预期（与 Deps.ArtifactImporter 判空同一套设计）。
func TestArtifact_VerifyWithNilVerifier_StillReturnsOK(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()
	// 有意不设置 ts.Server.deps.ArtifactVerifier（保持零值 nil）。

	set, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "as-verify-nil", Name: "x", State: model.ArtifactSetReady,
	})
	if err != nil {
		t.Fatalf("准备制品集失败: %v", err)
	}

	tok := ts.login("eve-admin")
	rr := ts.doJSON("POST", "/console/v1/artifact-sets/"+set.ID+"/verify", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("ArtifactVerifier 为 nil 时校验端点也应返回 HTTP 200: %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeOK) {
		t.Errorf("ArtifactVerifier 为 nil 时校验端点应返回成功，实际 code=%d", env.Code)
	}
}

// TestArtifact_DeleteRejectedWhenReferenced 锁住「制品集删除在有 active
// 模板引用时回对应错误码」——迁移契约 §0/§2 冻结 CodeStillReferenced 对应
// 真实 HTTP 409（不是迁移前遗留的 http.StatusOK 占位值），本测试同时断言
// HTTP 状态码与业务码，这是本次任务说明里特别要求锁住的一条行为。
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
	if rr.Code != http.StatusConflict {
		t.Errorf("被引用时删除应回 HTTP 409，实际 %d，body=%s", rr.Code, rr.Body.String())
	}
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

// ---- gin 迁移新增：请求解析与手写校验的行为锁（迁移契约 §7） ----
//
// artifact.go 的三条手写校验（sourcePath 非空、name 非空、delete 前置
// state 检查）迁移前从未被测试覆盖过——本轮迁移改变了请求解析的载体
// （json.NewDecoder 换成 c.ShouldBindJSON），必须先把这些校验行为锁进
// 测试，才能安全地换底层实现；state 检查已有
// TestArtifact_DeleteRejectedWhenNotDeprecated 覆盖，本节只补 sourcePath/
// name 与「请求体不是合法 JSON」两类此前完全没有测试的分支。

// TestArtifact_CreateRejectsEmptySourcePath 锁住 sourcePath 为空字符串时
// 返回 CodeBadRequest，且不落库、不触发导入。
func TestArtifact_CreateRejectsEmptySourcePath(t *testing.T) {
	ts := newTestServer(t)
	importer := &fakeArtifactImporter{}
	ts.Server.deps.ArtifactImporter = importer
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/artifact-sets", tok, map[string]string{
		"sourcePath": "", "name": "rhel9-x86_64",
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeBadRequest) {
		t.Errorf("sourcePath 为空应回 CodeBadRequest(%d)，实际 code=%d，body=%s",
			apierr.CodeBadRequest, env.Code, rr.Body.String())
	}
	if calls := importer.triggeredCalls(); len(calls) != 0 {
		t.Errorf("sourcePath 校验失败时不应触发导入，实际触发了 %v", calls)
	}
}

// TestArtifact_CreateRejectsWhitespaceOnlySourcePath 锁住
// sourcePath 全是空格时同样视为空——迁移前用 strings.TrimSpace 判断，这条
// 校验强度比 gin binding 的 required 标签更严（required 只拒绝字面空
// 字符串，不拒绝纯空格），迁移不能弱化这一条（迁移契约 §7：「校验条件
// 一条都不能少」），因此本条校验保留为 Go 代码，不改用 binding 标签。
func TestArtifact_CreateRejectsWhitespaceOnlySourcePath(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/artifact-sets", tok, map[string]string{
		"sourcePath": "   ", "name": "rhel9-x86_64",
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeBadRequest) {
		t.Errorf("sourcePath 全是空格应视为空并回 CodeBadRequest(%d)，实际 code=%d，body=%s",
			apierr.CodeBadRequest, env.Code, rr.Body.String())
	}
}

// TestArtifact_CreateRejectsEmptyName 锁住 name 为空字符串时返回
// CodeBadRequest。
func TestArtifact_CreateRejectsEmptyName(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/artifact-sets", tok, map[string]string{
		"sourcePath": "/data/isos/rhel9.iso", "name": "",
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeBadRequest) {
		t.Errorf("name 为空应回 CodeBadRequest(%d)，实际 code=%d，body=%s",
			apierr.CodeBadRequest, env.Code, rr.Body.String())
	}
}

// TestArtifact_CreateRejectsWhitespaceOnlyName 锁住 name 全是空格时同样
// 视为空，理由与 sourcePath 的空格测试相同。
func TestArtifact_CreateRejectsWhitespaceOnlyName(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/artifact-sets", tok, map[string]string{
		"sourcePath": "/data/isos/rhel9.iso", "name": "\t\t",
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeBadRequest) {
		t.Errorf("name 全是空白字符应视为空并回 CodeBadRequest(%d)，实际 code=%d，body=%s",
			apierr.CodeBadRequest, env.Code, rr.Body.String())
	}
}

// TestArtifact_CreateRejectsMalformedJSON 锁住「请求体不是合法 JSON」这条
// 分支——本轮迁移把解析载体从 json.NewDecoder 换成 c.ShouldBindJSON，两者
// 对语法错误的 JSON 都会返回非 nil error，但 c.ShouldBindJSON 出错时的
// error 类型不是 validator.ValidationErrors，必须单独判断分流到
// CodeBadRequest，否则会被 apierr.Fail 的兜底分支误判成 CodeInternal
// （见 gin 迁移契约 §3 对 Fail 判定顺序的说明：未识别错误才落 1000）。
// 用 httptest.NewRequest 直接发裸 body，不能用 doJSON——它总是先把
// body 序列化成合法 JSON。
func TestArtifact_CreateRejectsMalformedJSON(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	req := httptest.NewRequest("POST", "/console/v1/artifact-sets", bytes.NewReader([]byte("{not valid json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
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
