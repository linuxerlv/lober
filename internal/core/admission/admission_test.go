package admission

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// 准入判定只看对象的身份与状态：正文、参数与实现在激活判定和参数求值里查。
// 下面的样例因此只填判定要看的字段。

// rockyISO 是一个导入完成的 Rocky Linux 9.6 x86_64 ISO。
func rockyISO() model.ISO {
	return model.ISO{
		ID: "iso-rocky96", Name: "Rocky Linux 9.6", Family: "linux", Series: "rocky",
		Version: "9.6", Arch: "x86_64", State: model.ISOReady,
	}
}

// esxiISO 是一个导入完成的 ESXi 8 ISO。ESXi 只走 UEFI（spec §2.1）。
func esxiISO() model.ISO {
	return model.ISO{
		ID: "iso-esxi8", Name: "ESXi 8.0U3", Family: "esxi", Series: "esxi",
		Version: "8.0U3", Arch: "x86_64", State: model.ISOReady,
	}
}

// ubuntuARMISO 是一个导入完成的 Ubuntu 24.04 aarch64 ISO。
func ubuntuARMISO() model.ISO {
	return model.ISO{
		ID: "iso-ubuntu2404-arm", Name: "Ubuntu 24.04 arm64", Family: "linux", Series: "ubuntu",
		Version: "24.04", Arch: "aarch64", State: model.ISOReady,
	}
}

// hostOf 造一台主机；架构与固件可以留空，表示没登记（C7）。
func hostOf(sn string, arch catalog.Arch, fw catalog.Firmware) model.Host {
	return model.Host{SN: sn, Arch: arch, Firmware: fw}
}

func hookOf(id string, version int, state model.VersionState) model.Hook {
	return model.Hook{ID: id, Version: version, Name: id, State: state, Author: "alice", Approver: "bob"}
}

func hookMap(hooks ...model.Hook) map[model.HookKey]model.Hook {
	m := make(map[model.HookKey]model.Hook, len(hooks))
	for _, h := range hooks {
		m[h.Key()] = h
	}
	return m
}

// baselineTemplate 是一个已审批的 kickstart 模板 v4，勾 rocky 与 almalinux：分区插槽放
// lvm@1，装后插槽放同一个 nic@2 两次（C17）。
func baselineTemplate() model.Template {
	return model.Template{
		ID: "baseline", Version: 4, Name: "Linux 基线", State: model.VersionApproved,
		Author: "alice", Approver: "bob",
		Family: "linux", Series: []catalog.Series{"rocky", "almalinux"},
		Assembly: map[string][]model.Instance{
			"分区":   {{HookID: "lvm", HookVersion: 1}},
			"装后配置": {{HookID: "nic", HookVersion: 2}, {HookID: "nic", HookVersion: 2}},
		},
	}
}

// baselineHooks 是 baselineTemplate 引用的 hook 版本，都已审批。
func baselineHooks() map[model.HookKey]model.Hook {
	return hookMap(hookOf("lvm", 1, model.VersionApproved), hookOf("nic", 2, model.VersionApproved))
}

// esxiTemplate 是一个已审批、只勾 esxi 的模板，装配表为空。
func esxiTemplate() model.Template {
	return model.Template{
		ID: "esxi-base", Version: 1, Name: "ESXi 基线", State: model.VersionApproved,
		Author: "alice", Approver: "bob", Family: "esxi", Series: []catalog.Series{"esxi"},
	}
}

// windowsISO 是一个导入完成的 Windows Server 2025 ISO，offers 是它提供的取值集合（C21）。
func windowsISO(offers map[string][]model.Option) model.ISO {
	return model.ISO{
		ID: "iso-ws2025", Name: "Windows Server 2025", Family: "windows", Series: "windows-server",
		Version: "10.0.26100", Arch: "x86_64", State: model.ISOReady, Offers: offers,
	}
}

// editionOffers 是 Windows Server 的 ISO 提供的 editions 集合。
func editionOffers() map[string][]model.Option {
	return map[string][]model.Option{"editions": {
		{Value: "ServerStandard", Label: "Standard"}, {Value: "ServerDatacenter", Label: "Datacenter"},
	}}
}

// windowsTemplate 是一个已审批、只勾 windows-server 的模板 v2，装配表为空：版本（edition）的选项
// 取自 ISO 提供的 editions，界面语言是静态选项。
func windowsTemplate() model.Template {
	return model.Template{
		ID: "win-base", Version: 2, Name: "Windows-Base", State: model.VersionApproved,
		Author: "alice", Approver: "bob", Family: "windows", Series: []catalog.Series{"windows-server"},
		Params: []model.ParamDecl{
			{Name: "lang", Label: "界面语言", Type: "enum", Default: "zh-CN",
				Options: []model.Option{{Value: "zh-CN", Label: "简体中文"}, {Value: "en-US", Label: "English"}}},
			{Name: "edition", Label: "版本", Type: "enum", Offer: "editions"},
		},
	}
}

func onHost(sn string) model.Subject {
	return model.Subject{Kind: "host", ID: sn}
}

func onISO(id string) model.Subject {
	return model.Subject{Kind: "iso", ID: id}
}

// onTemplate 的参数是「模板 ID@版本」。
func onTemplate(id string) model.Subject {
	return model.Subject{Kind: "template", ID: id}
}

// sameProblems 逐条比对代码、对象、说明与下一步，次序也要一致：同样的输入永远得到同样
// 次序的输出（全局约定），向导里列出的原因才不会跳来跳去。
func sameProblems(t *testing.T, what string, got, want []model.Problem) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s：\n得到 %v\n应为 %v", what, got, want)
	}
}

// sameByHost 比对 ByHost：键要恰好是这批主机的 SN，每台的问题逐条一致。
func sameByHost(t *testing.T, got, want map[string][]model.Problem) {
	t.Helper()
	if g, w := slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)); !slices.Equal(g, w) {
		t.Errorf("ByHost 的键是 %q，应为 %q", g, w)
	}
	for _, sn := range slices.Sorted(maps.Keys(want)) {
		sameProblems(t, "ByHost["+sn+"]", got[sn], want[sn])
	}
}

// 已有未结束任务的主机照常列出，只是不能选，并写明原因（C31，spec §5.3 第 1 步）。
func TestHostSelectable已有未结束任务的主机不能选(t *testing.T) {
	h := hostOf("SN-1", "x86_64", "uefi")
	sameProblems(t, "已有未结束的任务", HostSelectable(h, true),
		[]model.Problem{model.NewProblem("host_has_open_task", onHost("SN-1"))})
	sameProblems(t, "没有未结束的任务", HostSelectable(h, false), nil)
}

// 只有导入完成的 ISO 能建任务（C5），已废弃的不能再被新引用（C26）。获取中、导入中、已废弃共用
// iso_not_ready，参数是状态的中文显示名（T5-1），说明里分得清是哪一种；导入失败的另报
// iso_import_failed：它不用等，可以直接重新导入（spec §5.1 第 5 步）。
func TestISOUsable不可用的ISO按状态报原因(t *testing.T) {
	hosts := []model.Host{hostOf("SN-1", "x86_64", "uefi")}
	on := onISO("iso-rocky96")
	cases := []struct {
		state model.ISOState
		want  model.Problem
	}{
		{model.ISOAcquiring, model.NewProblem("iso_not_ready", on, "获取中")},
		{model.ISOImporting, model.NewProblem("iso_not_ready", on, "导入中")},
		{model.ISOFailed, model.NewProblem("iso_import_failed", on)},
		{model.ISODeprecated, model.NewProblem("iso_not_ready", on, "已废弃")},
	}
	byMessage := map[string]model.ISOState{}
	for _, c := range cases {
		iso := rockyISO()
		iso.State = c.state
		got := ISOUsable(iso, hosts)
		sameProblems(t, "状态为 "+string(c.state), got, []model.Problem{c.want})
		if len(got) != 1 {
			continue
		}
		// 操作员看到的是中文的现状，不是 deprecated 这类标识。
		if strings.Contains(got[0].Message, string(c.state)) {
			t.Errorf("状态 %s 的说明里出现了标识：%s", c.state, got[0].Message)
		}
		if prev, dup := byMessage[got[0].Message]; dup {
			t.Errorf("状态 %s 与 %s 的说明一样，分不出是哪一种：%s", c.state, prev, got[0].Message)
		}
		byMessage[got[0].Message] = c.state
	}
	sameProblems(t, "可用的 ISO", ISOUsable(rockyISO(), hosts), nil)
}

// ISO 不可用时不再逐台比平台：获取中的 ISO 还没识别出架构，拿它去比，会给登记过的主机
// 报出不成立的冲突（比如说 esxi 不支持 uefi），而这个 ISO 本来就用不了。
func TestISOUsable不可用时不逐台比平台(t *testing.T) {
	iso := model.ISO{ID: "iso-new", Name: "ESXi 9.0", Family: "esxi", Series: "esxi", State: model.ISOAcquiring}
	hosts := []model.Host{hostOf("SN-1", "x86_64", "uefi"), hostOf("SN-2", "aarch64", "bios")}
	sameProblems(t, "获取中、还没有架构的 ISO", ISOUsable(iso, hosts),
		[]model.Problem{model.NewProblem("iso_not_ready", onISO("iso-new"), "获取中")})
}

// ready 却缺架构的 ISO 违反 C5，报 iso_incomplete，同样不逐台比平台（L-6）：拿它去比，没登记平台
// 的主机会通过，只登记了架构的主机却会收到「ISO 的架构是 ，…」和参数为空的 firmware_mismatch。
func TestISOUsable可用却缺架构时报iso_incomplete(t *testing.T) {
	iso := rockyISO()
	iso.Arch = ""
	want := []model.Problem{
		model.NewProblem("iso_incomplete", model.Subject{Kind: "iso", ID: "iso-rocky96", Path: "arch"}, "架构"),
	}
	for _, h := range []model.Host{
		hostOf("SN-BARE", "", ""),
		hostOf("SN-ARCH", "x86_64", ""),
		hostOf("SN-FW", "", "uefi"),
		hostOf("SN-FULL", "x86_64", "bios"),
	} {
		sameProblems(t, "配主机 "+h.SN, ISOUsable(iso, []model.Host{h}), want)
	}
	sameProblems(t, "没有主机", ISOUsable(iso, nil), want)

	res := Batch(BatchInput{
		Hosts:    []model.Host{hostOf("SN-BARE", "", ""), hostOf("SN-ARCH", "x86_64", "")},
		ISO:      iso,
		Template: baselineTemplate(),
		Hooks:    baselineHooks(),
	})
	sameProblems(t, "Batch 的 Common", res.Common, want)
	sameByHost(t, res.ByHost, map[string][]model.Problem{"SN-BARE": {}, "SN-ARCH": {}})
}

// 架构与固件分开报，问题指向冲突的那台主机：向导据此灰显 ISO 并写明是哪几台（spec §5.3 第 2 步）。
func TestISOUsable平台冲突指向那台主机(t *testing.T) {
	cases := []struct {
		name string
		iso  model.ISO
		host model.Host
		want []model.Problem
	}{
		{"架构不符", rockyISO(), hostOf("SN-ARM", "aarch64", "uefi"), []model.Problem{
			model.NewProblem("arch_mismatch", onHost("SN-ARM"), "x86_64", "aarch64"),
		}},
		{"只登记了架构且不符", ubuntuARMISO(), hostOf("SN-X86", "x86_64", ""), []model.Problem{
			model.NewProblem("arch_mismatch", onHost("SN-X86"), "aarch64", "x86_64"),
		}},
		{"esxi 配 bios 的主机", esxiISO(), hostOf("SN-BIOS", "x86_64", "bios"), []model.Problem{
			model.NewProblem("firmware_mismatch", onHost("SN-BIOS"), "esxi", "bios"),
		}},
		{"只登记了固件且不符", esxiISO(), hostOf("SN-BIOS", "", "bios"), []model.Problem{
			model.NewProblem("firmware_mismatch", onHost("SN-BIOS"), "esxi", "bios"),
		}},
		// 两个原因都报，操作员才知道换 ISO 时两项都得满足。
		{"架构与固件都不符", esxiISO(), hostOf("SN-OLD", "aarch64", "bios"), []model.Problem{
			model.NewProblem("arch_mismatch", onHost("SN-OLD"), "x86_64", "aarch64"),
			model.NewProblem("firmware_mismatch", onHost("SN-OLD"), "esxi", "bios"),
		}},
		// 登记了却不合法的值比不上任何 ISO（catalog.PlatformOK），报出来让人去核对登记。
		{"登记的固件写成了 iPXE 的上报值", rockyISO(), hostOf("SN-PXE", "", "pcbios"), []model.Problem{
			model.NewProblem("firmware_mismatch", onHost("SN-PXE"), "rocky", "pcbios"),
		}},
	}
	for _, c := range cases {
		sameProblems(t, c.name, ISOUsable(c.iso, []model.Host{c.host}), c.want)
	}
}

// C28：建任务时只校验主机登记了的项。限制了固件的 esxi、aarch64 的 ISO 也一样。
func TestISOUsable主机没登记架构和固件时通过(t *testing.T) {
	bare := []model.Host{hostOf("SN-1", "", ""), hostOf("SN-2", "", "")}
	for _, iso := range []model.ISO{rockyISO(), esxiISO(), ubuntuARMISO()} {
		sameProblems(t, iso.Name+" 配没登记架构和固件的主机", ISOUsable(iso, bare), nil)
	}
	sameProblems(t, "登记齐全且相符", ISOUsable(esxiISO(), []model.Host{hostOf("SN-3", "x86_64", "uefi")}), nil)
	sameProblems(t, "没有主机", ISOUsable(rockyISO(), nil), nil)
}

// esxi 的 ISO 配一批主机，只有一台登记了 bios：只报那一台，其余的照常通过，ISO 本身也
// 没有问题。向导灰显这个 ISO 时写明的就是那一台。
func TestEsxi的ISO只报批里那台bios主机(t *testing.T) {
	hosts := []model.Host{
		hostOf("SN-UEFI", "x86_64", "uefi"),
		hostOf("SN-BIOS", "x86_64", "bios"),
		hostOf("SN-BARE", "", ""),
		hostOf("SN-ARCH", "x86_64", ""),
	}
	conflict := []model.Problem{model.NewProblem("firmware_mismatch", onHost("SN-BIOS"), "esxi", "bios")}
	sameProblems(t, "ISOUsable", ISOUsable(esxiISO(), hosts), conflict)

	res := Batch(BatchInput{Hosts: hosts, ISO: esxiISO(), Template: esxiTemplate()})
	sameProblems(t, "Batch 的 Common", res.Common, nil)
	sameByHost(t, res.ByHost, map[string][]model.Problem{
		"SN-UEFI": {}, "SN-BIOS": conflict, "SN-BARE": {}, "SN-ARCH": {},
	})
}

// 冲突按主机传入的顺序报出，同一台的架构在前、固件在后：同一批主机每次核对，向导列出的
// 「哪几台」次序都一样。
func TestISOUsable按主机的顺序报冲突(t *testing.T) {
	hosts := []model.Host{
		hostOf("SN-C", "", "bios"),
		hostOf("SN-OK", "x86_64", "uefi"),
		hostOf("SN-A", "aarch64", ""),
		hostOf("SN-B", "aarch64", "bios"),
	}
	sameProblems(t, "三台冲突", ISOUsable(esxiISO(), hosts), []model.Problem{
		model.NewProblem("firmware_mismatch", onHost("SN-C"), "esxi", "bios"),
		model.NewProblem("arch_mismatch", onHost("SN-A"), "x86_64", "aarch64"),
		model.NewProblem("arch_mismatch", onHost("SN-B"), "x86_64", "aarch64"),
		model.NewProblem("firmware_mismatch", onHost("SN-B"), "esxi", "bios"),
	})
}

// t 应当是 CurrentApproved 取出的版本；调用方传错了，草稿、待审批、已废弃的版本也不能
// 建任务（C22、C23）。说明里的现状是中文显示名（T5-1）；不认识的状态没有显示名，回落到标识。
func TestTemplateUsable不是已审批的版本不能用(t *testing.T) {
	cases := []struct {
		state model.VersionState
		label string
	}{
		{model.VersionDraft, "草稿"},
		{model.VersionReview, "待审批"},
		{model.VersionDeprecated, "已废弃"},
		{"Approved", "Approved"}, // 只认原样的写法，大小写不同就不认识
	}
	for _, c := range cases {
		tpl := baselineTemplate()
		tpl.State = c.state
		sameProblems(t, "状态为 "+string(c.state), TemplateUsable(tpl, baselineHooks(), rockyISO()),
			[]model.Problem{model.NewProblem("template_not_approved", onTemplate("baseline@4"), c.label)})
	}
	sameProblems(t, "已审批", TemplateUsable(baselineTemplate(), baselineHooks(), rockyISO()), nil)
}

// 废弃的 hook 版本不能再被新引用，经模板间接引用也算（C26）。说明里点出是哪个 hook 的
// 哪个版本，作者才知道该升级哪一处引用。
func TestTemplateUsable引用了已废弃的hook版本时阻塞(t *testing.T) {
	hooks := hookMap(hookOf("lvm", 1, model.VersionApproved), hookOf("nic", 2, model.VersionDeprecated))
	got := TemplateUsable(baselineTemplate(), hooks, rockyISO())
	// nic@2 在装后插槽里放了两次（C17），是同一个引用，只报一次。
	sameProblems(t, "引用了已废弃的 nic@2", got,
		[]model.Problem{model.NewProblem("template_blocked_by_deprecated", onTemplate("baseline@4"), "nic", 2)})
	if len(got) == 1 && !strings.Contains(got[0].Message, "nic@2") {
		t.Errorf("说明没有点出 nic@2：%s", got[0].Message)
	}
}

// v5 引用的 hook 版本废弃后，v5 就被挡住，直到作者升级引用、审批出新版本。不悄悄改用 v4：
// 操作员选的是这个模板的当前版本，装出来却是旧内容，谁也不会知道。
func TestTemplateUsable当前版本被阻塞时不回落到旧版本(t *testing.T) {
	v4 := baselineTemplate()
	v4.Assembly["装后配置"] = []model.Instance{{HookID: "nic", HookVersion: 1}}
	v5 := baselineTemplate()
	v5.Version = 5
	hooks := hookMap(
		hookOf("lvm", 1, model.VersionApproved),
		hookOf("nic", 1, model.VersionApproved),
		hookOf("nic", 2, model.VersionDeprecated),
	)
	current, ok := model.CurrentApproved([]model.Template{v4, v5})
	if !ok || current.Version != 5 {
		t.Fatalf("当前的已审批版本应为 v5，得到 v%d, %v", current.Version, ok)
	}
	blocked := []model.Problem{model.NewProblem("template_blocked_by_deprecated", onTemplate("baseline@5"), "nic", 2)}
	sameProblems(t, "v5", TemplateUsable(current, hooks, rockyISO()), blocked)
	// 对照：v4 自己能用，正因为这样才不能悄悄回落到它。
	sameProblems(t, "v4", TemplateUsable(v4, hooks, rockyISO()), nil)

	res := Batch(BatchInput{Hosts: []model.Host{hostOf("SN-1", "x86_64", "uefi")}, ISO: rockyISO(), Template: current, Hooks: hooks})
	sameProblems(t, "建任务时的 Common", res.Common, blocked)
}

// 装配表是 map，遍历顺序随机：废弃的引用每个 hook 版本报一次，先按 hook ID、再按版本的数值排序，
// 同一个模板每次判定的原因次序都一样。后两组专门分开排序键与比较方式（T11-10②）：第一组里先比 ID
// 与先比版本排出来一样，版本又都是一位数，排错了也看不出来。
func TestTemplateUsable多个废弃引用按hook排序各报一次(t *testing.T) {
	cases := []struct {
		name  string
		add   map[string][]model.Instance // 在 baselineTemplate 的装配表上追加的插槽
		hooks map[model.HookKey]model.Hook
		want  []model.HookKey
	}{
		{"几个 hook 各有废弃的版本", map[string][]model.Instance{
			"软件包":  {{HookID: "pkgs", HookVersion: 3}, {HookID: "nic", HookVersion: 1}},
			"首次开机": {{HookID: "lvm", HookVersion: 1}},
		}, hookMap(
			hookOf("lvm", 1, model.VersionDeprecated),
			hookOf("nic", 1, model.VersionDeprecated),
			hookOf("nic", 2, model.VersionDeprecated),
			hookOf("pkgs", 3, model.VersionApproved),
		), []model.HookKey{{ID: "lvm", Version: 1}, {ID: "nic", Version: 1}, {ID: "nic", Version: 2}}},
		// 先比版本的话，pkgs@1 会排到 nic@2 前面。
		{"先按 ID 排", map[string][]model.Instance{"软件包": {{HookID: "pkgs", HookVersion: 1}}}, hookMap(
			hookOf("lvm", 1, model.VersionApproved),
			hookOf("nic", 2, model.VersionDeprecated),
			hookOf("pkgs", 1, model.VersionDeprecated),
		), []model.HookKey{{ID: "nic", Version: 2}, {ID: "pkgs", Version: 1}}},
		// 按字符串比的话，nic@10 会排到 nic@2 前面。
		{"版本按数值比", map[string][]model.Instance{"软件包": {{HookID: "nic", HookVersion: 10}}}, hookMap(
			hookOf("lvm", 1, model.VersionApproved),
			hookOf("nic", 2, model.VersionDeprecated),
			hookOf("nic", 10, model.VersionDeprecated),
		), []model.HookKey{{ID: "nic", Version: 2}, {ID: "nic", Version: 10}}},
	}
	subject := onTemplate("baseline@4")
	for _, c := range cases {
		tpl := baselineTemplate()
		maps.Copy(tpl.Assembly, c.add)
		var want []model.Problem
		for _, k := range c.want {
			want = append(want, model.NewProblem("template_blocked_by_deprecated", subject, k.ID, k.Version))
		}
		for range 20 {
			if got := TemplateUsable(tpl, c.hooks, rockyISO()); !slices.Equal(got, want) {
				t.Errorf("%s：得到 %v\n应为 %v", c.name, got, want)
				break
			}
		}
	}
}

// 这里不重做激活判定：引用的 hook 版本存在，批准时已经查过，批准后也不许删除（C26）。
// hooks 里缺了某个引用，是调用方漏给的，不在这里报；参数求值会报 hook_ref_missing。
func TestTemplateUsable不重做激活判定(t *testing.T) {
	cases := map[string]map[model.HookKey]model.Hook{
		"hooks 为空": nil,
		"缺了 nic@2": hookMap(hookOf("lvm", 1, model.VersionApproved)),
	}
	for name, hooks := range cases {
		sameProblems(t, name, TemplateUsable(baselineTemplate(), hooks, rockyISO()), nil)
	}
}

// ISO 的系必须是模板所勾的系之一（C27）。同格式而没勾的系也不行：激活判定只按所勾的系
// 核对过实现，没勾的系可能缺实现。
func TestTemplateUsableISO的系不在所勾的系里(t *testing.T) {
	for _, s := range []catalog.Series{"rhel", "opencloudos", "debian", "esxi"} {
		iso := rockyISO()
		iso.Family, iso.Series = s.Family(), s
		sameProblems(t, "ISO 的系为 "+string(s), TemplateUsable(baselineTemplate(), baselineHooks(), iso),
			[]model.Problem{model.NewProblem("series_not_ticked", onTemplate("baseline@4"), string(s))})
	}
	for _, s := range []catalog.Series{"rocky", "almalinux"} {
		iso := rockyISO()
		iso.Series = s
		sameProblems(t, "ISO 的系为 "+string(s), TemplateUsable(baselineTemplate(), baselineHooks(), iso), nil)
	}
}

// 模板参数要的选项集合 ISO 没提供，或给的是空集合：到向导第 4 步每台主机都会报 iso_offer_missing，
// 所以第 3 步就不列出这个模板（L-2）。条件与参数求值共用 model.ParamDecl.OfferMissing，提交时
// Batch 也在 Common 里报同一条。
func TestTemplateUsableISO没提供模板参数要的集合(t *testing.T) {
	want := []model.Problem{model.NewProblem("iso_offer_missing", onTemplate("win-base@2"), "版本", "editions")}
	for name, offers := range map[string]map[string][]model.Option{
		"没有提供任何集合":      nil,
		"只提供了别的集合":      {"images": editionOffers()["editions"]},
		"editions 是空集合": {"editions": {}},
	} {
		sameProblems(t, name, TemplateUsable(windowsTemplate(), nil, windowsISO(offers)), want)
	}
	sameProblems(t, "提供了 editions", TemplateUsable(windowsTemplate(), nil, windowsISO(editionOffers())), nil)

	res := Batch(BatchInput{Hosts: []model.Host{hostOf("SN-1", "x86_64", "uefi")}, ISO: windowsISO(nil), Template: windowsTemplate()})
	sameProblems(t, "建任务时的 Common", res.Common, want)
	sameByHost(t, res.ByHost, map[string][]model.Problem{"SN-1": {}})
}

// 装配表里实例所引 hook 的参数也算：hook 的参数由它的全部实现共用（C9），选了这个模板，向导里就有
// 这一格。hooks 里缺了的引用没有参数可看，不报，理由见 TestTemplateUsable不重做激活判定。
func TestTemplateUsableISO没提供hook参数要的集合(t *testing.T) {
	tpl := windowsTemplate()
	tpl.Params = nil
	tpl.Assembly = map[string][]model.Instance{"装后配置": {{HookID: "winimg", HookVersion: 1}}}
	h := hookOf("winimg", 1, model.VersionApproved)
	h.Params = []model.ParamDecl{{Name: "image", Label: "镜像", Type: "enum", Offer: "images"}}
	sameProblems(t, "hook 的参数要 images", TemplateUsable(tpl, hookMap(h), windowsISO(editionOffers())),
		[]model.Problem{model.NewProblem("iso_offer_missing", onTemplate("win-base@2"), "镜像", "images")})
	sameProblems(t, "hooks 里缺了这个引用", TemplateUsable(tpl, nil, windowsISO(editionOffers())), nil)
}

// ISO 不可用时不查它提供的集合，理由同 ISOUsable 不逐台比平台：导入中的 ISO 还没有提供的集合，拿它
// 去查会报出不成立的 iso_offer_missing，下一步还让人重新导入一个正在导入的 ISO。
func TestTemplateUsableISO不可用时不查集合(t *testing.T) {
	iso := windowsISO(nil)
	iso.State = model.ISOImporting
	sameProblems(t, "导入中的 ISO", TemplateUsable(windowsTemplate(), nil, iso), nil)
	res := Batch(BatchInput{Hosts: []model.Host{hostOf("SN-1", "x86_64", "uefi")}, ISO: iso, Template: windowsTemplate()})
	sameProblems(t, "建任务时的 Common", res.Common,
		[]model.Problem{model.NewProblem("iso_not_ready", onISO("iso-ws2025"), "导入中")})
}

// 缺的集合各报一条，次序与说明里称呼的参数都固定：先看模板参数，再按插槽名的字典序、实例在插槽里的
// 顺序看 hook 参数，同一个集合称呼第一个要它的参数。没写显示名的用参数名，与参数求值里那一格的称呼
// 相同。装配表是 map，多跑几次也得到同样的结果。
func TestTemplateUsable缺的集合各报一条且次序固定(t *testing.T) {
	tpl := windowsTemplate() // 模板参数 edition（版本）要 editions
	tpl.Assembly = map[string][]model.Instance{
		"post": {{HookID: "kbd", HookVersion: 1}},
		"disk": {{HookID: "img", HookVersion: 1}, {HookID: "kbd", HookVersion: 1}},
	}
	img := hookOf("img", 1, model.VersionApproved)
	img.Params = []model.ParamDecl{
		{Name: "edition", Label: "Edition", Type: "enum", Offer: "editions"}, // 模板参数已经要过 editions
		{Name: "image", Type: "enum", Offer: "images"},                       // 没写显示名
	}
	kbd := hookOf("kbd", 1, model.VersionApproved)
	kbd.Params = []model.ParamDecl{
		{Name: "image", Label: "镜像", Type: "enum", Offer: "images"}, // disk 里的 img 已经要过 images
		{Name: "layout", Label: "键盘布局", Type: "enum", Offer: "keyboards"},
	}
	subject := onTemplate("win-base@2")
	want := []model.Problem{
		model.NewProblem("iso_offer_missing", subject, "版本", "editions"),
		model.NewProblem("iso_offer_missing", subject, "image", "images"),
		model.NewProblem("iso_offer_missing", subject, "键盘布局", "keyboards"),
	}
	for range 20 {
		if got := TemplateUsable(tpl, hookMap(img, kbd), windowsISO(nil)); !slices.Equal(got, want) {
			t.Fatalf("得到 %v\n应为 %v", got, want)
		}
	}
}

// 几个原因同时存在时都报出来，次序固定：先是版本状态，再是废弃的引用，然后是系，最后是 ISO 没提供
// 的集合。
func TestTemplateUsable几个原因一起报(t *testing.T) {
	tpl := baselineTemplate()
	tpl.State = model.VersionDeprecated
	tpl.Params = []model.ParamDecl{{Name: "edition", Label: "版本", Type: "enum", Offer: "editions"}}
	hooks := hookMap(hookOf("lvm", 1, model.VersionApproved), hookOf("nic", 2, model.VersionDeprecated))
	subject := onTemplate("baseline@4")
	sameProblems(t, "四个原因", TemplateUsable(tpl, hooks, esxiISO()), []model.Problem{
		model.NewProblem("template_not_approved", subject, "已废弃"),
		model.NewProblem("template_blocked_by_deprecated", subject, "nic", 2),
		model.NewProblem("series_not_ticked", subject, "esxi"),
		model.NewProblem("iso_offer_missing", subject, "版本", "editions"),
	})
}

// 一批全部通过时 Common 为空，每台主机都有键且对应空切片。第 5 层把结果编码成 JSON
// 逐台返回，所以给的是空切片而不是 nil：nil 会编成 null。
func TestBatch全部通过(t *testing.T) {
	res := Batch(BatchInput{
		Hosts:    []model.Host{hostOf("SN-1", "x86_64", "uefi"), hostOf("SN-2", "x86_64", "bios"), hostOf("SN-3", "", "")},
		OpenTask: map[string]bool{"SN-1": false, "SN-OTHER": true},
		ISO:      rockyISO(),
		Template: baselineTemplate(),
		Hooks:    baselineHooks(),
	})
	if res.Common == nil || len(res.Common) != 0 {
		t.Errorf("Common 应为空切片，得到 %#v", res.Common)
	}
	if got, want := slices.Sorted(maps.Keys(res.ByHost)), []string{"SN-1", "SN-2", "SN-3"}; !slices.Equal(got, want) {
		t.Errorf("ByHost 的键是 %q，应为 %q", got, want)
	}
	for sn, problems := range res.ByHost {
		if problems == nil || len(problems) != 0 {
			t.Errorf("%s 应对应空切片，得到 %#v", sn, problems)
		}
	}
}

// ISO 与模板的问题整批只报一次，放 Common；主机的问题放 ByHost 里那台的键下，
// 一台有问题不连累别的主机。
func TestBatch按对象分开放问题(t *testing.T) {
	tpl := baselineTemplate()
	tpl.Series = []catalog.Series{"almalinux"}
	res := Batch(BatchInput{
		Hosts:    []model.Host{hostOf("SN-1", "x86_64", "uefi"), hostOf("SN-2", "aarch64", "uefi"), hostOf("SN-3", "", "")},
		OpenTask: map[string]bool{"SN-1": true},
		ISO:      rockyISO(),
		Template: tpl,
		Hooks:    hookMap(hookOf("lvm", 1, model.VersionApproved), hookOf("nic", 2, model.VersionDeprecated)),
	})
	sameProblems(t, "Common", res.Common, []model.Problem{
		model.NewProblem("template_blocked_by_deprecated", onTemplate("baseline@4"), "nic", 2),
		model.NewProblem("series_not_ticked", onTemplate("baseline@4"), "rocky"),
	})
	sameByHost(t, res.ByHost, map[string][]model.Problem{
		"SN-1": {model.NewProblem("host_has_open_task", onHost("SN-1"))},
		"SN-2": {model.NewProblem("arch_mismatch", onHost("SN-2"), "x86_64", "aarch64")},
		"SN-3": {},
	})
}

// ISO 不可用时 iso_not_ready 只在 Common 里报一次，不按主机重复，也不再逐台比平台。
func TestBatchISO不可用时只在Common报一次(t *testing.T) {
	iso := rockyISO()
	iso.State = model.ISODeprecated
	tpl := baselineTemplate()
	tpl.State = model.VersionReview
	res := Batch(BatchInput{
		Hosts:    []model.Host{hostOf("SN-1", "x86_64", "uefi"), hostOf("SN-2", "aarch64", "")},
		ISO:      iso,
		Template: tpl,
		Hooks:    baselineHooks(),
	})
	sameProblems(t, "Common", res.Common, []model.Problem{
		model.NewProblem("iso_not_ready", onISO("iso-rocky96"), "已废弃"),
		model.NewProblem("template_not_approved", onTemplate("baseline@4"), "待审批"),
	})
	sameByHost(t, res.ByHost, map[string][]model.Problem{"SN-1": {}, "SN-2": {}})
}

// 同一个 SN 在一批里出现不止一次，几处都算错，不是第一处照常建、后面的才算重复。ByHost
// 按 SN 取，几处共用一个键，原因在这个键下报一次；这台主机的其他问题也只判一次，不会成倍报出。
func TestBatch批内SN重复(t *testing.T) {
	res := Batch(BatchInput{
		Hosts: []model.Host{
			hostOf("SN-1", "x86_64", "uefi"),
			hostOf("SN-2", "aarch64", ""),
			hostOf("SN-1", "x86_64", "uefi"),
			hostOf("SN-3", "", ""),
			hostOf("SN-2", "aarch64", ""),
			hostOf("SN-2", "aarch64", ""),
		},
		OpenTask: map[string]bool{"SN-2": true},
		ISO:      rockyISO(),
		Template: baselineTemplate(),
		Hooks:    baselineHooks(),
	})
	sameProblems(t, "Common", res.Common, nil)
	sameByHost(t, res.ByHost, map[string][]model.Problem{
		"SN-1": {model.NewProblem("host_duplicate_in_batch", onHost("SN-1"))},
		"SN-2": {
			model.NewProblem("host_duplicate_in_batch", onHost("SN-2")),
			model.NewProblem("host_has_open_task", onHost("SN-2")),
			model.NewProblem("arch_mismatch", onHost("SN-2"), "x86_64", "aarch64"),
		},
		"SN-3": {},
	})
}
