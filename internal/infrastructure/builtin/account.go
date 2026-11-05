package builtin

import "github.com/lober/lober/internal/domain/model"

func accountHook() model.Hook {
	// 新版本只保留当前支持的系统，旧版本内容留在发布快照中。
	h := hook("account", 3, "Native account provisioning; supply a target-compatible crypt hash or Windows password. Windows AutoLogon runs the ordered firstboot chain once.")
	h.Params = []model.ParamDecl{{Name: "username", Type: "word", Default: "lober"}, {Name: "pwhash", Type: "pwhash"}, {Name: "password", Type: "word"}}
	h.Impls = []model.Impl{
		fragment("kickstart", "account.cfg"), fragment("preseed", "account.cfg"), fragment("autoinstall", "account.yaml"), fragment("unattend", "account.xml"), fragment("esxi-ks", "account.cfg"),
	}
	return h
}
