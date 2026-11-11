package selection

import "github.com/lober/lober/internal/domain/model"

// Manifest 是本次程序发布携带的内置版本，由调用方提供。
type Manifest struct {
	Templates []model.TemplateKey
	Hooks     []model.HookKey
}

// CurrentBuiltin 只判断新选择是否属于当前发布，不修改已经冻结的任务。
func CurrentBuiltin(t model.Template, hooks map[model.HookKey]model.Hook, manifest Manifest) bool {
	if t.Builtin {
		found := false
		for _, key := range manifest.Templates {
			found = found || key.ID == t.ID && key.Version == t.Version
		}
		if !found {
			return false
		}
	}
	shipped := make(map[model.HookKey]bool, len(manifest.Hooks))
	for _, key := range manifest.Hooks {
		shipped[key] = true
	}
	for _, key := range model.HookRefs(t) {
		if h, exists := hooks[key]; exists && h.Builtin && !shipped[key] {
			return false
		}
	}
	return true
}
