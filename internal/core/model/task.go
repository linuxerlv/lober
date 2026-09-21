// 本文件定义任务状态机的数据类型：TaskState、FailureReason、Phase、Task、
// TaskMutation。对应实现计划任务 2，依据 SPEC §2.4、§3.1、§3.4。
package model

// TaskState 是任务生命周期的七个取值。SPEC 0.4 已删除 armed 态（§3.2）：
// 建任务本身即授权，不存在「建好了但还不许装」的中间态。
type TaskState string

const (
	TaskStatePending      TaskState = "pending"
	TaskStateBooting      TaskState = "booting"
	TaskStateInstalling   TaskState = "installing"
	TaskStateRebooting    TaskState = "rebooting"
	TaskStateInitializing TaskState = "initializing"
	TaskStateDone         TaskState = "done"
	TaskStateFailed       TaskState = "failed"
)

// IsTerminal 判定该状态是否为终态。终态是 done 与 failed（§3.1）。
func (s TaskState) IsTerminal() bool {
	switch s {
	case TaskStateDone, TaskStateFailed:
		return true
	default:
		return false
	}
}

// FailureReason 是导致任务进入 failed 的九种具体原因（§3.4）。
type FailureReason string

const (
	// 可重装：机器还没真正装完，重来一次不会丢数据。
	FailureReasonBootTimeout     FailureReason = "boot_timeout"
	FailureReasonAttemptExceeded FailureReason = "attempt_exceeded"
	FailureReasonInstallPhase    FailureReason = "install_phase"
	FailureReasonHookRequired    FailureReason = "hook_required"
	FailureReasonPrecheck        FailureReason = "precheck"
	FailureReasonCanceled        FailureReason = "canceled"

	// 不可重装：机器已经装好、盘上可能已有数据，重装会擦掉（§3.4）。
	FailureReasonRebootTimeout FailureReason = "reboot_timeout"
	FailureReasonInitTimeout   FailureReason = "init_timeout"
	FailureReasonInitFailed    FailureReason = "init_failed"
)

// reinstallableFailureReasons 是可重装失败原因的白名单。
//
// 刻意做成白名单而非黑名单：未列入的一律不可重装，包括将来新增却忘了归类的
// 原因。方向是刻意选的——误判成可重装会擦掉生产机上已有的数据，误判成不可
// 重装只是多一次人工介入，两种误判的代价不对等（§3.4）。
var reinstallableFailureReasons = map[FailureReason]bool{
	FailureReasonBootTimeout:     true,
	FailureReasonAttemptExceeded: true,
	FailureReasonInstallPhase:    true,
	FailureReasonHookRequired:    true,
	FailureReasonPrecheck:        true,
	FailureReasonCanceled:        true,
}

// Reinstallable 判定该失败原因的任务是否允许重试装机（走回 pending）。
//
// 未列入白名单的原因（包括拼写错误、尚未存在的未来值）一律返回 false。
func (r FailureReason) Reinstallable() bool {
	return reinstallableFailureReasons[r]
}

// Phase 是任务当前所处的阶段，对应分阶段超时预算的计量单位（§3.3）。
type Phase string

const (
	PhasePending      Phase = "pending"
	PhaseBooting      Phase = "booting"
	PhasePre          Phase = "pre"
	PhasePackages     Phase = "packages"
	PhasePost         Phase = "post"
	PhaseRebooting    Phase = "rebooting"
	PhaseInitializing Phase = "initializing"
	PhaseDone         Phase = "done"
)

// FailureDetail 承载 §3.5 要求的失败诊断信息。state=failed 本身无用，
// 这六项才是运维排查时真正要看的内容。
type FailureDetail struct {
	Phase        Phase  // 失败发生在哪个阶段
	ExitCode     int    // 退出码
	HookDigest   string // 若为 hook 失败，是哪个 hook（空表示非 hook 失败）
	LogTail      string // 最后 4 KB 安装日志，直接可读
	SyslogTail   string // installer 自身通过 syslog 上报的原始报错
	SinceLastSig string // 距上次收到信号的时长描述，区分「机器活着」与「机器出事」
}

// Task 是一次装机任务的完整记录（§2.4）。字段按语义分组排列。
type Task struct {
	// 标识与创建信息
	ID        string
	CreatedAt int64 // Unix 秒
	CreatedBy string

	// 三个查找键，按 SN → MAC → UUID 优先级回退匹配（§4.3）
	HostSN   string
	HostMAC  string
	HostUUID string

	// 三项 pin 引用（§2.6）：正在运行的任务不受后续改模板影响，且可复现
	TemplateID      string
	TemplateVersion int
	ArtifactSetID   string
	HookDigests     []string

	// 表单与决策参数
	FormData       map[string]string // 运维填的原始表单：biz_ip / hostname / role / hook 参数
	ResolvedParams map[string]string // FormData + provider 决策结果 + 系统生成，渲染上下文的直接来源

	// 状态与并发控制
	State          TaskState
	StateUpdatedAt int64
	Version        int // 乐观锁：所有状态转换须以此做 CAS（§3.6）

	CurrentPhase   Phase
	PhaseEnteredAt int64 // 分阶段超时判定的起点；pending 阶段无预算（§3.3）

	AttemptCount int   // 菜单取用次数，超过上限即 failed（§9.1 第三道闸门）
	LastSeq      int   // 信号去重水位：收到的序号不大于此值时只记事件不改状态（§3.6）
	LastSignalAt int64 // 失败诊断用：距今多久收到过信号

	// 凭据
	Token             string
	TokenExpiresAt    int64 // 装机 token 过期时间，计时起点是首次 /v1/boot 匹配成功（§4.4）
	FinalizeToken     string
	FinalizeExpiresAt int64 // finalize token 有独立的过期时间，与装机 token 互不牵连（§6.4）

	// 失败详情
	FailureReason FailureReason
	FailureDetail *FailureDetail
}

// TaskMutation 是「可选字段更新集」：每个字段是指针，nil 表示不改该字段。
// 它是 Store 写操作（改运行时字段，不涉及状态转换）的统一入参，
// 避免给 Store 加二十个 UpdateXxx 方法（计划任务 2 设计要点）。
//
// 契约约束：携带 State 字段的 mutation 必须被 Store 实现拒绝——状态转换只能
// 走 CAS 方法，留一条不走 CAS 的改状态路径迟早会被用来绕过「禁止重装」判定。
// 本类型故意不含 State 字段，从类型层面消除这个后门。
type TaskMutation struct {
	CurrentPhase      *Phase
	PhaseEnteredAt    *int64
	AttemptCount      *int
	LastSeq           *int
	LastSignalAt      *int64
	Token             *string
	TokenExpiresAt    *int64
	FinalizeToken     *string
	FinalizeExpiresAt *int64
	ResolvedParams    *map[string]string
	FailureReason     *FailureReason
	FailureDetail     *FailureDetail
}
