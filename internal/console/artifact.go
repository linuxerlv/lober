// 本文件实现制品集四个端点：登记、列表、校验完整性、删除。对应实现计划
// 任务 4，依据 docs/contracts/http-console.md §3、SPEC §2.1、§2.7。
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

// routesArtifactSet 注册制品集四个端点。
func (s *Server) routesArtifactSet() {
	s.handleFunc("POST", "/artifact-sets", codeArtifactImport, "artifact_set.create", s.handleCreateArtifactSet)
	s.handleFunc("GET", "/artifact-sets", "", "", s.handleListArtifactSets)
	s.handleFunc("POST", "/artifact-sets/{id}/verify", codeArtifactImport, "artifact_set.verify", s.handleVerifyArtifactSet)
	s.handleFunc("DELETE", "/artifact-sets/{id}", codeArtifactImport, "artifact_set.delete", s.handleDeleteArtifactSet)
}

// artifactSetItem 是响应形态，字段名与 model.ArtifactSet 对应。
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

// handleCreateArtifactSet 处理 POST /artifact-sets：登记触发异步导入 job，
// 本端点立即返回 state=importing 的记录（本模块不 import M7 的实际导入
// 实现，异步 job 本身由装配点接入的其它组件驱动——控制台只负责落一条
// importing 记录，状态更新走 ArtifactSetStore.UpdateArtifactSetState，
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
