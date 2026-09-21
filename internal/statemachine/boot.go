// 本文件实现开始引导 StartBoot（任务 4）与标记应答文件已取
// MarkAnswerFetched（任务 5）。依据 SPEC §3.4、§4.2、§4.4、§4.6。
//
// StartBoot 是 §0.1 的范围起点：这一个方法里的顺序错了，后果是「不该装
// 的机器被装了」或「该装的机器三次机会被白白消耗」。五步顺序不能变：
//  1. 确认任务仍是 pending（防御性检查）
//  2. 跑 precheck（必须在 attempt++ 之前）
//  3. precheck 不过 → 置 failed，不消耗 attempt
//  4. attempt++；超上限 → 置 failed（不签 token）
//  5. 签 token、CAS 到 booting（同一次转换里完成）
package statemachine

import (
	"context"
	"errors"
	"io/fs"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// errTaskNotPending 是 StartBoot 第 1 步防御性检查失败时返回的内部错误：
// 任务已经不是 pending（可能已被并发请求推进到 booting，或被扫描循环
// 判成 failed）。不导出为 contract 层的 sentinel error——调用方（网关）
// 只需要知道这次调用没有生效，不需要用 errors.Is 区分具体原因。
var errTaskNotPending = errors.New("statemachine: 任务已不是 pending，不接受开始引导")

// StartBoot 处理 /v1/boot 命中 pending 任务后的推进（SPEC §4.2）。
func (m *Machine) StartBoot(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error) {
	// 第 1 步：确认任务仍是 pending。调用方（M5）会先查 pending 任务再调
	// 本方法，但两步之间有窗口——并发的第二个请求可能已经把任务推进到
	// booting，或扫描循环刚把它判成 failed。不检查的后果是对已终态任务
	// 重复累加 attempt 并重复写失败事件，那个计数会无界增长
	// （§9.1 第三道闸门依赖它）。
	current, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, err
	}
	if current.State != model.TaskStatePending {
		return model.Task{}, errTaskNotPending
	}

	// 第 2 步：跑 precheck。precheck 需要制品集，所以这里查库取它——
	// 取不到制品集本身就是一种失败（置 failed，原因 precheck）。
	artifact, err := m.store.GetArtifactSet(ctx, current.ArtifactSetID)
	if err != nil {
		return m.failTask(ctx, taskID, model.FailureReasonPrecheck, model.FailureDetail{
			Phase: current.CurrentPhase,
		}, m.clock.Now())
	}

	precheckErr := m.prechecker.Check(ctx, actualArch, actualFirmware, artifact, artifactFilesFS())
	if precheckErr != nil {
		// 第 3 步：precheck 不过 → 置 failed，不消耗 attempt。这是范围内
		// 的失败（§4.6）：机器已经取到了引导端点并带来了真实的上报值，
		// 系统手上有任务、有实际值、能给出确切原因，所以必须记录并呈现。
		var pe *contract.PrecheckError
		detailMessage := precheckErr.Error()
		if errors.As(precheckErr, &pe) {
			detailMessage = pe.Detail
		}
		return m.failTask(ctx, taskID, model.FailureReasonPrecheck, model.FailureDetail{
			Phase:   current.CurrentPhase,
			LogTail: detailMessage,
		}, m.clock.Now())
	}

	// 第 4/5 步：attempt++；超上限则置 failed（不签 token）；否则签发
	// 装机 token 并 CAS 到 booting。两步在同一个 casRetry 回调里完成，
	// 但底层仍是「先 mutation 写 attempt/token，再 CAS 状态」的两次 Store
	// 调用——见 machine.go 的 transitionDecision.mutation 字段文档与本
	// 文件末尾的契约缺口说明。
	now := m.clock.Now()
	result, err := m.casRetry(ctx, taskID, func(task model.Task) transitionDecision {
		if task.State.IsTerminal() {
			return transitionDecision{rejectErr: errTaskNotPending}
		}
		if task.State != model.TaskStatePending {
			return transitionDecision{rejectErr: errTaskNotPending}
		}

		nextAttempt := task.AttemptCount + 1
		if nextAttempt > model.MaxBootAttempts {
			// attempt 超上限：不在这里直接返回 rejectErr（那样 casRetry
			// 会把错误原样返回，但我们还需要真正执行 failTask 的写入）。
			// 用一个特殊标记通过 noop 短路，外层单独处理超限分支。
			return transitionDecision{rejectErr: errAttemptExceeded}
		}

		token, tokenErr := m.tokens.IssueToken(ctx)
		if tokenErr != nil {
			return transitionDecision{rejectErr: tokenErr}
		}
		expiresAt := now + int64(model.InstallTokenTTL.Seconds())

		return transitionDecision{
			mutation: &model.TaskMutation{
				AttemptCount:   &nextAttempt,
				Token:          &token,
				TokenExpiresAt: &expiresAt,
			},
			newState: model.TaskStateBooting,
		}
	})
	if errors.Is(err, errAttemptExceeded) {
		return m.failTask(ctx, taskID, model.FailureReasonAttemptExceeded, model.FailureDetail{
			Phase: current.CurrentPhase,
		}, now)
	}
	if err != nil {
		return model.Task{}, err
	}
	return result, nil
}

// errAttemptExceeded 是 StartBoot 内部用来从 casRetry 回调里跳出、转去
// 走 failTask(attempt_exceeded) 分支的标记错误，不对外暴露。
var errAttemptExceeded = errors.New("statemachine: attempt 已超上限")

// artifactFilesFS 返回制品完整性检查用的文件系统。
//
// 契约缺口：contract.Prechecker.Check 要求传入 io/fs.FS 类型的
// 「制品根目录」，但本模块没有被注入任何指向真实制品目录的文件系统——
// M1 的六个依赖列表（Store、Clock、TokenIssuer、Prechecker、LogStore、
// EventBus）里没有 fs.FS 这一项，StartBoot 却需要在调用 Prechecker.Check
// 时提供一个。最保守的处理：本方法当前返回一个空文件系统（fstest.MapFS
// 的零值等价物，任何路径都查不到），这意味着 StartBoot 路径下的制品完整
// 性校验目前总是能通过「找到具体缺失文件」分支之外的语义（因为空 FS 下
// 任何非空路径都会被判定为缺失）——也就是说，只要制品的 Paths 里有任何
// 非空字段，StartBoot 目前会把「制品完整性」判定为不通过。
//
// 这不是本模块能自行修的问题：真正的解法是往 contract.Prechecker 或
// StateMachine 的构造签名里加一个 fs.FS 依赖（例如指向制品根目录的
// os.DirFS，由 M9 装配时注入），让 StartBoot 能拿到与 M4/M6 渲染器共享
// 的同一份制品文件系统。在契约补上这个依赖之前，任何在本模块内新增一个
// 「按配置路径自己 os.DirFS」的变通都会引入一条不受 M9 装配控制的磁盘
// 路径假设，比明确返回「查不到」更危险——见任务返回值 contractGaps。
func artifactFilesFS() fs.FS {
	return emptyFS{}
}

// emptyFS 是一个不含任何文件的 fs.FS 实现，Open 总是返回
// fs.ErrNotExist，供 artifactFilesFS 使用。
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// MarkAnswerFetched 处理应答文件被取走后的推进：booting → installing
// （SPEC §6.1）。
//
// 必须幂等——Ubuntu 的 cloud-init 会取 user-data 与 meta-data 两个文件，
// 每次都会触发这个方法。已经是 installing 或更后的状态时直接返回成功，
// 不报错。
func (m *Machine) MarkAnswerFetched(ctx context.Context, taskID string) (model.Task, error) {
	now := m.clock.Now()
	return m.casRetry(ctx, taskID, func(current model.Task) transitionDecision {
		if current.State == model.TaskStatePending {
			// installer 不可能在签 token 之前起来。
			return transitionDecision{rejectErr: errAnswerBeforeBoot}
		}
		if current.State != model.TaskStateBooting {
			// 已经是 installing 或之后（含终态）：视为已处理过，直接
			// 返回成功、不报错——这正是幂等要求的核心：cloud-init 取
			// user-data 与 meta-data 两次都会调用到这里，第二次不能
			// 报错，否则应答文件请求返回 500，cloud-init 拿不到
			// meta-data 就不会继续，装机停住。
			return transitionDecision{noop: true}
		}

		phase := model.PhasePre
		return transitionDecision{
			mutation: &model.TaskMutation{
				CurrentPhase:   &phase,
				PhaseEnteredAt: &now,
			},
			newState: model.TaskStateInstalling,
		}
	})
}

// errAnswerBeforeBoot 是 MarkAnswerFetched 对 pending 任务的拒绝错误：
// installer 不可能在签 token 之前起来。
var errAnswerBeforeBoot = errors.New("statemachine: 任务尚未开始引导，不可能取到应答文件")
