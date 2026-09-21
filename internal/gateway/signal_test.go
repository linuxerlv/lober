// 本文件测试信号端点 POST /v1/signal/{task}/{token}/{phase}。对应实现
// 计划任务 7，依据 docs/contracts/http-machine.md 端点 4、SPEC §6.1、§6.2、
// §6.4。
//
// 成功响应状态码严格按 docs/contracts/http-machine.md（冻结契约）：
// 200 空 body。这与计划任务 7 文字描述的「返回 204」不同——冻结契约文档
// 优先，偏差已记入本模块返回值的 contractGaps。
package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// signalFixture 打包信号端点测试所需的前置数据：一个持有装机 token 与
// finalize token 的任务。
type signalFixture struct {
	deps  Deps
	store *fake.Store
	task  model.Task
}

const (
	signalFixtureInstallToken  = "fixed-install-token"
	signalFixtureFinalizeToken = "fixed-finalize-token"
)

func newSignalFixture(t *testing.T) signalFixture {
	t.Helper()
	store := fake.NewStore()
	ctx := t.Context()

	task := model.Task{
		ID: "task-sig-1", HostSN: "SN-1",
		State: model.TaskStateInstalling, CurrentPhase: model.PhasePre,
		Token: signalFixtureInstallToken, TokenExpiresAt: 9_999_999_999,
		FinalizeToken: signalFixtureFinalizeToken, FinalizeExpiresAt: 9_999_999_999,
	}
	created, err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	deps := newTestDeps()
	deps.TaskStore = store
	deps.FactStore = store
	deps.EventStore = store
	deps.TemplateStore = store
	deps.ArtifactSetStore = store
	deps.HostStore = store

	return signalFixture{deps: deps, store: store, task: created}
}

// doSignal 请求 POST /v1/signal/{task}/{token}/{phase}?rc=&seq=&digest=。
func doSignal(t *testing.T, deps Deps, taskID, token, phase, query, body string) (respBody string, status int) {
	t.Helper()
	srv := NewServer(deps)

	target := "/v1/signal/" + taskID + "/" + token + "/" + phase
	if query != "" {
		target += "?" + query
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
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

// allFlowSignals 是十一个流程信号（不含 hook-report）。
var allFlowSignals = []model.SignalKind{
	model.SignalPreStart, model.SignalPreDone,
	model.SignalPackagesStart, model.SignalPackagesDone,
	model.SignalPostStart, model.SignalPostDone,
	model.SignalRebooting,
	model.SignalBooted, model.SignalInitStart, model.SignalInitDone, model.SignalReady,
}

// TestSignal_AllElevenFlowSignals_Accepted 锁住：十一个流程信号各自被
// 接受并回 200（不是计划文字描述的 204——见文件头注释）。
func TestSignal_AllElevenFlowSignals_Accepted(t *testing.T) {
	for i, kind := range allFlowSignals {
		t.Run(string(kind), func(t *testing.T) {
			fx := newSignalFixture(t)
			seq := (i + 1) * 1000 // 每个子测试用不重复的大序号，避免与信号表自身序号冲突导致误判去重
			body, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken, string(kind), "rc=0&seq="+strconv.Itoa(seq), "log tail")

			if status != http.StatusOK {
				t.Errorf("信号 %s 状态码 = %d, want %d", kind, status, http.StatusOK)
			}
			if body != "" {
				t.Errorf("信号 %s 响应体 = %q, want 空", kind, body)
			}
		})
	}
}

// TestSignal_UnknownPhase_Returns400 锁住：未知阶段名回 400。
func TestSignal_UnknownPhase_Returns400(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken, "not-a-real-phase", "rc=0&seq=5", "")

	if status != http.StatusBadRequest {
		t.Errorf("状态码 = %d, want %d —— 信号名来自 URL 路径段，不查表校验等于让机器"+
			"指定自己的阶段", status, http.StatusBadRequest)
	}
}

// TestSignal_DuplicateSeq_StillReturns200 锁住：重复序号仍回 200（不是
// 409 或 500）——机器侧的 signal() 会重试三次，序号重复是正常现象。
func TestSignal_DuplicateSeq_StillReturns200(t *testing.T) {
	fx := newSignalFixture(t)

	body1, status1 := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken, string(model.SignalPreStart), "rc=0&seq=1", "first")
	body2, status2 := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken, string(model.SignalPreStart), "rc=0&seq=1", "retry")

	if status1 != http.StatusOK {
		t.Fatalf("第一次请求状态码 = %d, want %d", status1, http.StatusOK)
	}
	if status2 != http.StatusOK {
		t.Errorf("重复序号请求状态码 = %d, want %d —— 机器侧重试三次是常态，不是错误路径",
			status2, http.StatusOK)
	}
	if body1 != "" || body2 != "" {
		t.Errorf("响应体应为空，got %q / %q", body1, body2)
	}
}

// TestSignal_FinalizeToken_InstallPhaseSignal_Returns403 锁住：finalize
// token 发装机期信号回 403（安全关键：finalize token 写在目标系统的磁盘
// 上、TTL 24 小时，拿到它就能伪造整个装机流程的信号）。
func TestSignal_FinalizeToken_InstallPhaseSignal_Returns403(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureFinalizeToken, string(model.SignalPreStart), "rc=0&seq=1", "")

	if status != http.StatusForbidden {
		t.Errorf("状态码 = %d, want %d —— finalize token 只能发 FinalizeCanSend 为真的信号，"+
			"pre-start 是装机期信号，超出授权范围", status, http.StatusForbidden)
	}
}

// TestSignal_FinalizeToken_AllowedSignal_Succeeds 锁住：finalize token 发
// 允许集合内的信号成功。
func TestSignal_FinalizeToken_AllowedSignal_Succeeds(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureFinalizeToken, string(model.SignalBooted), "rc=0&seq=1", "")

	if status != http.StatusOK {
		t.Errorf("状态码 = %d, want %d —— booted 在 model.FinalizeCanSend 的允许集合内",
			status, http.StatusOK)
	}
}

// TestSignal_HookReport_ValidDigest_PassedToStateMachine 锁住：合法 digest
// 被传给状态机；信号仍被应用且回 200。
func TestSignal_HookReport_ValidDigest_PassedToStateMachine(t *testing.T) {
	fx := newSignalFixture(t)
	validDigest := strings.Repeat("a", 64)

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken,
		string(model.SignalHookReport), "rc=0&seq=1&digest="+validDigest, "hook output")

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d", status, http.StatusOK)
	}

	events, err := fx.store.ListEvents(t.Context(), fx.task.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	found := false
	for _, e := range events {
		if e.HookDigest == validDigest {
			found = true
		}
	}
	if !found {
		t.Errorf("未找到 HookDigest = %q 的事件，events=%+v", validDigest, events)
	}
}

// TestSignal_HookReport_MalformedDigest_IgnoredButSignalStillApplied 锁住：
// 畸形 digest 被忽略但信号仍被应用且回 200——上报失败绝不中断装机，一个
// 畸形 digest 不该让机器装不完。
func TestSignal_HookReport_MalformedDigest_IgnoredButSignalStillApplied(t *testing.T) {
	fx := newSignalFixture(t)
	malformed := "NOT-VALID-HEX-" + strings.Repeat("z", 50)

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken,
		string(model.SignalHookReport), "rc=1&seq=1&digest="+malformed, "hook output")

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d —— 畸形 digest 不应拒绝整个信号", status, http.StatusOK)
	}

	events, err := fx.store.ListEvents(t.Context(), fx.task.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("未找到任何事件——信号应该仍被应用并记事件")
	}
	for _, e := range events {
		if e.HookDigest != "" {
			t.Errorf("事件的 HookDigest = %q, want 空——畸形 digest 应被忽略，视为未提供", e.HookDigest)
		}
	}
}

// TestSignal_NonHookReportSignal_DigestIgnored 锁住：非 hook-report 信号
// 带 digest 时 digest 被忽略。
func TestSignal_NonHookReportSignal_DigestIgnored(t *testing.T) {
	fx := newSignalFixture(t)
	someDigest := strings.Repeat("b", 64)

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken,
		string(model.SignalPreStart), "rc=0&seq=1&digest="+someDigest, "")

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d", status, http.StatusOK)
	}

	events, err := fx.store.ListEvents(t.Context(), fx.task.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	for _, e := range events {
		if e.HookDigest != "" {
			t.Errorf("非 hook-report 信号的事件 HookDigest = %q, want 空——digest 参数只在"+
				"hook-report 信号时读取，其余信号带了也忽略", e.HookDigest)
		}
	}
}

// TestSignal_LogBodyPassedToLogStore 锁住：日志正文被交给日志存储。
func TestSignal_LogBodyPassedToLogStore(t *testing.T) {
	fx := newSignalFixture(t)
	logStub := newLogStoreStub()
	fx.deps.LogStore = logStub

	doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken, string(model.SignalPreStart), "rc=0&seq=1", "这是日志正文")

	if len(logStub.appendLogCalls) != 1 {
		t.Fatalf("AppendLog 被调用 %d 次, want 1", len(logStub.appendLogCalls))
	}
	if logStub.appendLogCalls[0].content != "这是日志正文" {
		t.Errorf("AppendLog 传入的 content = %q, want %q", logStub.appendLogCalls[0].content, "这是日志正文")
	}
	if logStub.appendLogCalls[0].taskID != fx.task.ID {
		t.Errorf("AppendLog 传入的 taskID = %q, want %q", logStub.appendLogCalls[0].taskID, fx.task.ID)
	}
}

// TestSignal_InvalidToken_Returns403 补充覆盖：token 完全无效（两种用途都
// 查不到）时回 403。
func TestSignal_InvalidToken_Returns403(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doSignal(t, fx.deps, fx.task.ID, "totally-wrong-token", string(model.SignalPreStart), "rc=0&seq=1", "")

	if status != http.StatusForbidden {
		t.Errorf("状态码 = %d, want %d", status, http.StatusForbidden)
	}
}

// TestSignal_MissingRC_Returns400 补充覆盖：rc 缺失时回 400。
func TestSignal_MissingRC_Returns400(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken, string(model.SignalPreStart), "seq=1", "")

	if status != http.StatusBadRequest {
		t.Errorf("状态码 = %d, want %d —— rc 缺失应回 400", status, http.StatusBadRequest)
	}
}

// TestSignal_MissingSeq_Returns400 补充覆盖：seq 缺失时回 400。
func TestSignal_MissingSeq_Returns400(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken, string(model.SignalPreStart), "rc=0", "")

	if status != http.StatusBadRequest {
		t.Errorf("状态码 = %d, want %d —— seq 缺失应回 400", status, http.StatusBadRequest)
	}
}

// TestSignal_NonIntegerRC_Returns400 补充覆盖：rc 非整数时回 400。
func TestSignal_NonIntegerRC_Returns400(t *testing.T) {
	fx := newSignalFixture(t)

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken, string(model.SignalPreStart), "rc=abc&seq=1", "")

	if status != http.StatusBadRequest {
		t.Errorf("状态码 = %d, want %d —— rc 非整数应回 400", status, http.StatusBadRequest)
	}
}

// TestSignal_TaskAlreadyTerminal_Returns200WithoutStateChange 锁住契约
// 文档端点 4「错误行为」表最后一行：task 已处于终态时，200，仅记事件不做
// 任何状态变更（终态之后的信号视为迟到噪声，不是错误）。
func TestSignal_TaskAlreadyTerminal_Returns200WithoutStateChange(t *testing.T) {
	fx := newSignalFixture(t)
	ctx := t.Context()
	if _, err := fx.store.CASTaskState(ctx, fx.task.ID, model.TaskStateInstalling, fx.task.Version, model.TaskStateDone); err != nil {
		t.Fatalf("CASTaskState 失败: %v", err)
	}

	_, status := doSignal(t, fx.deps, fx.task.ID, signalFixtureInstallToken, string(model.SignalPreStart), "rc=0&seq=1", "")

	if status != http.StatusOK {
		t.Errorf("状态码 = %d, want %d —— 终态之后的信号视为迟到噪声，不是错误", status, http.StatusOK)
	}

	stored, err := fx.store.GetTask(ctx, fx.task.ID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if stored.State != model.TaskStateDone {
		t.Errorf("任务状态 = %q, want %q（不应被信号改变）", stored.State, model.TaskStateDone)
	}
}
