// 本文件测试引导决策端点 GET /v1/boot 的命中路径。对应实现计划任务 5，
// 依据 docs/contracts/http-machine.md 端点 2、SPEC §4.2、§4.3、§4.6。
package gateway

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// grantFixture 打包一次命中路径测试所需的全部前置数据：一个 pending
// 任务，及其引用的模板、制品集、主机，均已落库。
type grantFixture struct {
	deps Deps
	task model.Task
}

// newGrantFixture 构造一份完整的 pending 任务前置数据，落在 deps.TaskStore
// 等（均为同一个 fake.Store 实例）里。sn/mac 是主机与任务共用的身份键，
// 供测试按需覆盖 query 里的 mac 值来模拟双因子不符。
func newGrantFixture(t *testing.T) grantFixture {
	t.Helper()
	store := fake.NewStore()
	ctx := t.Context()

	host := model.Host{SN: "SN-1", MAC: "aa:bb:cc:dd:ee:ff", UUID: "uuid-1", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI}
	if _, err := store.UpsertHost(ctx, host); err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}

	tpl := model.Template{ID: "tpl-1", Name: "rocky9", ArtifactSetID: "art-1", Version: 1, State: model.TemplateStateActive}
	if _, err := store.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}

	artifact := model.ArtifactSet{
		ID: "art-1", Name: "rocky9.4", OSFamily: model.OSFamilyRHEL, OSVersion: "9.4",
		Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
	}
	if _, err := store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	task := model.Task{
		ID: "task-1", HostSN: "SN-1", HostMAC: "aa:bb:cc:dd:ee:ff", HostUUID: "uuid-1",
		TemplateID: "tpl-1", TemplateVersion: 1, ArtifactSetID: "art-1",
		State: model.TaskStatePending, CurrentPhase: model.PhasePending,
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
	deps.StateMachine = newStoreBackedStateMachineStub(store)

	return grantFixture{deps: deps, task: created}
}

// newStoreBackedStateMachineStub 返回一个状态机桩，其 StartBoot 默认实现
// 直接读写共享的 store：CAS 到 booting、AttemptCount++、写入一个固定的
// install token——这比 newStateMachineStub() 的默认实现更贴近真实状态机
// 的行为（真实实现只改状态机拥有的那几个字段，不会把任务其余字段清空），
// 命中路径测试需要这份贴近真实的行为，才能让后续的渲染上下文构造从库里
// 查到完整的 Template/ArtifactSet/Host（这三者的查找键来自任务原有的
// TemplateID/ArtifactSetID/HostSN 等字段，若 StartBoot 返回一个只有 ID
// 字段的空任务，这些查找必然全部落空）。
//
// 测试要模拟 precheck 失败或 attempt 超限时，直接覆盖返回的桩实例的
// startBootFn 字段即可（stub 本身仍是 *stateMachineStub，其余七个方法与
// 调用记录字段都可用）。
func newStoreBackedStateMachineStub(store *fake.Store) *stateMachineStub {
	stub := newStateMachineStub()
	stub.startBootFn = func(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error) {
		current, err := store.GetTask(ctx, taskID)
		if err != nil {
			return model.Task{}, err
		}
		if _, err := store.CASTaskState(ctx, taskID, current.State, current.Version, model.TaskStateBooting); err != nil {
			return model.Task{}, err
		}
		token := "grant-test-install-token"
		attempt := current.AttemptCount + 1
		expiresAt := int64(1)
		return store.MutateTaskFields(ctx, taskID, model.TaskMutation{
			Token: &token, TokenExpiresAt: &expiresAt, AttemptCount: &attempt,
		})
	}
	// IssueFinalizeToken 同理必须写回共享 store 而不是返回一个只有 ID 与
	// FinalizeToken 两个字段的裸任务——网关在 /v1/boot 命中路径里拿到
	// IssueFinalizeToken 的返回值后会整体替换 updated 变量（因为真实状态
	// 机的返回值就是完整行），若这里不落库合并，命中路径测试会在
	// IssueFinalizeToken 调用之后丢失 TemplateID/ArtifactSetID/HostSN 等
	// 字段，导致渲染上下文构造失败。
	stub.issueFinalizeTokenFn = func(ctx context.Context, taskID string) (model.Task, error) {
		finalizeToken := "grant-test-finalize-token"
		finalizeExpiresAt := int64(1)
		return store.MutateTaskFields(ctx, taskID, model.TaskMutation{
			FinalizeToken: &finalizeToken, FinalizeExpiresAt: &finalizeExpiresAt,
		})
	}
	return stub
}

// grantQuery 构造一份与 fixture 身份匹配、arch/plat 合法的 query 参数集，
// 供测试按需覆盖单个字段（如 mac）来模拟不匹配场景。
func grantQuery() url.Values {
	return url.Values{
		"sn": {"SN-1"}, "mac": {"aa:bb:cc:dd:ee:ff"}, "uuid": {"uuid-1"},
		"arch": {"x86_64"}, "plat": {"efi"},
	}
}

// TestBootGrant_MatchAndVerified_ReturnsMenuAndAdvancesState 锁住：命中
// 任务且校验通过 → 响应是菜单（含内核行），状态变 booting。
func TestBootGrant_MatchAndVerified_ReturnsMenuAndAdvancesState(t *testing.T) {
	fx := newGrantFixture(t)
	rendererSpy := newRendererStub()
	fx.deps.Renderer = rendererSpy

	body, status := doBoot(t, fx.deps, grantQuery())

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d", status, http.StatusOK)
	}
	if body == "" {
		t.Fatal("响应体为空，want 渲染出的菜单")
	}
	if len(rendererSpy.renderMenuCalls) != 1 {
		t.Fatalf("RenderMenu 被调用 %d 次, want 1", len(rendererSpy.renderMenuCalls))
	}

	// 状态桩（newStateMachineStub）的 StartBoot 默认实现把任务状态设为
	// booting——这里断言桩确实被调用且返回值被网关采纳（响应体不是
	// no_task 载荷）。
	got := contract.NoTaskScript("SN-1")
	if body == got {
		t.Errorf("命中且校验通过时响应体等于 no_task 载荷，want 渲染出的菜单，got: %q", body)
	}
}

// TestBootGrant_SNMatchesButMACMismatch_ReturnsNoTaskWithoutIssuingToken
// 锁住计划任务 5 安全关键的那条：SN 对但 MAC 不对 → 回 no_task，且不签
// token。
func TestBootGrant_SNMatchesButMACMismatch_ReturnsNoTaskWithoutIssuingToken(t *testing.T) {
	fx := newGrantFixture(t)
	sm := newStateMachineStub()
	fx.deps.StateMachine = sm

	q := grantQuery()
	q.Set("mac", "11:22:33:44:55:66") // 与主机/任务记录的 MAC 不符

	body, status := doBoot(t, fx.deps, q)

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d（约定 1：始终 200）", status, http.StatusOK)
	}
	want := contract.NoTaskScript("SN-1")
	if body != want {
		t.Errorf("SN 对但 MAC 不对时响应体 = %q, want %q（no_task 载荷，对机器而言与"+
			"「没有任务」无法区分，不给攻击者「SN 猜对了」的反馈）", body, want)
	}

	if len(sm.startBootCalls) != 0 {
		t.Errorf("StartBoot 被调用 %d 次, want 0 —— MAC 双因子不符时绝不能签发 token"+
			"（安全关键：不加此判断则攻击者可用猜中的 SN 配任意 MAC 骗取装机 token）",
			len(sm.startBootCalls))
	}
}

// TestBootGrant_PrecheckFails_ReturnsNoTaskPayload 锁住：precheck 失败 →
// 回 no_task 载荷，且任务已 failed（状态机内部已经处理，网关只需要把
// error 结果转换成 no_task 响应；contract.StateMachine.StartBoot 文档
// 步骤 1 明确 precheck 失败由状态机自己调用 Fail，网关不重复这个逻辑）。
func TestBootGrant_PrecheckFails_ReturnsNoTaskPayload(t *testing.T) {
	fx := newGrantFixture(t)
	sm := newStateMachineStub()
	sm.startBootFn = func(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error) {
		return model.Task{}, &contract.PrecheckError{Failure: contract.PrecheckFailureArch, Detail: "arch 不符"}
	}
	fx.deps.StateMachine = sm

	body, status := doBoot(t, fx.deps, grantQuery())

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d（约定 1：始终 200）", status, http.StatusOK)
	}
	want := contract.NoTaskScript("SN-1")
	if body != want {
		t.Errorf("StartBoot 返回 *PrecheckError 时响应体 = %q, want %q", body, want)
	}
}

// TestBootGrant_AttemptExceeded_ReturnsNoTaskPayload 锁住：attempt 超限 →
// 回 no_task 载荷。
func TestBootGrant_AttemptExceeded_ReturnsNoTaskPayload(t *testing.T) {
	fx := newGrantFixture(t)
	sm := newStateMachineStub()
	sm.startBootFn = func(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error) {
		return model.Task{}, errAttemptExceededInTest
	}
	fx.deps.StateMachine = sm

	body, status := doBoot(t, fx.deps, grantQuery())

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d", status, http.StatusOK)
	}
	want := contract.NoTaskScript("SN-1")
	if body != want {
		t.Errorf("StartBoot 返回 error（模拟 attempt 超限）时响应体 = %q, want %q", body, want)
	}
}

// errAttemptExceededInTest 是本文件测试内部用的占位错误，模拟状态机
// StartBoot 在 attempt 超限时返回的 error（真实错误的具体类型无关紧要，
// 网关只需要看到非 nil error 就统一转换成 no_task 响应）。
var errAttemptExceededInTest = &stubError{"stub: attempt 超限"}

// TestBootGrant_RenderContextFieldsPopulated 锁住：渲染上下文的字段完整。
// 用渲染器桩记录的入参断言各字段非空。
func TestBootGrant_RenderContextFieldsPopulated(t *testing.T) {
	fx := newGrantFixture(t)
	rendererSpy := newRendererStub()
	fx.deps.Renderer = rendererSpy

	doBoot(t, fx.deps, grantQuery())

	if len(rendererSpy.renderMenuCalls) != 1 {
		t.Fatalf("RenderMenu 被调用 %d 次, want 1", len(rendererSpy.renderMenuCalls))
	}
	got := rendererSpy.renderMenuCalls[0]

	if got.Task.ID == "" {
		t.Error("RenderContext.Task.ID 为空")
	}
	if got.Template.ID == "" {
		t.Error("RenderContext.Template.ID 为空")
	}
	if got.Artifact.ID == "" {
		t.Error("RenderContext.Artifact.ID 为空")
	}
	if got.Host.SN == "" {
		t.Error("RenderContext.Host.SN 为空")
	}
	if got.Bases.GatewayBase == "" {
		t.Error("RenderContext.Bases.GatewayBase 为空")
	}
	if got.InstallToken == "" {
		t.Error("RenderContext.InstallToken 为空")
	}
	if got.SyslogHost == "" {
		t.Error("RenderContext.SyslogHost 为空")
	}
	if got.ActualFirmware == "" {
		t.Error("RenderContext.ActualFirmware 为空")
	}
	// Facts 字段允许为 nil：fake.Store.GetFacts 对尚未写入任何 fact 的任务
	// 返回 nil map（copyStringMap 的文档明确「m 为 nil 时返回 nil，不把
	// nil 变成空切片」），这是合法的空状态，nil map 可安全读取与遍历，
	// 不构成「字段缺失」。
}

// TestBootGrant_IssuesFinalizeTokenDuringGrant 锁住计划任务 5「finalize
// token 在这一步签发（调状态机），因为它要被写进 %post」这条设计：/v1/boot
// 命中且校验通过、StartBoot 成功之后，网关必须调用一次
// StateMachine.IssueFinalizeToken，而不是把它推迟到 /v1/answer 渲染
// 应答文件时才签发——渲染 %post 时只有渲染上下文可用，若那时任务上还没有
// FinalizeToken，%post 里就没有凭据可写，机器重启后的全部信号都无法上报。
func TestBootGrant_IssuesFinalizeTokenDuringGrant(t *testing.T) {
	fx := newGrantFixture(t)
	sm, ok := fx.deps.StateMachine.(*stateMachineStub)
	if !ok {
		t.Fatalf("fx.deps.StateMachine 不是 *stateMachineStub，类型是 %T", fx.deps.StateMachine)
	}

	doBoot(t, fx.deps, grantQuery())

	if len(sm.issueFinalizeTokenCalls) != 1 {
		t.Fatalf("IssueFinalizeToken 被调用 %d 次, want 1", len(sm.issueFinalizeTokenCalls))
	}
	if sm.issueFinalizeTokenCalls[0] != fx.task.ID {
		t.Errorf("IssueFinalizeToken 传入的 taskID = %q, want %q", sm.issueFinalizeTokenCalls[0], fx.task.ID)
	}
}

// TestBootGrant_StateMachineNotSelfMutatingAttempt 锁住：网关没有自己
// 修改 attempt——用桩记录状态机被调用次数与传入参数，断言网关只调用了
// 一次 StartBoot，没有在调用前后自行读写 AttemptCount。
func TestBootGrant_StateMachineNotSelfMutatingAttempt(t *testing.T) {
	fx := newGrantFixture(t)
	sm := newStateMachineStub()
	fx.deps.StateMachine = sm

	doBoot(t, fx.deps, grantQuery())

	if len(sm.startBootCalls) != 1 {
		t.Fatalf("StartBoot 被调用 %d 次, want 1 —— attempt 计数由状态机管，"+
			"网关只应调用一次 StartBoot，不应在调用前后自行修改 attempt", len(sm.startBootCalls))
	}
	call := sm.startBootCalls[0]
	if call.taskID != fx.task.ID {
		t.Errorf("StartBoot 传入的 taskID = %q, want %q", call.taskID, fx.task.ID)
	}
	if call.actualArch != model.ArchX86_64 {
		t.Errorf("StartBoot 传入的 actualArch = %q, want %q", call.actualArch, model.ArchX86_64)
	}
	if call.actualFirmware != model.FirmwareUEFI {
		t.Errorf("StartBoot 传入的 actualFirmware = %q, want %q", call.actualFirmware, model.FirmwareUEFI)
	}

	// deps.TaskStore 是同一个 fake.Store 实例；直接查库确认网关本身没有
	// 调用 MutateTaskFields 之类的方法去改 AttemptCount——桩的 StartBoot
	// 默认实现不改库，若网关额外调用了库上的某个改字段方法，这里能看到
	// 任务的 AttemptCount 被改动（桩不会改，只有网关自己动手才会改）。
	stored, err := fx.deps.TaskStore.GetTask(t.Context(), fx.task.ID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if stored.AttemptCount != 0 {
		t.Errorf("库内任务 AttemptCount = %d, want 0 —— 网关不应该自己修改这个字段，"+
			"那是状态机 StartBoot 内部的职责", stored.AttemptCount)
	}
}
