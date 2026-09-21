package main

import (
	"context"
	"errors"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// stubStateMachine 是 contract.StateMachine 的可编程测试替身，只用于
// 验证 stateMachineErrorFixup 的包装行为——不需要搭出真实的状态机。
type stubStateMachine struct {
	startBootResult model.Task
	startBootErr    error
	startBootCalled bool

	scanTimeoutsCalled bool
}

func (s *stubStateMachine) StartBoot(ctx context.Context, taskID string, arch model.Arch, fw model.FirmwareMode) (model.Task, error) {
	s.startBootCalled = true
	return s.startBootResult, s.startBootErr
}
func (s *stubStateMachine) MarkAnswerFetched(ctx context.Context, taskID string) (model.Task, error) {
	return model.Task{ID: taskID}, nil
}
func (s *stubStateMachine) ApplySignal(ctx context.Context, taskID string, signal model.SignalKind, seq int) (contract.SignalOutcome, error) {
	return contract.SignalOutcome{Accepted: true}, nil
}
func (s *stubStateMachine) IssueFinalizeToken(ctx context.Context, taskID string) (model.Task, error) {
	return model.Task{ID: taskID}, nil
}
func (s *stubStateMachine) Fail(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error) {
	return model.Task{ID: taskID, State: model.TaskStateFailed}, nil
}
func (s *stubStateMachine) Cancel(ctx context.Context, taskID string) (model.Task, error) {
	return model.Task{ID: taskID, State: model.TaskStateFailed}, nil
}
func (s *stubStateMachine) Retry(ctx context.Context, taskID string) (model.Task, error) {
	return model.Task{ID: taskID, State: model.TaskStatePending}, nil
}
func (s *stubStateMachine) ScanTimeouts(ctx context.Context, asOf int64) (int, error) {
	s.scanTimeoutsCalled = true
	return 0, nil
}

var _ contract.StateMachine = (*stubStateMachine)(nil)

// TestStateMachineErrorFixup_ConvertsFailedTaskToError 是本任务在实测
// e2e 负面用例（架构不符）时发现的真实契约裂缝的回归测试：
// internal/gateway/boot.go 的 handleBootGrant 依赖 StartBoot 返回非 nil
// error 来判断是否该回 sanboot，但 internal/statemachine 的实际实现对
// precheck 失败/attempt 超限的情形返回 (result, nil)——已经落库为
// failed 的任务快照，配一个 nil error。不修的后果是机器会收到一份完整
// 的成功菜单，即使它已经被 §4.6 硬拒绝，这是一个真实复现过的、会让
// 不该装的机器被装的阻塞级 bug。
func TestStateMachineErrorFixup_ConvertsFailedTaskToError(t *testing.T) {
	stub := &stubStateMachine{
		startBootResult: model.Task{ID: "t1", State: model.TaskStateFailed, FailureReason: model.FailureReasonPrecheck},
		startBootErr:    nil, // 复现真实实现的行为：失败但不返回 error。
	}
	fixup := stateMachineErrorFixup{inner: stub}

	result, err := fixup.StartBoot(context.Background(), "t1", model.ArchARM64, model.FirmwareUEFI)

	if !stub.startBootCalled {
		t.Fatal("底层 StartBoot 未被调用")
	}
	if err == nil {
		t.Fatal("StartBoot 返回的任务已是 failed 终态，但 fixup 未把 nil error 转换成非 nil——" +
			"这正是让 gateway 的 `if err != nil` 分支失效、机器收到成功菜单的那个 bug")
	}
	if result.State != model.TaskStateFailed {
		t.Errorf("result.State = %q, want failed（fixup 不应该丢弃或篡改原始任务快照）", result.State)
	}
}

// TestStateMachineErrorFixup_PassesThroughSuccessUnchanged 断言正常成功
// （非终态）的 StartBoot 调用不受任何影响——fixup 只应该在「已经是终态
// 失败」这一种情形下改写返回值，不能误伤正常的 booting 转换。
func TestStateMachineErrorFixup_PassesThroughSuccessUnchanged(t *testing.T) {
	stub := &stubStateMachine{
		startBootResult: model.Task{ID: "t2", State: model.TaskStateBooting},
		startBootErr:    nil,
	}
	fixup := stateMachineErrorFixup{inner: stub}

	result, err := fixup.StartBoot(context.Background(), "t2", model.ArchX86_64, model.FirmwareUEFI)

	if err != nil {
		t.Errorf("正常成功的 StartBoot 被 fixup 误转换出错误: %v", err)
	}
	if result.State != model.TaskStateBooting {
		t.Errorf("result.State = %q, want booting", result.State)
	}
}

// TestStateMachineErrorFixup_PassesThroughRealError 断言底层 StartBoot
// 本身就返回了真实错误（如数据库故障）时，fixup 原样透传，不做任何改写
// ——只应该处理「nil error + 终态失败」这一种特定的裂缝情形。
func TestStateMachineErrorFixup_PassesThroughRealError(t *testing.T) {
	wantErr := errors.New("boom: 模拟数据库故障")
	stub := &stubStateMachine{
		startBootResult: model.Task{},
		startBootErr:    wantErr,
	}
	fixup := stateMachineErrorFixup{inner: stub}

	_, err := fixup.StartBoot(context.Background(), "t3", model.ArchX86_64, model.FirmwareUEFI)

	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want 原样透传 %v", err, wantErr)
	}
}

// TestStateMachineErrorFixup_OtherMethodsPassThrough 断言其余七个方法
// 原样透传，不受本次修复影响——这条裂缝只存在于 StartBoot 这一个方法的
// 调用方假设上（见 stateMachineErrorFixup 的类型文档），fixup 不应该为
// 了修一个方法而意外改变另外七个方法的行为。
func TestStateMachineErrorFixup_OtherMethodsPassThrough(t *testing.T) {
	stub := &stubStateMachine{}
	fixup := stateMachineErrorFixup{inner: stub}
	ctx := context.Background()

	if _, err := fixup.MarkAnswerFetched(ctx, "t4"); err != nil {
		t.Errorf("MarkAnswerFetched 透传失败: %v", err)
	}
	if _, err := fixup.ApplySignal(ctx, "t4", model.SignalPreStart, 1); err != nil {
		t.Errorf("ApplySignal 透传失败: %v", err)
	}
	if _, err := fixup.IssueFinalizeToken(ctx, "t4"); err != nil {
		t.Errorf("IssueFinalizeToken 透传失败: %v", err)
	}
	if _, err := fixup.Fail(ctx, "t4", model.FailureReasonPrecheck, model.FailureDetail{}); err != nil {
		t.Errorf("Fail 透传失败: %v", err)
	}
	if _, err := fixup.Cancel(ctx, "t4"); err != nil {
		t.Errorf("Cancel 透传失败: %v", err)
	}
	if _, err := fixup.Retry(ctx, "t4"); err != nil {
		t.Errorf("Retry 透传失败: %v", err)
	}
	if _, err := fixup.ScanTimeouts(ctx, 0); err != nil {
		t.Errorf("ScanTimeouts 透传失败: %v", err)
	}
	if !stub.scanTimeoutsCalled {
		t.Error("ScanTimeouts 未被透传调用到底层实现")
	}
}
