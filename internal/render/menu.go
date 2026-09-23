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
// RHEL/Debian/Ubuntu/Windows/ESXi 五个家族已接线到各自的渲染实现（本函数
// 即 t08-dispatch 一轨的接线点，归它独占，见文件顶部说明）；SUSE 仍留在
// "未实现"分支——AutoYaST 的渲染实现（AssembleAutoYaST/RenderAutoYaSTMenu）
// 在本 worktree 内不存在（已检查 internal/render/ 下所有文件，没有任何
// 名字含 "AutoYaST" 或 "suse" 的导出函数），没有实现可接，接了会编译失败。
// 见文件末尾 contractGaps。
//
// Windows 与 ESXi 的接线细节：
//   - Windows：RenderWindowsMenu（winpe.go）内部已经自己做了"实际固件 vs
//     任务 pin 固件"的硬校验（§4.6），本分支不重复；
//   - ESXi：RenderESXiMenuLine（esxi.go）需要 mbootEFIPath 与
//     answerFileURL 两个额外参数，本分支负责从 ctx 拼出它们——这是
//     renderESXiMenu 辅助函数存在的原因，与 renderRockyMenu 自己从 ctx
//     拼 URL 是同一模式，不是 esxi.go 的职责（esxi.go 顶部注释也说明了
//     "接线由编排方完成"，即本文件）。ESXi 只支持 UEFI（§0.3 附录
//     A.14）：BIOS+ESXi 的拒绝已经在 RenderESXiMenuLine 内部用
//     ctx.ActualFirmware 校验（返回 ErrESXiRequiresUEFI），本分支不重复
//     判断，直接把 ctx 转发过去。
func RenderMenu(ctx model.RenderContext) (string, error) {
	switch ctx.Artifact.OSFamily {
	case model.OSFamilyRHEL:
		return renderRockyMenu(ctx)
	case model.OSFamilyDebian:
		return renderDebianMenu(ctx)
	case model.OSFamilyUbuntu:
		return renderUbuntuMenu(ctx)
	case model.OSFamilyWindows:
		return RenderWindowsMenu(ctx)
	case model.OSFamilyESXi:
		return renderESXiMenu(ctx)
	case model.OSFamilySUSE:
		return "", unimplementedFamilyError(ctx.Artifact.OSFamily)
	default:
		return "", fmt.Errorf("render: 未知的 OS 家族 %q，无法渲染菜单", ctx.Artifact.OSFamily)
	}
}

// esxiMbootEFIExtraKey 是 ArtifactPaths.Extra 中 ESXi 引导器文件的键名
// （与 esxi.go／imports 侧对 mboot.efi 的约定键一致——ESXi 的制品集只有
// 这一个 Extra 项要在菜单渲染时读取，boot.cfg 模板本身走应答文件分派，
// 不在菜单渲染路径里）。
const esxiMbootEFIExtraKey = "mboot.efi"

// renderESXiMenu 是 ESXi 菜单渲染的分派辅助函数：从 ctx 里取出
// RenderESXiMenuLine 需要的 mbootEFIPath 与 answerFileURL 两个参数，再转发
// 过去。拼 answerFileURL 的方式与 renderRockyMenu 拼 ksURL 完全一致（同一个
// GatewayURL("v1","answer",taskID,token,文件名) 模式），确保 ESXi 与 RHEL
// 两条路径对"应答文件 URL 怎么拼"这件事没有第二种写法。
func renderESXiMenu(ctx model.RenderContext) (string, error) {
	mbootEFIPath, ok := ctx.Artifact.Paths.Extra[esxiMbootEFIExtraKey]
	if !ok || mbootEFIPath == "" {
		return "", fmt.Errorf(
			"render: ESXi 制品集缺少 ArtifactPaths.Extra[%q]，无法渲染引导菜单", esxiMbootEFIExtraKey)
	}

	answerFileName, ok := ctx.AnswerFileName()
	if !ok {
		return "", fmt.Errorf("render: 无法确定 OS 家族 %q 的应答文件名", ctx.Artifact.OSFamily)
	}
	answerFileURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, answerFileName)

	return RenderESXiMenuLine(ctx, mbootEFIPath, answerFileURL)
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

// renderDebianMenu 渲染 Debian 家族的两行菜单：内核行带自动应答开关
// （`auto=true priority=critical`）、优先级设置与应答文件地址
// （`preseed/url=`），加 initrd 行（SPEC §4.5 的 Debian 示例）。
//
// token 走路径段（SPEC §4.4："Debian preseed/url= 中带 & 在内核命令行上
// 也容易出事，token 走路径段"）——ctx.Bases.GatewayURL 拼出的 URL 本身
// 不含 query，这条约束由 BaseURLs 的拼接函数与 AnswerFileName 的调用方式
// 共同保证，本函数不需要额外处理。
func renderDebianMenu(ctx model.RenderContext) (string, error) {
	kernelURL := ctx.Bases.ArtifactURL(ctx.Artifact.Paths.Kernel)
	answerFileName, ok := ctx.AnswerFileName()
	if !ok {
		return "", fmt.Errorf("render: 无法确定 OS 家族 %q 的应答文件名", ctx.Artifact.OSFamily)
	}
	preseedURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, answerFileName)

	if len(ctx.Artifact.Paths.Initrd) == 0 {
		return "", fmt.Errorf("render: Debian 制品集缺少 initrd 路径")
	}
	initrdURL := ctx.Bases.ArtifactURL(ctx.Artifact.Paths.Initrd[0])

	return fmt.Sprintf(
		"kernel %s auto=true priority=critical \\\n"+
			"  preseed/url=%s\n"+
			"initrd %s\n",
		kernelURL, preseedURL, initrdURL,
	), nil
}

// renderUbuntuMenu 渲染 Ubuntu 家族的两行菜单：内核行带 autoinstall 开关
// 与数据源地址（`ds=nocloud-net;s=<url>`，结尾带斜杠），加 initrd 行
// （SPEC §4.5 的 Ubuntu 示例）。
//
// 两处易错点（计划任务 6"两处易错点已在前面标出"）：
//  1. `s=<url>` 必须是目录且结尾带斜杠（SPEC §4.4）——本函数通过给
//     GatewayURL 的最后一段拼接方式传入 InstallToken+"/"（而不是
//     InstallToken 本身）来保证这一点：model.joinURL 只在"最后一段本身
//     以 / 结尾"时才保留结尾斜杠（见 render_test.go 的
//     TestBaseURLs_TrailingSlashPreserved），这里故意不经过
//     AnswerFileName 拼具体文件名，因为 ds=nocloud-net 要求的是"该任务
//     answer 目录本身"，不是某个具体文件——cloud-init 会在这个目录下
//     自己依次请求 user-data 与 meta-data 两个文件。
//  2. 三连横线分隔符（`---`）之后的参数传给目标系统而非 installer——
//     本函数只渲染 installer 侧参数（autoinstall 加数据源地址），不追加
//     `---` 之后的任何目标系统参数（MVP 范围内没有需要透传给目标系统的
//     内核参数），但仍然把 `---` 本身写在行尾，与 SPEC §4.5 的示例一致
//     （即使右侧当前为空，分隔符的位置本身也是"正确" autoinstall
//     引导行的组成部分，遗漏它不算错误但保留它更贴近真实可核对的形态）。
func renderUbuntuMenu(ctx model.RenderContext) (string, error) {
	kernelURL := ctx.Bases.ArtifactURL(ctx.Artifact.Paths.Kernel)
	answerDirURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken+"/")

	if len(ctx.Artifact.Paths.Initrd) == 0 {
		return "", fmt.Errorf("render: Ubuntu 制品集缺少 initrd 路径")
	}
	initrdURL := ctx.Bases.ArtifactURL(ctx.Artifact.Paths.Initrd[0])

	return fmt.Sprintf(
		"kernel %s autoinstall ds=nocloud-net;s=%s ---\n"+
			"initrd %s\n",
		kernelURL, answerDirURL, initrdURL,
	), nil
}
