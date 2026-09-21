// 本文件实现内置 Notify（记结构化日志，是 MVP 的告警出口）与告警内容装饰
// 器 AlertingNotify。对应实现计划 phase4-01 任务 5，依据 SPEC §3.4、§3.5、
// §7.1、§7.4、§7.5。
//
// AlertingNotify 是一个 contract.NotifyProviderV1 装饰器，包住任意底层
// Notify 实现（内置的 builtinNotify，或将来接入的真实告警系统），只做
// 两件事：
//  1. 把 §3.5 要求的诊断字段（phase/rc/hook_digest/log_tail/syslog_tail/
//     距上次信号多久）与 §3.4 要求的「不可重装文案与装机失败区分」组装
//     进一条可读的消息文案，写入转发给底层的 LifecyclePayload.Detail
//     ["message"]；
//  2. 对同一任务的同一次失败做去重，避免扫描循环反复触发同一条告警
//     （计划任务 5「一条降噪要求」）。
//
// 为什么不直接改 builtinNotify：真实生产环境会换成 P2-03 的 http 加载器
// 接的外部告警系统（计划任务 5「用 P2-03 的 http 加载器接」），
// AlertingNotify 与具体的告警传输方式解耦——它只负责把内容做对，传输层
// 是任何满足 contract.NotifyProviderV1 的实现，装配点用
// NewAlertingNotify(真实实现) 包一层即可获得诊断字段与降噪能力，不需要
// 每接一个新的告警后端都重新实现这两件事。
//
// 契约缺口：真正把 AlertingNotify 接到「任务失败」这个生命周期事件上，
// 需要在 internal/provider/lifecycle.go 的 onTaskFailed 里把
// task.FailureDetail 的六个字段展开进 Detail map（目前 onTaskFailed 只
// 塞了 state 与 failure_reason 两个键，见该文件），以及在装配点
// （cmd/lober）用 NewAlertingNotify 包住真正下发给 RegisterLifecycleHooks
// 的 Notify 实例——但 lifecycle.go 与 cmd/lober 都不在本文件的归属范围
// （本轮文件分区只包含 notify.go），因此本文件只提供 AlertingNotify 本身
// 与它期望的 Detail 键约定（见 buildMessage 文档），实际接线需要由拥有
// 那两处文件的改动方完成，见本次任务返回值的 contractGaps 与 unfinished
// 说明。
package provider

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// builtinNotify 是 NotifyProviderV1 的内置实现：记结构化日志。这是 MVP
// 的告警出口——日志里含任务 id、状态、失败原因，便于运维用 journalctl
// 过滤（任务 5 明确要求）。
type builtinNotify struct{}

// NewBuiltinNotify 构造一个内置 Notify。
func NewBuiltinNotify() contract.NotifyProviderV1 {
	return &builtinNotify{}
}

// Emit 记一行结构化日志。字段顺序固定（event、task_id、detail 逐个展开），
// 便于运维写 journalctl 的 grep 表达式时有稳定的字段位置可依赖。
func (b *builtinNotify) Emit(ctx context.Context, ev contract.LifecyclePayload) error {
	log.Printf("provider/notify: event=%s task_id=%s detail=%v", ev.Event, ev.TaskID, ev.Detail)
	return nil
}

// Ping 直接返回成功。
func (b *builtinNotify) Ping(ctx context.Context) error {
	return nil
}

// 编译期断言：builtinNotify 必须满足 contract.NotifyProviderV1。
var _ contract.NotifyProviderV1 = (*builtinNotify)(nil)

// nonReinstallableReasons 是「机器已装好，重装会擦盘」的三种失败原因
// （§3.4）。本包不复用 model.FailureReason.Reinstallable()（虽然它已经是
// 权威判据）的原因很直接：Reinstallable() 的输入是 model.FailureReason
// 类型，而 AlertingNotify 拿到的是 contract.LifecyclePayload.Detail 里的
// 字符串值（来自跨模块边界传递的 map[string]string，见本文件顶部契约
// 缺口说明）——这里做一次字符串到 model.FailureReason 的转换后仍然调用
// Reinstallable()，不重新定义判据本身，只是把「从哪里取这个字符串」的
// 转换写在这个包边界上。
func isNonReinstallableFailure(failureReason string) bool {
	if failureReason == "" {
		return false
	}
	return !model.FailureReason(failureReason).Reinstallable()
}

// diagnosticDetailKeys 是 buildMessage 期望在 LifecyclePayload.Detail 里
// 找到的诊断字段键名，对应 SPEC §3.5 的六项（state/failure_reason 是既有
// 两项，phase/rc/hook_digest/log_tail/syslog_tail/since_last_signal 是
// 本任务新增读取的四项加一项）。键缺失时对应字段在消息里被省略，不是
// 错误——上游（lifecycle.go）目前还没有把 task.FailureDetail 展开进
// Detail（见本文件顶部契约缺口说明），因此在那处改动落地之前，本函数
// 必须能在只有 state/failure_reason 两个键的输入下也产出一条不出错、
// 有意义的消息。
const (
	detailKeyState           = "state"
	detailKeyFailureReason   = "failure_reason"
	detailKeyPhase           = "phase"
	detailKeyExitCode        = "rc"
	detailKeyHookDigest      = "hook_digest"
	detailKeyLogTail         = "log_tail"
	detailKeySyslogTail      = "syslog_tail"
	detailKeySinceLastSignal = "since_last_signal"
	detailKeyMessage         = "message" // buildMessage 的输出写入这个键
)

// doNotReinstallWarning 是不可重装失败告警文案里必须出现的明确提示
// （§3.4：「该机器系统已安装完成，请勿重装，需人工登录处理」），与
// reinstallableFailureLabel 的措辞刻意不同，避免运维用同一套心智模型
// 处理两类完全不同的失败。
const doNotReinstallWarning = "该机器系统已安装完成，请勿重装，需人工登录处理"

// buildMessage 把 LifecyclePayload 组装成一条给运维看的可读消息，含
// §3.5 的诊断字段与 §3.4 的不可重装警示。
//
// event 决定消息的前缀措辞：task.failed 且失败原因不可重装时，用
// 「该机器系统已安装完成，请勿重装」而不是「装机失败」——§3.4 明确要求
// 两者的告警文案必须能被一眼区分，因为处理方式完全相反（前者禁止走
// retry 端点，后者可以）。
func buildMessage(event contract.LifecycleEvent, detail map[string]string) string {
	var b strings.Builder

	failureReason := detail[detailKeyFailureReason]

	switch {
	case event == contract.LifecycleEventTaskFailed && isNonReinstallableFailure(failureReason):
		// 不可重装失败：先给出与「装机失败」明确区分的警示句，再附诊断
		// 字段——警示句必须在最前面，运维扫一眼消息开头就要能做出正确
		// 判断，不能要求先读完整段诊断信息才发现「这条不能点重试」。
		b.WriteString(doNotReinstallWarning)
	case event == contract.LifecycleEventTaskFailed:
		b.WriteString("任务失败")
	default:
		b.WriteString(fmt.Sprintf("生命周期事件: %s", event))
	}

	appendDetailField(&b, "phase", detail[detailKeyPhase])
	appendDetailField(&b, "failure_reason", failureReason)
	appendDetailField(&b, "rc", detail[detailKeyExitCode])
	appendDetailField(&b, "hook_digest", detail[detailKeyHookDigest])
	appendDetailField(&b, "log_tail", detail[detailKeyLogTail])
	appendDetailField(&b, "syslog_tail", detail[detailKeySyslogTail])
	appendDetailField(&b, "距上次信号", detail[detailKeySinceLastSignal])

	return b.String()
}

// appendDetailField 在消息里追加一个「label=value」片段，value 为空时
// 跳过——避免消息里出现一长串 "rc= hook_digest= log_tail=" 这类空字段
// 噪声（上游尚未展开全部诊断字段时的常见情形，见本文件顶部契约缺口）。
func appendDetailField(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, " %s=%s", label, value)
}

// dedupKey 是 AlertingNotify 降噪判定用的键：任务 ID 加失败原因（若无
// 失败原因则用状态代替）。
//
// 只用「任务 ID + 失败原因」而不加时间窗口：计划任务 5 的降噪要求是
// 「告警在状态转换到 failed 的那一刻发一次，不在扫描时发」——若上游
// 正确实现（只在真正的状态转换时调用一次 Emit），本装饰器的去重根本
// 不会被触发；去重存在的意义是给「上游实现不当、对同一失败反复调用
// Emit」这种情况上一道安全网，而不是本装饰器自己维护一个定时降噪窗口
// （那会需要注入 Clock，超出「只加告警接入相关内容」的范围）。
//
// 键里包含失败原因（不是简单的「同任务只告警一次」）：同一任务重试后
// 若再次失败（哪怑是同一个失败原因），必须重新告警——用一次
// task.ready/task.phase_changed 之类的非失败事件作为「周期已翻转」的
// 信号，清空该任务的去重记录（见 Emit 的实现）。
func dedupKey(taskID, failureReason string) string {
	return taskID + "\x00" + failureReason
}

// AlertingNotify 是 contract.NotifyProviderV1 的装饰器：组装告警内容
// （§3.4、§3.5）并对同一任务的同一失败去重（计划任务 5）。见本文件顶部
// 的类型级文档。
type AlertingNotify struct {
	inner contract.NotifyProviderV1

	mu      sync.Mutex
	alerted map[string]bool // key: dedupKey(taskID, failureReason)
}

// NewAlertingNotify 用 inner 作为实际的告警传输实现构造一个 AlertingNotify。
func NewAlertingNotify(inner contract.NotifyProviderV1) *AlertingNotify {
	return &AlertingNotify{inner: inner, alerted: make(map[string]bool)}
}

// Emit 组装消息、执行去重判定，仅在「未被去重跳过」时才真正调用底层
// Notify。
//
// 非 task.failed 事件（task.ready、phase.changed 等）不参与去重——它们
// 本身就是低频、每次都有意义的事件（计划任务 5 表格：任务就绪等级
// 「低」，与失败告警的降噪诉求不同），同时这类事件会清空该任务此前记录
// 的失败去重键：这是任务重试周期翻转的信号，下一次失败（即使失败原因
// 相同）必须重新告警。
func (a *AlertingNotify) Emit(ctx context.Context, ev contract.LifecyclePayload) error {
	failureReason := ev.Detail[detailKeyFailureReason]

	if ev.Event != contract.LifecycleEventTaskFailed {
		a.clearDedup(ev.TaskID)
	} else {
		key := dedupKey(ev.TaskID, failureReason)
		if a.markAlerted(key) {
			// 已经为这个任务的这次失败告警过，跳过——不调用底层 Notify，
			// 也不返回错误（这不是失败，是正确的降噪行为）。
			return nil
		}
	}

	message := buildMessage(ev.Event, ev.Detail)

	forwardDetail := make(map[string]string, len(ev.Detail)+1)
	for k, v := range ev.Detail {
		forwardDetail[k] = v
	}
	forwardDetail[detailKeyMessage] = message

	return a.inner.Emit(ctx, contract.LifecyclePayload{
		Event:  ev.Event,
		TaskID: ev.TaskID,
		Detail: forwardDetail,
	})
}

// markAlerted 原子地检查并标记「已告警」，返回值为 true 表示这个键此前
// 已经被标记过（本次应该跳过）。
func (a *AlertingNotify) markAlerted(key string) (alreadyAlerted bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.alerted[key] {
		return true
	}
	a.alerted[key] = true
	return false
}

// clearDedup 清空该任务此前记录的全部失败去重键——用任务 ID 作为前缀
// 匹配（dedupKey 用 "\x00" 分隔任务 ID 与失败原因，任务 ID 本身不含
// "\x00"，因此前缀匹配不会误伤其它任务）。
//
// 为什么要整体清空而不是精确删除「当前失败原因」那一个键：调用本方法
// 的场景是任务翻到了非失败事件（ready/phase_changed），意味着这个任务
// 已经离开了「失败态」，此前记录的任何失败原因对它都不再有意义——若只
// 精确匹配却漏了某个历史失败原因，会遗留一个永远不会被再次命中、纯粹
// 占内存的僵尸键。
func (a *AlertingNotify) clearDedup(taskID string) {
	prefix := taskID + "\x00"
	a.mu.Lock()
	defer a.mu.Unlock()
	for key := range a.alerted {
		if strings.HasPrefix(key, prefix) {
			delete(a.alerted, key)
		}
	}
}

// Ping 透传给底层 Notify。
func (a *AlertingNotify) Ping(ctx context.Context) error {
	return a.inner.Ping(ctx)
}

// 编译期断言：AlertingNotify 必须满足 contract.NotifyProviderV1。
var _ contract.NotifyProviderV1 = (*AlertingNotify)(nil)
