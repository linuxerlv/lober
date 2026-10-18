package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/params"
)

// Exercise debconf's real selection reader: db_set alone leaves seen=false,
// causing choose-mirror to ask an interactive question despite a preset value.
func TestPreseedMirrorSelectionsAreSeenAndAuthenticated(t *testing.T) {
	for _, tool := range []string{"debconf-set-selections", "debconf-communicate"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("native Debian preseed reader unavailable: %s", tool)
		}
	}
	for _, tc := range []struct{ name, host, directory string }{
		{"official", "deb.debian.org", "/debian"},
		{"internal-signed", "mirror.example.test:8080", "/signed/debian"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tpl := templateFor(t, "preseed")
			in := fixtureInput(t, tpl)
			if tc.name != "official" {
				in.Entered[params.FieldKey("tpl/mirror_host")] = tc.host
				in.Entered[params.FieldKey("tpl/mirror_path")] = tc.directory
			}
			answer := renderFixture(t, withInput(t, fixtureRequest(t, tpl, "bios"), in))
			var selections []string
			for _, line := range strings.Split(answer, "\n") {
				if strings.HasPrefix(line, "d-i mirror/") || strings.HasPrefix(line, "d-i debian-installer/allow_unauthenticated ") {
					selections = append(selections, line)
				}
			}
			dir := t.TempDir()
			config := filepath.Join(dir, "debconf.conf")
			if err := os.WriteFile(config, []byte("Config: config\nTemplates: templates\n\nName: config\nDriver: File\nFilename: "+filepath.Join(dir, "config.dat")+"\n\nName: templates\nDriver: File\nFilename: "+filepath.Join(dir, "templates.dat")+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			env := append(os.Environ(), "DEBCONF_SYSTEMRC="+config, "DEBIAN_FRONTEND=noninteractive")
			load := exec.Command("debconf-set-selections")
			load.Env = env
			load.Stdin = strings.NewReader(strings.Join(selections, "\n") + "\n")
			if out, err := load.CombinedOutput(); err != nil {
				t.Fatalf("read rendered preseed: %v: %s", err, out)
			}
			for _, setting := range []struct{ key, value string }{
				{"mirror/country", "manual"}, {"mirror/protocol", "http"},
				{"mirror/http/hostname", tc.host}, {"mirror/http/directory", tc.directory},
				{"debian-installer/allow_unauthenticated", "false"},
			} {
				query := exec.Command("debconf-communicate", "d-i")
				query.Env = env
				query.Stdin = strings.NewReader("GET " + setting.key + "\nFGET " + setting.key + " seen\n")
				out, err := query.CombinedOutput()
				if err != nil || string(out) != "0 "+setting.value+"\n0 true\n" {
					t.Errorf("%s must be preset and seen: got %q (%v)", setting.key, out, err)
				}
			}
		})
	}
}
