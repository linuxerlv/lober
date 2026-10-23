package iso

import (
	"context"
	"io"
	"path/filepath"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

const (
	isoSourceReasonURL   = model.ISOReasonURL
	isoSourceReasonLocal = model.ISOReasonPath
	isoSourceReasonChunk = model.ISOReasonChunk
)

func sourceRejected(id, role, reason string) error {
	return &repository.ProblemsError{Kind: repository.ErrInvalid, Problems: []model.Problem{model.NewProblem("iso_source_rejected", sourceSubject(id, role), reason)}}
}

func sourceSubject(id, role string) model.Subject {
	path := "source"
	if role != "" {
		path += "." + role
	}
	return model.Subject{Kind: "iso", ID: id, Path: path}
}

func (s *Sources) getSource(ctx context.Context, id, role string) (model.Source, error) {
	src, ok, err := s.store.GetSource(ctx, id, role)
	if err != nil {
		return model.Source{}, err
	}
	if !ok {
		return model.Source{}, repository.ErrNotFound
	}
	return src, nil
}

func (s *Sources) stagingPath(id, role string) string { return s.finalPath(id, role) + ".part" }
func (s *Sources) finalPath(id, role string) string {
	if role != "" {
		id += "." + role
	}
	return filepath.Join(s.dirs.Staging, id)
}
func (s *Sources) sourcePath(src model.Source) string {
	if src.Kind == model.SourceLocal {
		return src.Path
	}
	return s.finalPath(src.ISOID, src.Role)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
