package render

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

func TestRuntimeAllPOSIXFormats(t *testing.T) {
	for _, format := range []catalog.Format{"kickstart", "preseed", "autoinstall", "autoyast", "agama", "esxi-ks"} {
		t.Run(string(format), func(t *testing.T) {
			r := runtimeFixture("pre", runtimeInstance("printf 'executed' > \"$QA_OUTPUT\"\n", true, "sh", 3))
			r.Snapshot.Format = format
			rc, sig, result, _ := runRuntime(t, r, false, true)
			if rc != 0 || !strings.HasPrefix(result, "executed\x00") || !strings.Contains(sig, "timing=pre&rc=0&instance=1&digest=") {
				t.Fatalf("%d %s %q", rc, sig, result)
			}
		})
	}
}
func TestESXiProcessGroupTimeout(t *testing.T) {
	if _, e := exec.LookPath("setsid"); e != nil {
		t.Fatalf("ESXi ABI test requires setsid: %v", e)
	}
	r := runtimeFixture("firstboot", runtimeInstance("sleep 20 &\nprintf '%s' \"$!\" > \"$QA_OUTPUT\"\nwait\n", true, "sh", 1))
	r.Snapshot.Format = "esxi-ks"
	rc, sig, result, elapsed := runRuntime(t, r, false, true)
	if rc != 124 || elapsed > 5*time.Second || !strings.Contains(sig, "rc=124") {
		t.Fatalf("group timeout rc%d elapsed%s sig%s", rc, elapsed, sig)
	}
	pid := strings.SplitN(result, "\x00", 2)[0]
	if pid == "" {
		t.Fatal("hook never started")
	}
	state, e := exec.Command("ps", "-p", pid, "-o", "stat=").CombinedOutput()
	if e == nil && !strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
		t.Fatalf("descendant %s survived timeout: %s", pid, state)
	}
}
func TestESXiMissingRuntimeABIFailsBeforeHook(t *testing.T) {
	r := runtimeFixture("firstboot", runtimeInstance("exit 0\n", true, "sh", 1))
	r.Snapshot.Format = "esxi-ks"
	script := renderRuntime(t, r)
	path := filepath.Join(t.TempDir(), "runtime.sh")
	if e := os.WriteFile(path, []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("/bin/bash", path)
	cmd.Env = []string{"PATH=" + t.TempDir()}
	b, e := cmd.CombinedOutput()
	exit, ok := e.(*exec.ExitError)
	if !ok || exit.ExitCode() != 127 {
		t.Fatalf("missing ABI %v %s", e, b)
	}
}

func TestLinuxNativeFirstbootRunsOfflineSlotsInSnapshotOrder(t *testing.T) {
	for _, requiredFailure := range []bool{false, true} {
		for _, shell := range requiredShells(t) {
			t.Run(fmt.Sprintf("required=%v/%s", requiredFailure, shell), func(t *testing.T) {
				d := t.TempDir()
				local := filepath.Join(d, "var/lib/lober")
				bin := filepath.Join(d, "bin")
				if e := os.MkdirAll(bin, 0700); e != nil {
					t.Fatal(e)
				}
				signals := filepath.Join(d, "signals")
				output := filepath.Join(d, "output")
				transport := `#!/bin/sh
for value in "$@"; do
 case "$value" in -o) exit 88;; *v1/signal*) printf '%s\n' "$value" >> "$QA_SIGNALS";; esac
done
exit 7
`
				if e := os.WriteFile(filepath.Join(bin, "curl"), []byte(transport), 0700); e != nil {
					t.Fatal(e)
				}
				a := runtimeInstance("printf 'one' >> \"$QA_OUTPUT\"\nexit 41\n", false, "sh", 3)
				b := runtimeInstance("printf 'two' >> \"$QA_OUTPUT\"\n", true, "bash", 3)
				if requiredFailure {
					b = runtimeInstance("printf 'two' >> \"$QA_OUTPUT\"\nexit 41\n", true, "bash", 3)
				}
				r := runtimeFixture("firstboot", a)
				r.Values["finalize-token"] = "native-finalize-independent"
				r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 9, Name: "nine", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{a}}, {Seq: 2, Name: "two", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{b}}}
				for _, part := range []catalog.SystemPart{"firstboot", "cleanup"} {
					command, p := answerPart(expansion{request: r}, part, model.Subject{})
					if len(p) != 0 {
						t.Fatal(p)
					}
					stage := strings.ReplaceAll(commandPayloads(command)[0], "/mnt/sysroot", d)
					cmd := exec.Command("bash", "-s")
					cmd.Stdin = strings.NewReader(stage)
					if out, e := cmd.CombinedOutput(); e != nil {
						t.Fatalf("stage %v %s", e, out)
					}
				}
				for _, slot := range r.Snapshot.Slots {
					r.Slot = slot.Name
					script := strings.ReplaceAll(renderRuntime(t, r), "/var/lib/lober", local)
					if e := os.WriteFile(filepath.Join(local, slotFile(r.Snapshot.Format, slot)), []byte(script), 0600); e != nil {
						t.Fatal(e)
					}
					for _, instance := range slot.Instances {
						if e := os.WriteFile(filepath.Join(local, hookFile(instance)), []byte(instance.Body), 0600); e != nil {
							t.Fatal(e)
						}
					}
				}
				for _, name := range []string{"firstboot.sh", "cleanup.sh"} {
					path := filepath.Join(local, name)
					body, e := os.ReadFile(path)
					if e != nil {
						t.Fatal(e)
					}
					if e := os.WriteFile(path, []byte(strings.NewReplacer("/var/lib/lober", local, "/root/", d+"/root/", "/var/log/installer/", d+"/var/log/installer/", "/var/adm/autoinstall/", d+"/var/adm/autoinstall/").Replace(string(body))), 0600); e != nil {
						t.Fatal(e)
					}
				}
				args := []string{filepath.Join(local, "firstboot.sh")}
				if shell == "busybox" {
					args = append([]string{"sh"}, args...)
				}
				cmd := exec.Command(shell, args...)
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_OUTPUT="+output, "QA_SIGNALS="+signals)
				out, e := cmd.CombinedOutput()
				if requiredFailure {
					exit, ok := e.(*exec.ExitError)
					if !ok || exit.ExitCode() != 41 {
						t.Fatalf("required status %v %s", e, out)
					}
				} else if e != nil {
					t.Fatalf("offline firstboot %v %s", e, out)
				}
				got, _ := os.ReadFile(output)
				if string(got) != "onetwo" {
					t.Fatalf("snapshot order %q", got)
				}
				sig, _ := os.ReadFile(signals)
				text := string(sig)
				if strings.Contains(text, r.Values["token"]) || !strings.Contains(text, "native-finalize-independent") {
					t.Fatal("wrong reboot credential")
				}
				if strings.Contains(text, "init-done") == requiredFailure || strings.Contains(text, "ready") == requiredFailure {
					t.Fatal("cleanup ran after required failure or omitted success")
				}
				_, e = os.Stat(filepath.Join(local, "finalize.token"))
				if requiredFailure && e != nil {
					t.Fatal("failed task credential erased")
				}
				if !requiredFailure && !os.IsNotExist(e) {
					t.Fatal("success credential retained")
				}
			})
		}
	}
}

func TestESXiNativeEncodedPayloadExecutesOfflineAndStopsCleanup(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("required=%v", failure), func(t *testing.T) {
			d := t.TempDir()
			local := filepath.Join(d, "lober")
			bin := filepath.Join(d, "bin")
			if e := os.MkdirAll(bin, 0700); e != nil {
				t.Fatal(e)
			}
			signals := filepath.Join(d, "signals")
			output := filepath.Join(d, "output")
			fake := `#!/bin/sh
for value in "$@"; do case "$value" in --post-*) post=1;; *v1/signal*) printf '%s\n' "$value" >> "$QA_SIGNALS";; esac; done
[ "${post:-0}" = 1 ] || exit 88
exit 7
`
			if e := os.WriteFile(filepath.Join(bin, "wget"), []byte(fake), 0700); e != nil {
				t.Fatal(e)
			}
			first := runtimeInstance("printf '%s' \"$LOBER_PARAM_TEXT\" >> \"$QA_OUTPUT\"\nexit 41\n", false, "sh", 3)
			hostile := "原样 '% ! \" ; $()\n"
			first.Params = map[string]model.SnapshotValue{"text": {Type: "text", Items: []string{hostile}}}
			second := runtimeInstance("printf second >> \"$QA_OUTPUT\"\n", true, "sh", 3)
			if failure {
				second = runtimeInstance("printf second >> \"$QA_OUTPUT\"\nexit 41\n", true, "sh", 3)
			}
			r := esxiAnswerFixture()
			r.Snapshot.Template.Body = "vmaccepteula\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:one@firstboot}}\n{{slot:two@firstboot}}\n{{lober:cleanup}}\n"
			r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 9, Name: "one", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{first}}, {Seq: 2, Name: "two", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{second}}}
			b, p, e := Render(r)
			if e != nil || len(p) != 0 {
				t.Fatalf("%v %v", p, e)
			}
			_, native, ok := strings.Cut(string(b), "%firstboot --interpreter=busybox\n")
			if !ok {
				t.Fatal("native firstboot section absent")
			}
			native = rewriteESXiEncodedCommands(t, native, local)
			syntaxCheck(t, native)
			path := filepath.Join(d, "native-firstboot.sh")
			if e := os.WriteFile(path, []byte(native), 0600); e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command("bash", path)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_OUTPUT="+output, "QA_SIGNALS="+signals)
			out, e := cmd.CombinedOutput()
			if failure {
				exit, ok := e.(*exec.ExitError)
				if !ok || exit.ExitCode() != 41 {
					t.Fatalf("required native status %v %s", e, out)
				}
			} else if e != nil {
				t.Fatalf("native offline %v %s", e, out)
			}
			result, _ := os.ReadFile(output)
			if string(result) != hostile+"second" {
				t.Fatalf("raw EnvValue/order %q", result)
			}
			sig, _ := os.ReadFile(signals)
			if strings.Contains(string(sig), r.Values["token"]) || !strings.Contains(string(sig), "finalize-independent") {
				t.Fatal("boot credential reused")
			}
			if strings.Contains(string(sig), "ready") == failure {
				t.Fatal("native cleanup ordering")
			}
			_, e = os.Stat(filepath.Join(local, "finalize.token"))
			if failure && e != nil {
				t.Fatal("failed token removed")
			}
			if !failure && !os.IsNotExist(e) {
				t.Fatal("success token retained")
			}
			_, e = os.Stat(path)
			if failure && e != nil {
				t.Fatal("failed native section removed")
			}
			if !failure && !os.IsNotExist(e) {
				t.Fatal("owned native credential section retained")
			}

		})
	}
}

// Only the target path in generated wrappers is relocated for isolation. Raw
// hook bytes stay untouched, so the generated SHA256 check verifies them.
func rewriteESXiTestPaths(t *testing.T, body, local string) string {
	t.Helper()
	pattern := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)' \| base64 -d > '([^']+)'`)
	body = pattern.ReplaceAllStringFunc(body, func(match string) string {
		groups := pattern.FindStringSubmatch(match)
		if !strings.HasPrefix(groups[2], "/tmp/lober/script-") {
			return match
		}
		decoded, e := base64.StdEncoding.DecodeString(groups[1])
		if e != nil {
			t.Fatal(e)
		}
		rewritten := strings.ReplaceAll(string(decoded), "/tmp/lober", local)
		return strings.Replace(match, groups[1], base64.StdEncoding.EncodeToString([]byte(rewritten)), 1)
	})
	return strings.ReplaceAll(body, "/tmp/lober", local)
}

func TestESXiCleanupRejectsUnownedNativeSection(t *testing.T) {
	for _, content := range []string{"#!/bin/sh\n# supervisor\n", "#!/bin/sh\nLOBER_ESXI_OWNER=lober-esxi-firstboot-other;\n"} {
		t.Run(content, func(t *testing.T) {
			d := t.TempDir()
			native := filepath.Join(d, "native.sh")
			local := filepath.Join(d, "lober")
			if e := os.MkdirAll(local, 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(native, []byte(content), 0600); e != nil {
				t.Fatal(e)
			}
			token := filepath.Join(local, "finalize.token")
			if e := os.WriteFile(token, []byte("must-stay"), 0600); e != nil {
				t.Fatal(e)
			}
			r := esxiAnswerFixture()
			command, p := esxiPart(expansion{request: r}, "cleanup", model.Subject{})
			if len(p) != 0 {
				t.Fatal(p)
			}
			payload := strings.ReplaceAll(commandPayloads(command)[0], "/tmp/lober", local)
			cmd := exec.Command("bash", "-s")
			cmd.Stdin = strings.NewReader("wget() { printf unexpected-ready; }\n" + payload)
			cmd.Env = append(os.Environ(), "LOBER_ESXI_NATIVE_SCRIPT="+native, "LOBER_ESXI_OWNER=lober-esxi-firstboot-42")
			out, e := cmd.CombinedOutput()
			exit, ok := e.(*exec.ExitError)
			if !ok || exit.ExitCode() != 66 {
				t.Fatalf("unowned native accepted: %v %s", e, out)
			}
			if len(out) != 0 {
				t.Fatalf("unexpected success signal %s", out)
			}
			if b, e := os.ReadFile(native); e != nil || string(b) != content {
				t.Fatal("unowned file changed")
			}
			if b, e := os.ReadFile(token); e != nil || string(b) != "must-stay" {
				t.Fatal("unowned cleanup removed credential")
			}
		})
	}
}

func rewriteESXiEncodedCommands(t *testing.T, body, local string) string {
	t.Helper()
	pattern := regexp.MustCompile(`echo ([A-Za-z0-9+/=]+) \| base64 -d \| sh`)
	return pattern.ReplaceAllStringFunc(body, func(match string) string {
		groups := pattern.FindStringSubmatch(match)
		decoded, e := base64.StdEncoding.DecodeString(groups[1])
		if e != nil {
			t.Fatal(e)
		}
		rewritten := rewriteESXiTestPaths(t, string(decoded), local)
		return strings.Replace(match, groups[1], base64.StdEncoding.EncodeToString([]byte(rewritten)), 1)
	})
}

func TestESXiCleanupRejectsSymlinkAndRelativeNativePath(t *testing.T) {
	for _, kind := range []string{"symlink", "relative"} {
		t.Run(kind, func(t *testing.T) {
			d := t.TempDir()
			local := filepath.Join(d, "lober")
			if e := os.MkdirAll(local, 0700); e != nil {
				t.Fatal(e)
			}
			owned := filepath.Join(d, "owned.sh")
			content := "LOBER_ESXI_OWNER=lober-esxi-firstboot-42;\n"
			if e := os.WriteFile(owned, []byte(content), 0600); e != nil {
				t.Fatal(e)
			}
			native := filepath.Join(d, "link.sh")
			if kind == "symlink" {
				if e := os.Symlink(owned, native); e != nil {
					t.Fatal(e)
				}
			} else {
				native = "owned.sh"
			}
			token := filepath.Join(local, "finalize.token")
			if e := os.WriteFile(token, []byte("must-stay"), 0600); e != nil {
				t.Fatal(e)
			}
			r := esxiAnswerFixture()
			command, p := esxiPart(expansion{request: r}, "cleanup", model.Subject{})
			if len(p) != 0 {
				t.Fatal(p)
			}
			payload := strings.ReplaceAll(commandPayloads(command)[0], "/tmp/lober", local)
			cmd := exec.Command("bash", "-s")
			cmd.Dir = d
			cmd.Stdin = strings.NewReader(payload)
			cmd.Env = append(os.Environ(), "LOBER_ESXI_NATIVE_SCRIPT="+native, "LOBER_ESXI_OWNER=lober-esxi-firstboot-42")
			out, e := cmd.CombinedOutput()
			exit, ok := e.(*exec.ExitError)
			if !ok || exit.ExitCode() != 66 {
				t.Fatalf("unsafe native accepted: %v %s", e, out)
			}
			if b, e := os.ReadFile(owned); e != nil || string(b) != content {
				t.Fatal("owned target removed via invalid path")
			}
			if _, e := os.Stat(token); e != nil {
				t.Fatal("credential removed")
			}
		})
	}
}
