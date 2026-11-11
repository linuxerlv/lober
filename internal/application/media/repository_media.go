package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
	mediarules "github.com/lober/lober/internal/domain/media"
	"io/fs"
	"path"
	"strings"
)

func validateDebianRepository(ctx context.Context, arch catalog.Arch, tree port.MediaView) error {
	debArch := mediarules.DebianArch(arch)
	nonempty := func(name string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		st, err := tree.Stat(ctx, name)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return st.IsRegular && st.Size > 0, nil
	}
	suites, err := tree.Entries(ctx, "dists")
	if err != nil {
		return err
	}
	validIndex := false
	for _, suite := range suites {
		if !suite.IsDir {
			continue
		}
		root := path.Join("dists", suite.Name)
		ok, err := nonempty(path.Join(root, "Release"))
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		for _, suffix := range []string{"", ".gz", ".xz", ".bz2"} {
			ok, err = nonempty(path.Join(root, "main", "binary-"+debArch, "Packages"+suffix))
			if err != nil {
				return err
			}
			if ok {
				validIndex = true
				break
			}
		}
		if validIndex {
			break
		}
	}
	hasPackage := false
	var walk func(string, port.Entry) error
	walk = func(name string, entry port.Entry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir && strings.HasSuffix(name, ".deb") {
			ok, err := nonempty(name)
			if err != nil {
				return err
			}
			if ok {
				hasPackage = true
				return fs.SkipAll
			}
		}
		if entry.IsDir {
			children, err := tree.Entries(ctx, name)
			if err != nil {
				return err
			}
			for _, child := range children {
				if err := walk(path.Join(name, child.Name), child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	pool, err := tree.Stat(ctx, "pool")
	if err == nil {
		err = walk("pool", pool)
	}
	if errors.Is(err, fs.SkipAll) {
		err = nil
	}

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return mediarules.CheckDebianRepository(validIndex, hasPackage, arch)
}
