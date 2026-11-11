package model

import (
	"cmp"
	"slices"
	"strings"
)

// HookRefs 列出模板引用的组件版本，去重后按编号、版本排序。
func HookRefs(t Template) []HookKey {
	var refs []HookKey
	for _, insts := range t.Assembly {
		for _, in := range insts {
			refs = append(refs, HookKey{ID: in.HookID, Version: in.HookVersion})
		}
	}
	slices.SortFunc(refs, func(a, b HookKey) int {
		return cmp.Or(strings.Compare(a.ID, b.ID), cmp.Compare(a.Version, b.Version))
	})
	return slices.Compact(refs)
}
