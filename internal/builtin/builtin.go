package builtin

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// Bodies are compiled data. Reading this embed.FS never accesses a host filesystem.
//
//go:embed kickstart preseed autoinstall autoyast agama unattend esxi
var bodies embed.FS

var hooks = []model.Hook{partitionHook(), packagesHook(), networkHook(), accountHook(), sshkeyHook()}
var templates = shippedTemplates()

func body(format catalog.Format, file string) string {
	dir := string(format)
	if format == "esxi-ks" {
		dir = "esxi"
	}
	data, err := bodies.ReadFile(dir + "/" + file)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func series(format catalog.Format) []catalog.Series {
	if format == "unattend" {
		return []catalog.Series{"windows-server"}
	}
	return catalog.SeriesOfFormat(format)
}

func fragment(format catalog.Format, file string) model.Impl {
	ss := series(format)
	return model.Impl{Family: ss[0].Family(), Series: ss, Kind: model.ImplFragment, Body: body(format, file)}
}

func script(format catalog.Format, file string, interpreter catalog.Interpreter, timing catalog.Timing) model.Impl {
	impl := fragment(format, file)
	impl.Kind = model.ImplScript
	impl.Interpreter = interpreter
	impl.Timings = []catalog.Timing{timing}
	return impl
}

func hook(name string, version int, description string) model.Hook {
	return model.Hook{ID: "builtin-" + name, Version: version, Name: "lober:" + name, Description: description, State: model.VersionApproved, Builtin: true, TimeoutSec: 300, Required: true}
}

func instance(name string) model.Instance {
	version := 1
	// account v2 drops the Leap 15.6 schema-invalid create_home user field (D3);
	// partition v2 inlines kickstart part commands with reqpart instead of
	// %pre+%include, which AlmaLinux 10 parses before %pre runs (D2).
	if name == "packages" || name == "account" || name == "partition" {
		version = 2
	}
	return model.Instance{HookID: "builtin-" + name, HookVersion: version}
}

func fixedInstance(name, param, value string, editable bool) model.Instance {
	in := instance(name)
	in.Bindings = map[string]model.Binding{param: {Source: model.BindingFixed, Value: value, Editable: editable}}
	return in
}

func shippedTemplates() []model.Template {
	var result []model.Template
	for _, format := range []catalog.Format{"kickstart", "preseed", "autoinstall", "autoyast", "agama", "unattend", "esxi-ks"} {
		file := "answer.cfg"
		switch format {
		case "autoinstall":
			file = "answer.yaml"
		case "autoyast", "unattend":
			file = "answer.xml"
		case "agama":
			file = "answer.jsonnet"
		}
		id := string(format)
		if format == "esxi-ks" {
			id = "esxi"
		}
		// Content changes ship as higher version numbers: account/partition v2
		// rebinds every template, kickstart/preseed already carried a v2.
		version := 2
		if format == "kickstart" || format == "preseed" {
			version = 3
		}
		ss := series(format)
		tpl := model.Template{ID: "builtin-" + id, Version: version, Name: "lober:" + id, State: model.VersionApproved, Builtin: true, Family: ss[0].Family(), Series: ss, Body: body(format, file), Assembly: map[string][]model.Instance{}}
		for _, name := range []string{"partition", "network", "account", "sshkey"} {
			tpl.Assembly[name] = []model.Instance{instance(name)}
		}
		tpl.Assembly["packages"] = []model.Instance{fixedInstance("packages", "packages", "curl", true)}
		switch format {
		case "preseed":
			// A DVD tree is not a signed HTTP APT repository. Keep repository
			// selection explicit and let the installer enforce archive signatures.
			tpl.Params = []model.ParamDecl{
				{Name: "mirror_host", Label: "Debian mirror host", Type: "word", Default: "deb.debian.org"},
				{Name: "mirror_path", Label: "Debian mirror path", Type: "word", Default: "/debian"},
			}
		case "autoinstall":
			tpl.Params = []model.ParamDecl{{Name: "hostname", Type: "hostname", Default: "lober-node"}}
		case "agama":
			tpl.Params = []model.ParamDecl{{Name: "product", Type: "enum", Offer: "products"}}
		case "unattend":
			tpl.Params = []model.ParamDecl{{Name: "edition", Type: "enum", Offer: "editions"}}
			// The native pre-hook and InstallTo must name the same disk. This shipped
			// profile pins disk0; deriving it for another disk requires changing both.
			tpl.Assembly["partition"] = []model.Instance{fixedInstance("partition", "disk_index", "0", false)}
			tpl.Assembly["configure"] = []model.Instance{fixedInstance("packages", "packages", "Telnet-Client", true), instance("sshkey")}
			delete(tpl.Assembly, "packages")
			delete(tpl.Assembly, "sshkey")
		case "esxi-ks":
			tpl.Assembly["partition"] = []model.Instance{fixedInstance("partition", "disk", "local", true)}
			tpl.Assembly["network"] = []model.Instance{fixedInstance("network", "interface", "vmnic0", true)}
			tpl.Assembly["configure"] = []model.Instance{instance("packages"), fixedInstance("sshkey", "username", "root", false)}
			delete(tpl.Assembly, "packages")
			delete(tpl.Assembly, "sshkey")
		}
		result = append(result, tpl)
	}
	return result
}

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
