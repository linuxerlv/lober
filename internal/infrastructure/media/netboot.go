package iso

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
)

// Official mini.iso lacks dists/: it is an auxiliary network boot medium,
// not a repository-bearing preseed main ISO. Debian's build config writes
// .disk/info as Debian GNU/Linux <version> <arch> - netboot mini.iso <date>.
// https://sources.debian.org/src/debian-installer/20150422/build/config/x86.cfg/
var netbootInfo = regexp.MustCompile(`^Debian GNU/Linux ([0-9A-Za-z][0-9A-Za-z._-]{0,31})(?:\s|$)`)

// The official trixie mini.iso labels its release as "13", while the build
// suffix binds the exact point release (deb13u7 -> 13.7.0). The base build is
// pinned as well: a date-only development build is not evidence of a release.
var trixieNetbootInfo = regexp.MustCompile(`^Debian GNU/Linux 13 \(trixie\) (amd64|arm64) - netboot mini\.iso 20250803(?:\+deb13u([1-9][0-9]*))?$`)
var netbootUpdate = regexp.MustCompile(`\+deb([1-9][0-9]*)u([1-9][0-9]*)$`)

func identifyNetboot(ctx context.Context, tree fs.FS) (Identified, error) {
	text, err := identifyText(ctx, tree, ".disk/info")
	if err != nil {
		return Identified{}, err
	}
	m := netbootInfo.FindStringSubmatch(strings.TrimSpace(text))
	if len(m) != 2 || !strings.Contains(strings.ToLower(text), "netboot") {
		return Identified{}, fmt.Errorf("missing Debian netboot media metadata")
	}
	version := m[1]
	if !strings.Contains(version, ".") {
		build := trixieNetbootInfo.FindStringSubmatch(strings.TrimSpace(text))
		if build == nil {
			return Identified{}, fmt.Errorf("unqualified Debian netboot release")
		}
		update := "0"
		if build[2] != "" {
			// Bound numeric parsing and reject noncanonical/oversized versions.
			if _, err := strconv.ParseUint(build[2], 10, 32); err != nil {
				return Identified{}, fmt.Errorf("invalid Debian netboot update")
			}
			update = build[2]
		}
		version = "13." + update + ".0"
	} else if strings.Contains(text, "+deb") {
		build := netbootUpdate.FindStringSubmatch(strings.TrimSpace(text))
		if build == nil || version != build[1]+"."+build[2]+".0" {
			return Identified{}, fmt.Errorf("conflicting Debian netboot update")
		}
	}
	out := Identified{Format: "preseed", Series: "debian", Version: version}
	for _, field := range strings.Fields(text) {
		arch := identifyArch(field)
		if arch != "" {
			if out.Arch != "" && out.Arch != arch {
				return Identified{}, fmt.Errorf("mixed netboot architectures")
			}
			out.Arch = arch
		}
	}
	if !out.Arch.Valid() {
		return Identified{}, fmt.Errorf("missing netboot architecture")
	}
	return out, nil
}
