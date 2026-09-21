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

// mvpUnsupportedFamilies 是识别成功但 MVP 明确不支持的家族集合（任务 9：
// Windows 与 ESXi 被识别但返回「MVP 不支持」）。
var mvpUnsupportedFamilies = map[model.OSFamily]bool{
	model.OSFamilyWindows: true,
	model.OSFamilyESXi:    true,
}

// Identify 按特征文件判定 root 对应的 ISO 内容属于哪个 OS 家族（SPEC
// §2.1 的特征文件表）：
//
//	RHEL 系   .treeinfo
//	Ubuntu    .disk/info
//	Debian    dists/
//	ESXi      boot.cfg + mboot.efi
//	Windows   sources/install.wim
//
// 识别顺序要考虑歧义（任务 9 明确要求写进注释）：Ubuntu 的 ISO 也含
// dists/ 目录，所以 Ubuntu 的判定必须排在 Debian 之前——否则将来有人
// 「顺手整理」了判定顺序，就会把 Ubuntu 识别成 Debian。本函数按
// RHEL → Ubuntu → Debian → ESXi → Windows 的固定顺序逐一检查特征文件，
// 这个顺序本身就是对歧义的处理，不能因为「看起来更整齐」而重排。
//
// 一块特征文件都不命中时返回 ErrOSFamilyNotIdentified；命中 Windows 或
// ESXi 时返回 (该家族, ErrUnsupportedOSFamily)——注意这种情况下返回值
// 的第一个字段不是零值，调用方可以从中知道「识别出了什么，只是不支持」，
// 而不是把这类情况与「完全没识别出来」混为一谈。
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
	if exists(root, "boot.cfg") && exists(root, "mboot.efi") {
		return model.OSFamilyESXi, ErrUnsupportedOSFamily
	}
	if exists(root, "sources/install.wim") {
		return model.OSFamilyWindows, ErrUnsupportedOSFamily
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
