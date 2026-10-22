package model

import (
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

// 属性值原样存、系统不解释（C7）：存得下就行，引号、尖括号、换行和制表符都照收，只有键要守规则。
func TestHostValidate属性键合法时通过(t *testing.T) {
	longest := "a" + strings.Repeat("b", 31)
	hosts := []Host{
		{SN: "SN-1"},
		{SN: "SN-2", Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{}},
		{SN: "SN-3", Arch: "aarch64", Attrs: map[string]string{
			"ip":       "10.0.0.3/24",
			"hostname": "node01",
			"rack_u":   "机房 A <R1-U12> \"#$(id)\"",
			"nic2_mac": "",
			"note":     "第一行\r\n第二行\t带制表符",
			longest:    "最长的键",
		}},
	}
	for _, h := range hosts {
		if got := h.Validate(); len(got) != 0 {
			t.Errorf("主机 %s 的属性键都合法，不应报问题：%v", h.SN, got)
		}
	}
}

func TestHostValidate拒收非法与保留的属性键(t *testing.T) {
	bad := []string{
		"", "IP", "Rack", "1st_nic", "_ip", "rack-u", "rack.u", "rack u", "机架", "ip\n",
		"a" + strings.Repeat("b", 32),
		// 保留键：CSV 导入时这三列填的是主机自己的字段。
		"sn", "arch", "firmware",
	}
	for _, key := range bad {
		h := Host{SN: "SN-1", Attrs: map[string]string{key: "v", "hostname": "node01"}}
		want := []Problem{NewProblem("attr_key_invalid", Subject{Kind: "host", ID: "SN-1", Path: key}, key)}
		if got := h.Validate(); !slices.Equal(got, want) {
			t.Errorf("属性键 %q 应恰好报一条 attr_key_invalid，得到 %v", key, got)
		}
	}
}

// map 的遍历顺序是随机的：同一台主机每次校验都要按同样的顺序报出，界面标红的次序才不会跳。
func TestHostValidate按属性键的字典序报出(t *testing.T) {
	h := Host{SN: "SN-1", Attrs: map[string]string{
		"sn": "", "Zone": "", "arch": "", "ok_key": "", "9u": "", "firmware": "",
	}}
	want := []string{"9u", "Zone", "arch", "firmware", "sn"}
	for range 20 {
		var got []string
		for _, p := range h.Validate() {
			got = append(got, p.Subject.Path)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("报出的顺序是 %q，应为 %q", got, want)
		}
	}
}

// onHostField 指向主机 SN-1 的一个字段：arch、firmware，或某个属性键。
func onHostField(path string) Subject {
	return Subject{Kind: "host", ID: "SN-1", Path: path}
}

// 登记的架构与固件只认目录里的原样写法，区分大小写（T4-1）。写错的要在登记时就拒收：放进库里，
// 建任务时每个 ISO 都会报 arch_mismatch 或 firmware_mismatch，看着像是 ISO 不对。
func TestHostValidate拒收不合法的架构与固件(t *testing.T) {
	archBad := func(v string) Problem {
		return NewProblem("host_platform_invalid", onHostField("arch"), fieldLabels["arch"], v)
	}
	fwBad := func(v string) Problem {
		return NewProblem("host_platform_invalid", onHostField("firmware"), fieldLabels["firmware"], v)
	}
	cases := []struct {
		name string
		arch catalog.Arch
		fw   catalog.Firmware
		want []Problem
	}{
		{"固件的大小写不对", "", "UEFI", []Problem{fwBad("UEFI")}},
		{"固件写成显示名", "", "BIOS", []Problem{fwBad("BIOS")}},
		// iPXE 上报的是自己的构建架构与平台，要经 catalog 换算，不能原样登记。
		{"架构是 iPXE 的上报值 i386", "i386", "", []Problem{archBad("i386")}},
		{"架构是 iPXE 的上报值 arm64", "arm64", "", []Problem{archBad("arm64")}},
		{"固件是 iPXE 的上报值 pcbios", "", "pcbios", []Problem{fwBad("pcbios")}},
		{"固件是 iPXE 的上报值 efi", "", "efi", []Problem{fwBad("efi")}},
		{"架构写成别名", "amd64", "uefi", []Problem{archBad("amd64")}},
		{"架构的大小写不对", "X86_64", "", []Problem{archBad("X86_64")}},
		{"两项都不对时先架构后固件", "i386", "pcbios", []Problem{archBad("i386"), fwBad("pcbios")}},
		// 没登记的不查（C28 登记了才校验）。
		{"都没登记", "", "", nil},
		{"只登记了架构", "aarch64", "", nil},
		{"只登记了固件", "", "bios", nil},
		{"都登记且合法", "x86_64", "uefi", nil},
	}
	for _, c := range cases {
		h := Host{SN: "SN-1", Arch: c.arch, Firmware: c.fw, Attrs: map[string]string{"hostname": "node01"}}
		if got := h.Validate(); !slices.Equal(got, c.want) {
			t.Errorf("%s：得到 %v\n应为 %v", c.name, got, c.want)
		}
	}
}

// 属性值要存得下（T13-10）：PostgreSQL 不收 NUL，encoding/json 会把非法的 UTF-8 字节换成 U+FFFD，
// 存进去再取出来就不是登记的值了。问题落在那个属性键上。
func TestHostValidate拒收存不下的属性值(t *testing.T) {
	for _, v := range []string{"a\x00b", "\x00", "bad\xffbyte", "截断的\xe6\x9c"} {
		h := Host{SN: "SN-1", Attrs: map[string]string{"note": v, "ip": "10.0.0.3/24"}}
		want := []Problem{NewProblem("text_not_storable", onHostField("note"))}
		if got := h.Validate(); !slices.Equal(got, want) {
			t.Errorf("属性值 %q 应恰好报一条 text_not_storable，得到 %v", v, got)
		}
	}
}

// 几处同时有问题时都报出来，次序固定：先架构、固件，再按属性键的字典序；同一个键先报键、再报值。
func TestHostValidate几处问题按固定次序报出(t *testing.T) {
	h := Host{SN: "SN-1", Arch: "amd64", Firmware: "EFI", Attrs: map[string]string{
		"zone": "\x00", "IP": "\xff", "ok": "机房 A", "arch": "",
	}}
	want := []Problem{
		NewProblem("host_platform_invalid", onHostField("arch"), fieldLabels["arch"], "amd64"),
		NewProblem("host_platform_invalid", onHostField("firmware"), fieldLabels["firmware"], "EFI"),
		NewProblem("attr_key_invalid", onHostField("IP"), "IP"),
		NewProblem("text_not_storable", onHostField("IP")),
		NewProblem("attr_key_invalid", onHostField("arch"), "arch"),
		NewProblem("text_not_storable", onHostField("zone")),
	}
	for range 20 {
		if got := h.Validate(); !slices.Equal(got, want) {
			t.Fatalf("得到 %v\n应为 %v", got, want)
		}
	}
}

// 状态的字面量会存进数据库，发布后不能改名。
func TestISOState的字面量(t *testing.T) {
	got := []ISOState{ISOAcquiring, ISOImporting, ISOReady, ISOFailed, ISODeprecated}
	want := []ISOState{"acquiring", "importing", "ready", "failed", "deprecated"}
	if !slices.Equal(got, want) {
		t.Errorf("ISO 状态的字面量是 %q，应为 %q", got, want)
	}
}

// 状态的显示名给说明与界面用（T5-1）：每个状态都有，是合格的显示名；不认识的为空串，由调用方回落到标识。
func TestISOState的显示名(t *testing.T) {
	want := map[ISOState]string{
		ISOAcquiring: "获取中", ISOImporting: "导入中", ISOReady: "可用", ISOFailed: "导入失败", ISODeprecated: "已废弃",
	}
	for s, label := range want {
		if got := s.Label(); got != label || !ValidLabel(got) {
			t.Errorf("%q.Label() = %q，应为 %q", s, got, label)
		}
	}
	for _, s := range []ISOState{"", "Ready", "READY", "ready ", "imported", "failure"} {
		if got := s.Label(); got != "" {
			t.Errorf("不认识的状态 %q 的显示名应为空，得到 %q", s, got)
		}
	}
}

// 状态会进数据库：第 2 层建 CHECK 约束、第 5 层保存时拒收，都只调 Valid，不另抄取值表（T7-2）。
func TestISOStateValid只认五个状态(t *testing.T) {
	for _, s := range []ISOState{ISOAcquiring, ISOImporting, ISOReady, ISOFailed, ISODeprecated} {
		if !s.Valid() {
			t.Errorf("%q.Valid() 应为 true", s)
		}
	}
	for _, s := range []ISOState{"", "Ready", "READY", "ready ", "imported", "failure"} {
		if s.Valid() {
			t.Errorf("%q.Valid() 应为 false：只认原样的写法", s)
		}
	}
}

// 说明里对状态的称呼（T5-1、C-7）：认识的用中文显示名；不认识的没有显示名，回落到标识；空串连标识都
// 没有，回落到 codes.go 登记的「未知状态」。ISO 的状态与版本的状态共用这一条，iso_not_ready 与
// template_not_approved 的说明里因此都不会出现「现为「」」。
func TestStateLabel(t *testing.T) {
	for s, want := range map[ISOState]string{ISODeprecated: "已废弃", "Ready": "Ready", "": "未知状态"} {
		if got := StateLabel(s); got != want {
			t.Errorf("StateLabel(ISOState(%q)) = %q，应为 %q", s, got, want)
		}
	}
	for s, want := range map[VersionState]string{VersionReview: "待审批", "Approved": "Approved", "": "未知状态"} {
		if got := StateLabel(s); got != want {
			t.Errorf("StateLabel(VersionState(%q)) = %q，应为 %q", s, got, want)
		}
	}
	for _, c := range []Code{"iso_not_ready", "template_not_approved"} {
		if p := NewProblem(c, Subject{}, StateLabel(VersionState(""))); strings.Contains(p.Message, "「」") {
			t.Errorf("%s 的说明里出现了空的称呼：%s", c, p.Message)
		}
	}
}

// ValidAttrKey 就是 Host.Validate 查属性键的规则，导出给激活判定查 attr 来源（T4-1）：保留键表
// 只在本包一处。只有保留键本身被拒，长得像的照收。
func TestValidAttrKey(t *testing.T) {
	for _, key := range []string{"ip", "hostname", "rack_u", "nic2_mac", "a" + strings.Repeat("b", 31), "sn2", "arch_note", "firmwares"} {
		if !ValidAttrKey(key) {
			t.Errorf("ValidAttrKey(%q) 应为 true", key)
		}
	}
	for _, key := range []string{"", "IP", "1st_nic", "_ip", "rack-u", "rack u", "机架", "a" + strings.Repeat("b", 32), "sn", "arch", "firmware", "SN"} {
		if ValidAttrKey(key) {
			t.Errorf("ValidAttrKey(%q) 应为 false", key)
		}
	}
}

// completeISO 是一份记录齐全、导入完成的 ISO。选 Windows 是因为它带提供的集合 editions。
func completeISO() ISO {
	return ISO{
		ID: "iso-ws2025", Name: "Windows Server 2025", Family: "windows", Series: "windows-server",
		Version: "2025", Arch: "x86_64", State: ISOReady, SHA256: strings.Repeat("ab", 32),
		Artifacts: map[string]string{"bootwim": "windows-server/2025/x86_64/sources/boot.wim"},
		Offers: map[string][]Option{
			"editions": {{Value: "ServerStandard", Label: "Standard"}, {Value: "ServerDatacenter", Label: "数据中心版"}},
		},
	}
}

// isoIncomplete 是 completeISO 的 path 那一项不合法时的问题，称呼按 path 的第一段查 fieldLabels：
// 提供的集合以 offers/<集合名> 定位，称呼都是 offers 的那一条。
func isoIncomplete(path string) Problem {
	field, _, _ := strings.Cut(path, "/")
	return NewProblem("iso_incomplete", Subject{Kind: "iso", ID: "iso-ws2025", Path: path}, fieldLabels[field])
}

// Validate 只看记录本身、不看状态：第 4 层在置 ready 之前调它，那时状态还是导入中。名称只要求存得下。
func TestISOValidate记录齐全时通过(t *testing.T) {
	rocky := ISO{ID: "iso-rocky96", Name: "Rocky Linux 9.6", Family: "linux", Series: "rocky",
		Version: "9.6", Arch: "x86_64", State: ISOImporting}
	esxi := ISO{ID: "iso-esxi8", Name: "ESXi 8.0U3", Family: "esxi", Series: "esxi",
		Version: "8.0U3", Arch: "x86_64", State: ISOReady}
	arm := ISO{ID: "iso-ubuntu2404-arm", Name: "Ubuntu 24.04 arm64", Family: "linux", Series: "ubuntu",
		Version: "24.04", Arch: "aarch64", State: ISODeprecated}
	unnamed := completeISO()
	unnamed.Name = ""
	for _, iso := range []ISO{completeISO(), rocky, esxi, arm, unnamed} {
		if got := iso.Validate(); len(got) != 0 {
			t.Errorf("%s 的记录齐全，不应报问题：%v", iso.ID, got)
		}
	}
}

// 每个反例只改坏一处，恰好报这一处，问题落在那个字段上（L-6）。
func TestISOValidate逐个字段报出不合法(t *testing.T) {
	onName := NewProblem("text_not_storable", Subject{Kind: "iso", ID: "iso-ws2025", Path: "name"})
	cases := []struct {
		name string
		edit func(*ISO)
		want Problem
	}{
		// 族不对时不再拿系去比族：错的是族，系本身没错，不连带报它。
		{"族为空", func(i *ISO) { i.Family = "" }, isoIncomplete("family")},
		{"族的大小写不对", func(i *ISO) { i.Family = "Windows" }, isoIncomplete("family")},
		{"系为空", func(i *ISO) { i.Series = "" }, isoIncomplete("series")},
		{"系不认识", func(i *ISO) { i.Series = "windows-2025" }, isoIncomplete("series")},
		{"系不属于该族", func(i *ISO) { i.Series = "rocky" }, isoIncomplete("series")},
		{"架构为空", func(i *ISO) { i.Arch = "" }, isoIncomplete("arch")},
		{"架构写成别名", func(i *ISO) { i.Arch = "amd64" }, isoIncomplete("arch")},
		{"架构是 iPXE 的上报值", func(i *ISO) { i.Arch = "i386" }, isoIncomplete("arch")},
		{"版本为空", func(i *ISO) { i.Version = "" }, isoIncomplete("version")},
		{"名称含 NUL", func(i *ISO) { i.Name = "Windows\x00Server" }, onName},
		{"名称不是合法的 UTF-8", func(i *ISO) { i.Name = "Windows \xff" }, onName},
	}
	for _, c := range cases {
		iso := completeISO()
		c.edit(&iso)
		if got := iso.Validate(); !slices.Equal(got, []Problem{c.want}) {
			t.Errorf("%s：得到 %v\n应为 %v", c.name, got, c.want)
		}
	}
}

// 版本会成为制品目录里的一段路径（rocky/9.6/x86_64/…，L-6），首字符必须是字母或数字：. 与 .. 会让
// 不同的 ISO 落进同一个目录，或者穿出制品目录；以 - 开头的放进命令行会被当成选项。
func TestISOValidate版本的规则(t *testing.T) {
	good := []string{"9.6", "2025", "8.0U3", "24.04", "10.0.26100", "15-SP6", "16.0", "1", strings.Repeat("9", 32)}
	for _, v := range good {
		iso := completeISO()
		iso.Version = v
		if got := iso.Validate(); len(got) != 0 {
			t.Errorf("版本 %q 应合法，得到 %v", v, got)
		}
	}
	bad := []string{"", ".", "..", "-9", "_9", ".9", "9 6", "9/6", "9\\6", "9.6\n", "９.６", "9.6版", strings.Repeat("9", 33)}
	for _, v := range bad {
		iso := completeISO()
		iso.Version = v
		if got := iso.Validate(); !slices.Equal(got, []Problem{isoIncomplete("version")}) {
			t.Errorf("版本 %q 应恰好报一条 iso_incomplete，得到 %v", v, got)
		}
	}
}

// ISO 提供的集合（T7-5）：集合名被参数声明的 Offer 引用，要是标识符；选项值原样进正文，要是安全词，
// 同一个集合里重复了，操作员分不清选的是哪个；选项的显示名给人看，过 ValidLabel。
func TestISOValidate提供的集合不合法(t *testing.T) {
	ok := []Option{{Value: "ServerStandard", Label: "Standard"}}
	cases := []struct {
		name   string
		offers map[string][]Option
		want   []Problem
	}{
		{"集合名有大写", map[string][]Option{"Editions": ok}, []Problem{isoIncomplete("offers/Editions")}},
		{"集合名为空", map[string][]Option{"": ok}, []Problem{isoIncomplete("offers/")}},
		{"集合名带连字符", map[string][]Option{"edition-list": ok}, []Problem{isoIncomplete("offers/edition-list")}},
		{"选项值带空格", map[string][]Option{"editions": {{Value: "Windows Server 2025 Datacenter", Label: "Datacenter"}}},
			[]Problem{isoIncomplete("offers/editions")}},
		{"选项值为空", map[string][]Option{"editions": {{Value: "", Label: "Standard"}}},
			[]Problem{isoIncomplete("offers/editions")}},
		{"选项值带引号", map[string][]Option{"editions": {{Value: `"ServerStandard"`, Label: "Standard"}}},
			[]Problem{isoIncomplete("offers/editions")}},
		{"同一个集合里选项值重复", map[string][]Option{"editions": {
			{Value: "ServerStandard", Label: "Standard"}, {Value: "ServerStandard", Label: "标准版"},
		}}, []Problem{isoIncomplete("offers/editions")}},
		{"选项显示名为空", map[string][]Option{"editions": {{Value: "ServerStandard", Label: ""}}},
			[]Problem{isoIncomplete("offers/editions")}},
		{"选项显示名含零宽空格", map[string][]Option{"editions": {{Value: "ServerStandard", Label: "Stan" + string(rune(0x200b)) + "dard"}}},
			[]Problem{isoIncomplete("offers/editions")}},
		{"选项显示名首尾有空格", map[string][]Option{"editions": {{Value: "ServerStandard", Label: " Standard"}}},
			[]Problem{isoIncomplete("offers/editions")}},
		{"选项显示名超过 64 个字", map[string][]Option{"editions": {{Value: "ServerStandard", Label: strings.Repeat("版", 65)}}},
			[]Problem{isoIncomplete("offers/editions")}},
		// 一个集合里有几处毛病也只报一条：问题落在同一个集合上，说明是同一句，多报只是重复。
		{"一个集合里几处都不对", map[string][]Option{"editions": {
			{Value: "", Label: ""}, {Value: "a b", Label: "x"}, {Value: "a b", Label: "x"},
		}}, []Problem{isoIncomplete("offers/editions")}},
		// 几个集合各报各的，按集合名的字典序；合法的集合不报。
		{"两个集合不对", map[string][]Option{
			"zones": {{Value: "", Label: "x"}}, "Editions": ok, "editions": ok,
		}, []Problem{isoIncomplete("offers/Editions"), isoIncomplete("offers/zones")}},
	}
	for _, c := range cases {
		iso := completeISO()
		iso.Offers = c.offers
		for range 20 {
			if got := iso.Validate(); !slices.Equal(got, c.want) {
				t.Errorf("%s：得到 %v\n应为 %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestISOValidate提供的集合合法时通过(t *testing.T) {
	cases := map[string]map[string][]Option{
		"没有提供集合": nil,
		// 空集合等同于没提供：要它的参数由参数求值与准入判定报 iso_offer_missing，不算记录不合法。
		"空集合": {"editions": {}},
		"选项值在不同的集合里可以相同": {
			"editions": {{Value: "1", Label: "Standard"}},
			"images":   {{Value: "1", Label: "第 1 个镜像"}},
		},
		"显示名用中文和空格": {"editions": {{Value: "ServerDatacenter", Label: "数据中心版 （桌面体验）"}}},
	}
	for name, offers := range cases {
		iso := completeISO()
		iso.Offers = offers
		if got := iso.Validate(); len(got) != 0 {
			t.Errorf("%s：不应报问题，得到 %v", name, got)
		}
	}
}

// 几处都不对时都报出来，次序固定：族、系、架构、版本、名称，再按集合名的字典序。
func TestISOValidate几处问题按固定次序报出(t *testing.T) {
	iso := ISO{ID: "iso-ws2025", Name: "\xff", Family: "Linux", Series: "centos", Arch: "amd64", Version: "..",
		Offers: map[string][]Option{
			"zones": {{Value: "", Label: "x"}}, "Editions": {{Value: "ok", Label: "OK"}}, "editions": {{Value: "a", Label: "A"}},
		}}
	want := []Problem{
		isoIncomplete("family"), isoIncomplete("series"), isoIncomplete("arch"), isoIncomplete("version"),
		NewProblem("text_not_storable", Subject{Kind: "iso", ID: "iso-ws2025", Path: "name"}),
		isoIncomplete("offers/Editions"), isoIncomplete("offers/zones"),
	}
	for range 20 {
		if got := iso.Validate(); !slices.Equal(got, want) {
			t.Fatalf("得到 %v\n应为 %v", got, want)
		}
	}
}

// Unusable 给出 ISO 不能用来建任务的原因（C5、C26、T5-1）。状态的原因里写中文显示名；导入失败
// 另报 iso_import_failed，它的下一步是重新导入，而不是「等它导入完成」。
func TestISOUnusable按状态给出原因(t *testing.T) {
	on := Subject{Kind: "iso", ID: "iso-ws2025"}
	cases := []struct {
		state ISOState
		want  []Problem
	}{
		{ISOAcquiring, []Problem{NewProblem("iso_not_ready", on, "获取中")}},
		{ISOImporting, []Problem{NewProblem("iso_not_ready", on, "导入中")}},
		{ISOReady, nil},
		{ISOFailed, []Problem{NewProblem("iso_import_failed", on)}},
		{ISODeprecated, []Problem{NewProblem("iso_not_ready", on, "已废弃")}},
		// 不认识的状态没有显示名，回落到标识本身；只认原样的写法。空状态连标识都没有，回落到
		// 「未知状态」（C-7），说明里不会出现「现为「」」。
		{"Ready", []Problem{NewProblem("iso_not_ready", on, "Ready")}},
		{"", []Problem{NewProblem("iso_not_ready", on, "未知状态")}},
	}
	for _, c := range cases {
		iso := completeISO()
		iso.State = c.state
		if got := iso.Unusable(); !slices.Equal(got, c.want) {
			t.Errorf("状态为 %q：得到 %v\n应为 %v", c.state, got, c.want)
		}
	}
}

// 没导入完的 ISO 本来就还没有版本和架构：只报状态，不拿记录去校验。
func TestISOUnusable不是ready时不校验记录(t *testing.T) {
	on := Subject{Kind: "iso", ID: "iso-new"}
	cases := map[ISOState]Problem{
		ISOAcquiring:  NewProblem("iso_not_ready", on, "获取中"),
		ISOImporting:  NewProblem("iso_not_ready", on, "导入中"),
		ISOFailed:     NewProblem("iso_import_failed", on),
		ISODeprecated: NewProblem("iso_not_ready", on, "已废弃"),
	}
	for state, want := range cases {
		iso := ISO{ID: "iso-new", Name: "ESXi 9.0", Family: "esxi", Series: "esxi", State: state}
		if got := iso.Unusable(); !slices.Equal(got, []Problem{want}) {
			t.Errorf("状态为 %q、还没有版本和架构：得到 %v\n应为 %v", state, got, want)
		}
	}
}

// ready 却记录不全的 ISO 违反 C5，不能用；原因就是 Validate 给的问题（L-6），准入据此报出，不再
// 拿它逐台比平台。
func TestISOUnusable可用却记录不全时给出Validate的问题(t *testing.T) {
	noArch := completeISO()
	noArch.Arch = ""
	if got, want := noArch.Unusable(), []Problem{isoIncomplete("arch")}; !slices.Equal(got, want) {
		t.Errorf("ready 却缺架构：得到 %v\n应为 %v", got, want)
	}
	broken := completeISO()
	broken.Version = ".."
	broken.Offers = map[string][]Option{"editions": {{Value: "Windows Server", Label: "Standard"}}}
	if got, want := broken.Unusable(), []Problem{isoIncomplete("version"), isoIncomplete("offers/editions")}; !slices.Equal(got, want) {
		t.Errorf("ready 却版本与集合不合法：得到 %v\n应为 %v", got, want)
	}
}

// Usable 就是 Unusable 为空（L-6）：不会一个说能用、一个又给出原因。只有 ready 且记录齐全的能用。
func TestISOUsable与Unusable一致(t *testing.T) {
	for _, state := range []ISOState{ISOAcquiring, ISOImporting, ISOReady, ISOFailed, ISODeprecated, "", "Ready"} {
		for _, complete := range []bool{true, false} {
			iso := completeISO()
			iso.State = state
			if !complete {
				iso.Arch = ""
			}
			usable, reasons := iso.Usable(), iso.Unusable()
			if usable != (len(reasons) == 0) {
				t.Errorf("状态 %q、记录齐全 %v：Usable() = %v，Unusable() = %v，两者不一致", state, complete, usable, reasons)
			}
			if want := state == ISOReady && complete; usable != want {
				t.Errorf("状态 %q、记录齐全 %v：Usable() = %v，应为 %v", state, complete, usable, want)
			}
		}
	}
}
