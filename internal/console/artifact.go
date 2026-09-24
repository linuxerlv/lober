// 本文件实现制品集四个端点：登记、列表、校验完整性、删除。对应实现计划
// 任务 4，依据 docs/contracts/http-console.md §3、SPEC §2.1、§2.7。
//
// 本轮新增：登记成功后触发异步导入 job（importing → ready/failed），对应
// SPEC §11 Phase 2、§12.6「ISO 登记与导入进度（异步 job）」的要求。
package console

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// ArtifactImporter 是控制台触发异步导入 job 需要的最窄接口：只有一个
// 方法，「异步导入这个制品集」。
//
// 接口定义放在 console 包内（不放 internal/core/contract）：契约层
// internal/core/contract/** 对本轮任务只读，且「调用方定义接口」本身就是
// Go 的惯例——真正的导入逻辑在 internal/imports 包，但 console 不能
// import internal/imports（archtest 的依赖方向守卫拦住任何实现模块之间
// 互相 import，唯一豁免是 cmd/lober 装配点），因此装配点
// （cmd/lober/importrunner.go）持有 internal/imports 的具体类型、实现
// 这个窄接口后注入进来，console 包本身的代码全程看不到 internal/imports
// 的任何类型。
//
// TriggerImport 的语义是「异步触发，立即返回」——本接口的实现必须自己
// 起 goroutine（或复用某种任务队列），不能阻塞调用方；handleCreateArtifactSet
// 在 HTTP 响应写出之前调用它，若这里同步跑几 GB 的解压，会让登记请求
// 挂起到导入完成才返回，与 SPEC §2.1「导入是异步 job」的要求直接冲突。
//
// isoPath 是登记时运维填写的本地路径（气隙环境下以「登记服务器本地
// 路径」为主，与 createArtifactSetRequest.SourcePath 同一个值）——
// model.ArtifactSet 没有承载这个值的字段（该结构体的字段列表见
// internal/core/model/inventory.go，只有 SourceISOSHA256 记录内容
// 摘要，没有记录来源路径本身），所以本接口在触发时机直接把 handler
// 收到的原始请求值转发过去，不依赖任何持久化字段——这也是为什么导入
// 只能在登记这一次 HTTP 请求的生命周期内触发：进程重启后没有任何地方
// 能重新取到这个路径（详见 contractGaps 与 notes 中对「在途导入中断」
// 的运维说明）。
type ArtifactImporter interface {
	// TriggerImport 异步触发对 artifactSetID 的导入：从 isoPath 打开 ISO、
	// 识别家族、抽取、写回制品集内容与状态。方法本身不返回执行结果
	// （结果体现在制品集的 state 字段上，供前端轮询 GET /artifact-sets
	// 观察，与 docs/contracts/http-console.md 对 POST /artifact-sets 的
	// 现有约定「导入是异步 job，本端点立即返回，进度由前端轮询观察」
	// 完全一致，不需要新增端点）。
	TriggerImport(ctx context.Context, artifactSetID, isoPath string)
}

// ArtifactVerifier 是控制台触发异步「重算 manifest 校验完整性」需要的
// 最窄接口：只有一个方法。
//
// 与 ArtifactImporter 同一种接缝设计（接口定义在 console 包内，具体实现
// 放装配点 cmd/lober/verifier.go）：真正的 manifest 生成逻辑
// （imports.GenerateManifest）在 internal/imports 包，而 console 不能
// import internal/imports（archtest 的依赖方向守卫拦住任何实现模块之间
// 互相 import，唯一豁免是 cmd/lober 装配点）。
//
// TriggerVerify 的语义是「异步触发，立即返回」——校验要重算整棵制品树的
// sha256（可能是几 GB 文件），本接口的实现必须自己起 goroutine，不能
// 阻塞调用方；handleVerifyArtifactSet 在 HTTP 响应写出之前调用它，若这里
// 同步跑完，会让校验请求挂起到整棵树算完 sha256 才返回，与 SPEC §8.2
// 「重算 sha256 校验完整性」这一端点应保持的响应及时性、以及本模块对
// ArtifactImporter 已经确立的「异步 job，端点立即返回」纪律相冲突。
//
// 方法本身不返回执行结果（结果体现在制品集的字段上，供前端轮询
// GET /artifact-sets 观察——具体体现方式见 verifier.go 里生产实现的文档，
// 那里详细论证了「校验失败时该不该把状态置为 failed」这一设计决策）。
type ArtifactVerifier interface {
	// TriggerVerify 异步触发对 artifactSetID 的完整性校验：重算
	// manifest、与已登记的 ManifestSHA256 比对，把可观察的结果写回。
	TriggerVerify(ctx context.Context, artifactSetID string)
}

// routesArtifactSet 注册制品集四个端点。
//
// 路径参数语法从 "{id}" 换成 ":id"——这是 gin 路由声明本身的语法差异
// （迁移契约 §4：「gin 的 :param 只是路由声明语法，不改 URL 本身」），
// 对外可见的 URL（如 /console/v1/artifact-sets/abc/verify）一个字节不变。
func (s *Server) routesArtifactSet() {
	s.handle("POST", "/artifact-sets", codeArtifactImport, "artifact_set.create", s.handleCreateArtifactSet)
	s.handle("GET", "/artifact-sets", "", "", s.handleListArtifactSets)
	s.handle("POST", "/artifact-sets/:id/verify", codeArtifactImport, "artifact_set.verify", s.handleVerifyArtifactSet)
	s.handle("DELETE", "/artifact-sets/:id", codeArtifactImport, "artifact_set.delete", s.handleDeleteArtifactSet)
}

// artifactSetItem 是响应形态，字段名与 model.ArtifactSet 对应。
//
// FailReason 是本次契约变更新增字段（t01 记录的缺口：导入失败时运维在
// 列表里只看到一个没有信息量的 "failed"，不知道是路径打不开、识别不出
// 家族、抽取失败还是 manifest 生成失败）。非 failed 状态下通常为空字符串
// ——与 model.ArtifactSet.FailReason 的零值语义一致，不是需要特殊处理的
// 哨兵值。
type artifactSetItem struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	OSFamily        string            `json:"osFamily"`
	OSVersion       string            `json:"osVersion"`
	Arch            string            `json:"arch"`
	Firmware        string            `json:"firmware"`
	SourceISOSHA256 string            `json:"sourceIsoSha256"`
	State           string            `json:"state"`
	ManifestSHA256  string            `json:"manifestSha256"`
	FailReason      string            `json:"failReason"`
	Paths           artifactPathsItem `json:"paths"`
}

type artifactPathsItem struct {
	Kernel string            `json:"kernel"`
	Initrd []string          `json:"initrd"`
	Repo   string            `json:"repo"`
	Extra  map[string]string `json:"extra"`
}

func toArtifactSetItem(a model.ArtifactSet) artifactSetItem {
	extra := a.Paths.Extra
	if extra == nil {
		extra = map[string]string{}
	}
	initrd := a.Paths.Initrd
	if initrd == nil {
		initrd = []string{}
	}
	return artifactSetItem{
		ID:              a.ID,
		Name:            a.Name,
		OSFamily:        string(a.OSFamily),
		OSVersion:       a.OSVersion,
		Arch:            string(a.Arch),
		Firmware:        string(a.Firmware),
		SourceISOSHA256: a.SourceISOSHA256,
		State:           string(a.State),
		ManifestSHA256:  a.ManifestSHA256,
		FailReason:      a.FailReason,
		Paths: artifactPathsItem{
			Kernel: a.Paths.Kernel,
			Initrd: initrd,
			Repo:   a.Paths.Repo,
			Extra:  extra,
		},
	}
}

// createArtifactSetRequest 是 POST /artifact-sets 的入参（气隙环境登记
// 服务器本地路径，不接受上传流，§2.1）。
type createArtifactSetRequest struct {
	SourcePath string `json:"sourcePath"`
	Name       string `json:"name"`
}

// handleCreateArtifactSet 处理 POST /artifact-sets：落库 state=importing
// 记录后，通过 Deps.ArtifactImporter 这个窄接口触发异步导入 job，本端点
// 立即返回（本模块不 import internal/imports 的实际实现，真正的抽取/
// manifest/状态写回逻辑由装配点注入的 ArtifactImporter 实现完成——控制台
// 只负责落库与触发，状态更新走 ArtifactSetStore.UpdateArtifactSetState，
// 不在本 handler 内同步执行导入）。
//
// sourcePath/name 两条非空校验没有改用 gin binding 的 required 标签：
// required 只拒绝字面空字符串，不拒绝纯空格（如 "   "），而这里迁移前
// 用的是 strings.TrimSpace(x) == ""，校验强度更严。迁移契约 §7 明确
// 「校验条件一条都不能少」，标签表达不了原有强度时必须留在 handler 里
// 用 Go 代码做，不能为了「全用标签」反而弱化校验，因此这两条继续手写。
func (s *Server) handleCreateArtifactSet(c *gin.Context) {
	var req createArtifactSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		bindErr := bindJSONError(err)
		apierr.Fail(c, bindErr)
		return
	}
	if strings.TrimSpace(req.SourcePath) == "" {
		apierr.Fail(c, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest, "sourcePath 不能为空"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		apierr.Fail(c, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest, "name 不能为空"))
		return
	}

	set := model.ArtifactSet{
		ID:    uuid.NewString(),
		Name:  req.Name,
		State: model.ArtifactSetImporting,
	}
	created, err := s.deps.ArtifactSets.CreateArtifactSet(c.Request.Context(), set)
	if err != nil {
		apierr.Fail(c, err)
		return
	}

	// 触发异步导入（本轮新增）：登记本身已经落库成功，导入是否成功不影响
	// 本次 HTTP 响应——本端点始终返回 state=importing 的记录，真正的
	// ready/failed 由异步 job 事后写回，前端轮询 GET /artifact-sets 观察
	// （docs/contracts/http-console.md 对本端点的既有约定）。
	//
	// Deps.ArtifactImporter 判空：可为 nil（测试或某些部署形态可能不需要
	// 触发能力，与 Deps.EventBus 同一种「按需注入、handler 调用前判空」
	// 的接缝设计），为 nil 时制品集会停留在 importing，直到有人用
	// psql CLI 或未来的手工修复路径把它推进——这不是本次改动引入的
	// 新风险，是「没有导入触发」这一断口在 Deps.ArtifactImporter 缺省时
	// 的自然延续。
	if s.deps.ArtifactImporter != nil {
		s.deps.ArtifactImporter.TriggerImport(context.WithoutCancel(c.Request.Context()), created.ID, req.SourcePath)
	}

	apierr.OK(c, toArtifactSetItem(created))
}

// bindJSONError 把 c.ShouldBindJSON 的返回值分流成两类：
//
//  1. validator.ValidationErrors（结构体标签校验失败）：原样返回，交给
//     apierr.Fail 识别并翻译成中文（422 + CodeValidationFailed，迁移
//     契约 §3）。
//  2. 其余情况（JSON 语法错误、类型不匹配等解码阶段失败）：改写成
//     CodeBadRequest（400），不能直接把 err 交给 apierr.Fail——它未被
//     任何已知 sentinel 或 *Error 识别，会落进「未识别错误」兜底分支
//     变成 CodeInternal（500），而请求体格式错误是客户端的错，不是
//     服务端内部错误，混进 1000 会让前端把「你传错了」误报成「服务器
//     炸了」。
//
// 本文件的两个 create/无请求体端点里只有 createArtifactSetRequest 会经过
// c.ShouldBindJSON，且该结构体目前没有任何 binding 标签（两条校验强度
// 都超出标签表达能力，见 handleCreateArtifactSet 的说明），因此分支 1
// 在当前代码路径下不会真正触发——保留这层判断是为了不让本函数的正确性
// 依赖于「未来没有人给这个请求体加 binding 标签」这一隐含假设。
func bindJSONError(err error) error {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		return err
	}
	return apierr.New(http.StatusBadRequest, apierr.CodeBadRequest, "请求体不是合法的 JSON")
}

// handleListArtifactSets 处理 GET /artifact-sets。
func (s *Server) handleListArtifactSets(c *gin.Context) {
	sets, err := s.deps.ArtifactSets.ListArtifactSets(c.Request.Context())
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	items := make([]artifactSetItem, 0, len(sets))
	for _, a := range sets {
		items = append(items, toArtifactSetItem(a))
	}
	apierr.OK(c, items)
}

// handleVerifyArtifactSet 处理 POST /artifact-sets/:id/verify：触发一次
// 真正的 manifest 重算，校验制品完整性（SPEC §8.2「重算 sha256 校验完整
// 性」；docs/contracts/http-console.md 同一定义）。
//
// 校验本身是长耗时操作（要重算几 GB 文件的 sha256），因此本 handler 只做
// 两件事：确认制品集存在（不存在返回 apierr.CodeNotFound），再通过
// Deps.ArtifactVerifier 这个窄接口异步触发校验，本端点立即返回——与
// handleCreateArtifactSet 触发导入 job 的既有纪律完全一致（不在 HTTP
// handler 里同步跑完长耗时操作）。
//
// Deps.ArtifactVerifier 判空：可为 nil（测试或某些部署形态可能不需要
// 校验能力，与 Deps.ArtifactImporter 同一种「按需注入、handler 调用前
// 判空」的接缝设计）——为 nil 时端点仍返回接受，只是不会真的触发任何
// 重算，这与「校验能力缺省时端点应表现为不阻塞、不报错」的既有降级预期
// 一致（任务要求的「verifier 为 nil 时的降级行为」，参照
// handleCreateArtifactSet 对 ArtifactImporter 判空的既有处理）。
func (s *Server) handleVerifyArtifactSet(c *gin.Context) {
	// c.Param("id") 取路径参数——gin 接管路由后 r.PathValue("id") 在
	// ":id" 语法登记的路由下永远取不到值（迁移契约 §5、audit.go 同一处
	// 说明），必须改用 gin.Context.Param。
	id := c.Param("id")
	if _, err := s.deps.ArtifactSets.GetArtifactSet(c.Request.Context(), id); err != nil {
		apierr.Fail(c, err)
		return
	}

	// 与 handleCreateArtifactSet 触发导入时同样的理由：用
	// context.WithoutCancel 剥离本次 HTTP 请求的生命周期，否则响应写出、
	// 请求结束后 ctx 被取消，goroutine 里刚起步的重算就可能被牵连中断。
	if s.deps.ArtifactVerifier != nil {
		s.deps.ArtifactVerifier.TriggerVerify(context.WithoutCancel(c.Request.Context()), id)
	}

	apierr.OK(c, nil)
}

// handleDeleteArtifactSet 处理 DELETE /artifact-sets/:id。
//
// 前置条件（§2.7）：仅 state=deprecated 且未被任何 active template 引用
// 时可删除。ArtifactSetStore.DeleteArtifactSet 已经实现了引用检查（返回
// contract.ErrStillReferenced），本 handler 补上「state != deprecated」
// 这一层——Store 契约本身不检查调用方是否先确认了 state，是 handler 的
// 职责。
func (s *Server) handleDeleteArtifactSet(c *gin.Context) {
	id := c.Param("id")

	set, err := s.deps.ArtifactSets.GetArtifactSet(c.Request.Context(), id)
	if err != nil {
		apierr.Fail(c, err)
		return
	}
	if set.State != model.ArtifactSetDeprecated {
		// state 检查是本 handler 的职责（Store 契约不检查调用方是否先
		// 确认了 state），迁移契约 §2 冻结 CodeConflict → HTTP 409（不是
		// 迁移前遗留的 http.StatusOK 占位值），因此这里改用真实 409。
		apierr.Fail(c, apierr.New(http.StatusConflict, apierr.CodeConflict,
			"只有已停用（deprecated）的制品集才能删除，请先停用"))
		return
	}

	if err := s.deps.ArtifactSets.DeleteArtifactSet(c.Request.Context(), id); err != nil {
		if errors.Is(err, contract.ErrStillReferenced) {
			// 保留制品集专属文案（比 apierr.go sentinelMappings 里
			// ErrStillReferenced 的通用文案「该对象仍被…」更具体，指明是
			// 「制品集」），不改用 apierr.Fail(c, err) 走通用 sentinel
			// 分支——契约没有冻结 message 的具体文本，但这条更精确的
			// 提示是既有行为，迁移不应该在没有人要求的情况下把它降级成
			// 通用文案。HTTP 状态码/业务码与 sentinel 映射表完全一致
			// （409 + CodeStillReferenced），只是文案来源不同。
			apierr.Fail(c, apierr.New(http.StatusConflict, apierr.CodeStillReferenced,
				"该制品集仍被启用中的模板引用，无法删除"))
			return
		}
		apierr.Fail(c, err)
		return
	}
	apierr.OK(c, nil)
}
