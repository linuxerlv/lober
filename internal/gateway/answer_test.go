// 本文件测试应答文件端点 GET /v1/answer/{task}/{token}/{file}。对应实现
// 计划任务 6，依据 docs/contracts/http-machine.md 端点 3、SPEC §4.4、§4.5、
// §5.1。
//
// 注意：本文件的错误响应形态严格按 docs/contracts/http-machine.md（冻结
// 文档）实现，而不是按计划任务 6 文字描述的「渲染失败 → 200 加注释文本」——
// 两者在这一点上有冲突，冻结契约文档具有更高权威性（任务说明「契约是
// docs/contracts/http-machine.md，路径、响应体、状态码按它实现，不自己
// 发明」）。契约文档端点 3「错误行为」表明确：渲染出错 → 500 + TaskEvent，
// 不是 200。这条偏差已记入本模块返回值的 contractGaps。
package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// answerFixture 打包一次应答文件端点测试所需的前置数据：一个处于
// booting 状态、持有装机 token 的任务。
type answerFixture struct {
	deps  Deps
	store *fake.Store
	task  model.Task
}

// newAnswerFixture 落库一个 state=booting、Token=fixedInstallToken 的任务。
func newAnswerFixture(t *testing.T, state model.TaskState) answerFixture {
	t.Helper()
	store := fake.NewStore()
	ctx := t.Context()

	task := model.Task{
		ID: "task-ans-1", HostSN: "SN-1",
		TemplateID: "tpl-ans-1", TemplateVersion: 1, ArtifactSetID: "art-1",
		State: state, Token: "fixed-install-token", TokenExpiresAt: 9_999_999_999,
	}
	created, err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	host := model.Host{SN: "SN-1", MAC: "aa:bb:cc:dd:ee:ff", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI}
	if _, err := store.UpsertHost(ctx, host); err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}

	tpl := model.Template{ID: "tpl-ans-1", Version: 1, ArtifactSetID: "art-1", State: model.TemplateStateActive}
	if _, err := store.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}

	artifact := model.ArtifactSet{ID: "art-1", OSFamily: model.OSFamilyRHEL, State: model.ArtifactSetReady}
	if _, err := store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	deps := newTestDeps()
	deps.TaskStore = store
	deps.FactStore = store
	deps.EventStore = store
	deps.TemplateStore = store
	deps.ArtifactSetStore = store
	deps.HostStore = store

	return answerFixture{deps: deps, store: store, task: created}
}

// doAnswer 请求 GET /v1/answer/{task}/{token}/{file}。
func doAnswer(t *testing.T, deps Deps, taskID, token, file string) (body string, status int) {
	t.Helper()
	srv := NewServer(deps)

	req := httptest.NewRequest(http.MethodGet, "/v1/answer/"+taskID+"/"+token+"/"+file, nil)
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

// TestAnswer_HappyPath_ReturnsRenderedContentAndMarksFetched 锁住：正常
// 路径返回渲染结果，且状态机的「已取」被调用。
func TestAnswer_HappyPath_ReturnsRenderedContentAndMarksFetched(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)
	sm := newStateMachineStub()
	fx.deps.StateMachine = sm
	renderer := newRendererStub()
	fx.deps.Renderer = renderer

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d", status, http.StatusOK)
	}
	if body == "" {
		t.Error("响应体为空，want 渲染出的应答文件正文")
	}
	if len(renderer.renderAnswerFileCalls) != 1 {
		t.Fatalf("RenderAnswerFile 被调用 %d 次, want 1", len(renderer.renderAnswerFileCalls))
	}
	if renderer.renderAnswerFileCalls[0].fileName != "ks.cfg" {
		t.Errorf("RenderAnswerFile 传入的 fileName = %q, want %q", renderer.renderAnswerFileCalls[0].fileName, "ks.cfg")
	}
}

// TestAnswer_InvalidToken_Returns403Empty 锁住：token 无效 → 403 空响应。
func TestAnswer_InvalidToken_Returns403Empty(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)

	body, status := doAnswer(t, fx.deps, fx.task.ID, "wrong-token", "ks.cfg")

	if status != http.StatusForbidden {
		t.Errorf("状态码 = %d, want %d", status, http.StatusForbidden)
	}
	if body != "" {
		t.Errorf("响应体 = %q, want 空——token 无效时不该产出任何内容", body)
	}
}

// TestAnswer_StateNotGated_Returns403 锁住：状态不在门控集合内 → 403
// （用 done 状态验证）。
func TestAnswer_StateNotGated_Returns403(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateDone)

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status != http.StatusForbidden {
		t.Errorf("状态码 = %d, want %d —— done 不在 {booting, installing} 门控集合内", status, http.StatusForbidden)
	}
	if body != "" {
		t.Errorf("响应体 = %q, want 空", body)
	}
}

// TestAnswer_PathTaskIDMismatchesToken_Returns403 锁住：路径 id 与 token
// 解出的任务不一致 → 403。这条不能省——不校验的后果是可以拿 A 任务的
// token 配 B 任务的 id 去取应答文件，路径里那个 id 会被用于构造渲染上下
// 文，不一致时会渲染出张冠李戴的内容。
func TestAnswer_PathTaskIDMismatchesToken_Returns403(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)

	body, status := doAnswer(t, fx.deps, "some-other-task-id", "fixed-install-token", "ks.cfg")

	if status != http.StatusForbidden {
		t.Errorf("状态码 = %d, want %d", status, http.StatusForbidden)
	}
	if body != "" {
		t.Errorf("响应体 = %q, want 空", body)
	}
}

// TestAnswer_FileNameMismatch_Returns404 锁住约定 4：{file} 与
// RenderContext.AnswerFileName() 不一致 → 404（不是模糊匹配）。
func TestAnswer_FileNameMismatch_Returns404(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)

	_, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "wrong-name.cfg")

	if status != http.StatusNotFound {
		t.Errorf("状态码 = %d, want %d —— {file} 与该任务 AnswerFileName() 不一致时"+
			"必须 404，不接受 handler 自行拼接的候选名单", status, http.StatusNotFound)
	}
}

// TestAnswer_RenderFails_Returns500AndRecordsEvent 锁住契约文档端点 3
// 「错误行为」：渲染本身出错（模板/参数问题）→ 500，body 为简短纯文本
// 原因；同时记一条 TaskEvent（level=error）。
func TestAnswer_RenderFails_Returns500AndRecordsEvent(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)
	renderer := newRendererStub()
	renderer.renderAnswerFileFn = func(ctx model.RenderContext, fileName string) (string, error) {
		return "", errRenderFailureInTest
	}
	fx.deps.Renderer = renderer

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status != http.StatusInternalServerError {
		t.Errorf("状态码 = %d, want %d", status, http.StatusInternalServerError)
	}
	if body == "" {
		t.Error("响应体为空，want 简短纯文本原因")
	}

	events, err := fx.store.ListEvents(t.Context(), fx.task.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Level == model.EventLevelError {
			found = true
		}
	}
	if !found {
		t.Errorf("渲染失败后未找到 level=error 的 TaskEvent，events=%+v", events)
	}
}

// errRenderFailureInTest 是本文件测试内部用的占位渲染错误。
var errRenderFailureInTest = &stubError{"stub: 渲染失败"}

// TestAnswer_RepeatedRequests_BothSucceed 锁住：重复请求两次都成功
// （幂等，对应 cloud-init 取两个文件）。
func TestAnswer_RepeatedRequests_BothSucceed(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)

	body1, status1 := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")
	body2, status2 := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status1 != http.StatusOK || status2 != http.StatusOK {
		t.Fatalf("两次请求状态码 = %d, %d, want 都是 %d", status1, status2, http.StatusOK)
	}
	if body1 == "" || body2 == "" {
		t.Error("两次请求响应体都不该为空")
	}
}

// TestAnswer_MarkAnswerFetchedCalledForEachRequest 单独锁住 MarkAnswerFetched
// 被调用的事实（不依赖具体实现是否真的推进状态——那是状态机的职责，网关
// 只需要调用它）。
func TestAnswer_MarkAnswerFetchedCalledForEachRequest(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)
	sm := newStateMachineStub()
	var markCalls int
	sm.markAnswerFetchedFn = func(ctx context.Context, taskID string) (model.Task, error) {
		markCalls++
		return model.Task{ID: taskID, State: model.TaskStateInstalling}, nil
	}
	fx.deps.StateMachine = sm

	doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if markCalls != 1 {
		t.Errorf("MarkAnswerFetched 被调用 %d 次, want 1", markCalls)
	}
}
