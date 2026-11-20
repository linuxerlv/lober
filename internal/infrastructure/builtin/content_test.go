package builtin

import (
	"github.com/lober/lober/internal/domain/catalog"
	"testing"
)

func TestCurrentManifestSupportsOnlyFiveLinuxSeries(t *testing.T) {
	want := map[catalog.Series]bool{"rhel": false, "almalinux": false, "rocky": false, "ubuntu": false, "debian": false}
	for _, tpl := range Templates() {
		if tpl.Family != "linux" {
			continue
		}
		for _, s := range tpl.Series {
			if _, ok := want[s]; !ok {
				t.Errorf("template %s still offers unsupported Linux %s", tpl.ID, s)
			} else {
				want[s] = true
			}
		}
	}
	for s, found := range want {
		if !found {
			t.Errorf("no current template supports %s", s)
		}
	}
	for _, h := range Hooks() {
		for _, impl := range h.Impls {
			if impl.Family != "linux" {
				continue
			}
			for _, s := range impl.Series {
				if _, ok := want[s]; !ok {
					t.Errorf("hook %s still supports unsupported Linux %s", h.ID, s)
				}
			}
		}
	}
}

func TestCurrentManifestDoesNotShipSUSETemplates(t *testing.T) {
	for _, tpl := range Templates() {
		if tpl.ID == "builtin-agama" || tpl.ID == "builtin-autoyast" {
			t.Errorf("retired template %s remains available", tpl.ID)
		}
	}
}

func TestCompleteInstallerManifest(t *testing.T) {
	want := map[catalog.Format]bool{"kickstart": false, "preseed": false, "autoinstall": false, "unattend": false, "esxi-ks": false}
	for _, tpl := range Templates() {
		f, ok := tpl.Format()
		if !ok {
			t.Errorf("%s has no format", tpl.ID)
			continue
		}
		if _, ok := want[f]; !ok {
			t.Errorf("unexpected current installer %s", f)
		}
		want[f] = true
		used := map[string]bool{}
		for _, instances := range tpl.Assembly {
			for _, in := range instances {
				used[in.HookID] = true
			}
		}
		for _, id := range []string{"partition", "packages", "network", "account", "sshkey"} {
			if !used["builtin-"+id] {
				t.Errorf("%s does not assemble %s", tpl.ID, id)
			}
		}
	}
	for f, found := range want {
		if !found {
			t.Errorf("missing installer %s", f)
		}
	}
	if len(Templates()) != 6 || len(Hooks()) != 5 {
		t.Errorf("manifest = %d templates / %d hooks; want 6 / 5", len(Templates()), len(Hooks()))
	}
}
