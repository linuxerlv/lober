// 本文件实现 ESXi 家族 boot.cfg 请求的专属处理路径：读出导入时保存的
// 原始引导参数文件模板，算出改写所需的两个 URL，再交给注入的
// RenderESXiBootCfg 函数做三处改写（prefix=/kernelopt= 的
// ks=/modules= 去前导斜杠）。
//
// 存在的理由（对应 renderer.go 与 esxi.go 文件末尾已记录的契约缺口）：
// internal/render.RenderESXiBootCfg(template, bootCfgURLBase, answerFileURL)
// 需要「原始 boot.cfg 模板文本」作为输入，但渲染器包被要求零 IO、纯函数，
// 不能自己读盘；而 model.RenderContext.Artifact.Paths.Extra["boot.cfg"]
// 只是磁盘相对路径字符串（值就是 "boot.cfg" 本身），不是文件内容。读盘
// 只能发生在网关层——它本来就做 HTTP 层的 IO，这是它第一次需要读本地
// 磁盘。
//
// 网关不能直接 import internal/render：那是两个互斥模块之间的跨模块
// import，internal/archtest 的 TestModulesDoNotImportEachOther 会拦住
// （已用 `go test ./internal/archtest/... -run TestModulesDoNotImportEachOther`
// 验证过这条规则确实生效，console 包也没有任何地方 import render，全仓
// 没有任何一处两个 M2-M8 模块互相 import 的先例——这与本任务原计划描述的
// 「gateway 读盘后直接调用 render.RenderESXiBootCfg」不同，那条路径已经
// 用 archtest 验证过是编译期会被拦住的违规，详见文件末尾 contractGaps）。
//
// 本文件因此不 import internal/render，而是通过 Deps.RenderESXiBootCfg
// 这个函数值间接引用它——签名逐字符一致，装配点（cmd/lober/wire.go，
// 全仓唯一豁免跨模块 import 的包）负责把真实实现注入进来：
// `RenderESXiBootCfg: render.RenderESXiBootCfg`。网关本身对渲染逻辑零
// 依赖，只负责读盘、拼参数、调用、处理错误。这与 Deps 里其它六个 Store
// 字段、StateMachine、Renderer 都是「跨模块通过接口/函数值编程，不直接
// import 实现包」的同一种模式，只是这次用的是函数类型而不是具名接口——
// RenderESXiBootCfg 本身是无状态纯函数，不需要为它单独定义一个只有一个
// 方法的接口。
package gateway

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lober/lober/internal/core/model"
)

// esxiBootCfgExtraKey 是 ArtifactPaths.Extra 中「原始未改写 boot.cfg
// 模板」相对路径的键名。与 internal/imports.ESXiRawBootCfgArtifactKey
// （值同为 "boot.cfg"，那个包定义了具名常量供下游引用）和
// internal/render/menu.go 的 esxiMbootEFIExtraKey（同样的本地镜像做法）
// 是同一约定的三份独立镜像——网关不能 import imports 包（同样受模块互斥
// 约束），因此不能直接引用那个常量，只能像 menu.go 一样在本包内镜像同一个
// 字面量。三处若某天不一致，是最难排查的一类 bug（表现为"boot.cfg 读到的
// 内容是别的文件"），因此保持字面量与本注释里的交叉引用，不让读者去猜。
const esxiBootCfgExtraKey = "boot.cfg"

// esxiKickstartFileName 是 boot.cfg 的 kernelopt= 里 ks= 参数应指向的独立
// kickstart 方言正文的文件名（与 answer.go 的 answerFileNameAllowed、
// internal/render/renderer.go 放行的第二候选文件名 "ks.cfg" 一致）。
const esxiKickstartFileName = "ks.cfg"

// errESXiArtifactRootNotConfigured 是 ArtifactRoot 未注入时的错误。
var errESXiArtifactRootNotConfigured = fmt.Errorf(
	"gateway: 未配置制品根目录（Deps.ArtifactRoot 为空），无法读取 ESXi 原始 boot.cfg 模板")

// errESXiRenderFuncNotConfigured 是 RenderESXiBootCfg 未注入时的错误。
var errESXiRenderFuncNotConfigured = fmt.Errorf(
	"gateway: 未注入 boot.cfg 改写函数（Deps.RenderESXiBootCfg 为 nil），无法改写引导参数文件")

// renderESXiBootCfgResponse 是 handleAnswer 在 ESXi 家族收到 boot.cfg 请求
// 时调用的专属渲染路径，替代对 s.deps.Renderer.RenderAnswerFile 的调用。
//
// 三类失败都必须让调用方走"500 + 简短纯文本 + 一条 level=error 的
// TaskEvent"（docs/contracts/http-machine.md 端点 3「错误行为」），因此
// 本函数统一返回 (string, error)，与 s.deps.Renderer.RenderAnswerFile 的
// 返回值形态一致，调用方 handleAnswer 可以用同一段错误处理代码：
//  1. deps.ArtifactRoot 为空（未注入）；
//  2. deps.RenderESXiBootCfg 为 nil（未注入）；
//  3. 读盘失败（文件缺失/权限不足）或改写函数本身返回错误。
//
// 绝不能在任何一类失败上静默返回 kickstart 正文兜底——那正是本次改动要
// 修复的错误行为本身：真实部署下 mboot.efi 会把 kickstart 正文当引导
// 参数解析，失败发生在引导器读引导参数这一步，比"装到一半停住"更早，且
// 现场只看到引导器报错、看不出根因。
func (s *Server) renderESXiBootCfgResponse(_ context.Context, renderCtx model.RenderContext) (string, error) {
	if renderCtx.Artifact.ID == "" {
		return "", fmt.Errorf("gateway: 渲染上下文缺少制品集 ID，无法定位 boot.cfg 模板所在目录")
	}

	relPath, ok := renderCtx.Artifact.Paths.Extra[esxiBootCfgExtraKey]
	if !ok || relPath == "" {
		return "", fmt.Errorf(
			"gateway: ESXi 制品集 %s 缺少 ArtifactPaths.Extra[%q]，无法定位原始 boot.cfg 模板",
			renderCtx.Artifact.ID, esxiBootCfgExtraKey)
	}

	if s.deps.ArtifactRoot == "" {
		return "", errESXiArtifactRootNotConfigured
	}
	if s.deps.RenderESXiBootCfg == nil {
		return "", errESXiRenderFuncNotConfigured
	}

	// 与 assemblyPrechecker.Check（cmd/lober/wire.go）、importRunner.
	// TriggerImport（cmd/lober/importrunner.go）拼接制品集子目录同一种
	// 方式：filepath.Join(制品根目录, 制品集ID, 相对路径)——
	// docs/contracts/filesystem.md 硬约束 1：ArtifactSet.Paths 里存的
	// 相对路径是相对于该制品集自己的根目录
	// /srv/lober/artifacts/<artifact_set_id>/。
	diskPath := filepath.Join(s.deps.ArtifactRoot, renderCtx.Artifact.ID, relPath)
	raw, err := os.ReadFile(diskPath)
	if err != nil {
		return "", fmt.Errorf("gateway: 读取原始 boot.cfg 模板 %s 失败: %w", diskPath, err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("gateway: 原始 boot.cfg 模板 %s 内容为空，这不是一份可用的模板", diskPath)
	}

	// bootCfgURLBase 与 internal/render/menu.go 的 renderESXiMenu 拼
	// kernel URL 同一套方法（ctx.SetArtifactURL），确保菜单里的制品 URL
	// 与本函数改写出的 prefix= 指向同一个基址（含制品集 ID 那一层）——
	// 不一致的后果是菜单指向一个地址、boot.cfg 里的 prefix 指向另一个。
	// 传入空字符串取的是"本制品集根目录"这一层，不含任何具体文件名。
	bootCfgURLBase := renderCtx.SetArtifactURL("")

	// answerFileURL 与 renderESXiMenu 拼 answerFileURL 同一套方法
	// （ctx.Bases.GatewayURL("v1","answer",taskID,token,文件名)），只是
	// 文件名固定为 "ks.cfg"——boot.cfg 的 kernelopt= 行 ks= 参数指向的
	// 正是这份独立的 kickstart 方言正文，不是 boot.cfg 自己（两者从来
	// 不是同一个文件，见 internal/render/esxi.go 文件头注释）。
	answerFileURL := renderCtx.Bases.GatewayURL(
		"v1", "answer", renderCtx.Task.ID, renderCtx.InstallToken, esxiKickstartFileName)

	out, err := s.deps.RenderESXiBootCfg(string(raw), bootCfgURLBase, answerFileURL)
	if err != nil {
		return "", fmt.Errorf("gateway: 改写 boot.cfg 失败: %w", err)
	}
	return out, nil
}
