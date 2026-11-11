package repository

import (
	"cmp"
	"slices"
	"strings"

	"github.com/lober/lober/internal/domain/model"
)

// TemplateHookKeys 汇总可读取的组件引用。非法草稿引用留给后续诊断，不能让整页读取失败。
func TemplateHookKeys(templates []model.Template) []model.HookKey {
	unique := map[model.HookKey]bool{}
	for _, t := range templates {
		for _, k := range model.HookRefs(t) {
			if CheckVersionKey(k.ID, k.Version) == nil {
				unique[k] = true
			}
		}
	}
	keys := make([]model.HookKey, 0, len(unique))
	for k := range unique {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b model.HookKey) int {
		return cmp.Or(strings.Compare(a.ID, b.ID), cmp.Compare(a.Version, b.Version))
	})
	return keys
}
