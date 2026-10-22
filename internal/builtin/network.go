package builtin

import "github.com/lober/lober/internal/domain/model"

func networkHook() model.Hook {
	h := hook("network", 1, "DHCP network and host naming. Interface names must match the selected target.")
	h.Params = []model.ParamDecl{{Name: "hostname", Type: "hostname", Default: "lober-node"}, {Name: "interface", Type: "word", Default: "eth0"}}
	h.Impls = []model.Impl{
		fragment("kickstart", "network.cfg"), fragment("preseed", "network.cfg"), fragment("autoinstall", "network.yaml"), fragment("autoyast", "network.xml"), fragment("agama", "network.jsonnet"), fragment("unattend", "network.xml"), fragment("esxi-ks", "network.cfg"),
	}
	return h
}
