package render

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/catalog"
)

// Exercise real downloads and POSTs with precisely the installer tools, so a
// host curl cannot conceal selection of an unavailable installer transport.
func TestDebianInstallerLifecycleHTTP(t *testing.T) {
	for _, timing := range []catalog.Timing{"pre", "post-installer", "post-target", "firstboot"} {
		t.Run(string(timing), func(t *testing.T) {
			d := t.TempDir()
			bin := filepath.Join(d, "bin")
			local := filepath.Join(d, "local")
			for _, p := range []string{bin, local} {
				if e := os.Mkdir(p, 0700); e != nil {
					t.Fatal(e)
				}
			}
			busybox, e := exec.LookPath("busybox")
			if e != nil {
				t.Fatal(e)
			}
			for _, app := range []string{"sh", "mkdir", "cat", "mktemp", "rm", "sleep", "sha256sum", "tail", "base64"} {
				if e := os.Symlink(busybox, filepath.Join(bin, app)); e != nil {
					t.Fatal(e)
				}
			}
			transport := "wget"
			if timing == "post-target" || timing == "firstboot" {
				transport = "curl"
			}
			exe, e := exec.LookPath(transport)
			if e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(exe, filepath.Join(bin, transport)); e != nil {
				t.Fatal(e)
			}
			if transport == "curl" {
				wget, e := exec.LookPath("wget")
				if e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(wget, filepath.Join(bin, "wget")); e != nil {
					t.Fatal(e)
				}
			}
			instance := runtimeInstance("printf '%05000d' 0\nprintf '\\000end\\n'\nprintf ran > \"$QA_OUTPUT\"\n", true, "sh", 5)
			type event struct {
				method, path, content string
				query                 url.Values
				body                  []byte
			}
			var mu sync.Mutex
			var events []event
			downloads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/hooks/"+instance.Digest) {
					downloads++
					io.WriteString(w, instance.Body)
					return
				}
				b, e := io.ReadAll(r.Body)
				if e != nil {
					t.Error(e)
				}
				events = append(events, event{r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.URL.Query(), b})
			}))
			defer server.Close()
			r := runtimeFixture(timing, instance)
			r.Snapshot.Format = "preseed"
			r.Values["gateway"] = server.URL
			if timing == "firstboot" {
				if e := os.WriteFile(filepath.Join(local, hookFile(instance)), []byte(instance.Body), 0600); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(local, "finalize.token"), []byte("finalize-independent"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			script := strings.ReplaceAll(renderRuntime(t, r), "/var/lib/lober", local)
			if transport == "wget" {
				if !strings.Contains(script, "/usr/bin/wget -q") {
					t.Fatal("installer POST must invoke packaged GNU wget, bypassing ash applets")
				}
				// Only relocate the qualified absolute client path. The HTTP
				// client and all its POST/download behavior remain real.
				client := os.Getenv("LOBER_DEBIAN_WGET")
				if client == "" {
					client = exe
				}
				version, e := exec.Command(client, "--version").Output()
				if e != nil || !strings.HasPrefix(string(version), "GNU Wget ") {
					t.Fatalf("GNU wget required: %v %s", e, version)
				}
				script = strings.ReplaceAll(script, "/usr/bin/wget", shQuote(client))
			}

			file := filepath.Join(d, "runtime.sh")
			if e := os.WriteFile(file, []byte(script), 0600); e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command(busybox, "sh", file)
			cmd.Env = []string{"PATH=" + bin, "QA_OUTPUT=" + filepath.Join(d, "ran")}
			if b, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("runtime %v: %s", e, b)
			}
			if b, e := os.ReadFile(filepath.Join(d, "ran")); e != nil || string(b) != "ran" {
				t.Fatalf("hook did not run: %q %v", b, e)
			}
			raw, e := os.ReadFile(filepath.Join(local, hookFile(instance)))
			if e != nil || string(raw) != instance.Body {
				t.Fatalf("frozen hook changed: %v", e)
			}
			mu.Lock()
			defer mu.Unlock()
			wantDownloads := 1
			if timing == "firstboot" {
				wantDownloads = 0
			}
			if downloads != wantDownloads {
				t.Errorf("downloads=%d", downloads)
			}
			if len(events) != 4 {
				t.Fatalf("signals=%d, want 4", len(events))
			}
			phases := []string{"slot-start", "hook-start", "hook-report", "slot-end"}
			token := r.Values["token"]
			if timing == "firstboot" {
				token = "finalize-independent"
			}
			log := append(bytes.Repeat([]byte("0"), 5000), []byte{0, 'e', 'n', 'd', '\n'}...)
			for i, event := range events {
				if event.method != "POST" || event.content != "text/plain" || event.path != "/v1/signal/42/"+token+"/"+phases[i] {
					t.Errorf("event %d: %+v", i, event)
				}
				want := url.Values{"slot": {"3"}, "timing": {string(timing)}, "rc": {"0"}}
				if i == 1 || i == 2 {
					want.Set("instance", "1")
					want.Set("digest", instance.Digest)
				}
				if event.query.Encode() != want.Encode() {
					t.Errorf("query=%v want%v", event.query, want)
				}
				var body []byte
				if i == 2 {
					body = log[len(log)-4096:]
				}
				if !bytes.Equal(event.body, body) {
					t.Errorf("phase %s body bytes=%d want%d", phases[i], len(event.body), len(body))
				}
			}
		})
	}
}

func TestESXiWatchdogReapsSleepOnQuickCompletion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		required bool
		status   int
		want     int
	}{{"success", true, 0, 0}, {"required-failure", true, 41, 41}, {"optional-failure", false, 41, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			pidfile := filepath.Join(t.TempDir(), "sleeper")
			// Observe the actual sleep process without replacing its behavior. Wait for
			// its PID before ending the hook, so this deterministically tests cancellation.
			body := "while [ ! -s " + shQuote(pidfile) + " ]; do sleep 0.01; done\nexit " + fmt.Sprint(tc.status) + "\n"
			r := runtimeFixture("firstboot", runtimeInstance(body, tc.required, "sh", 30))
			r.Snapshot.Format = "esxi-ks"
			observer := func(script string) string {
				command := "printf '%s' \"$$\" > " + shQuote(pidfile) + "; exec /bin/sleep \"$@\""
				return strings.ReplaceAll(script, `sleep "$seconds"`, "/bin/sh -c "+shQuote(command)+` sleep "$seconds"`)
			}
			rc, sig, _, elapsed := runRuntime(t, r, false, false, observer)
			if rc != tc.want || elapsed > 5*time.Second || strings.Contains(sig, "rc=124") {
				t.Errorf("rc=%d elapsed=%s signals=%s", rc, elapsed, sig)
			}
			pid, e := os.ReadFile(pidfile)
			if e != nil {
				t.Fatal(e)
			}
			if _, e := exec.LookPath("ps"); e != nil {
				t.Fatal(e)
			}
			state, e := exec.Command("ps", "-p", string(pid), "-o", "stat=").CombinedOutput()
			if e != nil {
				if status, ok := e.(*exec.ExitError); !ok || status.ExitCode() != 1 {
					t.Fatalf("process inspection failed: %v %s", e, state)
				}
			}
			if e == nil && strings.TrimSpace(string(state)) != "" {
				exec.Command("kill", "-KILL", string(pid)).Run()
				t.Fatalf("watchdog sleep %s survived completion: %s", pid, state)
			}
		})
	}
}

func TestESXiAnswerPasswordFixtureExpanded(t *testing.T) {
	r := esxiAnswerFixture()
	b, p, e := Render(r)
	if e != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, e)
	}
	if !strings.Contains(string(b), "rootpw --iscrypted $6$loberfixture$s48HMcReZuTl/hNjsbSzMJwS02PdRGxg.c4S/fcvnK13iqHyU.awFlFtXuNgMr03IcKPWRhuoIqMge0LZ.KGX0\n") || strings.Contains(string(b), "{{param:") {
		t.Fatalf("invalid password fixture: %s", strings.SplitN(string(b), "\n", 3)[1])
	}
}

func TestESXiWatchdogStartupCancellationAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("initialization-failure=", fail), func(t *testing.T) {
			body := "exit 0\n"
			want := 0
			if fail {
				body = "sleep 20\n"
				want = 125
			}
			r := runtimeFixture("firstboot", runtimeInstance(body, true, "sh", 30))
			r.Snapshot.Format = "esxi-ks"
			schedule := func(script string) string {
				// Delay startup while the hook completes; production never requires
				// fractional sleep. The second case fails the ready-file operation.
				script = strings.Replace(script, " (\n sleep", " (\n /bin/sleep 0.05\n sleep", 1)
				if fail {
					script = strings.Replace(script, `: > "$WORK/watchdog-ready"`, "false", 1)
				}
				return script
			}
			rc, sig, _, elapsed := runRuntime(t, r, false, false, schedule)
			if rc != want || elapsed > 5*time.Second || strings.Contains(sig, "rc=124") {
				t.Fatalf("rc=%d elapsed=%s signals=%s", rc, elapsed, sig)
			}
		})
	}
}

func TestESXiWatchdogSequentialHookPolicy(t *testing.T) {
	for _, timedOut := range []bool{false, true} {
		for _, required := range []bool{false, true} {
			t.Run(fmt.Sprintf("timeout=%v/required=%v", timedOut, required), func(t *testing.T) {
				body := "exit 41\n"
				hookStatus := 41
				seconds := 30
				if timedOut {
					body = "sleep 20\n"
					hookStatus = 124
					seconds = 1
				}
				r := runtimeFixture("firstboot", runtimeInstance(body, required, "sh", seconds), runtimeInstance("printf second > \"$QA_OUTPUT\"\n", true, "sh", 30))
				r.Snapshot.Format = "esxi-ks"
				rc, sig, result, elapsed := runRuntime(t, r, false, false)
				want := 0
				if required {
					want = hookStatus
				}
				if rc != want || elapsed > 5*time.Second || strings.HasPrefix(result, "second\x00") == required || !strings.Contains(sig, fmt.Sprint("rc=", hookStatus)) {
					t.Fatalf("rc=%d elapsed=%s result=%q signals=%s", rc, elapsed, result, sig)
				}
			})
		}
	}
}
