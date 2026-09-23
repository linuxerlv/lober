// 本文件把 ExtractESXi 适配成 job.go 的 familyExtractor 查表形状，供编排方
// 在 identify.go 与 job.go 完成各自的接线之后，把 ESXi 接入
// familyExtractors 这张表（对应任务分配文档「解除 ESXi 的导入封锁」，依据
// SPEC §0.3、§2.1、附录 A.14）。
//
// 本文件不修改 job.go 与 identify.go（两者由并行的另一条轨改动，同时改
// 同一个文件必然冲突），只新增一个适配点：NewESXiFamilyExtractor。
package imports

import (
	"io/fs"

	"github.com/lober/lober/internal/core/model"
)

// NewESXiFamilyExtractor 把 ExtractESXi 适配成 familyExtractor 的形状
// （job.go：`func(root fs.FS, destDir string) (model.ArtifactPaths, error)`）。
//
// ExtractESXi 比 familyExtractor 多一个 declaredFirmware 入参（ESXi 固件
// 字段只接受 uefi，见 extract_esxi.go），而 familyExtractors 这张表的值
// 类型没有余地传第三个参数——job.go 的注释已经指出这正是 ESXi 没能进表
// 的原因。本函数用闭包把 declaredFirmware 固定下来，返回的函数值就能直接
// 赋给 familyExtractors[model.OSFamilyESXi]。
//
// declaredFirmware 从哪里取得，是本函数故意不回答的问题：这个值是运维
// 登记 artifact_set 时填写的属性，不是从 ISO 内容里能识别出来的事实，
// 调用方（job.go 的 Run，或其上层）必须先从别处（登记表单，或将来
// SetArtifactSetContent 补的固件参数）取得这个值，再调用本函数构造出
// 具体的抽取函数——本文件只负责「给定一个已知的声明值，产出一个签名
// 兼容的抽取函数」，不擅自决定这个值该从 Store 的哪个方法读，那需要触及
// 冻结的 contract/store.go 或不在本次分配范围内的 job.go（详见文件末尾
// wiringNeeded 说明与 extract_esxi.go 已有的契约缺口记录，两处指向同一个
// 缺口，不重复展开）。
//
// 用法示例（供编排方接线时参考，不在本文件内实际调用）：
//
//	familyExtractors[model.OSFamilyESXi] = NewESXiFamilyExtractor(declaredFirmware)
func NewESXiFamilyExtractor(declaredFirmware model.FirmwareMode) func(root fs.FS, destDir string) (model.ArtifactPaths, error) {
	return func(root fs.FS, destDir string) (model.ArtifactPaths, error) {
		return ExtractESXi(root, destDir, declaredFirmware)
	}
}

// ESXiRawBootCfgArtifactKey 是 ExtractESXi 返回的 model.ArtifactPaths.Extra
// 里存放「原始未改写 boot.cfg」的键名。RenderESXiBootCfg（渲染模块，本包
// 不可见）需要以这份原始模板为输入去做三处改写（prefix/kernelopt/modules
// 路径），因此把这个键名定义成一个具名常量导出，而不是让下游模块去猜
// 或硬编码字符串字面量 "boot.cfg"。
//
// 键名与磁盘上的相对文件名一致（ExtractESXi 的产出约定，见该函数文档），
// 值恰好等于常量本身（键与值相同）。
const ESXiRawBootCfgArtifactKey = esxiBootCfgName

// 编译期断言：ESXiRawBootCfgArtifactKey 的值确实是 "boot.cfg"（防止未来
// 有人改了 esxiBootCfgName 的字面量却没意识到这个常量也依赖它）。
var _ = func() struct{} {
	if ESXiRawBootCfgArtifactKey != "boot.cfg" {
		panic("imports: ESXiRawBootCfgArtifactKey 必须与 esxiBootCfgName 保持一致")
	}
	return struct{}{}
}()

// ---------------------------------------------------------------------------
// wiringNeeded：编排方在 n1（Windows 解封）与本轨都完成后，需要在
// identify.go 与 job.go 里手工接的两行改动（本文件不touch这两个文件，
// 理由见文件顶部说明）。
// ---------------------------------------------------------------------------
//
// 1. identify.go 第 26-31 行的 mvpUnsupportedFamilies：
//
//	var mvpUnsupportedFamilies = map[model.OSFamily]bool{
//		model.OSFamilyWindows: true,
//		model.OSFamilyESXi:    true,  // <- 删掉这一行（或整行连同注释一起移除）
//	}
//
//    删除后 Identify 命中 boot.cfg+mboot.efi 时会返回 (model.OSFamilyESXi, nil)
//    而不再是 (model.OSFamilyESXi, ErrUnsupportedOSFamily)——job.go 的 Run
//    才会继续往下走到 familyExtractors 查表这一步，而不是被第 2 步的
//    errIsUnsupported 分支拦下判定为「MVP 不支持」。
//
// 2. job.go 第 82-87 行的 familyExtractors 表，需要加一行：
//
//	var familyExtractors = map[model.OSFamily]familyExtractor{
//		model.OSFamilyRHEL:   ExtractRHEL,
//		model.OSFamilyDebian: ExtractDebian,
//		model.OSFamilyUbuntu: ExtractUbuntu,
//		model.OSFamilySUSE:   ExtractSUSE,
//		model.OSFamilyESXi:   NewESXiFamilyExtractor(declaredFirmware),  // <- 加这一行
//	}
//
//    这一行没法照抄——declaredFirmware 在这张表初始化的时机（包级 var，
//    程序启动时求值一次）根本拿不到：它是每次导入 job 运行时、针对某个
//    具体 artifact_set 才知道的值，不是编译期常量。因此这张表不能是
//    包级变量意义上的「静态加一行」，需要下面二选一：
//
//    方案 A（改动面更小）：Run 方法内部在查表之前，对 ESXi 这一个家族单独
//    分支处理（不查 familyExtractors，直接调用
//    ExtractESXi(root, destDir, declaredFirmware)），declaredFirmware 通过
//    Run 方法新增的入参或 Job 结构体新增字段传入——类似 RunRHEL 之外新增
//    一个 RunESXi 方法，或是把 Run 本身的签名扩出一个可选固件参数。
//
//    方案 B（表保持「值类型统一」但初始化推迟）：familyExtractors 不再是
//    包级 var，改成 Run 方法内部按需构造的局部 map（或是 Job 结构体新增
//    一个「运行时按 artifact_set 取到 declaredFirmware 后再调用
//    NewESXiFamilyExtractor 填这一行」的组装步骤），本质与方案 A 相同，
//    只是把「特判」包装成「查表」的外观。
//
//    两种方案都要回答同一个上游问题：declaredFirmware 这个值在 Run 被
//    调用的那一刻，从哪个参数或哪次 Store 读取里拿到——这正是
//    extract_esxi.go 文件末尾记录的契约缺口（Store.SetArtifactSetContent
//    签名没有固件参数），本文件不擅自选择 A 或 B，只指出两条路径与它们
//    共同依赖的同一个上游缺口。
