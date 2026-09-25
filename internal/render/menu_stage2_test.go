// 本文件锁住 RHEL 菜单把 stage2 与包仓库分成两个参数。
//
// **这两个位置在真实 RHEL 9 系介质上不是同一个目录**，用一个参数指必然有一边
// 取不到（两种错法都实测过，症状不同但都是装机中止）：
//
//	inst.stage2  安装介质根，其下有 images/install.img（installer 的运行时
//	             镜像）与 .treeinfo。
//	inst.repo    包仓库，其下有 repodata/repomd.xml。真实 RHEL 9 系在
//	             BaseOS/ 下，不在介质根。
//
// 错法一（只用 inst.repo 指包仓库，2026-09-25 实测）：
//
//	".../repo/BaseOS//images/install.img"  404
//	".../repo/BaseOS//LiveOS/squashfs.img" 404
//
// anaconda 取不到 stage2，任务停在 installing/pre。
//
// 错法二（只用 inst.repo 指介质根，2026-09-25 实测）：stage2 取到了
// （install.img 200，933MB），但装包阶段
//
//	".../repo//.treeinfo"              404
//	".../repo/repodata/repomd.xml"     404（重试 4 次）
//
// 任务停在 installing/packages，目标盘一个字节都没写。
//
// 两个参数各指各的位置才两边都对——这也是 anaconda 文档里网络安装的标准写法。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestRockyMenu_Stage2AndRepoAreSeparate 锁住两个参数都存在且指向不同位置。
func TestRockyMenu_Stage2AndRepoAreSeparate(t *testing.T) {
	ctx := menuCtxRHEL()
	// 真实 RHEL 9 系布局：repodata 在 BaseOS/ 下，images/ 在介质根。
	ctx.Artifact.Paths.MediaRoot = "repo/"
	ctx.Artifact.Paths.Repo = "repo/BaseOS/"

	got, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}

	stage2 := extractKernelArg(got, "inst.stage2=")
	repo := extractKernelArg(got, "inst.repo=")

	if stage2 == "" {
		t.Fatalf("菜单里没有 inst.stage2=——anaconda 会退回从 inst.repo 根找 "+
			"images/install.img，而真实 RHEL 9 系的包仓库在 BaseOS/ 下、那里没有"+
			" stage2。完整菜单:\n%s", got)
	}
	if repo == "" {
		t.Fatalf("菜单里没有 inst.repo=——anaconda 装包阶段会找不到 "+
			"repodata/repomd.xml。完整菜单:\n%s", got)
	}
	if !strings.HasSuffix(stage2, "/repo/") {
		t.Errorf("inst.stage2 应指向安装介质根（MediaRoot），实际 %q", stage2)
	}
	if !strings.HasSuffix(repo, "/repo/BaseOS/") {
		t.Errorf("inst.repo 应指向包仓库（Repo），实际 %q", repo)
	}
	if stage2 == repo {
		t.Errorf("两个参数指向同一个位置 %q——真实 RHEL 9 系布局下它们不同，"+
			"指同一个必然有一边取不到", stage2)
	}
}

// TestRockyMenu_FlatLayoutBothSame 锁住扁平布局下两者相同是允许的。
//
// RHEL 7 系与一些精简介质把 repodata 直接放在 ISO 根，此时 MediaRoot 与 Repo
// 相同，两个参数指同一个位置是正确的——不是上一条要防的那种错误。
func TestRockyMenu_FlatLayoutBothSame(t *testing.T) {
	ctx := menuCtxRHEL()
	ctx.Artifact.Paths.MediaRoot = "repo/"
	ctx.Artifact.Paths.Repo = "repo/"

	got, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}
	stage2 := extractKernelArg(got, "inst.stage2=")
	repo := extractKernelArg(got, "inst.repo=")
	if stage2 != repo {
		t.Errorf("扁平布局下两者应相同，实际 stage2=%q repo=%q", stage2, repo)
	}
	if stage2 == "" {
		t.Errorf("扁平布局下 inst.stage2 仍应存在:\n%s", got)
	}
}

// TestRockyMenu_MediaRootFallback 锁住 MediaRoot 为空时回落到 Repo。
//
// 兼容性要求：Phase 1/2 导入的制品集这一列是空的，不能因为菜单新增了
// inst.stage2 就渲染出空值——`inst.stage2=` 带空值会让 anaconda 在找不到
// 任何介质的情况下失败，而那个失败只能到 BMC 控制台看。
func TestRockyMenu_MediaRootFallback(t *testing.T) {
	ctx := menuCtxRHEL()
	ctx.Artifact.Paths.MediaRoot = ""
	ctx.Artifact.Paths.Repo = "repo/"

	got, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}
	stage2 := extractKernelArg(got, "inst.stage2=")
	if stage2 == "" || strings.HasSuffix(stage2, "=") {
		t.Errorf("MediaRoot 为空时 inst.stage2 应回落到 Repo，实际 %q", stage2)
	}
}

// extractKernelArg 从菜单里取某个内核参数的值（到下一个空白或行尾为止）。
func extractKernelArg(menu, prefix string) string {
	idx := strings.Index(menu, prefix)
	if idx == -1 {
		return ""
	}
	rest := menu[idx+len(prefix):]
	if end := strings.IndexAny(rest, " \t\n\\"); end != -1 {
		return rest[:end]
	}
	return rest
}

// 确保 model 被引用（menuCtxRHEL 在同包的另一个测试文件里）。
var _ = model.OSFamilyRHEL
