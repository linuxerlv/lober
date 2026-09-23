// 本文件实现同步决策编排器（contract.TaskParamsResolver），依次调用模板
// 解析、IPAM、磁盘规划三个阻塞式 provider，任一失败即整体失败。对应实现
// 计划任务 8，依据 SPEC §7.4、§7.5、§2.6。
//
// 这是一处偏离 SPEC 图示的实现决定：SPEC §7.5 画的是「task.created → 申请
// IP、分配主机名，结果写回决策参数」，但事件总线是 fire-and-forget 的
// （contract.EventBus 的文档明确说明：Publish 不返回错误，事件发出时任务
// 已经落库了），IPAM 此时失败已经来不及阻塞建任务。那张图描述的是因果
// 关系，不是实现机制——真正阻塞的落地方式是本文件的同步编排器，在写库
// 之前由 M6 调用。
//
// 编排顺序：模板解析 → IPAM → 磁盘规划。模板解析在最前，因为后两者可能
// 需要知道装什么系统（比如不同 OS 的分区方案不同，任务 8 明确要求）。
//
// 决策结果被固化（SPEC §2.6：决策要固化，IO 可以实时）——固化动作由 M6
// 在写库时完成，本模块只负责产出。这条必须在这里写明：否则可能有人在
// 渲染时再调一次编排器，那会让同一任务前后两次拿到不同的 IP（任务 8
// 明确要求）。
package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// taskParamsResolver 是 contract.TaskParamsResolver 的内置实现。
type taskParamsResolver struct {
	resolver    contract.TemplateResolver
	ipamFactory func(form map[string]string) contract.IPAMProviderV1
	diskPlanner contract.DiskPlanner
	diskIntent  contract.DiskIntent
}

// NewTaskParamsResolver 构造同步决策编排器。
//
// diskIntent 由调用方（装配点）传入：Policy 与 Params（如 by-model 需要的
// 型号串）来自配置，本编排器不重新解析配置，只负责把已经决定好的意图
// 转交给磁盘规划器（DiskIntent 的构造属于 factory.go/装配点的职责，不是
// 本编排器的职责——编排器只负责「顺序调用并合并结果」这一件事）。
func NewTaskParamsResolver(
	resolver contract.TemplateResolver,
	diskPlanner contract.DiskPlanner,
	diskIntent contract.DiskIntent,
) contract.TaskParamsResolver {
	return &taskParamsResolver{
		resolver:    resolver,
		ipamFactory: func(form map[string]string) contract.IPAMProviderV1 { return newStaticIPAM(form) },
		diskPlanner: diskPlanner,
		diskIntent:  diskIntent,
	}
}

// Resolve 依次调用模板解析、IPAM、磁盘规划，任一失败立即返回错误与空
// 参数——不产生部分结果，避免调用方误用（任务 8 明确要求）。
//
// 契约缺口（写在这里，因为本方法是唯一的调用现场）：facts 参数的类型是
// map[string]string，即 SPEC §5.4 的 fact 命名空间本身（LOBER_OS_FAMILY /
// LOBER_VENDOR / LOBER_PRODUCT 三个键与 contract.HWFacts 的对应字段一一
// 对齐，没有缺口）。但 contract.DiskPlanner.Plan 需要的 []contract.Disk
// 每块盘含 SizeBytes/Rotational/Model 三个结构化字段，而 SPEC §5.4 对
// LOBER_DISKS 的描述只是「by-id 列表（inspect 采集）」——一个不含容量、
// 是否旋转、型号的纯路径列表，且列表本身的分隔符格式 SPEC 也未给出精确
// 定义（只有 LOBER_NICS 给了 "eth0:aa:bb:cc:...:up,eth1:...:down" 这个
// 格式示例，LOBER_DISKS 没有对应示例）。
//
// 这不是我能在本模块内部悄悄补一个默认值就绕过的缺口——磁盘规划的输入
// 数据从哪来是 discovery/pre 阶段采集链路（不属于本模块，本模块只拥有
// internal/provider 与 internal/imports）与磁盘规划器之间的接缝，缺的是
// 「谁负责把采集到的结构化磁盘信息编码进 facts KV，用什么编码」这条尚未
// 定义的契约。
//
// 本轨（t12-inspect-wire）接线修复：上面这段契约缺口描述的是"facts 里的
// 结构化磁盘信息该用什么编码"这个尚未冻结的问题，但本文件历史上给出的
// 保守处理（自造一个私有 "|" 分隔格式，只解析这一种自己发明的格式）绕过了
// 一个更根本的问题——render/inspect.go 实际产出、hwfacts.go 已经实现并
// 测试过解析的 LOBER_INSPECT_DISK_<n>_<field> 格式，从未被接到磁盘规划
// 输入这一步：整个仓库里 ParseHWDisks 在本次修复前只有单元测试调用它
// （hwfacts.go 文件头注释已经明确记录了这个断口）。真机采集出来的磁盘
// 事实因此根本进不了磁盘决策。
//
// 修复：decodeDisksFromFacts 改为直接调用同包的 ParseHWDisks（对
// render/inspect.go 产出格式的解析，已有完整测试覆盖畸形输入），把
// HWDisk 逐字段转换为 contract.Disk——两个类型字段集合完全一致
// （ByIDPath/SizeBytes/Rotational/Model），转换是纯粹的搬运，不引入新的
// 解析逻辑。原来的私有 "|" 分隔格式与本函数一起删除：全仓搜索确认除本
// 文件自己的测试外没有任何生产代码依赖该格式（不是对外契约，删除不需要
// 兼容期）。
//
// 仍然成立的契约缺口（未解决，留给契约持有者）：SPEC §5.4 冻结的
// LOBER_DISKS 键本身（"by-id 列表（inspect 采集）"）没有给出字符串序列化
// 格式，本修复没有去填 LOBER_DISKS 这个键——它绕开了那个未冻结的字符串
// 格式问题，直接消费 render/inspect.go 与 hwfacts.go 之间已经存在、已经
// 测试过的键值对编码（LOBER_INSPECT_DISK_*），这是本轨文件归属范围内能
// 做到的最小、最正确的接线；LOBER_DISKS 该长什么样仍是正解要走契约变更
// 流程解决的问题，不受本次修复影响。
func decodeDisksFromFacts(facts map[string]string) ([]contract.Disk, error) {
	hwDisks, err := ParseHWDisks(facts)
	if err != nil {
		// ParseHWDisks 当前实现对畸形字段值总是跳过而不报错（见其文档），
		// 这个 err 分支目前不可达；保留是因为 ParseHWDisks 的签名承诺了
		// error 返回值，忽略它会在该函数未来收紧行为（比如键名本身彻底
		// 不可读）时丢掉一个应该往上传的错误。
		return nil, fmt.Errorf("provider: 解析磁盘采集事实失败: %w", err)
	}

	disks := make([]contract.Disk, 0, len(hwDisks))
	for _, d := range hwDisks {
		disks = append(disks, contract.Disk{
			ByIDPath:   d.ByIDPath,
			SizeBytes:  d.SizeBytes,
			Rotational: d.Rotational,
			Model:      d.Model,
		})
	}
	return disks, nil
}

func (r *taskParamsResolver) Resolve(ctx context.Context, formData map[string]string, facts map[string]string) (map[string]string, error) {
	hwFacts := contract.HWFacts{
		OSFamily: facts[model.FactOSFamily],
		Vendor:   facts[model.FactVendor],
		Product:  facts[model.FactProduct],
		Arch:     facts[model.FactArch],
	}

	templateRef, err := r.resolver.Resolve(ctx, formData, hwFacts)
	if err != nil {
		return nil, fmt.Errorf("provider: 同步决策编排失败于模板解析步骤: %w", err)
	}

	ipam := r.ipamFactory(formData)
	allocResult, err := ipam.Allocate(ctx, contract.AllocRequest{
		Hostname: formData[model.FactHostname],
	})
	if err != nil {
		return nil, fmt.Errorf("provider: 同步决策编排失败于 IPAM 步骤: %w", err)
	}

	disks, err := decodeDisksFromFacts(facts)
	if err != nil {
		return nil, fmt.Errorf("provider: 同步决策编排失败于磁盘规划步骤（解析磁盘清单时）: %w", err)
	}

	layout, err := r.diskPlanner.Plan(ctx, disks, r.diskIntent)
	if err != nil {
		return nil, fmt.Errorf("provider: 同步决策编排失败于磁盘规划步骤: %w", err)
	}
	if len(layout.Selected) == 0 {
		return nil, fmt.Errorf("provider: 磁盘规划器返回了空的选盘结果——这不应该发生" +
			"（DiskPlanner 契约要求零块盘时返回错误而不是空 Selected），视为编排失败")
	}

	resolved := map[string]string{
		model.FactHostname: formData[model.FactHostname],
		model.FactBizIP:    allocResult.IP,
		model.FactBizGW:    allocResult.Gateway,
		model.FactBizMask:  allocResult.Mask,
		"template_id":      templateRef.TemplateID,
		"template_version": strconv.Itoa(templateRef.TemplateVersion),
	}

	// by-hook 策略（diskplanner.go 的 diskPlannerByHook）契约上会返回
	// len(Selected)==1 但 Disk 是零值——它不做服务端决策，选盘逻辑推到
	// %pre 阶段的 hook 动态计算（SPEC §5.6：「也可在 %pre 中动态计算磁盘，
	// 但那把逻辑推到机器上」）。这种情况下没有「服务端决策结果」可写
	// （SPEC §5.4 把 LOBER_DISK_PRIMARY 明确标注为「服务端决策结果」），
	// 若仍然写入空字符串，%pre 里的 hook 脚本读到的是一个看起来存在、
	// 实际为空的路径变量（如 `parted "$LOBER_DISK_PRIMARY" ...` 会用空
	// 路径调用而不是明确报错）——这比完全不注入这个键更危险：完全不注入
	// 时 `[ -n "$LOBER_DISK_PRIMARY" ]` 之类的判断能正确探测到「没有服务端
	// 决策」，注入空字符串则不能。因此这里不是遗漏一个 else 分支，而是
	// 刻意选择「宁可键缺失，也不要键存在但值是误导性的空字符串」——与本
	// 计划贯穿的「错误优先于静默退化」是同一个方向。
	if primary := layout.Selected[0].Disk.ByIDPath; primary != "" {
		resolved[model.FactDiskPrimary] = primary
	}

	// 新增步骤（P2-02 计划任务 3）：网卡决策。刻意放在磁盘规划步骤之后、
	// 作为编排顺序的第四步追加，不改动前三步（模板解析 → IPAM → 磁盘
	// 规划）任何既有行为——铁律第 3 条明确要求"不改既有函数行为，只加新
	// 分支/新函数"。
	//
	// 与前三步的失败语义不同：模板解析/IPAM/磁盘规划任一失败都会让整体
	// Resolve 提前返回错误（本方法开头的三个 if err != nil 分支）。网卡
	// 决策失败（没有采集到网卡事实，或采集到了但全部 link down 且无 MAC
	// 匹配）刻意不让整体 Resolve 失败——理由：
	//
	//  1. 这是本次改动新增的步骤，若让它的失败传染到整体编排，等价于
	//     悄悄改变了"建任务时编排失败"的既有语义（哪些失败会阻塞建任务），
	//     这正是铁律第 3 条要防止的"不改既有步骤行为"（虽然网卡决策本身
	//     是新步骤，但让新步骤获得与模板解析/IPAM/磁盘规划同等的阻塞
	//     地位是一个需要走契约评审的决定，不应该在本次改动里悄悄引入）。
	//  2. "决策失败时机器该怎么办"是 P2-02 计划任务 4 的职责（同步等待
	//     决策结果、读不到决策键时中止安装），任务 4 涉及
	//     internal/gateway/facts.go，不在本文件（也不在本 agent 的文件
	//     归属）范围内——本步骤只负责"有网卡事实时尽力决策，没有依据时
	//     不写决策键"，把"没有决策键时机器要不要继续分区"这个判断完全
	//     留给持有 gateway/facts.go 的实现者。
	//
	// 与磁盘 by-hook 策略同一方向的处理：宁可键缺失，也不要键存在但值
	// 是误导性的空字符串（decideAndAppendInstallNICMAC 内部保证这一点）。
	decideAndAppendInstallNICMAC(facts, resolved)

	return resolved, nil
}

// nicInstallMACParamKey 是网卡决策结果（装机网卡 MAC）在 Resolve 返回的
// resolved_params 中使用的键名。
//
// 契约缺口：SPEC §5.4 冻结的 24 个 LOBER_ 键里没有对应"网卡决策结果"的
// 键（对比磁盘决策有 LOBER_DISK_PRIMARY）；model/facts.go 是冻结契约，
// 不在本 agent 的文件归属内，不能在此新增常量。因此这里用一个与
// "template_id"/"template_version"同一先例的非冻结临时键名（本文件已有
// 的这两个键同样不属于 FactKeysV1，是编排器自行追加进 resolved_params 的
// 附加信息）。正解是走契约变更流程，在 model/facts.go 新增一个
// LOBER_NIC_INSTALL_MAC 之类的冻结键，届时把这里的字面量换成那个常量
// 即可，不需要改动本文件的其它逻辑。
const nicInstallMACParamKey = "nic_install_mac"

// decideAndAppendInstallNICMAC 尝试从 facts 中解析网卡采集事实并决策装机
// 网卡，成功时把结果写入 resolved（原地修改）。
//
// 两种"不写入"的情形，都不算错误：
//  1. facts 中没有任何网卡采集键（ParseHWNICs 返回空切片）——上游尚未
//     采集，是正常的中间状态，与磁盘规划面对空磁盘清单时"由 DiskPlanner
//     自己报错"不同：网卡决策在本文件中不是强制步骤（不在 taskParamsResolver
//     构造时传入的三个 provider 之列），没有数据就跳过。
//  2. PlanInstallNIC 因为找不到依据而报错（全部 link down 且无 MAC 匹配）
//     ——记录不下决策结果，但不让调用方的 Resolve 因此失败（见调用处的
//     详细说明）。
func decideAndAppendInstallNICMAC(facts map[string]string, resolved map[string]string) {
	nics, err := ParseHWNICs(facts)
	if err != nil || len(nics) == 0 {
		return
	}

	decision, err := PlanInstallNIC(nics, NICIntent{})
	if err != nil {
		return
	}
	resolved[nicInstallMACParamKey] = decision.MAC
}

// 编译期断言：taskParamsResolver 必须满足 contract.TaskParamsResolver。
var _ contract.TaskParamsResolver = (*taskParamsResolver)(nil)
