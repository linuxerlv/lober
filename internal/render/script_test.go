package render

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

func runtimeInstance(body string, required bool, interpreter catalog.Interpreter, seconds int) model.SnapshotInstance {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	return model.SnapshotInstance{HookID: "hook", HookVersion: 1, Kind: model.ImplScript, Body: body, Digest: digest, Required: required, Interpreter: interpreter, TimeoutSec: seconds}
}
func runtimeFixture(timing catalog.Timing, instances ...model.SnapshotInstance) Request {
	r := menuFixture("rocky", "uefi")
	r.Kind = catalog.FileScript
	r.Slot = "运行"
	r.Snapshot.Slots = []model.SnapshotSlot{{Name: r.Slot, Seq: 3, Exec: true, Timing: timing, Instances: instances}}
	return r
}
func renderRuntime(t *testing.T, r Request) string {
	t.Helper()
	b, p, e := Render(r)
	if e != nil || len(p) != 0 {
		t.Fatalf("runtime render: %v %v", p, e)
	}
	return string(b)
}
func requiredShells(t *testing.T) []string {
	t.Helper()
	for _, exe := range []string{"busybox", "bash"} {
		if _, e := exec.LookPath(exe); e != nil {
			t.Fatalf("required target syntax checker %s absent: %v", exe, e)
		}
	}
	return []string{"busybox", "bash"}
}
func syntaxCheck(t *testing.T, script string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "syntax.sh")
	if e := os.WriteFile(p, []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	for _, shell := range requiredShells(t) {
		args := []string{"-n", p}
		if shell == "busybox" {
			args = append([]string{"sh"}, args...)
		}
		if b, e := exec.Command(shell, args...).CombinedOutput(); e != nil {
			t.Fatalf("%s syntax: %v %s", shell, e, b)
		}
	}
}
func runRuntime(t *testing.T, r Request, corrupt bool, transportFailure bool, schedule ...func(string) string) (int, string, string, time.Duration) {
	t.Helper()
	d := t.TempDir()
	local := filepath.Join(d, "local")
	remote := filepath.Join(d, "remote")
	bin := filepath.Join(d, "bin")
	for _, p := range []string{local, remote, bin} {
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for _, i := range r.Snapshot.Slots[0].Instances {
		body := i.Body
		if corrupt {
			body += "\n#corrupt"
		}
		if e := os.WriteFile(filepath.Join(remote, i.Digest), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		if r.Snapshot.Slots[0].Timing == "firstboot" {
			if e := os.WriteFile(filepath.Join(local, hookFile(i)), []byte(body), 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := os.WriteFile(filepath.Join(local, "finalize.token"), []byte("finalize-independent"), 0600); e != nil {
		t.Fatal(e)
	}
	fake := `#!/bin/sh
out=; body=; post=0; address=
while [ "$#" -gt 0 ]; do
 case "$1" in
 -o|-O) out=$2; shift 2;;
 --data-binary) body=$2; shift 2;;
 -X) post=1; shift 2;;
 --retry|-m|--max-time|-H|-T) shift 2;;
 --post-file=*) post=1; body="@${1#*=}"; shift;;
 --post-data=*) post=1; body="${1#*=}"; shift;;
 -*) shift;;
 *) address=$1; shift;;
 esac
done
if [ "$post" = 1 ]; then
 printf '%s\n' "$address" >> "$QA_TELEMETRY"
 if [ -n "$body" ]; then case "$body" in @*) cat "${body#@}" >> "$QA_BODY";; esac; fi
 [ "$QA_TRANSPORT_FAIL" = 0 ] || exit 7
else
 [ "$QA_OFFLINE" = 0 ] || exit 88
 cp "$QA_REMOTE/${address##*/}" "$out"
fi
`
	if e := os.WriteFile(filepath.Join(bin, "wget"), []byte(fake), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(bin, "curl"), []byte(fake), 0700); e != nil {
		t.Fatal(e)
	}
	script := renderRuntime(t, r)
	for _, transform := range schedule {
		script = transform(script)
	}
	if r.Snapshot.Format == "preseed" || r.Snapshot.Format == "esxi-ks" {
		script = "#!/bin/sh\nwget() { " + shQuote(filepath.Join(bin, "wget")) + " \"$@\"; }\n" + script
	}
	syntaxCheck(t, script)
	script = strings.ReplaceAll(script, "/var/lib/lober", local)
	script = strings.ReplaceAll(script, "/tmp/lober", local)
	path := filepath.Join(d, "runtime.sh")
	if e := os.WriteFile(path, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	for _, shell := range requiredShells(t) {
		args := []string{"-n", path}
		if shell == "busybox" {
			args = append([]string{"sh"}, args...)
		}
		if b, e := exec.Command(shell, args...).CombinedOutput(); e != nil {
			t.Fatalf("syntax: %v %s", e, b)
		}
	}
	telemetry := filepath.Join(d, "signals")
	body := filepath.Join(d, "body")
	output := filepath.Join(d, "output")
	offline := "0"
	if r.Snapshot.Slots[0].Timing == "firstboot" {
		offline = "1"
	}
	failure := "0"
	if transportFailure {
		failure = "1"
	}
	args := []string{"sh", path}
	cmd := exec.Command("busybox", args...)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_REMOTE="+remote, "QA_TELEMETRY="+telemetry, "QA_BODY="+body, "QA_OUTPUT="+output, "QA_TRANSPORT_FAIL="+failure, "QA_OFFLINE="+offline)
	start := time.Now()
	b, e := cmd.CombinedOutput()
	elapsed := time.Since(start)
	rc := 0
	if e != nil {
		var ok bool
		exit, yes := e.(*exec.ExitError)
		ok = yes
		if !ok {
			t.Fatal(e)
		}
		rc = exit.ExitCode()
	}
	sig, _ := os.ReadFile(telemetry)
	result, _ := os.ReadFile(output)
	if rc != 0 {
		t.Logf("runtime status %d output %s", rc, b)
	}
	tail, _ := os.ReadFile(body)
	return rc, string(sig), string(result) + "\x00" + string(tail), elapsed
}
func TestRuntimeExportsHostileValuesAndOrder(t *testing.T) {
	first := runtimeInstance("printf '%s' \"$LOBER_PARAM_TEXT\" > \"$QA_OUTPUT\"\nprintf '%s' \"$LOBER_PARAM_ITEMS\" >> \"$QA_OUTPUT\"\nprintf '%05000d' 0\n", true, "sh", 5)
	hostile := "单引号'\"; $(touch /tmp/lober-injection) %PATH% !bang! & | <>\nlast\n"
	first.Params = map[string]model.SnapshotValue{"text": {Type: "text", Items: []string{hostile}}, "items": {Type: "text", List: true, Items: []string{"one'", "two\nthree"}}}
	second := runtimeInstance("printf 'second' >> \"$QA_OUTPUT\"\n", true, "bash", 5)
	r := runtimeFixture("pre", first, second)
	rc, sig, result, _ := runRuntime(t, r, false, false)
	if rc != 0 {
		t.Fatal(rc)
	}
	pieces := strings.SplitN(result, "\x00", 2)
	want := hostile + "one'\ntwo\nthree" + "second"
	if pieces[0] != want {
		t.Fatalf("export changed bytes: %q != %q", pieces[0], want)
	}
	if len(pieces[1]) != 4096 {
		t.Fatalf("tail bytes %d", len(pieces[1]))
	}
	for _, phase := range []string{"slot-start", "hook-start", "hook-report", "slot-end"} {
		if !strings.Contains(sig, "/"+phase+"?slot=3") {
			t.Fatal(sig)
		}
	}
	if strings.Count(sig, "hook-report") != 2 {
		t.Fatal(sig)
	}
}
func TestRuntimeRequiredOptionalDigestAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, body        string
		required, corrupt bool
		want              int
		seconds           int
	}{{"required", "exit 41\n", true, false, 41, 5}, {"optional", "exit 41\n", false, false, 0, 5}, {"digest", "exit 0\n", true, true, 65, 5}, {"timeout", "sleep 20\nprintf unexpected >> \"$QA_OUTPUT\"\n", true, false, 124, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			a := runtimeInstance(tc.body, tc.required, "sh", tc.seconds)
			b := runtimeInstance("printf second >> \"$QA_OUTPUT\"\n", true, "sh", 5)
			rc, sig, result, d := runRuntime(t, runtimeFixture("pre", a, b), tc.corrupt, true)
			if rc != tc.want {
				t.Fatalf("rc=%d want%d signals=%s", rc, tc.want, sig)
			}
			ran := strings.HasPrefix(result, "second\x00")
			if ran == tc.required {
				t.Fatalf("required policy result %q", result)
			}
			if !strings.Contains(sig, fmt.Sprintf("rc=%d", map[bool]int{true: tc.want, false: 41}[tc.required])) {
				t.Fatal(sig)
			}
			if tc.name == "timeout" && d > 5*time.Second {
				t.Fatalf("timeout not enforced: %s", d)
			}
		})
	}
}
func TestRuntimeOfflineFirstbootUsesIndependentToken(t *testing.T) {
	r := runtimeFixture("firstboot", runtimeInstance("printf offline > \"$QA_OUTPUT\"\n", true, "sh", 5))
	script := renderRuntime(t, r)
	if strings.Contains(script, r.Values["token"]) || strings.Contains(script, "hooks/") {
		t.Fatal("reboot payload contains boot credential/download")
	}
	rc, sig, result, _ := runRuntime(t, r, false, true)
	if rc != 0 || !strings.HasPrefix(result, "offline\x00") || !strings.Contains(sig, "/finalize-independent/") {
		t.Fatalf("offline rc=%d %s %q", rc, sig, result)
	}
}

// Expose the cancellation interleaving deterministically: after terminating the
// watchdog's sleep child, let its parent shell run before it is itself killed.
// Hook interpreters, watchdog, wait and kill operations are all actually run.
func TestRuntimeWatchdogCancellationPreservesOptionalContinuation(t *testing.T) {
	first := runtimeInstance("exit 41\n", false, "sh", 5)
	second := runtimeInstance("printf second >> \"$QA_OUTPUT\"\n", true, "sh", 5)
	schedule := func(script string) string {
		return strings.Replace(script, ` kill -KILL "$1" 2>/dev/null || :`, ` if [ "$1" = "${watchdog:-}" ]; then /bin/sleep 0.05; fi
 kill -KILL "$1" 2>/dev/null || :`, 1)
	}
	rc, sig, result, elapsed := runRuntime(t, runtimeFixture("pre", first, second), false, false, schedule)
	if rc != 0 || !strings.HasPrefix(result, "second\x00") || strings.Contains(sig, "rc=124") || !strings.Contains(sig, "rc=41") {
		t.Fatalf("cancelled timer changed hook status: rc=%d elapsed=%s output=%q signals=%s", rc, elapsed, result, sig)
	}
}
