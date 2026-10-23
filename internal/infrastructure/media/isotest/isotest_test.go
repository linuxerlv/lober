package isotest_test

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/infrastructure/media/iso9660"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

func TestImageNestedNamesAndContent(t *testing.T) {
	files := map[string][]byte{".treeinfo": []byte("[general]\n"), ".disk/info": []byte("Debian 13"), "Mixed/Boot.Bin": []byte("kernel")}
	b := isotest.Image(files)
	if !bytes.Equal(b, isotest.Image(files)) {
		t.Fatal("image generation is not deterministic")
	}
	f, err := iso9660.Open(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := fs.ReadFile(f, name)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("ReadFile(%q) = %q %v", name, got, err)
		}
	}
}

func TestPathSwitch(t *testing.T) {
	if os.Getenv("LOBER_ISOTEST_CHILD") == "1" {
		got := isotest.Path(t)
		if got != os.Getenv("LOBER_TEST_REAL_ISO") {
			t.Fatalf("Path = %q", got)
		}
		return
	}
	existing := filepath.Join(t.TempDir(), "media.iso")
	if err := os.WriteFile(existing, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value, marker string
		fails               bool
	}{{"unset", "", "SKIP", false}, {"missing", existing + "-missing", "FAIL", true}, {"present", existing, "PASS", false}} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestPathSwitch$", "-test.v")
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, "LOBER_TEST_REAL_ISO=") && !strings.HasPrefix(env, "LOBER_ISOTEST_CHILD=") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			cmd.Env = append(cmd.Env, "LOBER_ISOTEST_CHILD=1", "LOBER_TEST_REAL_ISO="+tc.value)
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.fails || !strings.Contains(string(out), tc.marker) {
				t.Fatalf("child output: %s, error %v", out, err)
			}
		})
	}
}
