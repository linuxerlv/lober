// 本文件测试 Debian preseed 渲染（对应实现计划 phase2-01 任务 2，依据
// SPEC §4.4、§4.5、§5.2）。
package render

import (
	"os"
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
			ID:        "as-debian12",
			OSFamily:  model.OSFamilyDebian,
			OSVersion: "12",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "linux",
				Initrd: []string{"initrd.gz"},
				Repo:   "repo/",
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
		RootSSHPublicKeys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
		},
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

	// run_hook 调用应该在被落盘的脚本正文里。两个阶段的脚本都是 base64 编码后
	// 塞进 debconf 单行值的，所以要解码才能断言——早先 early_command 是明文，
	// 于是本测试在 out 里直接搜就能命中；那个明文单行做法在真机上产出非法 shell
	// （见 earlyCommandValue 的说明），改成 base64 之后明文里不再有 run_hook。
	early, err := earlyCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("earlyCommandValue 返回错误: %v", err)
	}
	late, err := lateCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeSoleBase64Blob(t, early) + decodeLateCommandScript(t, late)
	if !strings.Contains(decoded, "run_hook '") {
		t.Errorf("两个阶段的脚本里都没有 run_hook 调用，hook 没有被落进任何脚本文件。"+
			"解码后的正文:\n%s", decoded)
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

	// pre-start/pre-done 与 post-start/post-done/rebooting 现在都落在 base64
	// 编码的脚本里（early_command 与 late_command 各一份），都要先解码再断言。
	//
	// early_command 此前是明文单行，本测试也因此直接在 out 里搜。那个做法在真机
	// 上装不了机：ClientLib 含函数定义与行内注释，按行用 `; ` 拼接会产出非法
	// shell（实测 `sh: syntax error: unexpected ";"`），见 earlyCommandValue 的说明。
	early, err := earlyCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("earlyCommandValue 返回错误: %v", err)
	}
	decodedEarly := decodeSoleBase64Blob(t, early)

	earlyMarkers := []model.SignalKind{model.SignalPreStart, model.SignalPreDone}
	for _, sig := range earlyMarkers {
		marker := `signal "` + string(sig) + `"`
		if !strings.Contains(decodedEarly, marker) {
			t.Errorf("Debian preseed 的 early_command 脚本中缺少信号 %q（marker=%q），decoded:\n%s",
				sig, marker, decodedEarly)
		}
	}
	_ = out

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

// TestPreseed_SSHKeyWrittenToAuthorizedKeysInTarget 锁住本轮核心行为：
// preseed 没有与 kickstart `sshkey` 或 autoinstall `ssh.authorized-keys`
// 直接等价的键，公钥注入走 late_command 里已经在跑的 in-target 落盘机制
// ——把公钥追加写进目标系统的 /root/.ssh/authorized_keys（SPEC §1.5）。
func TestPreseed_SSHKeyWrittenToAuthorizedKeysInTarget(t *testing.T) {
	ctx := minimalPreseedCtx()
	late, err := lateCommandValue(ctx)
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)

	if !strings.Contains(decoded, "authorized_keys") {
		t.Fatalf("late_command 脚本应含写 authorized_keys 的命令，decoded:\n%s", decoded)
	}
	if !strings.Contains(decoded, ctx.RootSSHPublicKeys[0]) {
		t.Errorf("late_command 脚本应含公钥文本 %q，decoded:\n%s", ctx.RootSSHPublicKeys[0], decoded)
	}
	if !strings.Contains(decoded, "mkdir") || !strings.Contains(decoded, ".ssh") {
		t.Errorf("落盘前应先 mkdir /root/.ssh（目标系统的全新安装，该目录默认不存在），decoded:\n%s", decoded)
	}
}

// TestPreseed_MultipleSSHKeysAllWritten 锁住多把公钥全部写入。
func TestPreseed_MultipleSSHKeysAllWritten(t *testing.T) {
	ctx := minimalPreseedCtx()
	ctx.RootSSHPublicKeys = []string{
		"ssh-ed25519 AAAAalice alice@lober",
		"ssh-rsa AAAAbob bob@lober",
	}
	late, err := lateCommandValue(ctx)
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)
	for _, key := range ctx.RootSSHPublicKeys {
		if !strings.Contains(decoded, key) {
			t.Errorf("late_command 脚本应含公钥 %q，decoded:\n%s", key, decoded)
		}
	}
}

// TestPreseed_EmptySSHKeysRejected 锁住"渲染器万一拿到空值就返回错误而
// 绝不再写一个假公钥"这条防御，与 kickstart/autoinstall 同一个设计要点。
func TestPreseed_EmptySSHKeysRejected(t *testing.T) {
	ctx := minimalPreseedCtx()
	ctx.RootSSHPublicKeys = nil
	_, err := AssemblePreseed(ctx)
	if err == nil {
		t.Fatal("AssemblePreseed 对空 RootSSHPublicKeys 未返回错误——" +
			"这会静默产出一台 root 既没有密码也没有公钥、没人能登进去的机器")
	}
}

// TestPreseed_MaliciousSSHKeyDoesNotBreakInTargetQuoting 锁住"恶意公钥不
// 逃逸 in-target bash -c 的单引号包裹"这条注入防护（本轮设计要点第 4 条）。
// late_command 脚本本身经 base64 编码传输，公钥文本作为脚本内容的一部分
// 落进这个已编码的载体里；脚本内部把公钥写进 authorized_keys 时用单引号
// heredoc 或等价的单引号包裹——若公钥里含单引号且未被正确转义，会提前
// 结束这段 shell 语法，让后续文本被解释成新的 shell 命令。
func TestPreseed_MaliciousSSHKeyDoesNotBreakInTargetQuoting(t *testing.T) {
	ctx := minimalPreseedCtx()
	malicious := `ssh-ed25519 AAAAtest evil' ; touch /tmp/pwned ; echo '`
	ctx.RootSSHPublicKeys = []string{malicious}

	late, err := lateCommandValue(ctx)
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)

	stubDir := t.TempDir()
	writeStub(t, stubDir, "curl")
	writeStub(t, stubDir, "sha256sum")
	writeInTargetStub(t, stubDir)
	stubDirPosix := posixPath(t, stubDir)

	// 真实执行解码后的脚本（用与 preseed_exec_test.go 相同的 in-target
	// stub），断言恶意值没有让 touch /tmp/pwned 这条注入的命令被执行到——
	// 只做静态字符串包含断言无法证明"运行时是否真的被 shell 解释执行"，
	// 必须像 kickstart_exec_test.go/preseed_exec_test.go 一样起真实 bash
	// 子进程才能验证转义在运行期而不仅是文本层面生效。用 t.TempDir() 下的
	// marker 文件替代真实的 /tmp/pwned 路径，跨平台可写且测试结束自动清理。
	pwnedMarkerNative := t.TempDir() + string(os.PathSeparator) + "pwned-marker"
	pwnedMarkerPosix := posixPath(t, pwnedMarkerNative)
	scriptWithMarkerRedirect := strings.ReplaceAll(decoded, "touch /tmp/pwned", "touch "+pwnedMarkerPosix)
	runBashSectionOffline(t, stubDirPosix, scriptWithMarkerRedirect)

	if _, statErr := os.Stat(pwnedMarkerNative); statErr == nil {
		t.Errorf("恶意公钥里的单引号逃逸了 in-target bash -c 的单引号包裹，" +
			"注入的 touch 命令被真实执行了")
	}
}

// TestPreseed_FirstbootScaffoldCalledInPostStage 锁住本轮核心缺口修复
// 第一条：preseed.go 必须调用 FirstbootScaffold，把 firstboot 脚手架接进
// late_command 的 post 阶段（SPEC §5.2 的阶段映射：firstboot → systemd
// oneshot，与 RHEL 相同）。修复前 preseed.go 通篇零处调用 FirstbootScaffold
// （任务描述已 grep 确认），后果是重启后没有任何机制发出 booted/
// init-start/init-done/ready 四个信号，任务会卡在 rebooting 直到超时——
// 而 rebooting 超时是不可重装的失败态（§3.4）。本测试直接断言脚手架的
// 标志性产出（systemd unit、ConditionPathExists、标记文件、lober-init.sh）
// 出现在解码后的 late_command 脚本里。
func TestPreseed_FirstbootScaffoldCalledInPostStage(t *testing.T) {
	late, err := lateCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)

	wants := []struct {
		label string
		want  string
	}{
		{"systemd unit 文件", "/etc/systemd/system/lober-init.service"},
		{"ConditionPathExists 只跑一次保证", "ConditionPathExists=/var/lib/lober/init.pending"},
		{"标记文件", "touch /var/lib/lober/init.pending"},
		{"enable 命令", "systemctl enable lober-init.service"},
		{"firstboot 执行脚本", "/usr/local/sbin/lober-init.sh"},
	}
	for _, w := range wants {
		if !strings.Contains(decoded, w.want) {
			t.Errorf("late_command 脚本缺少%s（期望包含 %q）——preseed.go 未正确调用 "+
				"FirstbootScaffold，重启后 firstboot 脚手架不会被埋点，got:\n%s", w.label, w.want, decoded)
		}
	}
}

// TestPreseed_FirstbootScaffoldRunsEvenWithoutHooks 锁住"即使当前渲染的
// 这台机器没有任何 firstboot hook，脚手架本身（systemd unit、四个重启后
// 信号的触发机制）仍必须存在"——与 RHEL 的 AssembleKickstart 无条件在
// %post 追加 FirstbootScaffold 是同一个设计：脚手架描述的是装机流程本身
// 的能力，不因为这台机器的模板没挂 firstboot hook 就整体消失。
func TestPreseed_FirstbootScaffoldRunsEvenWithoutHooks(t *testing.T) {
	late, err := lateCommandValue(minimalPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)
	if !strings.Contains(decoded, "/etc/systemd/system/lober-init.service") {
		t.Errorf("无 hook 的最小 preseed 也应含 firstboot 脚手架（systemd unit），got:\n%s", decoded)
	}
}

// TestPreseed_FirstbootScaffoldWrappedInTargetForChroot 锁住"FirstbootScaffold
// 的产出必须包在 in-target 里执行"——它写的路径（/var/lib/lober、
// /etc/systemd/system、/usr/local/sbin）全部是目标系统路径，late_command
// 本身跑在 d-i 环境（未加 in-target 时是宿主 initrd，不是目标盘），若不加
// in-target 前缀，这些 mkdir/cat/systemctl 全部会作用在 installer 自己的
// 临时环境上，重启后目标系统里什么都不会有（与 Ubuntu autoinstall.go 的
// firstbootInTargetCommands 同一个设计要点）。
func TestPreseed_FirstbootScaffoldWrappedInTargetForChroot(t *testing.T) {
	late, err := lateCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)

	idx := strings.Index(decoded, "/etc/systemd/system/lober-init.service")
	if idx == -1 {
		t.Fatalf("找不到 systemd unit 路径，decoded:\n%s", decoded)
	}
	// 从该行往前找最近一次出现的 "in-target"，确认它确实处于某个 in-target
	// 包裹范围内，而不是裸露在 late_command 顶层脚本中直接执行。
	before := decoded[:idx]
	if !strings.Contains(before, "in-target") {
		t.Errorf("FirstbootScaffold 的产出前面找不到 in-target 前缀，说明它没有被包进"+
			"chroot 执行，脚手架会错误地作用在 d-i 环境而不是目标系统，decoded 前缀:\n%s", before)
	}
}

// TestPreseed_FirstbootHookScriptsFetchedInPostStage 锁住本轮任务要点：
// "用户的 firstboot hook 脚本与参数必须在 post 阶段就落进目标系统"（SPEC
// §5.8），不能等重启后再去 nginx 拉——重启后业务网可能已生效，装机 VLAN
// 未必还可达。验证方式：fullPreseedCtx 里挂了一个 firstboot 阶段的 hook
// （dddd4444firstboot），解码后的 late_command 脚本里应包含对它的下载与
// 校验命令（curl + sha256sum -c），且这些命令必须出现在脚本正文里（即
// %post 执行期就会跑到），不是只出现在重启后才执行的 lober-init.sh heredoc
// 之外的某个不会被执行的位置——用 curl 命令本身在 heredoc 边界之外（即
// late_command 脚本会直接执行到它，不是被 heredoc 引用包裹成"仅供重启后
// 使用的文本"）来确认。
func TestPreseed_FirstbootHookScriptsFetchedInPostStage(t *testing.T) {
	late, err := lateCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)

	fetchLine := "curl -fsS -o /var/lib/lober/hooks/010-dddd4444firstboot.sh"
	if !strings.Contains(decoded, fetchLine) {
		t.Fatalf("late_command 脚本缺少 firstboot hook 下载命令 %q——"+
			"用户的 firstboot hook 没有在 post 阶段落进目标系统，got:\n%s", fetchLine, decoded)
	}
	if !strings.Contains(decoded, "dddd4444firstboot  /var/lib/lober/hooks/010-dddd4444firstboot.sh") {
		t.Errorf("firstboot hook 下载后应有 sha256sum -c 校验，got:\n%s", decoded)
	}

	// 下载命令必须出现在 lober-init.sh 的 heredoc 定界符之外（不能只是被
	// 引用成"重启后才会执行的文本的一部分"）——heredoc 内容以
	// LOBER_INIT_EOF 结尾，下载命令应出现在这对定界符之外的脚本主体中。
	heredocStart := strings.Index(decoded, "cat > /usr/local/sbin/lober-init.sh")
	heredocEnd := strings.Index(decoded, "LOBER_INIT_EOF\n")
	fetchIdx := strings.Index(decoded, fetchLine)
	if heredocStart == -1 || heredocEnd == -1 || fetchIdx == -1 {
		t.Fatalf("定位标记失败，heredocStart=%d heredocEnd=%d fetchIdx=%d", heredocStart, heredocEnd, fetchIdx)
	}
	if fetchIdx > heredocStart && fetchIdx < heredocEnd {
		t.Errorf("firstboot hook 下载命令落在了 lober-init.sh 的 heredoc 内容内部，"+
			"说明它被写进了「重启后才执行」的脚本正文，而不是 post 阶段立即执行的命令，"+
			"decoded:\n%s", decoded)
	}
}

// TestPreseed_RebootSignalsHaveTriggerMechanism 锁住"重启后四个信号
// （booted/init-start/init-done/ready）有触发机制"——即 lober-init.sh 的
// 正文里包含这四个 signal 调用，且该脚本被 systemd oneshot unit
// enable，这正是"有机制会在重启后跑起来并发出它们"的证据链（与
// firstboot_test.go 的 TestFirstbootScaffold_InitScriptSignalOrder 验证
// 的是同一件事，这里是从 preseed 组装层面确认没有在接线时被漏掉或截断）。
func TestPreseed_RebootSignalsHaveTriggerMechanism(t *testing.T) {
	late, err := lateCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)

	markers := []string{`signal "booted" 0`, `signal "init-start" 0`, `signal "init-done" 0`, `signal "ready" 0`}
	lastIdx := -1
	for _, m := range markers {
		idx := strings.Index(decoded, m)
		if idx == -1 {
			t.Fatalf("找不到重启后信号 %q，decoded:\n%s", m, decoded)
		}
		if idx <= lastIdx {
			t.Errorf("信号 %q 出现顺序不对（应晚于前一个），decoded:\n%s", m, decoded)
		}
		lastIdx = idx
	}
	if !strings.Contains(decoded, "ExecStart=/usr/local/sbin/lober-init.sh") {
		t.Errorf("systemd unit 应以 lober-init.sh 为 ExecStart，即触发机制的落点，got:\n%s", decoded)
	}
}

// TestPreseed_PackagesStartDoneSignalsPresent 锁住本轮缺口修复第二条：
// packages-start/packages-done 两个信号点必须生成（§6.1 的信号点表里有
// 它们，计划任务 4 的三家族信号点映射表把 Debian 的这两个信号标注为
// "安装基础系统前/后"）。preseed 没有对应的 debconf 钩子可以精确挂在
// debootstrap/base-installer 步骤的前后——late_command 本身执行时点已经
// 晚于基础系统安装完成，因此这里采用与 Ubuntu autoinstall.go
// （§6.1 映射表同一栏"只能近似"）相同的近似方案：在 post-start 之后立即
// 成对发出，不追求精确对应真实解包动作的起止时刻（contractGaps 记录该
// 近似的理由）。本测试只锁住"两个信号确实被发出"，顺序测试见下一个函数。
func TestPreseed_PackagesStartDoneSignalsPresent(t *testing.T) {
	late, err := lateCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)

	for _, sig := range []model.SignalKind{model.SignalPackagesStart, model.SignalPackagesDone} {
		marker := `signal "` + string(sig) + `"`
		if !strings.Contains(decoded, marker) {
			t.Errorf("late_command 脚本缺少信号 %q（marker=%q）——§6.1 信号点表要求的 "+
				"packages-start/packages-done 未生成，装包这个预算最长的阶段完全没有进度"+
				"可见性，got:\n%s", sig, marker, decoded)
		}
		if _, ok := model.LookupSignal(sig); !ok {
			t.Errorf("信号 %q 不在 M1 的信号表里", sig)
		}
	}
}

// TestPreseed_PackagesStartDoneOrderedAfterPostStartBeforeHooks 锁住
// packages-start/packages-done 的插入位置：必须在 post-start 之后（post
// 阶段已经开始）、且成对相邻出现（近似方案不精确对应真实起止，但至少要
// 保证两者语义上的先后关系不倒），不应该混进用户 hook 调用的中间或之后
// （装包阶段的语义边界在"用户 hook 开始跑之前"——用户的 post 阶段 hook
// 本质上是"装完包之后做点什么"，若 packages-done 排在用户 hook 之后，会
// 让运维误以为装包一直持续到用户 hook 跑完，混淆两个不同阶段的边界）。
func TestPreseed_PackagesStartDoneOrderedAfterPostStartBeforeHooks(t *testing.T) {
	late, err := lateCommandValue(fullPreseedCtx())
	if err != nil {
		t.Fatalf("lateCommandValue 返回错误: %v", err)
	}
	decoded := decodeLateCommandScript(t, late)

	postStartIdx := strings.Index(decoded, `signal "post-start" 0`)
	pkgStartIdx := strings.Index(decoded, `signal "packages-start" 0`)
	pkgDoneIdx := strings.Index(decoded, `signal "packages-done" 0`)
	// fullPreseedCtx 挂了一个 required=false 的 post-nochroot hook，其
	// run_hook 调用是第一个真正的用户 hook 调用行。
	nochrootHookIdx := strings.Index(decoded, "run_hook 'bbbb2222nochroot'")
	if postStartIdx == -1 || pkgStartIdx == -1 || pkgDoneIdx == -1 || nochrootHookIdx == -1 {
		t.Fatalf("定位标记失败：postStartIdx=%d pkgStartIdx=%d pkgDoneIdx=%d nochrootHookIdx=%d",
			postStartIdx, pkgStartIdx, pkgDoneIdx, nochrootHookIdx)
	}
	if pkgStartIdx <= postStartIdx {
		t.Errorf("packages-start 应出现在 post-start 之后，got 顺序不对，decoded:\n%s", decoded)
	}
	if pkgDoneIdx <= pkgStartIdx {
		t.Errorf("packages-done 应出现在 packages-start 之后，decoded:\n%s", decoded)
	}
	if pkgDoneIdx >= nochrootHookIdx {
		t.Errorf("packages-done 应出现在第一个用户 hook 调用之前——装包阶段的信号边界"+
			"不应晚于用户 hook 开始执行，否则会让运维误以为装包持续到用户 hook 跑完，"+
			"decoded:\n%s", decoded)
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
