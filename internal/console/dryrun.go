// 本文件实现磁盘 dry-run 端点。对应实现计划任务 8，依据
// docs/contracts/http-console.md §7、SPEC §8.2。
//
// 接收策略名与一批磁盘描述，调磁盘规划 provider，返回布局与「为什么这么
// 选」的说明，不落地——这是与 preview 并列的两个高价值端点之一：磁盘
// 策略决定擦哪块盘，能用录制的硬件事实先跑一遍是安全上的实质收益。
package console

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
)

// routesDiskDryRun 注册磁盘 dry-run 端点。
func (s *Server) routesDiskDryRun() {
	s.handle("POST", "/disk-policy/dryrun", "", "", s.handleDiskDryRun)
}

// diskDryRunRequest 是入参：策略名与一批模拟的硬件采集结果（facts，键名
// 形如真机采集脚本实际产出的 LOBER_INSPECT_DISK_<n>_<field>，见
// parseDisksFact 的文档）。
//
// binding 标签替换原来手写的两条校验（diskPolicy 不能为空、取值必须在
// 白名单内）：required 顶替空值判定，oneof 顶替 validDiskPolicies 那张
// map——五个取值直接抄 model.DiskPolicy 的五个具名常量值（authoring.go），
// binding tag 是字面量字符串，不能引用 Go 常量，因此这里没有取消「白名单
// 取值与 model 常量同源」这条注释所述的保证，只是把「同源」这件事从
// map 初始化搬到了肉眼核对 tag 字面量与常量定义（两处任何一边改动都需要
// 人工同步，与迁移前 map 初始化用常量、之后写字面量相比，多了一点点
// 走散风险，但 dryrun_test.go 的 TestDryRun_UnknownPolicyRejected 会在
// 两者不一致时立刻测试失败，不是无保护的裸字面量）。facts 字段没有
// 校验标签——它的合法性检查（分组、字段齐备、类型转换）依赖动态 map
// 遍历与查库无关的纯计算，标签表达不了，留在 parseDisksFact 里用 Go
// 代码做（契约 §7：标签表达不了的留在 handler 里）。
type diskDryRunRequest struct {
	DiskPolicy string            `json:"diskPolicy" binding:"required,oneof=first largest by-model raid1-ssd by-hook"`
	Facts      map[string]string `json:"facts"`
}

// diskPlanResult 是响应形态。
type diskPlanResult struct {
	ChosenDisk string `json:"chosenDisk"`
	Reasoning  string `json:"reasoning"`
}

// handleDiskDryRun 处理 POST /disk-policy/dryrun。
//
// 本轨（t12-inspect-wire）接线修复：本函数曾解析一个自造的、从未与真机
// 采集链路对齐的 facts[LOBER_DISKS] 字符串格式（用逗号/分号分隔字段），
// 导致"用录制的真实硬件事实跑一遍策略"这个 dry-run 端点的核心承诺
// （SPEC §8.2）名不副实：喂给它真实机器上报的 facts（render/inspect.go
// 采集脚本产出、格式已固化为 LOBER_INSPECT_DISK_<n>_<field>）永远解析
// 不出任何盘，因为它找的是另一个键、认的是另一种格式。现在改为解析
// 与真机采集链路一致的 LOBER_INSPECT_DISK_ 前缀键（parseDisksFact）。
//
// 为什么在本文件本地重新实现分组解析，而不是调用
// internal/provider.ParseHWDisks（已经实现、已经测试过同样的格式）：
// console 与 provider 是两个互不可见的实现模块（archtest 规则三），
// 本文件不能 import provider。这段解析逻辑目前也不是冻结契约（hwfacts.go
// 的文档已经记录：LOBER_INSPECT_DISK_ 前缀是"采集脚本与 hwfacts.go 解析
// 逻辑之间的私有编码约定"，不落进 model.FactKeysV1），因此没有 model/
// contract 层的公共位置可以放这份逻辑——只能在两个模块里各自实现一份，
// 这是当前模块边界下的真实代价，不是本文件的选择失误。若未来这个格式
// 升级为冻结契约，正解是把分组解析逻辑收敛到 model 或 contract 包一次，
// 两个模块的调用点都改为调用公共实现。
func (s *Server) handleDiskDryRun(c *gin.Context) {
	var req diskDryRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Fail(c, err)
		return
	}

	disks := parseDisksFact(req.Facts)
	layout, err := s.deps.DiskPlanner.Plan(c.Request.Context(), disks, contract.DiskIntent{
		Policy: req.DiskPolicy, Params: req.Facts,
	})
	if err != nil {
		apierr.Fail(c, apierr.New(http.StatusOK, apierr.CodeInternal, "磁盘规划失败: "+err.Error()))
		return
	}

	chosen := ""
	var reasons []string
	for _, sel := range layout.Selected {
		if chosen == "" {
			chosen = sel.Disk.ByIDPath
		}
		reasons = append(reasons, sel.Reason)
	}
	apierr.OK(c, diskPlanResult{ChosenDisk: chosen, Reasoning: strings.Join(reasons, "; ")})
}

// inspectDiskKeyPrefix 是采集专用键前缀，必须与
// internal/render/inspect.go 的产出、internal/provider/hwfacts.go 的
// inspectDiskKeyPrefix 逐字符保持一致——三处是同一份编码约定的三个独立
// 实现（render 产出、provider 解析、本文件解析），任何一处改动都要同步
// 改其它两处，靠测试而不是编译期检查守住一致性（三个包互不可见，无法用
// 类型系统强制）。
const inspectDiskKeyPrefix = "LOBER_INSPECT_DISK_"

// diskFieldKeys 是磁盘每条记录必须齐备的四个字段后缀，与
// provider/hwfacts.go 的同名切片保持一致的取值集合（顺序不影响解析，
// hasAllFields 只做存在性检查）。
var diskFieldKeys = []string{"BYID", "SIZE", "ROTATIONAL", "MODEL"}

// groupDiskRecordsBySerial 把形如 "LOBER_INSPECT_DISK_<index>_<field>" 的
// 键按 index 分组。与 provider/hwfacts.go 的 groupBySerial 同一逻辑（本
// 模块不能 import provider，见 handleDiskDryRun 上方的说明），只是简化为
// 磁盘专用（provider 版本是磁盘/网卡通用的，本文件 dry-run 端点只需要
// 磁盘）。
func groupDiskRecordsBySerial(facts map[string]string) map[string]map[string]string {
	groups := make(map[string]map[string]string)
	for key, value := range facts {
		if !strings.HasPrefix(key, inspectDiskKeyPrefix) {
			continue
		}
		rest := key[len(inspectDiskKeyPrefix):]
		sep := strings.IndexByte(rest, '_')
		if sep <= 0 || sep == len(rest)-1 {
			continue // 序号或字段名部分为空——畸形键，跳过（与 provider 版本同一防线）
		}
		index := rest[:sep]
		field := rest[sep+1:]

		group, ok := groups[index]
		if !ok {
			group = make(map[string]string)
			groups[index] = group
		}
		group[field] = value
	}
	return groups
}

// sortedDiskIndices 把字符串索引集合按数值升序排列，非数值索引整体丢弃
// （畸形分组）——与 provider/hwfacts.go 的 sortedNumericIndices 同一目的：
// 保证同一输入两次解析得到同一顺序，dry-run 展示的"选中哪块盘"不应因为
// map 遍历顺序不同而在两次请求间变化。
func sortedDiskIndices(groups map[string]map[string]string) []int {
	indices := make([]int, 0, len(groups))
	for idxStr := range groups {
		n, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		indices = append(indices, n)
	}
	sort.Ints(indices)
	return indices
}

// hasAllDiskFields 检查 group 是否包含 diskFieldKeys 列出的全部字段键，
// 且值非空——值为空字符串等同于字段缺失（与 provider/hwfacts.go 的
// hasAllFields 同一理由：一个"存在但是空"的容量字段与"完全没有这个键"
// 一样危险）。
func hasAllDiskFields(group map[string]string) bool {
	for _, f := range diskFieldKeys {
		v, ok := group[f]
		if !ok || v == "" {
			return false
		}
	}
	return true
}

// parseDisksFact 从完整的 facts map 中解析出结构化磁盘清单，消费
// render/inspect.go 真机采集脚本实际产出、与 provider/hwfacts.go 的
// ParseHWDisks 同一编码约定的 LOBER_INSPECT_DISK_<n>_<field> 键。
//
// 行为与 provider.ParseHWDisks 对齐（真实测试见 dryrun_test.go）：
//   - 没有任何该前缀键时返回空切片、不报错——dry-run 请求体不带磁盘事实
//     是合法输入（运维可能只想验证策略名校验逻辑），不是异常。
//   - 每条记录缺任一字段、或 SIZE/ROTATIONAL 字段值非法时，整条记录跳过
//     而不是让整个请求报错——dry-run 端点面对真机导出的、可能不完整的
//     facts 快照时应尽力展示能展示的结果，不应因为一条脏数据整体炸掉。
//   - 返回顺序按序号数值升序，保证同一输入两次解析结果一致。
func parseDisksFact(facts map[string]string) []contract.Disk {
	groups := groupDiskRecordsBySerial(facts)
	indices := sortedDiskIndices(groups)

	disks := make([]contract.Disk, 0, len(indices))
	for _, idx := range indices {
		group := groups[strconv.Itoa(idx)]
		if !hasAllDiskFields(group) {
			continue
		}

		size, err := strconv.ParseInt(group["SIZE"], 10, 64)
		if err != nil {
			continue
		}
		rotational, err := strconv.ParseBool(group["ROTATIONAL"])
		if err != nil {
			continue
		}

		disks = append(disks, contract.Disk{
			ByIDPath:   group["BYID"],
			SizeBytes:  size,
			Rotational: rotational,
			Model:      group["MODEL"],
		})
	}
	return disks
}
