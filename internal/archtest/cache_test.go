package archtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The real incremental Makefile gate must discover packages even when Go has
// a cached architecture PASS and a non-whitelisted root already existed.
func TestIncrementalGateRejectsPackageInExistingRoot(t *testing.T) {
	root := filepath.Dir(goModPath(t))
	tmp := t.TempDir()
	tracked := append([]string{"ls-files", "-z", "--", "go.mod", "go.sum", "Makefile"}, sourceRoots...)
	cmd := exec.Command("git", tracked...)
	cmd.Dir = root
	files, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range strings.Split(strings.TrimSuffix(string(files), "\x00"), "\x00") {
		body, e := os.ReadFile(filepath.Join(root, name))
		if e != nil {
			t.Fatal(e)
		}
		target := filepath.Join(tmp, name)
		if e := os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(target, body, 0600); e != nil {
			t.Fatal(e)
		}
		old := time.Now().Add(-time.Hour)
		if e := os.Chtimes(target, old, old); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.Mkdir(filepath.Join(tmp, "tools"), 0700); e != nil {
		t.Fatal(e)
	}
	run := func(program string, args ...string) (string, error) {
		cmd := exec.Command(program, args...)
		cmd.Dir = tmp
		cmd.Env = append(os.Environ(), "GOFLAGS=-run=^Test全模块依赖守卫$", "GOWORK=off")
		b, e := cmd.CombinedOutput()
		t.Logf("%s %s\n%s", program, strings.Join(args, " "), b)
		return string(b), e
	}
	// This exact Go command is the pre-fix normal gate, with other test bodies
	// filtered out. Two passes establish that the architecture result is cached.
	for _, args := range [][]string{{"test", "./..."}, {"test", "-race", "./..."}} {
		for i := 0; i < 2; i++ {
			out, e := run("go", args...)
			if e != nil {
				t.Fatalf("warm: %v %s", e, out)
			}
			if i == 1 && !strings.Contains(out, "internal/archtest\t(cached)") {
				t.Fatal("did not establish cached architecture PASS")
			}
		}
	}

	probe := filepath.Join(tmp, "tools", "reviewprobe")
	if e := os.Mkdir(probe, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(probe, "probe.go"), []byte("package reviewprobe\nimport _ \"net/http\"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, gate := range []string{"test", "race"} {
		out, e := run("make", gate)
		if e == nil || !strings.Contains(out, "tools/reviewprobe") || !strings.Contains(out, string(ruleLayer)) {
			t.Errorf("make %s accepted unclassified package or failed for wrong reason: %v\n%s", gate, e, out)
		}
	}
}
