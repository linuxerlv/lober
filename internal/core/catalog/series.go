package catalog

// Family 是族，给人分组用，固定 linux、windows、esxi 三个（C1）。
type Family string

// Format 是应答格式，决定执行时机、解释器、系统部件这些能力。一种格式只属于一个族。
type Format string

// Series 是系，发行版一级：ISO 声明它，模板勾选它。每个系属于一个族、对应一种格式；
// 同一发行版跨大版本换了安装器的拆成两个系，所以 SLES 和 openSUSE Leap 都按 15、16 分开。
type Series string

// 下面三张表就是全部取值。标识会写进数据库和 URL，发布后不能改名；显示名只给界面看。
// 系的族由它的格式推出，不在系上另记一份：两处各记一份，迟早会对不上。
// 表都很小，查询直接顺序扫描，不另建索引。

type familyRow struct {
	id    Family
	label string
}

type formatRow struct {
	id     Format
	family Family
	label  string
}

type seriesRow struct {
	id     Series
	format Format
	label  string
}

// familyTable 的顺序就是 Families 返回的顺序。
var familyTable = []familyRow{
	{FamilyLinux, "Linux"},
	{FamilyWindows, "Windows"},
	{FamilyESXi, "ESXi"},
}

// formatTable 里格式的显示名照 spec §2.1 与 C1 的写法，界面与文档才对得上：kickstart、
// preseed、autoinstall、unattend 就是标识本身，另三种是 AutoYaST、Agama、ESXi ks。
var formatTable = []formatRow{
	{FormatKickstart, FamilyLinux, "kickstart"},
	{FormatPreseed, FamilyLinux, "preseed"},
	{FormatAutoinstall, FamilyLinux, "autoinstall"},
	{FormatAutoyast, FamilyLinux, "AutoYaST"},
	{FormatAgama, FamilyLinux, "Agama"},
	{FormatUnattend, FamilyWindows, "unattend"},
	{FormatESXiKS, FamilyESXi, "ESXi ks"},
}

// seriesTable 的顺序就是 SeriesOf 与 SeriesOfFormat 返回的顺序。
var seriesTable = []seriesRow{
	{SeriesRHEL, FormatKickstart, "RHEL"},
	{SeriesRocky, FormatKickstart, "Rocky Linux"},
	{SeriesAlmalinux, FormatKickstart, "AlmaLinux"},
	{SeriesOpeneuler, FormatKickstart, "openEuler"},
	{SeriesOpencloudos, FormatKickstart, "OpenCloudOS"},
	{SeriesDebian, FormatPreseed, "Debian"},
	{SeriesUbuntu, FormatAutoinstall, "Ubuntu"},
	{SeriesSLES15, FormatAutoyast, "SLES 15"},
	{SeriesLeap15, FormatAutoyast, "openSUSE Leap 15"},
	{SeriesSLES16, FormatAgama, "SLES 16"},
	{SeriesLeap16, FormatAgama, "openSUSE Leap 16"},
	{SeriesWinServer, FormatUnattend, "Windows Server"},
	{SeriesWinDesktop, FormatUnattend, "Windows 桌面版"},
	{SeriesESXi, FormatESXiKS, "ESXi"},
}

// Valid 报告 f 是不是内置的族。族、格式、系都只认表里的原样写法，区分大小写、
// 不做规范化：它们由系统内置、用户不能增改（C1），来自用户的值一律按白名单收，
// 同一个标识也就不会在库里和 URL 里出现两种写法。
func (f Family) Valid() bool {
	_, ok := familyRowOf(f)
	return ok
}

// Label 是族的界面显示名；未知的族返回空串。
func (f Family) Label() string {
	r, _ := familyRowOf(f)
	return r.label
}

// Valid 报告 f 是不是内置的格式，规则同 Family.Valid。
func (f Format) Valid() bool {
	_, ok := formatRowOf(f)
	return ok
}

// Label 是格式的界面显示名；未知的格式返回空串。
func (f Format) Label() string {
	r, _ := formatRowOf(f)
	return r.label
}

// Valid 报告 s 是不是内置的系，规则同 Family.Valid。
func (s Series) Valid() bool {
	_, ok := seriesRowOf(s)
	return ok
}

// Label 是系的界面显示名；未知的系返回空串。
func (s Series) Label() string {
	r, _ := seriesRowOf(s)
	return r.label
}

// Family 是系所属的族，建 ISO 时据此确认声明的系属于声明的族（C2）；未知的系返回空串。
func (s Series) Family() Family {
	r, _ := formatRowOf(s.Format())
	return r.family
}

// Format 是系对应的应答格式；未知的系返回空串。
func (s Series) Format() Format {
	r, _ := seriesRowOf(s)
	return r.format
}

// Families 按声明顺序返回全部族。它和 SeriesOf、SeriesOfFormat 每次都返回新切片：
// 调用方拿去排序、改写，不会改到目录。
func Families() []Family {
	out := make([]Family, 0, len(familyTable))
	for _, r := range familyTable {
		out = append(out, r.id)
	}
	return out
}

// SeriesOf 按声明顺序返回族 f 下的系；未知的族返回空。
func SeriesOf(f Family) []Series {
	var out []Series
	for _, r := range seriesTable {
		if r.id.Family() == f {
			out = append(out, r.id)
		}
	}
	return out
}

// SeriesOfFormat 按声明顺序返回用格式 f 的系；未知的格式返回空。
func SeriesOfFormat(f Format) []Series {
	var out []Series
	for _, r := range seriesTable {
		if r.format == f {
			out = append(out, r.id)
		}
	}
	return out
}

func familyRowOf(f Family) (familyRow, bool) {
	for _, r := range familyTable {
		if r.id == f {
			return r, true
		}
	}
	return familyRow{}, false
}

func formatRowOf(f Format) (formatRow, bool) {
	for _, r := range formatTable {
		if r.id == f {
			return r, true
		}
	}
	return formatRow{}, false
}

func seriesRowOf(s Series) (seriesRow, bool) {
	for _, r := range seriesTable {
		if r.id == s {
			return r, true
		}
	}
	return seriesRow{}, false
}
