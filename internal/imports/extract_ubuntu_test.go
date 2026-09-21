// 本文件测试 Ubuntu 系的抽取逻辑（对应实现计划 phase2-01 任务 5，依据
// SPEC §2.1、§4.6 第三项）。
package imports

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/model"
)

// ubuntuFakeRoot 构造一个最小的 Ubuntu ISO 内容视图：casper 目录下的内核、
// initrd 与 squashfs 镜像（计划任务 5："Ubuntu 的内核与 initrd 在 casper
// 目录下"，"仓库不是 deb 仓库而是一个 squashfs 镜像"），加 .disk/info
// 识别特征文件。
func ubuntuFakeRoot() fstest.MapFS {
	return fstest.MapFS{
		"casper/vmlinuz":             &fstest.MapFile{Data: []byte("fake-kernel")},
		"casper/initrd":              &fstest.MapFile{Data: []byte("fake-initrd")},
		"casper/filesystem.squashfs": &fstest.MapFile{Data: []byte("fake-squashfs-image")},
		".disk/info":                 &fstest.MapFile{Data: []byte("Ubuntu 24.04 LTS")},
	}
}

// TestExtractUbuntu_ProducesCorrectPathStructure 锁住"两个家族的抽取各自
// 产出正确的路径结构"（计划任务 5 测试要求第一条）。
func TestExtractUbuntu_ProducesCorrectPathStructure(t *testing.T) {
	root := ubuntuFakeRoot()
	destDir := t.TempDir()

	paths, err := ExtractUbuntu(root, destDir)
	if err != nil {
		t.Fatalf("ExtractUbuntu 返回错误: %v", err)
	}

	if paths.Kernel != "vmlinuz" {
		t.Errorf("Ubuntu 内核固定输出文件名应为 vmlinuz（SPEC §4.5），got %q", paths.Kernel)
	}
	if len(paths.Initrd) != 1 || paths.Initrd[0] != "initrd" {
		t.Errorf("Ubuntu initrd 固定输出文件名应为 initrd（SPEC §4.5），got %v", paths.Initrd)
	}

	for _, rel := range []string{"vmlinuz", "initrd"} {
		if _, err := os.Stat(filepath.Join(destDir, rel)); err != nil {
			t.Errorf("落盘文件 %q 应存在: %v", rel, err)
		}
	}
}

// TestExtractUbuntu_SquashfsRecordedInExtra 锁住"Ubuntu 的 squashfs 镜像
// 被记入额外文件且被完整性校验覆盖"（计划任务 5 测试要求第二条）。
//
// "被完整性校验覆盖"这半句由 internal/statemachine/precheck.go 的
// artifactCheckPaths 函数保证（它遍历 ArtifactPaths.Extra 的全部值做
// 存在性检查，是通用逻辑，不需要为 Ubuntu 新增任何代码），本测试只锁住
// "被记入额外文件"这一半——squashfs 的相对路径必须出现在返回的
// ArtifactPaths.Extra 里。
func TestExtractUbuntu_SquashfsRecordedInExtra(t *testing.T) {
	root := ubuntuFakeRoot()
	destDir := t.TempDir()

	paths, err := ExtractUbuntu(root, destDir)
	if err != nil {
		t.Fatalf("ExtractUbuntu 返回错误: %v", err)
	}

	squashfsPath, ok := paths.Extra[UbuntuExtraKeySquashfs]
	if !ok {
		t.Fatalf("ArtifactPaths.Extra 应含键 %q，got Extra=%v", UbuntuExtraKeySquashfs, paths.Extra)
	}
	if _, err := os.Stat(filepath.Join(destDir, squashfsPath)); err != nil {
		t.Errorf("Extra 记录的 squashfs 相对路径 %q 在磁盘上应真实存在: %v", squashfsPath, err)
	}
}

// TestExtractUbuntu_MissingSquashfsReturnsError 锁住"缺关键文件时导入
// 置失败且原因具体"（计划任务 5 测试要求第四条），专门针对 Ubuntu 特有的
// squashfs 镜像缺失场景（计划任务 5 明确点名："Ubuntu 少了 squashfs 镜像
// 就装不了，而那个缺失在 Phase 1 的校验逻辑里不会被发现"——本测试锁住的
// 是抽取阶段就该拒绝，不是等完整性校验才发现）。
func TestExtractUbuntu_MissingSquashfsReturnsError(t *testing.T) {
	root := fstest.MapFS{
		"casper/vmlinuz": &fstest.MapFile{Data: []byte("fake-kernel")},
		"casper/initrd":  &fstest.MapFile{Data: []byte("fake-initrd")},
	}
	destDir := t.TempDir()

	_, err := ExtractUbuntu(root, destDir)
	if err == nil {
		t.Fatal("缺 squashfs 镜像时 ExtractUbuntu 应返回错误")
	}
}

// TestExtractUbuntu_MissingKernelReturnsError 缺内核场景。
func TestExtractUbuntu_MissingKernelReturnsError(t *testing.T) {
	root := fstest.MapFS{
		"casper/initrd":              &fstest.MapFile{Data: []byte("fake-initrd")},
		"casper/filesystem.squashfs": &fstest.MapFile{Data: []byte("fake-squashfs")},
	}
	destDir := t.TempDir()

	_, err := ExtractUbuntu(root, destDir)
	if err == nil {
		t.Fatal("缺内核文件时 ExtractUbuntu 应返回错误")
	}
}

// TestIdentifyOrder_UbuntuBeforeDebian 锁住"同时含 Debian 与 Ubuntu 特征
// 时识别为 Ubuntu"（计划任务 5 测试要求第三条，与 identify_test.go 的
// TestIdentify_AmbiguousUbuntuAndDebianResolvesToUbuntu 是同一条不变量，
// 这里加入更接近真实 Ubuntu ISO 的完整特征文件集合后再验证一遍——确认
// 加入 casper/squashfs 等真实特征后判定顺序仍正确，不仅仅是 identify.go
// 单元测试里的极简 fixture）。
func TestIdentifyOrder_UbuntuBeforeDebian(t *testing.T) {
	root := ubuntuFakeRoot()
	// 手动补上 dists/ 目录，模拟"同时含 Debian 与 Ubuntu 特征"的真实歧义
	// 场景（计划任务 5 原文点名："Ubuntu 的 ISO 也含 dists/"）。
	root["dists/stable/Release"] = &fstest.MapFile{Data: []byte("")}

	family, err := Identify(root)
	if err != nil {
		t.Fatalf("Identify 返回错误: %v", err)
	}
	if family != model.OSFamilyUbuntu {
		t.Fatalf("同时含真实 Ubuntu 特征集合与 dists/ 时应识别为 Ubuntu，got %s", family)
	}
}
