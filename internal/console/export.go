// 本文件实现 P4-04 任务 4 的 CSV 流式导出，三个入口：审计查询结果、装机
// 统计、任务溯源。依据该计划的四条实现要求与一条规模保护。
//
// 为什么是 CSV 而不是 Excel 或 PDF：标准库自带 encoding/csv 不需要第三方库，
// 任何工具都能打开，**气隙环境里不依赖任何外部渲染服务**（计划任务 4）。
//
// 三条容易写错、且错了不会报错的地方，都在下面各自的位置注释了：
//
//  1. BOM 必须在第一行之前写出去；
//  2. 响应头一旦 flush，状态码就改不了了——所以行数上限与权限必须在写第一个
//     字节之前判完；
//  3. 不能用 apierr.OK（它内部 c.JSON 会写一个完整信封就结束响应）。
package console

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
)

// exportRowLimit 是单次导出的行数上限（计划任务 4 的规模保护）。
//
// 越过它是**拒绝并提示缩小时间范围**，不是截断——截断会产出一个看起来完整
// 但实际缺行的文件，而那个文件可能已经交给审计方了才被发现。
//
// 十万行的量级依据：一个无界导出会占满连接与磁盘 IO，而那会影响同一进程里
// 正在装机的机器（计划任务 4 原话）。十万行 CSV 约几十 MB，是"大但不至于
// 拖垮装机链路"的量级。
const exportRowLimit = 100000

// routesExport 注册三个导出入口。
//
// 全部 roleAdmin，且 action 非空——**导出动作本身要写审计**（计划任务 4
// 第 3 条）：把审计记录导出带走是敏感操作，「谁在什么时候导走了什么」必须
// 可追溯，否则审计链正好在最关键的这一步断掉。action 非空时
// handleFuncRole 会挂上 auditMiddleware，不需要在 handler 里自己写。
func (s *Server) routesExport() {
	s.handleFuncRole("GET", "/audit/export", roleAdmin, "audit.export", s.handleExportAudit)
	s.handleFuncRole("GET", "/reports/provisioning/export", roleAdmin, "report.export", s.handleExportProvisioning)
	s.handleFuncRole("GET", "/tasks/:id/provenance/export", roleAdmin, "provenance.export", s.handleExportProvenance)
}

// beginCSVDownload 写出下载响应头与 UTF-8 BOM，返回一个可直接写行的
// csv.Writer。调用方写完后必须 Flush。
//
// 三件事按固定顺序做，顺序本身有意义：
//
//  1. 设 Content-Type 与 Content-Disposition。文件名用**英文加时间戳**
//     （计划任务 4 第 4 条）——中文文件名在跨平台下载时会遇到编码问题
//     （RFC 6266 的 filename* 扩展并非所有工具都支持），而列名用中文已经
//     满足"运维直接能用"这个目标。
//  2. **不设 Content-Length**：流式写出时总长度事先不可知。设了它就意味着
//     实现先把整个文件拼在内存里算长度——几十万条审计记录会撑爆内存
//     （计划任务 4 第 1 条）。
//  3. 写 BOM，必须在任何 CSV 内容之前。中文 Windows 上的 Excel 读没有 BOM
//     的 UTF-8 会按 GBK 解码，表头直接乱码。这一条常被当成多余，但它是
//     「运维直接能用」与「运维要先转码」的区别（计划任务 4 第 2 条）。
func beginCSVDownload(c *gin.Context, namePrefix string) *csv.Writer {
	filename := fmt.Sprintf("%s-%d.csv", namePrefix, time.Now().Unix())
	c.Writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Writer.WriteHeader(http.StatusOK)

	// BOM 直接写进响应体，不经 csv.Writer——它会把 BOM 当成第一个字段的内容
	// 而给整个字段加引号。
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	return csv.NewWriter(c.Writer)
}

// refuseOversizedExport 在行数越过上限时写一个 4xx 并返回 true。
//
// 必须在 beginCSVDownload **之前**调用：响应头一旦写出，状态码就改不了了，
// 之后再失败只能表现为一个被截断的文件，而前端拦截器看到的是 200 成功。
func refuseOversizedExport(c *gin.Context, rows int) bool {
	if rows <= exportRowLimit {
		return false
	}
	apierr.Fail(c, apierr.New(http.StatusRequestEntityTooLarge, apierr.CodeBadRequest,
		fmt.Sprintf("本次导出命中 %d 行，超过单次上限 %d 行。请缩小时间范围（since/until）"+
			"或加上更具体的过滤条件后重试——刻意不做截断，因为一个被截断的导出文件"+
			"看起来是完整的，交给审计方之后才发现缺行的代价远大于当场重试一次。",
			rows, exportRowLimit)))
	return true
}

// auditExportHeader 是审计导出的中文表头（计划任务 4 第 4 条）。
var auditExportHeader = []string{"时间", "操作人", "动作", "目标", "详情"}

// handleExportAudit 处理 GET /audit/export：把审计查询结果导成 CSV。
//
// 过滤条件与 GET /audit 完全一致（复用 auditQueryFilterFromRequest），只是
// limit 的上限放到 exportRowLimit——导出的用途就是"一次带走一批"，套用列表
// 页那个 1000 的默认值会让导出永远只有一千行而没人察觉。
func (s *Server) handleExportAudit(c *gin.Context) {
	if s.deps.AuditQuery == nil {
		apierr.Fail(c, apierr.New(http.StatusServiceUnavailable, apierr.CodeInternal,
			"本部署未配置审计查询能力，无法导出"))
		return
	}

	filter := auditQueryFilterFromRequest(c)
	// 多取一行用来判断"是否越过上限"：取到 limit+1 条说明还有更多，属于越界。
	filter.Limit = exportLimitFromRequest(c)

	records, err := s.deps.AuditQuery.ListAudit(c.Request.Context(), filter)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	if refuseOversizedExport(c, len(records)) {
		return
	}

	w := beginCSVDownload(c, "audit")
	defer w.Flush()

	_ = w.Write(auditExportHeader)
	for _, r := range records {
		// 逐行写、逐行交给底层 Writer，不在内存里拼完整文件（计划任务 4
		// 第 1 条）。csv.Writer 内部有缓冲，但那是固定大小的缓冲，不随行数
		// 线性增长。
		_ = w.Write([]string{
			formatUnixTS(r.TS),
			r.Actor,
			r.Action,
			r.Target,
			r.Detail,
		})
	}
}

// exportLimitFromRequest 解析导出的 limit：默认取 exportRowLimit + 1。
//
// 为什么默认多取一行：refuseOversizedExport 靠"实际取回的行数是否超过上限"
// 判断越界，若默认就取 exportRowLimit 条，正好有 exportRowLimit 条数据时无法
// 区分"刚好到上限"与"还有更多被截掉了"——那正是要避免的静默截断。
func exportLimitFromRequest(c *gin.Context) int {
	raw := c.Query("limit")
	if raw == "" {
		return exportRowLimit + 1
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 || v > exportRowLimit+1 {
		return exportRowLimit + 1
	}
	return v
}

// formatUnixTS 把 Unix 秒格式化成人能读的本地时间，供 CSV 里的时间列使用。
//
// 导出文件是给人看的（汇报、离线分析、交给审计方），Unix 秒在表格软件里是
// 一串无意义的数字。API 的 JSON 响应仍然给 Unix 秒——那是给前端的，前端自己
// 按用户时区渲染。两处格式不同是刻意的，不是遗漏。
func formatUnixTS(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).Local().Format("2006-01-02 15:04:05")
}

// handleExportProvisioning 处理 GET /reports/provisioning/export。
func (s *Server) handleExportProvisioning(c *gin.Context) {
	tasks, err := s.deps.Tasks.ListTasks(c.Request.Context(), taskReportFilterFromRequest(c))
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	report := buildProvisioningReport(tasks)
	if refuseOversizedExport(c, len(report.ByState)) {
		return
	}

	w := beginCSVDownload(c, "provisioning-report")
	defer w.Flush()

	_ = w.Write([]string{"状态", "任务数", "占比"})
	for _, row := range report.ByState {
		share := 0.0
		if report.Total > 0 {
			share = float64(row.Count) / float64(report.Total)
		}
		_ = w.Write([]string{
			row.State,
			strconv.Itoa(row.Count),
			strconv.FormatFloat(share, 'f', 4, 64),
		})
	}
}

// handleExportProvenance 处理 GET /tasks/:id/provenance/export。
//
// 溯源是键值形态而不是等宽表格（一个任务对应一屏信息，不是多行同构记录），
// 因此 CSV 用「项, 值」两列——硬塞成宽表会得到一行几十列、在表格软件里要
// 横向滚动才能看完的东西。
func (s *Server) handleExportProvenance(c *gin.Context) {
	prov, err := s.buildTaskProvenance(c.Request.Context(), c.Param("id"))
	if err != nil {
		apierr.Fail(c, err)
		return
	}

	rows := provenanceCSVRows(prov)
	if refuseOversizedExport(c, len(rows)) {
		return
	}

	w := beginCSVDownload(c, "task-provenance")
	defer w.Flush()

	_ = w.Write([]string{"项", "值"})
	for _, row := range rows {
		_ = w.Write(row)
	}
}

// provenanceCSVRows 把溯源视图拍平成「项, 值」两列的行序列。
func provenanceCSVRows(p taskProvenance) [][]string {
	rows := [][]string{
		{"任务 ID", p.TaskID},
		{"创建人", p.CreatedBy},
		{"创建时间", formatUnixTS(p.CreatedAt)},
		{"创建时客户端 IP", p.CreatedFromIP},
		{"主机 SN", p.HostSN},
		{"主机 MAC", p.HostMAC},
		{"当前状态", p.State},
		{"模板", fmt.Sprintf("%s（v%d）", p.Template.Name, p.Template.Version)},
		{"制品集", p.ArtifactSet.Name},
	}
	if p.FailureReason != "" {
		rows = append(rows, []string{"失败原因", p.FailureReason})
	}
	for _, h := range p.Hooks {
		rows = append(rows, []string{
			"hook",
			fmt.Sprintf("%s（作者 %s，审批 %s，digest %s）", h.Name, h.Author, h.Approver, h.Digest),
		})
	}
	for _, op := range p.Operations {
		rows = append(rows, []string{
			"后续操作",
			fmt.Sprintf("%s %s %s", formatUnixTS(op.TS), op.Actor, op.Action),
		})
	}
	for _, e := range p.Timeline {
		rows = append(rows, []string{
			"事件",
			fmt.Sprintf("%s [%s] %s", formatUnixTS(e.TS), e.Phase, e.Message),
		})
	}
	return rows
}

// 编译期确认 AuditFilter 的字段名没有漂移——导出与查询共用同一个过滤器构造
// 函数，两边若分叉会导致"页面上看到的和导出的不是同一批数据"。
var _ = contract.AuditFilter{Actor: "", Action: "", Target: "", SinceTS: 0, UntilTS: 0, Limit: 0}
