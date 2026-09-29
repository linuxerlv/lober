// 本文件实现模板四个端点：创建、更新（建新版本）、取表单 schema、
// preview。对应实现计划任务 5，依据 docs/contracts/http-console.md §5、
// SPEC §2.3、§8.3、§12.7。
package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/model"
)

// routesTemplate 注册模板四个端点。
//
// POST /templates、PUT /templates/{id} 仍用 handleFuncRole/roleAdmin——
// 这两个端点不在迁移契约 §4 冻结的 29 条列表里（契约缺口，server.go 的
// handleFuncRole 文档与本任务报告的 blockers 已记录），保留按角色卡门是
// 「不改任何业务语义」这条硬约束下唯一的选择。
//
// GET .../form-schema、POST .../preview 是契约 §4 冻结的两个端点，权限码
// 与审计动作列都是「—」，改用骨架新 API s.handle(method, path, "", "", h)——
// 与迁移前 s.handleFunc(method, path, "", "", h) 语义逐一对应（都是「只要
// 已登录，不要求权限码，不写审计」），只是骨架方法名从 handleFunc 换成了
// handle（契约 §5）。
func (s *Server) routesTemplate() {
	s.handleFuncRole("POST", "/templates", roleAdmin, "template.create", s.handleCreateTemplate)
	s.handleFuncRole("PUT", "/templates/:id", roleAdmin, "template.update", s.handleUpdateTemplate)
	s.handle("GET", "/templates/:id/form-schema", "", "", s.handleTemplateFormSchema)
	s.handle("POST", "/templates/:id/preview", "", "", s.handleTemplatePreview)
	// state 流转端点。**这个端点补的是一条断掉的链路**：模板只能被创建成
	// draft（handleCreateTemplate 硬编码），而 handleCreateTask 硬性要求
	// active，而契约原先冻结的四个模板端点里没有任何一个能改状态——于是
	// 任何模板都永远无法用于建任务。store 层的 UpdateTemplateState 一直
	// 完备，缺的只是这个调用点（scripts/e2e.sh 头部注释第 24-29 行记下了
	// 这件事，并用 db_template_active 直接写库绕过）。
	//
	// 权限用 roleAdmin，与 POST/PUT /templates 一致：激活一个模板等于让它
	// 可被用于擦盘装机，§9.4 把模板与 provider 并列为需代码评审级别的写
	// 权限。刻意**不**引入 hook 那样的双人复核——hook 是「一段任意 root
	// 代码」，模板是「选哪套制品、引哪些已批准的 hook」，后者引用的 hook
	// 本身已经过双人复核，再叠一层是重复确认（同 §0.2 删 armed 的理由）。
	s.handleFuncRole("POST", "/templates/:id/state", roleAdmin, "template.state", s.handleTemplateState)
}

// templateItem 是响应形态，字段名与 model.Template 对应。
type templateItem struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	ArtifactSetID      string        `json:"artifactSetId"`
	AnswerTemplate     string        `json:"answerTemplate"`
	HookRefs           []hookRefItem `json:"hookRefs"`
	KernelArgs         string        `json:"kernelArgs"`
	DiskPolicy         string        `json:"diskPolicy"`
	OnFailure          string        `json:"onFailure"`
	RequiredFormFields []string      `json:"requiredFormFields"`
	Version            int           `json:"version"`
	State              string        `json:"state"`
}

type hookRefItem struct {
	HookID         string            `json:"hookId"`
	HookDigest     string            `json:"hookDigest"`
	Stage          string            `json:"stage"`
	Order          int               `json:"order"`
	Required       bool              `json:"required"`
	ParamsDefaults map[string]string `json:"paramsDefaults"`
}

func toTemplateItem(tpl model.Template) templateItem {
	refs := make([]hookRefItem, 0, len(tpl.HookRefs))
	for _, r := range tpl.HookRefs {
		defaults := r.ParamsDefaults
		if defaults == nil {
			defaults = map[string]string{}
		}
		refs = append(refs, hookRefItem{
			HookID: r.HookID, HookDigest: r.HookDigest, Stage: string(r.Stage),
			Order: r.Order, Required: r.Required, ParamsDefaults: defaults,
		})
	}
	fields := tpl.RequiredFormFields
	if fields == nil {
		fields = []string{}
	}
	return templateItem{
		ID: tpl.ID, Name: tpl.Name, ArtifactSetID: tpl.ArtifactSetID,
		AnswerTemplate: tpl.AnswerTemplate, HookRefs: refs,
		KernelArgs: tpl.KernelArgs, DiskPolicy: string(tpl.DiskPolicy),
		OnFailure: string(tpl.OnFailure), RequiredFormFields: fields,
		Version: tpl.Version, State: string(tpl.State),
	}
}

// templateWriteRequest 是 POST /templates 与 PUT /templates/{id} 共用的
// 入参形态。
type templateWriteRequest struct {
	Name               string           `json:"name"`
	ArtifactSetID      string           `json:"artifactSetId"`
	AnswerTemplate     string           `json:"answerTemplate"`
	HookRefs           []hookRefRequest `json:"hookRefs"`
	KernelArgs         string           `json:"kernelArgs"`
	DiskPolicy         string           `json:"diskPolicy"`
	OnFailure          string           `json:"onFailure"`
	RequiredFormFields []string         `json:"requiredFormFields"`
}

type hookRefRequest struct {
	HookID         string            `json:"hookId"`
	HookDigest     string            `json:"hookDigest"`
	Stage          string            `json:"stage"`
	Order          int               `json:"order"`
	Required       bool              `json:"required"`
	ParamsDefaults map[string]string `json:"paramsDefaults"`
}

// resolveHookRefs 校验并补全模板的 hook_refs（§2.2 末段、§2.7）：
//   - hookDigest 由服务端按 hookId 查出该 hook 当前 active 版本的
//     bodySha256 自动填入（前端不传，或传了服务端也以查出的值为准）
//   - 阶段与 hook 声明的一致
//   - 家族兼容：hook 自己的家族列表为空表示不限，模板绑定了具体制品集，
//     此时才能判定
func (s *Server) resolveHookRefs(ctx context.Context, refs []hookRefRequest, artifactSetID string) ([]model.HookRef, *apierr.Error) {
	var artifactFamily model.OSFamily
	if artifactSetID != "" {
		set, err := s.deps.ArtifactSets.GetArtifactSet(ctx, artifactSetID)
		if err != nil {
			return nil, apierr.New(http.StatusNotFound, apierr.CodeNotFound, "artifactSetId 指向的制品集不存在")
		}
		artifactFamily = set.OSFamily
	}

	out := make([]model.HookRef, 0, len(refs))
	for _, ref := range refs {
		// 取该 hook 当前 active 版本（GetHook 的 version<=0 语义是「取最新
		// 版本」，但模板必须 pin 住 active 版本的内容——这里改用
		// ListHooks 过滤，确保拿到的是 active 状态而不是「最新但可能还是
		// draft」的版本）。
		hooks, err := s.deps.Hooks.ListHooks(ctx)
		if err != nil {
			return nil, apierr.New(http.StatusInternalServerError, apierr.CodeInternal, "查询 hook 失败")
		}
		var active *model.Hook
		for i := range hooks {
			if hooks[i].ID == ref.HookID && hooks[i].State == model.HookStateActive {
				if active == nil || hooks[i].Version > active.Version {
					active = &hooks[i]
				}
			}
		}
		if active == nil {
			return nil, apierr.New(http.StatusNotFound, apierr.CodeNotFound,
				fmt.Sprintf("hookId=%s 没有已激活（active）的版本，无法引用", ref.HookID))
		}

		if ref.Stage != "" && ref.Stage != string(active.Stage) {
			return nil, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest,
				fmt.Sprintf("hookId=%s 引用声明的 stage=%q 与该 hook 实际声明的 stage=%q 不一致",
					ref.HookID, ref.Stage, active.Stage))
		}
		if artifactSetID != "" && !active.SupportsFamily(artifactFamily) {
			return nil, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest,
				fmt.Sprintf("hookId=%s 不支持制品集的 OS 家族 %q（家族限制在这里才能判定："+
					"hook 自己的家族列表为空表示不限，模板绑定了具体制品集时才有判定基准）",
					ref.HookID, artifactFamily))
		}

		out = append(out, model.HookRef{
			HookID: ref.HookID, HookDigest: active.BodySHA256, Stage: active.Stage,
			Order: ref.Order, Required: ref.Required, ParamsDefaults: ref.ParamsDefaults,
		})
	}
	return out, nil
}

// bindTemplateWriteRequest 用 gin 的 ShouldBindJSON 解析
// POST /templates、PUT /templates/{id} 共用的请求体，返回一个可以直接
// 交给 apierr.Fail 的 error（迁移契约 §3：Fail 认得 validator.ValidationErrors
// 与 *apierr.Error 两种）。
//
// templateWriteRequest 目前没有任何 binding 标签，ShouldBindJSON 唯一
// 可能失败的路径是请求体本身不是合法 JSON（decodeJSON 内部先解码、解码
// 成功后才跑 validate()，见 gin binding/json.go）——因此这里直接判定
// 「非 validator.ValidationErrors 即视为解析失败」，用固定文案回 400，
// 与迁移前 json.NewDecoder(r.Body).Decode 出错时的响应码、文案完全一致
// （迁移契约 §2：不改任何观察得到的错误响应内容）。
func bindTemplateWriteRequest(c *gin.Context) (templateWriteRequest, error) {
	var req templateWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			return req, err
		}
		return req, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest, "请求体不是合法的 JSON")
	}
	return req, nil
}

// handleCreateTemplate 处理 POST /templates。
func (s *Server) handleCreateTemplate(c *gin.Context) {
	req, err := bindTemplateWriteRequest(c)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	// strings.TrimSpace 判空留在 Go 代码里，不改用 binding:"required"——
	// 该标签只判断零值，"   " 这种全空白字符串不是零值、不会被标签拦下，
	// 换成标签会悄悄弱化这条校验（迁移契约 §7：标签表达不了的校验条件
	// 不能因为「换成标签」就放弱）。
	if strings.TrimSpace(req.Name) == "" {
		apierr.Fail(c, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest, "name 不能为空"))
		return
	}

	refs, verr := s.resolveHookRefs(c.Request.Context(), req.HookRefs, req.ArtifactSetID)
	if verr != nil {
		apierr.Fail(c, verr)
		return
	}

	tpl := model.Template{
		ID: uuid.NewString(), Name: req.Name, ArtifactSetID: req.ArtifactSetID,
		AnswerTemplate: req.AnswerTemplate, HookRefs: refs, KernelArgs: req.KernelArgs,
		DiskPolicy: model.DiskPolicy(req.DiskPolicy), OnFailure: model.OnFailurePolicy(req.OnFailure),
		RequiredFormFields: req.RequiredFormFields, Version: 1, State: model.TemplateStateDraft,
	}
	created, err := s.deps.Templates.CreateTemplate(c.Request.Context(), tpl)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	apierr.OK(c, toTemplateItem(created))
}

// handleUpdateTemplate 处理 PUT /templates/{id}：建新版本。
func (s *Server) handleUpdateTemplate(c *gin.Context) {
	id := c.Param("id")
	current, err := s.deps.Templates.GetTemplate(c.Request.Context(), id, 0)
	if err != nil {
		apierr.Fail(c, err)
		return
	}

	req, err := bindTemplateWriteRequest(c)
	if err != nil {
		apierr.Fail(c, err)
		return
	}

	refs, verr := s.resolveHookRefs(c.Request.Context(), req.HookRefs, req.ArtifactSetID)
	if verr != nil {
		apierr.Fail(c, verr)
		return
	}

	next := model.Template{
		ID: id, Name: req.Name, ArtifactSetID: req.ArtifactSetID,
		AnswerTemplate: req.AnswerTemplate, HookRefs: refs, KernelArgs: req.KernelArgs,
		DiskPolicy: model.DiskPolicy(req.DiskPolicy), OnFailure: model.OnFailurePolicy(req.OnFailure),
		RequiredFormFields: req.RequiredFormFields, Version: current.Version + 1, State: model.TemplateStateDraft,
	}
	created, err := s.deps.Templates.CreateTemplate(c.Request.Context(), next)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	apierr.OK(c, toTemplateItem(created))
}

// fixedFormFields 是表单 schema 三段聚合的第一段：固定字段（§8.3）。
// sn 字段标记 x-widget=host-select（不是自由文本输入，§4.3 硬要求）。
func fixedFormFieldsSchema() map[string]json.RawMessage {
	raw := `{
		"sn": {"type": "string", "title": "序列号", "x-widget": "host-select"},
		"os": {"type": "string", "title": "操作系统家族"},
		"version": {"type": "string", "title": "版本"},
		"hostname": {"type": "string", "title": "主机名"},
		"bizIp": {"type": "string", "title": "业务 IP", "x-widget": "ip"}
	}`
	var fields map[string]json.RawMessage
	// 固定字面量，解析失败即代码本身有 bug，用 panic 让测试直接暴露，
	// 不需要对调用方返回 error（这不是运行时可能失败的路径）。
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		panic("console: fixedFormFieldsSchema 内嵌 JSON 解析失败: " + err.Error())
	}
	return fields
}

// requiredFieldsSchema 把 template.RequiredFormFields 转换成第二段聚合
// 片段：每个字段声明为必填字符串，无更多约束（模板作者只声明「需要这个
// 字段」，具体校验规则交给对应 hook 的 params_schema 或前端的通用类型
// 推断）。
func requiredFieldsSchema(fields []string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(fields))
	for _, f := range fields {
		out[f] = json.RawMessage(`{"type": "string", "required": true}`)
	}
	return out
}

// buildFormSchema 聚合三段并校验落在 §12.7 子集内（计划任务 5：聚合结果
// 必须在子集内，用一个超出子集的 hook schema 验证被拒）。
func buildFormSchema(tpl model.Template, hooks []model.Hook) (map[string]json.RawMessage, error) {
	fixed := fixedFormFieldsSchema()
	required := requiredFieldsSchema(tpl.RequiredFormFields)

	hookSchemas := make([]map[string]json.RawMessage, 0, len(tpl.HookRefs))
	for _, ref := range tpl.HookRefs {
		for _, h := range hooks {
			if h.BodySHA256 == ref.HookDigest {
				fields, err := parseParamsSchemaFields(h.ParamsSchema)
				if err != nil {
					return nil, fmt.Errorf("hook %s 的 params_schema 解析失败: %w", h.ID, err)
				}
				hookSchemas = append(hookSchemas, fields)
				break
			}
		}
	}

	merged := mergeParamsSchemas(append([]map[string]json.RawMessage{fixed, required}, hookSchemas...)...)
	for name, raw := range merged {
		if err := validateFieldSchema(raw); err != nil {
			return nil, fmt.Errorf("聚合后的字段 %q 超出 §12.7 子集: %w", name, err)
		}
	}
	return merged, nil
}

// handleTemplateFormSchema 处理 GET /templates/{id}/form-schema。
func (s *Server) handleTemplateFormSchema(c *gin.Context) {
	id := c.Param("id")
	tpl, err := s.deps.Templates.GetTemplate(c.Request.Context(), id, 0)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	hooks, err := s.deps.Hooks.ListHooks(c.Request.Context())
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	merged, err := buildFormSchema(tpl, hooks)
	if err != nil {
		// 聚合失败视为服务端内部状态异常（500/CodeInternal）：走到这里说明
		// 某个 active hook 的 params_schema 超出 §12.7 子集，本该在 hook
		// 注册期就被拦下（jsonschema.go 的 validateParamsSchemaSubset），
		// 不是这次请求的输入本身有问题，因此不用 400/404。
		apierr.Fail(c, apierr.New(http.StatusInternalServerError, apierr.CodeInternal, err.Error()))
		return
	}
	// 本端点「不套 {code,data,message} 之外的额外包装」——data 本身就是
	// merged，与其它端点一样走 apierr.OK，data 字段的值就是可直接喂给
	// 前端 schema-to-form 映射层的 JSON Schema，没有再嵌一层。
	apierr.OK(c, merged)
}

// previewRequest 是 POST /templates/{id}/preview 的入参。
type previewRequest struct {
	HostID   string            `json:"hostId"`
	FormData map[string]string `json:"formData"`
}

// previewResult 是响应形态。
type previewResult struct {
	AnswerFile string   `json:"answerFile"`
	BootMenu   string   `json:"bootMenu"`
	Warnings   []string `json:"warnings"`
}

// fakePreviewHost 是 preview 端点在未指定 hostId 时使用的内置假主机
// （固定形态，计划任务 5 要求：两次 preview 的差异只来自模板改动）。
var fakePreviewHost = model.Host{
	SN: "PREVIEW-SN-000001", MAC: "aa:bb:cc:dd:ee:ff", UUID: "00000000-0000-0000-0000-000000000000",
	Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI, Rack: "PREVIEW-RACK",
}

// handleTemplatePreview 处理 POST /templates/{id}/preview：用假数据构造
// 渲染上下文调渲染器，返回应答文件与引导菜单两段纯文本。不建任务、不签
// token、不落库（计划任务 5 明确要求）。
func (s *Server) handleTemplatePreview(c *gin.Context) {
	id := c.Param("id")
	tpl, err := s.deps.Templates.GetTemplate(c.Request.Context(), id, 0)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	artifact, err := s.deps.ArtifactSets.GetArtifactSet(c.Request.Context(), tpl.ArtifactSetID)
	if err != nil {
		apierr.Fail(c, err)
		return
	}

	var req previewRequest
	_ = c.ShouldBindJSON(&req) // preview 入参整体可选，解析失败按空 body 处理，与迁移前行为一致

	// 修复：这里曾直接调用 HostStore.GetHost(req.HostID)——但 req.HostID
	// 是前端从 GET /hosts 拿到的暴露 id（hostID() 算出的裸 SN/MAC/UUID
	// 值），而 GetHost 的 id 取法「由实现决定」（contract.HostStore 文档
	// 明确说明），fake.Store 的具体实现用带前缀的内部 key，两者不是同一
	// 个字符串，导致真实存在的主机永远查不到、静默退回 fakePreviewHost，
	// 前端完全无法察觉自己看到的是假数据。改用 findHostByExposedID——与
	// host.go/task.go 同一套反查逻辑，只依赖 contract 文档化的
	// identityMatches 语义，不依赖任何具体 Store 实现的内部 key 格式。
	host := fakePreviewHost
	if req.HostID != "" {
		if h, err := s.findHostByExposedID(c.Request.Context(), req.HostID); err == nil {
			host = h
		}
	}

	// preview 假数据：固定的假 SN/IP/token，保证两次 preview 的差异只来自
	// 模板改动本身（计划任务 5 明确要求）。
	ctx := model.RenderContext{
		Task: model.Task{
			ID: "preview-task", HostSN: host.SN, HostMAC: host.MAC, HostUUID: host.UUID,
			TemplateID: tpl.ID, TemplateVersion: tpl.Version, ArtifactSetID: artifact.ID,
			FormData: req.FormData, ResolvedParams: req.FormData,
		},
		Template: tpl, Artifact: artifact, Host: host,
		Hooks:         sortedHookRefsForPreview(tpl),
		Facts:         map[string]string{},
		InstallToken:  "preview-install-token",
		FinalizeToken: "preview-finalize-token",
		// 部署级配置值，与 gateway.buildRenderContext 取自同一处
		// （Deps.RootSSHPublicKeys）。渲染器对空值硬拒绝，不传这一项
		// preview 会对任何模板都渲染失败（SPEC §1.5）。
		RootSSHPublicKeys: s.deps.RootSSHPublicKeys,
		ActualFirmware:    host.Firmware,
	}

	answerFileName, ok := ctx.AnswerFileName()
	if !ok {
		apierr.Fail(c, apierr.New(http.StatusInternalServerError, apierr.CodeInternal,
			"该制品集的 OS 家族未知，无法确定应答文件名"))
		return
	}

	answerFile, err := s.deps.Renderer.RenderAnswerFile(ctx, answerFileName)
	if err != nil {
		apierr.Fail(c, apierr.New(http.StatusInternalServerError, apierr.CodeInternal, "渲染应答文件失败: "+err.Error()))
		return
	}
	bootMenu, err := s.deps.Renderer.RenderMenu(ctx)
	if err != nil {
		apierr.Fail(c, apierr.New(http.StatusInternalServerError, apierr.CodeInternal, "渲染引导菜单失败: "+err.Error()))
		return
	}

	apierr.OK(c, previewResult{AnswerFile: answerFile, BootMenu: bootMenu, Warnings: []string{}})
}

// sortedHookRefsForPreview 汇总模板全部阶段的 hook 引用，按阶段+顺序排好
// （渲染器不再自己排序，排序逻辑收敛在 Template.HooksForStage，这里遍历
// 全部四个阶段调用它，拼成一份完整列表）。
func sortedHookRefsForPreview(tpl model.Template) []model.HookRef {
	stages := []model.HookStage{
		model.HookStagePre, model.HookStagePostNoChroot, model.HookStagePost, model.HookStageFirstboot,
	}
	var out []model.HookRef
	for _, st := range stages {
		out = append(out, tpl.HooksForStage(st)...)
	}
	return out
}

// templateStateRequest 是 POST /templates/{id}/state 的入参。
//
// version 必填且参与乐观锁：两个人同时看着同一个 draft 模板，一个先改了
// 内容（version 前进），另一个拿着旧 version 点激活——静默放行会让「被
// 激活的那一版」不是点击者看到的那一版，而模板内容决定分区方案与 hook
// 链，这不是可以将错就错的东西。
type templateStateRequest struct {
	State   string `json:"state"`
	Version int    `json:"version"`
}

// handleTemplateState 处理 POST /templates/{id}/state：模板状态流转。
//
// 只认 model 里已定义的三个状态值，不做「未知值原样写入」的兜底：
// handleCreateTask 用 `!= active` 判定可用性，一个拼错的状态值
// （"activ"）会让模板永久不可用，且没有任何地方报错——这种失败查起来
// 要先怀疑权限、怀疑缓存，最后才会去看那一列的字面值。
func (s *Server) handleTemplateState(c *gin.Context) {
	id := c.Param("id")

	var req templateStateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Fail(c, bindJSONError(err))
		return
	}

	next, ok := parseTemplateState(req.State)
	if !ok {
		apierr.Fail(c, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest,
			fmt.Sprintf("state 只能是 draft / active / deprecated 之一，收到 %q", req.State)))
		return
	}

	// 先取一次确认模板存在：UpdateTemplateState 命中 0 行时无法区分
	// 「模板不存在」与「version 不匹配」，而这两种情形对调用方的含义完全
	// 不同（前者是 404 该去检查 ID，后者是 409 该重新加载后再试）。
	current, err := s.deps.Templates.GetTemplate(c.Request.Context(), id, 0)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	if current.Version != req.Version {
		apierr.Fail(c, apierr.New(http.StatusConflict, apierr.CodeConflict,
			fmt.Sprintf("模板当前版本是 %d，请求里的 version 是 %d——"+
				"期间它被改过，请重新加载后再流转", current.Version, req.Version)))
		return
	}

	updated, err := s.deps.Templates.UpdateTemplateState(c.Request.Context(), id, req.Version, next)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	apierr.OK(c, toTemplateItem(updated))
}

// parseTemplateState 把入参字符串映射成 model.TemplateState，只认三个已定义
// 的值（区分大小写、不 trim：容忍 "ACTIVE" 或 "draft " 会让调用方以为这层
// 有归一化，而数据库里存的到底是哪种形态就变得不可预测）。
func parseTemplateState(raw string) (model.TemplateState, bool) {
	switch model.TemplateState(raw) {
	case model.TemplateStateDraft, model.TemplateStateActive, model.TemplateStateDeprecated:
		return model.TemplateState(raw), true
	default:
		return "", false
	}
}
