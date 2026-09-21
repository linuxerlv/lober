// 本文件测试 Debian preseed 渲染（对应实现计划 phase2-01 任务 2，依据
// SPEC §4.4、§4.5、§5.2）。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// minimalPreseedCtx 构造"无 hook 的最小 preseed"渲染上下文。
func minimalPreseedCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID:            fixedTaskID,
			HostSN:        fixedSN,
			FinalizeToken: fixedFinalizeToken,
		},
		Template: model.Template{
			ID: "tpl-debian12-minimal",
		},
		Artifact: model.ArtifactSet{
			OSFamily:  model.OSFamilyDebian,
			OSVersion: "12",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "debian12/linux",
				Initrd: []string{"debian12/initrd.gz"},
				Repo:   "debian12/repo/",
			},
		},
		Facts: map[string]string{
			model.FactTaskID:       fixedTaskID,
			model.FactSN:           fixedSN,
			model.FactHostname:     fixedHostname,
			model.FactBizIP:        fixedBizIP,
			model.FactDiskPrimary:  fixedDiskByID,
			model.FactOSFamily:     string(model.OSFamilyDebian),
			model.FactArtifactBase: goldenBases.ArtifactURL(),
			model.FactGatewayBase:  goldenBases.GatewayURL(),
		},
		Bases:         goldenBases,
		InstallToken:  fixedInstallToken,
		FinalizeToken: fixedFinalizeToken,
		SyslogHost:    "10.10.0.1",
		SyslogPort:    514,
	}
}

// fullPreseedCtx 构造"带三阶段 hook 的完整 preseed"渲染上下文。
func fullPreseedCtx() model.RenderContext {
	ctx := minimalPreseedCtx()
	ctx.Template.ID = "tpl-debian12-full"
	ctx.Hooks = []model.HookRef{
		{HookDigest: "aaaa1111pre", Stage: model.HookStagePre, Order: 10, Required: true},
		{HookDigest: "bbbb2222nochroot", Stage: model.HookStagePostNoChroot, Order: 10, Required: false},
		{HookDigest: "cccc3333post", Stage: model.HookStagePost, Order: 10, Required: true},
		{HookDigest: "dddd4444firstboot", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}
	return ctx
}

// TestPreseed_FiveSectionsFourFieldFormat 锁住"五组段落都生成，格式是
// 四段式"（计划任务 2 测试要求第一条）。
func TestPreseed_FiveSectionsFourFieldFormat(t *testing.T) {
	out, err := AssemblePreseed(minimalPreseedCtx())
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}

	// debconf 预置项四段式：所有者 键 类型 值，以 tab 分隔。
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	dataLines := 0
	for _, l := range lines {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		fields := strings.Split(l, "\t")
		if len(fields) < 4 {
			t.Errorf("preseed 行 %q 不是四段式（所有者/键/类型/值），got %d 段", l, len(fields))
			continue
		}
		dataLines++
	}
	if dataLines == 0 {
		t.Fatal("preseed 输出中没有任何四段式数据行")
	}

	requiredOwners := []string{"d-i"}
	for _, owner := range requiredOwners {
		if !strings.Contains(out, owner+"\t") {
			t.Errorf("preseed 输出应含所有者 %q 开头的行，got:\n%s", owner, out)
		}
	}

	sections := []struct {
		name string
		key  string
	}{
		{"本地化", "localechooser/languagelist"},
		{"网络", "netcfg/get_hostname"},
		{"镜像源", "mirror/http/hostname"},
		{"分区", "partman-auto/disk"},
		{"账户", "passwd/root-login"},
	}
	for _, s := range sections {
		if !strings.Contains(out, s.key) {
			t.Errorf("preseed 输出缺少 %s 段落的键 %q，got:\n%s", s.name, s.key, out)
		}
	}
}

// TestPreseed_PostHooksUseScriptFileNotSingleLine 锁住"post 阶段的 hook
// 走「落脚本再执行」而不是压进单行"（计划任务 2 三处细节第一条）。
func TestPreseed_PostHooksUseScriptFileNotSingleLine(t *testing.T) {
	out, err := AssemblePreseed(fullPreseedCtx())
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}

	// late_command 的值本身只应是"落脚本 + 执行脚本"这两三条命令，不应把
	// 每个 hook 的 run_hook 调用压缩进 late_command 那一行里——找到
	// late_command 那一行，确认其中不直接包含 run_hook 调用（run_hook 应该
	// 出现在被写入的脚本文件内容里，而不是 late_command 单行本身）。
	idx := strings.Index(out, "preseed/late_command")
	if idx == -1 {
		t.Fatalf("preseed 输出中找不到 preseed/late_command，got:\n%s", out)
	}
	lineEnd := strings.Index(out[idx:], "\n")
	if lineEnd == -1 {
		lineEnd = len(out) - idx
	}
	lateCommandLine := out[idx : idx+lineEnd]
	if strings.Contains(lateCommandLine, "run_hook '") {
		t.Errorf("late_command 那一行直接包含 run_hook 调用，说明 hook 被压进了单行，"+
			"而不是「落脚本再执行」，got 该行:\n%s", lateCommandLine)
	}

	// 脚本文件内容（落在 preseed 输出的其它位置，通过 in-target tee 落盘）
	// 应该包含 run_hook 调用。
	if !strings.Contains(out, "run_hook '") {
		t.Errorf("preseed 输出中完全没有 run_hook 调用，hook 没有被落进任何脚本文件，got:\n%s", out)
	}
}

// TestPreseed_InTargetPrefixForChrootCommands 锁住"对目标系统的命令带
// in-target 前缀"（计划任务 2 三处细节第二条）。
func TestPreseed_InTargetPrefixForChrootCommands(t *testing.T) {
	out, err := AssemblePreseed(fullPreseedCtx())
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}
	if !strings.Contains(out, "in-target") {
		t.Errorf("preseed 输出应含 in-target 前缀（post 阶段的 hook 需要在目标系统内执行"+
			"，不加前缀会在 installer 环境里跑），got:\n%s", out)
	}
}

// TestPreseed_MirrorAndMenuURLsHaveNoQueryString 锁住"生成的应答文件 URL
// 里无 query string"（计划任务 2 三处细节第三条，SPEC §4.4：preseed/url=
// 在内核命令行上带 & 容易出事）。
//
// 这条约束的对象是"作为内核引导参数/preseed 键值出现的 URL"（mirror
// 段落的 hostname/directory 就属于这一类——它们最终会被 d-i 拼成完整的
// HTTP 请求 URL），不是"preseed.cfg 正文里任何出现的字符串都不能带
// ?"——ClientLib 注入的 signal() 函数会用 curl 发起带 ?rc=...&seq=...
// 的 HTTP 请求，那是运行在目标机 shell 里的普通 HTTP 调用，不是内核命令
// 行参数，query string 在那个位置完全安全（curl 本身能正确处理），也是
// SPEC §6.2 明确要求的协议形态。因此本测试只检查 mirror 段落两个键的值，
// 不对整份输出做无差别的"不含 ?"断言。
func TestPreseed_MirrorAndMenuURLsHaveNoQueryString(t *testing.T) {
	out, err := AssemblePreseed(minimalPreseedCtx())
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}
	for _, key := range []string{"mirror/http/hostname", "mirror/http/directory"} {
		idx := strings.Index(out, key)
		if idx == -1 {
			t.Fatalf("找不到键 %q，got:\n%s", key, out)
		}
		lineEnd := strings.Index(out[idx:], "\n")
		line := out[idx : idx+lineEnd]
		if strings.Contains(line, "?") {
			t.Errorf("键 %q 所在行含 ? 字符，说明该 URL 片段带了 query string，got: %q", key, line)
		}
	}
}

// TestPreseed_PartitionRecipeUsesByID 锁住"分区配方用 by-id 路径"（计划
// 任务 2"分区要用服务端决策的盘，设备标识用 by-id"）。
func TestPreseed_PartitionRecipeUsesByID(t *testing.T) {
	out, err := AssemblePreseed(minimalPreseedCtx())
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}
	if !strings.Contains(out, fixedDiskByID) {
		t.Errorf("preseed 分区配方应引用 by-id 盘标识 %q，got:\n%s", fixedDiskByID, out)
	}
}

// TestPreseed_NoLiteralHTTPScheme 锁住"输出里不含字面 http://"（与
// golden_test.go 的 RHEL 版本同等要求）。
func TestPreseed_NoLiteralHTTPScheme(t *testing.T) {
	httpsBases := model.BaseURLs{
		GatewayBase:  "https://gw.internal",
		ArtifactBase: "https://ng.internal",
	}
	ctx := fullPreseedCtx()
	ctx.Bases = httpsBases
	ctx.Facts = cloneFacts(ctx.Facts)
	ctx.Facts[model.FactArtifactBase] = httpsBases.ArtifactURL()
	ctx.Facts[model.FactGatewayBase] = httpsBases.GatewayURL()

	out, err := AssemblePreseed(ctx)
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}
	if strings.Contains(out, "http://") {
		t.Errorf("Bases 配置为 https:// 时，preseed 输出仍含字面 http://，got:\n%s", out)
	}
}

// TestPreseed_RejectsMaliciousParams 端到端注入测试：恶意表单值不能进入
// preseed 输出（复用 injection_test.go 同一套 payload 的精神，preseed 是
// 独立语法，需要单独验证 guard 校验在这条路径上也生效）。
func TestPreseed_RejectsMaliciousParams(t *testing.T) {
	ctx := fullPreseedCtx()
	ctx.Task.ResolvedParams = map[string]string{
		"hostname": "web01; rm -rf /",
	}
	out, err := AssemblePreseed(ctx)
	if err == nil {
		t.Errorf("恶意 hostname 应导致 AssemblePreseed 返回错误，got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("恶意值应导致渲染失败时不返回任何内容，got:\n%s", out)
	}
}

// TestSignalPoints_DebianAllPresentAndKnown 锁住"三个家族各自生成全部
// 信号点，且信号名都在 M1 的信号表里"（计划 phase2-01 任务 4 测试要求
// 第一条）。
func TestSignalPoints_DebianAllPresentAndKnown(t *testing.T) {
	out, err := AssemblePreseed(fullPreseedCtx())
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}
	late, err := lateCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decodedLate := decodeLateCommandScript(t, late)

	// pre-start/pre-done 直接出现在 early_command 的值里（未经 base64
	// 编码），post-start/post-done/rebooting 落在 late_command 的 base64
	// 编码脚本内，需要先解码——两者在渲染管线中的落点不同，测试分别检查。
	earlyMarkers := []model.SignalKind{model.SignalPreStart, model.SignalPreDone}
	for _, sig := range earlyMarkers {
		marker := `signal "` + string(sig) + `"`
		if !strings.Contains(out, marker) {
			t.Errorf("Debian preseed 的 early_command 中缺少信号 %q（marker=%q），got:\n%s", sig, marker, out)
		}
	}

	lateMarkers := []model.SignalKind{model.SignalPostStart, model.SignalPostDone, model.SignalRebooting}
	for _, sig := range lateMarkers {
		marker := `signal "` + string(sig) + `"`
		if !strings.Contains(decodedLate, marker) {
			t.Errorf("Debian preseed 的 late_command 脚本中缺少信号 %q（marker=%q），decoded:\n%s", sig, marker, decodedLate)
		}
	}

	for _, sig := range append(append([]model.SignalKind{}, earlyMarkers...), lateMarkers...) {
		if _, ok := model.LookupSignal(sig); !ok {
			t.Errorf("信号 %q 不在 M1 的信号表里（LookupSignal 返回 ok=false）——"+
				"渲染器不应发出信号表之外的信号名", sig)
		}
	}
}

// TestSignalPoints_DebianRebootingAtEndOfLastStage 锁住"三个家族的
// rebooting 都在最后一个阶段的末尾"（计划任务 4 测试要求第三条）。
func TestSignalPoints_DebianRebootingAtEndOfLastStage(t *testing.T) {
	late, err := lateCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)

	postDoneIdx := strings.Index(decoded, `signal "post-done"`)
	rebootingIdx := strings.Index(decoded, `signal "rebooting"`)
	if postDoneIdx == -1 || rebootingIdx == -1 {
		t.Fatalf("找不到 post-done 或 rebooting 信号，decoded:\n%s", decoded)
	}
	if rebootingIdx < postDoneIdx {
		t.Errorf("rebooting 信号出现在 post-done 之前，说明它不在最后一个阶段的末尾")
	}
	// rebooting 之后不应再有任何 hook 调用或其它信号。
	after := decoded[rebootingIdx+len(`signal "rebooting"`):]
	if strings.Contains(after, "run_hook '") {
		t.Errorf("rebooting 信号之后仍有 run_hook 调用，说明它不在末尾: %q", after)
	}
}

// TestPreseed_MaliciousFactValueDoesNotInjectExtraDebconfLine 锁住"换行
// 注入"这一类（计划任务 2/8 明确要求"至少覆盖换行注入与 YAML 结构破坏"，
// preseed 没有 YAML 结构，对应的等价风险是"注入一条伪造的 debconf 行"）。
// facts 命名空间的值不经过 guard 校验器，用一个含换行、看起来像合法
// debconf 行的 fact 值走一遍渲染，确认它不会被解析成独立的第五段式数据
// 行——它应该只出现在 early_command 的值字段内部（该字段本身经过
// preseedShellValue 压成单行，裸换行已被替换为空格，不会产生新的
// preseed 行）。
func TestPreseed_MaliciousFactValueDoesNotInjectExtraDebconfLine(t *testing.T) {
	ctx := fullPreseedCtx()
	ctx.Facts = cloneFacts(ctx.Facts)
	maliciousLine := "d-i\tpasswd/root-login\tboolean\ttrue"
	ctx.Facts[model.FactRack] = "rack-01\n" + maliciousLine

	out, err := AssemblePreseed(ctx)
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}

	// 合法的 passwd/root-login 行本身值是 false（preseedAccountAndPackages
	// 固定写死）；若恶意 fact 值成功注入了一条新的、值为 true 的同名行，
	// 说明换行注入突破了字段边界，伪造出了一条独立的 debconf 数据行。
	lines := strings.Split(out, "\n")
	trueLineCount := 0
	for _, l := range lines {
		if l == maliciousLine {
			trueLineCount++
		}
	}
	if trueLineCount > 0 {
		t.Errorf("恶意 fact 值中的伪造 debconf 行 %q 被解析成了独立的一行，"+
			"说明换行注入突破了 early_command 字段边界。got 完整输出:\n%s", maliciousLine, out)
	}
}

// TestPreseed_HostnameFactCannotForgeDebconfLine 是审阅时补的回归测试：
// TestPreseed_MaliciousFactValueDoesNotInjectExtraDebconfLine 只覆盖了
// LOBER_RACK（该值只流经 earlyCommandValue，经 preseedShellValue 压成
// 单行后天然安全），从未覆盖 LOBER_HOSTNAME/LOBER_DISK_PRIMARY 这两个
// 直接以四段式 debconfLine 写入 preseedNetwork/preseedMirror/
// preseedPartitioning 的字段——它们才是真正未经压缩、原样拼进
// "所有者\t键\t类型\t值"这行文本的位置。
//
// LOBER_HOSTNAME 的值来自 internal/provider/resolve.go 的
// `resolved[model.FactHostname] = formData[model.FactHostname]`——直接
// 复制表单原始值，不经过 InlineParams/guard 校验（那套校验只覆盖
// ctx.Task.ResolvedParams，不覆盖 ctx.Facts）。本测试验证换行注入不能
// 借道 preseedNetwork 伪造出一条独立的 debconf 行、覆盖
// preseedAccountAndPackages 固定写死的 passwd/root-login=false（SPEC
// §5.6 的账户策略不应被这条路径绕过）。
func TestPreseed_HostnameFactCannotForgeDebconfLine(t *testing.T) {
	ctx := fullPreseedCtx()
	ctx.Facts = cloneFacts(ctx.Facts)
	forgedLine := "d-i\tpasswd/root-login\tboolean\ttrue"
	ctx.Facts[model.FactHostname] = "web01\n" + forgedLine

	out, err := AssemblePreseed(ctx)
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}

	lines := strings.Split(out, "\n")
	for _, l := range lines {
		if l == forgedLine {
			t.Errorf("恶意 LOBER_HOSTNAME 换行注入伪造出了独立的 debconf 行 %q，"+
				"覆盖了固定的 passwd/root-login=false。got 完整输出:\n%s", forgedLine, out)
		}
	}
	// 双重确认：合法的 root-login=false 那一行仍然完整存在（没有被后续的
	// 伪造行覆盖或替换）。
	if !strings.Contains(out, "d-i\tpasswd/root-login\tboolean\tfalse") {
		t.Errorf("合法的 passwd/root-login=false 行应始终存在，got:\n%s", out)
	}
}

// TestPreseed_DiskPrimaryFactCannotForgeDebconfLine 同上，覆盖
// LOBER_DISK_PRIMARY——它经 preseedPartitioning 直接写入
// partman-auto/disk 的值段，是分区配方唯一的设备来源（计划任务 2"分区要
// 用服务端决策的盘"）。即使该值当前来自磁盘规划器（服务端计算，不是表单
// 直传），debconfLine 消费端的清理不应因值的来源"目前较可信"而省略——
// sanitizeDebconfValue 是 debconfLine 的统一写入口，任何未来改动都不应
// 悄悄绕开它。
func TestPreseed_DiskPrimaryFactCannotForgeDebconfLine(t *testing.T) {
	ctx := fullPreseedCtx()
	ctx.Facts = cloneFacts(ctx.Facts)
	forgedLine := "d-i\tpasswd/root-login\tboolean\ttrue"
	ctx.Facts[model.FactDiskPrimary] = "/dev/disk/by-id/evil\n" + forgedLine

	out, err := AssemblePreseed(ctx)
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}
	for _, l := range strings.Split(out, "\n") {
		if l == forgedLine {
			t.Errorf("恶意 LOBER_DISK_PRIMARY 换行注入伪造出了独立的 debconf 行 %q，got 完整输出:\n%s", forgedLine, out)
		}
	}
}

// TestGolden_MinimalPreseed 锁住无 hook 的最小 preseed 输出。
func TestGolden_MinimalPreseed(t *testing.T) {
	out, err := AssemblePreseed(minimalPreseedCtx())
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}
	assertGolden(t, "preseed_minimal.golden", out)
}

// TestGolden_FullPreseed 锁住带三阶段 hook 的完整 preseed 输出。
func TestGolden_FullPreseed(t *testing.T) {
	out, err := AssemblePreseed(fullPreseedCtx())
	if err != nil {
		t.Fatalf("AssemblePreseed 返回错误: %v", err)
	}
	assertGolden(t, "preseed_full.golden", out)
}

// TestGolden_DebianMenu 锁住 Debian 的 iPXE 菜单输出。
func TestGolden_DebianMenu(t *testing.T) {
	ctx := minimalPreseedCtx()
	ctx.ActualFirmware = model.FirmwareUEFI
	out, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 返回错误: %v", err)
	}
	assertGolden(t, "menu_debian.golden", out)
}

// TestMenu_DebianAnswerURLNoQueryString 锁住 Debian 菜单里 preseed/url=
// 的值不带 query string（SPEC §4.4）。
func TestMenu_DebianAnswerURLNoQueryString(t *testing.T) {
	ctx := minimalPreseedCtx()
	ctx.ActualFirmware = model.FirmwareUEFI
	out, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 返回错误: %v", err)
	}
	idx := strings.Index(out, "preseed/url=")
	if idx == -1 {
		t.Fatalf("Debian 菜单应含 preseed/url=，got:\n%s", out)
	}
	rest := out[idx:]
	end := strings.IndexAny(rest, " \n\\")
	if end == -1 {
		end = len(rest)
	}
	urlValue := rest[len("preseed/url="):end]
	if strings.Contains(urlValue, "?") {
		t.Errorf("preseed/url= 的值不应含 query string，got %q", urlValue)
	}
}
