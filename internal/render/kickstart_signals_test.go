// 本文件锁住 kickstart 渲染出装机期的阶段信号。
//
// **RHEL 的 kickstart 一个装机期信号都不发，状态机因此永远等到阶段超时。**
// 其余四个家族都发：autoinstall.go:227、autoyast.go:282、preseed.go:190、
// winpe.go:100 各自渲染 pre-start，只有 kickstart.go 里 grep 不到任何
// `signal "` 字样。kickstart_full.golden 里只有 hook-report 与 firstboot
// 脚手架的 booted/init-start/init-done/ready，装机期的七个信号全缺。
//
// 实测（2026-09-26，QEMU 目标机，前面十三个缺口都已修掉）：pre hook 成功执行
// 并回报 rc=0
//
//	hook-report  rc=0 seq=1
//
// 此后再无任何信号。任务停在 installing/pre，直到 §3.3 的 10 分钟阶段预算
// 耗尽判失败。盘一个字节都没写——anaconda 其实在等 %pre 段结束后继续，而
// 状态机在等一个永远不会来的 pre-done。
//
// 根因在 RenderStageSection：它以 `if len(lines) == 0 { return "" }` 开头，
// 没有 hook 的阶段整段不渲染。而信号是阶段边界标记，本该独立于 hook 存在
// ——没有 hook 的 packages 阶段同样需要 packages-start/packages-done。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestKickstart_EmitsInstallPhaseSignals 锁住七个装机期信号都出现。
//
// 这七个是 SPEC §3.3 阶段预算表与 model.SignalKind 定义的装机期边界：
// pre 段两个、packages 两个、post 段两个、以及重启前的 rebooting。
// 其余四个（booted/init-start/init-done/ready）由 firstboot 脚手架发出，
// 已有 golden 覆盖，不在本测试范围。
func TestKickstart_EmitsInstallPhaseSignals(t *testing.T) {
	got, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}

	for _, sig := range []model.SignalKind{
		model.SignalPreStart, model.SignalPreDone,
		model.SignalPackagesStart, model.SignalPackagesDone,
		model.SignalPostStart, model.SignalPostDone,
		model.SignalRebooting,
	} {
		marker := `signal "` + string(sig) + `"`
		if !strings.Contains(got, marker) {
			t.Errorf("kickstart 里缺少信号 %q——状态机靠它划定阶段边界，"+
				"缺了就一直等到 §3.3 的阶段预算耗尽判失败（实测：pre hook "+
				"回报 rc=0 之后再无信号，任务停在 installing/pre 直到 10 分钟超时）",
				sig)
		}
	}
}

// TestKickstart_SignalsPresentWithoutAnyHook 锁住没有 hook 时信号照样发。
//
// 这是本文件最重要的一条。信号是阶段边界标记，与 hook 是两件独立的事——
// 一个不挂任何 hook 的模板同样需要状态机知道它走到了哪一步。原实现的
// RenderStageSection 以「没有 hook 就整段不渲染」开头，信号跟着一起没了。
func TestKickstart_SignalsPresentWithoutAnyHook(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Hooks = nil

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}

	for _, sig := range []model.SignalKind{
		model.SignalPreStart, model.SignalPreDone,
		model.SignalPackagesStart, model.SignalPackagesDone,
		model.SignalPostStart, model.SignalPostDone,
		model.SignalRebooting,
	} {
		if !strings.Contains(got, `signal "`+string(sig)+`"`) {
			t.Errorf("无 hook 的模板缺少信号 %q——信号是阶段边界标记，与 hook "+
				"是两件独立的事，不挂 hook 的模板同样需要状态机知道进度", sig)
		}
	}
}

// TestKickstart_PreDoneAfterPreHooks 锁住 pre-done 排在 pre 阶段 hook 之后。
//
// 顺序错了会让状态机以为 pre 阶段已结束，而 hook 还在跑——之后 hook 若失败，
// 失败会被归到下一个阶段，诊断信息指向错误的位置。
func TestKickstart_PreDoneAfterPreHooks(t *testing.T) {
	got, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}

	idxStart := strings.Index(got, `signal "pre-start"`)
	idxHook := strings.Index(got, "run_hook ")
	idxDone := strings.Index(got, `signal "pre-done"`)
	if idxStart == -1 || idxDone == -1 {
		t.Fatalf("pre-start 或 pre-done 缺失:\n%s", got)
	}
	if idxHook != -1 && !(idxStart < idxHook && idxHook < idxDone) {
		t.Errorf("顺序应为 pre-start → run_hook → pre-done，实际位置 "+
			"start=%d hook=%d done=%d——顺序错了会让 hook 的失败被归到下一个阶段，"+
			"诊断信息指向错误的位置", idxStart, idxHook, idxDone)
	}
}

// TestKickstart_SignalsInsideSections 锁住信号落在 %pre/%post 段落内。
//
// kickstart 的语法要求：段落外的裸文本 anaconda 会当语法错误或直接忽略。
// 这条与 golden_test.go 里 TestGolden_FirstbootScaffoldWrappedInPostSection
// 是同一类保护，那条测的是 firstboot 脚手架，本条测信号。
func TestKickstart_SignalsInsideSections(t *testing.T) {
	got, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}

	for _, sig := range []string{"pre-start", "pre-done", "post-start", "post-done"} {
		idx := strings.Index(got, `signal "`+sig+`"`)
		if idx == -1 {
			continue // 缺失由别的测试报
		}
		before := got[:idx]
		lastSectionHead := strings.LastIndexAny(before, "%")
		if lastSectionHead == -1 {
			t.Errorf("信号 %q 之前没有任何段落头（%%pre/%%post），说明它游离在所有"+
				"段落之外——anaconda 会当语法错误或直接忽略", sig)
			continue
		}
		// 段落头之后、信号之前不该出现 %end（那意味着段落已经关闭）。
		between := got[lastSectionHead:idx]
		if strings.Contains(between, "%end") {
			t.Errorf("信号 %q 出现在 %%end 之后，游离在段落之外", sig)
		}
	}
}
