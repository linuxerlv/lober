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

// TestKickstart_PackagesStartBeforePackagesSection 锁住 packages-start 在
// %pre 段末尾发出，而不是在某个 %post 段里。
//
// **kickstart 没有「装包开始」这个钩子。** anaconda 的段落只有 %pre（分区前）、
// %post --nochroot 与 %post（都在装包之后），所以「装包即将开始」这个时刻只能
// 由 %pre 的末尾代表——那正是 anaconda 接下来要去装包的前一刻。
//
// 把 packages-start 放进 %post 段的后果（实测 2026-09-26）：pre-start 与
// pre-done 都正常到达，但 packages-start 永远不来，状态机停在 PhasePre 等到
// §3.3 的 10 分钟预算耗尽：
//
//	pre-start?rc=0  →  hook-report?rc=0  →  pre-done?rc=0  →  （无）
//	logTail: "阶段 pre 超时: 预算=10m0s 实际已过=615秒"
//
// 而 anaconda 其实正在装包——它要等装完才跑 %post，那时状态机早判失败了。
func TestKickstart_PackagesStartBeforePackagesSection(t *testing.T) {
	got, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}

	idxPreDone := strings.Index(got, `signal "pre-done"`)
	idxPkgStart := strings.Index(got, `signal "packages-start"`)
	idxPackages := strings.Index(got, "%packages")
	idxFirstPost := strings.Index(got, "%post")

	if idxPkgStart == -1 {
		t.Fatalf("packages-start 缺失:\n%s", got)
	}
	if idxPreDone == -1 {
		t.Fatalf("pre-done 缺失:\n%s", got)
	}
	// packages-start 必须在第一个 %post 之前——%post 跑在装包之后，放那里
	// 等于永远不发。
	if idxFirstPost != -1 && idxPkgStart > idxFirstPost {
		t.Errorf("packages-start 落在 %%post 段之后（位置 %d > %d）——anaconda 的 "+
			"%%post 跑在装包**之后**，状态机会停在 PhasePre 等到阶段预算耗尽",
			idxPkgStart, idxFirstPost)
	}
	// 它应当紧跟在 pre-done 之后（同在 %pre 段末尾）。
	if idxPkgStart < idxPreDone {
		t.Errorf("packages-start 排在 pre-done 之前（%d < %d）——阶段顺序颠倒，"+
			"§3.6 的只进不退判定会把 pre-done 当成回退丢弃", idxPkgStart, idxPreDone)
	}
	// 刻意不断言 packages-start 与 %packages 段的相对位置：%packages 是**声明**
	// 段（告诉 anaconda 装哪些包），位置在 kickstart 主体靠前；%pre 是执行段，
	// 写在其后。anaconda 的执行顺序是 %pre → 装包 → %post，与 %packages 这个
	// 声明写在文件哪一行无关。用文本位置去断言执行顺序在这里是错的判据。
	_ = idxPackages
}

// TestKickstart_PackagesDoneInPostSection 锁住 packages-done 在 %post 段里。
//
// 与 packages-start 相反：装包**结束**这个时刻只能由 %post 的开头代表，因为
// anaconda 正是装完包才跑 %post。两个信号因此落在不同段落——这不是不对称的
// 设计瑕疵，是 kickstart 的段落语义决定的。
func TestKickstart_PackagesDoneInPostSection(t *testing.T) {
	got, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}
	idxPkgDone := strings.Index(got, `signal "packages-done"`)
	idxFirstPost := strings.Index(got, "%post")
	if idxPkgDone == -1 {
		t.Fatalf("packages-done 缺失:\n%s", got)
	}
	if idxFirstPost == -1 || idxPkgDone < idxFirstPost {
		t.Errorf("packages-done 应在 %%post 段内发出（装完包才跑 %%post），"+
			"实际位置 %d，第一个 %%post 在 %d", idxPkgDone, idxFirstPost)
	}
}

// TestKickstart_SignalOrderIsMonotonic 锁住信号在 kickstart 里的出现顺序与
// model.signalTable 的 Seq 单调一致。
//
// **这是 §3.6「只进不退」的直接要求。** statemachine/signal.go:78 的判据是
// `entry.Seq <= current.LastSeq` 就丢弃——序号倒退的信号被当成重复/回退，
// 静默忽略。所以渲染出的顺序必须与 Seq 升序一致，否则某些信号发了也不算：
//
//	pre-start 10 → pre-done 20 → packages-start 30 → packages-done 40
//	→ post-start 50 → post-done 60 → rebooting 70
//
// 最容易踩的是 packages-done：装包结束这个时刻只能由 %post 的开头代表（见
// TestKickstart_PackagesDoneInPostSection），而 post-start/post-done 也在
// %post 段里——如果 packages-done 那段排在 hook 的 %post 段之后，它的 Seq 40
// 就会撞上已经推到 60 的水位而被丢弃。
func TestKickstart_SignalOrderIsMonotonic(t *testing.T) {
	got, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}

	// 按 Seq 升序列出装机期信号，逐个取它在文本里的首次出现位置。
	ordered := []model.SignalKind{
		model.SignalPreStart, model.SignalPreDone,
		model.SignalPackagesStart, model.SignalPackagesDone,
		model.SignalPostStart, model.SignalPostDone,
		model.SignalRebooting,
	}

	prevIdx, prevSig := -1, model.SignalKind("")
	for _, sig := range ordered {
		idx := strings.Index(got, `signal "`+string(sig)+`"`)
		if idx == -1 {
			t.Errorf("信号 %q 缺失", sig)
			continue
		}
		if idx < prevIdx {
			entry, _ := model.LookupSignal(sig)
			prevEntry, _ := model.LookupSignal(prevSig)
			t.Errorf("信号顺序倒退：%q（Seq=%d，位置 %d）出现在 %q（Seq=%d，位置 %d）"+
				"之前。statemachine/signal.go 对 Seq 不大于当前水位的信号直接丢弃，"+
				"所以这个信号发了也不算——状态机会卡在前一个阶段等到预算耗尽",
				sig, entry.Seq, idx, prevSig, prevEntry.Seq, prevIdx)
		}
		prevIdx, prevSig = idx, sig
	}
}
