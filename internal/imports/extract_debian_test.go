// 本文件测试 Debian 系的抽取逻辑（对应实现计划 phase2-01 任务 5，依据
// SPEC §2.1、§4.6 第三项）。抽取逻辑通过内存文件系统测试，不依赖真 ISO
// （M7 已立的规矩，manifest_test.go 的 ExtractRHEL 测试同样如此）。
package imports

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// debianFakeRoot 构造一个最小的 Debian ISO 内容视图：installer 目录下的
// 内核与 initrd（按架构分子目录），加 dists/ 与 pool/ 两棵仓库树
// （计划任务 5："仓库树是 dists/ 与 pool/ 两棵"）。
func debianFakeRoot() fstest.MapFS {
	return fstest.MapFS{
		"install.amd/vmlinuz":                     &fstest.MapFile{Data: []byte("fake-kernel")},
		"install.amd/initrd.gz":                   &fstest.MapFile{Data: []byte("fake-initrd")},
		"dists/stable/Release":                    &fstest.MapFile{Data: []byte("Codename: bookworm\n")},
		"dists/stable/main/binary-amd64/Packages": &fstest.MapFile{Data: []byte("Package: bash\n")},
		"pool/main/b/bash/bash_5.2-5_amd64.deb":   &fstest.MapFile{Data: []byte("fake-deb")},
		".disk/base_installable":                  &fstest.MapFile{Data: []byte("")},
	}
}

// TestExtractDebian_ProducesCorrectPathStructure 锁住"两个家族的抽取各自
// 产出正确的路径结构"（计划任务 5 测试要求第一条）。
func TestExtractDebian_ProducesCorrectPathStructure(t *testing.T) {
	root := debianFakeRoot()
	destDir := t.TempDir()

	paths, err := ExtractDebian(root, destDir)
	if err != nil {
		t.Fatalf("ExtractDebian 返回错误: %v", err)
	}

	if paths.Kernel != "linux" {
		t.Errorf("Debian 内核固定输出文件名应为 linux（SPEC §4.5），got %q", paths.Kernel)
	}
	if len(paths.Initrd) != 1 || paths.Initrd[0] != "initrd.gz" {
		t.Errorf("Debian initrd 固定输出文件名应为 initrd.gz（SPEC §4.5），got %v", paths.Initrd)
	}
	if paths.Repo != "repo/" {
		t.Errorf("Repo 应为 repo/，got %q", paths.Repo)
	}

	for _, rel := range []string{"linux", "initrd.gz"} {
		if _, err := os.Stat(filepath.Join(destDir, rel)); err != nil {
			t.Errorf("落盘文件 %q 应存在: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(destDir, "repo", "dists", "stable", "Release")); err != nil {
		t.Errorf("仓库树 dists/ 应被复制到 repo/ 下: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "repo", "pool", "main", "b", "bash", "bash_5.2-5_amd64.deb")); err != nil {
		t.Errorf("仓库树 pool/ 应被复制到 repo/ 下: %v", err)
	}
}

// TestExtractDebian_MissingKernelReturnsSpecificError 锁住"缺关键文件时
// 导入置失败且原因具体"（计划任务 5 测试要求第四条）。
func TestExtractDebian_MissingKernelReturnsSpecificError(t *testing.T) {
	root := fstest.MapFS{
		"dists/stable/Release": &fstest.MapFile{Data: []byte("")},
	}
	destDir := t.TempDir()

	_, err := ExtractDebian(root, destDir)
	if err == nil {
		t.Fatal("缺内核文件时 ExtractDebian 应返回错误")
	}
}

// TestExtractDebian_MissingInitrdReturnsSpecificError 同上，缺 initrd。
func TestExtractDebian_MissingInitrdReturnsSpecificError(t *testing.T) {
	root := fstest.MapFS{
		"install.amd/vmlinuz":  &fstest.MapFile{Data: []byte("fake-kernel")},
		"dists/stable/Release": &fstest.MapFile{Data: []byte("")},
	}
	destDir := t.TempDir()

	_, err := ExtractDebian(root, destDir)
	if err == nil {
		t.Fatal("缺 initrd 文件时 ExtractDebian 应返回错误")
	}
}
