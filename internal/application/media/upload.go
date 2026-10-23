package media

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
)

func (s *Sources) Upload(ctx context.Context, id, role string, offset int64, r io.Reader, a repository.Audit) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = checkSourceKey(id, role); err != nil {
		return err
	}
	if a != (repository.Audit{}) {
		if err = repository.CheckAudit(a); err != nil {
			return err
		}
	}
	unlock := s.locks.Lock(id, role)
	defer unlock()
	src, err := s.getSource(ctx, id, role)
	if err != nil {
		return err
	}
	if src.Kind != model.SourceUpload || src.State != model.SourcePending && src.State != model.SourceFetching && src.State != model.SourceFetched {
		return repository.ErrConflict
	}
	if offset < 0 || offset >= src.TotalSize || src.TotalSize <= 0 || r == nil {
		return sourceRejected(id, role, isoSourceReasonChunk)
	}
	chunk, err := s.transfer.BeginUpload(ctx, src, offset, r)
	if err != nil {
		return s.problem(src, err)
	}
	closed := false
	closeChunk := func() error {
		if closed {
			return nil
		}
		closed = true
		return chunk.Close()
	}
	defer closeChunk()
	ranges, err := s.sources.RangesOf(ctx, id, role)
	if err != nil {
		return err
	}
	if err = chunk.CheckOverlap(ctx, ranges); err != nil {
		return s.problem(src, err)
	}
	if src.State == model.SourceFetched {
		if err = chunk.VerifyReplay(ctx); err != nil {
			return s.problem(src, err)
		}
		if a != (repository.Audit{}) {
			_, err = s.sources.SaveSource(ctx, src, a)
		}
		return err
	}
	if src.State == model.SourcePending {
		src.State = model.SourceFetching
	}
	src, err = s.sources.SaveSource(ctx, src, a)
	if err != nil {
		return err
	}
	if err = chunk.Write(ctx); err != nil {
		return s.problem(src, err)
	}
	if err = s.sources.SaveRange(ctx, id, role, offset, chunk.Size()); err != nil {
		return err
	}
	src, err = s.getSource(ctx, id, role)
	if err != nil {
		return err
	}
	if src.State != model.SourceFetching {
		return repository.ErrConflict
	}
	ranges, err = s.sources.RangesOf(ctx, id, role)
	if err != nil {
		return err
	}
	if len(ranges) == 1 && ranges[0].Start == 0 && ranges[0].Length == src.TotalSize {
		// 确认区间提交且重读成功后才检查关闭错误，保留原来的保存顺序。
		if err = closeChunk(); err != nil {
			return err
		}
		session, e := s.transfer.BeginPublish(ctx, src)
		if e != nil {
			err = s.problem(src, e)
		} else {
			defer session.Close()
			err = s.finishFetch(ctx, src, session)
		}
		if err != nil {
			s.failFetch(ctx, src, err)
		}
	}
	return err
}
