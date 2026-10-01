package params

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

const hostSN = "SN-0001"

// baselineBody 是一份 kickstart 正文：两个模板参数，片段插槽「分区」「软件包」，
// post-target 执行插槽「装后配置」，三个必需部件各一次。
const baselineBody = `text
url --url={{sys:repo}}
{{lober:syslog}}
network --hostname={{param:hostname}}
timezone {{param:timezone}}
{{slot:分区}}
%packages
{{slot:软件包}}
%end
%post
{{slot:装后配置@post-target}}
{{lober:firstboot}}
{{lober:cleanup}}
%end
`

func int64p(v int64) *int64 { return &v }

// diskHook 的两份片段引用的参数不同：rocky 只用系统盘，almalinux 另要交换分区的大小。
// 同一个模板配不同系的 ISO，选中的实现不同，必填的参数也就不同。
func diskHook() model.Hook {
	return model.Hook{
		ID: "disk", Version: 1, Name: "系统盘分区", State: model.VersionApproved,
		Params: []model.ParamDecl{
			{Name: "disk", Label: "系统盘", Type: "diskpath"},
			{Name: "swap_mb", Label: "交换分区（MiB）", Type: "int", Min: int64p(0)},
		},
		Impls: []model.Impl{
			{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment,
				Body: "ignoredisk --only-use={{param:disk}}\nautopart --type=lvm\n"},
			{Family: "linux", Series: []catalog.Series{"almalinux"}, Kind: model.ImplFragment,
				Body: "ignoredisk --only-use={{param:disk}}\npart / --fstype=xfs --grow\npart swap --size={{param:swap_mb}}\n"},
		},
	}
}

func pkgsHook() model.Hook {
	return model.Hook{
		ID: "pkgs", Version: 1, Name: "软件包", State: model.VersionApproved,
		Params: []model.ParamDecl{
			{Name: "pkgs", Label: "软件包", Type: "word", List: true, Default: "vim-enhanced\ntmux"},
		},
		Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky", "almalinux"},
			Kind: model.ImplFragment, Body: "{{param:pkgs}}\n"}},
	}
}

// nicHook 是装后脚本：它的参数只经环境变量进脚本，必填与否看声明。
func nicHook() model.Hook {
	return model.Hook{
		ID: "nic", Version: 2, Name: "业务网卡", State: model.VersionApproved, TimeoutSec: 300, Required: true,
		Params: []model.ParamDecl{
			{Name: "ip", Label: "地址与前缀", Type: "cidr", Required: true},
			{Name: "device", Label: "网卡名", Type: "word", Required: true},
			{Name: "mtu", Label: "MTU", Type: "int", Default: "1500", Min: int64p(576), Max: int64p(9216)},
			{Name: "note", Label: "备注", Type: "text"},
		},
		Impls: []model.Impl{{
			Family: "linux", Series: []catalog.Series{"rocky", "almalinux"}, Kind: model.ImplScript,
			Interpreter: "sh", Timings: []catalog.Timing{"post-target"},
			Body: "#!/bin/sh\nnmcli con add type ethernet ifname \"$LOBER_PARAM_DEVICE\" " +
				"ipv4.addresses \"$LOBER_PARAM_IP\" 802-3-ethernet.mtu \"$LOBER_PARAM_MTU\"\n",
		}},
	}
}

func hookMap(hs ...model.Hook) map[model.HookKey]model.Hook {
	m := make(map[model.HookKey]model.Hook, len(hs))
	for _, h := range hs {
		m[h.Key()] = h
	}
	return m
}

// baseline 勾 rocky 与 almalinux。「装后配置」里放了同一个脚本 hook 两次：第一个实例的地址
// 锁定取自主机属性 ip、网卡名写死，第二个都可改。装配表故意不按正文的顺序写。
func baseline() model.Template {
	return model.Template{
		ID: "baseline", Version: 3, Name: "Linux 基线", State: model.VersionApproved,
		Family: "linux", Series: []catalog.Series{"rocky", "almalinux"}, Body: baselineBody,
		Params: []model.ParamDecl{
			{Name: "hostname", Label: "主机名", Type: "hostname"},
			{Name: "timezone", Label: "时区", Type: "word", Default: "Asia/Shanghai"},
		},
		Bindings: map[string]model.Binding{
			"hostname": {Source: model.BindingAttr, Attr: "hostname", Editable: true},
		},
		Assembly: map[string][]model.Instance{
			"装后配置": {
				{HookID: "nic", HookVersion: 2, Bindings: map[string]model.Binding{
					"ip":     {Source: model.BindingAttr, Attr: "ip"},
					"device": {Source: model.BindingFixed, Value: "eth1"},
				}},
				{HookID: "nic", HookVersion: 2, Bindings: map[string]model.Binding{
					"ip":     {Source: model.BindingAttr, Attr: "ip2", Editable: true},
					"device": {Source: model.BindingFixed, Value: "eth2", Editable: true},
				}},
			},
			"软件包": {{HookID: "pkgs", HookVersion: 1}},
			"分区": {{HookID: "disk", HookVersion: 1, Bindings: map[string]model.Binding{
				"disk": {Source: model.BindingAttr, Attr: "disk"},
			}}},
		},
	}
}

func baselineInput(s catalog.Series) Input {
	return Input{
		Template: baseline(),
		Hooks:    hookMap(diskHook(), pkgsHook(), nicHook()),
		ISO: model.ISO{ID: string(s) + "-9.6", Name: string(s) + " 9.6", Family: "linux", Series: s,
			Version: "9.6", Arch: "x86_64", State: model.ISOReady},
		Host: model.Host{SN: hostSN, Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{
			"hostname": "web-01", "disk": "/dev/sda", "ip": "10.0.0.11/24", "ip2": "192.168.10.11/24", "rack": "A-07",
		}},
	}
}

// at 是落在字段 key 上的问题应有的代码与 Subject：Kind 为 param，ID 为主机 SN，Path 为字段键。
func at(code model.Code, key FieldKey) model.Problem {
	return model.Problem{Code: code, Subject: model.Subject{Kind: "param", ID: hostSN, Path: string(key)}}
}

// slotAt 是落在实例上的结构性问题：Kind 为 slot，ID 为「模板 ID@版本」，Path 为「插槽名/序号」。
func slotAt(code model.Code, path string) model.Problem {
	return model.Problem{Code: code, Subject: model.Subject{Kind: "slot", ID: "baseline@3", Path: path}}
}

// mustFail 按顺序比较问题的代码与 Subject（说明由各测试另查），并确认有问题时 Values 为空。
// 问题对不上就停下：调用方接着要按下标查说明。
func mustFail(t *testing.T, res Result, want ...model.Problem) {
	t.Helper()
	if len(res.Values) != 0 {
		t.Errorf("有问题时 Values 应为空，得到 %q", canonical(res.Values))
	}
	got := make([]model.Problem, len(res.Problems))
	for i, p := range res.Problems {
		if p.Message == "" || p.Next == "" {
			t.Errorf("问题 %s 缺少说明或下一步", p.Code)
		}
		got[i] = model.Problem{Code: p.Code, Subject: p.Subject}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("问题是\n%v\n应为\n%v", got, want)
	}
}

// mustPass 确认没有问题，返回各字段取值的规范形式。
func mustPass(t *testing.T, res Result) map[FieldKey]string {
	t.Helper()
	if len(res.Problems) != 0 {
		t.Fatalf("不应有问题，得到 %v", res.Problems)
	}
	return canonical(res.Values)
}

func canonical(vs Values) map[FieldKey]string {
	out := make(map[FieldKey]string, len(vs))
	for k, v := range vs {
		out[k] = v.Canonical()
	}
	return out
}

func fieldKeys(res Result) []FieldKey {
	keys := make([]FieldKey, len(res.Fields))
	for i, f := range res.Fields {
		keys[i] = f.Key
	}
	return keys
}

func fieldOf(t *testing.T, res Result, key FieldKey) Field {
	t.Helper()
	i := slices.IndexFunc(res.Fields, func(f Field) bool { return f.Key == key })
	if i < 0 {
		t.Fatalf("表单里没有字段 %s，只有 %v", key, fieldKeys(res))
	}
	return res.Fields[i]
}

// 完整模板配 rocky 的 ISO：先列模板参数，再按插槽在正文里的顺序、实例在插槽里的顺序；
// 预填按取值来源，锁定看 Editable，必填看引用与声明。
func TestEvaluate完整模板的表单与取值(t *testing.T) {
	res := Evaluate(baselineInput("rocky"))
	want := []Field{
		{Key: "tpl/hostname", Label: "主机名", Type: "hostname", Required: true, Prefill: "web-01"},
		{Key: "tpl/timezone", Label: "时区", Type: "word", Required: true, Prefill: "Asia/Shanghai"},
		{Key: "分区/1/disk", Label: "系统盘", Type: "diskpath", Required: true, Locked: true, Prefill: "/dev/sda"},
		{Key: "分区/1/swap_mb", Label: "交换分区（MiB）", Type: "int"},
		{Key: "软件包/1/pkgs", Label: "软件包", Type: "word", List: true, Required: true, Prefill: "vim-enhanced\ntmux"},
		{Key: "装后配置/1/ip", Label: "地址与前缀", Type: "cidr", Required: true, Locked: true, Prefill: "10.0.0.11/24"},
		{Key: "装后配置/1/device", Label: "网卡名", Type: "word", Required: true, Locked: true, Prefill: "eth1"},
		{Key: "装后配置/1/mtu", Label: "MTU", Type: "int", Prefill: "1500"},
		{Key: "装后配置/1/note", Label: "备注", Type: "text"},
		{Key: "装后配置/2/ip", Label: "地址与前缀", Type: "cidr", Required: true, Prefill: "192.168.10.11/24"},
		{Key: "装后配置/2/device", Label: "网卡名", Type: "word", Required: true, Prefill: "eth2"},
		{Key: "装后配置/2/mtu", Label: "MTU", Type: "int", Prefill: "1500"},
		{Key: "装后配置/2/note", Label: "备注", Type: "text"},
	}
	if !reflect.DeepEqual(res.Fields, want) {
		t.Errorf("表单是\n%+v\n应为\n%+v", res.Fields, want)
	}
	got := mustPass(t, res)
	// 没有值的可选字段（交换分区、备注）不进 Values。
	wantValues := map[FieldKey]string{
		"tpl/hostname": "web-01", "tpl/timezone": "Asia/Shanghai",
		"分区/1/disk": "/dev/sda", "软件包/1/pkgs": "vim-enhanced\ntmux",
		"装后配置/1/ip": "10.0.0.11/24", "装后配置/1/device": "eth1", "装后配置/1/mtu": "1500",
		"装后配置/2/ip": "192.168.10.11/24", "装后配置/2/device": "eth2", "装后配置/2/mtu": "1500",
	}
	if !reflect.DeepEqual(got, wantValues) {
		t.Errorf("取值是 %q，应为 %q", got, wantValues)
	}
}

// Assembly 与 Entered 都是 map，遍历顺序每次不同；同样的输入，表单与问题的顺序必须不变。
func TestEvaluate同样的输入顺序不变(t *testing.T) {
	in := baselineInput("almalinux")
	in.Entered = map[FieldKey]string{"装后配置/2/mtu": "abc", "zzz": "1", "tpl/x": "1", "分区/9/disk": "/dev/sdb"}
	first := Evaluate(in)
	if len(first.Problems) < 3 {
		t.Fatalf("这组输入应有多个问题，才比较得出问题的顺序：%v", first.Problems)
	}
	for range 50 {
		if got := Evaluate(in); !reflect.DeepEqual(got, first) {
			t.Fatalf("同样的输入得到了不同的结果：\n%+v\n%+v", got, first)
		}
	}
}

// 同一个 hook 在一个插槽里放两次（C17）：字段键按实例的序号分开，各自的取值来源和操作员
// 填的值只作用于自己。
func TestEvaluate同一hook放两次时两个实例互不干扰(t *testing.T) {
	in := baselineInput("rocky")
	in.Entered = map[FieldKey]string{"装后配置/2/mtu": " 9000 ", "装后配置/2/note": "接存储网"}
	got := mustPass(t, Evaluate(in))
	for key, want := range map[FieldKey]string{
		"装后配置/1/ip": "10.0.0.11/24", "装后配置/1/device": "eth1", "装后配置/1/mtu": "1500",
		"装后配置/2/ip": "192.168.10.11/24", "装后配置/2/device": "eth2", "装后配置/2/mtu": "9000",
		"装后配置/2/note": "接存储网",
	} {
		if got[key] != want {
			t.Errorf("%s 取值 %q，应为 %q", key, got[key], want)
		}
	}
	if v, ok := got["装后配置/1/note"]; ok {
		t.Errorf("第一个实例的备注没人填，不应有值，得到 %q", v)
	}
}

// 可改的字段用操作员填的值，再按类型规范化；没填（含填了空串）就用预填值。列表每行一项。
func TestEvaluate可改的字段用操作员填的值(t *testing.T) {
	in := baselineInput("rocky")
	in.Entered = map[FieldKey]string{
		"tpl/hostname":  "WEB-01.Lab",
		"tpl/timezone":  "",
		"软件包/1/pkgs":    "nginx\r\n\r\nchrony\n",
		"装后配置/2/ip":     "192.168.20.11/24",
		"装后配置/2/device": "bond0",
	}
	got := mustPass(t, Evaluate(in))
	for key, want := range map[FieldKey]string{
		"tpl/hostname": "web-01.lab", "tpl/timezone": "Asia/Shanghai", "软件包/1/pkgs": "nginx\nchrony",
		"装后配置/2/ip": "192.168.20.11/24", "装后配置/2/device": "bond0",
	} {
		if got[key] != want {
			t.Errorf("%s 取值 %q，应为 %q", key, got[key], want)
		}
	}
}

// 锁定的字段只用预填值，操作员填了就报 param_locked，填的与预填相同也算；填空串不算填。
func TestEvaluate锁定的字段不能填(t *testing.T) {
	in := baselineInput("rocky")
	in.Entered = map[FieldKey]string{"装后配置/1/device": "eth9", "装后配置/1/ip": "", "分区/1/disk": "/dev/sda"}
	res := Evaluate(in)
	mustFail(t, res, at("param_locked", "分区/1/disk"), at("param_locked", "装后配置/1/device"))
	if msg := res.Problems[1].Message; !strings.Contains(msg, "网卡名") {
		t.Errorf("param_locked 的说明应点出字段的显示名：%s", msg)
	}
}

// 主机没登记被引用的属性，就当作没有值，不回落到声明的默认值：回落会让按主机指定系统盘的
// 模板，在漏登记的主机上悄悄用默认盘。锁定的报 param_attr_missing，下一步是去补属性；
// 可改的报 param_required，操作员填上就能过。
func TestEvaluate主机缺属性时不回落到默认值(t *testing.T) {
	input := func(editable bool) Input {
		in := baselineInput("rocky")
		disk := diskHook()
		disk.Params[0].Default = "/dev/vda"
		in.Hooks = hookMap(disk, pkgsHook(), nicHook())
		in.Template.Assembly["分区"][0].Bindings["disk"] = model.Binding{Source: model.BindingAttr, Attr: "disk", Editable: editable}
		delete(in.Host.Attrs, "disk")
		return in
	}

	locked := Evaluate(input(false))
	mustFail(t, locked, at("param_attr_missing", "分区/1/disk"))
	if f := fieldOf(t, locked, "分区/1/disk"); f.Prefill != "" || !f.Locked || !f.Required {
		t.Errorf("缺属性的锁定字段应必填、锁定、没有预填：%+v", f)
	}
	if p := locked.Problems[0]; p.Code.Category() != model.CategoryIncompatible ||
		!strings.Contains(p.Message, "系统盘") || !strings.Contains(p.Next, "disk") {
		t.Errorf("param_attr_missing 应为 incompatible，说明点出字段，下一步点出属性键：%+v", p)
	}

	editable := input(true)
	mustFail(t, Evaluate(editable), at("param_required", "分区/1/disk"))
	editable.Entered = map[FieldKey]string{"分区/1/disk": "/dev/nvme0n1"}
	if got := mustPass(t, Evaluate(editable)); got["分区/1/disk"] != "/dev/nvme0n1" {
		t.Errorf("操作员填了系统盘，取值应为 /dev/nvme0n1，得到 %q", got["分区/1/disk"])
	}
}

// 取值来源写明 default 时与没有 Binding 一样，预填声明的默认值，锁不锁照样看 Editable：模板把
// 参数锁在默认值上，写的就是 {Source: default, Editable: false}（C20）。锁定的只用默认值，填了
// 报 param_locked；可改的用操作员填的值，没填用默认值。
func TestEvaluate取值来源为default时用声明的默认值(t *testing.T) {
	input := func(editable bool, entered string) Input {
		in := baselineInput("rocky")
		in.Template.Bindings["timezone"] = model.Binding{Source: model.BindingDefault, Editable: editable}
		in.Entered = map[FieldKey]string{"tpl/timezone": entered}
		return in
	}

	locked := Evaluate(input(false, ""))
	if f := fieldOf(t, locked, "tpl/timezone"); !f.Locked || f.Prefill != "Asia/Shanghai" {
		t.Errorf("锁在默认值上的时区应锁定、预填 Asia/Shanghai：%+v", f)
	}
	if got := mustPass(t, locked); got["tpl/timezone"] != "Asia/Shanghai" {
		t.Errorf("锁定的时区取值 %q，应为默认值 Asia/Shanghai", got["tpl/timezone"])
	}
	mustFail(t, Evaluate(input(false, "UTC")), at("param_locked", "tpl/timezone"))

	editable := Evaluate(input(true, ""))
	if f := fieldOf(t, editable, "tpl/timezone"); f.Locked || f.Prefill != "Asia/Shanghai" {
		t.Errorf("可改的时区不应锁定，应预填 Asia/Shanghai：%+v", f)
	}
	if got := mustPass(t, editable); got["tpl/timezone"] != "Asia/Shanghai" {
		t.Errorf("没填时区，取值 %q，应为默认值 Asia/Shanghai", got["tpl/timezone"])
	}
	if got := mustPass(t, Evaluate(input(true, "UTC"))); got["tpl/timezone"] != "UTC" {
		t.Errorf("填了时区 UTC，取值 %q，应为 UTC", got["tpl/timezone"])
	}
}

// 回归 072cf91、7d22dfa：正文和片段没有条件语法，引用的参数缺值只会渲染出空值。所以被引用的
// 参数一律必填，不看声明的 Required；没有值时报 param_required，而不是得到空串。
func TestEvaluate正文和片段引用的参数没有值时报必填(t *testing.T) {
	in := baselineInput("rocky")
	in.Template.Params[1].Default = ""                     // 正文引用的时区
	delete(in.Template.Assembly["分区"][0].Bindings, "disk") // 片段引用的系统盘改用声明的默认值，而它没有
	res := Evaluate(in)
	mustFail(t, res, at("param_required", "tpl/timezone"), at("param_required", "分区/1/disk"))
	for _, key := range []FieldKey{"tpl/timezone", "分区/1/disk"} {
		if f := fieldOf(t, res, key); !f.Required || f.Locked || f.Prefill != "" {
			t.Errorf("%s 应必填、可改、没有预填：%+v", key, f)
		}
	}
	if msg := res.Problems[1].Message; !strings.Contains(msg, "系统盘") {
		t.Errorf("param_required 的说明应点出字段的显示名：%s", msg)
	}
}

// netmask 只适用于 IPv4，激活判定只看声明，取值是不是 IPv6 要到这里才知道。正文或所选片段
// 取了 netmask 的参数，取值是 IPv6 的 cidr（列表里有一项是也算），报 param_accessor_invalid，
// 落在那一格上：不然建任务和快照构建都会放行，要到渲染应答文件时才失败。只取 addr 与 prefix
// 的，取值是 IPv6 也能用。
func TestEvaluate正文和片段取的子值要取得出(t *testing.T) {
	// 正文取模板参数 mgmt 的地址与掩码、v6 的地址与前缀；「网络」插槽里的片段对 ips 的每一项
	// 写一行，取地址与掩码。
	input := func(entered map[FieldKey]string) Input {
		in := baselineInput("rocky")
		in.Template.Body += "network --device=eth0 --ip={{param:mgmt.addr}} --netmask={{param:mgmt.netmask}}" +
			" --ipv6={{param:v6.addr}}/{{param:v6.prefix}}\n{{slot:网络}}\n"
		in.Template.Params = append(in.Template.Params,
			model.ParamDecl{Name: "mgmt", Label: "管理网地址", Type: "cidr"},
			model.ParamDecl{Name: "v6", Label: "管理网 IPv6 地址", Type: "cidr"})
		network := model.Hook{
			ID: "net", Version: 1, Name: "业务网络", State: model.VersionApproved,
			Params: []model.ParamDecl{{Name: "ips", Label: "业务地址", Type: "cidr", List: true}},
			Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky", "almalinux"}, Kind: model.ImplFragment,
				Body: "network --device=bond0 --ip={{param:ips.addr}} --netmask={{param:ips.netmask}}\n"}},
		}
		in.Hooks = hookMap(diskHook(), pkgsHook(), nicHook(), network)
		in.Template.Assembly["网络"] = []model.Instance{{HookID: "net", HookVersion: 1}}
		in.Entered = entered
		return in
	}

	good := map[FieldKey]string{"tpl/mgmt": "10.0.0.5/24", "tpl/v6": "2001:db8::5/64", "网络/1/ips": "10.0.1.11/24\n10.0.2.11/24"}
	got := mustPass(t, Evaluate(input(good)))
	for key, want := range good {
		if got[key] != want {
			t.Errorf("%s 取值 %q，应为 %q", key, got[key], want)
		}
	}

	for name, c := range map[string]struct {
		key FieldKey
		raw string
	}{
		"正文取掩码的是 IPv6":      {"tpl/mgmt", "2001:db8::5/64"},
		"片段取掩码的列表有一项是 IPv6": {"网络/1/ips", "10.0.1.11/24\n2001:db8:1::11/64"},
	} {
		t.Run(name, func(t *testing.T) {
			entered := maps.Clone(good)
			entered[c.key] = c.raw
			res := Evaluate(input(entered))
			mustFail(t, res, at("param_accessor_invalid", c.key))
			if msg := res.Problems[0].Message; !strings.Contains(msg, "「netmask」") {
				t.Errorf("param_accessor_invalid 的说明应点出取不出的子值：%s", msg)
			}
		})
	}
}

// 只被脚本用到的参数按声明的 Required：缺值的必填参数报错，可选的没有值也不报。
func TestEvaluate只被脚本用到的参数按声明决定必填(t *testing.T) {
	in := baselineInput("rocky")
	delete(in.Template.Assembly["装后配置"][0].Bindings, "device")
	nic := nicHook()
	nic.Params[2].Default = "" // mtu
	in.Hooks = hookMap(diskHook(), pkgsHook(), nic)
	res := Evaluate(in)
	mustFail(t, res, at("param_required", "装后配置/1/device"))
	for key, required := range map[FieldKey]bool{"装后配置/1/device": true, "装后配置/1/mtu": false, "装后配置/1/note": false} {
		if f := fieldOf(t, res, key); f.Required != required {
			t.Errorf("%s 的 Required 是 %v，应为 %v", key, f.Required, required)
		}
	}
}

// 同一个模板配 rocky 与 almalinux 的 ISO，按 ISO 的系选实现，必填集合随之不同：
// almalinux 的分区片段另引用了交换分区的大小。
func TestEvaluate按ISO的系选实现(t *testing.T) {
	required := func(res Result) []FieldKey {
		var keys []FieldKey
		for _, f := range res.Fields {
			if f.Required {
				keys = append(keys, f.Key)
			}
		}
		return keys
	}
	rocky := Evaluate(baselineInput("rocky"))
	mustPass(t, rocky)
	alma := Evaluate(baselineInput("almalinux"))
	mustFail(t, alma, at("param_required", "分区/1/swap_mb"))
	r, a := required(rocky), required(alma)
	if want := slices.Insert(slices.Clone(r), 3, "分区/1/swap_mb"); !slices.Equal(a, want) {
		t.Errorf("必填字段：rocky 是 %v，almalinux 是 %v；almalinux 应只多出 分区/1/swap_mb", r, a)
	}

	in := baselineInput("almalinux")
	in.Entered = map[FieldKey]string{"分区/1/swap_mb": "2048"}
	if got := mustPass(t, Evaluate(in)); got["分区/1/swap_mb"] != "2048" {
		t.Errorf("交换分区取值 %q，应为 2048", got["分区/1/swap_mb"])
	}
}

// winInput 是 unattend 模板配 Windows Server 的 ISO：版本（edition）的选项取自 ISO 提供的
// editions 集合（C21），界面语言是静态选项。
func winInput(offers map[string][]model.Option) Input {
	body := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<unattend xmlns=\"urn:schemas-microsoft-com:unattend\">\n" +
		"  <settings pass=\"windowsPE\">\n" +
		"    <MetaData><Key>/IMAGE/INDEX</Key><Value>{{param:edition}}</Value></MetaData>\n" +
		"    <UILanguage>{{param:lang}}</UILanguage>\n" +
		"  </settings>\n  {{lober:firstboot}}\n  {{lober:cleanup}}\n</unattend>\n"
	return Input{
		Template: model.Template{
			ID: "win", Version: 1, Name: "Windows Server", State: model.VersionApproved,
			Family: "windows", Series: []catalog.Series{"windows-server"}, Body: body,
			Params: []model.ParamDecl{
				{Name: "edition", Label: "版本", Type: "enum", Offer: "editions"},
				{Name: "lang", Label: "界面语言", Type: "enum", Default: "zh-CN",
					Options: []model.Option{{Value: "zh-CN", Label: "简体中文"}, {Value: "en-US", Label: "English"}}},
			},
		},
		ISO: model.ISO{ID: "ws2025", Name: "Windows Server 2025", Family: "windows", Series: "windows-server",
			Version: "10.0.26100", Arch: "x86_64", State: model.ISOReady, Offers: offers},
		Host: model.Host{SN: hostSN},
	}
}

func editions() []model.Option {
	return []model.Option{
		{Value: "1", Label: "Windows Server 2025 Standard"},
		{Value: "2", Label: "Windows Server 2025 Standard（桌面体验）"},
		{Value: "3", Label: "Windows Server 2025 Datacenter"},
	}
}

// 声明了 Offer 的 enum 以 ISO 提供的同名集合为选项，取值必须在其中（C21），不在报
// param_not_offered；静态选项不符仍是 param_value_invalid。
func TestEvaluateISO提供的选项(t *testing.T) {
	in := winInput(map[string][]model.Option{"editions": editions()})
	in.Entered = map[FieldKey]string{"tpl/edition": "2"}
	res := Evaluate(in)
	if got := mustPass(t, res); got["tpl/edition"] != "2" || got["tpl/lang"] != "zh-CN" {
		t.Errorf("取值是 %q，应为版本 2、界面语言 zh-CN", got)
	}
	if f := fieldOf(t, res, "tpl/edition"); !reflect.DeepEqual(f.Options, editions()) || !f.Required {
		t.Errorf("版本字段应必填，选项取 ISO 的 editions：%+v", f)
	}
	if f := fieldOf(t, res, "tpl/lang"); !reflect.DeepEqual(f.Options, in.Template.Params[1].Options) {
		t.Errorf("界面语言的选项应为声明的静态选项：%+v", f)
	}

	in.Entered = map[FieldKey]string{"tpl/edition": "7"}
	res = Evaluate(in)
	mustFail(t, res, at("param_not_offered", "tpl/edition"))
	if msg := res.Problems[0].Message; !strings.Contains(msg, "版本") || !strings.Contains(msg, "「7」") || !strings.Contains(msg, "editions") {
		t.Errorf("param_not_offered 的说明应点出字段、填的值和集合名：%s", msg)
	}

	in.Entered = map[FieldKey]string{"tpl/edition": "2", "tpl/lang": "fr-FR"}
	mustFail(t, Evaluate(in), at("param_value_invalid", "tpl/lang"))
}

// ISO 没有提供参数要的集合，或者给的是空集合：组合不兼容，报 iso_offer_missing。选项无从
// 列出，填的值也无从核对，这一格不再另报别的问题。
func TestEvaluateISO没有提供参数要的集合(t *testing.T) {
	for name, offers := range map[string]map[string][]model.Option{
		"没有 Offers":     nil,
		"只有别的集合":        {"langs": editions()},
		"editions 是空集合": {"editions": {}},
	} {
		t.Run(name, func(t *testing.T) {
			in := winInput(offers)
			in.Entered = map[FieldKey]string{"tpl/edition": "2"}
			res := Evaluate(in)
			mustFail(t, res, at("iso_offer_missing", "tpl/edition"))
			if f := fieldOf(t, res, "tpl/edition"); len(f.Options) != 0 {
				t.Errorf("ISO 没有提供集合时版本字段不应有选项：%+v", f)
			}
			if c := res.Problems[0].Code.Category(); c != model.CategoryIncompatible {
				t.Errorf("iso_offer_missing 的类别是 %s，应为 incompatible", c)
			}
		})
	}
}

// Entered 里有表单中不存在的字段键（模板或 ISO 换过了，表单是旧的）：按键的字典序报在
// 各字段的问题之后，填的是空串也报。
func TestEvaluate表单里没有的字段键(t *testing.T) {
	in := baselineInput("rocky")
	in.Entered = map[FieldKey]string{
		"软件包/1/pkgz": "", "tpl/nope": "x", "分区/2/disk": "/dev/sdb", "装后配置/1/mtu": "abc",
	}
	res := Evaluate(in)
	mustFail(t, res,
		at("param_value_invalid", "装后配置/1/mtu"),
		at("param_unknown_field", "tpl/nope"),
		at("param_unknown_field", "分区/2/disk"),
		at("param_unknown_field", "软件包/1/pkgz"))
	if msg := res.Problems[1].Message; !strings.Contains(msg, "tpl/nope") {
		t.Errorf("param_unknown_field 的说明应点出字段键：%s", msg)
	}
}

// 本函数假定模板已通过激活判定。引用的 hook 不在 Hooks 里、或实例没有覆盖 ISO 的系的实现，
// 报对应的代码并跳过那个实例，其余字段照常给出，不 panic。
func TestEvaluate结构性缺失时跳过那个实例(t *testing.T) {
	in := baselineInput("rocky")
	delete(in.Hooks, model.HookKey{ID: "nic", Version: 2})
	res := Evaluate(in)
	mustFail(t, res, slotAt("hook_ref_missing", "装后配置/1"), slotAt("hook_ref_missing", "装后配置/2"))
	if got, want := fieldKeys(res), []FieldKey{"tpl/hostname", "tpl/timezone", "分区/1/disk", "分区/1/swap_mb", "软件包/1/pkgs"}; !slices.Equal(got, want) {
		t.Errorf("字段是 %v，应为 %v", got, want)
	}
	if msg := res.Problems[0].Message; !strings.Contains(msg, "nic@2") {
		t.Errorf("hook_ref_missing 的说明应点出 hook 与版本：%s", msg)
	}

	in = baselineInput("almalinux")
	pkgs := pkgsHook()
	pkgs.Impls[0].Series = []catalog.Series{"rocky"}
	in.Hooks = hookMap(diskHook(), pkgs, nicHook())
	in.Entered = map[FieldKey]string{"分区/1/swap_mb": "2048"}
	res = Evaluate(in)
	mustFail(t, res, slotAt("impl_missing_for_series", "软件包/1"))
	if slices.Contains(fieldKeys(res), "软件包/1/pkgs") {
		t.Errorf("跳过的实例不应有字段：%v", fieldKeys(res))
	}
	if msg := res.Problems[0].Message; !strings.Contains(msg, "pkgs@1") || !strings.Contains(msg, "almalinux") {
		t.Errorf("impl_missing_for_series 的说明应点出 hook 与系：%s", msg)
	}

	if res := Evaluate(Input{}); len(res.Fields) != 0 || len(res.Problems) != 0 {
		t.Errorf("空输入应得到空表单、没有问题：%+v", res)
	}
}

// 返回的选项是副本：调用方改了它，ISO 与参数声明都不变，下一次求值也不受影响。
func TestEvaluate返回的选项是副本(t *testing.T) {
	in := winInput(map[string][]model.Option{"editions": editions()})
	res := Evaluate(in)
	for _, f := range res.Fields {
		if len(f.Options) == 0 {
			t.Fatalf("%s 应有选项", f.Key)
		}
		f.Options[0].Value = "changed"
	}
	if !reflect.DeepEqual(in.ISO.Offers["editions"], editions()) || in.Template.Params[1].Options[0].Value != "zh-CN" {
		t.Errorf("改动表单的选项改到了 ISO 或参数声明：%+v，%+v", in.ISO.Offers, in.Template.Params[1].Options)
	}
	if f := fieldOf(t, Evaluate(in), "tpl/edition"); !reflect.DeepEqual(f.Options, editions()) {
		t.Errorf("再求一次，版本的选项应不受影响：%+v", f.Options)
	}
}

// 声明里没写显示名时用参数名：表单的列头和问题的说明里才不会是空的。
func TestEvaluate显示名为空时用参数名(t *testing.T) {
	in := baselineInput("rocky")
	in.Template.Params[1].Label = ""
	in.Template.Params[1].Default = ""
	res := Evaluate(in)
	mustFail(t, res, at("param_required", "tpl/timezone"))
	if f := fieldOf(t, res, "tpl/timezone"); f.Label != "timezone" {
		t.Errorf("显示名为空时 Label 应为参数名，得到 %q", f.Label)
	}
	if msg := res.Problems[0].Message; !strings.Contains(msg, "「timezone」") {
		t.Errorf("说明应用参数名点出字段：%s", msg)
	}
}

// 插槽重名、参数重名由激活判定拦下（slot_duplicate、param_decl_invalid）。这里只保证字段键
// 不重复：同名的只按第一次出现的算。
func TestEvaluate重名的插槽与参数只算第一次出现(t *testing.T) {
	in := baselineInput("rocky")
	in.Template.Body += "{{slot:分区}}\n"
	in.Template.Params = append(in.Template.Params, model.ParamDecl{Name: "hostname", Label: "重名的主机名", Type: "word"})
	res := Evaluate(in)
	mustPass(t, res)
	if got, want := fieldKeys(res), fieldKeys(Evaluate(baselineInput("rocky"))); !slices.Equal(got, want) {
		t.Errorf("字段是 %v，应与没有重名时相同：%v", got, want)
	}
	if f := fieldOf(t, res, "tpl/hostname"); f.Label != "主机名" {
		t.Errorf("重名的参数应按第一次出现的算，得到 %+v", f)
	}
}
