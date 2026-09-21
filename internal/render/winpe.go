// 本文件实现 Windows 装机通道（对应实现计划 P3-01：任务 2 引导菜单、任务 4
// WinPE 内遥测通道、任务 5 firstboot 的 Windows 对应机制），依据 SPEC
// §4.5、§4.6、§5.2 末段、§6.3、§6.5、§9.5，附录 A.4-A.6。
//
// Windows 与 Linux 是两套完全不同的机制，不套用 kickstart.go /
// firstboot.go 的思路（计划边界提醒）：
//   - wimboot 通过 HTTP 引导的是 WinPE，不是完整 Windows（附录 A.6）；
//     真正的安装动作由 WinPE 内的 DISM/setup.exe 执行；
//   - 应答文件 unattend.xml 由 WinPE 内的 startnet.cmd 去取，不烧进 WIM
//     （见 unattend.go）；
//   - WinPE 里没有 bash、默认没有 curl，遥测改用 PowerShell（本文件任务 4）；
//   - firstboot 对应 SetupComplete.cmd（系统身份，首次登录前执行），不是
//     systemd oneshot（本文件任务 5）。
//
// menu.go 归属另一轨（P2-multios），本文件不修改它，也不 import 它——
// RenderWindowsMenu 是本文件独立导出的函数，供编排方合并后在 menu.go 的
// RenderMenu 分派里补一行 `case model.OSFamilyWindows: return
// renderWindowsMenuInternal(ctx)` 之类的调用（见文件末尾契约缺口说明）。
package render

import (
	"fmt"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// 任务 2：引导菜单与固件选键
// ---------------------------------------------------------------------------

// windowsExtraKeys 是 ArtifactPaths.Extra 中 Windows 制品集必须携带的四个
// 键（与 imports/extract_windows.go 的 windowsArtifactFiles 使用同一组键
// 名，两处是同一份契约的两端：导入落盘时写这些键，渲染菜单时读这些键）。
var windowsExtraKeys = []string{"wimboot", "BCD", "boot.sdi", "boot.wim"}

// RenderWindowsMenu 渲染 Windows 家族的 wimboot 引导菜单（计划任务 2，
// SPEC §4.5）：wimboot 作为「内核」的一行，加三个各带标签的 initrd 行
// （BCD、boot.sdi、boot.wim）。
//
// 固件选键是本函数的核心校验（§4.6 第二项）：ctx.ActualFirmware 是机器
// 引导时实际上报的固件模式，ctx.Artifact.Firmware 是这个任务 pin 住的
// 制品集声明的固件。建任务时已经按机器记录的固件选好了制品集（M6 的建
// 任务校验），这里是引导时的第二道核验——记录值可能与眼前机器不符
// （§4.6："建任务时机器还没上电...库里存的是 discovery 阶段的记录值，
// 而运维可能选错机器"）。不符时硬拒绝，绝不在这里切换到另一个制品集：
// 任务 pin 住的内容必须与实际下发的一致，否则破坏 §2.6 的可复现性。
func RenderWindowsMenu(ctx model.RenderContext) (string, error) {
	if ctx.Artifact.Firmware != ctx.ActualFirmware {
		return "", fmt.Errorf(
			"render: 机器实际上报的固件模式 %q 与任务 pin 住的 Windows 制品集声明固件 %q 不符"+
				"——按 SPEC §4.6 硬拒绝下发，不尝试切换到另一个制品集（那会破坏任务的可复现性）",
			ctx.ActualFirmware, ctx.Artifact.Firmware)
	}

	paths := make(map[string]string, len(windowsExtraKeys))
	for _, key := range windowsExtraKeys {
		rel, ok := ctx.Artifact.Paths.Extra[key]
		if !ok || rel == "" {
			return "", fmt.Errorf(
				"render: Windows 制品集缺少 ArtifactPaths.Extra[%q]，无法渲染 wimboot 菜单"+
					"（SPEC §4.5 要求 wimboot/BCD/boot.sdi/boot.wim 四项齐全）", key)
		}
		paths[key] = rel
	}

	wimbootURL := ctx.Bases.ArtifactURL(paths["wimboot"])
	bcdURL := ctx.Bases.ArtifactURL(paths["BCD"])
	bootSDIURL := ctx.Bases.ArtifactURL(paths["boot.sdi"])
	bootWIMURL := ctx.Bases.ArtifactURL(paths["boot.wim"])

	var b strings.Builder
	fmt.Fprintf(&b, "kernel %s\n", wimbootURL)
	fmt.Fprintf(&b, "initrd %s       BCD\n", bcdURL)
	fmt.Fprintf(&b, "initrd %s  boot.sdi\n", bootSDIURL)
	fmt.Fprintf(&b, "initrd %s  boot.wim\n", bootWIMURL)
	return b.String(), nil
}

// ---------------------------------------------------------------------------
// 任务 4：WinPE 内的遥测通道
// ---------------------------------------------------------------------------

// winPESignalPoints 是 WinPE 装机流程各阶段对应的信号名，按计划任务 4 给出
// 的映射表排列（WinPE 起来 → 分区 → 应用镜像 → 配置引导 → 重启 → 首次
// 启动配置 → 完成）：
//
//	pre-start / pre-done         → startnet.cmd 的开始与分区前
//	packages-start / packages-done → 应用镜像的前后（对应 Linux 的装包）
//	post-start / post-done       → 配置引导与写入首次启动脚本的前后
//	rebooting                    → 重启前
//
// booted 及之后的信号在首次登录前执行的脚本里发出（任务 5，见
// WindowsFirstbootScaffold），不在本函数覆盖范围内——两者是不同执行环境
// （WinPE vs 已安装系统首次启动），信号点因此分属两个不同的生成函数。
var winPESignalPoints = []model.SignalKind{
	model.SignalPreStart, model.SignalPreDone,
	model.SignalPackagesStart, model.SignalPackagesDone,
	model.SignalPostStart, model.SignalPostDone,
	model.SignalRebooting,
}

// WinPESignalPoints 返回 WinPE 装机流程覆盖的信号名列表（供测试与其它
// 调用方核对信号点是否齐全，不返回内部共享切片本身）。
func WinPESignalPoints() []model.SignalKind {
	out := make([]model.SignalKind, len(winPESignalPoints))
	copy(out, winPESignalPoints)
	return out
}

// WinPEClientLib 渲染 WinPE 内用于上报信号的 PowerShell 函数
// Send-LoberSignal（计划任务 4，SPEC §6.3）。
//
// 选 PowerShell 而不是 bitsadmin 或塞一个 curl 二进制进 WinPE 的理由见
// 计划任务 4 的取舍表：PowerShell 能正常发 POST 与纯文本 body，且 WinPE
// 加 PowerShell 组件是标准做法——但这是 WinPE 构建时的前置条件，不含
// PowerShell 组件的 WinPE 会让整个遥测静默失效（部署期核对项，非本函数
// 能保证）。
//
// 与 bash 侧 signal()（clientlib.go）保持同样的四条行为：
//  1. 重试三次，每次之间退避（第 i 次等 3i 秒，与 bash 侧 `sleep
//     $((i*3))` 对应，PowerShell 侧写作 `Start-Sleep -Seconds ($i*3)`）
//  2. digest 非空时才拼进 query
//  3. 用 POST 发送日志文件内容作为纯文本 body
//  4. 三次都失败时落本地日志文件兜底（WinPE 没有 syslog 客户端，§6.5，
//     不能像 bash 侧那样用 `logger`），且函数最终仍要让调用方继续——
//     即不 throw、不 exit，遥测失败绝不能杀装机
//
// 网络调用包在 try/catch 里，是 PowerShell 侧实现"遥测失败不中断脚本"
// 的手段（Invoke-WebRequest 在网络不可达时会抛异常，未捕获的异常会终止
// 整个脚本执行，等价于 bash 里一条命令失败但没有 `|| true` 时的中止效果，
// 因此这里必须显式 catch）。
func WinPEClientLib(ctx model.RenderContext) string {
	gwURL := ctx.Bases.GatewayURL()
	artURL := ctx.Bases.ArtifactURL()

	return fmt.Sprintf(`$LoberGW = '%s'
$LoberART = '%s'
$LoberTask = '%s'
$LoberToken = '%s'
$LoberSeq = 0
$LoberLocalLog = "$env:SystemDrive\Windows\Temp\lober-signal-fallback.log"

function Send-LoberSignal {
    param(
        [string]$Phase,
        [int]$Rc = 0,
        [string]$LogPath = $null,
        [string]$Digest = $null
    )

    $LoberSeq++
    $query = "$LoberGW/v1/signal/$LoberTask/$LoberToken/$Phase?rc=$Rc&seq=$LoberSeq"
    if (-not [string]::IsNullOrEmpty($Digest)) {
        $query = "$query&digest=$Digest"
    }

    $body = ""
    if ($LogPath -and (Test-Path $LogPath)) {
        $body = Get-Content -Raw $LogPath
    }

    for ($i = 1; $i -le 3; $i++) {
        try {
            Invoke-WebRequest -Uri $query -Method Post -Body $body -ContentType 'text/plain' -TimeoutSec 10 -UseBasicParsing | Out-Null
            return 0
        } catch {
            Start-Sleep -Seconds ($i * 3)
        }
    }

    # 三次都失败：WinPE 没有 syslog 客户端（SPEC 6.5），落本地文件兜底，
    # 供后续（若能触达）一次性上传，而不是彻底丢失这条记录。
    Add-Content -Path $LoberLocalLog -Value "signal failed: $Phase rc=$Rc seq=$LoberSeq"

    # 遥测失败绝不能杀装机——与 bash 侧 clientlib.go 的 return 0 同一
    # 设计意图：服务器临时不可达而把正在装的机器搞挂是最没必要的失败。
    return 0
}
`, gwURL, artURL, ctx.Task.ID, ctx.InstallToken)
}

// WinPEInstallScript 渲染 WinPE 内驱动整个安装流程的 PowerShell 脚本骨架
// （计划任务 4，供 startnet.cmd 调用）：在 §4.5 的四个安装阶段边界各插入
// 对应的信号点，中间的实际安装动作（分区、应用镜像、配置引导）用
// DISM/Apply-WindowsImage 等 Windows 自带命令的调用占位。
//
// 这不是完整的分区/应用镜像实现（那依赖具体的磁盘决策结果与镜像索引号，
// 是控制台/provider 决策的产物，超出渲染器骨架的职责——与 kickstart.go
// 的 baseDirectives 用 ctx.Facts[model.FactDiskPrimary] 的方式一致，本
// 函数同样只从 ctx.Facts 读已经决策好的磁盘标识，不在这里做决策），而是
// 「信号点必须出现在流程的哪个位置」这个骨架——真实的 DISM 调用参数
// （镜像索引、卷号）留给未来接入具体的应答文件驱动细节时补全。
func WinPEInstallScript(ctx model.RenderContext) string {
	diskID := ctx.Facts[model.FactDiskPrimary]

	var b strings.Builder
	b.WriteString(WinPEClientLib(ctx))
	b.WriteString("\n")

	fmt.Fprintf(&b, "Send-LoberSignal -Phase '%s'\n", model.SignalPreStart)
	b.WriteString("# 分区目标磁盘（服务端决策结果，by-id/by-path 等价物见 §5.6）\n")
	fmt.Fprintf(&b, "# diskpart 目标磁盘标识: %s\n", diskID)
	fmt.Fprintf(&b, "Send-LoberSignal -Phase '%s'\n\n", model.SignalPreDone)

	fmt.Fprintf(&b, "Send-LoberSignal -Phase '%s'\n", model.SignalPackagesStart)
	b.WriteString("Apply-WindowsImage -ImagePath 'X:\\sources\\install.wim' -Index 1 -ApplyPath 'W:\\'\n")
	fmt.Fprintf(&b, "Send-LoberSignal -Phase '%s'\n\n", model.SignalPackagesDone)

	fmt.Fprintf(&b, "Send-LoberSignal -Phase '%s'\n", model.SignalPostStart)
	b.WriteString("# 配置引导（bcdboot）与写入首次启动脚本（见 WindowsFirstbootScaffold）\n")
	b.WriteString("bcdboot W:\\Windows /s S: /f UEFI\n")
	fmt.Fprintf(&b, "Send-LoberSignal -Phase '%s'\n\n", model.SignalPostDone)

	fmt.Fprintf(&b, "Send-LoberSignal -Phase '%s'\n", model.SignalRebooting)
	b.WriteString("wpeutil reboot\n")

	return b.String()
}

// ---------------------------------------------------------------------------
// 任务 5：firstboot 的 Windows 对应机制
// ---------------------------------------------------------------------------

// windowsFinalizeRoot 是目标系统内落地凭据与参数文件的固定目录（与 Linux
// 侧 /var/lib/lober 对应，但路径形态是 Windows 惯例）。
const windowsFinalizeRoot = `C:\ProgramData\Lober`

// windowsSetupScriptsDir 是 SetupComplete.cmd 必须存在的固定路径
// （§5.2 末段："Windows 的 firstboot 对应
// %WINDIR%\Setup\Scripts\SetupComplete.cmd（以 SYSTEM 身份在首次登录前
// 执行）"）。
const windowsSetupScriptsDir = `%WINDIR%\Setup\Scripts`

// windowsAnswerFileCopyPath 是 Windows 安装过程把应答文件复制到目标系统的
// 固定位置（真实 Windows 安装的固定行为：Setup 会把处理过的 unattend.xml
// 留一份在 Panther 目录，供故障排查用）。这份副本含明文密码，必须在凭据
// 清理时删除（计划任务 5、SPEC §9.5 的 Windows 对应）。
const windowsAnswerFileCopyPath = `%WINDIR%\Panther\unattend.xml`

// windowsFirstbootHookFileName 按顺序返回 firstboot hook 落地后的文件名，
// 与 firstboot.go 的 hookScriptFileName 同样的编号规则（`<三位序号>-
// <digest>.ps1`），只是脚本后缀换成 .ps1——WinPE 与已安装的目标系统都有
// PowerShell，用它执行用户 hook 比批处理更清晰。
func windowsFirstbootHookFileName(ref model.HookRef, position int) string {
	return fmt.Sprintf("%03d-%s.ps1", (position+1)*10, ref.HookDigest)
}

// WindowsFirstbootScaffold 渲染 Windows 侧 firstboot 脚手架与凭据清理逻辑
// （计划任务 5，依据 SPEC §5.2 末段、§5.8、§9.5）。
//
// 选"首次登录前以系统身份执行的脚本"（SetupComplete.cmd）而非 oobeSystem
// pass 的 FirstLogonCommands，理由与计划任务 5 一致：
//  1. SYSTEM 身份执行，权限等同 Linux 的 root——FirstLogonCommands 依赖
//     有人登录，权限也弱；
//  2. 在首次登录前跑，与 Linux firstboot（systemd oneshot 在启动时跑）
//     时机一致；
//  3. 无人值守装机不该依赖登录动作。
//
// 四样落进目标系统（对应 firstboot.go 的五样，Windows 侧少一个"独立的
// systemd unit"，因为 SetupComplete.cmd 本身就是那个执行入口）：
//  1. finalize token 文件，权限用 icacls 限制到仅 SYSTEM 可读
//  2. 参数文件（Facts 注入，与 Linux 侧同样的内容）
//  3. 用户 firstboot hook 脚本，按 digest 校验后落地
//  4. SetupComplete.cmd 本身（执行入口）
//
// 契约缺口（与 firstboot.go 的 FirstbootScaffold 同一个缺口）：
// model.HookRef 不携带 hook 正文文本，只有 digest。本实现同样在"重启前
// 网络仍保证可达"的窗口——WinPE 阶段（由 WinPEInstallScript 在应用镜像
// 之后调用本函数的产出，写入目标系统盘）——下载并用 sha256 校验后落地，
// 而不是在 SetupComplete.cmd 真正执行时（那时已是重启后，装机 VLAN 未必
// 可达）才去下载。这与 Linux 侧的处理原则完全一致："必须在重启前就落进
// 目标系统"这条硬要求的字面意思与安全意图都保留了，只是落地手段是"校验
// 下载"而非"直接内嵌明文"。
func WindowsFirstbootScaffold(ctx model.RenderContext) string {
	hooks := firstbootHookRefsSorted(ctx.Hooks)

	var b strings.Builder

	fmt.Fprintf(&b, "New-Item -ItemType Directory -Force -Path '%s' | Out-Null\n", windowsFinalizeRoot)
	fmt.Fprintf(&b, "New-Item -ItemType Directory -Force -Path '%s\\hooks' | Out-Null\n\n", windowsFinalizeRoot)

	// finalize token 与参数文件：写入后用 icacls 限制到仅 SYSTEM 可读，
	// 对应 Linux 侧 install -m 600（Windows 没有 Unix 权限位，icacls 是
	// 等价的最小权限收紧手段）。
	tokenPath := windowsFinalizeRoot + `\finalize.token`
	fmt.Fprintf(&b, "Set-Content -Path '%s' -Value '%s' -NoNewline\n", tokenPath, ctx.FinalizeToken)
	fmt.Fprintf(&b, "icacls '%s' /inheritance:r /grant:r 'SYSTEM:(R)' | Out-Null\n\n", tokenPath)

	paramsPath := windowsFinalizeRoot + `\params.env`
	fmt.Fprintf(&b, "Set-Content -Path '%s' -Value @'\n", paramsPath)
	b.WriteString(InjectFacts(ctx.Facts))
	b.WriteString("'@\n")
	fmt.Fprintf(&b, "icacls '%s' /inheritance:r /grant:r 'SYSTEM:(R)' | Out-Null\n\n", paramsPath)

	// 用户 firstboot hook：按 digest 下载并校验后落地，同时生成 manifest。
	manifestPath := windowsFinalizeRoot + `\hooks\manifest`
	fmt.Fprintf(&b, "Set-Content -Path '%s' -Value @'\n", manifestPath)
	for i, h := range hooks {
		fileName := windowsFirstbootHookFileName(h, i)
		fmt.Fprintf(&b, "%s digest=%s required=%t order=%d\n", fileName, h.HookDigest, h.Required, h.Order)
	}
	b.WriteString("'@\n\n")

	for i, h := range hooks {
		fileName := windowsFirstbootHookFileName(h, i)
		dest := windowsFinalizeRoot + `\hooks\` + fileName
		artifactURL := ctx.Bases.ArtifactURL("hooks", h.HookDigest, "script.ps1")
		fmt.Fprintf(&b, "Invoke-WebRequest -Uri '%s' -OutFile '%s' -UseBasicParsing\n", artifactURL, dest)
		fmt.Fprintf(&b, "if ((Get-FileHash -Algorithm SHA256 '%s').Hash -ne '%s') { throw 'hook sha256 mismatch: %s' }\n\n",
			dest, strings.ToUpper(h.HookDigest), h.HookDigest)
	}

	// 执行入口：SetupComplete.cmd 本身，加两层一次性保证（系统机制：只在
	// 首次登录前跑一次；标记文件：脚本开头检查、成功后删除，中途死掉时
	// 标记仍在，可看出未跑完——与 Linux 侧 ConditionPathExists 的设计
	// 意图完全一致）。
	b.WriteString(windowsInitScript(ctx, hooks))

	return b.String()
}

// windowsInitScript 渲染写入 SetupComplete.cmd 的内容：先检查标记文件
// （第二层一次性保证），再走信号顺序 booted → init-start →（顺序跑用户
// firstboot hook）→ init-done → ready → 清理凭据 → 删标记文件。
//
// SetupComplete.cmd 本身是批处理，但直接调用 PowerShell 执行主体逻辑——
// 批处理只负责"这是那个系统认识的入口文件名"这一层，实际逻辑复用
// WinPEClientLib 提供的 Send-LoberSignal 函数（该函数不依赖 WinPE 特有的
// 任何东西，已安装的目标系统同样有 PowerShell）。
func windowsInitScript(ctx model.RenderContext, hooks []model.HookRef) string {
	markerPath := windowsFinalizeRoot + `\init.pending`

	var ps strings.Builder
	ps.WriteString(WinPEClientLib(ctx))
	b := &ps

	fmt.Fprintf(b, "if (-not (Test-Path '%s')) { exit 0 }\n\n", markerPath)

	fmt.Fprintf(b, "Send-LoberSignal -Phase '%s'\n", model.SignalBooted)
	fmt.Fprintf(b, "Send-LoberSignal -Phase '%s'\n\n", model.SignalInitStart)

	for i, h := range hooks {
		fileName := windowsFirstbootHookFileName(h, i)
		path := windowsFinalizeRoot + `\hooks\` + fileName
		logPath := fmt.Sprintf(`C:\Windows\Temp\lober-hook-%s.log`, h.HookDigest)
		fmt.Fprintf(b, "& '%s' *> '%s'; $rc = $LASTEXITCODE\n", path, logPath)
		fmt.Fprintf(b, "Send-LoberSignal -Phase '%s' -Rc $rc -LogPath '%s' -Digest '%s'\n\n",
			model.SignalHookReport, logPath, h.HookDigest)
	}

	fmt.Fprintf(b, "Send-LoberSignal -Phase '%s'\n", model.SignalInitDone)
	fmt.Fprintf(b, "Send-LoberSignal -Phase '%s'\n\n", model.SignalReady)

	b.WriteString(windowsCredentialCleanup())
	fmt.Fprintf(b, "Remove-Item -Force '%s'\n", markerPath)

	psScriptPath := windowsFinalizeRoot + `\lober-init.ps1`

	var out strings.Builder
	fmt.Fprintf(&out, "Set-Content -Path '%s' -Value @'\n", psScriptPath)
	out.WriteString(ps.String())
	out.WriteString("'@\n\n")

	out.WriteString("@'\n")
	out.WriteString("@echo off\n")
	fmt.Fprintf(&out, "powershell.exe -NoProfile -ExecutionPolicy Bypass -File \"%s\"\n", psScriptPath)
	fmt.Fprintf(&out, "'@ | Set-Content -Path \"$env:WINDIR\\%s\\SetupComplete.cmd\"\n",
		strings.TrimPrefix(windowsSetupScriptsDir, `%WINDIR%\`))

	return out.String()
}

// windowsCredentialCleanup 渲染凭据清理动作（SPEC §9.5 的 Windows 对应）：
//  1. 删除参数文件与 finalize token（Windows 没有 shred 等价物，用
//     Remove-Item——§9.5 的清理对象是"防止明文长期留存"，MVP 不在这里
//     实现覆写擦除，与 Linux 侧 shred -u 相比是已知的较弱处理，原因是
//     标准库/系统自带工具没有直接等价物，记入 contractGaps）
//  2. 删除应答文件在目标系统的固定副本 %WINDIR%\Panther\unattend.xml——
//     这份副本含明文密码，是 Windows 路线特有的清理项（Linux 侧的对应
//     是清理 anaconda-ks.cfg 里的密码哈希，两者都是"安装过程遗留的
//     应答文件残留"，但 Windows 是完整文件副本，必须整个删除而不是像
//     sed 那样删特定行）
func windowsCredentialCleanup() string {
	// windowsAnswerFileCopyPath 形如 `%WINDIR%\Panther\unattend.xml`——
	// PowerShell 用 `$env:WINDIR` 而不是批处理的 `%WINDIR%` 展开环境变量，
	// 因此这里把字面前缀替换成 PowerShell 语法，其余部分（子路径与文件名）
	// 原样复用，不在两处各写一份路径字符串。
	answerFileCopy := "$env:WINDIR" + strings.TrimPrefix(windowsAnswerFileCopyPath, "%WINDIR%")

	var b strings.Builder
	fmt.Fprintf(&b, "Remove-Item -Force -ErrorAction SilentlyContinue '%s\\params.env'\n", windowsFinalizeRoot)
	fmt.Fprintf(&b, "Remove-Item -Force -ErrorAction SilentlyContinue '%s\\finalize.token'\n", windowsFinalizeRoot)
	fmt.Fprintf(&b, "Remove-Item -Force -ErrorAction SilentlyContinue \"%s\"\n", answerFileCopy)
	return b.String()
}
