package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// renderMenu assembles executable iPXE for all seven formats.
// Layer 7 supplies the actual firmware; optional registration metadata is not
// a substitute. The gateway owns actual architecture and on-disk checks.
func renderMenu(r Request) ([]byte, []model.Problem, error) {
	if !r.Firmware.Valid() {
		return nil, []model.Problem{model.NewProblem("render_firmware_invalid", model.Subject{Kind: "host", ID: r.Snapshot.Host.SN, Path: "firmware"}, r.Firmware)}, nil
	}
	if p := bootSnapshotProblems(r); len(p) != 0 {
		return nil, p, nil
	}
	if !slices.Contains(r.Snapshot.Series.Firmwares(), r.Firmware) {
		return nil, []model.Problem{model.NewProblem("firmware_mismatch", model.Subject{Kind: "host", ID: r.Snapshot.Host.SN, Path: "firmware"}, r.Snapshot.Series.Label(), r.Firmware.Label())}, nil
	}
	switch r.Snapshot.Format {
	case "kickstart", "preseed", "autoinstall", "esxi-ks", "autoyast", "agama", "unattend":
	default:
		return nil, nil, errUnsupportedFormat
	}
	keys := []catalog.SystemValue{"gateway", "artifacts", "task", "token"}
	if r.Snapshot.Format == "kickstart" {
		keys = append(keys, "syslog-addr", "repo")
	}
	if r.Snapshot.Format == "autoyast" || r.Snapshot.Format == "agama" {
		keys = append(keys, "repo")
	}
	if r.Snapshot.Format == "autoinstall" {
		keys = append(keys, "iso")
	}
	values, problems := assemblerValues(r, keys...)
	if len(problems) != 0 {
		return nil, problems, nil
	}
	roles := catalog.RequiredRoles(r.Snapshot.Series)
	artifacts := map[string]string{}
	for _, role := range roles {
		value, p := artifactURL(r, values["artifacts"], role)
		artifacts[role] = value
		if p != nil {
			problems = append(problems, *p)
		}
	}
	if len(problems) != 0 {
		return nil, problems, nil
	}
	file := catalog.FileAnswer
	if r.Snapshot.Format == "esxi-ks" {
		file = catalog.FileAux
	}
	name, _ := catalog.DeliveryFileName(r.Snapshot.Format, file)
	answer := catalog.AnswerURL(values["gateway"], values["task"], values["token"], name)
	const console = "console=tty0 console=ttyS0,115200n8"
	var body string
	switch r.Snapshot.Format {
	case "kickstart":
		body = fmt.Sprintf("kernel %s \\\n  inst.stage2=%s \\\n  inst.repo=%s \\\n  inst.ks=%s inst.cmdline inst.notmux ip=dhcp \\\n  %s inst.syslog=%s\ninitrd %s\n", artifacts["kernel"], artifacts["stage2"], values["repo"], answer, console, values["syslog-addr"], artifacts["initrd"])
	case "preseed":
		body = fmt.Sprintf("kernel %s auto=true priority=critical \\\n  preseed/url=%s \\\n  %s\ninitrd %s\n", artifacts["kernel"], answer, console, artifacts["initrd"])
	case "autoinstall":
		seed := catalog.AnswerURL(values["gateway"], values["task"], values["token"], "")
		body = fmt.Sprintf("kernel %s autoinstall ip=dhcp url=%s ds=nocloud-net;s=%s %s ---\ninitrd %s\n", artifacts["kernel"], values["iso"], seed, console, artifacts["initrd"])
	case "autoyast":
		body = fmt.Sprintf("kernel %s install=%s autoyast=%s %s\ninitrd %s\n", artifacts["kernel"], values["repo"], answer, console, artifacts["initrd"])
	case "agama":
		body = fmt.Sprintf("kernel %s root=live:%s ip=dhcp rd.neednet=1 inst.install_url=%s inst.auto=%s %s\ninitrd %s\n", artifacts["kernel"], artifacts["live-root"], values["repo"], answer, console, artifacts["initrd"])
	case "unattend":
		aux, _ := catalog.DeliveryFileName(r.Snapshot.Format, catalog.FileAux)
		startup := catalog.AnswerURL(values["gateway"], values["task"], values["token"], aux)
		body = fmt.Sprintf("kernel %s\ninitrd %s %s\ninitrd %s winpeshl.ini\ninitrd %s busybox.exe\ninitrd %s curl.exe\ninitrd %s BCD\ninitrd %s boot.sdi\ninitrd %s boot.wim\n", artifacts["wimboot"], startup, aux, artifacts["winpeshl"], artifacts["winpe-busybox"], artifacts["winpe-curl"], artifacts["bcd"], artifacts["boot-sdi"], artifacts["boot-wim"])
	case "esxi-ks":
		body = fmt.Sprintf("kernel %s -c %s\n", artifacts["mboot"], answer)
	}
	return []byte("#!ipxe\n" + body + "boot\n"), nil, nil
}

func bootSnapshotProblems(r Request) []model.Problem {
	s := r.Snapshot
	if !s.Series.Valid() || s.Series.Format() != s.Format || s.Series.Family() != s.Family {
		return []model.Problem{model.NewProblem("render_boot_snapshot_invalid", model.Subject{Kind: "template", ID: templateRef(s.Template)})}
	}
	return nil
}

func assemblerValues(r Request, keys ...catalog.SystemValue) (map[catalog.SystemValue]string, []model.Problem) {
	e := expansion{request: r}
	values := map[catalog.SystemValue]string{}
	var problems []model.Problem
	for _, key := range keys {
		value, p := e.systemValue(key, model.Subject{Kind: "template", ID: templateRef(r.Snapshot.Template)})
		values[key] = value
		if p != nil {
			problems = append(problems, *p)
		}
	}
	return values, problems
}

func artifactURL(r Request, base, role string) (string, *model.Problem) {
	rel := r.Snapshot.ISO.Artifacts[role]
	if strings.TrimSpace(rel) == "" {
		p := model.NewProblem("render_artifact_missing", model.Subject{Kind: "iso", ID: r.Snapshot.ISO.ID, Path: "artifacts/" + role}, role)
		return "", &p
	}
	return catalog.ArtifactURL(base, rel), nil
}
