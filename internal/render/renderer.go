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
	case model.OSFamilyUbuntu, model.OSFamilyDebian, model.OSFamilySUSE,
		model.OSFamilyWindows, model.OSFamilyESXi:
		// SUSE 用 AutoYaST，不是 kickstart（SPEC §5.1、计划任务 6/8 的
		// 反复警示）。这里的分支结构本身就是防线：SUSE 与其它四个未实现
		// 家族一样落进"未实现"分支，绝不会有任何路径把 SUSE 送进
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
