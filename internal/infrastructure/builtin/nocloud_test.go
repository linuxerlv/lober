package builtin

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/snapshot"
)

// Valid authors may name a hostname parameter freely or write a literal.
func TestNoCloudCustomHostnameBuildAndRender(t *testing.T) {
	for _, name := range []string{"literal", "renamed", "builtin"} {
		t.Run(name, func(t *testing.T) {
			var tpl model.Template
			for _, candidate := range Templates() {
				if f, _ := candidate.Format(); f == "autoinstall" {
					tpl = candidate
				}
			}
			want := "lober-node"
			if name != "builtin" {
				for i, d := range tpl.Params {
					if d.Name == "hostname" {
						if name == "literal" {
							tpl.Params = append(tpl.Params[:i], tpl.Params[i+1:]...)
							tpl.Body = strings.ReplaceAll(tpl.Body, "{{param:hostname}}", "literal-node")
							want = "literal-node"
						} else {
							tpl.Params[i].Name = "node_name"
							tpl.Body = strings.ReplaceAll(tpl.Body, "{{param:hostname}}", "{{param:node_name}}")
						}
						break
					}
				}
			}
			input := fixtureInput(t, tpl)
			if ps := activation.Template(tpl, input.Hooks); len(ps) != 0 {
				t.Fatalf("activation: %v", ps)
			}
			snap, ps := snapshot.Build(input)
			if len(ps) != 0 {
				t.Fatalf("Build: %v", ps)
			}
			r := fixtureRequest(t, tpl, "uefi")
			r.Snapshot = snap
			answer := renderFixture(t, r)
			if !strings.Contains(answer, "hostname: '"+want+"'") || strings.Contains(answer, "{{param:") {
				t.Fatalf("hostname answer: %s", answer)
			}
			r.Kind = catalog.FileAux
			if got := renderFixture(t, r); got != "instance-id: lober-task-task-9\n" {
				t.Fatalf("metadata = %q", got)
			}
		})
	}
}
