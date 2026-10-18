package render

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

func TestFirstbootRetainsCredentialsAndReplaysSignalsWithoutRerunningHooks(t *testing.T) {
	var recovered, initDone, ready atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		phase := filepath.Base(r.URL.Path)
		if phase == "installed" {
			w.WriteHeader(200)
			return
		}
		if !strings.Contains(r.URL.Path, "/final-independent/") {
			w.WriteHeader(403)
			return
		}
		if !recovered.Load() {
			w.WriteHeader(503)
			return
		}
		if phase == "init-done" {
			initDone.Store(true)
		}
		if phase == "ready" {
			if !initDone.Load() {
				w.WriteHeader(409)
				return
			}
			ready.Store(true)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	dir := t.TempDir()
	local := filepath.Join(dir, "var/lib/lober")
	output := filepath.Join(dir, "hook-output")
	instance := runtimeInstance("printf 'once' >> \"$QA_OUTPUT\"\n", true, "sh", 3)
	r := runtimeFixture("firstboot", instance)
	r.Slot = "init"
	r.Values["gateway"] = server.URL
	r.Values["finalize-token"] = "final-independent"
	r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 1, Name: "init", Exec: true, Timing: catalog.TimingFirstboot, Instances: []model.SnapshotInstance{instance}}}
	replace := strings.NewReplacer("/mnt/sysroot/var/lib/lober", local, "/mnt/sysroot/etc", dir+"/etc", "/mnt/sysroot", dir, "/var/lib/lober", local, "/etc/systemd/system", dir+"/etc/systemd/system", "/root/", dir+"/root/", "/var/log/installer/", dir+"/var/log/installer/", "/var/adm/autoinstall/", dir+"/var/adm/autoinstall/")
	run := func(body string) {
		t.Helper()
		cmd := exec.Command("bash", "-s")
		cmd.Env = append(os.Environ(), "QA_OUTPUT="+output)
		cmd.Stdin = strings.NewReader("systemctl() { return 0; }\n" + body)
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("script failed: %v %s", err, data)
		}
	}
	for _, part := range []catalog.SystemPart{"firstboot", "cleanup"} {
		command, p := answerPart(expansion{request: r}, part, model.Subject{})
		if len(p) != 0 {
			t.Fatal(p)
		}
		run(replace.Replace(commandPayloads(command)[0]))
	}
	if err := os.WriteFile(filepath.Join(local, slotFile(r.Snapshot.Format, r.Snapshot.Slots[0])), []byte(replace.Replace(renderRuntime(t, r))), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, hookFile(instance)), []byte(instance.Body), 0600); err != nil {
		t.Fatal(err)
	}
	run("sh " + shQuote(filepath.Join(local, "firstboot.sh")))
	if _, err := os.Stat(filepath.Join(local, "finalize.token")); err != nil {
		t.Fatal("lost acknowledgment erased recovery credential")
	}
	if ready.Load() {
		t.Fatal("server completed while unavailable")
	}
	// A timer invokes completion/replay only, never the installation hooks.
	recovered.Store(true)
	run("sh " + shQuote(filepath.Join(local, "cleanup.sh")))
	got, err := os.ReadFile(output)
	if err != nil || string(got) != "once" {
		t.Fatal("recovery reran or skipped hook", string(got), err)
	}
	if !ready.Load() {
		t.Fatal("recovery failed to acknowledge completion")
	}
	if _, err := os.Stat(filepath.Join(local, "finalize.token")); !os.IsNotExist(err) {
		t.Fatal("acknowledged success retained credential")
	}
}
