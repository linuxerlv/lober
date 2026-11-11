package media_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

// 两份元数据来自公开安装盘的局部读取，尚未据此验证整盘签名。
// Alma 来源：https://repo.almalinux.org/almalinux/10.2/isos/x86_64/AlmaLinux-10.2-x86_64-minimal.iso
// Rocky 来源：https://mirrors.ustc.edu.cn/rocky/10.2/isos/x86_64/Rocky-10.2-x86_64-minimal.iso
// 引导文件和仓库内容只用小样本；本测试检查真实导入和发布，不验证整盘签名或软件包。
func TestImportMinimalKickstartRepository(t *testing.T) {
	for _, tc := range []struct {
		series  catalog.Series
		version string
		fixture string
		sha256  string
	}{
		{"almalinux", "10", "almalinux-10-minimal.treeinfo", "201ff50943c388434cc0daf4dbea50b97e1b6ce2840f333824222e6d2583d3cb"},
		{"rocky", "10.2", "rocky-10.2-minimal.treeinfo", "f371295d7bebc28de26f08fb5b04c6fab854ec0bc3875aa9d8015183883dbd0f"},
	} {
		t.Run(string(tc.series), func(t *testing.T) {
			treeinfo, err := os.ReadFile(filepath.Join("testdata", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(treeinfo)
			if got := hex.EncodeToString(sum[:]); got != tc.sha256 {
				t.Fatalf("原始元数据摘要改变：得到 %s，期望 %s", got, tc.sha256)
			}
			files := map[string][]byte{
				".treeinfo":                   treeinfo,
				"images/pxeboot/vmlinuz":      []byte("minimal-kernel"),
				"images/pxeboot/initrd.img":   []byte("minimal-initrd"),
				"images/install.img":          []byte("minimal-stage2"),
				"Minimal/repodata/repomd.xml": []byte("minimal-repository-metadata"),
				"Minimal/Packages/sample.rpm": []byte("minimal-package"),
			}
			s, db := importSetup(t)
			i, err := db.CreateISO(t.Context(), model.ISO{Name: "repository-" + string(tc.series), Family: "linux", Series: tc.series}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			importUpload(t, s, i, "", isotest.Image(files))
			if err := s.Import(t.Context(), i.ID); err != nil {
				t.Fatalf("Minimal 安装盘导入失败：%v", err)
			}
			got, err := db.GetISO(t.Context(), i.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != model.ISOReady || len(got.Validate()) != 0 {
				t.Fatalf("导入后未就绪：%+v", got)
			}
			if got.Version != tc.version || got.Series != tc.series || got.Arch != "x86_64" {
				t.Fatalf("识别结果改变：series=%s version=%s arch=%s", got.Series, got.Version, got.Arch)
			}
			for _, role := range []string{"kernel", "initrd", "stage2", "repo"} {
				rel := got.Artifacts[role]
				if !fs.ValidPath(strings.TrimSuffix(rel, "/")) || strings.ContainsAny(rel, "\\:") {
					t.Fatalf("%s 发布路径无效：%q", role, rel)
				}
				info, err := os.Stat(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(rel)))
				if err != nil {
					t.Fatalf("%s 发布内容不可读：%v", role, err)
				}
				wantDir := role == "stage2" || role == "repo"
				if info.IsDir() != wantDir || (!wantDir && (!info.Mode().IsRegular() || info.Size() == 0)) {
					t.Fatalf("%s 发布内容类型或大小错误：%v", role, info)
				}
			}
			for role, name := range map[string]string{"kernel": "images/pxeboot/vmlinuz", "initrd": "images/pxeboot/initrd.img"} {
				body, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(got.Artifacts[role])))
				if err != nil || !bytes.Equal(body, files[name]) {
					t.Fatalf("%s 发布内容改变：%v", role, err)
				}
			}
			for _, name := range []string{".treeinfo", "Minimal/repodata/repomd.xml", "Minimal/Packages/sample.rpm"} {
				body, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(got.Artifacts["repo"]), filepath.FromSlash(name)))
				if err != nil || !bytes.Equal(body, files[name]) {
					t.Fatalf("安装树中的 %s 不可读或内容改变：%v", name, err)
				}
			}
			stage2, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(got.Artifacts["stage2"]), "images", "install.img"))
			if err != nil || !bytes.Equal(stage2, files["images/install.img"]) {
				t.Fatalf("安装器镜像不可读或内容改变：%v", err)
			}
		})
	}
}
