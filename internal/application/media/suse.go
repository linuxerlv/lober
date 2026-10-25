package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	mediarules "github.com/lober/lober/internal/domain/media"
	"io/fs"
	"path"
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
)

// identifySUSELayout handles the two verified official Leap DVD layouts. The
// historical root layout without .treeinfo still follows catalog markers.
// Repository metadata stays separate from the boot/live filesystem at ISO root.
func identifySUSELayout(ctx context.Context, tree port.MediaView) (Identified, bool, error) {
	var out Identified
	repo := "."
	general, err := identifyGeneral(ctx, tree)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return out, false, err
	}
	rootPresent := err == nil
	nested, nestedErr := identifyGeneralAt(ctx, tree, "install/.treeinfo")
	if nestedErr != nil && !errors.Is(nestedErr, fs.ErrNotExist) {
		return out, false, nestedErr
	}
	if nestedErr == nil {
		if rootPresent {
			return out, true, identifyProblem("iso_media_unrecognized", "media")
		}
		general, repo = nested, "install"
	} else if !rootPresent || !strings.HasPrefix(general["name"], "openSUSE Leap") {
		return out, false, nil
	}
	invalid := func() (Identified, bool, error) {
		return out, true, identifyProblem("iso_media_unrecognized", "media")
	}
	version := general["version"]
	if !mediarules.SUSEHeader(general) {
		return invalid()
	}
	products, err := identifyText(ctx, tree, path.Join(repo, "media.1/products"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return invalid()
		}
		return out, true, err
	}
	product, ok := mediarules.SUSEProduct(products, version)
	if !ok {
		return invalid()
	}
	arch := mediarules.Arch(general["arch"])
	if !arch.Valid() {
		return invalid()
	}
	for _, candidate := range []catalog.Arch{"x86_64", "aarch64"} {
		st, err := tree.Stat(ctx, "boot/"+string(candidate)+"/loader")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return out, true, err
		}
		if candidate == arch && (err != nil || !st.IsDir) || candidate != arch && err == nil {
			return invalid()
		}
	}
	st, err := tree.Stat(ctx, "LiveOS/squashfs.img")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return out, true, err
	}
	live := err == nil && st.IsRegular && st.Size > 0
	out.Format, out.Series, ok = mediarules.SUSEFormat(version, repo, live, product)
	if !ok {
		return invalid()
	}
	for _, required := range catalog.RepoPaths(out.Series) {
		st, err := tree.Stat(ctx, path.Join(repo, required))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return out, true, err
		}
		if err != nil || !st.IsRegular || st.Size == 0 {
			return invalid()
		}
	}
	out.Version, out.Arch, out.RepoRoot = version, arch, repo
	return out, true, nil
}
