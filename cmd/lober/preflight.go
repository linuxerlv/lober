// 本文件实现启动自检：校验 ready 状态制品集的 paths 里每个文件真实存在
// 于盘上。对应实现计划任务 2，依据 SPEC §10 运维点一、
// docs/contracts/filesystem.md 硬约束 3。
//
// nginx 按路径找文件、Go 按同一套路径签发 URL，不一致即引导时 404——
// 这条检查把「机器已经在机房上电、引导失败、运维翻日志」压缩成「启动
// 日志里一行告警」。发现问题不让服务起不来，只打日志：一个坏掉的旧制品
// 集不该阻止整个系统启动，那些机器的引导会在 §4.6 三项校验里被拒绝，
// 届时有确切原因。
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// artifactSetLister 是 runPreflight 依赖的最窄接口：只需要列出全部制品集
// （测试用假实现，不需要搭出完整 Store）。
type artifactSetLister interface {
	ListArtifactSets(ctx context.Context) ([]model.ArtifactSet, error)
}

// preflightIssue 是一条自检发现的问题：制品集名字 + 具体缺失路径。
type preflightIssue struct {
	ArtifactSetID   string
	ArtifactSetName string
	MissingPath     string
}

// runPreflight 跑一遍启动自检并把发现的问题逐条打日志。只检查 ready
// 状态的制品集——importing 中的当然缺文件，failed 与 deprecated 的没人
// 会用（计划任务 2「实现流程」）。
func runPreflight(ctx context.Context, app *App) {
	issues, err := preflightCheck(ctx, app.Store, os.DirFS(app.artifactRoot))
	if err != nil {
		slog.Error("启动自检本身执行失败", slog.Any("error", err))
		return
	}
	if len(issues) == 0 {
		slog.Info("启动自检：全部 ready 状态制品集文件完整")
		return
	}
	for _, issue := range issues {
		slog.Warn("启动自检发现制品文件缺失",
			slog.String("artifact_set_id", issue.ArtifactSetID),
			slog.String("artifact_set_name", issue.ArtifactSetName),
			slog.String("missing_path", issue.MissingPath),
		)
	}
}

// preflightCheck 是 runPreflight 的可测试核心：给定制品集列表与文件系统
// 视图，逐一检查 ready 状态制品集的 Paths 是否真实存在。
//
// 文件系统通过 fs.FS 注入，测试用内存文件系统（fstest.MapFS），不需要在
// 测试里真的造文件（计划任务 2「设计要点」）。
func preflightCheck(ctx context.Context, lister artifactSetLister, files fs.FS) ([]preflightIssue, error) {
	sets, err := lister.ListArtifactSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("cmd/lober: 列出制品集失败: %w", err)
	}

	var issues []preflightIssue
	for _, set := range sets {
		if set.State != model.ArtifactSetReady {
			continue
		}
		for _, p := range artifactCheckPaths(set.ID, set.Paths) {
			if p.relPath == "" {
				continue
			}
			checkedPath, err := checkArtifactPathExists(files, p.diskPath)
			if err != nil {
				issues = append(issues, preflightIssue{
					ArtifactSetID:   set.ID,
					ArtifactSetName: set.Name,
					MissingPath:     checkedPath,
				})
			}
		}
	}
	return issues, nil
}

// artifactPathCheck 是待检查的一条路径：relPath 是制品集里存的相对路径
// （用于判空），diskPath 是拼上 <artifact_set_id>/ 前缀后、相对于制品
// 根目录 fs.FS 的路径（docs/contracts/filesystem.md 硬约束 1：制品目录
// 下的路径与制品集里存的相对路径逐字符一致，都相对于
// /srv/lober/artifacts/<artifact_set_id>/）。
type artifactPathCheck struct {
	relPath  string
	diskPath string
}

// artifactCheckPaths 收集某制品集需要存在性检查的全部路径：内核、全部
// initrd、仓库、额外文件（计划任务 2「实现流程」第 2 步）。
func artifactCheckPaths(artifactSetID string, p model.ArtifactPaths) []artifactPathCheck {
	// path.Join 会清理掉结尾斜杠（path.Clean 的副作用），而结尾斜杠正是
	// 「这是目录，要落到 repodata/repomd.xml」这一判断依据（与
	// internal/statemachine/precheck.go 的同名约定一致）。因此必须在
	// path.Join 之前记录原始输入是否带结尾斜杠，再在拼接后手动补回去——
	// 不能指望 path.Join 之后的字符串还留着这个信息。
	join := func(rel string) string {
		if rel == "" {
			return ""
		}
		joined := path.Join(artifactSetID, rel)
		if strings.HasSuffix(rel, "/") {
			joined += "/"
		}
		return joined
	}

	out := make([]artifactPathCheck, 0, 2+len(p.Initrd)+len(p.Extra))
	out = append(out, artifactPathCheck{relPath: p.Kernel, diskPath: join(p.Kernel)})
	for _, initrd := range p.Initrd {
		out = append(out, artifactPathCheck{relPath: initrd, diskPath: join(initrd)})
	}
	out = append(out, artifactPathCheck{relPath: p.Repo, diskPath: join(p.Repo)})
	for _, extra := range p.Extra {
		out = append(out, artifactPathCheck{relPath: extra, diskPath: join(extra)})
	}
	return out
}

// repoMetadataFile 是仓库目录下用来判定「仓库真正可用」的元数据文件相对
// 路径——空目录能 stat 成功但对 installer 毫无用处，检查目录本身等于
// 没检查（计划任务 2「设计要点」，与 internal/statemachine/precheck.go
// 的同名约定保持一致）。
const repoMetadataFile = "repodata/repomd.xml"

// checkArtifactPathExists 判定给定路径在 files 中是否存在。路径以斜杠
// 结尾则视为目录，检查其下的 repodata/repomd.xml；否则直接检查该文件
// 本身。返回值 checkedPath 是实际被 fs.Stat 校验的那个路径——目录形式
// 会展开成 repodata/repomd.xml 的完整路径，供调用方在问题报告里给出
// 精确位置，而不是只报「repo/ 这个目录缺失」这种运维还要再反查一层的
// 信息。
func checkArtifactPathExists(files fs.FS, p string) (checkedPath string, err error) {
	target := p
	if strings.HasSuffix(p, "/") {
		dir := strings.TrimSuffix(p, "/")
		target = path.Join(dir, repoMetadataFile)
	}
	_, err = fs.Stat(files, target)
	return target, err
}
