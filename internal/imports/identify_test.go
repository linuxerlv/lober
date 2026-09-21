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
