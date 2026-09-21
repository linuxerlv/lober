// 本文件实现 iPXE 菜单渲染（对应实现计划任务 7，依据 SPEC §4.5、§4.7）。
//
// 菜单是引导链的第二层分流：所有机器拿到同一份首跳脚本（/boot.ipxe），
// 差异全部体现在本文件渲染出的这几行文本里（SPEC §4.7）。
package render

import (
	"fmt"

	"github.com/lober/lober/internal/core/model"
)

// RenderMenu 按 OS 家族渲染 iPXE 菜单文本（contract.Renderer.RenderMenu 的
// 实现之一）。
//
// MVP 只有 RHEL 家族（Rocky 9）真正实现；其余五个家族留分支返回"未实现"
// 错误——分支位置就是 Phase 2/3 的锚点，不为了"看起来完整"而实现半个
// Ubuntu 渲染逻辑（没有 golden file 覆盖、没有真机验证的渲染逻辑比明确的
// "未实现"更危险，计划"MVP 范围提示"）。
func RenderMenu(ctx model.RenderContext) (string, error) {
	switch ctx.Artifact.OSFamily {
	case model.OSFamilyRHEL:
		return renderRockyMenu(ctx)
	case model.OSFamilyUbuntu, model.OSFamilyDebian, model.OSFamilySUSE,
		model.OSFamilyWindows, model.OSFamilyESXi:
		return "", unimplementedFamilyError(ctx.Artifact.OSFamily)
	default:
		return "", fmt.Errorf("render: 未知的 OS 家族 %q，无法渲染菜单", ctx.Artifact.OSFamily)
	}
}

// unimplementedFamilyError 统一构造"该家族 MVP 未实现"的错误，携带具体
// 家族名（供上层日志与人工诊断直接可读，也供测试断言错误信息里包含家族名）。
func unimplementedFamilyError(family model.OSFamily) error {
	return fmt.Errorf("render: OS 家族 %q 的 iPXE 菜单渲染在 MVP 范围内未实现"+
		"（Phase 2/3 才会补齐；SUSE 需要注意它用 AutoYaST 而非 kickstart，"+
		"不能路由到本渲染器的 kickstart 分支）", family)
}

// renderRockyMenu 渲染 RHEL 家族（Rocky 9）的四行菜单：kernel 行（带仓库
// 地址、应答文件地址、文本模式与网络配置）、initrd 行（SPEC §4.5 的 RHEL
// 示例）。
//
// 三个 URL 全部从 ctx.Bases 的方法拼出（不出现字面 scheme，SPEC §1.5），
// token 在路径段里、不在 query（SPEC §4.4）。
func renderRockyMenu(ctx model.RenderContext) (string, error) {
	kernelURL := ctx.Bases.ArtifactURL(ctx.Artifact.Paths.Kernel)
	repoURL := ctx.Bases.RepoURL(ctx.Artifact.Paths.Repo)
	answerFileName, ok := ctx.AnswerFileName()
	if !ok {
		return "", fmt.Errorf("render: 无法确定 OS 家族 %q 的应答文件名", ctx.Artifact.OSFamily)
	}
	ksURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, answerFileName)

	if len(ctx.Artifact.Paths.Initrd) == 0 {
		return "", fmt.Errorf("render: RHEL 制品集缺少 initrd 路径")
	}
	initrdURL := ctx.Bases.ArtifactURL(ctx.Artifact.Paths.Initrd[0])

	return fmt.Sprintf(
		"kernel %s \\\n"+
			"  inst.repo=%s \\\n"+
			"  inst.ks=%s inst.text ip=dhcp\n"+
			"initrd %s\n",
		kernelURL, repoURL, ksURL, initrdURL,
	), nil
}
