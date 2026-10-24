package media_test

import (
	"bytes"
	"io/fs"
	"os"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/infrastructure/media/iso9660"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

// The optional media must independently match a qualification target. Unlisted
// Kali and VMware Tools images exercise the reader, not these installer probes.
func TestIdentifyRealISO(t *testing.T) {
	p := isotest.Path(t)
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	tree, err := iso9660.Open(iso9660.FileReader{ReaderAt: f, Length: st.Size()})
	if err != nil {
		t.Fatal(err)
	}
	var expected catalog.Series
	windows := false
	if info, err := fs.ReadFile(tree, ".disk/info"); err == nil && bytes.Contains(info, []byte("Debian GNU/Linux")) {
		expected = "debian"
	} else if info, err := fs.ReadFile(tree, ".treeinfo"); err == nil && bytes.Contains(info, []byte("OpenCloudOS")) {
		expected = "opencloudos"
	} else if st, err := fs.Stat(tree, "sources/install.wim"); err == nil && st.Mode().IsRegular() {
		windows = true
	} else {
		t.Skip("media is not a Debian, OpenCloudOS or Windows installation qualification target")
	}
	found, err := Identify(t.Context(), tree)
	if err != nil {
		t.Fatal(err)
	}
	if expected != "" && found.Series != expected {
		t.Fatalf("series %q, expected %q", found.Series, expected)
	}
	if windows && (found.Series.Family() != "windows" || len(found.Editions) == 0) {
		t.Fatalf("missing Windows editions: %+v", found)
	}
	if found.Version == "" || found.Arch == "" {
		t.Fatalf("incomplete identification: %+v", found)
	}
}
