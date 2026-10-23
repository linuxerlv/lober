package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
)

type Identified = mediarules.Identified

const identifyTextLimit = 1 << 20

func identifyProblem(code model.Code, path string, args ...any) error {
	return mediarules.Problem(code, path, args...)
}

// Identify executes catalog media markers in their declared priority order.
// Context cancellation and filesystem IO errors remain available to the caller.
func Identify(ctx context.Context, fsys port.MediaView) (Identified, error) {
	var out Identified
	if ctx == nil || fsys == nil {
		return out, fmt.Errorf("Identify: context and filesystem are required")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	// Real SUSE DVDs also carry .treeinfo. Resolve their product metadata before
	// the generic Anaconda marker, including Agama's nested install/ repository.
	if suse, matched, err := identifySUSELayout(ctx, fsys); matched || err != nil {
		return suse, err
	}
	for _, f := range catalog.Formats() {
		media, _ := catalog.FormatMedia(f)
		ok, err := identifyMarkers(ctx, fsys, media.Markers)
		if err != nil {
			return out, err
		}
		if ok {
			out.Format = f
			break
		}
	}
	if out.Format == "" {
		return out, identifyProblem("iso_media_unrecognized", "media")
	}
	var images []mediarules.EditionFact
	if out.Format == "unattend" {
		var err error
		images, err = fsys.WIM(ctx, "sources/install.wim")
		if err != nil {
			return out, importFileError("", err)
		}
		names, flags := make([]string, len(images)), make([]string, len(images))
		for i, image := range images {
			names[i], flags[i] = image.Name, image.Flags
		}
		out.Series, _ = catalog.SeriesByEdition(out.Format, names, flags)
	} else {
		for _, s := range catalog.SeriesOfFormat(out.Format) {
			media, _ := catalog.SeriesMedia(s)
			markers := media.Markers
			// Kickstart product markers belong to [general]. A different INI
			// section mentioning an old product does not identify this media.
			if media.Probe == catalog.ProbeTreeinfo {
				general, err := identifyGeneral(ctx, fsys)
				if err != nil {
					return out, err
				}
				match := mediarules.GeneralMatches(general, markers)
				if match {
					out.Series = s
					break
				}
				continue
			}
			ok, err := identifyMarkers(ctx, fsys, markers)
			if err != nil {
				return out, err
			}
			if ok {
				out.Series = s
				break
			}
		}
	}
	if out.Series == "" {
		return out, mediarules.UnknownSeries(out.Format)
	}
	media, _ := catalog.SeriesMedia(out.Series)
	switch media.Probe {
	case catalog.ProbeTreeinfo:
		general, err := identifyGeneral(ctx, fsys)
		if err != nil {
			return out, err
		}
		out.Version, out.Arch = general["version"], mediarules.Arch(general["arch"])
	case catalog.ProbeDiskInfo:
		text, err := identifyText(ctx, fsys, ".disk/info")
		if err != nil {
			return out, err
		}
		out.Version, out.Arch = mediarules.DiskInfo(text)
	case catalog.ProbeProducts:
		text, err := identifyText(ctx, fsys, "media.1/products")
		if err != nil {
			return out, err
		}
		out.Version = mediarules.ProductVersion(text)
		for _, arch := range []catalog.Arch{"x86_64", "aarch64"} {
			info, err := fsys.Stat(ctx, "boot/"+string(arch)+"/loader")
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return out, err
			}
			if err == nil && info.IsDir {
				if out.Arch != "" {
					out.Arch = ""
					break
				}
				out.Arch = arch
			}
		}
	case catalog.ProbeBootCfg:
		out.Arch = "x86_64" // The catalog marker guarantees bootx64.efi.
		// No verified version source exists for ESXi; never infer one from title.
	case catalog.ProbeWIM:
		var err error
		out, err = mediarules.CheckEditions(out, images)
		if err != nil {
			return out, err
		}
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := mediarules.CheckIdentified(out); err != nil {
		return out, err
	}
	return out, nil
}

func identifyMarkers(ctx context.Context, fsys port.MediaView, markers []catalog.Marker) (bool, error) {
	for _, marker := range markers {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		name := strings.TrimSuffix(marker.Path, "/")
		info, err := fsys.Stat(ctx, name)
		if errors.Is(err, fs.ErrNotExist) {
			if marker.Absent {
				continue
			}
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if marker.Absent || strings.HasSuffix(marker.Path, "/") && !info.IsDir {
			return false, nil
		}
		if len(marker.Contains) > 0 {
			if info.IsDir {
				return false, nil
			}
			text, err := identifyText(ctx, fsys, name)
			if err != nil {
				return false, err
			}
			if !mediarules.TextMatches(text, marker.Contains) {
				return false, nil
			}
		}
	}
	return true, nil
}

func identifyText(ctx context.Context, fsys port.MediaView, name string) (string, error) {
	text, err := fsys.Text(ctx, name, identifyTextLimit)
	return text, importFileError("", err)
}

func identifyGeneral(ctx context.Context, fsys port.MediaView) (map[string]string, error) {
	return identifyGeneralAt(ctx, fsys, ".treeinfo")
}

func identifyGeneralAt(ctx context.Context, fsys port.MediaView, name string) (map[string]string, error) {
	text, err := identifyText(ctx, fsys, name)
	if err != nil {
		return nil, err
	}
	return mediarules.General(text), nil
}
