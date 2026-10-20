package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

var answerFormats = []catalog.Format{"kickstart", "preseed", "autoinstall", "autoyast", "unattend"}

// Handwritten snapshots exercise the renderer independently of builtin content.
func answerFixture(t *testing.T, format catalog.Format, full bool, firmware catalog.Firmware) Request {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", string(format)+".tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	r := Request{Kind: catalog.FileAnswer, Values: map[catalog.SystemValue]string{
		"gateway": "http://gw.internal", "artifacts": "http://ng.internal/artifacts", "task": "task-42", "token": "install-tok-fixed", "finalize-token": "finalize-tok-fixed", "syslog-addr": "10.10.0.1:514", "sn": "SN-FIXED-0001", "smb": "//files.internal/windows",
	}, Snapshot: model.Snapshot{Schema: model.SnapshotSchema, Family: "linux", Format: format, Host: model.SnapshotHost{SN: "SN-FIXED-0001", Firmware: firmware}, ISO: model.SnapshotISO{ID: "iso-1", Artifacts: map[string]string{"repo": "fixture/repo/", "iso": "fixture/image.iso"}}, Template: model.SnapshotTemplate{ID: "fixture", Version: 1, Body: string(body), Params: map[string]model.SnapshotValue{"hostname": {Type: "hostname", Items: []string{"web-01"}}}}}}
	if format == "unattend" {
		r.Snapshot.Family = "windows"
	}
	partition, err := os.ReadFile(filepath.Join("testdata", string(format)+"-"+string(firmware)+".fragment"))
	if err != nil {
		t.Fatal(err)
	}
	r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 1, Name: "partition", Instances: []model.SnapshotInstance{{Kind: model.ImplFragment, Body: string(partition)}}}}
	for _, timing := range format.Timings() {
		s := model.SnapshotSlot{Seq: len(r.Snapshot.Slots) + 1, Name: string(timing), Exec: true, Timing: timing}
		if full {
			body := "printf hook-ok\n"
			interp := catalog.Interpreter("sh")
			if format == "unattend" {
				body = "@echo hook-ok\r\n"
				interp = "cmd"
			}
			digest := sha256.Sum256([]byte(body))
			s.Instances = []model.SnapshotInstance{{HookID: "fixture-hook", HookVersion: 1, Kind: model.ImplScript, Body: body, Digest: hex.EncodeToString(digest[:]), Interpreter: interp, Required: true, TimeoutSec: 30}}
		}
		r.Snapshot.Slots = append(r.Snapshot.Slots, s)
	}
	return r
}

func TestAnswerGolden(t *testing.T) {
	for _, format := range answerFormats {
		for _, firmware := range []catalog.Firmware{"bios", "uefi"} {
			for _, mode := range []string{"full", "minimal"} {
				name := string(format) + "-" + mode + "-" + string(firmware)
				t.Run(name, func(t *testing.T) {
					// Deliberately read the missing golden first for the specified RED phase.
					path := filepath.Join("testdata", name+".golden")
					want, err := os.ReadFile(path)
					if err != nil && os.Getenv("LOBER_UPDATE_GOLDEN") != "1" {
						t.Fatal(err)
					}
					r := answerFixture(t, format, mode == "full", firmware)
					got, p, err := Render(r)
					if err != nil || len(p) != 0 {
						t.Fatalf("render: %v %v", p, err)
					}
					if os.Getenv("LOBER_UPDATE_GOLDEN") == "1" {
						if err := os.WriteFile(path, got, 0644); err != nil {
							t.Fatal(err)
						}
						return
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("golden mismatch %s", path)
					}
					again, p, err := Render(r)
					if err != nil || len(p) != 0 || !bytes.Equal(got, again) {
						t.Fatal("nondeterministic answer")
					}
				})
			}
		}
	}
}

func TestKickstartInstallerPayloadsDoNotRequireBase64(t *testing.T) {
	r := answerFixture(t, "kickstart", true, "bios")
	got, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("render: %v %v", problems, err)
	}
	answer := string(got)
	if strings.Contains(answer, "| base64 -d | sh") {
		t.Fatal("Kickstart installer payload still requires base64, which is absent from the minimal AlmaLinux installer")
	}
	if !strings.Contains(answer, "| /usr/libexec/platform-python -m base64 -d | sh") {
		t.Fatal("Kickstart installer payload does not use the Anaconda-provided Python decoder")
	}
}

func TestKickstartPythonPayloadPreservesShellBytes(t *testing.T) {
	want := "O'Brien 50% backslash \\ unicode 宁波\n"
	command := installerShellPayload("kickstart", want)
	if strings.Contains(command, "\n") {
		t.Fatal("Kickstart payload wrapper contains a raw newline")
	}
	if !strings.Contains(command, " | /usr/libexec/platform-python -m base64 -d") {
		t.Fatal("Kickstart payload wrapper does not decode through Anaconda's Python runtime")
	}
	command = strings.TrimSuffix(command, " | sh")
	command = strings.Replace(command, "/usr/libexec/platform-python", "python", 1)
	got, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		t.Fatalf("Anaconda Python decoder wrapper: %v", err)
	}
	if !bytes.Equal(got, []byte(want)) {
		t.Fatalf("POSIX printf changed payload bytes: got %q, want %q", got, want)
	}
}

func TestKickstartSyslogOmitsUnsupportedLevel(t *testing.T) {
	r := answerFixture(t, "kickstart", false, "bios")
	e, problems, err := answerExpansion(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("answer expansion: %v %v", problems, err)
	}
	if got, want := e.parts["syslog"], "logging --host=10.10.0.1 --port=514"; got != want {
		t.Fatalf("Kickstart syslog directive = %q, want %q", got, want)
	}
}

func commandPayloads(text string) []string {
	var out []string
	pattern := regexp.MustCompile(`echo ([A-Za-z0-9+/=]+) \| (?:base64 -d|/usr/libexec/platform-python -m base64 -d) \| sh`)
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		b, err := base64.StdEncoding.DecodeString(match[1])
		if err == nil {
			out = append(out, string(b))
		}
	}
	return out
}

func TestAnswerLinuxScaffolds(t *testing.T) {
	for _, format := range answerFormats[:4] {
		t.Run(string(format), func(t *testing.T) {
			r := answerFixture(t, format, true, "uefi")
			got, p, err := Render(r)
			if err != nil || len(p) != 0 {
				t.Fatalf("%v %v", p, err)
			}
			payloads := commandPayloads(string(got))
			if len(payloads) < 5 {
				t.Fatal("missing inspectable scaffold payloads")
			}
			all := strings.Join(payloads, "\n")
			for _, want := range []string{"finalize-tok-fixed", "/var/lib/lober/finalize.token", "hooks/", "scripts/", "lober-firstboot.service", "ConditionPathExists=", "set -eu"} {
				if !strings.Contains(all, want) {
					t.Fatalf("missing %s", want)
				}
			}
			if strings.Contains(all, "lober run") {
				t.Fatal("fictional machine-side lober command")
			}
			for _, payload := range payloads {
				if strings.Contains(payload, "[Unit]") {
					// The staged native runner must never retain the install token.
					start := strings.Index(payload, "<<'LOBER_FIRSTBOOT_EOF'\n")
					if start < 0 {
						t.Fatal("runner not staged")
					}
					start += len("<<'LOBER_FIRSTBOOT_EOF'\n")
					end := strings.Index(payload[start:], "\nLOBER_FIRSTBOOT_EOF")
					if end < 0 {
						t.Fatal("runner terminator absent")
					}
					runner := payload[start : start+end]
					if strings.Contains(runner, "install-tok-fixed") || strings.Contains(runner, "/v1/answer/") || !strings.Contains(runner, "script-") {
						t.Fatalf("firstboot must execute local staged scripts: %s", runner)
					}
				}
				for _, env := range []string{"LOBER_TEST_BUSYBOX", "LOBER_TEST_BASH"} {
					tool := os.Getenv(env)
					if tool == "" {
						t.Fatalf("%s required", env)
					}
					args := []string{"-n"}
					if env == "LOBER_TEST_BUSYBOX" {
						args = []string{"sh", "-n"}
					}
					cmd := exec.Command(tool, args...)
					cmd.Stdin = strings.NewReader(payload)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("%s syntax: %v %s", env, err, out)
					}
				}
			}
		})
	}
}

func TestAnswerAddressesAndOneLine(t *testing.T) {
	for _, format := range answerFormats {
		t.Run(string(format), func(t *testing.T) {
			r := answerFixture(t, format, true, "bios")
			e, p, err := answerExpansion(r)
			if err != nil || len(p) != 0 {
				t.Fatalf("%v %v", p, err)
			}
			for _, part := range format.RequiredParts() {
				if e.parts[part] == "" {
					t.Fatalf("missing required %s", part)
				}
			}
			for name, line := range e.slots {
				if !oneLine(line) {
					t.Fatalf("slot %s unsafe %q", name, line)
				}
			}
			for name, line := range e.parts {
				if !oneLine(line) {
					t.Fatalf("part %s unsafe %q", name, line)
				}
			}
			all := strings.Join(commandPayloads(strings.Join(mapStrings(e.slots), "\n")+"\n"+strings.Join(partStrings(e.parts), "\n")), "\n")
			if format == "unattend" {
				return
			} // Windows answer executes staged local files; auxiliary owns downloads.
			matches := regexp.MustCompile(`http://gw\.internal/v1/answer/[^\s'";]+`).FindAllString(all, -1)
			if len(matches) == 0 {
				t.Fatal("no machine URLs")
			}
			for _, address := range matches {
				u, err := url.Parse(address)
				if err != nil {
					t.Fatal(err)
				}
				segs := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
				if len(segs) != 6 || segs[0] != "v1" || segs[1] != "answer" {
					t.Fatalf("bad route %s", address)
				}
				for i := range segs {
					segs[i], err = url.PathUnescape(segs[i])
					if err != nil {
						t.Fatal(err)
					}
				}
				if segs[2] != "task-42" || segs[3] != "install-tok-fixed" || (segs[4] != "scripts" && segs[4] != "hooks") {
					t.Fatalf("wrong address %v", segs)
				}
				if strings.Contains(segs[5], "firstboot") {
					t.Fatal("slot route must use ASCII Seq")
				}
			}
		})
	}
}

func TestAnswerFirstbootStagesWithoutExecuting(t *testing.T) {
	r := answerFixture(t, "kickstart", true, "bios")
	e, p, err := answerExpansion(r)
	if err != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, err)
	}
	payload := commandPayloads(e.slots["firstboot"])[0]
	dir := filepath.ToSlash(t.TempDir())
	payload = strings.ReplaceAll(payload, "/mnt/sysroot", dir)
	// A real shell runs the generated staging scaffold. The download transport
	// is replaced by a local function writing a payload that must NOT execute.
	preamble := "curl() { while [ $# -gt 0 ]; do if [ \"$1\" = -o ]; then shift; destination=$1; fi; shift; done; printf 'exit 71\\n' > \"$destination\"; }\n"
	tool := os.Getenv("LOBER_TEST_BASH")
	if tool == "" {
		t.Fatal("LOBER_TEST_BASH required")
	}
	cmd := exec.Command(tool, "-s")
	cmd.Stdin = strings.NewReader(preamble + payload)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("staging executed hook or failed: %v %s", err, out)
	}
	for _, file := range []string{slotFile("kickstart", r.Snapshot.Slots[4]), hookFile(r.Snapshot.Slots[4].Instances[0])} {
		if got, err := os.ReadFile(filepath.Join(dir, "var/lib/lober", file)); err != nil || string(got) != "exit 71\n" {
			t.Fatalf("not staged %s: %s %v", file, got, err)
		}
	}
	for _, part := range []catalog.SystemPart{"firstboot", "cleanup"} {
		stage := strings.ReplaceAll(commandPayloads(e.parts[part])[0], "/mnt/sysroot", dir)
		cmd := exec.Command(tool, "-s")
		cmd.Stdin = strings.NewReader("curl() { return 0; }\n" + stage)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("stage %s: %v %s", part, err, out)
		}
	}
	info, err := os.Stat(filepath.Join(dir, "var/lib/lober/finalize.token"))
	if err != nil || info.Size() != int64(len("finalize-tok-fixed")) {
		t.Fatalf("finalize file %v %v", info, err)
	}
	runner, err := os.ReadFile(filepath.Join(dir, "var/lib/lober/firstboot.sh"))
	if err != nil {
		t.Fatal(err)
	}
	native := strings.ReplaceAll(string(runner), "/var/lib/lober", dir+"/var/lib/lober")
	cmd = exec.Command(tool, "-s")
	cmd.Stdin = strings.NewReader("curl() { return 1; }\n" + native)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("required wrapper failure was ignored: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "var/lib/lober/finalize.token")); err != nil {
		t.Fatal("cleanup ran after failed wrapper")
	}
	if _, err := os.Stat(filepath.Join(dir, "etc/systemd/system/lober-firstboot.service")); err != nil {
		t.Fatal("native unit was not staged")
	}
}

func TestPowerShellStagedBytesNonBMP(t *testing.T) {
	r := answerFixture(t, "unattend", true, "uefi")
	r.Values["gateway"] += "/机器-🐧"
	data, p := windowsPartScript(expansion{request: r}, "firstboot", model.Subject{})
	if len(p) != 0 || !bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) || !strings.Contains(string(data), "机器-🐧/v1/signal/") {
		t.Fatalf("staged UTF8 BOM payload lost Unicode: %q %v", data, p)
	}
	// FileAux transports script bytes through base64 on WinPE, without asking
	// stock WinPE to run PowerShell. Assert the exact staged file roundtrip.
	again, err := base64.StdEncoding.DecodeString(base64.StdEncoding.EncodeToString(data))
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("staging byte corruption %v", err)
	}
}

func mapStrings(m map[string]string) []string {
	var a []string
	for _, s := range m {
		a = append(a, s)
	}
	return a
}
func partStrings(m map[catalog.SystemPart]string) []string {
	var a []string
	for _, s := range m {
		a = append(a, s)
	}
	return a
}

func TestAnswerFirmwareAndKickstartRegressions(t *testing.T) {
	for _, firmware := range []catalog.Firmware{"bios", "uefi"} {
		r := answerFixture(t, "kickstart", false, firmware)
		got, p, err := Render(r)
		if err != nil || len(p) != 0 {
			t.Fatalf("%v %v", p, err)
		}
		text := string(got)
		if !strings.Contains(text, "url --url=http://ng.internal/artifacts/fixture/repo/") {
			t.Fatal("743e8af installation source")
		}
		if !strings.Contains(text, "cmdline\n") || strings.Contains(text, "\ntext\n") {
			t.Fatal("9a252db noninteractive")
		}
		if strings.Index(text, "%include") < strings.Index(text, "%pre") {
			t.Fatal("2d59bbb include precedes pre")
		}
		if start, end := strings.Index(text, "LOBER_PARTITION_EOF'\n"), strings.Index(text, "\nLOBER_PARTITION_EOF\n"); start < 0 || end < start || !strings.Contains(text[start:end], "clearpart") {
			t.Fatal("pre must write real include contents")
		}
		if strings.Contains(text, "--fstype=biosboot") != (firmware == "bios") {
			t.Fatal("83584d7 BIOS GPT biosboot")
		}
		if strings.Contains(text, "--fstype=efi") != (firmware == "uefi") {
			t.Fatal("UEFI ESP")
		}
	}
}

func TestAnswerWindowsNativeFirstboot(t *testing.T) {
	r := answerFixture(t, "unattend", true, "uefi")
	got, p, err := Render(r)
	if err != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, err)
	}
	decoder := xml.NewDecoder(bytes.NewReader(got))
	for {
		_, err := decoder.Token()
		if err != nil {
			if err.Error() != "EOF" {
				t.Fatal(err)
			}
			break
		}
	}
	text := string(got)
	if strings.Count(text, "<SynchronousCommand ") != 1 {
		t.Fatal("Windows needs one ordered native firstboot command")
	}
	pe := text[:strings.Index(text, `<settings pass="oobeSystem">`)]
	if strings.Contains(pe, "ProgramData/lober/script-") || strings.Contains(pe, "powershell") {
		t.Fatal("firstboot executing in WinPE")
	}
	commands := firstLogonCommandLines(t, got)
	want := "cmd /d /c powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/firstboot.ps1 && cmd /d /c %SystemDrive%/ProgramData/lober/script-3.cmd && powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/cleanup.ps1"
	if len(commands) != 1 || commands[0] != want {
		t.Fatalf("native single ordered chain %q", commands)
	}
	if strings.Contains(text, "install-tok-fixed") || strings.Contains(text, "-EncodedCommand") {
		t.Fatal("answer must use compact native staged-file paths")
	}
}

func TestAnswerWindowsStagedPartsGolden(t *testing.T) {
	r := answerFixture(t, "unattend", true, "uefi")
	for _, part := range []catalog.SystemPart{"firstboot", "cleanup"} {
		t.Run(string(part), func(t *testing.T) {
			got, p := windowsPartScript(expansion{request: r}, part, model.Subject{})
			if len(p) != 0 {
				t.Fatal(p)
			}
			path := filepath.Join("testdata", "unattend-"+windowsPartFile(part)+".golden")
			if os.Getenv("LOBER_UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, got, 0644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("native staged %s changed", part)
			}
			if !bytes.HasPrefix(got, []byte{0xef, 0xbb, 0xbf}) || strings.Contains(string(got), "install-tok-fixed") {
				t.Fatal("invalid native staged bytes/credential")
			}
			phase := "/booted"
			if part == "cleanup" {
				phase = "/ready"
			}
			if !strings.Contains(string(got), phase) || !strings.Contains(string(got), "finalize.token") {
				t.Fatal("native firstboot phase/credential missing")
			}
		})
	}
}

func firstLogonCommandLines(t *testing.T, body []byte) []string {
	t.Helper()
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var out []string
	for {
		token, err := decoder.Token()
		if err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "CommandLine" {
			var command string
			if err := decoder.DecodeElement(&command, &start); err != nil {
				t.Fatal(err)
			}
			out = append(out, command)
		}
	}
	return out
}

func TestAnswerWindowsNativeCommandLineLimit(t *testing.T) {
	for _, firmware := range []catalog.Firmware{"bios", "uefi"} {
		for _, full := range []bool{false, true} {
			t.Run(string(firmware)+"-"+map[bool]string{false: "minimal", true: "full"}[full], func(t *testing.T) {
				got, p, err := Render(answerFixture(t, "unattend", full, firmware))
				if err != nil || len(p) != 0 {
					t.Fatalf("%v %v", p, err)
				}
				commands := firstLogonCommandLines(t, got)
				if len(commands) != 1 {
					t.Fatalf("native command count %d", len(commands))
				}
				if n := len(utf16.Encode([]rune(commands[0]))); n > 1024 {
					t.Fatalf("XML-decoded CommandLine %d characters exceeds native maximum 1024", n)
				}
			})
		}
	}
}

func TestAnswerWindowsEightSlotsCompactChain(t *testing.T) {
	r := answerFixture(t, "unattend", true, "uefi")
	var placeholders []string
	for i := 0; i < 8; i++ {
		slot := r.Snapshot.Slots[2]
		slot.Seq = i + 3
		slot.Name = "firstboot-" + string(rune('a'+i))
		if i == 0 {
			r.Snapshot.Slots[2] = slot
		} else {
			r.Snapshot.Slots = append(r.Snapshot.Slots, slot)
		}
		placeholders = append(placeholders, "{{slot:"+slot.Name+"@firstboot}}")
	}
	r.Snapshot.Template.Body = strings.Replace(r.Snapshot.Template.Body, "{{slot:firstboot@firstboot}}", strings.Join(placeholders, " &amp;&amp; "), 1)
	got, p, err := Render(r)
	if err != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, err)
	}
	commands := firstLogonCommandLines(t, got)
	if len(commands) != 1 || len(utf16.Encode([]rune(commands[0]))) > 1024 || strings.Count(commands[0], " && ") != 9 || !strings.HasSuffix(commands[0], "/cleanup.ps1") {
		t.Fatalf("eight slots must remain one bounded success-only chain: %q", commands)
	}
}

func TestAnswerMissingFinalizeValue(t *testing.T) {
	r := answerFixture(t, "kickstart", false, "bios")
	delete(r.Values, "finalize-token")
	got, p, err := Render(r)
	if got != nil || err != nil || len(p) != 1 || p[0].Code != "render_system_value_missing" {
		t.Fatalf("%q %v %v", got, p, err)
	}
	if !catalog.SystemValue("finalize-token").Valid() {
		t.Fatal("private value absent from catalog")
	}
	for _, format := range answerFormats {
		for _, value := range format.SystemValues() {
			if value == "finalize-token" {
				t.Fatal("finalize token exposed to authors")
			}
		}
	}
}

func TestAnswerAllRenderedURLKinds(t *testing.T) {
	for _, format := range answerFormats {
		t.Run(string(format), func(t *testing.T) {
			r := answerFixture(t, format, true, "uefi")
			got, p, err := Render(r)
			if err != nil || len(p) != 0 {
				t.Fatalf("%v %v", p, err)
			}
			all := string(got) + "\n" + strings.Join(commandPayloads(string(got)), "\n")
			if format == "unattend" {
				for _, part := range []catalog.SystemPart{"firstboot", "cleanup"} {
					data, p := windowsPartScript(expansion{request: r}, part, model.Subject{})
					if len(p) != 0 {
						t.Fatal(p)
					}
					all += "\n" + string(data)
				}
			}
			// Join literal URL segments with the independently issued runtime value.
			all = strings.ReplaceAll(all, "'\"$TOKEN\"'", "finalize-tok-fixed")
			all = strings.ReplaceAll(all, "'+$token+'", "finalize-tok-fixed")
			matches := regexp.MustCompile(`http://(?:gw|ng)\.internal/[^\s'"<)]+`).FindAllString(all, -1)
			if len(matches) == 0 {
				t.Fatal("no decoded URLs")
			}
			for _, address := range matches {
				u, err := url.Parse(address)
				if err != nil || u.RawQuery != "" || u.Fragment != "" {
					t.Fatalf("bad URL %s %v", address, err)
				}
				segments := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
				for i := range segments {
					segments[i], err = url.PathUnescape(segments[i])
					if err != nil {
						t.Fatal(err)
					}
				}
				if u.Host == "ng.internal" {
					if strings.Join(segments, "/") != "artifacts/fixture/repo" {
						t.Fatal(address)
					}
					continue
				}
				// The durable queue joins this catalog prefix with its protected
				// token and relative request at runtime. Real HTTP recovery tests
				// check the assembled route and credential, not this string fragment.
				if len(segments) == 3 && segments[0] == "v1" && segments[1] == "signal" && segments[2] == "task-42" && strings.HasSuffix(address, "/") {
					continue
				}
				if len(segments) < 5 || segments[0] != "v1" || segments[2] != "task-42" {
					t.Fatal(address)
				}
				switch segments[1] {
				case "answer":
					if len(segments) != 6 || segments[3] != "install-tok-fixed" || (segments[4] != "scripts" && segments[4] != "hooks") {
						t.Fatal(address)
					}
				case "signal":
					credential := "finalize-tok-fixed"
					if len(segments) == 5 && segments[4] == "installed" {
						credential = "install-tok-fixed"
					}
					if len(segments) != 5 || segments[3] != credential {
						t.Fatal(address)
					}
				default:
					t.Fatal(address)
				}
			}
		})
	}
}
