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

// TestStageMapping_EmptyStageProducesNoSection 锁住"空阶段不产生空段落
// （%pre 里没东西就不要写 %pre）"（计划任务 5 测试要求第四条）。
func TestStageMapping_EmptyStageProducesNoSection(t *testing.T) {
	ctx := model.RenderContext{Hooks: []model.HookRef{
		{HookDigest: "post001", Stage: model.HookStagePost, Order: 10, Required: true},
	}}

	pre := RenderStageSection(ctx, model.HookStagePre)
	if pre != "" {
		t.Errorf("没有 pre 阶段 hook 时 RenderStageSection 应返回空字符串"+
			"（不应写出空的 %%pre 段落头），got:\n%s", pre)
	}

	nochroot := RenderStageSection(ctx, model.HookStagePostNoChroot)
	if nochroot != "" {
		t.Errorf("没有 post-nochroot 阶段 hook 时应返回空字符串，got:\n%s", nochroot)
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
