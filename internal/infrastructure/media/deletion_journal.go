package iso

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/media/port"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type DeletionJournal struct{ dir string }

func NewDeletionJournal(staging string) (*DeletionJournal, error) {
	dir := filepath.Join(staging, "deletions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &DeletionJournal{dir: dir}, nil
}

var journalIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func journalID(id string) error {
	if !journalIDPattern.MatchString(id) {
		return &port.Fault{Kind: port.InvalidFile}
	}
	return nil
}
func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func (j *DeletionJournal) Persist(ctx context.Context, s port.DeletionSnapshot) error {
	if err := journalID(s.ISO.ID); err != nil {
		return err
	}
	if s.Sources == nil {
		return &port.Fault{Kind: port.InvalidFile}
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(j.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	name := "." + s.ISO.ID + ".tmp"
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		if err = root.Remove(name); err != nil {
			return err
		}
		f, err = root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	}
	if err != nil {
		return err
	}
	defer root.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = root.Rename(name, s.ISO.ID+".json"); err != nil {
		return err
	}
	return syncDirectory(j.dir)
}
func (j *DeletionJournal) Discard(ctx context.Context, id string) error {
	if err := journalID(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(j.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(id + ".json"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(j.dir)
}

func (j *DeletionJournal) List(ctx context.Context) ([]port.DeletionSnapshot, error) {
	root, err := os.OpenRoot(j.dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, err
	}
	var errs []error
	var snapshots []port.DeletionSnapshot
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return snapshots, errors.Join(append(errs, err)...)
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if err := journalID(id); err != nil {
			errs = append(errs, err)
			continue
		}
		info, err := root.Lstat(entry.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.Mode().IsRegular() {
			errs = append(errs, &port.Fault{Kind: port.InvalidFile})
			continue
		}
		f, err := root.Open(entry.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var snapshot port.DeletionSnapshot
		decoder := json.NewDecoder(f)
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&snapshot)
		var extra any
		if err == nil {
			if endErr := decoder.Decode(&extra); endErr != io.EOF {
				err = &port.Fault{Kind: port.InvalidFile}
			}
		}
		f.Close()
		if err != nil || snapshot.ISO.ID != id || snapshot.Sources == nil {
			errs = append(errs, fmt.Errorf("invalid deletion snapshot %s", id))
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, errors.Join(errs...)
}
