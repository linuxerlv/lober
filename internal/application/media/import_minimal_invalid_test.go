package media_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

func TestImportMinimalKickstartRepositoryRejectsInvalid(t *testing.T) {
	for _, series := range []catalog.Series{"almalinux", "rocky"} {
		fixture := "almalinux-10-minimal.treeinfo"
		if series == "rocky" {
			fixture = "rocky-10.2-minimal.treeinfo"
		}
		original, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name   string
			change func(map[string][]byte)
		}{
			{"缺仓库", func(f map[string][]byte) { delete(f, "Minimal/repodata/repomd.xml") }},
			{"空仓库", func(f map[string][]byte) { f["Minimal/repodata/repomd.xml"] = nil }},
			{"冲突重复", func(f map[string][]byte) { f[".treeinfo"] = append(f[".treeinfo"], []byte("repository = BaseOS\n")...) }},
			{"相同重复", func(f map[string][]byte) {
				f[".treeinfo"] = append(f[".treeinfo"], []byte("repository = Minimal\n")...)
			}},
			{"混合分组", func(f map[string][]byte) {
				f[".treeinfo"] = append(f[".treeinfo"], []byte("[variant-BaseOS]\nid = BaseOS\nrepository = BaseOS\n")...)
			}},
			{"危险路径", func(f map[string][]byte) {
				f[".treeinfo"] = []byte(strings.ReplaceAll(string(f[".treeinfo"]), "repository = Minimal", "repository = ../Minimal"))
			}},
			{"缺分组清单", func(f map[string][]byte) {
				f[".treeinfo"] = []byte(strings.ReplaceAll(string(f[".treeinfo"]), "variants = Minimal\n", ""))
			}},
			{"完整盘缺AppStream", func(f map[string][]byte) {
				name := "AlmaLinux"
				if series == "rocky" {
					name = "Rocky Linux"
				}
				f[".treeinfo"] = []byte("[general]\nname=" + name + "\nversion=10\narch=x86_64\npackagedir=Packages\n; variants=Minimal\n# [variant-Minimal]\n")
				f["BaseOS/repodata/repomd.xml"] = []byte("base")
			}},
		} {
			t.Run(string(series)+"/"+tc.name, func(t *testing.T) {
				files := map[string][]byte{
					".treeinfo":                   append([]byte(nil), original...),
					"images/pxeboot/vmlinuz":      []byte("kernel"),
					"images/pxeboot/initrd.img":   []byte("initrd"),
					"images/install.img":          []byte("stage2"),
					"Minimal/repodata/repomd.xml": []byte("repository"),
				}
				tc.change(files)
				s, db := importSetup(t)
				i, err := db.CreateISO(t.Context(), model.ISO{Name: "minimal-invalid-" + string(series), Family: "linux", Series: series}, importAudit())
				if err != nil {
					t.Fatal(err)
				}
				importUpload(t, s, i, "", isotest.Image(files))
				err = s.Import(t.Context(), i.ID)
				var p model.Problem
				if !errors.As(err, &p) || p.Code != "iso_repo_incomplete" || p.Subject.ID != i.ID || p.Subject.Path != "artifacts.repo" {
					t.Fatalf("错误分类或镜像编号改变：%v", err)
				}
				got, err := db.GetISO(t.Context(), i.ID)
				if err != nil || got.State != model.ISOFailed || len(got.Artifacts) != 0 {
					t.Fatalf("无效介质仍发布或未记失败：%+v %v", got, err)
				}
				if err := fs.WalkDir(os.DirFS(s.dirs.Artifacts), ".", func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.IsDir() {
						t.Errorf("无效介质留下制品文件：%s", path)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestImportMinimalKickstartRepositoryKeepsExistingFullPublication(t *testing.T) {
	for _, series := range []catalog.Series{"almalinux", "rocky"} {
		t.Run(string(series), func(t *testing.T) {
			fixture, name, version := "almalinux-10-minimal.treeinfo", "AlmaLinux", "10"
			if series == "rocky" {
				fixture, name, version = "rocky-10.2-minimal.treeinfo", "Rocky Linux", "10.2"
			}
			treeinfo, err := os.ReadFile(filepath.Join("testdata", fixture))
			if err != nil {
				t.Fatal(err)
			}
			s, db := importSetup(t)
			first, err := db.CreateISO(t.Context(), model.ISO{Name: "existing-full", Family: "linux", Series: series}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			full := map[string][]byte{
				".treeinfo":                     []byte("[general]\nname=" + name + "\nversion=" + version + "\narch=x86_64\npackagedir=Packages\n"),
				"images/pxeboot/vmlinuz":        []byte("full-kernel"),
				"images/pxeboot/initrd.img":     []byte("full-initrd"),
				"images/install.img":            []byte("full-stage2"),
				"BaseOS/repodata/repomd.xml":    []byte("base"),
				"AppStream/repodata/repomd.xml": []byte("app"),
			}
			importUpload(t, s, first, "", isotest.Image(full))
			if err = s.Import(t.Context(), first.ID); err != nil {
				t.Fatal(err)
			}
			second, err := db.CreateISO(t.Context(), model.ISO{Name: "incoming-minimal", Family: "linux", Series: series}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			minimal := map[string][]byte{
				".treeinfo":                   treeinfo,
				"images/pxeboot/vmlinuz":      []byte("minimal-kernel"),
				"images/pxeboot/initrd.img":   []byte("minimal-initrd"),
				"images/install.img":          []byte("minimal-stage2"),
				"Minimal/repodata/repomd.xml": []byte("minimal"),
			}
			importUpload(t, s, second, "", isotest.Image(minimal))
			if err = s.Import(t.Context(), second.ID); !errors.Is(err, repository.ErrConflict) {
				t.Fatalf("同版本新介质未被发布保护拒绝：%v", err)
			}
			failed, err := db.GetISO(t.Context(), second.ID)
			if err != nil || failed.State != model.ISOFailed || len(failed.Artifacts) != 0 {
				t.Fatalf("发布冲突状态错误：%+v %v", failed, err)
			}
			old, err := db.GetISO(t.Context(), first.ID)
			if err != nil || old.State != model.ISOReady {
				t.Fatalf("旧完整盘状态改变：%+v %v", old, err)
			}
			for _, rel := range []string{".treeinfo", "BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"} {
				body, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(old.Artifacts["repo"]), filepath.FromSlash(rel)))
				if err != nil || string(body) != string(full[rel]) {
					t.Fatalf("旧仓库 %s 被修改：%v", rel, err)
				}
			}
			body, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(old.Artifacts["kernel"])))
			if err != nil || string(body) != "full-kernel" {
				t.Fatalf("旧内核被修改：%v", err)
			}
		})
	}
}

func TestImportFullKickstartRepositoryNamedMinimal(t *testing.T) {
	for _, series := range []catalog.Series{"almalinux", "rocky"} {
		for _, tc := range []struct {
			root   string
			groups bool
		}{
			{root: "media/Minimal"},
			{root: "Minimal"},
			{root: "media/Minimal", groups: true},
		} {
			root, label := tc.root, string(series)+"/"+tc.root
			if tc.groups {
				label += "/full-groups"
			}
			t.Run(label, func(t *testing.T) {
				name := "AlmaLinux"
				if series == "rocky" {
					name = "Rocky Linux"
				}
				// 保留原 general-only 反例，再覆盖明确声明完整盘分组的情况。
				treeinfo := "[general]\nname=" + name + "\nversion=10.2\narch=x86_64\npackagedir=" + root + "/Packages\n"
				if tc.groups {
					treeinfo += "variant=BaseOS\nvariants=BaseOS,AppStream\nrepository=" + root + "/BaseOS\n" +
						"[tree]\nvariants=BaseOS,AppStream\n" +
						"[variant-BaseOS]\nid=BaseOS\nrepository=" + root + "/BaseOS\npackages=" + root + "/BaseOS/Packages\n" +
						"[variant-AppStream]\nid=AppStream\nrepository=" + root + "/AppStream\npackages=" + root + "/AppStream/Packages\n"
				}
				files := map[string][]byte{
					".treeinfo":                             []byte(treeinfo),
					"images/pxeboot/vmlinuz":                []byte("full-kernel"),
					"images/pxeboot/initrd.img":             []byte("full-initrd"),
					"images/install.img":                    []byte("full-stage2"),
					root + "/BaseOS/repodata/repomd.xml":    []byte("full-base-repository"),
					root + "/AppStream/repodata/repomd.xml": []byte("full-app-repository"),
					root + "/Packages/sample.rpm":           []byte("full-package"),
				}
				s, db := importSetup(t)
				i, err := db.CreateISO(t.Context(), model.ISO{Name: "full-with-Minimal-directory", Family: "linux", Series: series}, importAudit())
				if err != nil {
					t.Fatal(err)
				}
				importUpload(t, s, i, "", isotest.Image(files))
				if err = s.Import(t.Context(), i.ID); err != nil {
					t.Fatalf("完整盘因普通目录名含 Minimal 被拒绝：%v", err)
				}
				got, err := db.GetISO(t.Context(), i.ID)
				if err != nil || got.State != model.ISOReady || len(got.Validate()) != 0 {
					t.Fatalf("完整盘未就绪：%+v %v", got, err)
				}
				if got.Series != series || got.Version != "10.2" || got.Arch != "x86_64" || len(got.Artifacts) != 4 {
					t.Fatalf("完整盘识别或制品角色改变：%+v", got)
				}
				repo := got.Artifacts["repo"]
				if !fs.ValidPath(strings.TrimSuffix(repo, "/")) || strings.ContainsAny(repo, "\\:") || !strings.HasSuffix(repo, "/install/"+root+"/") {
					t.Fatalf("完整盘仓库未指向原相对根 %s：%q", root, repo)
				}
				for _, rel := range []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml", "Packages/sample.rpm"} {
					body, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(repo), filepath.FromSlash(rel)))
					if err != nil || string(body) != string(files[root+"/"+rel]) {
						t.Fatalf("完整盘原仓库 %s 不可读或内容改变：%v", rel, err)
					}
				}
				for role, source := range map[string]string{"kernel": "images/pxeboot/vmlinuz", "initrd": "images/pxeboot/initrd.img"} {
					body, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(got.Artifacts[role])))
					if err != nil || string(body) != string(files[source]) {
						t.Fatalf("完整盘 %s 内容改变：%v", role, err)
					}
				}
				body, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(got.Artifacts["stage2"]), "images", "install.img"))
				if err != nil || string(body) != string(files["images/install.img"]) {
					t.Fatalf("完整盘 stage2 内容改变：%v", err)
				}
			})
		}
	}
}
