package render

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/scan"
)

// Request supplies a frozen snapshot and the values for this rendering call.
type Request struct {
	Snapshot model.Snapshot
	// Firmware is the actual boot firmware, mandatory for FileMenu. Registered
	// Snapshot.Host.Firmware metadata does not select or override it.
	Firmware catalog.Firmware
	// ESXiBootConfig is the immutable original extracted ISO boot.cfg supplied
	// by the caller for ESXi FileAux. Rendering never reads artifact files.
	ESXiBootConfig string
	// The caller supplies gateway, artifacts (base URLs), task, token, sn, smb,
	// syslog-addr, and independent finalize-token when needed by the requested
	// output. finalize-token is private, never an author placeholder, and is
	// required by Linux firstboot staging (Windows FileAux stages it). repo and iso are
	// reserved: rendering derives them with catalog.ArtifactURL from artifacts
	// and Snapshot.ISO.Artifacts["repo"] / ["iso"], respectively. Supplying either
	// reserved key is a content problem, even if its value matches the artifact.
	// lbrace is always the literal "{{" and requires no caller value.
	Values map[catalog.SystemValue]string
	Kind   catalog.FileKind
	// Slot names an execution slot only when Kind is FileScript.
	Slot string
}

var errNotImplemented = errors.New("render: assembler not implemented")

// Render returns one complete file, or content Problems with no partial bytes.
// Errors indicate an unsupported interface request. Concrete assemblers are
// deliberately deferred; their temporary sentinel is errNotImplemented.
func Render(r Request) ([]byte, []model.Problem, error) {
	if !r.Kind.Valid() {
		return nil, nil, fmt.Errorf("render: unknown file kind %q", r.Kind)
	}
	problems := reservedValues(r)
	if r.Kind == catalog.FileScript {
		slot, ok := findSlot(r.Snapshot, r.Slot)
		if !ok || !slot.Exec {
			problems = append(problems, model.NewProblem("render_slot_invalid", model.Subject{Kind: "slot", ID: templateRef(r.Snapshot.Template), Path: r.Slot}, r.Slot))
		}
	}
	// Answer text references determine the values needed by the shared expander.
	// Other assembler-specific requirements are checked by those assemblers.
	if r.Kind == catalog.FileAnswer {
		e := expansion{request: r}
		check := func(body string, subject model.Subject) {
			result, _ := scan.Scan(body)
			for _, seg := range result.Segments {
				if seg.Kind == scan.KindSys {
					_, p := e.systemValue(seg.Sys, atSegment(subject, seg))
					if p != nil {
						problems = append(problems, *p)
					}
				}
			}
		}
		check(r.Snapshot.Template.Body, model.Subject{Kind: "template", ID: templateRef(r.Snapshot.Template)})
		for _, slot := range r.Snapshot.Slots {
			if slot.Exec {
				continue
			}
			for i, instance := range slot.Instances {
				check(instance.Body, model.Subject{Kind: "slot", ID: templateRef(r.Snapshot.Template), Path: model.InstancePath(slot.Name, i+1)})
			}
		}
	}
	if len(problems) != 0 {
		return nil, problems, nil
	}
	switch r.Kind {
	case catalog.FileMenu:
		return renderMenu(r)
	case catalog.FileAnswer:
		return renderAnswer(r)
	case catalog.FileAux:
		return renderAux(r)
	case catalog.FileScript:
		return renderScript(r)
	}
	panic("unreachable file kind")
}

func renderScript(Request) ([]byte, []model.Problem, error) { return nil, nil, errNotImplemented }

func reservedValues(r Request) []model.Problem {
	var out []model.Problem
	for _, key := range []catalog.SystemValue{"repo", "iso"} {
		if _, supplied := r.Values[key]; supplied {
			out = append(out, model.NewProblem("render_system_value_reserved", model.Subject{Kind: "template", ID: templateRef(r.Snapshot.Template)}, key))
		}
	}
	return out
}

func templateRef(t model.SnapshotTemplate) string {
	return t.ID + "@" + strconv.Itoa(t.Version)
}

func findSlot(snapshot model.Snapshot, name string) (model.SnapshotSlot, bool) {
	for _, slot := range snapshot.Slots {
		if slot.Name == name {
			return slot, true
		}
	}
	return model.SnapshotSlot{}, false
}
