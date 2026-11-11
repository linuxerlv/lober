package iso

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lober/lober/internal/application/media/port"
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
		return fault(port.PathRejected)
	}
	return nil
}
