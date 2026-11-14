package render

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func TestWindowsReadyReportFailureRetainsFinalizeToken(t *testing.T) {
	var mu sync.Mutex
	var posts []string
	recovered := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		posts = append(posts, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/v1/signal/task-42/finalize-independent/ready" && !recovered {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	r := windowsAuxFixture()
	r.Values["gateway"] = server.URL
	r.Values["task"] = "task-42"
	target := stageWindowsReportTarget(t, r)
	target.runPowerShell(t, "firstboot.ps1")
	target.runPowerShell(t, "cleanup.ps1")
	mu.Lock()
	got := slices.Clone(posts)
	recovered = true
	mu.Unlock()
	want := []string{
		"POST /v1/signal/task-42/finalize-independent/booted",
		"POST /v1/signal/task-42/finalize-independent/init-start",
		"POST /v1/signal/task-42/finalize-independent/init-done",
		"POST /v1/signal/task-42/finalize-independent/ready",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("cleanup reports: got %v, want %v", got, want)
	}
	contents, err := os.ReadFile(target.token)
	if err != nil || string(contents) != "finalize-independent" {
		t.Fatalf("ready failed, but cleanup did not preserve its finalize token: %v", err)
	}
	target.runScheduledReplay(t)
	mu.Lock()
	got = slices.Clone(posts)
	mu.Unlock()
	want = append(want, "POST /v1/signal/task-42/finalize-independent/ready")
	if !slices.Equal(got, want) {
		t.Fatalf("replay repeated acknowledged reports: got %v, want %v", got, want)
	}
	if _, err := os.Stat(target.token); !os.IsNotExist(err) {
		t.Fatal("acknowledged completion retained the finalize token:", err)
	}
}

func TestWindowsReportReplayPreservesOrderAndDoesNotRerunHooks(t *testing.T) {
	for _, failedPhase := range []string{"init-start", "hook-report"} {
		t.Run(failedPhase, func(t *testing.T) {
			phases := []string{"booted", "init-start", "slot-start", "hook-start", "hook-report", "slot-end", "init-done", "ready"}
			hook := runtimeInstance(`[IO.File]::AppendAllText($env:LOBER_TEST_MARKER, 'once')
[Console]::OpenStandardOutput().Write([byte[]](104, 0, 105), 0, 3)
`, true, "powershell", 10)
			var mu sync.Mutex
			var accepted []string
			recovered := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				phase := filepath.Base(req.URL.Path)
				if req.Method != "POST" || req.URL.Path != "/v1/signal/42/finalize-independent/"+phase {
					t.Errorf("unexpected report %s %s", req.Method, req.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				next := len(accepted)
				if next >= len(phases) || phases[next] != phase {
					t.Errorf("report %s arrived out of order after %v", phase, accepted)
					w.WriteHeader(http.StatusConflict)
					return
				}
				if phase == "hook-report" {
					data, err := io.ReadAll(req.Body)
					query := req.URL.Query()
					if err != nil || string(data) != "h\x00i" || query.Get("rc") != "0" || query.Get("slot") != "2" || query.Get("instance") != "1" || query.Get("timing") != "firstboot" || query.Get("digest") != hook.Digest {
						t.Errorf("incorrect hook report %q %s: %v", data, req.URL.RawQuery, err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
				}
				if phase == failedPhase && !recovered {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				accepted = append(accepted, phase)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			r := windowsAuxFixture()
			r.Values["gateway"] = server.URL
			r.Snapshot.Slots[1].Instances = []model.SnapshotInstance{hook}
			target := stageWindowsReportTarget(t, r)
			target.runPowerShell(t, "firstboot.ps1")
			target.runSlot(t, r, 1, "0")
			target.runPowerShell(t, "cleanup.ps1")
			if data, err := os.ReadFile(target.token); err != nil || string(data) != "finalize-independent" {
				t.Fatalf("blocked report lost its finalize token: %v", err)
			}

			mu.Lock()
			before := slices.Clone(accepted)
			recovered = true
			mu.Unlock()
			if want := phases[:slices.Index(phases, failedPhase)]; !slices.Equal(before, want) {
				t.Fatalf("accepted while offline: got %v, want %v", before, want)
			}
			target.runScheduledReplay(t)

			mu.Lock()
			after := slices.Clone(accepted)
			mu.Unlock()
			if !slices.Equal(after, phases) {
				t.Fatalf("accepted after replay: got %v, want %v", after, phases)
			}
			if data, err := os.ReadFile(target.marker); err != nil || string(data) != "once" {
				t.Fatalf("hook should execute once: got %q, error %v", data, err)
			}
			for _, path := range []string{target.token, target.schedule, filepath.Join(target.dir, hookFile(hook)), filepath.Join(target.dir, "cleanup.ps1")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("acknowledged completion retained %s: %v", filepath.Base(path), err)
				}
			}
		})
	}
}

func TestWindowsReportReplayDoesNotMarkFailedHookReady(t *testing.T) {
	var mu sync.Mutex
	var reports []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		phase := filepath.Base(req.URL.Path)
		reports = append(reports, phase)
		switch phase {
		case "hook-report":
			if rc := req.URL.Query().Get("rc"); rc != "71" {
				t.Errorf("failed hook report returned rc=%s, want 71", rc)
			}
		case "slot-end":
			// 必需钩子失败后，服务端已经结束任务。
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	hook := runtimeInstance("[IO.File]::AppendAllText($env:LOBER_TEST_MARKER, 'once')\nexit 71\n", true, "powershell", 10)
	r := windowsAuxFixture()
	r.Values["gateway"] = server.URL
	r.Snapshot.Slots[1].Instances = []model.SnapshotInstance{hook}
	target := stageWindowsReportTarget(t, r)
	target.runPowerShell(t, "firstboot.ps1")
	target.runSlot(t, r, 1, "71")
	target.runScheduledReplay(t)
	mu.Lock()
	got := slices.Clone(reports)
	mu.Unlock()
	want := []string{"booted", "init-start", "slot-start", "hook-start", "hook-report", "slot-end"}
	if !slices.Equal(got, want) {
		t.Fatalf("failed hook reports: got %v, want %v", got, want)
	}
	if data, err := os.ReadFile(target.marker); err != nil || string(data) != "once" {
		t.Fatalf("failed hook should execute once: got %q, error %v", data, err)
	}
	if _, err := os.Stat(target.token); err != nil {
		t.Fatal("rejected report caused success cleanup:", err)
	}
}

func TestWindowsInstallerSlotRetriesReportsBeforeContinuing(t *testing.T) {
	for _, tc := range []struct {
		name       string
		startFails int
		wantRC     string
		wantPhases []string
	}{
		{"recovered", 2, "0", []string{"slot-start", "slot-start", "slot-start", "slot-end"}},
		{"unavailable", 5, "125", []string{"slot-start", "slot-start", "slot-start", "slot-start", "slot-start"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var phases []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				phase := filepath.Base(req.URL.Path)
				if req.Method != "POST" || req.URL.Path != "/v1/signal/42/install-tok-fixed-0000000000000000/"+phase || req.URL.Query().Get("timing") != "pre" {
					t.Errorf("unexpected installer report %s %s", req.Method, req.URL)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				phases = append(phases, phase)
				if len(phases) <= tc.startFails {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			r := windowsAuxFixture()
			r.Values["gateway"] = server.URL
			r.Snapshot.Slots[0].Instances = nil
			target := stageWindowsReportTarget(t, r)
			target.runSlot(t, r, 0, tc.wantRC)
			mu.Lock()
			got := slices.Clone(phases)
			mu.Unlock()
			if !slices.Equal(got, tc.wantPhases) {
				t.Fatalf("installer reports: got %v, want %v", got, tc.wantPhases)
			}
		})
	}
}

type windowsReportTarget struct {
	pwsh, dir, token, marker, schedule, harness string
	env                                         []string
}

func stageWindowsReportTarget(t *testing.T, r Request) windowsReportTarget {
	t.Helper()
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Fatal("PowerShell is required to test generated Windows report scripts:", err)
	}
	drive := t.TempDir()
	dir := filepath.Join(drive, "ProgramData", "lober")
	windir := filepath.Join(drive, "Windows")
	for _, path := range []string{dir, filepath.Join(windir, "System32")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	target := windowsReportTarget{
		pwsh: pwsh, dir: dir, token: filepath.Join(dir, "finalize.token"),
		marker: filepath.Join(drive, "hook-runs"), schedule: filepath.Join(drive, "scheduled-task.xml"),
		harness: filepath.Join(drive, "invoke-generated.ps1"),
	}
	target.env = append(os.Environ(), "SystemDrive="+drive, "WINDIR="+windir, "LOBER_TEST_MARKER="+target.marker, "LOBER_TEST_TASK="+target.schedule, "LOBER_DIR="+dir)

	aux, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("render Windows staging: %v %v", problems, err)
	}
	stagedFiles := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)' \| base64 -d > "\$OEM/([a-z-]+\.ps1)"`)
	for _, file := range stagedFiles.FindAllStringSubmatch(decodeWindowsRuntime(t, string(aux)), -1) {
		data, err := base64.StdEncoding.DecodeString(file[1])
		if err != nil {
			t.Fatal(err)
		}
		writeWindowsReportFixture(t, filepath.Join(dir, file[2]), data, 0600)
	}
	writeWindowsReportFixture(t, target.token, []byte(r.Values["finalize-token"]), 0600)
	writeWindowsReportFixture(t, filepath.Join(windir, "System32", "icacls.exe"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	// Linux 上运行生成的 PowerShell 和真实网络请求，只模拟 Windows 的定时任务和权限命令。
	writeWindowsReportFixture(t, target.harness, []byte(`param([string]$Entry, [switch]$Replay)
$ErrorActionPreference = 'Stop'
function Register-ScheduledTask {
    param([string]$TaskName, [string]$Xml, [string]$User, [switch]$Force)
    [IO.File]::WriteAllText($env:LOBER_TEST_TASK, $Xml)
}
function Get-ScheduledTask {
    [CmdletBinding()]
    param([string]$TaskName)
    if (Test-Path -LiteralPath $env:LOBER_TEST_TASK) { return $TaskName }
}
function Unregister-ScheduledTask {
    [CmdletBinding(SupportsShouldProcess)]
    param([string]$TaskName)
    Remove-Item -LiteralPath $env:LOBER_TEST_TASK
}
if ($Replay) { & $Entry -Replay } else { & $Entry }
exit $LASTEXITCODE
`), 0600)
	return target
}

func (target windowsReportTarget) runPowerShell(t *testing.T, entry string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args = append([]string{"-NoProfile", "-NonInteractive", "-File", target.harness, filepath.Join(target.dir, entry)}, args...)
	cmd := exec.CommandContext(ctx, target.pwsh, args...)
	cmd.Env = target.env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", entry, err, out)
	}
}

func (target windowsReportTarget) runScheduledReplay(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(target.schedule)
	if err != nil {
		t.Fatal("replay was not scheduled:", err)
	}
	var task struct {
		Actions []struct {
			Command   string
			Arguments string
		} `xml:"Actions>Exec"`
	}
	if err := xml.Unmarshal(data, &task); err != nil {
		t.Fatal("invalid scheduled task:", err)
	}
	want := `-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "` + filepath.Join(target.dir, "cleanup.ps1") + `" -Replay`
	if len(task.Actions) != 1 || !strings.HasSuffix(task.Actions[0].Command, "powershell.exe") || task.Actions[0].Arguments != want {
		t.Fatalf("scheduled task must invoke report-only replay: %+v", task.Actions)
	}
	target.runPowerShell(t, "cleanup.ps1", "-Replay")
}

func (target windowsReportTarget) runSlot(t *testing.T, r Request, index int, wantRC string) {
	t.Helper()
	slot := r.Snapshot.Slots[index]
	r.Kind, r.Slot = catalog.FileScript, slot.Name
	script := filepath.Join(target.dir, "test-runtime.sh")
	writeWindowsReportFixture(t, script, []byte(decodeWindowsRuntime(t, renderRuntime(t, r))), 0600)
	for _, hook := range slot.Instances {
		writeWindowsReportFixture(t, filepath.Join(target.dir, hookFile(hook)), []byte(hook.Body), 0600)
	}
	// 适配 Windows 启动参数，在 Linux 上执行生成的插槽脚本和真实钩子。
	writeWindowsReportFixture(t, filepath.Join(target.dir, "busybox.exe"), []byte(`#!/bin/sh
if [ "$1" = sh ] && [ "$2" = -X ]; then shift 2; exec busybox sh "$@"; fi
if [ "$1" = kill ]; then kill "$2" "${3#-}" 2>/dev/null || :; exit 0; fi
exec busybox "$@"
`), 0700)
	writeWindowsReportFixture(t, filepath.Join(target.dir, "cmd.exe"), []byte(`#!/bin/sh
pwsh -NoProfile -NonInteractive -File "$HOOK"
printf '%s' "$?" > "$LOBER_NATIVE_RC_FILE"
`), 0700)
	writeWindowsReportFixture(t, filepath.Join(target.dir, "curl.exe"), []byte("#!/bin/sh\nexec curl \"$@\"\n"), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "busybox", "sh", script)
	cmd.Env = append(target.env, "PATH="+target.dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("slot script: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(filepath.Join(target.dir, "runtime-"+strconv.Itoa(slot.Seq)+".rc")); err != nil || string(data) != wantRC {
		t.Fatalf("slot returned %q, want %s: %v", data, wantRC, err)
	}
}

func writeWindowsReportFixture(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
