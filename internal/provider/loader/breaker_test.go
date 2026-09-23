// 本文件测试熔断器的状态转换：连续失败达到阈值后打开、成功清零计数、
// 阈值非正数时禁用熔断。exec/http 加载器接入熔断器后的端到端行为
// （「熔断后不再真正发起调用」）在各自的 _test.go 里用真实加载器验证，
// 本文件只测熔断器自身的状态机，不依赖任何传输层。
package loader

import "testing"

// TestCircuitBreaker_OpensAfterThreshold 验证连续失败达到阈值后 Allow
// 返回 false。
func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	b := newCircuitBreaker(3)

	for i := 0; i < 2; i++ {
		if !b.Allow() {
			t.Fatalf("第 %d 次失败前 Allow() = false, want true（未达阈值不应熔断）", i+1)
		}
		b.RecordResult(errFake)
	}

	if !b.Allow() {
		t.Fatal("第 2 次连续失败后 Allow() = false, want true（阈值是 3，还差一次）")
	}
	b.RecordResult(errFake)

	if b.Allow() {
		t.Fatal("第 3 次连续失败后 Allow() = true, want false（应该已经熔断）")
	}
	if !b.isOpen() {
		t.Error("isOpen() = false, want true")
	}
}

// TestCircuitBreaker_SuccessResetsCount 验证熔断打开前，一次成功会清零
// 连续失败计数——之后需要重新连续失败满阈值次才会打开，不是「之前失败
// 过的次数继续累加」。
func TestCircuitBreaker_SuccessResetsCount(t *testing.T) {
	b := newCircuitBreaker(3)

	b.RecordResult(errFake)
	b.RecordResult(errFake)
	b.RecordResult(nil) // 成功，清零

	if !b.Allow() {
		t.Fatal("清零后 Allow() = false, want true")
	}

	// 再失败两次（未达阈值，因为计数已清零）。
	b.RecordResult(errFake)
	b.RecordResult(errFake)
	if !b.Allow() {
		t.Fatal("清零后仅累计 2 次失败，Allow() = false, want true（阈值 3，未达标）")
	}

	// 第三次失败才应该真正达到阈值。
	b.RecordResult(errFake)
	if b.Allow() {
		t.Fatal("清零后累计 3 次失败，Allow() = true, want false（应该熔断）")
	}
}

// TestCircuitBreaker_ThresholdZeroOrNegativeDisablesBreaker 验证阈值
// 非正数时熔断被禁用：无论记录多少次失败，Allow 始终返回 true。
func TestCircuitBreaker_ThresholdZeroOrNegativeDisablesBreaker(t *testing.T) {
	for _, threshold := range []int{0, -1} {
		b := newCircuitBreaker(threshold)
		for i := 0; i < 100; i++ {
			b.RecordResult(errFake)
		}
		if !b.Allow() {
			t.Errorf("threshold=%d 时经过 100 次失败后 Allow() = false, want true（阈值非正数应禁用熔断）", threshold)
		}
	}
}

// errFake 是本文件测试用的哨兵失败错误，不携带任何具体语义，仅用于
// 「非 nil」这个判定条件。
var errFake = fakeError("模拟失败")

type fakeError string

func (e fakeError) Error() string { return string(e) }
