package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
)

func TestLinuxInstallerAcknowledgesRebootBoundary(t *testing.T) {
	for _, format := range []catalog.Format{"kickstart", "preseed", "autoinstall", "autoyast"} {
		t.Run(string(format), func(t *testing.T) {
			r := answerFixture(t, format, true, "bios")
			body, p, err := Render(r)
			if err != nil || len(p) != 0 {
				t.Fatal(p, err)
			}
			found := false
			for _, payload := range commandPayloads(string(body)) {
				if strings.Contains(payload, "/installed'") {
					found = true
					if !strings.Contains(payload, "/task-42/install-tok-fixed/installed") {
						t.Fatal("reboot marker uses wrong credential")
					}
					for _, line := range strings.Split(payload, "\n") {
						if strings.Contains(line, "/installed'") && strings.Contains(line, "|| :") {
							t.Fatal("reboot marker silently ignores server rejection")
						}
					}
				}
			}
			if !found {
				t.Fatal("installer never reports irreversible reboot boundary")
			}
		})
	}
}
