package iso

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// Official mini.iso lacks dists/: it is an auxiliary network boot medium,
// not a repository-bearing preseed main ISO. Debian's build config writes
// .disk/info as Debian GNU/Linux <version> <arch> - netboot mini.iso <date>.
// https://sources.debian.org/src/debian-installer/20150422/build/config/x86.cfg/
var netbootInfo = regexp.MustCompile(`^Debian GNU/Linux ([0-9A-Za-z][0-9A-Za-z._-]{0,31})(?:\s|$)`)

func identifyNetboot(ctx context.Context, tree fs.FS) (Identified, error) {
	text, err := identifyText(ctx, tree, ".disk/info")
	if err != nil {
		return Identified{}, err
	}
	m := netbootInfo.FindStringSubmatch(strings.TrimSpace(text))
	if len(m) != 2 || !strings.Contains(strings.ToLower(text), "netboot") {
		return Identified{}, fmt.Errorf("missing Debian netboot media metadata")
	}
	out := Identified{Format: "preseed", Series: "debian", Version: m[1]}
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
