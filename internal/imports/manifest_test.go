package imports

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// ---------------------------------------------------------------------------
// TestExtractRHEL
// ---------------------------------------------------------------------------

func sampleTreeinfo() string {
	return `[general]
family = Rocky Linux
version = 9.4
arch = x86_64

[images-x86_64]
kernel = images/pxeboot/vmlinuz
initrd = images/pxeboot/initrd.img
`
}

func sampleRHELRoot() fstest.MapFS {
	return fstest.MapFS{
		".treeinfo":                  &fstest.MapFile{Data: []byte(sampleTreeinfo())},
		"images/pxeboot/vmlinuz":     &fstest.MapFile{Data: []byte("fake-kernel-bytes")},
		"images/pxeboot/initrd.img":  &fstest.MapFile{Data: []byte("fake-initrd-bytes")},
		"BaseOS/Packages/foo.rpm":    &fstest.MapFile{Data: []byte("fake-rpm-bytes")},
		"AppStream/Packages/bar.rpm": &fstest.MapFile{Data: []byte("fake-rpm-bytes-2")},
	}
}

func TestExtractRHEL_ParsesKernelAndInitrdPaths(t *testing.T) {
	root := sampleRHELRoot()
	destDir := t.TempDir()

	paths, err := ExtractRHEL(root, destDir)
	if err != nil {
		t.Fatalf("正常 .treeinfo 应抽取成功: %v", err)
	}
	if paths.Kernel != "vmlinuz" {
		t.Fatalf("got Kernel=%q, want vmlinuz", paths.Kernel)
	}
	if len(paths.Initrd) != 1 || paths.Initrd[0] != "initrd.img" {
		t.Fatalf("got Initrd=%v, want [initrd.img]", paths.Initrd)
	}
	if paths.Repo != "repo/" {
		t.Fatalf("got Repo=%q, want repo/", paths.Repo)
	}
}

func TestExtractRHEL_FilesLandAtContractPaths(t *testing.T) {
	root := sampleRHELRoot()
	destDir := t.TempDir()

	if _, err := ExtractRHEL(root, destDir); err != nil {
		t.Fatalf("不应报错: %v", err)
	}

	mustExist := []string{
		filepath.Join(destDir, "vmlinuz"),
		filepath.Join(destDir, "initrd.img"),
		filepath.Join(destDir, "repo", "BaseOS", "Packages", "foo.rpm"),
		filepath.Join(destDir, "repo", "AppStream", "Packages", "bar.rpm"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("抽取后应存在文件 %s，got err=%v——路径必须与文件系统契约一致，"+
				"不一致的后果是引导时 404（docs/contracts/filesystem.md）", p, err)
		}
	}
}

func TestExtractRHEL_MalformedTreeinfoMissingImagesSection(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte("[general]\nfamily = Rocky Linux\n")},
	}
	destDir := t.TempDir()

	_, err := ExtractRHEL(root, destDir)
	if err == nil {
		t.Fatal("缺镜像段的 .treeinfo 应返回错误，不能猜测默认路径")
	}
}

func TestExtractRHEL_MalformedTreeinfoEmptyPaths(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte("[images-x86_64]\nkernel = \ninitrd = \n")},
	}
	destDir := t.TempDir()

	_, err := ExtractRHEL(root, destDir)
	if err == nil {
		t.Fatal("镜像段路径为空的 .treeinfo 应返回错误")
	}
}

func TestExtractRHEL_MissingTreeinfoFile(t *testing.T) {
	root := fstest.MapFS{"README.txt": &fstest.MapFile{Data: []byte("no treeinfo here")}}
	destDir := t.TempDir()

	_, err := ExtractRHEL(root, destDir)
	if err == nil {
		t.Fatal("没有 .treeinfo 文件时应返回错误")
	}
}

// ---------------------------------------------------------------------------
// TestManifest
// ---------------------------------------------------------------------------

func TestManifest_ContainsAllFilesWithCorrectSHA256(t *testing.T) {
	destDir := t.TempDir()
	writeFile(t, filepath.Join(destDir, "vmlinuz"), []byte("kernel-content"))
	writeFile(t, filepath.Join(destDir, "initrd.img"), []byte("initrd-content"))
	writeFile(t, filepath.Join(destDir, "repo", "a.rpm"), []byte("rpm-a"))

	manifest, err := GenerateManifest(destDir)
	if err != nil {
		t.Fatalf("生成 manifest 不应报错: %v", err)
	}
	if len(manifest.Entries) != 3 {
		t.Fatalf("应含 3 个文件条目，got %d: %+v", len(manifest.Entries), manifest.Entries)
	}
	if manifest.SHA256 == "" {
		t.Fatal("整体校验值不应为空")
	}

	byPath := make(map[string]string)
	for _, e := range manifest.Entries {
		byPath[e.RelPath] = e.SHA256
	}
	wantSHA := sha256Hex(t, []byte("kernel-content"))
	if byPath["vmlinuz"] != wantSHA {
		t.Fatalf("vmlinuz 的 sha256 got %s, want %s", byPath["vmlinuz"], wantSHA)
	}
}

func TestManifest_DeterministicOrderAcrossCalls(t *testing.T) {
	destDir := t.TempDir()
	writeFile(t, filepath.Join(destDir, "z.txt"), []byte("z"))
	writeFile(t, filepath.Join(destDir, "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(destDir, "m.txt"), []byte("m"))

	m1, err1 := GenerateManifest(destDir)
	if err1 != nil {
		t.Fatalf("不应报错: %v", err1)
	}
	m2, err2 := GenerateManifest(destDir)
	if err2 != nil {
		t.Fatalf("不应报错: %v", err2)
	}

	if len(m1.Entries) != len(m2.Entries) {
		t.Fatalf("两次生成的条目数应相同")
	}
	for i := range m1.Entries {
		if m1.Entries[i].RelPath != m2.Entries[i].RelPath {
			t.Fatalf("两次生成的顺序应相同，第 %d 项 got %s vs %s", i, m1.Entries[i].RelPath, m2.Entries[i].RelPath)
		}
	}
	if m1.SHA256 != m2.SHA256 {
		t.Fatalf("两次生成的整体校验值应相同，got %s vs %s", m1.SHA256, m2.SHA256)
	}
	if m1.Entries[0].RelPath != "a.txt" {
		t.Fatalf("应按字典序排列，第一项 got %s, want a.txt", m1.Entries[0].RelPath)
	}
}

// writeFile 在测试里创建文件（含父目录）。
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

// sha256Hex 是测试专用的 sha256 计算辅助：把 data 写入一个临时文件后调用
// sha256File 复算，避免测试文件重复 import crypto/sha256 与 encoding/hex
// 只为了这一处断言。sha256File 只接受路径不接受 []byte，因此这里多一步
// 落盘再读回。
func sha256Hex(t *testing.T, data []byte) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "tmp-for-hash")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	sum, err := sha256File(tmp)
	if err != nil {
		t.Fatalf("计算 sha256 失败: %v", err)
	}
	return sum
}

// TestExtractRHEL_RepoPointsAtDeclaredRepository 验证 Paths.Repo 指向
// .treeinfo 声明的 repository 子目录，而不是 ISO 根。
//
// **这是真实 RHEL 9 系 ISO 上的一个阻断性 bug。** RHEL 9 及其衍生（Rocky、
// AlmaLinux、OpenCloudOS）把仓库放在 BaseOS/ 下，ISO 根目录**没有**
// repodata/；.treeinfo 用 `[general] repository = BaseOS` 与
// `[variant-BaseOS] repository = BaseOS` 权威地声明了这一点。
//
// Repo 硬编码成 "repo/"（即 ISO 根的副本）的后果有两层，都是硬失败：
//
//  1. 渲染器把它拼成 kickstart 的 `url --url=.../repo/` 交给 anaconda，而那个
//     目录下没有 repodata/ —— 装机在第一次取仓库元数据时就失败。
//  2. 更早一层：statemachine 的 precheck 按家族判据查 <repo>/repodata/repomd.xml
//     （RPM-MD 家族），查不到就拒签装机 token，机器连菜单都拿不到。
//
// 实测依据：对一份真实 OpenCloudOS 9.6 镜像跑导入，抽出的 repo/ 下是
// [.discinfo BaseOS EFI Minimal boot images media.repo]，repodata 在
// repo/BaseOS/repodata/。
func TestExtractRHEL_RepoPointsAtDeclaredRepository(t *testing.T) {
	treeinfo := `[general]
family = OpenCloudOS
version = 9.6
arch = x86_64
repository = BaseOS

[images-x86_64]
kernel = images/pxeboot/vmlinuz
initrd = images/pxeboot/initrd.img
`
	root := fstest.MapFS{
		".treeinfo":                          &fstest.MapFile{Data: []byte(treeinfo)},
		"images/pxeboot/vmlinuz":             &fstest.MapFile{Data: []byte("k")},
		"images/pxeboot/initrd.img":          &fstest.MapFile{Data: []byte("i")},
		"BaseOS/repodata/repomd.xml":         &fstest.MapFile{Data: []byte("<repomd/>")},
		"BaseOS/Packages/bash-5.2-1.el9.rpm": &fstest.MapFile{Data: []byte("rpm")},
	}

	paths, err := ExtractRHEL(root, t.TempDir())
	if err != nil {
		t.Fatalf("ExtractRHEL 失败: %v", err)
	}
	if paths.Repo != "repo/BaseOS/" {
		t.Errorf("Paths.Repo = %q, want \"repo/BaseOS/\" —— .treeinfo 声明了 "+
			"repository = BaseOS，仓库元数据在那个子目录下而不是 ISO 根；"+
			"指向根目录会让 anaconda 取不到 repodata，也会让 precheck 拒签 token",
			paths.Repo)
	}
}

// TestExtractRHEL_RepoFallsBackToRootWhenNoRepositoryDeclared 验证 .treeinfo
// 没有声明 repository 时退回 ISO 根。
//
// 老的树（RHEL 7 系与一些精简介质）确实把 repodata 放在根目录、且 .treeinfo
// 里没有 repository 键。不能因为要修 RHEL 9 就把那些树一起弄坏——两种布局都
// 真实存在，判据只能来自 .treeinfo 自己的声明。
func TestExtractRHEL_RepoFallsBackToRootWhenNoRepositoryDeclared(t *testing.T) {
	// sampleTreeinfo() 不含 repository 键，正好是这个情形。
	paths, err := ExtractRHEL(sampleRHELRoot(), t.TempDir())
	if err != nil {
		t.Fatalf("ExtractRHEL 失败: %v", err)
	}
	if paths.Repo != "repo/" {
		t.Errorf("Paths.Repo = %q, want \"repo/\" —— .treeinfo 未声明 repository 时"+
			"仓库就在根，退回根目录；否则会把 RHEL 7 系那类布局弄坏", paths.Repo)
	}
}
