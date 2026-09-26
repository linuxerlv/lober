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

// linuxConsoleArgs 是四个 Linux 家族共用的控制台内核参数。
//
// **没有它，装机过程全程不可观测。** 实测（2026-09-26，QEMU BIOS 目标机）：
// iPXE 的输出一直正常到 `Probing EDD ... ok`，内核接管后清屏，此后串口一个
// 字节都没有——anaconda 在跑，但输出全部送到 tty1，而那块屏幕没人看得到。
// 它卡在装包前的某处时，唯一能看到的只有「服务端没收到下一个信号」。
//
// 这不是「调试不便」这一级的问题：真机排障同样如此。SPEC §0.1 把 iPXE 之前的
// 故障划为范围外、要求运维去 BMC 控制台看，而 BMC 的串口重定向拿到的正是
// ttyS0——内核不往那里写，BMC 上也是一片空白。
//
// 两个 console= 都给：内核对重复的 console= 取并集（**所有**都收到内核消息），
// 把**最后一个**当主控制台（交互用）。顺序是 ttyS0 在前、tty0 在后，于是有
// 物理显示器的真机仍然在屏幕上看到交互界面，而串口/BMC 侧同时拿到完整日志。
// 反过来写会让机房里的人对着黑屏。
//
// 115200n8 是 BMC 串口重定向的事实标准速率（IPMI SOL 与绝大多数厂商的默认
// 值），也是 QEMU 的默认值，两边不需要额外配置就能对上。
//
// Windows 与 ESXi 不用这个常量：wimboot 与 mboot 各有自己的控制台机制
// （BCD 的 bootdebug / boot.cfg 的 tty2Port），不是内核命令行上的事。
const linuxConsoleArgs = "console=ttyS0,115200n8 console=tty0"

// anacondaAutoArgs 是 RHEL 系全自动装机必需的两个 anaconda 参数。
//
// **inst.cmdline 而不是 inst.text。** 两者都是「非图形」，但交互性完全不同：
//
//	inst.text     文本 TUI。仍然是交互式的——anaconda 起 tmux、画界面、等键盘
//	              输入。kickstart 没覆盖到的问题它会停下来问。
//	inst.cmdline  非交互。输出直接往控制台打，遇到未应答的问题立刻失败退出。
//
// 实测（2026-09-26，QEMU BIOS 目标机，串口已配 console=ttyS0 所以看得见）：
// inst.text 下 installer 起到
//
//	Created slice system-anaconda\x2dtmux.slice
//	Starting anaconda-pre.service - pre-anaconda logging service...
//	Starting plymouth-quit-wait.service - Hold until boot process finishes up...
//
// 就停住了，内核时间戳冻结在 39 秒，此后七分钟无任何输出——anaconda 在等一个
// 可交互终端，而 -display none 的虚拟机（以及真实机房里没插显示器的服务器）
// 给不了它。
//
// SPEC §4.5 的示例写的是 inst.text，那是示例本身的问题：它没有在真机上跑过。
//
// **plymouth.enable=0**：plymouth 是引导动画，它的 plymouth-quit-wait.service
// 语义是「等引导过程结束」——无图形环境下那个条件不会满足，服务一直 Starting
// （同一次实测里它与 anaconda-pre 一起挂着）。装机不需要动画，关掉它少一类
// 挂起原因，也让串口输出干净（plymouth 会抢控制台）。
//
// 排在模板 KernelArgs 之前：运维想临时换回 inst.text 调试某台机器时，在
// KernelArgs 里给一个就能覆盖（anaconda 对冲突的显示模式参数取最后一个）。
const anacondaAutoArgs = "inst.cmdline inst.notmux plymouth.enable=0"

// 为什么还要 inst.notmux：anaconda 默认把自己包在 tmux 里（实测串口上能看到
// `Created slice system-anaconda-tmux.slice`），而 tmux 的输出不会进串口，
// 于是它报错退出时串口上只有一段空白——实测 2026-09-26：日志从内核 42 秒直接
// 跳到 251 秒，中间 209 秒的 anaconda 输出全部丢失，然后
// `dracut Warning: Killing all remaining processes` → 重启。
//
// kickstart 的 `logging --host=` 指令也救不了：那是 anaconda **启动之后**才
// 生效的转发，而它在启动阶段就失败了（实测服务端只收到 %pre hook 的输出，
// anaconda 自己一条都没有）。
//
// notmux 让 anaconda 直接往控制台写，配合 console=ttyS0 才能看到失败原因。
// 这不只是调试参数：真机排障同样依赖它，否则运维在 BMC 上看到的也是同一段空白。

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

	// stage2 与包仓库是**两个不同的位置**，各用一个参数指——真实 RHEL 9 系
	// 介质上它们不是同一个目录，用一个参数指必然有一边取不到（两种错法都实测
	// 过，症状不同但都是装机中止）：
	//
	//	inst.stage2  安装介质根（MediaRoot），其下有 images/install.img
	//	             （installer 的运行时镜像）与 .treeinfo。
	//	inst.repo    包仓库（Repo），其下有 repodata/repomd.xml。真实 RHEL 9
	//	             系在 BaseOS/ 下，不在介质根。
	//
	// 错法一（只用 inst.repo 指包仓库）：anaconda 取 stage2 时
	// `repo/BaseOS//images/install.img` 404，任务停在 installing/pre。
	//
	// 错法二（只用 inst.repo 指介质根）：stage2 取到了（install.img 200，
	// 933MB），但装包阶段 `repo/repodata/repomd.xml` 404 重试四次，任务停在
	// installing/packages，目标盘一个字节都没写。
	//
	// 两个参数各指各的位置才两边都对，这也是 anaconda 文档里网络安装的标准
	// 写法。扁平布局（RHEL 7 系与精简介质，repodata 直接在介质根）下两者指向
	// 同一个位置，那是正确的、不是退化。
	stage2URL := ctx.SetRepoURL(ctx.Artifact.Paths.MediaRootOrRepo())
	repoURL := ctx.SetRepoURL(ctx.Artifact.Paths.Repo)

	answerFileName, ok := ctx.AnswerFileName()
	if !ok {
		return "", fmt.Errorf("render: 无法确定 OS 家族 %q 的应答文件名", ctx.Artifact.OSFamily)
	}
	ksURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, answerFileName)

	if len(ctx.Artifact.Paths.Initrd) == 0 {
		return "", fmt.Errorf("render: RHEL 制品集缺少 initrd 路径")
	}
	initrdURL := ctx.SetArtifactURL(ctx.Artifact.Paths.Initrd[0])

	// installer 启动阶段的日志转发。**kickstart 的 `logging --host=` 指令救不了
	// 启动阶段的失败**：那一行是 anaconda 启动之后才解析并生效的，而 installer
	// 在启动阶段失败时它还没被读到。内核参数从 dracut 阶段就生效。
	//
	// 实测（2026-09-26）：串口时间戳从内核 40 秒直接跳到 254 秒，中间 213 秒的
	// installer 输出一个字节都没留下，服务端同期只收到 %pre hook 的输出（那是
	// 我们自己的 signal 函数发的）。「装机为什么失败」在缺这个参数时无法回答
	// ——串口空白、syslog 空白、目标盘没挂载所以 /tmp 里的 anaconda.log 也
	// 取不到。SPEC §10 的失败模式表要求把失败原因呈现给运维，而这条链路上它
	// 根本不存在。
	//
	// SyslogHost 为空时整个参数省掉，不渲染 `inst.syslog=:514`：空主机名会让
	// anaconda 每条日志都发失败一次，拖慢整个安装。
	syslogArg := ""
	if ctx.SyslogHost != "" {
		syslogArg = fmt.Sprintf(" inst.syslog=%s:%d", ctx.SyslogHost, ctx.SyslogPort)
	}

	// 模板声明的额外内核参数（Template.KernelArgs）。为空时 suffix 是空串，
	// 输出与接线之前逐字节一致。
	kargs, err := kernelArgsSuffix(ctx.Template.KernelArgs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"kernel %s \\\n"+
			"  inst.stage2=%s \\\n"+
			"  inst.repo=%s \\\n"+
			"  inst.ks=%s "+anacondaAutoArgs+" ip=dhcp "+linuxConsoleArgs+"%s%s\n"+
			"initrd %s\n"+
			ipxeBootCommand,
		kernelURL, stage2URL, repoURL, ksURL, syslogArg, kargs, initrdURL,
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
			"  preseed/url=%s "+linuxConsoleArgs+"%s\n"+
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
		"kernel %s autoinstall ds=nocloud-net;s=%s "+linuxConsoleArgs+"%s ---\n"+
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
