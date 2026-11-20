package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

// Full release fixtures, rather than just hashes, preserve old versions for upgrade replay.
type release struct {
	Templates []model.Template
	Hooks     []model.Hook
}

func historicalReleases(t *testing.T) []release {
	t.Helper()
	files, err := filepath.Glob("testdata/releases/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no historical builtin release fixtures")
	}
	var releases []release
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var r release
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Templates) == 0 || len(r.Hooks) == 0 {
			t.Fatalf("empty release fixture %s", file)
		}
		releases = append(releases, r)
	}
	return releases
}

func assertFingerprint(t *testing.T, kind, id string, version int, v any) {
	t.Helper()
	file := fmt.Sprintf("testdata/fingerprints/%s-%s-v%d.sha256", kind, id, version)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if want, got := strings.TrimSpace(string(data)), Fingerprint(v); want != got {
		t.Errorf("%s: fingerprint %s, want %s; changed content requires a new version", file, got, want)
	}
}

func TestFingerprintsPinEveryCurrentAndHistoricalVersion(t *testing.T) {
	history := historicalReleases(t)
	archivedTemplates := map[model.TemplateKey]model.Template{}
	archivedHooks := map[model.HookKey]model.Hook{}
	for _, r := range history {
		for _, tpl := range r.Templates {
			archivedTemplates[model.TemplateKey{ID: tpl.ID, Version: tpl.Version}] = tpl
		}
		for _, h := range r.Hooks {
			archivedHooks[h.Key()] = h
		}
	}
	for _, tpl := range Templates() {
		if !reflect.DeepEqual(tpl, archivedTemplates[model.TemplateKey{ID: tpl.ID, Version: tpl.Version}]) {
			t.Errorf("current template %s@%d has no matching full release fixture", tpl.ID, tpl.Version)
		}
	}
	for _, h := range Hooks() {
		if !reflect.DeepEqual(h, archivedHooks[h.Key()]) {
			t.Errorf("current hook %s@%d has no matching full release fixture", h.ID, h.Version)
		}
	}
	releases := append(history, release{Templates: Templates(), Hooks: Hooks()})
	for _, r := range releases {
		for _, tpl := range r.Templates {
			assertFingerprint(t, "template", tpl.ID, tpl.Version, tpl)
		}
		for _, h := range r.Hooks {
			assertFingerprint(t, "hook", h.ID, h.Version, h)
		}
	}
}

func TestFingerprintUsesDeterministicJSON(t *testing.T) {
	if got := Fingerprint(nil); got != "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b" {
		t.Fatalf("JSON null SHA256: %s", got)
	}
	a := map[string]int{"b": 2, "a": 1}
	b := map[string]int{"a": 1, "b": 2}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("map insertion order changed the fingerprint")
	}
	b["a"] = 2
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("changed content kept its fingerprint")
	}
}

func TestFingerprintRejectsUnserializableInput(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported input silently fingerprinted")
		}
	}()
	Fingerprint(make(chan int))
}

func TestReleasedVersionsLoadAndUpgradeIdempotently(t *testing.T) {
	s := pgtest.Open(t)
	ctx := t.Context()
	hookRevs := map[model.HookKey]int64{}
	templateRevs := map[model.TemplateKey]int64{}
	releases := append(historicalReleases(t), release{Templates: Templates(), Hooks: Hooks()})
	for n, r := range releases {
		for pass := 0; pass < 2; pass++ {
			for _, h := range r.Hooks {
				if err := s.EnsureBuiltinHook(ctx, h); err != nil {
					t.Fatalf("release %d hook %s: %v", n, h.ID, err)
				}
				got, err := s.GetHook(ctx, h.Key())
				if err != nil {
					t.Fatal(err)
				}
				if rev, exists := hookRevs[h.Key()]; exists && rev != got.Rev {
					t.Fatalf("idempotent hook loading changed revision: %s", h.ID)
				}
				hookRevs[h.Key()] = got.Rev
			}
			for _, tpl := range r.Templates {
				if err := s.EnsureBuiltinTemplate(ctx, tpl); err != nil {
					t.Fatalf("release %d template %s: %v", n, tpl.ID, err)
				}
				key := model.TemplateKey{ID: tpl.ID, Version: tpl.Version}
				got, err := s.GetTemplate(ctx, key)
				if err != nil {
					t.Fatal(err)
				}
				if rev, exists := templateRevs[key]; exists && rev != got.Rev {
					t.Fatalf("idempotent template loading changed revision: %s", tpl.ID)
				}
				templateRevs[key] = got.Rev
			}
		}
		// Re-read all older versions after every upgrade, proving later loading preserves them.
		for _, old := range releases[:n+1] {
			for _, h := range old.Hooks {
				got, err := s.GetHook(ctx, h.Key())
				if err != nil {
					t.Fatal(err)
				}
				if got.Rev != hookRevs[h.Key()] {
					t.Fatalf("upgrade changed old hook revision: %s@%d", h.ID, h.Version)
				}
				got.Rev = 0
				if !reflect.DeepEqual(got, h) {
					t.Fatalf("stored hook %s@%d changed", h.ID, h.Version)
				}
			}
			for _, tpl := range old.Templates {
				got, err := s.GetTemplate(ctx, model.TemplateKey{ID: tpl.ID, Version: tpl.Version})
				if err != nil {
					t.Fatal(err)
				}
				if got.Rev != templateRevs[model.TemplateKey{ID: tpl.ID, Version: tpl.Version}] {
					t.Fatalf("upgrade changed old template revision: %s@%d", tpl.ID, tpl.Version)
				}
				got.Rev = 0
				if !reflect.DeepEqual(got, tpl) {
					t.Fatalf("stored template %s@%d changed", tpl.ID, tpl.Version)
				}
			}
		}
	}
	for _, h := range Hooks() {
		changed := h
		changed.Impls = append([]model.Impl(nil), h.Impls...)
		changed.Impls[0].Body += "# changed\n"
		if err := s.EnsureBuiltinHook(ctx, changed); !errors.Is(err, repository.ErrBuiltinMismatch) {
			t.Fatalf("changed hook: %v", err)
		}
		h.Version++
		h.Name += "-renamed"
		if err := s.EnsureBuiltinHook(ctx, h); !errors.Is(err, repository.ErrBuiltinMismatch) {
			t.Fatalf("renamed hook: %v", err)
		}
	}
	for _, tpl := range Templates() {
		changed := tpl
		changed.Body += "# changed\n"
		if err := s.EnsureBuiltinTemplate(ctx, changed); !errors.Is(err, repository.ErrBuiltinMismatch) {
			t.Fatalf("changed template: %v", err)
		}
		tpl.Version++
		tpl.Name += "-renamed"
		if err := s.EnsureBuiltinTemplate(ctx, tpl); !errors.Is(err, repository.ErrBuiltinMismatch) {
			t.Fatalf("renamed template: %v", err)
		}
	}
}
