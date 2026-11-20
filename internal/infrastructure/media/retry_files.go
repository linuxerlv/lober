package iso

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"os"
)

type parkedFile struct{ original, backup string }
type retryFiles struct{ parked []parkedFile }

func (t *Transfer) ParkRetry(ctx context.Context, src model.Source) (port.RetryFiles, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files := &retryFiles{}
	parked := []parkedFile{}
	restore := func() error {
		var errs []error
		for i := len(parked) - 1; i >= 0; i-- {
			if err := os.Rename(parked[i].backup, parked[i].original); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	if src.Kind != model.SourceLocal {
		for _, path := range []string{t.stagingPath(src.ISOID, src.Role), t.sourcePath(src)} {
			st, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, errors.Join(err, restore())
			}
			if !st.Mode().IsRegular() {
				return nil, errors.Join(fault(port.InvalidFile), restore())
			}
			temp, err := os.CreateTemp(t.dirs.Staging, ".retry-"+src.ISOID+"-")
			if err != nil {
				return nil, errors.Join(err, restore())
			}
			backup := temp.Name()
			if err = temp.Close(); err != nil {
				os.Remove(backup)
				return nil, errors.Join(err, restore())
			}
			if err = os.Remove(backup); err != nil {
				return nil, errors.Join(err, restore())
			}
			if err = os.Rename(path, backup); err != nil {
				return nil, errors.Join(err, restore())
			}
			parked = append(parked, parkedFile{original: path, backup: backup})
		}
	}
	files.parked = parked
	return files, nil
}
func (f *retryFiles) Restore() error {
	var errs []error
	for i := len(f.parked) - 1; i >= 0; i-- {
		p := f.parked[i]
		errs = append(errs, os.Rename(p.backup, p.original))
	}
	return errors.Join(errs...)
}
func (f *retryFiles) Commit() error {
	var errs []error
	for _, p := range f.parked {
		errs = append(errs, os.Remove(p.backup))
	}
	return errors.Join(errs...)
}
