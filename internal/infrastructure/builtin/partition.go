package builtin

import "github.com/lober/lober/internal/domain/model"

func partitionHook() model.Hook {
	// 新版本只保留当前支持的系统，旧版本内容留在发布快照中。
	h := hook("partition", 3, "Destructive whole-disk installation. Native firmware determines boot partitions.")
	zero := int64(0)
	h.Params = []model.ParamDecl{{Name: "disk", Type: "word", Default: "sda"}, {Name: "disk_index", Type: "int", Default: "0", Min: &zero}}
	h.Impls = []model.Impl{
		fragment("kickstart", "partition.cfg"), fragment("preseed", "partition.cfg"), fragment("autoinstall", "partition.yaml"),
		script("unattend", "partition.cmd", "cmd", "pre"), fragment("esxi-ks", "partition.cfg"),
	}
	return h
}
