package iso

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/media/port"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func importDigest(ctx context.Context, f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx: ctx, r: f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func prepareImportTemp(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	return os.Mkdir(p, 0755)
}
func publishImportTree(tmp, root, owner string) (bool, error) {
	if st, err := os.Lstat(root); err == nil {
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return false, fault(port.PublicationConflict)
		}
		b, err := os.ReadFile(filepath.Join(root, ".lober-owner"))
		if err != nil || string(b) != owner {
			return false, fmt.Errorf("%w: artifact destination already belongs to other content", fault(port.PublicationConflict))
		}
		// A crash between rename and DB commit can reuse a complete immutable tree.
		same, err := equalImportTrees(tmp, root)
		if err != nil {
			return false, err
		}
		if !same {
			return false, fmt.Errorf("%w: existing artifact tree differs", fault(port.PublicationConflict))
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.Rename(tmp, root); err != nil {
		return false, err
	}
	return true, nil
}
func equalImportTrees(a, b string) (bool, error) {
	same := true
	countA, countB := 0, 0
	err := fs.WalkDir(os.DirFS(a), ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		countA++
		left, e := os.Lstat(filepath.Join(a, p))
		if e != nil {
			return e
		}
		right, e := os.Lstat(filepath.Join(b, p))
		if e != nil {
			if errors.Is(e, os.ErrNotExist) {
				same = false
				return nil
			}
			return e
		}
		if left.Mode().Type() != right.Mode().Type() || left.Size() != right.Size() && !left.IsDir() {
			same = false
			return nil
		}
		if left.Mode()&os.ModeSymlink != 0 {
			x, e := os.Readlink(filepath.Join(a, p))
			if e != nil {
				return e
			}
			y, e := os.Readlink(filepath.Join(b, p))
			if e != nil {
				return e
			}
			same = same && x == y
			return nil
		}
		if left.Mode().IsRegular() {
			x, e := os.Open(filepath.Join(a, p))
			if e != nil {
				return e
			}
			defer x.Close()
			y, e := os.Open(filepath.Join(b, p))
			if e != nil {
				return e
			}
			defer y.Close()
			hx, hy := sha256.New(), sha256.New()
			if _, e = io.Copy(hx, x); e != nil {
				return e
			}
			if _, e = io.Copy(hy, y); e != nil {
				return e
			}
			same = same && hex.EncodeToString(hx.Sum(nil)) == hex.EncodeToString(hy.Sum(nil))
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	err = fs.WalkDir(os.DirFS(b), ".", func(_ string, _ fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		countB++
		return nil
	})
	return same && countA == countB, err
}
