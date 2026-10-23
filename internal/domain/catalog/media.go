package catalog

import (
	"slices"
	"strings"
)

// Marker 是一条安装介质线索：Path 必须存在；Contains 非空时，文件内容必须包含其中每一段。
// 一组 Marker 是合取关系。它们由 ISO 导入层解释，目录只登记静态事实。
type Marker struct {
	Path     string
	Contains []string
	Absent   bool // 为真时要求该路径不存在，用于区分传统 SUSE 与 live 介质。
}

// ExtraSource 是一个系要求的附加介质。现在只有 Debian 的 netboot 包；它与主介质必须同版本，
// 并提供 kernel 和 initrd 两个角色。
type ExtraSource struct {
	Role           string
	SameVersion    bool
	RolesFromExtra []string
}

// ProbeKind 指定 ISO 导入层如何从特征文件读取版本和架构。
type ProbeKind string

const (
	ProbeTreeinfo ProbeKind = "treeinfo"
	ProbeDiskInfo ProbeKind = "diskinfo"
	ProbeProducts ProbeKind = "products"
	ProbeBootCfg  ProbeKind = "bootcfg"
	ProbeWIM      ProbeKind = "wim"
)

// Media 是格式或系的只读介质事实视图。调用方得到的切片均为副本，可以自由改写。
type Media struct {
	Markers      []Marker
	EditionHints []string
	Probe        ProbeKind
}

type formatMediaRow struct {
	format  Format
	markers []Marker
}

type seriesMediaRow struct {
	series       Series
	markers      []Marker
	editionHints []string
	probe        ProbeKind
	repoPaths    []string
	extraSource  *ExtraSource
	roles        []string
}

// formatMediaTable 的顺序就是识别顺序。kickstart 必须在 SUSE 之前，Ubuntu 必须在 Debian
// 之前；否则同一张介质可能被错误地归到后一个格式。
var formatMediaTable = []formatMediaRow{
	{"kickstart", []Marker{{Path: ".treeinfo"}}},
	{"autoinstall", []Marker{{Path: ".disk/info", Contains: []string{"Ubuntu"}}}},
	{"preseed", []Marker{{Path: ".disk/info", Contains: []string{"Debian"}}, {Path: "dists/"}}},
	{"autoyast", []Marker{{Path: "repodata/repomd.xml"}, {Path: "media.1/products"}, {Path: "LiveOS/squashfs.img", Absent: true}}},
	{"agama", []Marker{{Path: "repodata/repomd.xml"}, {Path: "media.1/products"}, {Path: "LiveOS/squashfs.img"}}},
	{"esxi-ks", []Marker{{Path: "boot.cfg"}, {Path: "efi/boot/bootx64.efi"}}},
	{"unattend", []Marker{{Path: "sources/install.wim"}}},
}

var seriesMediaTable = []seriesMediaRow{
	{"rhel", []Marker{{Path: ".treeinfo", Contains: []string{"Red Hat Enterprise Linux"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"rocky", []Marker{{Path: ".treeinfo", Contains: []string{"Rocky Linux"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"almalinux", []Marker{{Path: ".treeinfo", Contains: []string{"AlmaLinux"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"openeuler", []Marker{{Path: ".treeinfo", Contains: []string{"openEuler"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"opencloudos", []Marker{{Path: ".treeinfo", Contains: []string{"OpenCloudOS"}}}, nil, ProbeTreeinfo, []string{"BaseOS/repodata/repomd.xml", "AppStream/repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "stage2", "repo"}},
	{"debian", nil, nil, ProbeDiskInfo, nil, &ExtraSource{Role: "netboot", SameVersion: true, RolesFromExtra: []string{"kernel", "initrd"}}, []string{"kernel", "initrd", "repo"}},
	{"ubuntu", nil, nil, ProbeDiskInfo, nil, nil, []string{"kernel", "initrd", "iso"}},
	{"sles15", []Marker{{Path: "media.1/products", Contains: []string{"SLES"}}}, nil, ProbeProducts, []string{"repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "repo"}},
	{"leap15", []Marker{{Path: "media.1/products", Contains: []string{"openSUSE"}}}, nil, ProbeProducts, []string{"repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "repo"}},
	{"sles16", []Marker{{Path: "media.1/products", Contains: []string{"SLES"}}, {Path: "LiveOS/squashfs.img"}}, nil, ProbeProducts, []string{"repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "live-root", "repo"}},
	{"leap16", []Marker{{Path: "media.1/products", Contains: []string{"openSUSE"}}, {Path: "LiveOS/squashfs.img"}}, nil, ProbeProducts, []string{"repodata/repomd.xml"}, nil, []string{"kernel", "initrd", "live-root", "repo"}},
	{"windows-server", nil, []string{"Server"}, ProbeWIM, nil, nil, []string{"wimboot", "bcd", "boot-sdi", "boot-wim", "winpeshl", "winpe-busybox", "winpe-curl"}},
	{"windows-desktop", nil, nil, ProbeWIM, nil, nil, []string{"wimboot", "bcd", "boot-sdi", "boot-wim", "winpeshl", "winpe-busybox", "winpe-curl"}},
	{"esxi", nil, nil, ProbeBootCfg, nil, nil, []string{"mboot", "esxi-tree"}},
}

// FormatMedia 返回格式的识别线索；未知格式返回 false。
func FormatMedia(f Format) (Media, bool) {
	for _, row := range formatMediaTable {
		if row.format == f {
			return Media{Markers: cloneMarkers(row.markers)}, true
		}
	}
	return Media{}, false
}

// SeriesMedia 返回系的识别线索与版本、架构探针；未知系返回 false。
func SeriesMedia(s Series) (Media, bool) {
	for _, row := range seriesMediaTable {
		if row.series == s {
			return Media{Markers: cloneMarkers(row.markers), EditionHints: slices.Clone(row.editionHints), Probe: row.probe}, true
		}
	}
	return Media{}, false
}

// Formats 按介质识别的优先顺序返回全部格式。
func Formats() []Format {
	out := make([]Format, 0, len(formatMediaTable))
	for _, row := range formatMediaTable {
		out = append(out, row.format)
	}
	return out
}

// RequiredRoles 返回系导入完成时必须存在的制品角色；未知系返回空。
func RequiredRoles(s Series) []string {
	row, ok := seriesMediaOf(s)
	if !ok {
		return nil
	}
	return slices.Clone(row.roles)
}

// ExtraSourceOf 返回系所需的附加来源；没有或未知时返回 nil、false。
func ExtraSourceOf(s Series) (*ExtraSource, bool) {
	row, ok := seriesMediaOf(s)
	if !ok || row.extraSource == nil {
		return nil, false
	}
	return &ExtraSource{Role: row.extraSource.Role, SameVersion: row.extraSource.SameVersion, RolesFromExtra: slices.Clone(row.extraSource.RolesFromExtra)}, true
}

// RepoPaths 返回仓库根内必须存在的相对路径；没有独立仓库树或未知系返回空。
func RepoPaths(s Series) []string {
	row, ok := seriesMediaOf(s)
	if !ok {
		return nil
	}
	return slices.Clone(row.repoPaths)
}

// SeriesByEdition 把 Windows install.wim 的 edition 名称与 flags 归到内置系。只要任一
// 名称或 flag 含 Server 就归 Windows Server；其余非空 edition 归 Windows 桌面版。
func SeriesByEdition(f Format, names, flags []string) (Series, bool) {
	if f != "unattend" || len(names) == 0 && len(flags) == 0 {
		return "", false
	}
	nonempty := false
	for _, values := range [][]string{names, flags} {
		for _, value := range values {
			nonempty = nonempty || strings.TrimSpace(value) != ""
			if strings.Contains(value, "Server") {
				return "windows-server", true
			}
		}
	}
	if !nonempty {
		return "", false
	}
	return "windows-desktop", true
}

func seriesMediaOf(s Series) (seriesMediaRow, bool) {
	for _, row := range seriesMediaTable {
		if row.series == s {
			return row, true
		}
	}
	return seriesMediaRow{}, false
}

func cloneMarkers(in []Marker) []Marker {
	out := make([]Marker, len(in))
	for i, marker := range in {
		out[i] = Marker{Path: marker.Path, Contains: slices.Clone(marker.Contains), Absent: marker.Absent}
	}
	return out
}
