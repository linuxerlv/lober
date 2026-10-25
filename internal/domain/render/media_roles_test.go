package render

import (
	"slices"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

// Capture the Layer 3 artifact contract independently of the catalog consumer.
// repo/iso are derived system values; esxi-tree is consumed by boot.cfg.
var layer3MediaRoles = map[catalog.Series][]string{
	"rhel":            {"kernel", "initrd", "stage2", "repo"},
	"rocky":           {"kernel", "initrd", "stage2", "repo"},
	"almalinux":       {"kernel", "initrd", "stage2", "repo"},
	"openeuler":       {"kernel", "initrd", "stage2", "repo"},
	"opencloudos":     {"kernel", "initrd", "stage2", "repo"},
	"debian":          {"kernel", "initrd", "repo"},
	"ubuntu":          {"kernel", "initrd", "iso"},
	"sles15":          {"kernel", "initrd", "repo"},
	"leap15":          {"kernel", "initrd", "repo"},
	"sles16":          {"kernel", "initrd", "live-root", "repo"},
	"leap16":          {"kernel", "initrd", "live-root", "repo"},
	"windows-server":  {"wimboot", "bcd", "boot-sdi", "boot-wim", "winpeshl", "winpe-busybox", "winpe-curl"},
	"windows-desktop": {"wimboot", "bcd", "boot-sdi", "boot-wim", "winpeshl", "winpe-busybox", "winpe-curl"},
	"esxi":            {"mboot", "esxi-tree"},
}

func TestCatalogRolesMatchLayer3MediaContract(t *testing.T) {
	for series, want := range layer3MediaRoles {
		if got := catalog.RequiredRoles(series); !slices.Equal(got, want) {
			t.Errorf("%s: roles %v, want %v", series, got, want)
		}
	}
}

func completeMediaFixture(series catalog.Series) Request {
	r := menuFixture(series, "uefi")
	for _, role := range layer3MediaRoles[series] {
		r.Snapshot.ISO.Artifacts[role] = "media/" + role
	}
	r.Values["finalize-token"] = "finalize-independent"
	r.Values["smb"] = "//server/install"
	return r
}

func TestRenderRejectsIncompleteCatalogMedia(t *testing.T) {
	for series, roles := range layer3MediaRoles {
		kinds := []catalog.FileKind{catalog.FileMenu}
		if series.Family() == "windows" {
			kinds = append(kinds, catalog.FileAux, catalog.FileScript)
		}
		for _, kind := range kinds {
			t.Run(string(series)+"/"+string(kind), func(t *testing.T) {
				r := completeMediaFixture(series)
				if kind == catalog.FileScript {
					r = runtimeFixture("firstboot", runtimeInstance("exit /b 0\r\n", true, "cmd", 3))
					r.Snapshot.Family, r.Snapshot.Series, r.Snapshot.Format = series.Family(), series, series.Format()
					for _, role := range roles {
						r.Snapshot.ISO.Artifacts[role] = "media/" + role
					}
				}
				r.Kind = kind
				if b, p, err := Render(r); err != nil || len(p) != 0 || len(b) == 0 {
					t.Fatalf("complete media: %v %v", p, err)
				}
				for _, role := range roles {
					t.Run(role, func(t *testing.T) {
						saved := r.Snapshot.ISO.Artifacts[role]
						delete(r.Snapshot.ISO.Artifacts, role)
						defer func() { r.Snapshot.ISO.Artifacts[role] = saved }()
						b, p, err := Render(r)
						if b != nil || err != nil || len(p) != 1 || p[0].Code != "render_artifact_missing" || p[0].Subject.Path != "artifacts/"+role {
							t.Fatalf("missing %s: bytes=%d problems=%v err=%v", role, len(b), p, err)
						}
					})
				}
			})
		}
	}
}

func TestWindowsMediaRolesRejectIncompatibleSnapshot(t *testing.T) {
	for _, kind := range []catalog.FileKind{catalog.FileAux, catalog.FileScript} {
		for _, series := range []catalog.Series{"", "unknown", "rocky"} {
			t.Run(string(kind)+"/"+string(series), func(t *testing.T) {
				r := completeMediaFixture("windows-server")
				if kind == catalog.FileScript {
					r = runtimeFixture("firstboot", runtimeInstance("exit /b 0\r\n", true, "cmd", 3))
					windowsRuntimeRoles(&r)
				}
				r.Kind = kind
				r.Snapshot.Series = series
				assertRenderProblem(t, r, "render_boot_snapshot_invalid")
			})
		}
	}
}
