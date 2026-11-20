package render

import (
	"github.com/lober/lober/internal/domain/catalog"
	"testing"
)

func TestRenderRejectsRetiredFormats(t *testing.T) {
	for _, format := range []catalog.Format{"autoyast", "agama"} {
		for _, kind := range []catalog.FileKind{catalog.FileAnswer, catalog.FileAux, catalog.FileScript, catalog.FileMenu} {
			r := runtimeFixture("pre", runtimeInstance("printf hook\n", true, "sh", 3))
			r.Snapshot.Format = format
			r.Kind = kind
			r.Firmware = "uefi"
			b, p, err := Render(r)
			if len(b) != 0 || err == nil && len(p) == 0 {
				t.Errorf("停用格式 %s 仍可生成 %s", format, kind)
			}
		}
	}
}
