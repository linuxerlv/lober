// 本文件实现超时扫描 ScanTimeouts（任务 9）与自动重试 AutoRetryFailedTasks
// （Phase 4 新增，实现计划 phase4-01 任务 4）。依据 SPEC §3.3、§3.4、§11。
//
// 分阶段超时能在数分钟内报出卡在哪个阶段。这是「机器卡住了」的唯一发现
// 途径——机器不会主动报告自己挂了。
//
// AutoRetryFailedTasks 与 ScanTimeouts 是两个独立的扫描动作，各自被
// 装配点（cmd/lober/wire.go）在 30 秒周期循环里分别调用：ScanTimeouts
// 找「卡住」的任务，AutoRetryFailedTasks 找「已经失败但值得自动再试一次」
// 的任务。两者共用同一个扫描节奏纯粹是实现上的便利（沿用 Phase 1 已有的
// 循环，不新开一个 goroutine），不代表两者有调用顺序依赖——本文件不修改
// ScanTimeouts 或 handleTimedOutTask 的任何既有行为（铁律 3：不得改动
// 既有函数导致任何现存测试变红）。
package statemachine

import (
	"context"
	"fmt"
	"strings"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// rebootingTimeoutMarker 与 initializingTimeoutMarker 是 rebooting/
// initializing 超时诊断事件里必须出现的标记文案，用来与「装机失败」明确
// 区分——两者处理方式完全不同（前者不能重装，见 SPEC §3.4）。
const (
	rebootingTimeoutMarker    = "装好但未起来"
	initializingTimeoutMarker = "已就绪但初始化未完成"
)

// ScanTimeouts 由 30 秒周期循环调用，扫描并处理所有超时任务
// （model.PhaseBudget 定义的预算，pending 阶段永不超时）。返回本轮被判
// 失败的任务数（不含回 pending 的）。
func (m *Machine) ScanTimeouts(ctx context.Context, asOf int64) (int, error) {
	candidates, err := m.store.ListTimedOutTasks(ctx, asOf)
	if err != nil {
		return 0, err
	}

	failedCount := 0
	for _, candidate := range candidates {
		failed, err := m.handleTimedOutTask(ctx, candidate, asOf)
		if err != nil {
			// 单个任务处置失败不中断整轮：记日志继续，不返回错误——
			// 一个任务撞 CAS 冲突不该让其余任务本轮不被处理。本模块
			// 没有被注入 logger 依赖（六个依赖里没有这一项），因此这里
			// 只能静默跳过；见本文件末尾契约缺口说明。
			continue
		}
		if failed {
			failedCount++
		}
	}
	return failedCount, nil
}

// handleTimedOutTask 处置单个超时候选任务，返回该任务是否被判 failed
// （true）或回到 pending / 因已被并发操作终态化而跳过（false）。
func (m *Machine) handleTimedOutTask(ctx context.Context, candidate model.Task, asOf int64) (bool, error) {
	if candidate.CurrentPhase == model.PhaseBooting {
		return m.handleBootingTimeout(ctx, candidate, asOf)
	}
	return m.handleOtherPhaseTimeout(ctx, candidate, asOf)
}

// handleBootingTimeout 处置 booting 阶段超时：SPEC §3.3 与 §3.4 两处措辞
// 不同（前者写「允许重试，attempt++」，后者把「booting 超时」列为失败
// 态），本模块采用的决断：attempt_count < 上限则回 pending（机器可自行
// 再来一次，且回 pending 时不增加 attempt——attempt 是在实际签发 token
// 时增加的，这里再加一次会让每次超时消耗两个额度）；已达上限则置
// failed（原因是 attempt 超限）。
func (m *Machine) handleBootingTimeout(ctx context.Context, candidate model.Task, asOf int64) (bool, error) {
	if candidate.AttemptCount < model.MaxBootAttempts {
		result, err := m.casRetry(ctx, candidate.ID, func(current model.Task) transitionDecision {
			if current.State.IsTerminal() || current.CurrentPhase != model.PhaseBooting {
				// 扫描到候选之后、真正处置之前，任务已被并发操作
				// 终态化或推进出 booting 阶段——跳过，不算失败。
				return transitionDecision{noop: true}
			}
			pendingPhase := model.PhasePending
			return transitionDecision{
				mutation: &model.TaskMutation{
					CurrentPhase:   &pendingPhase,
					PhaseEnteredAt: &asOf,
				},
				newState: model.TaskStatePending,
			}
		})
		if err != nil {
			return false, err
		}
		_ = result
		return false, nil
	}

	if _, err := m.failTask(ctx, candidate.ID, model.FailureReasonAttemptExceeded, model.FailureDetail{
		Phase: model.PhaseBooting,
	}, asOf); err != nil {
		return false, err
	}
	return true, nil
}

// handleOtherPhaseTimeout 处置 booting 之外各阶段的超时：按 M1 的超时
// 原因映射置 failed。rebooting 与 initializing 的失败原因落在不可重装
// 那一组（M1 已有测试锁住这条映射，本模块只需正确使用它）。
func (m *Machine) handleOtherPhaseTimeout(ctx context.Context, candidate model.Task, asOf int64) (bool, error) {
	reason, ok := model.PhaseTimeoutReason(candidate.CurrentPhase)
	if !ok {
		// model.PhaseBudget 已经保证只有有预算的阶段才会出现在
		// ListTimedOutTasks 的结果里，因此这里理论上不会发生；防御性地
		// 跳过而不是 panic。
		return false, nil
	}

	budget, _ := model.PhaseBudget(candidate.CurrentPhase)
	actualElapsed := asOf - candidate.PhaseEnteredAt

	logTail := fmt.Sprintf("阶段 %s 超时: 预算=%s 实际已过=%d秒", candidate.CurrentPhase, budget, actualElapsed)
	switch candidate.CurrentPhase {
	case model.PhaseRebooting:
		logTail = fmt.Sprintf("[%s] %s", rebootingTimeoutMarker, logTail)
	case model.PhaseInitializing:
		logTail = fmt.Sprintf("[%s] %s", initializingTimeoutMarker, logTail)
	}

	if _, err := m.failTask(ctx, candidate.ID, reason, model.FailureDetail{
		Phase:   candidate.CurrentPhase,
		LogTail: logTail,
	}, asOf); err != nil {
		return false, err
	}
	return true, nil
}

// autoRetryWhitelist 是「值得自动重试」的失败原因白名单（计划任务 4）。
//
// 这是比 model.FailureReason.Reinstallable() 更窄的第二层判定——不是
// 「能不能重装」，是「重装大概率会成功、值得系统自己动手」。计划任务 4
// 明确给出的理由：
//   - boot_timeout：常见原因是网络抖动或制品传输失败，重试大概率成功
//   - install_phase：安装阶段失败，若退出码指向瞬时问题（如仓库暂时不可
//     达）值得重试
//
// 刻意排除的三个可重装原因（哪怕它们通过 Reinstallable() 也不进这个
// 白名单，见 isAutoRetryEligible 的两层判定）：
//   - precheck：§4.6「重试必然同样失败」——架构/固件与制品不匹配，系统
//     自动重试不会让这个事实改变
//   - hook_required：脚本或环境的确定性问题，不是瞬时故障
//   - canceled：人工主动取消是一个深思熟虑的决定，系统不应该自作主张
//     撤销它——这与「稍后重试就会成功」的判据完全无关，是另一个维度的
//     排除理由
//   - attempt_exceeded：任务已经用尽三次装机 token 机会仍未成功（§9.1
//     第三道闸门），这本身就是「已经给过三次机会，需要人工判断为什么
//     每次都不成功」的信号，自动重试会绕开这道闸门存在的意义
//
// 契约缺口：计划任务 4 的原话是「安装阶段失败且退出码指向瞬时问题」——
// 这暗示 install_phase 的白名单资格应该再按 FailureDetail.ExitCode 细分
// （只对指向瞬时问题的具体退出码自动重试，而不是所有 install_phase 失败
// 都重试）。但本仓库目前没有任何地方定义「哪些退出码算瞬时问题」这份
// 清单（installer 的退出码含义因 OS 家族而异，且 SPEC 与既有代码都没有
// 建立这层退出码分类），本模块无法凭空发明一份没有依据的清单——错误地把
// 某个「确定性失败」的退出码也归类成瞬时问题，后果是对一个必然复现的
// 失败反复自动重试，浪费的是运维本该更早介入的时间窗口。最保守的处理：
// install_phase 整体进入白名单（与计划任务 4 表格的字面意思一致，「安装
// 阶段失败」本身就是那一行的判据），退出码细分留给正解：在
// docs/contracts 或 model 层为每个 OS 家族建立「瞬时 vs 确定性」退出码
// 分类表之后，再把这里的判据从「reason==install_phase」细化为
// 「reason==install_phase && isTransientExitCode(detail.ExitCode)」。
var autoRetryWhitelist = map[model.FailureReason]bool{
	model.FailureReasonBootTimeout:  true,
	model.FailureReasonInstallPhase: true,
}

// autoRetryEventMarker 是自动重试写入的事件 Message 里固定出现的前缀，
// 供 countPriorAutoRetries 用「查库统计含这个标记的历史事件条数」的方式
// 实现「本任务已经被自动重试过几次」这个计数——见 countPriorAutoRetries
// 文档的契约缺口说明：这不是本模块的第一选择，是在契约现状下能做到的
// 最保守替代。
const autoRetryEventMarker = "lober:auto-retry"

// AutoRetryConfig 是驱动 AutoRetryFailedTasks 的开关与参数，由装配点
// （cmd/lober/wire.go）从 config.Scan 读出后原样传入。做成一个显式参数
// 结构体而不是 Machine 的字段：Machine 的六个依赖在 machine.go 冻结（本
// 任务的文件归属不含 machine.go），本方法用「调用时传参」而不是「构造时
// 注入」绕开这个限制——AutoRetryFailedTasks 本身是无状态的纯函数式扫描
// （每次调用独立查库判定，不依赖任何跨调用保留的字段），传参这种形式对
// 它完全够用，不像 AdmissionController 的上限那样需要跨越 StartBoot 与
// 装配点两个不同代码位置共享同一个值（那种情形才需要 admission.go 的
// atomic 包级变量方案）。
type AutoRetryConfig struct {
	// Enabled 是计划任务 4「独立开关，默认关闭」——自动重装是一个需要
	// 运维明确同意的行为：§3.2 已经说明「建任务即授权」的残留风险（机器
	// 一旦网络启动就会装机，本系统没有远程关机手段），自动重试把这个
	// 风险窗口从「一次」变成「多次」，运维可能没意识到自己开启了这个
	// 行为，因此默认必须是关闭。
	Enabled bool

	// MaxAutoRetries 是同一任务允许被自动重试的独立次数上限，必须比
	// 手工重试（无次数上限，人一直点就一直允许）更严格——计划任务 4
	// 建议「自动只重试一次，之后必须人工介入」。0 或负数视为「不允许
	// 自动重试任何任务」（即使 Enabled=true）——这与「Enabled 默认
	// false」是两道独立的闸门，任何一道关着都不会发生自动重试。
	MaxAutoRetries int
}

// AutoRetryFailedTasks 扫描当前处于 failed 状态的任务，对满足「可重装 +
// 在自动重试白名单内 + 尚未用尽独立的自动重试次数上限」这三条的任务调用
// 现有的 Retry（复用它已经做的 CAS 转换与不可重装拒绝，本方法不重新实现
// 任何状态转换逻辑），返回本轮实际发起的自动重试次数。
//
// 三层判定，缺一层都不能触发自动重试（计划任务 4「三条限制，缺一条都
// 不能上这个功能」）：
//  1. cfg.Enabled 为 true（独立开关，默认关闭）
//  2. reason.Reinstallable() 为 true 且 autoRetryWhitelist[reason] 为
//     true（先看可否重装，再看是否在「值得自动重试」的白名单里——计划
//     任务 4 的原话）。第一层判定直接复用 model.FailureReason.
//     Reinstallable()，不重新定义判据本身（三种不可重装原因——
//     reboot_timeout/init_timeout/init_failed——由这一层挡住，永不
//     触达 Retry 调用）。
//  3. 本任务此前被自动重试的次数 < cfg.MaxAutoRetries（独立于手工重试
//     次数的上限，见 countPriorAutoRetries）
//
// 不复用 ScanTimeouts 的候选查询：ScanTimeouts 用 ListTimedOutTasks 找
// 「阶段预算已过期」的任务，自动重试关心的是「已经是 failed 终态」的
// 任务，两者是完全不同的查询维度（前者查未终态任务的阶段超时，后者查
// 终态任务的失败原因），因此这里用 ListTasks(TaskFilter{State: Failed})
// 独立查询，不与 ScanTimeouts 共享候选集合或处置循环。
func (m *Machine) AutoRetryFailedTasks(ctx context.Context, cfg AutoRetryConfig) (int, error) {
	if !cfg.Enabled || cfg.MaxAutoRetries <= 0 {
		return 0, nil
	}

	candidates, err := m.store.ListTasks(ctx, contract.TaskFilter{State: model.TaskStateFailed})
	if err != nil {
		return 0, err
	}

	retried := 0
	for _, candidate := range candidates {
		did, err := m.maybeAutoRetryTask(ctx, candidate, cfg)
		if err != nil {
			// 单个任务处置失败不中断整轮：与 ScanTimeouts 的既有取舍
			// 一致（见 ScanTimeouts 文档），一个任务撞 CAS 冲突或查询
			// 失败不该让其余候选任务本轮不被处理。
			continue
		}
		if did {
			retried++
		}
	}
	return retried, nil
}

// maybeAutoRetryTask 对单个失败任务判定是否应该自动重试，若判定通过则
// 真正调用 Retry 并写一条可被 countPriorAutoRetries 统计到的标记事件。
func (m *Machine) maybeAutoRetryTask(ctx context.Context, candidate model.Task, cfg AutoRetryConfig) (bool, error) {
	if !candidate.FailureReason.Reinstallable() {
		// 第二层判定的前半句：三种不可重装原因（reboot_timeout/
		// init_timeout/init_failed）必须在这里就被挡住，永不触达下面
		// 的白名单检查或 Retry 调用——这是本任务最危险的一条逻辑，见
		// 本文件autoRetryWhitelist 文档与调用方（scan_test.go）逐个
		// 原因验证的测试。
		return false, nil
	}
	if !autoRetryWhitelist[candidate.FailureReason] {
		// 第二层判定的后半句：即使可重装，也只对「值得自动重试」的
		// 白名单内原因（boot_timeout、install_phase）动手——precheck/
		// hook_required/canceled 即使可重装也不在这个更窄的白名单内。
		return false, nil
	}

	priorCount, err := m.countPriorAutoRetries(ctx, candidate.ID)
	if err != nil {
		return false, err
	}
	if priorCount >= cfg.MaxAutoRetries {
		return false, nil
	}

	if _, err := m.Retry(ctx, candidate.ID); err != nil {
		// Retry 自身的 Reinstallable 判定与本方法上面那一层判定完全
		// 一致（两处都调用 model.FailureReason.Reinstallable()），
		// 正常情况下不会在这里被拒绝；若确实拒绝了（例如两次判定之间
		// 任务被并发操作改变），静默跳过而不是把这个内部一致性问题
		// 当作整轮扫描的错误上抛。
		return false, nil
	}

	m.appendAutoRetryMarkerEvent(ctx, candidate)
	return true, nil
}

// appendAutoRetryMarkerEvent 写一条标记事件，供 countPriorAutoRetries
// 统计「这个任务已经被自动重试过几次」，同时也是计划任务 4「自动重试要
// 写审计记录，操作人记为系统」这条要求在事件表这一层的落地方式——见本
// 方法文档下方的契约缺口说明。
//
// 契约缺口：计划任务 4 的原话是「自动重试要写审计记录，操作人记为系统
// （理由：误装事故的追溯需要能区分人建的任务与系统自动重试的任务）」，
// 这暗示应该调用 contract.AuditLogger.Log(ctx, "system", "task.auto_retry",
// taskID, ...)——但 Machine 的六个依赖（Store、Clock、TokenIssuer、
// Prechecker、LogStore、EventBus）里没有 AuditLogger 这一项，
// statemachine.New 的构造签名冻结在 machine.go（不在本任务文件归属范围
// 内），本模块无法在不改动 machine.go 的前提下注入一个新依赖。
//
// 最保守的绕法（不改 machine.go）：把「操作人是系统」这条信息写进任务
// 事件表（本方法），而不是审计表——TaskEvent 本身就有 Source 字段
// （model.EventSourceProvider，本模块选用这个既有取值表示「系统内部
// 决策产生的事件」，不新增枚举值——model 包已冻结）与 Message 字段
// （承载「auto-retry」这个可读标记），任务详情页/事件流已经能看到这条
// 记录，区分度足以支持「这次重试是系统自动发起还是运维手工点击」这条
// 追溯需求。这不等价于真正写入 audit_log 表（那张表是人面操作审计的
// 权威落点，§9.4），只是在当前契约下能做到的最接近的替代。
//
// 正解方向（不是本方法能做到的）：给 statemachine.New 或 Machine 结构体
// 加一个 contract.AuditLogger 依赖（需要改动 machine.go），
// AutoRetryFailedTasks 判定通过后改为调用真正的 AuditLogger.Log 而不是
// 只写任务事件；同时 EventFilter 也应该补上按 Message 前缀或新增
// 「事件类型」维度过滤的能力（当前只有 AfterSeq，countPriorAutoRetries
// 不得不取回该任务全部事件再在内存里过滤，见该方法文档），避免历史事件
// 很多的任务在每次扫描时都要整表反序列化。
func (m *Machine) appendAutoRetryMarkerEvent(ctx context.Context, task model.Task) {
	now := m.clock.Now()
	message := fmt.Sprintf("%s: 系统自动重试（failure_reason=%s），原任务已重置为 pending",
		autoRetryEventMarker, task.FailureReason)
	_, _ = m.store.AppendEvent(ctx, model.TaskEvent{
		TaskID:  task.ID,
		Seq:     0,
		TS:      now,
		Phase:   task.CurrentPhase,
		Level:   model.EventLevelInfo,
		Message: message,
		Source:  model.EventSourceProvider,
	})
}

// countPriorAutoRetries 统计该任务此前被自动重试过的次数，用于
// AutoRetryConfig.MaxAutoRetries 这条独立于手工重试次数的上限判定。
//
// 契约缺口：这个计数本应该是 Task 或 TaskMutation 上的一个持久化字段
// （例如 AutoRetryCount int），随每次自动重试自增、随 Retry 复用既有的
// CAS 转换一起原子更新——但 internal/core/model 已冻结（本任务文件归属
// 不含 model 包），不能加新字段；TaskMutation 同理冻结，且它本来就没有
// 「新建一个字段」这种口子。contract.EventFilter 也只支持 AfterSeq 这一
// 个过滤维度（没有按 Message 内容或来源过滤的能力，见 store.go），因此
// 这里能做到的最保守方式是：取回该任务的全部事件（ListEvents 不分页，
// 见 sqlite/event.go 的实现——一次性返回全部匹配行，不会漏计），在内存
// 里数出 Message 含 autoRetryEventMarker 标记的条数。
//
// 这不是理论上最高效的实现（任务事件很多时每次扫描都要整表取回），但
// 任务事件表本身就是「百级任务 × 个位数事件」的量级（§2.5 的信号点加
// hook 上报），不是无界增长的日志表；且 AutoRetryFailedTasks 只在
// MaxAutoRetries 通常取 1 的场景下调用，被扫描到的 failed 任务数本身
// 也是运维批次装机的量级，不是持续高频路径。正解方向见
// appendAutoRetryMarkerEvent 文档：加一个持久化计数字段 + 原子更新，
// 或至少让 EventFilter 支持按 Source 过滤，减少一次性拉回的数据量。
func (m *Machine) countPriorAutoRetries(ctx context.Context, taskID string) (int, error) {
	events, err := m.store.ListEvents(ctx, taskID, contract.EventFilter{})
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range events {
		if e.Source == model.EventSourceProvider && strings.Contains(e.Message, autoRetryEventMarker) {
			count++
		}
	}
	return count, nil
}
