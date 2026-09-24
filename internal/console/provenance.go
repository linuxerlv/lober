// 本文件实现 P4-04 任务 1 的任务溯源视图：GET /tasks/:id/provenance。
//
// 驱动场景是「误装事故的四个问题」（计划任务 1）：谁在什么时候建了那个任务；
// 当时选的是哪台机器、哪个模板、哪个制品集；那个任务用了哪些 hook、谁写的谁
// 批的；从建任务到擦盘之间发生了什么。四个问题原本要跨五张表手工拼，这个端点
// 把它们做成一屏。
//
// 它不加任何装机能力，但它决定事故之后的处置速度——§3.2 承认「建任务即授权」
// 的残留风险没有被消除只是被缓解，选错 SN 的后果是一台生产机被擦盘
// （§10 失败模式 2b）。所以事故迟早会发生一次。
//
// **本端点对「被引用对象已删除」是显式支持的正常路径，不是错误路径。**
// §2.7 只保证「被 active 模板引用的 hook 不能删」，模板本身在任务终态后可以
// 删；而事故往往发生在几个月后，那时相关对象可能已经清理过。此时报 500 等于
// 在运维最需要它的时刻交白卷，因此每个引用块都带一个 Missing 标记，查不到就
// 标记加提示，仍然把 pin 住的版本号 / digest 原样给出——那是从备份里找回那一
// 版的唯一线索。
package console

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// routesProvenance 注册溯源视图。
//
// roleAdmin 而非权限码：视图含操作人信息（计划任务 1 的权限一节）。计划里
// 建议过「管理员与审计角色」，但 §9.4 没有定义审计角色，加一项角色是契约
// 变更——**先只给管理员**，等真有专职审计人员时再议。
//
// action 传空：这是读端点，§9.4 的审计只记写操作。导出那个入口不同，它在
// export.go 里显式记审计（导出是把数据带走，属于敏感操作）。
func (s *Server) routesProvenance() {
	s.handleFuncRole("GET", "/tasks/:id/provenance", roleAdmin, "", s.handleTaskProvenance)
}

// taskProvenance 是溯源视图的响应形态，五块内容对应计划任务 1 列出的五项。
type taskProvenance struct {
	// 第一块：创建信息。
	TaskID    string `json:"taskId"`
	CreatedAt int64  `json:"createdAt"`
	CreatedBy string `json:"createdBy"`

	// CreatedFromIP 从建任务那条审计记录的 detail 里取（auditMiddleware 写的
	// 是 "client_ip=<ip>"）。它是「谁建的」之外的第二条线索：同一账号被多人
	// 共用时，只能靠 IP 区分到底是谁。取不到时为空串，不是错误。
	CreatedFromIP string `json:"createdFromIp"`

	HostSN   string `json:"hostSn"`
	HostMAC  string `json:"hostMac"`
	HostUUID string `json:"hostUuid"`

	State         string `json:"state"`
	FailureReason string `json:"failureReason"`

	// 第二块：pin 的三项，各自解析成可读信息。
	Template    provenanceTemplate    `json:"template"`
	ArtifactSet provenanceArtifactSet `json:"artifactSet"`
	Hooks       []provenanceHook      `json:"hooks"`

	// 第三块：完整事件时间线（含非信号来源的事件）。
	Timeline []taskEventItem `json:"timeline"`

	// 第四块：失败详情（§3.5 的六个字段）。nil 表示未失败。
	FailureDetail *failureDetailItem `json:"failureDetail"`

	// 第五块：后续操作（重试、取消等，含 P4-01 的自动重试）。
	Operations []provenanceOperation `json:"operations"`
}

// provenanceTemplate 是解析后的模板信息。Missing 为真时 Name 为空、Version
// 仍是任务 pin 住的那个版本号。
type provenanceTemplate struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
	Missing bool   `json:"missing"`
	Hint    string `json:"hint"`
}

// provenanceArtifactSet 是解析后的制品集信息。
type provenanceArtifactSet struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OSFamily  string `json:"osFamily"`
	OSVersion string `json:"osVersion"`
	Missing   bool   `json:"missing"`
	Hint      string `json:"hint"`
}

// provenanceHook 是解析后的 hook 信息。作者与审批人是追责链上最关键的一环
// ——hook 以 root 身份在分区之前运行，能力等同「擦掉这台机器所有盘」（§9.4）。
type provenanceHook struct {
	Digest     string `json:"digest"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Stage      string `json:"stage"`
	Version    int    `json:"version"`
	Author     string `json:"author"`
	Approver   string `json:"approver"`
	ApprovedAt int64  `json:"approvedAt"`
	Missing    bool   `json:"missing"`
	Hint       string `json:"hint"`
}

// provenanceOperation 是一条后续操作记录。
type provenanceOperation struct {
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

// handleTaskProvenance 处理 GET /tasks/:id/provenance。
func (s *Server) handleTaskProvenance(c *gin.Context) {
	prov, err := s.buildTaskProvenance(c.Request.Context(), c.Param("id"))
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	apierr.OK(c, prov)
}

// buildTaskProvenance 组装五块内容。被导出端点（export.go）复用，因此逻辑在
// 这里而不在 handler 里——两个入口必须给出同一份溯源，否则「页面上看到的」与
// 「导出交给审计方的」会是两份不一致的材料。
//
// 只有「任务本身查不到」是错误（返回 contract.ErrNotFound → 404）。pin 的三项
// 查不到一律降级成 Missing 标记，见文件头注释。
func (s *Server) buildTaskProvenance(ctx context.Context, taskID string) (taskProvenance, error) {
	tk, err := s.deps.Tasks.GetTask(ctx, taskID)
	if err != nil {
		return taskProvenance{}, err
	}

	prov := taskProvenance{
		TaskID:        tk.ID,
		CreatedAt:     tk.CreatedAt,
		CreatedBy:     tk.CreatedBy,
		HostSN:        tk.HostSN,
		HostMAC:       tk.HostMAC,
		HostUUID:      tk.HostUUID,
		State:         string(tk.State),
		FailureReason: string(tk.FailureReason),
		Template:      s.resolveProvenanceTemplate(ctx, tk),
		ArtifactSet:   s.resolveProvenanceArtifactSet(ctx, tk),
		Hooks:         s.resolveProvenanceHooks(ctx, tk),
		Timeline:      s.resolveProvenanceTimeline(ctx, tk),
	}
	if tk.FailureDetail != nil {
		prov.FailureDetail = toFailureDetailItem(tk.FailureDetail)
	}
	prov.CreatedFromIP, prov.Operations = s.resolveProvenanceAudit(ctx, tk)
	return prov, nil
}

// resolveProvenanceTemplate 取 pin 住的那一版模板。
//
// 用 GetTemplate 的精确版本查询而不是取最新版：pin 的意义就是「正在运行的
// 任务不受后续改模板影响，且可复现」（§2.6）。取最新版会在事故复盘时给出
// 一份**当时并没有被使用**的模板正文，比查不到更糟——那会把排查引向错误方向。
func (s *Server) resolveProvenanceTemplate(ctx context.Context, tk model.Task) provenanceTemplate {
	out := provenanceTemplate{ID: tk.TemplateID, Version: tk.TemplateVersion}
	tpl, err := s.deps.Templates.GetTemplate(ctx, tk.TemplateID, tk.TemplateVersion)
	if err != nil {
		out.Missing = true
		out.Hint = "该模板版本已删除或查不到；pin 的版本号仍保留在上方，可据此从备份中定位当时的模板正文"
		return out
	}
	out.Name = tpl.Name
	// 以 pin 的版本号为准而不是查回记录的 Version——两者理论上相同，不同则
	// 说明存储层的版本查询有 bug，这时显示 pin 的那个更忠于「当时用了什么」。
	return out
}

// resolveProvenanceArtifactSet 取制品集。
func (s *Server) resolveProvenanceArtifactSet(ctx context.Context, tk model.Task) provenanceArtifactSet {
	out := provenanceArtifactSet{ID: tk.ArtifactSetID}
	set, err := s.deps.ArtifactSets.GetArtifactSet(ctx, tk.ArtifactSetID)
	if err != nil {
		out.Missing = true
		out.Hint = "该制品集已删除或查不到；制品集 ID 仍保留在上方"
		return out
	}
	out.Name = set.Name
	out.OSFamily = string(set.OSFamily)
	out.OSVersion = set.OSVersion
	return out
}

// resolveProvenanceHooks 按任务 pin 的 digest 列表逐个反查 hook。
//
// 按 digest 而不是按 id 反查：§2.7 的浮动引用防护——hook 正文不可变，修改即
// 新版本新 digest，任务 pin 的是 digest。按 id 查会拿到后来改过的版本，而那
// 份正文当时根本没跑过。
//
// 查不到时仍然列出那个 digest：它是「当时到底跑了什么」的唯一记录。返回空
// 切片而非 nil，让 JSON 序列化出 [] 而不是 null（前端不需要为两种空值各写
// 一个分支）。
func (s *Server) resolveProvenanceHooks(ctx context.Context, tk model.Task) []provenanceHook {
	out := make([]provenanceHook, 0, len(tk.HookDigests))
	for _, digest := range tk.HookDigests {
		item := provenanceHook{Digest: digest}
		h, err := s.deps.Hooks.GetHookByDigest(ctx, digest)
		if err != nil {
			item.Missing = true
			item.Hint = "该 digest 对应的 hook 已删除或查不到；digest 本身仍保留，" +
				"它是「当时到底跑了什么」的唯一记录"
			out = append(out, item)
			continue
		}
		item.ID = h.ID
		item.Name = h.Name
		item.Stage = string(h.Stage)
		item.Version = h.Version
		item.Author = h.Author
		item.Approver = h.Approver
		item.ApprovedAt = h.ApprovedAt
		out = append(out, item)
	}
	return out
}

// resolveProvenanceTimeline 取完整事件时间线（含非信号来源的事件：扫描循环判
// 超时、人工操作、任务创建）。
//
// 取数失败时返回空时间线而不是让整个溯源失败：四块里少一块仍然有用，而
// 「事件表暂时查不到」不该让「谁建的、用了哪些 hook」这些已经拿到的信息一起
// 消失。
func (s *Server) resolveProvenanceTimeline(ctx context.Context, tk model.Task) []taskEventItem {
	events, err := s.deps.Events.ListEvents(ctx, tk.ID, contract.EventFilter{})
	if err != nil {
		return []taskEventItem{}
	}
	out := make([]taskEventItem, 0, len(events))
	for _, e := range events {
		out = append(out, toTaskEventItem(e))
	}
	return out
}

// auditDetailClientIPPrefix 是 auditMiddleware 写进 detail 的客户端 IP 前缀。
//
// 这里与 audit.go 的写入侧是一对耦合：那边写 "client_ip="+ip，这边解析同一个
// 前缀。做成常量是为了让两处的耦合可被 grep 到——若将来改了 detail 的格式而
// 忘了这里，溯源视图的客户端 IP 会静默变成空串（不报错、只是少一条线索）。
const auditDetailClientIPPrefix = "client_ip="

// resolveProvenanceAudit 从审计记录里取出两样：建任务时的客户端 IP，与后续
// 操作列表。
//
// 一次查询两用：按 Target=taskID 拉该任务的全部审计记录，其中 action 为
// task.create 的那条给出客户端 IP，其余的构成后续操作。**task.create 不进
// 后续操作列表**——它属于第一块创建信息，重复列出会让同一件事在界面上出现
// 两次。
//
// AuditQuery 未配置时返回空值，不报错：审计查询是可选依赖（Deps.AuditQuery
// 可为 nil），而溯源视图的其余四块不依赖它。
func (s *Server) resolveProvenanceAudit(ctx context.Context, tk model.Task) (createdFromIP string, ops []provenanceOperation) {
	ops = []provenanceOperation{}
	if s.deps.AuditQuery == nil {
		return "", ops
	}

	records, err := s.deps.AuditQuery.ListAudit(ctx, contract.AuditFilter{
		Target: tk.ID,
		Limit:  defaultListLimit,
	})
	if err != nil {
		return "", ops
	}

	for _, rec := range records {
		if rec.Action == auditActionTaskCreate {
			if ip, ok := clientIPFromAuditDetail(rec.Detail); ok {
				createdFromIP = ip
			}
			continue
		}
		ops = append(ops, provenanceOperation{
			TS: rec.TS, Actor: rec.Actor, Action: rec.Action, Detail: rec.Detail,
		})
	}
	return createdFromIP, ops
}

// auditActionTaskCreate 是建任务的审计动作名，与 task.go 的 routesTask() 里
// 注册 POST /tasks 时传的 action 必须一致。
const auditActionTaskCreate = "task.create"

// clientIPFromAuditDetail 从审计记录的 detail 里解析客户端 IP。
//
// detail 的格式是 auditMiddleware 定的 "client_ip=<ip>"，但本函数不假设它是
// detail 的全部内容——将来若 detail 里多带别的字段（逗号分隔之类），前缀匹配
// 仍然能取到，而整串相等的写法会在那一刻静默失效。
func clientIPFromAuditDetail(detail string) (string, bool) {
	idx := strings.Index(detail, auditDetailClientIPPrefix)
	if idx < 0 {
		return "", false
	}
	rest := detail[idx+len(auditDetailClientIPPrefix):]
	// 到第一个分隔符为止（逗号、空格、分号），没有分隔符则取到末尾。
	if end := strings.IndexAny(rest, ", ;"); end >= 0 {
		rest = rest[:end]
	}
	if rest == "" {
		return "", false
	}
	return rest, true
}
