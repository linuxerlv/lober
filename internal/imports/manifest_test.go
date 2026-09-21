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
