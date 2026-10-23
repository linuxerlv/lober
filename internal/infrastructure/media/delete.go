package iso

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// DeletionSnapshot is captured before the caller's audited Store.DeleteISO.
// Sources must be a nonnil snapshot of SourcesOf, including an empty slice when
// no source exists. Source kinds preserve operator-owned local originals even
// when allowed roots overlap staging. The assembly point retains this snapshot
// until CleanupDeleted succeeds, and supplies its durable retry policy.
type DeletionSnapshot struct {
	ISO     model.ISO
	Sources []model.Source
}

// CleanupDeleted removes only managed bytes belonging to an already deleted
// ISO. It is idempotent and cannot delete while its database row still exists.
// It never performs the audited database deletion itself.
func (s *Sources) CleanupDeleted(ctx context.Context, snapshot DeletionSnapshot) error {
	item := snapshot.ISO
	if err := repository.CheckID(item.ID); err != nil {
		return err
	}
	// Acquiring/failed records may legitimately lack identification metadata.
	// Validate any supplied path components without requiring readiness.
	checked := item
	if checked.Version == "" {
		checked.Version = "pending"
	}
	if checked.Arch == "" {
		checked.Arch = "x86_64"
	}
	if snapshot.Sources == nil || len(checked.Validate()) > 0 {
		return repository.ErrInvalid
	}
	seen := map[string]bool{}
	for _, source := range snapshot.Sources {
		if source.ISOID != item.ID || seen[source.Role] {
			return repository.ErrInvalid
		}
		if err := repository.CheckSource(source); err != nil {
			return err
		}
		seen[source.Role] = true
	}
	if err := s.confirmDeleted(ctx, item.ID); err != nil {
		return err
	}
	unlock := s.lockSource(item.ID, "@import")
	defer unlock()
	for _, source := range snapshot.Sources {
		s.locks.Cancel(item.ID, source.Role)
		unlock := s.lockSource(item.ID, source.Role)
		defer unlock()
	}
	if err := s.confirmDeleted(ctx, item.ID); err != nil {
		return err
	}
	if item.Version != "" && item.Arch != "" {
		rel := filepath.Join(string(item.Series), item.Version, string(item.Arch))
		owner := item.ID + "\n" + item.SHA256 + "\n"
		bases := []string{s.dirs.Artifacts}
		if item.Family == "windows" {
			bases = append(bases, s.dirs.SMB)
		}
		for _, base := range bases {
			if err := cleanupOwnedTree(ctx, base, rel, owner, false); err != nil {
				return err
			}
			if err := cleanupOwnedTree(ctx, base, rel+".tmp-"+item.ID, owner, true); err != nil {
				return err
			}
		}
	}
	root, err := os.OpenRoot(s.dirs.Staging)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	localPaths := map[string]bool{}
	for _, source := range snapshot.Sources {
		if source.Kind == model.SourceLocal {
			localPaths[filepath.Clean(source.Path)] = true
		}
	}
	remove := func(name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := resolvedPath(filepath.Join(s.dirs.Staging, name))
		if err != nil {
			return err
		}
		if localPaths[filepath.Clean(p)] {
			return nil
		}
		st, err := root.Lstat(name)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() && st.Mode()&fs.ModeSymlink == 0 {
			return repository.ErrConflict
		}
		return root.Remove(name)
	}
	for _, source := range snapshot.Sources {
		if source.Kind == model.SourceLocal {
			continue
		}
		name := filepath.Base(s.finalPath(item.ID, source.Role))
		for _, name := range []string{name, name + ".part"} {
			if err := remove(name); err != nil {
				return err
			}
		}
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".chunk-"+item.ID+"-") || strings.HasPrefix(name, ".retry-"+item.ID+"-") {
			if err := remove(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Sources) confirmDeleted(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.store.GetISO(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return repository.ErrConflict
}

func cleanupOwnedTree(ctx context.Context, base, rel, owner string, temp bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(base)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	st, err := root.Lstat(rel)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&fs.ModeSymlink != 0 {
		return repository.ErrConflict
	}
	f, err := root.Open(filepath.Join(rel, ".lober-owner"))
	if err != nil {
		// A partial temporary tree has not reached the ownership-marker step yet.
		if !temp || !os.IsNotExist(err) {
			return fmt.Errorf("%w: publication ownership unavailable", repository.ErrConflict)
		}
	} else {
		b, readErr := io.ReadAll(io.LimitReader(f, 256))
		f.Close()
		if readErr != nil {
			return readErr
		}
		if string(b) != owner {
			return fmt.Errorf("%w: publication belongs to other content", repository.ErrConflict)
		}
	}
	return root.RemoveAll(rel)
}
