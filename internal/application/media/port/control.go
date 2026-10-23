package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
)

// SourceControl 只给可信的应用编排使用；成功写入后由控制实现唤醒扫描。
type SourceControl interface {
	Register(context.Context, model.ISO, string, string, string, int64, string, repository.Audit) error
	Upload(context.Context, string, string, int64, io.Reader, repository.Audit) error
	Cancel(context.Context, string, repository.Audit) error
	Retry(context.Context, string, repository.Audit) error
	Reimport(context.Context, string, repository.Audit) error
	FreeSpace(context.Context) (int64, int64, error)
}

// Deletion 保存快照后删除记录，已提交后的文件清理失败由后台重试。
type Deletion interface {
	Delete(context.Context, string, int64, repository.Audit) error
}
