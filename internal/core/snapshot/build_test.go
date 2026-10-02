package snapshot

import (
	"bytes"
	"cmp"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/params"
)

const hostSN = "SN-0001"

// 两把公钥是 model 包测试里由 ssh-keygen 生成的样例：sshkey 的取值要过 blob 校验，随手编的过不了。
const (
	keyEd25519  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDNaJi8uw2oGwAug7HWX7cvFNvum8Lk5qwBmH3KVfWx3 ops@lober-test"
	keyECDSA384 = "ecdsa-sha2-nistp384 AAAAE2VjZHNhLXNoYTItbmlzdHAzODQAAAAIbmlzdHAzODQAAABhBIGOkTSFzJWDoOdKGjXYH16L0GycaJYFdxatj31pKmlacP0E9Xjd9J266BfdquMH0edbjT6tnDElpkH+/o/2Kfyda6C63hUBpCsDoZBtJdsQlESX84J6MDEgqLiYtxZq7A== root@pxe"
)

// baselineBody 是一份 kickstart 正文，插槽依次是：片段插槽「分区」「账号」，装前执行插槽
// 「装前检查」（装配表里没有实例），装后执行插槽「装后配置」（同一个脚本 hook 放两次）。
const baselineBody = `text
url --url={{sys:repo}}
{{lober:syslog}}
network --hostname={{param:hostname}}
timezone {{param:timezone}}
{{slot:分区}}
{{slot:账号}}
%pre
{{slot:装前检查@pre}}
%end
%post
{{slot:装后配置@post-target}}
{{lober:firstboot}}
{{lober:cleanup}}
%end
`

const (
	diskFragment  = "ignoredisk --only-use={{param:disk}}\nautopart --type=lvm\n"
	sshFragment   = "sshkey --username=root \"{{param:keys}}\"\n"
	nicScriptSh   = "#!/bin/sh\nnmcli con add type ethernet ifname \"$LOBER_PARAM_DEVICE\" ipv4.addresses \"$LOBER_PARAM_IP\" 802-3-ethernet.mtu \"$LOBER_PARAM_MTU\"\n"
	nicScriptBash = "#!/bin/bash\nset -euo pipefail\nnmcli con add type ethernet ifname \"$LOBER_PARAM_DEVICE\" ipv4.addresses \"$LOBER_PARAM_IP\" 802-3-ethernet.mtu \"$LOBER_PARAM_MTU\"\n"
)

func ptr[T any](v T) *T { return &v }

func diskHook() model.Hook {
	return model.Hook{
		ID: "disk", Version: 1, Name: "系统盘分区", State: model.VersionApproved,
		Params: []model.ParamDecl{{Name: "disk", Label: "系统盘", Type: "diskpath"}},
		Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky", "almalinux"},
			Kind: model.ImplFragment, Body: diskFragment}},
	}
}

// sshHook 在 kickstart 里是片段，在 Debian 系里是装后脚本（spec §2.4 的例子）。它的超时与
// 是否必需是给那份脚本的。
func sshHook() model.Hook {
	return model.Hook{
		ID: "ssh-key", Version: 2, Name: "SSH 公钥", State: model.VersionApproved, TimeoutSec: 60, Required: true,
		Params: []model.ParamDecl{{Name: "keys", Label: "公钥", Type: "sshkey", List: true}},
		Impls: []model.Impl{
			{Family: "linux", Series: []catalog.Series{"rocky", "almalinux"}, Kind: model.ImplFragment, Body: sshFragment},
			{Family: "linux", Series: []catalog.Series{"debian", "ubuntu"}, Kind: model.ImplScript,
				Interpreter: "sh", Timings: []catalog.Timing{"post-target"},
				Body: "#!/bin/sh\nmkdir -p /root/.ssh\nprintf '%s\\n' \"$LOBER_PARAM_KEYS\" >>/root/.ssh/authorized_keys\n"},
		},
	}
}

// nicHook 是装后脚本，rocky 与 almalinux 各一份：解释器和正文不同，摘要也就不同。
func nicHook() model.Hook {
	return model.Hook{
		ID: "nic", Version: 2, Name: "业务网卡", State: model.VersionApproved, TimeoutSec: 300, Required: true,
		Params: []model.ParamDecl{
			{Name: "ip", Label: "地址与前缀", Type: "cidr", Required: true},
			{Name: "device", Label: "网卡名", Type: "word", Required: true},
			{Name: "mtu", Label: "MTU", Type: "int", Default: "1500"},
			{Name: "note", Label: "备注", Type: "text"},
		},
		Impls: []model.Impl{
			{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplScript,
				Interpreter: "sh", Timings: []catalog.Timing{"post-target"}, Body: nicScriptSh},
			{Family: "linux", Series: []catalog.Series{"almalinux"}, Kind: model.ImplScript,
				Interpreter: "bash", Timings: []catalog.Timing{"post-target"}, Body: nicScriptBash},
		},
	}
}

func hookMap(hs ...model.Hook) map[model.HookKey]model.Hook {
	m := make(map[model.HookKey]model.Hook, len(hs))
	for _, h := range hs {
		m[h.Key()] = h
	}
	return m
}

// baseline 勾 rocky 与 almalinux。「装后配置」里放了同一个脚本 hook 两次：第一个实例覆盖了超时
// 与是否必需，地址锁定取自主机属性 ip；第二个沿用 hook 的默认值，地址取自 ip2、可改。「账号」的
// 实例也覆盖了超时，但它选中的是片段。
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
			"分区": {{HookID: "disk", HookVersion: 1, Bindings: map[string]model.Binding{
				"disk": {Source: model.BindingAttr, Attr: "disk"},
			}}},
			"账号": {{HookID: "ssh-key", HookVersion: 2, TimeoutSec: ptr(30), Bindings: map[string]model.Binding{
				"keys": {Source: model.BindingFixed, Value: keyEd25519 + "\n" + keyECDSA384},
			}}},
			"装后配置": {
				{HookID: "nic", HookVersion: 2, TimeoutSec: ptr(600), Required: ptr(false), Bindings: map[string]model.Binding{
					"ip":     {Source: model.BindingAttr, Attr: "ip"},
					"device": {Source: model.BindingFixed, Value: "eth1"},
				}},
				{HookID: "nic", HookVersion: 2, Bindings: map[string]model.Binding{
					"ip":     {Source: model.BindingAttr, Attr: "ip2", Editable: true},
					"device": {Source: model.BindingFixed, Value: "eth2", Editable: true},
				}},
			},
		},
	}
}

func isoFor(s catalog.Series) model.ISO {
	return model.ISO{
		ID: string(s) + "-9.6", Name: s.Label() + " 9.6", Family: s.Family(), Series: s, Version: "9.6", Arch: "x86_64",
		State: model.ISOReady, SHA256: strings.Repeat("ab", 32),
		Artifacts: map[string]string{
			"kernel": string(s) + "/9.6/x86_64/images/pxeboot/vmlinuz",
			"initrd": string(s) + "/9.6/x86_64/images/pxeboot/initrd.img",
			"repo":   string(s) + "/9.6/x86_64/os",
		},
	}
}

// baselineInput 是 baseline 配 s 系的 ISO 和一台登记齐全的主机；操作员给第二块网卡填了 MTU 和备注。
func baselineInput(s catalog.Series) params.Input {
	return params.Input{
		Template: baseline(),
		Hooks:    hookMap(diskHook(), sshHook(), nicHook()),
		ISO:      isoFor(s),
		Host: model.Host{SN: hostSN, Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{
			"hostname": "web-01", "disk": "/dev/sda", "ip": "10.0.0.11/24", "ip2": "192.168.10.11/24", "rack": "A-07",
		}},
		Entered: map[params.FieldKey]string{"装后配置/2/mtu": "9000", "装后配置/2/note": "接存储网"},
	}
}

func scalar(typ model.ParamType, item string) model.SnapshotValue {
	return model.SnapshotValue{Type: typ, Items: []string{item}}
}

// mustBuild 构建快照，并核对渲染要依赖的三条不变式（L-7，见 Build 的文档）：每个构建得出的快照
// 都要满足，不只是专门测它们的那几例。
func mustBuild(t *testing.T, in params.Input) model.Snapshot {
	t.Helper()
	snap, ps := Build(in)
	if len(ps) != 0 {
		t.Fatalf("不应有问题，得到 %v", ps)
	}
	if snap.Family == "" || snap.Format == "" {
		t.Errorf("族与格式应非空：%q、%q", snap.Family, snap.Format)
	}
	for _, s := range snap.Slots {
		for i, inst := range s.Instances {
			switch {
			case s.Exec && (inst.Kind != model.ImplScript || inst.Digest == ""):
				t.Errorf("执行插槽 %s 的第 %d 个实例应是带摘要的脚本：%+v", s.Name, i+1, inst)
			case !s.Exec && inst.Kind != model.ImplFragment:
				t.Errorf("片段插槽 %s 的第 %d 个实例应是片段：%+v", s.Name, i+1, inst)
			}
		}
	}
	return snap
}

// mustFail 确认构建不出快照：快照为零值，问题的代码与对象依次如 want（说明由各测试另查）。
func mustFail(t *testing.T, in params.Input, want ...model.Problem) []model.Problem {
	t.Helper()
	snap, ps := Build(in)
	return wantNoSnapshot(t, snap, ps, want...)
}

// wantNoSnapshot 是 mustFail 的核对部分：白盒测试直接调包内函数，结果也照此核对。
func wantNoSnapshot(t *testing.T, snap model.Snapshot, ps []model.Problem, want ...model.Problem) []model.Problem {
	t.Helper()
	if !reflect.DeepEqual(snap, model.Snapshot{}) {
		t.Errorf("有问题时不应产出快照，得到 %+v", snap)
	}
	got := make([]model.Problem, len(ps))
	for i, p := range ps {
		if p.Message == "" || p.Next == "" {
			t.Errorf("问题 %s 缺少说明或下一步", p.Code)
		}
		got[i] = model.Problem{Code: p.Code, Subject: p.Subject}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("问题是\n%v\n应为\n%v", got, want)
	}
	return ps
}

// templateAt 与 slotAt 是问题应有的代码与对象：落在模板整体上，或落在「插槽名/序号」那个实例上。
func templateAt(code model.Code) model.Problem {
	return model.Problem{Code: code, Subject: model.Subject{Kind: "template", ID: "baseline@3"}}
}

func slotAt(code model.Code, path string) model.Problem {
	return model.Problem{Code: code, Subject: model.Subject{Kind: "slot", ID: "baseline@3", Path: path}}
}

// hostAt 是落在主机 sn 上的问题应有的代码与对象。
func hostAt(code model.Code, sn string) model.Problem {
	return model.Problem{Code: code, Subject: model.Subject{Kind: "host", ID: sn}}
}

// instanceOf 取插槽 slot 里的第 n 个实例（从 1 起）。
func instanceOf(t *testing.T, snap model.Snapshot, slot string, n int) model.SnapshotInstance {
	t.Helper()
	for _, s := range snap.Slots {
		if s.Name == slot {
			if n < 1 || n > len(s.Instances) {
				t.Fatalf("插槽 %s 只有 %d 个实例，取不到第 %d 个", slot, len(s.Instances), n)
			}
			return s.Instances[n-1]
		}
	}
	t.Fatalf("快照里没有插槽 %s", slot)
	return model.SnapshotInstance{}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("JSON 编码失败：%v", err)
	}
	return b
}

func indentJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("JSON 编码失败：%v", err)
	}
	return string(b)
}

// reinsert 按键的逆序把 m 重新插入一遍：内容不变，只换构造顺序。
func reinsert[K cmp.Ordered, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for _, k := range slices.Backward(slices.Sorted(maps.Keys(m))) {
		out[k] = m[k]
	}
	return out
}

// 完整模板配 rocky 的 ISO：快照逐项写全渲染要的东西。片段插槽、执行插槽、同一 hook 的两个实例、
// 一个空插槽都在其中，参数的最终取值一个不漏。
func TestBuild完整模板配rocky的快照(t *testing.T) {
	nic := nicHook()
	want := model.Snapshot{
		Schema: 1, Family: "linux", Series: "rocky", Format: "kickstart",
		Host: model.SnapshotHost{SN: hostSN, Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{
			"hostname": "web-01", "disk": "/dev/sda", "ip": "10.0.0.11/24", "ip2": "192.168.10.11/24", "rack": "A-07",
		}},
		ISO: model.SnapshotISO{ID: "rocky-9.6", Name: "Rocky Linux 9.6", Version: "9.6", Arch: "x86_64",
			Artifacts: map[string]string{
				"kernel": "rocky/9.6/x86_64/images/pxeboot/vmlinuz",
				"initrd": "rocky/9.6/x86_64/images/pxeboot/initrd.img",
				"repo":   "rocky/9.6/x86_64/os",
			}},
		Template: model.SnapshotTemplate{ID: "baseline", Version: 3, Name: "Linux 基线", Body: baselineBody,
			Params: map[string]model.SnapshotValue{
				"hostname": scalar("hostname", "web-01"),
				"timezone": scalar("word", "Asia/Shanghai"),
			}},
		Slots: []model.SnapshotSlot{
			{Seq: 1, Name: "分区", Instances: []model.SnapshotInstance{{
				HookID: "disk", HookVersion: 1, HookName: "系统盘分区", Kind: model.ImplFragment, Body: diskFragment,
				Params: map[string]model.SnapshotValue{"disk": scalar("diskpath", "/dev/sda")},
			}}},
			// 选中的是片段：实例覆盖了超时，hook 也给了默认值，片段照样没有解释器、超时、是否必需与摘要。
			{Seq: 2, Name: "账号", Instances: []model.SnapshotInstance{{
				HookID: "ssh-key", HookVersion: 2, HookName: "SSH 公钥", Kind: model.ImplFragment, Body: sshFragment,
				Params: map[string]model.SnapshotValue{
					"keys": {Type: "sshkey", List: true, Items: []string{keyEd25519, keyECDSA384}},
				},
			}}},
			// 装配表里没有实例的插槽照样列出，实例为空。
			{Seq: 3, Name: "装前检查", Exec: true, Timing: "pre", Instances: []model.SnapshotInstance{}},
			{Seq: 4, Name: "装后配置", Exec: true, Timing: "post-target", Instances: []model.SnapshotInstance{
				// 备注没人填、也没有默认值，不进快照。
				{HookID: "nic", HookVersion: 2, HookName: "业务网卡", Kind: model.ImplScript, Body: nicScriptSh,
					Interpreter: "sh", TimeoutSec: 600, Required: false, Digest: nic.Impls[0].Digest(),
					Params: map[string]model.SnapshotValue{
						"ip": scalar("cidr", "10.0.0.11/24"), "device": scalar("word", "eth1"), "mtu": scalar("int", "1500"),
					}},
				{HookID: "nic", HookVersion: 2, HookName: "业务网卡", Kind: model.ImplScript, Body: nicScriptSh,
					Interpreter: "sh", TimeoutSec: 300, Required: true, Digest: nic.Impls[0].Digest(),
					Params: map[string]model.SnapshotValue{
						"ip": scalar("cidr", "192.168.10.11/24"), "device": scalar("word", "eth2"), "mtu": scalar("int", "9000"),
						"note": scalar("text", "接存储网"),
					}},
			}},
		},
	}
	if got := mustBuild(t, baselineInput("rocky")); !reflect.DeepEqual(got, want) {
		t.Errorf("快照是\n%s\n应为\n%s", indentJSON(t, got), indentJSON(t, want))
	}
}

// 回归 41b2fbe：旧系统的决策结果送不到渲染器。参数求值给出的每一个取值都要进快照，类型与是否
// 列表照表单；快照里也不能多出求值没给的。
func TestBuild回归41b2fbe_参数求值的取值全部进快照(t *testing.T) {
	in := baselineInput("rocky")
	res := params.Evaluate(in)
	want := map[params.FieldKey]model.SnapshotValue{}
	for _, f := range res.Fields {
		if v, ok := res.Values[f.Key]; ok {
			want[f.Key] = model.SnapshotValue{Type: f.Type, List: f.List, Items: v.Items()}
		}
	}
	if len(want) != 11 {
		t.Fatalf("这组输入应有 11 个取值，模板参数、片段与脚本的参数才都覆盖得到：%v", want)
	}
	snap := mustBuild(t, in)
	got := map[params.FieldKey]model.SnapshotValue{}
	for name, v := range snap.Template.Params {
		got[params.FieldKey("tpl/"+name)] = v
	}
	for _, s := range snap.Slots {
		for i, inst := range s.Instances {
			for name, v := range inst.Params {
				got[params.FieldKey(s.Name+"/"+strconv.Itoa(i+1)+"/"+name)] = v
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("快照里的取值是\n%v\n参数求值给的是\n%v", got, want)
	}
}

// 生效的超时与是否必需：实例的覆盖优先，否则取 hook 的默认值（C11）。覆盖是指针，nil 才算沿用，
// 覆盖成 false 也算覆盖。沿用的是这个 hook 自己的默认值，不是常量：hook 换一个超时、改成非必需，
// 沿用的实例跟着变。
func TestBuild超时与是否必需按实例覆盖否则取hook默认值(t *testing.T) {
	cases := []struct {
		name         string
		hookTimeout  int
		hookRequired bool
		timeout      *int
		required     *bool
		wantTimeout  int
		wantRequired bool
	}{
		{"都沿用 hook 的默认值", 300, true, nil, nil, 300, true},
		{"只覆盖超时", 300, true, ptr(1800), nil, 1800, true},
		{"只覆盖成非必需", 300, true, nil, ptr(false), 300, false},
		{"两项都覆盖", 300, true, ptr(30), ptr(false), 30, false},
		{"沿用 hook 的非必需与超时", 120, false, nil, nil, 120, false},
		{"把非必需的覆盖成必需", 300, false, nil, ptr(true), 300, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := baselineInput("rocky")
			nic := nicHook()
			nic.TimeoutSec, nic.Required = c.hookTimeout, c.hookRequired
			in.Hooks = hookMap(diskHook(), sshHook(), nic)
			inst := &in.Template.Assembly["装后配置"][0]
			inst.TimeoutSec, inst.Required = c.timeout, c.required
			got := instanceOf(t, mustBuild(t, in), "装后配置", 1)
			if got.TimeoutSec != c.wantTimeout || got.Required != c.wantRequired {
				t.Errorf("超时 %d、必需 %v，应为超时 %d、必需 %v", got.TimeoutSec, got.Required, c.wantTimeout, c.wantRequired)
			}
		})
	}
}

// 解释器、超时、是否必需和摘要只对脚本有意义：片段在渲染时就写进了应答文件，没有执行这一步，
// 网关也只该对脚本的摘要放行。hook 给了默认值、实例做了覆盖、片段上还残留着解释器，片段实例的
// 这四项照样为零值。
func TestBuild片段实例没有脚本才有的几项(t *testing.T) {
	in := baselineInput("rocky")
	in.Template.Assembly["账号"][0].Required = ptr(true)
	ssh := sshHook()
	ssh.Impls[0].Interpreter = "bash"
	in.Hooks = hookMap(diskHook(), ssh, nicHook())
	got := instanceOf(t, mustBuild(t, in), "账号", 1)
	if got.Kind != model.ImplFragment || got.Body != sshFragment {
		t.Fatalf("应选中覆盖 rocky 的片段，得到 %+v", got)
	}
	if got.Interpreter != "" || got.TimeoutSec != 0 || got.Required || got.Digest != "" {
		t.Errorf("片段实例的解释器、超时、是否必需与摘要应为零值：%+v", got)
	}
}

// 每个 hook 取覆盖 ISO 的系的那份实现（C27）：同一个模板配 almalinux 的 ISO，网卡脚本换成 bash
// 的那一份。摘要就是所选实现的 Impl.Digest，网关只对它放行，机器侧按它校验下载到的脚本。
func TestBuild按ISO的系选实现与摘要(t *testing.T) {
	nic := nicHook()
	if nic.Impls[0].Digest() == nic.Impls[1].Digest() {
		t.Fatal("两份脚本的摘要应不同，才区分得出选的是哪一份")
	}
	for i, s := range []catalog.Series{"rocky", "almalinux"} {
		snap := mustBuild(t, baselineInput(s))
		if snap.Family != "linux" || snap.Series != s || snap.Format != "kickstart" {
			t.Errorf("%s：族、系、格式是 %s、%s、%s", s, snap.Family, snap.Series, snap.Format)
		}
		impl := nic.Impls[i]
		for n := 1; n <= 2; n++ {
			got := instanceOf(t, snap, "装后配置", n)
			if got.Body != impl.Body || got.Interpreter != impl.Interpreter || got.Digest != impl.Digest() {
				t.Errorf("%s 的第 %d 个网卡实例应取 %s 的那份脚本：%+v", s, n, impl.Interpreter, got)
			}
		}
	}
}

// 前提不满足时不产出快照。正常流程里准入判定已经挡住了这些，这里是最后一道：错的快照要到机器
// 装到一半才会暴露。
func TestBuild前提不满足时不产出快照(t *testing.T) {
	t.Run("ISO 的系不在所勾的系里", func(t *testing.T) {
		// 账号的 hook 有覆盖 debian 的脚本，分区与网卡的没有。只报 series_not_ticked 一条：接着
		// 求值，只会多出一串因它而起的 impl_missing_for_series。
		ps := mustFail(t, baselineInput("debian"), templateAt("series_not_ticked"))
		if !strings.Contains(ps[0].Message, "debian") {
			t.Errorf("series_not_ticked 的说明应点出 ISO 的系：%s", ps[0].Message)
		}
	})
	// 不是目录里的系推不出族与格式，渲染无从下手（L-7）。草稿勾了它也算没勾：激活判定不让模板勾
	// 不认识的系。清空装配表与填的值，参数求值就没有别的问题，构建原先会产出族与格式都为空的快照。
	for _, c := range []struct {
		name   string
		series catalog.Series
	}{
		{"ISO 的系不在目录里", "centos"},
		{"ISO 的系大小写不对", "Rocky"},
		{"ISO 的系为空", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := baselineInput("rocky")
			in.Template.Series = append(in.Template.Series, c.series)
			in.ISO.Series = c.series
			in.Template.Assembly, in.Entered = nil, nil
			ps := mustFail(t, in, templateAt("series_not_ticked"))
			if !strings.Contains(ps[0].Message, "「"+string(c.series)+"」") {
				t.Errorf("series_not_ticked 的说明应点出 ISO 的系：%s", ps[0].Message)
			}
		})
	}
	t.Run("实例没有覆盖该系的实现", func(t *testing.T) {
		in := baselineInput("almalinux")
		ssh := sshHook()
		ssh.Impls[0].Series = []catalog.Series{"rocky"}
		in.Hooks = hookMap(diskHook(), ssh, nicHook())
		ps := mustFail(t, in, slotAt("impl_missing_for_series", "账号/1"))
		if msg := ps[0].Message; !strings.Contains(msg, "ssh-key@2") || !strings.Contains(msg, "almalinux") {
			t.Errorf("impl_missing_for_series 的说明应点出 hook 与系：%s", msg)
		}
	})
	t.Run("引用的 hook 不在入参里", func(t *testing.T) {
		in := baselineInput("rocky")
		in.Entered = nil // 跳过的实例没有字段，填给它们的值会另报 param_unknown_field
		delete(in.Hooks, model.HookKey{ID: "nic", Version: 2})
		mustFail(t, in, slotAt("hook_ref_missing", "装后配置/1"), slotAt("hook_ref_missing", "装后配置/2"))
	})
}

// nicIn 是放进任意插槽都过得了参数求值的一个网卡实例：地址锁定取自主机属性 ip，网卡名写死。
func nicIn() model.Instance {
	return model.Instance{HookID: "nic", HookVersion: 2, Bindings: map[string]model.Binding{
		"ip":     {Source: model.BindingAttr, Attr: "ip"},
		"device": {Source: model.BindingFixed, Value: "eth3"},
	}}
}

// 实现放不进插槽，激活判定会拦，草稿却照样能预览（C22）。渲染要依赖执行插槽里只有带摘要的脚本、
// 片段插槽里只有片段（L-7），所以构建时每个实例都按 model.Impl.FitsSlot 再查一遍：不适配的问题
// 落在「插槽名/序号」上，说明点出 hook、系，以及不对的种类或时机，不产出快照。
func TestBuild实现放不进插槽时不产出快照(t *testing.T) {
	cases := []struct {
		name string
		edit func(in *params.Input)
		want []model.Problem
		msg  []string // 每条问题的说明里都应出现
	}{
		{"片段放进执行插槽", func(in *params.Input) {
			in.Template.Assembly["装前检查"] = []model.Instance{{HookID: "disk", HookVersion: 1, Bindings: map[string]model.Binding{
				"disk": {Source: model.BindingAttr, Attr: "disk"},
			}}}
		}, []model.Problem{slotAt("impl_kind_mismatch", "装前检查/1")}, []string{"disk@1", "「rocky」", "fragment"}},
		{"脚本放进片段插槽", func(in *params.Input) {
			in.Template.Assembly["分区"] = append(in.Template.Assembly["分区"], nicIn())
		}, []model.Problem{slotAt("impl_kind_mismatch", "分区/2")}, []string{"nic@2", "「rocky」", "script"}},
		{"脚本的时机不含执行插槽的时机", func(in *params.Input) {
			in.Template.Assembly["装前检查"] = []model.Instance{nicIn()}
		}, []model.Problem{slotAt("impl_timing_mismatch", "装前检查/1")}, []string{"nic@2", "「rocky」", "「pre」"}},
		// 种类不认识的放进哪种插槽都不适配：参数求值把它当片段读，渲染却不知道该怎么展开它。
		{"片段插槽里的种类不认识", func(in *params.Input) {
			disk := diskHook()
			disk.Impls[0].Kind = "macro"
			in.Hooks[disk.Key()] = disk
		}, []model.Problem{slotAt("impl_kind_mismatch", "分区/1")}, []string{"disk@1", "macro"}},
		// 种类只认原样的写法，大小写不对的 Script 也不是脚本；两个实例各报一条。
		{"执行插槽里的种类不认识", func(in *params.Input) {
			nic := nicHook()
			nic.Impls[0].Kind = "Script"
			in.Hooks[nic.Key()] = nic
		}, []model.Problem{slotAt("impl_kind_mismatch", "装后配置/1"), slotAt("impl_kind_mismatch", "装后配置/2")},
			[]string{"nic@2", "Script"}},
		// 每处都报，按插槽在正文里的顺序：不是查到第一处就停。
		{"几处同时不适配", func(in *params.Input) {
			in.Template.Assembly["分区"] = append(in.Template.Assembly["分区"], nicIn())
			in.Template.Assembly["装前检查"] = []model.Instance{nicIn()}
		}, []model.Problem{slotAt("impl_kind_mismatch", "分区/2"), slotAt("impl_timing_mismatch", "装前检查/1")}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := baselineInput("rocky")
			c.edit(&in)
			if ps := params.Evaluate(in).Problems; len(ps) != 0 {
				t.Fatalf("参数求值应当通过，问题才只出在插槽适配上：%v", ps)
			}
			for _, p := range mustFail(t, in, c.want...) {
				for _, s := range c.msg {
					if !strings.Contains(p.Message, s) {
						t.Errorf("%s 的说明应含「%s」：%s", p.Code, s, p.Message)
					}
				}
			}
		})
	}
	// 参数求值有问题时原样返回它的问题，不再往下查插槽：与 {{sys:sn}} 的检查一样，取值都通过了才
	// 整理快照。
	t.Run("参数求值也有问题", func(t *testing.T) {
		in := baselineInput("rocky")
		in.Template.Assembly["装前检查"] = []model.Instance{nicIn()}
		in.Entered["tpl/nope"] = "x"
		want := params.Evaluate(in).Problems
		if len(want) != 1 || want[0].Code != "param_unknown_field" {
			t.Fatalf("这组输入的参数求值应只有一个 param_unknown_field：%v", want)
		}
		snap, got := Build(in)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("问题是\n%v\n应原样是参数求值的\n%v", got, want)
		}
		if !reflect.DeepEqual(snap, model.Snapshot{}) {
			t.Errorf("有问题时不应产出快照，得到 %+v", snap)
		}
	})
}

// 参数求值与整理快照各查一遍 hook 和实现，两边遍历同一组插槽与实例，求值通过了本不会查不到。两边
// 一旦走岔，整理快照也不能丢掉查找结果、产出种类与正文全空的实例（T13-8），而要报出与参数求值相同的
// 问题，不产出快照。正常的入参走不到这一步，所以白盒：按完整的入参求值，再拿走岔的入参调 buildFrom。
func TestBuild求值之后查不到hook或实现时不产出快照(t *testing.T) {
	in := baselineInput("rocky")
	res := params.Evaluate(in)
	if len(res.Problems) != 0 {
		t.Fatalf("完整的入参应能通过参数求值：%v", res.Problems)
	}
	t.Run("查不到 hook", func(t *testing.T) {
		diverged := in
		diverged.Hooks = hookMap(diskHook(), sshHook())
		snap, ps := buildFrom(diverged, res)
		ps = wantNoSnapshot(t, snap, ps, slotAt("hook_ref_missing", "装后配置/1"), slotAt("hook_ref_missing", "装后配置/2"))
		if !strings.Contains(ps[0].Message, "nic@2") {
			t.Errorf("hook_ref_missing 的说明应点出 hook：%s", ps[0].Message)
		}
	})
	t.Run("查不到覆盖该系的实现", func(t *testing.T) {
		diverged := in
		ssh := sshHook()
		ssh.Impls[0].Series = []catalog.Series{"almalinux"}
		diverged.Hooks = hookMap(diskHook(), ssh, nicHook())
		snap, ps := buildFrom(diverged, res)
		ps = wantNoSnapshot(t, snap, ps, slotAt("impl_missing_for_series", "账号/1"))
		if msg := ps[0].Message; !strings.Contains(msg, "ssh-key@2") || !strings.Contains(msg, "「rocky」") {
			t.Errorf("impl_missing_for_series 的说明应点出 hook 与系：%s", msg)
		}
	})
}

// 取值有问题时，原样返回参数求值的问题（代码、对象、说明都不变），不产出快照：建任务、预览与
// 这里用的是同一套取值规则，向导里标红的格子与这里报的一致。
func TestBuild参数求值有问题时原样返回(t *testing.T) {
	in := baselineInput("rocky")
	delete(in.Host.Attrs, "disk")
	in.Entered["装后配置/2/mtu"] = "abc"
	in.Entered["tpl/nope"] = "x"
	want := params.Evaluate(in).Problems
	if len(want) != 3 {
		t.Fatalf("这组输入应有三个问题：%v", want)
	}
	snap, got := Build(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("问题是\n%v\n参数求值报的是\n%v", got, want)
	}
	if !reflect.DeepEqual(snap, model.Snapshot{}) {
		t.Errorf("有问题时不应产出快照，得到 %+v", snap)
	}
}

// unsafeSNs 是登记得下、却不是安全词的 SN。前四个各带一种会改变结构的字符：空格（VMware 虚拟机的
// SN 常这样写）、引号、<、&；后三个是整层验收时探出来的：一串 shell 与标记语言的特殊字符、YAML 的
// 「冒号加空格」、看着像占位符的花括号。
var unsafeSNs = []string{"VMWARE-42 1A 2B 3C", `A"B`, "A<B", "A&B", `A"B<C>&D#E$(ID)`, "A: B", "{{SYS:REPO}}"}

// withSN 把主机的 SN 换成 sn，并确认它原样就是 NormalizeSN 的结果：这类 SN 真能登记进来，
// 登记规则不拦它。
func withSN(t *testing.T, in params.Input, sn string) params.Input {
	t.Helper()
	if got, err := model.NormalizeSN(sn); err != nil || got != sn {
		t.Fatalf("SN %q 应能原样登记，NormalizeSN 得到 %q、%v", sn, got, err)
	}
	in.Host.SN = sn
	return in
}

// replaceInBody 把正文里的 old 换成 new；正文里没有 old 时直接判失败，免得测试改了个寂寞。
func replaceInBody(t *testing.T, in *params.Input, old, new string) {
	t.Helper()
	if !strings.Contains(in.Template.Body, old) {
		t.Fatalf("正文里没有 %q", old)
	}
	in.Template.Body = strings.Replace(in.Template.Body, old, new, 1)
}

// snHostnameHook 拿主机 SN 当主机名，不少机房就是这样起名的。只有覆盖 rocky 的那份片段引用了
// {{sys:sn}}，覆盖 almalinux 的那份用 DHCP 给的名字。
func snHostnameHook() model.Hook {
	return model.Hook{
		ID: "sn-hostname", Version: 1, Name: "主机名取 SN", State: model.VersionApproved,
		Impls: []model.Impl{
			{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment, Body: "network --hostname={{sys:sn}}\n"},
			{Family: "linux", Series: []catalog.Series{"almalinux"}, Kind: model.ImplFragment, Body: "network --bootproto=dhcp\n"},
		},
	}
}

// useSNHostname 把正文里设主机名的那一行换成片段插槽「主机名」，放进 snHostnameHook。
func useSNHostname(t *testing.T, in *params.Input) {
	t.Helper()
	replaceInBody(t, in, "network --hostname={{param:hostname}}\n", "{{slot:主机名}}\n")
	h := snHostnameHook()
	in.Template.Assembly["主机名"] = []model.Instance{{HookID: h.ID, HookVersion: h.Version}}
	in.Hooks[h.Key()] = h
}

// echoSNInPost 在正文的 %post 里加一行 echo {{sys:sn}}：SN 里的空格会把它拆成几个参数，引号、<、&
// 会让这段 shell 出语法错，或者去读文件、另起一条命令。
func echoSNInPost(t *testing.T, in *params.Input) {
	t.Helper()
	replaceInBody(t, in, "%post\n", "%post\necho {{sys:sn}} >/etc/lober-sn\n")
}

// {{sys:sn}} 把主机 SN 原样代入正文与片段，渲染不解析语法、不知道该按什么位置转义（C18）；SN 的
// 登记规则却收空格和任意可打印 ASCII。空格会把 kickstart、preseed 的指令和 shell 参数拆开，引号、
// <>&、# 会破坏 JSON、XML、YAML 的结构，这类错要等机器装到一半才暴露。所以模板正文或按 ISO 的系
// 选中的片段引用了 {{sys:sn}}、而 SN 不是安全词时，报一条落在这台主机上的问题，不产出快照。
func TestBuild引用sys_sn而SN不是安全词时不产出快照(t *testing.T) {
	cases := []struct {
		name string
		edit func(t *testing.T, in *params.Input)
	}{
		{"模板正文引用", echoSNInPost},
		{"所选片段引用", useSNHostname},
		// 引用了几处都只报一条：问题出在这台主机的 SN 上。
		{"正文与片段都引用", func(t *testing.T, in *params.Input) {
			echoSNInPost(t, in)
			useSNHostname(t, in)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, sn := range unsafeSNs {
				t.Run(sn, func(t *testing.T) {
					in := withSN(t, baselineInput("rocky"), sn)
					c.edit(t, &in)
					ps := mustFail(t, in, hostAt("sn_unsafe_in_body", sn))
					if !strings.Contains(ps[0].Message, "「"+sn+"」") {
						t.Errorf("说明应点出这台主机的 SN：%s", ps[0].Message)
					}
				})
			}
		})
	}
	// SN 查的是即将产出的快照：参数求值有问题时没有快照可查，原样返回参数求值的问题，不另加这一条；
	// 取值都改对了，再构建时才报 SN。
	t.Run("参数求值也有问题", func(t *testing.T) {
		in := withSN(t, baselineInput("rocky"), unsafeSNs[0])
		echoSNInPost(t, &in)
		delete(in.Host.Attrs, "disk")
		want := params.Evaluate(in).Problems
		if len(want) != 1 || want[0].Code != "param_attr_missing" {
			t.Fatalf("这组输入的参数求值应只有一个 param_attr_missing：%v", want)
		}
		snap, got := Build(in)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("问题是\n%v\n应原样是参数求值的\n%v", got, want)
		}
		if !reflect.DeepEqual(snap, model.Snapshot{}) {
			t.Errorf("有问题时不应产出快照，得到 %+v", snap)
		}
	})
}

// 不引用 {{sys:sn}} 的照常构建：SN 的登记规则不动，只有代入正文与片段时才要求它是安全词。片段只看
// 按 ISO 的系选中的那一份；脚本的正文原样下发、不展开占位符（网关按摘要放行，机器侧按摘要校验），
// 里面字面写着 {{sys:sn}} 也不算引用。SN 是安全词时，正文与片段照常引用它。
func TestBuild不引用sys_sn时SN不是安全词也照常构建(t *testing.T) {
	cases := []struct {
		name   string
		series catalog.Series
		edit   func(t *testing.T, in *params.Input)
	}{
		{"正文与片段都不引用", "rocky", func(*testing.T, *params.Input) {}},
		{"引用它的片段没被选中", "almalinux", useSNHostname},
		{"只有脚本里字面写着", "rocky", func(_ *testing.T, in *params.Input) {
			nic := nicHook()
			nic.Impls[0].Body += "echo {{sys:sn}}\n"
			in.Hooks = hookMap(diskHook(), sshHook(), nic)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, sn := range unsafeSNs {
				t.Run(sn, func(t *testing.T) {
					in := withSN(t, baselineInput(c.series), sn)
					c.edit(t, &in)
					if snap := mustBuild(t, in); snap.Host.SN != sn {
						t.Errorf("快照里的 SN 是 %q，应原样为 %q", snap.Host.SN, sn)
					}
				})
			}
		})
	}
	t.Run("SN 是安全词", func(t *testing.T) {
		in := baselineInput("rocky")
		echoSNInPost(t, &in)
		useSNHostname(t, &in)
		if snap := mustBuild(t, in); snap.Host.SN != hostSN {
			t.Errorf("快照里的 SN 是 %q，应为 %q", snap.Host.SN, hostSN)
		}
	})
}

// 同样的输入得到深度相等的快照，用 encoding/json 编码字节相同（第 2 层按 JSON 存它）。输入里的
// map 换一种构造顺序，遍历顺序跟着变，结果仍然不变。
func TestBuild打乱输入里map的构造顺序后JSON仍逐字节相同(t *testing.T) {
	first := mustBuild(t, baselineInput("rocky"))
	firstJSON := mustJSON(t, first)
	in := baselineInput("rocky")
	in.Hooks = hookMap(nicHook(), sshHook(), diskHook())
	in.Template.Bindings = reinsert(in.Template.Bindings)
	in.Template.Assembly = reinsert(in.Template.Assembly)
	for _, insts := range in.Template.Assembly {
		for i := range insts {
			insts[i].Bindings = reinsert(insts[i].Bindings)
		}
	}
	in.Host.Attrs = reinsert(in.Host.Attrs)
	in.ISO.Artifacts = reinsert(in.ISO.Artifacts)
	in.Entered = reinsert(in.Entered)
	for range 20 {
		got := mustBuild(t, in)
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("同样的输入得到了不同的快照：\n%s\n%s", indentJSON(t, got), indentJSON(t, first))
		}
		if b := mustJSON(t, got); !bytes.Equal(b, firstJSON) {
			t.Fatalf("JSON 编码不同：\n%s\n%s", b, firstJSON)
		}
	}
}

// 渲染读到的必须就是构建出的：快照经 JSON 存进库再读回来，与原来深度相等。快照里因此只能有
// 导出的字段，不能有 Value 这类内部结构。
func TestBuild快照经JSON存取后不变(t *testing.T) {
	snap := mustBuild(t, baselineInput("rocky"))
	var back model.Snapshot
	if err := json.Unmarshal(mustJSON(t, snap), &back); err != nil {
		t.Fatalf("JSON 解码失败：%v", err)
	}
	if !reflect.DeepEqual(back, snap) {
		t.Errorf("经 JSON 存取后变了：\n%s\n应为\n%s", indentJSON(t, back), indentJSON(t, snap))
	}
}

// 快照一建成就固化（C29）：之后改主机登记或 ISO 记录，快照不变；改快照也改不到入参。
func TestBuild快照不与入参共用map(t *testing.T) {
	in := baselineInput("rocky")
	snap := mustBuild(t, in)
	in.Host.Attrs["disk"] = "/dev/sdz"
	in.ISO.Artifacts["kernel"] = "elsewhere/vmlinuz"
	if want := mustBuild(t, baselineInput("rocky")); !reflect.DeepEqual(snap, want) {
		t.Errorf("改动入参改到了已建的快照：\n%s", indentJSON(t, snap))
	}
	snap.Host.Attrs["rack"] = "B-01"
	snap.ISO.Artifacts["repo"] = "elsewhere/os"
	if in.Host.Attrs["rack"] != "A-07" || in.ISO.Artifacts["repo"] != "rocky/9.6/x86_64/os" {
		t.Errorf("改动快照改到了入参：%v，%v", in.Host.Attrs, in.ISO.Artifacts)
	}
}

// 没有内容的 map 与切片也给空值：快照的 JSON 里统一是 {} 与 []，读它的一方不用另判 null。
func TestBuild空集合在JSON里不是null(t *testing.T) {
	const parts = "{{lober:syslog}}\n%post\n{{lober:firstboot}}\n{{lober:cleanup}}\n%end\n"
	for name, body := range map[string]string{
		"正文里没有插槽": "text\n" + parts,
		"插槽里没有实例": "text\n%pre\n{{slot:装前检查@pre}}\n%end\n" + parts,
	} {
		t.Run(name, func(t *testing.T) {
			in := params.Input{
				Template: model.Template{ID: "bare", Version: 1, Name: "最小模板", State: model.VersionDraft,
					Family: "linux", Series: []catalog.Series{"rocky"}, Body: body},
				ISO:  model.ISO{ID: "rocky-9.6", Name: "Rocky Linux 9.6", Family: "linux", Series: "rocky", Version: "9.6", State: model.ISOReady},
				Host: model.Host{SN: hostSN},
			}
			if b := mustJSON(t, mustBuild(t, in)); bytes.Contains(b, []byte("null")) {
				t.Errorf("JSON 里不应有 null：%s", b)
			}
		})
	}
}

// 插槽重名、参数重名由激活判定拦下（slot_duplicate、param_decl_invalid），草稿照样能预览。这里与
// 参数求值一样只按第一次出现的算：插槽不重复列出，序号连续；参数的类型按进了表单的那个声明记。
// 重名的插槽插在正文中间，后面还有两个插槽：序号若按扫描到的位置编，它们会往后错一位（T11-10③）。
// 它写成执行插槽，「分区」里的片段也不按它查适配。
func TestBuild重名的插槽与参数只算第一次出现(t *testing.T) {
	in := baselineInput("rocky")
	replaceInBody(t, &in, "{{slot:账号}}\n", "{{slot:账号}}\n{{slot:分区@firstboot}}\n")
	in.Template.Params = append(in.Template.Params, model.ParamDecl{Name: "hostname", Label: "重名的主机名", Type: "word"})
	got := mustBuild(t, in)
	want := mustBuild(t, baselineInput("rocky"))
	if !reflect.DeepEqual(got.Slots, want.Slots) {
		t.Errorf("插槽是\n%s\n应与没有重名时相同：\n%s", indentJSON(t, got.Slots), indentJSON(t, want.Slots))
	}
	if !reflect.DeepEqual(got.Template.Params, want.Template.Params) {
		t.Errorf("模板参数的取值是 %v，应与没有重名时相同：%v", got.Template.Params, want.Template.Params)
	}
}
