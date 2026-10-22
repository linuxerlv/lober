package builtin

import "github.com/lober/lober/internal/domain/model"

func packagesHook() model.Hook {
	h := hook("packages", 2, "Additional native packages, Windows Server features, or qualified individual ESXi VIB URLs.")
	h.TimeoutSec = 1800
	h.Params = []model.ParamDecl{{Name: "packages", Type: "word", List: true, Required: true}}
	h.Impls = []model.Impl{
		fragment("kickstart", "packages.cfg"), fragment("preseed", "packages.cfg"), fragment("autoinstall", "packages.yaml"), fragment("autoyast", "packages.xml"), fragment("agama", "packages.jsonnet"),
		script("unattend", "packages.ps1", "powershell", "firstboot"), script("esxi-ks", "packages.sh", "sh", "firstboot"),
	}
	return h
}
