package iso

import (
	"encoding/binary"
	"errors"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/iso/isotest"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

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
