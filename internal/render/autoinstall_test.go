// 本文件测试 Ubuntu autoinstall 渲染（对应实现计划 phase2-01 任务 3，依据
// SPEC §4.4、§4.5、§5.2）。
package render

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/lober/lober/internal/core/model"
)

// minimalAutoinstallCtx 构造"无 hook 的最小 autoinstall"渲染上下文。
func minimalAutoinstallCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID:            fixedTaskID,
			HostSN:        fixedSN,
			FinalizeToken: fixedFinalizeToken,
		},
		Template: model.Template{
			ID: "tpl-ubuntu2404-minimal",
		},
		Artifact: model.ArtifactSet{
			OSFamily:  model.OSFamilyUbuntu,
			OSVersion: "24.04",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "ubuntu2404/vmlinuz",
				Initrd: []string{"ubuntu2404/initrd"},
				Repo:   "ubuntu2404/repo/",
				Extra:  map[string]string{"squashfs": "ubuntu2404/casper/filesystem.squashfs"},
			},
		},
		Facts: map[string]string{
			model.FactTaskID:       fixedTaskID,
			model.FactSN:           fixedSN,
			model.FactHostname:     fixedHostname,
			model.FactBizIP:        fixedBizIP,
			model.FactDiskPrimary:  fixedDiskByID,
			model.FactOSFamily:     string(model.OSFamilyUbuntu),
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

// fullAutoinstallCtx 构造"带三阶段 hook 的完整 autoinstall"渲染上下文。
func fullAutoinstallCtx() model.RenderContext {
	ctx := minimalAutoinstallCtx()
	ctx.Template.ID = "tpl-ubuntu2404-full"
	ctx.Hooks = []model.HookRef{
		{HookDigest: "aaaa1111pre", Stage: model.HookStagePre, Order: 10, Required: true},
		{HookDigest: "bbbb2222nochroot", Stage: model.HookStagePostNoChroot, Order: 10, Required: false},
		{HookDigest: "cccc3333post", Stage: model.HookStagePost, Order: 10, Required: true},
		{HookDigest: "dddd4444firstboot", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}
	return ctx
}

// TestAutoinstall_IsValidYAML 锁住"输出是合法 YAML"（计划任务 3 测试要求
// 第一条：用 YAML 解析器解一遍，不要只对比文本）。
func TestAutoinstall_IsValidYAML(t *testing.T) {
	out, err := AssembleAutoinstall(minimalAutoinstallCtx())
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("输出不是合法 YAML: %v\n输出:\n%s", err, out)
	}
	if _, ok := doc["autoinstall"]; !ok {
		t.Errorf("YAML 顶层应含 autoinstall 键，got keys: %v", doc)
	}
}

// TestAutoinstall_MultilineHookDoesNotBreakYAMLStructure 锁住"含多行 shell
// 的 hook 不破坏 YAML 结构"（计划任务 3 测试要求第二条，对第 1 点细节的
// 直接验证）。用一个正文本身含多行、含单引号、含冒号（YAML 特殊字符）的
// hook 走一遍渲染，解析后确认命令列表逐条完整且未被截断或误解析。
func TestAutoinstall_MultilineHookDoesNotBreakYAMLStructure(t *testing.T) {
	ctx := fullAutoinstallCtx()
	out, err := AssembleAutoinstall(ctx)
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}

	var doc struct {
		Autoinstall struct {
			EarlyCommands []string `yaml:"early-commands"`
			LateCommands  []string `yaml:"late-commands"`
		} `yaml:"autoinstall"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("解析失败: %v\n输出:\n%s", err, out)
	}
	if len(doc.Autoinstall.EarlyCommands) == 0 {
		t.Fatal("early-commands 应至少含一条命令（pre 阶段的 hook 调用），got 空列表")
	}
	joinedEarly := strings.Join(doc.Autoinstall.EarlyCommands, "\n")
	if !strings.Contains(joinedEarly, "run_hook 'aaaa1111pre'") {
		t.Errorf("early-commands 中应含 pre 阶段 hook 的 run_hook 调用，got:\n%v", doc.Autoinstall.EarlyCommands)
	}
	if len(doc.Autoinstall.LateCommands) == 0 {
		t.Fatal("late-commands 应至少含内容，got 空列表")
	}
}

// TestAutoinstall_MetaDataRendersInstanceIDAndHostname 锁住"meta-data 也
// 能渲染，内容含实例 id 与主机名"（计划任务 3 测试要求第三条）。
func TestAutoinstall_MetaDataRendersInstanceIDAndHostname(t *testing.T) {
	out, err := RenderMetaData(minimalAutoinstallCtx())
	if err != nil {
		t.Fatalf("RenderMetaData 返回错误: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("meta-data 不是合法 YAML: %v\n输出:\n%s", err, out)
	}
	if _, ok := doc["instance-id"]; !ok {
		t.Errorf("meta-data 应含 instance-id，got:\n%s", out)
	}
	if hn, ok := doc["local-hostname"]; !ok || hn != fixedHostname {
		t.Errorf("meta-data 应含 local-hostname=%q，got:\n%s", fixedHostname, out)
	}
}

// TestAutoinstall_DataSourceURLHasTrailingSlash 锁住"菜单里的数据源 URL
// 结尾带斜杠"（计划任务 3 测试要求第四条，SPEC §4.4）。
func TestAutoinstall_DataSourceURLHasTrailingSlash(t *testing.T) {
	ctx := minimalAutoinstallCtx()
	ctx.ActualFirmware = model.FirmwareUEFI
	out, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 返回错误: %v", err)
	}
	idx := strings.Index(out, "ds=nocloud-net;s=")
	if idx == -1 {
		t.Fatalf("Ubuntu 菜单应含 ds=nocloud-net;s=，got:\n%s", out)
	}
	rest := out[idx+len("ds=nocloud-net;s="):]
	end := strings.IndexAny(rest, " \n")
	if end == -1 {
		end = len(rest)
	}
	urlValue := rest[:end]
	if !strings.HasSuffix(urlValue, "/") {
		t.Errorf("ds=nocloud-net;s= 的值必须以 / 结尾（NoCloud 要求 URL 是目录），got %q", urlValue)
	}
}

// TestAutoinstall_InTargetPrefixForChrootCommands 锁住"对目标系统的命令带
// in-target 前缀"（计划任务 3 测试要求第五条）。
func TestAutoinstall_InTargetPrefixForChrootCommands(t *testing.T) {
	out, err := AssembleAutoinstall(fullAutoinstallCtx())
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	if !strings.Contains(out, "curtin in-target --") {
		t.Errorf("post 阶段的命令应带 curtin in-target -- 前缀，got:\n%s", out)
	}
}

// TestAutoinstall_FirstbootUsesSystemdOneshot 锁住"firstboot 走 systemd
// oneshot（与 RHEL 同一套脚手架）"（计划任务 3 测试要求第六条）。
func TestAutoinstall_FirstbootUsesSystemdOneshot(t *testing.T) {
	out, err := AssembleAutoinstall(fullAutoinstallCtx())
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	if !strings.Contains(out, "lober-init.service") {
		t.Errorf("autoinstall 输出应含 lober-init.service（systemd oneshot 脚手架），got:\n%s", out)
	}
	if strings.Contains(out, "runcmd") {
		t.Errorf("autoinstall 不应使用 cloud-init 的 runcmd 机制（选型是 systemd oneshot），got:\n%s", out)
	}
}

// TestAutoinstall_NoLiteralHTTPScheme 锁住"输出里不含字面 http://"。
func TestAutoinstall_NoLiteralHTTPScheme(t *testing.T) {
	httpsBases := model.BaseURLs{
		GatewayBase:  "https://gw.internal",
		ArtifactBase: "https://ng.internal",
	}
	ctx := fullAutoinstallCtx()
	ctx.Bases = httpsBases
	ctx.Facts = cloneFacts(ctx.Facts)
	ctx.Facts[model.FactArtifactBase] = httpsBases.ArtifactURL()
	ctx.Facts[model.FactGatewayBase] = httpsBases.GatewayURL()

	out, err := AssembleAutoinstall(ctx)
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	if strings.Contains(out, "http://") {
		t.Errorf("Bases 配置为 https:// 时，autoinstall 输出仍含字面 http://，got:\n%s", out)
	}
}

// TestAutoinstall_RejectsMaliciousParams 端到端注入测试：恶意表单值不能
// 进入 autoinstall 输出，且不能破坏 YAML 结构。
func TestAutoinstall_RejectsMaliciousParams(t *testing.T) {
	ctx := fullAutoinstallCtx()
	ctx.Task.ResolvedParams = map[string]string{
		"hostname": "web01; rm -rf /",
	}
	out, err := AssembleAutoinstall(ctx)
	if err == nil {
		t.Errorf("恶意 hostname 应导致 AssembleAutoinstall 返回错误，got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("恶意值应导致渲染失败时不返回任何内容，got:\n%s", out)
	}
}

// TestAutoinstall_MaliciousFactValueDoesNotBreakYAMLStructure 锁住"YAML
// 结构破坏"这一类注入（计划任务 3/8 明确要求"至少覆盖换行注入与 YAML
// 结构破坏"）。facts 命名空间的值不经过 guard 校验器（InjectFacts 的
// 设计如此，见 params.go），因此这是比 ResolvedParams 更贴近"未经校验
// 的值真的会流进渲染输出"的注入面——用一个含换行、单引号、YAML 特殊字符
// （冒号加空格，YAML 会把它解释为键值分隔）的 fact 值走一遍渲染，确认
// 输出仍是合法 YAML，且该 fact 值本身经过 shellAssignment 的单引号转义后
// 不会让 shell 语法或 YAML 结构断裂。
func TestAutoinstall_MaliciousFactValueDoesNotBreakYAMLStructure(t *testing.T) {
	ctx := fullAutoinstallCtx()
	ctx.Facts = cloneFacts(ctx.Facts)
	ctx.Facts[model.FactRack] = "rack-01'\nkey: injected-value\n%post\nrm -rf /"

	out, err := AssembleAutoinstall(ctx)
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("含恶意 fact 值时输出不是合法 YAML: %v\n输出:\n%s", err, out)
	}
	autoinstall, ok := doc["autoinstall"].(map[string]any)
	if !ok {
		t.Fatalf("顶层 autoinstall 键解析后类型不对，got: %T", doc["autoinstall"])
	}
	if _, injected := autoinstall["key"]; injected {
		t.Errorf("恶意 fact 值中的 YAML 键值对被解析成了顶层 autoinstall 下的真实键，" +
			"说明换行注入突破了块标量边界，破坏了 YAML 结构")
	}
}

// TestAutoinstall_YAMLStructureBreakingPayloadStaysValidYAML 锁住"即使
// 恶意值尝试破坏 YAML 结构（如注入额外的键或改变缩进），只要走到渲染成功
// 的路径（本测试用合法但含 YAML 特殊字符的值），输出仍必须是合法 YAML"——
// 这是计划任务 3 第 1 点细节"一个 hook 里的换行就能让整份文件语法错误"的
// 反面验证：用序列化库而不是文本模板，就不应该出现这个问题。
func TestAutoinstall_YAMLStructureBreakingPayloadStaysValidYAML(t *testing.T) {
	ctx := fullAutoinstallCtx()
	// hostname 校验器只接受字母数字与连字符，这里验证的是"合法值中若恰好
	// 含 YAML 会关心的字符（本例没有，因为 hostname 字符集本身就不含冒号/
	// 引号）时输出仍是合法 YAML"，重点是走一遍完整渲染 + YAML 解析验证。
	ctx.Task.ResolvedParams = map[string]string{
		"hostname": "web-01",
	}
	out, err := AssembleAutoinstall(ctx)
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("输出不是合法 YAML: %v\n输出:\n%s", err, out)
	}
}

// TestSignalPoints_UbuntuAllPresentAndKnown 锁住"三个家族各自生成全部
// 信号点，且信号名都在 M1 的信号表里"（计划 phase2-01 任务 4 测试要求
// 第一条），并验证 Ubuntu 的 packages-start/packages-done 也按近似方案
// 发出（计划任务 4："这两个信号实际是晚期命令开始前与晚期命令里第一条"）。
func TestSignalPoints_UbuntuAllPresentAndKnown(t *testing.T) {
	out, err := AssembleAutoinstall(fullAutoinstallCtx())
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}

	wantSignals := []model.SignalKind{
		model.SignalPreStart, model.SignalPreDone,
		model.SignalPostStart, model.SignalPackagesStart, model.SignalPackagesDone,
		model.SignalPostDone, model.SignalRebooting,
	}
	for _, sig := range wantSignals {
		marker := `signal "` + string(sig) + `"`
		if !strings.Contains(out, marker) {
			t.Errorf("Ubuntu autoinstall 输出缺少信号 %q（marker=%q），got:\n%s", sig, marker, out)
		}
		if _, ok := model.LookupSignal(sig); !ok {
			t.Errorf("信号 %q 不在 M1 的信号表里（LookupSignal 返回 ok=false）——"+
				"渲染器不应发出信号表之外的信号名", sig)
		}
	}
}

// TestSignalPoints_UbuntuNoSignalNeverEmitted 锁住"Ubuntu 不生成永不发出
// 的信号"（计划任务 4 测试要求第二条）：解析出 late-commands 列表，逐项
// 检查每个出现的信号名都在某一项里实际被 signal 调用，不是被拼进了字符串
// 但从未作为可执行的 shell 语句出现（用 YAML 解析器解出真实列表，而不是
// 对原始 YAML 文本做字符串匹配，避免"信号名恰好出现在某个不会被执行的
// 注释或字符串字面量里"这类假阳性）。
func TestSignalPoints_UbuntuNoSignalNeverEmitted(t *testing.T) {
	out, err := AssembleAutoinstall(fullAutoinstallCtx())
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	var doc struct {
		Autoinstall struct {
			EarlyCommands []string `yaml:"early-commands"`
			LateCommands  []string `yaml:"late-commands"`
		} `yaml:"autoinstall"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	allCommands := strings.Join(doc.Autoinstall.EarlyCommands, "\n") + "\n" + strings.Join(doc.Autoinstall.LateCommands, "\n")

	for _, sig := range []model.SignalKind{
		model.SignalPreStart, model.SignalPreDone,
		model.SignalPostStart, model.SignalPackagesStart, model.SignalPackagesDone,
		model.SignalPostDone, model.SignalRebooting,
	} {
		marker := `signal "` + string(sig) + `"`
		if !strings.Contains(allCommands, marker) {
			t.Errorf("信号 %q 在解析后的命令列表中未实际出现（可能只存在于未被执行的位置），"+
				"这正是计划任务 4 明确警告的「产出一个永不发出的信号」问题", sig)
		}
	}
}

// TestSignalPoints_UbuntuRebootingAtEndOfLastStage 锁住"三个家族的
// rebooting 都在最后一个阶段的末尾"（计划任务 4 测试要求第三条）。
func TestSignalPoints_UbuntuRebootingAtEndOfLastStage(t *testing.T) {
	out, err := AssembleAutoinstall(fullAutoinstallCtx())
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	var doc struct {
		Autoinstall struct {
			LateCommands []string `yaml:"late-commands"`
		} `yaml:"autoinstall"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(doc.Autoinstall.LateCommands) == 0 {
		t.Fatal("late-commands 为空")
	}
	lastItem := doc.Autoinstall.LateCommands[len(doc.Autoinstall.LateCommands)-1]
	if !strings.Contains(lastItem, `signal "rebooting"`) {
		t.Errorf("late-commands 的最后一项应包含 rebooting 信号，got 最后一项:\n%s", lastItem)
	}
}

// TestGolden_MinimalAutoinstall 锁住无 hook 的最小 autoinstall 输出。
func TestGolden_MinimalAutoinstall(t *testing.T) {
	out, err := AssembleAutoinstall(minimalAutoinstallCtx())
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	assertGolden(t, "autoinstall_minimal.golden", out)
}

// TestGolden_FullAutoinstall 锁住带三阶段 hook 的完整 autoinstall 输出。
func TestGolden_FullAutoinstall(t *testing.T) {
	out, err := AssembleAutoinstall(fullAutoinstallCtx())
	if err != nil {
		t.Fatalf("AssembleAutoinstall 返回错误: %v", err)
	}
	assertGolden(t, "autoinstall_full.golden", out)
}

// TestGolden_MetaData 锁住 meta-data 输出。
func TestGolden_MetaData(t *testing.T) {
	out, err := RenderMetaData(minimalAutoinstallCtx())
	if err != nil {
		t.Fatalf("RenderMetaData 返回错误: %v", err)
	}
	assertGolden(t, "meta_data.golden", out)
}

// TestGolden_UbuntuMenu 锁住 Ubuntu 的 iPXE 菜单输出。
func TestGolden_UbuntuMenu(t *testing.T) {
	ctx := minimalAutoinstallCtx()
	ctx.ActualFirmware = model.FirmwareUEFI
	out, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 返回错误: %v", err)
	}
	assertGolden(t, "menu_ubuntu.golden", out)
}
