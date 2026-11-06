package render

import (
	"slices"
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

type menuInput struct {
	format    catalog.Format
	values    map[catalog.SystemValue]string
	artifacts map[string]string
}

func (m menuInput) answerURL(kind catalog.FileKind) (url, name string) {
	name, _ = catalog.DeliveryFileName(m.format, kind)
	return catalog.AnswerURL(m.values["gateway"], m.values["task"], m.values["token"], name), name
}

// renderMenu assembles executable iPXE for all supported formats.
// Layer 7 supplies the actual firmware; optional registration metadata is not
// a substitute. The gateway owns actual architecture and on-disk checks.
func (f installer) renderMenu(r Request) ([]byte, []model.Problem, error) {
	if !r.Firmware.Valid() {
		return nil, []model.Problem{model.NewProblem("render_firmware_invalid", model.Subject{Kind: "host", ID: r.Snapshot.Host.SN, Path: "firmware"}, r.Firmware)}, nil
	}
	if p := bootSnapshotProblems(r); len(p) != 0 {
		return nil, p, nil
	}
	if !slices.Contains(r.Snapshot.Series.Firmwares(), r.Firmware) {
		return nil, []model.Problem{model.NewProblem("firmware_mismatch", model.Subject{Kind: "host", ID: r.Snapshot.Host.SN, Path: "firmware"}, r.Snapshot.Series.Label(), r.Firmware.Label())}, nil
	}
	keys := []catalog.SystemValue{"gateway", "artifacts", "task", "token"}
	values, problems := assemblerValues(r, append(keys, f.extraMenuValues...)...)
	if len(problems) != 0 {
		return nil, problems, nil
	}
	artifacts, problems := requiredArtifacts(r, values["artifacts"])
	if len(problems) != 0 {
		return nil, problems, nil
	}
	body := f.menu(menuInput{format: r.Snapshot.Format, values: values, artifacts: artifacts})
	return []byte("#!ipxe\n" + body + "boot\n"), nil, nil
}

func requiredArtifacts(r Request, base string) (map[string]string, []model.Problem) {
	artifacts := map[string]string{}
	var problems []model.Problem
	for _, role := range catalog.RequiredRoles(r.Snapshot.Series) {
		value, p := artifactURL(r, base, role)
		artifacts[role] = value
		if p != nil {
			problems = append(problems, *p)
		}
	}
	return artifacts, problems
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
