// 本文件实现 hook 八个端点：创建、列表、更新（建新版本）、校验、列版本、
// 审批、驳回、审计查询。对应实现计划任务 4，依据 docs/contracts/http-console.md
// §4、SPEC §2.2、§7.5、§9.4、§12.7；驳回与审计查询是本次契约变更新增
// （见 docs/contracts/http-console.md「变更记录」）。
package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// routesHook 注册 hook 八个端点（gin 路径参数语法 :id，对外 URL 不变，
// 见迁移契约 §4/§5：gin 的 :param 只是路由声明语法，不改 URL 本身）。
func (s *Server) routesHook() {
	s.handle("POST", "/hooks", codeHookWrite, "hook.create", s.handleCreateHook)
	s.handle("GET", "/hooks", "", "", s.handleListHooks)
	s.handle("PUT", "/hooks/:id", codeHookWrite, "hook.update", s.handleUpdateHook)
	s.handle("POST", "/hooks/:id/validate", codeHookWrite, "hook.validate", s.handleValidateHook)
	s.handle("GET", "/hooks/:id/versions", "", "", s.handleHookVersions)
	// approve 的权限细分在 handler 内部按当前状态判断（draft→review 允许
	// 作者自己发起，review→active 需要 hook:approve 且审批人不能等于
	// 作者），不在路由层用单一权限码卡死。
	s.handle("POST", "/hooks/:id/approve", "", "hook.approve", s.handleApproveHook)
	// reject 与 approve 对称：权限判定（hook:approve 且审批人不能等于
	// 作者）与状态校验（只有 review 能被驳回）都在 handler 内部完成，
	// 同样不在路由层用单一权限码卡死（本次契约变更，见
	// docs/contracts/http-console.md「变更记录」）。
	s.handle("POST", "/hooks/:id/reject", "", "hook.reject", s.handleRejectHook)
	// audit 端点（本次契约变更新增，端点编号 29，见
	// docs/contracts/http-console.md「变更记录」）：权限是「hook:write 或
	// hook:approve 或 admin」三者之一，不是单一权限码，因此同样不在路由层
	// 用 codeXxx 卡死，判定逻辑在 handleHookAudit 内部完成。
	s.handle("GET", "/hooks/:id/audit", "", "", s.handleHookAudit)
}

// hookItem 是响应形态，字段名与 model.Hook 对应。
type hookItem struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Stage        string           `json:"stage"`
	OSFamilies   []string         `json:"osFamilies"`
	Interpreter  string           `json:"interpreter"`
	Body         string           `json:"body"`
	BodySHA256   string           `json:"bodySha256"`
	ParamsSchema string           `json:"paramsSchema"`
	Requires     hookRequiresItem `json:"requires"`
	Required     bool             `json:"required"`
	TimeoutSec   int              `json:"timeoutSec"`
	Version      int              `json:"version"`
	State        string           `json:"state"`
	Author       string           `json:"author"`
	Approver     string           `json:"approver"`
	ApprovedAt   int64            `json:"approvedAt"`
}

type hookRequiresItem struct {
	ContractVersion string   `json:"contractVersion"`
	Facts           []string `json:"facts"`
	Capabilities    []string `json:"capabilities"`
}

func toHookItem(h model.Hook) hookItem {
	families := make([]string, 0, len(h.OSFamilies))
	for _, f := range h.OSFamilies {
		families = append(families, string(f))
	}
	facts := h.Requires.Facts
	if facts == nil {
		facts = []string{}
	}
	caps := h.Requires.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return hookItem{
		ID:           h.ID,
		Name:         h.Name,
		Description:  h.Description,
		Stage:        string(h.Stage),
		OSFamilies:   families,
		Interpreter:  h.Interpreter,
		Body:         h.Body,
		BodySHA256:   h.BodySHA256,
		ParamsSchema: h.ParamsSchema,
		Requires: hookRequiresItem{
			ContractVersion: h.Requires.ContractVersion,
			Facts:           facts,
			Capabilities:    caps,
		},
		Required:   h.Required,
		TimeoutSec: h.TimeoutSec,
		Version:    h.Version,
		State:      string(h.State),
		Author:     h.Author,
		Approver:   h.Approver,
		ApprovedAt: h.ApprovedAt,
	}
}

// hookWriteRequest 是 POST /hooks 与 PUT /hooks/{id} 共用的入参形态
// （契约：除 id/bodySha256/version/state/approver/approvedAt 外的全部
// 字段）。
//
// 校验标签迁移说明（迁移契约 §7）：本结构体对应的手写校验点原有四项
// （registrationValidate），其中只有 Stage 能安全换成 binding 标签——
// model.HookStage.Valid() 的合法取值是编译期固定的四个字符串常量，
// oneof 标签能一字不差地表达同一条件（已用 apierr 包内的验证器实例
// 跑过 oneof 对空串同样报错，不需要额外叠加 required）。其余三项
// 留在 registrationValidate 里用 Go 代码做，理由逐条如下：
//   - Name：原校验对空白字符做 TrimSpace 后判空，binding:"required"
//     只认零值（""），"   " 这种纯空白输入会被 required 放过而被原校验
//     拦下——加标签等于悄悄放宽这条校验，因此不加。
//   - Requires.Facts：每个元素是否属于 model.IsFactV1 是运行时查一个
//     24 项集合（未来会增长），不是编译期固定的字面量枚举，标签表达
//     不了。
//   - Requires.ContractVersion：要过 checkContractVersion 解析
//     ">=1" 之类的比较表达式，标签语法不支持。
//   - ParamsSchema：要过 validateParamsSchemaSubset 解析 JSON Schema
//     子集，标签语法不支持。
//
// Stage 换成标签后，同一条件不再经过 registrationValidate 里的
// model.HookStage(req.Stage).Valid() 分支（该分支因此对本结构体的调用
// 路径而言是不可达的冗余判断，但函数本身仍被其它场景需要的校验项
// 共用，不单独拆分）。
type hookWriteRequest struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	Stage        string              `json:"stage" binding:"oneof=pre post-nochroot post firstboot"`
	OSFamilies   []string            `json:"osFamilies"`
	Interpreter  string              `json:"interpreter"`
	Body         string              `json:"body"`
	ParamsSchema string              `json:"paramsSchema"`
	Requires     hookRequiresRequest `json:"requires"`
	Required     bool                `json:"required"`
	TimeoutSec   int                 `json:"timeoutSec"`
}

type hookRequiresRequest struct {
	ContractVersion string   `json:"contractVersion"`
	Facts           []string `json:"facts"`
	Capabilities    []string `json:"capabilities"`
}

// registrationValidate 执行计划任务 4 的四项注册期校验，把运行时意外
// 提前为注册时错误（§2.2、§7.5）：
//  1. 阶段是白名单内的值（gin 迁移后已改由 hookWriteRequest.Stage 的
//     binding 标签在 ShouldBindJSON 阶段拦下，见该字段的文档注释；本
//     函数内的分支因此对经过 ShouldBindJSON 的调用路径不可达，保留
//     不删是因为下面三项校验仍需要它，拆开反而增加认知负担）
//  2. requires 声明的 fact 键都在 v1 契约内
//  3. contract 版本要求能被满足
//  4. params_schema 在 §12.7 的子集内
//
// 返回 *apierr.Error，可直接传给 apierr.Fail。
//
// HTTP 状态码：本函数四个失败分支在 gin 迁移前一律传 http.StatusOK
// （200），与迁移契约 §2 冻结的错误码表（CodeBadRequest→400、
// CodeValidationFailed→422）不一致——那是过期行为，不是本次要保留的
// 业务语义（契约 §0：后端固定 200 是过期认知，迁移目的正是让状态码
// 开始反映真实语义）。这里改成契约表冻结的真实状态码，与 rbac.go 的
// requireRole/requireCode 把 CodeForbidden 从 200 改成 403 是同一处理
// 方式、同一条依据，不是本 agent 自行变更业务语义。
func registrationValidate(req hookWriteRequest) *apierr.Error {
	if strings.TrimSpace(req.Name) == "" {
		return apierr.New(http.StatusBadRequest, apierr.CodeBadRequest, "name 不能为空")
	}
	if !model.HookStage(req.Stage).Valid() {
		return apierr.New(http.StatusUnprocessableEntity, apierr.CodeValidationFailed,
			fmt.Sprintf("stage 取值 %q 不合法，必须是 pre/post-nochroot/post/firstboot 之一", req.Stage))
	}
	for _, f := range req.Requires.Facts {
		if !model.IsFactV1(f) {
			return apierr.New(http.StatusUnprocessableEntity, apierr.CodeValidationFailed,
				fmt.Sprintf("requires.facts 声明了不存在的 fact 键 %q（v1 契约不承诺这个键，"+
					"该 hook 运行时会读到空值）", f))
		}
	}
	if err := checkContractVersion(req.Requires.ContractVersion, model.FactContractVersion); err != nil {
		return apierr.New(http.StatusUnprocessableEntity, apierr.CodeValidationFailed, err.Error())
	}
	if err := validateParamsSchemaSubset(req.ParamsSchema); err != nil {
		return apierr.New(http.StatusUnprocessableEntity, apierr.CodeValidationFailed,
			"params_schema 超出 §12.7 定义的 JSON Schema 子集: "+err.Error())
	}
	return nil
}

// bindHookJSON 包一层 c.ShouldBindJSON，把「请求体本身不是合法 JSON／
// 空体」与「JSON 合法但 binding 标签校验不过」区分成两种不同的响应：
// 前者退化成 *apierr.Error（400 + CodeBadRequest，文案与 gin 迁移前的
// json.NewDecoder(r.Body).Decode 失败分支一致），后者原样把
// validator.ValidationErrors 传给调用方，让 apierr.Fail 走 422 +
// CodeValidationFailed 分支翻译成中文。
//
// 存在的理由：apierr.Fail/resolveFailure 只认识
// validator.ValidationErrors、*apierr.Error、已知 sentinel 三类错误，
// c.ShouldBindJSON 在请求体不是合法 JSON 或为空时返回的是
// *json.SyntaxError 或裸的 io.EOF——两者都不属于这三类，若不在此处
// 拦截会直接落进 resolveFailure 的「未识别错误」分支，被错误地映射成
// 500 + CodeInternal（把「客户端发错了 JSON」误判成「服务端内部错误」，
// 状态码类别从 4xx 变成 5xx，且原本给运维的明确文案「请求体不是合法的
// JSON」被替换成通用的「服务器内部错误」，运维定位不到问题出在客户端）。
// 迁移前 hook.go 三处 json.NewDecoder(r.Body).Decode 失败都回 400 类
// 错误，这里必须保住同一分类，不能因为换了 binding 库就退化成 500。
func bindHookJSON(c *gin.Context, out any) error {
	if err := c.ShouldBindJSON(out); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			return err
		}
		return apierr.New(http.StatusBadRequest, apierr.CodeBadRequest, "请求体不是合法的 JSON")
	}
	return nil
}

// handleCreateHook 处理 POST /hooks。
func (s *Server) handleCreateHook(c *gin.Context) {
	var req hookWriteRequest
	if err := bindHookJSON(c, &req); err != nil {
		apierr.Fail(c, err)
		return
	}
	if verr := registrationValidate(req); verr != nil {
		apierr.Fail(c, verr)
		return
	}

	user, _ := userFromContext(c.Request.Context())
	families := make([]model.OSFamily, 0, len(req.OSFamilies))
	for _, f := range req.OSFamilies {
		families = append(families, model.OSFamily(f))
	}

	hook := model.Hook{
		ID:           uuid.NewString(),
		Name:         req.Name,
		Description:  req.Description,
		Stage:        model.HookStage(req.Stage),
		OSFamilies:   families,
		Interpreter:  req.Interpreter,
		Body:         req.Body,
		BodySHA256:   model.HookDigest(req.Body),
		ParamsSchema: req.ParamsSchema,
		Requires: model.HookRequires{
			ContractVersion: req.Requires.ContractVersion,
			Facts:           req.Requires.Facts,
			Capabilities:    req.Requires.Capabilities,
		},
		Required:   req.Required,
		TimeoutSec: req.TimeoutSec,
		Version:    1,
		State:      model.HookStateDraft,
		Author:     user.Username,
	}

	created, err := s.deps.Hooks.CreateHook(c.Request.Context(), hook)
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}
	apierr.OK(c, toHookItem(created))
}

// handleListHooks 处理 GET /hooks?stage=&os_family=，均为精确匹配过滤
// （不做子串搜索）。os_family 匹配 osFamilies 数组包含该值，或该 hook 的
// osFamilies 为空（model.Hook.SupportsFamily 语义）。
func (s *Server) handleListHooks(c *gin.Context) {
	hooks, err := s.deps.Hooks.ListHooks(c.Request.Context())
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}

	stage := c.Query("stage")
	osFamily := c.Query("os_family")

	items := make([]hookItem, 0, len(hooks))
	for _, h := range hooks {
		if stage != "" && string(h.Stage) != stage {
			continue
		}
		if osFamily != "" && !h.SupportsFamily(model.OSFamily(osFamily)) {
			continue
		}
		items = append(items, toHookItem(h))
	}
	apierr.OK(c, items)
}

// handleUpdateHook 处理 PUT /hooks/{id}：建新版本，不改旧版本（hook 正文
// 不可变，§2.2、§9.4）。
func (s *Server) handleUpdateHook(c *gin.Context) {
	id := c.Param("id")

	current, err := s.deps.Hooks.GetHook(c.Request.Context(), id, 0)
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}

	var req hookWriteRequest
	if err := bindHookJSON(c, &req); err != nil {
		apierr.Fail(c, err)
		return
	}
	if verr := registrationValidate(req); verr != nil {
		apierr.Fail(c, verr)
		return
	}

	user, _ := userFromContext(c.Request.Context())
	families := make([]model.OSFamily, 0, len(req.OSFamilies))
	for _, f := range req.OSFamilies {
		families = append(families, model.OSFamily(f))
	}

	next := model.Hook{
		ID:           id,
		Name:         req.Name,
		Description:  req.Description,
		Stage:        model.HookStage(req.Stage),
		OSFamilies:   families,
		Interpreter:  req.Interpreter,
		Body:         req.Body,
		BodySHA256:   model.HookDigest(req.Body),
		ParamsSchema: req.ParamsSchema,
		Requires: model.HookRequires{
			ContractVersion: req.Requires.ContractVersion,
			Facts:           req.Requires.Facts,
			Capabilities:    req.Requires.Capabilities,
		},
		Required:   req.Required,
		TimeoutSec: req.TimeoutSec,
		Version:    current.Version + 1,
		State:      model.HookStateDraft,
		Author:     user.Username,
	}

	created, err := s.deps.Hooks.CreateHook(c.Request.Context(), next)
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}
	apierr.OK(c, toHookItem(created))
}

// handleHookVersions 处理 GET /hooks/{id}/versions：该 id 对应 hook 的
// 全部历史版本，按 version 升序。
//
// contract.HookStore 没有「按 id 列出全部版本」的方法（ListHooks 返回
// 全部 hook 的全部版本，GetHook 按单个 version 查）——本 handler 用
// ListHooks 取回后在 Go 里过滤，规模是十位数量级的 hook、每个 hook 版本
// 数是个位数，遍历代价可忽略。
func (s *Server) handleHookVersions(c *gin.Context) {
	id := c.Param("id")
	all, err := s.deps.Hooks.ListHooks(c.Request.Context())
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}
	var versions []model.Hook
	for _, h := range all {
		if h.ID == id {
			versions = append(versions, h)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version < versions[j].Version })

	items := make([]hookItem, 0, len(versions))
	for _, h := range versions {
		items = append(items, toHookItem(h))
	}
	apierr.OK(c, items)
}

// hookValidateResult 是 POST /hooks/{id}/validate 的响应形态。
type hookValidateResult struct {
	Passed bool        `json:"passed"`
	Issues []hookIssue `json:"issues"`
}

type hookIssue struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// syntaxCheckTimeout 是 bash -n 子进程调用的超时保护（计划任务 4 明确
// 要求「要设超时」）——语法检查是运维在等待响应的同步调用，卡住的子
// 进程不能无限期占用请求。
const syntaxCheckTimeout = 5 * time.Second

// lober 变量名的正则：匹配 $LOBER_XXX 或 ${LOBER_XXX} 形式。
var loberVarPattern = regexp.MustCompile(`\$\{?(LOBER_[A-Z0-9_]+)\}?`)

// handleValidateHook 处理 POST /hooks/{id}/validate。
//
// 校验内容（计划任务 4、SPEC §2.2）：
//   - bash -n（或对应 interpreter 的语法检查）；PowerShell 与 ash 在
//     MVP 下跳过，响应里说明原因
//   - requires.facts 是否为 model.IsFactV1 承诺的键
//   - paramsSchema 是否落在 §12.7 子集内
//   - 静态检查：脚本引用了某个 LOBER_ 变量但既不在 schema 也不在 fact
//     命名空间中 → 警告
func (s *Server) handleValidateHook(c *gin.Context) {
	id := c.Param("id")
	hook, err := s.deps.Hooks.GetHook(c.Request.Context(), id, 0)
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}

	var issues []hookIssue

	for _, f := range hook.Requires.Facts {
		if !model.IsFactV1(f) {
			issues = append(issues, hookIssue{Level: "error",
				Message: fmt.Sprintf("requires.facts 声明了不存在的 fact 键 %q", f)})
		}
	}

	if err := validateParamsSchemaSubset(hook.ParamsSchema); err != nil {
		issues = append(issues, hookIssue{Level: "error",
			Message: "params_schema 超出 §12.7 子集: " + err.Error()})
	}

	switch hook.Interpreter {
	case "bash":
		if msg, ok := bashSyntaxCheck(c.Request.Context(), hook.Body); !ok {
			issues = append(issues, hookIssue{Level: "error", Message: "bash -n 语法检查未通过: " + msg})
		}
	case "powershell", "ash":
		issues = append(issues, hookIssue{Level: "warn",
			Message: fmt.Sprintf("interpreter=%s 在 MVP 阶段跳过语法检查（未实现对应检查器），"+
				"本结果不代表脚本语法已确认无误", hook.Interpreter)})
	default:
		issues = append(issues, hookIssue{Level: "warn",
			Message: fmt.Sprintf("未知 interpreter %q，跳过语法检查", hook.Interpreter)})
	}

	// 静态变量名检查（产出警告，不阻止注册/校验通过）。
	schemaFields, err := parseParamsSchemaFields(hook.ParamsSchema)
	if err != nil {
		// schema 本身解析失败已经在上面的 validateParamsSchemaSubset 报过
		// 错误，这里静默跳过静态检查（避免重复报告同一处问题）。
		schemaFields = map[string]json.RawMessage{}
	}
	for _, name := range referencedLoberVars(hook.Body) {
		if name == model.FactContractVersionKey {
			continue
		}
		_, inSchema := schemaFields[strings.TrimPrefix(strings.ToLower(name), "lober_")]
		inFacts := model.IsFactV1(name)
		if !inSchema && !inFacts {
			issues = append(issues, hookIssue{Level: "warn",
				Message: fmt.Sprintf("脚本引用了 $%s，但它既不在 params_schema 中声明，"+
					"也不是 fact 命名空间内的键——可能是拼写错误", name)})
		}
	}

	passed := true
	for _, iss := range issues {
		if iss.Level == "error" {
			passed = false
			break
		}
	}
	if issues == nil {
		issues = []hookIssue{}
	}
	apierr.OK(c, hookValidateResult{Passed: passed, Issues: issues})
}

// bashSyntaxCheck 用 `bash -n` 检查脚本正文语法，带超时保护。ok 为 false
// 时 msg 携带具体的语法错误信息（stderr 内容）。
//
// 找不到 bash 可执行文件（如在没有 bash 的纯 Windows 环境测试运行）时，
// 也返回 ok=false 并在消息里说明原因——不能把「环境没有 bash」误报成
// 「脚本语法有误」，但两者对调用方（hook 作者）的处置是一样的：这条校验
// 现在给不出确定结论。
func bashSyntaxCheck(ctx context.Context, body string) (msg string, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, syntaxCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-n", "-c", body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "语法检查超时（" + syntaxCheckTimeout.String() + "），可能是脚本内容异常或环境问题", false
		}
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return "本环境找不到 bash 可执行文件，无法确认语法（不代表脚本有误）", false
		}
		return strings.TrimSpace(string(out)), false
	}
	return "", true
}

// referencedLoberVars 扫描脚本正文，返回全部被引用的 LOBER_ 前缀变量名
// （去重，不含前导 $ 或花括号）。
func referencedLoberVars(body string) []string {
	matches := loberVarPattern.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool)
	var out []string
	for _, m := range matches {
		name := m[1]
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// handleApproveHook 处理 POST /hooks/{id}/approve：draft → review →
// active 单向流转，每次调用推进一步（不能跳步）。
//
// 权限细分（§9.4、http-console.md）：draft→review 允许 hook-author 自己
// 发起；review→active 必须 hook-approver（hook:approve），且审批人不能
// 等于作者。这两条判据依赖 hook 的当前状态，因此在 handler 内部判断，
// 不在路由层用单一权限码卡死。
func (s *Server) handleApproveHook(c *gin.Context) {
	id := c.Param("id")
	hook, err := s.deps.Hooks.GetHook(c.Request.Context(), id, 0)
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}

	user, ok := userFromContext(c.Request.Context())
	if !ok {
		apierr.Fail(c, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
			"未登录或登录状态已过期，请重新登录"))
		return
	}
	codes := codesForRoles(user.Roles)

	switch hook.State {
	case model.HookStateDraft:
		if !hasCode(codes, codeHookWrite) {
			// HTTP 状态码从迁移前的 200 改为 403：迁移契约 §2 冻结
			// CodeForbidden→403，理由与 rbac.go 的 requireRole/requireCode
			// 完全一致（见该文件的说明），不是本 agent 自行变更业务语义。
			apierr.Fail(c, apierr.New(http.StatusForbidden, apierr.CodeForbidden,
				"draft → review 需要 hook:write 权限（作者本人发起）"))
			return
		}
		updated, err := s.deps.Hooks.UpdateHookState(c.Request.Context(), id, hook.Version, model.HookStateReview, "", 0)
		if err != nil {
			writeInternal(c.Writer, err)
			return
		}
		apierr.OK(c, toHookItem(updated))
	case model.HookStateReview:
		if !hasCode(codes, codeHookApprove) {
			apierr.Fail(c, apierr.New(http.StatusForbidden, apierr.CodeForbidden,
				"review → active 需要 hook:approve 权限"))
			return
		}
		if user.Username == hook.Author {
			apierr.Fail(c, apierr.New(http.StatusForbidden, apierr.CodeForbidden,
				"审批人不能等于作者（pre 阶段 hook 能碰分区，双人复核前提是审批人与作者不是同一人）"))
			return
		}
		updated, err := s.deps.Hooks.UpdateHookState(c.Request.Context(), id, hook.Version, model.HookStateActive,
			user.Username, s.clock.Now())
		if err != nil {
			writeInternal(c.Writer, err)
			return
		}
		apierr.OK(c, toHookItem(updated))
	default:
		apierr.Fail(c, apierr.New(http.StatusConflict, apierr.CodeConflict,
			"该 hook 已是终态或已完成审批（active/deprecated），不能再次流转"))
	}
}

// hookRejectRequest 是 POST /hooks/{id}/reject 的入参形态：驳回必须能填
// 理由（否则作者不知道为什么被拒），理由是必填字段。
type hookRejectRequest struct {
	Reason string `json:"reason"`
}

// hookRejectResponse 在 HookItem 之外附带回显的驳回理由——这是「调用方
// （前端）当场拿到理由」最快的路径，不需要多一次请求。事后查（作者当时
// 不在场）的路径是 GET /hooks/{id}/audit（本次契约变更新增，见
// handleHookAudit），两条路径并存：当场回显优化首次可见性，事后查询
// 解决「审计记录只在写入那一刻的响应体里可见、之后无法复原」这个更深的
// 缺口（此前 AuditLogger 接口只写不读，且没有任何 HTTP 端点能查审计
// 记录，见 docs/real-machine/05-审计持久化.md）。
type hookRejectResponse struct {
	hookItem
	Reason string `json:"reason"`
}

// handleRejectHook 处理 POST /hooks/{id}/reject：review → draft 单步回退，
// 与 handleApproveHook 对称（本次契约变更，见
// docs/contracts/http-console.md「变更记录」）。
//
// 设计要点：
//   - HookState 的四个取值是冻结的（model.HookState），驳回只能落在
//     draft/review/active/deprecated 之内——回退到 draft，不发明新状态。
//   - 只有 review 状态能被驳回：draft 驳回没有意义（还没提交审批），
//     active 已经生效（要停用是 deprecate 不是 reject）；两者都回
//     CodeConflict，理由写清楚，不是笼统的「状态不对」。
//   - 权限与 approve 的 review→active 分支完全一致：需要 hook:approve，
//     且审批人不能等于作者——驳回同样是审批权，不是作者自己能做的事。
//   - 不改动 Body/BodySHA256：SPEC §2.7 要求 hook 正文不可变，驳回只是
//     状态回退，不是内容变更；UpdateHookState 本身就不接受修改 Body，
//     这条约束由 store 层的方法签名保证，handler 无法意外绕过。
//   - 理由不进 model.Hook（该 struct 是冻结契约，不能加字段）：写入
//     审计日志的 detail 字段，并在响应体里原样回显。
func (s *Server) handleRejectHook(c *gin.Context) {
	id := c.Param("id")
	hook, err := s.deps.Hooks.GetHook(c.Request.Context(), id, 0)
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}

	// Reason 不用 binding:"required" 标签：原校验对空白字符做 TrimSpace
	// 后判空，required 只认零值（""），"   " 这种纯空白输入会被 required
	// 放过而被原校验拦下——与 hookWriteRequest.Name 的处理是同一条理由
	// （见该字段文档注释），加标签等于悄悄放宽这条校验。
	var req hookRejectRequest
	if err := bindHookJSON(c, &req); err != nil {
		apierr.Fail(c, err)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		apierr.Fail(c, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest,
			"reason 不能为空：驳回必须填理由，否则作者不知道为什么被拒"))
		return
	}

	user, ok := userFromContext(c.Request.Context())
	if !ok {
		apierr.Fail(c, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
			"未登录或登录状态已过期，请重新登录"))
		return
	}
	codes := codesForRoles(user.Roles)

	if hook.State != model.HookStateReview {
		apierr.Fail(c, apierr.New(http.StatusConflict, apierr.CodeConflict,
			fmt.Sprintf("只有 review 状态能被驳回，当前状态是 %q（draft 驳回没有意义，"+
				"active 要停用应该用 deprecate 而不是 reject）", hook.State)))
		return
	}
	if !hasCode(codes, codeHookApprove) {
		// HTTP 状态码从迁移前的 200 改为 403，理由与 handleApproveHook 同处
		// 的说明一致（迁移契约 §2 冻结 CodeForbidden→403）。
		apierr.Fail(c, apierr.New(http.StatusForbidden, apierr.CodeForbidden,
			"驳回需要 hook:approve 权限（驳回同样是审批权，不是作者自己能做的事）"))
		return
	}
	if user.Username == hook.Author {
		apierr.Fail(c, apierr.New(http.StatusForbidden, apierr.CodeForbidden,
			"作者不能驳回自己的 hook（与审批对称：审批人不能等于作者）"))
		return
	}

	updated, err := s.deps.Hooks.UpdateHookState(c.Request.Context(), id, hook.Version, model.HookStateDraft, "", 0)
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}

	if s.deps.Audit != nil {
		detail := fmt.Sprintf("reason=%s", req.Reason)
		// 错误被有意吞掉：审计写入失败不影响主流程（与 auditMiddleware
		// 的既有约定一致，计划任务 2）。这里额外单独调用（而不是只靠
		// auditMiddleware 记的那条不含理由的记录）是因为 auditMiddleware
		// 的 detail 固定是 "client_ip=..."，装不下理由原文。
		_ = s.deps.Audit.Log(c.Request.Context(), user.Username, "hook.reject", id, detail)
	}

	apierr.OK(c, hookRejectResponse{hookItem: toHookItem(updated), Reason: req.Reason})
}

// auditRecordItem 是 GET /hooks/{id}/audit 的响应元素形态，字段名与
// contract.AuditRecord 对应。
type auditRecordItem struct {
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target"`
	Detail string `json:"detail"`
}

func toAuditRecordItem(r contract.AuditRecord) auditRecordItem {
	return auditRecordItem{TS: r.TS, Actor: r.Actor, Action: r.Action, Target: r.Target, Detail: r.Detail}
}

// handleHookAudit 处理 GET /hooks/{id}/audit（本次契约变更新增，端点
// 编号 29，见 docs/contracts/http-console.md「变更记录」）：按 hook id
// 查该 hook 的审计记录，最新的在前。
//
// 存在的理由：hook 驳回理由此前只在驳回那一次的 HTTP 响应体里可见
// （hookRejectResponse.Reason）——作者如果当时不在场，之后没有任何 API
// 能查到自己的 hook 为什么被驳回，这是本次契约变更要补的第一层缺口
// （详见 docs/real-machine/05-审计持久化.md）。本端点让这条信息随时可查，
// 不再要求作者必须在驳回发生的那一刻在场。
//
// 权限：hook-author（hook:write）、hook-approver（hook:approve）、
// admin 三者之一即可——这三个角色正是「与这个 hook 的审批流程有关的人」
// （SPEC §9.4：hook 的创建/编辑/激活审批各自独立于其它资源分权）；不
// 开放给 operator/viewer，审计记录含操作人信息，无关角色不应看到别人的
// 操作留痕。三者取一用 OR 语义，不是单一权限码能表达的，因此本端点在
// 路由层不传 code（routesHook 的注释已说明），判定放在本函数内部——与
// handleApproveHook/handleRejectHook 的既有风格一致。
func (s *Server) handleHookAudit(c *gin.Context) {
	if s.deps.AuditQuery == nil {
		apierr.OK(c, []auditRecordItem{})
		return
	}

	user, ok := userFromContext(c.Request.Context())
	if !ok {
		apierr.Fail(c, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
			"未登录或登录状态已过期，请重新登录"))
		return
	}
	codes := codesForRoles(user.Roles)
	// admin 不需要单独判断：roleCodes 表（rbac.go）已经把 codeHookWrite 与
	// codeHookApprove 都聚合进了 roleAdmin，因此持有 admin 角色的用户在
	// codesForRoles 展开后天然满足下面两个 hasCode 之一，不需要第三个
	// 分支。
	if !hasCode(codes, codeHookWrite) && !hasCode(codes, codeHookApprove) {
		// HTTP 状态码从迁移前的 200 改为 403，理由与本文件其它 CodeForbidden
		// 分支一致（迁移契约 §2 冻结 CodeForbidden→403）。
		apierr.Fail(c, apierr.New(http.StatusForbidden, apierr.CodeForbidden,
			"查看 hook 审计记录需要 hook:write 或 hook:approve 权限（admin 通过聚合权限满足）"))
		return
	}

	id := c.Param("id")
	limit := parseLimit(c.Query("limit"), defaultListLimit)
	records, err := s.deps.AuditQuery.ListAudit(c.Request.Context(), contract.AuditFilter{Target: id, Limit: limit})
	if err != nil {
		writeInternal(c.Writer, err)
		return
	}

	items := make([]auditRecordItem, 0, len(records))
	for _, rec := range records {
		items = append(items, toAuditRecordItem(rec))
	}
	apierr.OK(c, items)
}
