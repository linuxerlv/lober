package iso

import (
	"context"
	"fmt"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type DeletedFiles struct{ dirs Dirs }

func NewDeletedFiles(d Dirs) *DeletedFiles { return &DeletedFiles{dirs: d} }
func (s *DeletedFiles) finalPath(id, role string) string {
	if role != "" {
		id += "." + role
	}
	return filepath.Join(s.dirs.Staging, id)
}

// Cleanup 只接收应用已检查的快照，所有删除仍受根目录及拥有者检查约束。
func (s *DeletedFiles) Cleanup(ctx context.Context, snapshot port.DeletionSnapshot) error {
	item := snapshot.ISO
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
			return &port.Fault{Kind: port.PublicationConflict}
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
		return &port.Fault{Kind: port.PublicationConflict}
	}
	f, err := root.Open(filepath.Join(rel, ".lober-owner"))
	if err != nil {
		// 未完成的临时目录可能还没写入拥有者标记。
		if !temp || !os.IsNotExist(err) {
			return fmt.Errorf("%w: publication ownership unavailable", &port.Fault{Kind: port.PublicationConflict})
		}
	} else {
		b, readErr := io.ReadAll(io.LimitReader(f, 256))
		f.Close()
		if readErr != nil {
			return readErr
		}
		if string(b) != owner {
			return fmt.Errorf("%w: publication belongs to other content", &port.Fault{Kind: port.PublicationConflict})
		}
	}
	return root.RemoveAll(rel)
}
