package main

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/lober/lober/internal/domain/model"
)

type bootConfigReader struct{ root string }

func (b bootConfigReader) ReadESXiBootConfig(ctx context.Context, s model.SnapshotISO) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	repo := strings.TrimSuffix(s.Artifacts["repo"], "/")
	if !fs.ValidPath(repo) || repo == "." || strings.ContainsAny(repo, "\\:") {
		return "", errors.New("missing immutable ESXi repository")
	}
	root, err := os.OpenRoot(b.root)
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := root.Open(path.Join(repo, "boot.cfg"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(data), nil
}
