// 本文件测试 ReloadableLoader：Swap 后新调用立即用上新实现、in-flight
// 调用不受影响、nil 校验、并发安全（配合 go test -race 跑）。
package loader

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeCaller 是本文件测试用的 Caller 假实现：每次 Call 记录被调用次数，
// 返回构造时指定的固定错误（nil 表示总是成功）。
type fakeCaller struct {
	name   string
	err    error
	calls  int
	mu     sync.Mutex
	onCall func() // 可选，Call 执行期间的回调，供构造"调用中"的时序窗口
}

func (f *fakeCaller) Call(ctx context.Context, method string, params any, result any) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.onCall != nil {
		f.onCall()
	}
	return f.err
}

func (f *fakeCaller) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestReloadableLoader_CallUsesCurrentCaller 验证初始状态下 Call 转发给
// 构造时传入的初始 Caller。
func TestReloadableLoader_CallUsesCurrentCaller(t *testing.T) {
	initial := &fakeCaller{name: "initial"}
	r, err := NewReloadableLoader(initial)
	if err != nil {
		t.Fatalf("NewReloadableLoader() 失败: %v", err)
	}

	if err := r.Call(context.Background(), "dns.register", nil, nil); err != nil {
		t.Fatalf("Call() 失败: %v", err)
	}
	if initial.callCount() != 1 {
		t.Errorf("initial.callCount() = %d, want 1", initial.callCount())
	}
}

// TestReloadableLoader_SwapTakesEffectImmediately 验证 Swap 后，下一次
// Call 立即使用新的 Caller，旧 Caller 不再被调用。
func TestReloadableLoader_SwapTakesEffectImmediately(t *testing.T) {
	oldCaller := &fakeCaller{name: "old"}
	newCaller := &fakeCaller{name: "new"}

	r, err := NewReloadableLoader(oldCaller)
	if err != nil {
		t.Fatalf("NewReloadableLoader() 失败: %v", err)
	}

	if err := r.Call(context.Background(), "m", nil, nil); err != nil {
		t.Fatalf("Call() 失败: %v", err)
	}

	previous, err := r.Swap(newCaller)
	if err != nil {
		t.Fatalf("Swap() 失败: %v", err)
	}
	if previous != oldCaller {
		t.Error("Swap() 返回的 previous 不是替换前的旧 Caller")
	}

	if err := r.Call(context.Background(), "m", nil, nil); err != nil {
		t.Fatalf("Call() 失败: %v", err)
	}

	if oldCaller.callCount() != 1 {
		t.Errorf("oldCaller.callCount() = %d, want 1 —— Swap 之后不应该再被调用", oldCaller.callCount())
	}
	if newCaller.callCount() != 1 {
		t.Errorf("newCaller.callCount() = %d, want 1 —— Swap 之后的调用应该立即生效", newCaller.callCount())
	}
}

// TestReloadableLoader_InFlightCallNotAffectedBySwap 验证一个正在执行中
// 的 Call（已经拿到旧 Caller 的引用）不会因为期间发生的 Swap 而改变行为
// ——它应该跑完这一次对旧 Caller 的调用，不是被替换成新 Caller。
func TestReloadableLoader_InFlightCallNotAffectedBySwap(t *testing.T) {
	callStarted := make(chan struct{})
	releaseCall := make(chan struct{})

	oldCaller := &fakeCaller{
		name: "old",
		onCall: func() {
			close(callStarted)
			<-releaseCall
		},
	}
	newCaller := &fakeCaller{name: "new"}

	r, err := NewReloadableLoader(oldCaller)
	if err != nil {
		t.Fatalf("NewReloadableLoader() 失败: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var callErr error
	go func() {
		defer wg.Done()
		callErr = r.Call(context.Background(), "m", nil, nil)
	}()

	select {
	case <-callStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("等待 in-flight 调用开始超时")
	}

	// 此时 in-flight 调用已经拿到 oldCaller 的引用，正卡在 onCall 里。
	// 在这个窗口期做 Swap，验证 in-flight 调用不受影响。
	if _, err := r.Swap(newCaller); err != nil {
		t.Fatalf("Swap() 失败: %v", err)
	}

	close(releaseCall) // 放行 in-flight 调用，让它跑完
	wg.Wait()

	if callErr != nil {
		t.Fatalf("in-flight Call() 失败: %v", callErr)
	}
	if oldCaller.callCount() != 1 {
		t.Errorf("oldCaller.callCount() = %d, want 1 —— in-flight 调用应该跑完在 oldCaller 上", oldCaller.callCount())
	}

	// Swap 之后的新调用应该用 newCaller。
	if err := r.Call(context.Background(), "m", nil, nil); err != nil {
		t.Fatalf("Call() 失败: %v", err)
	}
	if newCaller.callCount() != 1 {
		t.Errorf("newCaller.callCount() = %d, want 1", newCaller.callCount())
	}
}

// TestNewReloadableLoader_RejectsNilInitial 验证初始 Caller 为 nil 时
// 构造函数拒绝。
func TestNewReloadableLoader_RejectsNilInitial(t *testing.T) {
	_, err := NewReloadableLoader(nil)
	if err == nil {
		t.Fatal("NewReloadableLoader(nil) 返回 nil error, want 非 nil")
	}
}

// TestReloadableLoader_SwapRejectsNil 验证 Swap(nil) 被拒绝，且拒绝后
// 当前 Caller 保持不变。
func TestReloadableLoader_SwapRejectsNil(t *testing.T) {
	original := &fakeCaller{name: "original"}
	r, err := NewReloadableLoader(original)
	if err != nil {
		t.Fatalf("NewReloadableLoader() 失败: %v", err)
	}

	_, err = r.Swap(nil)
	if err == nil {
		t.Fatal("Swap(nil) 返回 nil error, want 非 nil")
	}

	if err := r.Call(context.Background(), "m", nil, nil); err != nil {
		t.Fatalf("Call() 失败: %v", err)
	}
	if original.callCount() != 1 {
		t.Errorf("original.callCount() = %d, want 1 —— 被拒绝的 Swap 不应该改变当前 Caller", original.callCount())
	}
}

// TestReloadableLoader_ConcurrentCallAndSwap 用并发的 Call 与 Swap
// 混合压测，配合 go test -race 验证没有数据竞争。不断言具体的调用分布
// （那本身是竞态的、不确定的），只断言过程中不发生 panic 或死锁——
// 竞争安全性由 -race 检测器负责发现，本测试提供触发竞争的并发场景。
func TestReloadableLoader_ConcurrentCallAndSwap(t *testing.T) {
	r, err := NewReloadableLoader(&fakeCaller{name: "seed"})
	if err != nil {
		t.Fatalf("NewReloadableLoader() 失败: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 若干个持续发起 Call 的 goroutine。
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = r.Call(context.Background(), "m", nil, nil)
				}
			}
		}()
	}

	// 若干个持续发起 Swap 的 goroutine。
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = r.Swap(&fakeCaller{name: "swapped"})
				}
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
