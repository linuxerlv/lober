package catalog

import (
	"reflect"
	"testing"
)

func Test全局词表与登记表逐项一致(t *testing.T) {
	cases := []struct {
		name      string
		got, want any
	}{
		{"formats", AllFormats(), []Format{"kickstart", "preseed", "autoinstall", "autoyast", "agama", "unattend", "esxi-ks"}},
		{"series", AllSeries(), []Series{"rhel", "rocky", "almalinux", "openeuler", "opencloudos", "debian", "ubuntu", "sles15", "leap15", "sles16", "leap16", "windows-server", "windows-desktop", "esxi"}},
		{"timings", AllTimings(), []Timing{"pre", "post-installer", "post-target", "firstboot"}},
		{"interpreters", AllInterpreters(), []Interpreter{"sh", "bash", "cmd", "powershell"}},
		{"parts", AllSystemParts(), []SystemPart{"syslog", "firstboot", "cleanup"}},
		{"values", AllSystemValues(), []SystemValue{"repo", "iso", "smb", "gateway", "task", "sn", "lbrace", "token", "syslog-addr", "artifacts", "finalize-token"}},
		{"archs", AllArchs(), []Arch{"x86_64", "aarch64"}},
		{"firmwares", AllFirmwares(), []Firmware{"bios", "uefi"}},
	}
	for _, tt := range cases {
		if !reflect.DeepEqual(tt.got, tt.want) {
			t.Errorf("%s: got %v want %v", tt.name, tt.got, tt.want)
		}
	}
}
func Test全局词表返回副本(t *testing.T) {
	for _, load := range []func() any{func() any { return AllFormats() }, func() any { return AllSeries() }, func() any { return AllTimings() }, func() any { return AllInterpreters() }, func() any { return AllSystemParts() }, func() any { return AllSystemValues() }, func() any { return AllArchs() }, func() any { return AllFirmwares() }} {
		before := load()
		changed := reflect.ValueOf(load())
		changed.Index(0).Set(reflect.ValueOf("changed").Convert(changed.Index(0).Type()))
		if !reflect.DeepEqual(load(), before) {
			t.Fatal("caller changed registry")
		}
	}
}
func Test词表全局查询覆盖已登记项(t *testing.T) {
	if len(AllFormats()) != len(formatTable) || len(AllSeries()) != len(seriesTable) || len(AllTimings()) != len(timingTable) || len(AllInterpreters()) != len(interpreterTable) || len(AllSystemParts()) != len(systemPartTable) || len(AllSystemValues()) != len(systemValueTable) || len(AllArchs()) != len(platformArchTable) || len(AllFirmwares()) != len(platformFirmwareTable) {
		t.Fatal("registry item omitted")
	}
}
