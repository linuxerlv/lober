package builtin

import (
	_ "embed"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

//go:embed shared/linux-sshkey.sh
var linuxSSHKeyBody string

func linuxSSHKey(format catalog.Format) model.Impl {
	ss := series(format)
	return model.Impl{Family: ss[0].Family(), Series: ss, Kind: model.ImplScript, Body: linuxSSHKeyBody, Interpreter: "sh", Timings: []catalog.Timing{"firstboot"}}
}

func sshkeyHook() model.Hook {
	h := hook("sshkey", 1, "SSH public key access. Match username to the account hook. Windows requires OpenSSH Server; only Server 2025 is guaranteed by the content contract.")
	h.Params = []model.ParamDecl{{Name: "username", Type: "word", Default: "lober"}, {Name: "key", Type: "sshkey", Required: true}}
	h.Impls = []model.Impl{
		fragment("kickstart", "sshkey.cfg"), linuxSSHKey("preseed"), fragment("autoinstall", "sshkey.yaml"), fragment("autoyast", "sshkey.xml"), linuxSSHKey("agama"),
		script("unattend", "sshkey.ps1", "powershell", "firstboot"), script("esxi-ks", "sshkey.sh", "sh", "firstboot"),
	}
	return h
}
