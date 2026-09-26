// 本文件锁住 kickstart 声明安装源（url 指令）。
//
// **`inst.repo=` 内核参数不能替代 kickstart 里的 `url` 指令。** 前者告诉
// installer 去哪取 stage2 与默认仓库，后者是 kickstart 自己的安装源声明——
// `%packages` 要装包，而 anaconda 在 inst.cmdline（非交互）模式下遇到「要装包
// 但没有安装源声明」这种未应答的问题会**立即退出**，不等任何人。
//
// 实测（2026-09-26，QEMU BIOS 目标机，串口与 inst.cmdline 都已配好）：
//
//	19:44:07  repo/BaseOS/repodata/repomd.xml            200  3974
//	19:44:07  repo/BaseOS/repodata/0186e493e67d2a78...   200
//	19:47:19  /boot.ipxe                                 200  132   ← 机器重启了
//
// 索引全部取到，三分钟后机器重新 PXE 引导——anaconda 解完 repodata 就退出了。
// 任务停在 installing/packages 直到阶段预算耗尽。
//
// 对照 Debian：preseed 有 `mirror/http/hostname` 与 `mirror/http/directory`
// 两行（preseed_full.golden 里能看到），也就是说那一家从一开始就声明了安装源，
// 只有 RHEL 这条路径漏了。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestKickstart_DeclaresInstallSource 锁住 url 指令存在且指向包仓库。
func TestKickstart_DeclaresInstallSource(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Artifact.Paths.MediaRoot = "repo/"
	ctx.Artifact.Paths.Repo = "repo/BaseOS/"

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}

	if !strings.Contains(got, "url --url=") {
		t.Fatalf("kickstart 没有 url 指令——%%packages 要装包却没有安装源声明，"+
			"anaconda 在 inst.cmdline 模式下会立即退出（实测：解完 repodata 三分钟后"+
			"机器重新 PXE 引导）。kickstart:\n%s", got[:600])
	}

	line := ""
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "url --url=") {
			line = l
			break
		}
	}
	if !strings.Contains(line, "/repo/BaseOS/") {
		t.Errorf("url 指令应指向**包仓库**（Repo，有 repodata/），不是介质根——"+
			"anaconda 拿它当 yum 仓库根。实际: %s", line)
	}
}

// TestKickstart_UrlBeforePackages 锁住 url 排在 %packages 之前。
//
// kickstart 是声明式的，但 anaconda 的解析器对「装包时安装源还未声明」这种
// 顺序仍然敏感。放在前面与 lang/keyboard 那批基础指令同区，符合 anaconda 文档
// 里的示例顺序。
func TestKickstart_UrlBeforePackages(t *testing.T) {
	got, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}
	idxURL := strings.Index(got, "url --url=")
	idxPkg := strings.Index(got, "%packages")
	if idxURL == -1 || idxPkg == -1 {
		t.Fatalf("url 或 %%packages 缺失:\n%s", got[:600])
	}
	if idxURL > idxPkg {
		t.Errorf("url 指令应排在 %%packages 之前（位置 %d > %d）", idxURL, idxPkg)
	}
}

// TestKickstart_UrlFallsBackToMediaRoot 锁住 Repo 为空时回落。
//
// 兼容性：极简介质的 .treeinfo 不声明 repository，此时 Repo 与 MediaRoot 都是
// 介质根。渲染出 `url --url=` 空值会让 anaconda 在没有安装源的情况下失败——
// 与本文件要修的缺口是同一个症状。
func TestKickstart_UrlFallsBackToMediaRoot(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Artifact.Paths.Repo = ""
	ctx.Artifact.Paths.MediaRoot = "repo/"

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "url --url=") {
			if strings.HasSuffix(strings.TrimSpace(l), "url --url=") {
				t.Errorf("Repo 为空时渲染出了空的 url 指令: %q", l)
			}
			if !strings.Contains(l, "/repo/") {
				t.Errorf("Repo 为空时 url 应回落到 MediaRoot，实际: %s", l)
			}
			return
		}
	}
	t.Errorf("找不到 url 指令:\n%s", got[:400])
}

var _ = model.OSFamilyRHEL
