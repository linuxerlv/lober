package media_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	media "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
)

const minimalTreeinfo = `[general]
arch = x86_64
family = AlmaLinux
name = AlmaLinux 10
packagedir = Minimal/Packages
repository = Minimal
variant = Minimal
variants = Minimal
version = 10
[release]
name = AlmaLinux
short = AlmaLinux
version = 10
[tree]
arch = x86_64
variants = Minimal
[variant-Minimal]
id = Minimal
name = Minimal
packages = Minimal/Packages
repository = Minimal
type = variant
uid = Minimal
`

func TestMinimalKickstartRepository(t *testing.T) {
	rocky := strings.ReplaceAll(minimalTreeinfo, "AlmaLinux", "Rocky Linux Minimal")
	rocky = strings.ReplaceAll(rocky, "short = Rocky Linux Minimal", "short = Rocky-minimal")
	rocky = strings.ReplaceAll(rocky, "10", "10.2")
	for _, tc := range []struct {
		name    string
		series  catalog.Series
		text    string
		matched bool
		invalid bool
	}{
		{"Alma 原布局", "almalinux", minimalTreeinfo, true, false},
		{"Rocky 原布局", "rocky", rocky, true, false},
		{"其它系列不扩展", "rhel", minimalTreeinfo, false, false},
		{"仅注释和名称", "almalinux", "[general]\nname=AlmaLinux Minimal\n; variants=Minimal\n# [variant-Minimal]\n", false, false},
		{"原完整盘", "rocky", "[general]\nname=Rocky Linux\npackagedir=media/Packages\n", false, false},
		{"只有分组段", "almalinux", "[variant-Minimal]\nid=Minimal\n", true, true},
		{"只有兼容声明", "almalinux", "[general]\nvariant=Minimal\n", true, true},
		{"只有仓库路径不决定分组", "almalinux", "[general]\nrepository=Minimal\n", false, false},
		{"带引号的声明不能回退", "almalinux", "[general]\nvariants=\"Minimal\"\n", true, true},
		{"危险分组段不能回退", "almalinux", "[variant-Minimal/../BaseOS]\nid=BaseOS\n", true, true},
		{"缺分组段", "almalinux", strings.Split(minimalTreeinfo, "[variant-Minimal]")[0], true, true},
		{"缺清单", "almalinux", strings.ReplaceAll(minimalTreeinfo, "variants = Minimal\n", ""), true, true},
		{"冲突重复", "almalinux", minimalTreeinfo + "repository = BaseOS\n", true, true},
		{"相同重复也拒绝", "almalinux", minimalTreeinfo + "repository = Minimal\n", true, true},
		{"重复分组段", "almalinux", minimalTreeinfo + "[variant-Minimal]\nid = Minimal\n", true, true},
		{"危险重复不能被覆盖", "almalinux", strings.Replace(minimalTreeinfo, "repository = Minimal", "repository = ../escape\nrepository = Minimal", 1), true, true},
		{"混合清单", "almalinux", strings.Replace(minimalTreeinfo, "variants = Minimal", "variants = Minimal,BaseOS", 1), true, true},
		{"混合分组", "almalinux", minimalTreeinfo + "[variant-BaseOS]\nrepository=BaseOS\n", true, true},
		{"越界路径", "almalinux", strings.ReplaceAll(minimalTreeinfo, "repository = Minimal", "repository = ../Minimal"), true, true},
		{"绝对路径", "almalinux", strings.ReplaceAll(minimalTreeinfo, "repository = Minimal", "repository = /Minimal"), true, true},
		{"网址路径", "almalinux", strings.ReplaceAll(minimalTreeinfo, "repository = Minimal", "repository = https://example.invalid/Minimal"), true, true},
		{"包目录冲突", "almalinux", strings.Replace(minimalTreeinfo, "packages = Minimal/Packages", "packages = BaseOS/Packages", 1), true, true},
		{"分组ID冲突", "almalinux", strings.Replace(minimalTreeinfo, "id = Minimal", "id = BaseOS", 1), true, true},
		{"分组段大小写冲突", "almalinux", strings.Replace(minimalTreeinfo, "[variant-Minimal]", "[variant-minimal]", 1), true, true},
		{"架构冲突", "almalinux", strings.Replace(minimalTreeinfo, "arch = x86_64", "arch = aarch64", 1), true, true},
		{"未知架构", "almalinux", strings.ReplaceAll(minimalTreeinfo, "x86_64", "unknown"), true, true},
		{"版本冲突", "almalinux", strings.Replace(minimalTreeinfo, "version = 10", "version = 10.2", 1), true, true},
		{"系列冲突", "rocky", minimalTreeinfo, true, true},
		{"缺身份", "almalinux", strings.Replace(minimalTreeinfo, "family = AlmaLinux\n", "", 1), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layout, matched, err := media.MinimalKickstartRepository(tc.series, tc.text)
			if matched != tc.matched || (err != nil) != tc.invalid {
				t.Fatalf("matched=%v err=%v，期望 matched=%v invalid=%v", matched, err, tc.matched, tc.invalid)
			}
			if tc.invalid {
				var p model.Problem
				if !errors.As(err, &p) || p.Code != "iso_repo_incomplete" || p.Subject.Kind != "iso" || p.Subject.Path != "artifacts.repo" {
					t.Fatalf("错误分类改变：%v", err)
				}
			}
			want := media.RepositoryLayout{}
			if matched && err == nil {
				want = media.RepositoryLayout{Root: ".", RepoPaths: []string{"Minimal/repodata/repomd.xml"}}
			}
			if !reflect.DeepEqual(layout, want) {
				t.Fatalf("布局=%+v，期望%+v", layout, want)
			}
		})
	}
}

func TestMinimalKickstartRepositoryFullPathsDoNotDeclareMinimal(t *testing.T) {
	for _, series := range []catalog.Series{"almalinux", "rocky"} {
		for _, root := range []string{"media/Minimal", "Minimal"} {
			for _, fields := range []string{
				"[general]\npackagedir=" + root + "/Packages\n",
				"[general]\nrepository=" + root + "/BaseOS\n",
				"[general]\nvariants=BaseOS,AppStream\n[tree]\nvariants=BaseOS,AppStream\n" +
					"[variant-BaseOS]\nid=BaseOS\nrepository=" + root + "/BaseOS\npackages=" + root + "/BaseOS/Packages\n" +
					"[variant-AppStream]\nid=AppStream\nrepository=" + root + "/AppStream\npackages=" + root + "/AppStream/Packages\n",
			} {
				layout, matched, err := media.MinimalKickstartRepository(series, fields)
				if matched || err != nil || !reflect.DeepEqual(layout, media.RepositoryLayout{}) {
					t.Fatalf("%s 的普通完整盘路径被当成 Minimal 分组：输入=%q 布局=%+v matched=%v err=%v", series, fields, layout, matched, err)
				}
			}
		}
	}
}
