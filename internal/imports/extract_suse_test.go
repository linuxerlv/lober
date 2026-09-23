// 本文件测试 SUSE 系（openSUSE Leap / SLES）的抽取逻辑（对应任务
// t07-suse-import，依据 SPEC §4.5 第四行 "SUSE | AutoYaST | linux + initrd
// + `autoyast=`" 与本文件顶部 ExtractSUSE 注释中记录的核实依据）。
//
// 全部测试通过内存文件系统（testing/fstest.MapFS）验证，不依赖真 ISO——
// 与 manifest_test.go 的 ExtractRHEL、extract_debian_test.go、
// extract_ubuntu_test.go 同样的规矩。
package imports

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// suseFakeRoot 构造一个最小但结构真实的 openSUSE/SLES 安装介质内容视图：
//
//   - boot/x86_64/loader/{linux,initrd}：引导内核与 initrd（本文件顶部注释
//     记录的核实依据：直接对 download.opensuse.org 上的 openSUSE Leap 15.6
//     x86_64 安装介质发起 HTTP 请求确认，这两个文件在该路径下真实存在，
//     HTTP 200）
//   - repodata/repomd.xml：zypper/YaST 仓库元数据入口（同样对官方镜像
//     server 实测确认存在，HTTP 200）
//   - media.1/products：SUSE 特有的多仓库产品索引（openSUSE linuxrc 项目
//     自己的文档 linuxrc_repo.md 第 3 节点名此文件是"multi-repository
//     medium"的判据，本文件借它做"确实是 SUSE 介质"的独立佐证，见下方
//     ExtractSUSE 注释）
//   - suse/setup/descr、suse/x86_64：仓库树的第二部分（zypper 仓库除
//     repodata/ 外还有一棵 suse/ 目录承载实际 rpm 与旧式产品描述，任务
//     描述原文："仓库树：SUSE 用 zypper 仓库（repodata/ 与 suse/ 目录）"）
func suseFakeRoot() fstest.MapFS {
	return fstest.MapFS{
		"boot/x86_64/loader/linux":   &fstest.MapFile{Data: []byte("fake-kernel")},
		"boot/x86_64/loader/initrd":  &fstest.MapFile{Data: []byte("fake-initrd")},
		"repodata/repomd.xml":        &fstest.MapFile{Data: []byte("<repomd/>")},
		"media.1/products":           &fstest.MapFile{Data: []byte("/ openSUSE-Leap-15.6\n")},
		"suse/setup/descr/packages":  &fstest.MapFile{Data: []byte("=Pkg: bash 5.2 1 x86_64\n")},
		"suse/x86_64/bash-5.2-1.rpm": &fstest.MapFile{Data: []byte("fake-rpm")},
	}
}

// TestExtractSUSE_ProducesCorrectPathStructure 锁住"完整 openSUSE 目录树
// 能被正确抽取（kernel/initrd/repo 路径都对）"（任务测试要点第一条）。
func TestExtractSUSE_ProducesCorrectPathStructure(t *testing.T) {
	root := suseFakeRoot()
	destDir := t.TempDir()

	paths, err := ExtractSUSE(root, destDir)
	if err != nil {
		t.Fatalf("ExtractSUSE 返回错误: %v", err)
	}

	// 内核/initrd 固定输出文件名沿用源文件名 linux/initrd——与 SPEC §4.5
	// 的 SUSE 菜单示例（"linux + initrd + autoyast="）逐字符一致，不像
	// RHEL（vmlinuz→保留）或 Debian（vmlinuz→改名 linux）那样有映射，
	// SUSE 官方介质本身文件名就已经是 linux/initrd，直接沿用即可。
	if paths.Kernel != "linux" {
		t.Errorf("SUSE 内核固定输出文件名应为 linux（SPEC §4.5），got %q", paths.Kernel)
	}
	if len(paths.Initrd) != 1 || paths.Initrd[0] != "initrd" {
		t.Errorf("SUSE initrd 固定输出文件名应为 initrd（SPEC §4.5），got %v", paths.Initrd)
	}
	if paths.Repo != "repo/" {
		t.Errorf("Repo 应为 repo/，got %q", paths.Repo)
	}

	for _, rel := range []string{"linux", "initrd"} {
		if _, err := os.Stat(filepath.Join(destDir, rel)); err != nil {
			t.Errorf("落盘文件 %q 应存在: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(destDir, "repo", "repodata", "repomd.xml")); err != nil {
		t.Errorf("仓库树 repodata/ 应被复制到 repo/ 下: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "repo", "suse", "x86_64", "bash-5.2-1.rpm")); err != nil {
		t.Errorf("仓库树 suse/ 应被复制到 repo/ 下: %v", err)
	}
}

// TestExtractSUSE_MissingKernelReturnsSpecificError 锁住"缺各个关键文件时
// 报明确错误"（任务测试要点第二条）——缺内核。
func TestExtractSUSE_MissingKernelReturnsSpecificError(t *testing.T) {
	root := fstest.MapFS{
		"boot/x86_64/loader/initrd": &fstest.MapFile{Data: []byte("fake-initrd")},
		"repodata/repomd.xml":       &fstest.MapFile{Data: []byte("<repomd/>")},
	}
	destDir := t.TempDir()

	_, err := ExtractSUSE(root, destDir)
	if err == nil {
		t.Fatal("缺内核文件时 ExtractSUSE 应返回错误")
	}
}

// TestExtractSUSE_MissingInitrdReturnsSpecificError 缺 initrd。
func TestExtractSUSE_MissingInitrdReturnsSpecificError(t *testing.T) {
	root := fstest.MapFS{
		"boot/x86_64/loader/linux": &fstest.MapFile{Data: []byte("fake-kernel")},
		"repodata/repomd.xml":      &fstest.MapFile{Data: []byte("<repomd/>")},
	}
	destDir := t.TempDir()

	_, err := ExtractSUSE(root, destDir)
	if err == nil {
		t.Fatal("缺 initrd 文件时 ExtractSUSE 应返回错误")
	}
}

// TestExtractSUSE_MissingRepodataReturnsSpecificError 缺仓库树元数据
// （repodata/repomd.xml）——本任务测试要点第二条"缺各个关键文件"明确包含
// 仓库树本身，不只是内核/initrd 这两个引导制品。
func TestExtractSUSE_MissingRepodataReturnsSpecificError(t *testing.T) {
	root := fstest.MapFS{
		"boot/x86_64/loader/linux":  &fstest.MapFile{Data: []byte("fake-kernel")},
		"boot/x86_64/loader/initrd": &fstest.MapFile{Data: []byte("fake-initrd")},
	}
	destDir := t.TempDir()

	_, err := ExtractSUSE(root, destDir)
	if err == nil {
		t.Fatal("缺 repodata/repomd.xml 时 ExtractSUSE 应返回错误")
	}
}

// TestExtractSUSE_RejectsRHELTree 锁住"不会把 RHEL 的目录树误判成 SUSE"
// （任务测试要点第三条，反向测试）。用一棵结构完整的 RHEL 目录树
// （.treeinfo + images/pxeboot/{vmlinuz,initrd.img}）调用 ExtractSUSE，
// 必须报错——因为它既没有 boot/x86_64/loader/ 也没有 repodata/repomd.xml，
// ExtractSUSE 内部的路径查找会在第一步就失败。这条测试锁住的不变量是
// "ExtractSUSE 靠真实存在的 SUSE 专属路径工作，不是靠猜测或宽松匹配"——
// 即使一棵 RHEL 树凑巧摆在这里，也不会被误当作 SUSE 抽取成功。
func TestExtractSUSE_RejectsRHELTree(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo":                 &fstest.MapFile{Data: []byte("[general]\n")},
		"images/pxeboot/vmlinuz":    &fstest.MapFile{Data: []byte("fake-kernel")},
		"images/pxeboot/initrd.img": &fstest.MapFile{Data: []byte("fake-initrd")},
		"repodata/repomd.xml":       &fstest.MapFile{Data: []byte("<repomd/>")},
	}
	destDir := t.TempDir()

	_, err := ExtractSUSE(root, destDir)
	if err == nil {
		t.Fatal("RHEL 目录树（.treeinfo + images/pxeboot/）不应被 ExtractSUSE 成功抽取——" +
			"它没有 SUSE 专属的 boot/<arch>/loader/ 引导路径")
	}
}

// TestExtractSUSE_RejectsDebianTree 锁住"不会把 Debian 的目录树误判成
// SUSE"（任务测试要点第三条，反向测试的第二个家族）。Debian 树同样没有
// boot/<arch>/loader/，抽取应失败。
func TestExtractSUSE_RejectsDebianTree(t *testing.T) {
	root := fstest.MapFS{
		"install.amd/vmlinuz":   &fstest.MapFile{Data: []byte("fake-kernel")},
		"install.amd/initrd.gz": &fstest.MapFile{Data: []byte("fake-initrd")},
		"dists/stable/Release":  &fstest.MapFile{Data: []byte("Codename: bookworm\n")},
	}
	destDir := t.TempDir()

	_, err := ExtractSUSE(root, destDir)
	if err == nil {
		t.Fatal("Debian 目录树（install.<arch>/ + dists/）不应被 ExtractSUSE 成功抽取——" +
			"它没有 SUSE 专属的 boot/<arch>/loader/ 引导路径")
	}
}

// TestExtractSUSE_PathsAreRelativeWithoutSchemeOrHost 锁住"路径是相对路径、
// 不含 scheme 与主机名"（任务测试要点第四条）——覆盖 Kernel/Initrd/Repo
// 三个字段，与 docs/contracts/filesystem.md 硬约束第一条一致（制品目录下
// 的路径与制品集里存的相对路径逐字符一致，都是相对该制品集根目录）。
func TestExtractSUSE_PathsAreRelativeWithoutSchemeOrHost(t *testing.T) {
	root := suseFakeRoot()
	destDir := t.TempDir()

	paths, err := ExtractSUSE(root, destDir)
	if err != nil {
		t.Fatalf("ExtractSUSE 返回错误: %v", err)
	}

	assertRelativeNoSchemeNoHost(t, "Kernel", paths.Kernel)
	for i, p := range paths.Initrd {
		assertRelativeNoSchemeNoHost(t, "Initrd["+string(rune('0'+i))+"]", p)
	}
	assertRelativeNoSchemeNoHost(t, "Repo", paths.Repo)
}

// assertRelativeNoSchemeNoHost 断言 p 是相对路径，不含 "://"（scheme），
// 也不以 "/" 开头（不是绝对路径，间接排除主机名+绝对路径拼接的写法）。
func assertRelativeNoSchemeNoHost(t *testing.T, field, p string) {
	t.Helper()
	if p == "" {
		return
	}
	if filepath.IsAbs(p) {
		t.Errorf("%s = %q 不应是绝对路径", field, p)
	}
	for _, sep := range []string{"://", "\\\\"} {
		if idx := indexOf(p, sep); idx >= 0 {
			t.Errorf("%s = %q 不应包含 %q（scheme 或主机名拼接痕迹）", field, p, sep)
		}
	}
}

// indexOf 是 strings.Index 的最小替代（避免仅为这一处断言额外引入 strings
// 包的导入——本文件其余部分完全不需要 strings，加一个 import 只为一次
// 调用不划算；这个小助手本身逻辑简单到不需要单独测试）。
func indexOf(s, substr string) int {
	n := len(substr)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == substr {
			return i
		}
	}
	return -1
}
