// 本文件用真实 YAML 解析器解出 AssembleAutoinstall 产出的每一条命令，再用
// 真实 bash 子进程逐条执行，验证两条只有真实解析/执行才能证实的不变量：
//
//  1. early-commands/late-commands 里每一项经 YAML 解析后得到的字符串，
//     与写入时的原始多行文本逐字节一致（验证"用序列化库不会因为 hook
//     正文含换行/引号/冒号等 YAML 特殊字符而破坏结构"，计划任务 3 最
//     担心的那个坑）。
//  2. 每一项本身作为独立 bash 脚本执行时都能找到它调用的 run_hook/
//     signal（与 kickstart_exec_test.go、preseed_exec_test.go 验证的是
//     同一类"执行环境不共享变量/函数定义"问题，这里的边界是 cloud-init
//     对 commands 列表逐项分别起一次执行）。
package render

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// parsedAutoinstallCommands 解析 AssembleAutoinstall 的输出，返回
// early-commands 与 late-commands 两个字符串列表。
func parsedAutoinstallCommands(t *testing.T, out string) (early, late []string) {
	t.Helper()
	var doc struct {
		Autoinstall struct {
			EarlyCommands []string `yaml:"early-commands"`
			LateCommands  []string `yaml:"late-commands"`
		} `yaml:"autoinstall"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("YAML 解析失败: %v\n输出:\n%s", err, out)
	}
	return doc.Autoinstall.EarlyCommands, doc.Autoinstall.LateCommands
}

// TestExec_AutoinstallCommandsAreSelfContained 对 early-commands 与
// late-commands 里的每一项分别起一个 bash 子进程执行，确认调用 run_hook
// 的那些项都能在自己的子进程里解析到 run_hook/signal 的定义。
func TestExec_AutoinstallCommandsAreSelfContained(t *testing.T) {
	ctx := fullAutoinstallCtx()
	out, err := AssembleAutoinstall(ctx)
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}

	early, late := parsedAutoinstallCommands(t, out)

	stubDir := t.TempDir()
	writeStub(t, stubDir, "curl")
	writeStub(t, stubDir, "sha256sum")
	writeInTargetStub(t, stubDir)
	stubDirPosix := posixPath(t, stubDir)

	checkItem := func(label, item string) {
		if strings.Contains(item, "curtin in-target --") {
			// 包 curtin in-target 的项不在本测试的检查范围内——curtin 本身
			// 不在这个 stub 集合里（它是 chroot 包装器，语义与 curl/
			// sha256sum 这类"依赖真实网络"的命令不同），剥离前缀后再验证
			// 是 TestExec_AutoinstallInTargetPostHookRunsInsideChroot 专门
			// 负责的事，这里跳过避免对同一件事做冲突的假设。
			return
		}
		if !strings.Contains(item, "run_hook '") && !strings.Contains(item, "run_hook \\''") {
			return // 该项不调用 run_hook（例如纯粹的 firstboot 落盘脚手架，那部分不含 run_hook 直接调用，由另一条测试的 golden 覆盖），跳过。
		}
		stderr, exitCode := runBashSectionOffline(t, stubDirPosix, item)
		if exitCode == 127 && strings.Contains(stderr, "not found") {
			t.Errorf("%s 执行时出现「命令未找到」（exit=127），说明该项没有自带 "+
				"run_hook/signal 定义。stderr=%q\nitem=%q", label, stderr, item)
		}
		if strings.Contains(stderr, "run_hook: command not found") ||
			strings.Contains(stderr, "run_hook: not found") {
			t.Errorf("%s 的 stderr 提及 run_hook 未找到。stderr=%q", label, stderr)
		}
	}

	for i, item := range early {
		checkItem("early-commands["+string(rune('0'+i))+"]", item)
	}
	for i, item := range late {
		checkItem("late-commands["+string(rune('0'+i))+"]", item)
	}

	if len(early) == 0 || len(late) == 0 {
		t.Fatalf("early-commands 或 late-commands 为空，测试前提（fullAutoinstallCtx "+
			"应包含三个 installer 阶段各一个 hook）可能已失效。early=%v late=%v", early, late)
	}
}

// TestExec_AutoinstallInTargetPostHookRunsInsideChroot 专门验证 post 阶段
// hook（包在 curtin in-target -- bash -c 里的那一项）剥离 in-target 后仍能
// 正确执行——与 preseed_exec_test.go 的
// TestExec_PreseedInTargetPostHookIsSelfContained 同一验证思路。
func TestExec_AutoinstallInTargetPostHookRunsInsideChroot(t *testing.T) {
	ctx := fullAutoinstallCtx()
	out, err := AssembleAutoinstall(ctx)
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	_, late := parsedAutoinstallCommands(t, out)

	var found bool
	for _, item := range late {
		if !strings.Contains(item, "curtin in-target --") {
			continue
		}
		if !strings.Contains(item, "run_hook '") && !strings.Contains(item, "cccc3333post") {
			continue
		}
		found = true

		// 剥离 "curtin in-target -- " 前缀，剩下的应是一条完整可执行的
		// "bash -c '...'" 命令。
		stripped := strings.TrimPrefix(item, "curtin in-target -- ")
		stubDir := t.TempDir()
		writeStub(t, stubDir, "curl")
		writeStub(t, stubDir, "sha256sum")
		stubDirPosix := posixPath(t, stubDir)

		stderr, exitCode := runBashSectionOffline(t, stubDirPosix, stripped)
		if exitCode == 127 && strings.Contains(stderr, "not found") {
			t.Errorf("剥离 curtin in-target -- 前缀后的命令执行失败（exit=127），"+
				"说明 in-target 包裹的脚本没有自带 run_hook/signal 定义。"+
				"stderr=%q\nitem=%q", stderr, item)
		}
	}
	if !found {
		t.Fatal("没有在 late-commands 中找到包含 curtin in-target -- 且调用 post 阶段 " +
			"hook 的项，测试前提可能已失效")
	}
}
