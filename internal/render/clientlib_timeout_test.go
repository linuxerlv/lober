// 本文件锁住 run_hook 不硬依赖 timeout(1)。
//
// **anaconda 的 installer 环境里没有 timeout 命令。** 它是 coreutils 的一部分，
// 而 installer ramdisk 是裁剪过的。实测（2026-09-25，QEMU 目标机，hook 脚本
// 已能正确取到并校验）：
//
//	/artifacts/hooks/438ecbbf.../script.sh              200  1876
//	/v1/signal/.../hook-report?rc=127&seq=1&digest=...  200
//
// 服务端落盘的 hook 日志正文只有一行：
//
//	/tmp/ks-script-vpwi0936: line 27: timeout: command not found
//
// rc=127 是「命令未找到」的约定退出码。required=true 的 hook 因此判定失败，
// %pre 段以非零退出，装机中止——而失败信息里只有 rc=127，看不出缺的是
// timeout 而非 hook 自己的问题。
//
// 同一份 clientlib 被六个家族的 %pre / %post / firstboot 段共用，所以这个缺口
// 影响每一次带 hook 的装机；不带 hook 的模板不受影响（run_hook 不会被调用）。
package render

import (
	"os"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestClientLib_RunHookDoesNotRequireTimeoutBinary 锁住 run_hook 在没有
// timeout(1) 的环境里仍能执行 hook。
//
// 判据是「不出现裸的 timeout 调用」：要么完全不用它，要么先探测存在性再决定
// 走哪条路。硬编码 `timeout "$x" bash ...` 在 installer 里必然 127。
func TestClientLib_RunHookDoesNotRequireTimeoutBinary(t *testing.T) {
	lib := ClientLib(clientLibTestCtx())

	// 允许的形态：command -v timeout 探测后分支，或完全不用 timeout。
	// 不允许的形态：无条件直接调用。
	if strings.Contains(lib, `timeout "$timeout_sec" bash`) &&
		!strings.Contains(lib, "command -v timeout") {
		t.Errorf("run_hook 无条件调用 timeout(1)，而 anaconda 的 installer 环境"+
			"没有这个命令（实测 rc=127: timeout: command not found）。要么先用 "+
			"command -v 探测，要么改用别的超时机制。当前 clientlib:\n%s", lib)
	}
}

// TestClientLib_RunHookStillEnforcesTimeoutWhenAvailable 锁住有 timeout 时
// 仍然用它。
//
// 超时保护本身是必要的：hook 是运维写的任意 shell，一个 `while true` 或一次
// 网络长挂就会让 %pre 永久卡住——那时只能等 SPEC §3.3 的阶段预算（pre 是
// 10 分钟）超时，而阶段超时的错误信息指向阶段而非 hook，排查要慢一大截。
// 所以不能为了兼容就把超时整个去掉。
func TestClientLib_RunHookStillEnforcesTimeoutWhenAvailable(t *testing.T) {
	lib := ClientLib(clientLibTestCtx())
	if !strings.Contains(lib, "timeout") {
		t.Error("run_hook 完全不提 timeout——hook 是运维写的任意 shell，" +
			"一个 while true 就会让 pre 段卡到阶段预算耗尽，而阶段超时的错误信息" +
			"指向阶段而非 hook。有 timeout(1) 时应当用它。")
	}
}

// TestClientLib_ExternalCommandsAreDocumented 锁住 clientlib 对外部命令的
// 依赖被写进注释。
//
// 这条不测行为，测的是「下一个人能否知道这段脚本依赖什么」。它跑在四种差异
// 很大的环境里（anaconda installer ramdisk、debian-installer、装好的目标
// 系统、WinPE 之外的 firstboot），每种环境可用的命令集都不同，而缺一个命令的
// 症状统一是 rc=127——不写清依赖，下次还要再实测一轮才能定位。
func TestClientLib_ExternalCommandsAreDocumented(t *testing.T) {
	src := readClientLibSource(t)
	for _, cmd := range []string{"curl", "sha256sum", "logger", "timeout"} {
		if !strings.Contains(src, cmd) {
			t.Errorf("clientlib.go 的注释里没提到对 %s 的依赖——"+
				"缺命令的症状统一是 rc=127，不写清依赖下次还要再实测一轮", cmd)
		}
	}
}

// clientLibTestCtx 造 ClientLib 需要的最小上下文。
func clientLibTestCtx() model.RenderContext {
	return model.RenderContext{
		Task:         model.Task{ID: "task-cl-1"},
		InstallToken: "tok-cl-1",
		Bases: model.BaseURLs{
			GatewayBase:  "http://gw.internal",
			ArtifactBase: "http://ng.internal/artifacts",
		},
	}
}

// readClientLibSource 读 clientlib.go 自身的源码，用于断言注释内容。
func readClientLibSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("clientlib.go")
	if err != nil {
		t.Fatalf("读 clientlib.go 失败: %v", err)
	}
	return string(b)
}
