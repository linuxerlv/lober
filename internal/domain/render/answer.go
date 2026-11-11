package render

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/scan"
)

// Answer bodies own native installer syntax and placement. Linux post-target,
// firstboot staging and system parts run outside chroot, after target mounting:
// kickstart %post --nochroot; preseed/Subiquity late.
// Windows firstboot/cleanup and firstboot slots share ONE native oobe command
// chain, separated by XML-escaped &&. Separate FirstLogonCommands run concurrently.
func (f installer) renderAnswer(r Request) ([]byte, []model.Problem, error) {
	e, problems := f.answerExpansion(r)
	if len(problems) != 0 {
		return nil, problems, nil
	}
	if f.finishAnswer != nil {
		return f.finishAnswer(e)
	}
	return expandAnswerTemplate(e)
}

func expandAnswerTemplate(e expansion) ([]byte, []model.Problem, error) {
	t := e.request.Snapshot.Template
	b, problems := e.expand(t.Body, t.Params, model.Subject{Kind: "template", ID: templateRef(t)})
	return b, problems, nil
}

func (f installer) answerExpansion(r Request) (expansion, []model.Problem) {
	e := expansion{request: r, slots: map[string]string{}, parts: map[catalog.SystemPart]string{}}
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
			command, p := f.slot(e, slot, subject)
			problems = append(problems, p...)
			e.slots[slot.Name] = command
		}
		if seg.Kind == scan.KindPart {
			if _, ok := e.parts[seg.Part]; ok {
				continue
			}
			command, p := f.part(e, seg.Part, subject)
			problems = append(problems, p...)
			e.parts[seg.Part] = command
		}
	}
	return e, problems
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

func shellAnswerSlot(e expansion, slot model.SnapshotSlot, subject model.Subject) (string, []model.Problem) {
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
	path, _ := catalog.DeliveryScriptPath(e.request.Snapshot.Format, slot.Seq)
	b.WriteString(download(e.request.Snapshot.Format, catalog.AnswerURL(values[0], values[1], values[2], path), local))
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
	return installerShellPayload(e.request.Snapshot.Format, b.String()), nil
}

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
	default:
		return "/target"
	}
}

func download(format catalog.Format, address, path string) string {
	if format == "preseed" || format == "esxi-ks" {
		return "wget -O " + shQuote(path) + " " + shQuote(address) + "\n"
	}
	return "curl -fsS --retry 3 -o " + shQuote(path) + " " + shQuote(address) + "\n"
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
func writeHeredoc(path, marker, body string) string {
	return "cat > " + shQuote(path) + " <<'" + marker + "'\n" + body + marker + "\n"
}
func installerShellPayload(format catalog.Format, body string) string {
	switch format {
	case "kickstart":
		return "echo " + base64.StdEncoding.EncodeToString([]byte(body)) + " | /usr/libexec/platform-python -m base64 -d | sh"
	case "esxi-ks":
		return esxiEncodedShell(body)
	default:
		return encodedShell(body)
	}
}

func encodedShell(body string) string {
	return "echo " + base64.StdEncoding.EncodeToString([]byte(body)) + " | base64 -d | sh"
}
