package builtin

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"testing"
)

func TestAgamaDefaultTimezoneIsSupportedByLeap16Installer(t *testing.T) {
	data, err := os.ReadFile("testdata/agama-leap16-timezones.json")
	if err != nil {
		t.Fatal(err)
	}
	var observed struct {
		Count     int      `json:"count"`
		Timezones []string `json:"timezones"`
	}
	if err := json.Unmarshal(data, &observed); err != nil {
		t.Fatal(err)
	}
	if observed.Count != 400 || len(observed.Timezones) != observed.Count {
		t.Fatal("incomplete installer timezone catalog")
	}
	answer := renderFixture(t, fixtureRequest(t, templateFor(t, "agama"), "uefi"))
	// 内置配置在这一行直接填字符串；从实际交付内容读取，不用主机的时区库代替安装器。
	localization := regexp.MustCompile(`(?m)^\s*localization:\s*\{([^}\n]*)\}`).FindStringSubmatch(answer)
	if len(localization) != 2 {
		t.Fatal("rendered profile has no literal localization object")
	}
	timezone := regexp.MustCompile(`\btimezone:\s*'([^']+)'`).FindStringSubmatch(localization[1])
	if len(timezone) != 2 {
		t.Fatal("rendered localization has no literal timezone")
	}
	if !slices.Contains(observed.Timezones, timezone[1]) {
		t.Fatalf("rendered timezone %q is not supported by the Leap 16.0 installer (%d observed timezones)", timezone[1], observed.Count)
	}
}
