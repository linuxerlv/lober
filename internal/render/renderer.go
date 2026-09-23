// 本文件实现 contract.Renderer 的入口分派：两个方法各自的家族分流与端到端
// 注入防护的最后一道校验（对应实现计划任务 8，依据 SPEC §5.1、§5.6）。
//
// 渲染器整体是纯函数——不查库、不碰时钟、不做 IO（SPEC §5.1），这是控制台
// preview 端点能用假数据跑的前提，也是 golden 测试的前提。
package render

import (
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// Renderer 实现 contract.Renderer。不持有任何状态——两个方法都是纯函数，
// 零值 *Renderer 即可直接使用（调用方不需要构造函数）。
type Renderer struct{}

// RenderMenu 按家族分派渲染 iPXE 菜单（contract.Renderer 接口方法）。
//
// 实际的家族分流逻辑在 menu.go 的包级函数 RenderMenu 中——本方法只是把
// 接口调用转发过去，保持 renderer.go 作为"入口分派"这一层的职责单一：
// 它决定"该调用哪个包级函数"，不重复任何家族分支逻辑。
func (r *Renderer) RenderMenu(ctx model.RenderContext) (string, error) {
	return RenderMenu(ctx)
}

// RenderAnswerFile 校验文件名后按家族分派渲染应答文件（contract.Renderer
// 接口方法）。
//
// 校验顺序（计划任务 8"两个入口的职责"）：
//  1. 先校验传入的 fileName 等于 ctx.AnswerFileName() 算出的文件名——
//     该参数来自机器请求的 URL 路径段，不能用它去选模板，否则机器可以
//     通过改文件名让服务端渲染另一种格式的应答文件。Ubuntu 例外：额外
//     接受 "meta-data"（NoCloud 要求该目录下两个文件都存在）。
//  2. 文件名通过后，再按家族分派到具体渲染逻辑。
func (r *Renderer) RenderAnswerFile(ctx model.RenderContext, fileName string) (string, error) {
	if err := validateAnswerFileName(ctx, fileName); err != nil {
		return "", err
	}

	switch ctx.Artifact.OSFamily {
	case model.OSFamilyRHEL:
		return AssembleKickstart(ctx)
	case model.OSFamilyDebian:
		return AssemblePreseed(ctx)
	case model.OSFamilyUbuntu:
		// meta-data 是 validateAnswerFileName 额外放行的第二个文件名
		// （NoCloud 要求该目录下 user-data 与 meta-data 都存在），必须在
		// 这里单独分派到 RenderMetaData——若不判断 fileName，请求
		// meta-data 时会被误当作 user-data 走 AssembleAutoinstall，产出
		// 内容错误但格式仍合法的 YAML，cloud-init 会因为缺少真正的
		// meta-data 内容（实例 id/主机名）而在后续阶段行为异常，且这类
		// 错误不会在渲染层报错——必须在分派处就分清楚两个文件名各自对应
		// 哪个渲染函数。
		if fileName == "meta-data" {
			return RenderMetaData(ctx)
		}
		return AssembleAutoinstall(ctx)
	case model.OSFamilyWindows:
		return RenderUnattend(ctx)
	case model.OSFamilyESXi:
		// 契约缺口（见 esxi.go 文件末尾"契约缺口 1"与本文件末尾
		// contractGaps）：model.AnswerFileName(esxi) 固定为 "boot.cfg"，
		// 且 docs/contracts/http-machine.md 约定 4 明确"{file} 不接受
		// handler 自行拼接的候选名单"——只有一个文件名可以走到这个分支，
		// 没有第二个路径能命中 AssembleESXiKickstart。两个候选实现里，
		// RenderESXiBootCfg 需要"原始 boot.cfg 模板文本"作为输入，而渲染器
		// 是不做 IO 的纯函数（文件顶部注释），RenderContext 里没有任何
		// 字段携带这份原始文本（ArtifactPaths.Extra["boot.cfg"] 是磁盘
		// 相对路径字符串，不是文件内容，见 imports/extract_esxi.go）——
		// 调用 RenderESXiBootCfg 需要先读盘，这里没有合法方式获得那份输入。
		// 保守选择 AssembleESXiKickstart：它是不需要额外磁盘输入的纯函数，
		// 与本方法"零 IO"的约束兼容；代价是产出的正文是 ESXi kickstart
		// 方言（ks= 指向的那份文件），而不是 mboot.efi 真正解析的 boot.cfg
		// 引导参数文件——这意味着 boot.cfg 的三处必需改写（prefix=/
		// kernelopt= 的 ks=/modules= 去前导斜杠）目前没有任何调用点会
		// 执行，mboot.efi 拿到的仍是导入时原样保存的模板，不会指向正确的
		// HTTP 地址。这不是"看起来能用但细节有误"，是"这条链路目前打不通"，
		// 已在 contractGaps 完整记录，需要 gateway 侧另开一个可以做磁盘
		// IO 的调用点（读 Extra["boot.cfg"] 路径 → RenderESXiBootCfg）
		// 才能真正解决，本方法的签名（RenderContext, string) (string,
		// error)，纯函数、无 IO）无法安全地自行读盘。
		return AssembleESXiKickstart(ctx)
	case model.OSFamilySUSE:
		// AutoYaST 的渲染实现（AssembleAutoYaST）在本 worktree 内不存在
		// （已检查 internal/render/ 下所有文件），没有实现可接。SUSE 与
		// kickstart 分支的隔离仍然成立：它落"未实现"分支，绝不会被送进
		// AssembleKickstart——那是 RHEL 专属的 case，SUSE 不会匹配到它。
		return "", unimplementedFamilyError(ctx.Artifact.OSFamily)
	default:
		return "", fmt.Errorf("render: 未知的 OS 家族 %q，无法渲染应答文件", ctx.Artifact.OSFamily)
	}
}

// validateAnswerFileName 校验 fileName 是否是该任务允许的应答文件名。
//
// Ubuntu 家族额外接受 "meta-data"：NoCloud 数据源（ds=nocloud-net）要求
// 目录下同时存在 user-data 与 meta-data 两个文件，cloud-init 会依次取两者
// （SPEC §4.5）。其它家族只接受 ctx.AnswerFileName() 算出的唯一文件名。
func validateAnswerFileName(ctx model.RenderContext, fileName string) error {
	want, ok := ctx.AnswerFileName()
	if !ok {
		return fmt.Errorf("render: 无法确定 OS 家族 %q 的应答文件名", ctx.Artifact.OSFamily)
	}
	if fileName == want {
		return nil
	}
	if ctx.Artifact.OSFamily == model.OSFamilyUbuntu && fileName == "meta-data" {
		return nil
	}
	return fmt.Errorf("render: 请求的应答文件名 %q 与该任务应有的文件名 %q 不一致"+
		"——文件名来自机器请求的 URL 路径段，不能用它选择渲染哪种格式的应答文件，"+
		"否则机器可以通过改路径段拿到另一种模板内容", fileName, want)
}

// 编译期接口断言（计划任务 8："编译期接口断言放文件末尾"）。
var _ contract.Renderer = (*Renderer)(nil)
