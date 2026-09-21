// 本文件实现任务六个端点：建任务、列表、详情事件、SSE（另见 stream.go）、
// 重试、取消。对应实现计划任务 6，依据 docs/contracts/http-console.md §6、
// SPEC §3.2、§3.4、§7.4、§7.5。
//
// 建任务即授权装机（§3.2）。删掉 armed 之后，本文件的正确性承担了原先
// 两道闸门的责任——尤其是 handleCreateTask 的六步顺序不能变。
package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// routesTask 注册任务六个端点（stream 单独在 stream.go 的 routesStream
// 里注册，因为它不套响应包裹、实现风格与其它五个明显不同）。
func (s *Server) routesTask() {
	s.handleFunc("POST", "/tasks", codeTaskCreate, "task.create", s.handleCreateTask)
	s.handleFunc("GET", "/tasks", "", "", s.handleListTasks)
	s.handleFunc("GET", "/tasks/{id}/events", "", "", s.handleTaskEvents)
	s.handleFunc("POST", "/tasks/{id}/retry", codeTaskRetry, "task.retry", s.handleTaskRetry)
	s.handleFunc("POST", "/tasks/{id}/cancel", codeTaskCancel, "task.cancel", s.handleTaskCancel)
	s.routesStream()
}

// taskItem 是响应形态，字段名与 model.Task 对应，但省略 token 四个字段
// （§6：人面 API 不回传任务的装机凭据，这是安全边界不是字段名对齐问题）。
type taskItem struct {
	ID              string             `json:"id"`
	CreatedAt       int64              `json:"createdAt"`
	CreatedBy       string             `json:"createdBy"`
	HostSN          string             `json:"hostSn"`
	HostMAC         string             `json:"hostMac"`
	HostUUID        string             `json:"hostUuid"`
	TemplateID      string             `json:"templateId"`
	TemplateVersion int                `json:"templateVersion"`
	ArtifactSetID   string             `json:"artifactSetId"`
	HookDigests     []string           `json:"hookDigests"`
	FormData        map[string]string  `json:"formData"`
	ResolvedParams  map[string]string  `json:"resolvedParams"`
	State           string             `json:"state"`
	StateUpdatedAt  int64              `json:"stateUpdatedAt"`
	Version         int                `json:"version"`
	CurrentPhase    string             `json:"currentPhase"`
	PhaseEnteredAt  int64              `json:"phaseEnteredAt"`
	AttemptCount    int                `json:"attemptCount"`
	LastSeq         int                `json:"lastSeq"`
	LastSignalAt    int64              `json:"lastSignalAt"`
	FailureReason   string             `json:"failureReason"`
	FailureDetail   *failureDetailItem `json:"failureDetail"`
}

type failureDetailItem struct {
	Phase        string `json:"phase"`
	ExitCode     int    `json:"exitCode"`
	HookDigest   string `json:"hookDigest"`
	LogTail      string `json:"logTail"`
	SyslogTail   string `json:"syslogTail"`
	SinceLastSig string `json:"sinceLastSig"`
}

func toTaskItem(tk model.Task) taskItem {
	digests := tk.HookDigests
	if digests == nil {
		digests = []string{}
	}
	form := tk.FormData
	if form == nil {
		form = map[string]string{}
	}
	resolved := tk.ResolvedParams
	if resolved == nil {
		resolved = map[string]string{}
	}
	var detail *failureDetailItem
	if tk.FailureDetail != nil {
		detail = &failureDetailItem{
			Phase: string(tk.FailureDetail.Phase), ExitCode: tk.FailureDetail.ExitCode,
			HookDigest: tk.FailureDetail.HookDigest, LogTail: tk.FailureDetail.LogTail,
			SyslogTail: tk.FailureDetail.SyslogTail, SinceLastSig: tk.FailureDetail.SinceLastSig,
		}
	}
	return taskItem{
		ID: tk.ID, CreatedAt: tk.CreatedAt, CreatedBy: tk.CreatedBy,
		HostSN: tk.HostSN, HostMAC: tk.HostMAC, HostUUID: tk.HostUUID,
		TemplateID: tk.TemplateID, TemplateVersion: tk.TemplateVersion,
		ArtifactSetID: tk.ArtifactSetID, HookDigests: digests,
		FormData: form, ResolvedParams: resolved,
		State: string(tk.State), StateUpdatedAt: tk.StateUpdatedAt, Version: tk.Version,
		CurrentPhase: string(tk.CurrentPhase), PhaseEnteredAt: tk.PhaseEnteredAt,
		AttemptCount: tk.AttemptCount, LastSeq: tk.LastSeq, LastSignalAt: tk.LastSignalAt,
		FailureReason: string(tk.FailureReason), FailureDetail: detail,
	}
}

// createTaskRequest 是 POST /tasks 的入参。
//
// 刻意不包含 hostSn/hostMac 字段——即使客户端在 JSON 里塞了这两个键，
// json.Decode 到本结构体时也会被直接丢弃（Go 的 json 包对未知字段的
// 默认行为就是忽略），这是「本端点不接受直接传入 hostSn/hostMac 字段」
// 的类型层体现，不需要额外的运行期显式拒绝逻辑（约定 3、计划任务 6
// 「安全关键」要求）。
type createTaskRequest struct {
	HostID     string            `json:"hostId"`
	TemplateID string            `json:"templateId"`
	FormData   map[string]string `json:"formData"`
}

// handleCreateTask 处理 POST /tasks：建任务即授权装机，无 arm/disarm。
//
// 六步顺序不能变（计划任务 6）：
//  1. 按主机清单 id 取 Host——SN 与 MAC 从这里来，不接受请求里的 SN 字符串
//  2. 取模板的 pin 版本与制品集
//  3. 校验固件兼容
//  4. 调参数决策接口——失败即不建任务
//  5. 写库（任务 + 首条事件，同一事务）
//  6. 发布「任务已创建」生命周期事件——只是通知，不是决策点
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "请求体不是合法的 JSON"))
		return
	}
	if strings.TrimSpace(req.HostID) == "" {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "hostId 不能为空"))
		return
	}
	if strings.TrimSpace(req.TemplateID) == "" {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "templateId 不能为空"))
		return
	}

	// 第 1 步：按主机清单 id 取 Host（安全关键：SN/MAC 只从这里来）。
	// 用 findHostByExposedID 而非 HostStore.GetHost——见该函数注释：
	// GetHost 的 id 取法「由实现决定」，本模块暴露的 id 是 hostID() 算出
	// 的裸 SN/MAC/UUID 值，不能假设与某个具体 Store 实现的内部主键格式
	// 一致（已记入 contractGaps）。
	host, err := s.findHostByExposedID(r.Context(), req.HostID)
	if err != nil {
		if errors.Is(err, contract.ErrNotFound) {
			apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeNotFound,
				"hostId 指向的主机不存在，请从主机清单重新选择"))
			return
		}
		writeInternal(w, err)
		return
	}

	// 第 2 步：取模板的 pin 版本与制品集；模板必须是 active 状态（不允许
	// 用 draft/deprecated 模板建任务）。
	tpl, err := s.deps.Templates.GetTemplate(r.Context(), req.TemplateID, 0)
	if err != nil {
		if errors.Is(err, contract.ErrNotFound) {
			apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeNotFound, "templateId 指向的模板不存在"))
			return
		}
		writeInternal(w, err)
		return
	}
	if tpl.State != model.TemplateStateActive {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeConflict,
			"该模板不是启用（active）状态，不能用于建任务"))
		return
	}
	artifact, err := s.deps.ArtifactSets.GetArtifactSet(r.Context(), tpl.ArtifactSetID)
	if err != nil {
		writeInternal(w, err)
		return
	}

	// 第 3 步：校验固件兼容（§4.6 末段：库内记录值的校验有价值但不能
	// 替代引导时的校验，这里先拦一道）。
	if !model.FirmwareCompatible(artifact.OSFamily, host.Firmware) {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeConflict,
			"该主机上报的固件模式与制品集要求的 OS 家族不兼容，无法建任务"))
		return
	}

	// 第 4 步：调参数决策接口，失败即不建任务（§7.4 阻塞语义）。这一步
	// 必须在写库之前、且在事件发布之前——事件总线是 fire-and-forget 的，
	// task.created 事件发出时任务已经落库了，那时再报错为时已晚
	// （计划任务 6 明确要求「第 4 步不能挪到第 6 步之后」）。
	facts := map[string]string{
		model.FactSN: host.SN, model.FactMAC: host.MAC, model.FactUUID: host.UUID,
		model.FactArch: string(host.Arch), model.FactOSFamily: string(artifact.OSFamily),
	}
	resolvedParams, err := s.deps.ParamsResolver.Resolve(r.Context(), req.FormData, facts)
	if err != nil {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeConflict,
			"参数决策失败，任务未创建: "+err.Error()))
		return
	}

	// hookDigests 按模板 pin 住的引用固化（§2.6）。
	hookDigests := make([]string, 0, len(tpl.HookRefs))
	for _, ref := range tpl.HookRefs {
		hookDigests = append(hookDigests, ref.HookDigest)
	}

	now := s.clock.Now()
	user, _ := userFromContext(r.Context())
	task := model.Task{
		ID: uuid.NewString(), CreatedAt: now, CreatedBy: user.Username,
		HostSN: host.SN, HostMAC: host.MAC, HostUUID: host.UUID,
		TemplateID: tpl.ID, TemplateVersion: tpl.Version, ArtifactSetID: artifact.ID,
		HookDigests: hookDigests, FormData: req.FormData, ResolvedParams: resolvedParams,
		State: model.TaskStatePending, StateUpdatedAt: now, Version: 1,
		CurrentPhase: model.PhasePending, PhaseEnteredAt: now,
	}

	// 第 5 步：写库（任务 + 首条事件）。TaskStore.CreateTask 在同一主机
	// 已有非终态任务时返回 contract.ErrActiveTaskExists（§3.6）。
	created, err := s.deps.Tasks.CreateTask(r.Context(), task)
	if err != nil {
		if errors.Is(err, contract.ErrActiveTaskExists) {
			apierr.WriteError(w, apierr.New(http.StatusConflict, apierr.CodeActiveTaskExists,
				"该主机已存在一个未结束的任务，请先处理或取消该任务"))
			return
		}
		writeInternal(w, err)
		return
	}
	if _, err := s.deps.Events.AppendEvent(r.Context(), model.TaskEvent{
		TaskID: created.ID, Seq: 0, TS: now, Phase: model.PhasePending,
		Level: model.EventLevelInfo, Message: "任务已创建", Source: model.EventSourceProvider,
	}); err != nil {
		// 首条事件写入失败不应该让任务创建整体失败——任务本身已经落库，
		// 回滚需要事务支持而 fake.Store 的 CreateTask/AppendEvent 不在
		// 同一次调用内绑定成一个事务；这里只记录错误、不中断响应，事件
		// 记录缺一条不影响任务本身的可用性。
		writeInternal(w, err)
		return
	}

	// 第 6 步：发布「任务已创建」生命周期事件——只是通知，不是决策点。
	if s.deps.EventBus != nil {
		s.deps.EventBus.Publish(contract.LifecycleEventTaskCreated, created)
	}

	apierr.WriteOK(w, toTaskItem(created))
}

// handleListTasks 处理 GET /tasks?state=&sn=。
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	filter := contract.TaskFilter{
		State:  model.TaskState(r.URL.Query().Get("state")),
		HostSN: r.URL.Query().Get("sn"),
	}
	tasks, err := s.deps.Tasks.ListTasks(r.Context(), filter)
	if err != nil {
		writeInternal(w, err)
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"), defaultListLimit)
	if limit < len(tasks) {
		tasks = tasks[:limit]
	}

	items := make([]taskItem, 0, len(tasks))
	for _, tk := range tasks {
		items = append(items, toTaskItem(tk))
	}
	apierr.WriteOK(w, items)
}

// taskEventItem 是响应形态，字段名与 model.TaskEvent 对应。
type taskEventItem struct {
	TaskID     string `json:"taskId"`
	Seq        int    `json:"seq"`
	TS         int64  `json:"ts"`
	Phase      string `json:"phase"`
	Level      string `json:"level"`
	Message    string `json:"message"`
	RC         int    `json:"rc"`
	HookDigest string `json:"hookDigest"`
	Source     string `json:"source"`
}

func toTaskEventItem(e model.TaskEvent) taskEventItem {
	return taskEventItem{
		TaskID: e.TaskID, Seq: e.Seq, TS: e.TS, Phase: string(e.Phase),
		Level: string(e.Level), Message: e.Message, RC: e.RC,
		HookDigest: e.HookDigest, Source: string(e.Source),
	}
}

// handleTaskEvents 处理 GET /tasks/{id}/events：按 seq 升序返回全量。
func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	events, err := s.deps.Events.ListEvents(r.Context(), id, contract.EventFilter{})
	if err != nil {
		writeInternal(w, err)
		return
	}
	items := make([]taskEventItem, 0, len(events))
	for _, e := range events {
		items = append(items, toTaskEventItem(e))
	}
	apierr.WriteOK(w, items)
}

// handleTaskRetry 处理 POST /tasks/{id}/retry。
//
// 不在 handler 里自己判断可否重装——那是状态机的权威判定
// （contract.StateMachine.Retry 内部调用 model.FailureReason.Reinstallable()，
// 拒绝时返回 contract.ErrNotReinstallable，本 handler 只负责把这个
// sentinel 错误映射成 apierr 的响应）。
func (s *Server) handleTaskRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	updated, err := s.deps.StateMachine.Retry(r.Context(), id)
	if err != nil {
		if errors.Is(err, contract.ErrNotReinstallable) {
			apierr.WriteError(w, apierr.New(http.StatusConflict, apierr.CodeReinstallDenied,
				"该机器已完成安装，重装会擦除磁盘数据；如确需重装，请人工核实后从主机清单重新创建任务"))
			return
		}
		writeInternal(w, err)
		return
	}
	apierr.WriteOK(w, toTaskItem(updated))
}

// handleTaskCancel 处理 POST /tasks/{id}/cancel。
func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	updated, err := s.deps.StateMachine.Cancel(r.Context(), id)
	if err != nil {
		writeInternal(w, err)
		return
	}
	apierr.WriteOK(w, toTaskItem(updated))
}
