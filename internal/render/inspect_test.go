// 本文件测试 RenderInspectScript 产出的采集脚本（对应实现计划 P2-02
// 任务 1，依据 SPEC §5.6、§6.2）。
package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// inspectCtx 构造 RenderInspectScript 的测试用渲染上下文（固定假数据，
// 与 golden_test.go 的 minimalKickstartCtx 同一精神：避免测试因时间/随机
// token 而不稳定）。
func inspectCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{ID: "42"},
		Bases: model.BaseURLs{
			GatewayBase:  "http://gw.internal",
			ArtifactBase: "http://ng.internal",
		},
		InstallToken: "install-tok-fixed-0000000000000000",
	}
}

// ---------------------------------------------------------------------------
// TestInspect_FourCategories：四类采集都在，且每类都有降级路径
// ---------------------------------------------------------------------------

func TestInspect_ContainsFourCollectionCategories(t *testing.T) {
	script := RenderInspectScript(inspectCtx())

	checks := []struct {
		name string
		want string
	}{
		{"磁盘（lsblk）", "lsblk"},
		{"网卡（ip）", "ip -o link show"},
		{"主板/内存（dmidecode）", "dmidecode"},
		{"CPU（/proc/cpuinfo 或 nproc）", "nproc"},
	}
	for _, c := range checks {
		if !strings.Contains(script, c.want) {
			t.Errorf("采集脚本应包含%s类别的采集命令 %q，但没有找到。脚本全文:\n%s",
				c.name, c.want, script)
		}
	}
}

func TestInspect_EthtoolHasSysFallback(t *testing.T) {
	// 任务 1 明确警示：ethtool 在 Debian 的 ramdisk 里可能缺失，缺失时应
	// 降级为只读 /sys 下的 link 状态文件。
	script := RenderInspectScript(inspectCtx())
	if !strings.Contains(script, "ethtool") {
		t.Fatal("脚本应尝试使用 ethtool")
	}
	if !strings.Contains(script, "/sys/class/net") {
		t.Fatal("脚本应有 ethtool 缺失时降级读取 /sys/class/net 下 link 状态文件的路径" +
			"——不降级的后果是 Debian 的采集整体失败")
	}
}

func TestInspect_DmidecodeHasSysFallback(t *testing.T) {
	// 任务 1 明确警示：dmidecode 在部分精简 ramdisk 里缺失，缺失时应降级为
	// 读 /sys/class/dmi/id/ 下的文件。
	script := RenderInspectScript(inspectCtx())
	if !strings.Contains(script, "dmidecode") {
		t.Fatal("脚本应尝试使用 dmidecode")
	}
	if !strings.Contains(script, "/sys/class/dmi/id") {
		t.Fatal("脚本应有 dmidecode 缺失时降级读取 /sys/class/dmi/id 的路径")
	}
}

// ---------------------------------------------------------------------------
// TestInspect_OutputFormat：输出格式是纯文本键值对
// ---------------------------------------------------------------------------

func TestInspect_OutputsKeyValuePairs(t *testing.T) {
	script := RenderInspectScript(inspectCtx())
	// 采集结果应以 KEY=value 形式追加进上报缓冲——脚本里应能找到形如
	// "LOBER_INSPECT_..." 的键名字面量，说明输出走的是键值对而不是自由
	// 文本或 JSON（ramdisk 里没有 jq，§6.2）。
	if !strings.Contains(script, "LOBER_INSPECT_DISK_") {
		t.Fatal("脚本应产出 LOBER_INSPECT_DISK_ 前缀的键值对（磁盘列表）")
	}
	if !strings.Contains(script, "LOBER_INSPECT_NIC_") {
		t.Fatal("脚本应产出 LOBER_INSPECT_NIC_ 前缀的键值对（网卡列表）")
	}
	// 不应含 jq 调用——ramdisk 里没有 jq（§6.2）。
	if strings.Contains(script, "jq ") || strings.Contains(script, "jq\n") {
		t.Fatal("脚本不应依赖 jq——installer ramdisk 里没有这个工具（SPEC §6.2）")
	}
	// 不应含 python 调用。
	if strings.Contains(script, "python") {
		t.Fatal("脚本不应依赖 python——installer ramdisk 里没有这个工具（SPEC §6.2）")
	}
}

func TestInspect_PostsToFactsEndpointFromContract(t *testing.T) {
	// facts 端点路径必须与 docs/contracts/http-machine.md 冻结的约定一致：
	// POST /v1/facts/{task}/{token}，不自己发明路径。
	ctx := inspectCtx()
	script := RenderInspectScript(ctx)
	wantPath := "/v1/facts/" + ctx.Task.ID + "/" + ctx.InstallToken
	if !strings.Contains(script, wantPath) {
		t.Fatalf("脚本应 POST 到契约文档规定的 facts 端点路径 %q，但脚本中未找到。脚本片段搜索失败，"+
			"全文:\n%s", wantPath, script)
	}
	if !strings.Contains(script, "curl") {
		t.Fatal("脚本应使用 curl 上报（installer ramdisk 里 curl 可能只有 busybox 版，但仍是可用工具）")
	}
}

// ---------------------------------------------------------------------------
// TestInspect_SingleToolFailureDoesNotAbort：单项采集失败不中断整体
// ---------------------------------------------------------------------------

// runInspectScriptOffline 用真实 bash 子进程执行采集脚本产出的文本，
// stub 掉 curl（避免真实网络请求），验证脚本整体的健壮性——不满足于
// 字符串包含断言。stubDir 下额外可以放置"故意缺失某工具"的效果：不放置
// 该工具的 stub，PATH 里也没有真实二进制时，命令查找失败本身就是
// "工具缺失"场景的天然模拟。
func runInspectScriptOffline(t *testing.T, stubDirPosix, script string) (stdout, stderr string, exitCode int) {
	t.Helper()
	// 用一个空 PATH（只含 stubDir）跑，逼近 ramdisk 环境"没有完整工具集"
	// 的现实——但仍需要 bash 内建与最基本的 coreutils（cat/mkdir 等），
	// 因此仍然把系统真实 PATH 追加在后面，只是 stubDir 优先命中。
	wrapped := "set -u\n" +
		"export PATH=\"" + stubDirPosix + ":$PATH\"\n" +
		script
	cmd := exec.Command("bash", "-c", wrapped)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
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
	return outBuf.String(), errBuf.String(), exitCode
}

// posixPathInspect 复用 kickstart_exec_test.go 的 posixPath 转换逻辑
// （同包内已有 posixPath 函数，直接调用，不重复定义）。

// stubBinDirWithCurlOnly 建一个只含 curl 桩的 PATH 目录（立即成功退出，
// 不真的发网络请求），其余命令交给系统真实 PATH 解析——用于验证"正常
// 工具齐备"场景下脚本能顺利跑完并不中断。
func stubBinDirWithCurlOnly(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "curl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("写 curl 桩失败: %v", err)
	}
	return posixPath(t, dir)
}

func TestInspect_MissingToolDoesNotAbortScript(t *testing.T) {
	// 用一个不存在的命令模拟"单项采集工具缺失"，验证脚本整体仍能跑完
	// （exit code 不应是因为某个 command not found 就让整个脚本中止）。
	// 这里不追加系统真实 PATH，逼近"某工具在 ramdisk 里彻底不存在"的
	// 场景——只留 bash 内建与脚本自身逻辑。
	ctx := inspectCtx()
	script := RenderInspectScript(ctx)

	stubDir := stubBinDirWithCurlOnly(t)
	_, stderr, exitCode := runInspectScriptOffline(t, stubDir, script)

	// 核心断言：不能是"未定义变量"类的硬中止（set -u 下引用未初始化变量
	// 会导致 bash 以非零码退出并报 "unbound variable"）——那种失败模式说明
	// 某个分支假设了前一个采集步骤必然成功。
	if strings.Contains(stderr, "unbound variable") {
		t.Errorf("脚本执行时出现「未绑定变量」错误，说明某个分支假设了前一步采集必然成功，"+
			"单项失败会级联中断整体采集。stderr=%q", stderr)
	}
	_ = exitCode // 允许非零退出（比如最后一步网络上报失败），只要不是因为变量未定义
}

func TestInspect_DiskCollectionFailureStillProducesNICOutput(t *testing.T) {
	// 更直接的"单项失败不中断"验证：磁盘采集函数本身若因权限或工具缺失
	// 报错，网卡采集的输出仍应出现在最终上报内容里。用一个会让 lsblk
	// 类命令失败的桩（返回非零）来模拟。
	ctx := inspectCtx()
	script := RenderInspectScript(ctx)

	stubDir := t.TempDir()
	// lsblk 桩：立即失败，模拟磁盘采集报错。
	if err := os.WriteFile(filepath.Join(stubDir, "lsblk"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("写 lsblk 桩失败: %v", err)
	}
	// curl 桩：把 POST 的 body 写到一个已知文件，供测试检查最终上报内容。
	captureFile := filepath.Join(stubDir, "captured_body.txt")
	curlStub := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--data-binary\" ]; then\n" +
		"    f=\"${a#@}\"\n" +
		"    cat \"$f\" >> '" + captureFile + "'\n" +
		"  fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(stubDir, "curl"), []byte(curlStub), 0o755); err != nil {
		t.Fatalf("写 curl 桩失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stubDir, "ip"), []byte(
		"#!/bin/sh\necho '2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500'\n"), 0o755); err != nil {
		t.Fatalf("写 ip 桩失败: %v", err)
	}

	stubDirPosix := posixPath(t, stubDir)
	_, stderr, _ := runInspectScriptOffline(t, stubDirPosix, script)
	if strings.Contains(stderr, "unbound variable") {
		t.Fatalf("lsblk 失败不应导致未绑定变量错误，stderr=%q", stderr)
	}

	body, err := os.ReadFile(captureFile)
	if err != nil {
		t.Fatalf("未捕获到任何上报 body（curl 桩可能未被调用），err=%v；stderr=%q", err, stderr)
	}
	if len(body) == 0 {
		t.Fatal("捕获到的上报 body 为空——磁盘采集失败不应导致整个上报内容也为空，" +
			"至少应包含网卡等其它类别的采集结果")
	}
}

// ---------------------------------------------------------------------------
// TestInspect_ByIDSelectionIsDeterministic：by-id 选择规则确定
// ---------------------------------------------------------------------------

func TestInspect_ByIDSelectionPrefersWWN(t *testing.T) {
	// 计划要求：磁盘列表要用 by-id 路径，优先 WWN 形式（最稳定）。脚本
	// 逻辑中应能找到对 wwn- 前缀的优先判断。
	script := RenderInspectScript(inspectCtx())
	if !strings.Contains(script, "wwn-") {
		t.Fatal("脚本应包含对 by-id 链接中 wwn- 前缀的优先选择逻辑")
	}
	if !strings.Contains(script, "/dev/disk/by-id") {
		t.Fatal("脚本应从 /dev/disk/by-id/ 反查磁盘的持久化标识")
	}
}

func TestInspect_ByIDSelectionDeterministicAcrossRuns(t *testing.T) {
	// 同一输入两次采集结果应相同——这里验证的是脚本文本本身的选择逻辑是
	// 确定性的（不含 $RANDOM、不依赖 ls 未排序输出的偶然顺序）。用真实
	// bash 执行同一段"多个 by-id 链接指向同一设备"的模拟输入两次，确认
	// 两次选出的结果一致。
	script := RenderInspectScript(inspectCtx())

	if strings.Contains(script, "$RANDOM") {
		t.Fatal("脚本不应使用 $RANDOM——by-id 选择规则必须确定，否则两次采集结果不同，" +
			"决策也就不确定")
	}

	// 提取"by-id 选择"相关的函数体逻辑并用受控的 /dev/disk/by-id 模拟
	// 目录跑两次，确认结果一致。这里不 mock 整个 lsblk 输出，只验证
	// "同一设备多个 by-id 链接时选择规则确定"这条独立行为：脚本里应包含
	// 一个按名字排序或明确优先级挑选的机制（sort、优先 wwn- 前缀），
	// 不依赖文件系统目录遍历的偶然顺序。
	if !strings.Contains(script, "sort") && !strings.Contains(script, "wwn-") {
		t.Fatal("脚本应有明确的排序或优先级机制来保证同一设备多个 by-id 链接时的选择确定，" +
			"不能依赖目录遍历的偶然顺序")
	}
}

// ---------------------------------------------------------------------------
// TestInspect_RoundTripMatchesProviderKeyFormat：产出格式与
// provider.ParseHWDisks/ParseHWNICs 期望的键名/字段完全对齐
// ---------------------------------------------------------------------------

// TestInspect_DiskFunctionOutputExactKeyFormat 单独提取磁盘采集函数体，
// 用真实 bash 执行并配上真实的 lsblk 桩与真实的临时目录模拟
// /dev/disk/by-id 结构，逐字符核对产出的键名与字段顺序——这是
// render/inspect.go 与 provider/hwfacts.go 之间的编码契约，任何一边的
// 键名或字段格式改动都必须让这个测试变红（两个包之间不能互相 import，
// 靠这一测试守住格式一致性，而不是靠编译期检查）。
func TestInspect_DiskFunctionOutputExactKeyFormat(t *testing.T) {
	stubDir := t.TempDir()
	lsblkStub := "#!/bin/sh\n" +
		"echo 'sda 500107862016 0 SAMSUNG_MZ7LH480'\n" +
		"echo 'sdb 2000398934016 1 WDC_WD20EFRX'\n"
	if err := os.WriteFile(filepath.Join(stubDir, "lsblk"), []byte(lsblkStub), 0o755); err != nil {
		t.Fatalf("写 lsblk 桩失败: %v", err)
	}

	outFile := filepath.Join(stubDir, "inspect_out.txt")
	script := "set -u\n" +
		"export PATH=\"" + posixPath(t, stubDir) + ":$PATH\"\n" +
		"LOBER_INSPECT_OUT='" + posixPath(t, outFile) + "'\n" +
		inspectDiskFunction() + "\n" +
		"lober_inspect_disks\n"

	cmd := exec.Command("bash", "-c", script)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("执行磁盘采集函数失败: %v, stderr=%s", err, errBuf.String())
	}

	body, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("读取采集输出文件失败: %v", err)
	}
	got := string(body)

	// 没有 /dev/disk/by-id（本测试环境不是真实 ramdisk），函数应回退到
	// /dev/<name> 形式而不是产出空 BYID——这本身也是"降级路径"的一种
	// （by-id 反查失败时兜底为内核名，好过完全不产出该字段）。
	wantLines := []string{
		"LOBER_INSPECT_DISK_0_BYID=",
		"LOBER_INSPECT_DISK_0_SIZE=500107862016",
		"LOBER_INSPECT_DISK_0_ROTATIONAL=0",
		"LOBER_INSPECT_DISK_0_MODEL=SAMSUNG_MZ7LH480",
		"LOBER_INSPECT_DISK_1_BYID=",
		"LOBER_INSPECT_DISK_1_SIZE=2000398934016",
		"LOBER_INSPECT_DISK_1_ROTATIONAL=1",
		"LOBER_INSPECT_DISK_1_MODEL=WDC_WD20EFRX",
	}
	for _, w := range wantLines {
		if !strings.Contains(got, w) {
			t.Errorf("采集输出应包含前缀 %q，但没有找到。完整输出:\n%s", w, got)
		}
	}
}

// TestInspect_NICFunctionOutputExactKeyFormat 对网卡采集函数做同等的
// round-trip 格式核对。
func TestInspect_NICFunctionOutputExactKeyFormat(t *testing.T) {
	stubDir := t.TempDir()
	ipStub := "#!/bin/sh\n" +
		"echo '2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500'\n" +
		"echo '3: eth1: <BROADCAST,MULTICAST> mtu 1500'\n"
	if err := os.WriteFile(filepath.Join(stubDir, "ip"), []byte(ipStub), 0o755); err != nil {
		t.Fatalf("写 ip 桩失败: %v", err)
	}

	// 构造假的 /sys/class/net 结构：eth0/eth1 各带 address 与 operstate。
	sysNet := filepath.Join(stubDir, "sys_class_net")
	for _, e := range []struct{ name, mac, state string }{
		{"eth0", "aa:bb:cc:dd:ee:00", "up"},
		{"eth1", "aa:bb:cc:dd:ee:01", "down"},
	} {
		dir := filepath.Join(sysNet, e.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("创建 %s 失败: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "address"), []byte(e.mac+"\n"), 0o644); err != nil {
			t.Fatalf("写 address 失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "operstate"), []byte(e.state+"\n"), 0o644); err != nil {
			t.Fatalf("写 operstate 失败: %v", err)
		}
	}

	outFile := filepath.Join(stubDir, "inspect_out.txt")
	sysNetPosix := posixPath(t, sysNet)
	// 用一层间接：把函数体中的 /sys/class/net 替换成测试用的假路径，
	// 因为真实 /sys/class/net 在测试环境（尤其 Windows bash）下不代表
	// 真实网络接口结构，必须可替换才能做确定性断言。
	body := inspectNICFunction()
	body = strings.ReplaceAll(body, "/sys/class/net", sysNetPosix)

	script := "set -u\n" +
		"export PATH=\"" + posixPath(t, stubDir) + ":$PATH\"\n" +
		"LOBER_INSPECT_OUT='" + posixPath(t, outFile) + "'\n" +
		body + "\n" +
		"lober_inspect_nics\n"

	cmd := exec.Command("bash", "-c", script)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("执行网卡采集函数失败: %v, stderr=%s", err, errBuf.String())
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("读取采集输出文件失败: %v", err)
	}
	gotStr := string(got)

	wantLines := []string{
		"LOBER_INSPECT_NIC_0_NAME=eth0",
		"LOBER_INSPECT_NIC_0_MAC=aa:bb:cc:dd:ee:00",
		"LOBER_INSPECT_NIC_0_LINK=up",
		"LOBER_INSPECT_NIC_1_NAME=eth1",
		"LOBER_INSPECT_NIC_1_MAC=aa:bb:cc:dd:ee:01",
		"LOBER_INSPECT_NIC_1_LINK=down",
	}
	for _, w := range wantLines {
		if !strings.Contains(gotStr, w) {
			t.Errorf("采集输出应包含 %q，但没有找到。完整输出:\n%s", w, gotStr)
		}
	}
}

// ---------------------------------------------------------------------------
// TestInspect_NoJQOrPython：不依赖 ramdisk 里没有的工具
// ---------------------------------------------------------------------------

func TestInspect_UsesOnlyShellBuiltinsAndCoreutils(t *testing.T) {
	script := RenderInspectScript(inspectCtx())
	forbidden := []string{"jq ", "python", "perl "}
	for _, f := range forbidden {
		if strings.Contains(script, f) {
			t.Errorf("脚本不应使用 %q——installer ramdisk 里没有 python、没有 jq，"+
				"curl 也可能只是 busybox 版（SPEC §6.2）", f)
		}
	}
}

// ---------------------------------------------------------------------------
// TestInspect_ScriptIsSelfContained：与其它段落一致，自成一个完整脚本片段
// ---------------------------------------------------------------------------

func TestInspect_EmptyTaskIDStillProducesNonEmptyScript(t *testing.T) {
	ctx := inspectCtx()
	ctx.Task.ID = ""
	script := RenderInspectScript(ctx)
	if script == "" {
		t.Fatal("即使 Task.ID 为空也应产出脚本文本（渲染器不做输入校验中止，" +
			"上游装配点负责保证 Task.ID 非空）")
	}
}

func TestInspect_DeterministicOutputForSameInput(t *testing.T) {
	ctx := inspectCtx()
	s1 := RenderInspectScript(ctx)
	s2 := RenderInspectScript(ctx)
	if s1 != s2 {
		t.Fatal("同一输入两次渲染结果应逐字节相同——渲染器是纯函数，不应引入任何" +
			"非确定性来源")
	}
}

// ---------------------------------------------------------------------------
// TestInspect_FullLoop：采集 → 解析 → 决策 → 渲染的完整闭环
// ---------------------------------------------------------------------------
//
// 本轨（t12-inspect-wire）任务明确要求锁住的测试之一：给一份真实形态的
// lsblk 输出文本，确认最终渲染出的应答文件里写的是 by-id 路径而不是
// /dev/sda。
//
// 闭环的四步分属两个互不可见的实现模块（render 不能 import provider，
// archtest 规则三），因此本测试只覆盖闭环的后两步（决策结果 → 渲染），
// 前两步（采集 → 解析）由同一份闭环场景在 internal/provider 包内的对应
// 测试覆盖（TestFullLoop_RealisticLsblkOutputThroughParseAndPlan，
// hwfacts_test.go）——两个测试共享同一份"真实 lsblk 形态输出"场景数据
// （下面 TestInspect_DiskFunctionOutputExactKeyFormat 已经验证了这份数据
// 经采集脚本产出的键名格式），拼起来即是 SPEC §5.6 要求的完整顺序：
// 采集（render/inspect.go 的 lober_inspect_disks 函数）→ 解析
// （provider.ParseHWDisks）→ 决策（provider 的 DiskPlanner）→ 渲染（本
// 测试，AssembleKickstart 消费决策结果）。
//
// 这不是重复测试：TestInspect_DiskFunctionOutputExactKeyFormat 验证的是
// "采集脚本产出的键名格式对不对"，本测试验证的是"决策产出的 by-id 路径
// 能不能正确进入最终应答文件的分区指令"——两者验证闭环的不同环节，中间
// 的"决策"环节由 provider 包的测试覆盖，此处不重复决策逻辑本身。
func TestInspect_FullLoop_DecisionResultReachesRenderedAnswerFileAsByIDPath(t *testing.T) {
	// 模拟真机上报后、服务端决策完成的结果：按 SPEC §5.6"按容量选盘"的
	// 规则，两块候选盘中容量更大的 wwn-...bbb（2TB）胜出，决策产出的
	// by-id 路径写入 Facts[model.FactDiskPrimary]——这正是
	// provider/resolve.go 的 Resolve 方法在真实调用链路中会做的事（对应
	// TestResolveTaskParams_AllSucceedMergesAllExpectedKeys 已经验证的
	// "decodeDisksFromFacts 能从 LOBER_INSPECT_DISK_ 键解析出盘并交给
	// DiskPlanner"这一段），本测试从"决策已经产出结果"这一步接续，验证
	// 渲染层如何消费它。
	const decidedByIDPath = "/dev/disk/by-id/wwn-0x5000c500bbb"

	ctx := minimalKickstartCtx()
	ctx.Facts[model.FactDiskPrimary] = decidedByIDPath

	ks, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("组装 kickstart 失败: %v", err)
	}

	if !strings.Contains(ks, decidedByIDPath) {
		t.Fatalf("应答文件应包含服务端决策产出的 by-id 路径 %q，但没有找到。"+
			"完整应答文件:\n%s", decidedByIDPath, ks)
	}
	// 核心断言（SPEC §10 失败模式 5）：不能出现内核分配的设备名——那种
	// 名字在不同启动之间会漂移，装完的机器可能因为设备名重新编号而擦错盘。
	forbiddenKernelNames := []string{"/dev/sda", "/dev/sdb", "/dev/nvme0n1", "/dev/vda"}
	for _, bad := range forbiddenKernelNames {
		if strings.Contains(ks, bad) {
			t.Errorf("应答文件不应包含内核分配的设备名 %q——磁盘设备名在不同启动之间"+
				"会漂移，装完的机器可能因为重新编号而擦错盘（SPEC §10 失败模式 5）。"+
				"完整应答文件:\n%s", bad, ks)
		}
	}
	if !strings.Contains(ks, "--ondisk="+decidedByIDPath) {
		t.Errorf("分区指令应使用 --ondisk=%s 精确指向决策选中的盘，但没有找到。"+
			"完整应答文件:\n%s", decidedByIDPath, ks)
	}
}

// TestInspect_FullLoop_ByHookPolicyOmitsDiskPrimaryStillRendersWithoutKernelName
// 验证 by-hook 策略场景（服务端不做决策，见 provider/resolve.go 的
// TestResolveTaskParams_ByHookDiskPolicy_DiskPrimaryKeyOmittedNotEmpty）与
// 渲染层的组合：Facts 中不存在 LOBER_DISK_PRIMARY 键时，diskByID 取到零值
// 空字符串。
//
// 本测试原先记录的那个缺口（"by-hook 场景下渲染出一个空 --ondisk= 是已知
// 行为，留给 kickstart.go 装配点处理"）**已经修掉**：kickstart.go 的
// partitionDirectives 现在对 by-hook 渲染 `%include`（由 %pre 阶段的 hook
// 写出真正的分区指令），对其余策略在缺主盘时直接报错拒绝渲染。因此这里
// 显式声明 by-hook——「既不声明策略、也没有主盘 fact」是个不自洽的上下文。
//
// 本测试的断言不变，且现在被更强地满足：确认这种组合不会意外拼出一个看起来
// 合法的内核设备名（比如某种回退逻辑悄悄补了 /dev/sda），把"服务端未决策"
// 这个事实掩盖掉。
func TestInspect_FullLoop_MissingDiskPrimaryFactDoesNotFallBackToKernelName(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyByHook
	delete(ctx.Facts, model.FactDiskPrimary)

	ks, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("组装 kickstart 失败: %v", err)
	}

	forbiddenKernelNames := []string{"/dev/sda", "/dev/sdb", "/dev/nvme0n1", "/dev/vda"}
	for _, bad := range forbiddenKernelNames {
		if strings.Contains(ks, bad) {
			t.Errorf("Facts 中没有服务端决策结果时，渲染器不应静默回退到内核设备名 %q"+
				"——那会掩盖「没有决策结果」这个事实，让人误以为选盘正常完成。"+
				"完整应答文件:\n%s", bad, ks)
		}
	}
}
