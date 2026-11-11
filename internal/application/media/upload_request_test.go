package media

import (
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"strings"
	"testing"
)

func Test上传流式确长与范围错误分开(t *testing.T) {
	for _, body := range []string{"x", "1234567890", "123456789"} {
		f := &fixture{source: model.Source{Kind: model.SourceUpload, TotalSize: 20}}
		err := setup(f).Upload(t.Context(), admin, UploadRequest{ID: "image", End: 8, Total: -1, ContentLength: -1, Body: strings.NewReader(body)})
		if len(body) == 9 {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			var re *RangeError
			if !errors.Is(err, repository.ErrInvalid) || errors.As(err, &re) {
				t.Fatal(err)
			}
		}
		if len(f.audit) != 1 || f.audit[0] != (repository.Audit{}) {
			t.Fatal(f.audit)
		}
	}
	f := &fixture{source: model.Source{Kind: model.SourceUpload, TotalSize: 4}}
	err := setup(f).Upload(t.Context(), admin, UploadRequest{ID: "image", End: 8, Total: -1, ContentLength: -1, Body: f})
	var re *RangeError
	if !errors.As(err, &re) || f.reads != 0 {
		t.Fatal(err)
	}
}
