package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
)

// DeletionSnapshot 保留旧日志字段；Sources 必须非 nil，来源身份在登记后不变。
type DeletionSnapshot struct {
	ISO     model.ISO
	Sources []model.Source
}

// DeletionJournal 必须先同步保存再返回；List 返回有效快照和坏文件的合并错误。
// Discard 可重复；调用方串行化日志写入及恢复。
type DeletionJournal interface {
	// Persist 的八项能力约定唯一连接 docs/contracts/media.md 第10节「DeletionJournal.Persist」行及该节共同边界。
	Persist(context.Context, DeletionSnapshot) error
	// List 的八项能力约定唯一连接 docs/contracts/media.md 第10节「DeletionJournal.List」行及该节共同边界。
	List(context.Context) ([]DeletionSnapshot, error)
	// Discard 的八项能力约定唯一连接 docs/contracts/media.md 第10节「DeletionJournal.Discard」行及该节共同边界。
	Discard(context.Context, string) error
}

// DeletedFiles 仅清理拥有的字节；调用方先确认记录消失并持有导入和来源锁。
type DeletedFiles interface {
	// Cleanup 的八项能力约定唯一连接 docs/contracts/media.md 第10节「DeletedFiles.Cleanup」行及该节共同边界。
	Cleanup(context.Context, DeletionSnapshot) error
}
