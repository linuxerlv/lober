package iso

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
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
