// 本文件实现 OS 家族识别：按特征文件判定 ISO 内容属于哪个家族。对应实现
// 计划任务 9，依据 SPEC §2.1。
package imports

import (
	"fmt"
	"io/fs"

	"github.com/lober/lober/internal/core/model"
)

// ErrUnsupportedOSFamily 是 Identify 识别出 Windows 或 ESXi 时返回的
// sentinel error（用 errors.Is 判定）——MVP 支持闸门：识别出但明确报
// 「不支持」，把制品集置失败，不要装作成功（任务 9）。理由：一个
// state=ready 但内容不完整的制品集会被建任务页列出来，运维选了它才发现
// 装不了。
//
// ErrOSFamilyNotIdentified 是没有任何特征文件匹配时返回的 sentinel
// error——识别不出来要明确失败，不要猜（任务 9）：猜错的后果是按错误的
// 家族抽取，产出一个结构不对的制品集，而那个错误要等机器引导失败才发现。
var (
	ErrUnsupportedOSFamily   = fmt.Errorf("imports: 识别出的 OS 家族在 MVP 阶段不支持")
	ErrOSFamilyNotIdentified = fmt.Errorf("imports: 未能识别出任何已知 OS 家族")
)

// Identify 按特征文件判定 root 对应的 ISO 内容属于哪个 OS 家族（SPEC
// §2.1 的特征文件表）：
//
//	RHEL 系   .treeinfo
//	Ubuntu    .disk/info
//	Debian    dists/
//	SUSE 系   repodata/repomd.xml + media.1/products（见下方实证说明）
//	ESXi      boot.cfg + mboot.efi
//	Windows   sources/install.wim
//
// 识别顺序要考虑歧义（任务 9 明确要求写进注释）：Ubuntu 的 ISO 也含
// dists/ 目录，所以 Ubuntu 的判定必须排在 Debian 之前——否则将来有人
// 「顺手整理」了判定顺序，就会把 Ubuntu 识别成 Debian。本函数按
// RHEL → Ubuntu → Debian → SUSE → ESXi → Windows 的固定顺序逐一检查
// 特征文件，这个顺序本身就是对歧义的处理，不能因为「看起来更整齐」而重排。
//
// SUSE 的判据与 SPEC §2.1 的表不同，理由已实证（2026-09-23）：
//
// SPEC 那张表里没有 SUSE 一行，而常见资料给的 content 文件判据**已经过时**。
// openSUSE 官方 linuxrc 文档明确写 content 文件识别法 "is obsolete since
// SLE15/openSUSE Leap 15, the newer products use the repomd repositories on
// the installation media"。对 download.opensuse.org 的实测印证了这一点：
// Leap 15.6 的 repo 下 repodata/repomd.xml 与 media.1/products 都是 200，
// 而 content 是 404。
//
// **单用 repodata/repomd.xml 不足以区分 SUSE 与现代 RHEL**——后者同样是
// RPM-MD 仓库、也有这个文件。所以 SUSE 的判据是 repomd.xml 与 media.1/products
// 的组合（后者是 SUSE 专属，RHEL/Debian/Ubuntu 都没有），且排在 RHEL 的
// .treeinfo 判据之后。只用 repomd.xml 且排在 RHEL 之前会把现代 RHEL 误判成
// SUSE，那会让 RHEL 机器拿到 AutoYaST 应答文件而安装器根本不认。
//
// 一块特征文件都不命中时返回 ErrOSFamilyNotIdentified。
//
// **六个家族现在全部返回 nil**：Windows 与 ESXi 曾经返回
// ErrUnsupportedOSFamily（MVP 支持闸门），两者的抽取器与渲染分派都已接线，
// job.go 的 Run 各有专门分派分支（runWindows / runESXi，它们的抽取函数比
// familyExtractors 的行类型多参数，所以不进那张查表）。
//
// 顺带删掉了 mvpUnsupportedFamilies 那张表：它定义了却**从没被任何代码
// 查询过**（Identify 的 if 分支一直是硬编码返回 sentinel error），是装饰性
// 死代码。这类「看着是开关、实际不起作用」的东西最危险——有人改了表以为
// 解封了，实际没有。ErrUnsupportedOSFamily 本身保留：将来真有不支持的
// 家族时仍然用它，但那时必须真的从 Identify 返回它，而不是往一张没人查的
// 表里加一行。
func Identify(root fs.FS) (model.OSFamily, error) {
	if exists(root, ".treeinfo") {
		return model.OSFamilyRHEL, nil
	}
	if exists(root, ".disk/info") {
		return model.OSFamilyUbuntu, nil
	}
	if existsDir(root, "dists") {
		return model.OSFamilyDebian, nil
	}
	// SUSE：必须两个条件同时成立（见函数注释的实证说明）。media.1/products
	// 是 SUSE 专属标记，用它把 SUSE 与同样含 repodata/ 的现代 RHEL 分开。
	if exists(root, "repodata/repomd.xml") && exists(root, "media.1/products") {
		return model.OSFamilySUSE, nil
	}
	if exists(root, "boot.cfg") && exists(root, "mboot.efi") {
		return model.OSFamilyESXi, nil
	}
	if exists(root, "sources/install.wim") {
		return model.OSFamilyWindows, nil
	}
	return "", ErrOSFamilyNotIdentified
}

// exists 判定 root 中是否存在名为 name 的普通文件（或至少可以被 Stat
// 到——不区分文件与目录，特征文件表里的多数条目本身就是文件，dists/ 这个
// 目录条目单独用 existsDir 处理以确保语义清晰）。
func exists(root fs.FS, name string) bool {
	_, err := fs.Stat(root, name)
	return err == nil
}

// existsDir 判定 root 中是否存在名为 name 的目录。
func existsDir(root fs.FS, name string) bool {
	info, err := fs.Stat(root, name)
	if err != nil {
		return false
	}
	return info.IsDir()
}
