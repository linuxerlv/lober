package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func TestRemainingMenuGolden(t *testing.T) {
	for _, series := range []catalog.Series{"sles15", "sles16", "windows-server"} {
		t.Run(string(series), func(t *testing.T) {
			r := menuFixture(series, "uefi")
			r.Snapshot.ISO.Artifacts["live-root"] = "agama/LiveOS/squashfs.img"
			for _, role := range []string{"wimboot", "bcd", "boot-sdi", "boot-wim", "winpeshl", "winpe-busybox", "winpe-curl"} {
				r.Snapshot.ISO.Artifacts[role] = "windows/" + role
			}
			got, p, e := Render(r)
			if e != nil || len(p) != 0 {
				t.Fatalf("%v %v", p, e)
			}
			want, e := os.ReadFile(filepath.Join("testdata", "menu-"+string(series)+".golden"))
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("mismatch:\n%s", got)
			}
		})
	}
}
func TestAgamaAnswerDelivery(t *testing.T) {
	name, ok := catalog.DeliveryFileName("agama", catalog.FileAnswer)
	if !ok || name != "autoinst.jsonnet" {
		t.Fatalf("canonical Agama filename=%q,%v", name, ok)
	}
	r := menuFixture("sles16", "uefi")
	r.Kind = catalog.FileAnswer
	r.Snapshot.Template.Body = "{ product: { id: 'SLES' }, scripts: { pre: [] } }\n"
	got, p, e := Render(r)
	if e != nil || len(p) != 0 || string(got) != r.Snapshot.Template.Body {
		t.Fatalf("%s %v %v", got, p, e)
	}
}

func TestWindowsRuntimeStaticContract(t *testing.T) {
	i := runtimeInstance("@echo off\r\nexit /b 0\r\n", true, "cmd", 3)
	i.Params = map[string]model.SnapshotValue{"text": {Type: "text", Items: []string{"汉字 ' \" % ! & <>\nlast\n"}}}
	r := windowsRuntimeFixture("firstboot", i)
	windowsRuntimeRoles(&r)
	b, p, e := Render(r)
	if e != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, e)
	}
	if !strings.HasPrefix(string(b), "@echo off\r\n") || strings.Contains(string(b), r.Values["token"]) {
		t.Fatalf("invalid Windows wrapper %s", b)
	}
	for _, line := range strings.Split(string(b), "\r\n") {
		if len(line) > 4096 {
			t.Fatalf("CMD payload not chunked: %d", len(line))
		}
	}
	i.Params["text"] = model.SnapshotValue{Type: "text", Items: []string{strings.Repeat("x", 8192)}}
	r.Snapshot.Slots[0].Instances[0] = i
	assertRenderProblem(t, r, "render_windows_env_limit")
}
func TestWindowsAuxRequiresRealTools(t *testing.T) {
	r := menuFixture("windows-server", "uefi")
	r.Kind = catalog.FileAux
	r.Values["smb"] = "//server/install"
	r.Values["finalize-token"] = "finalize-only"
	for _, role := range catalog.RequiredRoles(r.Snapshot.Series) {
		r.Snapshot.ISO.Artifacts[role] = "windows/" + role
	}
	b, p, e := Render(r)
	if e != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, e)
	}
	decoded := decodeWindowsRuntime(t, string(b))
	syntaxCheck(t, decoded)
	expanded := decodeEmbeddedFiles(t, decoded)
	for _, want := range []string{"/m:X:/lober-oem", "$1/ProgramData/lober", "unattend.xml", "firstboot.ps1", "cleanup.ps1", "finalize.token"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if !strings.Contains(string(b), "wpeinit") {
		t.Fatal("WinPE initialization missing")
	}
	delete(r.Snapshot.ISO.Artifacts, "winpe-curl")
	assertRenderProblem(t, r, "render_artifact_missing")
}

func decodeWindowsRuntime(t *testing.T, body string) string {
	t.Helper()
	var chunks strings.Builder
	for _, line := range strings.Split(body, "\r\n") {
		if strings.HasPrefix(line, "> ") || strings.HasPrefix(line, ">> ") {
			_, chunk, ok := strings.Cut(line, " echo ")
			if !ok {
				t.Fatal(line)
			}
			chunks.WriteString(chunk)
		}
	}
	decoded, e := base64.StdEncoding.DecodeString(chunks.String())
	if e != nil {
		t.Fatal(e)
	}
	return string(decoded)
}

func TestESXiNativeFirstbootOffline(t *testing.T) {
	r := runtimeFixture("firstboot", runtimeInstance("printf 'raw ESXi\n'\nexit 41\n", true, "sh", 2))
	r.Snapshot.Format = "esxi-ks"
	r.Snapshot.Family = "esxi"
	r.Snapshot.Series = "esxi"
	r.Kind = catalog.FileAnswer
	r.Values["finalize-token"] = "esxi-finalize-only"
	r.Snapshot.Template.Body = "vmaccepteula\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	b, p, e := Render(r)
	if e != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, e)
	}
	payloads := commandPayloads(string(b))
	if len(payloads) != 3 {
		t.Fatalf("want firstboot,slot,cleanup payloads: %d", len(payloads))
	}
	script := strings.Join(payloads, "\n")
	if strings.Contains(script, r.Values["token"]) || strings.Contains(script, "systemd") || strings.Contains(script, "/var/lib/lober") || strings.Contains(script, "hooks/") {
		t.Fatal("ESXi uses a Linux lifecycle/boot download")
	}
	if !strings.Contains(script, "esxi-finalize-only") {
		t.Fatal("independent finalize missing")
	}
	syntaxCheck(t, script)
	// Native installer owns persistence of these encoded bytes in %firstboot.
	instance := r.Snapshot.Slots[0].Instances[0]
	if !strings.Contains(payloads[1], base64.StdEncoding.EncodeToString([]byte(instance.Body))) {
		t.Fatal("hook bytes rewritten/not embedded")
	}
}

func TestWindowsScriptRequiresToolRoles(t *testing.T) {
	r := windowsRuntimeFixture("pre", runtimeInstance("exit /b 0\r\n", true, "cmd", 3))
	assertRenderProblem(t, r, "render_artifact_missing")
}

func windowsRuntimeFixture(timing catalog.Timing, instances ...model.SnapshotInstance) Request {
	r := runtimeFixture(timing, instances...)
	r.Snapshot.Series = "windows-server"
	r.Snapshot.Family = r.Snapshot.Series.Family()
	r.Snapshot.Format = r.Snapshot.Series.Format()
	return r
}

func windowsRuntimeRoles(r *Request) {
	for _, role := range catalog.RequiredRoles(r.Snapshot.Series) {
		r.Snapshot.ISO.Artifacts[role] = "windows/" + role
	}
	r.Snapshot.ISO.Artifacts["winpe-busybox"] = "windows/busybox64u.exe"
	r.Snapshot.ISO.Artifacts["winpe-curl"] = "windows/curl.exe"
}

func TestRemainingMenuRequiresAllNativeInputs(t *testing.T) {
	for _, tc := range []struct {
		series catalog.Series
		roles  []string
	}{{"sles15", []string{"kernel", "initrd", "repo"}}, {"sles16", []string{"kernel", "initrd", "repo", "live-root"}}, {"windows-server", []string{"wimboot", "bcd", "boot-sdi", "boot-wim", "winpeshl", "winpe-busybox", "winpe-curl"}}} {
		for _, role := range tc.roles {
			t.Run(string(tc.series)+"/"+role, func(t *testing.T) {
				r := menuFixture(tc.series, "bios")
				for _, name := range tc.roles {
					r.Snapshot.ISO.Artifacts[name] = "fixture/" + name
				}
				delete(r.Snapshot.ISO.Artifacts, role)
				assertRenderProblem(t, r, "render_artifact_missing")
			})
		}
	}
}
func TestWindowsEnvironmentUTF16Boundary(t *testing.T) {
	prefix := len(model.EnvName("text") + "=")
	for _, unicode := range []bool{false, true} {
		for _, overflow := range []bool{false, true} {
			r := windowsRuntimeFixture("firstboot", runtimeInstance("exit /b 0\r\n", true, "cmd", 3))
			windowsRuntimeRoles(&r)
			n := 8191 - prefix
			if overflow {
				n++
			}
			value := strings.Repeat("x", n)
			if unicode {
				value = strings.Repeat("🐧", n/2) + strings.Repeat("x", n%2)
			}
			r.Snapshot.Slots[0].Instances[0].Params = map[string]model.SnapshotValue{"text": {Type: "text", Items: []string{value}}}
			b, p, e := Render(r)
			if e != nil {
				t.Fatal(e)
			}
			if overflow {
				if b != nil || len(p) != 1 || p[0].Code != "render_windows_env_limit" {
					t.Fatalf("oversize unicode=%v %v", unicode, p)
				}
			} else if len(b) == 0 || len(p) != 0 {
				t.Fatalf("valid boundary unicode=%v %v", unicode, p)
			}
		}
	}
}

func TestWindowsPowerShellRawUTF8ExecutionCopy(t *testing.T) {
	for _, bom := range []bool{false, true} {
		t.Run(fmt.Sprint(bom), func(t *testing.T) {
			body := "Write-Output '中文 🐧'\r\n"
			if bom {
				body = "\xef\xbb\xbf" + body
			}
			r := windowsRuntimeFixture("firstboot", runtimeInstance(body, true, "powershell", 3))
			windowsRuntimeRoles(&r)
			b, p, e := Render(r)
			if e != nil || len(p) != 0 {
				t.Fatalf("%v %v", p, e)
			}
			native := decodeWindowsRuntime(t, string(b))
			start := strings.Index(native, "prepare_powershell() {")
			if start < 0 {
				t.Fatal("PowerShell UTF8 execution copy absent")
			}
			end := strings.Index(native[start:], "\n}\n")
			if end < 0 {
				t.Fatal("copy helper incomplete")
			}
			helper := native[start : start+end+3]
			dir := t.TempDir()
			raw := filepath.Join(dir, "raw.ps1")
			copy := filepath.Join(dir, "execute.ps1")
			if e := os.WriteFile(raw, []byte(body), 0600); e != nil {
				t.Fatal(e)
			}
			script := "WORK=" + shQuote(dir) + "\n" + helper + "prepare_powershell " + shQuote(raw) + " " + shQuote(copy) + "\n"
			syntaxCheck(t, script)
			cmd := exec.Command("busybox", "sh", "-s")
			cmd.Stdin = strings.NewReader(script)
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("byte preparation %v %s", e, out)
			}
			got, e := os.ReadFile(copy)
			if e != nil {
				t.Fatal(e)
			}
			want := []byte("\xef\xbb\xbf" + strings.TrimPrefix(body, "\xef\xbb\xbf"))
			if !bytes.Equal(got, want) {
				t.Fatalf("BOM bytes %x != %x", got, want)
			}
			original, _ := os.ReadFile(raw)
			if string(original) != body {
				t.Fatal("digest cache bytes changed")
			}
		})
	}
}
func TestWindowsRuntimePreservesNativeStatusThroughCMDFile(t *testing.T) {
	r := windowsRuntimeFixture("firstboot", runtimeInstance("exit /b 1000\r\n", true, "cmd", 3))
	windowsRuntimeRoles(&r)
	b, p, e := Render(r)
	if e != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, e)
	}
	native := decodeWindowsRuntime(t, string(b))
	if !strings.Contains(native, "LOBER_NATIVE_RC_FILE") || !strings.Contains(string(b), "set /p") || !strings.Contains(native, "finish_slot") {
		t.Fatal("native DWORD exit status can be truncated by POSIX shell")
	}
}

func decodeEmbeddedFiles(t *testing.T, body string) string {
	t.Helper()
	pattern := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)' \| base64 -d`)
	var b strings.Builder
	b.WriteString(body)
	for _, match := range pattern.FindAllStringSubmatch(body, -1) {
		decoded, e := base64.StdEncoding.DecodeString(match[1])
		if e != nil {
			t.Fatal(e)
		}
		b.Write(decoded)
	}
	return b.String()
}

func TestPowerShellParametersRespectIntermediateCMDLimit(t *testing.T) {
	r := windowsRuntimeFixture("firstboot", runtimeInstance("Write-Output 'hook'\r\n", true, "powershell", 3))
	windowsRuntimeRoles(&r)
	r.Snapshot.Slots[0].Instances[0].Params = map[string]model.SnapshotValue{"text": {Type: "text", Items: []string{strings.Repeat("x", 8192)}}}
	assertRenderProblem(t, r, "render_windows_env_limit")
}
