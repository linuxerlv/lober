// 本文件实现 Debian preseed 应答文件的组装（对应实现计划 phase2-01 任务
// 2，依据 SPEC §4.4、§4.5、§5.2）。
//
// preseed 的语法是 debconf 预置项：每行「所有者 键 类型 值」四段，用 tab
// 分隔（真实 debian-installer 的 preseed.cfg 惯例）。与 kickstart 的脚本式
// 语法不同，preseed 本身不是 shell——hook 的执行动作要通过
// `preseed/late_command`（安装阶段末尾，d-i 会执行这个值指定的 shell
// 命令）间接触发，因此 pre/post-nochroot/post 三个 installer 阶段的
// hook 调用不是"直接写在段落里"，而是"生成一段安装期执行的 shell，塞进
// late_command 的值"。
//
// 跨家族映射（SPEC §5.2）：
//
//	pre            → preseed/early_command（d-i 环境，分区之前）
//	post-nochroot  → preseed/late_command，命令不加 in-target（目标盘已挂
//	                 但仍在 d-i 环境执行，对应 kickstart 的 %post --nochroot）
//	post           → preseed/late_command，命令加 in-target 前缀（chroot
//	                 进目标系统执行，对应 kickstart 的 %post）
//
// 两个阶段共用同一个 late_command 键，靠"是否加 in-target 前缀"区分能力
// 边界——这正是 SPEC §5.2"阶段不是排序，是能力边界"在 Debian 下的具体
// 体现：post-nochroot 与 post 的 hook 若放错，不是执行顺序错了，而是
// post-nochroot 的 hook 一旦被错误地加上 in-target，会在目标系统里执行
// 一个此刻还没有完整依赖（比如目标系统里可能还没有某个 nochroot 阶段本该
// 直接操作宿主工具才能做的事）的命令；反之 post 的 hook 若漏了 in-target，
// 会在 installer 环境里跑，装的包、建的用户全部落空。
package render

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// base64Encode 是标准 base64（非 URL 安全变体）编码，供 lateCommandValue
// 把可能含引号与特殊字符的脚本正文安全地塞进 debconf 单行值。
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// debconfLine 渲染一行「所有者 键 类型 值」的 debconf 预置项，字段间用
// tab 分隔（真实 preseed.cfg 惯例，debconf-set-selections 也按此解析）。
//
// value 在写入前经 sanitizeDebconfValue 处理——四段式是靠 tab 分隔、靠
// 换行分行的纯文本格式，value 里若含裸 tab 或换行，就能伪造出额外的列或
// 独立一行（真的验证过：把 ctx.Facts[LOBER_HOSTNAME] 设成"web01\nd-i\t
// passwd/root-login\tboolean\ttrue"会让 preseedNetwork 产出一条独立的
// 「passwd/root-login=true」数据行，覆盖 preseedAccountAndPackages 固定
// 写的「false」，这正是 SPEC §5.6 强调的"账户策略不可被绕过"这条不变量
// 会被打破的方式）。value 的来源里 ctx.Facts[LOBER_HOSTNAME] 直接来自
// 表单 formData（internal/provider/resolve.go 的 Resolve 方法），不经过
// InlineParams/guard 校验——ResolvedParams 与 Facts 是两个独立的字符串
// 表，注入防护只覆盖了前者（injection_test.go 端到端验证的是
// ResolvedParams），后者目前没有值级校验。在契约层补上 hostname 格式
// 校验之前，本函数在写入点做最小化清理：忠实反映一条 SPEC §5.4 事实——
// facts 是"文本 KV"，不承诺值本身免受这类结构破坏，所以清理放在消费端
// （这里）而不是假装 facts 本身已经是安全的。
func debconfLine(owner, key, valueType, value string) string {
	return fmt.Sprintf("%s\t%s\t%s\t%s\n", owner, key, valueType, sanitizeDebconfValue(value))
}

// sanitizeDebconfValue 清除 value 中能伪造出额外列或额外行的字符——裸
// tab（会被解析成第五段，破坏四段式）与裸换行（会被解析成新的一行，即
// 本文件重点修复的注入路径）。替换为空格而不是报错：与 preseedShellValue
// 对 early_command/late_command 整体值的处理方式一致（那里也是替换而非
// 拒绝），保持同一份文件内"值级清理"这件事只有一种处理哲学。
func sanitizeDebconfValue(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

// preseedLocalization 渲染本地化段落：语言、键盘、时区。
func preseedLocalization() string {
	var b strings.Builder
	b.WriteString(debconfLine("d-i", "localechooser/languagelist", "select", "en_US.UTF-8"))
	b.WriteString(debconfLine("d-i", "keyboard-configuration/xkb-keymap", "select", "us"))
	b.WriteString(debconfLine("d-i", "clock-setup/utc", "boolean", "true"))
	b.WriteString(debconfLine("d-i", "time/zone", "string", "UTC"))
	return b.String()
}

// preseedNetwork 渲染网络段落：主机名与 DHCP（装机 VLAN 上安装器自身
// 正在使用的网卡，激活它不违反 SPEC §5.7——那条规则约束的是业务网，见
// baseDirectives 同等注释）。
func preseedNetwork(ctx model.RenderContext) string {
	hostname := ctx.Facts[model.FactHostname]

	var b strings.Builder
	b.WriteString(debconfLine("d-i", "netcfg/choose_interface", "select", "auto"))
	b.WriteString(debconfLine("d-i", "netcfg/get_hostname", "string", hostname))
	b.WriteString(debconfLine("d-i", "netcfg/get_domain", "string", "unassigned-domain"))
	return b.String()
}

// preseedMirror 渲染镜像源段落：指向制品服务的仓库树（本地导入的 ISO
// 内容，不是公网镜像）。
func preseedMirror(ctx model.RenderContext) string {
	repoURL := ctx.SetRepoURL(ctx.Artifact.Paths.Repo)
	hostPart, dirPart := splitHostAndPath(repoURL)

	var b strings.Builder
	b.WriteString(debconfLine("d-i", "mirror/country", "string", "manual"))
	b.WriteString(debconfLine("d-i", "mirror/http/hostname", "string", hostPart))
	b.WriteString(debconfLine("d-i", "mirror/http/directory", "string", dirPart))
	b.WriteString(debconfLine("d-i", "mirror/http/proxy", "string", ""))
	return b.String()
}

// splitHostAndPath 把一个完整 URL 拆成"host（含端口，不含 scheme）"与
// "路径"两部分，供 mirror/http/hostname、mirror/http/directory 这类
// debconf 键分别填值（它们各自只接受 URL 的一部分，不接受完整 URL）。
func splitHostAndPath(rawURL string) (host, path string) {
	withoutScheme := rawURL
	if idx := strings.Index(rawURL, "://"); idx != -1 {
		withoutScheme = rawURL[idx+3:]
	}
	slashIdx := strings.Index(withoutScheme, "/")
	if slashIdx == -1 {
		return withoutScheme, "/"
	}
	return withoutScheme[:slashIdx], withoutScheme[slashIdx:]
}

// preseedPartitioning 渲染分区段落：用服务端决策的盘（by-id 标识，SPEC
// §5.6），配方用 partman-auto 的 recipe 语法，单分区（MVP 范围，与
// baseDirectives 的 RHEL 单分区策略一致）。
func preseedPartitioning(ctx model.RenderContext) string {
	diskByID := ctx.Facts[model.FactDiskPrimary]

	var b strings.Builder
	b.WriteString(debconfLine("d-i", "partman-auto/disk", "string", diskByID))
	b.WriteString(debconfLine("d-i", "partman-auto/method", "string", "regular"))
	b.WriteString(debconfLine("d-i", "partman-auto/choose_recipe", "select", "atomic"))
	b.WriteString(debconfLine("d-i", "partman-partitioning/confirm_write_new_label", "boolean", "true"))
	b.WriteString(debconfLine("d-i", "partman/choose_partition", "select", "finish"))
	b.WriteString(debconfLine("d-i", "partman/confirm", "boolean", "true"))
	b.WriteString(debconfLine("d-i", "partman/confirm_nooverwrite", "boolean", "true"))
	return b.String()
}

// preseedAccountAndPackages 渲染账户与软件包段落：root 不设可复用密码
// （同 kickstart 的 rootpw --lock，MVP 无 TLS，SPEC §1.5），只装基础任务。
//
// preseed 没有与 kickstart `sshkey` 指令直接等价的 debconf 键——真实
// debian-installer 的 SSH 公钥注入惯例是通过 late_command 里的一条
// `in-target mkdir -p /root/.ssh && in-target sh -c 'echo ... >> ...'`
// 完成，而不是一个独立的 debconf 键。本函数只负责 disable 密码登录这一半
// （root-login 设为 false，等价于 kickstart 的 rootpw --lock），SSH 公钥
// 注入部分复用 AssemblePreseed 组装时已经在跑的 post 阶段 hook 落盘机制
// （与 kickstart.go 的 rockyRootSSHKeyPlaceholder 同一个契约缺口，见该
// 变量的注释——本文件不重复声明第二份占位符，复用同一个包级变量）。
func preseedAccountAndPackages() string {
	var b strings.Builder
	b.WriteString(debconfLine("d-i", "passwd/root-login", "boolean", "false"))
	b.WriteString(debconfLine("d-i", "passwd/make-user", "boolean", "false"))
	b.WriteString(debconfLine("tasksel", "tasksel/first", "multiselect", "standard"))
	b.WriteString(debconfLine("popularity-contest", "popularity-contest/participate", "boolean", "false"))
	return b.String()
}

// earlyCommandValue 渲染 preseed/early_command 的值：pre 阶段的全部
// hook 调用压成一段 shell（early_command 在真实 d-i 里执行得早、环境
// 简单，不落盘脚本文件，直接内联执行——这与 post 阶段的"落脚本再执行"
// 不同，是因为 pre 阶段通常只是少量探测/准备动作，压缩成分号连接的单行
// 不会像 post 阶段那样容易因为 hook 数量增长而失控）。
//
// 与 kickstart 序言段一致：先内联参数、注入 facts，再注入 ClientLib，
// 最后按 Order 逐个调用 run_hook。
func earlyCommandValue(ctx model.RenderContext) (string, error) {
	params, err := InlineParams(ctx.Task.ResolvedParams)
	if err != nil {
		return "", fmt.Errorf("render: 参数内联失败: %w", err)
	}

	var script strings.Builder
	script.WriteString(inlineForSingleLine(params))
	script.WriteString(inlineForSingleLine(InjectFacts(ctx.Facts)))
	script.WriteString(inlineForSingleLine(ClientLib(ctx)))

	// pre-start/pre-done 信号点（phase2-01 任务 4，SPEC §6.1：pre 阶段 →
	// early_command 首尾）。RHEL 家族当前尚未实际发出这一对信号（M4 的
	// kickstart.go/firstboot.go 只发 firstboot 阶段的四个信号，installer
	// 阶段的七个信号在 RHEL 侧目前是空白——见本文件末尾契约缺口说明），
	// Debian 在这里补上，不等待 RHEL 对齐后再实现。
	script.WriteString(inlineForSingleLine(`signal "pre-start" 0` + "\n"))

	lines := StageHookCallLines(ctx.Hooks, model.HookStagePre)
	for _, line := range lines {
		script.WriteString(inlineForSingleLine(line))
	}

	script.WriteString(inlineForSingleLine(`signal "pre-done" 0` + "\n"))

	return preseedShellValue(script.String()), nil
}

// inlineForSingleLine 把一段可能含多行的 shell 文本转换成"用 ; 连接的
// 单行等价形式"的中间表示——本函数只是简单地把换行替换成 `; `，调用方
// 负责保证被替换的文本本身是可以按行分号连接执行的简单赋值/函数体（这里
// 传入的都是 params.go/clientlib.go 产出的规整文本，不含需要保留换行语义
// 的复杂控制结构，如多行 heredoc）。
func inlineForSingleLine(text string) string {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return ""
	}
	return strings.ReplaceAll(trimmed, "\n", "; ") + "; "
}

// preseedShellValue 把一段 shell 命令包装成可以安全塞进 debconf 值字段的
// 形式：外层用双引号包裹交给 late_command/early_command 解释执行的语义由
// d-i 自身处理（这两个键的值本身就是"整段传给 sh -c 的字符串"），本函数
// 只做最基本的清理——确保值本身不含裸换行（debconf 预置项本身是逐行解析
// 的文本格式，值里的裸换行会被误解析成新的一行）。
func preseedShellValue(script string) string {
	return strings.ReplaceAll(strings.TrimSpace(script), "\n", " ")
}

// sshAuthorizedKeysHeredocDelimiter 是写入 authorized_keys 那段 heredoc 的
// 定界符。选一个不太可能出现在真实 SSH 公钥文本里的具体串（LOBER 前缀 +
// 固定后缀），配合 config.Validate 与 baseDirectives 同款的"拒绝裸换行"
// 校验：公钥值本身不可能包含换行，因此也不可能包含"独占一行、内容恰好
// 等于这个定界符"的那一行——heredoc 定界符逃逸依赖的正是"注入的文本里有
// 一行内容与定界符相同"，换行已被别处拒绝，这条逃逸路径不成立。
const sshAuthorizedKeysHeredocDelimiter = "LOBER_SSH_AUTHORIZED_KEYS_EOF"

// sshAuthorizedKeysInTargetCommand 生成"在目标系统里追加写入
// /root/.ssh/authorized_keys"的 in-target 命令（SPEC §1.5：root 不设可
// 复用密码，注入 SSH 公钥并关闭密码登录）。
//
// preseed 没有与 kickstart `sshkey` 或 autoinstall `ssh.authorized-keys`
// 直接等价的键——真实 debian-installer 的惯例正是通过 late_command 里的
// in-target 命令完成这件事（preseedAccountAndPackages 函数文档已经记录
// 了这个事实）。本函数复用 late_command 脚本已经在用的 in-target bash -c
// 载体，用带引号的 heredoc（<<'EOF'，定界符加引号会关闭 shell 对 heredoc
// 正文的全部展开/解释，公钥里的单引号、双引号、$、反引号都不会被解释成
// shell 语法）把每把公钥各写一行追加进 authorized_keys。
//
// 写入前先 mkdir -p ~/.ssh 并 chmod 700：目标系统是全新安装，
// /root/.ssh 默认不存在，sshd 对权限过宽的 .ssh 目录/文件会拒绝使用
// （直接影响"公钥登录到底能不能用"这个功能本身，不只是安全加固）。
func sshAuthorizedKeysInTargetCommand(ctx model.RenderContext) (string, error) {
	inTarget, err := inTargetPrefix(model.OSFamilyDebian)
	if err != nil {
		return "", err
	}

	var inner strings.Builder
	inner.WriteString("mkdir -p /root/.ssh && chmod 700 /root/.ssh\n")
	inner.WriteString("cat >> /root/.ssh/authorized_keys <<'" + sshAuthorizedKeysHeredocDelimiter + "'\n")
	for _, key := range ctx.RootSSHPublicKeys {
		inner.WriteString(key)
		inner.WriteString("\n")
	}
	inner.WriteString(sshAuthorizedKeysHeredocDelimiter + "\n")
	inner.WriteString("chmod 600 /root/.ssh/authorized_keys\n")

	// 整段 inner 脚本被包进 in-target bash -c '...'——与 postLines 的处理
	// 方式一致，外层用单引号包裹整段命令，因此这里只需转义 inner 文本中
	// 出现的单引号本身（heredoc 定界符加了引号，内部的单引号不会被解释成
	// shell 语法，但 heredoc 结束之后、外层 in-target 命令仍然是被单引号
	// 包裹的整体，所以 heredoc 语法字符 `<<'...'` 里的单引号必须转义，
	// 否则会提前结束外层的 bash -c '...' 包裹）。
	escaped := strings.ReplaceAll(strings.TrimRight(inner.String(), "\n"), `'`, `'\''`)
	return inTarget + " bash -c '" + escaped + "'\n", nil
}

// lateCommandValue 渲染 preseed/late_command 的值：post-nochroot 阶段的
// hook（不加 in-target，直接在 d-i 环境执行）与 post 阶段的 hook（加
// in-target 前缀，chroot 进目标系统执行）依次落地为脚本文件后执行，外加
// 写入 root 的 SSH 公钥（SPEC §1.5）。
//
// 三处必须小心的细节里的第一条："late_command 是单行的，所有 post 阶段
// 的 hook 调用要压进一行——推荐落一个脚本文件再执行它，单行的可读性
// 极差，且任何一个 hook 正文里有换行就会坏掉"。本函数正是这样做：把每个
// hook 的 run_hook 调用写进一个独立脚本文件（用 in-target tee 落盘），
// late_command 的值本身只是"落盘 + chmod + 执行"这几条固定命令，不随
// hook 数量或 hook 正文长度变化——hook 数量再多，late_command 那一行的
// 长度也是常数。
//
// ctx.RootSSHPublicKeys 为空时返回错误：root 不设可复用密码
// （passwd/root-login=false），公钥是唯一登录通路，绝不能省略写入
// authorized_keys 的命令或写入占位公钥（与 kickstart.go baseDirectives、
// autoinstall.go AssembleAutoinstall 同一个设计要点）。正常部署路径下这个
// 错误不会触发：internal/config.Validate 已经在启动时拒绝了空的
// gateway.root_ssh_public_keys，本函数的检查是第二层防御。
func lateCommandValue(ctx model.RenderContext) (string, error) {
	if len(ctx.RootSSHPublicKeys) == 0 {
		return "", fmt.Errorf("render: RenderContext.RootSSHPublicKeys 为空，" +
			"无法渲染 preseed late_command——root 不设可复用密码，公钥是唯一登录通路，" +
			"绝不能省略写入 authorized_keys 的命令或写入占位公钥，" +
			"那会静默产出一台没人能登录的机器（SPEC §1.5）")
	}

	nochrootLines := StageHookCallLines(ctx.Hooks, model.HookStagePostNoChroot)
	postLines := StageHookCallLines(ctx.Hooks, model.HookStagePost)

	// 即使没有任何 post-nochroot/post 阶段的 hook，late_command 仍必须
	// 发出 post-start/post-done/rebooting 三个信号（phase2-01 任务 4：
	// "三个家族各自生成全部信号点"，不因为用户没挂 hook 就让信号点跟着
	// 消失——信号协议描述的是装机流程本身的阶段推进，与用户是否挂了 hook
	// 无关）。因此这里不再有"无 hook 就用占位命令跳过整个脚本"的早退
	// 分支，一律走下面的完整脚本生成路径。

	const scriptPath = "/tmp/lober-late.sh"

	var script strings.Builder
	script.WriteString("#!/bin/bash\n")
	script.WriteString(ClientLib(ctx))

	// post-start 信号（phase2-01 任务 4，SPEC §6.1：post 阶段 → late_command
	// 首尾）。在 d-i 环境（chroot 外）发出，与 rebooting 一起构成"这个脚本
	// 自己负责的信号边界"——post-nochroot 与 post 两类 hook 都夹在这一对
	// 信号之间，不区分谁在 chroot 内谁在 chroot 外，因为对信号协议而言
	// "post 阶段"是 late_command 整体，chroot 与否是 Debian 内部的能力
	// 边界（本文件头部注释），不是信号协议关心的边界。
	script.WriteString(`signal "post-start" 0` + "\n")

	// packages-start/packages-done（本轮补上的第二个缺口，§6.1 信号点表；
	// 计划任务 4 的三家族信号点映射表把 Debian 这两个信号标注为"安装基础
	// 系统前/后"）。真实 debian-installer 里，装包（base-installer 跑
	// debootstrap，随后 pkgsel 装 tasksel 选中的任务）发生在 late_command
	// 执行之前——late_command 是整个 d-i 流程的最后一步，此刻基础系统与
	// 任务包早已装完。这意味着 preseed 没有任何 debconf 钩子能让我们在
	// debootstrap/pkgsel 真正开始或结束的那个时刻插入命令，我们能拿到的
	// 最早执行点就是 late_command 脚本本身——而这个点在时间上已经晚于真实
	// 的装包动作。
	//
	// 采用与 Ubuntu autoinstall.go 的 firstbootInTargetCommands 同一栏
	// （§6.1 映射表："只能近似"）相同的近似方案：在 post-start 之后立即
	// 成对发出这两个信号，不追求精确对应真实解包动作的起止时刻。选择"成对
	// 紧邻"而不是"分开插在脚本中间某处"，是因为分开插不会让近似更准（装包
	// 早已结束，脚本里任何位置都同样是"晚于真实动作"），却会让人误读为
	// "这两个信号之间的时间跨度对应真实装包耗时"——紧邻发出更清楚地表达
	// "这只是一个占位性的进度打点，不是精确计量"。
	//
	// 这不是完全没有意义的占位：至少它让运维在事件时间线上能看到"装包阶段
	// 已上报"这件事本身（区分"卡在装包"与"卡在更早的阶段"仍然成立——若
	// packages-start 都没发出，说明卡在 pre 阶段或更早；若发出了但一直没有
	// post-done，说明卡在 post 阶段的某处，只是不能再精确到"卡在装包本身"
	// 还是"卡在用户 hook"）。
	//
	// 契约缺口：见本文件末尾 AssemblePreseed 之后的说明。
	script.WriteString(`signal "packages-start" 0` + "\n")
	script.WriteString(`signal "packages-done" 0` + "\n")

	// SSH 公钥写入是独立于 hook 的固定动作，不依赖用户是否挂了任何 post
	// 阶段的 hook——放在 post-start 之后、hook 调用之前执行，与
	// passwd/root-login=false（preseedAccountAndPackages）在装机流程上
	// 尽量靠近，都是"账户登录策略"这一类固定基础设施。
	sshCmd, err := sshAuthorizedKeysInTargetCommand(ctx)
	if err != nil {
		return "", err
	}
	script.WriteString(sshCmd)

	for _, line := range nochrootLines {
		script.WriteString(line)
	}
	if len(postLines) > 0 {
		inTarget, err := inTargetPrefix(model.OSFamilyDebian)
		if err != nil {
			return "", err
		}

		// 修复的真实 bug（本文件设计期发现，与 kickstart.go 修复过的同类
		// bug 同源）：`in-target bash -c '...'` 是 chroot 包装器起的一个
		// 全新 bash 子进程，运行在目标系统里——它与外层脚本（d-i 环境里
		// 定义 run_hook/signal 的那个进程）不共享任何变量或函数定义，
		// chroot 边界比 kickstart 多个 %post 段落各自独立子进程的边界
		// 更彻底（chroot 连文件系统根都换了）。若只把 "run_hook '...' ||
		// exit 1" 这一行原样塞进 in-target bash -c，目标系统里的 bash
		// 会得到 "run_hook: command not found"（exit 127），required
		// hook 失败即中止装机这条安全不变量会被静默架空——这与
		// kickstart.go RenderStageSection 头部注释描述的 bug 是同一类
		// 错误，只是载体从"anaconda 各段落独立子进程"变成了"in-target
		// chroot 边界"。
		//
		// 修复方式一致：把 ClientLib(ctx) 与该 hook 的调用行一并塞进
		// 同一个 in-target bash -c 调用里，让目标系统里的这个新 bash
		// 进程也能看到 run_hook/signal 的定义。
		for _, line := range postLines {
			inTargetScript := ClientLib(ctx) + line
			script.WriteString(inTarget + ` bash -c '` +
				strings.ReplaceAll(strings.TrimRight(inTargetScript, "\n"), `'`, `'\''`) + "'\n")
		}
	}

	// firstboot 脚手架（本轮补上的第一个、也是更严重的那个缺口）：埋点
	// systemd oneshot unit、hook 脚本、finalize token，全部落在 in-target
	// 之内——FirstbootScaffold 写的路径（/var/lib/lober、
	// /etc/systemd/system、/usr/local/sbin）都是目标系统路径，late_command
	// 顶层脚本本身跑在 d-i 环境（未 chroot），若不加 in-target 直接执行会
	// 落在 installer 自己的临时环境里，重启后目标系统里什么都不会有，
	// 四个重启后信号（booted/init-start/init-done/ready）就没有任何机制
	// 会发出——这正是 Debian preseed 端目前最严重的缺口（任务描述：装完
	// 重启后会卡在 rebooting 直到超时，且 rebooting 超时是不可重装的失败
	// 态，SPEC §3.4）。
	//
	// 放在这里（用户 post 阶段 hook 之后、post-done 之前）而不是更早：与
	// kickstart.go AssembleKickstart 的顺序一致（firstboot 脚手架在三个
	// installer 阶段的 hook 段落之后追加），保证两个家族在"脚手架相对用户
	// hook 的先后关系"上是一致的行为，便于跨家族排查问题。
	//
	// 这里独立再取一次 in-target 前缀（不复用上面 if 块内的 inTarget 局部
	// 变量）：那个变量的作用域被限制在 postLines 非空的分支里，postLines
	// 为空时（minimalPreseedCtx 就是这种情况）该变量不存在，而 firstboot
	// 脚手架必须无条件执行（TestPreseed_FirstbootScaffoldRunsEvenWithoutHooks
	// 锁住的正是这一点——脚手架描述装机流程本身的能力，不因为这台机器的
	// 模板没挂任何 hook 就整体消失）。
	scaffoldInTarget, err := inTargetPrefix(model.OSFamilyDebian)
	if err != nil {
		return "", err
	}
	scaffold := FirstbootScaffold(ctx)
	script.WriteString(scaffoldInTarget + ` bash -c '` +
		strings.ReplaceAll(strings.TrimRight(scaffold, "\n"), `'`, `'\''`) + "'\n")

	// post-done 与 rebooting（SPEC §6.1："rebooting 与 booted 配成一对，
	// 中间的空窗即明确的重启窗口"）。两者都在 d-i 环境发出——late_command
	// 执行完毕后 d-i 即将触发重启，这个脚本本身就是 late_command 阶段
	// 全部工作的收尾点。
	script.WriteString(`signal "post-done" 0` + "\n")
	script.WriteString(`signal "rebooting" 0` + "\n")

	// late_command 的值：用 in-target 把脚本内容写入目标系统的临时路径
	// （late_command 阶段目标盘已经挂好，即使是 post-nochroot 的命令也能
	// 直接操作目标盘上的文件，无需额外 mkdir），赋可执行权限后执行。
	// 写入动作复用 preseedShellValue 里"值不能含裸换行"的约束，用 base64
	// 规避——脚本正文可能含引号与特殊字符（hook digest 本身是十六进制，
	// 但脚本里的 ClientLib 正文含大量 shell 语法），转义穿透 late_command
	// 单行值的风险远高于直接 base64 编码后用 base64 -d 落盘。
	encoded := base64Encode(script.String())

	return fmt.Sprintf(
		"in-target sh -c 'echo %s | base64 -d > %s && chmod 755 %s && %s'",
		encoded, scriptPath, scriptPath, scriptPath,
	), nil
}

// AssemblePreseed 把五组段落与三个 installer 阶段的 hook 组装成完整的
// Debian preseed.cfg 文本（计划任务 2）。
func AssemblePreseed(ctx model.RenderContext) (string, error) {
	// 提前做一次参数内联校验：即使 early_command/late_command 内部各自也会
	// 调用 InlineParams，这里先校验一遍可以让"表单值不合法"的错误在组装
	// 最开始就被发现，不必等到拼好一半才失败（与 kickstart 序言段的校验
	// 时机一致：错误发生点越早，越不容易让人误以为"前面的段落已经生效"）。
	if _, err := InlineParams(ctx.Task.ResolvedParams); err != nil {
		return "", fmt.Errorf("render: 参数内联失败: %w", err)
	}

	var b strings.Builder

	b.WriteString(preseedLocalization())
	b.WriteString(preseedNetwork(ctx))
	b.WriteString(preseedMirror(ctx))
	b.WriteString(preseedPartitioning(ctx))
	b.WriteString(preseedAccountAndPackages())

	early, err := earlyCommandValue(ctx)
	if err != nil {
		return "", err
	}
	b.WriteString(debconfLine("d-i", "preseed/early_command", "string", early))

	late, err := lateCommandValue(ctx)
	if err != nil {
		return "", err
	}
	b.WriteString(debconfLine("d-i", "preseed/late_command", "string", late))

	return b.String(), nil
}
