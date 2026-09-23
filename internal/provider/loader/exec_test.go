// 本文件测试 ExecLoader：正常解码、非零退出、超时杀进程、启动校验。
//
// 子进程用的不是 shell 脚本，而是本测试二进制自身的重新执行（标准库
// os/exec 测试的惯用手法：TestHelperProcess 模式）——理由是跨平台确定性：
// Windows 上没有 /bin/sh，若依赖 bash/sleep 之类的 Unix 命令，测试在
// Windows 主机（本任务的实际运行环境）上根本跑不起来。用 os.Args[0]
// （即测试二进制自己）加一个环境变量标记重新执行自己，行为完全由 Go
// 代码决定，不依赖任何外部命令是否存在于 PATH。
package loader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// helperProcessEnv 是标记「本次重新执行是作为测试子进程角色运行」的环境
// 变量名，值决定子进程的具体行为（见 TestHelperProcess 的 switch）。
const helperProcessEnv = "LOBER_LOADER_TEST_HELPER_MODE"

// TestHelperProcess 不是一个真正的测试用例——它是被 execTestHelper 以
// `-test.run=TestHelperProcess` 的方式重新拉起本二进制时，实际执行的
// 「子进程程序」。之所以放在 _test.go 里而不是单独的 testdata 脚本：
// 复用同一份 go test 二进制，不需要在测试运行前额外 go build 一个外部
// 可执行文件，也不需要关心目标平台的可执行文件后缀（.exe）。
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperProcessEnv)
	if mode == "" {
		return // 正常的 go test 执行路径，不是被当作子进程拉起，直接跳过
	}

	switch mode {
	case "echo":
		// 原样读 stdin 的 Request，把 Params 塞进 Response.Result 返回——
		// 用于验证「正常返回 → 结果被正确解码」。
		var req Request
		if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
			fmt.Fprintf(os.Stderr, "解析 stdin 失败: %v", err)
			os.Exit(1)
		}
		resp := Response{Result: req.Params}
		_ = json.NewEncoder(os.Stdout).Encode(resp)
		os.Exit(0)

	case "fail-exit":
		// 非零退出，不产生任何 stdout——验证「子进程非零退出 → 错误」。
		fmt.Fprintln(os.Stderr, "模拟子进程失败")
		os.Exit(1)

	case "fail-business":
		// 退出码 0，但 Response.Error 非空——验证「业务错误也算失败」。
		resp := Response{Error: "模拟的业务错误：外部系统拒绝了请求"}
		_ = json.NewEncoder(os.Stdout).Encode(resp)
		os.Exit(0)

	case "sleep-forever":
		// 一直不退出（直到被杀）——验证「子进程超时 → 被杀掉且报超时」。
		// 用 time.Sleep 一个远超测试超时设置的时长，而不是无限阻塞在
		// select{}，这样如果 Kill 因为某种原因没生效，测试进程本身
		// 也不会真的悬挂到 go test 的全局超时才结束（sleep 结束后子
		// 进程会自然退出，不会无限占用系统资源）。
		time.Sleep(2 * time.Minute)
		os.Exit(0)

	default:
		fmt.Fprintf(os.Stderr, "未知的 helper 模式: %s", mode)
		os.Exit(2)
	}
}

// execTestHelper 构造一个「重新执行本测试二进制、以 helper 模式运行」的
// ExecConfig，供各测试用例指定希望子进程表现出的行为。
func execTestHelper(t *testing.T, mode string) ExecConfig {
	t.Helper()
	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() 失败: %v", err)
	}
	return ExecConfig{
		Command: exePath,
		Args:    []string{"-test.run=^TestHelperProcess$", "-test.v=false"},
	}
}

// withHelperEnv 返回一个已设置 helperProcessEnv 的测试专用环境变量列表，
// 但 exec.Cmd 的 Env 字段由 ExecLoader 内部构造（当前实现未暴露注入
// 环境变量的入口），因此改用进程级 os.Setenv/Unsetenv 在测试期间临时
// 设置——ExecLoader 拉起的子进程会继承调用方（go test 进程）的环境变量，
// 这与真实部署场景一致（子进程本就该继承运行 lober 进程的环境）。
func withHelperEnv(t *testing.T, mode string) {
	t.Helper()
	if err := os.Setenv(helperProcessEnv, mode); err != nil {
		t.Fatalf("设置环境变量失败: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv(helperProcessEnv)
	})
}

// TestExecLoader_DecodesResult 验证子进程正常返回时结果被正确解码。
func TestExecLoader_DecodesResult(t *testing.T) {
	withHelperEnv(t, "echo")
	l, err := NewExecLoader(execTestHelper(t, "echo"), Config{BreakerThreshold: -1})
	if err != nil {
		t.Fatalf("NewExecLoader() 失败: %v", err)
	}

	type payload struct {
		Hostname string `json:"hostname"`
	}
	var result payload
	err = l.Call(context.Background(), "ipam.allocate", payload{Hostname: "node-01"}, &result)
	if err != nil {
		t.Fatalf("Call() 失败: %v", err)
	}
	if result.Hostname != "node-01" {
		t.Errorf("result.Hostname = %q, want %q —— echo 模式应该原样把请求参数回传回来", result.Hostname, "node-01")
	}
}

// TestExecLoader_NonZeroExit 验证子进程非零退出时 Call 返回错误。
func TestExecLoader_NonZeroExit(t *testing.T) {
	withHelperEnv(t, "fail-exit")
	l, err := NewExecLoader(execTestHelper(t, "fail-exit"), Config{BreakerThreshold: -1})
	if err != nil {
		t.Fatalf("NewExecLoader() 失败: %v", err)
	}

	err = l.Call(context.Background(), "dns.register", map[string]string{}, nil)
	if err == nil {
		t.Fatal("Call() 返回 nil, want 非 nil —— 子进程非零退出应该报错")
	}
}

// TestExecLoader_BusinessError 验证子进程退出码 0 但 Response.Error 非空
// 时同样算作失败。
func TestExecLoader_BusinessError(t *testing.T) {
	withHelperEnv(t, "fail-business")
	l, err := NewExecLoader(execTestHelper(t, "fail-business"), Config{BreakerThreshold: -1})
	if err != nil {
		t.Fatalf("NewExecLoader() 失败: %v", err)
	}

	err = l.Call(context.Background(), "dns.register", map[string]string{}, nil)
	if err == nil {
		t.Fatal("Call() 返回 nil, want 非 nil —— Response.Error 非空应该算失败")
	}
	if !strings.Contains(err.Error(), "模拟的业务错误") {
		t.Errorf("Call() 错误 = %v，未包含子进程报告的业务错误文案", err)
	}
}

// TestExecLoader_TimeoutKillsProcess 验证子进程超时后：
//  1. Call 返回的错误能用 errors.Is 判定为 ErrTimeout；
//  2. 子进程真的被杀掉了，不是「Go 侧放弃等待但进程仍在运行」。
//
// 第 2 点的证明方式：ExecLoader 内部用 cmd.Start() + cmd.Wait()（不是
// cmd.Run() 就不管）。Go 标准库 os/exec 的文档保证 Wait() 只有在底层 OS
// 进程真正终止后才会返回——这不是本测试的假设，是 os/exec 包的既定契约。
// 因此 callOnce 返回（进而 Call 返回）这件事本身就是「进程已终止」的
// 直接证据，不需要额外验证手段。用 onProcessStarted 钩子取到 PID 只是
// 为了让失败时的诊断信息更具体（哪个 PID 没有在预期时间内退出），不是
// 判定逻辑本身依赖的信号。
//
// 之所以不用「查询该 PID 是否仍存活」这类看起来更直接的验证：Windows
// 上 Go 标准库的 os.Process.Signal 不支持信号 0 探活（syscall.Signal(0)
// 返回 "not supported by windows"，已用临时程序实测确认），要做到这一点
// 需要引入 golang.org/x/sys/windows 这类不在允许依赖列表内的第三方包，
// 为了一个测试断言引入新依赖不成比例；而 Wait() 语义已经是比「查一次
// 进程表」更强的证据（进程表查询存在 PID 复用的理论竞态窗口，Wait()
// 没有）。
func TestExecLoader_TimeoutKillsProcess(t *testing.T) {
	withHelperEnv(t, "sleep-forever")
	l, err := NewExecLoader(execTestHelper(t, "sleep-forever"),
		Config{Timeout: 200 * time.Millisecond, BreakerThreshold: -1})
	if err != nil {
		t.Fatalf("NewExecLoader() 失败: %v", err)
	}

	var pid int
	l.onProcessStarted = func(p int) { pid = p }

	start := time.Now()
	err = l.Call(context.Background(), "dns.register", map[string]string{}, nil)
	callDuration := time.Since(start)

	if err == nil {
		t.Fatal("Call() 返回 nil, want 非 nil —— 超时应该报错")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("Call() 错误 = %v，未包裹 ErrTimeout", err)
	}
	if pid == 0 {
		t.Fatal("onProcessStarted 钩子未被调用，子进程可能根本没启动成功——测试前提不成立")
	}
	// Call 应该在略高于配置的 200ms 超时后返回，远早于 sleep-forever 的
	// 2 分钟——按上面的文档说明，这个耗时短的事实本身就是 Wait() 已经
	// 返回、进程已终止的直接证据，不是间接推断。
	if callDuration > 5*time.Second {
		t.Fatalf("Call() 耗时 %v（子进程 PID=%d），远超配置的 200ms 超时——"+
			"说明子进程没有被真正杀掉，cmd.Wait() 仍在等待它自然退出（2 分钟的 sleep）",
			callDuration, pid)
	}
}

// TestNewExecLoader_RejectsEmptyCommand 验证配置里选了 exec 但没给
// command 时，构造函数直接拒绝（启动校验，不是运行期才发现）。
func TestNewExecLoader_RejectsEmptyCommand(t *testing.T) {
	_, err := NewExecLoader(ExecConfig{Command: ""}, Config{})
	if err == nil {
		t.Fatal("NewExecLoader() 返回 nil error, want 非 nil —— 空 command 必须在构造期被拒绝")
	}
}

// TestExecLoader_CircuitBreakerStopsCallingProcess 验证熔断打开后，
// 后续调用不再真正拉起子进程——用一个会累加调用次数的计数文件式验证
// 太重，改用更直接的方式：给一个不存在的可执行文件路径（保证每次尝试
// 都会失败在「启动子进程」这一步），熔断阈值设为 2，第三次调用前熔断
// 应该已经打开；用「熔断后的错误文案」而不是「进程是否被启动」来断言
// ——熔断打开后 Call 应该在进入 callWithRetry 之前就直接返回，错误
// 文案里带有"已熔断"字样，与"子进程执行失败"的错误文案不同，可用于
// 区分两种失败原因。
func TestExecLoader_CircuitBreakerStopsCallingProcess(t *testing.T) {
	l, err := NewExecLoader(
		ExecConfig{Command: "lober-loader-test-nonexistent-command-xyz"},
		Config{MaxRetries: 0, RetryBackoff: time.Millisecond, BreakerThreshold: 2})
	if err != nil {
		t.Fatalf("NewExecLoader() 失败: %v", err)
	}

	for i := 0; i < 2; i++ {
		err := l.Call(context.Background(), "dns.register", map[string]string{}, nil)
		if err == nil {
			t.Fatalf("第 %d 次调用 Call() 返回 nil, want 非 nil（命令不存在应该失败）", i+1)
		}
		if strings.Contains(err.Error(), "已熔断") {
			t.Fatalf("第 %d 次调用就已经报熔断，want 前 2 次是真实的子进程启动失败", i+1)
		}
	}

	err = l.Call(context.Background(), "dns.register", map[string]string{}, nil)
	if err == nil {
		t.Fatal("第 3 次调用 Call() 返回 nil, want 非 nil")
	}
	if !strings.Contains(err.Error(), "已熔断") {
		t.Errorf("第 3 次调用错误 = %v，want 包含「已熔断」——阈值 2，前两次失败后应该已经打开", err)
	}
}
