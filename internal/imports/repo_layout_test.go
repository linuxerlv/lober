// 本文件锁住 RHEL 系制品集里「安装介质根」与「包索引目录」是两个不同路径。
//
// **混用这两者会让 anaconda 在取 stage2 时 404，装机卡在 installing。**
// 实测（2026-09-25，QEMU BIOS 目标机接真实服务端，任务已进 installing/pre）：
//
//	14:13:54 10.20.20.112 ".../repo/BaseOS//images/install.img"   404
//	14:13:55 10.20.20.112 ".../repo/BaseOS//LiveOS/squashfs.img"  404
//	14:13:55 10.20.20.112 ".../repo/BaseOS//.treeinfo"            404
//
// anaconda 拿 inst.repo 当**安装介质根**，在其下找 images/install.img
// （installer 的 stage2 运行时镜像）与 .treeinfo。而 ExtractRHEL 把整个 ISO
// 树复制到 repo/，images/ 落在 repo/images/，于是 repo/BaseOS/images/ 不存在。
//
// 为什么 Paths.Repo 会是 repo/BaseOS/：commit 3468d64 为修「RHEL 仓库路径
// 硬编码成 ISO 根」把它改成按 .treeinfo 的 repository 声明取——那一改让
// precheck 能找到 repodata/repomd.xml（真实 RHEL 9 系的 repodata 确实在
// BaseOS/ 下，不在根），但同时让 inst.repo 指错了地方。一个字段承担了两种
// 语义不同的用途。
package imports

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/model"
)

// rhelRootWithVariant 造一份真实 RHEL 9 系布局：repodata 在 BaseOS/ 下，
// stage2 镜像在根的 images/ 下，.treeinfo 声明 repository = BaseOS。
func rhelRootWithVariant() fstest.MapFS {
	return fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte(
			"[general]\narch = x86_64\nversion = 9.6\nrepository = BaseOS\nvariant = BaseOS\n" +
				"[images-x86_64]\nkernel = images/pxeboot/vmlinuz\ninitrd = images/pxeboot/initrd.img\n")},
		"images/pxeboot/vmlinuz":    &fstest.MapFile{Data: []byte("kernel-bytes")},
		"images/pxeboot/initrd.img": &fstest.MapFile{Data: []byte("initrd-bytes")},
		// stage2：anaconda 从 inst.repo 根下取它。
		"images/install.img":          &fstest.MapFile{Data: []byte("stage2-bytes")},
		"BaseOS/repodata/repomd.xml":  &fstest.MapFile{Data: []byte("<repomd/>")},
		"BaseOS/Packages/bash.rpm":    &fstest.MapFile{Data: []byte("rpm-bytes")},
		"Minimal/repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
	}
}

// TestExtractRHEL_MediaRootHoldsStage2 锁住 MediaRoot 指向的目录下能找到
// images/install.img。
//
// 这是 anaconda 取 stage2 的位置，也是 inst.repo 该指的地方。
func TestExtractRHEL_MediaRootHoldsStage2(t *testing.T) {
	destDir := t.TempDir()
	paths, err := ExtractRHEL(rhelRootWithVariant(), destDir)
	if err != nil {
		t.Fatalf("ExtractRHEL 失败: %v", err)
	}

	if paths.MediaRoot == "" {
		t.Fatal("MediaRoot 为空——anaconda 的 inst.repo 需要指向安装介质根" +
			"（其下有 images/install.img 与 .treeinfo），不能与包索引目录混用")
	}

	stage2 := filepath.Join(destDir, filepath.FromSlash(paths.MediaRoot), "images", "install.img")
	if _, err := os.Stat(stage2); err != nil {
		t.Errorf("MediaRoot=%q 之下找不到 images/install.img（%v）——anaconda 会在"+
			"取 stage2 时 404，装机卡在 installing 阶段", paths.MediaRoot, err)
	}
}

// TestExtractRHEL_RepoHoldsPackageIndex 锁住 Repo 指向的目录下能找到
// repodata/repomd.xml。
//
// 这是 precheck 的制品完整性闸门要查的位置（model.RepoIndexPattern），也是
// 包管理器取索引的位置。它与 MediaRoot **不是同一个目录**：真实 RHEL 9 系的
// repodata 在 BaseOS/ 下，而 images/ 在根。
func TestExtractRHEL_RepoHoldsPackageIndex(t *testing.T) {
	destDir := t.TempDir()
	paths, err := ExtractRHEL(rhelRootWithVariant(), destDir)
	if err != nil {
		t.Fatalf("ExtractRHEL 失败: %v", err)
	}

	repomd := filepath.Join(destDir, filepath.FromSlash(paths.Repo), "repodata", "repomd.xml")
	if _, err := os.Stat(repomd); err != nil {
		t.Errorf("Repo=%q 之下找不到 repodata/repomd.xml（%v）——precheck 的制品"+
			"完整性闸门会拒绝这个制品集", paths.Repo, err)
	}
}

// TestExtractRHEL_MediaRootAndRepoDiffer 锁住这份真实布局下两者确实不同。
//
// 如果它们相同，说明有人把其中一个改回了另一个的值——那会让两类消费者里的
// 一个 404：inst.repo 指向 BaseOS/ 则 stage2 取不到，precheck 查根目录则
// repomd.xml 找不到。
func TestExtractRHEL_MediaRootAndRepoDiffer(t *testing.T) {
	paths, err := ExtractRHEL(rhelRootWithVariant(), t.TempDir())
	if err != nil {
		t.Fatalf("ExtractRHEL 失败: %v", err)
	}
	if paths.MediaRoot == paths.Repo {
		t.Errorf("MediaRoot 与 Repo 都是 %q——真实 RHEL 9 系布局下 repodata 在 "+
			"BaseOS/ 下、images/ 在介质根，两者必须不同，否则两类消费者里必有"+
			"一个 404", paths.Repo)
	}
}

// TestExtractRHEL_FlatLayoutBothPointToRoot 锁住老式扁平布局（repodata 直接
// 在 ISO 根）下两者都指向根。
//
// RHEL 7 系与一些精简介质是这种布局，.treeinfo 不声明 repository。此时
// MediaRoot 与 Repo 相同是正确的，不是上一条测试要防的那种错误。
func TestExtractRHEL_FlatLayoutBothPointToRoot(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte(
			"[general]\narch = x86_64\nversion = 7.9\n" +
				"[images-x86_64]\nkernel = images/pxeboot/vmlinuz\ninitrd = images/pxeboot/initrd.img\n")},
		"images/pxeboot/vmlinuz":    &fstest.MapFile{Data: []byte("k")},
		"images/pxeboot/initrd.img": &fstest.MapFile{Data: []byte("i")},
		"images/install.img":        &fstest.MapFile{Data: []byte("s")},
		"repodata/repomd.xml":       &fstest.MapFile{Data: []byte("<repomd/>")},
	}

	paths, err := ExtractRHEL(root, t.TempDir())
	if err != nil {
		t.Fatalf("ExtractRHEL 失败: %v", err)
	}
	if paths.MediaRoot != paths.Repo {
		t.Errorf("扁平布局下 MediaRoot=%q 与 Repo=%q 应相同（repodata 就在介质根）",
			paths.MediaRoot, paths.Repo)
	}
}

// TestArtifactPaths_MediaRootFallsBackToRepo 锁住 MediaRoot 为空时回落到 Repo。
//
// 兼容性要求：Phase 1/2 导入的制品集这一列为空（数据库里的历史记录），不能
// 因为加了这个字段就让它们全部渲染出空的 inst.repo。
func TestArtifactPaths_MediaRootFallsBackToRepo(t *testing.T) {
	p := model.ArtifactPaths{Repo: "repo/", MediaRoot: ""}
	if got := p.MediaRootOrRepo(); got != "repo/" {
		t.Errorf("MediaRoot 为空时应回落到 Repo，got %q", got)
	}
	p.MediaRoot = "repo/"
	p.Repo = "repo/BaseOS/"
	if got := p.MediaRootOrRepo(); got != "repo/" {
		t.Errorf("MediaRoot 非空时应用它，got %q", got)
	}
}
