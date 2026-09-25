// 本文件锁住 by-hook 策略下 %include 排在 %pre 段之后。
//
// **anaconda 在解析 kickstart 时就要展开 %include，而那时 %pre 还没跑。**
// %pre 写出的文件只能被 %pre **之后**处理的内容引用——把 %include 放在文件
// 前部（与 lang/keyboard 那批基础指令同区）时，anaconda 读到它时
// /tmp/lober-part-include 根本不存在。
//
// 实测（2026-09-26，QEMU BIOS 目标机，前面十九个缺口都已修掉）：
//
//	%include 在第 8 行，%pre 在第 18 行
//
// 结果：repodata 的全部索引取到（200），anaconda 在 209-213 秒后
// `dracut Warning: Killing all remaining processes` 并重启，**目标盘只有
// 192KB**——分区从未执行过。它在校验 kickstart 阶段就失败了，那一步排在分区
// 与装包之前。
//
// 失败信息拿不到（串口、kickstart logging、inst.syslog 三条通道都试过，
// anaconda 自己的输出一条都没出来），所以这个缺口只能靠「盘上什么都没有」
// 这个物理证据反推：分区没跑，而 %pre 明明成功写出了文件（hook-report rc=0，
// 服务端收到了它打印的分区指令正文）。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestKickstart_IncludeAfterPreSection 锁住 %include 排在 %pre 段之后。
func TestKickstart_IncludeAfterPreSection(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyByHook
	delete(ctx.Facts, model.FactDiskPrimary)

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}

	idxInclude := strings.Index(got, "%include "+ByHookPartitionIncludePath)
	idxPre := strings.Index(got, "\n%pre\n")
	if idxInclude == -1 {
		t.Fatalf("by-hook 策略下应渲染 %%include:\n%s", got[:500])
	}
	if idxPre == -1 {
		t.Fatalf("找不到 %%pre 段:\n%s", got[:500])
	}

	// %include 必须在 %pre 段**结束之后**——anaconda 解析到 %include 时会立刻
	// 去读那个文件，而 %pre 是在解析完成后才执行的。
	preEnd := strings.Index(got[idxPre:], "\n%end\n")
	if preEnd == -1 {
		t.Fatalf("%%pre 段没有 %%end:\n%s", got[idxPre:idxPre+300])
	}
	preEnd += idxPre

	if idxInclude < preEnd {
		t.Errorf("%%include 在位置 %d，%%pre 段在 %d-%d ——include 排在 pre 之前或"+
			"之内时，anaconda 解析它时 %s 还不存在，会在校验阶段就失败退出"+
			"（实测：分区从未执行，目标盘只有 192KB）",
			idxInclude, idxPre, preEnd, ByHookPartitionIncludePath)
	}
}

// TestKickstart_NonByHookHasNoIncludeOrdering 回归保护：非 by-hook 策略下不该
// 出现 %include，分区指令直接写在基础指令区。
func TestKickstart_NonByHookHasNoIncludeOrdering(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyLargest
	ctx.Facts[model.FactDiskPrimary] = "/dev/disk/by-id/wwn-0xabc"

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}
	if strings.Contains(got, "%include") {
		t.Errorf("非 by-hook 策略不应渲染 %%include:\n%s", got[:400])
	}
	if !strings.Contains(got, "clearpart --all --drives=/dev/disk/by-id/wwn-0xabc") {
		t.Errorf("非 by-hook 策略应直接写分区指令:\n%s", got[:400])
	}
}

// TestKickstart_IncludeCommentTravelsWithIt 锁住说明注释跟着 %include 一起移动。
//
// 那两行注释是失败现场唯一的线索来源（anaconda 会把用过的 kickstart 存成
// /root/anaconda-ks.cfg）。如果 %include 移了位而注释留在原处，现场的人会在
// 文件前部看到一段解释、却找不到它说的那一行。
func TestKickstart_IncludeCommentTravelsWithIt(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyByHook
	delete(ctx.Facts, model.FactDiskPrimary)

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}
	idxComment := strings.Index(got, "分区指令由 %pre 阶段的 hook 写出")
	idxInclude := strings.Index(got, "%include "+ByHookPartitionIncludePath)
	if idxComment == -1 {
		t.Fatalf("找不到说明注释——它是失败现场唯一的线索来源:\n%s", got[:400])
	}
	if idxComment > idxInclude {
		t.Error("说明注释应紧排在 %include 之前")
	}
	// 两者之间不该隔着别的段落。
	between := got[idxComment:idxInclude]
	if strings.Contains(between, "%end") || strings.Contains(between, "%post") {
		t.Errorf("注释与 %%include 之间隔着别的段落:\n%s", between)
	}
}
