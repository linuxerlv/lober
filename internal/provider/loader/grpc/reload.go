// 本文件实现 gRPC 插件的原子替换与健康闸门（P3-04 第一半任务 2），
// 依据 SPEC §7.3 第一与第三项。
//
// P2-03 已经为 exec/http 做过「原子换」与「健康检查」这两项，但 gRPC 的情况
// 不同：那里每次调用是独立进程，这里有一个长驻进程要替换。于是多出两个问题：
//
//	插件崩溃   go-plugin 会检测到；下一次调用触发重建（见 manager.Call）
//	在途调用   替换前要等在途调用结束（drain.go）
//
// # 顺序是这一层最要紧的东西
//
//	拉起新进程 → 探活 → 切指针 → 排空 → 停旧进程
//
// **顺序错了（先停旧的）会让替换期间的调用全部失败。** 而在气隙环境里 reload
// 往往是为了修一个正在影响装机的问题——替换期的失败会让运维以为修复没生效，
// 于是去改别的地方，把一个问题变成两个。
package grpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// defaultDrainTimeout 是替换时等待在途调用的默认上限。
//
// 30 秒的依据：单次 provider 调用的超时默认 10 秒（loader.DefaultTimeout），
// 加上最多两次重试的退避，一个正常调用最长在 30 秒量级结束。比它短会频繁误杀
// 正常调用，比它长会让 reload 在插件卡住时等太久——而 reload 通常是在修问题。
const defaultDrainTimeout = 30 * time.Second

// defaultProbeTimeout 是新实例探活的超时。
//
// 探活必须是廉价的（见 PluginServer.Ping 的文档：不该依赖外部系统），因此 3 秒
// 足够；给太长会让一个起不来的插件把 reload 拖住。
const defaultProbeTimeout = 3 * time.Second

// pluginInstance 是一个已经拉起并握手完成的插件实例。
//
// 抽成接口是为了让 reload 的顺序逻辑能被单测覆盖而不必真的拉起子进程——那部分
// 逻辑（探活失败保留旧的、切指针的时机、排空与停止的先后）是本文件的全部价值，
// 而它与「进程怎么拉起来」正交。
type pluginInstance interface {
	// Call 与 loader.Caller.Call 同签名。
	Call(ctx context.Context, method string, params any, result any) error

	// ping 探活。
	ping(ctx context.Context) error

	// kill 停止该实例的进程并释放资源。必须幂等。
	kill()
}

// manager 持有当前生效的插件实例，实现 loader.Caller，并提供 reload。
type manager struct {
	// current 是当前生效的实例。用 atomic.Pointer 让 Call 路径无锁——provider
	// 调用在建任务的同步路径上，那里加一把与 reload 共享的锁会让一次 reload
	// 阻塞所有正在进行的建任务。
	current atomic.Pointer[instanceRef]

	// reloadMu 串行化 reload：两次并发 reload 会各自拉起一个新进程、各自切
	// 指针，结果是其中一个进程既不被使用也不被停止（泄漏），而哪一个泄漏取决
	// 于调度顺序。
	reloadMu sync.Mutex

	drainTimeout time.Duration
	probeTimeout time.Duration

	// start 是拉起一个新插件实例的函数，由 New 注入。测试构造的 manager 留
	// nil——那些测试直接调 reloadWith 传自己的 start，不经过 Reload。
	start func() (pluginInstance, error)
}

// instanceRef 把实例与它自己的排空门绑在一起。
//
// 一个实例一个门（而不是 manager 共用一个门）：门是一次性的（见 drainGate 的
// draining 字段说明），复用会出现「排空完成后门又被打开、而那个进程已经停了」
// 这种状态。
type instanceRef struct {
	inst pluginInstance
	gate *drainGate
}

// newManagerWithInstance 用一个已有实例构造 manager。供测试与 New 共用。
func newManagerWithInstance(inst pluginInstance) (*manager, error) {
	if inst == nil {
		return nil, fmt.Errorf("loader/grpc: 初始插件实例不能为 nil")
	}
	m := &manager{
		drainTimeout: defaultDrainTimeout,
		probeTimeout: defaultProbeTimeout,
	}
	m.current.Store(&instanceRef{inst: inst, gate: newDrainGate()})
	return m, nil
}

// Call 实现 loader.Caller：转发给当前实例，并在排空门里登记本次在途调用。
//
// 取指针的方式（先 Load 出 ref 再用它）保证「这一次调用期间用的是同一个实例」
// ——即使 reload 在本次调用执行期间切了指针，已经拿到的 ref 不会变，正确体现
// 「在途调用不受替换影响」。
//
// 排空期间返回 errDraining：调用方据此可以重试（那是一个短暂窗口），与「插件
// 真的坏了」区分开。
func (m *manager) Call(ctx context.Context, method string, params any, result any) error {
	ref := m.current.Load()
	if ref == nil {
		return fmt.Errorf("loader/grpc: 没有可用的插件实例（manager 未初始化）")
	}

	release, err := ref.gate.enter()
	if err != nil {
		return err
	}
	// 必须 defer：错误路径与 panic 路径都要减计数。漏减的后果是排空永远等不到
	// 零，而 reload 永远超时（见 drainGate.enter 的文档）。
	defer release()

	return ref.inst.Call(ctx, method, params, result)
}

// reloadWith 用 start 拉起一个新实例并替换当前实例。
//
// 五步，顺序见文件头。start 失败或探活失败时**保留旧实例并返回错误**——
// §7.3 第三项。
func (m *manager) reloadWith(ctx context.Context, start func() (pluginInstance, error)) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	// 第 1 步：拉起新实例。此时旧实例仍在服务，一个调用都没受影响。
	next, err := start()
	if err != nil {
		return fmt.Errorf("loader/grpc: 拉起新插件实例失败，保留旧实例: %w", err)
	}
	if next == nil {
		return fmt.Errorf("loader/grpc: start 返回了 nil 实例，保留旧实例")
	}

	// 第 2 步：探活。失败即拒绝本次 reload，并把刚拉起的新进程停掉——不停的话
	// 几次失败的 reload 之后会攒出一堆僵尸插件进程。
	probeCtx, cancel := context.WithTimeout(ctx, m.probeTimeout)
	defer cancel()
	if err := next.ping(probeCtx); err != nil {
		next.kill()
		return fmt.Errorf("loader/grpc: 新插件实例探活失败，已停止它并保留旧实例"+
			"（一次配错的 reload 不该让 provider 整体不可用）: %w", err)
	}

	// 第 3 步：切指针。这一刻之后的新调用走新实例；已经在途的调用仍用旧实例。
	previous := m.current.Swap(&instanceRef{inst: next, gate: newDrainGate()})

	// 第 4 步：排空旧实例的在途调用。
	if previous == nil {
		return nil
	}
	drainErr := previous.gate.drain(m.drainTimeout)

	// 第 5 步：停旧实例。**排空超时也要停**（计划任务 3 明确要求）：一个卡住的
	// 调用不该无限期阻塞 reload。但要记一条日志说明有调用被强制中断，那是排查
	// 「为什么某个任务的 provider 调用失败」的线索。
	previous.inst.kill()

	var timeoutErr *DrainTimeoutError
	if errors.As(drainErr, &timeoutErr) {
		slog.Warn("插件替换时排空超时，旧实例已被强制停止",
			slog.Int("in_flight", timeoutErr.InFlight),
			slog.Duration("waited", timeoutErr.Waited),
		)
	}
	// 排空超时不算 reload 失败：新实例已经在服务了，这是一条运维信息而不是
	// 一个要让调用方回滚的错误。
	return nil
}

// resolvePluginBinary 校验插件路径的形态并解析出真实可执行文件。
//
// **要求插件路径是一个符号链接**（§7.3 第一项）：换二进制要原子，做法是版本
// 目录加一个符号链接，reload 时切链接。给一个普通文件意味着更新只能靠覆盖写，
// 而**运行中的进程可能读到半个文件**——那种故障的症状是插件随机启动失败或行为
// 异常，而文件本身看起来是完整的（覆盖已经完成了）。
//
// 这里只做形态检测，不阻止运维用别的办法覆盖写——那不是代码能保证的。检测的
// 价值是让「配了一个普通文件路径」这个常见错误在启动校验时就被指出来，并在错误
// 文案里给出正确形态。
func resolvePluginBinary(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("loader/grpc: 插件路径 %q 不可访问: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("loader/grpc: 插件路径 %q 不是符号链接。"+
			"§7.3 第一项要求原子替换：正确形态是「版本目录 + 一个符号链接」，"+
			"reload 时切链接指向新版本目录，例如\n"+
			"  /etc/lober/providers/ipam/v1/provider\n"+
			"  /etc/lober/providers/ipam/v2/provider\n"+
			"  /etc/lober/providers/ipam/current -> v2/provider   ← 配这个路径\n"+
			"直接配一个普通文件意味着更新只能覆盖写，而运行中的进程可能读到半个文件",
			path)
	}

	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("loader/grpc: 插件符号链接 %q 解析失败"+
			"（指向的目标可能不存在）: %w", path, err)
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("loader/grpc: 插件符号链接 %q 指向的 %q 不可访问: %w",
			path, target, err)
	}
	if targetInfo.IsDir() {
		return "", fmt.Errorf("loader/grpc: 插件符号链接 %q 指向的 %q 是目录，"+
			"应当指向版本目录里的那个可执行文件", path, target)
	}
	return target, nil
}
