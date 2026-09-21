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
	"strings"

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
// 保守处理：本文件定义并使用一个私有的、显式标注为「临时」的编码约定
// （见 decodeDisksFromFacts 的文档），只解析本约定产出的格式；未来正解
// 应该是给 contract.HWFacts 或 TaskParamsResolver.Resolve 的签名补一个
// 结构化的磁盘清单参数（不再从扁平字符串 map 里二次解析），届时删除本文件
// 的编码/解码函数，调用点改为直接传结构化值即可，不影响编排顺序本身。
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

	return resolved, nil
}

// decodeDisksFromFacts 从 facts KV 解析出结构化的磁盘清单。
//
// 临时编码约定（详见 Resolve 的契约缺口说明）：facts[model.FactDisks]
// 是用 ";" 分隔的记录列表，每条记录内部用 "|" 分隔四个字段，顺序固定为
// "by_id_path|size_bytes|rotational|model"，例如：
//
//	/dev/disk/by-id/aaa|500000000000|false|SAMSUNG SSD 860;/dev/disk/by-id/bbb|2000000000000|true|WDC HDD
//
// 选 ";" 与 "|" 而不是 JSON：facts KV 的值类型是纯字符串
// （contract.TaskParamsResolver.Resolve 的 facts 参数类型是
// map[string]string），塞一段 JSON 进单个 string 值技术上可行，但会让
// 「facts 是简单文本 KV」这条 SPEC §5.5 强调的设计意图在这一个字段上
// 悄悄破例。用简单分隔符与其它 fact 值（如 LOBER_NICS 的
// "eth0:aa:bb:...:up,eth1:..."）保持同一种「文本分隔」的调性。
//
// facts 中不存在该键或值为空串时返回空切片、不报错——上游（discovery）
// 尚未采集到磁盘信息是正常的中间状态，不应该在这里就失败；真正的「零块
// 盘」判断交给 DiskPlanner.Plan 自己处理（它已经对空清单返回明确错误）。
func decodeDisksFromFacts(facts map[string]string) ([]contract.Disk, error) {
	raw := facts[model.FactDisks]
	if raw == "" {
		return nil, nil
	}

	records := strings.Split(raw, ";")
	disks := make([]contract.Disk, 0, len(records))
	for i, rec := range records {
		fields := strings.Split(rec, "|")
		if len(fields) != 4 {
			return nil, fmt.Errorf("provider: %s 第 %d 条记录字段数=%d，want 4"+
				"（临时编码约定：by_id_path|size_bytes|rotational|model），原始记录=%q",
				model.FactDisks, i, len(fields), rec)
		}

		size, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("provider: %s 第 %d 条记录的容量字段 %q 不是合法整数: %w",
				model.FactDisks, i, fields[1], err)
		}
		rotational, err := strconv.ParseBool(fields[2])
		if err != nil {
			return nil, fmt.Errorf("provider: %s 第 %d 条记录的 rotational 字段 %q 不是合法布尔值: %w",
				model.FactDisks, i, fields[2], err)
		}

		disks = append(disks, contract.Disk{
			ByIDPath:   fields[0],
			SizeBytes:  size,
			Rotational: rotational,
			Model:      fields[3],
		})
	}
	return disks, nil
}

// 编译期断言：taskParamsResolver 必须满足 contract.TaskParamsResolver。
var _ contract.TaskParamsResolver = (*taskParamsResolver)(nil)
