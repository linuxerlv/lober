// 本文件实现 ESXi 制品集的抽取（对应实现计划
// docs/superpowers/plans/2026-09-20-lober-phase3-02-esxi.md 任务 1，依据
// SPEC §0.3、§2.1、§4.6 第三项）。
//
// ESXi 的 ISO 结构与三个 Linux 家族都不同：没有 .treeinfo/.disk/info/
// dists/ 这类"内核 + initrd + 仓库树"的结构，取而代之的是一个引导器
// （mboot.efi）、一份引导配置文件（boot.cfg）、加几十个按 boot.cfg 的
// modules= 行列出的模块文件。抽取的产出因此与 ExtractRHEL（manifest.go）
// 完全不同：不产出 Kernel/Initrd/Repo，只产出 Extra 里的一组文件。
package imports

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// esxiMbootEFIName / esxiBootCfgName 是 ESXi 制品集的两个固定识别特征
// 文件名（与 identify.go 的 Identify 函数使用的同一对特征文件一致，SPEC
// §2.1 的特征文件表：ESXi 用 boot.cfg + mboot.efi）。
const (
	// esxiMbootEFIName 是 UEFI 引导器的**落盘名**，也是 SPEC §0.3/§4.5 与
	// 渲染器菜单形态使用的名字。
	esxiMbootEFIName = "mboot.efi"

	// esxiUEFILoaderSrcPath 是 UEFI 引导器在 ISO 内的**真实路径**。
	//
	// **真实 ESXi ISO 里没有 mboot.efi。** UEFI 引导器按 UEFI 规范的可移动介质
	// 默认路径放在 EFI/BOOT/BOOTX64.EFI（固件自动发现那个路径）；根目录的
	// mboot.c32 是 BIOS 路径的 syslinux COMBOOT 模块，与 UEFI 无关。
	//
	// SPEC 写的「mboot.efi」是 **PXE 部署时的惯例名字**——VMware 的 PXE 文档
	// 让运维把 bootx64.efi 复制出来用那个名字。因此落盘名保持 mboot.efi
	// （渲染器与菜单形态一个字都不用改），只有查找路径按真实 ISO 来。
	//
	// 与 Debian 那个「ISO 内叫 vmlinuz、落盘叫 linux」是同一个模式：查找名与
	// 落盘名分开，两者都不是随便选的。实测依据见
	// TestExtractESXi_TakesUEFILoaderFromEFIBoot。
	esxiUEFILoaderSrcPath = "efi/boot/bootx64.efi"

	esxiBootCfgName = "boot.cfg"

	// esxiBootCfgKernelKey 是 boot.cfg 里内核那一行的键名。
	//
	// 它**独立于 modules=**：实测真实 boot.cfg 是 `kernel=/b.b00`，而 modules=
	// 那一行从 /jumpstrt.gz 开始、整行不含 b.b00。此前只抽 modules 会漏掉内核，
	// 后果是引导器取内核时 404——而那时机器已经上电了。
	esxiBootCfgKernelKey = "kernel"

	// esxiBootCfgModulesKey / esxiModulesSeparator 是 boot.cfg 里
	// modules= 行的键名与分隔符。与 internal/render/esxi.go 里同名
	// 常量的值完全一致（真实 ESXi boot.cfg 格式本身固定如此），但两个
	// 包故意各自定义一份、不共享——internal/imports 与 internal/render
	// 是两个互相不可见的实现模块（archtest 依赖方向守卫拦住任何跨模块
	// import，唯一豁免是 cmd/lober 装配点），本文件只是导入期的搬运，
	// 不依赖渲染器的任何逻辑，重复这两个字面量常量比引入一个跨模块
	// 依赖更符合"实现模块互不 import"的架构约束。
	esxiBootCfgModulesKey = "modules"
	esxiModulesSeparator  = " --- "
)

// ExtractESXi 执行 ESXi 制品集的抽取（计划任务 1）：
//  1. 校验 declaredFirmware 必须是 UEFI——ESXi 固件字段固定为 UEFI，
//     导入时不接受其它值（若有人试图把 ESXi 制品集标为 BIOS，直接拒绝，
//     理由是 MVP 与 Phase 3 的范围决定，见 SPEC §0.3、附录 A.14）。
//  2. 读取原始 boot.cfg，解析出 modules= 行列出的模块相对路径列表——
//     解析时不改写路径本身，抽取阶段只搬运文件，不做任何渲染期的三处
//     改写（那是 RenderESXiBootCfg 的职责，见 esxi.go）。
//  3. 校验 mboot.efi、boot.cfg 与全部模块文件都能在 root 中找到——
//     完整性校验要覆盖全部模块文件（计划任务 1 明确要求，理由：缺一个
//     模块的表现是"ESXi 引导到某一步停住"，错误信息不会指出缺哪个文件，
//     所以导入时的清单校验比 Linux 侧更重要）。
//  4. 把 mboot.efi、boot.cfg（原始未改写版本）与全部模块文件复制到
//     destDir 下，保持 boot.cfg 中列出的顺序不重排（顺序是引导要求）。
//  5. 返回 model.ArtifactPaths，Kernel/Initrd/Repo 三个字段留空（ESXi
//     不用这套结构），全部文件路径落在 Extra 里，键名与磁盘上的相对
//     文件名一致。
//
// root 是 Identify 确认过是 ESXi 家族之后的 ISO 内容视图；destDir 是制品
// 集在磁盘上的根目录。declaredFirmware 是运维登记 artifact_set 时填写的
// 固件值——契约缺口：contract.ArtifactSetStore.SetArtifactSetContent 的
// 签名只接受 (osFamily, paths) 两个参数，没有第三个"固件"参数可传，本函数
// 的调用方（job.go，不在本次分配范围内）暂时只能在调用本函数之前另行获知
// 这个值并传入；详见文件末尾 contractGaps 说明。
func ExtractESXi(root fs.FS, destDir string, declaredFirmware model.FirmwareMode) (model.ArtifactPaths, error) {
	if declaredFirmware != model.FirmwareUEFI {
		return model.ArtifactPaths{}, fmt.Errorf(
			"imports: ESXi 制品集的固件字段只接受 uefi，收到 %q——"+
				"传统 BIOS 路径依赖 syslinux 的 mboot.c32 加 iPXE 的 COMBOOT 支持，"+
				"MVP 与 Phase 3 均明确排除（SPEC §0.3、附录 A.14），"+
				"不接受把 ESXi 制品集标为其它固件值", declaredFirmware)
	}

	bootCfgData, err := fs.ReadFile(root, esxiBootCfgName)
	if err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 读取 %s 失败: %w", esxiBootCfgName, err)
	}

	modules, err := parseESXiBootCfgModules(string(bootCfgData))
	if err != nil {
		return model.ArtifactPaths{}, err
	}

	// kernel= 是 boot.cfg 里独立于 modules= 的一行，必须单独取。
	//
	// 漏掉它的后果：引导器去取 kernel= 指向的文件时 404，而那时机器已经上电、
	// 已经走到引导器——正是本函数注释说的「引导到某一步会停住且错误信息不会
	// 指出缺哪个文件」。实测真实 boot.cfg 是 `kernel=/b.b00`，而 modules= 那行
	// 从 /jumpstrt.gz 开始、不含 b.b00。
	kernelRel, err := parseESXiBootCfgKernel(string(bootCfgData))
	if err != nil {
		return model.ArtifactPaths{}, err
	}

	// copies 是「ISO 内源路径 → 落盘相对路径」的搬运清单。
	//
	// 用两列而不是一个路径列表：UEFI 引导器的源路径（efi/boot/bootx64.efi）与
	// 落盘名（mboot.efi）**刻意不同**，见 esxiUEFILoaderSrcPath 的文档。其余
	// 文件两者相同。
	type copyItem struct{ src, dest string }
	copies := make([]copyItem, 0, len(modules)+3)
	copies = append(copies,
		copyItem{src: esxiUEFILoaderSrcPath, dest: esxiMbootEFIName},
		copyItem{src: esxiBootCfgName, dest: esxiBootCfgName},
		copyItem{src: kernelRel, dest: kernelRel},
	)
	for _, m := range modules {
		copies = append(copies, copyItem{src: m, dest: m})
	}

	// 完整性校验：引导器、boot.cfg、内核与全部模块文件都必须能在 root 中找到，
	// 缺一个都整体失败并指出具体是哪一个（计划任务 1 明确要求，不同于"缺了就
	// 跳过"——跳过会让引导到那一步才失败，且错误信息不会指出缺哪个文件）。
	for _, it := range copies {
		if _, err := fs.Stat(root, it.src); err != nil {
			return model.ArtifactPaths{}, fmt.Errorf(
				"imports: ESXi 制品集缺少文件 %q（%w）——它由 boot.cfg 的 kernel=/modules= 行"+
					"或 UEFI 引导器的固定路径指定，但 ISO 中找不到；ESXi 引导到某一步会停住"+
					"且错误信息不会指出缺哪个文件，因此在导入阶段就要发现并拒绝", it.src, err)
		}
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 创建制品目录 %q 失败: %w", destDir, err)
	}

	extra := make(map[string]string, len(copies))
	for _, it := range copies {
		destPath := filepath.Join(destDir, filepath.FromSlash(it.dest))
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return model.ArtifactPaths{}, fmt.Errorf("imports: 创建目录 %q 失败: %w", filepath.Dir(destPath), err)
		}
		if err := copyFileFromFS(root, it.src, destPath); err != nil {
			return model.ArtifactPaths{}, fmt.Errorf("imports: 复制文件 %q 失败: %w", it.src, err)
		}
		extra[it.dest] = it.dest
	}

	return model.ArtifactPaths{Extra: extra}, nil
}

// parseESXiBootCfgKernel 从 boot.cfg 里取 kernel= 那一行指向的文件路径。
//
// 返回值去掉前导斜杠（boot.cfg 里写的是 ISO 内绝对路径形式 `/b.b00`，而
// fs.FS 的路径不接受前导斜杠）。缺 kernel= 行时报错而不是跳过：没有内核的
// ESXi 制品集引导不起来，那是导入期就该拒绝的。
func parseESXiBootCfgKernel(text string) (string, error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, esxiBootCfgKernelKey+"=") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, esxiBootCfgKernelKey+"="))
		v = strings.TrimPrefix(v, "/")
		if v == "" {
			return "", fmt.Errorf("imports: boot.cfg 的 %s= 行为空——"+
				"没有内核的 ESXi 制品集引导不起来", esxiBootCfgKernelKey)
		}
		return v, nil
	}
	return "", fmt.Errorf("imports: 原始 boot.cfg 中没有找到 %s= 行，"+
		"这不是一份可用的 ESXi 引导配置", esxiBootCfgKernelKey)
}

// parseESXiBootCfgModules 从原始 boot.cfg 文本中解析出 modules= 行列出的
// 模块相对路径列表，顺序与原文一致（不重排——顺序是引导要求，计划任务 1
// 明确警示）。
//
// 真实 ESXi boot.cfg 的 modules= 值格式是以 " --- " 分隔的路径列表，
// 每个路径可能带前导斜杠（ISO 内的绝对路径写法）。本函数保留原始路径
// 形态（含前导斜杠），不做任何改写——路径改写是渲染期
// RenderESXiBootCfg 的职责（esxi.go），抽取期只用这些路径去 ISO 里找到
// 对应文件并复制，找文件时前导斜杠不影响 fs.FS 的 Stat/Open（本函数在
// 返回前会统一去掉前导斜杠，因为 io/fs 的路径规范不接受前导斜杠，
// fs.ValidPath 会拒绝）。
func parseESXiBootCfgModules(bootCfg string) ([]string, error) {
	for _, line := range strings.Split(bootCfg, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, esxiBootCfgModulesKey+"=") {
			continue
		}
		value := strings.TrimPrefix(line, esxiBootCfgModulesKey+"=")
		parts := strings.Split(value, esxiModulesSeparator)
		modules := make([]string, 0, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimPrefix(strings.TrimSpace(p), "/")
			if trimmed == "" {
				continue
			}
			modules = append(modules, trimmed)
		}
		if len(modules) == 0 {
			return nil, fmt.Errorf("imports: boot.cfg 的 modules= 行为空——" +
				"一份可用的 ESXi 制品集至少要能启动内核模块，空列表说明这不是完整的 boot.cfg")
		}
		return modules, nil
	}
	return nil, fmt.Errorf("imports: 原始 boot.cfg 中没有找到 modules= 行，" +
		"无法确定要搬运哪些模块文件——这不是一份可用的 ESXi boot.cfg 模板")
}

// 契约缺口：ArtifactSet.Firmware 在导入流程中没有可靠的写入路径。
//
// contract.ArtifactSetStore.SetArtifactSetContent（internal/core/contract/
// store.go）的签名是
// `SetArtifactSetContent(ctx, id, osFamily, paths) (ArtifactSet, error)`，
// 只接受 OSFamily 与 ArtifactPaths 两项导入完成后才知道的内容，没有第三个
// 参数可以写回 Firmware 字段。而 model.ArtifactSet.Firmware
// （internal/core/model/inventory.go）恰恰是 SPEC §0.3、§2.1 明确要求的
// 字段：ESXi 只能是 uefi，Windows 需要按固件分两套制品集，这个值必须落库
// 才能在选集与 precheck 时被读到。
//
// 本文件把"固件校验"做成 ExtractESXi 的第三个入参
// （declaredFirmware model.FirmwareMode），而不是让本函数自己去猜或从
// ISO 内容反推——ISO 内容本身不会自报"我是 UEFI 制品"，这是运维登记
// artifact_set 时填写的属性，不是从文件系统结构里能识别出来的事实。
// 因此保守处理是：本函数只负责"给定一个声明值，校验它是否被 ESXi 接受"，
// 至于这个声明值从哪里来、最终又如何写回 ArtifactSet.Firmware 字段，
// 那需要调用方（job.go，不在本次分配范围内）与 Store 契约共同解决——
// 具体来说，job.go 的 RunESXi（若编排方按 RunRHEL 的模式补一个同名方法）
// 需要从某处（登记 artifact_set 时的表单，或一个新的 Store 方法参数）
// 取得这个值，传给 ExtractESXi，再在导入成功后想办法让它落库。
//
// 正解方向：contract.ArtifactSetStore.SetArtifactSetContent 的签名需要
// 新增一个 firmware model.FirmwareMode 参数（或者干脆是 Firmware 字段
// 在 artifact_set 创建时就已经写入、导入只是补全 OSFamily/Paths 两项，
// 若是后者，本函数的 declaredFirmware 参数就应该改为"调用方从已创建的
// ArtifactSet 记录里读出的现有值"，而不是一个全新输入）——两种方向都需要
// 触及 contract/store.go（冻结）或 job.go（不在本次分配范围内），本文件
// 不擅自选择其中一种并动手实现，只在此明确指出缺口与两个可能的正解方向。
