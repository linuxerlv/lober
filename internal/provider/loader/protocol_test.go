// 本文件测试 callWithRetry 的重试与退避行为，以及 Config.withDefaults 的
// 默认值填充。exec.go/http.go 各自的传输层测试见对应测试文件，本文件只
// 覆盖两者共用的重试逻辑，避免同一段行为在多个测试文件里重复断言。
package loader

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestCallWithRetry_SucceedsFirstTry 验证首次尝试成功时不重试、不等待。
func TestCallWithRetry_SucceedsFirstTry(t *testing.T) {
	calls := 0
	err := callWithRetry(context.Background(), Config{MaxRetries: 3, RetryBackoff: time.Millisecond},
		func(ctx context.Context) error {
			calls++
			return nil
		})
	if err != nil {
		t.Fatalf("callWithRetry() 返回错误 = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("尝试次数 = %d, want 1 —— 首次成功不应该有任何重试", calls)
	}
}

// TestCallWithRetry_RetriesUntilSuccess 验证失败几次后成功，总尝试次数
// 等于失败次数+1，且不超过 MaxRetries 允许的上限。
func TestCallWithRetry_RetriesUntilSuccess(t *testing.T) {
	calls := 0
	wantSucceedOnAttempt := 3
	err := callWithRetry(context.Background(), Config{MaxRetries: 5, RetryBackoff: time.Millisecond},
		func(ctx context.Context) error {
			calls++
			if calls < wantSucceedOnAttempt {
				return errors.New("模拟失败")
			}
			return nil
		})
	if err != nil {
		t.Fatalf("callWithRetry() 返回错误 = %v, want nil（第 %d 次尝试应该成功）", err, wantSucceedOnAttempt)
	}
	if calls != wantSucceedOnAttempt {
		t.Errorf("尝试次数 = %d, want %d —— 应该在成功的那一次立即停止，不多试", calls, wantSucceedOnAttempt)
	}
}

// TestCallWithRetry_ExhaustsRetries 验证一直失败时，总尝试次数恰好是
// MaxRetries+1（首次 + 重试次数），且返回的错误包裹了最后一次的失败原因。
func TestCallWithRetry_ExhaustsRetries(t *testing.T) {
	calls := 0
	wantErr := errors.New("持续失败")
	err := callWithRetry(context.Background(), Config{MaxRetries: 2, RetryBackoff: time.Millisecond},
		func(ctx context.Context) error {
			calls++
			return wantErr
		})

	if err == nil {
		t.Fatal("callWithRetry() 返回 nil, want 非 nil —— 重试耗尽后必须报错")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("callWithRetry() 错误 = %v，未包裹最后一次失败原因 %v —— "+
			"调用方需要知道具体为什么失败，不能只看到一句「重试耗尽」", err, wantErr)
	}
	if calls != 3 {
		t.Errorf("尝试次数 = %d, want 3（MaxRetries=2，应为首次 1 + 重试 2）", calls)
	}
}

// TestCallWithRetry_BackoffIsExponential 验证两次重试之间的等待时间按
// 指数退避增长（第二次等待明显长于第一次），不精确断言具体毫秒数（计时
// 类断言在 CI 环境下容易因调度抖动误报），只断言「第二段间隔显著大于
// 第一段」这个方向性关系。
func TestCallWithRetry_BackoffIsExponential(t *testing.T) {
	var timestamps []time.Time
	err := callWithRetry(context.Background(),
		Config{MaxRetries: 2, RetryBackoff: 20 * time.Millisecond},
		func(ctx context.Context) error {
			timestamps = append(timestamps, time.Now())
			return errors.New("总是失败，用来测量退避间隔")
		})
	if err == nil {
		t.Fatal("callWithRetry() 返回 nil, want 非 nil")
	}
	if len(timestamps) != 3 {
		t.Fatalf("记录到 %d 次尝试时间戳, want 3", len(timestamps))
	}

	gap1 := timestamps[1].Sub(timestamps[0])
	gap2 := timestamps[2].Sub(timestamps[1])

	// 指数退避：第二段间隔应该约为第一段的 2 倍。用「大于 1.5 倍」而不是
	// 精确的 2 倍断言，容忍调度抖动，但仍然能拦住「完全没有退避增长」
	// 或「退避是线性而不是指数」这类回归。
	if gap2 < gap1+gap1/2 {
		t.Errorf("第二段退避间隔 = %v, 第一段 = %v —— want 第二段显著大于第一段"+
			"（指数退避应该翻倍，不是保持恒定或线性增长）", gap2, gap1)
	}
}

// TestCallWithRetry_ContextCancelledDuringBackoff 验证在退避等待期间
// ctx 被取消时，callWithRetry 立即返回而不是等完剩余的退避时间——重试
// 机制不能变成一个忽略上层取消信号的黑洞（协议层文档明确要求）。
func TestCallWithRetry_ContextCancelledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := callWithRetry(ctx, Config{MaxRetries: 10, RetryBackoff: 5 * time.Second},
		func(ctx context.Context) error {
			return errors.New("总是失败，逼进入退避等待")
		})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("callWithRetry() 返回 nil, want 非 nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("callWithRetry() 错误 = %v，未包裹 context.Canceled", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("callWithRetry() 耗时 %v，远超取消信号发出的时间点——"+
			"说明退避等待没有正确响应 ctx.Done()，仍在傻等 5 秒的退避间隔", elapsed)
	}
}

// TestCallWithRetry_ContextAlreadyCancelled 验证 ctx 在调用前就已取消时，
// 一次尝试都不发起。
func TestCallWithRetry_ContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := callWithRetry(ctx, Config{MaxRetries: 3, RetryBackoff: time.Millisecond},
		func(ctx context.Context) error {
			calls++
			return nil
		})

	if err == nil {
		t.Fatal("callWithRetry() 返回 nil, want 非 nil —— ctx 已取消时不应该假装成功")
	}
	if calls != 0 {
		t.Errorf("尝试次数 = %d, want 0 —— ctx 在调用前已取消，不应该发起任何尝试", calls)
	}
}

// TestConfig_WithDefaults 验证零值字段被填上默认值，非零值字段保留原值，
// BreakerThreshold 的负数（显式禁用熔断）不被当成零值覆盖。
func TestConfig_WithDefaults(t *testing.T) {
	got := Config{}.withDefaults()
	if got.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", got.Timeout, DefaultTimeout)
	}
	if got.RetryBackoff != DefaultRetryBackoff {
		t.Errorf("RetryBackoff = %v, want %v", got.RetryBackoff, DefaultRetryBackoff)
	}
	if got.BreakerThreshold != DefaultBreakerThreshold {
		t.Errorf("BreakerThreshold = %v, want %v", got.BreakerThreshold, DefaultBreakerThreshold)
	}

	custom := Config{Timeout: time.Second, RetryBackoff: time.Second, BreakerThreshold: 7}.withDefaults()
	if custom.Timeout != time.Second || custom.RetryBackoff != time.Second || custom.BreakerThreshold != 7 {
		t.Errorf("withDefaults() 覆盖了显式设置的非零值 = %+v", custom)
	}

	disabled := Config{BreakerThreshold: -1}.withDefaults()
	if disabled.BreakerThreshold != -1 {
		t.Errorf("BreakerThreshold = %d, want -1（显式禁用熔断不应被默认值覆盖）", disabled.BreakerThreshold)
	}
}
