// 本文件实现五个具名磁盘策略与策略工厂。对应实现计划任务 1、2、3、4，
// 依据 SPEC §7.1、§7.2、§7.6、§10 失败模式 5。
//
// 全文件纯函数：无 IO、无时钟、无随机数。这套系统会擦盘，磁盘规划函数若
// 行为不完全确定则故障无法复现——排序取代随机、错误取代静默退化，是本文件
// 贯穿全部策略的两条底线。
package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/lober/lober/internal/core/contract"
)

// diskByIDPathAsc 按 ByIDPath 字典序升序排序，作为「同容量/同型号并列时选谁」
// 的确定性依据（计划任务 1）。
//
// 不能依赖输入顺序：那个顺序来自机器侧采集上报，不保证跨请求稳定
// （任务 1 明确指出）；by-id 路径本身在同一台机器上是稳定的持久化标识，
// 拿它排序不会引入新的不确定性来源。
func diskByIDPathAsc(disks []contract.Disk) []contract.Disk {
	out := make([]contract.Disk, len(disks))
	copy(out, disks)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ByIDPath < out[j].ByIDPath
	})
	return out
}

// ---------------------------------------------------------------------------
// 任务 1：first 与 largest
// ---------------------------------------------------------------------------

// diskPlannerFirst 实现 model.DiskPolicyFirst：按输入顺序取第一块。
//
// 用于「机器型号统一、盘位固定」的批量场景（任务 1）——这批机器的第一块盘
// 位置本身就是稳定的选择依据，不需要再按容量或型号二次判断。
type diskPlannerFirst struct{}

func (diskPlannerFirst) Plan(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
	if len(disks) == 0 {
		return contract.Layout{}, fmt.Errorf("provider: disk_policy=first 但磁盘清单为空——" +
			"零块盘不能返回空布局，那会让渲染器产出一份没有磁盘指令的应答文件，" +
			"anaconda 遇到这种情况会交互式提问，机器会挂在那里等人（SPEC §10 失败模式 5）")
	}

	chosen := disks[0]
	return contract.Layout{
		Selected: []contract.LayoutDisk{
			{
				Disk: chosen,
				Reason: fmt.Sprintf(
					"策略 first：按输入顺序取第一块盘（%s，容量 %d 字节）——"+
						"该策略假定机型批次盘位固定，第一块即目标盘",
					chosen.ByIDPath, chosen.SizeBytes,
				),
			},
		},
	}, nil
}

// diskPlannerLargest 实现 model.DiskPolicyLargest：取容量最大的一块。
//
// 同容量并列时按设备标识（by-id 路径）排序后取第一个（任务 1）——不能依赖
// 输入顺序，因为那个顺序来自机器侧采集，不保证稳定。
type diskPlannerLargest struct{}

func (diskPlannerLargest) Plan(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
	if len(disks) == 0 {
		return contract.Layout{}, fmt.Errorf("provider: disk_policy=largest 但磁盘清单为空——" +
			"零块盘不能返回空布局（SPEC §10 失败模式 5，理由与 first 策略相同）")
	}

	sorted := diskByIDPathAsc(disks)

	var largest contract.Disk
	found := false
	for _, d := range sorted {
		if !found || d.SizeBytes > largest.SizeBytes {
			largest = d
			found = true
		}
	}

	return contract.Layout{
		Selected: []contract.LayoutDisk{
			{
				Disk: largest,
				Reason: fmt.Sprintf(
					"策略 largest：取容量最大的盘（%s，容量 %d 字节）——"+
						"若与其它盘容量并列，按设备标识（by-id 路径）字典序取第一个，"+
						"保证同一批次的选择结果与采集上报顺序无关",
					largest.ByIDPath, largest.SizeBytes,
				),
			},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// 任务 2：by-model
// ---------------------------------------------------------------------------

// diskPlannerByModel 实现 model.DiskPolicyByModel：匹配型号含指定子串的盘。
//
// 型号串从配置来（intent.Params["model"]，M1 的配置校验已保证选了这个策略
// 时它非空）。多块匹配时按设备标识排序取第一个；一块都不匹配时返回明确
// 错误——不回落到其它策略（任务 2：回落的后果是运维以为装在了 SSD 上，
// 实际装在了随便一块盘上，而这种错误装完才发现，那时数据已经被擦了）。
type diskPlannerByModel struct{}

// diskIntentModelParamKey 是 DiskIntent.Params 里型号串对应的键名。
const diskIntentModelParamKey = "model"

func (diskPlannerByModel) Plan(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
	if len(disks) == 0 {
		return contract.Layout{}, fmt.Errorf("provider: disk_policy=by-model 但磁盘清单为空——" +
			"零块盘不能返回空布局（SPEC §10 失败模式 5）")
	}

	modelSubstr := intent.Params[diskIntentModelParamKey]
	if modelSubstr == "" {
		return contract.Layout{}, fmt.Errorf("provider: disk_policy=by-model 但意图参数缺少 %q——"+
			"配置校验应已保证选了 by-model 策略时型号串非空，走到这里说明调用方"+
			"没有正确传递配置里的 disk_model 字段", diskIntentModelParamKey)
	}

	needle := toLowerASCII(modelSubstr)

	var matched []contract.Disk
	for _, d := range disks {
		if containsASCIIFold(d.Model, needle) {
			matched = append(matched, d)
		}
	}

	if len(matched) == 0 {
		return contract.Layout{}, fmt.Errorf("provider: disk_policy=by-model 未匹配到任何盘"+
			"（型号子串 %q，候选盘型号见日志）——不回落到其它策略：回落会让运维"+
			"以为装在了指定型号的盘上，实际装在了随便一块盘上，这个错误要等擦盘完成"+
			"才会被发现（SPEC 计划任务 2）", modelSubstr)
	}

	sorted := diskByIDPathAsc(matched)
	chosen := sorted[0]

	return contract.Layout{
		Selected: []contract.LayoutDisk{
			{
				Disk: chosen,
				Reason: fmt.Sprintf(
					"策略 by-model：型号 %q 含子串 %q（大小写不敏感）——"+
						"共匹配 %d 块盘，按设备标识排序取第一个",
					chosen.Model, modelSubstr, len(matched),
				),
			},
		},
	}, nil
}

// toLowerASCII 把字符串中的 ASCII 大写字母转小写，其余字节原样保留。
//
// 只处理 ASCII 范围：磁盘型号串（如 "SAMSUNG MZ7LH480HAHQ-00005"）是厂商
// 打印在固件里的标识，不含非 ASCII 字符是这类字段的实际情况，用
// strings.ToLower 的完整 Unicode 折叠规则纯属过度设计。
func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// containsASCIIFold 判定 toLowerASCII(s) 是否包含 needle（needle 必须已经是
// 小写）。子串匹配而非精确相等：型号字符串里常带空格、修订号与厂商前缀，
// 要求运维填精确值不现实（任务 2）。
func containsASCIIFold(s, needleLower string) bool {
	sLower := toLowerASCII(s)
	return indexOf(sLower, needleLower) >= 0
}

// indexOf 是 strings.Index 的直接替身；单独包一层只是为了让
// containsASCIIFold 的调用点不必额外 import "strings"，同时把「大小写折叠
// 后再找子串」这条逻辑聚在一处，避免调用方各自拼接两步。
func indexOf(s, substr string) int {
	n, m := len(s), len(substr)
	if m == 0 {
		return 0
	}
	if m > n {
		return -1
	}
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == substr {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// 任务 3：raid1-ssd
// ---------------------------------------------------------------------------

// diskPlannerRAID1SSD 实现 model.DiskPolicyRAID1SSD：从非旋转盘（SSD）里挑
// 两块容量相同的，返回主盘与两个成员。
//
// 不足两块同容量 SSD 时返回明确错误，不退化为单盘（任务 3）——静默退化会让
// 同一个策略名在不同机器上做不同的事，违反纯函数确定性；也会掩盖「这批
// 机器 SSD 不够」这个应该被运维立即看到的事实。
type diskPlannerRAID1SSD struct{}

func (diskPlannerRAID1SSD) Plan(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
	var ssds []contract.Disk
	for _, d := range disks {
		if !d.Rotational {
			ssds = append(ssds, d)
		}
	}

	if len(ssds) == 0 {
		return contract.Layout{}, fmt.Errorf("provider: disk_policy=raid1-ssd 但没有找到任何 SSD" +
			"（清单里全是旋转盘或为空）——不退化为单盘：静默退化会掩盖" +
			"「这批机器 SSD 不够」这个事实，运维应该立即看到这个错误而不是" +
			"事后发现装的是没有冗余的单盘（SPEC 计划任务 3）")
	}
	if len(ssds) == 1 {
		return contract.Layout{}, fmt.Errorf("provider: disk_policy=raid1-ssd 但只找到 1 块 SSD"+
			"（%s），RAID1 至少需要两块——不退化为单盘（SPEC 计划任务 3：静默退化会让"+
			"同一策略名在不同机器上做不同的事，且掩盖 SSD 不足这个应立即可见的事实）",
			ssds[0].ByIDPath)
	}

	// 按容量分组：RAID1 成员容量必须相同（不同容量会浪费到小的那块，且多数
	// 实现会拒绝，任务 3 明确要求）。容量不同的多组 SSD 时，取容量最大的
	// 那一组。
	bySize := make(map[int64][]contract.Disk)
	for _, d := range ssds {
		bySize[d.SizeBytes] = append(bySize[d.SizeBytes], d)
	}

	var bestSize int64 = -1
	var bestGroup []contract.Disk
	for size, group := range bySize {
		if len(group) < 2 {
			continue // 该容量的 SSD 不足两块，不能单独组 RAID1
		}
		if size > bestSize {
			bestSize = size
			bestGroup = group
		}
	}

	if bestGroup == nil {
		return contract.Layout{}, fmt.Errorf("provider: disk_policy=raid1-ssd 找到 %d 块 SSD"+
			"但没有任何两块容量相同——RAID1 要求成员容量一致（不同容量会浪费到小的"+
			"那块，且多数实现会拒绝），不退化为跨容量配对（SPEC 计划任务 3）", len(ssds))
	}

	sorted := diskByIDPathAsc(bestGroup)
	primary := sorted[0]
	secondary := sorted[1]

	reason := fmt.Sprintf(
		"策略 raid1-ssd：从 %d 块候选 SSD 中选出容量最大的同容量组（%d 字节，共 %d 块），"+
			"按设备标识排序取前两块组 RAID1——主盘 %s，成员盘 %s",
		len(ssds), bestSize, len(bestGroup), primary.ByIDPath, secondary.ByIDPath,
	)

	return contract.Layout{
		Selected: []contract.LayoutDisk{
			{Disk: primary, Reason: reason},
			{Disk: secondary, Reason: reason},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// 任务 4：by-hook 与工厂
// ---------------------------------------------------------------------------

// diskPlannerByHook 实现 model.DiskPolicyByHook：不做决策，把选盘逻辑推到
// %pre 阶段的 hook。
//
// 返回一个空的主盘与一句说明「由 pre 阶段的 hook 决定」（任务 4）。这是兜底
// 而非首选——服务端决策可调试、可 dry-run、可审计，把逻辑推到机器上失去
// 这三条（SPEC §5.6）。
type diskPlannerByHook struct{}

func (diskPlannerByHook) Plan(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
	return contract.Layout{
		Selected: []contract.LayoutDisk{
			{
				Disk: contract.Disk{},
				Reason: "策略 by-hook：不在服务端做决策，选盘逻辑由 %pre 阶段的 hook 动态决定。" +
					"代价（SPEC §5.6）：把逻辑推到机器上后，失败时无日志可看、无法复现、" +
					"无法 dry-run——服务端决策可调试、可审计，这条路径都不具备，因此" +
					"by-hook 是兜底而非首选，只在规则表确实表达不了某机型的选盘逻辑时才用",
			},
		},
	}, nil
}

// NewDiskPlanner 按策略名返回对应的 DiskPlanner 实现（计划任务 4）。
//
// 未知名字返回错误——这与 by-model 的「不匹配不回落」是同一个方向的设计：
// 配置里写错一个策略名应该在能被发现的地方失败，而不是静默套用某个默认
// 策略（那个默认策略可能恰好不是运维期望的行为）。
func NewDiskPlanner(policy string) (contract.DiskPlanner, error) {
	switch policy {
	case "first":
		return diskPlannerFirst{}, nil
	case "largest":
		return diskPlannerLargest{}, nil
	case "by-model":
		return diskPlannerByModel{}, nil
	case "raid1-ssd":
		return diskPlannerRAID1SSD{}, nil
	case "by-hook":
		return diskPlannerByHook{}, nil
	default:
		return nil, fmt.Errorf("provider: 未知的磁盘策略名 %q，合法取值为"+
			" first/largest/by-model/raid1-ssd/by-hook 之一", policy)
	}
}

// 编译期断言：五个策略实现都满足 contract.DiskPlanner（计划任务 12）。
var (
	_ contract.DiskPlanner = diskPlannerFirst{}
	_ contract.DiskPlanner = diskPlannerLargest{}
	_ contract.DiskPlanner = diskPlannerByModel{}
	_ contract.DiskPlanner = diskPlannerRAID1SSD{}
	_ contract.DiskPlanner = diskPlannerByHook{}
)
