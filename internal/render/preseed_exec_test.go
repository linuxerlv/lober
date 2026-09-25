// 本文件用真实 bash 子进程执行 lateCommandValue 产出的脚本片段，验证一条
// 只有真实执行才能证实的不变量：**`in-target bash -c '...'` 里的 post
// 阶段 hook 调用，必须在那个 chroot 后的新 bash 进程里就能找到 run_hook/
// signal 函数**——与 kickstart_exec_test.go 验证的是同一类 bug（子进程/
// chroot 边界不共享变量与函数定义），只是载体从 kickstart 的段落独立子
// 进程变成了 in-target 的 chroot 边界。
package render

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeInTargetStub 在 dir 下写一个 in-target 的假实现：真实 in-target
// 是 chroot 包装器（会切换文件系统根），测试环境没有可 chroot 的目标系统，
// 因此这个 stub 只做测试关心的那一件事——把 "in-target bash -c '...'"
// 精确等价地退化为 "bash -c '...'"（即只剥离 in-target 本身，不引入
// chroot 语义），从而验证"传给 in-target 的那段脚本文本本身是否自洽"
// （若脚本文本里缺 run_hook 定义，剥离 chroot 之后跑起来同样会报
// "command not found"；若脚本文本本身自洽，剥离 chroot 后也应该正常
// 执行——chroot 只影响文件系统可见性，不影响"这段 bash 脚本文本本身有没有
// 定义它调用的函数"这件事，因此这个简化是安全的）。
func writeInTargetStub(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "in-target")
	content := "#!/bin/sh\nexec \"$@\"\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("写 in-target stub 失败: %v", err)
	}
}

// TestExec_PreseedInTargetPostHookIsSelfContained 锁住修复后的行为：
// lateCommandValue 里 post 阶段的 hook 调用，包在 `in-target bash -c` 里
// 的那段脚本必须自带 run_hook/signal 定义，不能依赖外层脚本（d-i 环境里
// 定义这两个函数的那部分）——in-target 是 chroot 边界，外层的函数定义
// 在里面不可见。
func TestExec_PreseedInTargetPostHookIsSelfContained(t *testing.T) {
	ctx := fullPreseedCtx()
	late, err := lateCommandValue(ctx)
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}

	// late 的值形如：in-target sh -c 'echo <base64> | base64 -d > ... &&
	// chmod 755 ... && /tmp/lober-late.sh'——本测试关心的是"落地后被执行的
	// 那个脚本文件内容"，把 base64 解出来直接验证，不依赖真实落盘。
	decoded := decodeLateCommandScript(t, late)

	stubDir := t.TempDir()
	writeStub(t, stubDir, "curl")
	writeStub(t, stubDir, "sha256sum")
	writeInTargetStub(t, stubDir)
	stubDirPosix := posixPath(t, stubDir)

	stderr, exitCode := runBashSectionOffline(t, stubDirPosix, decoded)
	if exitCode == 127 && strings.Contains(stderr, "not found") {
		t.Errorf("late_command 脚本执行时出现「命令未找到」（exit=127），说明 "+
			"in-target bash -c 里的 post 阶段 hook 调用没有自带 run_hook/signal "+
			"定义——目标系统 chroot 后的新 bash 进程看不到外层脚本定义的函数。"+
			"stderr=%q\nscript=%q", stderr, decoded)
	}
	if strings.Contains(stderr, "run_hook: command not found") ||
		strings.Contains(stderr, "run_hook: not found") {
		t.Errorf("stderr 提及 run_hook 未找到，即使整体 exit code 不是 127，仍说明 "+
			"in-target 里的函数未被正确定义。stderr=%q", stderr)
	}
}

// decodeLateCommandScript 从 lateCommandValue 的返回值里提取 base64 编码
// 的脚本正文并解码——late 的值形如
// `in-target sh -c 'echo <base64> | base64 -d > /tmp/lober-late.sh && ...'`。
func decodeLateCommandScript(t *testing.T, late string) string {
	t.Helper()
	const marker = "echo "
	idx := strings.Index(late, marker)
	if idx == -1 {
		t.Fatalf("late_command 值中找不到 %q 标记，got: %q", marker, late)
	}
	rest := late[idx+len(marker):]
	end := strings.Index(rest, " | base64 -d")
	if end == -1 {
		t.Fatalf("late_command 值中找不到 base64 解码管道，got: %q", late)
	}
	encoded := rest[:end]

	// 用 Go 原生解码，不经 shell。此前写的是
	// `exec.Command("bash","-c","echo "+encoded+" | base64 -d")`，而这段
	// base64 会随 clientlib 增长——firstboot 脚手架把整个 clientlib 又嵌了
	// 一层，编码后可达数万字符。超过 Windows 的命令行长度上限（约 32KB）后
	// echo 被静默截断，解码出一段不完整的脚本，测试于是报「缺少信号
	// post-start」之类——而信号其实就在脚本里，只是被截掉了。
	//
	// 排查这种失败要先怀疑渲染器、再怀疑 golden，最后才会想到是测试辅助函数
	// 自己的长度上限，所以换成不依赖外部进程的实现。
	out, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 解码失败: %v（编码长度 %d）", err, len(encoded))
	}
	return string(out)
}
