// 本文件定义信号表：机器侧上报的信号名到单调序号、目标状态、目标阶段的映射。
// 对应实现计划任务 4，依据 SPEC §3.6、§6.1、§6.4。
//
// SPEC §3.6 要求延迟到达的 packages-start 不能把已经 post-done 的任务打回去。
// 判据是给每个信号一个单调序号，收到的序号不大于当前水位（Task.LastSeq）时
// 只记事件、不改状态。这张表是 M3（状态机）与 M5（网关）共同依赖的唯一判据。
package model

// SignalKind 是信号名，来自机器侧 curl 请求的 URL 路径段（§6.2）。
type SignalKind string

const (
	SignalPreStart      SignalKind = "pre-start"
	SignalPreDone       SignalKind = "pre-done"
	SignalPackagesStart SignalKind = "packages-start"
	SignalPackagesDone  SignalKind = "packages-done"
	SignalPostStart     SignalKind = "post-start"
	SignalPostDone      SignalKind = "post-done"
	SignalRebooting     SignalKind = "rebooting"
	SignalBooted        SignalKind = "booted"
	SignalInitStart     SignalKind = "init-start"
	SignalInitDone      SignalKind = "init-done"
	SignalReady         SignalKind = "ready"
	SignalHookReport    SignalKind = "hook-report"
)

// SignalEntry 是信号表一行：单调序号、该信号推进到的状态与阶段。
type SignalEntry struct {
	// Seq 是单调序号，用于判定「只进不退」：收到的序号不大于任务当前水位时
	// 只记事件、不改状态（§3.6）。序号之间刻意留间隔，便于将来插入中间信号
	// 而不必改已有值。
	Seq int
	// State 是该信号使任务进入的状态。
	State TaskState
	// Phase 是该信号使任务进入的阶段。
	Phase Phase
}

// signalTable 是信号名到信号表项的映射，十一个装机流程信号加 hook-report。
//
// hook-report 的序号是 0，且 State/Phase 留零值——它与阶段正交，一个阶段内
// 可来任意多次（有几个 hook 就有几次上报），永不推进水位（§6.1）。
var signalTable = map[SignalKind]SignalEntry{
	SignalPreStart:      {Seq: 10, State: TaskStateInstalling, Phase: PhasePre},
	SignalPreDone:       {Seq: 20, State: TaskStateInstalling, Phase: PhasePre},
	SignalPackagesStart: {Seq: 30, State: TaskStateInstalling, Phase: PhasePackages},
	SignalPackagesDone:  {Seq: 40, State: TaskStateInstalling, Phase: PhasePackages},
	SignalPostStart:     {Seq: 50, State: TaskStateInstalling, Phase: PhasePost},
	SignalPostDone:      {Seq: 60, State: TaskStateInstalling, Phase: PhasePost},
	SignalRebooting:     {Seq: 70, State: TaskStateRebooting, Phase: PhaseRebooting},
	SignalBooted:        {Seq: 80, State: TaskStateInitializing, Phase: PhaseInitializing},
	SignalInitStart:     {Seq: 90, State: TaskStateInitializing, Phase: PhaseInitializing},
	SignalInitDone:      {Seq: 100, State: TaskStateInitializing, Phase: PhaseInitializing},
	SignalReady:         {Seq: 110, State: TaskStateDone, Phase: PhaseDone},

	// hook-report：序号 0，不带状态与阶段推进（保持 TaskState/Phase 零值，
	// 调用方必须依据 LookupSignal 的第二个返回值判断是否要应用状态/阶段，
	// 不能把零值误当作「推进到 pending」）。
	SignalHookReport: {Seq: 0},
}

// finalizeSignals 是 finalize token 有权发送的信号集合（§6.4）：只有
// 重启后阶段的几个信号与逐 hook 上报。M5 用它做权限判定——装机 token 的
// 权限覆盖应答文件与全部信号，finalize token 的权限窄得多。
var finalizeSignals = map[SignalKind]bool{
	SignalBooted:     true,
	SignalInitStart:  true,
	SignalInitDone:   true,
	SignalReady:      true,
	SignalHookReport: true,
}

// LookupSignal 按信号名查表。ok 为 false 表示该信号名不在表中，调用方必须
// 拒绝——信号名来自 URL 路径段，不查表校验等于让机器指定自己的阶段（§6.1）。
func LookupSignal(kind SignalKind) (entry SignalEntry, ok bool) {
	entry, ok = signalTable[kind]
	return entry, ok
}

// FinalizeCanSend 判定 finalize token 是否有权发送该信号（§6.4）。
func FinalizeCanSend(kind SignalKind) bool {
	return finalizeSignals[kind]
}
