package render

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/scan"
)

// Answer bodies own native installer syntax and placement. Linux post-target,
// firstboot staging and system parts run outside chroot, after target mounting:
// kickstart %post --nochroot; preseed/Subiquity late; AutoYaST chrooted=false.
// Windows firstboot/cleanup and firstboot slots share ONE native oobe command
// chain, separated by XML-escaped &&. Separate FirstLogonCommands run concurrently.
func renderAnswer(r Request) ([]byte, []model.Problem, error) {
	e, problems, err := answerExpansion(r)
	if err != nil || len(problems) != 0 {
		return nil, problems, err
	}
	b, problems := e.expand(r.Snapshot.Template.Body, r.Snapshot.Template.Params, model.Subject{Kind: "template", ID: templateRef(r.Snapshot.Template)})
	return b, problems, nil
}

func answerExpansion(r Request) (expansion, []model.Problem, error) {
	e := expansion{request: r, slots: map[string]string{}, parts: map[catalog.SystemPart]string{}}
	switch r.Snapshot.Format {
	case "kickstart", "preseed", "autoinstall", "autoyast", "unattend":
	default:
		return e, nil, errNotImplemented
	}
	// Only prepare referenced parts/slots: a derived author body may need no
	// network credentials. Required-part cardinality belongs to activation.
	scanned, _ := scan.Scan(r.Snapshot.Template.Body)
	var problems []model.Problem
	for _, seg := range scanned.Segments {
		subject := atSegment(model.Subject{Kind: "template", ID: templateRef(r.Snapshot.Template)}, seg)
		if seg.Kind == scan.KindSlot && seg.Slot.Exec {
			if _, ok := e.slots[seg.Slot.Name]; ok {
				continue
			}
			slot, ok := findSlot(r.Snapshot, seg.Slot.Name)
			if !ok || !slot.Exec {
				problems = append(problems, model.NewProblem("render_slot_invalid", subject, seg.Slot.Name))
				continue
			}
			command, p := answerSlot(e, slot, subject)
			problems = append(problems, p...)
			e.slots[slot.Name] = command
		}
		if seg.Kind == scan.KindPart {
			if _, ok := e.parts[seg.Part]; ok {
				continue
			}
			command, p := answerPart(e, seg.Part, subject)
			problems = append(problems, p...)
			e.parts[seg.Part] = command
		}
	}
	return e, problems, nil
}

// These names are a machine route/local staging contract shared with FileScript
// and Windows FileAux. Seq, not the author-visible slot name, identifies routes.
func slotFile(format catalog.Format, slot model.SnapshotSlot) string {
	ext := "sh"
	if format == "unattend" {
		ext = "cmd"
	}
	return "script-" + strconv.Itoa(slot.Seq) + "." + ext
}

func hookFile(instance model.SnapshotInstance) string {
	ext := "sh"
	switch instance.Interpreter {
	case "cmd":
		ext = "cmd"
	case "powershell":
		ext = "ps1"
	}
	return "hook-" + instance.Digest + "." + ext
}

func answerSlot(e expansion, slot model.SnapshotSlot, subject model.Subject) (string, []model.Problem) {
	if e.request.Snapshot.Format == "unattend" {
		if slot.Timing == "firstboot" {
			return "cmd /d /c %SystemDrive%/ProgramData/lober/" + slotFile("unattend", slot), nil
		}
		// WinPE pre slots are injected alongside startnet by FileAux, never
		// downloaded by an assumed stock WinPE HTTP client.
		return "cmd /d /c X:/lober/" + slotFile("unattend", slot), nil
	}
	values, p := answerValues(e, subject, "gateway", "task", "token")
	if len(p) != 0 {
		return "", p
	}
	root := targetRoot(e.request.Snapshot.Format)
	local := "/tmp/" + slotFile(e.request.Snapshot.Format, slot)
	if slot.Timing == "post-target" || slot.Timing == "firstboot" {
		local = root + "/var/lib/lober/" + slotFile(e.request.Snapshot.Format, slot)
	}
	var b strings.Builder
	b.WriteString("set -eu\numask 077\nmkdir -p " + shQuote(parentPath(local)) + "\n")
	b.WriteString(download(e.request.Snapshot.Format, catalog.AnswerURL(values[0], values[1], values[2], "scripts/"+strconv.Itoa(slot.Seq)+".sh"), local))
	if slot.Timing == "firstboot" {
		for _, instance := range slot.Instances {
			path := root + "/var/lib/lober/" + hookFile(instance)
			fetch := download(e.request.Snapshot.Format, catalog.AnswerURL(values[0], values[1], values[2], "hooks/"+instance.Digest), path)
			if instance.Required {
				b.WriteString(fetch)
			} else {
				b.WriteString("(\n" + fetch + ") || :\n")
			}
		}
		// Staging only: no interpreter runs firstboot payloads in the installer.
	} else if slot.Timing == "post-target" {
		b.WriteString("chroot " + shQuote(root) + " sh " + shQuote(strings.TrimPrefix(local, root)) + "\n")
	} else {
		b.WriteString("sh " + shQuote(local) + "\n")
	}
	return encodedShell(b.String()), nil
}

func answerPart(e expansion, part catalog.SystemPart, subject model.Subject) (string, []model.Problem) {
	if e.request.Snapshot.Format == "unattend" {
		return windowsPart(e, part, subject)
	}
	root := targetRoot(e.request.Snapshot.Format)
	switch part {
	case "syslog":
		values, p := answerValues(e, subject, "syslog-addr")
		if len(p) != 0 {
			return "", p
		}
		pos := strings.LastIndex(values[0], ":")
		if pos < 1 {
			return "", []model.Problem{model.NewProblem("render_line_invalid", subject, "syslog-addr")}
		}
		host, port := values[0][:pos], values[0][pos+1:]
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", []model.Problem{model.NewProblem("render_line_invalid", subject, "syslog-addr")}
		}
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
		return "logging --host=" + host + " --port=" + port + " --level=info", nil
	case "firstboot":
		values, p := answerValues(e, subject, "finalize-token")
		if len(p) != 0 {
			return "", p
		}
		gateway, p := answerValues(e, subject, "gateway", "task")
		if len(p) != 0 {
			return "", p
		}
		var runner strings.Builder
		runner.WriteString("#!/bin/sh\nset -eu\n")
		runner.WriteString(finalizeSignal(gateway[0], gateway[1], "booted"))
		runner.WriteString(finalizeSignal(gateway[0], gateway[1], "init-start"))
		for _, slot := range e.request.Snapshot.Slots {
			if slot.Exec && slot.Timing == "firstboot" {
				runner.WriteString("sh /var/lib/lober/" + slotFile(e.request.Snapshot.Format, slot) + "\n")
			}
		}
		runner.WriteString("sh /var/lib/lober/cleanup.sh\n")
		unit := "[Unit]\nDescription=Lober first boot\nAfter=network-online.target\nWants=network-online.target\nConditionPathExists=/var/lib/lober/firstboot.pending\n[Service]\nType=oneshot\nExecStart=/bin/sh /var/lib/lober/firstboot.sh\n[Install]\nWantedBy=multi-user.target\n"
		body := "set -eu\numask 077\nmkdir -p " + shQuote(root+"/var/lib/lober") + " " + shQuote(root+"/etc/systemd/system/multi-user.target.wants") + "\n"
		body += "printf '%s' " + shQuote(values[0]) + " > " + shQuote(root+"/var/lib/lober/finalize.token") + "\n"
		body += writeHeredoc(root+"/var/lib/lober/firstboot.sh", "LOBER_FIRSTBOOT_EOF", runner.String())
		body += writeHeredoc(root+"/etc/systemd/system/lober-firstboot.service", "LOBER_UNIT_EOF", unit)
		body += "ln -sf ../lober-firstboot.service " + shQuote(root+"/etc/systemd/system/multi-user.target.wants/lober-firstboot.service") + "\ntouch " + shQuote(root+"/var/lib/lober/firstboot.pending") + "\n"
		return encodedShell(body), nil
	case "cleanup":
		values, p := answerValues(e, subject, "gateway", "task")
		if len(p) != 0 {
			return "", p
		}
		cleanup := "#!/bin/sh\nset -eu\n" + finalizeSignal(values[0], values[1], "init-done") + finalizeSignal(values[0], values[1], "ready")
		cleanup += "rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh\n"
		// Installer copies may retain the answer's passwords and boot credential.
		cleanup += "rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml\n"
		return encodedShell("set -eu\numask 077\nmkdir -p " + shQuote(root+"/var/lib/lober") + "\n" + writeHeredoc(root+"/var/lib/lober/cleanup.sh", "LOBER_CLEANUP_EOF", cleanup)), nil
	}
	return "", []model.Problem{model.NewProblem("render_replacement_missing", subject, part)}
}

func windowsPart(e expansion, part catalog.SystemPart, subject model.Subject) (string, []model.Problem) {
	_, p := windowsPartScript(e, part, subject)
	if len(p) != 0 {
		return "", p
	}
	return "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/" + windowsPartFile(part), nil
}

// FileAux stages these concrete bytes into its local OEM $1/ProgramData/lober
// tree BEFORE setup /m copies them to the target. UTF-8 BOM preserves all text
// on Windows PowerShell 5.1. The answer uses compact -File paths to keep its
// single native command chain within Microsoft's 1024-character field limit.
func windowsPartScript(e expansion, part catalog.SystemPart, subject model.Subject) ([]byte, []model.Problem) {
	values, p := answerValues(e, subject, "gateway", "task")
	if len(p) != 0 {
		return nil, p
	}
	if part != "firstboot" && part != "cleanup" {
		return nil, []model.Problem{model.NewProblem("render_replacement_missing", subject, part)}
	}
	body := "$ErrorActionPreference='Stop'\n$dir=Join-Path $env:SystemDrive 'ProgramData/lober'\n$token=(Get-Content -Raw (Join-Path $dir 'finalize.token')).Trim()\n"
	phases := []string{"booted", "init-start"}
	if part == "cleanup" {
		phases = []string{"init-done", "ready"}
	}
	for _, phase := range phases {
		// Token is read locally after reboot; the install token never enters this payload.
		base := catalog.SignalURL(values[0], values[1], "TOKEN", phase)
		before, after := tokenURLParts(base)
		body += "try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri (" + psQuote(before) + "+$token+" + psQuote(after) + ") -Body '' | Out-Null } catch {}\n"
	}
	if part == "cleanup" {
		body += "Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $dir 'finalize.token'),(Join-Path $dir 'script-*.cmd'),(Join-Path $dir 'hook-*'),(Join-Path $dir 'firstboot.ps1'),(Join-Path $dir 'cleanup.ps1')\n"
		body += "Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $env:WINDIR 'Panther/Unattend.xml'),(Join-Path $env:WINDIR 'Panther/Unattend/Unattend.xml')\n"
	}
	return append([]byte{0xef, 0xbb, 0xbf}, []byte(body+"exit 0\n")...), nil
}

func windowsPartFile(part catalog.SystemPart) string { return string(part) + ".ps1" }

func answerValues(e expansion, subject model.Subject, keys ...catalog.SystemValue) ([]string, []model.Problem) {
	values := make([]string, len(keys))
	var problems []model.Problem
	for i, key := range keys {
		value, p := e.systemValue(key, subject)
		values[i] = value
		if p != nil {
			problems = append(problems, *p)
		}
	}
	return values, problems
}

func targetRoot(format catalog.Format) string {
	switch format {
	case "kickstart":
		return "/mnt/sysroot"
	case "autoyast":
		return "/mnt"
	default:
		return "/target"
	}
}

func download(format catalog.Format, address, path string) string {
	if format == "preseed" {
		return "wget -O " + shQuote(path) + " " + shQuote(address) + "\n"
	}
	return "curl -fsS --retry 3 -o " + shQuote(path) + " " + shQuote(address) + "\n"
}

// The native runner gets its finalize credential from a protected local file.
// Telemetry transport failure never aborts the install/firstboot script.
func finalizeSignal(gateway, task, phase string) string {
	address := catalog.SignalURL(gateway, task, "TOKEN", phase)
	before, after := tokenURLParts(address)
	return "TOKEN=$(cat /var/lib/lober/finalize.token)\ncurl -fsS -m 10 -X POST " + shQuote(before) + "\"$TOKEN\"" + shQuote(after) + " --data-binary '' >/dev/null 2>&1 || :\n"
}

func tokenURLParts(address string) (string, string) {
	i := strings.LastIndex(address, "/TOKEN/") + 1
	return address[:i], address[i+len("TOKEN"):]
}

func parentPath(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "."
	}
	return path[:i]
}
func shQuote(text string) string { return "'" + strings.ReplaceAll(text, "'", "'\"'\"'") + "'" }
func psQuote(text string) string { return "'" + strings.ReplaceAll(text, "'", "''") + "'" }
func writeHeredoc(path, marker, body string) string {
	return "cat > " + shQuote(path) + " <<'" + marker + "'\n" + body + marker + "\n"
}
func encodedShell(body string) string {
	return "echo " + base64.StdEncoding.EncodeToString([]byte(body)) + " | base64 -d | sh"
}
