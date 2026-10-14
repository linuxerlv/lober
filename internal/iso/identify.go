package iso

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// Identified is the metadata read from installation media, never from its filename.
type Identified struct {
	Format   catalog.Format
	Series   catalog.Series
	Version  string
	Arch     catalog.Arch
	Editions []model.Option
}

const identifyTextLimit = 1 << 20

var identifyVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$`)
var identifyDottedVersion = regexp.MustCompile(`\b[0-9]+(?:\.[0-9]+)+\b`)

func identifyProblem(code model.Code, path string, args ...any) error {
	return model.NewProblem(code, model.Subject{Kind: "iso", Path: path}, args...)
}

// Identify executes catalog media markers in their declared priority order.
// Context cancellation and filesystem IO errors remain available to the caller.
func Identify(ctx context.Context, fsys fs.FS) (Identified, error) {
	var out Identified
	if ctx == nil || fsys == nil {
		return out, fmt.Errorf("Identify: context and filesystem are required")
	}
	if err := ctx.Err(); err != nil {
		return out, err
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
	var images []WIMImage
	if out.Format == "unattend" {
		var err error
		images, err = identifyWIM(ctx, fsys)
		if err != nil {
			return out, err
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
				match := true
				for _, marker := range markers {
					for _, hint := range marker.Contains {
						match = match && strings.Contains(general["name"], hint)
					}
				}
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
		return out, identifyProblem("iso_series_unrecognized", "series", fmt.Sprintf("%s (%v)", out.Format, catalog.SeriesOfFormat(out.Format)))
	}
	media, _ := catalog.SeriesMedia(out.Series)
	switch media.Probe {
	case catalog.ProbeTreeinfo:
		general, err := identifyGeneral(ctx, fsys)
		if err != nil {
			return out, err
		}
		out.Version, out.Arch = general["version"], identifyArch(general["arch"])
	case catalog.ProbeDiskInfo:
		text, err := identifyText(ctx, fsys, ".disk/info")
		if err != nil {
			return out, err
		}
		text, _, _ = strings.Cut(text, "\n")
		out.Version = identifyDottedVersion.FindString(text)
		for _, field := range strings.Fields(text) {
			if arch := identifyArch(field); arch != "" {
				if out.Arch != "" && out.Arch != arch {
					out.Arch = ""
					break
				}
				out.Arch = arch
			}
		}
	case catalog.ProbeProducts:
		text, err := identifyText(ctx, fsys, "media.1/products")
		if err != nil {
			return out, err
		}
		out.Version = identifyDottedVersion.FindString(text)
		for _, arch := range []catalog.Arch{"x86_64", "aarch64"} {
			info, err := fs.Stat(fsys, "boot/"+string(arch)+"/loader")
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return out, err
			}
			if err == nil && info.IsDir() {
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
		seen := map[string]bool{}
		for i, image := range images {
			if image.Version == "" {
				return out, identifyProblem("iso_version_missing", "version", model.ISOVersionLabel)
			}
			if !image.Arch.Valid() {
				return out, identifyProblem("iso_version_missing", "arch", model.ISOArchLabel)
			}
			if i == 0 {
				out.Version = image.Version
				out.Arch = image.Arch
			}
			if image.Version != out.Version {
				return out, identifyProblem("iso_version_missing", "version", model.ISOVersionLabel)
			}
			if image.Arch != out.Arch {
				return out, identifyProblem("iso_version_missing", "arch", model.ISOArchLabel)
			}
			if !model.IsSafeWord(image.Flags) || !model.ValidLabel(image.Name) {
				return out, identifyProblem("iso_version_missing", "offers/edition", model.ISOVersionLabel)
			}
			if !seen[image.Flags] {
				out.Editions = append(out.Editions, model.Option{Value: image.Flags, Label: image.Name})
				seen[image.Flags] = true
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if !identifyVersion.MatchString(out.Version) {
		return out, identifyProblem("iso_version_missing", "version", model.ISOVersionLabel)
	}
	if !out.Arch.Valid() {
		return out, identifyProblem("iso_version_missing", "arch", model.ISOArchLabel)
	}
	return out, nil
}

func identifyArch(value string) catalog.Arch {
	switch value {
	case "x86_64", "amd64":
		return "x86_64"
	case "aarch64", "arm64":
		return "aarch64"
	}
	return ""
}

func identifyMarkers(ctx context.Context, fsys fs.FS, markers []catalog.Marker) (bool, error) {
	for _, marker := range markers {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		name := strings.TrimSuffix(marker.Path, "/")
		info, err := fs.Stat(fsys, name)
		if errors.Is(err, fs.ErrNotExist) {
			if marker.Absent {
				continue
			}
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if marker.Absent || strings.HasSuffix(marker.Path, "/") && !info.IsDir() {
			return false, nil
		}
		if len(marker.Contains) > 0 {
			if info.IsDir() {
				return false, nil
			}
			text, err := identifyText(ctx, fsys, name)
			if err != nil {
				return false, err
			}
			for _, hint := range marker.Contains {
				if !strings.Contains(text, hint) {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

func identifyText(ctx context.Context, fsys fs.FS, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := fsys.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, identifyTextLimit+1))
	if err != nil {
		return "", err
	}
	if len(b) > identifyTextLimit {
		return "", identifyProblem("iso_version_missing", "version", model.ISOVersionLabel)
	}
	return strings.TrimPrefix(string(b), "\ufeff"), ctx.Err()
}

func identifyGeneral(ctx context.Context, fsys fs.FS) (map[string]string, error) {
	text, err := identifyText(ctx, fsys, ".treeinfo")
	if err != nil {
		return nil, err
	}
	general := map[string]string{}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if section == "general" {
			if key, value, ok := strings.Cut(line, "="); ok {
				general[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
			}
		}
	}
	return general, nil
}

func identifyWIM(ctx context.Context, fsys fs.FS) ([]WIMImage, error) {
	f, err := fsys.Open("sources/install.wim")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var r io.ReaderAt
	if at, ok := f.(io.ReaderAt); ok {
		r = at
	} else {
		r = &identifyForwardReader{ctx: ctx, r: f}
	}
	return WIMImages(r, info.Size())
}

// WIM metadata requires only ascending reads: header followed by XML resource.
// This adapter keeps Identify compatible with fs.File implementations without
// Seek or ReaderAt, without buffering the many-gigabyte install.wim.
type identifyForwardReader struct {
	ctx    context.Context
	r      io.Reader
	offset int64
}

func (r *identifyForwardReader) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if off < r.offset {
		return 0, fmt.Errorf("WIM read offset moved backwards")
	}
	if off > r.offset {
		n, err := io.CopyN(io.Discard, identifyContextReader{ctx: r.ctx, r: r.r}, off-r.offset)
		r.offset += n
		if err != nil {
			return 0, err
		}
	}
	n, err := io.ReadFull(identifyContextReader{ctx: r.ctx, r: r.r}, p)
	r.offset += int64(n)
	return n, err
}

type identifyContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r identifyContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
