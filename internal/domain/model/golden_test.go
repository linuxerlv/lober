// 落库类型的 JSON 编码钉在 testdata 里（T7-4）。这些类型都不带 json tag，第 2 层按 encoding/json
// 存它们时，键就是 Go 的字段名：谁在 Go 里改了字段名，库里的旧数据解码时这一项会静默变成零值，
// 不报错。这里给每个类型取一份有代表性的值，把 json.Marshal 的输出与期望文件逐字节比对，字段名、
// 字段类型或嵌套结构一变就报红。
//
// 期望文件按缩进写，评审时看得清 diff；比对前用 json.Compact 去掉缩进与换行，比的仍是 json.Marshal
// 的原始字节，也不受 git 换行转换的影响。
//
// 期望文件可以用 -update 重写：
//
//	go test ./internal/core/model/ -run Golden -update
//
// 只在下面几种情况下这样做：新增字段，或调整字段的先后（旧数据照样按键解码，新字段取零值）；第 2 层
// 已经写好迁移、把库里的旧键改成了新键，或者确认还没有任何数据按旧键落库；代表值本身要换。只为
// 让测试变绿就重写，等于拆掉这道防线。重写之后逐行看 git diff，确认变的只是想改的那几处。

package model

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

var updateGolden = flag.Bool("update", false, "按当前的编码重写 testdata 里的 golden 文件，什么时候可以这样做见 golden_test.go 的文件头")

func TestGolden落库类型的JSON编码(t *testing.T) {
	cases := []struct {
		file  string
		value any
	}{
		{"snapshot", goldenSnapshot()},
		{"template", goldenTemplate()},
		{"hook", goldenHook()},
		{"host", goldenHost()},
		{"iso", goldenISO()},
		{"task", goldenTask()},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			got, err := json.Marshal(c.value)
			if err != nil {
				t.Fatalf("JSON 编码失败：%v", err)
			}
			path := filepath.Join("testdata", c.file+".golden.json")
			if *updateGolden {
				writeGolden(t, path, got)
				return
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读不到期望文件：%v（第一次要用 -update 生成，再逐行核对）", err)
			}
			var want bytes.Buffer
			if err := json.Compact(&want, raw); err != nil {
				t.Fatalf("期望文件 %s 不是合法的 JSON：%v", path, err)
			}
			if !bytes.Equal(got, want.Bytes()) {
				t.Errorf("%T 的 JSON 编码与 %s 不一致，%s\n改字段名之前先看 golden_test.go 的文件头",
					c.value, path, firstDiffLine(got, want.Bytes()))
			}
		})
	}
}

func writeGolden(t *testing.T, path string, compact []byte) {
	t.Helper()
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		t.Fatalf("缩进 JSON 失败：%v", err)
	}
	out.WriteByte('\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatalf("写期望文件失败：%v", err)
	}
	t.Logf("已重写 %s", path)
}

// firstDiffLine 把两边都缩进成多行，指出第一处不同的那一行：一整行的 JSON 只给字节偏移，找不到
// 是哪个字段变了。
func firstDiffLine(got, want []byte) string {
	g, w := indentedLines(got), indentedLines(want)
	for i := range max(len(g), len(w)) {
		var a, b string
		if i < len(g) {
			a = g[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			return fmt.Sprintf("缩进后第 %d 行起不同：\n得到 %s\n期望 %s", i+1, strings.TrimSpace(a), strings.TrimSpace(b))
		}
	}
	return "缩进后逐行相同"
}

func indentedLines(b []byte) []string {
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return []string{string(b)}
	}
	return strings.Split(out.String(), "\n")
}

// 下面的代表值只给本测试用，别的测试改了样例不会牵动期望文件。指针、切片、map 与嵌套结构都填上
// 内容，它们编码后的形态也一并钉住。

// goldenBody 是 rocky-base@4 的正文：一个片段插槽、一个空的执行插槽、一个放了脚本的执行插槽。
const goldenBody = "text\nurl --url={{sys:repo}}\n{{lober:syslog}}\nnetwork --hostname={{param:hostname}}\n{{slot:分区}}\n" +
	"%pre\n{{slot:装前检查@pre}}\n%end\n%post\n{{slot:装后配置@post-target}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n%end\n"

// goldenScript 是 nic@2 覆盖 rocky、almalinux 的脚本，快照里取的就是它和它的摘要。
const goldenScript = "#!/bin/sh\nip addr add \"$LOBER_PARAM_IP\" dev eth1\nip link set eth1 mtu \"$LOBER_PARAM_MTU\"\n"

func goldenHost() Host {
	return Host{SN: "SN-0001", Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{
		"hostname": "web-01", "ip": "10.0.0.3/24", "rack_u": "机房 A 第 12 层",
	}}
}

func goldenISO() ISO {
	return ISO{
		ID: "iso-ws2025", Name: "Windows Server 2025", Family: "windows", Series: "windows-server",
		Version: "2025", Arch: "x86_64", State: ISOReady,
		SHA256: "3f2b8c1d4e5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0",
		Artifacts: map[string]string{
			"bootwim":    "windows-server/2025/x86_64/sources/boot.wim",
			"installwim": "windows-server/2025/x86_64/sources/install.wim",
		},
		Offers: map[string][]Option{
			"editions": {{Value: "ServerStandard", Label: "Standard"}, {Value: "ServerDatacenter", Label: "Datacenter"}},
		},
	}
}

func goldenHook() Hook {
	minMTU, maxMTU := int64(576), int64(9000)
	return Hook{
		ID: "nic", Version: 2, Name: "Business-NIC", Description: "配置业务网卡的地址、DNS 与 MTU",
		State: VersionApproved, Author: "alice", Approver: "bob",
		Params: []ParamDecl{
			{Name: "ip", Label: "业务地址", Type: "cidr"},
			{Name: "mtu", Label: "MTU", Type: "int", Default: "1500", Min: &minMTU, Max: &maxMTU},
			{Name: "dns", Label: "DNS 服务器", Type: "ip", List: true, Required: true},
			{Name: "mode", Label: "绑定模式", Type: "enum", Default: "active-backup",
				Options: []Option{{Value: "active-backup", Label: "主备"}, {Value: "802.3ad", Label: "LACP"}}},
		},
		TimeoutSec: 300, Required: true,
		Impls: []Impl{
			{Family: "linux", Series: []catalog.Series{"debian"}, Kind: ImplFragment,
				Body: "d-i netcfg/get_ipaddress string {{param:ip.addr}}\nd-i netcfg/get_netmask string {{param:ip.netmask}}\n"},
			{Family: "linux", Series: []catalog.Series{"rocky", "almalinux"}, Kind: ImplScript, Body: goldenScript,
				Interpreter: "sh", Timings: []catalog.Timing{"post-target", "firstboot"}},
		},
	}
}

func goldenTemplate() Template {
	minSwap, maxSwap := int64(0), int64(65536)
	timeout, required := 900, false
	return Template{
		ID: "rocky-base", Version: 4, Name: "Rocky-Base", State: VersionApproved,
		Author: "alice", Approver: "bob", DerivedFrom: &TemplateKey{ID: "builtin-kickstart", Version: 3},
		Family: "linux", Series: []catalog.Series{"rocky", "almalinux"}, Body: goldenBody,
		Params: []ParamDecl{
			{Name: "hostname", Label: "主机名", Type: "hostname"},
			{Name: "swap_mb", Label: "交换分区（MiB）", Type: "int", Default: "4096", Min: &minSwap, Max: &maxSwap},
		},
		Bindings: map[string]Binding{
			"hostname": {Source: BindingAttr, Attr: "hostname", Editable: true},
			"swap_mb":  {Source: BindingFixed, Value: "8192"},
		},
		Assembly: map[string][]Instance{
			"分区": {{HookID: "lvm", HookVersion: 1, Bindings: map[string]Binding{
				"disk": {Source: BindingDefault, Editable: true},
			}}},
			"装后配置": {{HookID: "nic", HookVersion: 2, TimeoutSec: &timeout, Required: &required, Bindings: map[string]Binding{
				"ip": {Source: BindingAttr, Attr: "ip"},
			}}},
		},
	}
}

func goldenSnapshot() Snapshot {
	return Snapshot{
		Schema: SnapshotSchema, Family: "linux", Series: "rocky", Format: "kickstart",
		Host: SnapshotHost{SN: "SN-0001", Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{
			"hostname": "web-01", "ip": "10.0.0.3/24",
		}},
		ISO: SnapshotISO{ID: "iso-rocky96", Name: "Rocky Linux 9.6", Version: "9.6", Arch: "x86_64",
			Artifacts: map[string]string{
				"initrd": "rocky/9.6/x86_64/images/pxeboot/initrd.img",
				"kernel": "rocky/9.6/x86_64/images/pxeboot/vmlinuz",
			}},
		Template: SnapshotTemplate{ID: "rocky-base", Version: 4, Name: "Rocky-Base", Body: goldenBody,
			Params: map[string]SnapshotValue{
				"hostname": {Type: "hostname", Items: []string{"web-01"}},
				"swap_mb":  {Type: "int", Items: []string{"8192"}},
			}},
		Slots: []SnapshotSlot{
			{Seq: 1, Name: "分区", Instances: []SnapshotInstance{{
				HookID: "lvm", HookVersion: 1, HookName: "LVM-Layout", Kind: ImplFragment,
				Body:   "part pv.01 --grow --ondisk={{param:disk}}\n",
				Params: map[string]SnapshotValue{"disk": {Type: "diskpath", Items: []string{"/dev/sda"}}},
			}}},
			{Seq: 2, Name: "装前检查", Exec: true, Timing: "pre", Instances: []SnapshotInstance{}},
			{Seq: 3, Name: "装后配置", Exec: true, Timing: "post-target", Instances: []SnapshotInstance{{
				HookID: "nic", HookVersion: 2, HookName: "Business-NIC", Kind: ImplScript, Body: goldenScript,
				Interpreter: "sh", TimeoutSec: 900, Required: false, Digest: Impl{Body: goldenScript}.Digest(),
				Params: map[string]SnapshotValue{
					"dns":  {Type: "ip", List: true, Items: []string{"10.0.0.53", "10.0.1.53"}},
					"ip":   {Type: "cidr", Items: []string{"10.0.0.3/24"}},
					"mode": {Type: "enum", Items: []string{"active-backup"}},
					"mtu":  {Type: "int", Items: []string{"1500"}},
				},
			}}},
		},
	}
}

func goldenTask() Task {
	return Task{
		ID: "t-20261002-0001", HostSN: "SN-0001", ISOID: "iso-rocky96", TemplateID: "rocky-base", TemplateVersion: 4,
		State: TaskInstalling, Snapshot: goldenSnapshot(), CreatedBy: "alice", CreatedAt: 1790000000,
	}
}
