package builtin

import "github.com/lober/lober/internal/core/model"

func accountHook() model.Hook {
	// v2: the autoyast fragment drops create_home, which is not a valid <user>
	// child in the Leap 15.6 AutoYaST schema (yast2-schema-default) and made
	// YaST reject the whole <users> element as an extra interleave branch (D3).
	h := hook("account", 2, "Native account provisioning; supply a target-compatible crypt hash or Windows password. Windows AutoLogon runs the ordered firstboot chain once.")
	h.Params = []model.ParamDecl{{Name: "username", Type: "word", Default: "lober"}, {Name: "pwhash", Type: "pwhash"}, {Name: "password", Type: "word"}}
	h.Impls = []model.Impl{
		fragment("kickstart", "account.cfg"), fragment("preseed", "account.cfg"), fragment("autoinstall", "account.yaml"), fragment("autoyast", "account.xml"), fragment("agama", "account.jsonnet"), fragment("unattend", "account.xml"), fragment("esxi-ks", "account.cfg"),
	}
	return h
}
