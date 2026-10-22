package builtin

import "github.com/lober/lober/internal/core/model"

func partitionHook() model.Hook {
	// v2: kickstart part commands inlined at render time with reqpart, which
	// creates the firmware-required boot partition (biosboot or /boot/efi);
	// AlmaLinux 10 resolves %include before %pre runs, so generated part
	// commands never reached the installer (D2).
	h := hook("partition", 2, "Destructive whole-disk installation. Native firmware determines boot partitions.")
	zero := int64(0)
	h.Params = []model.ParamDecl{{Name: "disk", Type: "word", Default: "sda"}, {Name: "disk_index", Type: "int", Default: "0", Min: &zero}}
	h.Impls = []model.Impl{
		fragment("kickstart", "partition.cfg"), fragment("preseed", "partition.cfg"), fragment("autoinstall", "partition.yaml"), fragment("autoyast", "partition.xml"), fragment("agama", "partition.jsonnet"),
		script("unattend", "partition.cmd", "cmd", "pre"), fragment("esxi-ks", "partition.cfg"),
	}
	return h
}
