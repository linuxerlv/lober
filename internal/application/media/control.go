package media

import (
	"context"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
)

type sourceControl struct {
	source *Sources
	waker  port.Waker
}

func NewControl(s *Sources, w port.Waker) port.SourceControl { return &sourceControl{s, w} }
func (c *sourceControl) Register(ctx context.Context, i model.ISO, k, a, r string, n int64, d string, audit repository.Audit) error {
	err := c.source.RegisterQueued(ctx, i, k, a, r, n, d, audit)
	if err == nil {
		c.waker.Wake()
	}
	return err
}
func (c *sourceControl) Upload(ctx context.Context, id, r string, n int64, b io.Reader, a repository.Audit) error {
	err := c.source.Upload(ctx, id, r, n, b, a)
	if err == nil {
		c.waker.Wake()
	}
	return err
}
func (c *sourceControl) Cancel(ctx context.Context, id string, a repository.Audit) error {
	return c.source.Cancel(ctx, id, a)
}
func (c *sourceControl) Retry(ctx context.Context, id string, a repository.Audit) error {
	err := c.source.Retry(ctx, id, a)
	if err == nil {
		c.waker.Wake()
	}
	return err
}
func (c *sourceControl) Reimport(ctx context.Context, id string, a repository.Audit) error {
	err := c.source.RequestReimport(ctx, id, a)
	if err == nil {
		c.waker.Wake()
	}
	return err
}
func (c *sourceControl) FreeSpace(ctx context.Context) (int64, int64, error) {
	return c.source.FreeSpace(ctx)
}
