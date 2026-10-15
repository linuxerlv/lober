package iso

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/lober/lober/internal/store"
)

// resolvedPath resolves even a not-yet-existing child, by resolving its nearest
// existing ancestor. This prevents symlinked parent directories escaping roots.
func resolvedPath(p string) (string, error) {
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	var tail []string
	for {
		actual, err := filepath.EvalSymlinks(p)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				actual = filepath.Join(actual, tail[i])
			}
			return actual, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", err
		}
		tail = append(tail, filepath.Base(p))
		p = parent
	}
}

func localRoot(roots []string, p string) (string, string, bool) {
	actual, err := resolvedPath(p)
	if err != nil {
		return "", "", false
	}
	for _, root := range roots {
		allowed, err := resolvedPath(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(allowed, actual)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return allowed, rel, true
		}
	}
	return "", "", false
}

// CheckLocalPath compares path segments after absolute normalization and
// resolving symlinked ancestors; lexical prefix matches alone are insufficient.
func CheckLocalPath(roots []string, p string) error {
	if _, _, ok := localRoot(roots, p); !ok {
		return sourceRejected("", "", isoSourceReasonLocal)
	}
	return nil
}

func (s *Sources) fetchLocal(ctx context.Context, src store.Source) error {
	root, rel, ok := localRoot(s.dirs.LocalRoots, src.Path)
	if !ok {
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonLocal)
	}
	// OpenInRoot prevents a concurrent symlink change from escaping the root.
	f, err := os.OpenInRoot(root, rel)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 {
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonLocal)
	}
	src.TotalSize = st.Size()
	src, err = s.store.SaveSource(ctx, src, store.Audit{})
	if err != nil {
		return err
	}
	// finishFetch revalidates the path and hashes in place. Original local files
	// remain operator-owned: cancellation and retry never remove them.
	return s.finishFetch(ctx, src)
}
