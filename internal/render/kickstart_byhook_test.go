// 本文件测试 by-hook 磁盘策略下的分区渲染。
//
// **这条路径此前渲染出的是一份必然失败的 kickstart。** by-hook 策略的语义
// 是「不在服务端选盘，交给 %pre 阶段的 hook 动态决定」（provider/
// diskplanner.go 的 diskPlannerByHook），因此它不产出 LOBER_DISK_PRIMARY；
// 而 baseDirectives 无条件把这个 fact 插进 clearpart/part 两行，于是渲染
// 结果是：
//
//	clearpart --all --drives=
//	part / --fstype=xfs --grow --ondisk=
//
// anaconda 拿到空的 --drives= 会直接报错退出。也就是说 by-hook 这个
// SPEC §2.3 列出的五个策略之一，在真实装机路径上从来不可用。
//
// 这个缺口此前被两层掩盖：六个 e2e 脚本全都用 by-hook（因为它是无硬件事实
// 时唯一能建出任务的策略），但它们只用 curl 核对 HTTP 状态码与状态机流转，
// 从不把渲染出的 kickstart 交给真的 anaconda；而单元测试的 RenderContext
// 都手工塞了 LOBER_DISK_PRIMARY，走不到空值分支。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// byHookContext 造一个 by-hook 策略、且不含 LOBER_DISK_PRIMARY 的渲染上下文
// ——这正是真实建任务路径在 by-hook 下产出的形态。
func byHookContext(t *testing.T) model.RenderContext {
	t.Helper()
	ctx := minimalKickstartCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyByHook
	delete(ctx.Facts, model.FactDiskPrimary)
	return ctx
}

// TestKickstart_ByHookEmitsPartitionInclude 锁住 by-hook 下渲染出
// `%include <路径>` 而不是带空值的 clearpart/part。
//
// anaconda 的标准做法：%pre 段把分区指令写进一个文件，主体用 %include 引入
// 它。这是 by-hook「让 hook 决定分区」唯一能落地的机制——kickstart 是
// anaconda 开始装机前就要拿到的完整文本，服务端此刻还没有任何硬件信息，
// 只有机器自己跑起来的 %pre 才看得到盘。
func TestKickstart_ByHookEmitsPartitionInclude(t *testing.T) {
	got, err := AssembleKickstart(byHookContext(t))
	if err != nil {
		t.Fatalf("by-hook 渲染失败: %v", err)
	}

	if !strings.Contains(got, "%include "+ByHookPartitionIncludePath) {
		t.Errorf("by-hook 应渲染出 %%include %s，实际输出:\n%s",
			ByHookPartitionIncludePath, got)
	}
	// 关键负面断言：不能再出现带空值的分区指令。
	for _, bad := range []string{"--drives=\n", "--ondisk=\n", "--drives= ", "--ondisk= "} {
		if strings.Contains(got, bad) {
			t.Errorf("渲染结果里出现了空值分区指令 %q——anaconda 会直接报错退出:\n%s", bad, got)
		}
	}
	if strings.Contains(got, "clearpart --all --drives=\n") {
		t.Error("clearpart 的 --drives= 是空值")
	}
}

// TestKickstart_ByHookDocumentsContractInComment 锁住渲染结果里写明了
// 「这个文件由 %pre 阶段的 hook 负责写」。
//
// 理由：by-hook 是把责任交给运维写的 hook，而 kickstart 是失败现场唯一能
// 拿到的证据（anaconda 会把它存成 /root/anaconda-ks.cfg）。如果 hook 忘了
// 写那个文件，装机会以「%include 的文件不存在」失败——现场的人必须能从
// kickstart 本身看出这个文件该由谁产生，否则只能去翻源码。
func TestKickstart_ByHookDocumentsContractInComment(t *testing.T) {
	got, err := AssembleKickstart(byHookContext(t))
	if err != nil {
		t.Fatalf("by-hook 渲染失败: %v", err)
	}
	if !strings.Contains(got, "#") || !strings.Contains(got, "by-hook") {
		t.Errorf("渲染结果应含注释说明 by-hook 契约，实际:\n%s", got)
	}
}

// TestKickstart_NonByHookWithEmptyDiskFactFails 锁住「非 by-hook 策略下
// 磁盘 fact 为空时报错」，不静默渲染出空值分区指令。
//
// 这是本文件最重要的一条。空的 LOBER_DISK_PRIMARY 在非 by-hook 策略下意味着
// 决策链某处断了（resolve.go 算出了主盘却没能送进 Facts，那是一个真实存在
// 的断口），此时渲染出 `--drives=` 会让失败推迟到 anaconda 启动之后才发生，
// 而那时唯一的证据在目标机的 BMC 控制台上。渲染期报错则把失败挡在服务端，
// 有完整的任务上下文可查。
func TestKickstart_NonByHookWithEmptyDiskFactFails(t *testing.T) {
	for _, policy := range []model.DiskPolicy{
		model.DiskPolicyFirst, model.DiskPolicyLargest,
		model.DiskPolicyByModel, model.DiskPolicyRAID1SSD,
	} {
		ctx := minimalKickstartCtx()
		ctx.Template.DiskPolicy = policy
		delete(ctx.Facts, model.FactDiskPrimary)

		_, err := AssembleKickstart(ctx)
		if err == nil {
			t.Errorf("策略 %s 下磁盘 fact 为空时必须报错，不能渲染出空值分区指令", policy)
			continue
		}
		if !strings.Contains(err.Error(), model.FactDiskPrimary) {
			t.Errorf("策略 %s 的报错应点明缺哪个 fact，实际: %v", policy, err)
		}
	}
}

// TestKickstart_NonByHookWithDiskFactUnchanged 回归保护：非 by-hook 且磁盘
// fact 有值时，输出与改动前逐字节一致（既有 golden 覆盖的就是这条路径）。
func TestKickstart_NonByHookWithDiskFactUnchanged(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyLargest
	ctx.Facts[model.FactDiskPrimary] = "/dev/disk/by-id/wwn-0xabc"

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(got, "clearpart --all --initlabel --drives=/dev/disk/by-id/wwn-0xabc") {
		t.Errorf("有磁盘 fact 时应照原样渲染 clearpart，实际:\n%s", got)
	}
	if strings.Contains(got, "%include") {
		t.Error("非 by-hook 策略不应渲染 %include")
	}
}
