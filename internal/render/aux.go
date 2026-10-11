package render

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

func renderAux(r Request) ([]byte, []model.Problem, error) {
	switch r.Snapshot.Format {
	case "autoinstall":
		values, problems := assemblerValues(r, "task")
		hostname, ok := r.Snapshot.Template.Params["hostname"]
		if !ok || len(hostname.Items) != 1 || hostname.Items[0] == "" {
			problems = append(problems, model.NewProblem("param_required", model.Subject{Kind: "template", ID: templateRef(r.Snapshot.Template), Path: "hostname"}, "hostname"))
		}
		if len(problems) != 0 {
			return nil, problems, nil
		}
		return []byte(fmt.Sprintf("instance-id: lober-task-%s\nlocal-hostname: %s\n", values["task"], hostname.Items[0])), nil, nil
	case "esxi-ks":
		if p := bootSnapshotProblems(r); len(p) != 0 {
			return nil, p, nil
		}
		return renderESXiBootConfig(r)
	default:
		return nil, nil, errNotImplemented
	}
}

// Rebase the original ISO config for HTTP. All lines, unknown fields, module
// order, nested paths, options and line endings survive. Prefix applies only to
// relative file tokens, so only their leading ISO-root slash is removed.
func renderESXiBootConfig(r Request) ([]byte, []model.Problem, error) {
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
