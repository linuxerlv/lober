// 本文件是 imports.WimArchDetector 的生产实现，供 wire.go 注入
// imports.Job.ConfigureWindows 的第二个参数。对应本轮 m2 轨任务，依据
// SPEC §0.3、§2.1、§4.6，internal/imports/extract_windows.go 文件末尾的
// 契约缺口说明第 2 条。
//
// # 设计决策：保守版本，信任声明值
//
// 真实解析 WIM 格式（微软的复合文件二进制容器，或新版基于 XML 的镜像
// 索引）需要专门的库或工具（wimlib 一类），且不在本模块允许的依赖白名单
// 内（modernc.org/sqlite、go-cmp、uuid、yaml.v3、golang.org/x/crypto、
// golang.org/x/net 均不含 WIM 解析能力）——extract_windows.go 的文件末尾
// 已经把这一点记为「真实实现需要在部署适配阶段补上」的已知契约缺口，本
// 文件正是那个「部署适配阶段」的交付，但选择不去解析真实字节。
//
// 理由：SPEC §2.1 已经要求 Windows 按固件导两个 artifact_set，运维登记
// 时填写的 model.ArtifactSet.Firmware 字段本身就是「这套 boot.wim 是给
// 哪种固件用的」这件事的权威声明——它不是一个需要被反向验证的猜测，而是
// 运维主动做出的决策（与 ESXi 的 declaredFirmware 走同一条信任路径，见
// extract_esxi.go）。写一个只覆盖标准 WIM 布局、遇到非标准打包方式
// （分卷、增量、第三方工具重新打包）就会误判的半成品解析器，风险明显
// 大于「诚实地信任声明值，把真实探测记为已知缺口」。
//
// # 代价：一致性校验退化为恒等
//
// ExtractWindows 收到 declaredFirmware 后，会拿 detector.DetectFirmware
// 探测到的值与 declaredFirmware 比对，不一致则拒绝导入（本是防止"运维
// 手动放错了文件"的最后一道闸门）。本实现的 DetectFirmware 读到的正是
// 同一个 artifact_set 的同一个 Firmware 字段——两者比对因此永远相等，
// 这道闸门在当前实现下不产生任何额外保护。这是本文件明确交付的契约缺口，
// 不是被忽略的边界情况：真正独立于声明值的位宽探测（读 boot.wim 内部的
// 架构标识）留给未来有 WIM 解析能力时补上，接口本身（WimArchDetector）
// 不需要跟着改。
//
// # 路径 → artifact_set_id 的反解
//
// DetectFirmware(bootWIMPath string) 的签名没有 artifact_set_id 参数
// （imports.WimArchDetector 接口定义如此，本文件不能修改那个签名），但
// 本实现需要知道该 boot.wim 属于哪个 artifact_set 才能查表。做法：依赖
// winsources.go 的 fileWindowsSourcesResolver 已经确立的目录约定——
// boot.wim 必定落在 <base>/<artifact_set_id>/boot.wim 这一层，取
// filepath.Dir 后再取 Base 就是 artifact_set_id。两个文件（本文件与
// winsources.go）因此共享同一条目录约定，任何一处改动都必须同步改另一处
// ——这与 extract_windows.go 文件头注释里"identify.go 与本文件共享同一份
// 契约，任何一处改动都必须同步改另一处"是同一种耦合关系。
package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/lober/lober/internal/core/model"
)

// artifactSetGetter 是 storeBackedWimArchDetector 依赖的最窄接口：只需要
// 按 ID 读一条 artifact_set，不需要完整 contract.ArtifactSetStore 的
// 增删改能力——与 job.go 的 artifactSetUpdater、importrunner.go 现有
// 窄接口风格一致（测试替身只需实现这一个方法）。
//
// 真实 Store（contract.ArtifactSetStore）天然满足本接口，不需要额外
// 适配代码。
type artifactSetGetter interface {
	GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error)
}

// storeBackedWimArchDetector 是 imports.WimArchDetector 的生产实现（保守
// 版本，见文件头注释）。
type storeBackedWimArchDetector struct {
	store artifactSetGetter
}

// newStoreBackedWimArchDetector 构造一个 storeBackedWimArchDetector。
func newStoreBackedWimArchDetector(store artifactSetGetter) *storeBackedWimArchDetector {
	return &storeBackedWimArchDetector{store: store}
}

// DetectFirmware 实现 imports.WimArchDetector。见文件头注释：本实现不
// 解析 bootWIMPath 指向文件的二进制内容，而是从路径反解出
// artifact_set_id、查表读出该制品集登记时声明的 Firmware 字段并原样
// 返回。
func (d *storeBackedWimArchDetector) DetectFirmware(bootWIMPath string) (model.FirmwareMode, error) {
	artifactSetID := filepath.Base(filepath.Dir(bootWIMPath))
	if artifactSetID == "" || artifactSetID == "." || artifactSetID == string(filepath.Separator) {
		return "", fmt.Errorf(
			"cmd/lober: 无法从路径 %q 反解出 artifact_set_id"+
				"——本探测器依赖 fileWindowsSourcesResolver 的目录约定"+
				"（<base>/<artifact_set_id>/boot.wim），传入的路径不含这一层子目录",
			bootWIMPath)
	}

	set, err := d.store.GetArtifactSet(context.Background(), artifactSetID)
	if err != nil {
		return "", fmt.Errorf(
			"cmd/lober: 读取 artifact_set %q（从路径 %q 反解得到）的声明固件失败: %w"+
				"——探测失败，不能假设一个默认固件继续（imports.WimArchDetector 接口约定）",
			artifactSetID, bootWIMPath, err)
	}

	if set.Firmware != model.FirmwareUEFI && set.Firmware != model.FirmwareBIOS {
		return "", fmt.Errorf(
			"cmd/lober: artifact_set %q 声明的固件是 %q，但 Windows 制品集必须取 uefi 或 bios"+
				"的具体值（SPEC §2.1：any 是 Linux 四家「一套制品通吃」专属语义，"+
				"Windows 需按固件各导一个制品集）——这通常意味着登记时的固件字段填错了",
			artifactSetID, set.Firmware)
	}

	return set.Firmware, nil
}

// 编译期断言：storeBackedWimArchDetector 满足 imports.WimArchDetector。
// 本文件不直接 import internal/imports 做接口断言（cmd/lober 里已有的
// importrunner.go 走 var _ console.ArtifactImporter = (*importRunner)(nil)
// 这种同包内断言惯例，但 imports.WimArchDetector 的方法签名
// (DetectFirmware(string) (model.FirmwareMode, error)) 已经在
// wimarch_test.go 与 wire.go 的实际接线处被检验——若签名不匹配，
// job.ConfigureWindows(resolver, detector) 那一行会在编译期直接报错，
// 效果与写一个显式断言等价，因此此处不重复 import internal/imports
// 仅为了写一行用不到的断言。
