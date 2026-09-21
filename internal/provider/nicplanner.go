// 本文件实现网卡决策：按 MAC / PCI 路径 / link 状态选出装机网卡与业务
// 网卡。对应实现计划 P2-02 任务 3，依据 SPEC §5.6、§10 失败模式 6。
//
// 与 diskplanner.go 同构：纯函数、无 IO、无时钟、无随机数，返回选择与
// 理由。网卡命名漂移会让装完的机器网络不通、需要人到现场排查（SPEC §10
// 失败模式 6，仅次于装错盘的严重故障——机器装好了但连不上，所有远程手段
// 都失效），因此本文件的确定性要求与 diskplanner.go 完全一致：排序取代
// 随机、错误取代静默退化。
package provider

import (
	"fmt"
	"sort"
)

// NICIntent 是网卡决策的意图输入：主机清单里记录的已知 MAC（若有）、
// 期望的 PCI 路径（同型号多网卡时的区分依据）、业务网卡的已知 MAC。
//
// 三个字段都是可选的（零值即"未提供该判据"）——调用方（resolve.go）按
// 装配点能拿到的信息填充，本类型不强制要求任何字段非空，由 PlanInstallNIC
// / PlanBusinessNIC 各自决定"信息不足时如何回退或报错"。
type NICIntent struct {
	// KnownMAC 是主机清单登记的装机网卡 MAC（最可靠的判据）。
	KnownMAC string
	// PreferredPCIPath 是期望的装机网卡 PCI 路径（同型号多网卡时的区分
	// 依据，MAC 未命中时的次优回退）。
	PreferredPCIPath string
	// BusinessMAC 是主机清单登记的业务网卡 MAC。
	BusinessMAC string
}

// NICDecision 是网卡决策的结果：选中网卡的 MAC（不是接口名——接口名会
// 因固件升级、插卡位置变化而漂移，MAC 不会）与选择理由。
//
// 计划明确要求"输出是 MAC 而不是接口名"：应答文件里用 MAC 绑定网卡配置
// （三个家族都支持按 MAC 匹配），再固化接口命名策略，两者结合才能让重启
// 后的名字与装机时一致。
type NICDecision struct {
	MAC string
	// Reason 是"为什么选这块网卡"的说明，dry-run 端点展示它，与
	// contract.LayoutDisk.Reason 同一设计意图：决策会影响装完后机器是否
	// 能联网，选择依据必须可审计。
	Reason string
}

// nicByPCIPathAsc 按 PCIPath 字典序升序排序，作为"多块网卡都满足同等
// 判据时选谁"的确定性依据——与 diskplanner.go 的 diskByIDPathAsc 同一
// 理由：不能依赖输入顺序，那个顺序来自机器侧采集上报，不保证跨请求稳定。
func nicByPCIPathAsc(nics []HWNIC) []HWNIC {
	out := make([]HWNIC, len(nics))
	copy(out, nics)
	sort.Slice(out, func(i, j int) bool {
		return out[i].PCIPath < out[j].PCIPath
	})
	return out
}

// findByMAC 在候选网卡中查找 MAC 精确匹配的一块。ok 为 false 表示未命中
// （mac 为空也视为未命中，调用方不应传空字符串来"匹配任意网卡"）。
func findByMAC(nics []HWNIC, mac string) (HWNIC, bool) {
	if mac == "" {
		return HWNIC{}, false
	}
	for _, n := range nics {
		if n.MAC == mac {
			return n, true
		}
	}
	return HWNIC{}, false
}

// findByPCIPath 在候选网卡中查找 PCI 路径精确匹配的一块。
func findByPCIPath(nics []HWNIC, pciPath string) (HWNIC, bool) {
	if pciPath == "" {
		return HWNIC{}, false
	}
	for _, n := range nics {
		if n.PCIPath == pciPath {
			return n, true
		}
	}
	return HWNIC{}, false
}

// linkUpNICs 筛出 link up 的网卡子集。
func linkUpNICs(nics []HWNIC) []HWNIC {
	var out []HWNIC
	for _, n := range nics {
		if n.LinkUp {
			out = append(out, n)
		}
	}
	return out
}

// PlanInstallNIC 决策装机网卡（当前通着的那块），按判据优先级
// MAC 精确匹配 → link 状态 → PCI 路径 依次回退（计划任务 3）。
//
// 零网卡或全部 link down（且无 MAC/PCI 命中）时返回明确错误——不猜测一个
// 默认选择：网卡选错的后果是装完网络不通、需要人到现场，比空跑一次装机
// 更难排查（SPEC §10 失败模式 6，与 diskplanner.go 的"零块盘不能返回空
// 布局"同一方向）。
func PlanInstallNIC(nics []HWNIC, intent NICIntent) (NICDecision, error) {
	if len(nics) == 0 {
		return NICDecision{}, fmt.Errorf("provider: 网卡决策失败——候选网卡清单为空（0 块），" +
			"没有任何网卡就没有装机网络，不能返回一个看似合法但实际不可用的空决策" +
			"（SPEC §10 失败模式 6：网卡选错会导致装完网络不通，需人到现场排查）")
	}

	// 判据 1：MAC 精确匹配——主机清单记录的 MAC 是最可靠的判据，即使此刻
	// link 是 down（可能是交换机端口临时抖动），仍应优先于 link 状态。
	if chosen, ok := findByMAC(nics, intent.KnownMAC); ok {
		return NICDecision{
			MAC: chosen.MAC,
			Reason: fmt.Sprintf(
				"判据 1（MAC 精确匹配）：主机清单记录的 MAC %s 命中候选网卡 %s（PCI %s，"+
					"link=%s）——清单记录的物理事实优先于此刻探测到的 link 状态",
				chosen.MAC, chosen.Name, chosen.PCIPath, linkStateText(chosen.LinkUp),
			),
		}, nil
	}

	// 判据 2：link 状态——当前通着的那块就是机器实际用来 PXE 引导到这里
	// 的那块（清单未命中或未提供 MAC 时的回退）。
	upNICs := linkUpNICs(nics)
	if len(upNICs) == 1 {
		chosen := upNICs[0]
		return NICDecision{
			MAC: chosen.MAC,
			Reason: fmt.Sprintf(
				"判据 2（link 状态）：无 MAC 匹配，候选网卡中唯一 link up 的是 %s（MAC %s，"+
					"PCI %s）——装机阶段通着的那块即为装机网卡",
				chosen.Name, chosen.MAC, chosen.PCIPath,
			),
		}, nil
	}

	if len(upNICs) > 1 {
		// 判据 3：PCI 路径——同型号多网卡且都 link up 时用它区分板载与
		// 扩展卡（PreferredPCIPath 命中的场景），未提供或未命中时按
		// PCIPath 字典序取第一个以保证确定性（不依赖采集上报顺序）。
		if chosen, ok := findByPCIPath(upNICs, intent.PreferredPCIPath); ok {
			return NICDecision{
				MAC: chosen.MAC,
				Reason: fmt.Sprintf(
					"判据 3（PCI 路径）：无 MAC 匹配，多块网卡同时 link up，"+
						"按期望 PCI 路径 %s 命中网卡 %s（MAC %s）——用于区分板载与扩展卡",
					intent.PreferredPCIPath, chosen.Name, chosen.MAC,
				),
			}, nil
		}

		sorted := nicByPCIPathAsc(upNICs)
		chosen := sorted[0]
		return NICDecision{
			MAC: chosen.MAC,
			Reason: fmt.Sprintf(
				"判据 3（PCI 路径，确定性回退）：无 MAC 匹配，多块网卡同时 link up 且"+
					"未命中期望 PCI 路径，按 PCI 路径字典序取第一个（%s，MAC %s）——"+
					"保证同一批网卡换一次采集上报顺序仍选出同一块，不依赖输入顺序",
				chosen.PCIPath, chosen.MAC,
			),
		}, nil
	}

	// 全部 link down 且无 MAC/PCI 命中：没有任何依据，明确报错。
	return NICDecision{}, fmt.Errorf("provider: 网卡决策失败——候选网卡共 %d 块，"+
		"全部 link down，且无 MAC 匹配（清单 MAC=%q）——没有任何判据能确定哪块是装机网卡，"+
		"不能猜一个（SPEC §10 失败模式 6：网卡选错会导致装完网络不通，需人到现场排查）",
		len(nics), intent.KnownMAC)
}

// PlanBusinessNIC 决策业务网卡（要配业务 IP 的那块），依据 intent.BusinessMAC
// 精确匹配。
//
// 只有一块候选网卡时，业务网卡与装机网卡必然是同一块——不要求
// BusinessMAC 命中（此时装机网卡本身就是唯一选择，与判据是否满足无关）。
// 多块网卡且 BusinessMAC 未命中任何候选时返回明确错误——业务网配置写错
// 网卡同样会导致装完网络不通（SPEC §10 失败模式 6 的另一种触发路径），
// 不能静默复用装机网卡的决策结果去顶替。
//
// 决策出的业务网卡在应答文件里必须是"写配置不激活"形态（SPEC §5.7）——
// 该约束由渲染器落地（不是本函数的职责，本函数只负责"选哪块卡"），但
// 调用方必须知道：本函数的返回值只是意图，不隐含任何激活动作。
func PlanBusinessNIC(nics []HWNIC, intent NICIntent, installDecision NICDecision) (NICDecision, error) {
	if len(nics) == 0 {
		return NICDecision{}, fmt.Errorf("provider: 业务网卡决策失败——候选网卡清单为空（0 块）")
	}

	if len(nics) == 1 {
		only := nics[0]
		return NICDecision{
			MAC: only.MAC,
			Reason: fmt.Sprintf(
				"单网卡机器：业务网卡与装机网卡是同一块物理网卡（MAC %s），没有第二块卡可选",
				only.MAC,
			),
		}, nil
	}

	if chosen, ok := findByMAC(nics, intent.BusinessMAC); ok {
		return NICDecision{
			MAC: chosen.MAC,
			Reason: fmt.Sprintf(
				"按主机清单记录的业务网卡 MAC %s 命中候选网卡 %s（PCI %s）",
				chosen.MAC, chosen.Name, chosen.PCIPath,
			),
		}, nil
	}

	return NICDecision{}, fmt.Errorf("provider: 业务网卡决策失败——候选网卡共 %d 块，"+
		"主机清单未提供或未命中业务网卡 MAC（BusinessMAC=%q，装机网卡已选定为 %s）——"+
		"多网卡场景下没有依据不能猜一个业务网卡（SPEC §10 失败模式 6：网卡选错会导致"+
		"装完网络不通，需人到现场排查）", len(nics), intent.BusinessMAC, installDecision.MAC)
}

// linkStateText 把 bool 形式的 link 状态渲染为 "up"/"down" 文本，用于
// Reason 说明中人类可读的展示。
func linkStateText(up bool) string {
	if up {
		return "up"
	}
	return "down"
}
