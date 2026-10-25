package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
)

// SourceControl 只给可信的应用编排使用；成功写入后由控制实现唤醒扫描。
type SourceControl interface {
	// Register 的八项应用约定唯一连接 docs/contracts/media.md 第3节及第10节SourceControl说明；
	// 可信调用、错误原样传递和成功后唤醒位置按该说明，不增加网页授权。
	Register(context.Context, model.ISO, string, string, string, int64, string, repository.Audit) error
	// Upload 的八项应用约定唯一连接 docs/contracts/media.md 第4节及第10节SourceControl说明；
	// 可信调用、错误原样传递和成功后唤醒位置按该说明，不增加网页授权。
	Upload(context.Context, string, string, int64, io.Reader, repository.Audit) error
	// Cancel 的八项应用约定唯一连接 docs/contracts/media.md 第5节及第10节SourceControl说明；
	// 可信调用、错误原样传递和成功后唤醒位置按该说明，不增加网页授权。
	Cancel(context.Context, string, repository.Audit) error
	// Retry 的八项应用约定唯一连接 docs/contracts/media.md 第5节及第10节SourceControl说明；
	// 可信调用、错误原样传递和成功后唤醒位置按该说明，不增加网页授权。
	Retry(context.Context, string, repository.Audit) error
	// Reimport 的八项应用约定唯一连接 docs/contracts/media.md 第6节及第10节SourceControl说明；
	// 可信调用、错误原样传递和成功后唤醒位置按该说明，不增加网页授权。
	Reimport(context.Context, string, repository.Audit) error
	// FreeSpace 的八项应用约定唯一连接 docs/contracts/media.md 第10节及第10节SourceControl说明；
	// 可信调用、错误原样传递和成功后唤醒位置按该说明，不增加网页授权。
	FreeSpace(context.Context) (int64, int64, error)
}

// Deletion 保存快照后删除记录，已提交后的文件清理失败由后台重试。
type Deletion interface {
	// Delete 的八项应用约定唯一连接 docs/contracts/media.md 第7节与第10节Deletion说明；
	// 先落快照再删除及审计，未知结果保留，清理失败不撤回已提交删除。
	Delete(context.Context, string, int64, repository.Audit) error
}

// Waker 只提示后台重读持久化待办。
// Wake 的八项约定唯一连接 docs/contracts/media.md 第10节「Waker.Wake」行及第8节唤醒边界。
type Waker interface{ Wake() }
