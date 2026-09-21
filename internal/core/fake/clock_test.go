// 本文件锁住 Clock 可控时钟的行为：现在、前进一段、设为某时刻，以及并发安全。
// 对应实现计划任务 15，依据 contract.Clock 的存在理由——超时逻辑必须可测
// （分阶段超时与 token TTL 都需要在测试里瞬时推进时间，60 分钟的 packages
// 预算没法真等）。
package fake

import (
	"sync"
	"testing"
	"time"
)

// TestNewClock_固定起点 锁住「固定起点的构造函数」这条要求：调用方不需要
// 每个测试各写一个时间字面量，NewClock() 必须给出一个确定、可预测的起点，
// 否则每个用到它的测试都要自己拼一个 time.Date，且互相之间的起点会不一致。
func TestNewClock_固定起点(t *testing.T) {
	c1 := NewClock()
	c2 := NewClock()

	if c1.Now() != c2.Now() {
		t.Fatalf("NewClock() 起点不固定：c1=%d c2=%d，"+
			"两次构造应给出同一个确定起点，否则测试之间的时间基准会漂移", c1.Now(), c2.Now())
	}

	// 起点必须是一个确定的、非零值的时刻——Unix 秒的 0 值对应 1970 年，
	// 会让「距今多久」之类的计算（SPEC §3.5 last_signal_at）在测试里
	// 产出荒谬的巨大时长。
	if c1.Now() == 0 {
		t.Fatal("NewClock() 起点是 Unix 0（1970-01-01），会让 duration 计算产出荒谬结果")
	}
}

// TestClock_Now_初始值等于构造起点 锁住「现在」操作：未做任何 Advance/Set
// 之前，Now() 必须原样返回构造时设定的起点，不能有隐式的时间流逝。
func TestClock_Now_初始值等于构造起点(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewClockAt(start)

	want := start.Unix()
	if got := c.Now(); got != want {
		t.Fatalf("NewClockAt(%v).Now() = %d，want %d；"+
			"可控时钟不能有隐式流逝，否则超时测试里的时间基准不可预测", start, got, want)
	}
}

// TestClock_Advance_前进一段 锁住「前进一段」操作：这是分阶段超时测试的核心
// 依赖——SPEC §3.3 的 60 分钟 packages 预算不能真等，必须能瞬时推进。
func TestClock_Advance_前进一段(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewClockAt(start)

	c.Advance(90 * time.Minute)

	want := start.Add(90 * time.Minute).Unix()
	if got := c.Now(); got != want {
		t.Fatalf("Advance(90min) 后 Now() = %d，want %d；"+
			"§4.4 装机 token TTL 90 分钟的测试依赖这个操作精确推进，"+
			"推进量不对会让 token 过期判定的边界测试失真", got, want)
	}

	// 连续多次 Advance 必须累加，不是覆盖——否则测试里模拟「先过 pre 预算、
	// 再过 packages 预算」这种分阶段场景时，第二次 Advance 会把第一次的
	// 效果吞掉。
	c.Advance(30 * time.Minute)
	want = start.Add(90 * time.Minute).Add(30 * time.Minute).Unix()
	if got := c.Now(); got != want {
		t.Fatalf("连续两次 Advance 后 Now() = %d，want %d；"+
			"Advance 必须是累加语义而非覆盖，否则分阶段超时的多步场景测不出来", got, want)
	}
}

// TestClock_Advance_负值与零值 锁住 Advance 接受负值（回退）与零值（无操作）
// 两种边界——契约文本只说「前进一段」，但没有理由把类型收窄到只接受正数，
// 而状态机测试里「验证还没超时」的场景需要 Advance(0) 或极小值。
func TestClock_Advance_负值与零值(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewClockAt(start)

	c.Advance(0)
	if got, want := c.Now(), start.Unix(); got != want {
		t.Fatalf("Advance(0) 后 Now() = %d，want %d（不变）", got, want)
	}

	c.Advance(-1 * time.Hour)
	want := start.Add(-1 * time.Hour).Unix()
	if got := c.Now(); got != want {
		t.Fatalf("Advance(-1h) 后 Now() = %d，want %d；"+
			"负值应等价于时间回退，Advance 不应假设参数非负", got, want)
	}
}

// TestClock_SetTime_设为某时刻 锁住「设为某时刻」操作：与 Advance 的相对推进
// 不同，这是绝对赋值，用于测试里直接跳到某个具体时间点（例如跳到某天的
// 凌晨验证跨日边界），而不必先算出与当前时刻的差值再 Advance。
func TestClock_SetTime_设为某时刻(t *testing.T) {
	c := NewClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	target := time.Date(2030, 6, 15, 12, 30, 0, 0, time.UTC)
	c.SetTime(target)

	want := target.Unix()
	if got := c.Now(); got != want {
		t.Fatalf("SetTime(%v) 后 Now() = %d，want %d；"+
			"SetTime 是绝对赋值，不应与 Advance 的历史推进量叠加", target, got, want)
	}

	// SetTime 之后再 Advance，必须以新设的时刻为基准，而不是旧的内部状态。
	c.Advance(1 * time.Hour)
	want = target.Add(1 * time.Hour).Unix()
	if got := c.Now(); got != want {
		t.Fatalf("SetTime 后再 Advance(1h)：Now() = %d，want %d；"+
			"Advance 必须以 SetTime 设定的新基准为起点", got, want)
	}
}

// TestClock_并发安全 锁住计划任务 15 明确要求的「并发安全」——状态机测试会
// 并发用它（多个 goroutine 同时驱动不同任务的超时扫描）。用 -race 跑此测试
// 才能真正验证；不加锁的实现在 -race 下会直接报数据竞争。
func TestClock_并发安全(t *testing.T) {
	c := NewClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				c.Advance(time.Second)
				_ = c.Now()
			}
		}()
	}
	wg.Wait()

	// 每个 goroutine 累计推进 opsPerGoroutine 秒，共 goroutines 个并发者，
	// 只要没有数据竞争丢更新，总推进量必须精确等于 goroutines*opsPerGoroutine 秒。
	// 这条断言本身也是并发正确性的证据：如果 Advance 内部有竞态导致更新丢失，
	// 总量会小于期望值。
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	want := start.Add(time.Duration(goroutines*opsPerGoroutine) * time.Second).Unix()
	if got := c.Now(); got != want {
		t.Fatalf("50 个 goroutine 各并发 Advance(1s) 100 次后 Now() = %d，want %d；"+
			"总推进量少于期望说明存在丢更新的数据竞争——"+
			"状态机测试并发驱动超时扫描时会依赖这个不变量", got, want)
	}
}
