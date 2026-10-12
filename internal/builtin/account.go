package builtin

import "github.com/lober/lober/internal/core/model"

func accountHook() model.Hook {
	h := hook("account", 1, "Native account provisioning; supply a target-compatible crypt hash or Windows password. Windows AutoLogon runs the ordered firstboot chain once.")
	h.Params = []model.ParamDecl{{Name: "username", Type: "word", Default: "lober"}, {Name: "pwhash", Type: "pwhash"}, {Name: "password", Type: "word"}}
	h.Impls = []model.Impl{
		fragment("kickstart", "account.cfg"), fragment("preseed", "account.cfg"), fragment("autoinstall", "account.yaml"), fragment("autoyast", "account.xml"), fragment("agama", "account.jsonnet"), fragment("unattend", "account.xml"), fragment("esxi-ks", "account.cfg"),
	}
	return h
}
