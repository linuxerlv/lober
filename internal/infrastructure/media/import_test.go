package iso

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func importSetup(t *testing.T) (*acquisitionFixture, repository.Store) {
	t.Helper()
	db := pgtest.Open(t)
	root := t.TempDir()
	d := Dirs{Staging: filepath.Join(root, "staging"), Artifacts: filepath.Join(root, "artifacts"), SMB: filepath.Join(root, "smb"), Tools: filepath.Join(root, "tools")}
	for _, dir := range []string{d.Staging, d.Artifacts, d.Tools} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"wimboot", "winpe-busybox", "winpe-curl"} {
		if err := os.WriteFile(filepath.Join(d.Tools, tool), []byte(tool), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return newAcquisitionFixture(db, d), db
}
func importAudit() repository.Audit {
	return repository.Audit{Actor: "alice", Action: "iso.source", At: 1790000000}
}
func importUpload(t *testing.T, s *acquisitionFixture, i model.ISO, role string, b []byte) {
	t.Helper()
	sum := sha256.Sum256(b)
	if err := s.Register(t.Context(), i, "upload", "", role, int64(len(b)), hex.EncodeToString(sum[:]), importAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), i.ID, role, 0, bytes.NewReader(b), repository.Audit{}); err != nil {
		t.Fatal(err)
	}
}
func importFixture(series catalog.Series) map[string][]byte {
	f := map[string][]byte{}
	switch series.Format() {
	case "kickstart":
		f[".treeinfo"] = []byte("[general]\nname=Rocky Linux\nversion=9.6\narch=x86_64\npackagedir=media/Packages\n")
		f["images/pxeboot/vmlinuz"] = []byte("kernel")
		f["images/pxeboot/initrd.img"] = []byte("initrd")
		f["images/install.img"] = []byte("stage2")
		f["media/BaseOS/repodata/repomd.xml"] = []byte("base")
		f["media/AppStream/repodata/repomd.xml"] = []byte("app")
		f["media/Packages/sample.rpm"] = []byte("rpm")
	case "preseed":
		f[".disk/info"] = []byte("Debian GNU/Linux 13.6.0 Trixie - Official amd64 NETINST Binary-1")
		f["dists/trixie/Release"] = []byte("release")
		f["dists/trixie/main/binary-amd64/Packages.gz"] = []byte("package-index")
		f["pool/main/c/curl/curl.deb"] = []byte("main-media-package")
		f["install.amd/vmlinuz"] = []byte("wrong-cd-kernel")
		f["install.amd/initrd.gz"] = []byte("wrong-cd-initrd")
	case "autoinstall":
		f[".disk/info"] = []byte("Ubuntu-Server 24.04.3 LTS Noble - Release amd64")
		f["casper/vmlinuz"] = []byte("kernel")
		f["casper/initrd"] = []byte("initrd")
	case "autoyast", "agama":
		product, version := "SLES", "15.6"
		if strings.HasPrefix(string(series), "leap") {
			product = "openSUSE"
		}
		if series.Format() == "agama" {
			version = "16.0"
			f["LiveOS/squashfs.img"] = []byte("live-root")
		}
		f["media.1/products"] = []byte(product + "|" + version + "|product")
		f["repodata/repomd.xml"] = []byte("repo")
		f["boot/x86_64/loader/linux"] = []byte("kernel")
		f["boot/x86_64/loader/initrd"] = []byte("initrd")
	case "unattend":
		f["sources/install.wim"] = importWIM()
		f["sources/boot.wim"] = []byte("bootwim")
		f["boot/bcd"] = []byte("bcd")
		f["boot/boot.sdi"] = []byte("sdi")
	case "esxi-ks":
		f["boot.cfg"] = []byte("title=Loading ESXi installer\n")
		f["efi/boot/bootx64.efi"] = []byte("mboot")
	}
	return f
}

func TestDebianRepositoryMustHaveReleaseIndexAndPool(t *testing.T) {
	for _, broken := range []string{"dists/trixie/Release", "dists/trixie/main/binary-amd64/Packages.gz", "pool/main/c/curl/curl.deb"} {
		t.Run(broken, func(t *testing.T) {
			s, db := importSetup(t)
			i, err := db.CreateISO(t.Context(), model.ISO{Name: "incomplete-debian", Family: "linux", Series: "debian"}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			main := importFixture("debian")
			delete(main, broken)
			importUpload(t, s, i, "", isotest.Image(main))
			importUpload(t, s, i, "netboot", isotest.Image(map[string][]byte{".disk/info": []byte("Debian GNU/Linux 13.6.0 amd64 NETBOOT"), "linux": []byte("kernel"), "initrd.gz": []byte("initrd")}))
			err = s.Import(t.Context(), i.ID)
			var p model.Problem
			if !errors.As(err, &p) || p.Code != "iso_repo_incomplete" {
				t.Fatalf("incomplete Debian repository accepted: %v", err)
			}
			got, err := db.GetISO(t.Context(), i.ID)
			if err != nil || got.State != model.ISOFailed {
				t.Fatal(got.State, err)
			}
		})
	}
}

func TestDebianRepositoryDiscoversSuiteAndMatchesArchitecture(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			s, db := importSetup(t)
			i, err := db.CreateISO(t.Context(), model.ISO{Name: "debian-suite-layout", Family: "linux", Series: "debian"}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			main := importFixture("debian")
			main[".disk/info"] = []byte("Debian GNU/Linux 13.6.0 Official " + arch + " DVD Binary-1")
			for name, data := range main {
				if strings.HasPrefix(name, "dists/trixie/") {
					delete(main, name)
					main[strings.Replace(strings.Replace(name, "dists/trixie/", "dists/new-suite/", 1), "binary-amd64/", "binary-"+arch+"/", 1)] = data
				}
			}
			importUpload(t, s, i, "", isotest.Image(main))
			importUpload(t, s, i, "netboot", isotest.Image(map[string][]byte{".disk/info": []byte("Debian GNU/Linux 13.6.0 " + arch + " NETBOOT"), "linux": []byte("kernel"), "initrd.gz": []byte("initrd")}))
			if err := s.Import(t.Context(), i.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestImportFormats(t *testing.T) {
	for _, series := range []catalog.Series{"rocky", "debian", "ubuntu", "sles15", "leap15", "sles16", "leap16", "windows-server"} {
		t.Run(string(series), func(t *testing.T) {
			s, db := importSetup(t)
			i, err := db.CreateISO(t.Context(), model.ISO{Name: string(series), Family: series.Family(), Series: series}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			original := isotest.Image(importFixture(series))
			importUpload(t, s, i, "", original)
			if series == "debian" {
				extra := map[string][]byte{".disk/info": []byte("Debian GNU/Linux 13.6.0 Trixie - Official amd64 NETBOOT"), "dists/trixie/Release": []byte("release"), "install.amd/vmlinuz": []byte("netbootkernel"), "install.amd/initrd.gz": []byte("netbootinitrd")}
				importUpload(t, s, i, "netboot", isotest.Image(extra))
			}
			if err := s.Import(t.Context(), i.ID); err != nil {
				t.Fatal(err)
			}
			got, err := db.GetISO(t.Context(), i.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != model.ISOReady || len(got.Validate()) != 0 {
				t.Fatal(got)
			}
			for _, role := range catalog.RequiredRoles(series) {
				rel := got.Artifacts[role]
				trim := strings.TrimSuffix(rel, "/")
				if !fs.ValidPath(trim) || strings.Contains(rel, "\\") {
					t.Fatal(role, rel)
				}
				st, err := os.Stat(filepath.Join(s.dirs.Artifacts, filepath.FromSlash(rel)))
				if err != nil {
					t.Fatal(role, err)
				}
				if !st.IsDir() && st.Size() == 0 {
					t.Fatal(role, "empty")
				}
			}
			if series == "rocky" {
				if got.Artifacts["stage2"] == got.Artifacts["repo"] || !strings.HasSuffix(got.Artifacts["repo"], "/install/media/") {
					t.Fatal(got.Artifacts)
				}
			}
			if series == "debian" {
				if got.Artifacts["repo"] == "" {
					t.Fatal("ready Debian ISO has no HTTP repository for the shipped preseed")
				}
				for _, name := range []string{"dists/trixie/Release", "pool/main/c/curl/curl.deb"} {
					b, err := os.ReadFile(filepath.Join(s.dirs.Artifacts, got.Artifacts["repo"], name))
					if err != nil || !bytes.Equal(b, importFixture(series)[name]) {
						t.Fatalf("main Debian repository %s unavailable: %v", name, err)
					}
				}
				b, _ := os.ReadFile(filepath.Join(s.dirs.Artifacts, got.Artifacts["initrd"]))
				if string(b) != "netbootinitrd" {
					t.Fatal(string(b))
				}
			}
			if series == "ubuntu" {
				b, _ := os.ReadFile(filepath.Join(s.dirs.Artifacts, got.Artifacts["iso"]))
				if !bytes.Equal(b, original) {
					t.Fatal("whole ISO changed")
				}
			}
			if series == "windows-server" {
				b, _ := os.ReadFile(filepath.Join(s.dirs.Artifacts, got.Artifacts["winpeshl"]))
				if string(b) != winpeshlContent {
					t.Fatal(string(b))
				}
				if len(got.Offers["editions"]) != 1 {
					t.Fatal(got.Offers)
				}
				if _, err := os.Stat(filepath.Join(s.dirs.SMB, "windows-server", "10.0", "x86_64", "sources", "install.wim")); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Import(t.Context(), i.ID); !errors.Is(err, repository.ErrConflict) {
				t.Fatal("ready reimport", err)
			}
		})
	}
}
func TestImportRejectsAndCleans(t *testing.T) {
	for _, c := range []struct {
		name   string
		series catalog.Series
		change func(map[string][]byte)
		code   model.Code
	}{
		{"repo", "rocky", func(f map[string][]byte) { delete(f, "media/AppStream/repodata/repomd.xml") }, "iso_repo_incomplete"},
		{"kernel", "ubuntu", func(f map[string][]byte) { delete(f, "casper/vmlinuz") }, "iso_artifact_missing"},
		{"esxi", "esxi", func(map[string][]byte) {}, "iso_version_missing"},
		{"series", "rhel", func(map[string][]byte) {}, "iso_series_mismatch"},
		{"netboot", "debian", func(map[string][]byte) {}, "iso_extra_source_invalid"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, db := importSetup(t)
			i, err := db.CreateISO(t.Context(), model.ISO{Name: c.name, Family: c.series.Family(), Series: c.series}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			f := importFixture(c.series)
			c.change(f)
			importUpload(t, s, i, "", isotest.Image(f))
			err = s.Import(t.Context(), i.ID)
			var p model.Problem
			if !errors.As(err, &p) || p.Code != c.code || p.Subject.ID != i.ID {
				t.Fatal(err)
			}
			got, err := db.GetISO(t.Context(), i.ID)
			if err != nil || got.State != model.ISOFailed {
				t.Fatal(got, err)
			}
			n := 0
			fs.WalkDir(os.DirFS(s.dirs.Artifacts), ".", func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					n++
				}
				return err
			})
			if n != 0 {
				t.Fatal("published failed import")
			}
		})
	}
}

// Store wrapper makes the last database write fail after filesystem publication.
type importFailStore struct {
	repository.Store
	fail bool
}

func (s *importFailStore) UpdateISO(ctx context.Context, i model.ISO) (model.ISO, error) {
	if i.State == model.ISOReady && s.fail {
		return model.ISO{}, errors.New("publication DB failure")
	}
	return s.Store.UpdateISO(ctx, i)
}
func TestImportRollbackAndRecovery(t *testing.T) {
	s, db := importSetup(t)
	fault := &importFailStore{Store: db, fail: true}
	s.store = fault
	i, err := db.CreateISO(t.Context(), model.ISO{Name: "recovery", Family: "linux", Series: "ubuntu"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, i, "", isotest.Image(importFixture("ubuntu")))
	if err = s.Import(t.Context(), i.ID); err == nil {
		t.Fatal("DB failure accepted")
	}
	if _, err = os.Stat(filepath.Join(s.dirs.Artifacts, "ubuntu", "24.04.3", "x86_64")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed publication remained", err)
	}
	fault.fail = false
	s = newAcquisitionFixture(db, s.dirs)
	if err = s.Import(t.Context(), i.ID); err != nil {
		t.Fatal(err)
	}
}

func TestImportDebianExtraMustBeNetworkMedia(t *testing.T) {
	for _, c := range []struct {
		name, info string
		wantError  bool
	}{
		{"mini", "Debian GNU/Linux 13.6.0 amd64 - netboot mini.iso 20260901", false},
		{"wrong version", "Debian GNU/Linux 12.0.0 amd64 - netboot mini.iso", true},
		{"wrong arch", "Debian GNU/Linux 13.6.0 arm64 - netboot mini.iso", true},
		{"optical initrd", "Debian GNU/Linux 13.6.0 amd64 - NETINST Binary-1", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, db := importSetup(t)
			i, e := db.CreateISO(t.Context(), model.ISO{Name: c.name, Family: "linux", Series: "debian"}, importAudit())
			if e != nil {
				t.Fatal(e)
			}
			importUpload(t, s, i, "", isotest.Image(importFixture("debian")))
			extra := map[string][]byte{".disk/info": []byte(c.info), "linux": []byte("network kernel"), "initrd.gz": []byte("network initrd")}
			importUpload(t, s, i, "netboot", isotest.Image(extra))
			e = s.Import(t.Context(), i.ID)
			if c.wantError {
				var p model.Problem
				if !errors.As(e, &p) || p.Code != "iso_extra_source_invalid" {
					t.Fatal(e)
				}
			} else if e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestImportDuplicateAndChangedBytes(t *testing.T) {
	s, db := importSetup(t)
	b := isotest.Image(importFixture("ubuntu"))
	create := func(name string) model.ISO {
		i, e := db.CreateISO(t.Context(), model.ISO{Name: name, Family: "linux", Series: "ubuntu"}, importAudit())
		if e != nil {
			t.Fatal(e)
		}
		importUpload(t, s, i, "", b)
		return i
	}
	first := create("first")
	if err := s.Import(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	dup := create("duplicate")
	err := s.Import(t.Context(), dup.ID)
	var de *repository.DuplicateError
	if !errors.As(err, &de) || de.Existing != first.ID {
		t.Fatal(err)
	}
	got, _ := db.GetISO(t.Context(), dup.ID)
	if got.State != model.ISOFailed {
		t.Fatal(got)
	}
	changed := create("changed")
	f, e := os.OpenFile(s.finalPath(changed.ID, ""), os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.WriteAt([]byte{1}, 0)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	err = s.Import(t.Context(), changed.ID)
	var p model.Problem
	if !errors.As(err, &p) || p.Code != "iso_content_mismatch" {
		t.Fatal(err)
	}
	firstAfter, _ := db.GetISO(t.Context(), first.ID)
	if firstAfter.State != model.ISOReady {
		t.Fatal(firstAfter)
	}
}

func TestImportExistingDestinationNeverOverwritten(t *testing.T) {
	s, db := importSetup(t)
	first, e := db.CreateISO(t.Context(), model.ISO{Name: "first", Family: "linux", Series: "ubuntu"}, importAudit())
	if e != nil {
		t.Fatal(e)
	}
	importUpload(t, s, first, "", isotest.Image(importFixture("ubuntu")))
	if e = s.Import(t.Context(), first.ID); e != nil {
		t.Fatal(e)
	}
	second, e := db.CreateISO(t.Context(), model.ISO{Name: "second", Family: "linux", Series: "ubuntu"}, importAudit())
	if e != nil {
		t.Fatal(e)
	}
	f := importFixture("ubuntu")
	f["casper/vmlinuz"] = []byte("otherkernel")
	importUpload(t, s, second, "", isotest.Image(f))
	if e = s.Import(t.Context(), second.ID); !errors.Is(e, repository.ErrConflict) {
		t.Fatal(e)
	}
	i, _ := db.GetISO(t.Context(), second.ID)
	if i.State != model.ISOFailed {
		t.Fatal("deterministic publication collision should fail", i.State)
	}
	old, _ := db.GetISO(t.Context(), first.ID)
	b, e := os.ReadFile(filepath.Join(s.dirs.Artifacts, old.Artifacts["kernel"]))
	if e != nil || string(b) != "kernel" {
		t.Fatal(string(b), e)
	}
}

func TestImportCrashAfterRenameAndResidualTemp(t *testing.T) {
	s, db := importSetup(t)
	i, e := db.CreateISO(t.Context(), model.ISO{Name: "crash", Family: "linux", Series: "ubuntu"}, importAudit())
	if e != nil {
		t.Fatal(e)
	}
	importUpload(t, s, i, "", isotest.Image(importFixture("ubuntu")))
	if e = s.Import(t.Context(), i.ID); e != nil {
		t.Fatal(e)
	}
	i, e = db.GetISO(t.Context(), i.ID)
	if e != nil {
		t.Fatal(e)
	}
	i.State = model.ISOImporting
	i.Artifacts = nil
	i, e = db.UpdateISO(t.Context(), i)
	if e != nil {
		t.Fatal(e)
	}
	tmp := s.artifactRoot(i.Series, i.Version, i.Arch) + ".tmp-" + i.ID
	if e = os.MkdirAll(tmp, 0755); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(tmp, "partial"), []byte("incomplete"), 0644); e != nil {
		t.Fatal(e)
	}
	s = newAcquisitionFixture(db, s.dirs)
	if e = s.Import(t.Context(), i.ID); e != nil {
		t.Fatal(e)
	}
	i, e = db.GetISO(t.Context(), i.ID)
	if e != nil || i.State != model.ISOReady {
		t.Fatal(i, e)
	}
	if _, e = os.Stat(tmp); !os.IsNotExist(e) {
		t.Fatal("stale temp survived", e)
	}
}
