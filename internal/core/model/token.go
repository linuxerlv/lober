// 本文件定义 token 语义与准入常量。对应实现计划任务 3，依据 SPEC §3.2、§4.4、
// §6.4、§9.1。删掉 armed 之后，token 是唯一的执行许可：机器没有 token 就取不到
// 应答文件、装不了机，这是协议级约束，不依赖任何人工开关。
package model

import "time"

// TokenPurpose 区分两种 token 用途。装机 token 权限覆盖应答文件与全部信号；
// finalize token 写进目标系统，只能发重启后阶段的几个信号（§6.4）。
// Store 的 token 查询以用途为参数，用途不符等同查不到（ErrNotFound）。
type TokenPurpose string

const (
	TokenPurposeInstall  TokenPurpose = "install"
	TokenPurposeFinalize TokenPurpose = "finalize"
)

const (
	// InstallTokenTTL 是装机 token 的有效期。计时起点是首次 /v1/boot 匹配成功
	// 时，不是建任务时——运维可能上午建任务、下午才上电，pending 无 TTL（§3.3），
	// 而 token 的 90 分钟必须是装机窗口本身（§4.4）。
	InstallTokenTTL = 90 * time.Minute

	// FinalizeTokenTTL 是 finalize token 的有效期，与装机 token 的 90 分钟
	// 互不牵连（§6.4）：装机用了 70 分钟、重启又停在 BIOS 数分钟，booted 发出
	// 时装机 token 可能刚好过期，因此重启后阶段需要一个独立的、更长的凭据。
	FinalizeTokenTTL = 24 * time.Hour

	// MaxBootAttempts 是同一任务窗口内菜单最多取用的次数（§9.1 第三道闸门）。
	// 超过后置 failed(attempt_exceeded)，不再签发新 token。
	MaxBootAttempts = 3
)

// answerFileServableStates 是应答文件可被下发的任务状态集合（§4.4）。
//
// 用状态门控而非一次性失效：一次性失效会直接掐死正常重试（installer 可能
// 重复取应答文件，例如 Ubuntu 的 cloud-init 会取 user-data 与 meta-data 两个
// 文件）。这个集合放在 model 里，网关与控制台各自引用同一份，避免各写一套。
var answerFileServableStates = map[TaskState]bool{
	TaskStateBooting:    true,
	TaskStateInstalling: true,
}

// AnswerFileServable 判定该任务状态下是否可以下发应答文件。
func AnswerFileServable(s TaskState) bool {
	return answerFileServableStates[s]
}
