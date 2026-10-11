package builtin

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

//go:embed kickstart/answer.cfg
var kickstartBody string

//go:embed kickstart/packages.cfg
var kickstartPackages string

var templates = []model.Template{{
	ID: "builtin-kickstart", Version: 1, Name: "lober:kickstart",
	State: model.VersionApproved, Builtin: true,
	Family: "linux", Series: catalog.SeriesOfFormat("kickstart"), Body: kickstartBody,
	Params:   []model.ParamDecl{{Name: "hostname", Type: "hostname", Default: "lober-node"}},
	Bindings: map[string]model.Binding{"hostname": {Source: model.BindingDefault, Editable: true}},
	Assembly: map[string][]model.Instance{
		"packages": {{HookID: "builtin-packages", HookVersion: 1,
			Bindings: map[string]model.Binding{"packages": {Source: model.BindingDefault, Editable: true}}}},
	},
}}

var hooks = []model.Hook{{
	ID: "builtin-packages", Version: 1, Name: "lober:packages",
	State: model.VersionApproved, Builtin: true,
	Description: "Additional packages for kickstart installations.",
	Params:      []model.ParamDecl{{Name: "packages", Type: "word", List: true, Default: "curl"}},
	Impls:       []model.Impl{{Family: "linux", Series: catalog.SeriesOfFormat("kickstart"), Kind: model.ImplFragment, Body: kickstartPackages}},
}}

// Templates returns the shipped template versions, with no shared maps, slices or pointers.
func Templates() []model.Template { return clone(templates) }

// Hooks returns the shipped hook versions, with no shared maps, slices or pointers.
func Hooks() []model.Hook { return clone(hooks) }

// Fingerprint returns SHA256 of encoding/json's deterministic serialization, as lowercase hex.
// Unsupported JSON values panic: fingerprints must never silently omit invalid content.
func Fingerprint(v any) string {
	sum := sha256.Sum256(encode(v))
	return hex.EncodeToString(sum[:])
}

// Model values contain only JSON-compatible fields. Round-tripping recursively copies every
// collection and pointer, including future fields, and preserves nil versus empty collections.
func clone[T any](v T) T {
	var out T
	if err := json.Unmarshal(encode(v), &out); err != nil {
		panic(err)
	}
	return out
}

func encode(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
