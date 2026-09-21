// 本文件用真实 bash 子进程执行 AssembleKickstart 的产出，验证一条只有真实
// 执行才能证实的不变量：**渲染器生成的每一个独立 kickstart 段落，必须在
// 自己的子进程里就能找到它调用的 signal/run_hook 函数**。
//
// 背景（曾经真实存在过的 bug，本文件的测试专门锁住修复后的行为，防止
// 未来重构时回归）：anaconda 把每一个 %pre / %post / %post --nochroot
// 段落各自落成独立的 /tmp/ks-script-XXXX 文件，用 execWithRedirect 各自
// 起一个全新子进程执行——这一点由 anaconda 源码证实（pyanaconda/
// kickstart.py 的 runPreScripts：`for script in preScripts: script.run("/")`；
// pyanaconda/core/kickstart/scripts.py 的 run_script：每个段落各自
// mkstemp 成文件后独立执行）。同类型的多个 %pre 段落之间、以及 %pre 与
// %post/%post --nochroot 之间，完全不共享变量与函数定义。
//
// 因此，若某个 installer 阶段的 hook 段落只调用 run_hook 而不重新注入
// ClientLib（signal/run_hook 的函数定义），该段落在真机上会得到
// "run_hook: command not found"（exit 127）——required hook 失败即中止
// 装机这条安全不变量会被静默架空。RenderStageSection 与 firstboot.go 的
// initScript 现在都在各自产出的段落里重新注入了 ClientLib，本文件用真实
// bash 执行确认这一点，不满足于读代码或做字符串包含断言。
package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// splitKickstartSections 把完整 kickstart 文本按 %pre/%post/%post --nochroot
// ... %end 边界切成段落列表，模拟 anaconda 把每个段落各自落成一个独立文件、
// 各自起一个子进程执行的真实行为（见本文件头部注释引用的 anaconda 源码）。
// 每个返回项是"段落头"（如 "%pre"、"%post"、"%post --nochroot"）与"段落体
// 文本"（不含头尾标记行）的配对。
func splitKickstartSections(t *testing.T, ks string) []struct {
	header string
	body   string
} {
	t.Helper()
	var sections []struct {
		header string
		body   string
	}

	lines := strings.Split(ks, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		if line == "%pre" || line == "%post" || line == "%post --nochroot" {
			header := line
			i++
			var bodyLines []string
			for i < len(lines) && lines[i] != "%end" {
				bodyLines = append(bodyLines, lines[i])
				i++
			}
			if i >= len(lines) {
				t.Fatalf("段落 %q 缺少闭合的 %%end，kickstart 全文:\n%s", header, ks)
			}
			sections = append(sections, struct {
				header string
				body   string
			}{header: header, body: strings.Join(bodyLines, "\n")})
			i++ // 跳过 %end 本行
			continue
		}
		i++
	}
	return sections
}

// runBashSectionOffline 把单个段落体当成独立脚本，起一个全新 bash 子进程
// 执行——精确对应 anaconda run_script() 里"每个段落各自 mkstemp 成文件、
// 各自 execWithRedirect 起子进程"的真实模型。
//
// stubDir 通过脚本体最前面的 `export PATH="$stubDir:$PATH"` 语句注入，
// 而不是通过 exec.Cmd.Env 从外部设置——这是因为在 Windows 的 MSYS/Cygwin
// bash 上，进程级环境变量里的 PATH 是分号分隔的 Windows 原生格式，bash
// 自己的 PATH 查找逻辑按冒号分隔解析，两者不兼容，从外部塞一个分号版本
// 会被 bash 整体当成一大段不可用的路径而静默忽略；而在 bash 脚本内部用
// `export PATH="<posix 路径>:$PATH"` 赋值，走的是 bash 自己的字符串处理，
// 冒号分隔能被正确识别（本文件调试过程中用真实 bash 验证过这个差异）。
// stubDir 传入前已经转换为 posix 形式（见 posixPath）。
//
// stub 掉 curl/sha256sum 这两个"依赖真实网络/真实下载文件"的外部命令，
// 换成立即成功返回，其余命令（timeout/tee/logger/bash 等，run_hook 内部
// 靠它们组织控制流：子 shell、管道、PIPESTATUS）仍解析到系统真实二进制
// ——这样测试结果只由"signal/run_hook 是否被 shell 正确解析到"决定，不受
// 制品服务器可达性或 DNS 解析耗时影响。
func runBashSectionOffline(t *testing.T, stubDirPosix, body string) (stderr string, exitCode int) {
	t.Helper()
	script := "set -u\n" +
		"export PATH=\"" + stubDirPosix + ":$PATH\"\n" +
		body
	cmd := exec.Command("bash", "-c", script)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("启动 bash 子进程失败（非退出码错误）: %v", err)
		}
	}
	return errBuf.String(), exitCode
}

// posixPath 把 Go 原生（可能是 Windows 反斜杠盘符）形式的目录路径转换成
// bash 能在 PATH 里正确识别的 posix 形式。优先用 cygpath -u（MSYS/Cygwin
// bash 环境下的标准转换工具，若不可用则说明当前很可能已经在原生 POSIX
// 系统上，路径本身已经是 posix 形式，只需把反斜杠替换成正斜杠兜底）。
func posixPath(t *testing.T, dir string) string {
	t.Helper()
	if out, err := exec.Command("cygpath", "-u", dir).Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return filepath.ToSlash(dir)
}

// writeStub 在 dir 下写一个立即成功退出的可执行 stub 脚本，命名为 name。
func writeStub(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("写 stub %s 失败: %v", name, err)
	}
}

// stubBinDir 在 t.TempDir() 下建 curl/sha256sum 两个桩程序——它们是被测
// 段落里唯二"依赖真实网络/真实下载文件"的外部命令，桩化后让脚本执行不
// 再依赖测试环境是否能访问真实制品服务器，也不再受 DNS 解析失败的耗时
// 影响。其余命令（timeout/tee/logger/bash）继续走 bash 自身默认 PATH
// 解析到系统真实二进制。返回值已经是 posix 形式，可直接拼进
// `export PATH=` 语句。
func stubBinDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeStub(t, dir, "curl")
	writeStub(t, dir, "sha256sum")
	return posixPath(t, dir)
}

// TestExec_HookStageSectionsAreSelfContained 锁住修复后的行为：三个
// installer 阶段（pre / post-nochroot / post）各自产出的 hook 段落，必须
// 在自己的子进程里就能解析到 signal 与 run_hook——不依赖序言段那个独立
// 执行的 %pre 段落。用真实 bash 子进程验证，不满足于字符串包含断言（那种
// 断言曾经在"函数确实被调用"与"函数在调用处实际可解析"这两件事之间留下
// 过一个真实的空子）。
func TestExec_HookStageSectionsAreSelfContained(t *testing.T) {
	ctx := fullKickstartCtx()
	ks, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 返回错误: %v", err)
	}

	sections := splitKickstartSections(t, ks)

	var prologueSectionIdx = -1
	for i, s := range sections {
		if s.header == "%pre" && strings.Contains(s.body, "run_hook() {") &&
			strings.Contains(s.body, "# 用户 hook 从这里开始") {
			prologueSectionIdx = i
			break
		}
	}
	if prologueSectionIdx == -1 {
		t.Fatalf("找不到序言段落（应含「# 用户 hook 从这里开始」标记），"+
			"切分结果: %+v", sections)
	}

	stubDir := stubBinDir(t)

	foundCallSection := false
	for i, s := range sections {
		if i == prologueSectionIdx {
			continue // 序言段自己定义 run_hook，不是本测试要验证的对象
		}
		if !strings.Contains(s.body, "run_hook '") {
			continue
		}
		foundCallSection = true

		stderr, exitCode := runBashSectionOffline(t, stubDir, s.body)

		// 核心断言：不能是"函数未定义"的症状（exit=127 且 stderr 提及
		// run_hook/signal 加 not found）。exitCode 本身允许非零（stub 化的
		// sha256sum 校验、timeout 包裹的子 shell 等仍可能因为其它原因返回
		// 非零，那不是本测试关心的问题）。
		if exitCode == 127 && strings.Contains(stderr, "not found") {
			t.Errorf("段落 %q 执行时出现「命令未找到」（exit=127），说明它没有"+
				"在自己的子进程里重新定义 signal/run_hook——anaconda 会把这个"+
				"段落落成独立文件独立执行，序言段定义的函数在这里不可见。"+
				"stderr=%q\nbody=%q", s.header, stderr, s.body)
			continue
		}
		if strings.Contains(stderr, "run_hook: command not found") ||
			strings.Contains(stderr, "run_hook: not found") ||
			strings.Contains(stderr, "signal: command not found") ||
			strings.Contains(stderr, "signal: not found") {
			t.Errorf("段落 %q 的 stderr 提及 run_hook/signal 未找到，即使整体 "+
				"exit code 不是 127（可能被后续命令的返回值覆盖），仍说明函数"+
				"未被正确定义。stderr=%q", s.header, stderr)
		}
	}

	if !foundCallSection {
		t.Fatalf("没有找到任何调用 run_hook 的非序言段落，测试前提（fullKickstartCtx "+
			"应包含三个 installer 阶段各一个 hook）可能已失效，kickstart 全文:\n%s", ks)
	}
}

// TestExec_FirstbootInitScriptIsSelfContained 对 firstboot 的 initScript
// 做同等验证：它在自己的 heredoc 里重新注入了一份 ClientLib
// （kickstart.go 的 initScript 函数），因为它清楚自己是另一个独立执行的
// 脚本文件（/usr/local/sbin/lober-init.sh，由 systemd 在重启后单独执行）。
func TestExec_FirstbootInitScriptIsSelfContained(t *testing.T) {
	ctx := fullKickstartCtx()
	ks, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 返回错误: %v", err)
	}

	marker := "cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'"
	start := strings.Index(ks, marker)
	if start == -1 {
		t.Fatalf("找不到 lober-init.sh 的 heredoc 起始标记，kickstart 全文:\n%s", ks)
	}
	rest := ks[start+len(marker):]
	end := strings.Index(rest, "LOBER_INIT_EOF")
	if end == -1 {
		t.Fatal("找不到 lober-init.sh 的 heredoc 闭合标记")
	}
	initScriptBody := strings.TrimPrefix(rest[:end], "\n")

	if !strings.Contains(initScriptBody, "run_hook() {") {
		t.Fatal("lober-init.sh 脚本体内应自带 run_hook 函数定义（它是独立执行的" +
			"脚本文件，不能依赖 post 阶段的段落定义），但没找到")
	}

	stubDir := stubBinDir(t)
	stderr, exitCode := runBashSectionOffline(t, stubDir, initScriptBody)
	if exitCode == 127 && strings.Contains(stderr, "not found") {
		t.Errorf("lober-init.sh 执行时出现「命令未找到」（exit=127），"+
			"stderr=%q", stderr)
	}
}

// TestExec_RequiredHookFailureAbortsSectionEvenWithLaterHooks 锁住修复后的
// 行为：同一 kickstart 段落里若有多个 hook，较早的 required hook 失败必须
// 让整段脚本以非零码退出，不能被同一段落里较后的 hook（即使它成功）掩盖。
//
// 背景（曾经真实存在过的 bug）：run_hook 函数体自身在 required hook 失败
// 时会正确返回退出码 1（clientlib.go 的 TestClientLib_RunHookRequiredGate
// 与 signal/run_hook 的十三条行为要求已经锁住这一点），但原实现的调用点
// （runHookCall）只是裸调用 `run_hook '...' '...' '...'`，不检查返回值——
// bash 默认不会因为某一行非零就中止整段脚本（没有 set -e）。当计划任务 5
// 明确要求支持的"同阶段多个 hook 按 order 顺序出现"场景发生、且第一个
// required hook 失败、后面还有别的 hook（无论成功与否）时，整段脚本的
// 退出码会被最后一条命令的状态覆盖——anaconda 会认为这个 %post/%pre 段落
// 整体成功，继续装机，正是计划原文点名警示的"required hook 失败也不会
// 中止装机，机器带着半配置的状态装完"。
//
// 本测试用一个可控的假 run_hook（第一个 digest 返回失败，其余返回成功）
// 替换真实函数体，只隔离验证"段落整体退出码是否正确反映了较早的
// required 失败"这一件事，不依赖真实网络或真实下载校验。
func TestExec_RequiredHookFailureAbortsSectionEvenWithLaterHooks(t *testing.T) {
	ctx := model.RenderContext{
		Bases: testBases,
		Hooks: []model.HookRef{
			{HookDigest: "first-required-fails", Stage: model.HookStagePost, Order: 10, Required: true},
			{HookDigest: "second-succeeds", Stage: model.HookStagePost, Order: 20, Required: true},
		},
	}
	out := RenderStageSection(ctx, model.HookStagePost)

	fakeRunHook := `run_hook() {
  local digest="$1"
  if [ "$digest" = "first-required-fails" ]; then return 1; fi
  return 0
}
`
	idx := strings.Index(out, "run_hook() {")
	if idx == -1 {
		t.Fatalf("RenderStageSection 产出中找不到 run_hook 函数定义，got:\n%s", out)
	}
	endIdx := strings.Index(out[idx:], "\n}\n")
	if endIdx == -1 {
		t.Fatalf("找不到 run_hook 函数定义的闭合花括号，got:\n%s", out)
	}
	patched := out[:idx] + fakeRunHook + out[idx+endIdx+len("\n}\n"):]

	// 剔除 kickstart 段落标记行（%post / %end）——那是 kickstart 语法，
	// 直接喂给 bash 会被误解析成 job-control 语法（%1 等），不是本测试
	// 关心的问题。
	var bashLines []string
	for _, l := range strings.Split(patched, "\n") {
		if l == "%post" || l == "%end" {
			continue
		}
		bashLines = append(bashLines, l)
	}

	stubDir := stubBinDir(t)
	stderr, exitCode := runBashSectionOffline(t, stubDir, strings.Join(bashLines, "\n"))

	if exitCode == 0 {
		t.Errorf("段落整体以 exit=0 结束，即使第一个 required hook (first-required-fails) "+
			"失败——说明较早的 required 失败被同段落里较后的 hook 掩盖了，anaconda 会认为"+
			"这个段落成功并继续装机，机器带着半配置的状态装完。stderr=%q\npatched script:\n%s",
			stderr, strings.Join(bashLines, "\n"))
	}
}
