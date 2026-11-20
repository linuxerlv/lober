package iso9660_test

import (
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/lober/lober/internal/infrastructure/media/iso9660"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

func TestRealISO(t *testing.T) {
	name := isotest.Path(t)
	r, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	stat, err := r.Stat()
	if err != nil {
		t.Fatal(err)
	}
	f, err := iso9660.Open(iso9660.FileReader{ReaderAt: r, Length: stat.Size()})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	known := false
	sample := ""
	err = fs.WalkDir(f, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		lower := strings.ToLower(p)
		for _, prefix := range []string{"isolinux", "packages", "dists", "sources", "boot", "efi", "pool", "casper", "install.amd", ".disk", "program files"} {
			if lower == prefix {
				known = true
			}
		}
		if !d.IsDir() && d.Type().IsRegular() && sample == "" {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Size() > 0 {
				sample = p
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 2 || !known || sample == "" {
		t.Fatalf("real ISO lacks expected media paths or files: %d entries, known=%v, sample=%q", count, known, sample)
	}
	file, err := f.Open(sample)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	b := make([]byte, 64)
	n, err := file.Read(b)
	if n == 0 || (err != nil && err != io.EOF) {
		t.Fatalf("read %q: %d bytes, %v", sample, n, err)
	}
	t.Logf("walked %d entries; read %d bytes from %s", count, n, sample)
}
