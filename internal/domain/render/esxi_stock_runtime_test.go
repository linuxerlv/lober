package render

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

// ESXi 7 的原生环境没有 base64 命令和 command 内置命令。
func TestESXiNativeFirstbootUsesStockTools(t *testing.T) {
	r := esxiAnswerFixture()
	answer, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("render: %v %v", problems, err)
	}
	text := string(answer)
	if strings.Contains(text, "command -v") || strings.Contains(text, " | base64 -d") {
		t.Fatal("ESXi firstboot relies on commands missing from its stock shell")
	}
	pattern := regexp.MustCompile(`echo ([A-Za-z0-9+/=]+) \| openssl base64 -d -A`)
	matches := pattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		t.Fatal("no ESXi firstboot payload uses the installed decoder")
	}
	for _, match := range matches {
		payload, err := base64.StdEncoding.DecodeString(match[1])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), "command -v") || strings.Contains(string(payload), "--post-data") {
			t.Fatal("decoded ESXi firstboot payload uses an unavailable shell or wget option")
		}
		if strings.Contains(string(payload), "esxcli network firewall") {
			t.Fatal("ESXi firstboot must leave the host firewall unchanged")
		}
	}
}

func TestESXiEncodedShellRejectsDecoderFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("需要在 Linux 上运行 POSIX 脚本")
	}
	for name, decoder := range map[string]string{
		"error":   "#!/bin/sh\nexit 37\n",
		"corrupt": "#!/bin/sh\nprintf corrupt\nexit 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bin, "openssl"), []byte(decoder), 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(dir, "ran")
			cmd := exec.Command("bash", "-c", esxiEncodedShell("touch "+shQuote(marker)+"\n"))
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			if output, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("decoder failure was accepted: %s", output)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("payload ran despite decoder failure")
			}
		})
	}
}

func TestESXiHookRunsWhenPythonMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("需要在 Linux 上运行 POSIX 脚本")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	local := filepath.Join(dir, "local")
	for _, path := range []string{bin, local} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"which", "sha256sum", "sleep", "mktemp", "tail", "cat", "openssl", "setsid", "mkdir", "rm", "sh"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("需要测试工具 %s: %v", tool, err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	instance := runtimeInstance("printf ran > \"$QA_OUTPUT\"\n", true, "sh", 5)
	r := runtimeFixture("firstboot", instance)
	r.Snapshot.Format = "esxi-ks"
	if err := os.WriteFile(filepath.Join(local, hookFile(instance)), []byte(instance.Body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "finalize.token"), []byte("token"), 0600); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(renderRuntime(t, r), "/tmp/lober", local)
	output := filepath.Join(dir, "output")
	cmd := exec.Command("/bin/bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+bin, "QA_OUTPUT="+output)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("offline hook: %v %s", err, data)
	}
	if data, err := os.ReadFile(output); err != nil || string(data) != "ran" {
		t.Fatal("hook did not run without Python")
	}
}

// 真实安装环境的 wget 不支持发送请求体，回报必须保留二进制日志。
func TestESXiRuntimePostsBinaryHookReport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("需要在 Linux 上运行 POSIX 脚本")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("需要 bash 运行生成的脚本")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("需要 python3 运行生成的脚本")
	}
	got := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/hook-report") {
			http.Error(w, "wrong request", http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		got <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	body := []byte{'a', 0, 'b', '\n'}
	file := filepath.Join(dir, "report")
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	r := runtimeFixture("firstboot", runtimeInstance("exit 0\n", true, "sh", 5))
	r.Snapshot.Format = "esxi-ks"
	values := map[catalog.SystemValue]string{"gateway": server.URL, "task": "task"}
	script := "TOKEN=token\nWORK=" + shQuote(dir) + "\n" + runtimeSignalFunction(r, r.Snapshot.Slots[0], values) + "\nsignal hook-report 1 0 digest " + shQuote(file) + "\n"
	cmd := exec.Command("bash", "-c", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run signal: %v: %s", err, output)
	}
	select {
	case actual := <-got:
		if string(actual) != string(body) {
			t.Fatalf("report body %q", actual)
		}
	default:
		t.Fatal("firstboot did not send the hook report")
	}
}
