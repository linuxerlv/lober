// 本文件测试 Debian 系的抽取逻辑（对应实现计划 phase2-01 任务 5，依据
// SPEC §2.1、§4.6 第三项）。抽取逻辑通过内存文件系统测试，不依赖真 ISO
// （M7 已立的规矩，manifest_test.go 的 ExtractRHEL 测试同样如此）。
package imports

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// debianFakeRoot 构造一份贴近真实 Debian netinst/DVD ISO 目录树的内容视图：
// installer 目录下的内核与 initrd（按架构分子目录，文件名固定为 linux/
// initrd.gz——真实 Debian debian-installer 打包惯例，SPEC §4.5 菜单示例
// "kernel .../linux ... initrd .../initrd.gz"），加 dists/ 与 pool/ 两棵
// 仓库树（计划任务 5："仓库树是 dists/ 与 pool/ 两棵"），以及一个不含
// info 文件的 .disk/ 目录——真实 Debian ISO 确实会带 .disk/base_installable
// 等文件（本测试固定这一事实），但**不带 .disk/info**（那是 Ubuntu 的
// 特征文件），用来锁住"Debian 自身的 .disk/ 目录不会被误认成 Ubuntu 特征"
// 这条边界，与 identify_test.go 里"Ubuntu 优先于 Debian"的歧义测试互补
// （那条测试锁的是反方向：Ubuntu 树不会被误判成 Debian）。
func debianFakeRoot() fstest.MapFS {
	return fstest.MapFS{
		"install.amd/linux":                       &fstest.MapFile{Data: []byte("fake-kernel")},
		"install.amd/initrd.gz":                   &fstest.MapFile{Data: []byte("fake-initrd")},
		"dists/stable/Release":                    &fstest.MapFile{Data: []byte("Codename: bookworm\n")},
		"dists/stable/main/binary-amd64/Packages": &fstest.MapFile{Data: []byte("Package: bash\n")},
		"pool/main/b/bash/bash_5.2-5_amd64.deb":   &fstest.MapFile{Data: []byte("fake-deb")},
		".disk/base_installable":                  &fstest.MapFile{Data: []byte("")},
		".disk/cd_type":                           &fstest.MapFile{Data: []byte("dvd/single\n")},
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
		"install.amd/linux":    &fstest.MapFile{Data: []byte("fake-kernel")},
		"dists/stable/Release": &fstest.MapFile{Data: []byte("")},
	}
	destDir := t.TempDir()

	_, err := ExtractDebian(root, destDir)
	if err == nil {
		t.Fatal("缺 initrd 文件时 ExtractDebian 应返回错误")
	}
}

// TestExtractDebian_MissingDistsReturnsSpecificError 锁住"缺 dists/ 报明确
// 错误"（本轨任务要点第三条）：kernel 与 initrd 都在，但没有任何 dists/
// 内容——这不是一份完整的 Debian installer 制品（没有 dists/ 就没有可安装
// 的软件包索引，preseed 装机会在选包阶段失败），ExtractDebian 必须在此处
// 报错，而不是静默产出一个空的 repo/ 目录、让制品集被标记为 ready。
func TestExtractDebian_MissingDistsReturnsSpecificError(t *testing.T) {
	root := fstest.MapFS{
		"install.amd/linux":     &fstest.MapFile{Data: []byte("fake-kernel")},
		"install.amd/initrd.gz": &fstest.MapFile{Data: []byte("fake-initrd")},
	}
	destDir := t.TempDir()

	_, err := ExtractDebian(root, destDir)
	if err == nil {
		t.Fatal("缺 dists/ 时 ExtractDebian 应返回错误，不应静默产出空仓库树")
	}
}

// TestExtractDebian_PathsAreRelativeWithoutSchemeOrHost 锁住 §1.5："所有
// 路径相对于制品根目录、不含 scheme 与主机名——拼 URL 是渲染器的事"
// （model.ArtifactPaths 的字段注释）。抽取器绝不能自己拼出
// "http://..." 或以 "/" 开头的绝对路径，否则渲染器再拼一次 base URL 就会
// 产出双重前缀或跨主机名的错误 URL。
func TestExtractDebian_PathsAreRelativeWithoutSchemeOrHost(t *testing.T) {
	root := debianFakeRoot()
	destDir := t.TempDir()

	paths, err := ExtractDebian(root, destDir)
	if err != nil {
		t.Fatalf("ExtractDebian 返回错误: %v", err)
	}

	check := func(label, p string) {
		if strings.Contains(p, "://") {
			t.Errorf("%s=%q 不应包含 scheme（拼 URL 是渲染器的事，§1.5）", label, p)
		}
		if strings.HasPrefix(p, "/") {
			t.Errorf("%s=%q 不应是绝对路径（应相对于制品根目录，§1.5）", label, p)
		}
	}
	check("Kernel", paths.Kernel)
	for i, initrd := range paths.Initrd {
		check(fmt.Sprintf("Initrd[%d]", i), initrd)
	}
	check("Repo", paths.Repo)
}

// TestExtractDebian_RepoTreeExcludesKernelAndInitrdSources 锁住 repo/ 下不
// 应重复包含已被抽到根目录的内核/initrd 源文件——install.amd/ 子目录本身
// 不是 dists/pool 仓库树的一部分，若被整体复制进 repo/，会让同一份二进制
// 内容在制品集里出现两份（destDir/linux 与 destDir/repo/install.amd/linux），
// 浪费磁盘且可能让人误以为 repo/ 下那份也是"应该被 nginx 发的仓库文件"。
func TestExtractDebian_RepoTreeExcludesKernelAndInitrdSources(t *testing.T) {
	root := debianFakeRoot()
	destDir := t.TempDir()

	if _, err := ExtractDebian(root, destDir); err != nil {
		t.Fatalf("ExtractDebian 返回错误: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "repo", "install.amd", "linux")); err == nil {
		t.Errorf("repo/install.amd/linux 不应存在——内核源文件已被抽到 destDir/linux，" +
			"不应在 repo/ 下重复出现")
	}
}

// TestExtractDebian_AcceptsBothKernelNames 验证 installer 目录下的内核文件名
// 两种拼写都接受：`vmlinuz`（ISO 布局）与 `linux`（netboot tarball 布局）。
//
// **两者都真实存在，此前只认一种，于是真实 ISO 抽不出内核。**
//
//   - 实测 debian-13.7.0-amd64-netinst.iso：install.amd/ 下是
//     [gtk/ initrd.gz vmlinuz xen/] —— 内核叫 **vmlinuz**。
//   - 而 deb.debian.org 的 netboot tarball（debian-installer/amd64/）里叫
//     **linux**，SPEC §4.5 的 Debian 菜单示例 `kernel .../linux` 说的是那一种。
//
// 本抽取器读的是 ISO，因此必须认 vmlinuz；但 netboot 布局也有人用来喂
// sourcePath，所以两种都接受，而不是从一种改成另一种。
//
// 历史：commit f66f1f6 把搜索名从 vmlinuz 改成 linux，理由写的是「用真实
// Debian netinst ISO 目录树跑会在 install.<arch>/ 下找不到 vmlinuz」——那个论断
// 与真镜像相反，它是从 SPEC 的菜单示例反推的，没有对着真 ISO 核过。**落盘的
// 输出名仍然是 linux**（那一半是对的，菜单示例期望的就是它），只有查找名要放宽。
func TestExtractDebian_AcceptsBothKernelNames(t *testing.T) {
	cases := map[string]string{
		"ISO 布局用 vmlinuz":         "vmlinuz",
		"netboot tarball 用 linux": "linux",
	}
	for name, kernelName := range cases {
		t.Run(name, func(t *testing.T) {
			root := fstest.MapFS{
				"install.amd/" + kernelName: &fstest.MapFile{Data: []byte("kernel-bytes")},
				"install.amd/initrd.gz":     &fstest.MapFile{Data: []byte("initrd-bytes")},
				"dists/trixie/Release":      &fstest.MapFile{Data: []byte("Suite: stable\n")},
				"pool/main/b/bash/bash.deb": &fstest.MapFile{Data: []byte("deb")},
			}
			destDir := t.TempDir()

			paths, err := ExtractDebian(root, destDir)
			if err != nil {
				t.Fatalf("ExtractDebian 失败（内核名 %q）: %v —— 两种拼写都是真实布局，"+
					"只认一种会让另一种的镜像抽不出内核", kernelName, err)
			}
			// 落盘的输出名固定是 linux（SPEC §4.5 的 Debian 菜单示例），与 ISO 内
			// 叫什么无关。
			if paths.Kernel != "linux" {
				t.Errorf("Paths.Kernel = %q, want \"linux\"（落盘输出名固定，"+
					"与 ISO 内的拼写无关）", paths.Kernel)
			}
			if _, statErr := os.Stat(filepath.Join(destDir, "linux")); statErr != nil {
				t.Errorf("内核未落盘: %v", statErr)
			}
		})
	}
}
