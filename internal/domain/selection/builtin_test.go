package selection

import (
	"reflect"
	"testing"

	"github.com/lober/lober/internal/domain/model"
)

func Test内置选择只接受显式发布清单(t *testing.T) {
	manifest := Manifest{Templates: []model.TemplateKey{{ID: "builtin-test", Version: 2}}, Hooks: []model.HookKey{{ID: "builtin-hook", Version: 2}}}
	for _, tc := range []struct {
		name        string
		builtin     bool
		version     int
		hookVersion int
		hookBuiltin bool
		want        bool
	}{
		{"非内置", false, 1, 1, false, true},
		{"当前内置", true, 2, 2, true, true},
		{"旧内置模板", true, 1, 2, true, false},
		{"旧内置组件", true, 2, 1, true, false},
		{"自编模板引用旧内置组件", false, 1, 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tpl := model.Template{ID: "builtin-test", Version: tc.version, Builtin: tc.builtin, Assembly: map[string][]model.Instance{"pre": {{HookID: "builtin-hook", HookVersion: tc.hookVersion}}}}
			hooks := map[model.HookKey]model.Hook{{ID: "builtin-hook", Version: tc.hookVersion}: {Builtin: tc.hookBuiltin}}
			if got := CurrentBuiltin(tpl, hooks, manifest); got != tc.want {
				t.Fatalf("可选择=%v，期望=%v", got, tc.want)
			}
			if tpl.Version != tc.version || hooks[model.HookKey{ID: "builtin-hook", Version: tc.hookVersion}].Builtin != tc.hookBuiltin {
				t.Fatal("选择检查改动了输入")
			}
		})
	}
	missing := model.Template{Assembly: map[string][]model.Instance{"pre": {{HookID: "missing", HookVersion: 1}}}}
	if !CurrentBuiltin(missing, nil, manifest) {
		t.Fatal("缺失引用仍由原准入规则报告，不改成过期发布")
	}
	if !reflect.DeepEqual(manifest.Templates, []model.TemplateKey{{ID: "builtin-test", Version: 2}}) {
		t.Fatal("清单被修改")
	}
}
