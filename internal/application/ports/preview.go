package ports

import (
	"context"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// FrozenPreview 只用已经冻结的数据生成预览文件。
type FrozenPreview interface {
	RenderFrozen(context.Context, model.Snapshot, catalog.Firmware, string) ([]PreviewFile, []string, []model.Problem, error)
}
type PreviewFile struct {
	Kind, Name         string
	Sample             bool
	Body, Slot, Digest string
}
type PreviewEndpoints struct{ Gateway, Artifacts, SMB, SyslogAddr string }
