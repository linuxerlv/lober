package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func TestPreseedCleanupRedactsInstallerAuthenticationURLs(t *testing.T) {
	const installToken = "install-token-0123456789abcdef0123456789abcdef"
	shell := os.Getenv("LOBER_TEST_BASH")
	if shell == "" {
		var err error
		shell, err = exec.LookPath("bash")
		if err != nil {
			t.Skip("Bash is required to execute generated cleanup scripts")
		}
	}
	t.Run("bash", func(t *testing.T) {
		r := answerFixture(t, "preseed", true, "uefi")
		r.Values["token"] = installToken
		command, problems := answerPart(expansion{request: r}, "cleanup", model.Subject{Kind: "template", ID: "fixture"})
		if len(problems) != 0 {
			t.Fatal(problems)
		}
		payloads := commandPayloads(command)
		if len(payloads) != 1 {
			t.Fatalf("cleanup payload count = %d", len(payloads))
		}

		dir := filepath.ToSlash(t.TempDir())
		bin := filepath.Join(t.TempDir(), "bin")
		if err := os.MkdirAll(bin, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"curl", "wget", "systemctl"} {
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		env := append(os.Environ(), "PATH="+filepath.ToSlash(bin)+":"+os.Getenv("PATH"))
		run := func(script string) {
			t.Helper()
			cmd := exec.Command(shell, "-s")
			cmd.Env = env
			cmd.Stdin = strings.NewReader(script)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("Bash cleanup script failed: %v\n%s", err, output)
			}
		}

		stage := strings.ReplaceAll(payloads[0], "/target", dir)
		run(stage)
		loberDir := filepath.Join(dir, "var/lib/lober")
		if err := os.WriteFile(filepath.Join(loberDir, "execution.completed"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(loberDir, "finalize.token"), []byte("independent-finalize-token"), 0600); err != nil {
			t.Fatal(err)
		}

		logs := filepath.Join(dir, "var/log/installer")
		if err := os.MkdirAll(logs, 0700); err != nil {
			t.Fatal(err)
		}
		answerURL := catalog.AnswerURL(r.Values["gateway"], r.Values["task"], installToken, "preseed.cfg")
		signalURL := catalog.SignalURL(r.Values["gateway"], r.Values["task"], installToken, "installed")
		original := "kernel command line: preseed/url=" + answerURL + "\ninstaller requested " + signalURL + "\ninstallation audit remains\n"
		for _, name := range []string{"hardware-summary", "syslog"} {
			if err := os.WriteFile(filepath.Join(logs, name), []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
		}

		cleanupPath := filepath.Join(loberDir, "cleanup.sh")
		cleanup, err := os.ReadFile(cleanupPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(cleanup), installToken) {
			t.Fatal("firstboot cleanup retained the installer credential")
		}
		targeted := strings.NewReplacer(
			"/var/lib/lober", dir+"/var/lib/lober",
			"/var/lib/cloud", dir+"/var/lib/cloud",
			"/etc/systemd/system", dir+"/etc/systemd/system",
			"/root", dir+"/root",
			"/var/log/installer", dir+"/var/log/installer",
			"/var/log/anaconda", dir+"/var/log/anaconda",
			"/var/log/YaST2", dir+"/var/log/YaST2",
			"/var/adm/autoinstall", dir+"/var/adm/autoinstall",
		).Replace(string(cleanup))
		run(targeted)
		for _, name := range []string{"hardware-summary", "syslog"} {
			got, err := os.ReadFile(filepath.Join(logs, name))
			if err != nil {
				t.Fatal(err)
			}
			text := string(got)
			if strings.Contains(text, installToken) {
				t.Fatalf("installer token remains in %s", name)
			}
			if !strings.Contains(text, "/v1/answer/task-42/[redacted]/preseed.cfg") ||
				!strings.Contains(text, "/v1/signal/task-42/[redacted]/installed") ||
				!strings.Contains(text, "installation audit remains") {
				t.Fatalf("installer log was not redacted while preserving audit text: %q", text)
			}
		}
	})
}
