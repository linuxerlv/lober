package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

func TestNotify_EmitAndPingDoNotError(t *testing.T) {
	n := NewBuiltinNotify()
	err := n.Emit(context.Background(), contract.LifecyclePayload{
		Event:  contract.LifecycleEventTaskReady,
		TaskID: "task-1",
		Detail: map[string]string{"state": "ready"},
	})
	if err != nil {
		t.Fatalf("内置 Notify Emit 不应返回错误: %v", err)
	}
	if err := n.Ping(context.Background()); err != nil {
		t.Fatalf("内置 Notify Ping 不应返回错误: %v", err)
	}
}

// alertingRecordingNotify 是 contract.NotifyProviderV1 的测试替身：记录每次收到的
// LifecyclePayload，供断言告警内容与调用次数。emitErr 非 nil 时 Emit 总是
// 返回该错误——用于验证「告警发送失败不影响任务状态」这条语义只是「本
// 装饰器把错误原样上抛」，不是本装饰器自己吞掉真实的发送失败（那会让运
// 维误以为告警发出去了）。
type alertingRecordingNotify struct {
	payloads []contract.LifecyclePayload
	emitErr  error
}

func (r *alertingRecordingNotify) Emit(ctx context.Context, ev contract.LifecyclePayload) error {
	r.payloads = append(r.payloads, ev)
	return r.emitErr
}

func (r *alertingRecordingNotify) Ping(ctx context.Context) error { return nil }

// TestAlertingNotify_FourEventKindsAllTriggerAlert 验证计划任务 5 表格里
// 的四类事件（任务失败、任务就绪、重启超时/初始化失败、provider 熔断）
// 都会被转发到底层 Notify——AlertingNotify 是一个透传装饰器，不对事件
// 种类做白名单过滤（过滤应该是发布方 Subscribe 哪些事件的职责，不是
// Notify 装饰器的职责）。
func TestAlertingNotify_FourEventKindsAllTriggerAlert(t *testing.T) {
	kinds := []contract.LifecycleEvent{
		contract.LifecycleEventTaskFailed,
		contract.LifecycleEventTaskReady,
		contract.LifecycleEventTaskFailed, // 重启超时/初始化失败也走 task.failed，用 failure_reason 区分
		contract.LifecycleEventPhaseChanged,
	}
	for i, kind := range kinds {
		inner := &alertingRecordingNotify{}
		alerting := NewAlertingNotify(inner)
		taskID := "task-kind-" + string(rune('a'+i))
		err := alerting.Emit(context.Background(), contract.LifecyclePayload{
			Event:  kind,
			TaskID: taskID,
			Detail: map[string]string{"state": "x", "failure_reason": "boot_timeout"},
		})
		if err != nil {
			t.Fatalf("Emit(%s) 返回了错误: %v", kind, err)
		}
		if len(inner.payloads) != 1 {
			t.Errorf("事件 %s：底层 Notify 收到 %d 次调用, want 1", kind, len(inner.payloads))
		}
	}
}

// TestAlertingNotify_NonReinstallableFailureIncludesDoNotReinstallWarning
// 验证 §3.4 的核心要求：不可重装的失败（reboot_timeout / init_timeout /
// init_failed）告警文案必须含「请勿重装」的明确提示，且用词要与「装机
// 失败」区分——运维看到这条告警时，处理方式必须与看到 pre/packages/post
// 失败时完全不同：前者不能点重试，只能人工登录处理。
func TestAlertingNotify_NonReinstallableFailureIncludesDoNotReinstallWarning(t *testing.T) {
	nonReinstallable := []model.FailureReason{
		model.FailureReasonRebootTimeout,
		model.FailureReasonInitTimeout,
		model.FailureReasonInitFailed,
	}
	for _, reason := range nonReinstallable {
		inner := &alertingRecordingNotify{}
		alerting := NewAlertingNotify(inner)
		err := alerting.Emit(context.Background(), contract.LifecyclePayload{
			Event:  contract.LifecycleEventTaskFailed,
			TaskID: "task-noreinstall-" + string(reason),
			Detail: map[string]string{
				"state":          "failed",
				"failure_reason": string(reason),
				"phase":          "rebooting",
			},
		})
		if err != nil {
			t.Fatalf("Emit 返回了错误: %v", err)
		}
		if len(inner.payloads) != 1 {
			t.Fatalf("failure_reason=%s：底层 Notify 应被调用一次, got %d", reason, len(inner.payloads))
		}
		msg := inner.payloads[0].Detail["message"]
		if !strings.Contains(msg, "请勿重装") {
			t.Errorf("failure_reason=%s 的告警文案 = %q，缺少「请勿重装」提示"+
				"（SPEC §3.4：该机器系统已安装完成，重试会擦盘，需人工登录处理）", reason, msg)
		}
		if strings.Contains(msg, "装机失败") {
			t.Errorf("failure_reason=%s 的告警文案 = %q，不应包含「装机失败」这个措辞"+
				"（SPEC §3.4 要求文案必须与「装机失败」明确区分，两者处理方式完全不同）", reason, msg)
		}
	}
}

// TestAlertingNotify_ReinstallableFailureDoesNotWarnAgainstReinstall 验证
// 反例：可重装的失败（如 pre/packages/post 安装中途失败，映射到
// install_phase）不应出现「请勿重装」这类误导性文案——那会让运维以为一个
// 本可以直接点重试的任务不能重试。
func TestAlertingNotify_ReinstallableFailureDoesNotWarnAgainstReinstall(t *testing.T) {
	inner := &alertingRecordingNotify{}
	alerting := NewAlertingNotify(inner)
	err := alerting.Emit(context.Background(), contract.LifecyclePayload{
		Event:  contract.LifecycleEventTaskFailed,
		TaskID: "task-reinstallable",
		Detail: map[string]string{
			"state":          "failed",
			"failure_reason": string(model.FailureReasonInstallPhase),
			"phase":          "packages",
		},
	})
	if err != nil {
		t.Fatalf("Emit 返回了错误: %v", err)
	}
	msg := inner.payloads[0].Detail["message"]
	if strings.Contains(msg, "请勿重装") {
		t.Errorf("可重装失败 install_phase 的告警文案 = %q，不应出现「请勿重装」——"+
			"这会误导运维以为一个可以直接重试的任务不能重试", msg)
	}
}

// TestAlertingNotify_MessageIncludesDiagnosticFields 验证告警内容含
// §3.5 的诊断字段：至少阶段、失败原因、距上次信号多久——「state=failed」
// 本身无用，这些才是运维排查时真正要看的内容。
func TestAlertingNotify_MessageIncludesDiagnosticFields(t *testing.T) {
	inner := &alertingRecordingNotify{}
	alerting := NewAlertingNotify(inner)
	err := alerting.Emit(context.Background(), contract.LifecyclePayload{
		Event:  contract.LifecycleEventTaskFailed,
		TaskID: "task-diag",
		Detail: map[string]string{
			"state":             "failed",
			"failure_reason":    string(model.FailureReasonInstallPhase),
			"phase":             "packages",
			"rc":                "1",
			"hook_digest":       "sha256:abcd",
			"log_tail":          "some log tail content",
			"syslog_tail":       "some syslog content",
			"since_last_signal": "42 秒前",
		},
	})
	if err != nil {
		t.Fatalf("Emit 返回了错误: %v", err)
	}
	msg := inner.payloads[0].Detail["message"]
	for _, want := range []string{"packages", "install_phase", "42 秒前"} {
		if !strings.Contains(msg, want) {
			t.Errorf("告警文案 = %q，缺少诊断字段 %q（SPEC §3.5：phase/failure_reason/"+
				"距上次信号多久是运维排查时真正要看的内容）", msg, want)
		}
	}
}

// TestAlertingNotify_SameFailureNotAlertedTwice 验证降噪要求：同一任务的
// 同一失败不重复告警。扫描循环每 30 秒跑一次，若对同一个失败任务反复调用
// Emit（模拟实现不当的重复触发），本装饰器必须只让第一次真正送达底层
// Notify，其余的调用被吞掉（返回 nil，不产生第二次底层调用）。
func TestAlertingNotify_SameFailureNotAlertedTwice(t *testing.T) {
	inner := &alertingRecordingNotify{}
	alerting := NewAlertingNotify(inner)
	payload := contract.LifecyclePayload{
		Event:  contract.LifecycleEventTaskFailed,
		TaskID: "task-dedup",
		Detail: map[string]string{
			"state":          "failed",
			"failure_reason": string(model.FailureReasonBootTimeout),
			"phase":          "booting",
		},
	}

	for i := 0; i < 5; i++ {
		if err := alerting.Emit(context.Background(), payload); err != nil {
			t.Fatalf("第 %d 次 Emit 返回了错误: %v", i, err)
		}
	}

	if len(inner.payloads) != 1 {
		t.Errorf("同一任务同一失败原因重复 Emit 5 次，底层 Notify 被调用 %d 次, want 1 —— "+
			"扫描循环每 30 秒跑一次，若不做降噪会对同一个失败任务反复告警", len(inner.payloads))
	}
}

// TestAlertingNotify_DifferentTasksAlertIndependently 验证降噪逻辑的
// 去重键含任务 ID：不同任务即使失败原因相同，也应各自独立告警一次，不能
// 被误判为「同一失败」而漏发。
func TestAlertingNotify_DifferentTasksAlertIndependently(t *testing.T) {
	inner := &alertingRecordingNotify{}
	alerting := NewAlertingNotify(inner)
	detail := map[string]string{"state": "failed", "failure_reason": string(model.FailureReasonBootTimeout)}

	for _, taskID := range []string{"task-a", "task-b", "task-c"} {
		if err := alerting.Emit(context.Background(), contract.LifecyclePayload{
			Event:  contract.LifecycleEventTaskFailed,
			TaskID: taskID,
			Detail: detail,
		}); err != nil {
			t.Fatalf("Emit(%s) 返回了错误: %v", taskID, err)
		}
	}

	if len(inner.payloads) != 3 {
		t.Errorf("三个不同任务各失败一次，底层 Notify 被调用 %d 次, want 3 —— "+
			"去重键必须含任务 ID，不能把不同任务的相同失败原因误判为同一次失败",
			len(inner.payloads))
	}
}

// TestAlertingNotify_NewFailureAfterRetryAlertsAgain 验证降噪不能变成
// 「永久静音」：任务失败告警一次后，若该任务后来被重试（Retry）又再次
// 失败（哪怑是同一个 failure_reason），必须重新告警——不然运维会漏看
// 「重试后又失败了一次」这个信息。用一次 task.ready（模拟重试成功过一次
// 中间状态，代表两次失败之间任务确实发生了变化）加一次新的 task.failed
// 验证。
func TestAlertingNotify_NewFailureAfterRetryAlertsAgain(t *testing.T) {
	inner := &alertingRecordingNotify{}
	alerting := NewAlertingNotify(inner)
	failedDetail := map[string]string{"state": "failed", "failure_reason": string(model.FailureReasonBootTimeout)}

	if err := alerting.Emit(context.Background(), contract.LifecyclePayload{
		Event: contract.LifecycleEventTaskFailed, TaskID: "task-retry-cycle", Detail: failedDetail,
	}); err != nil {
		t.Fatalf("第一次 Emit 返回了错误: %v", err)
	}

	// 中间发生了一次 task.ready（等价于「已经被重试并重新推进」），应清空
	// 该任务的去重记录。
	if err := alerting.Emit(context.Background(), contract.LifecyclePayload{
		Event: contract.LifecycleEventTaskReady, TaskID: "task-retry-cycle", Detail: map[string]string{"state": "done"},
	}); err != nil {
		t.Fatalf("task.ready Emit 返回了错误: %v", err)
	}

	if err := alerting.Emit(context.Background(), contract.LifecyclePayload{
		Event: contract.LifecycleEventTaskFailed, TaskID: "task-retry-cycle", Detail: failedDetail,
	}); err != nil {
		t.Fatalf("第二次（重试后新的）Emit 返回了错误: %v", err)
	}

	failedCount := 0
	for _, p := range inner.payloads {
		if p.Event == contract.LifecycleEventTaskFailed {
			failedCount++
		}
	}
	if failedCount != 2 {
		t.Errorf("重试周期之间出现了一次 task.ready 之后，同任务的第二次 task.failed 应该"+
			"重新告警，底层收到的 task.failed 调用次数 = %d, want 2", failedCount)
	}
}

// TestAlertingNotify_EmitFailureDoesNotSwallowRealError 验证告警发送失败
// 本身要如实上抛（§7.4 的只告警语义是「调用方在拿到错误后不改任务状态」，
// 不是本装饰器把发送失败伪装成成功——那会让运维误以为告警已送达）。
func TestAlertingNotify_EmitFailureDoesNotSwallowRealError(t *testing.T) {
	boom := errors.New("boom: 下游告警系统不可达")
	inner := &alertingRecordingNotify{emitErr: boom}
	alerting := NewAlertingNotify(inner)

	err := alerting.Emit(context.Background(), contract.LifecyclePayload{
		Event:  contract.LifecycleEventTaskFailed,
		TaskID: "task-emit-fail",
		Detail: map[string]string{"state": "failed", "failure_reason": string(model.FailureReasonBootTimeout)},
	})
	if !errors.Is(err, boom) {
		t.Errorf("底层 Notify.Emit 失败时，AlertingNotify.Emit 返回的错误 = %v, want 原样包含 %v"+
			"——不能把真实的发送失败伪装成成功", err, boom)
	}
}

// TestAlertingNotify_PingDelegatesToInner 验证 Ping 直接透传给底层。
func TestAlertingNotify_PingDelegatesToInner(t *testing.T) {
	inner := &alertingRecordingNotify{}
	alerting := NewAlertingNotify(inner)
	if err := alerting.Ping(context.Background()); err != nil {
		t.Fatalf("Ping 不应返回错误: %v", err)
	}
}
