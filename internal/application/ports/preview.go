package ports

import (
	"context"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// FrozenPreview 只用已经冻结的数据生成预览文件。
type FrozenPreview interface {
	// RenderFrozen 的八项只读应用约定唯一连接 docs/contracts/authoring-preview.md「冻结预览能力」。
	// 只接受已授权调用方的冻结数据，原配置先于渲染；失败丢弃全部文件，示例令牌不是真实凭据。
	RenderFrozen(context.Context, model.Snapshot, catalog.Firmware, string) ([]PreviewFile, []string, []model.Problem, error)
}
type PreviewFile struct {
	Kind, Name         string
	Sample             bool
	Body, Slot, Digest string
}
type PreviewEndpoints struct{ Gateway, Artifacts, SMB, SyslogAddr string }
