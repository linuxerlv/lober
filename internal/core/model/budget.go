// 本文件定义分阶段超时预算表。对应实现计划任务 5，依据 SPEC §3.3、§3.4。
//
// 只设总超时（如 90 分钟）意味着卡在 pre 也要等 90 分钟才知道。分阶段预算
// 能在数分钟内报出，且报出的是具体阶段。
package model

import "time"

// phaseBudgets 是六个阶段各自的超时预算。
//
// pending 刻意不在此表中：那台机器什么时候上电不在本系统控制范围内（§0.1），
// 给它设超时等于替运维的排期做判断。查询该阶段返回「无预算」而不是零值——
// 用「键不存在」表达「永不超时」，比用 0 或一个哨兵值更难被误用（不会被后来
// 一次「顺手补个默认超时」的改动静默覆盖）。
var phaseBudgets = map[Phase]time.Duration{
	PhaseBooting:      15 * time.Minute,
	PhasePre:          10 * time.Minute,
	PhasePackages:     60 * time.Minute,
	PhasePost:         20 * time.Minute,
	PhaseRebooting:    8 * time.Minute,
	PhaseInitializing: 15 * time.Minute,
}

// phaseTimeoutReasons 是六个阶段超时后对应的失败原因。
//
// rebooting 与 initializing 的超时原因（reboot_timeout / init_timeout）落在
// FailureReason 的不可重装组——这两类机器已经装好，重装会擦掉盘上数据（§3.4）。
var phaseTimeoutReasons = map[Phase]FailureReason{
	PhaseBooting:      FailureReasonBootTimeout,
	PhasePre:          FailureReasonInstallPhase,
	PhasePackages:     FailureReasonInstallPhase,
	PhasePost:         FailureReasonInstallPhase,
	PhaseRebooting:    FailureReasonRebootTimeout,
	PhaseInitializing: FailureReasonInitTimeout,
}

// PhaseBudget 查询某阶段的超时预算。ok 为 false 表示该阶段无预算（目前只有
// pending），调用方必须据此理解为「永不超时」，不能当作错误处理。
func PhaseBudget(p Phase) (budget time.Duration, ok bool) {
	budget, ok = phaseBudgets[p]
	return budget, ok
}

// PhaseTimeoutReason 查询某阶段超时后应记录的失败原因。ok 为 false 表示该
// 阶段无预算，因此也不会有「超时」这件事。
func PhaseTimeoutReason(p Phase) (reason FailureReason, ok bool) {
	reason, ok = phaseTimeoutReasons[p]
	return reason, ok
}

// BudgetedPhases 返回有预算的阶段清单，供 Store 构造超时扫描查询时使用
// （扫描循环需要知道该对哪些阶段执行 phase_entered_at + budget < now 的判断，
// 必须排除 pending）。
func BudgetedPhases() []Phase {
	phases := make([]Phase, 0, len(phaseBudgets))
	for p := range phaseBudgets {
		phases = append(phases, p)
	}
	return phases
}
