// 本文件测试 ESXi 引导配置文件渲染与 ESXi 方言 kickstart 组装（对应实现
// 计划 docs/superpowers/plans/2026-09-20-lober-phase3-02-esxi.md 任务 2、
// 任务 3，依据 SPEC §0.3、§2.1、§4.5、§4.6、§5.2、§6.5）。
package render

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// 任务 2：RenderESXiBootCfg 测试
// ---------------------------------------------------------------------------

// rawBootCfgTemplate 是一份贴近真实 ESXi 8 boot.cfg 的原始模板（导入时从
// ISO 里原样取出、未经任何改写的版本）。modules= 行只列 4 个模块，真实文件
// 有几十个，但测试关心的是"顺序保持"与"前导斜杠去除"两条行为，4 个足以
// 覆盖多模块场景；build= 一行是一个本函数完全不识别的键，用来验证"未涉及
// 的行原样保留"。
const rawBootCfgTemplate = `bootstate=0
title=Loading ESXi installer
timeout=5
prefix=
kernel=/b.b00
kernelopt=runweasel ks=cdrom:/KS.CFG
modules=/jumpstrt.gz --- /useropts.gz --- /k.b00 --- /chain.b00
build=8.0.0-2000000
updated=0
`

func TestRenderESXiBootCfg_PrefixRewrittenToHTTP(t *testing.T) {
	out, err := RenderESXiBootCfg(rawBootCfgTemplate, "http://ng.internal/artifacts/esxi8", "http://gw.internal/v1/answer/42/tok/boot.cfg")
	if err != nil {
		t.Fatalf("RenderESXiBootCfg 返回错误: %v", err)
	}
	if !strings.Contains(out, "prefix=http://ng.internal/artifacts/esxi8") {
		t.Errorf("prefix= 行应被改写为 HTTP 路径，got:\n%s", out)
	}
	// 原始的空 prefix= 不应残留。
	if strings.Contains(out, "prefix=\n") {
		t.Errorf("原始的空 prefix= 不应残留，got:\n%s", out)
	}
}

func TestRenderESXiBootCfg_KernelOptGetsKsParam(t *testing.T) {
	answerURL := "http://gw.internal/v1/answer/42/tok-xyz/boot.cfg"
	out, err := RenderESXiBootCfg(rawBootCfgTemplate, "http://ng.internal/artifacts/esxi8", answerURL)
	if err != nil {
		t.Fatalf("RenderESXiBootCfg 返回错误: %v", err)
	}
	wantKsParam := "ks=" + answerURL
	if !strings.Contains(out, wantKsParam) {
		t.Errorf("kernelopt= 行应含 %q（附录 A.8：不设则脚本化安装无法启动），got:\n%s", wantKsParam, out)
	}
	// 原始占位值 ks=cdrom:/KS.CFG 必须被剔除，不能与新值同时出现——否则
	// 安装器会看到两个 ks= 参数，行为未定义。
	if strings.Contains(out, "cdrom:/KS.CFG") {
		t.Errorf("原始占位的 ks=cdrom:/KS.CFG 应被剔除，不能与新值共存，got:\n%s", out)
	}
	// runweasel 是原始 kernelopt 里 ks= 之外的选项，必须保留。
	if !strings.Contains(out, "runweasel") {
		t.Errorf("kernelopt= 原有的非 ks= 选项（runweasel）应被保留，got:\n%s", out)
	}
}

// TestRenderESXiBootCfg_ModulesLeadingSlashStripped 单独断言"模块路径的
// 前导斜杠被去掉"（计划任务 2 测试要求："第三项是最容易漏的，单独一条
// 断言"）。
func TestRenderESXiBootCfg_ModulesLeadingSlashStripped(t *testing.T) {
	out, err := RenderESXiBootCfg(rawBootCfgTemplate, "http://ng.internal/artifacts/esxi8", "http://gw.internal/v1/answer/42/tok/boot.cfg")
	if err != nil {
		t.Fatalf("RenderESXiBootCfg 返回错误: %v", err)
	}

	modulesLine := findLineWithPrefix(t, out, "modules=")
	if strings.Contains(modulesLine, "/jumpstrt.gz") || strings.Contains(modulesLine, " /useropts.gz") ||
		strings.Contains(modulesLine, " /k.b00") || strings.Contains(modulesLine, " /chain.b00") {
		t.Errorf("modules= 行中每个模块路径的前导斜杠都应被去掉（否则 prefix + 路径会拼出双斜杠，"+
			"导致引导器报模块取不到），got modules 行:\n%s", modulesLine)
	}
	want := "modules=jumpstrt.gz --- useropts.gz --- k.b00 --- chain.b00"
	if modulesLine != want {
		t.Errorf("modules= 行改写结果不符\ngot:  %s\nwant: %s", modulesLine, want)
	}
}

// TestRenderESXiBootCfg_ModulesOrderPreserved 验证模块顺序保持不变（计划
// 任务 1/2 均明确警示：顺序是引导要求，不能重排）。用一份模块名刻意不按
// 字母序排列的输入，确认输出顺序与输入完全一致（不是被意外排序）。
func TestRenderESXiBootCfg_ModulesOrderPreserved(t *testing.T) {
	template := "prefix=\n" +
		"kernelopt=runweasel\n" +
		"modules=/zzz.gz --- /aaa.gz --- /mmm.gz\n"
	out, err := RenderESXiBootCfg(template, "http://ng/artifacts/esxi8", "http://gw/v1/answer/42/tok/boot.cfg")
	if err != nil {
		t.Fatalf("RenderESXiBootCfg 返回错误: %v", err)
	}
	modulesLine := findLineWithPrefix(t, out, "modules=")
	want := "modules=zzz.gz --- aaa.gz --- mmm.gz"
	if modulesLine != want {
		t.Errorf("模块顺序必须与输入完全一致（引导要求，不能因为看起来更整齐而重排）\ngot:  %s\nwant: %s",
			modulesLine, want)
	}
}

// TestRenderESXiBootCfg_UnknownLinesPreservedVerbatim 锁住"未涉及的配置行
// 原样保留"（计划任务 2 测试要求，用一个含未知参数的输入验证）。
func TestRenderESXiBootCfg_UnknownLinesPreservedVerbatim(t *testing.T) {
	out, err := RenderESXiBootCfg(rawBootCfgTemplate, "http://ng.internal/artifacts/esxi8", "http://gw.internal/v1/answer/42/tok/boot.cfg")
	if err != nil {
		t.Fatalf("RenderESXiBootCfg 返回错误: %v", err)
	}
	for _, untouched := range []string{
		"bootstate=0",
		"title=Loading ESXi installer",
		"timeout=5",
		"build=8.0.0-2000000",
		"updated=0",
	} {
		if !strings.Contains(out, untouched) {
			t.Errorf("未涉及的行 %q 应原样保留（新 ESXi 版本加参数时不应被丢掉），got:\n%s", untouched, out)
		}
	}
}

// TestRenderESXiBootCfg_KernelLineUntouched 验证 kernel= 行本身不被三处
// 改写逻辑之一误改——kernel= 是键名，不在 prefix/kernelopt/modules 三者
// 之列，必须原样保留（真实引导器会把 prefix 与 kernel 拼在一起解析，本
// 函数不改写 kernel= 本身）。
func TestRenderESXiBootCfg_KernelLineUntouched(t *testing.T) {
	out, err := RenderESXiBootCfg(rawBootCfgTemplate, "http://ng.internal/artifacts/esxi8", "http://gw.internal/v1/answer/42/tok/boot.cfg")
	if err != nil {
		t.Fatalf("RenderESXiBootCfg 返回错误: %v", err)
	}
	if !strings.Contains(out, "kernel=/b.b00") {
		t.Errorf("kernel= 行不属于三处改写目标，应原样保留，got:\n%s", out)
	}
}

func TestRenderESXiBootCfg_MissingKernelOptRejected(t *testing.T) {
	template := "bootstate=0\nmodules=/a.gz\n"
	_, err := RenderESXiBootCfg(template, "http://ng/artifacts/esxi8", "http://gw/v1/answer/42/tok/boot.cfg")
	if err == nil {
		t.Fatal("原始模板缺少 kernelopt= 行时应返回错误，got nil")
	}
}

// TestRenderESXiBootCfg_MissingPrefixLineRejected 复查验证发现的回归点：
// 原始模板完全没有 prefix= 行时，三处必须改写之一（prefix）不存在，函数
// 之前会静默成功并产出一份没有 HTTP 前缀的 boot.cfg——引导器会去本地相对
// 路径找 kernel 与全部模块文件，找不到，且不会有任何报错指向这一步渲染
// 逻辑（计划任务 2 明确警示的故障模式）。现在必须像 kernelopt= 缺失那样
// 显式拒绝。
func TestRenderESXiBootCfg_MissingPrefixLineRejected(t *testing.T) {
	template := "kernelopt=runweasel\nmodules=/a.gz\n" // 无 prefix= 行
	_, err := RenderESXiBootCfg(template, "http://ng/artifacts/esxi8", "http://gw/v1/answer/42/tok/boot.cfg")
	if err == nil {
		t.Fatal("原始模板缺少 prefix= 行时应返回错误，got nil")
	}
	if !strings.Contains(err.Error(), "prefix") {
		t.Errorf("失败原因应提到 prefix，got: %v", err)
	}
}

// TestRenderESXiBootCfg_MissingModulesLineRejected 同上，针对 modules= 行
// 缺失的情形：原始模板没有 modules= 行时，函数之前会静默成功并产出一份
// 没有任何模块清单的 boot.cfg——ESXi 引导到某一步会因为找不到模块而停住。
func TestRenderESXiBootCfg_MissingModulesLineRejected(t *testing.T) {
	template := "prefix=\nkernelopt=runweasel\n" // 无 modules= 行
	_, err := RenderESXiBootCfg(template, "http://ng/artifacts/esxi8", "http://gw/v1/answer/42/tok/boot.cfg")
	if err == nil {
		t.Fatal("原始模板缺少 modules= 行时应返回错误，got nil")
	}
	if !strings.Contains(err.Error(), "modules") {
		t.Errorf("失败原因应提到 modules，got: %v", err)
	}
}

func TestRenderESXiBootCfg_EmptyURLBaseRejected(t *testing.T) {
	_, err := RenderESXiBootCfg(rawBootCfgTemplate, "", "http://gw/v1/answer/42/tok/boot.cfg")
	if err == nil {
		t.Fatal("bootCfgURLBase 为空时应返回错误，got nil")
	}
}

func TestRenderESXiBootCfg_EmptyAnswerURLRejected(t *testing.T) {
	_, err := RenderESXiBootCfg(rawBootCfgTemplate, "http://ng/artifacts/esxi8", "")
	if err == nil {
		t.Fatal("answerFileURL 为空时应返回错误，got nil")
	}
}

// TestRenderESXiBootCfg_AnswerFileURLFromBaseWithToken 验证应答文件 URL
// 拼接形态：从基址拼出、token 在路径段（计划任务 2 测试要求）。本测试模拟
// 调用方（menu.go 的 ESXi 分支）按 RHEL 侧同样的方式拼出这个 URL，确认
// RenderESXiBootCfg 原样把它塞进 kernelopt 行、不做任何二次改写。
func TestRenderESXiBootCfg_AnswerFileURLFromBaseWithToken(t *testing.T) {
	bases := model.BaseURLs{GatewayBase: "http://gw.internal", ArtifactBase: "http://ng.internal"}
	answerURL := bases.GatewayURL("v1", "answer", "42", "install-tok-abc", "boot.cfg")
	if !strings.Contains(answerURL, "/v1/answer/42/install-tok-abc/boot.cfg") {
		t.Fatalf("测试前提不满足：拼出的 answerURL 形态不对，got %q", answerURL)
	}

	out, err := RenderESXiBootCfg(rawBootCfgTemplate, bases.ArtifactURL("esxi8"), answerURL)
	if err != nil {
		t.Fatalf("RenderESXiBootCfg 返回错误: %v", err)
	}
	if !strings.Contains(out, "ks="+answerURL) {
		t.Errorf("kernelopt 行应含从基址拼出、token 在路径段的应答文件 URL，got:\n%s", out)
	}
}

// findLineWithPrefix 返回 text 中以 prefix 开头的第一行（不含换行符）。
func findLineWithPrefix(t *testing.T, text, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("在文本中找不到以 %q 开头的行，全文:\n%s", prefix, text)
	return ""
}

// ---------------------------------------------------------------------------
// RenderESXiMenuLine 测试（含 BIOS + ESXi 组合被拒绝的明确断言）
// ---------------------------------------------------------------------------

// esxiMenuCtx 构造一个 ESXi 家族的最小 RenderContext，供菜单行渲染测试
// 复用。
func esxiMenuCtx(firmware model.FirmwareMode) model.RenderContext {
	return model.RenderContext{
		Task: model.Task{ID: "42"},
		Artifact: model.ArtifactSet{
			OSFamily: model.OSFamilyESXi,
			Firmware: model.FirmwareUEFI,
		},
		Bases:          testBases,
		InstallToken:   "tok-menu-esxi",
		ActualFirmware: firmware,
	}
}

func TestRenderESXiMenuLine_UEFISucceeds(t *testing.T) {
	ctx := esxiMenuCtx(model.FirmwareUEFI)
	answerURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, "boot.cfg")
	out, err := RenderESXiMenuLine(ctx, "esxi8/mboot.efi", answerURL)
	if err != nil {
		t.Fatalf("UEFI 机器渲染 ESXi 菜单行应成功，got err=%v", err)
	}
	if !strings.HasPrefix(out, "kernel ") {
		t.Errorf("ESXi 菜单行应以 kernel 开头，got:\n%s", out)
	}
	if strings.Contains(out, "initrd") {
		t.Errorf("ESXi 菜单不应含 initrd 行（模块由 boot.cfg 的 modules= 列出，不走 iPXE 的 initrd 机制），got:\n%s", out)
	}
	if !strings.Contains(out, "-c "+answerURL) {
		t.Errorf("ESXi 菜单行应含 `-c <boot.cfg URL>` 选项，got:\n%s", out)
	}
}

// TestRenderESXiMenuLine_BIOSRejected 是计划要求的明确断言：
// "BIOS + ESXi 组合被拒绝"（docs/superpowers/plans/
// 2026-09-20-lober-phase3-02-esxi.md 验证清单）。
//
// 这是渲染层的纵深防御——第一道防线在
// internal/statemachine/precheck.go 的 checkFirmware（用
// model.FirmwareCompatible(esxi, bios) == false 在签发 token 前就拒绝，
// 该文件不在本次分配范围内，不能在本测试里直接调用，但本测试独立确认：
// 即使调用方绕过了 precheck 直接调渲染器，本函数自己也会拒绝，不产出一份
// 注定失败的菜单。
func TestRenderESXiMenuLine_BIOSRejected(t *testing.T) {
	ctx := esxiMenuCtx(model.FirmwareBIOS)
	answerURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, "boot.cfg")

	out, err := RenderESXiMenuLine(ctx, "esxi8/mboot.efi", answerURL)
	if err == nil {
		t.Fatalf("BIOS 机器请求 ESXi 菜单应被拒绝，got nil error, out=%q", out)
	}
	if !errors.Is(err, ErrESXiRequiresUEFI) {
		t.Errorf("拒绝原因应是 ErrESXiRequiresUEFI（供调用方用 errors.Is 判定），got err=%v", err)
	}
	if out != "" {
		t.Errorf("BIOS 机器被拒绝时不应返回任何菜单内容，got:\n%s", out)
	}
}

// TestRenderESXiMenuLine_ModelFirmwareMatrixAgreesWithRejection 交叉验证
// 本函数的拒绝逻辑与 M1 冻结的固件兼容矩阵（model.FirmwareCompatible）
// 结论一致——不是本文件自说自话定义了一条与契约矛盾的规则。这是
// SPEC §0.3、任务描述里"FirmwareCompatible(esxi, bios) 已经返回 false，
// 这是 M1 冻结的"这条事实的直接验证。
func TestRenderESXiMenuLine_ModelFirmwareMatrixAgreesWithRejection(t *testing.T) {
	if model.FirmwareCompatible(model.OSFamilyESXi, model.FirmwareBIOS) {
		t.Fatal("测试前提不满足：model.FirmwareCompatible(esxi, bios) 应为 false（M1 冻结），" +
			"若这条断言失败说明 model 层的固件矩阵变了，本文件的 UEFI-only 拒绝逻辑需要重新核对")
	}
	if !model.FirmwareCompatible(model.OSFamilyESXi, model.FirmwareUEFI) {
		t.Fatal("测试前提不满足：model.FirmwareCompatible(esxi, uefi) 应为 true")
	}

	_, errBIOS := RenderESXiMenuLine(esxiMenuCtx(model.FirmwareBIOS), "esxi8/mboot.efi", "http://gw/v1/answer/42/tok/boot.cfg")
	_, errUEFI := RenderESXiMenuLine(esxiMenuCtx(model.FirmwareUEFI), "esxi8/mboot.efi", "http://gw/v1/answer/42/tok/boot.cfg")
	if errBIOS == nil {
		t.Error("与 model.FirmwareCompatible 矩阵一致：BIOS 应被拒绝")
	}
	if errUEFI != nil {
		t.Errorf("与 model.FirmwareCompatible 矩阵一致：UEFI 应成功，got err=%v", errUEFI)
	}
}

func TestRenderESXiMenuLine_MissingMbootPathRejected(t *testing.T) {
	ctx := esxiMenuCtx(model.FirmwareUEFI)
	_, err := RenderESXiMenuLine(ctx, "", "http://gw/v1/answer/42/tok/boot.cfg")
	if err == nil {
		t.Fatal("mbootEFIPath 为空时应返回错误，got nil")
	}
}

func TestRenderESXiMenuLine_MissingAnswerURLRejected(t *testing.T) {
	ctx := esxiMenuCtx(model.FirmwareUEFI)
	_, err := RenderESXiMenuLine(ctx, "esxi8/mboot.efi", "")
	if err == nil {
		t.Fatal("answerFileURL 为空时应返回错误，got nil")
	}
}

func TestRenderESXiMenuLine_NoLiteralHTTPScheme(t *testing.T) {
	ctx := esxiMenuCtx(model.FirmwareUEFI)
	ctx.Bases = model.BaseURLs{GatewayBase: "https://gw.internal", ArtifactBase: "https://ng.internal"}
	answerURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, "boot.cfg")

	out, err := RenderESXiMenuLine(ctx, "esxi8/mboot.efi", answerURL)
	if err != nil {
		t.Fatalf("RenderESXiMenuLine 返回错误: %v", err)
	}
	if strings.Contains(out, "http://") {
		t.Errorf("Bases 配置为 https:// 时，ESXi 菜单行输出仍含字面 http://，got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// golden file：完整 boot.cfg 改写结果（计划任务 2 测试要求最后一条）
// ---------------------------------------------------------------------------

func TestGolden_ESXiBootCfg(t *testing.T) {
	out, err := RenderESXiBootCfg(rawBootCfgTemplate,
		"http://ng.internal/artifacts/esxi8",
		"http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/boot.cfg")
	if err != nil {
		t.Fatalf("RenderESXiBootCfg 返回错误: %v", err)
	}
	assertGolden(t, "esxi_bootcfg.golden", out)
}

// ---------------------------------------------------------------------------
// 任务 3：ESXi 方言 kickstart 测试
// ---------------------------------------------------------------------------

// esxiKickstartCtx 构造一个 ESXi 家族的最小 RenderContext，供 kickstart
// 组装测试复用。
func esxiKickstartCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID:            "42",
			HostSN:        "SN-ESXI-0001",
			FinalizeToken: "finalize-tok-esxi",
		},
		Template: model.Template{
			ID: "tpl-esxi8-minimal",
		},
		Artifact: model.ArtifactSet{
			OSFamily:  model.OSFamilyESXi,
			OSVersion: "8.0",
			Arch:      model.ArchX86_64,
			Firmware:  model.FirmwareUEFI,
			Paths: model.ArtifactPaths{
				Extra: map[string]string{
					"mboot.efi": "esxi8/mboot.efi",
					"boot.cfg":  "esxi8/boot.cfg",
				},
			},
		},
		Facts: map[string]string{
			model.FactTaskID:      "42",
			model.FactSN:          "SN-ESXI-0001",
			model.FactHostname:    "esxi-01",
			model.FactDiskPrimary: "/dev/disks/t10.NVMe_fixed0001",
			model.FactOSFamily:    string(model.OSFamilyESXi),
		},
		Bases:         goldenBases,
		InstallToken:  "install-tok-esxi-fixed",
		FinalizeToken: "finalize-tok-esxi",
		SyslogHost:    "10.10.0.1",
		SyslogPort:    514,
	}
}

func TestAssembleESXiKickstart_UsesNativeFirstboot(t *testing.T) {
	out, err := AssembleESXiKickstart(esxiKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleESXiKickstart 返回错误: %v", err)
	}
	if !strings.Contains(out, "%firstboot") {
		t.Errorf("ESXi kickstart 应使用原生 %%firstboot 段落（不需要 systemd oneshot 脚手架），got:\n%s", out)
	}
	// 不应出现 RHEL 侧 firstboot 脚手架的 systemd 特征——那套机制是给
	// Linux systemd 用的，ESXi 没有 systemd。
	for _, rhelOnly := range []string{"systemd", "lober-init.service", "ConditionPathExists"} {
		if strings.Contains(out, rhelOnly) {
			t.Errorf("ESXi kickstart 不应出现 RHEL 侧 systemd firstboot 脚手架的特征 %q"+
				"（ESXi 原生支持 %%firstboot，不需要这套机制），got:\n%s", rhelOnly, out)
		}
	}
}

func TestAssembleESXiKickstart_NoPostNochrootSection(t *testing.T) {
	ctx := esxiKickstartCtx()
	ctx.Hooks = []model.HookRef{
		{HookDigest: "aaaa", Stage: model.HookStagePre, Order: 10, Required: true},
		{HookDigest: "bbbb", Stage: model.HookStagePost, Order: 10, Required: true},
	}
	out, err := AssembleESXiKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleESXiKickstart 返回错误: %v", err)
	}
	if strings.Contains(out, "--nochroot") {
		t.Errorf("ESXi kickstart 不应出现 post-nochroot 段落（SPEC §5.2：该阶段在 ESXi 上不存在），got:\n%s", out)
	}
}

// TestAssembleESXiKickstart_PostNochrootHookSilentlyDropped 锁住"配了
// post-nochroot 阶段 hook 的 ESXi 模板"在渲染层的纵深防御行为：该 hook
// 不会导致渲染出一个 ESXi 不认识的 `%post --nochroot` 段落头（那会让整个
// kickstart 解析失败），而是被安全地排除在输出之外。真正的拒绝点应在 M6
// 的模板保存校验（见 esxi.go 文件末尾契约缺口 3），本测试只确认渲染层不会
// 因为这类非法输入产出一个更危险的畸形文件。
func TestAssembleESXiKickstart_PostNochrootHookSilentlyDropped(t *testing.T) {
	ctx := esxiKickstartCtx()
	ctx.Hooks = []model.HookRef{
		{HookDigest: "cccc-nochroot", Stage: model.HookStagePostNoChroot, Order: 10, Required: true},
	}
	out, err := AssembleESXiKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleESXiKickstart 返回错误: %v", err)
	}
	if strings.Contains(out, "cccc-nochroot") {
		t.Errorf("post-nochroot 阶段的 hook digest 不应出现在 ESXi kickstart 输出中"+
			"（该阶段在 ESXi 上不存在），got:\n%s", out)
	}
	if strings.Contains(out, "--nochroot") {
		t.Errorf("不应渲染出 ESXi 不认识的 %%post --nochroot 段落头，got:\n%s", out)
	}
}

func TestAssembleESXiKickstart_FourSignalsPresent(t *testing.T) {
	out, err := AssembleESXiKickstart(esxiKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleESXiKickstart 返回错误: %v", err)
	}
	for _, sig := range []string{
		`signal "booted"`, `signal "init-start"`, `signal "init-done"`, `signal "ready"`,
	} {
		if !strings.Contains(out, sig) {
			t.Errorf("ESXi 原生 firstboot 段落应发出信号 %q（信号语义与 RHEL 侧保持一致），got:\n%s", sig, out)
		}
	}
}

func TestAssembleESXiKickstart_SyslogUsesEsxcli(t *testing.T) {
	out, err := AssembleESXiKickstart(esxiKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleESXiKickstart 返回错误: %v", err)
	}
	if !strings.Contains(out, "esxcli system syslog config set") {
		t.Errorf("ESXi 的 syslog 配置应使用 esxcli（SPEC §6.5 末段），不是 kickstart 的 logging 指令，got:\n%s", out)
	}
	if strings.Contains(out, "logging --host=") {
		t.Errorf("ESXi kickstart 不应出现 RHEL 专属的 `logging --host=` 指令，got:\n%s", out)
	}
}

func TestAssembleESXiKickstart_InstallCommandUsesDiskFact(t *testing.T) {
	out, err := AssembleESXiKickstart(esxiKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleESXiKickstart 返回错误: %v", err)
	}
	if !strings.Contains(out, "/dev/disks/t10.NVMe_fixed0001") {
		t.Errorf("install 指令应使用 LOBER_DISK_PRIMARY 服务端决策结果，不能硬编码磁盘名，got:\n%s", out)
	}
}

// TestAssembleESXiKickstart_MaliciousHostnameRejected 复用与 RHEL/注入测试
// 同一套 guard 校验路径（InlineParams），确认 ESXi 侧同样不会让恶意表单
// 值进入以 root 身份执行的脚本。
func TestAssembleESXiKickstart_MaliciousHostnameRejected(t *testing.T) {
	ctx := esxiKickstartCtx()
	ctx.Task.ResolvedParams = map[string]string{"hostname": "web01; rm -rf /"}
	out, err := AssembleESXiKickstart(ctx)
	if err == nil {
		t.Fatalf("恶意 hostname 应导致 AssembleESXiKickstart 返回错误，got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("恶意 hostname 导致渲染失败时不应返回任何内容，got:\n%s", out)
	}
}

func TestAssembleESXiKickstart_NoLiteralHTTPScheme(t *testing.T) {
	ctx := esxiKickstartCtx()
	ctx.Bases = model.BaseURLs{GatewayBase: "https://gw.internal", ArtifactBase: "https://ng.internal"}
	ctx.Hooks = []model.HookRef{
		{HookDigest: "firstboot-hook-1", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}
	out, err := AssembleESXiKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleESXiKickstart 返回错误: %v", err)
	}
	if strings.Contains(out, "http://") {
		t.Errorf("Bases 配置为 https:// 时，ESXi kickstart 输出仍含字面 http://，got:\n%s", out)
	}
}

// TestGolden_ESXiKickstart 锁住带 pre/post/firstboot 三阶段 hook 的完整
// ESXi kickstart 输出。
func TestGolden_ESXiKickstart(t *testing.T) {
	ctx := esxiKickstartCtx()
	ctx.Hooks = []model.HookRef{
		{HookDigest: "aaaa1111pre", Stage: model.HookStagePre, Order: 10, Required: true},
		{HookDigest: "cccc3333post", Stage: model.HookStagePost, Order: 10, Required: true},
		{HookDigest: "dddd4444firstboot", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}
	out, err := AssembleESXiKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleESXiKickstart 返回错误: %v", err)
	}
	assertGolden(t, "esxi_kickstart_full.golden", out)
}

// ---------------------------------------------------------------------------
// ClientLibESXi 测试
// ---------------------------------------------------------------------------

func esxiClientLibCtx() model.RenderContext {
	return model.RenderContext{
		Task:         model.Task{ID: "42"},
		Bases:        testBases,
		InstallToken: "tok-abc123",
	}
}

// TestClientLibESXi_NoPipestatus 锁住"ash 兼容实现不依赖 PIPESTATUS"
// （ash 没有这个 bash 数组扩展；RHEL 侧的 ClientLib 依赖它，ash 版必须换
// 一种取真实退出码的方式）。
func TestClientLibESXi_NoPipestatus(t *testing.T) {
	out := ClientLibESXi(esxiClientLibCtx())
	if strings.Contains(out, "PIPESTATUS") {
		t.Errorf("ash 版 ClientLib 不应使用 PIPESTATUS（ash 没有这个 bash 扩展），got:\n%s", out)
	}
}

// TestClientLibESXi_RunHookNoPipeToTee 验证 run_hook 直接重定向到文件而
// 不经过 `| tee`——这是不依赖 PIPESTATUS 就能拿到真实退出码的手段。
func TestClientLibESXi_RunHookNoPipeToTee(t *testing.T) {
	out := ClientLibESXi(esxiClientLibCtx())
	fnBody := extractFunctionBody(t, out, "run_hook")
	if strings.Contains(fnBody, "| tee") {
		t.Errorf("ash 版 run_hook 不应通过 `| tee` 管道执行（那需要 PIPESTATUS 才能拿到真实退出码，"+
			"ash 没有该扩展），应直接重定向到文件，got:\n%s", fnBody)
	}
	if !strings.Contains(fnBody, "> /tmp/h.log 2>&1") {
		t.Errorf("ash 版 run_hook 应直接把 stdout/stderr 重定向到日志文件，got:\n%s", fnBody)
	}
}

func TestClientLibESXi_SignalRetryLoop(t *testing.T) {
	out := ClientLibESXi(esxiClientLibCtx())
	fnBody := extractFunctionBody(t, out, "signal")
	if !strings.Contains(fnBody, "while [ \"$_lc_i\" -le 3 ]") {
		t.Errorf("ash 版 signal 应包含三次重试循环，got:\n%s", fnBody)
	}
	if !strings.Contains(fnBody, "sleep $((_lc_i*3))") {
		t.Errorf("ash 版 signal 应包含退避表达式（第 i 次等 3i 秒），got:\n%s", fnBody)
	}
}

func TestClientLibESXi_SignalReturnsSuccess(t *testing.T) {
	out := ClientLibESXi(esxiClientLibCtx())
	fnBody := extractFunctionBody(t, out, "signal")
	trimmed := strings.TrimRight(fnBody, "\n \t")
	lines := strings.Split(trimmed, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last != "return 0" {
		t.Errorf("ash 版 signal 函数体最后一行 = %q, want \"return 0\"（遥测失败不能杀装机），got 全文:\n%s",
			last, fnBody)
	}
}

func TestClientLibESXi_RunHookChecksumVerify(t *testing.T) {
	out := ClientLibESXi(esxiClientLibCtx())
	fnBody := extractFunctionBody(t, out, "run_hook")
	if !strings.Contains(fnBody, "sha256sum -c") {
		t.Errorf("ash 版 run_hook 应用 sha256sum -c 校验下载到的脚本，got:\n%s", fnBody)
	}
}

func TestClientLibESXi_RunHookRequiredGate(t *testing.T) {
	out := ClientLibESXi(esxiClientLibCtx())
	fnBody := extractFunctionBody(t, out, "run_hook")
	if !strings.Contains(fnBody, "hook-report") || !strings.Contains(fnBody, "required") {
		t.Errorf("ash 版 run_hook 应上报 hook-report 并按 required 决定是否中止，got:\n%s", fnBody)
	}
}

func TestClientLibESXi_NoLiteralHTTPScheme(t *testing.T) {
	httpsCtx := esxiClientLibCtx()
	httpsCtx.Bases = model.BaseURLs{GatewayBase: "https://gw.internal", ArtifactBase: "https://ng.internal"}
	out := ClientLibESXi(httpsCtx)
	if strings.Contains(out, "http://") {
		t.Errorf("Bases 配置为 https:// 时，ClientLibESXi 输出仍含字面 http://，got:\n%s", out)
	}
}

// TestClientLibESXi_RealAshExecution 用真实 ash 子进程执行 ClientLibESXi
// 产出的函数定义，验证语法本身在真实 ash 下有效、signal 与 run_hook 都能
// 被调用而不报"command not found"（与 kickstart_exec_test.go 对 RHEL 侧
// ClientLib 的验证方式同构：不满足于字符串包含断言，用真实解释器确认）。
//
// 本测试不发起真实网络请求（会因 curl 访问不存在的地址而走到 signal 的
// 重试与超时路径，耗时且不稳定），只验证：把 signal 函数体单独抽出并
// 替换掉其中的 curl 调用为一个总是成功的占位命令后，函数定义本身能被
// ash 解析、调用、并正确处理 digest 参数的有无两种路径。
func TestClientLibESXi_RealAshExecution(t *testing.T) {
	if _, err := exec.LookPath("ash"); err != nil {
		t.Skip("本机没有 ash 可执行文件，跳过真实执行验证")
	}

	out := ClientLibESXi(esxiClientLibCtx())
	// 把 curl 换成一个总是成功、且能验证参数确实被传递的占位命令，避免
	// 测试依赖真实网络。sha256sum 保留真实调用——用一个已知内容验证。
	script := strings.ReplaceAll(out,
		`curl -fsS -m 10 -X POST "$_lc_q" \
      -H 'Content-Type: text/plain' --data-binary "@$_lc_log" && return 0`,
		`echo "posted: $_lc_q" && return 0`)

	script += "\n" +
		"echo hello > /tmp/lober_ash_test.log\n" +
		"signal \"booted\" 0 /tmp/lober_ash_test.log\n" +
		"signal \"hook-report\" 1 /tmp/lober_ash_test.log \"deadbeef\"\n"

	cmd := exec.Command("ash", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ash 执行失败: %v\n输出:\n%s", err, output)
	}
	got := string(output)
	if !strings.Contains(got, "posted:") {
		t.Errorf("signal 函数应被成功调用（占位 curl 被替换为 echo），got 输出:\n%s", got)
	}
	if !strings.Contains(got, "digest=deadbeef") {
		t.Errorf("带 digest 参数调用 signal 时，query 中应出现 digest=deadbeef，got 输出:\n%s", got)
	}
	if strings.Contains(got, "not found") || strings.Contains(got, "Syntax error") {
		t.Errorf("ash 执行 ClientLibESXi 产出的脚本时出现语法或命令找不到的错误，got:\n%s", got)
	}
}

// TestClientLibESXi_RealAshRunHookRequiredFailureExits 用真实 ash 验证
// run_hook 在"required hook 失败"时确实返回非零（与 clientlib_test.go 里
// 对 RHEL 侧 run_hook 同一行为的字符串断言相对，这里用真实执行验证 ash
// 版本没有因为改用直接重定向而丢失这条安全不变量）。
func TestClientLibESXi_RealAshRunHookRequiredFailureExits(t *testing.T) {
	if _, err := exec.LookPath("ash"); err != nil {
		t.Skip("本机没有 ash 可执行文件，跳过真实执行验证")
	}

	out := ClientLibESXi(esxiClientLibCtx())
	// 屏蔽真实网络调用：把 curl 下载替换为直接写一个已知内容的文件，
	// 校验值与之匹配；把 signal 调用替换为无操作，专注验证 run_hook 自身
	// 的 required 判定逻辑。
	script := strings.ReplaceAll(out,
		`curl -fsS -o /tmp/h.sh "$ART/hooks/$_lc_digest/script.sh" || return 1`,
		`echo 'exit 7' > /tmp/h.sh || return 1`)
	script = strings.ReplaceAll(script,
		`echo "$_lc_digest  /tmp/h.sh" | sha256sum -c - || return 1`,
		`true`) // 跳过校验，因为占位脚本内容与真实 digest 不匹配
	script = strings.ReplaceAll(script,
		`signal "hook-report" "$_lc_rc" /tmp/h.log "$_lc_digest"`,
		`echo "would-signal rc=$_lc_rc"`)

	script += "\n" +
		"run_hook anydigest true 5\n" +
		"echo \"run_hook_exit=$?\"\n"

	cmd := exec.Command("ash", "-c", script)
	output, _ := cmd.CombinedOutput()
	got := string(output)
	if !strings.Contains(got, "run_hook_exit=1") {
		t.Errorf("required hook 失败（脚本以 exit 7 退出）时，run_hook 应返回非零码，"+
			"got 输出:\n%s", got)
	}
}
