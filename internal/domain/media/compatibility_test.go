package media

import (
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"testing"
)

func Test网络介质按官方构建绑定小版本(t *testing.T) {
	for _, tc := range []struct{ text, version string }{
		{"Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso 20250803+deb13u7", "13.7.0"},
		{"Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso 20250803", "13.0.0"},
		{"Debian GNU/Linux 13.7.0 arm64 - netboot mini.iso 20260907", "13.7.0"},
		{"Debian GNU/Linux 13 (trixie) amd64 - netboot mini.iso 20260907", ""},
		{"Debian GNU/Linux 13.7.0 amd64 - netboot mini.iso 20250803+deb13u6", ""},
	} {
		got, err := Netboot(tc.text)
		if tc.version == "" {
			if err == nil {
				t.Fatal("未证实的构建被接受", got)
			}
		} else if err != nil || got.Version != tc.version {
			t.Fatal(got, err)
		}
	}
}
func Test镜像额外源须同系同架构且按要求同版本(t *testing.T) {
	main := Identified{Series: "debian", Version: "13.7.0", Arch: "x86_64"}
	for _, tc := range []struct {
		extra      Identified
		text       string
		same, want bool
	}{
		{main, "netboot", true, true},
		{Identified{Series: "debian", Version: "13.6.0", Arch: "x86_64"}, "NETBOOT", true, false},
		{Identified{Series: "debian", Version: "13.6.0", Arch: "x86_64"}, "NETBOOT", false, true},
		{Identified{Series: "debian", Version: "13.7.0", Arch: "aarch64"}, "NETBOOT", true, false},
		{main, "NETINST", true, false},
	} {
		if got := CheckExtra(main, tc.extra, tc.same, tc.text); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
func Test镜像版本列表拒绝混合版本架构和不安全标签(t *testing.T) {
	base := EditionFact{Name: "Windows Pro", Flags: "Professional", Version: "10.0", Arch: "x86_64"}
	for _, tc := range []struct {
		fact EditionFact
		path string
	}{
		{EditionFact{Name: base.Name, Flags: base.Flags, Version: "11.0", Arch: base.Arch}, "version"},
		{EditionFact{Name: base.Name, Flags: base.Flags, Version: base.Version, Arch: "aarch64"}, "arch"},
		{EditionFact{Name: base.Name, Flags: "bad flag", Version: base.Version, Arch: base.Arch}, "offers/edition"},
	} {
		_, err := CheckEditions(Identified{}, []EditionFact{base, tc.fact})
		var p model.Problem
		if !errors.As(err, &p) || p.Subject.Path != tc.path {
			t.Fatal(err)
		}
	}
	got, err := CheckEditions(Identified{}, []EditionFact{base, base})
	if err != nil || len(got.Editions) != 1 || got.Editions[0] != (model.Option{Value: "Professional", Label: "Windows Pro"}) {
		t.Fatal(got, err)
	}
}
func Test镜像导入先报系不匹配再报额外源缺失(t *testing.T) {
	item := model.ISO{ID: "debian", Series: "debian"}
	for _, tc := range []struct {
		found Identified
		code  model.Code
	}{
		{Identified{Series: "ubuntu"}, "iso_series_mismatch"},
		{Identified{Series: "debian"}, "iso_extra_source_invalid"},
	} {
		var p model.Problem
		if err := CheckImport(item, tc.found, nil); !errors.As(err, &p) || p.Code != tc.code || p.Subject.ID != item.ID {
			t.Fatal(err)
		}
	}
}
