package media

import (
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"testing"
)

func Test镜像文件故障转换保留并列错误(t *testing.T) {
	independent := errors.New("independent file error")
	hidden := errors.New("private source cause")
	joined := fmt.Errorf("wrapped: %w", errors.Join(&port.Fault{Kind: port.ContentMismatch, Role: "netboot", Expected: "first", Actual: "second", Cause: hidden}, independent))
	got := importFileError("image", joined)
	var p model.Problem
	if !errors.As(got, &p) || p.Code != "iso_content_mismatch" || p.Subject.ID != "image" || p.Subject.Path != "source.netboot" {
		t.Fatal(got)
	}
	if !errors.Is(got, independent) || errors.Is(got, hidden) {
		t.Fatal("独立错误丢失或内部原因暴露", got)
	}
	conflict := importFileError("image", fmt.Errorf("wrapped: %w", &port.Fault{Kind: port.PublicationConflict}))
	if !errors.Is(conflict, repository.ErrConflict) {
		t.Fatal(conflict)
	}
	unknown := fmt.Errorf("unknown: %w", independent)
	if got := importFileError("image", unknown); got != unknown {
		t.Fatal("未知错误包装改变", got)
	}
}
