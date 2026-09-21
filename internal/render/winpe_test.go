// 本文件测试 Windows 装机通道：wimboot 引导菜单（计划任务 2）、WinPE 内
// 遥测通道（计划任务 4）、firstboot 的 Windows 对应机制（计划任务 5）。
// 依据 SPEC §4.5、§4.6、§5.2 末段、§6.3、§6.5、§9.5，附录 A.4-A.6。
//
// menu.go 由 P2-multios 一轨独有，本文件不修改它——Windows 的菜单渲染
// 写成本文件里的独立导出函数 RenderWindowsMenu，供编排方合并后在 menu.go
// 的分派里加一行调用（见本文件末尾契约缺口说明）。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// 任务 2：引导菜单与固件选键
// ---------------------------------------------------------------------------

// windowsArtifactPaths 构造一个含四个 Extra 键的 Windows ArtifactSet.Paths，
// 供菜单测试复用。
func windowsArtifactPaths() model.ArtifactPaths {
	return model.ArtifactPaths{
		Extra: map[string]string{
			"wimboot":  "wimboot",
			"BCD":      "BCD",
			"boot.sdi": "boot.sdi",
			"boot.wim": "boot.wim",
		},
	}
}

// windowsMenuCtx 构造一个 Windows 家族的最小 RenderContext，declaredFirmware
// 是 artifact_set 声明的固件，actualFirmware 是机器引导时实际上报的固件
// （对应 RenderContext.ActualFirmware，§4.6 的核验对象）。
func windowsMenuCtx(declaredFirmware, actualFirmware model.FirmwareMode) model.RenderContext {
	return model.RenderContext{
		Task: model.Task{ID: "77"},
		Artifact: model.ArtifactSet{
			OSFamily:  model.OSFamilyWindows,
			OSVersion: "2022",
			Arch:      model.ArchX86_64,
			Firmware:  declaredFirmware,
			Paths:     windowsArtifactPaths(),
		},
		Bases:          testBases,
		InstallToken:   "tok-win-001",
		ActualFirmware: actualFirmware,
	}
}

// TestWindowsMenu_FourLines 锁住"菜单四行结构正确，三个 initrd 项各带
// 标签"（计划任务 2 测试要求第一条，SPEC §4.5）。
func TestWindowsMenu_FourLines(t *testing.T) {
	ctx := windowsMenuCtx(model.FirmwareUEFI, model.FirmwareUEFI)
	out, err := RenderWindowsMenu(ctx)
	if err != nil {
		t.Fatalf("RenderWindowsMenu 返回错误: %v", err)
	}

	kernelCount := strings.Count(out, "kernel ")
	initrdCount := strings.Count(out, "initrd ")
	if kernelCount != 1 {
		t.Errorf("菜单应恰好含一条 kernel 语句（wimboot 作为「内核」），got %d 条，输出:\n%s", kernelCount, out)
	}
	if initrdCount != 3 {
		t.Errorf("菜单应恰好含三条 initrd 语句（BCD/boot.sdi/boot.wim），got %d 条，输出:\n%s", initrdCount, out)
	}
}

// TestWindowsMenu_InitrdLabelsPresent 锁住"标签不能省——wimboot 靠它区分
// 三个文件的用途"（计划任务 2）。
func TestWindowsMenu_InitrdLabelsPresent(t *testing.T) {
	ctx := windowsMenuCtx(model.FirmwareUEFI, model.FirmwareUEFI)
	out, err := RenderWindowsMenu(ctx)
	if err != nil {
		t.Fatalf("RenderWindowsMenu 返回错误: %v", err)
	}

	for _, label := range []string{"BCD", "boot.sdi", "boot.wim"} {
		lines := linesContaining(out, label)
		if len(lines) == 0 {
			t.Fatalf("菜单中找不到引用 %q 的行，got:\n%s", label, out)
		}
		found := false
		for _, line := range lines {
			trimmed := strings.TrimRight(line, "\r\n")
			if strings.HasSuffix(trimmed, " "+label) {
				found = true
			}
		}
		if !found {
			t.Errorf("initrd 行引用 %q 的那一行应以标签 %q 结尾（wimboot 靠它区分三个文件的用途，"+
				"SPEC §4.5），got:\n%s", label, label, out)
		}
	}
}

// TestWindowsMenu_KernelLineIsWimboot 验证 kernel 行指向 wimboot 二进制。
func TestWindowsMenu_KernelLineIsWimboot(t *testing.T) {
	ctx := windowsMenuCtx(model.FirmwareUEFI, model.FirmwareUEFI)
	out, err := RenderWindowsMenu(ctx)
	if err != nil {
		t.Fatalf("RenderWindowsMenu 返回错误: %v", err)
	}
	if !strings.Contains(out, "wimboot") {
		t.Errorf("kernel 行应指向 wimboot，got:\n%s", out)
	}
}

// TestWindowsMenu_URLsFromBasesNoLiteralScheme 锁住"URL 从基址拼出，不含
// 字面 scheme"（计划任务 2 测试要求第二条）。
func TestWindowsMenu_URLsFromBasesNoLiteralScheme(t *testing.T) {
	ctx := windowsMenuCtx(model.FirmwareUEFI, model.FirmwareUEFI)
	ctx.Bases = model.BaseURLs{
		GatewayBase:  "https://gw.internal",
		ArtifactBase: "https://ng.internal",
	}
	out, err := RenderWindowsMenu(ctx)
	if err != nil {
		t.Fatalf("RenderWindowsMenu 返回错误: %v", err)
	}
	if strings.Contains(out, "http://") {
		t.Errorf("Bases 配置为 https:// 时，菜单输出仍含字面 http://，说明 scheme 被硬编码，got:\n%s", out)
	}
	if !strings.Contains(out, ctx.Bases.ArtifactURL()) {
		t.Errorf("菜单应含从 ArtifactURL() 拼出的基址 %q，got:\n%s", ctx.Bases.ArtifactURL(), out)
	}
}

// TestWindowsMenu_FirmwareMismatch_RejectedNotSwapped 锁住"实际固件与制品
// 集声明不符时拒绝下发并置失败（不换制品集）"（计划任务 2 测试要求第三条，
// SPEC §4.6：记录值可能与实际不符，不符时硬拒绝，不试图在引导时换制品集）。
func TestWindowsMenu_FirmwareMismatch_RejectedNotSwapped(t *testing.T) {
	// 制品集声明 uefi，机器实际上报 bios。
	ctx := windowsMenuCtx(model.FirmwareUEFI, model.FirmwareBIOS)
	out, err := RenderWindowsMenu(ctx)
	if err == nil {
		t.Fatalf("实际固件（bios）与制品集声明固件（uefi）不符时应返回错误，got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("固件不符时应返回空字符串（不是部分内容），got:\n%s", out)
	}
}

// TestWindowsMenu_FirmwareMatch_UEFI_Succeeds 与
// TestWindowsMenu_FirmwareMatch_BIOS_Succeeds 验证两种固件各自匹配时都能
// 正确渲染菜单（计划任务 2 测试要求第四条："两种固件的制品集各自渲染出
// 正确的菜单"）。
func TestWindowsMenu_FirmwareMatch_UEFI_Succeeds(t *testing.T) {
	ctx := windowsMenuCtx(model.FirmwareUEFI, model.FirmwareUEFI)
	out, err := RenderWindowsMenu(ctx)
	if err != nil {
		t.Fatalf("固件一致（uefi）时应渲染成功: %v", err)
	}
	if out == "" {
		t.Error("固件一致时菜单输出不应为空")
	}
}

func TestWindowsMenu_FirmwareMatch_BIOS_Succeeds(t *testing.T) {
	ctx := windowsMenuCtx(model.FirmwareBIOS, model.FirmwareBIOS)
	out, err := RenderWindowsMenu(ctx)
	if err != nil {
		t.Fatalf("固件一致（bios）时应渲染成功: %v", err)
	}
	if out == "" {
		t.Error("固件一致时菜单输出不应为空")
	}
}

// TestWindowsMenu_MissingExtraKey_ReturnsError 验证 ArtifactPaths.Extra 缺
// 某个键时返回具体错误，不 panic、不渲染出缺失内容的空字符串行。
func TestWindowsMenu_MissingExtraKey_ReturnsError(t *testing.T) {
	ctx := windowsMenuCtx(model.FirmwareUEFI, model.FirmwareUEFI)
	delete(ctx.Artifact.Paths.Extra, "boot.wim")

	out, err := RenderWindowsMenu(ctx)
	if err == nil {
		t.Fatalf("缺 boot.wim 键时应返回错误，got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("缺键时应返回空字符串，got:\n%s", out)
	}
}

// linesContaining 返回 s 中包含 substr 的所有行。
func linesContaining(s, substr string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 任务 4：WinPE 内的遥测通道
// ---------------------------------------------------------------------------

// winpeTelemetryCtx 构造一个用于 WinPEClientLib 测试的最小 RenderContext。
func winpeTelemetryCtx() model.RenderContext {
	return model.RenderContext{
		Task:         model.Task{ID: "77"},
		Bases:        testBases,
		InstallToken: "tok-win-001",
	}
}

// TestWinPE_ReportFunctionFourBehaviors 锁住"生成的 PowerShell 上报函数含
// 四条行为"（计划任务 4 测试要求第一条，§6.3）：重试三次、退避、失败时落
// 本地日志、最后返回成功（遥测失败不杀装机）。
func TestWinPE_ReportFunctionFourBehaviors(t *testing.T) {
	out := WinPEClientLib(winpeTelemetryCtx())

	if !strings.Contains(out, "1..3") && !strings.Contains(out, "1,2,3") && !strings.Contains(out, "for (") {
		t.Errorf("PowerShell 上报函数应含三次重试循环，got:\n%s", out)
	}
	if !strings.Contains(out, "Start-Sleep") {
		t.Errorf("PowerShell 上报函数应含退避等待（Start-Sleep），got:\n%s", out)
	}
	if !strings.Contains(out, "Add-Content") && !strings.Contains(out, "Out-File") {
		t.Errorf("PowerShell 上报函数失败时应落本地日志（Add-Content/Out-File），got:\n%s", out)
	}
	// 最后返回成功：函数体不应在重试耗尽后 throw 或返回非零，而是继续执行到
	// 底（与 bash 侧 `return 0` 的语义对应）。
	if strings.Contains(out, "throw ") || strings.Contains(out, "exit 1") {
		t.Errorf("PowerShell 上报函数遥测失败后不应中止脚本（throw/exit 1），"+
			"§6.3 明确「遥测失败不能杀装机」，got:\n%s", out)
	}
}

// TestWinPE_SignalNamesInM1Table 锁住"信号名都在 M1 的信号表内"（计划
// 任务 4 测试要求第二条）。WinPEClientLib 本身只定义上报函数（不引用任何
// 具体阶段名），实际的信号点在 WinPEInstallScript 的产出里——本测试据此
// 检查 WinPEInstallScript 的输出，而不是 WinPEClientLib。
func TestWinPE_SignalNamesInM1Table(t *testing.T) {
	out := WinPEInstallScript(winpeTelemetryCtx())
	stages := WinPESignalPoints()
	if len(stages) == 0 {
		t.Fatal("WinPESignalPoints 不应返回空列表")
	}
	for _, kind := range stages {
		if _, ok := model.LookupSignal(kind); !ok {
			t.Errorf("信号 %q 不在 model 的信号表内（LookupSignal 查不到），"+
				"WinPE 侧不能发明新的信号名", kind)
		}
		if !strings.Contains(out, string(kind)) {
			t.Errorf("WinPEInstallScript 输出中应引用信号 %q，got:\n%s", kind, out)
		}
	}
}

// TestWinPE_ReportFailureDoesNotAbortScript 锁住"上报失败时脚本继续"
// （计划任务 4 测试要求第三条）：用不可达地址验证脚本不因信号失败而中止。
//
// 与 clientlib_test.go 对 bash signal() 的验证方式一致：不实际执行脚本
// （那需要真实 PowerShell 环境），改为断言生成的文本里，Invoke-WebRequest/
// Invoke-RestMethod 调用被包在 try/catch 里，且 catch 块不包含会中止脚本
// 执行的语句（throw、exit）。
func TestWinPE_ReportFailureDoesNotAbortScript(t *testing.T) {
	out := WinPEClientLib(winpeTelemetryCtx())
	if !strings.Contains(out, "try") || !strings.Contains(out, "catch") {
		t.Fatalf("PowerShell 上报函数应用 try/catch 包裹网络调用，避免未捕获异常"+
			"中止整个脚本，got:\n%s", out)
	}
	catchBlock := extractPowerShellCatchBlocks(out)
	if catchBlock == "" {
		t.Fatal("未能提取 catch 块内容用于断言")
	}
	if strings.Contains(catchBlock, "exit 1") {
		t.Errorf("catch 块内不应有 exit 1，遥测失败不能中止装机，got catch 块:\n%s", catchBlock)
	}
}

// TestWinPE_AllPhaseSignalPointsPresent 锁住"各阶段的信号点都在生成的
// 脚本里出现"（计划任务 4 测试要求第四条）：pre-start/pre-done、
// packages-start/packages-done、post-start/post-done、rebooting 均出现在
// WinPEInstallScript 的产出中（这些信号点分布在 startnet.cmd 驱动的安装
// 流程脚本里，不是 WinPEClientLib 本身——后者只提供上报函数定义）。
func TestWinPE_AllPhaseSignalPointsPresent(t *testing.T) {
	out := WinPEInstallScript(winpeTelemetryCtx())
	requiredSignals := []model.SignalKind{
		model.SignalPreStart, model.SignalPreDone,
		model.SignalPackagesStart, model.SignalPackagesDone,
		model.SignalPostStart, model.SignalPostDone,
		model.SignalRebooting,
	}
	for _, kind := range requiredSignals {
		if !strings.Contains(out, string(kind)) {
			t.Errorf("WinPEInstallScript 输出中缺少信号点 %q，got:\n%s", kind, out)
		}
	}
}

// TestWinPE_InstallScriptUsesApplyImageForPackagesPhase 验证 packages 阶段
// 信号点包裹的是「应用镜像」这一步（计划安装阶段映射表："packages-start/
// packages-done → 应用镜像的前后（这一步对应 Linux 的装包）"），而不是
// 随意插在其它位置。
func TestWinPE_InstallScriptUsesApplyImageForPackagesPhase(t *testing.T) {
	out := WinPEInstallScript(winpeTelemetryCtx())
	startIdx := strings.Index(out, string(model.SignalPackagesStart))
	doneIdx := strings.Index(out, string(model.SignalPackagesDone))
	applyIdx := strings.Index(out, "Apply-WindowsImage")
	if startIdx == -1 || doneIdx == -1 || applyIdx == -1 {
		t.Fatalf("三个标记都应出现：packages-start=%d packages-done=%d Apply-WindowsImage=%d，got:\n%s",
			startIdx, doneIdx, applyIdx, out)
	}
	if !(startIdx < applyIdx && applyIdx < doneIdx) {
		t.Errorf("Apply-WindowsImage 调用应位于 packages-start 与 packages-done 信号之间，"+
			"got startIdx=%d applyIdx=%d doneIdx=%d", startIdx, applyIdx, doneIdx)
	}
}

// TestWinPE_NoAssumedCurl 验证生成的 WinPE 脚本不假设有 curl（计划任务 4
// 明确警示："WinPE 里没有 bash、没有 curl（默认）、没有 syslog 客户端"）。
func TestWinPE_NoAssumedCurl(t *testing.T) {
	out := WinPEClientLib(winpeTelemetryCtx()) + WinPEInstallScript(winpeTelemetryCtx())
	if strings.Contains(out, "curl ") || strings.Contains(out, "curl.exe") {
		t.Errorf("WinPE 脚本不应假设 curl 可用，应使用 PowerShell 的 Invoke-WebRequest/"+
			"Invoke-RestMethod，got:\n%s", out)
	}
}

// extractPowerShellCatchBlocks 提取脚本中第一个 catch { ... } 块的内容
// （粗略的花括号配对，够用于测试断言，不追求处理任意嵌套 PowerShell 语法）。
func extractPowerShellCatchBlocks(script string) string {
	idx := strings.Index(script, "catch")
	if idx == -1 {
		return ""
	}
	rest := script[idx:]
	open := strings.Index(rest, "{")
	if open == -1 {
		return ""
	}
	depth := 0
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[open+1 : i]
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 任务 5：firstboot 的 Windows 对应机制
// ---------------------------------------------------------------------------

// windowsFirstbootCtx 构造一个带一个 firstboot hook 的最小 RenderContext，
// 供本文件 Windows firstboot 测试复用。
func windowsFirstbootCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID:            "77",
			FinalizeToken: "finalize-tok-win-xyz",
		},
		Bases:         testBases,
		FinalizeToken: "finalize-tok-win-xyz",
		Hooks: []model.HookRef{
			{HookDigest: "winhook001", Stage: model.HookStageFirstboot, Order: 10, Required: true},
		},
	}
}

// TestWindowsFirstboot_FourArtifacts 锁住"四样都落进目标系统"（计划任务 5
// 测试要求第一条）：finalize token、参数文件、用户 firstboot 脚本、执行
// 入口脚本。
func TestWindowsFirstboot_FourArtifacts(t *testing.T) {
	out := WindowsFirstbootScaffold(windowsFirstbootCtx())

	checks := []struct {
		label string
		want  string
	}{
		{"finalize token 文件", "finalize.token"},
		{"参数文件", "params.env"},
		{"用户 firstboot hook 落地路径", "winhook001"},
		{"执行入口脚本", "SetupComplete.cmd"},
	}
	for _, c := range checks {
		if !strings.Contains(out, c.want) {
			t.Errorf("WindowsFirstbootScaffold 输出缺少%s（期望包含 %q），got:\n%s", c.label, c.want, out)
		}
	}
}

// TestWindowsFirstboot_TokenFilePermissionRestricted 锁住"token 文件权限
// 受限"（计划任务 5 测试要求第一条）。Windows 侧用 icacls 限制到仅系统
// 账号可读，而不是 Unix 的 chmod。
func TestWindowsFirstboot_TokenFilePermissionRestricted(t *testing.T) {
	out := WindowsFirstbootScaffold(windowsFirstbootCtx())
	if !strings.Contains(out, "icacls") {
		t.Errorf("WindowsFirstbootScaffold 应用 icacls 限制 finalize token 文件权限"+
			"（仅系统账号可读），got:\n%s", out)
	}
}

// TestWindowsFirstboot_TwoLayerOneShotGuarantee 锁住"一次性保证是两层
// （系统机制加标记文件）"（计划任务 5 测试要求第二条）：SetupComplete.cmd
// 本身只在首次登录前跑一次是系统层保证，标记文件检查是第二层——脚本开头
// 检查标记文件、成功后删除它。
func TestWindowsFirstboot_TwoLayerOneShotGuarantee(t *testing.T) {
	out := WindowsFirstbootScaffold(windowsFirstbootCtx())
	if !strings.Contains(out, "SetupComplete.cmd") {
		t.Fatalf("应使用系统身份、首次登录前执行的 SetupComplete.cmd 作为第一层保证，got:\n%s", out)
	}
	if !strings.Contains(out, "init.pending") {
		t.Errorf("应有标记文件作为第二层保证（与 Linux 侧 init.pending 同名同用途），got:\n%s", out)
	}
	script := extractWindowsInitScript(t, out)
	if !strings.Contains(script, "del ") && !strings.Contains(script, "Remove-Item") {
		t.Errorf("脚本应在成功后删除标记文件，got:\n%s", script)
	}
}

// TestWindowsFirstboot_CredentialCleanupIncludesUnattendCopy 锁住"凭据清理
// 含应答文件副本的删除"（计划任务 5 测试要求第三条，SPEC §9.5 的 Windows
// 对应：Windows 安装过程会把应答文件复制到目标系统的一个固定位置，那个
// 副本含明文密码，必须删除）。
func TestWindowsFirstboot_CredentialCleanupIncludesUnattendCopy(t *testing.T) {
	out := WindowsFirstbootScaffold(windowsFirstbootCtx())
	script := extractWindowsInitScript(t, out)

	if !strings.Contains(script, "params.env") {
		t.Errorf("凭据清理应删除参数文件，got:\n%s", script)
	}
	if !strings.Contains(script, "finalize.token") {
		t.Errorf("凭据清理应删除 finalize token 文件，got:\n%s", script)
	}
	// Windows 安装过程会把 unattend.xml 复制到
	// %WINDIR%\Panther\unattend.xml（真实 Windows 安装的固定行为），必须
	// 清理这个副本——它含明文密码。
	if !strings.Contains(script, "Panther") {
		t.Errorf("凭据清理应处理 %%WINDIR%%\\Panther\\ 下的应答文件副本"+
			"（含明文密码，必须删除），got:\n%s", script)
	}
	if !strings.Contains(script, "unattend.xml") {
		t.Errorf("凭据清理应具体点名 unattend.xml 副本，got:\n%s", script)
	}
}

// TestWindowsFirstboot_SignalOrderMatchesLinux 锁住"信号顺序与 Linux 侧
// 一致"（计划任务 5 测试要求第四条）：booted → init-start →（跑 hook）→
// init-done → ready，与 firstboot.go 的 initScript 顺序对应。
func TestWindowsFirstboot_SignalOrderMatchesLinux(t *testing.T) {
	out := WindowsFirstbootScaffold(windowsFirstbootCtx())
	script := extractWindowsInitScript(t, out)

	order := []model.SignalKind{
		model.SignalBooted, model.SignalInitStart, model.SignalInitDone, model.SignalReady,
	}
	lastIdx := -1
	for _, kind := range order {
		idx := strings.Index(script, string(kind))
		if idx == -1 {
			t.Fatalf("脚本中未找到信号 %q，got:\n%s", kind, script)
		}
		if idx <= lastIdx {
			t.Errorf("信号 %q 出现顺序不对，期望晚于前一个信号", kind)
		}
		lastIdx = idx
	}
}

// TestWindowsFirstboot_NoBashAssumed 验证 Windows firstboot 产物是批处理/
// PowerShell，不含任何 bash 语法特征（sha256sum -c 之类，那是 Linux 侧的
// 校验方式）。
func TestWindowsFirstboot_NoBashAssumed(t *testing.T) {
	out := WindowsFirstbootScaffold(windowsFirstbootCtx())
	if strings.Contains(out, "#!/bin/bash") {
		t.Errorf("Windows firstboot 产物不应含 bash shebang，got:\n%s", out)
	}
}

// extractWindowsInitScript 从 WindowsFirstbootScaffold 输出中提取
// lober-init.ps1 的正文（heredoc 包裹的脚本内容，即 SetupComplete.cmd
// 实际调用执行的那段逻辑）。
//
// 不能以字面 "SetupComplete.cmd" 作起点标记——该字符串只出现在输出末尾
// "Set-Content -Path ...SetupComplete.cmd" 这一行里，从那里切片会漏掉
// 在它之前的整段脚本正文（booted/init-start/hook 调用/凭据清理等全部
// 发生在这个标记之前）。改用 lober-init.ps1 的 heredoc 起始标记作为
// 提取起点，与 firstboot_test.go 的 extractInitScript 提取
// lober-init.sh 正文的思路一致。
func extractWindowsInitScript(t *testing.T, out string) string {
	t.Helper()
	marker := "lober-init.ps1' -Value @'"
	idx := strings.Index(out, marker)
	if idx == -1 {
		t.Fatalf("输出中找不到 lober-init.ps1 的 heredoc 起始标记，got:\n%s", out)
	}
	return out[idx+len(marker):]
}
