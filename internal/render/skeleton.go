// 本文件抽出三个家族共用的骨架生成逻辑（对应实现计划 phase2-01 任务 1，
// 依据 SPEC §5.1、§5.2、§4.5）。
//
// 抽出的是"内容"，不是"如何嵌入应答文件"——三种应答文件语法的嵌入方式本质
// 不同（kickstart 直接写 shell，preseed 要塞进 late_command 的一行里，
// autoinstall 要放进 YAML 的列表项），强行统一嵌入方式会让三种都别扭
// （计划任务 1 边界提醒）。因此本文件只返回结构化的片段，由各家族的渲染
// 函数决定怎么放进自己的语法。
//
// 五样共用内容对应本文件与既有文件的关系：
//  1. 参数内联与 fact 注入的内容 —— 复用 params.go 的 InlineParams/InjectFacts
//  2. 客户端 shell 函数的正文 —— 复用 clientlib.go 的 ClientLib
//  3. firstboot 脚手架要写入目标系统的文件清单 —— 见 firstbootFileList
//  4. 凭据清理的动作清单 —— 复用 firstboot.go 的 credentialCleanup
//  5. hook 阶段到"该阶段要执行哪些 digest"的解析 —— 见 StageHookCallLines
package render

import (
	"fmt"

	"github.com/lober/lober/internal/core/model"
)

// StageHookCallLines 返回某个 installer 阶段（pre / post-nochroot / post）
// 对应的 run_hook 调用行列表，按 Order 升序排列，每行已包含
// `|| exit 1` 短路退出（required hook 失败即中止装机，三个家族共用同一条
// 安全不变量）。
//
// 该阶段没有任何 hook 时返回 nil——调用方（kickstart.go/preseed.go/
// autoinstall.go）各自决定"空阶段是否要产出对应的语法结构"，本函数不代为
// 决定，因为三种语法对"空阶段"的处理方式不同（kickstart 不写空 %pre，
// preseed/autoinstall 的阶段键是否可以省略取决于各自格式）。
//
// 与 kickstart.go 的 hooksForStage/runHookCall 逻辑等价（那两个函数在
// RHEL 重构前就是这个实现，抽出后 kickstart.go 改为调用本函数，保证
// RHEL 的 golden file 逐字节不变）。
func StageHookCallLines(hooks []model.HookRef, stage model.HookStage) []string {
	refs := hooksForStage(hooks, stage)
	if len(refs) == 0 {
		return nil
	}
	lines := make([]string, 0, len(refs))
	for _, ref := range refs {
		lines = append(lines, runHookCall(ref))
	}
	return lines
}

// FirstbootHookRefsSorted 返回 ctx.Hooks 中 Stage 为 firstboot 的引用，按
// Order 升序排列（对外暴露 firstboot.go 的 firstbootHookRefsSorted 逻辑，
// 供 preseed.go/autoinstall.go 复用同一份排序结果，不再各自实现一遍）。
func FirstbootHookRefsSorted(hooks []model.HookRef) []model.HookRef {
	return firstbootHookRefsSorted(hooks)
}

// firstbootFileList 是 firstboot 脚手架要写入目标系统的文件清单（骨架
// 共用内容第 3 项）。三个家族的落地路径、权限、文件名规则完全一致
// （SPEC §5.8、docs/contracts/filesystem.md），只有"落地动作写在应答文件
// 的哪个位置"不同：
//
//	RHEL      落在 %post 段（本身就是 chroot 后的 shell）
//	Ubuntu    落在 late-commands 的 curtin in-target 调用里
//	Debian    落在 late_command 的 in-target 脚本里
//
// 本类型只承载"清单"本身（路径与用途），不承载"如何嵌入"——嵌入方式由
// 各家族渲染函数决定（计划任务 1 边界提醒："不抽出的：如何把这五样嵌进
// 应答文件"）。当前 firstboot.go 的 FirstbootScaffold/initScript 已经把
// 这份清单与 RHEL 的嵌入方式耦合在一起生成完整 shell 文本；Ubuntu/Debian
// 复用的是同一套路径常量与生成规则（见 preseed.go、autoinstall.go 对
// firstbootScriptFileNames 等辅助函数的调用），不是重新发明一套目录结构。
type firstbootFileList struct {
	WorkDir       string // /var/lib/lober
	HooksDir      string // /var/lib/lober/hooks
	FinalizeToken string // /var/lib/lober/finalize.token，权限 600
	ParamsEnv     string // /var/lib/lober/params.env，权限 600
	Manifest      string // /var/lib/lober/hooks/manifest
	UnitPath      string // /etc/systemd/system/lober-init.service
	InitScript    string // /usr/local/sbin/lober-init.sh
	PendingFlag   string // /var/lib/lober/init.pending
}

// standardFirstbootFiles 是三个家族共用的固定路径集合（与 firstboot.go
// 的字面量保持一致，未来若要改路径，只需改这一处常量与 firstboot.go 的
// 对应字面量同步更新）。
var standardFirstbootFiles = firstbootFileList{
	WorkDir:       "/var/lib/lober",
	HooksDir:      "/var/lib/lober/hooks",
	FinalizeToken: "/var/lib/lober/finalize.token",
	ParamsEnv:     "/var/lib/lober/params.env",
	Manifest:      "/var/lib/lober/hooks/manifest",
	UnitPath:      "/etc/systemd/system/lober-init.service",
	InitScript:    "/usr/local/sbin/lober-init.sh",
	PendingFlag:   "/var/lib/lober/init.pending",
}

// hookScriptDestPath 返回某个 firstboot hook 落地后的目标路径（复用
// firstboot.go 的 hookScriptFileName 命名规则：`<三位序号>-<digest>.sh`）。
func hookScriptDestPath(ref model.HookRef, position int) string {
	return standardFirstbootFiles.HooksDir + "/" + hookScriptFileName(ref, position)
}

// inTargetPrefix 按家族返回"对目标系统执行命令"的前缀（SPEC §5.2"阶段是
// 能力边界"在 Ubuntu/Debian 下的具体形式）：
//
//	Ubuntu   curtin in-target --
//	Debian   in-target
//
// 不提供 RHEL 的等价项——kickstart 的 %post 本身就已经 chroot 进目标系统，
// 不需要任何前缀，这正是三种格式"能力边界"体现方式不同的例子，不该被
// 统一成一个对 RHEL 也返回空字符串的"通用前缀"函数（那会诱使调用方无脑
// 拼接，模糊了"有没有 chroot"这条本质差异）。
func inTargetPrefix(family model.OSFamily) (string, error) {
	switch family {
	case model.OSFamilyUbuntu:
		return "curtin in-target --", nil
	case model.OSFamilyDebian:
		return "in-target", nil
	default:
		return "", fmt.Errorf("render: 家族 %q 没有定义 in-target 前缀（该概念只适用于"+
			"镜像式/两阶段安装的 Ubuntu 与 Debian）", family)
	}
}
