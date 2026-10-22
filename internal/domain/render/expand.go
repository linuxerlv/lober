package render

import (
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/scan"
)

// expansion is the common text engine. Assemblers prepare the format-specific
// one-line execution commands and system parts; the engine owns their insertion,
// fragment expansion, parameter substitution, and list/line handling.
type expansion struct {
	request Request
	slots   map[string]string
	parts   map[catalog.SystemPart]string
	// Fragments are leaves (C8): they can contain raw/sys/param, never slots or parts.
	fragmentOnly bool
}

func (e expansion) expand(body string, params map[string]model.SnapshotValue, subject model.Subject) ([]byte, []model.Problem) {
	if problems := reservedValues(e.request); len(problems) != 0 {
		return nil, problems
	}
	result, _ := scan.Scan(body) // Malformed placeholders intentionally remain raw.
	if e.fragmentOnly {
		var problems []model.Problem
		for _, seg := range result.Segments {
			if seg.Kind == scan.KindSlot || seg.Kind == scan.KindPart {
				problems = append(problems, model.NewProblem("fragment_forbidden_placeholder", atSegment(subject, seg), seg.Raw))
			}
		}
		if len(problems) != 0 {
			return nil, problems
		}
	}
	var out strings.Builder
	var problems []model.Problem
	for _, line := range result.Lines() {
		text, p := e.expandLine(line, params, subject)
		problems = append(problems, p...)
		out.WriteString(text)
	}
	if len(problems) != 0 {
		return nil, problems
	}
	return []byte(out.String()), nil
}

func (e expansion) expandLine(line scan.Line, params map[string]model.SnapshotValue, subject model.Subject) (string, []model.Problem) {
	// A fragment replaces its whole source line, including otherwise redundant
	// indentation/trailing whitespace. Empty fragments remove the line entirely.
	for _, seg := range line.Segments {
		if seg.Kind == scan.KindSlot && !seg.Slot.Exec {
			where := atSegment(subject, seg)
			if !seg.Alone {
				return "", []model.Problem{model.NewProblem("fragment_slot_not_alone", where, seg.Slot.Name)}
			}
			return e.fragment(seg, line.End, where)
		}
	}
	values := make([]model.SnapshotValue, len(line.Segments))
	texts := make([]string, len(line.Segments))
	var problems []model.Problem
	repeated, count := "", 1
	for i, seg := range line.Segments {
		where := atSegment(subject, seg)
		switch seg.Kind {
		case scan.KindText:
			texts[i] = seg.Raw
		case scan.KindSys:
			text, p := e.systemValue(seg.Sys, where)
			texts[i] = text
			if p != nil {
				problems = append(problems, *p)
			}
		case scan.KindParam:
			value, ok := params[seg.Param.Name]
			if !ok || len(value.Items) == 0 {
				problems = append(problems, model.NewProblem("param_required", where, seg.Param.Name))
				continue
			}
			if seg.Param.Sub != "" {
				var available bool
				value, available = value.Sub(seg.Param.Sub)
				if !available {
					problems = append(problems, model.NewProblem("param_sub_unavailable", where, seg.Param.Name, seg.Param.Sub))
					continue
				}
			}
			values[i] = value
			if value.List && !seg.Param.Join {
				if repeated == "" {
					repeated, count = seg.Param.Name, len(value.Items)
				}
				// Activation rejects distinct lists on one line; enforce no
				// accidental cross-product if a caller hand-builds bad input.
				if repeated != seg.Param.Name {
					problems = append(problems, model.NewProblem("list_repeat_conflict", where, seg.Param.Name))
				}
			} else {
				texts[i] = value.Join()
			}
		case scan.KindSlot:
			text, ok := e.slots[seg.Slot.Name]
			if !ok {
				problems = append(problems, model.NewProblem("render_replacement_missing", where, seg.Raw))
			} else if !oneLine(text) {
				problems = append(problems, model.NewProblem("render_line_invalid", where, seg.Raw))
			}
			texts[i] = text
		case scan.KindPart:
			text, ok := e.parts[seg.Part]
			if !ok {
				problems = append(problems, model.NewProblem("render_replacement_missing", where, seg.Raw))
			} else if !oneLine(text) {
				problems = append(problems, model.NewProblem("render_line_invalid", where, seg.Raw))
			}
			texts[i] = text
		}
	}
	if len(problems) != 0 {
		return "", problems
	}
	var out strings.Builder
	for row := 0; row < count; row++ {
		for i, seg := range line.Segments {
			if seg.Kind == scan.KindParam && values[i].List && !seg.Param.Join {
				out.WriteString(values[i].Items[row])
			} else {
				out.WriteString(texts[i])
			}
		}
		if line.End != "" {
			out.WriteString(line.End)
		} else if row+1 < count {
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}

func (e expansion) fragment(seg scan.Segment, end string, subject model.Subject) (string, []model.Problem) {
	slot, ok := findSlot(e.request.Snapshot, seg.Slot.Name)
	if !ok || slot.Exec {
		return "", []model.Problem{model.NewProblem("render_slot_invalid", subject, seg.Slot.Name)}
	}
	var lines []scan.Line
	var problems []model.Problem
	child := e
	child.fragmentOnly = true
	for i, instance := range slot.Instances {
		text, p := child.expand(instance.Body, instance.Params, model.Subject{Kind: "slot", ID: subject.ID, Path: model.InstancePath(slot.Name, i+1)})
		problems = append(problems, p...)
		// Only the scanned line view is used here, so inserted braces stay literal.
		result, _ := scan.Scan(string(text))
		lines = append(lines, result.Lines()...)
	}
	if len(problems) != 0 {
		return "", problems
	}
	var out strings.Builder
	for i, line := range lines {
		out.WriteString(seg.Indent)
		out.WriteString(line.Raw())
		if i == len(lines)-1 {
			out.WriteString(end)
		} else if line.End != "" {
			out.WriteString(line.End)
		} else {
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}

func (e expansion) systemValue(key catalog.SystemValue, subject model.Subject) (string, *model.Problem) {
	if key == "lbrace" {
		return "{{", nil
	}
	if key == "repo" || key == "iso" {
		base, p := e.systemValue("artifacts", subject)
		if p != nil {
			return "", p
		}
		rel := e.request.Snapshot.ISO.Artifacts[string(key)]
		if rel == "" {
			p := model.NewProblem("render_artifact_missing", model.Subject{Kind: "iso", ID: e.request.Snapshot.ISO.ID, Path: "artifacts/" + string(key)}, key)
			return "", &p
		}
		return catalog.ArtifactURL(base, rel), nil
	}
	value, ok := e.request.Values[key]
	if !ok || value == "" {
		p := model.NewProblem("render_system_value_missing", subject, key)
		return "", &p
	}
	return value, nil
}

func atSegment(subject model.Subject, seg scan.Segment) model.Subject {
	subject.Line, subject.Col = seg.Line, seg.Col
	return subject
}

func oneLine(text string) bool {
	return !strings.ContainsAny(text, "\r\n\"'<>&\\#") && !strings.Contains(text, ": ")
}
