package selection

import (
	"encoding/json"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"testing"
)

func Test模板候选沿用原资格且不改输入(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.Template, map[model.HookKey]model.Hook, *model.ISO, *Manifest)
		want   bool
	}{
		{"审批", nil, true},
		{"草稿", func(t *model.Template, _ map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) {
			t.State = model.VersionDraft
		}, false},
		{"废弃", func(t *model.Template, _ map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) {
			t.State = model.VersionDeprecated
		}, false},
		{"系不符", func(t *model.Template, _ map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) { t.Series = nil }, false},
		{"nil选项", func(t *model.Template, _ map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) {
			t.Params = []model.ParamDecl{{Type: "enum", Offer: "editions"}}
		}, false},
		{"空选项", func(t *model.Template, _ map[model.HookKey]model.Hook, iso *model.ISO, _ *Manifest) {
			t.Params = []model.ParamDecl{{Type: "enum", Offer: "editions"}}
			iso.Offers = map[string][]model.Option{"editions": {}}
		}, false},
		{"有选项", func(t *model.Template, _ map[model.HookKey]model.Hook, iso *model.ISO, _ *Manifest) {
			t.Params = []model.ParamDecl{{Type: "enum", Offer: "editions"}}
			iso.Offers = map[string][]model.Option{"editions": {{Value: "server"}}}
		}, true},
		{"当前内置", func(t *model.Template, _ map[model.HookKey]model.Hook, _ *model.ISO, m *Manifest) {
			t.Builtin = true
			m.Templates = []model.TemplateKey{{ID: t.ID, Version: t.Version}}
		}, true},
		{"旧内置", func(t *model.Template, _ map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) { t.Builtin = true }, false},
		{"钉住旧组件", func(_ *model.Template, h map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) {
			h[model.HookKey{ID: "hook", Version: 2}] = model.Hook{State: model.VersionDeprecated}
		}, true},
		{"引用废弃", func(_ *model.Template, h map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) {
			h[model.HookKey{ID: "hook", Version: 1}] = model.Hook{State: model.VersionDeprecated}
		}, false},
		{"旧内置组件", func(_ *model.Template, h map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) {
			h[model.HookKey{ID: "hook", Version: 1}] = model.Hook{Builtin: true}
		}, false},
		{"当前内置组件", func(_ *model.Template, h map[model.HookKey]model.Hook, _ *model.ISO, m *Manifest) {
			h[model.HookKey{ID: "hook", Version: 1}] = model.Hook{Builtin: true}
			m.Hooks = []model.HookKey{{ID: "hook", Version: 1}}
		}, true},
		{"缺失引用仍由激活诊断", func(_ *model.Template, h map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) {
			delete(h, model.HookKey{ID: "hook", Version: 1})
		}, true},
		{"组件缺少选项", func(_ *model.Template, h map[model.HookKey]model.Hook, _ *model.ISO, _ *Manifest) {
			h[model.HookKey{ID: "hook", Version: 1}] = model.Hook{Params: []model.ParamDecl{{Type: "enum", Offer: "editions"}}}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tpl := model.Template{ID: "template", Version: 1, State: model.VersionApproved, Series: []catalog.Series{"rocky"}, Assembly: map[string][]model.Instance{"pre": {{HookID: "hook", HookVersion: 1}}}}
			hooks := map[model.HookKey]model.Hook{{ID: "hook", Version: 1}: {State: model.VersionApproved}}
			iso := model.ISO{ID: "iso", Name: "Rocky", Family: "linux", Series: "rocky", Version: "9", Arch: "x86_64", State: model.ISOReady}
			manifest := Manifest{}
			if tc.change != nil {
				tc.change(&tpl, hooks, &iso, &manifest)
			}
			before, _ := json.Marshal([]any{tpl, hooksForJSON(hooks), iso, manifest})
			if got := TemplateEligible(tpl, hooks, iso, manifest); got != tc.want {
				t.Fatalf("资格=%v，期望=%v", got, tc.want)
			}
			after, _ := json.Marshal([]any{tpl, hooksForJSON(hooks), iso, manifest})
			if string(before) != string(after) {
				t.Fatal("检查改动了输入")
			}
		})
	}
}

func hooksForJSON(h map[model.HookKey]model.Hook) map[string]model.Hook {
	out := map[string]model.Hook{}
	for k, v := range h {
		out[k.String()] = v
	}
	return out
}
