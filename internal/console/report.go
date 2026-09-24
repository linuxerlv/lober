// 本文件实现 P4-04 的审计查询与三张装机统计报表，依据
// docs/superpowers/plans/2026-09-20-lober-phase4-04-audit.md 任务 2 与任务 3，
// 以及 SPEC §9.4（权限分级）、§3.4（失败原因分类的诊断含义）。
//
// 四个端点全部只给管理员（用 handleFuncRole + roleAdmin，不是 handle +
// 权限码）：审计记录含全体操作人的动作明细，报表含全局装机数据，两者都不该
// 对普通 operator 可见。计划任务 1 讨论过是否要加一个「审计角色」——结论是
// §9.4 没有定义它，加一项角色是契约变更，**先只给管理员**，等真有专职审计
// 人员时再议。
//
// 统计一律在 Go 侧算，不下推成 SQL 聚合。两个理由：
//
//  1. 中位数与高分位在 PostgreSQL 里要用 percentile_cont，在 GORM 下只能塞
//     进 Select 的字符串里——而本仓库刚把最后一条裸 SQL 改成 GORM 表达
//     （commit 08d123c），为一张报表重新引入裸 SQL 片段不值得。
//  2. 阶段耗时**必须**逐任务遍历事件序列才能算（计划任务 3：任务表只有当前
//     阶段的进入时间，完整耗时要靠事件时间差），这一步本来就绕不开应用层。
//
// 代价是取数规模受限于 contract.TaskFilter.Limit（默认 1000）。这对装机任务
// 是合适的量级——任务数受「装了多少台机器」约束，与审计记录那种「每次写操作
// 一条」的增长方式不同。真需要跨几万条任务做报表时，正确的方向是加物化视图
// 或定时汇总表，而不是把这段逻辑改写成一条巨大的 SQL。
package console

import (
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// parseUnixTS 解析 since/until 这类 Unix 秒时间戳查询参数。空串或解析失败
// 返回 0，即「不按这一端过滤」——与 contract 里各 Filter 的零值语义一致。
//
// 解析失败不报错而是退化成不过滤：时间参数由前端日期控件生成，真出现非法值
// 说明是前端 bug 而不是运维输入错误，为它返回 400 会让整个报表页白屏；退化
// 成「不按时间过滤」至少还能看到数据，而范围明显不对这件事运维一眼就能发现。
func parseUnixTS(raw string) int64 {
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// routesReport 注册审计查询与三张报表。
//
// 全部用 handleFuncRole(roleAdmin) 而非 handle(code)：见文件头注释。action
// 传空——四个都是读端点，§9.4 的审计只记写操作（audit.go 的既有约定：读的量
// 大且价值低）。**唯一例外是 CSV 导出**，那是敏感读操作，在 export.go 里单独
// 记审计（计划任务 4 第 3 条）。
func (s *Server) routesReport() {
	s.handleFuncRole("GET", "/audit", roleAdmin, "", s.handleAuditQuery)
	s.handleFuncRole("GET", "/reports/provisioning", roleAdmin, "", s.handleReportProvisioning)
	s.handleFuncRole("GET", "/reports/failures", roleAdmin, "", s.handleReportFailures)
	s.handleFuncRole("GET", "/reports/phase-durations", roleAdmin, "", s.handleReportPhaseDurations)
}

// auditQueryFilterFromRequest 把查询串解析成 contract.AuditFilter。
//
// 四项条件（操作人、动作类型、目标资源、时间范围）全部可选、按「与」相交，
// 解析在这一层做，相交语义由 Store 保证（conformance 的「审计四项条件可组合」
// 锁住了它）。
//
// **limit 一定要补默认值**：contract.AuditFilter.Limit 的零值语义是「不限制」，
// 而审计表会长到几十万条（计划任务 2 的规模问题）。把零值原样透下去等于开放
// 一个无界查询，一次请求就能把内存和连接占满——而那会影响同一进程里正在装机
// 的机器。
func auditQueryFilterFromRequest(c *gin.Context) contract.AuditFilter {
	return contract.AuditFilter{
		Actor:   c.Query("actor"),
		Action:  c.Query("action"),
		Target:  c.Query("target"),
		SinceTS: parseUnixTS(c.Query("since")),
		UntilTS: parseUnixTS(c.Query("until")),
		Limit:   parseLimit(c.Query("limit"), defaultListLimit),
	}
}

// handleAuditQuery 处理 GET /audit：四项可组合条件的审计记录查询（计划任务 2）。
//
// 与 GET /hooks/{id}/audit（hook.go）的关系：那个是「某条 hook 的审计」这一
// 个具体用例的快捷入口，权限是 hook:write 或 hook:approve；本端点是通用查询，
// 权限收紧到 admin。两者并存不是重复——前者让 hook 作者能查自己的驳回理由而
// 不需要管理员权限，后者面向「某个人某段时间做了什么」这类跨资源审查。
func (s *Server) handleAuditQuery(c *gin.Context) {
	if s.deps.AuditQuery == nil {
		apierr.OK(c, []auditRecordItem{})
		return
	}

	records, err := s.deps.AuditQuery.ListAudit(c.Request.Context(), auditQueryFilterFromRequest(c))
	if err != nil {
		apierr.Fail(c, err)
		return
	}

	items := make([]auditRecordItem, 0, len(records))
	for _, rec := range records {
		items = append(items, toAuditRecordItem(rec))
	}
	apierr.OK(c, items)
}

// taskReportFilterFromRequest 把查询串解析成报表取数用的 contract.TaskFilter。
//
// 时间范围与审计查询用同一组参数名（since/until）与同一种包含性（闭区间）
// ——两处是运维在同一个页面的不同标签上看同一个时间窗口，参数名或包含性不同
// 会制造一类极难被报出来的困惑（contract.TaskFilter 的字段文档）。
func taskReportFilterFromRequest(c *gin.Context) contract.TaskFilter {
	return contract.TaskFilter{
		CreatedSinceTS: parseUnixTS(c.Query("since")),
		CreatedUntilTS: parseUnixTS(c.Query("until")),
		Limit:          parseLimit(c.Query("limit"), defaultListLimit),
	}
}

// provisioningReport 是装机量报表（计划任务 3 第一张表）：按状态分组的任务
// 计数，用于容量规划与汇报。
type provisioningReport struct {
	Total   int                    `json:"total"`
	ByState []provisioningStateRow `json:"byState"`
}

type provisioningStateRow struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

// handleReportProvisioning 处理 GET /reports/provisioning。
func (s *Server) handleReportProvisioning(c *gin.Context) {
	tasks, err := s.deps.Tasks.ListTasks(c.Request.Context(), taskReportFilterFromRequest(c))
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	apierr.OK(c, buildProvisioningReport(tasks))
}

// buildProvisioningReport 按状态分组计数。
//
// 状态顺序取 model 里声明的生命周期顺序而不是 map 遍历顺序：报表要能稳定
// 对照（两次刷新行序不同会让运维怀疑数据在变），而 Go 的 map 遍历顺序是刻意
// 随机化的。**只列出真实出现过的状态**，不为计数 0 的状态占一行——报表是给人
// 看的，七行里五行是 0 会淹掉有信息的那两行。
func buildProvisioningReport(tasks []model.Task) provisioningReport {
	counts := map[model.TaskState]int{}
	for _, tk := range tasks {
		counts[tk.State]++
	}

	order := []model.TaskState{
		model.TaskStatePending, model.TaskStateBooting, model.TaskStateInstalling,
		model.TaskStateRebooting, model.TaskStateInitializing,
		model.TaskStateDone, model.TaskStateFailed,
	}
	rows := make([]provisioningStateRow, 0, len(counts))
	for _, st := range order {
		if n := counts[st]; n > 0 {
			rows = append(rows, provisioningStateRow{State: string(st), Count: n})
		}
	}
	return provisioningReport{Total: len(tasks), ByState: rows}
}

// failureReport 是失败分布报表（计划任务 3 第二张表，「最有价值的一张」）。
//
// §3.4 的九种失败原因是有诊断含义的：集中在 boot_timeout 说明网络或制品传输
// 有问题，集中在 hook_required 说明某个 hook 不可靠，集中在 precheck 说明建
// 任务时经常选错机器（那是 §10 失败模式 2b 的信号）。没有这张表，这几种情况
// 看起来一样。
type failureReport struct {
	TotalFailed int                `json:"totalFailed"`
	ByReason    []failureReasonRow `json:"byReason"`
}

type failureReasonRow struct {
	Reason string  `json:"reason"`
	Count  int     `json:"count"`
	Share  float64 `json:"share"`

	// Reinstallable 直接带上 §3.4 的可重装判定，省得前端再维护一份白名单
	// 副本——那份副本迟早与 model.FailureReason.Reinstallable() 漂移，而漂移
	// 的方向若是「把不可重装误标成可重装」，代价是运维照着报表点重试、擦掉
	// 一台已经装好的机器。
	Reinstallable bool `json:"reinstallable"`
}

// handleReportFailures 处理 GET /reports/failures。
func (s *Server) handleReportFailures(c *gin.Context) {
	filter := taskReportFilterFromRequest(c)
	filter.State = model.TaskStateFailed
	tasks, err := s.deps.Tasks.ListTasks(c.Request.Context(), filter)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	apierr.OK(c, buildFailureReport(tasks))
}

// buildFailureReport 按失败原因分组，算计数与占比，按计数降序排列。
//
// 降序是为了让最该改进的环节排最前。同计数时按原因名升序，保证顺序确定
// （否则两次刷新行序可能不同）。
//
// 占比的分母是**本次统计到的失败任务总数**，不是全部任务数——这张表回答的是
// 「失败里哪一类最多」，而不是「总体失败率」。总体失败率由装机量报表的
// failed/total 给出，两个数字回答两个不同的问题，混在一起会让人误读。
func buildFailureReport(failed []model.Task) failureReport {
	counts := map[model.FailureReason]int{}
	for _, tk := range failed {
		counts[tk.FailureReason]++
	}

	rows := make([]failureReasonRow, 0, len(counts))
	for reason, n := range counts {
		share := 0.0
		if len(failed) > 0 {
			share = float64(n) / float64(len(failed))
		}
		rows = append(rows, failureReasonRow{
			Reason:        string(reason),
			Count:         n,
			Share:         share,
			Reinstallable: reason.Reinstallable(),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Reason < rows[j].Reason
	})

	return failureReport{TotalFailed: len(failed), ByReason: rows}
}

// phaseDurationReport 是阶段耗时报表（计划任务 3 第三张表）。
type phaseDurationReport struct {
	ByPhase []phaseDurationRow `json:"byPhase"`
}

type phaseDurationRow struct {
	Phase string `json:"phase"`

	// SampleCount 是真正算出了耗时的任务数，**不是**落在该阶段的任务数。
	// 两者不同这件事要能从响应里看出来：运维看到「booting 中位数 12 秒，
	// 样本 3 个」时才知道这个数字有多少可信度，只给中位数会让 3 个样本和
	// 300 个样本看起来一样权威。
	SampleCount int `json:"sampleCount"`

	MedianSec int64 `json:"medianSec"`
	P95Sec    int64 `json:"p95Sec"`
	MaxSec    int64 `json:"maxSec"`
}

// handleReportPhaseDurations 处理 GET /reports/phase-durations。
//
// 取数分两步：先按时间范围拿任务列表，再逐个任务读事件序列算阶段耗时。第二步
// 是 N 次 ListEvents，受 TaskFilter.Limit 约束（见文件头注释对规模的说明）。
func (s *Server) handleReportPhaseDurations(c *gin.Context) {
	ctx := c.Request.Context()
	tasks, err := s.deps.Tasks.ListTasks(ctx, taskReportFilterFromRequest(c))
	if err != nil {
		apierr.Fail(c, err)
		return
	}

	samples := map[model.Phase][]int64{}
	for _, tk := range tasks {
		events, err := s.deps.Events.ListEvents(ctx, tk.ID, contract.EventFilter{})
		if err != nil {
			apierr.Fail(c, err)
			return
		}
		for phase, sec := range phaseDurationsFromEvents(events) {
			samples[phase] = append(samples[phase], sec)
		}
	}
	apierr.OK(c, buildPhaseDurationReport(samples))
}

// phaseDurationsFromEvents 从一个任务的事件序列算出各阶段的耗时（秒）。
//
// 算法：事件按 Seq 升序（contract.EventStore.ListEvents 的约定）。每当阶段
// 发生变化，上一个阶段的耗时 = 本次变化的时刻 − 上一个阶段首次出现的时刻。
//
// **最后一个阶段不计入**，这是本函数最要紧的一条：它没有「下一个阶段的时刻」
// 作为终点，耗时无从得知。计划任务 3 明确要求这种情况**跳过而不是算成 0**
// ——中途失败的任务只有部分阶段有事件，把缺失算成 0 会把中位数往下拽，结果是
// 失败越多、报表显示的耗时越"好看"，与真实情况完全相反，而且数据上看起来
// 完全正常、不会有任何报错。
//
// 同一阶段的多条事件只取首次出现的时刻（一个阶段内会有多条日志事件）。阶段
// 若出现回退（正常情况下不会，状态机只进不退），后一次出现覆盖前一次的统计，
// 不做特殊处理——那属于数据异常，不值得为它设计一套语义。
func phaseDurationsFromEvents(events []model.TaskEvent) map[model.Phase]int64 {
	out := map[model.Phase]int64{}

	var curPhase model.Phase
	var curStart int64
	started := false

	for _, e := range events {
		if e.Phase == "" {
			continue
		}
		if !started {
			curPhase, curStart, started = e.Phase, e.TS, true
			continue
		}
		if e.Phase == curPhase {
			continue
		}
		// 阶段切换：上一个阶段的耗时就此确定。负数（事件时间戳倒序）视为
		// 数据异常，丢弃而不是记成负耗时——负耗时会污染中位数与分位数。
		if d := e.TS - curStart; d >= 0 {
			out[curPhase] = d
		}
		curPhase, curStart = e.Phase, e.TS
	}
	// 循环结束后 curPhase 是最后一个阶段，刻意不写进 out：见函数文档。
	return out
}

// buildPhaseDurationReport 把各阶段的样本算成中位数、P95 与最大值。
//
// 统计口径定死用中位数与高分位，**不用平均值**（计划任务 3 的一条硬要求）：
// 装机耗时分布是长尾的，少数机器因重试或慢盘远超常规，平均值会被那些异常值
// 拉高到没有参考价值。样本 {10,10,10,1000} 的平均值是 257.5 而中位数是 10
// ——前者不描述任何一台真实机器。
//
// 同时给 P95 与 Max 是为了让长尾本身可见：中位数说明"通常多快"，P95 与 Max
// 说明"最糟多慢"。只给中位数会让「有几台机器特别慢」这个信息完全消失，而那
// 往往正是要排查的对象。
func buildPhaseDurationReport(samples map[model.Phase][]int64) phaseDurationReport {
	order := []model.Phase{
		model.PhaseBooting, model.PhasePre, model.PhasePackages, model.PhasePost,
		model.PhaseRebooting, model.PhaseInitializing,
	}

	rows := make([]phaseDurationRow, 0, len(samples))
	for _, phase := range order {
		vals := samples[phase]
		if len(vals) == 0 {
			continue
		}
		sorted := make([]int64, len(vals))
		copy(sorted, vals)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

		rows = append(rows, phaseDurationRow{
			Phase:       string(phase),
			SampleCount: len(sorted),
			MedianSec:   median(sorted),
			P95Sec:      percentile(sorted, 0.95),
			MaxSec:      sorted[len(sorted)-1],
		})
	}
	return phaseDurationReport{ByPhase: rows}
}

// median 返回已升序排好的样本的中位数。偶数个样本取中间两个的均值——取下界
// 会让 {10,30} 报成 10，看起来像"一半的机器 10 秒装完"，而真实情况是两个样本
// 一个 10 一个 30。
func median(sorted []int64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// percentile 返回已升序排好的样本的 p 分位值（p ∈ [0,1]），用最近秩法
// （nearest-rank）：取第 ceil(p×n) 个样本。
//
// 选最近秩而不是线性插值：分位数在这里的用途是"指出一个真实存在的慢样本"，
// 插值会产出一个没有任何机器实际达到的数字。样本少时插值的结果尤其容易误导
// ——4 个样本的 P95 用插值算出来是一个介于第 3 与第 4 个之间的虚构值，而运维
// 想知道的是"最慢那台到底多慢"。
func percentile(sorted []int64, p float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := int(float64(n)*p + 0.999999) // 向上取整，避免浮点误差导致少取一位
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}
