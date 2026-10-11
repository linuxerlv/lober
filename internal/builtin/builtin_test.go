package builtin

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/activation"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// Every shipped object must pass activation: EnsureBuiltin* only validates storage metadata.
func TestShippedContentActivates(t *testing.T) {
	templates, hooks := Templates(), Hooks()
	if len(templates) == 0 || len(hooks) == 0 {
		t.Fatal("builtin manifest is empty")
	}
	hookSet := map[model.HookKey]model.Hook{}
	names := map[string]bool{}
	for _, h := range hooks {
		checkIdentity(t, h.ID, h.Name, h.Version, h.Builtin, h.State)
		if _, exists := hookSet[h.Key()]; exists || names[strings.ToLower(h.Name)] {
			t.Fatalf("duplicate hook identity: %s", h.ID)
		}
		hookSet[h.Key()] = h
		names[strings.ToLower(h.Name)] = true
		if ps := activation.Hook(h); len(ps) != 0 {
			t.Errorf("%s@%d: %v", h.ID, h.Version, ps)
		}
	}
	ids := map[model.TemplateKey]bool{}
	for _, tpl := range templates {
		checkIdentity(t, tpl.ID, tpl.Name, tpl.Version, tpl.Builtin, tpl.State)
		key := model.TemplateKey{ID: tpl.ID, Version: tpl.Version}
		if ids[key] || names[strings.ToLower(tpl.Name)] {
			t.Fatalf("duplicate template identity: %s", tpl.ID)
		}
		ids[key] = true
		names[strings.ToLower(tpl.Name)] = true
		if ps := activation.Template(tpl, hookSet); len(ps) != 0 {
			t.Errorf("%s@%d: %v", tpl.ID, tpl.Version, ps)
		}
		for _, instances := range tpl.Assembly {
			for _, in := range instances {
				if _, exists := hookSet[model.HookKey{ID: in.HookID, Version: in.HookVersion}]; !exists {
					t.Errorf("template %s references a hook outside the builtin manifest", tpl.ID)
				}
			}
		}
	}
}

func checkIdentity(t *testing.T, id, name string, version int, builtin bool, state model.VersionState) {
	t.Helper()
	if !model.ValidID(id) || !strings.HasPrefix(id, "builtin-") || !strings.HasPrefix(name, "lober:") || version < 1 || !builtin || state != model.VersionApproved {
		t.Fatalf("invalid shipped identity: %s@%d %s builtin=%v state=%s", id, version, name, builtin, state)
	}
}

// Mutate every populated nested value, so new manifest fields join this ownership check automatically.
func TestManifestReturnsIndependentCopies(t *testing.T) {
	for _, tc := range []struct {
		name string
		get  func() any
	}{
		{"templates", func() any { return Templates() }},
		{"hooks", func() any { return Hooks() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, other := tc.get(), tc.get()
			if !reflect.DeepEqual(want, other) {
				t.Fatal("repeated calls differ")
			}
			before, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			mutate(reflect.ValueOf(other))
			after, err := json.Marshal(tc.get())
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("caller mutation escaped into the manifest")
			}
		})
	}
}

func TestClonePreservesAllNestedFields(t *testing.T) {
	min, max, timeout, required := int64(1), int64(9), 60, true
	decl := model.ParamDecl{Name: "mode", Options: []model.Option{{Value: "a", Label: "A"}}, Min: &min, Max: &max}
	binding := map[string]model.Binding{"mode": {Source: model.BindingFixed, Value: "a"}}
	tpl := model.Template{DerivedFrom: &model.TemplateKey{ID: "base", Version: 1}, Series: []catalog.Series{"rocky"}, Params: []model.ParamDecl{decl}, Bindings: binding,
		Assembly: map[string][]model.Instance{"slot": {{HookID: "hook", TimeoutSec: &timeout, Required: &required, Bindings: binding}}, "empty": {}, "nil": nil}}
	hook := model.Hook{Params: []model.ParamDecl{decl}, Impls: []model.Impl{{Series: []catalog.Series{"rocky"}, Timings: []catalog.Timing{"post-target"}}}}
	for _, original := range []any{tpl, hook, model.Template{}, model.Hook{Params: []model.ParamDecl{}}} {
		var copied any
		switch v := original.(type) {
		case model.Template:
			copied = clone(v)
		case model.Hook:
			copied = clone(v)
		}
		if !reflect.DeepEqual(original, copied) {
			t.Fatal("clone changed content or nil/empty representation")
		}
		before, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		mutate(reflect.ValueOf(copied))
		after, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("clone aliases a nested collection or pointer")
		}
	}
}

func mutate(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			mutate(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			mutate(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			mutate(v.Index(i))
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			value := reflect.New(v.Type().Elem()).Elem()
			value.Set(v.MapIndex(key))
			mutate(value)
			v.SetMapIndex(key, value)
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString("mutated")
		}
	case reflect.Int, reflect.Int64:
		if v.CanSet() {
			v.SetInt(v.Int() + 1)
		}
	case reflect.Bool:
		if v.CanSet() {
			v.SetBool(!v.Bool())
		}
	}
}
