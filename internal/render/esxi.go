// 本文件实现 ESXi 的引导配置文件渲染与 ESXi 方言 kickstart 组装（对应实现
// 计划 docs/superpowers/plans/2026-09-20-lober-phase3-02-esxi.md 任务 2、
// 任务 3，依据 SPEC §0.3、§2.1、§4.5、§4.6、§5.2、§6.5）。
//
// ESXi 的引导形态与 RHEL/Ubuntu/Debian 完全不同：它没有 iPXE 的
// kernel+initrd 两行结构，而是一个引导器（mboot.efi）加一个引导配置文件
// （boot.cfg）。boot.cfg 本身不是 kickstart 语法，是 mboot.efi 的参数文件，
// 其中列出全部要加载的模块与内核选项；真正驱动无人值守安装的是 boot.cfg
// 的 kernelopt 行里 `ks=` 指向的另一份文件——ESXi 自己的 kickstart 方言。
//
// 这是本文件与 kickstart.go（RHEL 专属）刻意分离实现的原因：两者都叫
// "kickstart"，但语法不同、阶段模型不同（ESXi 没有 post-nochroot，firstboot
// 原生支持），直接复用会把 anaconda 特有的段落头/hook 阶段映射套到一个完全
// 不认识这些指令的安装器上，是计划文档反复警示的错误（同样的警告适用于
// SUSE 的 AutoYaST）。
//
// 本文件产出三类独立可调用的导出函数：
//
//  1. RenderESXiBootCfg：改写导入时保存的原始 boot.cfg 模板，三处替换
//     （prefix、kernelopt 的 ks=、模块路径前导斜杠）。
//  2. RenderESXiMenuLine：渲染 ESXi 的 iPXE 菜单唯一一行
//     （`mboot.efi -c <boot.cfg URL>`），内含 UEFI-only 的纵深防御拒绝。
//  3. AssembleESXiKickstart：组装 ESXi 方言的 kickstart 应答文件正文。
//
// 三者都供 menu.go 的 ESXi 分支与 renderer.go 的 RenderAnswerFile 分派
// 调用（menu.go 归属 P2-multios，renderer.go 不在本次任何单个 agent 的
// 文件归属内），接线由编排方完成，见文件末尾 contractGaps 说明。
package render

import (
	"fmt"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// 任务 2：boot.cfg 渲染（三处改写）
// ---------------------------------------------------------------------------

// esxiBootCfgPrefixKey / esxiBootCfgKernelOptKey / esxiBootCfgModulesKey 是
// boot.cfg 中三个需要识别的键名（真实 ESXi boot.cfg 的固定格式，逐行
// `key=value`）。
const (
	esxiBootCfgPrefixKey    = "prefix"
	esxiBootCfgKernelOptKey = "kernelopt"
	esxiBootCfgModulesKey   = "modules"
)

// esxiModulesSeparator 是 modules= 行里模块路径之间的分隔符（真实 ESXi
// boot.cfg 格式：`modules=a.b00 --- b.b00 --- c.b00`）。
const esxiModulesSeparator = " --- "

// RenderESXiBootCfg 改写导入时保存的原始 boot.cfg 模板，产出可直接下发给
// mboot.efi 的最终版本（计划任务 2）。
//
// 输入是原始模板文本（导入时从 ISO 里原样取出，未经任何修改——不要从零
// 生成这份文件，理由见本文件顶部注释），输出是改写后的版本。逐行处理，
// 只改三类行，其余原样输出（计划任务 2 明确的实现方式）：这样新 ESXi
// 版本给 boot.cfg 加了新参数时不会被丢掉，因为本函数根本不识别那些新增行、
// 只是原样搬运。
//
// 三处改写（计划任务 2 的表）：
//
//  1. prefix=<ISO 内相对路径> → prefix=<HTTP 路径>：引导器按此前缀解析
//     kernel 与每个模块的完整 URL，不改写会让引导器去本地找文件。
//  2. kernelopt=<原始内核选项，不含 ks=> → 追加 ` ks=<应答文件 URL>`：
//     不改写会导致脚本化安装无法启动（附录 A.8），退回交互式安装。
//  3. modules=<以 --- 分隔的模块路径列表>：每个模块路径若带前导斜杠，去掉
//     它。这是计划文档点名"最容易漏"的一条——prefix 已经是 HTTP 路径时，
//     模块路径若保留前导斜杠，`prefix + "/" + "/module"` 拼出的 URL 会出现
//     双斜杠，现场表现是"引导器报某个模块取不到"，而运维会去查 nginx 而
//     不是查这个渲染逻辑。
//
// bootCfgURLBase 是改写后 prefix 应指向的 HTTP 基址（通常是
// ctx.Bases.ArtifactURL(<制品目录>)，由调用方算出并传入——本函数不查
// RenderContext，保持与 kickstart.go 里"逐行文本处理"同一层级的纯函数
// 签名，调用方（menu.go 的 ESXi 分支）负责从 ctx 拼出这个基址）。
// answerFileURL 是改写后 kernelopt 的 ks= 应指向的应答文件 URL（同样由
// 调用方拼好传入，含 per-task token）。
func RenderESXiBootCfg(template string, bootCfgURLBase string, answerFileURL string) (string, error) {
	if strings.TrimSpace(bootCfgURLBase) == "" {
		return "", fmt.Errorf("render: ESXi boot.cfg 渲染缺少制品基址（bootCfgURLBase 为空），" +
			"无法改写 prefix= 行")
	}
	if strings.TrimSpace(answerFileURL) == "" {
		return "", fmt.Errorf("render: ESXi boot.cfg 渲染缺少应答文件 URL（answerFileURL 为空），" +
			"kernelopt= 行若不含 ks= 参数，脚本化安装无法启动（附录 A.8）")
	}

	lines := strings.Split(template, "\n")
	out := make([]string, len(lines))

	sawKernelOpt := false
	for i, line := range lines {
		key, value, isKV := splitBootCfgLine(line)
		if !isKV {
			out[i] = line // 空行、注释行或任何本函数不识别的行，原样保留
			continue
		}

		switch key {
		case esxiBootCfgPrefixKey:
			out[i] = esxiBootCfgPrefixKey + "=" + bootCfgURLBase
		case esxiBootCfgKernelOptKey:
			sawKernelOpt = true
			out[i] = esxiBootCfgKernelOptKey + "=" + rewriteKernelOpt(value, answerFileURL)
		case esxiBootCfgModulesKey:
			out[i] = esxiBootCfgModulesKey + "=" + rewriteModulesLine(value)
		default:
			out[i] = line // 未涉及的键（如 bootstate/title/timeout/build/updated），原样保留
		}
	}

	if !sawKernelOpt {
		return "", fmt.Errorf("render: 原始 boot.cfg 模板中没有找到 kernelopt= 行，" +
			"无法注入应答文件参数——这不是一份可用的 ESXi boot.cfg 模板")
	}

	return strings.Join(out, "\n"), nil
}

// splitBootCfgLine 把一行 boot.cfg 文本拆成 key/value（若它是形如
// `key=value` 的键值行）。空行、不含 `=` 的行返回 isKV=false，原样保留由
// 调用方处理。
func splitBootCfgLine(line string) (key, value string, isKV bool) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", "", false
	}
	return line[:idx], line[idx+1:], true
}

// rewriteKernelOpt 在原始 kernelopt 值末尾追加 ` ks=<answerFileURL>`。
//
// 若原始值已经含 `ks=`（真实 ISO 里的默认值通常是 `ks=cdrom:/KS.CFG` 之类
// 指向本地介质的占位值），必须先剔除它再追加我们的值——否则安装器会看到
// 两个 ks= 参数，行为未定义（多数实现取最后一个生效，但依赖"最后一个生效"
// 这种未文档化行为不可靠，直接剔除更明确）。
func rewriteKernelOpt(original string, answerFileURL string) string {
	fields := strings.Fields(original)
	kept := make([]string, 0, len(fields)+1)
	for _, f := range fields {
		if strings.HasPrefix(f, "ks=") {
			continue // 剔除原始占位值，不保留
		}
		kept = append(kept, f)
	}
	kept = append(kept, "ks="+answerFileURL)
	return strings.Join(kept, " ")
}

// rewriteModulesLine 把 modules= 的值（以 " --- " 分隔的模块相对路径列表）
// 中每一项的前导斜杠去掉，顺序保持不变（顺序是引导要求，计划任务 1/2 均
// 明确警示不能重排）。
func rewriteModulesLine(value string) string {
	parts := strings.Split(value, esxiModulesSeparator)
	for i, p := range parts {
		parts[i] = strings.TrimPrefix(strings.TrimSpace(p), "/")
	}
	return strings.Join(parts, esxiModulesSeparator)
}

// ErrESXiRequiresUEFI 是 RenderESXiMenuLine 在机器实际固件不是 UEFI 时
// 返回的 sentinel error（供调用方用 errors.Is 判定，与本包其它拒绝路径的
// 错误处理风格一致）。
//
// ESXi 只支持 UEFI 是 MVP 的明确范围决定（SPEC §0.3、§1.2、附录 A.14），
// 不是暂未实现——传统 BIOS 需经 syslinux 的 mboot.c32 加 iPXE 的 COMBOOT
// 支持，且该支持仅存在于 BIOS 构建、部分预编译二进制未开启。
var ErrESXiRequiresUEFI = fmt.Errorf("render: ESXi 只支持 UEFI，机器实际固件模式不是 uefi")

// RenderESXiMenuLine 渲染 ESXi 的 iPXE 菜单文本：唯一一行，UEFI 引导器
// 作为"内核"，带一个指向渲染后 boot.cfg 的选项（SPEC §4.5：
// `kernel http://ng/artifacts/esxi8/mboot.efi -c http://gw/v1/answer/42/<token>/boot.cfg`）。
//
// 没有 initrd 行（计划任务 3 明确要求）——ESXi 的模块由 boot.cfg 的
// modules= 行列出并由 mboot.efi 自己按该文件加载，不走 iPXE 的 initrd
// 机制，硬塞一行 initrd 不会被 mboot.efi 使用，只会误导排查。
//
// plat 不是 UEFI 时直接返回 ErrESXiRequiresUEFI，不下发此菜单（SPEC §4.5
// 注明、§4.6 硬拒绝）。这是纵深防御的第二道——第一道在
// internal/statemachine/precheck.go 的 checkFirmware（用
// model.FirmwareCompatible 判定，对 esxi+bios 组合已经生效，这条校验发生
// 在签发 token 与下发任何菜单之前），本函数的检查确保即使调用方跳过了
// precheck 直接调渲染器，也不会产出一份注定失败的菜单——它只包含 UEFI
// 引导器一条路径，硬塞给 BIOS 机器只会在引导层面炸得更晚、更难定位。
//
// answerFileURL 是调用方（menu.go 的 ESXi 分支）按与 RHEL 侧
// renderRockyMenu 同样的方式从 ctx.Bases 与 ctx.Task 拼出的 boot.cfg 应答
// 文件 URL（形如 GatewayURL("v1","answer",taskID,token,"boot.cfg")）。
func RenderESXiMenuLine(ctx model.RenderContext, mbootEFIPath string, answerFileURL string) (string, error) {
	if ctx.ActualFirmware != model.FirmwareUEFI {
		return "", fmt.Errorf("%w（上报固件模式 %q）", ErrESXiRequiresUEFI, ctx.ActualFirmware)
	}
	if strings.TrimSpace(mbootEFIPath) == "" {
		return "", fmt.Errorf("render: ESXi 菜单渲染缺少 mboot.efi 路径")
	}
	if strings.TrimSpace(answerFileURL) == "" {
		return "", fmt.Errorf("render: ESXi 菜单渲染缺少 boot.cfg 应答文件 URL")
	}

	kernelURL := ctx.Bases.ArtifactURL(mbootEFIPath)
	return fmt.Sprintf("kernel %s -c %s\n", kernelURL, answerFileURL), nil
}

// ---------------------------------------------------------------------------
// 任务 3：ESXi 方言 kickstart 组装
// ---------------------------------------------------------------------------

// esxiHookStageDirectives 是 ESXi kickstart 阶段到段落头的映射。刻意不包含
// post-nochroot——SPEC §5.2 的跨家族映射表里 ESXi 那一列在这一行是"—"，即
// 该阶段在 ESXi 上不存在（计划任务 3 第 2 条差异）。若调用方（M6 的模板
// 保存校验）放行了一个声明 post-nochroot 阶段且家族为 esxi 的 hook，本文件
// 的 RenderESXiStageSection 对该阶段返回空字符串（视为"没有这个阶段"），
// 不会 panic，但那意味着该 hook 会被静默丢弃——真正的拒绝点必须在模板保存
// 时（M6 职责），本文件的处理是纵深防御的最后一道，不是主防线，详见文件
// 末尾 contractGaps。
var esxiHookStageDirectives = map[model.HookStage]string{
	model.HookStagePre:  "%pre --interpreter=busybox",
	model.HookStagePost: "%post --interpreter=busybox",
}

// RenderESXiStageSection 渲染 ESXi kickstart 的 pre / post 段落文本（不含
// firstboot——firstboot 由 esxiFirstbootSection 单独处理，且 ESXi 原生支持
// 该阶段，不需要 systemd oneshot 脚手架）。
//
// stage 为 post-nochroot 或任何不在 esxiHookStageDirectives 中的值时返回
// 空字符串（该阶段在 ESXi 上不存在，或没有该阶段的 hook）。
//
// --interpreter=busybox 是 ESXi kickstart 的真实语法（ESXi 的 %pre/%post
// 默认解释器是内置的极简 shell，busybox 选项切到功能更全的 busybox ash，
// 这是运行 ClientLibESXi 注入的 signal/run_hook 函数所必需的——默认解释器
// 不支持函数定义）。
func RenderESXiStageSection(ctx model.RenderContext, stage model.HookStage) (string, error) {
	header, ok := esxiHookStageDirectives[stage]
	if !ok {
		return "", nil
	}

	refs := hooksForStage(ctx.Hooks, stage)
	if len(refs) == 0 {
		return "", nil
	}

	params, err := InlineParams(ctx.Task.ResolvedParams)
	if err != nil {
		return "", fmt.Errorf("render: ESXi %s 阶段参数内联失败: %w", stage, err)
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	// 参数与 fact 注入必须在本段落内重新做一遍，不能依赖某个更早的独立
	// 段落——ESXi 的 %pre/%post/%firstboot 各自是独立执行的脚本（与 RHEL
	// 侧 anaconda 的段落隔离模型同样的道理，见 RenderStageSection 与
	// initScript 对 ClientLib 的重复注入），否则本段落的用户 hook 引用
	// `$LOBER_HOSTNAME` 之类变量时会拿到空值而非真实数据，且不会有任何
	// 报错——这是最难排查的一类静默失败。
	b.WriteString(params)
	b.WriteString(InjectFacts(ctx.Facts))
	b.WriteString(ClientLibESXi(ctx))
	for _, ref := range refs {
		b.WriteString(runHookCallESXi(ref))
	}
	b.WriteString("%end\n")
	return b.String(), nil
}

// runHookCallESXi 渲染一行对 ash 版 run_hook 的调用（与 kickstart.go 的
// runHookCall 同样的必要性：required hook 失败必须让本段落脚本以非零码
// 退出，run_hook 只在"失败且 required"时返回非零，见 ClientLibESXi）。
func runHookCallESXi(ref model.HookRef) string {
	return fmt.Sprintf("run_hook '%s' '%t' '%d' || exit 1\n", ref.HookDigest, ref.Required, defaultHookTimeoutSec)
}

// esxiFirstbootSection 渲染 ESXi 原生的 %firstboot 段落（计划任务 3 第 1
// 条差异："原生支持首次启动段，不需要 Linux 侧那套 systemd oneshot
// 脚手架"）。
//
// 信号语义与 RHEL 侧保持一致（计划明确要求）：仍要发 booted、init-start、
// init-done、ready 四个信号，用 finalize token；区别只是执行环境——这里
// 不需要写 systemd unit、不需要 ConditionPathExists 标记文件、不需要等
// 下一次重启才执行，%firstboot 段落本身就是"首次启动后执行一次"的原生
// 语义（ESXi 安装器保证只执行一次）。
//
// hooks 参数是已过滤好的 firstboot 阶段 hook 引用列表（按 Order 排序，
// 复用 firstboot.go 的 firstbootHookRefsSorted）。
func esxiFirstbootSection(ctx model.RenderContext) (string, error) {
	hooks := firstbootHookRefsSorted(ctx.Hooks)

	params, err := InlineParams(ctx.Task.ResolvedParams)
	if err != nil {
		return "", fmt.Errorf("render: ESXi firstboot 阶段参数内联失败: %w", err)
	}

	var b strings.Builder
	b.WriteString("%firstboot --interpreter=busybox\n")
	// 与 RenderESXiStageSection 同样的理由：%firstboot 是独立执行的脚本，
	// 用户 firstboot hook 同样需要看到 $LOBER_* 变量。
	b.WriteString(params)
	b.WriteString(InjectFacts(ctx.Facts))
	b.WriteString(ClientLibESXi(ctx))
	b.WriteString("\n")

	b.WriteString(`signal "booted" 0` + "\n")
	b.WriteString(`signal "init-start" 0` + "\n\n")

	for i, h := range hooks {
		fileName := hookScriptFileName(h, i)
		dest := "/tmp/lober-hooks/" + fileName
		artifactURL := ctx.Bases.ArtifactURL("hooks", h.HookDigest, "script.sh")
		fmt.Fprintf(&b, "curl -fsS -o %s '%s'\n", dest, artifactURL)
		fmt.Fprintf(&b, "echo '%s  %s' | sha256sum -c -\n", h.HookDigest, dest)
		fmt.Fprintf(&b, "chmod 700 %s\n", dest)
		fmt.Fprintf(&b, "%s > /tmp/fb-%s.log 2>&1\n", dest, h.HookDigest)
		fmt.Fprintf(&b, "signal \"hook-report\" \"$?\" /tmp/fb-%s.log \"%s\"\n\n", h.HookDigest, h.HookDigest)
	}

	b.WriteString(`signal "init-done" 0` + "\n")
	b.WriteString(`signal "ready" 0` + "\n\n")

	b.WriteString(esxiCredentialCleanup())
	b.WriteString("%end\n")
	return b.String(), nil
}

// esxiCredentialCleanup 渲染 ESXi 侧的凭据清理（SPEC §9.5 的精神，机制
// 按 ESXi 的目标系统布局调整）：finalize token 与参数文件在 %firstboot
// 段落写入的临时位置，用完即擦除。ESXi 没有 anaconda-ks.cfg 那样的安装
// 记录副本需要清理——它自己的 kickstart 脚本不会被安装器复制进目标系统的
// 固定位置留档，因此本函数只清理本文件自己写入的临时文件。
func esxiCredentialCleanup() string {
	return "rm -f /tmp/lober-params.env /tmp/lober-finalize.token\n" +
		"rm -rf /tmp/lober-hooks\n"
}

// esxiInstallCommand 渲染 ESXi kickstart 的安装动作指令。MVP 只覆盖全新
// 安装（install），不覆盖升级（upgrade）——磁盘用 by-id 标识，来自服务端
// 决策（ctx.Facts[LOBER_DISK_PRIMARY]），与 kickstart.go 的 baseDirectives
// 同样的理由（不硬编码 /dev/sda 之类内核分配名）。
//
// --overwritevmfs 允许覆盖目标盘上已存在的 VMFS 分区——这是"重装场景"下
// 必需的，否则安装器发现盘上已有 VMFS 会拒绝继续；--novmfsondisk 未使用，
// 因为我们确实要在选中的盘上建 VMFS 数据分区（ESXi 的默认行为）。
// rootpw 用 --lock 锁定 root 密码登录，只保留 SSH 已经在 %firstboot 里
// 处理不到——ESXi 的 kickstart 语法本身不提供 sshkey 指令（不同于 RHEL），
// 这是一个契约缺口，见文件末尾说明。
//
// network 指令刻意不带 --device=：ESXi kickstart 的 network 命令支持
// --device=<vmnic 名或 MAC>，但硬编码 vmnic0 会在多网卡服务器上选错
// 网卡——PXE 引导用的网卡不保证是 vmnic0（与 kickstart.go 的
// baseDirectives 同样的理由：那里的 `network --bootproto=dhcp --activate`
// 也不指定设备名，让安装器沿用引导时已经在用的那块网卡）。省略
// --device= 时 ESXi 安装器沿用安装介质引导时已经配置好的那块网卡，这正是
// 我们需要的行为——机器正是通过它 PXE 引导到这里的。model.RenderContext
// 目前没有携带"引导网卡的 vmnic 名称"这个事实（FactNICs 是全部网卡的列表，
// 不单独标出哪个是引导网卡），若将来需要显式指定，需要 M1 补一个类似
// FactDiskPrimary 的 fact 键。
func esxiInstallCommand(ctx model.RenderContext) string {
	diskByID := ctx.Facts[model.FactDiskPrimary]
	var b strings.Builder
	fmt.Fprintf(&b, "install --firstdisk=%s --overwritevmfs\n", diskByID)
	b.WriteString("rootpw --lock\n")
	b.WriteString("network --bootproto=dhcp\n")
	b.WriteString("reboot\n")
	return b.String()
}

// esxiSyslogDirective 渲染一条关于 syslog 配置方式的注释行，说明真正的
// syslog 配置不在 kickstart 指令里完成（计划任务 3 第 3 条差异：ESXi 用
// `esxcli system syslog config set --loghost=` 而非 kickstart 里的一行
// 指令，SPEC §6.5 末段）。这行注释本身不是可执行代码，只是让读到这份
// kickstart 正文的人明白 syslog 转发是通过下方 %firstboot 段落里的
// esxcli 调用完成的，不要去 kickstart 顶部找一行不存在的 logging 指令。
func esxiSyslogDirective(ctx model.RenderContext) string {
	return fmt.Sprintf("# syslog 转发通过 esxcli 配置（见 %%firstboot 段落），"+
		"ESXi kickstart 语法没有 RHEL 那一行 logging 指令\n"+
		"# esxcli system syslog config set --loghost=%s:%d\n", ctx.SyslogHost, ctx.SyslogPort)
}

// AssembleESXiKickstart 组装完整的 ESXi 方言 kickstart 应答文件正文（计划
// 任务 3）。
//
// 与 kickstart.go 的 AssembleKickstart 结构类似但内容完全不同——两者都叫
// "kickstart"只是巧合的命名重合（附录里点名警示的那种），实际语法、阶段、
// 指令集完全不同，本函数不复用也不调用 AssembleKickstart 的任何逻辑。
//
// 组装顺序：安装动作指令 → syslog 说明行 → %packages（若模板给出）→
// %pre 段落 → %post 段落 → %firstboot 段落。ESXi 没有 post-nochroot 段落，
// 三个安装期阶段实际只有 pre 与 post 两个（第三个 firstboot 是重启后的
// 原生段落）。
//
// 参数与 fact 注入不在本函数里做一次性的"序言"（不同于 kickstart.go 的
// AssembleKickstart 那种单独 prologue 段落的做法）——ESXi 的 %pre/%post/
// %firstboot 三个段落各自是独立执行的脚本文件，互不共享变量（与 RHEL
// 侧 anaconda 的段落隔离模型同样的道理），因此注入点分散在
// RenderESXiStageSection 与 esxiFirstbootSection 内部，各自重新注入一份，
// 与它们各自重新注入 ClientLibESXi 是同一处理原则。本函数只负责按顺序
// 拼接这些已经各自完整的段落。
func AssembleESXiKickstart(ctx model.RenderContext) (string, error) {
	var b strings.Builder

	b.WriteString(esxiInstallCommand(ctx))
	b.WriteString(esxiSyslogDirective(ctx))
	if ctx.Template.AnswerTemplate != "" {
		b.WriteString(ctx.Template.AnswerTemplate)
		b.WriteString("\n")
	}

	for _, stage := range []model.HookStage{model.HookStagePre, model.HookStagePost} {
		section, err := RenderESXiStageSection(ctx, stage)
		if err != nil {
			return "", err
		}
		b.WriteString(section)
	}

	firstboot, err := esxiFirstbootSection(ctx)
	if err != nil {
		return "", err
	}
	b.WriteString(firstboot)

	return b.String(), nil
}

// ---------------------------------------------------------------------------
// ash 版 ClientLib：signal 与 run_hook，供 ESXi 的 ash shell 执行
// ---------------------------------------------------------------------------

// ClientLibESXi 渲染 ash（busybox ash，ESXi 的内置 shell）兼容的 signal 与
// run_hook 两个函数定义。行为语义与 clientlib.go 的 ClientLib 完全一致
// （七条 signal 行为要求、六条 run_hook 行为要求），但实现手法必须换成
// ash 认识的语法：
//
//   - 不使用 PIPESTATUS：ash 没有这个 bash 数组扩展。RHEL 侧用
//     `cmd 2>&1 | tee log` 再取 PIPESTATUS[0]，是为了"同时看到输出又拿到
//     真实退出码"；ash 版改为 `cmd > log 2>&1; rc=$?`——直接重定向到文件
//     而不经过管道，天然就能用 `$?` 拿到 cmd 自己的退出码，不需要
//     PIPESTATUS 这个概念本来要解决的问题（tee 会覆盖 $?）在这个写法下
//     不存在，因为管道整体消失了。
//   - 不依赖 `local`：ash 实际支持 local（已用真实 ash 二进制验证），但
//     变量名前缀改为 LOBER_ 系列以避免与 %firstboot 段落里注入的
//     LOBER_* fact 变量以及 InlineParams 产出的表单变量意外重名——两个
//     函数体内部用到的临时变量全部加短前缀 `_lc`（local client 缩写）
//     区分，降低与外部注入变量撞名的概率。
//   - logger 命令：ESXi 的 busybox 环境不保证有独立的 syslog 客户端
//     二进制叫 logger，改用 esxcli 的系统日志写入方式作为兜底
//     （`esxcli system syslog mark` 不适用于任意消息写入，这里改用直接
//     append 到本地 vmkernel 可读的位置：`/var/log/lober-signal-fail.log`，
//     该文件本身不是 ESXi 的标准日志文件，但在信号三次重试全部失败这种
//     边缘情况下，把失败记录留在本地磁盘供人工登录后查看，比完全无声
//     更有诊断价值——这与 RHEL 侧 `logger` 兜底的设计意图一致：遥测失败
//     不能杀装机，但也不能让失败悄无声息）。
//
// 不出现字面 http://（与 ClientLib 同样的约束，SPEC §1.5）：两个 URL 变量
// 完全来自 ctx.Bases。
func ClientLibESXi(ctx model.RenderContext) string {
	gw := ctx.Bases.GatewayURL()
	art := ctx.Bases.ArtifactURL()

	return fmt.Sprintf(`GW='%s'
ART='%s'
TASK='%s'
TOKEN='%s'
SEQ=0

signal() {
  _lc_phase="$1"
  _lc_rc="${2:-0}"
  _lc_log="${3:-/dev/null}"
  _lc_digest="${4:-}"
  SEQ=$((SEQ+1))
  _lc_q="$GW/v1/signal/$TASK/$TOKEN/$_lc_phase?rc=$_lc_rc&seq=$SEQ"
  if [ -n "$_lc_digest" ]; then
    _lc_q="$_lc_q&digest=$_lc_digest"
  fi
  _lc_i=1
  while [ "$_lc_i" -le 3 ]; do
    curl -fsS -m 10 -X POST "$_lc_q" \
      -H 'Content-Type: text/plain' --data-binary "@$_lc_log" && return 0
    sleep $((_lc_i*3))
    _lc_i=$((_lc_i+1))
  done
  echo "$(date) signal failed: $_lc_phase rc=$_lc_rc" >> /var/log/lober-signal-fail.log
  return 0
}

run_hook() {
  _lc_digest="$1"
  _lc_required="$2"
  _lc_timeout="$3"
  curl -fsS -o /tmp/h.sh "$ART/hooks/$_lc_digest/script.sh" || return 1
  echo "$_lc_digest  /tmp/h.sh" | sha256sum -c - || return 1
  timeout "$_lc_timeout" /bin/sh /tmp/h.sh > /tmp/h.log 2>&1
  _lc_rc=$?
  signal "hook-report" "$_lc_rc" /tmp/h.log "$_lc_digest"
  [ "$_lc_rc" -eq 0 ] || [ "$_lc_required" != "true" ]
}
`, gw, art, ctx.Task.ID, ctx.InstallToken)
}

// 契约缺口 1：ESXi 应答文件命名与冻结契约的一对一约束冲突。
//
// model.AnswerFileName(esxi) 固定返回 "boot.cfg"（internal/core/model/
// render.go），而 docs/contracts/http-machine.md 端点 3 明确要求
// `{file}` 必须与 RenderContext.AnswerFileName() 逐字符相等，不接受第二个
// 候选名单——这个约束是本轮实现中唯一没有先例的地方：Ubuntu 确实有先例
// （renderer.go 的 validateAnswerFileName 额外接受 "meta-data"），但那是
// 已经写进 renderer.go 的现成代码，本模块的文件归属不包含 renderer.go。
//
// 而 SPEC §0.3 的表格明确写着 ESXi 的"应答文件机制"是"kickstart"，
// §2.1 的 ESXi 特例说明也明确 boot.cfg 与 kickstart 是两个不同文件——
// boot.cfg 只是 mboot.efi 的引导参数文件，其 kernelopt 行的 ks= 指向
// 另一份真正的 kickstart 脚本。真实 ESXi 部署里这两个文件从来不是同一个。
//
// 本文件的保守处理：产出两个独立的导出函数（RenderESXiBootCfg 与
// AssembleESXiKickstart），不擅自把两者的产出硬塞进同一个 boot.cfg 响应
// 里（那会产出一个 mboot.efi 无法解析的畸形文件），也不擅自修改
// renderer.go 的分派逻辑或 model.AnswerFileName 的返回值（前者不在本
// agent 的文件归属内，后者是冻结契约）。
//
// 正解方向（供编排方接线时参考，与 Ubuntu 的 meta-data 先例完全同构）：
//  1. renderer.go 的 validateAnswerFileName 对 OSFamilyESXi 额外接受一个
//     第二文件名（建议 "ks.cfg"，与 SPEC 附录里各家族的常见扩展名习惯
//     一致），校验通过后 RenderAnswerFile 的 switch 分支按 fileName 是
//     "boot.cfg" 还是 "ks.cfg" 分别调用 RenderESXiBootCfg 或
//     AssembleESXiKickstart（RenderESXiBootCfg 还需要 bootCfgURLBase 与
//     answerFileURL 两个参数，需要从 ctx.Bases 与 ctx.Task 在 renderer.go
//     里现算，与 menu.go 里 renderRockyMenu 现算 ksURL 的方式一致）。
//  2. docs/contracts/http-machine.md 的"约定 4"需要补一行说明 ESXi 的
//     第二候选文件名，与 Ubuntu 的 meta-data 那条并列。
//  3. gateway/answer.go 的 answerFileContentType 对 "ks.cfg" 与 "boot.cfg"
//     都应返回 text/plain（当前实现已经是 default 分支返回 text/plain，
//     两个新文件名都会落进这个分支，不需要改动）。
//
// 契约缺口 2：ESXi kickstart 没有 sshkey 指令。
//
// kickstart.go 的 baseDirectives 用 `sshkey --username=root "<公钥>"` 给
// RHEL 注入 SSH 公钥登录（对应另一个已记录的契约缺口：SSH 公钥本身没有
// 契约字段来源，见 kickstart.go 顶部注释）。ESXi 的官方 kickstart 语法
// 不提供等价指令——ESXi 的 SSH 访问默认通过 Dropbear，公钥通常在
// %firstboot 阶段用 `esxcli` 或直接写入
// /etc/ssh/keys-root/authorized_keys 完成。本文件的 esxiInstallCommand
// 因此不产出任何 sshkey 类指令；若将来需要注入公钥，应在
// esxiFirstbootSection 里追加一段写 authorized_keys 的逻辑，同样受
// kickstart.go 已记录的那个契约缺口（缺 SSH 公钥来源字段）约束，不是
// 本文件能独立解决的。
//
// 契约缺口 3：post-nochroot 阶段的模板保存校验（M6 职责）。
//
// 计划任务 3 明确要求"配了 post-nochroot 阶段 hook 的 ESXi 模板保存被
// 拒绝"，这是一条注册期/保存期校验，而不是渲染期校验——渲染器拿到
// RenderContext 时，模板已经保存成功，若那时才发现有一个 ESXi 家族的
// post-nochroot hook，为时已晚（装机流程已经走到渲染这一步）。
//
// internal/core/model/authoring.go 的 Template 与 Hook 类型（见
// SupportsFamily、HooksForStage）目前没有任何"某家族是否支持某阶段"的
// 校验方法——HookStage.Valid() 只校验阶段名是否在四个合法值之内，不校验
// "阶段 × 家族"的组合是否有意义。本文件不能新增这类校验方法（model 是
// 冻结契约，不在本次分配范围内），只能在此明确指出：M6（控制台）保存
// 模板时，若模板的 artifact_set 指向 esxi 家族，且 hook_refs 中存在
// stage=post-nochroot 的条目，必须拒绝保存并给出具体原因。
//
// 本文件对这个缺口的纵深防御：RenderESXiStageSection 对 post-nochroot
// 阶段直接返回空字符串（esxiHookStageDirectives 表中不含该键），因此
// 即使 M6 的校验缺失、一个非法模板被保存下来，最终渲染出的 kickstart
// 正文里也不会出现任何 post-nochroot 相关内容——该 hook 会被静默丢弃而
// 不是渲染出一个 ESXi 根本不认识的 `%post --nochroot` 段落头（那会让
// 整个 kickstart 文件解析失败，波及范围从"一个 hook 不生效"扩大到"这台
// 机器完全装不上"）。但"静默丢弃"本身也不是理想状态——运维会觉得这个
// hook"配了但没生效"却没有任何报错，这正是为什么本文件反复强调 M6 的
// 保存期校验是真正的解决方案，这里的处理只是不让缺陷升级为更大的故障。
