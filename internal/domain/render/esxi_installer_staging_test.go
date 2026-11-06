package render

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

func TestESXiInstallerStagesFrozenPayloadAndRejectsUnsafeTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("需要 Linux 下的 BusyBox 和本机 HTTP 服务")
	}
	busybox := os.Getenv("LOBER_TEST_BUSYBOX")
	if busybox == "" {
		var err error
		busybox, err = exec.LookPath("busybox")
		if err != nil {
			t.Fatal("需要 BusyBox：", err)
		}
	}
	for _, scenario := range []string{"success", "corrupt", "preterm", "prehup", "preint", "postterm", "posthup", "postint", "tempmode", "missing", "ambiguous", "wrongfs", "nodisk", "multidisk", "diskquery", "symlink", "existing"} {
		t.Run(scenario, func(t *testing.T) {
			d := t.TempDir()
			bin := filepath.Join(d, "bin")
			volumes := filepath.Join(d, "volumes")
			volume := filepath.Join(volumes, "OSDATA-fixture")
			for _, dir := range []string{bin, volume} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			// 测试环境使用 GNU timeout；这里适配 ESXi BusyBox 的参数形式。
			fsType := "vmfs6l"
			if scenario == "wrongfs" {
				fsType = "vfat"
			}
			commands := map[string]string{
				"timeout":  "#!/bin/sh\n[ \"$1\" = -t ] || exit 125\nshift\nexec /usr/bin/timeout \"$@\"\n",
				"stat":     "#!/bin/sh\nif [ \"$1\" = -f ]; then echo " + fsType + "; elif [ \"$1\" = -c ]; then printf '0:%s\\n' \"$(/usr/bin/stat -c %a \"$3\")\"; else exit 125; fi\n",
				"esxcli":   "#!/bin/sh\necho installer-service-unavailable >&2\nexit 1\n",
				"localcli": "#!/bin/sh\n[ \"$*\" = 'storage core device list' ] || exit 125\nprintf '%s\\n' '   Device Type: Direct-Access'\n",
			}
			switch scenario {
			case "nodisk":
				commands["localcli"] = "#!/bin/sh\nexit 0\n"
			case "multidisk":
				commands["localcli"] = "#!/bin/sh\nprintf '%s\\n' '   Device Type: Direct-Access' '   Device Type: Direct-Access'\n"
			case "diskquery":
				commands["localcli"] = "#!/bin/sh\nprintf '%s\\n' '   Device Type: Direct-Access'\nexit 1\n"
			}
			if strings.HasPrefix(scenario, "pre") {
				signal := strings.ToUpper(strings.TrimPrefix(scenario, "pre"))
				commands["timeout"] = "#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -O ]; then shift; printf secret > \"$1\"; break; fi; shift; done\nkill -" + signal + " \"$PPID\"\nexit 143\n"
			}
			if strings.HasPrefix(scenario, "post") {
				signal := strings.ToUpper(strings.TrimPrefix(scenario, "post"))
				commands["cp"] = "#!/bin/sh\n/bin/cp \"$@\" || exit $?\nkill -" + signal + " \"$PPID\"\nexit 143\n"
			}
			for name, script := range commands {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			var payload atomic.Value
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/v1/answer/42/stage-install-fixture/esxi-firstboot.sh" {
					http.Error(w, "unexpected path", http.StatusForbidden)
					return
				}
				if scenario == "corrupt" {
					_, _ = w.Write([]byte("corrupt"))
					return
				}
				data, ok := payload.Load().([]byte)
				if !ok {
					http.Error(w, "stage not ready", http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write(data)
			}))
			defer server.Close()
			r := runtimeFixture("firstboot", runtimeInstance("printf 'stage data'\n", true, "sh", 2))
			r.Snapshot.Format, r.Snapshot.Family, r.Snapshot.Series = "esxi-ks", "esxi", "esxi"
			r.Values["gateway"], r.Values["finalize-token"], r.Values["token"] = server.URL, "private-finalize-fixture", "stage-install-fixture"
			r.Snapshot.Template.Body = "vmaccepteula\nreboot\n%firstboot --interpreter=busybox\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
			r.Kind = catalog.FileESXiStage
			rendered, stageProblems, err := Render(r)
			if err != nil || len(stageProblems) != 0 {
				t.Fatalf("render stage: %v %v", stageProblems, err)
			}
			payload.Store(rendered)
			r.Kind = catalog.FileAnswer
			answer, ps, err := Render(r)
			if err != nil || len(ps) != 0 {
				t.Fatalf("render answer: %v %v", ps, err)
			}
			_, sections, ok := strings.Cut(string(answer), "%pre --interpreter=busybox\n")
			if !ok {
				t.Fatal("missing pre section")
			}
			pre, sections, ok := strings.Cut(sections, "%post --interpreter=busybox\n")
			if !ok {
				t.Fatal("missing post section")
			}
			post, _, ok := strings.Cut(sections, "%firstboot --interpreter=busybox\n")
			if !ok {
				t.Fatal("missing firstboot section")
			}
			taskDigest := sha256.Sum256([]byte("42"))
			name := fmt.Sprintf("lober-esxi-%x", taskDigest)
			temporary := filepath.Join(d, "tmp", name)
			if err := os.Mkdir(filepath.Dir(temporary), 0700); err != nil {
				t.Fatal(err)
			}
			pre = strings.ReplaceAll(pre, "/tmp/"+name, temporary)
			post = strings.ReplaceAll(post, "/tmp/"+name, temporary)
			post = strings.ReplaceAll(post, "/vmfs/volumes/OSDATA-*", filepath.Join(volumes, "OSDATA-*"))
			// BusyBox 可能优先使用内置命令，测试替身必须指定完整路径。
			pre = strings.ReplaceAll(pre, "timeout -t ", shQuote(filepath.Join(bin, "timeout"))+" -t ")
			pre = strings.ReplaceAll(pre, "stat -", shQuote(filepath.Join(bin, "stat"))+" -")
			post = strings.ReplaceAll(post, "stat -", shQuote(filepath.Join(bin, "stat"))+" -")
			if strings.HasPrefix(scenario, "post") {
				post = strings.ReplaceAll(post, "cp ", shQuote(filepath.Join(bin, "cp"))+" ")
			}
			run := func(script string) error {
				cmd := exec.Command(busybox, "sh", "-s")
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
				cmd.Stdin = strings.NewReader(script)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Logf("installer script failed: %v %s", err, out)
				}
				return err
			}
			if err := run(pre); (err != nil) != (scenario == "corrupt" || strings.HasPrefix(scenario, "pre")) {
				t.Fatalf("pre result: %v", err)
			}
			if scenario == "corrupt" || strings.HasPrefix(scenario, "pre") {
				if _, err := os.Stat(temporary); !os.IsNotExist(err) {
					t.Fatal("corrupt download left temporary data")
				}
				return
			}
			if scenario == "ambiguous" {
				if err := os.Mkdir(filepath.Join(volumes, "OSDATA-other"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "missing" {
				if err := os.Remove(volume); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "tempmode" {
				if err := os.Chmod(temporary, 0777); err != nil {
					t.Fatal(err)
				}
			}
			target := filepath.Join(volume, name)
			protected := ""
			if scenario == "symlink" || scenario == "existing" {
				protected = filepath.Join(d, "unrelated")
				if err := os.Mkdir(protected, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(protected, "keep"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "symlink" {
				if err := os.Symlink(protected, target); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "existing" {
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, "keep"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := run(post); (err != nil) != (scenario != "success") {
				t.Fatalf("post result: %v", err)
			}
			if _, err := os.Stat(temporary); scenario == "tempmode" {
				if err != nil {
					t.Fatal("untrusted temporary directory was removed")
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("installer left temporary stage data")
			}
			if scenario != "success" {
				if strings.HasPrefix(scenario, "post") {
					if _, err := os.Lstat(target); !os.IsNotExist(err) {
						t.Fatal("interrupted staging left persistent data")
					}
				}
				if protected != "" {
					if got, err := os.ReadFile(filepath.Join(protected, "keep")); err != nil || string(got) != "keep" {
						t.Fatal("unrelated data was changed")
					}
				}
				if scenario == "existing" {
					if got, err := os.ReadFile(filepath.Join(target, "keep")); err != nil || string(got) != "keep" {
						t.Fatal("existing target was changed")
					}
				}
				return
			}
			got, err := os.ReadFile(filepath.Join(target, "stage.sh"))
			if err != nil || string(got) != string(rendered) {
				t.Fatal("protected stage differs from the frozen payload")
			}
			for path, mode := range map[string]os.FileMode{target: 0700, filepath.Join(target, "stage.sh"): 0600} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != mode {
					t.Fatalf("unsafe stage permissions: %v", err)
				}
			}
		})
	}
}

