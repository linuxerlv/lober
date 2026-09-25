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

// TestIdentify_Ubuntu 的判据是 casper/ 而不是 .disk/info。
//
// 夹具改过：`.disk/info` 是 Debian 的约定（Ubuntu 继承），两家共有，用它判
// Ubuntu 会把真实 Debian 镜像认成 Ubuntu——见
// TestIdentify_RealDebianNetinstIsNotUbuntu 里的实测依据。casper/ 是 Ubuntu
// 独有的 live 文件系统目录，也正是 ExtractUbuntu 需要的输入。
func TestIdentify_Ubuntu(t *testing.T) {
	root := fstest.MapFS{
		".disk/info":     &fstest.MapFile{Data: []byte("Ubuntu 22.04")},
		"casper/vmlinuz": &fstest.MapFile{Data: []byte("k")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("含 casper/ 应识别为 Ubuntu，不应报错: %v", err)
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

// TestIdentify_ESXi_NowSupported 锁住 ESXi 的 MVP 封锁已解除：
// Identify 对 boot.cfg + mboot.efi 返回 (esxi, nil)，不再是
// ErrUnsupportedOSFamily。
//
// ESXi 的抽取器（ExtractESXi，经 NewESXiFamilyExtractor 适配）与渲染分派
// 都已接线，job.go 的 Run 有专门的 runESXi 分支（它比 familyExtractors 的
// 行类型多一个 declaredFirmware 参数，所以不进那张查表）。
//
// 顺带记一笔：原先挡住 ESXi 的 mvpUnsupportedFamilies 表**从没被任何代码
// 查询过**，Identify 的 if 分支一直是硬编码返回 sentinel error —— 那张表是
// 装饰性死代码，已删除。这类「看着是开关、实际不起作用」的东西最危险：
// 有人改了表以为解封了，实际没有。
func TestIdentify_ESXi_NowSupported(t *testing.T) {
	root := fstest.MapFS{
		"boot.cfg":             &fstest.MapFile{Data: []byte("")},
		"efi/boot/bootx64.efi": &fstest.MapFile{Data: []byte("")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("ESXi 已解封，应返回 nil error，got %v", err)
	}
	if family != model.OSFamilyESXi {
		t.Fatalf("got %s, want esxi", family)
	}
}

// TestIdentify_Windows_NowSupported 锁住本轮解封：Windows 从
// mvpUnsupportedFamilies 移除后，Identify 对 sources/install.wim 特征文件
// 应返回 (windows, nil)——不再是 ErrUnsupportedOSFamily。这条测试替换了
// 旧版 TestIdentify_Windows_UnsupportedButIdentified（旧版断言的正是本轮
// 要解除的封锁行为，若保留会与新行为直接矛盾）。ESXi 的对应测试
// TestIdentify_ESXi_UnsupportedButIdentified 保持不变，不在本任务范围内。
func TestIdentify_Windows_NowSupported(t *testing.T) {
	root := fstest.MapFS{
		"sources/install.wim": &fstest.MapFile{Data: []byte("")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("Windows 已解除 MVP 封锁，含 sources/install.wim 应识别成功不报错，got err=%v", err)
	}
	if family != model.OSFamilyWindows {
		t.Fatalf("识别出的家族字段应为 windows，got %s", family)
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

// TestIdentify_AmbiguousUbuntuAndDebianResolvesToUbuntu 锁住 Ubuntu 与 Debian
// 之间的歧义处理：Ubuntu 的 ISO 也含 dists/ 目录，Ubuntu 的判定必须排在
// Debian 之前，否则 Ubuntu 会被误判为 Debian。
//
// **本测试的夹具改过一次，因为它原先的前提是错的。** 它原本用
// `.disk/info + dists/` 这一组作为「歧义」的代表，断言结果是 Ubuntu——而那条
// 断言把一个 bug 锁成了期望行为：`.disk/info` 原本是 Debian 的约定（Ubuntu 只是
// 继承），**真实 Debian netinst 同样有这个文件**，于是每一份真实 Debian 镜像都被
// 认成 Ubuntu，接着 ExtractUbuntu 去找 casper/vmlinuz 而失败。
//
// 实测 debian-13.7.0-amd64-netinst.iso 的根目录就是 `.disk` 与 `dists` 并存
// （见 TestIdentify_RealDebianNetinstIsNotUbuntu）。
//
// 因此判据换成 casper/ —— 那才是 Ubuntu 独有、且正是 ExtractUbuntu 需要的输入。
// 本测试保留它真正该锁的那件事：**casper/ 与 dists/ 并存时必须是 Ubuntu**
// （Ubuntu live-server 带一份离线安装用的 pool，因此确实两者都有）。
func TestIdentify_AmbiguousUbuntuAndDebianResolvesToUbuntu(t *testing.T) {
	root := fstest.MapFS{
		".disk/info":           &fstest.MapFile{Data: []byte("Ubuntu 22.04")},
		"casper/vmlinuz":       &fstest.MapFile{Data: []byte("k")},
		"dists/stable/Release": &fstest.MapFile{Data: []byte("")},
	}
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if family != model.OSFamilyUbuntu {
		t.Fatalf("同时含 casper/ 与 dists/ 时必须识别为 Ubuntu，got %s——"+
			"Ubuntu live-server 两者都有，casper 的判定必须排在 dists 之前", family)
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

// TestIdentify_RealDebianNetinstIsNotUbuntu 验证真实 Debian netinst 的根目录
// 布局被识别为 debian，不是 ubuntu。
//
// **这是真实 ISO 上的一个阻断性 bug。** `.disk/info` 本来就是 Debian 的约定
// （Ubuntu 是继承来的），因此**真实 Debian ISO 同样有这个文件**；而判定顺序把
// `.disk/info → ubuntu` 排在 `dists/ → debian` 之前，于是每一份真实 Debian
// 镜像都被认成 Ubuntu，接着 ExtractUbuntu 去找 casper/vmlinuz 而失败：
//
//	在 ISO 内容的 casper/ 目录下找不到 "vmlinuz"（期望路径 "casper/vmlinuz"）
//
// 它此前没被发现，是因为夹具被塑造成绕开了这个 bug——scripts/e2e/
// mkfixture-debian.sh 的注释原话：「注意 Ubuntu 的 .disk/info 判定排在 Debian
// 之前，本脚本不造 .disk/info，避免被误判为 Ubuntu」。**夹具知道这个 bug 存在，
// 却是绕着它走而不是让它红。**
//
// 下面的根目录项照抄真实 debian-13.7.0-amd64-netinst.iso 的实测结果。
func TestIdentify_RealDebianNetinstIsNotUbuntu(t *testing.T) {
	root := fstest.MapFS{
		".disk/info":                &fstest.MapFile{Data: []byte("Debian GNU/Linux 13.7.0 \"Trixie\" - Official amd64 NETINST")},
		"dists/trixie/Release":      &fstest.MapFile{Data: []byte("Suite: stable\n")},
		"install.amd/vmlinuz":       &fstest.MapFile{Data: []byte("k")},
		"install.amd/initrd.gz":     &fstest.MapFile{Data: []byte("i")},
		"pool/main/b/bash/bash.deb": &fstest.MapFile{Data: []byte("deb")},
		"isolinux/isolinux.cfg":     &fstest.MapFile{Data: []byte("cfg")},
	}

	got, err := Identify(root)
	if err != nil {
		t.Fatalf("Identify 失败: %v", err)
	}
	if got != model.OSFamilyDebian {
		t.Errorf("Identify(真实 Debian netinst 布局) = %q, want %q —— "+
			".disk/info 是 Debian 自己的约定，Ubuntu 只是继承；用它判 Ubuntu 会把"+
			"每一份真实 Debian 镜像都认成 Ubuntu，接着 ExtractUbuntu 去找 "+
			"casper/vmlinuz 失败，装机链路第一步就断", got, model.OSFamilyDebian)
	}
}

// TestIdentify_RealUbuntuLiveServerIsUbuntu 是上一条的对照组：真实 Ubuntu
// live-server 的布局仍然识别为 ubuntu。
//
// 关键差异是 casper/ —— Ubuntu 的 live 文件系统在那里，而 ExtractUbuntu 要的
// 正是 casper/vmlinuz 与 casper/*.squashfs。**用 casper/ 判 Ubuntu 让「识别出
// 这个家族」蕴含「抽取器能抽出东西」**，比 .disk/info 那种两家共有的标记可靠。
//
// 注意 Ubuntu live-server 同样有 dists/ 与 pool/（它带一份离线安装用的 pool），
// 因此 casper 的判定必须排在 dists 之前，否则 Ubuntu 会被认成 Debian——这条
// 断言正是锁住那个顺序。
func TestIdentify_RealUbuntuLiveServerIsUbuntu(t *testing.T) {
	root := fstest.MapFS{
		".disk/info":                    &fstest.MapFile{Data: []byte("Ubuntu-Server 24.04.1 LTS \"Noble Numbat\" - Release amd64")},
		"casper/vmlinuz":                &fstest.MapFile{Data: []byte("k")},
		"casper/initrd":                 &fstest.MapFile{Data: []byte("i")},
		"casper/ubuntu-server.squashfs": &fstest.MapFile{Data: []byte("sq")},
		"dists/noble/Release":           &fstest.MapFile{Data: []byte("Suite: noble\n")},
		"pool/main/b/bash/bash.deb":     &fstest.MapFile{Data: []byte("deb")},
	}

	got, err := Identify(root)
	if err != nil {
		t.Fatalf("Identify 失败: %v", err)
	}
	if got != model.OSFamilyUbuntu {
		t.Errorf("Identify(真实 Ubuntu live-server 布局) = %q, want %q —— "+
			"Ubuntu live-server 同样带 dists/ 与 pool/，因此 casper/ 的判定必须排在"+
			"dists/ 之前", got, model.OSFamilyUbuntu)
	}
}
