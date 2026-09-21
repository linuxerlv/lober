// 本文件实现 contract.Clock：可控时钟，供测试瞬时推进时间。对应实现计划
// 任务 15。
//
// 存在的唯一理由是超时逻辑必须可测（依据 SPEC §3.3 分阶段超时、§4.4 token
// TTL）：60 分钟的 packages 预算、90 分钟的装机 token TTL，测试里都没法真等，
// 必须能瞬时推进或直接跳到某个时刻。
//
// contract.Clock 只有一个方法 Now() int64（返回 Unix 秒，与 model.Task 里
// 时间字段的单位对齐，见 internal/core/contract/service.go）。Advance 与
// SetTime 是本类型独有的测试控制方法，不属于 contract.Clock 接口本身——
// 生产代码只读时间，只有测试需要拨动它。
package fake

import (
	"sync"
	"time"

	"github.com/lober/lober/internal/core/contract"
)

// fixedStart 是 NewClock() 使用的固定起点：2026-01-01T00:00:00Z 的 Unix 秒。
//
// 选一个固定值而非 time.Now()，是计划任务 15 明确要求的「另给一个固定起点
// 的构造函数，避免每个测试各写一个时间字面量」——如果起点是真实的
// time.Now()，两次测试运行得到的起点不同，任何把起点打进断言消息或日志里
// 的测试都会在不同时间运行时产出不同输出，排查时徒增困扰。
var fixedStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()

// Clock 是可控时钟：状态机测试用它模拟时间流逝，不依赖真实的 time.Sleep。
// 实现 contract.Clock。
//
// 并发安全（计划任务 15 明确要求）：状态机测试会并发调用它——多个 goroutine
// 同时驱动不同任务的超时扫描循环，若无锁保护，-race 会报数据竞争，实际值
// 也可能因丢更新而不可预测。
type Clock struct {
	mu  sync.Mutex
	now int64 // Unix 秒，与 contract.Clock.Now() 的返回单位一致
}

// 编译期断言：*Clock 必须满足 contract.Clock。
var _ contract.Clock = (*Clock)(nil)

// NewClock 返回一个以固定起点（2026-01-01T00:00:00Z）初始化的 Clock。
//
// 用固定起点而非 time.Now()：避免每个测试各写一个时间字面量，且让测试的
// 时间基准与运行时刻无关、可重复。
func NewClock() *Clock {
	return &Clock{now: fixedStart}
}

// NewClockAt 返回一个以指定时刻初始化的 Clock，供需要特定起点的测试使用
// （例如验证跨日、跨月边界的场景）。
func NewClockAt(start time.Time) *Clock {
	return &Clock{now: start.Unix()}
}

// Now 返回时钟当前记录的时刻，Unix 秒。
func (c *Clock) Now() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance 把时钟向前推进 d（d 为负值时等价于回退）。
//
// 累加语义而非覆盖：连续多次 Advance 的效果必须叠加，否则模拟「先过 pre
// 预算、再过 packages 预算」这类分阶段超时场景时，后一次调用会把前一次的
// 效果吞掉（SPEC §3.3）。
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now += int64(d / time.Second)
}

// SetTime 把时钟直接设为指定时刻（绝对赋值），与 Advance 的相对推进不同。
//
// 用于测试里需要跳到某个具体时间点的场景（例如验证跨日边界），而不必先
// 算出与当前时刻的差值再调用 Advance。SetTime 之后再调用 Advance，以新设的
// 时刻为基准。
func (c *Clock) SetTime(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t.Unix()
}
