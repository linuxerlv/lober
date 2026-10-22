package render

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func agamaFixture(t *testing.T) Request {
	t.Helper()
	r := menuFixture("sles16", "uefi")
	r.Kind = catalog.FileAnswer
	r.Values["finalize-token"] = "finalize-independent"
	template, e := os.ReadFile("testdata/agama.tmpl")
	if e != nil {
		t.Fatal(e)
	}
	r.Snapshot.Template.Body = string(template)
	r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 1, Name: "product", Instances: []model.SnapshotInstance{{Kind: model.ImplFragment, Body: "product: { id: '{{param:product}}' },", Params: map[string]model.SnapshotValue{"product": {Type: "enum", Items: []string{"SLES"}}}}}}, {Seq: 2, Name: "pre", Exec: true, Timing: "pre", Instances: []model.SnapshotInstance{runtimeInstance("printf 'pre'\n", true, "sh", 3)}}, {Seq: 3, Name: "firstboot", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{runtimeInstance("printf 'firstboot'\n", true, "bash", 3)}}}
	return r
}
func windowsAuxFixture() Request {
	r := menuFixture("windows-server", "uefi")
	r.Kind = catalog.FileAux
	r.Values["smb"] = "//server/install"
	r.Values["finalize-token"] = "finalize-independent"
	windowsRuntimeRoles(&r)
	r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 1, Name: "pre", Exec: true, Timing: "pre", Instances: []model.SnapshotInstance{runtimeInstance("@echo off\r\nexit /b 0\r\n", true, "cmd", 3)}}, {Seq: 2, Name: "firstboot", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{runtimeInstance("Write-Output 'firstboot'\r\n", true, "powershell", 3)}}}
	return r
}
func esxiAnswerFixture() Request {
	r := runtimeFixture("firstboot", runtimeInstance("printf 'firstboot'\n", true, "sh", 3))
	r.Snapshot.Format = "esxi-ks"
	r.Snapshot.Family = "esxi"
	r.Snapshot.Series = "esxi"
	r.Kind = catalog.FileAnswer
	r.Values["finalize-token"] = "finalize-independent"
	r.Snapshot.Template.Body = "vmaccepteula\nrootpw --iscrypted {{param:root_password}}\ninstall --firstdisk --overwritevmfs\nnetwork --bootproto=dhcp\nreboot\n%pre --interpreter=busybox\nset -eu\n{{slot:pre@pre}}\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	r.Snapshot.Template.Params["root_password"] = model.SnapshotValue{Type: "word", Items: []string{"$6$loberfixture$s48HMcReZuTl/hNjsbSzMJwS02PdRGxg.c4S/fcvnK13iqHyU.awFlFtXuNgMr03IcKPWRhuoIqMge0LZ.KGX0"}}
	r.Snapshot.Slots = append([]model.SnapshotSlot{{Name: "pre", Seq: 1, Exec: true, Timing: "pre", Instances: []model.SnapshotInstance{runtimeInstance("printf 'pre'\n", true, "sh", 3)}}}, r.Snapshot.Slots...)
	return r
}
func TestTask8NewAnswerAuxScriptGoldens(t *testing.T) {
	posix := runtimeFixture("pre", runtimeInstance("printf 'hook'\n", true, "sh", 3))
	win := windowsRuntimeFixture("firstboot", runtimeInstance("@echo off\r\nexit /b 0\r\n", true, "cmd", 3))
	windowsRuntimeRoles(&win)
	for _, tc := range []struct {
		name string
		r    Request
	}{{"agama-answer.golden", agamaFixture(t)}, {"startnet.cmd.golden", windowsAuxFixture()}, {"esxi-answer.golden", esxiAnswerFixture()}, {"script-posix.golden", posix}, {"script-unattend.cmd.golden", win}} {
		t.Run(tc.name, func(t *testing.T) {
			got, p, e := Render(tc.r)
			if e != nil || len(p) != 0 {
				t.Fatalf("%v %v", p, e)
			}
			want, e := os.ReadFile(filepath.Join("testdata", tc.name))
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("golden mismatch %s", tc.name)
			}
		})
	}
}
func TestAgamaPostStagesOneNativeRunner(t *testing.T) {
	r := agamaFixture(t)
	body, p, e := Render(r)
	if e != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, e)
	}
	if strings.Contains(string(body), "init:") || !strings.Contains(string(body), "chroot: false") {
		t.Fatal("duplicate native firstboot or wrong installer context")
	}
	for _, script := range commandPayloads(string(body)) {
		syntaxCheck(t, script)
		if strings.Contains(script, "/target") || strings.Contains(script, "/mnt/sysroot") {
			t.Fatal("Agama target is /mnt")
		}
	}
}
func TestWindowsAuxStagesExactOwnedPowerShellFiles(t *testing.T) {
	r := windowsAuxFixture()
	b, p, e := Render(r)
	if e != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, e)
	}
	native := decodeWindowsRuntime(t, string(b))
	for _, part := range []catalog.SystemPart{"firstboot", "cleanup"} {
		want, problems := windowsPartScript(expansion{request: r}, part, model.Subject{})
		if len(problems) != 0 {
			t.Fatal(problems)
		}
		line := "printf '%s' " + shQuote(base64.StdEncoding.EncodeToString(want)) + " | base64 -d > \"$OEM/" + windowsPartFile(part) + "\""
		if !strings.Contains(native, line) {
			t.Fatalf("owned %s bytes not staged", part)
		}
	}
	if strings.Contains(native, "powershell.exe") {
		t.Fatal("PowerShell executed in stock WinPE")
	}
}
