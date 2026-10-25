package repository_test

import (
	"reflect"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func TestTemplateHookKeys跨模板去重过滤草稿并按编号版本排序(t *testing.T) {
	templates := []model.Template{
		{Assembly: map[string][]model.Instance{
			"first": {{HookID: "b", HookVersion: 1}, {HookID: "a", HookVersion: 10}, {HookID: "a", HookVersion: 2}},
			"other": {{HookID: "a", HookVersion: 2}, {HookID: "BAD", HookVersion: 1}, {HookID: "", HookVersion: 1}},
		}},
		{Assembly: map[string][]model.Instance{
			"first": {{HookID: "a", HookVersion: 2}, {HookID: "a", HookVersion: 1}, {HookID: "b", HookVersion: 1}, {HookID: "a/b", HookVersion: 1}, {HookID: "zero", HookVersion: 0}, {HookID: "negative", HookVersion: -1}},
		}},
	}
	want := []model.HookKey{{ID: "a", Version: 1}, {ID: "a", Version: 2}, {ID: "a", Version: 10}, {ID: "b", Version: 1}}
	if got := repository.TemplateHookKeys(templates); !reflect.DeepEqual(got, want) {
		t.Fatalf("组件引用 = %#v，想要 %#v", got, want)
	}
	// 草稿中的非法引用要保留，后续诊断才能指出原来的问题。
	if got := templates[0].Assembly["other"][1]; got.HookID != "BAD" || got.HookVersion != 1 {
		t.Fatalf("输入草稿被修改：%#v", got)
	}
}

func TestTemplateHookKeys没有合法引用仍返回非nil空列表(t *testing.T) {
	for _, tc := range []struct {
		name      string
		templates []model.Template
	}{
		{"nil输入", nil},
		{"空输入", []model.Template{}},
		{"空装配", []model.Template{{}}},
		{"仅非法引用", []model.Template{{Assembly: map[string][]model.Instance{"slot": {{HookID: "BAD", HookVersion: 1}, {HookID: "valid", HookVersion: 0}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := repository.TemplateHookKeys(tc.templates); got == nil || len(got) != 0 {
				t.Fatalf("应返回非 nil 空列表，得到 %#v", got)
			}
		})
	}
}
