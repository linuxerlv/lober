package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func (s *Sources) Retry(ctx context.Context, id string, a repository.Audit) error {
	if err := repository.CheckAudit(a); err != nil {
		return err
	}
	item, err := s.isos.GetISO(ctx, id)
	if err != nil {
		return err
	}
	if item.State != model.ISOAcquiring && item.State != model.ISOFailed {
		return repository.ErrConflict
	}
	sources, err := s.sources.SourcesOf(ctx, id)
	if err != nil {
		return err
	}
	for _, src := range sources {
		if src.State != model.SourceFailed && src.State != model.SourceCancelled {
			continue
		}
		unlock := s.locks.Lock(id, src.Role)
		err = func() error {
			current, err := s.getSource(ctx, id, src.Role)
			if err != nil {
				return err
			}
			if current.State != model.SourceFailed && current.State != model.SourceCancelled {
				return repository.ErrConflict
			}
			return s.retrySource(ctx, current, a)
		}()
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// 重试先移走托管字节，再重置记录。审计失败恢复文件，提交成功后删除旧副本。
func (s *Sources) retrySource(ctx context.Context, src model.Source, a repository.Audit) error {
	var files port.RetryFiles
	if src.Kind != model.SourceLocal {
		var err error
		files, err = s.transfer.ParkRetry(ctx, src)
		if err != nil {
			return s.problem(src, err)
		}
	}

	src.State = model.SourcePending
	src.Fetched = 0
	src.Error = ""
	if _, err := s.sources.SaveSource(ctx, src, a); err != nil {
		if files != nil {
			return errors.Join(err, files.Restore())
		}
		return err
	}
	if files != nil {
		return files.Commit()
	}
	return nil
}

// RequestReimport 先保存排队事实和审计，后台再执行导入，重启后仍能恢复。
func (s *Sources) RequestReimport(ctx context.Context, id string, a repository.Audit) error {
	if err := repository.CheckAudit(a); err != nil {
		return err
	}
	item, err := s.isos.GetISO(ctx, id)
	if err != nil {
		return err
	}
	if item.State != model.ISOFailed {
		return repository.ErrConflict
	}
	sources, err := s.sources.SourcesOf(ctx, id)
	if err != nil {
		return err
	}
	if !allFetched(item, sources) {
		return repository.ErrConflict
	}
	// 用主来源承载排队请求，版本、镜像状态和审计在同一事务提交。
	for _, src := range sources {
		if src.Role == "" {
			_, err = s.sources.SaveSourceAndAcquire(ctx, src, item.Rev, a)
			return err
		}
	}
	err = repository.ErrConflict
	return err
}
