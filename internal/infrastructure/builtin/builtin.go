package builtin

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
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
	// packages v3 removes the hard-coded base packages: the packages parameter
	// is now the only source, so the snapshot and the answer cannot disagree.
	switch name {
	case "packages":
		version = 3
	case "account", "partition":
		version = 2
	}
	return model.Instance{HookID: "builtin-" + name, HookVersion: version}
}

func fixedInstance(name, param, value string, editable bool) model.Instance {
	in := instance(name)
	in.Bindings = map[string]model.Binding{param: {Source: model.BindingFixed, Value: value, Editable: editable}}
	return in
}

func preseedParams() []model.ParamDecl {
	// A DVD tree is not a signed HTTP APT repository. Keep repository selection
	// explicit and let the installer enforce archive signatures.
	return []model.ParamDecl{
		{Name: "mirror_host", Label: "Debian mirror host", Type: "word", Default: "deb.debian.org"},
		{Name: "mirror_path", Label: "Debian mirror path", Type: "word", Default: "/debian"},
	}
}

// inventoryPreseedTemplate 是 Debian/Ubuntu 的采集变体：系统盘不再手填，锁定取自主机
// 属性 inv_system_disk。主机没采过时报 param_attr_missing，操作员会看到“先采集”的提示，
// 而不是悄悄按 sda 打错盘。
func inventoryPreseedTemplate() model.Template {
	ss := series("preseed")
	tpl := model.Template{
		ID: "builtin-preseed-inventory", Version: 1, Name: "lober:preseed-inventory",
		State: model.VersionApproved, Builtin: true,
		Family: ss[0].Family(), Series: ss, Body: body("preseed", "answer.cfg"),
		Params: preseedParams(), Assembly: map[string][]model.Instance{},
	}
	for _, name := range []string{"network", "account", "sshkey"} {
		tpl.Assembly[name] = []model.Instance{instance(name)}
	}
	tpl.Assembly["partition"] = []model.Instance{{
		HookID: "builtin-partition", HookVersion: instance("partition").HookVersion,
		Bindings: map[string]model.Binding{"disk": {Source: model.BindingAttr, Attr: "inv_system_disk"}},
	}}
	tpl.Assembly["packages"] = []model.Instance{fixedInstance("packages", "packages", defaultPackages("preseed"), true)}
	return tpl
}

// defaultPackages 是内置 Linux 模板给 packages 参数的默认值。SUSE 系的 SSH 服务包
// 叫 openssh，其余 Linux 系叫 openssh-server。它现在只存在这一处：应答文件里不再
// 写死包名，快照里的 packages 取值就是最终会装的包。
func defaultPackages(format catalog.Format) string {
	if format == "autoyast" || format == "agama" {
		return "curl\nsystemd\nopenssh"
	}
	return "curl\nsystemd\nopenssh-server"
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
		// Agama v3 改用安装器实际支持的 UTC 时区名。packages hook v3 去掉了
		// 应答文件里的写死包，所有模板改引用 v3 并升版本。
		version := 3 // autoinstall、autoyast、unattend、esxi
		if format == "kickstart" || format == "preseed" || format == "agama" {
			version = 4
		}
		ss := series(format)
		tpl := model.Template{ID: "builtin-" + id, Version: version, Name: "lober:" + id, State: model.VersionApproved, Builtin: true, Family: ss[0].Family(), Series: ss, Body: body(format, file), Assembly: map[string][]model.Instance{}}
		for _, name := range []string{"partition", "network", "account", "sshkey"} {
			tpl.Assembly[name] = []model.Instance{instance(name)}
		}
		tpl.Assembly["packages"] = []model.Instance{fixedInstance("packages", "packages", defaultPackages(format), true)}
		switch format {
		case "preseed":
			tpl.Params = preseedParams()
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
	return append(result, inventoryPreseedTemplate())
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
