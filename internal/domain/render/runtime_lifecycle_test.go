package render

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func TestRuntimeAllPOSIXFormats(t *testing.T) {
	for _, format := range []catalog.Format{"kickstart", "preseed", "autoinstall", "esxi-ks"} {
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
 case "$value" in */installed) printf '%s\n' "$value" >> "$QA_SIGNALS"; exit 0;; esac
 case "$value" in -o) exit 88;; *v1/signal*) printf '%s\n' "$value" >> "$QA_SIGNALS";; esac
done
[ -f "$QA_RECOVERED" ] && exit 0
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
					cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_SIGNALS="+filepath.Join(d, "installer-signals"))
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
					if e := os.WriteFile(path, []byte(strings.NewReplacer("/var/lib/lober", local, "/etc/systemd/system", d+"/etc/systemd/system", "/root/", d+"/root/", "/var/log/installer/", d+"/var/log/installer/", "/var/adm/autoinstall/", d+"/var/adm/autoinstall/").Replace(string(body))), 0600); e != nil {
						t.Fatal(e)
					}
				}
				args := []string{filepath.Join(local, "firstboot.sh")}
				if shell == "busybox" {
					args = append([]string{"sh"}, args...)
				}
				cmd := exec.Command(shell, args...)
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_OUTPUT="+output, "QA_SIGNALS="+signals, "QA_RECOVERED="+filepath.Join(d, "recovered"))
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
				queued := ""
				metas, _ := filepath.Glob(filepath.Join(local, "outbox-finalize", "*.meta"))
				for _, meta := range metas {
					body, _ := os.ReadFile(meta)
					queued += string(body)
				}
				if strings.Contains(queued, "init-done") == requiredFailure || strings.Contains(queued, "ready") == requiredFailure {
					t.Fatal("cleanup ran after required failure or omitted success")
				}
				_, e = os.Stat(filepath.Join(local, "finalize.token"))
				if requiredFailure && e != nil {
					t.Fatal("failed task credential erased")
				}
				if !requiredFailure {
					if e != nil {
						t.Fatal("offline completion discarded credential")
					}
					if e = os.WriteFile(filepath.Join(d, "recovered"), nil, 0600); e != nil {
						t.Fatal(e)
					}
					retry := exec.Command("sh", filepath.Join(local, "cleanup.sh"))
					retry.Env = cmd.Env
					if data, e := retry.CombinedOutput(); e != nil {
						t.Fatalf("completion replay: %v %s", e, data)
					}
					if _, e = os.Stat(filepath.Join(local, "finalize.token")); !os.IsNotExist(e) {
						t.Fatal("acknowledged success credential retained")
					}
					if data, _ := os.ReadFile(output); string(data) != "onetwo" {
						t.Fatal("replay reran hooks")
					}
				}
			})
		}
	}
}

func TestESXiNativeEncodedPayloadExecutesOfflineAndStopsCleanup(t *testing.T) {
	for _, scenario := range []string{"success", "required", "cleanup", "diskcleanup"} {
		t.Run(scenario, func(t *testing.T) {
			failure := scenario != "success"
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
			python := "#!/bin/sh\nprintf '%s\\n' \"$LOBER_SIGNAL_URL\" >> \"$QA_SIGNALS\"\nexit 7\n"
			if e := os.WriteFile(filepath.Join(bin, "python3"), []byte(python), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(bin, "esxcli"), []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
				t.Fatal(e)
			}
			if scenario == "diskcleanup" {
				fakeRemove := "#!/bin/sh\ncase \"$*\" in *stage.sh*) exit 1;; *) exec /bin/rm \"$@\";; esac\n"
				if e := os.WriteFile(filepath.Join(bin, "rm"), []byte(fakeRemove), 0700); e != nil {
					t.Fatal(e)
				}
			}
			// 用测试目录模拟实机上已确认的 vmfs6l 文件系统。
			if e := os.WriteFile(filepath.Join(bin, "stat"), []byte("#!/bin/sh\nif [ \"$1\" = -f ]; then echo vmfs6l; elif [ \"$1\" = -c ]; then case \"$3\" in */stage.sh) echo 0:600;; *) echo 0:700;; esac; else exit 1; fi\n"), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.MkdirAll(filepath.Join(d, "firewall"), 0700); e != nil {
				t.Fatal(e)
			}
			first := runtimeInstance("printf '%s' \"$LOBER_PARAM_TEXT\" >> \"$QA_OUTPUT\"\nexit 41\n", false, "sh", 3)
			hostile := "原样 '% ! \" ; $()\n"
			first.Params = map[string]model.SnapshotValue{"text": {Type: "text", Items: []string{hostile}}}
			second := runtimeInstance("printf second >> \"$QA_OUTPUT\"\n", true, "sh", 3)
			if scenario == "required" {
				second = runtimeInstance("printf second >> \"$QA_OUTPUT\"\nexit 41\n", true, "sh", 3)
			} else if scenario == "cleanup" {
				second = runtimeInstance("printf second >> \"$QA_OUTPUT\"\nprintf extra > \"$QA_LOCAL/leftover\"\n", true, "sh", 3)
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
			r.Kind = catalog.FileESXiStage
			stage, p, e := Render(r)
			if e != nil || len(p) != 0 {
				t.Fatalf("stage: %v %v", p, e)
			}
			oldDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
			_, originalDir := esxiStagePaths(r.Values["task"])
			stageDir := filepath.Join(d, "protected-stage")
			if e := os.Mkdir(stageDir, 0700); e != nil {
				t.Fatal(e)
			}
			stage = []byte(relocateESXiCommands(t, string(stage), local, originalDir, stageDir, "", ""))
			newDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
			stagePath := filepath.Join(stageDir, "stage.sh")
			if e := os.WriteFile(stagePath, stage, 0600); e != nil {
				t.Fatal(e)
			}
			native = relocateESXiCommands(t, native, local, originalDir, stageDir, oldDigest, newDigest)
			syntaxCheck(t, native)
			path := filepath.Join(d, "native-firstboot.sh")
			if e := os.WriteFile(path, []byte(native), 0600); e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command("bash", path)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_OUTPUT="+output, "QA_SIGNALS="+signals, "QA_LOCAL="+local)
			out, e := cmd.CombinedOutput()
			if scenario == "required" {
				exit, ok := e.(*exec.ExitError)
				if !ok || exit.ExitCode() != 41 {
					t.Fatalf("required native status %v %s", e, out)
				}
			} else if scenario == "cleanup" || scenario == "diskcleanup" {
				if e == nil {
					t.Fatal("cleanup failure was accepted")
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
			if !os.IsNotExist(e) {
				t.Fatal("firstboot token retained")
			}
			for _, pattern := range []string{"script-*.sh", "hook-*"} {
				matches, err := filepath.Glob(filepath.Join(local, pattern))
				if err != nil || len(matches) != 0 {
					t.Fatalf("temporary payloads retained: %v %v", matches, err)
				}
			}
			if _, e = os.Stat(path); e != nil {
				t.Fatal("native section must remain under ESXi ownership")
			}
			_, e = os.Stat(stagePath)
			if scenario == "diskcleanup" {
				if e != nil {
					t.Fatal("failed disk cleanup unexpectedly removed the stage")
				}
			} else if !os.IsNotExist(e) {
				t.Fatal("firstboot stage retained")
			}

		})
	}
}

func TestESXiNativeFirstbootRejectsChangedStage(t *testing.T) {
	d := t.TempDir()
	bin := filepath.Join(d, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "stat"), []byte("#!/bin/sh\nif [ \"$1\" = -f ]; then echo vmfs6l; elif [ \"$1\" = -c ]; then case \"$3\" in */stage.sh) echo 0:600;; *) echo 0:700;; esac; else exit 1; fi\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r := esxiAnswerFixture()
	r.Snapshot.Template.Body = "vmaccepteula\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 3, Name: "运行", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{runtimeInstance("printf ran > \"$QA_OUTPUT\"\n", true, "sh", 2)}}}
	answer, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("answer: %v %v", problems, err)
	}
	r.Kind = catalog.FileESXiStage
	stage, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("stage: %v %v", problems, err)
	}
	_, native, ok := strings.Cut(string(answer), "%firstboot --interpreter=busybox\n")
	if !ok {
		t.Fatal("missing native firstboot")
	}
	_, originalDir := esxiStagePaths(r.Values["task"])
	stageDir := filepath.Join(d, "stage")
	if err := os.Mkdir(stageDir, 0700); err != nil {
		t.Fatal(err)
	}
	stagePath := filepath.Join(stageDir, "stage.sh")
	if err := os.WriteFile(stagePath, append(stage, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	native = relocateESXiCommands(t, native, filepath.Join(d, "local"), originalDir, stageDir, "", "")
	path := filepath.Join(d, "native.sh")
	if err := os.WriteFile(path, []byte(native), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(d, "output")
	cmd := exec.Command("bash", path)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_OUTPUT="+output)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 65 {
		t.Fatalf("changed stage was not rejected: %v %s", err, out)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("hook ran from a changed stage")
	}
	if _, err := os.Stat(stagePath); err != nil {
		t.Fatal("untrusted stage was unexpectedly removed")
	}
}

func TestESXiFirstbootClearsTemporaryDataWhenStageChangesMidRun(t *testing.T) {
	runESXiFirstbootFailureScenario(t, "stage-change")
}

func TestESXiFirstbootCleansWhenTemplateCommandFails(t *testing.T) {
	runESXiFirstbootFailureScenario(t, "template-failure")
}

func TestESXiFirstbootDoesNotDeleteReplacedStageDirectory(t *testing.T) {
	runESXiFirstbootFailureScenario(t, "replaced-directory")
}

func runESXiFirstbootFailureScenario(t *testing.T, scenario string) {
	t.Helper()
	d := t.TempDir()
	bin, local, stageDir := filepath.Join(d, "bin"), filepath.Join(d, "local"), filepath.Join(d, "stage")
	for _, dir := range []string{bin, stageDir} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		"stat":    "#!/bin/sh\nif [ \"$1\" = -f ]; then echo vmfs6l; elif [ \"$1\" = -c ]; then case \"$3\" in */stage.sh) echo 0:600;; *) echo 0:700;; esac; else exit 1; fi\n",
		"python3": "#!/bin/sh\nexit 7\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	r := esxiAnswerFixture()
	extra, hook, want := "printf x >> \"$QA_STAGE\"\n", "printf ran > \"$QA_OUTPUT\"\n", 65
	if scenario == "template-failure" {
		extra, want = "false\n", 1
	}
	if scenario == "replaced-directory" {
		extra, hook, want = "", "mv \"$QA_STAGE_DIR\" \"$QA_STAGE_DIR.saved\"\nln -s \"$QA_VICTIM\" \"$QA_STAGE_DIR\"\nexit 41\n", 41
	}
	r.Snapshot.Template.Body = "vmaccepteula\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n" + extra + "{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 3, Name: "运行", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{runtimeInstance(hook, true, "sh", 2)}}}
	answer, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("answer: %v %v", problems, err)
	}
	r.Kind = catalog.FileESXiStage
	stage, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("stage: %v %v", problems, err)
	}
	_, native, ok := strings.Cut(string(answer), "%firstboot --interpreter=busybox\n")
	if !ok {
		t.Fatal("missing native firstboot")
	}
	_, originalDir := esxiStagePaths(r.Values["task"])
	oldDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
	stage = []byte(relocateESXiCommands(t, string(stage), local, originalDir, stageDir, "", ""))
	newDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
	stagePath := filepath.Join(stageDir, "stage.sh")
	if err := os.WriteFile(stagePath, stage, 0600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(d, "unrelated")
	if err := os.Mkdir(victim, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(victim, "stage.sh"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	native = relocateESXiCommands(t, native, local, originalDir, stageDir, oldDigest, newDigest)
	path, output := filepath.Join(d, "native.sh"), filepath.Join(d, "output")
	if err := os.WriteFile(path, []byte(native), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", path)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_STAGE="+stagePath, "QA_STAGE_DIR="+stageDir, "QA_VICTIM="+victim, "QA_OUTPUT="+output)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != want {
		t.Fatalf("firstboot failure was not reported: %v %s", err, out)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatal("temporary sensitive data survived stage verification failure")
	}
	if scenario == "template-failure" {
		if _, err := os.Stat(stagePath); !os.IsNotExist(err) {
			t.Fatal("verified stage survived a failed template command")
		}
	} else if _, err := os.Stat(stagePath); err != nil {
		t.Fatal("untrusted stage was unexpectedly removed")
	}
	if got, err := os.ReadFile(filepath.Join(victim, "stage.sh")); err != nil || string(got) != "keep" {
		t.Fatal("unrelated stage file was changed")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("hook ran despite the modified stage")
	}
}

func TestESXiFirstbootRejectsPreexistingTemporarySymlink(t *testing.T) {
	d := t.TempDir()
	bin := filepath.Join(d, "bin")
	other := filepath.Join(d, "other")
	for _, dir := range []string{bin, other} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		"stat":    "#!/bin/sh\nif [ \"$1\" = -f ]; then echo vmfs6l; elif [ \"$1\" = -c ]; then case \"$3\" in */stage.sh) echo 0:600;; *) echo 0:700;; esac; else exit 1; fi\n",
		"python3": "#!/bin/sh\nexit 7\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	local := filepath.Join(d, "local")
	if err := os.Symlink(other, local); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "keep"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	r := esxiAnswerFixture()
	r.Snapshot.Template.Body = "vmaccepteula\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 3, Name: "运行", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{runtimeInstance("printf ran > \"$QA_OUTPUT\"\n", true, "sh", 2)}}}
	answer, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("answer: %v %v", problems, err)
	}
	r.Kind = catalog.FileESXiStage
	stage, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("stage: %v %v", problems, err)
	}
	_, native, ok := strings.Cut(string(answer), "%firstboot --interpreter=busybox\n")
	if !ok {
		t.Fatal("missing native firstboot")
	}
	_, originalDir := esxiStagePaths(r.Values["task"])
	stageDir := filepath.Join(d, "stage")
	if err := os.Mkdir(stageDir, 0700); err != nil {
		t.Fatal(err)
	}
	oldDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
	stage = []byte(relocateESXiCommands(t, string(stage), local, originalDir, stageDir, "", ""))
	newDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
	if err := os.WriteFile(filepath.Join(stageDir, "stage.sh"), stage, 0600); err != nil {
		t.Fatal(err)
	}
	native = relocateESXiCommands(t, native, local, originalDir, stageDir, oldDigest, newDigest)
	path, output := filepath.Join(d, "native.sh"), filepath.Join(d, "output")
	if err := os.WriteFile(path, []byte(native), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", path)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_OUTPUT="+output)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 66 {
		t.Fatalf("temporary symlink accepted: %v %s", err, out)
	}
	if entries, err := os.ReadDir(other); err != nil || len(entries) != 1 || entries[0].Name() != "keep" {
		t.Fatal("existing directory was changed")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("hook ran despite the unsafe temporary path")
	}
	if _, err := os.Stat(filepath.Join(stageDir, "stage.sh")); err != nil {
		t.Fatal("unowned temporary path caused the protected stage to be removed")
	}
}

func relocateESXiCommands(t *testing.T, body, local, oldDir, newDir, oldDigest, newDigest string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(LOBER_(?:ESXI|FW)_CODE=\$\(echo )([A-Za-z0-9+/=]+)( \| openssl base64 -d -A\)\|\|exit 127;.*?case \$LOBER_(?:ESXI|FW)_HASH in )([0-9a-f]{64})`)
	result := pattern.ReplaceAllStringFunc(body, func(match string) string {
		groups := pattern.FindStringSubmatch(match)
		decoded, err := base64.StdEncoding.DecodeString(groups[2])
		if err != nil {
			t.Fatal(err)
		}
		rewritten := rewriteESXiTestPaths(t, string(decoded), local)
		rewritten = strings.ReplaceAll(rewritten, oldDir, newDir)
		if oldDigest != "" {
			rewritten = strings.ReplaceAll(rewritten, oldDigest, newDigest)
		}
		digest := sha256.Sum256([]byte(strings.TrimRight(rewritten, "\n")))
		return groups[1] + base64.StdEncoding.EncodeToString([]byte(rewritten)) + groups[3] + fmt.Sprintf("%x", digest)
	})
	result = strings.ReplaceAll(result, oldDir, newDir)
	result = strings.ReplaceAll(result, "/tmp/lober", local)
	if oldDigest != "" {
		result = strings.ReplaceAll(result, oldDigest, newDigest)
	}
	return result
}

// Only the target path in generated wrappers is relocated for isolation. Raw
// hook bytes stay untouched, so the generated SHA256 check verifies them.
func rewriteESXiTestPaths(t *testing.T, body, local string) string {
	t.Helper()
	pattern := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)' \| openssl base64 -d -A > '([^']+)'`)
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
	body = strings.ReplaceAll(body, "/etc/vmware/firewall", filepath.Join(filepath.Dir(local), "firewall"))
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
	pattern := regexp.MustCompile(`(LOBER_(?:ESXI|FW)_CODE=\$\(echo )([A-Za-z0-9+/=]+)( \| openssl base64 -d -A\)\|\|exit 127;.*?case \$LOBER_(?:ESXI|FW)_HASH in )([0-9a-f]{64})`)
	return pattern.ReplaceAllStringFunc(body, func(match string) string {
		groups := pattern.FindStringSubmatch(match)
		decoded, e := base64.StdEncoding.DecodeString(groups[2])
		if e != nil {
			t.Fatal(e)
		}
		rewritten := rewriteESXiTestPaths(t, string(decoded), local)
		digest := sha256.Sum256([]byte(strings.TrimRight(rewritten, "\n")))
		return groups[1] + base64.StdEncoding.EncodeToString([]byte(rewritten)) + groups[3] + fmt.Sprintf("%x", digest)
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
