package media

import (
	"fmt"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"regexp"
	"strings"
)

type Identified struct {
	Format   catalog.Format
	Series   catalog.Series
	Version  string
	Arch     catalog.Arch
	Editions []model.Option
	RepoRoot string
}
type EditionFact struct {
	Name, Flags, Version string
	Arch                 catalog.Arch
}

var versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$`)
var dottedVersion = regexp.MustCompile(`\b[0-9]+(?:\.[0-9]+)+\b`)

func Problem(code model.Code, path string, args ...any) error {
	return model.NewProblem(code, model.Subject{Kind: "iso", Path: path}, args...)
}
func CheckIdentified(out Identified) error {
	if !versionPattern.MatchString(out.Version) {
		return Problem("iso_version_missing", "version", model.ISOVersionLabel)
	}
	if !out.Arch.Valid() {
		return Problem("iso_version_missing", "arch", model.ISOArchLabel)
	}
	return nil
}
func CheckImport(item model.ISO, found Identified, sources []model.Source) error {
	if item.Series != found.Series {
		return model.NewProblem("iso_series_mismatch", model.Subject{Kind: "iso", ID: item.ID, Path: "series"}, item.Series, found.Series)
	}
	if need, ok := catalog.ExtraSourceOf(item.Series); ok {
		for _, src := range sources {
			if src.Role == need.Role {
				return nil
			}
		}
		return model.NewProblem("iso_extra_source_invalid", model.Subject{Kind: "iso", ID: item.ID, Path: "source." + need.Role}, need.Role)
	}
	return nil
}
func Fetched(sources []model.Source) bool {
	main := false
	for _, src := range sources {
		if src.State != model.SourceFetched {
			return false
		}
		main = main || src.Role == ""
	}
	return main
}
func AllFetched(item model.ISO, sources []model.Source) bool {
	if !Fetched(sources) {
		return false
	}
	need, ok := catalog.ExtraSourceOf(item.Series)
	if !ok {
		return true
	}
	for _, src := range sources {
		if src.Role == need.Role {
			return true
		}
	}
	return false
}
func CheckExtra(found, extra Identified, sameVersion bool, text string) bool {
	return extra.Series == found.Series && extra.Arch == found.Arch && (!sameVersion || extra.Version == found.Version) && strings.Contains(strings.ToUpper(text), "NETBOOT")
}
func GeneralMatches(general map[string]string, markers []catalog.Marker) bool {
	for _, marker := range markers {
		for _, hint := range marker.Contains {
			if !strings.Contains(general["name"], hint) {
				return false
			}
		}
	}
	return true
}
func TextMatches(text string, hints []string) bool {
	for _, hint := range hints {
		if !strings.Contains(text, hint) {
			return false
		}
	}
	return true
}
func CheckEditions(out Identified, images []EditionFact) (Identified, error) {
	seen := map[string]bool{}
	for i, image := range images {
		if image.Version == "" {
			return out, Problem("iso_version_missing", "version", model.ISOVersionLabel)
		}
		if !image.Arch.Valid() {
			return out, Problem("iso_version_missing", "arch", model.ISOArchLabel)
		}
		if i == 0 {
			out.Version, out.Arch = image.Version, image.Arch
		}
		if image.Version != out.Version {
			return out, Problem("iso_version_missing", "version", model.ISOVersionLabel)
		}
		if image.Arch != out.Arch {
			return out, Problem("iso_version_missing", "arch", model.ISOArchLabel)
		}
		if !model.IsSafeWord(image.Flags) || !model.ValidLabel(image.Name) {
			return out, Problem("iso_version_missing", "offers/edition", model.ISOVersionLabel)
		}
		if !seen[image.Flags] {
			out.Editions = append(out.Editions, model.Option{Value: image.Flags, Label: image.Name})
			seen[image.Flags] = true
		}
	}
	return out, nil
}
func DebianArch(arch catalog.Arch) string {
	if arch == "aarch64" {
		return "arm64"
	}
	return "amd64"
}
func CheckDebianRepository(validIndex, hasPackage bool, arch catalog.Arch) error {
	if !validIndex || !hasPackage {
		return Problem("iso_repo_incomplete", "artifacts.repo", "dists/*/Release, main/binary-"+DebianArch(arch)+"/Packages, pool/*.deb")
	}
	return nil
}
func DiskInfo(text string) (string, catalog.Arch) {
	text, _, _ = strings.Cut(text, "\n")
	var out catalog.Arch
	for _, field := range strings.Fields(text) {
		if arch := Arch(field); arch != "" {
			if out != "" && out != arch {
				out = ""
				break
			}
			out = arch
		}
	}
	return dottedVersion.FindString(text), out
}
func UnknownSeries(format catalog.Format) error {
	return Problem("iso_series_unrecognized", "series", fmt.Sprintf("%s (%v)", format, catalog.SeriesOfFormat(format)))
}

func ProductVersion(text string) string { return dottedVersion.FindString(text) }
