package render

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/model"
)

func TestWindowsReadyReportFailureRetainsFinalizeToken(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Fatal("PowerShell is required to test the generated Windows report script:", err)
	}
	var mu sync.Mutex
	var posts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts = append(posts, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/v1/signal/task-42/finalize-independent/ready" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	r := answerFixture(t, "unattend", true, "uefi")
	r.Values["gateway"] = server.URL
	cleanup, problems := windowsPartScript(expansion{request: r}, "cleanup", model.Subject{})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	drive := t.TempDir()
	dir := filepath.Join(drive, "ProgramData", "lober")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(dir, "finalize.token")
	if err := os.WriteFile(token, []byte("finalize-independent"), 0600); err != nil {
		t.Fatal(err)
	}
	entrypoint := filepath.Join(dir, "cleanup.ps1")
	if err := os.WriteFile(entrypoint, cleanup, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, pwsh, "-NoProfile", "-NonInteractive", "-File", entrypoint)
	cmd.Env = append(os.Environ(), "SystemDrive="+drive, "WINDIR="+filepath.Join(drive, "Windows"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cleanup script: %v\n%s", err, out)
	}
	mu.Lock()
	got := slices.Clone(posts)
	mu.Unlock()
	want := []string{
		"POST /v1/signal/task-42/finalize-independent/init-done",
		"POST /v1/signal/task-42/finalize-independent/ready",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("cleanup reports: got %v, want %v", got, want)
	}
	contents, err := os.ReadFile(token)
	if err != nil || string(contents) != "finalize-independent" {
		t.Fatalf("ready failed, but cleanup did not preserve its finalize token: %v", err)
	}
}
