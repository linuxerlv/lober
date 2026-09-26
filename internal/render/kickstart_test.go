// 本文件测试 hook 阶段到 kickstart 段落的映射（对应实现计划任务 5，依据
// SPEC §5.2）。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// stageMappingCtx 构造一个带三个 installer 阶段各一个 hook、外加一个
// firstboot 阶段 hook 的 RenderContext，用于验证阶段到段落的映射。
func stageMappingCtx() model.RenderContext {
	return model.RenderContext{
		Hooks: []model.HookRef{
			{HookDigest: "pre001", Stage: model.HookStagePre, Order: 10, Required: true},
			{HookDigest: "pre002", Stage: model.HookStagePre, Order: 20, Required: false},
			{HookDigest: "nochroot001", Stage: model.HookStagePostNoChroot, Order: 10, Required: true},
			{HookDigest: "post001", Stage: model.HookStagePost, Order: 10, Required: true},
			{HookDigest: "fb001", Stage: model.HookStageFirstboot, Order: 10, Required: true},
		},
	}
}

// TestStageMapping_ThreeInstallerStages 锁住"三个 installer 阶段的 hook
// 落进对应段落"（计划任务 5 测试要求第一条）。
func TestStageMapping_ThreeInstallerStages(t *testing.T) {
	ctx := stageMappingCtx()

	pre := RenderStageSection(ctx, model.HookStagePre)
	if !strings.Contains(pre, "%pre") {
		t.Errorf("pre 阶段渲染结果应含 %%pre 段落头，got:\n%s", pre)
	}
	if !strings.Contains(pre, "pre001") || !strings.Contains(pre, "pre002") {
		t.Errorf("pre 阶段渲染结果应含两个 hook 的 digest 引用，got:\n%s", pre)
	}

	nochroot := RenderStageSection(ctx, model.HookStagePostNoChroot)
	if !strings.Contains(nochroot, "%post --nochroot") {
		t.Errorf("post-nochroot 阶段渲染结果应含 %%post --nochroot 段落头，got:\n%s", nochroot)
	}
	if !strings.Contains(nochroot, "nochroot001") {
		t.Errorf("post-nochroot 阶段渲染结果应含其 hook 的 digest 引用，got:\n%s", nochroot)
	}

	post := RenderStageSection(ctx, model.HookStagePost)
	if !strings.Contains(post, "%post\n") && !strings.Contains(post, "%post ") {
		t.Errorf("post 阶段渲染结果应含 %%post 段落头，got:\n%s", post)
	}
	if !strings.Contains(post, "post001") {
		t.Errorf("post 阶段渲染结果应含其 hook 的 digest 引用，got:\n%s", post)
	}
}

// TestStageMapping_FirstbootNotInAnySection 锁住"firstboot 阶段的 hook 不
// 出现在任何 kickstart 段落里（它们进脚手架）"（计划任务 5 测试要求第
// 二条）。
func TestStageMapping_FirstbootNotInAnySection(t *testing.T) {
	ctx := stageMappingCtx()

	for _, stage := range []model.HookStage{
		model.HookStagePre, model.HookStagePostNoChroot, model.HookStagePost,
	} {
		out := RenderStageSection(ctx, stage)
		if strings.Contains(out, "fb001") {
			t.Errorf("阶段 %s 的渲染结果不应包含 firstboot hook (fb001) 的引用"+
				"——firstboot hook 只应出现在 FirstbootScaffold 的产出里，got:\n%s", stage, out)
		}
	}
}

// TestStageMapping_OrderPreserved 锁住"同阶段多个 hook 按 order 顺序出现"
// （计划任务 5 测试要求第三条）。
func TestStageMapping_OrderPreserved(t *testing.T) {
	ctx := model.RenderContext{
		Hooks: []model.HookRef{
			// 故意乱序传入：Order=20 排在 Order=10 前面，渲染结果必须按
			// Order 重新排列，不能假设调用方已经排好序。
			{HookDigest: "second", Stage: model.HookStagePre, Order: 20, Required: true},
			{HookDigest: "first", Stage: model.HookStagePre, Order: 10, Required: true},
		},
	}
	out := RenderStageSection(ctx, model.HookStagePre)
	idxFirst := strings.Index(out, "first")
	idxSecond := strings.Index(out, "second")
	if idxFirst == -1 || idxSecond == -1 {
		t.Fatalf("渲染结果应同时包含 first 与 second，got:\n%s", out)
	}
	if idxFirst > idxSecond {
		t.Errorf("Order=10 的 hook (first) 应先于 Order=20 的 hook (second) 出现，got:\n%s", out)
	}
}

// TestStageMapping_EmptyStageProducesNoSection 锁住"没有内容的阶段不产生
// 空段落"（计划任务 5 测试要求第四条）。
//
// 本用例的判据在「kickstart 补发阶段信号」之后收窄了：%pre 与 %post 即使没有
// hook 也**不是空段落**——它们要发 pre-start/pre-done 与 post-start/post-done
// 这两对阶段边界信号（状态机靠它们判进度，缺了就一直等到阶段预算耗尽，见
// kickstart_signals_test.go 的实测记录）。真正「没有内容就该整段省掉」的只剩
// %post --nochroot：它没有独立信号（与 %post 同属装完之后那一段，状态机只认
// 一对 post-start/post-done，多发一对会被 §3.6 的只进不退判定当成回退）。
func TestStageMapping_EmptyStageProducesNoSection(t *testing.T) {
	ctx := model.RenderContext{Hooks: []model.HookRef{
		{HookDigest: "post001", Stage: model.HookStagePost, Order: 10, Required: true},
	}}

	nochroot := RenderStageSection(ctx, model.HookStagePostNoChroot)
	if nochroot != "" {
		t.Errorf("没有 post-nochroot 阶段 hook 时应返回空字符串"+
			"（该阶段没有独立信号，没 hook 就真的没内容），got:\n%s", nochroot)
	}

	// 反过来确认 %pre 在没有 hook 时**照样渲染**——它有信号要发。
	pre := RenderStageSection(ctx, model.HookStagePre)
	if pre == "" {
		t.Error("没有 pre 阶段 hook 时仍应渲染 pre 段：pre-start 与 pre-done 是" +
			"阶段边界标记，与 hook 无关，缺了状态机就一直等到阶段预算耗尽")
	}
	if !strings.Contains(pre, `signal "pre-start"`) || !strings.Contains(pre, `signal "pre-done"`) {
		t.Errorf("无 hook 的 %%pre 段应含 pre-start 与 pre-done，got:\n%s", pre)
	}
}

// TestStageMapping_RunHookCallHasRequiredAndDigest 锁住"为每个生成一次
// run_hook 调用，参数是 digest、required 标志与超时值"（计划任务 5 实现
// 要点）。
func TestStageMapping_RunHookCallHasRequiredAndDigest(t *testing.T) {
	ctx := model.RenderContext{Hooks: []model.HookRef{
		{HookDigest: "digest123", Stage: model.HookStagePre, Order: 10, Required: true},
	}}
	out := RenderStageSection(ctx, model.HookStagePre)
	if !strings.Contains(out, "run_hook") {
		t.Errorf("阶段段落应通过 run_hook 调用 hook，got:\n%s", out)
	}
	if !strings.Contains(out, "digest123") {
		t.Errorf("run_hook 调用应带 digest 参数，got:\n%s", out)
	}
	if !strings.Contains(out, "true") {
		t.Errorf("run_hook 调用应带 required=true 参数，got:\n%s", out)
	}
}

// sshKeyBaseCtx 构造一个能通过 baseDirectives 全部前置条件的最小
// RenderContext（磁盘 fact 非空），供 SSH 公钥注入相关测试复用。
func sshKeyBaseCtx(keys []string) model.RenderContext {
	return model.RenderContext{
		Facts:             map[string]string{model.FactDiskPrimary: "/dev/disk/by-id/wwn-test"},
		RootSSHPublicKeys: keys,
	}
}

// TestBaseDirectives_SSHKeyInjected 锁住"公钥从 RenderContext 注入，不再是
// 写死的占位值"这条本轮的核心行为（SPEC §1.5）。
func TestBaseDirectives_SSHKeyInjected(t *testing.T) {
	ctx := sshKeyBaseCtx([]string{"ssh-ed25519 AAAAtestkey ops@lober"})
	out, err := baseDirectives(ctx)
	if err != nil {
		t.Fatalf("baseDirectives 返回错误: %v", err)
	}
	want := `sshkey --username=root "ssh-ed25519 AAAAtestkey ops@lober"` + "\n"
	if !strings.Contains(out, want) {
		t.Errorf("baseDirectives 输出应含公钥指令 %q，got:\n%s", want, out)
	}
	if strings.Contains(out, "REPLACE-ME") {
		t.Errorf("baseDirectives 输出仍含旧占位公钥标记 REPLACE-ME，说明占位值未被移除，got:\n%s", out)
	}
}

// TestBaseDirectives_MultipleSSHKeysAllInjected 锁住"支持多把公钥，全部
// 都要落进应答文件"——团队多人各自持有私钥，轮换期需要新旧并存。
func TestBaseDirectives_MultipleSSHKeysAllInjected(t *testing.T) {
	ctx := sshKeyBaseCtx([]string{
		"ssh-ed25519 AAAAalice alice@lober",
		"ssh-rsa AAAAbob bob@lober",
	})
	out, err := baseDirectives(ctx)
	if err != nil {
		t.Fatalf("baseDirectives 返回错误: %v", err)
	}
	if !strings.Contains(out, `sshkey --username=root "ssh-ed25519 AAAAalice alice@lober"`) {
		t.Errorf("输出应含 alice 的公钥指令，got:\n%s", out)
	}
	if !strings.Contains(out, `sshkey --username=root "ssh-rsa AAAAbob bob@lober"`) {
		t.Errorf("输出应含 bob 的公钥指令，got:\n%s", out)
	}
}

// TestBaseDirectives_EmptySSHKeysRejected 锁住"渲染器万一拿到空值就返回
// 错误而绝不再写一个假公钥"这条防御（本轮设计要点第 3 条：静默降级成
// 占位公钥不在可选项里）。config.Validate 已经在启动时拦住空配置，这里
// 是第二层防御——万一渲染上下文的构造路径漏了赋值，也不会静默产出一台
// 登不进去的机器。
func TestBaseDirectives_EmptySSHKeysRejected(t *testing.T) {
	ctx := sshKeyBaseCtx(nil)
	_, err := baseDirectives(ctx)
	if err == nil {
		t.Fatal("baseDirectives 对空 RootSSHPublicKeys 未返回错误——" +
			"这会静默产出一台 root 既没有密码也没有公钥、没人能登进去的机器")
	}
}

// TestBaseDirectives_MaliciousKeyDoesNotEscapeQuotes 锁住"恶意输入不逃逸
// kickstart 双引号包裹"这条注入防护测试（本轮设计要点第 4 条：三个家族的
// 转义规则不同，各自都要有测试证明恶意输入不逃逸）。正常情况下
// config.Validate 已经在配置层拒绝了含引号的公钥，这里额外验证：即使有
// 恶意值绕过配置层直接进了 RenderContext，baseDirectives 自身产出的这一
// 行也不会因为公钥里含双引号而被截断成两条指令。
func TestBaseDirectives_MaliciousKeyWithNewlineRejected(t *testing.T) {
	malicious := `ssh-ed25519 AAAAtest evil" ` + "\n" + `rootpw --plaintext hacked #`
	ctx := sshKeyBaseCtx([]string{malicious})
	_, err := baseDirectives(ctx)
	if err == nil {
		t.Fatal("baseDirectives 对含裸换行的公钥未返回错误——" +
			"kickstart 是逐行解析的文本格式，换行会在 sshkey 指令中间开出一条独立的新指令" +
			"（这里构造的恶意值会拼出一条独立的 rootpw --plaintext 指令），必须拒绝而不是" +
			"依赖转义，光转义双引号不足以防住换行造成的逃逸")
	}
}

// TestBaseDirectives_QuoteInKeyEscapedNotRejected 是转义路径的对照组：
// 单独一个双引号（不含换行）应该被转义后正常渲染，不应触发拒绝——拒绝的
// 判定标准是"能否开出新指令"，双引号单独出现时会被转义中和，不构成
// 逃逸，因此不应比换行更严格。
func TestBaseDirectives_QuoteInKeyEscapedNotRejected(t *testing.T) {
	withQuote := `ssh-ed25519 AAAAtest evil"comment`
	ctx := sshKeyBaseCtx([]string{withQuote})
	out, err := baseDirectives(ctx)
	if err != nil {
		t.Fatalf("baseDirectives 对含单个双引号（无换行）的公钥返回了错误: %v"+
			"——双引号本身会被转义中和，不应该触发拒绝", err)
	}
	if !strings.Contains(out, `evil\"comment`) {
		t.Errorf("公钥里的双引号应被转义为 \\\"，got:\n%s", out)
	}
	rootpwCount := strings.Count(out, "\nrootpw ")
	if rootpwCount != 1 {
		t.Errorf("独立成行的 rootpw 指令出现了 %d 次，want 1——转义后不应产生第二条指令，got:\n%s",
			rootpwCount, out)
	}
}
