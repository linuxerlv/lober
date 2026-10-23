package iso

import (
	"context"
	"io"
	"path/filepath"
)

func (s *Sources) finalPath(id, role string) string {
	if role != "" {
		id += "." + role
	}
	return filepath.Join(s.dirs.Staging, id)
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
