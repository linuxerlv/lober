package builtin

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReleaseRecorderRequiresExplicitWrite(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"releases", "fingerprints"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "releases", "v1.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tool := "./testdata/release-recorder"
	if out, err := exec.Command("go", "run", tool, "-dir", root).CombinedOutput(); err == nil {
		t.Fatalf("recorder wrote without explicit -write: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "releases", "v2.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected release file: %v", err)
	}
	if out, err := exec.Command("go", "run", tool, "-dir", root, "-write").CombinedOutput(); err != nil {
		t.Fatalf("recorder failed: %v: %s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(root, "releases", "v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record release
	if err := json.Unmarshal(data, &record); err != nil || len(record.Templates) == 0 || len(record.Hooks) == 0 {
		t.Fatalf("invalid release: %v", err)
	}
	fingerprints, err := filepath.Glob(filepath.Join(root, "fingerprints", "*.sha256"))
	if err != nil || len(fingerprints) == 0 {
		t.Fatalf("missing fingerprints: %v", err)
	}
	if err := os.WriteFile(fingerprints[0], []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "run", tool, "-dir", root, "-write").CombinedOutput(); err == nil {
		t.Fatalf("changed fingerprint accepted: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "releases", "v3.json")); !os.IsNotExist(err) {
		t.Fatalf("release created after fingerprint mismatch: %v", err)
	}
}
