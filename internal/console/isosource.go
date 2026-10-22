package console

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
)

// ISOSource is caller-owned: implementations need not import console. Register
// persists pending work; assembly supervises fetching and importing.
type ISOSource interface {
	Register(context.Context, model.ISO, string, string, string, int64, string, repository.Audit) error
	Upload(context.Context, string, string, int64, io.Reader, repository.Audit) error
	Cancel(context.Context, string, repository.Audit) error
	Retry(context.Context, string, repository.Audit) error
	Reimport(context.Context, string, repository.Audit) error
	FreeSpace(context.Context) (int64, int64, error)
}
type SourceKind string

// ISOSourceState is the read-only source/range contract consumed by HTTP.
type ISOSourceState interface {
	SourcesOf(context.Context, string) ([]model.Source, error)
	GetSource(context.Context, string, string) (model.Source, bool, error)
	RangesOf(context.Context, string, string) ([]model.Range, error)
}

// ISODeletion belongs to assembly: persist a snapshot before audited deletion,
// then keep retrying byte cleanup durably. It must not delete if preparation fails.
type ISODeletion interface {
	Delete(context.Context, string, int64, repository.Audit) error
}

const (
	SourceURL    SourceKind = "url"
	SourceUpload SourceKind = "upload"
	SourcePath   SourceKind = "path"
)

func (k SourceKind) Valid() bool { return k == SourceURL || k == SourceUpload || k == SourcePath }
