// 本文件定义服务契约：M3（状态机）、M4（渲染器）与 M5（网关）、M6（控制台）
// 之间的全部接缝。对应实现计划任务 11，依据 SPEC §3.1–§3.6、§4.2、§4.4、
// §4.6、§6.4、§7.5、§9.1、§10。
//
// M5 与 M6 只依赖本文件的接口、不 import M3/M4 的实现包，因此四个模块可以
// 完全并行开发。
package contract

import (
	"context"
	"errors"
	"io/fs"

	"github.com/lober/lober/internal/core/model"
)

// ErrNotReinstallable 是 StateMachine.Retry 对三种不可重装失败原因
// （reboot_timeout、init_timeout、init_failed）的拒绝信号，调用方用
// errors.Is 判定。判据与 model.FailureReason.Reinstallable() 完全一致——
// Retry 的实现必须直接调用该方法而不是自行重复分类逻辑，否则两处判据一旦
// 走散，就会出现「白名单说可以重装，状态机却拒绝」或反过来的裂缝（§3.4）。
var ErrNotReinstallable = errors.New("contract: 该失败原因不允许重装")

// Clock 存在的唯一理由是超时逻辑必须可测：分阶段超时与 token TTL 都需要
// 在测试里瞬时推进时间，60 分钟的 packages 预算没法真等（计划任务 11）。
type Clock interface {
	// Now 返回当前时间的 Unix 秒——与 model.Task 里时间字段的单位对齐。
	Now() int64
}

// PrecheckFailure 标识 §4.6 三项一致性校验里具体是哪一项没通过。
type PrecheckFailure string

const (
	// PrecheckFailureArch 表示 arch 一致性不通过：${buildarch} 与
	// artifact_set.arch 不符（x86_64 制品发给 arm64 机器会导致 kernel
	// 加载失败）。
	PrecheckFailureArch PrecheckFailure = "arch_mismatch"

	// PrecheckFailureFirmware 表示固件模式不兼容：查 os_family 的固件要求
	// 后发现不满足（如 esxi + pcbios，§0.3、§4.6）。
	PrecheckFailureFirmware PrecheckFailure = "firmware_incompatible"

	// PrecheckFailureArtifact 表示制品完整性不通过：paths 中声明的文件在
	// 盘上不真实存在（§10 运维点一）。
	PrecheckFailureArtifact PrecheckFailure = "artifact_incomplete"
)

// PrecheckError 是 Prechecker 返回的错误类型，调用方用类型断言取出「哪一项
// 不过」，据此给运维精确原因（如「这台机器是 BIOS，任务要装 ESXi」）。
//
// 之所以做成具体类型而不是把原因塞进 errors.New 的字符串，是因为调用方
// （状态机置 failed 时）需要把 Failure 字段结构化地写进
// model.FailureDetail，不能靠解析错误消息字符串。
type PrecheckError struct {
	Failure PrecheckFailure
	// Detail 是给运维看的具体说明（如「机器上报 pcbios，任务要求 uefi」），
	// 中文，可直接展示。
	Detail string
}

// Error 实现 error 接口。
func (e *PrecheckError) Error() string {
	return "precheck 未通过 [" + string(e.Failure) + "]: " + e.Detail
}

// Prechecker 在签发 token 与下发菜单之前调用，做 §4.6 的三项一致性校验：
// arch 一致性、固件模式兼容、制品完整性。三项校验全部通过返回 nil error；
// 任意一项不过返回 *PrecheckError。
type Prechecker interface {
	// Check 校验 actualArch/actualFirmware（机器在本次 /v1/boot 请求中通过
	// ${buildarch}/${platform} 实际上报的值，由调用方用
	// model.ParseIPXEBuildArch/ParseIPXEPlatform 解析后传入）与 artifact
	// 是否匹配，并校验 artifact.Paths 声明的文件是否都能在 files 中找到。
	//
	// files 用 io/fs.FS 注入而不是直接调用 os.Stat——测试里换成内存文件
	// 系统（fstest.MapFS 之类），不依赖真实磁盘布局。
	Check(ctx context.Context, actualArch model.Arch, actualFirmware model.FirmwareMode, artifact model.ArtifactSet, files fs.FS) error
}

// TokenIssuer 只负责生成不可预测的随机串，不负责持久化与校验——那是 Store
// 的事。分开的理由：token 必须能被状态门控（仅 booting/installing 时可用，
// §4.4），而状态在库里，自包含的签名 token（如 JWT）做不到即时状态门控
// （计划任务 11）。
type TokenIssuer interface {
	// IssueToken 生成一个不可预测的随机字符串，适合放入 URL 路径段
	// （§4.4：token 走路径段不用 query string）。
	IssueToken(ctx context.Context) (string, error)
}

// SignalOutcome 是 StateMachine.ApplySignal 的返回结构。「已接受」与「改变
// 了状态」是两回事，分别表达——handler 据此决定回什么给机器、以及是否要
// 落日志文件（计划任务 11 设计要点）。
type SignalOutcome struct {
	// Accepted 表示信号已被记录（无论是否推进了状态）。目前 ApplySignal
	// 对合法信号名总是返回 true；仅当信号名本身不在 model.LookupSignal 的
	// 表中时才不 Accepted（此时应由调用方在更早的位置拒绝，见
	// ApplySignal 的说明）。
	Accepted bool

	// StateChanged 表示本次信号是否让 task.state 或 task.current_phase
	// 发生了变化。三种情况下为 false：序号重复、序号不大于当前水位
	// （§3.6 阶段只进不退）、任务已处于终态（done/failed）——这三种情况都
	// 只记事件、不改状态。
	StateChanged bool

	// Task 是处理后的任务当前状态（无论 StateChanged 是否为真，都返回
	// 任务的最新值，便于调用方直接使用而不必再查一次）。
	Task model.Task
}

// StateMachine 是 task.state 的唯一所有者。网关与控制台都只通过它改状态，
// 这是「网关与控制台是同一进程的两个 listener」的根本理由（§1.1）。
type StateMachine interface {
	// StartBoot 处理 /v1/boot 命中 pending 任务后的推进（§4.2）。
	//
	// 顺序是硬约束，不可调整：
	//  1. 调用 Prechecker（§4.6）。不通过：置 failed（携带
	//     FailureReasonPrecheck），且不消耗 attempt 计数——这不是可重试的
	//     偶发故障，重试必然同样失败，消耗 attempt 只会让运维少三次真正
	//     的重试机会。
	//  2. attempt_count++。
	//  3. 若 attempt_count 超过 model.MaxBootAttempts：置 failed（携带
	//     FailureReasonAttemptExceeded）。
	//  4. 签发装机 token（调用 TokenIssuer），TokenExpiresAt = now +
	//     model.InstallTokenTTL。
	//  5. CAS 到 booting。
	//
	// actualArch/actualFirmware 是本次请求机器实际上报的值（供第 1 步的
	// Prechecker 使用）。
	StartBoot(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error)

	// MarkAnswerFetched 处理应答文件被取走后的推进：booting → installing。
	//
	// 必须幂等——installer 可能重复取应答文件（如 Ubuntu 的 cloud-init 会
	// 取 user-data 与 meta-data 两个文件），第二次调用不应报错，也不应
	// 再次触发状态变化之外的副作用。任务已不在 booting（已经是
	// installing 或之后）时，视为已处理过，直接返回当前任务、不报错。
	MarkAnswerFetched(ctx context.Context, taskID string) (model.Task, error)

	// ApplySignal 处理机器侧上报的信号（§6.1、§3.6）。
	//
	// 幂等的三种情况（只记事件、不改状态）：
	//   - 序号重复（Store 层的 (task_id, seq) 去重命中 ErrDuplicateSeq）
	//   - 序号不大于任务当前水位 LastSeq（阶段只能前进不能后退）
	//   - 任务已处于终态（done/failed）
	// 以上三种都不是错误，ApplySignal 应正常返回
	// SignalOutcome{Accepted: true, StateChanged: false}，不返回 error。
	//
	// signal 若不在 model.LookupSignal 的表中，说明信号名来自 URL 路径段
	// 且未经过校验就直接调用到这里——这是调用方（网关 handler）的职责
	// 疏漏，ApplySignal 返回 error，不是 SignalOutcome。
	ApplySignal(ctx context.Context, taskID string, signal model.SignalKind, seq int) (SignalOutcome, error)

	// IssueFinalizeToken 在渲染 %post 之前调用，签发 finalize token 并写入
	// 任务（供渲染器注入进目标系统，重启后阶段用，§6.4）。
	// FinalizeExpiresAt = now + model.FinalizeTokenTTL。
	IssueFinalizeToken(ctx context.Context, taskID string) (model.Task, error)

	// Fail 把任务置为失败，写入 model.FailureDetail 描述的 §3.5 六个诊断
	// 字段（作为一条事件落库，事件的 Level 为 EventLevelError）。
	Fail(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error)

	// Cancel 是人工取消：任意非终态 → failed（FailureReasonCanceled）。
	// pending 任务取消后即不再响应引导（§3.2 的缓解手段）。
	Cancel(ctx context.Context, taskID string) (model.Task, error)

	// Retry 重置 attempt_count 为 0、签发新 token → pending（§8.2）。
	//
	// 对三种不可重装的失败原因必须拒绝（§3.4）：reboot_timeout、
	// init_timeout、init_failed——判据是 reason.Reinstallable()（model 层
	// 已把这三个排除在白名单外）。不满足时返回 ErrNotReinstallable
	// （调用方用 errors.Is 判定，据此给出恰当的 HTTP 状态码）。拒绝在状态
	// 机里，不是靠 UI 隐藏按钮：即使控制台的按钮被绕过直接调 API，
	// 这里仍必须拒绝。
	Retry(ctx context.Context, taskID string) (model.Task, error)

	// ScanTimeouts 由 30 秒周期循环调用，扫描并处理所有超时任务
	// （model.PhaseBudget 定义的预算，pending 阶段永不超时）。
	// 返回本轮被判失败的任务数。
	ScanTimeouts(ctx context.Context, asOf int64) (failedCount int, err error)
}

// Renderer 是纯函数：输入渲染上下文，输出文本，不查库、不碰时钟（§5.1）。
// 这是控制台 preview 端点能用假数据跑的前提，也是 golden 测试的前提。
type Renderer interface {
	// RenderMenu 渲染 iPXE 菜单文本（§4.5 各 OS 家族形态）。
	RenderMenu(ctx model.RenderContext) (string, error)

	// RenderAnswerFile 渲染应答文件文本。fileName 是机器请求带来的路径段，
	// 必须校验它等于 ctx.AnswerFileName() 算出的文件名——路径段来自机器
	// 请求，不能用它去选模板，否则机器可以通过改路径段拿到别的模板内容
	// （Ubuntu 例外：额外接受 "meta-data"，因为 cloud-init 会取两个文件）。
	// 文件名不匹配时返回 error，不渲染任何内容。
	RenderAnswerFile(ctx model.RenderContext, fileName string) (string, error)
}

// NoTaskScript 返回「无任务时」的 iPXE 脚本正文（§4.2、§10 失败模式 1）。
//
// 这是包级函数，不是 Renderer 的方法——它不需要任何上下文（RenderContext
// 要求有 Task/Template/Artifact/Host，而「没有任务」这件事恰恰缺了这些），
// 且必须在「连任务都没有」时可用，这是它存在的全部意义。
//
// 正文三行：shebang、一行 echo、sanboot --no-describe --drive 0x80。那行
// sanboot 是 §10 失败模式 1 的唯一保护——BIOS/MBR 模式下没有 efibootmgr，
// 装完的机器完全依赖它回本地盘；对空盘机器，sanboot 自然失败、效果等同
// 于继续 PXE 轮询（§4.2 的说明）。serial 用于 echo 行里回显机器序列号，
// 便于运维在 nginx access log 之外，从机器自己的控制台看到这行输出。
func NoTaskScript(serial string) string {
	return "#!ipxe\n" +
		"echo lober: no task for " + serial + "\n" +
		"sanboot --no-describe --drive 0x80\n"
}

// LogStore 承接「日志正文不入库」（§2.5）：按任务与阶段落文件，读尾部供
// 失败事件与 UI 展示。
type LogStore interface {
	// AppendLog 把一段日志文本追加到该任务/阶段对应的日志文件。
	AppendLog(ctx context.Context, taskID string, phase model.Phase, content string) error

	// TailLog 读取该任务/阶段日志文件的最后 maxBytes 字节，供 §3.5 的
	// log_tail 字段使用。文件不存在时返回空字符串、不返回错误。
	TailLog(ctx context.Context, taskID string, phase model.Phase, maxBytes int) (string, error)
}

// AuditLogger 记人面动作（§9.4）。与任务事件（TaskEvent）分开：审计对象不
// 止任务（还有 hook 审批、provider 配置），且必须记操作人。
type AuditLogger interface {
	// Log 记一条审计日志。actor 是操作人标识，action 是动作名
	// （如 "task.create"、"hook.approve"），target 是被操作对象的标识
	// （如 task id、hook id），detail 是补充说明。
	Log(ctx context.Context, actor, action, target, detail string) error
}

// LifecycleEvent 是 EventBus 传递的生命周期通知种类，对应 §7.5 的四个
// 生命周期点。
type LifecycleEvent string

const (
	LifecycleEventTaskCreated  LifecycleEvent = "task.created"
	LifecycleEventPhaseChanged LifecycleEvent = "phase.changed"
	LifecycleEventTaskReady    LifecycleEvent = "task.ready"
	LifecycleEventTaskFailed   LifecycleEvent = "task.failed"
)

// EventBus 是生命周期通知总线（§7.5）。它是 fire-and-forget 的：Publish
// 不返回错误，订阅回调也不返回错误。
//
// 因此 EventBus 只能承载「失败了也不影响任务状态」的扩展点，即 §7.4 里
// 只告警、只记录的那几个（DNS、Notify、Heartbeat）。
//
// IPAM / TemplateResolver / DiskPlanner 这类阻塞式 provider 绝不能挂在
// EventBus 上——它们的失败语义是阻塞建任务（§7.4：IP 没申请到不能装），
// 而 task.created 事件发出时任务已经落库了，那时再报错为时已晚。这类
// provider 走 TaskParamsResolver，在写库之前同步调用。
type EventBus interface {
	// Publish 发布一个生命周期事件，task 是事件关联的任务快照。
	// 不返回错误——订阅者的失败不应影响发布方。
	Publish(event LifecycleEvent, task model.Task)

	// Subscribe 注册一个回调，收到 Publish 的事件时被调用。回调不返回
	// 错误；回调内部的失败（如 DNS 注册失败）由回调自己决定如何处理
	// （记录日志、告警），不能以任何方式让 Publish 的调用方感知到。
	Subscribe(event LifecycleEvent, callback func(model.Task))
}

// TaskParamsResolver 是建任务路径上的同步决策接口，由 M7 实现、M6 在写库
// 之前调用（计划任务 11）。它的存在理由就是 EventBus 做不到阻塞：
// IPAM/TemplateResolver/DiskPlanner 的失败必须阻塞建任务本身。
//
// 返回错误即建任务失败，错误直接呈现给运维。返回的参数成为任务的
// resolved_params 并被固化（§2.6：决策要固化，IO 可以实时）——之后不再
// 重算，因此 provider 热更新不会让同一任务前后决策不一致。
type TaskParamsResolver interface {
	// Resolve 依据表单数据与已知的硬件事实，产出该任务的最终决策参数
	// （FormData 与 provider 决策结果与系统生成值的合并）。
	Resolve(ctx context.Context, formData map[string]string, facts map[string]string) (resolvedParams map[string]string, err error)
}
