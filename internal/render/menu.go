// 本文件实现 iPXE 菜单渲染（对应实现计划任务 7，依据 SPEC §4.5、§4.7）。
//
// 菜单是引导链的第二层分流：所有机器拿到同一份首跳脚本（/boot.ipxe），
// 差异全部体现在本文件渲染出的这几行文本里（SPEC §4.7）。
package render

import (
	"fmt"
	"strings"

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
	body, err := renderMenuBody(ctx)
	if err != nil {
		return "", err
	}
	// #!ipxe 加在这个统一出口，而不是让六个家族各自拼一遍：它是「iPXE 脚本」
	// 这个产物的固有属性，与 OS 家族无关。六处各写一遍的话，新增家族时漏掉
	// 一处的后果是那个家族的机器全部装不上，而症状（机器在 PXE 里转圈）指向
	// 固件与 DHCP，排查不会怀疑渲染器。
	return ipxeShebang + body, nil
}

// ipxeShebang 是 iPXE 脚本的首行声明。
//
// **iPXE 只有在首行是它时才把内容当脚本执行。** 缺了它，chain 拿到的是一坨
// 无法识别的数据——不报错、不执行、直接返回，机器回落到固件引导顺序继续
// PXE 轮询。
//
// 实测（2026-09-25，QEMU BIOS 目标机接真实服务端）：目标机反复取到 434 字节
// 的完整菜单（kernel/initrd/boot 三行齐全、URL 全部正确、手动 curl 那几个
// URL 都是 200，14.5MB 的 vmlinuz 与 105MB 的 initrd 都能下全），而 vmlinuz
// 与 initrd.img 的 GET 一次都没从目标机发出过。
//
// 对照组说明了这个缺口的形状：no_task 那条路径（contract.NoTaskScript）一直
// 带 #!ipxe，所以「查不到任务」时机器能正确执行 sanboot 回落；唯独**成功
// 路径**的菜单没有——此前这个系统只有失败路径能被 iPXE 执行。
const ipxeShebang = "#!ipxe\n"

// renderMenuBody 按 OS 家族分派菜单正文（不含 #!ipxe 首行，那由 RenderMenu
// 统一补）。
func renderMenuBody(ctx model.RenderContext) (string, error) {
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
		// 菜单用 autoyast=<URL> 指向 autoinst.xml，不是 inst.ks=（见
		// renderer.go 的 SUSE case 注释）。
		return RenderAutoYaSTMenu(ctx)
	default:
		return "", fmt.Errorf("render: 未知的 OS 家族 %q，无法渲染菜单", ctx.Artifact.OSFamily)
	}
}

// ipxeBootCommand 是每份 iPXE 菜单的最后一行。
//
// **没有它，任何机器都装不上，而服务端侧看起来一切正常。** iPXE 的 kernel
// 与 initrd 命令只是把文件加载进内存并设置引导参数，真正把控制权交给内核的
// 是 boot。缺了它，iPXE 加载完镜像就走到脚本末尾，chain 返回后落到
// /boot.ipxe 的 `|| shell` 或直接退出，机器回到固件引导顺序继续 PXE 轮询。
//
// 现场症状是「机器在 PXE 里反复转圈」——而 SPEC §0.1 恰好把这类现象划为
// 范围外（判据在固件与 DHCP 里，本系统零可观测性），排查方向会被带去查
// BIOS 设置、DHCP 配置、网线。同时服务端侧每一项指标都正常：任务进了
// booting、nginx access log 里菜单请求 200、字节数也对。
//
// 实测（2026-09-25，QEMU BIOS 目标机走真实引导链）：目标机成功取到
// /boot.ipxe 与 /v1/boot（429 字节菜单，不是 76 字节的 no_task），此后
// vmlinuz 与 initrd.img 的 GET 一次都没出现过。
//
// 必须排在 initrd 行之后：iPXE 顺序执行，boot 之前 kernel 与 initrd 都要
// 加载完，否则它拿一个不完整的引导上下文去跳转。
const ipxeBootCommand = "boot\n"

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
	kernelURL := ctx.SetArtifactURL(ctx.Artifact.Paths.Kernel)
	// inst.repo 指向**安装介质根**，不是包索引目录：anaconda 从这一层下面取
	// stage2 运行时镜像（images/install.img）与 .treeinfo。真实 RHEL 9 系
	// 布局下 repodata 在 BaseOS/ 下、images/ 在介质根，两者不是同一个目录
	// ——指错的后果是 anaconda 取 stage2 时 404、任务停在 installing 不再
	// 前进（见 model.ArtifactPaths.MediaRoot 的文档与实测记录）。
	repoURL := ctx.SetRepoURL(ctx.Artifact.Paths.MediaRootOrRepo())
	answerFileName, ok := ctx.AnswerFileName()
	if !ok {
		return "", fmt.Errorf("render: 无法确定 OS 家族 %q 的应答文件名", ctx.Artifact.OSFamily)
	}
	ksURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, answerFileName)

	if len(ctx.Artifact.Paths.Initrd) == 0 {
		return "", fmt.Errorf("render: RHEL 制品集缺少 initrd 路径")
	}
	initrdURL := ctx.SetArtifactURL(ctx.Artifact.Paths.Initrd[0])

	// 模板声明的额外内核参数（Template.KernelArgs）。为空时 suffix 是空串，
	// 输出与接线之前逐字节一致。
	kargs, err := kernelArgsSuffix(ctx.Template.KernelArgs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"kernel %s \\\n"+
			"  inst.repo=%s \\\n"+
			"  inst.ks=%s inst.text ip=dhcp%s\n"+
			"initrd %s\n"+
			ipxeBootCommand,
		kernelURL, repoURL, ksURL, kargs, initrdURL,
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
	kernelURL := ctx.SetArtifactURL(ctx.Artifact.Paths.Kernel)
	answerFileName, ok := ctx.AnswerFileName()
	if !ok {
		return "", fmt.Errorf("render: 无法确定 OS 家族 %q 的应答文件名", ctx.Artifact.OSFamily)
	}
	preseedURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, answerFileName)

	if len(ctx.Artifact.Paths.Initrd) == 0 {
		return "", fmt.Errorf("render: Debian 制品集缺少 initrd 路径")
	}
	initrdURL := ctx.SetArtifactURL(ctx.Artifact.Paths.Initrd[0])

	// 模板声明的额外内核参数（Template.KernelArgs）。为空时 suffix 是空串，
	// 输出与接线之前逐字节一致。
	kargs, err := kernelArgsSuffix(ctx.Template.KernelArgs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"kernel %s auto=true priority=critical \\\n"+
			"  preseed/url=%s%s\n"+
			"initrd %s\n"+
			ipxeBootCommand,
		kernelURL, preseedURL, kargs, initrdURL,
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
	kernelURL := ctx.SetArtifactURL(ctx.Artifact.Paths.Kernel)
	answerDirURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken+"/")

	if len(ctx.Artifact.Paths.Initrd) == 0 {
		return "", fmt.Errorf("render: Ubuntu 制品集缺少 initrd 路径")
	}
	initrdURL := ctx.SetArtifactURL(ctx.Artifact.Paths.Initrd[0])

	// 模板声明的额外内核参数（Template.KernelArgs）。为空时 suffix 是空串，
	// 输出与接线之前逐字节一致。
	kargs, err := kernelArgsSuffix(ctx.Template.KernelArgs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"kernel %s autoinstall ds=nocloud-net;s=%s%s ---\n"+
			"initrd %s\n"+
			ipxeBootCommand,
		kernelURL, answerDirURL, kargs, initrdURL,
	), nil
}

// kernelArgsSuffix 把模板声明的 Template.KernelArgs 变成可直接拼在 kernel
// 行末尾的片段（前导空格自带，为空时返回空串）。
//
// **为什么需要它。** Template.KernelArgs 此前定义了但全仓没有任何渲染代码
// 读它：model 有字段、API 收得进来、store 落了 kernel_args 列、契约文档也
// 冻结了它，而渲染器一行都不读——运维填了内核参数，装机时完全不生效，且
// 没有任何地方报错或提示。
//
// 这个字段承载的正是「同一份 ISO 派生多个模板」这个模型里最需要的灵活性：
// 制品集固定（一个 ISO 一套 kernel/initrd），而串口重定向
// （console=ttyS0,115200n8）、网卡命名策略（net.ifnames=0）、显卡驱动黑
// 名单、安装器调试开关都是内核命令行上的事，不同机型与用途要填不同的值。
//
// 刻意不做的事：**不让模板存整段 iPXE 脚本**。那等于把 kernel/initrd 的 URL
// 拼接、token 注入、制品路径交给运维手写，而这些恰是服务端唯一有正确信息的
// 部分（token 是本次引导当场签的，制品路径带 artifact_set ID）。手写一份就
// 意味着每次改制品目录结构都要同步所有模板。模板只声明「追加这些参数」，
// URL 拼接仍归服务端。
func kernelArgsSuffix(args string) (string, error) {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return "", nil
	}
	if err := validateKernelArgs(trimmed); err != nil {
		return "", err
	}
	return " " + trimmed, nil
}

// validateKernelArgs 拒绝含换行的内核参数。
//
// 这是这条路径上唯一的安全校验，但它不可省：内核命令行与 inst.ks= 在同一
// 行上，一个换行就能结束 kernel 命令并注入任意 iPXE 命令——而 iPXE 脚本里
// 可以 chain 到任意 URL、可以 sanboot 本地盘。
//
// 模板由 admin 编写（§9.4 把模板与 provider 并列为需代码评审级别的写权限），
// 但「需要评审的权限」不等于「可以跳过校验」：评审看的是意图，校验挡的是
// 手误（从文档里复制粘贴带进了换行）与被篡改的模板内容。
//
// 只挡换行，不做参数白名单：内核命令行的合法取值集合由目标 OS 的内核与
// 安装器决定，不是本系统能枚举的（inst.* 是 anaconda 的、preseed/* 是
// debian-installer 的、各家还有大量驱动参数），做白名单只会在运维需要一个
// 没被列进去的参数时逼他绕过这一层。
func validateKernelArgs(args string) error {
	if strings.ContainsAny(args, "\r\n") {
		return fmt.Errorf("render: 模板的 KernelArgs 含换行符，拒绝渲染"+
			"——内核命令行与 inst.ks= 在同一行上，换行能结束 kernel 命令并注入"+
			"任意 iPXE 命令（chain 到任意 URL、sanboot 本地盘）。收到的值: %q", args)
	}
	return nil
}
