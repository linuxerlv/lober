package iso

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/iso/isotest"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestWindowsExtractionPreservesCancellation(t *testing.T) {
	s, _ := importSetup(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// A present source exercises cancellation during output, not missing input.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "boot"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sources"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"boot/bcd", "boot/boot.sdi", "sources/boot.wim"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("artifact"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.extractWindows(ctx, "test", os.DirFS(root), t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation replaced: %v", err)
	}
}

func importWIM() []byte {
	xml := `<WIM><IMAGE INDEX="1"><NAME>Windows Server 2025 Standard</NAME><FLAGS>ServerStandard</FLAGS><WINDOWS><ARCH>9</ARCH><VERSION><MAJOR>10</MAJOR><MINOR>0</MINOR><BUILD>26100</BUILD></VERSION></WINDOWS></IMAGE></WIM>`
	words := utf16.Encode([]rune(xml))
	body := []byte{0xff, 0xfe}
	for _, w := range words {
		body = binary.LittleEndian.AppendUint16(body, w)
	}
	b := make([]byte, 208)
	copy(b, []byte{'M', 'S', 'W', 'I', 'M', 0, 0, 0})
	binary.LittleEndian.PutUint32(b[8:], 208)
	binary.LittleEndian.PutUint32(b[12:], 0x10d00)
	binary.LittleEndian.PutUint32(b[44:], 1)
	binary.LittleEndian.PutUint64(b[72:], uint64(len(body)))
	binary.LittleEndian.PutUint64(b[80:], 208)
	binary.LittleEndian.PutUint64(b[88:], uint64(len(body)))
	return append(b, body...)
}
func TestImportWindowsMissingTools(t *testing.T) {
	for _, tool := range []string{"wimboot", "winpe-busybox", "winpe-curl"} {
		t.Run(tool, func(t *testing.T) {
			s, db := importSetup(t)
			if err := os.Remove(filepath.Join(s.dirs.Tools, tool)); err != nil {
				t.Fatal(err)
			}
			i, err := db.CreateISO(t.Context(), model.ISO{Name: "windows", Family: "windows", Series: "windows-server"}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			importUpload(t, s, i, "", isotest.Image(importFixture("windows-server")))
			err = s.Import(t.Context(), i.ID)
			var p model.Problem
			if !errors.As(err, &p) || p.Code != "iso_tool_missing" {
				t.Fatal(err)
			}
		})
	}
}
