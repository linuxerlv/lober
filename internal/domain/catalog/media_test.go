package catalog

import (
	"slices"
	"testing"
)

type mediaExpectation struct {
	series       Series
	format       Format
	markers      []Marker
	editionHints []string
	probe        ProbeKind
	repoPaths    []string
	extra        *ExtraSource
	roles        []string
}

var mediaFormats = []struct {
	format  Format
	markers []Marker
}{
	{"kickstart", []Marker{{Path: ".treeinfo"}}},
	{"autoinstall", []Marker{{Path: ".disk/info", Contains: []string{"Ubuntu"}}}},
	{"preseed", []Marker{{Path: ".disk/info", Contains: []string{"Debian"}}, {Path: "dists/"}}},
	{"autoyast", []Marker{{Path: "repodata/repomd.xml"}, {Path: "media.1/products"}, {Path: "LiveOS/squashfs.img", Absent: true}}},
	{"agama", []Marker{{Path: "repodata/repomd.xml"}, {Path: "media.1/products"}, {Path: "LiveOS/squashfs.img"}}},
	{"esxi-ks", []Marker{{Path: "boot.cfg"}, {Path: "efi/boot/bootx64.efi"}}},
	{"unattend", []Marker{{Path: "sources/install.wim"}}},
}

var mediaSeries = []mediaExpectation{
	{"rhel", "kickstart", []Marker{{Path: ".treeinfo", Contains: []string{"Red Hat Enterprise Linux"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"rocky", "kickstart", []Marker{{Path: ".treeinfo", Contains: []string{"Rocky Linux"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"almalinux", "kickstart", []Marker{{Path: ".treeinfo", Contains: []string{"AlmaLinux"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"openeuler", "kickstart", []Marker{{Path: ".treeinfo", Contains: []string{"openEuler"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"opencloudos", "kickstart", []Marker{{Path: ".treeinfo", Contains: []string{"OpenCloudOS"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"debian", "preseed", nil, nil, ProbeDiskInfo, nil, &ExtraSource{Role: "netboot", SameVersion: true, RolesFromExtra: []string{"kernel", "initrd"}}, []string{"kernel", "initrd", "repo"}},
	{"ubuntu", "autoinstall", nil, nil, ProbeDiskInfo, nil, nil, []string{"kernel", "initrd", "iso"}},
	{"sles15", "autoyast", []Marker{{Path: "media.1/products", Contains: []string{"SLES"}}}, nil, ProbeProducts, []string{"repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "repo"}},
	{"leap15", "autoyast", []Marker{{Path: "media.1/products", Contains: []string{"openSUSE"}}}, nil, ProbeProducts, []string{"repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "repo"}},
	{"sles16", "agama", []Marker{{Path: "media.1/products", Contains: []string{"SLES"}}, {Path: "LiveOS/squashfs.img"}}, nil, ProbeProducts, []string{"repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "live-root", "repo"}},
	{"leap16", "agama", []Marker{{Path: "media.1/products", Contains: []string{"openSUSE"}}, {Path: "LiveOS/squashfs.img"}}, nil, ProbeProducts, []string{"repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "live-root", "repo"}},
	{"windows-server", "unattend", nil, []string{"Server"}, ProbeWIM, nil, nil, []string{"wimboot", "bcd", "boot-sdi", "boot-wim", "winpeshl", "winpe-busybox", "winpe-curl"}},
	{"windows-desktop", "unattend", nil, nil, ProbeWIM, nil, nil, []string{"wimboot", "bcd", "boot-sdi", "boot-wim", "winpeshl", "winpe-busybox", "winpe-curl"}},
	{"esxi", "esxi-ks", nil, nil, ProbeBootCfg, nil, nil, []string{"mboot", "esxi-tree"}},
}

// pendingMedia 是还需要拿真 ISO 坐实的介质事实。它不是实现的兜底：目录仍给出首版规则，
// 这张清单要求后续真机验证显式处理每一项，不能把不确定性藏在注释里。
var pendingMedia = []struct {
	series Series
	fact   string
}{
	{"rhel", ".treeinfo 里的实际 name= 字样"},
	{"rocky", ".treeinfo 里的实际 name= 字样"},
	{"almalinux", ".treeinfo 里的实际 name= 字样"},
	{"openeuler", ".treeinfo 里的实际 name= 字样"},
	{"opencloudos", ".treeinfo 里的实际 name= 字样"},
	{"debian", ".disk/info 的版本与架构取法"},
	{"ubuntu", ".disk/info 的版本与架构取法"},
	{"sles15", "media.1/products 的产品名与版本字段"},
	{"leap15", "media.1/products 的产品名与版本字段"},
	{"sles16", "media.1/products 的版本字段"},
	{"leap16", "media.1/products 的版本字段"},
	{"esxi", "ISO 内的版本识别依据；首版明确不能 ready"},
}

func Test介质目录与计划一致(t *testing.T) {
	wantFormats := make([]Format, 0, len(mediaFormats))
	for _, want := range mediaFormats {
		wantFormats = append(wantFormats, want.format)
		got, ok := FormatMedia(want.format)
		if !ok || !equalMarkers(got.Markers, want.markers) {
			t.Errorf("FormatMedia(%q) = %#v、%v，想要 markers=%#v、true", want.format, got, ok, want.markers)
		}
	}
	if got := Formats(); !slices.Equal(got, wantFormats) {
		t.Errorf("Formats() = %q，想要 %q", got, wantFormats)
	}

	if len(mediaSeries) != len(seriesTable) {
		t.Fatalf("计划列了 %d 个系，目录里有 %d 个系", len(mediaSeries), len(seriesTable))
	}
	for _, want := range mediaSeries {
		got, ok := SeriesMedia(want.series)
		if !ok || !equalMarkers(got.Markers, want.markers) || !slices.Equal(got.EditionHints, want.editionHints) || got.Probe != want.probe {
			t.Errorf("SeriesMedia(%q) = %#v、%v，想要 markers=%#v、editionHints=%q、probe=%q、true", want.series, got, ok, want.markers, want.editionHints, want.probe)
		}
		if got := RepoPaths(want.series); !slices.Equal(got, want.repoPaths) {
			t.Errorf("RepoPaths(%q) = %q，想要 %q", want.series, got, want.repoPaths)
		}
		if got := RequiredRoles(want.series); !slices.Equal(got, want.roles) {
			t.Errorf("RequiredRoles(%q) = %q，想要 %q", want.series, got, want.roles)
		}
		gotExtra, gotExtraOK := ExtraSourceOf(want.series)
		if want.extra == nil {
			if gotExtraOK || gotExtra != nil {
				t.Errorf("ExtraSourceOf(%q) = %#v、%v，想要 nil、false", want.series, gotExtra, gotExtraOK)
			}
		} else if !gotExtraOK || !equalExtra(gotExtra, want.extra) {
			t.Errorf("ExtraSourceOf(%q) = %#v、%v，想要 %#v、true", want.series, gotExtra, gotExtraOK, want.extra)
		}
	}
}

func Test介质目录完整且引用合法(t *testing.T) {
	allRoles := map[string]bool{
		"kernel": true, "initrd": true, "stage2": true, "repo": true, "iso": true, "live-root": true,
		"mboot": true, "esxi-tree": true, "wimboot": true, "bcd": true, "boot-sdi": true, "boot-wim": true,
		"winpeshl": true, "winpe-busybox": true, "winpe-curl": true,
	}
	seen := map[Series]bool{}
	probes := map[ProbeKind]bool{}
	for _, row := range seriesMediaTable {
		if !row.series.Valid() || seen[row.series] {
			t.Errorf("介质表登记未知或重复的系 %q", row.series)
		}
		seen[row.series] = true
		probes[row.probe] = true
		if len(row.roles) == 0 {
			t.Errorf("系 %q 没有必需制品角色", row.series)
		}
		for _, role := range row.roles {
			if !allRoles[role] {
				t.Errorf("系 %q 有未知制品角色 %q", row.series, role)
			}
		}
	}
	for _, row := range seriesTable {
		if !seen[row.id] {
			t.Errorf("系 %q 没有介质事实，后续导入不会有识别与制品规则", row.id)
		}
	}
	for _, probe := range []ProbeKind{ProbeTreeinfo, ProbeDiskInfo, ProbeProducts, ProbeBootCfg, ProbeWIM} {
		if !probes[probe] {
			t.Errorf("介质表没有使用 probe %q", probe)
		}
	}
}

func Test待真机核实的介质事实仍被跟踪(t *testing.T) {
	if len(pendingMedia) == 0 {
		t.Fatal("待真机核实的介质事实被静默清空")
	}
	for _, item := range pendingMedia {
		if !item.series.Valid() || item.fact == "" {
			t.Errorf("待核实项 %#v 没有已知系或具体事实", item)
		}
	}
}

func TestWindowsEdition归类(t *testing.T) {
	cases := []struct {
		name         string
		names, flags []string
		want         Series
		ok           bool
	}{
		{"只有 Server", []string{"Windows Server 2025 Standard"}, []string{"ServerStandard"}, "windows-server", true},
		{"只有桌面版", []string{"Windows 11 Pro"}, []string{"Professional"}, "windows-desktop", true},
		{"两者都有时优先 Server", []string{"Windows 11 Pro", "Windows Server 2025 Standard"}, []string{"Professional", "ServerStandard"}, "windows-server", true},
		{"没有 edition", nil, nil, "", false},
		{"空白 edition", []string{"", " "}, []string{""}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, ok := SeriesByEdition("unattend", c.names, c.flags); got != c.want || ok != c.ok {
				t.Errorf("SeriesByEdition(unattend, %q, %q) = %q、%v，想要 %q、%v", c.names, c.flags, got, ok, c.want, c.ok)
			}
		})
	}
	if got, ok := SeriesByEdition("kickstart", []string{"Server"}, nil); got != "" || ok {
		t.Errorf("SeriesByEdition(kickstart, Server, nil) = %q、%v，想要空、false", got, ok)
	}
}

func Test介质目录返回副本(t *testing.T) {
	media, ok := SeriesMedia("rocky")
	if !ok || len(media.Markers) == 0 || len(media.Markers[0].Contains) == 0 {
		t.Fatal("rocky 的介质事实不完整，验证不了副本")
	}
	media.Markers[0].Path = "changed"
	media.Markers[0].Contains[0] = "changed"
	if again, _ := SeriesMedia("rocky"); again.Markers[0].Path == "changed" || again.Markers[0].Contains[0] == "changed" {
		t.Error("改动 SeriesMedia 的结果影响了下一次查询")
	}

	roles := RequiredRoles("ubuntu")
	roles[0] = "changed"
	if again := RequiredRoles("ubuntu"); again[0] == "changed" {
		t.Error("改动 RequiredRoles 的结果影响了下一次查询")
	}
	extra, ok := ExtraSourceOf("debian")
	if !ok || extra == nil {
		t.Fatal("debian 没有附加来源，验证不了副本")
	}
	extra.RolesFromExtra[0] = "changed"
	if again, _ := ExtraSourceOf("debian"); again.RolesFromExtra[0] == "changed" {
		t.Error("改动 ExtraSourceOf 的结果影响了下一次查询")
	}
}

func equalMarkers(got, want []Marker) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].Path != want[i].Path || got[i].Absent != want[i].Absent || !slices.Equal(got[i].Contains, want[i].Contains) {
			return false
		}
	}
	return true
}

func equalExtra(got, want *ExtraSource) bool {
	return got != nil && want != nil && got.Role == want.Role && got.SameVersion == want.SameVersion && slices.Equal(got.RolesFromExtra, want.RolesFromExtra)
}
