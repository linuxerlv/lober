// 本文件实现 Ubuntu autoinstall 应答文件的组装（对应实现计划 phase2-01
// 任务 3，依据 SPEC §4.4、§4.5、§5.2）。
//
// autoinstall 是 YAML（cloud-init NoCloud 数据源的 user-data），本文件用
// yaml.v3 的结构体序列化生成，不用 text/template 手工拼接——YAML 对缩进
// 敏感，hook 正文里的多行 shell 会破坏用文本模板拼出来的缩进（计划任务 3
// 明确警示的"最容易踩的坑"）。yaml.v3 遇到含换行的字符串时默认采用字面量
// 块标量语法（`|`），能正确处理任意多行内容而不破坏外层结构——这一点在
// 设计期已用真实 yaml.Marshal 验证过。
//
// 跨家族映射（SPEC §5.2）：
//
//	pre            → early-commands（live 环境，存储/网络配置之前）
//	post-nochroot  → late-commands，不加 curtin in-target 前缀（curtin
//	                 目标盘已解包但命令仍在安装环境执行，对应 kickstart 的
//	                 %post --nochroot）
//	post           → late-commands，加 `curtin in-target --` 前缀（chroot
//	                 进目标系统执行，对应 kickstart 的 %post）
//
// 与 Debian preseed 相同的"能力边界"道理：post-nochroot 与 post 的 hook
// 共用 late-commands 这一个列表，靠"是否加 in-target 前缀"区分执行环境。
package render

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lober/lober/internal/core/model"
)

// autoinstallDoc 是 user-data 顶层结构：`autoinstall:` 加一个版本号与
// 若干大键（本地化、身份、存储、网络、早期命令、晚期命令、包）。
type autoinstallDoc struct {
	Autoinstall autoinstallBody `yaml:"autoinstall"`
}

type autoinstallBody struct {
	Version int `yaml:"version"`

	Locale   string `yaml:"locale"`
	Keyboard struct {
		Layout string `yaml:"layout"`
	} `yaml:"keyboard"`

	Identity autoinstallIdentity `yaml:"identity"`
	Storage  autoinstallStorage  `yaml:"storage"`
	Network  autoinstallNetwork  `yaml:"network"`

	EarlyCommands []string `yaml:"early-commands"`
	LateCommands  []string `yaml:"late-commands"`

	Packages []string `yaml:"packages"`

	// SSH 对应真实 autoinstall 的 ssh 大键：ssh.install-server 与
	// ssh.authorized-keys（SPEC §1.5：root 不设可复用密码，注入 SSH 公钥
	// 并关闭密码登录）。来源是 ctx.RootSSHPublicKeys，见 AssembleAutoinstall
	// 的赋值处与 model.RenderContext.RootSSHPublicKeys 的字段文档。
	SSH autoinstallSSH `yaml:"ssh"`
}

// autoinstallSSH 对应 ssh 大键：是否安装 openssh-server（不装的话没有
// sshd 可登录，公钥配置再对也没用）与要写入 root 账户
// authorized_keys 的公钥列表。
type autoinstallSSH struct {
	InstallServer  bool     `yaml:"install-server"`
	AuthorizedKeys []string `yaml:"authorized-keys"`
}

// autoinstallIdentity 对应 identity 大键：主机名与账户策略。
//
// 真实 autoinstall 的 identity 段要求 username/password 二选一有值（无
// 密码登录需要配合单独的 ssh 段），MVP 阶段与 RHEL/Debian 保持一致的策略
// ——root 不设可复用密码，只允许 SSH 公钥登录（SPEC §1.5）。autoinstall
// 的 identity.password 字段语义是"某个具体用户的密码哈希"，不存在与
// kickstart `rootpw --lock`/preseed `passwd/root-login=false` 直接等价的
// 单一键；本实现用一个已锁定（用 `!` 前缀，Linux shadow 文件的标准
// "已锁定"写法）的占位哈希值表达同等意图，账户名固定为 root。
type autoinstallIdentity struct {
	Hostname string `yaml:"hostname"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// autoinstallStorage 对应 storage 大键：用服务端决策的盘（by-id，SPEC
// §5.6），采用 curtin 存储配置的 "layout" 简化语法（真实 autoinstall
// 支持这种简写形式，不要求逐分区手写完整的 storage config action list）。
type autoinstallStorage struct {
	Layout struct {
		Name string `yaml:"name"`
	} `yaml:"layout"`
	Config []map[string]any `yaml:"config,omitempty"`
}

// autoinstallNetwork 对应 network 大键：装机 VLAN 上安装器自身使用的网卡
// 走 DHCP（不违反 SPEC §5.7——那条规则约束业务网，见 baseDirectives 同等
// 注释），采用 netplan v2 语法（真实 autoinstall 的 network 键就是一份
// netplan 配置）。
type autoinstallNetwork struct {
	Version   int                    `yaml:"version"`
	Ethernets map[string]interface{} `yaml:"ethernets"`
}

// autoinstallRootPasswordLockedPlaceholder 是 root 密码哈希的占位值
// （`!` 前缀是 Linux shadow 文件"账户已锁定，密码登录不可用"的标准写法，
// 与 kickstart 的 `rootpw --lock` 语义等价）。
const autoinstallRootPasswordLockedPlaceholder = "!"

// buildAutoinstallCommandLines 把 ClientLib 与某阶段的 hook 调用行合并成
// "一条可执行的 shell 命令文本"列表项——cloud-init 对 early-commands/
// late-commands 里每一项的执行方式等价于整段文本交给 shell 执行一次
// （原生支持多行内容，不要求单行），因此这里不需要像 Debian preseed 那样
// 做 base64/单行压缩，只需保证每一项本身是一段完整可执行的 shell 脚本。
//
// 每次调用都重新拼一份完整的 ClientLib(ctx)——early-commands 与
// late-commands 各自的每一项在 cloud-init 里都是独立起一次 shell
// 执行（不共享变量或函数定义，这与 kickstart 各段落独立子进程、preseed
// in-target chroot 边界是同一类"执行环境不共享状态"的道理），因此这里的
// "自成一体"处理与本计划在 kickstart.go/preseed.go 修复过的两个真实 bug
// 一致：若省略 ClientLib 注入，call 那一项在 cloud-init 起的 shell 里会
// 得到 "run_hook: command not found"。
func buildAutoinstallCommandLines(ctx model.RenderContext, hookLines []string) []string {
	if len(hookLines) == 0 {
		return nil
	}
	var script strings.Builder
	script.WriteString(ClientLib(ctx))
	for _, line := range hookLines {
		script.WriteString(line)
	}
	return []string{strings.TrimRight(script.String(), "\n")}
}

// buildInTargetCommandLines 与 buildAutoinstallCommandLines 类似，但每条
// hook 调用行都整体包进 `curtin in-target -- bash -c '...'`（对目标系统
// 执行，SPEC §5.2 在 Ubuntu 下的能力边界）。
//
// 与 preseed.go 的 lateCommandValue 里 post 阶段 hook 的处理方式同源：
// in-target 是 chroot 包装器，不与外层脚本共享变量/函数定义，因此每条
// 包裹进 in-target 的命令都要自带一份 ClientLib(ctx)，否则 chroot 后的新
// shell 进程看不到 run_hook/signal 的定义（同一类 bug，preseed.go 的
// TestExec_PreseedInTargetPostHookIsSelfContained 已经用真实 bash 验证过
// 修复方式，这里复用同一个思路）。
func buildInTargetCommandLines(ctx model.RenderContext, hookLines []string) []string {
	if len(hookLines) == 0 {
		return nil
	}
	inTarget, err := inTargetPrefix(model.OSFamilyUbuntu)
	if err != nil {
		// inTargetPrefix 对 Ubuntu 家族固定返回 nil error（见 skeleton.go），
		// 这里的 panic 只在"有人误改了 inTargetPrefix 的家族分支"时触发，
		// 属于编程错误而非运行期可恢复的输入错误。
		panic(fmt.Sprintf("render: inTargetPrefix(Ubuntu) 意外返回错误: %v", err))
	}

	lines := make([]string, 0, len(hookLines))
	for _, line := range hookLines {
		script := ClientLib(ctx) + line
		escaped := strings.ReplaceAll(strings.TrimRight(script, "\n"), `'`, `'\''`)
		lines = append(lines, inTarget+" bash -c '"+escaped+"'")
	}
	return lines
}

// AssembleAutoinstall 把本地化/身份/存储/网络/命令/包/SSH 七个大键组装成
// 完整的 Ubuntu autoinstall（user-data）YAML 文本（计划任务 3）。
//
// ctx.RootSSHPublicKeys 为空时返回错误：root 不设可复用密码
// （identity.password 用锁定占位值），公钥是唯一登录通路，绝不能省略
// ssh.authorized-keys 或写入占位公钥（SPEC §1.5，与 kickstart.go
// baseDirectives 同一个设计要点）。正常部署路径下这个错误不会触发：
// internal/config.Validate 已经在启动时拒绝了空的
// gateway.root_ssh_public_keys，本函数的检查是第二层防御。
func AssembleAutoinstall(ctx model.RenderContext) (string, error) {
	if len(ctx.RootSSHPublicKeys) == 0 {
		return "", fmt.Errorf("render: RenderContext.RootSSHPublicKeys 为空，" +
			"无法渲染 autoinstall——root 不设可复用密码，公钥是唯一登录通路，" +
			"绝不能省略 ssh.authorized-keys 或写入占位公钥，" +
			"那会静默产出一台没人能登录的机器（SPEC §1.5）")
	}

	params, err := InlineParams(ctx.Task.ResolvedParams)
	if err != nil {
		return "", fmt.Errorf("render: 参数内联失败: %w", err)
	}

	diskByID := ctx.Facts[model.FactDiskPrimary]
	hostname := ctx.Facts[model.FactHostname]

	doc := autoinstallDoc{
		Autoinstall: autoinstallBody{
			Version: 1,
			Locale:  "en_US.UTF-8",
			Identity: autoinstallIdentity{
				Hostname: hostname,
				Username: "root",
				Password: autoinstallRootPasswordLockedPlaceholder,
			},
			Packages: []string{},
			SSH: autoinstallSSH{
				// 不装 openssh-server 就没有 sshd 可登录，公钥配置再对也
				// 没用——两者必须同时配置才是完整的"公钥能登录"意图。
				InstallServer: true,
				// yaml.Marshal 序列化，不是文本拼接：即使公钥文本含换行、
				// 引号、冒号等 YAML 特殊字符，标准库也会正确转义或用块
				// 标量语法处理，不会让恶意值突破这个列表项的边界去篡改
				// 同级的 install-server 或其它顶层键（TestAutoinstall_
				// MaliciousSSHKeyStaysValidYAML 用真实恶意输入验证过）。
				AuthorizedKeys: ctx.RootSSHPublicKeys,
			},
		},
	}
	doc.Autoinstall.Keyboard.Layout = "us"
	doc.Autoinstall.Storage.Layout.Name = "direct"
	doc.Autoinstall.Network.Version = 2
	doc.Autoinstall.Network.Ethernets = map[string]interface{}{
		"install-nic": map[string]interface{}{"dhcp4": true},
	}

	// pre 阶段：early-commands。第一项是"参数内联 + fact 注入 + ClientLib
	// 定义 + pre-start 信号"（与 kickstart 序言段、preseed early_command
	// 同等地位），随后是 pre 阶段各 hook 的调用（若有），最后一项发
	// pre-done（计划任务 4：pre-start/pre-done → early-commands 首尾）。
	var early []string
	early = append(early, strings.TrimRight(
		inlineForSingleLine(params)+inlineForSingleLineRaw(InjectFacts(ctx.Facts))+ClientLib(ctx)+`signal "pre-start" 0`+"\n",
		"\n"))
	preLines := StageHookCallLines(ctx.Hooks, model.HookStagePre)
	early = append(early, buildAutoinstallCommandLines(ctx, preLines)...)
	early = append(early, buildAutoinstallCommandLines(ctx, []string{`signal "pre-done" 0` + "\n"})[0])
	doc.Autoinstall.EarlyCommands = early

	// post-nochroot 与 post 两个阶段共用 late-commands 列表，靠是否包
	// curtin in-target 区分能力边界（本文件头部注释、SPEC §5.2）。分区
	// 元数据里记下 diskByID，供分区配方引用（curtin 的 direct layout 会
	// 自动选盘，但 late-commands 里若有 hook 需要读取该值，仍通过
	// LOBER_DISK_PRIMARY fact 拿到，这里不需要在 storage 段落之外重复
	// 拼接）。
	nochrootLines := StageHookCallLines(ctx.Hooks, model.HookStagePostNoChroot)
	postLines := StageHookCallLines(ctx.Hooks, model.HookStagePost)

	// 信号点（phase2-01 任务 4，SPEC §6.1、计划任务 4 的三家族信号点映射
	// 表）。post-start 在 late-commands 第一项发出；packages-start/
	// packages-done 只能近似——curtin 的解包过程不提供钩子，这两个信号
	// 实际是"晚期命令开始前"与"晚期命令里第一条"（计划任务 4 原文），
	// 因此在 post-start 之后立即成对发出，不追求精确对应解包动作本身的
	// 起止时刻。这个不准不处理（计划任务 4 明确结论：预算表的作用是
	// "卡住了要能报出来"，不是精确计量，Ubuntu 整体安装时长与 RHEL 同量级，
	// 60 分钟的 packages 预算同样适用）。
	late := []string{buildAutoinstallCommandLines(ctx, []string{
		`signal "post-start" 0` + "\n" +
			`signal "packages-start" 0` + "\n" +
			`signal "packages-done" 0` + "\n",
	})[0]}
	late = append(late, buildAutoinstallCommandLines(ctx, nochrootLines)...)
	late = append(late, buildInTargetCommandLines(ctx, postLines)...)
	late = append(late, firstbootInTargetCommands(ctx)...)
	late = append(late, buildAutoinstallCommandLines(ctx, []string{
		`signal "post-done" 0` + "\n" + `signal "rebooting" 0` + "\n",
	})[0])
	doc.Autoinstall.LateCommands = late

	_ = diskByID // 已通过 fact 注入传递，storage.layout 用 direct 简写不需要在此重复引用

	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("render: autoinstall YAML 序列化失败: %w", err)
	}
	return string(out), nil
}

// inlineForSingleLineRaw 与 preseed.go 的 inlineForSingleLine 逻辑相同，
// 独立保留一份是因为 autoinstall 的语义是"塞进 YAML 块标量的多行文本"，
// 不要求压缩成分号连接的单行（cloud-init 原生支持多行命令），因此这里
// 实际上不需要压缩——本函数直接返回原文本（保留换行），命名与
// inlineForSingleLine 呼应只是为了在 diff 中清楚看出两个家族在这一点上
// 的处理方式不同：Debian 需要压缩，Ubuntu 不需要。
func inlineForSingleLineRaw(text string) string {
	return text
}

// firstbootInTargetCommands 生成 firstboot 脚手架的落地命令（与 RHEL 的
// FirstbootScaffold 生成的内容语义一致：写工作目录、凭据文件、firstboot
// hook 脚本、systemd unit，然后 enable 它），包进 curtin in-target 执行
// （对目标系统写文件，SPEC §5.8 要求"必须在 %post 阶段就落进目标系统"，
// Ubuntu 下的等价时点是 late-commands 的 in-target 阶段——curtin 已经
// 把根文件系统解包完成，chroot 进去写文件是安全的）。
//
// 本函数复用 firstboot.go 的 firstbootHookRefsSorted/hookScriptFileName/
// initScript/credentialCleanup，不重新发明一套 firstboot 落地逻辑——
// 选型是"与 RHEL 共用同一套 firstboot 脚手架"（计划任务 3 明确的选择），
// 只是承载这份脚手架的 shell 文本需要包进 in-target 而不是直接嵌进
// kickstart 的 %post 段落。
func firstbootInTargetCommands(ctx model.RenderContext) []string {
	scaffold := FirstbootScaffold(ctx)
	inTarget, err := inTargetPrefix(model.OSFamilyUbuntu)
	if err != nil {
		panic(fmt.Sprintf("render: inTargetPrefix(Ubuntu) 意外返回错误: %v", err))
	}
	escaped := strings.ReplaceAll(strings.TrimRight(scaffold, "\n"), `'`, `'\''`)
	return []string{inTarget + " bash -c '" + escaped + "'"}
}

// RenderMetaData 渲染 NoCloud 数据源要求的 meta-data 文件（SPEC §4.5：
// 该目录下 user-data 与 meta-data 两个文件都必须存在，Phase 1 的渲染器
// 接口已经为此留了"额外接受 meta-data"的口子，见 renderer.go 的
// validateAnswerFileName）。内容极简：实例 id 与主机名（计划任务 3
// 测试要求第三条）。
func RenderMetaData(ctx model.RenderContext) (string, error) {
	instanceID := ctx.Task.ID
	if instanceID == "" {
		return "", fmt.Errorf("render: 无法渲染 meta-data——RenderContext.Task.ID 为空，" +
			"instance-id 需要一个非空的任务标识")
	}
	hostname := ctx.Facts[model.FactHostname]

	doc := map[string]string{
		"instance-id":    "lober-task-" + instanceID,
		"local-hostname": hostname,
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("render: meta-data YAML 序列化失败: %w", err)
	}
	return string(out), nil
}

// 契约缺口已解决（原注释见 git 历史）：model.RenderContext.RootSSHPublicKeys
// 字段落地后，identity.password 的锁定占位值与 ssh.authorized-keys/
// ssh.install-server 分别承担"锁定密码登录"与"注入公钥登录"这两半完整
// 意图（SPEC §1.5），不再是各说一半的临时状态——见 AssembleAutoinstall
// 的赋值处与 model.RenderContext.RootSSHPublicKeys 的字段文档。
