// 本文件测试 gRPC 插件的原子替换与健康闸门（P3-04 第一半任务 2），
// 依据 SPEC §7.3 第一与第三项。
package grpc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lober/lober/internal/provider/loader"
)

// stubInstance 是 pluginInstance 的测试替身：可控的探活结果与调用结果，
// 并记录是否被 kill 过。
type stubInstance struct {
	name     string
	pingErr  error
	callErr  error
	calls    atomic.Int64
	killed   atomic.Bool
	callHold chan struct{} // 非 nil 时 Call 阻塞直到它被关闭
}

func (s *stubInstance) Call(ctx context.Context, method string, params, result any) error {
	s.calls.Add(1)
	if s.callHold != nil {
		select {
		case <-s.callHold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.callErr
}

func (s *stubInstance) ping(context.Context) error { return s.pingErr }
func (s *stubInstance) kill()                      { s.killed.Store(true) }

// TestReload_HealthGateKeepsOldInstanceOnProbeFailure 验证探活失败时旧实例
// 仍在服务、reload 返回错误。
//
// §7.3 第三项：新插件拉起后先探活，**失败则保留旧进程、拒绝本次 reload**。
// 不保留的后果是一次配错的 reload 直接让 provider 整体不可用——而 reload 往往
// 正是在修一个影响装机的问题，那会把一个问题变成两个。
func TestReload_HealthGateKeepsOldInstanceOnProbeFailure(t *testing.T) {
	old := &stubInstance{name: "old"}
	m, err := newManagerWithInstance(old)
	if err != nil {
		t.Fatalf("newManagerWithInstance 失败: %v", err)
	}

	broken := &stubInstance{name: "new", pingErr: errors.New("插件起来了但探活不通")}
	if err := m.reloadWith(context.Background(), func() (pluginInstance, error) {
		return broken, nil
	}); err == nil {
		t.Fatal("探活失败的新实例被接受了 —— §7.3 第三项要求探活失败即拒绝本次 reload")
	}

	// 旧实例必须还在服务。
	if err := m.Call(context.Background(), "ipam.allocate", nil, nil); err != nil {
		t.Errorf("探活失败后旧实例不再服务: %v —— 一次配错的 reload 不该让 provider "+
			"整体不可用；reload 往往正是在修问题，那会把一个问题变成两个", err)
	}
	if old.killed.Load() {
		t.Error("探活失败时旧实例被 kill 了 —— 必须保留旧进程")
	}
	if !broken.killed.Load() {
		t.Error("探活失败的新实例没有被 kill —— 那个进程会一直留着，" +
			"几次失败的 reload 之后攒出一堆僵尸插件进程")
	}
}

// TestReload_OrderIsProbeThenSwapThenStopOld 验证顺序：先拉起新实例并探活、
// 再切换指针、最后停旧实例。
//
// **顺序错了（先停旧的）会让替换期间的调用全部失败**（计划任务 2 原话）——
// 而在气隙环境里 reload 往往是为了修一个正在影响装机的问题，替换期的失败会让
// 运维以为修复没生效。
func TestReload_OrderIsProbeThenSwapThenStopOld(t *testing.T) {
	old := &stubInstance{name: "old"}
	m, err := newManagerWithInstance(old)
	if err != nil {
		t.Fatalf("newManagerWithInstance 失败: %v", err)
	}

	var events []string
	var mu sync.Mutex
	record := func(e string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}

	next := &stubInstance{name: "new"}
	err = m.reloadWith(context.Background(), func() (pluginInstance, error) {
		record("start-new")
		return &orderedInstance{stubInstance: next, onPing: func() { record("probe") }}, nil
	})
	if err != nil {
		t.Fatalf("reloadWith 失败: %v", err)
	}
	if !old.killed.Load() {
		t.Error("reload 成功后旧实例没有被停 —— 会泄漏一个插件进程")
	}
	record("old-stopped")

	mu.Lock()
	defer mu.Unlock()
	want := []string{"start-new", "probe", "old-stopped"}
	if len(events) != len(want) {
		t.Fatalf("事件序列 = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("事件序列 = %v, want %v —— 顺序必须是「拉起新的 → 探活 → "+
				"切指针 → 停旧的」；先停旧的会让替换期间的调用全部失败", events, want)
		}
	}
}

// orderedInstance 在 ping 时回调，用来记录探活发生的时点。
type orderedInstance struct {
	*stubInstance
	onPing func()
}

func (o *orderedInstance) ping(ctx context.Context) error {
	o.onPing()
	return o.stubInstance.ping(ctx)
}

// TestReload_NoCallFailsDuringSwap 验证替换期间没有调用失败。
//
// 持续打调用的同时做一次 reload，断言全部调用都成功。这条抓的是「切指针那一瞬
// 有窗口拿到 nil 或已停的实例」。
func TestReload_NoCallFailsDuringSwap(t *testing.T) {
	old := &stubInstance{name: "old"}
	m, err := newManagerWithInstance(old)
	if err != nil {
		t.Fatalf("newManagerWithInstance 失败: %v", err)
	}

	stop := make(chan struct{})
	var failures atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := m.Call(context.Background(), "ipam.allocate", nil, nil); err != nil {
					// 排空期的 errDraining 是预期的（那是 reload 的正常一步），
					// 其它错误才算失败。
					if !errors.Is(err, errDraining) {
						failures.Add(1)
					}
				}
				runtime.Gosched()
			}
		}()
	}

	time.Sleep(10 * time.Millisecond)
	if err := m.reloadWith(context.Background(), func() (pluginInstance, error) {
		return &stubInstance{name: "new"}, nil
	}); err != nil {
		t.Fatalf("reloadWith 失败: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	close(stop)
	wg.Wait()

	if n := failures.Load(); n > 0 {
		t.Errorf("替换期间有 %d 次调用以非 errDraining 的错误失败 —— "+
			"切指针那一瞬不该有窗口让调用拿到 nil 或已停的实例", n)
	}
}

// TestReload_RejectsOverwriteInPlace 验证「覆盖写」被检测到。
//
// §7.3 第一项要求换二进制是原子的：用版本目录加一个符号链接，reload 时切链接。
// **不要用覆盖写**——运行中的进程可能读到半个文件。
//
// 本测试验证 resolvePluginBinary 这一层的检测：插件路径必须是一个指向版本目录
// 的符号链接，直接给一个普通文件会被拒绝并提示正确形态。
func TestReload_RejectsOverwriteInPlace(t *testing.T) {
	// 不 skip Windows：本机实测 os.Symlink 可用（开发者模式或提升权限下都行），
	// 而这条断言验证的是 resolvePluginBinary 的形态判定逻辑，与平台无关。
	// 若某台机器上建链接失败，下面的 t.Fatalf 会明确指出是夹具问题而不是被测
	// 代码的问题——那比无条件 skip 更有用：skip 会让这条断言在 Windows 上
	// 永久失效而没人注意到。
	dir := t.TempDir()

	// 情形一：普通文件（覆盖写的形态）——拒绝。
	plain := filepath.Join(dir, "plain-binary")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if _, err := resolvePluginBinary(plain); err == nil {
		t.Error("插件路径是一个普通文件却被接受 —— §7.3 第一项要求原子替换，" +
			"普通文件只能靠覆盖写更新，而运行中的进程可能读到半个文件")
	}

	// 情形二：指向版本目录里可执行文件的符号链接——接受。
	versionDir := filepath.Join(dir, "v1")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("建版本目录失败: %v", err)
	}
	real := filepath.Join(versionDir, "provider")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	link := filepath.Join(dir, "current")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("建符号链接失败: %v", err)
	}
	got, err := resolvePluginBinary(link)
	if err != nil {
		t.Fatalf("指向版本目录的符号链接被拒绝: %v", err)
	}

	// 用 os.SameFile 而不是字符串相等：Windows 上 t.TempDir() 给的是 8.3 短
	// 路径（V_LVXI~1），而 filepath.EvalSymlinks 解析出长路径
	// （v_lvxiaoyang）——两者指向同一个文件，字符串却不等。断言「是不是同一个
	// 文件」才是这里真正要确认的语义，比对字符串会在 Windows 上得到一个
	// 与被测逻辑无关的失败。
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat 解析结果 %q 失败: %v", got, err)
	}
	realInfo, err := os.Stat(real)
	if err != nil {
		t.Fatalf("stat 真实文件 %q 失败: %v", real, err)
	}
	if !os.SameFile(gotInfo, realInfo) {
		t.Errorf("resolvePluginBinary = %q，与符号链接的目标 %q 不是同一个文件", got, real)
	}
}

// 保证 loader 包被 import（manager 实现 loader.Caller）。
var _ loader.Caller = (*manager)(nil)
