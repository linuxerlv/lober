package iso

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/lober/lober/internal/core/catalog"
)

var leapProduct = regexp.MustCompile(`^/ (openSUSE-Leap|Leap) ([0-9]+\.[0-9]+)-[0-9]+$`)

// identifySUSELayout handles the two verified official Leap DVD layouts. The
// historical root layout without .treeinfo still follows catalog markers.
// Repository metadata stays separate from the boot/live filesystem at ISO root.
func identifySUSELayout(ctx context.Context, tree fs.FS) (Identified, bool, error) {
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
	if !identifyVersion.MatchString(version) || general["family"] != "openSUSE Leap" || general["name"] != "openSUSE Leap "+version {
		return invalid()
	}
	products, err := identifyText(ctx, tree, path.Join(repo, "media.1/products"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return invalid()
		}
		return out, true, err
	}
	product := leapProduct.FindStringSubmatch(strings.TrimSpace(products))
	if product == nil || product[2] != version {
		return invalid()
	}
	arch := identifyArch(general["arch"])
	if !arch.Valid() {
		return invalid()
	}
	for _, candidate := range []catalog.Arch{"x86_64", "aarch64"} {
		st, err := fs.Stat(tree, "boot/"+string(candidate)+"/loader")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return out, true, err
		}
		if candidate == arch && (err != nil || !st.IsDir()) || candidate != arch && err == nil {
			return invalid()
		}
	}
	st, err := fs.Stat(tree, "LiveOS/squashfs.img")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return out, true, err
	}
	live := err == nil && st.Mode().IsRegular() && st.Size() > 0
	switch {
	case strings.HasPrefix(version, "15.") && repo == "." && !live && product[1] == "openSUSE-Leap":
		out.Format, out.Series = "autoyast", "leap15"
	case strings.HasPrefix(version, "16.") && live:
		out.Format, out.Series = "agama", "leap16"
	default:
		return invalid()
	}
	for _, required := range catalog.RepoPaths(out.Series) {
		st, err := fs.Stat(tree, path.Join(repo, required))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return out, true, err
		}
		if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return invalid()
		}
	}
	out.Version, out.Arch, out.repoRoot = version, arch, repo
	return out, true, nil
}
