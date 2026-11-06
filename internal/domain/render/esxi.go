package render

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func esxiMenu(m menuInput) string {
	config, _ := m.answerURL(catalog.FileAux)
	return fmt.Sprintf("kernel %s -c %s\n", m.artifacts["mboot"], config)
}

// Rebase the original ISO config for HTTP. All lines, unknown fields, module
// order, nested paths, options and line endings survive. Prefix applies only to
// relative file tokens, so only their leading ISO-root slash is removed.
func renderESXiBootConfig(r Request) ([]byte, []model.Problem, error) {
	if p := bootSnapshotProblems(r); len(p) != 0 {
		return nil, p, nil
	}
	invalid := func() ([]byte, []model.Problem, error) {
		return nil, []model.Problem{model.NewProblem("render_esxi_bootcfg_invalid", model.Subject{Kind: "iso", ID: r.Snapshot.ISO.ID, Path: "boot.cfg"})}, nil
	}
	raw := r.ESXiBootConfig
	if strings.TrimSpace(raw) == "" || !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		return invalid()
	}
	values, problems := assemblerValues(r, "gateway", "artifacts", "task", "token")
	if len(problems) != 0 {
		return nil, problems, nil
	}
	tree, p := artifactURL(r, values["artifacts"], "esxi-tree")
	if p != nil {
		return nil, []model.Problem{*p}, nil
	}
	prefix := strings.TrimRight(tree, "/")
	name, _ := catalog.DeliveryFileName(r.Snapshot.Format, catalog.FileAnswer)
	ks := catalog.AnswerURL(values["gateway"], values["task"], values["token"], name)
	lines := strings.SplitAfter(raw, "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		end := ""
		if strings.HasSuffix(line, "\n") {
			end = "\n"
			line = strings.TrimSuffix(line, "\n")
			if strings.HasSuffix(line, "\r") {
				end = "\r\n"
				line = strings.TrimSuffix(line, "\r")
			}
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		head, value, ok := strings.Cut(line, "=")
		key := strings.TrimSpace(head)
		if !ok || key == "" || strings.ContainsAny(key, " \t\r") || strings.ContainsRune(value, '\r') {
			return invalid()
		}
		switch key {
		case "prefix", "kernel", "modules", "kernelopt":
			if seen[key] {
				return invalid()
			}
			seen[key] = true
		default:
			continue
		}
		switch key {
		case "prefix":
			value = prefix
		case "kernel":
			var ok bool
			value, ok = esxiFileToken(value, prefix)
			if !ok {
				return invalid()
			}
		case "modules":
			modules := strings.Split(value, "---")
			for j, module := range modules {
				var ok bool
				modules[j], ok = esxiFileToken(module, prefix)
				if !ok {
					return invalid()
				}
			}
			value = strings.Join(modules, "---")
		case "kernelopt":
			value = esxiKernelOptions(value, ks)
		}
		lines[i] = head + "=" + value + end
	}
	if !seen["kernel"] || !seen["modules"] {
		return invalid()
	}
	// Prefix and kernelopt are optional in original media. Append missing fields
	// using the original newline convention and retain the final-line contents.
	if !seen["prefix"] || !seen["kernelopt"] {
		end := "\n"
		if strings.Contains(raw, "\r\n") {
			end = "\r\n"
		}
		text := strings.Join(lines, "")
		if !strings.HasSuffix(text, "\n") {
			text += end
		}
		if !seen["prefix"] {
			text += "prefix=" + prefix + end
		}
		if !seen["kernelopt"] {
			text += "kernelopt=ks=" + ks + end
		}
		return []byte(text), nil, nil
	}
	return []byte(strings.Join(lines, "")), nil, nil
}

// A boot.cfg module is a file token followed by optional arguments. Strip only
// the ISO-root slash; never use basename or sort. Absolute HTTP URLs must
// already point into this immutable published extraction, or fail explicitly.
func esxiFileToken(value, prefix string) (string, bool) {
	start := len(value) - len(strings.TrimLeft(value, " \t"))
	left, rest := value[:start], value[start:]
	if rest == "" {
		return "", false
	}
	split := strings.IndexAny(rest, " \t")
	file, options := rest, ""
	if split >= 0 {
		file, options = rest[:split], rest[split:]
	}
	if strings.Contains(file, "://") {
		parsed, err := url.Parse(file)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasPrefix(file, prefix+"/") {
			return "", false
		}
	} else {
		file = strings.TrimPrefix(file, "/")
	}
	if file == "" {
		return "", false
	}
	path := file
	if strings.Contains(file, "://") {
		parsed, _ := url.Parse(file)
		path = strings.TrimPrefix(parsed.Path, "/")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return left + file + options, true
}

// Preserve every unrelated option and its spacing. HTTP boot cannot retain the
// exact optical-media-only cdromBoot token (VMware install guide, HTTP step 7c).
func esxiKernelOptions(value, ks string) string {
	var out strings.Builder
	found := false
	for len(value) != 0 {
		if value[0] == ' ' || value[0] == '\t' {
			out.WriteByte(value[0])
			value = value[1:]
			continue
		}
		end := strings.IndexAny(value, " \t")
		if end < 0 {
			end = len(value)
		}
		token := value[:end]
		if token == "cdromBoot" {
			value = strings.TrimLeft(value[end:], " \t")
			continue
		}
		if strings.HasPrefix(token, "ks=") {
			token = "ks=" + ks
			found = true
		}
		out.WriteString(token)
		value = value[end:]
	}
	text := out.String()
	if !found {
		if text != "" && !strings.HasSuffix(text, " ") && !strings.HasSuffix(text, "\t") {
			text += " "
		}
		text += "ks=" + ks
	}
	return text
}

func esxiEncodedShell(body string) string {
	wrapped := `if [ "$LOBER_IFS_SET" = 1 ]; then IFS=$LOBER_OLD_IFS; else unset IFS; fi
[ "$LOBER_OLD_GLOB" = 1 ] || set +f
unset LOBER_OLD_IFS LOBER_IFS_SET LOBER_OLD_GLOB LOBER_ESXI_CODE LOBER_ESXI_HASH
` + body
	digest := sha256.Sum256([]byte(strings.TrimRight(wrapped, "\n")))
	return "(LOBER_OLD_IFS=${IFS-};case ${IFS+x} in x) LOBER_IFS_SET=1;; *) LOBER_IFS_SET=0;; esac;" +
		"case $- in *f*) LOBER_OLD_GLOB=1;; *) LOBER_OLD_GLOB=0;; esac;IFS=;set -f;LOBER_ESXI_CODE=$(echo " + base64.StdEncoding.EncodeToString([]byte(wrapped)) +
		" | openssl base64 -d -A)||exit 127;LOBER_ESXI_HASH=$(printf %s $LOBER_ESXI_CODE | sha256sum)||exit 127;case $LOBER_ESXI_HASH in " +
		fmt.Sprintf("%x", digest) + "*) :;; *) exit 65;; esac;eval $LOBER_ESXI_CODE)"
}

func esxiAnswerSlot(e expansion, slot model.SnapshotSlot, subject model.Subject) (string, []model.Problem) {
	if slot.Timing != "firstboot" {
		return shellAnswerSlot(e, slot, subject)
	}
	// ESXi 按顺序执行原生首次启动段。冻结载荷由安装阶段存放到受保护的系统卷。
	wrapper, p := posixRuntime(e.request, slot, false)
	if len(p) != 0 {
		return "", p
	}
	var b strings.Builder
	b.WriteString("set -eu\numask 077\nmkdir -p /tmp/lober\n")
	b.WriteString(encodedFile("/tmp/lober/"+slotFile("esxi-ks", slot), []byte(wrapper)))
	for _, instance := range slot.Instances {
		b.WriteString(encodedFile("/tmp/lober/"+hookFile(instance), []byte(instance.Body)))
	}
	b.WriteString("sh " + shQuote("/tmp/lober/"+slotFile("esxi-ks", slot)) + "\n")
	return esxiEncodedShell(b.String()), nil
}

func encodedFile(path string, body []byte) string {
	return "printf '%s' " + shQuote(base64.StdEncoding.EncodeToString(body)) + " | openssl base64 -d -A > " + shQuote(path) + "\n"
}

func esxiPart(e expansion, part catalog.SystemPart, subject model.Subject) (string, []model.Problem) {
	keys := []catalog.SystemValue{"gateway", "task"}
	if part == "firstboot" {
		keys = append(keys, "finalize-token")
	}
	values, p := answerValues(e, subject, keys...)
	if len(p) != 0 {
		return "", p
	}
	body := "set -eu\numask 077\n" + esxiNativeOwnerCheck(values[1])
	switch part {
	case "firstboot":
		body += "mkdir -p /tmp/lober\nprintf '%s' " + shQuote(values[2]) + " > /tmp/lober/finalize.token\n"
		body += esxiFinalizeSignal(values[0], values[1], model.PhaseBooted) + esxiFinalizeSignal(values[0], values[1], model.PhaseInitStart)
	case "cleanup":
		stage, directory := esxiStagePaths(values[1])
		body += "[ \"${LOBER_ESXI_STAGE_PATH:-}\" = " + shQuote(stage) + " ] && [ -f \"$LOBER_ESXI_STAGE_PATH\" ] && [ ! -L \"$LOBER_ESXI_STAGE_PATH\" ] && [ ! -L " + shQuote(directory) + " ] || exit 66\n"
		body += "TOKEN=$(cat /tmp/lober/finalize.token)\n"
		body += "rm -f \"$LOBER_ESXI_STAGE_PATH\"\nrmdir " + shQuote(directory) + "\n"
		body += "rm -f /tmp/lober/finalize.token /tmp/lober/script-*.sh /tmp/lober/hook-* /tmp/lober/.owner\nrmdir /tmp/lober\n"
		body += esxiFinalizeSignalWithToken(values[0], values[1], model.PhaseInitDone) + esxiFinalizeSignalWithToken(values[0], values[1], model.PhaseReady)
	default:
		return "", []model.Problem{model.NewProblem("render_replacement_missing", subject, part)}
	}
	prefix := ""
	if part == "firstboot" {
		prefix += "LOBER_ESXI_NATIVE_SCRIPT=$0;LOBER_ESXI_OWNER=" + esxiNativeMarker(values[1]) + ";export LOBER_ESXI_NATIVE_SCRIPT LOBER_ESXI_OWNER;"
	}
	return prefix + esxiEncodedShell(body), nil
}

func esxiNativeMarker(task string) string { return "lober-esxi-firstboot-" + task }

func esxiNativeOwnerCheck(task string) string {
	marker := esxiNativeMarker(task)
	return "which grep >/dev/null 2>&1 || exit 127\ncase ${LOBER_ESXI_NATIVE_SCRIPT:-} in /*) ;; *) exit 66;; esac\n" +
		"[ \"${LOBER_ESXI_OWNER:-}\" = " + shQuote(marker) + " ] && [ -f \"$LOBER_ESXI_NATIVE_SCRIPT\" ] && [ ! -L \"$LOBER_ESXI_NATIVE_SCRIPT\" ] && grep -F -q " + shQuote("LOBER_ESXI_OWNER="+marker+";") + " \"$LOBER_ESXI_NATIVE_SCRIPT\" || exit 66\n"
}

func esxiFinalizeSignal(gateway, task string, phase model.SignalPhase) string {
	return "TOKEN=$(cat /tmp/lober/finalize.token)\n" + esxiFinalizeSignalWithToken(gateway, task, phase)
}

func esxiFinalizeSignalWithToken(gateway, task string, phase model.SignalPhase) string {
	address := catalog.SignalURL(gateway, task, "TOKEN", string(phase))
	before, after := tokenURLParts(address)
	return "address=" + shQuote(before) + "\"$TOKEN\"" + shQuote(after) + "\nbody=\n" + esxiHTTPPost() + " >/dev/null 2>&1 || :\n"
}

// ESXi 自带的 wget 不支持 POST；用系统自带的 Python 保留二进制内容。
func esxiHTTPPost() string {
	return `LOBER_SIGNAL_URL="$address" LOBER_SIGNAL_BODY="$body" python3 -c 'import os,urllib.request; p=os.environ["LOBER_SIGNAL_BODY"]; b=open(p,"rb").read() if p else b""; req=urllib.request.Request(os.environ["LOBER_SIGNAL_URL"],data=b,headers={"Content-Type":"text/plain"},method="POST"); urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req,timeout=10).close()'`
}
