// 本文件定义 fact 命名空间：24 个 LOBER_ 前缀键常量、契约版本、全部键的
// 切片。对应实现计划任务 8，依据 SPEC §5.4。
//
// fact 命名空间是对外 API：一旦有数十个 hook 在用就不能改名，所以键必须是
// 常量、必须有契约版本号、必须有测试防止误删（本文件的测试用一张 v1 承诺
// 键名快照锁住这一点）。宁可多注入，也别让 hook 去发明自己的取数方式——
// 一旦某个 hook 开始 curl 控制台 API 拿数据，耦合就失控了。
package model

// FactContractVersion 是当前 fact 命名空间的契约版本。服务端只增不删，
// 废弃字段需双写一个版本周期；升版本本身就是一次对外契约变更。
const FactContractVersion = 1

// 24 个 fact 键常量，全部以 LOBER_ 前缀（SPEC §5.4）。
const (
	FactContractVersionKey = "LOBER_CONTRACT_VERSION"

	FactTaskID   = "LOBER_TASK_ID"
	FactSN       = "LOBER_SN"
	FactMAC      = "LOBER_MAC"
	FactUUID     = "LOBER_UUID"
	FactHostname = "LOBER_HOSTNAME"

	FactOSFamily  = "LOBER_OS_FAMILY"
	FactOSVersion = "LOBER_OS_VERSION"
	FactArch      = "LOBER_ARCH"

	FactDisks       = "LOBER_DISKS"
	FactDiskPrimary = "LOBER_DISK_PRIMARY"
	FactNICs        = "LOBER_NICS"
	FactRAMMB       = "LOBER_RAM_MB"
	FactCPUCores    = "LOBER_CPU_CORES"
	FactVendor      = "LOBER_VENDOR"
	FactProduct     = "LOBER_PRODUCT"

	FactBizIP   = "LOBER_BIZ_IP"
	FactBizGW   = "LOBER_BIZ_GW"
	FactBizMask = "LOBER_BIZ_MASK"

	FactArtifactBase = "LOBER_ARTIFACT_BASE"
	FactGatewayBase  = "LOBER_GATEWAY_BASE"

	FactRack = "LOBER_RACK"
	FactRow  = "LOBER_ROW"
	FactDC   = "LOBER_DC"
)

// factKeysV1 是 v1 契约承诺的全部 24 个键。任何删除或改名都必须让测试
// 变红——新增键时同时改 factKeysV1 与本文件的常量定义，那一次「同时改」
// 就是在提醒作者：这是对外契约变更（SPEC §5.4，计划任务 8 设计要点）。
var factKeysV1 = []string{
	FactContractVersionKey,
	FactTaskID, FactSN, FactMAC, FactUUID, FactHostname,
	FactOSFamily, FactOSVersion, FactArch,
	FactDisks, FactDiskPrimary, FactNICs, FactRAMMB, FactCPUCores, FactVendor, FactProduct,
	FactBizIP, FactBizGW, FactBizMask,
	FactArtifactBase, FactGatewayBase,
	FactRack, FactRow, FactDC,
}

// FactKeysV1 返回 v1 契约承诺的全部键，供「无条件注入全部键」的遍历使用。
// 返回新分配的切片，调用方对其增删不影响内部状态。
func FactKeysV1() []string {
	out := make([]string, len(factKeysV1))
	copy(out, factKeysV1)
	return out
}

// factKeySet 是 factKeysV1 的集合形式，供 IsFactV1 做 O(1) 查询。
var factKeySet = func() map[string]bool {
	set := make(map[string]bool, len(factKeysV1))
	for _, k := range factKeysV1 {
		set[k] = true
	}
	return set
}()

// IsFactV1 判定某键是否属于 v1 契约，供 hook 注册期校验使用（§7.5：把
// 运行时意外提前为注册时错误）。
func IsFactV1(key string) bool {
	return factKeySet[key]
}
