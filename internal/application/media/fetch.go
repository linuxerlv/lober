package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

const (
	isoSourceReasonURL   = model.ISOReasonURL
	isoSourceReasonLocal = model.ISOReasonPath
	isoSourceReasonChunk = model.ISOReasonChunk
)

func sourceSubject(id, role string) model.Subject {
	path := "source"
	if role != "" {
		path += "." + role
	}
	return model.Subject{Kind: "iso", ID: id, Path: path}
}
func sourceRejected(id, role, reason string) error {
	return &repository.ProblemsError{Kind: repository.ErrInvalid, Problems: []model.Problem{model.NewProblem("iso_source_rejected", sourceSubject(id, role), reason)}}
}
func checkSourceKey(id, role string) error { return repository.CheckSourceKey(id, role) }
func (s *Sources) getSource(ctx context.Context, id, role string) (model.Source, error) {
	src, ok, err := s.sources.GetSource(ctx, id, role)
	if err != nil {
		return model.Source{}, err
	}
	if !ok {
		return model.Source{}, repository.ErrNotFound
	}
	return src, nil
}
func (s *Sources) problem(src model.Source, err error) error {
	mapped, _ := s.problemTree(src, err)
	return mapped
}
func (s *Sources) problemTree(src model.Source, err error) (error, bool) {
	var f *port.Fault
	if !errors.As(err, &f) {
		return err, false
	}
	// 故障本身整体替换并隐藏内部细节；合并的其他错误分支仍保留原对象。
	if _, direct := err.(*port.Fault); !direct {
		switch wrapped := err.(type) {
		case interface{ Unwrap() []error }:
			causes := wrapped.Unwrap()
			mapped := make([]error, len(causes))
			changed := false
			for i, cause := range causes {
				var replaced bool
				mapped[i], replaced = s.problemTree(src, cause)
				changed = changed || replaced
			}
			if changed {
				return errors.Join(mapped...), true
			}
			return err, false
		case interface{ Unwrap() error }:
			if mapped, changed := s.problemTree(src, wrapped.Unwrap()); changed {
				return mapped, true
			}
			return err, false
		}
	}
	switch f.Kind {
	case port.URLRejected:
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonURL), true
	case port.PathRejected:
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonLocal), true
	case port.ChunkRejected:
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk), true
	case port.ContentMismatch:
		return &repository.ProblemsError{Kind: repository.ErrRejected, Problems: []model.Problem{model.NewProblem("iso_content_mismatch", sourceSubject(src.ISOID, src.Role), f.Expected, f.Actual)}}, true
	case port.PublicationConflict:
		return repository.ErrConflict, true
	case port.InvalidFile:
		return repository.ErrInvalid, true
	}
	return err, false
}
func (s *Sources) FetchOne(ctx context.Context, id, role string) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = checkSourceKey(id, role); err != nil {
		return err
	}
	unlock := s.locks.Lock(id, role)
	defer unlock()
	src, err := s.getSource(ctx, id, role)
	if err != nil {
		return err
	}
	if src.State == model.SourceFetched {
		return nil
	}
	if src.Kind == model.SourceUpload || src.State != model.SourcePending && src.State != model.SourceFetching {
		return repository.ErrConflict
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	untrack := s.locks.Track(id, role, cancel)
	defer untrack()
	// 先发布取消函数，再读取版本，避免取消落在两个步骤之间。
	src, err = s.getSource(ctx, id, role)
	if err != nil {
		return err
	}
	if src.State == model.SourcePending {
		src.State = model.SourceFetching
		src, err = s.sources.SaveSource(ctx, src, repository.Audit{})
		if err != nil {
			return err
		}
	}
	if src.State != model.SourceFetching {
		return repository.ErrConflict
	}
	defer func() {
		if err != nil {
			s.failFetch(ctx, model.Source{ISOID: id, Role: role}, err)
		}
	}()
	session, err := s.transfer.BeginFetch(ctx, src)
	if err != nil {
		return s.problem(model.Source{ISOID: id, Role: role}, err)
	}
	defer session.Close()
	offset, err := session.Resume(ctx)
	if err != nil {
		return s.problem(model.Source{ISOID: id, Role: role}, err)
	}
	if offset != src.Fetched {
		src.Fetched = offset
		src, err = s.sources.SaveSource(ctx, src, repository.Audit{})
		if err != nil {
			return err
		}
	}
	info, openErr := session.Open(ctx)
	if openErr != nil && !info.Reset {
		return s.problem(model.Source{ISOID: id, Role: role}, openErr)
	}
	src.Fetched = info.Offset
	src.TotalSize = info.TotalSize
	src, err = s.sources.SaveSource(ctx, src, repository.Audit{})
	if openErr != nil {
		return errors.Join(s.problem(model.Source{ISOID: id, Role: role}, openErr), err)
	}
	if err != nil {
		return err
	}
	if !info.Complete {
		for {
			n, done, readErr := session.Step(ctx)
			var saveErr error
			if n != src.Fetched {
				src.Fetched = n
				src, saveErr = s.sources.SaveSource(ctx, src, repository.Audit{})
			}
			if readErr != nil || saveErr != nil {
				return errors.Join(s.problem(model.Source{ISOID: id, Role: role}, readErr), saveErr)
			}
			if done {
				break
			}
		}
	}
	return s.finishFetch(ctx, src, session)
}
func (s *Sources) finishFetch(ctx context.Context, src model.Source, session port.FetchSession) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	content, err := session.Verify(ctx)
	if err != nil {
		return s.problem(src, err)
	}
	if content.Size <= 0 || src.TotalSize > 0 && content.Size != src.TotalSize {
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk)
	}
	if src.ExpectedSHA256 != "" && content.SHA256 != src.ExpectedSHA256 {
		return s.problem(src, &port.Fault{Kind: port.ContentMismatch, Expected: src.ExpectedSHA256, Actual: content.SHA256})
	}
	current, err := s.getSource(ctx, src.ISOID, src.Role)
	if err != nil {
		return err
	}
	if current.State != model.SourceFetching || current.Rev != src.Rev {
		return repository.ErrConflict
	}
	if err = session.Publish(ctx); err != nil {
		return s.problem(src, err)
	}
	src.State = model.SourceFetched
	src.Error = ""
	src.Fetched = content.Size
	if src.TotalSize == 0 {
		src.TotalSize = content.Size
	}
	if _, err = s.sources.SaveSource(ctx, src, repository.Audit{}); err != nil {
		return errors.Join(err, session.Restore(context.WithoutCancel(ctx)))
	}
	return nil
}
func (s *Sources) failFetch(ctx context.Context, src model.Source, cause error) {
	if ctx.Err() != nil {
		return
	}
	cleanup := context.WithoutCancel(ctx)
	current, err := s.getSource(cleanup, src.ISOID, src.Role)
	if err != nil || current.State != model.SourceFetching {
		return
	}
	current.State = model.SourceFailed
	current.Error = cause.Error()
	_, _ = s.sources.SaveSource(cleanup, current, repository.Audit{})
}
