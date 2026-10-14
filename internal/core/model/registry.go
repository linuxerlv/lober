package model

import (
	"maps"
	"slices"

	"github.com/lober/lober/internal/core/catalog"
)

// Host 是一台登记过的主机。SN 是它唯一的身份（C6），其余都是可选属性（C7）。
// 第 2 层原样存它，字段名就是最终的名字。
type Host struct {
	SN string // 已经过 NormalizeSN
	// Arch 与 Firmware 可空：登记了才参与兼容校验（C28）。非空时必须是目录里的原样写法，
	// 由 Validate 查。
	Arch     catalog.Arch
	Firmware catalog.Firmware
	// Attrs 是自定义属性，如 IP 与前缀、主机名、机架位。键是标识符；值原样存、系统不解释，
	// 被参数引用时才按那个参数的类型校验。
	Attrs map[string]string
	// Rev 是仓储的乐观锁版本：每次写入加 1，由仓储维护；调用方改记录时原样带回，对不上就写不进去。
	// CSV 批量导入例外：已登记的按 SN 整条替换，不比它。第 1 层的判定都不看它，快照里也不放它。
	Rev int64
}

// hostReservedAttrs 不能当属性键：它们是主机自己的字段，CSV 导入按列名把这几列填进字段、
// 其余列才成为属性。属性键再用这几个名字，同一列就有了两种去处。
var hostReservedAttrs = []string{"sn", "arch", "firmware"}

// HostReservedAttrs returns an independent copy for CSV/schema consumers.
func HostReservedAttrs() []string { return slices.Clone(hostReservedAttrs) }

// ValidAttrKey 判断主机属性键：是标识符（ValidIdent），且不是保留键 sn、arch、firmware。
// Host.Validate 与激活判定查 attr 取值来源都用它，保留键表只在本包一处（T4-1）。
func ValidAttrKey(key string) bool {
	return ValidIdent(key) && !slices.Contains(hostReservedAttrs, key)
}

// Validate 校验主机登记（T4-1、T13-10）：非空的架构、固件必须是目录里的原样写法，大小写不对、
// 别名和 iPXE 的上报值都不算（host_platform_invalid）；属性键要过 ValidAttrKey（attr_key_invalid）；
// 属性值要存得下（text_not_storable）。SN 在登记时已经过 NormalizeSN；属性值是什么格式，要等
// 参数引用时才知道该按什么类型查。
//
// 问题先架构、后固件，再按属性键的字典序，同一个键先报键、再报值。Subject 的 Kind 为 host，ID 为 SN，
// Path 是 arch、firmware 或那个属性键。
func (h Host) Validate() []Problem {
	var out []Problem
	if h.Arch != "" && !h.Arch.Valid() {
		out = append(out, NewProblem("host_platform_invalid", h.at("arch"), fieldLabels["arch"], string(h.Arch)))
	}
	if h.Firmware != "" && !h.Firmware.Valid() {
		out = append(out, NewProblem("host_platform_invalid", h.at("firmware"), fieldLabels["firmware"], string(h.Firmware)))
	}
	for _, key := range slices.Sorted(maps.Keys(h.Attrs)) {
		if !ValidAttrKey(key) {
			out = append(out, NewProblem("attr_key_invalid", h.at(key), key))
		}
		if !StorableText(h.Attrs[key]) {
			out = append(out, NewProblem("text_not_storable", h.at(key)))
		}
	}
	return out
}

func (h Host) at(path string) Subject {
	return Subject{Kind: "host", ID: h.SN, Path: path}
}

// ISOState 是 ISO 的状态。字面量会存进数据库，发布后不能改名；显示名只给说明与界面看。
type ISOState string

const (
	ISOAcquiring  ISOState = "acquiring"  // 获取中：字节还在下载或上传
	ISOImporting  ISOState = "importing"  // 导入中
	ISOReady      ISOState = "ready"      // 可用
	ISOFailed     ISOState = "failed"     // 导入失败：字节到齐后的校验或导入没通过（spec §5.1 第 3、4 步），可以直接重新导入（第 5 步）
	ISODeprecated ISOState = "deprecated" // 已废弃：已建的任务不受影响，新任务不能再用（C26）
)

type isoStateRow struct {
	id    ISOState
	label string
}

// isoStateTable 是全部 ISO 状态及其显示名，照 catalog 的做法登记成表：准入的说明里显示的是中文的
// 现状，而不是 deprecated 这类标识（T5-1）。
var isoStateTable = []isoStateRow{
	{ISOAcquiring, "获取中"},
	{ISOImporting, "导入中"},
	{ISOReady, "可用"},
	{ISOFailed, "导入失败"},
	{ISODeprecated, "已废弃"},
}

// Valid 报告 s 是不是 ISO 的状态，只认原样的写法（T7-2）。第 2 层的 CHECK 约束、第 5 层保存时的
// 拒收都用它，不另抄取值表。
func (s ISOState) Valid() bool {
	_, ok := isoStateRowOf(s)
	return ok
}

// Label 是状态的中文显示名；不认识的状态返回空串，由调用方回落到标识。
func (s ISOState) Label() string {
	r, _ := isoStateRowOf(s)
	return r.label
}

func isoStateRowOf(s ISOState) (isoStateRow, bool) {
	for _, r := range isoStateTable {
		if r.id == s {
			return r, true
		}
	}
	return isoStateRow{}, false
}

// StateLabel 是状态在问题说明里的称呼，iso_not_ready、template_not_approved 的参数都用它（T5-1、C-7）：
// 中文显示名；不认识的状态没有显示名，回落到标识；空状态连标识也没有，回落到 codes.go 登记的
// stateUnknown，说明里才不会出现「现为「」」。ISO 的状态与版本的状态共用这一条规则，界面显示状态仍用
// Label。
func StateLabel[S interface {
	~string
	Label() string
}](s S) string {
	switch {
	case s.Label() != "":
		return s.Label()
	case s != "":
		return string(s)
	}
	return stateUnknown
}

// ISO 是一份登记过的安装介质。族和系在创建时就声明（C2），这时字节可能还没到齐，
// 所以版本、架构、制品和提供的集合都要等导入之后才有。导入完成的 ISO 必须带合法的版本和
// 架构（C5）：第 4 层置 ready 之前调用 Validate，不通过就不置 ready。
type ISO struct {
	ID     string
	Name   string // 要存得下（StorableText），可以用中文
	Family catalog.Family
	Series catalog.Series
	// Version 导入时按内容识别，要合 ^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$（L-6）。它会成为制品目录里的
	// 一段路径（rocky/9.6/x86_64/…）：. 与 .. 会让不同的 ISO 落进同一个目录，或者穿出制品目录；以 -
	// 开头的放进命令行会被当成选项。所以首字符必须是字母或数字。
	Version string
	Arch    catalog.Arch
	State   ISOState
	SHA256  string // 内容的 SHA256，相同的只导入一次（C4）
	// Artifacts 是角色到制品目录下相对路径的映射，角色由第 4 层定。只存相对路径：
	// 完整地址由渲染按制品目录布局拼出。
	Artifacts map[string]string
	// Offers 是 ISO 提供的取值集合，集合名到选项，如 Windows 的 editions。声明了 Offer 的
	// enum 参数，取值必须在同名的集合里（C21）。集合名是标识符（ValidIdent），参数声明的 Offer 按它
	// 引用；选项的规则与 enum 的静态选项相同（T7-5、C-8）：值原样进正文，必须是安全词（IsSafeWord），
	// 同一个集合里不重复；显示名过 ValidLabel。第 4 层导入 editions 时，Value 用 ServerDatacenter 这类
	// 安全词，全名放 Label。
	Offers map[string][]Option
	// Rev 是仓储的乐观锁版本：每次写入加 1，由仓储维护；调用方改记录时原样带回，对不上就写不进去。
	// 第 1 层的判定都不看它，快照里也不放它。
	Rev int64
}

// isoVersionMaxLen 是 ISO 版本的最大长度，见 ISO.Version。
const isoVersionMaxLen = 32

// Validate 校验 ISO 的记录本身，不看状态（L-6、T7-5）：族合法；系合法且属于该族；架构合法；版本合
// ISO.Version 的规则；名称存得下；提供的集合合 ISO.Offers 的规则。第 4 层置 ready 之前调用它。
//
// 问题依次落在 family、series、arch、version、name 上，再按集合名的字典序落在 offers/<集合名> 上；
// Subject 的 Kind 为 iso，ID 为 ISO 的 ID，Path 是这个字段。名称存不下报 text_not_storable，其余报
// iso_incomplete，参数是字段的中文称呼。族不对时只查系认不认识、不拿它去比族：错的是族，系本身
// 没错，不连带报它。一个集合里有几处毛病也只报一条，问题落在同一个集合上，说明是同一句。
func (iso ISO) Validate() []Problem {
	var out []Problem
	fields := []struct {
		name string
		ok   bool
	}{
		{"family", iso.Family.Valid()},
		{"series", iso.Series.Valid() && (!iso.Family.Valid() || iso.Series.Family() == iso.Family)},
		{"arch", iso.Arch.Valid()},
		{"version", validISOVersion(iso.Version)},
	}
	for _, f := range fields {
		if !f.ok {
			out = append(out, NewProblem("iso_incomplete", iso.at(f.name), fieldLabels[f.name]))
		}
	}
	if !StorableText(iso.Name) {
		out = append(out, NewProblem("text_not_storable", iso.at("name")))
	}
	for _, name := range slices.Sorted(maps.Keys(iso.Offers)) {
		// 选项的规则与 enum 的静态选项是同一份（validOptions）；空集合也算合格，它等同于没提供，要它的
		// 参数在求值与准入时报 iso_offer_missing。
		if !ValidIdent(name) || !validOptions(iso.Offers[name]) {
			out = append(out, NewProblem("iso_incomplete", iso.at("offers/"+name), fieldLabels["offers"]))
		}
	}
	return out
}

// validISOVersion 判断 ISO 的版本：^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$，理由见 ISO.Version。
func validISOVersion(v string) bool {
	if v == "" || len(v) > isoVersionMaxLen || !isASCIIAlnum(v[0]) {
		return false
	}
	for i := 1; i < len(v); i++ {
		if c := v[i]; !isASCIIAlnum(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// Unusable 给出 ISO 不能用来建任务的原因，能用时返回 nil（C5、C26）。导入失败报 iso_import_failed：
// 它不用等，可以直接重新导入。其余不是 ready 的状态报 iso_not_ready，参数是 StateLabel 给的称呼：
// 中文显示名，不认识的回落到标识，空状态回落到「未知状态」（T5-1、C-7）。ready 却记录不合法的违反 C5，
// 返回 Validate 的问题。Subject 的 Kind 为 iso，ID 为 ISO 的 ID。
//
// 不是 ready 的 ISO 不拿记录去校验：获取中、导入中的本来就还没有版本和架构。
func (iso ISO) Unusable() []Problem {
	switch iso.State {
	case ISOReady:
		return iso.Validate()
	case ISOFailed:
		return []Problem{NewProblem("iso_import_failed", iso.at(""))}
	}
	return []Problem{NewProblem("iso_not_ready", iso.at(""), StateLabel(iso.State))}
}

// Usable 报告 ISO 能不能用来建任务，就是 Unusable 为空（L-6）：只有 ready 且记录合法的算（C5），
// 已废弃的不能再被新引用（C26）。
func (iso ISO) Usable() bool {
	return len(iso.Unusable()) == 0
}

func (iso ISO) at(path string) Subject {
	return Subject{Kind: "iso", ID: iso.ID, Path: path}
}
