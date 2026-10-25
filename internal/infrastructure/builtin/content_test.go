package builtin

import (
	"github.com/lober/lober/internal/domain/catalog"
	"testing"
)

func TestCompleteInstallerManifest(t *testing.T) {
	want := map[catalog.Format]bool{"kickstart": false, "preseed": false, "autoinstall": false, "autoyast": false, "agama": false, "unattend": false, "esxi-ks": false}
	for _, tpl := range Templates() {
		f, ok := tpl.Format()
		if !ok {
			t.Errorf("%s has no format", tpl.ID)
			continue
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
	if len(Templates()) != 7 || len(Hooks()) != 5 {
		t.Errorf("manifest = %d templates / %d hooks; want 7 / 5", len(Templates()), len(Hooks()))
	}
}
