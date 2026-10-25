package authoring

import (
	"context"
	"errors"

	"github.com/lober/lober/internal/application/authoring/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type bootstrapper struct {
	repository port.BuiltinRepository
	hooks      []model.Hook
	templates  []model.Template
}

// NewBootstrapper 接收组装点提供的发布清单，不访问依赖。调用方保证仓储可用、
// 对象符合内置发布格式，并在初始化期间不修改清单或其中的对象；没有用户身份输入。
func NewBootstrapper(repository port.BuiltinRepository, hooks []model.Hook, templates []model.Template) Bootstrapper {
	return &bootstrapper{repository: repository, hooks: hooks, templates: templates}
}

func (b *bootstrapper) Initialise(ctx context.Context) error {
	for _, h := range b.hooks {
		if err := retryBuiltin(ctx, func() error { return b.repository.EnsureBuiltinHook(ctx, h) }); err != nil {
			return err
		}
	}
	for _, v := range b.templates {
		if err := retryBuiltin(ctx, func() error { return b.repository.EnsureBuiltinTemplate(ctx, v) }); err != nil {
			return err
		}
	}
	return nil
}

// retryBuiltin 仅供本模块的单对象装载使用，不向调用方开放任意存储回调。
func retryBuiltin(ctx context.Context, write func() error) error {
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := write()
		if err == nil || !errors.Is(err, repository.ErrConflict) || attempt == 3 {
			return err
		}
	}
	return repository.ErrConflict
}
