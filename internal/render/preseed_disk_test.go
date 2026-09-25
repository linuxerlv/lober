// 本文件锁住 preseed 的磁盘声明不会渲染出空值。
//
// **这是 kickstart 已经修过、preseed 没跟上的同一个缺口。** kickstart 的
// partitionDirectives 现在对空磁盘 fact 报错拒绝渲染（by-hook 策略另走
// %include），而 preseedPartitioning 仍然静默把空串塞进
// `d-i partman-auto/disk string `——debian-installer 拿到空值会因无盘可分而
// 停在分区界面等人，而那台机器没有人在看。
//
// 两家的机制不同，所以修法也不同：
//
//	kickstart  支持 %include，by-hook 策略能把选盘推到 %pre 阶段的 hook
//	preseed    partman-auto/disk 直接吃路径，**没有等价的延迟机制**
//	           （preseed/early_command 能跑脚本，但 partman 的 debconf 值在
//	           那之前就已经读定了）
//
// 于是 Debian 家族只能要求服务端已选定主盘。而建任务时服务端拿不到硬件事实
// （handleCreateTask 传给 ParamsResolver 的 facts 只有 SN/MAC/UUID/arch/
// os_family，不含磁盘），所以 Debian 目前只能用 by-hook 之外的策略配合
// inspect 阶段——那是 SPEC §11 Phase 2 的「inspect 阶段硬件采集」。
//
// 本文件要锁住的是：这个限制**在渲染期就明确报错**，而不是产出一份让
// debian-installer 停在分区界面的 preseed。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestPreseed_EmptyDiskFactFails 锁住空磁盘 fact 时报错而不是渲染空值。
func TestPreseed_EmptyDiskFactFails(t *testing.T) {
	ctx := minimalPreseedCtx()
	delete(ctx.Facts, model.FactDiskPrimary)

	_, err := AssemblePreseed(ctx)
	if err == nil {
		t.Fatal("磁盘 fact 为空时必须报错——渲染出 `partman-auto/disk string `" +
			"（空值）会让 debian-installer 因无盘可分而停在分区界面等人，" +
			"而那台机器没有人在看")
	}
	if !strings.Contains(err.Error(), model.FactDiskPrimary) {
		t.Errorf("报错应点明缺哪个 fact，实际: %v", err)
	}
}

// TestPreseed_ByHookPolicyAlsoFails 锁住 by-hook 策略对 Debian 同样报错。
//
// **这条与 kickstart 刻意不同。** kickstart 的 by-hook 走 %include，把选盘推到
// %pre 阶段的 hook；preseed 没有等价机制——partman-auto/disk 直接吃路径，而
// preseed/early_command 虽然能跑脚本，partman 的 debconf 值在那之前就已读定。
//
// 所以「Debian 模板配了 by-hook」是一个配置错误，必须在渲染期挡住并说清原因，
// 否则运维会以为它像 RHEL 那样能用。
func TestPreseed_ByHookPolicyAlsoFails(t *testing.T) {
	ctx := minimalPreseedCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyByHook
	delete(ctx.Facts, model.FactDiskPrimary)

	_, err := AssemblePreseed(ctx)
	if err == nil {
		t.Fatal("Debian + by-hook 必须报错：preseed 没有 %include 那样的延迟机制，" +
			"partman-auto/disk 直接吃路径")
	}
	if !strings.Contains(err.Error(), "by-hook") {
		t.Errorf("报错应说明 by-hook 对 Debian 不适用，实际: %v", err)
	}
}

// TestPreseed_WithDiskFactUnchanged 回归保护：有磁盘 fact 时输出不变。
func TestPreseed_WithDiskFactUnchanged(t *testing.T) {
	ctx := minimalPreseedCtx()
	ctx.Facts[model.FactDiskPrimary] = "/dev/disk/by-id/wwn-0xdeadbeef"

	got, err := AssemblePreseed(ctx)
	if err != nil {
		t.Fatalf("有磁盘 fact 时应正常渲染: %v", err)
	}
	if !strings.Contains(got, "partman-auto/disk") {
		t.Errorf("缺 partman-auto/disk 行:\n%s", got[:400])
	}
	if !strings.Contains(got, "/dev/disk/by-id/wwn-0xdeadbeef") {
		t.Errorf("磁盘路径没进 preseed:\n%s", got[:400])
	}
}
