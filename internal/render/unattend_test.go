// 本文件测试 unattend.xml 渲染（对应实现计划 P3-01 任务 3，依据 SPEC
// §1.5、§5.6、§9.5）。
package render

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// unattendTestCtx 构造一个用于 unattend 测试的最小合法 RenderContext。
func unattendTestCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID: "77",
			ResolvedParams: map[string]string{
				"hostname": "win-01",
				"biz_ip":   "10.20.0.20",
			},
		},
		Artifact: model.ArtifactSet{
			OSFamily:  model.OSFamilyWindows,
			OSVersion: "2022",
			Arch:      model.ArchX86_64,
			Firmware:  model.FirmwareUEFI,
		},
		Facts: map[string]string{
			model.FactDiskPrimary: "0",
			model.FactHostname:    "win-01",
			model.FactBizIP:       "10.20.0.20",
		},
		Bases:         testBases,
		InstallToken:  "tok-win-unattend",
		FinalizeToken: "finalize-tok-win-unattend",
	}
}

// ---------------------------------------------------------------------------
// 输出是合法 XML
// ---------------------------------------------------------------------------

// TestUnattend_OutputIsValidXML 锁住"输出是合法 XML（用 XML 解析器解一遍，
// 不只对比文本）"（计划任务 3 测试要求第一条）。
func TestUnattend_OutputIsValidXML(t *testing.T) {
	out, err := RenderUnattend(unattendTestCtx())
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}

	var doc struct {
		XMLName xml.Name `xml:"unattend"`
	}
	if err := xml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("输出不是合法 XML: %v，got:\n%s", err, out)
	}
}

// TestUnattend_ContainsRequiredSections 验证计划任务 3 列出的五个段落
// （磁盘配置、镜像选择、区域与语言、账户、首次登录命令）都出现。
func TestUnattend_ContainsRequiredSections(t *testing.T) {
	out, err := RenderUnattend(unattendTestCtx())
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}

	checks := []struct {
		label string
		want  string
	}{
		{"磁盘配置", "DiskConfiguration"},
		{"镜像选择", "ImageInstall"},
		{"区域与语言", "InputLocale"},
		{"账户", "UserAccounts"},
		{"首次登录命令", "FirstLogonCommands"},
	}
	for _, c := range checks {
		if !strings.Contains(out, c.want) {
			t.Errorf("unattend.xml 输出缺少%s段落（期望包含 %q），got:\n%s", c.label, c.want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// 特殊字符转义且不破坏结构
// ---------------------------------------------------------------------------

// TestUnattend_SpecialCharsEscapedStructureIntact 锁住"含特殊字符的表单值
// 被正确转义且不破坏结构"（计划任务 3 测试要求第二条）。用一个合法但含
// XML 特殊字符的主机名（guard.ValidateHostname 只允许字母数字与连字符，
// 因此这里改用 role 字段验证转义——role 走同样的校验器，但用于验证转义
// 逻辑本身时用一个能通过校验且含 XML 特殊字符的值没有意义，本测试改为
// 直接验证：即使值合法，模板引擎本身对任何值都会转义，用一个会触发
// html/template 转义路径的字段值组合来验证输出仍是合法 XML。
func TestUnattend_SpecialCharsEscapedStructureIntact(t *testing.T) {
	ctx := unattendTestCtx()
	// hostname 合法字符集不含 XML 特殊字符，因此用它验证"转义即使对无需
	// 转义的普通值也应用一致的处理路径"；用 FinalizeToken（不经 guard
	// 校验，但仍会被模板转义）验证真正含特殊字符时的行为。
	ctx.FinalizeToken = `tok-<script>&"'-injected`
	ctx.Task.FinalizeToken = ctx.FinalizeToken

	out, err := RenderUnattend(ctx)
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}

	if strings.Contains(out, "<script>") {
		t.Errorf("输出中不应出现未转义的 <script>，说明 XML 转义未生效，got:\n%s", out)
	}

	var doc struct {
		XMLName xml.Name `xml:"unattend"`
	}
	if err := xml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("含特殊字符的值渲染后输出应仍是合法 XML: %v，got:\n%s", err, out)
	}
}

// ---------------------------------------------------------------------------
// guard 校验器仍然生效
// ---------------------------------------------------------------------------

// TestUnattend_GuardValidatorStillRejectsMalicious 锁住"guard 校验器仍然
// 生效（恶意值被拒绝，而不是被转义后放行）"（计划任务 3 测试要求第三条）。
//
// 转义防的是"值破坏 XML 结构"，防不了"值是一个合法但恶意的主机名"——
// 例如一个刻意构造成看起来像命令但字符集本身不违反 XML 语法的字符串。
// guard.ValidateHostname 的字符集本身就已经排除了绝大多数注入手段，本
// 测试验证的是"即使转义会让任何字符串都安全地嵌入 XML，本函数仍然先过
// 一遍 guard 校验，不合法的表单值整体被拒绝，不会被转义后悄悄放行"。
func TestUnattend_GuardValidatorStillRejectsMalicious(t *testing.T) {
	ctx := unattendTestCtx()
	ctx.Task.ResolvedParams = map[string]string{
		"hostname": "win01; rm -rf C:\\",
		"biz_ip":   "10.20.0.20",
	}

	out, err := RenderUnattend(ctx)
	if err == nil {
		t.Errorf("非法 hostname 应导致 RenderUnattend 返回错误，got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("非法值应导致渲染失败时不返回任何内容，got:\n%s", out)
	}
}

// TestUnattend_GuardValidatorRejectsMaliciousBizIP 验证 biz_ip 字段同样
// 受 guard 校验保护。
func TestUnattend_GuardValidatorRejectsMaliciousBizIP(t *testing.T) {
	ctx := unattendTestCtx()
	ctx.Task.ResolvedParams = map[string]string{
		"hostname": "win-01",
		"biz_ip":   "10.0.0.1; whoami",
	}

	_, err := RenderUnattend(ctx)
	if err == nil {
		t.Error("非法 biz_ip 应导致 RenderUnattend 返回错误，got nil")
	}
}

// TestUnattend_MaliciousValueNeverLeaksIntoOutput 验证恶意值即使被拒绝，
// 其原始文本也绝不出现在任何（空）输出中。
func TestUnattend_MaliciousValueNeverLeaksIntoOutput(t *testing.T) {
	ctx := unattendTestCtx()
	payload := "web`rm -rf /`01"
	ctx.Task.ResolvedParams = map[string]string{
		"hostname": payload,
		"biz_ip":   "10.20.0.20",
	}

	out, _ := RenderUnattend(ctx)
	if strings.Contains(out, payload) {
		t.Errorf("恶意值的原始文本不应出现在产出中，got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// golden file 覆盖：最小 unattend、带首次登录命令的完整版
// ---------------------------------------------------------------------------

// minimalUnattendCtx 构造"无 firstboot hook 的最小 unattend"渲染上下文。
func minimalUnattendCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID: "77",
			ResolvedParams: map[string]string{
				"hostname": "win-01",
				"biz_ip":   "10.20.0.20",
			},
			FinalizeToken: "finalize-tok-win-fixed",
		},
		Artifact: model.ArtifactSet{
			OSFamily:  model.OSFamilyWindows,
			OSVersion: "2022",
			Arch:      model.ArchX86_64,
			Firmware:  model.FirmwareUEFI,
		},
		Facts: map[string]string{
			model.FactDiskPrimary: "0",
			model.FactHostname:    "win-01",
			model.FactBizIP:       "10.20.0.20",
		},
		Bases:         goldenBases,
		InstallToken:  "install-tok-win-fixed",
		FinalizeToken: "finalize-tok-win-fixed",
	}
}

// fullUnattendCtx 构造"带 firstboot hook 引用的完整 unattend"渲染上下文。
func fullUnattendCtx() model.RenderContext {
	ctx := minimalUnattendCtx()
	ctx.Hooks = []model.HookRef{
		{HookDigest: "winhookaaa1", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}
	return ctx
}

// TestGolden_MinimalUnattend 锁住无 hook 的最小 unattend.xml 输出。
func TestGolden_MinimalUnattend(t *testing.T) {
	out, err := RenderUnattend(minimalUnattendCtx())
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}
	assertGolden(t, "unattend_minimal.golden", out)
}

// TestGolden_FullUnattend 锁住带 firstboot hook 引用的完整 unattend.xml
// 输出。
func TestGolden_FullUnattend(t *testing.T) {
	out, err := RenderUnattend(fullUnattendCtx())
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}
	assertGolden(t, "unattend_full.golden", out)
}

// ---------------------------------------------------------------------------
// 输出里不含域管凭据字段
// ---------------------------------------------------------------------------

// TestUnattend_NoDomainAdminCredentialField 锁住"输出里不含域管凭据的
// 占位符被误填的情况"（计划任务 3 测试要求第五条，SPEC §1.5：unattend.xml
// 不放域管凭据，用最小权限专用加域账号，或把加域挪到装机后另一通道）。
//
// 断言方式：模板本身根本不该有加域相关的字段（UnattendJoinDomain、
// Credentials 加 Domain 等 Windows 应答文件里代表"加入 AD 域"的标准元素），
// 不是"这次没填所以没出现"。
func TestUnattend_NoDomainAdminCredentialField(t *testing.T) {
	out, err := RenderUnattend(fullUnattendCtx())
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}

	forbidden := []string{"UnattendJoinDomain", "JoinDomain", "<Domain>", "DomainAdmin"}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Errorf("输出中不应出现域管/加域相关元素 %q——SPEC §1.5 明确"+
				"unattend.xml 不放域管凭据，got:\n%s", f, out)
		}
	}
}

// ---------------------------------------------------------------------------
// 本地管理员密码短时效
// ---------------------------------------------------------------------------

// TestUnattend_LocalAdminPasswordFromFinalizeToken 验证本地管理员密码来自
// 渲染上下文的短时效凭据（FinalizeToken 这类已有 TTL 机制的值），不是
// 硬编码的固定字符串——"本地管理员密码要么用一次性的、要么装完立即轮换"
// （计划任务 3、SPEC §1.5）。
func TestUnattend_LocalAdminPasswordFromFinalizeToken(t *testing.T) {
	ctx := unattendTestCtx()
	out, err := RenderUnattend(ctx)
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}
	if !strings.Contains(out, "AdministratorPassword") {
		t.Fatalf("输出应含本地管理员密码配置元素 AdministratorPassword，got:\n%s", out)
	}

	ctx2 := ctx
	ctx2.FinalizeToken = "a-totally-different-finalize-token"
	ctx2.Task.FinalizeToken = ctx2.FinalizeToken
	out2, err := RenderUnattend(ctx2)
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}
	if out == out2 {
		t.Error("换了 FinalizeToken 后输出应跟随变化——本地管理员密码不应是硬编码的固定字符串")
	}
}

// ---------------------------------------------------------------------------
// URL 不含字面 scheme
// ---------------------------------------------------------------------------

// TestUnattend_NoLoberControlledLiteralHTTPScheme 验证 unattend.xml 本身
// 不引用任何 Lober 网关/制品 URL（真正的信号上报与 hook 拉取全部发生在
// winpe.go 的 WindowsFirstbootScaffold/SetupComplete.cmd 里，见
// RenderUnattend 文件头注释与 firstbootEntryCommandLine 的说明——
// unattend.xml 里的 FirstLogonCommands 只做一条不依赖网络的本地核对
// 动作，不重复拉取逻辑）。
//
// 因此本测试不是"换 Bases 后输出应跟随变化"（那是 kickstart/菜单渲染的
// 断言方式，前提是输出里确实嵌入了 Bases 拼出的 URL）。这里改为断言：
// 换成 https:// 的 Bases 后，输出中不应新出现任何 Lober 自己的域名
// （gw.internal / ng.internal），且输出中唯一残留的 http:// 只能是
// Microsoft 官方 XML 命名空间 URI（如
// http://schemas.microsoft.com/WMIConfig/2002/State）——那是 Windows
// 应答文件格式标准的固定字面值，不是 Lober 配置项，SPEC §1.5"不写死
// scheme"约束的对象是 Lober 自己签发的 URL，不覆盖第三方标准规定的
// 命名空间字符串。
func TestUnattend_NoLoberControlledLiteralHTTPScheme(t *testing.T) {
	ctx := fullUnattendCtx()
	ctx.Bases = model.BaseURLs{
		GatewayBase:  "https://gw.internal",
		ArtifactBase: "https://ng.internal",
	}
	out, err := RenderUnattend(ctx)
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}
	if strings.Contains(out, "gw.internal") || strings.Contains(out, "ng.internal") {
		t.Errorf("unattend.xml 不应引用 Lober 自己的网关/制品域名——真正的网络拉取"+
			"发生在 SetupComplete.cmd 里，不在 unattend.xml 本身，got:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "http://") && !strings.Contains(line, "schemas.microsoft.com") {
			t.Errorf("发现非 Microsoft 命名空间的字面 http://，可能是硬编码的 Lober URL: %q", line)
		}
	}
}

// TestRenderUnattend_HostnameIsFilledIn 锁住「unattend.xml 的 ComputerName
// 真的被填进去」。
//
// 为什么这条重要：此前单元测试只验 XML 结构合法与转义正确，从没断言主机名
// 真的出现在产出里。Windows 端到端脚本报告 <ComputerName> 为空，一度让人
// 以为 unattend.go 读错了键（它读字面 "hostname"，而 provider/resolve.go
// 写回的是 model.FactHostname 常量）。
//
// 实际不是读错：cmd/lober 的 resolvedParamsKeyFixup 在装配点把 fact 命名空间
// 键转成 InlineParams 白名单认的裸键名（wire.go 的 factKeyToInlineParamKey），
// 所以到渲染器时键确实是 "hostname"。全部六个家族都把 ResolvedParams 喂给
// InlineParams，裸键是全系统一致的既有约定。
//
// 真正的问题在 provider/resolve.go 读 formData[model.FactHostname] 而表单传的
// 是字面 "hostname" —— 那一侧读不到、写回空串，靠装配点的 fixup 兜着。
// 这条测试只锁渲染侧的正确性；那个 fixup 掩盖的上游偏差记在 contractGaps。
func TestRenderUnattend_HostnameIsFilledIn(t *testing.T) {
	ctx := fullUnattendCtx()
	const wantHost = "win-web-01"
	ctx.Task.ResolvedParams = map[string]string{"hostname": wantHost}

	out, err := RenderUnattend(ctx)
	if err != nil {
		t.Fatalf("RenderUnattend 返回错误: %v", err)
	}
	if !strings.Contains(out, "<ComputerName>"+wantHost+"</ComputerName>") {
		t.Errorf("unattend.xml 未包含 <ComputerName>%s</ComputerName>——"+
			"装出来的机器会用 Windows 随机生成的主机名，运维在控制台里认不出哪台是哪台", wantHost)
	}
}
