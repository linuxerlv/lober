package media_test

import (
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
	"os"
	"path/filepath"
	"testing"
)

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
