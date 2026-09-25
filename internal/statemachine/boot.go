// 本文件实现开始引导 StartBoot（任务 4）与标记应答文件已取
// MarkAnswerFetched（任务 5）。依据 SPEC §3.4、§4.2、§4.4、§4.6、§10。
//
// StartBoot 是 §0.1 的范围起点：这一个方法里的顺序错了，后果是「不该装
// 的机器被装了」或「该装的机器三次机会被白白消耗」。步骤顺序不能变：
//  1. 确认任务仍是 pending（防御性检查）
//  2. 跑 precheck（必须在 attempt++ 之前）
//  3. precheck 不过 → 置 failed，不消耗 attempt
//  4. 并发准入检查（Phase 4 新增，SPEC §10 失败模式 11）：precheck 已经
//     通过之后、attempt++ 之前，确认当前在装机器数没有打满配置的并发
//     上限——超限时排队（不消耗 attempt、不置 failed），见本文件下方
//     准入检查段落的详细注释
//  5. attempt++；超上限 → 置 failed（不签 token）
//  6. 签 token、CAS 到 booting（同一次转换里完成）
package statemachine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// errTaskNotPending 是 StartBoot 第 1 步防御性检查失败时返回的内部错误：
// 任务已经不是 pending（可能已被并发请求推进到 booting，或被扫描循环
// 判成 failed）。不导出为 contract 层的 sentinel error——调用方（网关）
// 只需要知道这次调用没有生效，不需要用 errors.Is 区分具体原因。
var errTaskNotPending = errors.New("statemachine: 任务已不是 pending，不接受开始引导")

// errAdmissionQueued 是 StartBoot 因并发准入上限被打满而拒绝本次引导时
// 包装进返回错误里的标记（用 %w 包装，调用方可用 errors.Is 精确识别）。
//
// 导出判定意义上不导出为 contract 层 sentinel：目前唯一的调用方（网关的
// handleBootGrant）对 StartBoot 返回的任何 error 已经统一回退到 no_task
// 载荷，不需要区分「排队」与「precheck 失败」——两者对网关这一层的处理
// 结果本来就相同（都是回 no_task，机器都会继续 PXE 轮询）。这里仍然定义
// 一个可被 errors.Is 识别的标记，是为了让本模块自己的测试能精确断言
// 「StartBoot 因为准入被拒绝，而不是因为别的原因返回了错误」，以及为
// 未来若网关想要区分这两种情形（例如排队场景要在 no_task 的 echo 行外
// 追加提示）留一个不需要改错误处理骨架就能识别的钩子。
var errAdmissionQueued = errors.New("statemachine: 并发装机已达上限，本次引导请求排队")

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
	if !acceptsBootRequest(current.State) {
		return model.Task{}, errTaskNotPending
	}
	// booting 状态的任务接受重试，但要先把「同一次引导的并发重复请求」与
	// 「机器真的回来重试」区分开——判据是距上次进入该阶段的时间间隔，
	// 见 minBootRetryInterval 的文档。
	if current.State == model.TaskStateBooting &&
		m.clock.Now()-current.PhaseEnteredAt < int64(minBootRetryInterval.Seconds()) {
		return model.Task{}, errBootRetryTooSoon
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

	// 第 3.5 步（并发准入检查，Phase 4 新增）：precheck 已经通过，
	// 说明这台机器身份合法且架构/固件与制品匹配——但在真正消耗 attempt、
	// 签发装机 token 之前，还要确认当前「同时在装的机器数」没有打满配置的
	// 上限（SPEC §10 失败模式 11：并发装机打满 HTTP，实际是带宽争抢，
	// 表现成一批机器一起阶段超时失败）。
	//
	// 放在 precheck 之后、attempt++ 之前：precheck 失败是「重试必然同样
	// 失败」，必须先于准入检查判断（不能让一台架构不匹配的机器仅仅因为
	// 「凑巧赶上有空位」就白白消耗一次装机 token）；attempt++ 在准入检查
	// 之后，因为超限本身不是这台机器的过错，不能消耗它的重试机会。
	//
	// 拒绝时的处理：返回一个不经过 failTask 的普通 error——任务原样留在
	// pending（不改状态、不改 attempt、不产生任何写操作，AdmissionController.
	// Check 本身是纯读操作）。网关侧（internal/gateway/boot.go 的
	// handleBootGrant）对 StartBoot 返回 error 的既有处理是统一回退到
	// no_task 载荷（sanboot），这正是「让机器继续 PXE 轮询、下一轮再来」
	// 这个诉求在不改动 gateway 任何代码的前提下最保守的实现方式——见本次
	// 任务返回值 notes 对这个选择的完整说明（为什么不新签一个 sleep+chain
	// 的 iPXE 脚本）。
	if decision, err := newAdmissionControllerFromGlobalLimit(m.store).Check(ctx); err != nil {
		return model.Task{}, err
	} else if !decision.Admitted {
		return model.Task{}, fmt.Errorf("statemachine: 并发装机已达上限（当前在装 %d 台，上限 %d 台），"+
			"本次引导请求排队，不消耗 attempt、不置 failed: %w", decision.InFlight, decision.Limit, errAdmissionQueued)
	}

	// 第 4/5 步：attempt++；超上限则置 failed（不签 token）；否则签发
	// 装机 token 并 CAS 到 booting。两步在同一个 casRetry 回调里完成，
	// 但底层仍是「先 mutation 写 attempt/token，再 CAS 状态」的两次 Store
	// 调用——见 machine.go 的 transitionDecision.mutation 字段文档与本
	// 文件末尾的契约缺口说明。
	now := m.clock.Now()
	result, err := m.casRetry(ctx, taskID, func(task model.Task) transitionDecision {
		if !acceptsBootRequest(task.State) {
			return transitionDecision{rejectErr: errTaskNotPending}
		}
		// 与上面第 1 步同样的去重判据，在 CAS 回调里再判一次：两步之间
		// 有窗口，并发的第二个请求可能刚把任务推进到 booting。少了这一道，
		// N 个并发请求会各签一个 token、attempt 加 N。
		if task.State == model.TaskStateBooting &&
			now-task.PhaseEnteredAt < int64(minBootRetryInterval.Seconds()) {
			return transitionDecision{rejectErr: errBootRetryTooSoon}
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
				// 每次签发 token 都刷 PhaseEnteredAt：它既是 §3.3 阶段超时的
				// 计时起点（每次重试都该重新计时，否则第二次引导一开始就
				// 带着上一次已经耗掉的时间），也是 minBootRetryInterval
				// 去重判据的基准。此前只在建任务时写过一次、之后永不更新，
				// 于是 booting 阶段的 15 分钟预算从第一次引导起算，重试
				// 继承了前一次的剩余时间。
				PhaseEnteredAt: &now,
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

// acceptsBootRequest 判定某个状态的任务是否接受 /v1/boot 引导请求。
//
// pending 与 booting 都接受，其余一律拒绝。
//
// **为什么 booting 也要接受。** SPEC 对引导重试有三处明文要求：
//
//	§3.3 阶段预算表   | booting | 15 min | 允许重试，attempt++ |
//	§9.1 第三道闸门   同一任务窗口内菜单最多取 3 次，超过即 failed
//	§4.4             「一次性失效会直接掐死正常重试」
//
// 只接受 pending 的后果是 MaxBootAttempts = 3 这个配额**一次都用不上**：第一次
// 引导把任务推到 booting，机器再来就查不到任务、拿到 no_task。而 PXE 引导重试
// 是常态而非异常——取 kernel 时网络抖动、initrd 校验失败、机器在 BIOS 里被人
// 手动重启、iPXE 自己的 retry，任何一种都会让机器回来再问一次。
//
// 实测（2026-09-25，QEMU BIOS 目标机）：第一次引导成功取到菜单、任务进
// booting；机器重试时 iPXE 屏显
//
//	http://gw.vmlab.internal/v1/boot... ok
//	lober: no task for
//	Booting from SAN device 0x80 failed: Input/output error
//
// 现场症状同样是「机器在 PXE 里转圈」，而服务端侧任务显示 booting、看起来一切
// 正常，只是永远不会前进。
//
// **为什么不放宽到 installing 及之后。** 那些阶段机器已经在跑 installer 了，
// 再发引导请求意味着它重启回到了 PXE——那是一次真实的异常，应当让阶段超时去
// 判定失败并留下诊断信息，而不是静默重来一轮（重来会把 attempt 计数与阶段
// 时间线都搅乱，失败时反而看不出到底发生了什么）。
//
// 终态（done / failed）绝不接受：done 的任务再接受引导会把一台已交付的生产机
// 重新擦盘装机——这是本系统最严重的一种误操作，也是 §9.1 那三道闸门存在的
// 首要理由。
func acceptsBootRequest(state model.TaskState) bool {
	return state == model.TaskStatePending || state == model.TaskStateBooting
}

// minBootRetryInterval 是两次引导请求之间的最短间隔。
//
// **它区分的是两件外观相同、语义完全相反的事：**
//
//   - 同一次引导的并发重复请求（毫秒级间隔）——必须去重。iPXE 在某些固件上
//     会对同一个 URL 发两次，负载均衡重试、网络重传也会造成同样效果。不去重
//     的后果是一次引导消耗多个 attempt 配额、签发多个 token（旧 token 并不
//     失效，暴露面随之变大）。
//   - 机器真的回来重试（秒级以上）——必须放行，这是 SPEC §3.3「booting |
//     15 min | 允许重试，attempt++」明文要求的。
//
// 30 秒的依据：机器从取到菜单、下载 kernel/initrd（真实镜像 105MB 的 initrd
// 在千兆网上也要几秒）、加载失败、重启回到 PXE、再走完一遍 DHCP，这一圈不可能
// 在 30 秒内完成；而同一次引导的并发重复都在毫秒到秒级。两者之间有两个数量级
// 的间隔，不是一个需要精调的阈值。
//
// 判据用 PhaseEnteredAt 而不是 TokenExpiresAt 或 StateUpdatedAt：前者恰好在
// 每次成功签发 token 时被刷新（见本方法的 mutation），语义正是「上次引导是
// 什么时候开始的」。
const minBootRetryInterval = 30 * time.Second

// errBootRetryTooSoon 表示引导请求来得太快，被判定为同一次引导的并发重复。
//
// 网关对 StartBoot 返回 error 的既有处理是回落到 no_task 载荷（sanboot），
// 这对本情形恰好是对的：并发的那个重复请求本来就不该拿到第二份菜单，而真正
// 在跑的那次引导已经拿到了。
var errBootRetryTooSoon = errors.New("statemachine: 引导请求距上次不足最短重试间隔，" +
	"判定为同一次引导的并发重复，不签发第二个 token")
