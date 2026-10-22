package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// HookReader 只读取组件，不能装载内置版本或写入其他对象。
type HookReader interface {
	GetHook(ctx context.Context, k model.HookKey) (model.Hook, error)
	GetHooks(ctx context.Context, ks []model.HookKey) (map[model.HookKey]model.Hook, error)
	ListHookVersions(ctx context.Context, id string) ([]model.Hook, error)
	ListHooks(ctx context.Context, q repository.HookQuery) (repository.Page[model.Hook], error)
}
type Hooks interface {
	HookReader
	CreateHook(ctx context.Context, h model.Hook) (model.Hook, error)
	SaveHookDraft(ctx context.Context, h model.Hook) (model.Hook, error)
	NewHookVersion(ctx context.Context, id string, from int, author string) (model.Hook, error)
	SubmitHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error
	ApproveHook(ctx context.Context, k model.HookKey, rev int64, approver string, a repository.Audit) error
	RejectHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error
	DeprecateHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error
	RenameHook(ctx context.Context, id, name string, a repository.Audit) error
	DeleteHookDraft(ctx context.Context, k model.HookKey, rev int64) error
}
