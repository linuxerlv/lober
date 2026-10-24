package media_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

// Metadata copied from official media using HTTP range reads on 2026-10-18.
// Sources, hashes, byte offsets and exact observations are recorded in
// .tmp/linux-qualification/media-research/official-media.md in the task workspace.
const officialLeapTreeinfo = "[header]\nversion = 1.0\n\n[release]\nname = openSUSE Leap\nversion = 16.0\n\n[general]\narch = x86_64\nfamily = openSUSE Leap\nname = openSUSE Leap 16.0\nversion = 16.0\nplatforms = x86_64,xen\n\n[images-x86_64]\nkernel = boot/x86_64/loader/linux\ninitrd = boot/x86_64/loader/initrd\n\n[images-xen]\nkernel = boot/x86_64/loader/linux\ninitrd = boot/x86_64/loader/initrd\n"

func officialLeapFiles(live bool) map[string][]byte {
	f := map[string][]byte{
		"boot/x86_64/loader/linux":  []byte("official kernel"),
		"boot/x86_64/loader/initrd": []byte("official initrd"),
	}
	if live {
		f["LiveOS/squashfs.img"] = []byte("official live root")
		f["install/.treeinfo"] = []byte(officialLeapTreeinfo)
		f["install/media.1/products"] = []byte("/ Leap 16.0-1\n")
		f["install/repodata/repomd.xml"] = []byte("official repository")
	} else {
		f[".treeinfo"] = []byte(strings.ReplaceAll(officialLeapTreeinfo, "16.0", "15.6"))
		f["media.1/products"] = []byte("/ openSUSE-Leap 15.6-1\n")
		f["repodata/repomd.xml"] = []byte("official repository")
	}
	return f
}

func TestIdentifyOfficialSUSELayouts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		live    bool
		series  catalog.Series
		version string
	}{
		{"leap15-root-treeinfo", false, "leap15", "15.6"},
		{"leap16-nested-repository", true, "leap16", "16.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := map[string]string{}
			for name, data := range officialLeapFiles(tc.live) {
				f[name] = string(data)
			}
			got, err := Identify(context.Background(), identifyTree(f))
			if err != nil || got.Series != tc.series || got.Format != tc.series.Format() || got.Version != tc.version || got.Arch != "x86_64" {
				t.Fatalf("official media: got %+v, %v", got, err)
			}
		})
	}
}

func TestIdentifyOfficialSUSERejectsConflictingMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string][]byte)
	}{
		{"version", func(f map[string][]byte) { f["install/media.1/products"] = []byte("/ Leap 15.6-1\n") }},
		{"product", func(f map[string][]byte) { f["install/media.1/products"] = []byte("/ SLES 16.0-1\n") }},
		{"architecture", func(f map[string][]byte) {
			f["install/.treeinfo"] = []byte(strings.ReplaceAll(officialLeapTreeinfo, "arch = x86_64", "arch = aarch64"))
		}},
		{"unknown product", func(f map[string][]byte) {
			f["install/.treeinfo"] = []byte(strings.ReplaceAll(officialLeapTreeinfo, "openSUSE Leap", "Unknown Linux"))
		}},
		{"multiple loaders", func(f map[string][]byte) { f["boot/aarch64/loader/linux"] = []byte("other arch") }},
		{"non-live nested tree", func(f map[string][]byte) { delete(f, "LiveOS/squashfs.img") }},
		{"missing repository metadata", func(f map[string][]byte) { delete(f, "install/repodata/repomd.xml") }},
		{"conflicting root tree", func(f map[string][]byte) {
			f[".treeinfo"] = []byte("[general]\nname=Rocky Linux\nversion=9.8\narch=x86_64\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := officialLeapFiles(true)
			tc.change(f)
			files := map[string]string{}
			for name, data := range f {
				files[name] = string(data)
			}
			if got, err := Identify(context.Background(), identifyTree(files)); err == nil {
				t.Fatalf("conflicting official media accepted: %+v", got)
			}
		})
	}
}

func TestImportOfficialSUSERepositoryRoots(t *testing.T) {
	for _, tc := range []struct {
		series catalog.Series
		live   bool
		repo   string
	}{
		{"leap15", false, "leap15/15.6/x86_64/install/"},
		{"leap16", true, "leap16/16.0/x86_64/install/install/"},
	} {
		t.Run(string(tc.series), func(t *testing.T) {
			s, db := importSetup(t)
			i, err := db.CreateISO(t.Context(), model.ISO{Name: "official layout", Family: "linux", Series: tc.series}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			importUpload(t, s, i, "", isotest.Image(officialLeapFiles(tc.live)))
			if err := s.Import(t.Context(), i.ID); err != nil {
				t.Fatal(err)
			}
			got, err := db.GetISO(t.Context(), i.ID)
			if err != nil || got.State != model.ISOReady || got.Artifacts["repo"] != tc.repo {
				t.Fatalf("got %+v, %v", got, err)
			}
			for _, tc := range []struct{ role, content string }{{"kernel", "official kernel"}, {"initrd", "official initrd"}} {
				b, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(got.Artifacts[tc.role])))
				if err != nil || string(b) != tc.content {
					t.Fatalf("%s: %q, %v", tc.role, b, err)
				}
			}
			b, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(got.Artifacts["repo"]), "repodata/repomd.xml"))
			if err != nil || string(b) != "official repository" {
				t.Fatalf("repo: %q, %v", b, err)
			}
			if tc.live {
				b, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(got.Artifacts["live-root"])))
				if err != nil || string(b) != "official live root" {
					t.Fatalf("live root: %q, %v", b, err)
				}
			}
		})
	}
}

func TestImportOfficialDebianBuildBinding(t *testing.T) {
	for _, tc := range []struct {
		name, info string
		accept     bool
	}{
		{"official update7", "Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso 20250803+deb13u7\n", true},
		{"wrong update6", "Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso 20250803+deb13u6\n", false},
		{"wrong update8", "Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso 20250803+deb13u8\n", false},
		{"wrong suffix major", "Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso 20250803+deb12u7\n", false},
		{"wrong codename", "Debian GNU/Linux 13 (bookworm) amd64 - netboot mini.iso 20250803+deb13u7\n", false},
		{"wrong arch", "Debian GNU/Linux 13 (trixie) arm64 - netboot mini.iso 20250803+deb13u7\n", false},
		{"ambiguous major", "Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso\n", false},
		{"unqualified development build", "Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso 20260907\n", false},
		{"trailing build qualifier", "Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso 20250803+deb13u7+local1\n", false},
		{"explicit exact version", "Debian GNU/Linux 13.7.0 amd64 - netboot mini.iso 20260907\n", true},
		{"explicit conflicting build", "Debian GNU/Linux 13.7.0 amd64 - netboot mini.iso 20250803+deb13u6\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db := importSetup(t)
			i, err := db.CreateISO(t.Context(), model.ISO{Name: tc.name, Family: "linux", Series: "debian"}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			main := importFixture("debian")
			main[".disk/info"] = []byte("Debian GNU/Linux 13.7.0 Trixie - Official amd64 NETINST Binary-1")
			importUpload(t, s, i, "", isotest.Image(main))
			importUpload(t, s, i, "netboot", isotest.Image(map[string][]byte{".disk/info": []byte(tc.info), "linux": []byte("network kernel"), "initrd.gz": []byte("network initrd")}))
			err = s.Import(t.Context(), i.ID)
			if tc.accept {
				if err != nil {
					t.Fatal(err)
				}
				got, err := db.GetISO(t.Context(), i.ID)
				if err != nil || got.Version != "13.7.0" || got.State != model.ISOReady {
					t.Fatalf("got %+v, %v", got, err)
				}
			} else {
				var p model.Problem
				if !errors.As(err, &p) || p.Code != "iso_extra_source_invalid" {
					t.Fatalf("invalid netboot accepted: %v", err)
				}
			}
		})
	}
}
