package builtin

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

func TestUpgradeFromLegacyVMKeepsBuiltinVersions(t *testing.T) {
	data, err := os.ReadFile("testdata/legacy-vm-b12aaa.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy release
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Hooks) != 2 || len(legacy.Templates) != 5 {
		t.Fatal("旧版本记录不完整")
	}
	s := pgtest.Open(t)
	ctx := t.Context()
	for _, h := range legacy.Hooks {
		if err := s.EnsureBuiltinHook(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	for _, tpl := range legacy.Templates {
		if err := s.EnsureBuiltinTemplate(ctx, tpl); err != nil {
			t.Fatal(err)
		}
	}
	for pass := 0; pass < 2; pass++ {
		for _, h := range Hooks() {
			if err := s.EnsureBuiltinHook(ctx, h); err != nil {
				t.Fatalf("加载新组件 %s@%d：%v", h.ID, h.Version, err)
			}
		}
		for _, tpl := range Templates() {
			if err := s.EnsureBuiltinTemplate(ctx, tpl); err != nil {
				t.Fatalf("加载新模板 %s@%d：%v", tpl.ID, tpl.Version, err)
			}
		}
	}
	for _, h := range legacy.Hooks {
		got, err := s.GetHook(ctx, h.Key())
		if err != nil {
			t.Fatal(err)
		}
		got.Rev = 0
		if !reflect.DeepEqual(got, h) {
			t.Fatalf("旧组件 %s@%d 被改动", h.ID, h.Version)
		}
	}
	for _, tpl := range legacy.Templates {
		got, err := s.GetTemplate(ctx, model.TemplateKey{ID: tpl.ID, Version: tpl.Version})
		if err != nil {
			t.Fatal(err)
		}
		got.Rev = 0
		if !reflect.DeepEqual(got, tpl) {
			t.Fatalf("旧模板 %s@%d 被改动", tpl.ID, tpl.Version)
		}
	}
}
