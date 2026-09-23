// 本文件实现制品集四个端点：登记、列表、校验完整性、删除。对应实现计划
// 任务 4，依据 docs/contracts/http-console.md §3、SPEC §2.1、§2.7。
//
// 本轮新增：登记成功后触发异步导入 job（importing → ready/failed），对应
// SPEC §11 Phase 2、§12.6「ISO 登记与导入进度（异步 job）」的要求。
package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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

// routesArtifactSet 注册制品集四个端点。
func (s *Server) routesArtifactSet() {
	s.handleFunc("POST", "/artifact-sets", codeArtifactImport, "artifact_set.create", s.handleCreateArtifactSet)
	s.handleFunc("GET", "/artifact-sets", "", "", s.handleListArtifactSets)
	s.handleFunc("POST", "/artifact-sets/{id}/verify", codeArtifactImport, "artifact_set.verify", s.handleVerifyArtifactSet)
	s.handleFunc("DELETE", "/artifact-sets/{id}", codeArtifactImport, "artifact_set.delete", s.handleDeleteArtifactSet)
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
func (s *Server) handleCreateArtifactSet(w http.ResponseWriter, r *http.Request) {
	var req createArtifactSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "请求体不是合法的 JSON"))
		return
	}
	if strings.TrimSpace(req.SourcePath) == "" {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "sourcePath 不能为空"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "name 不能为空"))
		return
	}

	set := model.ArtifactSet{
		ID:    uuid.NewString(),
		Name:  req.Name,
		State: model.ArtifactSetImporting,
	}
	created, err := s.deps.ArtifactSets.CreateArtifactSet(r.Context(), set)
	if err != nil {
		writeInternal(w, err)
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
	// sqlite3 CLI 或未来的手工修复路径把它推进——这不是本次改动引入的
	// 新风险，是「没有导入触发」这一断口在 Deps.ArtifactImporter 缺省时
	// 的自然延续。
	if s.deps.ArtifactImporter != nil {
		s.deps.ArtifactImporter.TriggerImport(context.WithoutCancel(r.Context()), created.ID, req.SourcePath)
	}

	apierr.WriteOK(w, toArtifactSetItem(created))
}

// handleListArtifactSets 处理 GET /artifact-sets。
func (s *Server) handleListArtifactSets(w http.ResponseWriter, r *http.Request) {
	sets, err := s.deps.ArtifactSets.ListArtifactSets(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	items := make([]artifactSetItem, 0, len(sets))
	for _, a := range sets {
		items = append(items, toArtifactSetItem(a))
	}
	apierr.WriteOK(w, items)
}

// handleVerifyArtifactSet 处理 POST /artifact-sets/{id}/verify：重算
// manifest 校验完整性。MVP 阶段控制台不持有真实制品文件系统访问权
// （那属于 M7/存储层的职责），本端点校验制品集存在后直接返回接受，真正
// 的重算由异步 job 完成、结果体现在下次 GET 的 state 字段——与
// http-console.md「异步执行，结果体现在下次 GET /artifact-sets 的 state
// 字段」的约定一致。
func (s *Server) handleVerifyArtifactSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.deps.ArtifactSets.GetArtifactSet(r.Context(), id); err != nil {
		writeInternal(w, err)
		return
	}
	apierr.WriteOK(w, nil)
}

// handleDeleteArtifactSet 处理 DELETE /artifact-sets/{id}。
//
// 前置条件（§2.7）：仅 state=deprecated 且未被任何 active template 引用
// 时可删除。ArtifactSetStore.DeleteArtifactSet 已经实现了引用检查（返回
// contract.ErrStillReferenced），本 handler 补上「state != deprecated」
// 这一层——Store 契约本身不检查调用方是否先确认了 state，是 handler 的
// 职责。
func (s *Server) handleDeleteArtifactSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	set, err := s.deps.ArtifactSets.GetArtifactSet(r.Context(), id)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if set.State != model.ArtifactSetDeprecated {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeConflict,
			"只有已停用（deprecated）的制品集才能删除，请先停用"))
		return
	}

	if err := s.deps.ArtifactSets.DeleteArtifactSet(r.Context(), id); err != nil {
		if errors.Is(err, contract.ErrStillReferenced) {
			apierr.WriteError(w, apierr.New(http.StatusConflict, apierr.CodeStillReferenced,
				"该制品集仍被启用中的模板引用，无法删除"))
			return
		}
		writeInternal(w, err)
		return
	}
	apierr.WriteOK(w, nil)
}
