package imports

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/model"
)

func TestIdentify_RHEL(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte("[general]\n")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("含 .treeinfo 应识别为 RHEL，不应报错: %v", err)
	}
	if family != model.OSFamilyRHEL {
		t.Fatalf("got %s, want rhel", family)
	}
}

func TestIdentify_Ubuntu(t *testing.T) {
	root := fstest.MapFS{
		".disk/info": &fstest.MapFile{Data: []byte("Ubuntu 22.04")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("含 .disk/info 应识别为 Ubuntu，不应报错: %v", err)
	}
	if family != model.OSFamilyUbuntu {
		t.Fatalf("got %s, want ubuntu", family)
	}
}

func TestIdentify_Debian(t *testing.T) {
	root := fstest.MapFS{
		"dists/stable/Release": &fstest.MapFile{Data: []byte("")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("含 dists/ 目录应识别为 Debian，不应报错: %v", err)
	}
	if family != model.OSFamilyDebian {
		t.Fatalf("got %s, want debian", family)
	}
}

func TestIdentify_ESXi_UnsupportedButIdentified(t *testing.T) {
	root := fstest.MapFS{
		"boot.cfg":  &fstest.MapFile{Data: []byte("")},
		"mboot.efi": &fstest.MapFile{Data: []byte("")},
	}
	family, err := Identify(root)
	if !errors.Is(err, ErrUnsupportedOSFamily) {
		t.Fatalf("ESXi 应被识别但报 MVP 不支持，got err=%v", err)
	}
	if family != model.OSFamilyESXi {
		t.Fatalf("即使不支持，识别出的家族字段也应该是 esxi，got %s", family)
	}
}

func TestIdentify_Windows_UnsupportedButIdentified(t *testing.T) {
	root := fstest.MapFS{
		"sources/install.wim": &fstest.MapFile{Data: []byte("")},
	}
	family, err := Identify(root)
	if !errors.Is(err, ErrUnsupportedOSFamily) {
		t.Fatalf("Windows 应被识别但报 MVP 不支持，got err=%v", err)
	}
	if family != model.OSFamilyWindows {
		t.Fatalf("即使不支持，识别出的家族字段也应该是 windows，got %s", family)
	}
}

func TestIdentify_NoFeatureFileReturnsError(t *testing.T) {
	root := fstest.MapFS{
		"README.txt": &fstest.MapFile{Data: []byte("just a readme")},
	}
	_, err := Identify(root)
	if !errors.Is(err, ErrOSFamilyNotIdentified) {
		t.Fatalf("无特征文件时应返回 ErrOSFamilyNotIdentified，got %v——"+
			"识别不出来要明确失败，不要猜，猜错的后果是产出结构不对的制品集", err)
	}
}

// TestIdentify_AmbiguousUbuntuAndDebianResolvesToUbuntu 锁住任务 9 明确要求
// 的歧义处理：Ubuntu 的 ISO 也含 dists/ 目录，Ubuntu 的判定必须排在
// Debian 之前，否则会被误判为 Debian。
func TestIdentify_AmbiguousUbuntuAndDebianResolvesToUbuntu(t *testing.T) {
	root := fstest.MapFS{
		".disk/info":           &fstest.MapFile{Data: []byte("Ubuntu 22.04")},
		"dists/stable/Release": &fstest.MapFile{Data: []byte("")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if family != model.OSFamilyUbuntu {
		t.Fatalf("同时含 dists/ 与 .disk/info 时必须识别为 Ubuntu，got %s——"+
			"若识别成 debian，说明判定顺序被意外调整了，这是计划任务 9 明确警告的"+
			"「顺手整理」类回归", family)
	}
}

// TestIdentify_SUSE 锁住 SUSE 的识别判据：repodata/repomd.xml 与
// media.1/products 两个条件的组合（见 Identify 注释里的实证说明）。
func TestIdentify_SUSE(t *testing.T) {
	root := fstest.MapFS{
		"repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
		"media.1/products":    &fstest.MapFile{Data: []byte("/ openSUSE-Leap 15.6-0")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("含 repomd.xml + media.1/products 应识别为 SUSE，不应报错: %v", err)
	}
	if family != model.OSFamilySUSE {
		t.Fatalf("got %s, want suse", family)
	}
}

// TestIdentify_ModernRHELWithRepodataIsNotMistakenForSUSE 是本轮加 SUSE
// 判据时最重要的一条防线。
//
// 现代 RHEL/Rocky/AlmaLinux 同样是 RPM-MD 仓库、同样含 repodata/repomd.xml。
// 若 SUSE 判据只看 repomd.xml，或排在 RHEL 的 .treeinfo 判据之前，这类 ISO
// 就会被识别成 SUSE —— 后果是 RHEL 机器拿到 AutoYaST 应答文件，而 anaconda
// 根本不认那个格式（SPEC 反复警示两者语法完全不同，不能因为都被叫「ks」就混用）。
//
// 本用例造一棵「同时含 .treeinfo 与 repodata/repomd.xml 但没有 media.1/products」
// 的树，断言它仍被识别为 RHEL。
func TestIdentify_ModernRHELWithRepodataIsNotMistakenForSUSE(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo":           &fstest.MapFile{Data: []byte("[general]\n")},
		"repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("现代 RHEL 树应识别为 RHEL，不应报错: %v", err)
	}
	if family != model.OSFamilyRHEL {
		t.Fatalf("got %s, want rhel —— 现代 RHEL 含 repodata/ 但不是 SUSE；"+
			"误判会让 RHEL 机器拿到 AutoYaST 应答文件，anaconda 不认那个格式", family)
	}
}

// TestIdentify_RepodataWithoutSUSEMarkerIsNotIdentifiedAsSUSE 补上另一半：
// 只有 repomd.xml、既无 .treeinfo 也无 media.1/products 的树（比如一个裸的
// 第三方 RPM 仓库镜像）不应被当成 SUSE —— 宁可报「识别不出」让运维检查，
// 也不要猜一个家族然后渲染出安装器不认的应答文件。
func TestIdentify_RepodataWithoutSUSEMarkerIsNotIdentifiedAsSUSE(t *testing.T) {
	root := fstest.MapFS{
		"repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
	}
	if family, err := Identify(root); err == nil {
		t.Fatalf("只有 repomd.xml 没有 media.1/products 时应识别不出，got family=%s err=nil", family)
	}
}
