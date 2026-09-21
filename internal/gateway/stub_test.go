// 本文件提供三个测试桩：状态机（contract.StateMachine，八个方法）、渲染器
// （contract.Renderer，两个方法）、日志存储（contract.LogStore，两个方法）。
// 对应实现计划任务 1。
//
// 网关不 import M3（statemachine）与 M4（render）的实现包，只依赖
// internal/core/contract 的接口，因此测试必须自己实现这三个接口的桩，
// 不能借用别的模块的测试夹具（计划「文件结构」表下方的说明）。
//
// 桩的形态：每个方法背后一个函数字段。构造函数（newXxxStub）给全部字段填
// 「正常返回」的默认实现，测试里只覆盖关心的那一个字段——这比「为每种场景
// 写一个桩类型」省得多，也比「桩里塞一堆 if 开关」清楚（计划任务 1）。
package gateway

import (
	"context"
	"io/fs"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// stateMachineStub：contract.StateMachine 的测试桩
// ---------------------------------------------------------------------------

// stateMachineStub 是 contract.StateMachine 的测试替身。每个方法背后是一个
// 函数字段，默认实现在 newStateMachineStub 里给出；测试只需要覆盖关心的
// 那一个字段。
type stateMachineStub struct {
	startBootFn          func(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error)
	markAnswerFetchedFn  func(ctx context.Context, taskID string) (model.Task, error)
	applySignalFn        func(ctx context.Context, taskID string, signal model.SignalKind, seq int) (contract.SignalOutcome, error)
	issueFinalizeTokenFn func(ctx context.Context, taskID string) (model.Task, error)
	failFn               func(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error)
	cancelFn             func(ctx context.Context, taskID string) (model.Task, error)
	retryFn              func(ctx context.Context, taskID string) (model.Task, error)
	scanTimeoutsFn       func(ctx context.Context, asOf int64) (int, error)

	// startBootCalls 记录 StartBoot 每次被调用时的入参，供测试断言「网关
	// 没有自己修改 attempt」——网关只应调用一次 StartBoot，不应在调用前后
	// 自行读写 AttemptCount（计划任务 5 测试要点）。
	startBootCalls []stateMachineStartBootCall

	// issueFinalizeTokenCalls 记录 IssueFinalizeToken 每次被调用时的
	// taskID，供测试断言「finalize token 在引导命中路径这一步签发」
	// （计划任务 5：因为它要被写进 %post，必须在 /v1/boot 成功渲染菜单
	// 之前就已存在，供 /v1/answer 渲染时可用）。
	issueFinalizeTokenCalls []string
}

// stateMachineStartBootCall 记录一次 StartBoot 调用的入参快照。
type stateMachineStartBootCall struct {
	taskID         string
	actualArch     model.Arch
	actualFirmware model.FirmwareMode
}

// 编译期断言：*stateMachineStub 必须满足 contract.StateMachine。
var _ contract.StateMachine = (*stateMachineStub)(nil)

// newStateMachineStub 返回一个全部方法均为「正常返回」的状态机桩。
//
// 「正常返回」的具体形态：StartBoot 把任务推进到 booting 并签发一个固定
// token；MarkAnswerFetched 把任务推进到 installing；ApplySignal 返回
// Accepted+StateChanged 且 Task 取查表得到的状态；其余方法原样返回一个
// 携带 taskID 的空任务，不模拟任何错误——测试按需覆盖单个字段。
func newStateMachineStub() *stateMachineStub {
	return &stateMachineStub{
		startBootFn: func(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error) {
			return model.Task{
				ID:    taskID,
				State: model.TaskStateBooting,
				Token: "stub-install-token",
			}, nil
		},
		markAnswerFetchedFn: func(ctx context.Context, taskID string) (model.Task, error) {
			return model.Task{ID: taskID, State: model.TaskStateInstalling}, nil
		},
		applySignalFn: func(ctx context.Context, taskID string, signal model.SignalKind, seq int) (contract.SignalOutcome, error) {
			entry, ok := model.LookupSignal(signal)
			if !ok {
				return contract.SignalOutcome{}, errUnknownSignalInStub
			}
			return contract.SignalOutcome{
				Accepted:     true,
				StateChanged: true,
				Task:         model.Task{ID: taskID, State: entry.State, CurrentPhase: entry.Phase, LastSeq: entry.Seq},
			}, nil
		},
		issueFinalizeTokenFn: func(ctx context.Context, taskID string) (model.Task, error) {
			return model.Task{ID: taskID, FinalizeToken: "stub-finalize-token"}, nil
		},
		failFn: func(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error) {
			return model.Task{ID: taskID, State: model.TaskStateFailed, FailureReason: reason}, nil
		},
		cancelFn: func(ctx context.Context, taskID string) (model.Task, error) {
			return model.Task{ID: taskID, State: model.TaskStateFailed, FailureReason: model.FailureReasonCanceled}, nil
		},
		retryFn: func(ctx context.Context, taskID string) (model.Task, error) {
			return model.Task{ID: taskID, State: model.TaskStatePending}, nil
		},
		scanTimeoutsFn: func(ctx context.Context, asOf int64) (int, error) {
			return 0, nil
		},
	}
}

// errUnknownSignalInStub 是桩内部用的错误，模拟 contract.StateMachine 文档
// 里「signal 若不在 model.LookupSignal 的表中，ApplySignal 返回 error」这条
// 行为。真实实现的具体错误类型无关紧要，桩只需要返回非 nil error。
var errUnknownSignalInStub = &stubError{"stub: 未知信号"}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

func (s *stateMachineStub) StartBoot(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error) {
	s.startBootCalls = append(s.startBootCalls, stateMachineStartBootCall{
		taskID: taskID, actualArch: actualArch, actualFirmware: actualFirmware,
	})
	return s.startBootFn(ctx, taskID, actualArch, actualFirmware)
}

func (s *stateMachineStub) MarkAnswerFetched(ctx context.Context, taskID string) (model.Task, error) {
	return s.markAnswerFetchedFn(ctx, taskID)
}

func (s *stateMachineStub) ApplySignal(ctx context.Context, taskID string, signal model.SignalKind, seq int) (contract.SignalOutcome, error) {
	return s.applySignalFn(ctx, taskID, signal, seq)
}

func (s *stateMachineStub) IssueFinalizeToken(ctx context.Context, taskID string) (model.Task, error) {
	s.issueFinalizeTokenCalls = append(s.issueFinalizeTokenCalls, taskID)
	return s.issueFinalizeTokenFn(ctx, taskID)
}

func (s *stateMachineStub) Fail(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error) {
	return s.failFn(ctx, taskID, reason, detail)
}

func (s *stateMachineStub) Cancel(ctx context.Context, taskID string) (model.Task, error) {
	return s.cancelFn(ctx, taskID)
}

func (s *stateMachineStub) Retry(ctx context.Context, taskID string) (model.Task, error) {
	return s.retryFn(ctx, taskID)
}

func (s *stateMachineStub) ScanTimeouts(ctx context.Context, asOf int64) (int, error) {
	return s.scanTimeoutsFn(ctx, asOf)
}

// ---------------------------------------------------------------------------
// rendererStub：contract.Renderer 的测试桩
// ---------------------------------------------------------------------------

// rendererStub 是 contract.Renderer 的测试替身。
type rendererStub struct {
	renderMenuFn       func(ctx model.RenderContext) (string, error)
	renderAnswerFileFn func(ctx model.RenderContext, fileName string) (string, error)

	// renderMenuCalls 与 renderAnswerFileCalls 记录每次调用传入的
	// RenderContext，供测试断言「渲染上下文的字段完整」（计划任务 5
	// 测试要点）。
	renderMenuCalls       []model.RenderContext
	renderAnswerFileCalls []rendererAnswerFileCall
}

// rendererAnswerFileCall 记录一次 RenderAnswerFile 调用的入参快照。
type rendererAnswerFileCall struct {
	ctx      model.RenderContext
	fileName string
}

// 编译期断言：*rendererStub 必须满足 contract.Renderer。
var _ contract.Renderer = (*rendererStub)(nil)

// newRendererStub 返回一个全部方法均为「正常返回」的渲染器桩：RenderMenu
// 返回一段固定的占位菜单文本（含 "kernel" 字样，供断言「响应是菜单」用），
// RenderAnswerFile 返回一段固定的占位应答文件正文。
func newRendererStub() *rendererStub {
	return &rendererStub{
		renderMenuFn: func(ctx model.RenderContext) (string, error) {
			return "#!ipxe\nkernel " + ctx.Bases.ArtifactURL("vmlinuz") + "\nboot\n", nil
		},
		renderAnswerFileFn: func(ctx model.RenderContext, fileName string) (string, error) {
			return "# stub answer file for " + fileName + "\n", nil
		},
	}
}

func (r *rendererStub) RenderMenu(ctx model.RenderContext) (string, error) {
	r.renderMenuCalls = append(r.renderMenuCalls, ctx)
	return r.renderMenuFn(ctx)
}

func (r *rendererStub) RenderAnswerFile(ctx model.RenderContext, fileName string) (string, error) {
	r.renderAnswerFileCalls = append(r.renderAnswerFileCalls, rendererAnswerFileCall{ctx: ctx, fileName: fileName})
	return r.renderAnswerFileFn(ctx, fileName)
}

// ---------------------------------------------------------------------------
// logStoreStub：contract.LogStore 的测试桩
// ---------------------------------------------------------------------------

// logStoreStub 是 contract.LogStore 的测试替身。
type logStoreStub struct {
	appendLogFn func(ctx context.Context, taskID string, phase model.Phase, content string) error
	tailLogFn   func(ctx context.Context, taskID string, phase model.Phase, maxBytes int) (string, error)

	// appendLogCalls 记录每次 AppendLog 调用的入参，供测试断言「日志正文
	// 被交给日志存储」（计划任务 7 测试要点）。
	appendLogCalls []logStoreAppendCall
}

// logStoreAppendCall 记录一次 AppendLog 调用的入参快照。
type logStoreAppendCall struct {
	taskID  string
	phase   model.Phase
	content string
}

// 编译期断言：*logStoreStub 必须满足 contract.LogStore。
var _ contract.LogStore = (*logStoreStub)(nil)

// newLogStoreStub 返回一个全部方法均为「正常返回」的日志存储桩。
func newLogStoreStub() *logStoreStub {
	return &logStoreStub{
		appendLogFn: func(ctx context.Context, taskID string, phase model.Phase, content string) error {
			return nil
		},
		tailLogFn: func(ctx context.Context, taskID string, phase model.Phase, maxBytes int) (string, error) {
			return "", nil
		},
	}
}

func (l *logStoreStub) AppendLog(ctx context.Context, taskID string, phase model.Phase, content string) error {
	l.appendLogCalls = append(l.appendLogCalls, logStoreAppendCall{taskID: taskID, phase: phase, content: content})
	return l.appendLogFn(ctx, taskID, phase, content)
}

func (l *logStoreStub) TailLog(ctx context.Context, taskID string, phase model.Phase, maxBytes int) (string, error) {
	return l.tailLogFn(ctx, taskID, phase, maxBytes)
}

// ---------------------------------------------------------------------------
// precheckerStub：contract.Prechecker 的测试桩
// ---------------------------------------------------------------------------

// precheckerStub 是 contract.Prechecker 的测试替身，供任务 5 的引导命中
// 路径测试构造完整依赖时使用（网关本身不直接调用 Prechecker——那是状态机
// StartBoot 内部的职责，见 contract.StateMachine.StartBoot 文档；这里提供
// 桩只是为了在测试里能构造出「precheck 失败」场景，驱动状态机桩间接体现
// 这一行为，而不是网关代码本身依赖这个接口）。
type precheckerStub struct {
	checkFn func(ctx context.Context, actualArch model.Arch, actualFirmware model.FirmwareMode, artifact model.ArtifactSet, files fs.FS) error
}

var _ contract.Prechecker = (*precheckerStub)(nil)

func newPrecheckerStub() *precheckerStub {
	return &precheckerStub{
		checkFn: func(ctx context.Context, actualArch model.Arch, actualFirmware model.FirmwareMode, artifact model.ArtifactSet, files fs.FS) error {
			return nil
		},
	}
}

func (p *precheckerStub) Check(ctx context.Context, actualArch model.Arch, actualFirmware model.FirmwareMode, artifact model.ArtifactSet, files fs.FS) error {
	return p.checkFn(ctx, actualArch, actualFirmware, artifact, files)
}
