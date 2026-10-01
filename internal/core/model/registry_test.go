package model

import (
	"slices"
	"strings"
	"testing"
)

// 属性值原样存、系统不解释（C7），值里有什么都不影响校验，只有键要守规则。
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

// 状态的字面量会存进数据库，发布后不能改名。
func TestISOState的字面量(t *testing.T) {
	got := []ISOState{ISOAcquiring, ISOImporting, ISOReady, ISOFailed, ISODeprecated}
	want := []ISOState{"acquiring", "importing", "ready", "failed", "deprecated"}
	if !slices.Equal(got, want) {
		t.Errorf("ISO 状态的字面量是 %q，应为 %q", got, want)
	}
}

// 只有导入完成的 ISO 能建任务（C5）；废弃的不能再被新引用（C26）。
func TestISOUsable只认可用状态(t *testing.T) {
	iso := ISO{
		ID:      "iso-1",
		Name:    "Windows Server 2025",
		Family:  "windows",
		Series:  "windows-server",
		Version: "2025",
		Arch:    "x86_64",
		SHA256:  strings.Repeat("ab", 32),
		Artifacts: map[string]string{
			"bootwim": "windows-server/2025/x86_64/sources/boot.wim",
		},
		Offers: map[string][]Option{
			"editions": {{Value: "ServerStandard", Label: "Standard"}, {Value: "ServerDatacenter", Label: "Datacenter"}},
		},
	}
	cases := map[ISOState]bool{
		"acquiring": false, "importing": false, "ready": true, "failed": false, "deprecated": false,
		"": false, "Ready": false, // 只认原样的写法
	}
	for state, want := range cases {
		iso.State = state
		if got := iso.Usable(); got != want {
			t.Errorf("状态为 %q 时 Usable() = %v，应为 %v", state, got, want)
		}
	}
}
