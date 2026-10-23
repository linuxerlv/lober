package iso

import (
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"io/fs"
	"testing"
)

var _ port.ImportFiles = (*ImportFiles)(nil)

func Test导入会话打开时不提前读取来源(t *testing.T) {
	files := NewImportFiles(Dirs{Staging: t.TempDir()})
	session, err := files.Open(t.Context(), model.ISO{ID: "lazy"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	_, err = session.OpenSource(t.Context(), model.Source{ISOID: "lazy", Kind: model.SourceUpload})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("应由指定来源打开时报不存在: %v", err)
	}
}
