// 本文件实现 hook 阶段到 kickstart 段落的映射，以及 kickstart 应答文件的
// 整体组装（对应实现计划任务 5、6，依据 SPEC §5.2、§5.6）。
//
// 阶段不是排序，是能力边界（SPEC §5.2）：装机过程中有几个能力完全不同的
// 执行环境，hook 放错阶段不是顺序错了，是根本跑不起来。
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// defaultHookTimeoutSec 是传给 run_hook 的默认超时值（单位秒）。
//
// 契约缺口：model.HookRef（RenderContext.Hooks 的元素类型）不携带
// TimeoutSec——那个字段只存在于 model.Hook（hook 的完整注册记录）上，
// 而 RenderContext 只拿到已经 pin 好版本的引用列表，没有对应的完整 Hook
// 记录可查。计划任务 5 明确要求"为每个生成一次 run_hook 调用，参数是
// digest、required 标志与超时值"，但当前契约没有提供获取该超时值的途径。
//
// 保守处理：对全部 installer 阶段的 hook 统一使用这个固定默认值（5
// 分钟），而不是编造一个"从 digest 反查超时"的臆测接口。5 分钟是一个
// 相对宽松、不太可能误伤正常 hook 的默认上限；宁可稍宽松也不要过严——
// 过短的超时会让合法 hook 被错误中止，而"稍微宽松"的代价只是在真正卡死
// 时多等几分钟才触发失败，两者不对等。
//
// 正解需要 M1 在 HookRef 上补一个 TimeoutSec 字段（与 Required、Order
// 平级），或在 RenderContext 上新增一个 map[digest]int 的并行结构。一旦
// 契约补上，只需把下面 runHookCall 里的 defaultHookTimeoutSec 替换成该
// 字段即可，不需要改动本文件的其它逻辑。
const defaultHookTimeoutSec = 300

// hookStageSections 是 HookStage 到 kickstart 段落头的映射表。firstboot
// 不在此表中——它不是段落，落进 FirstbootScaffold 生成的脚手架，绝不会
// 出现在任何 kickstart 段落里（计划任务 5 明确警示）。
var hookStageSections = map[model.HookStage]string{
	model.HookStagePre:          "%pre",
	model.HookStagePostNoChroot: "%post --nochroot",
	model.HookStagePost:         "%post",
}

// RenderStageSection 渲染某个 installer 阶段（pre / post-nochroot / post）
// 对应的 kickstart 段落文本。该阶段没有任何 hook 时返回空字符串——空阶段
// 不产生空段落（计划任务 5 测试要求："%pre 里没东西就不要写 %pre"）。
//
// stage 若是 firstboot 或任何不在 hookStageSections 表中的值，同样返回
// 空字符串：firstboot hook 不落在任何 kickstart 段落里（它们进
// FirstbootScaffold），这里不对非法阶段值报错，因为调用方（本包内部的
// AssembleKickstart）只会传入三个合法的 installer 阶段常量。
//
// 修复的真实 bug（原实现未处理，验证时用真实 bash 子进程执行才发现）：
// anaconda 把每一个 %pre / %post / %post --nochroot 段落各自落成独立的
// /tmp/ks-script-XXXX 文件，用 execWithRedirect 各自起一个全新子进程执行
// （anaconda 源码 pyanaconda/kickstart.py 的 runPreScripts 与
// pyanaconda/core/kickstart/scripts.py 的 run_script 证实）——同类型的多个
// %pre 段落之间、以及 %pre 与 %post/%post --nochroot 之间，完全不共享变量
// 与函数定义。序言段的 %pre 段落定义了 signal()/run_hook() 两个函数，但那
// 只在序言段自己的子进程里生效；本函数产出的三个 installer 阶段段落是
// **另外三个独立的段落**，若不在这里重新注入 ClientLib(ctx)，其中的
// run_hook 调用在真机上会得到 "run_hook: command not found"（exit 127）
// ——required hook 失败即中止装机这条安全不变量因此被静默架空（run_hook
// 本身都不存在，required 判断逻辑根本不会被执行到）。
//
// 修复方式与 firstboot.go 的 initScript 完全一致：那个函数已经正确地在
// 自己独立执行的 heredoc 脚本里重新注入了一份 ClientLib——因为原作者清楚
// 那是另一个独立进程。这里补上同样的处理，让每个 installer 阶段段落也
// 自成一个完整脚本。
func RenderStageSection(ctx model.RenderContext, stage model.HookStage) string {
	header, ok := hookStageSections[stage]
	if !ok {
		return ""
	}

	// 骨架生成器（skeleton.go）返回结构化的 hook 调用行列表，本函数只负责
	// 决定"怎么把这些行嵌进 kickstart 的段落语法"——内容与嵌入方式的分离，
	// 是 phase2-01 任务 1 抽出共用骨架的设计要点。
	lines := StageHookCallLines(ctx.Hooks, stage)
	if len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(ClientLib(ctx))
	for _, line := range lines {
		b.WriteString(line)
	}
	b.WriteString("%end\n")
	return b.String()
}

// hooksForStage 从完整的 hook 引用列表中筛出属于某阶段的条目，按 Order
// 升序排列。不假设调用方已经排好序——即使 M1 的 Template.HooksForStage
// 已经排过一遍，RenderContext.Hooks 的构造方在两者之间可能引入乱序（例如
// 合并了多个来源），渲染器自己的输出顺序不应依赖外部调用约定，否则同一份
// 输入换一个调用方就可能产出不同的 golden file。
//
// phase2-01：本函数现在是 skeleton.go 里 StageHookCallLines 的内部实现
// 细节（保持未导出，避免包内出现两套"筛选+排序"逻辑）；kickstart.go 的
// RenderStageSection 改为直接调用 StageHookCallLines，不再直接调用本函数，
// 但函数体本身原样保留（RHEL 的 golden file 逐字节不变的前提是排序与筛选
// 规则一字不改）。
func hooksForStage(hooks []model.HookRef, stage model.HookStage) []model.HookRef {
	var matched []model.HookRef
	for _, h := range hooks {
		if h.Stage == stage {
			matched = append(matched, h)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Order < matched[j].Order })
	return matched
}

// runHookCall 渲染一行对 run_hook 的调用，参数是 digest、required 标志与
// 超时值（计划任务 5 实现要点）。
//
// 修复的真实 bug（原实现未处理，验证时用真实 bash 子进程执行才发现）：
// run_hook 函数体末尾的 `[ "$rc" -eq 0 ] || [ "$required" != "true" ]`
// 在 required hook 真的失败时会正确返回退出码 1（计划任务 3 clientlib.go
// 已经验证过这一点）——但原实现只是裸调用 `run_hook '...' '...' '...'`，
// 不检查它的返回值。bash 脚本默认不会因为某一行命令返回非零就中止（没有
// `set -e`），会继续执行下一行。当同一阶段有多个 hook 时（计划任务 5 明确
// 要求支持"同阶段多个 hook 按 order 顺序出现"），若第一个 required hook
// 失败、第二个 hook 成功，整个段落脚本的退出码会被第二条命令的成功状态
// 覆盖——anaconda 会认为这个 %post/%pre 段落整体成功，继续装机，正是计划
// 原文点名警示的"required hook 失败也不会中止装机，机器带着半配置的状态
// 装完"。
//
// 修复：每条 run_hook 调用后追加 `|| exit 1`——run_hook 只有在"hook 失败
// 且 required"时才返回非零（成功、或失败但非 required 都返回 0，参见
// clientlib.go 的函数体），所以这个 `|| exit 1` 只在真正需要中止的那一种
// 情况下触发，不会误杀"失败但非 required，应记日志继续"的路径。
// exit 1 中止的是这个段落自己独立的脚本进程（不是整个 kickstart 文件的
// 其它段落），与 anaconda "各段落各自成脚本、各自决定是否因非零退出码而
// 中止装机"的执行模型完全吻合。
func runHookCall(ref model.HookRef) string {
	return fmt.Sprintf("run_hook '%s' '%t' '%d' || exit 1\n", ref.HookDigest, ref.Required, defaultHookTimeoutSec)
}

// baseDirectives 渲染 Rocky 9 kickstart 的基础指令块：语言、键盘、时区、
// 引导加载器、磁盘、网络、root 密码策略（计划任务 6"组装顺序"第一段）。
//
// MVP 范围内这部分内容由渲染器直接生成（不经由 Template.AnswerTemplate），
// 理由：计划明确要求"kickstart 模板文本...也由你写"，且 MVP 只需要 Rocky 9
// 走通一条完整链路；模板 CRUD（让运维通过 UI 编辑基础指令）是 M6/M8 的
// 职责，不在本模块范围内。AnswerTemplate 字段在本渲染器中只承载 %packages
// 内容（见 AssembleKickstart 的注释）。
//
// 磁盘用 by-id 标识，来自服务端决策（ctx.Facts[LOBER_DISK_PRIMARY]），不
// 在这里硬编码 /dev/sda（计划任务 6 明确警示）。
//
// SSH 公钥来自 ctx.RootSSHPublicKeys（model.RenderContext 新增字段，
// SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码登录）。为空时
// 返回错误而不是省略这一行或写死一个占位公钥——省略会让机器装完后 root
// 完全无法登录，写占位值会让人误以为已经配置了真实凭据，两者都比报错更
// 危险。正常部署路径下这个错误不会触发：internal/config.Validate 已经在
// 启动时拒绝了空的 gateway.root_ssh_public_keys，本函数这里的检查是第二层
// 防御（万一渲染上下文的构造路径漏了赋值）。
func baseDirectives(ctx model.RenderContext) (string, error) {
	if len(ctx.RootSSHPublicKeys) == 0 {
		return "", fmt.Errorf("render: RenderContext.RootSSHPublicKeys 为空，" +
			"无法渲染 kickstart——root 不设可复用密码（rootpw --lock），" +
			"公钥是唯一登录通路，绝不能省略这一行或写入占位公钥，" +
			"那会静默产出一台没人能登录的机器（SPEC §1.5）")
	}

	diskByID := ctx.Facts[model.FactDiskPrimary]

	var b strings.Builder
	b.WriteString("lang en_US.UTF-8\n")
	b.WriteString("keyboard us\n")
	b.WriteString("timezone UTC --utc\n")
	b.WriteString("bootloader --location=mbr\n")
	fmt.Fprintf(&b, "clearpart --all --drives=%s\n", diskByID)
	fmt.Fprintf(&b, "part / --fstype=xfs --grow --ondisk=%s\n", diskByID)
	// 这是装机 VLAN 上安装器自身正在使用的网卡（机器正是通过它 PXE 引导
	// 到这里的），激活它不违反 SPEC §5.7 的"业务网写配置但不激活"——那条
	// 规则约束的是 %post 阶段用户 hook 可能配置的业务网（另一 VLAN），与
	// 这里的装机网是两个完全不同的网络。装机 VLAN 的 DHCP 本就应保持到
	// rebooting 信号发出（§5.7），这行正是维持这一点的基础指令。
	b.WriteString("network --bootproto=dhcp --activate\n")
	// root 不设可复用密码：锁定密码登录，只允许 SSH 公钥登录（MVP 无 TLS，
	// 应答文件被截获也换不成持久访问，SPEC §1.5）。
	b.WriteString("rootpw --lock\n")
	// 逐把公钥各生成一行 sshkey 指令——kickstart 语法本身支持多次出现该
	// 指令（anaconda 会把每一把都写进 /root/.ssh/authorized_keys），这与
	// 该文件本身"任一行匹配即放行"的语义一致（config.go 字段文档：团队
	// 多人各自持有私钥，轮换期需要新旧并存）。
	//
	// 本函数不单靠上游 config.Validate 的白名单校验就假设输入无害——万一
	// 未来出现另一条不经过 config.Validate 的构造路径（例如测试或其它
	// 装配点直接构造 RenderContext），这里仍要独立防住两类逃逸：
	//  1. 双引号：转义为 \"，防止提前结束 sshkey 指令的字符串边界。
	//  2. 裸换行：kickstart 是逐行解析的文本格式，换行本身就能在这一行
	//     指令中间开出一条新指令，光转义引号不够——即使双引号被转义了，
	//     换行仍会让解析器把后半段文本当成下一行独立的 kickstart 指令
	//     （真实验证过：只转义引号、不处理换行时，一个跨两行的恶意公钥
	//     能在 sshkey 那一行之后拼出一条独立的 rootpw 指令）。因此换行
	//     直接拒绝整次渲染而不是转义或删除——公钥值本身就不应该含换行，
	//     出现即说明上游校验被绕过或数据已损坏，报错比生成一份结构被
	//     篡改的应答文件更安全。
	for _, key := range ctx.RootSSHPublicKeys {
		if strings.ContainsAny(key, "\n\r") {
			return "", fmt.Errorf("render: RootSSHPublicKeys 中的公钥 %q 含裸换行，"+
				"kickstart 是逐行解析的文本格式，换行会在 sshkey 指令中间开出一条独立的新指令"+
				"（SPEC §5.6 注入防护）——这个值不应该出现在这里，说明上游校验被绕过或数据已损坏", key)
		}
		escaped := strings.ReplaceAll(key, `"`, `\"`)
		fmt.Fprintf(&b, "sshkey --username=root \"%s\"\n", escaped)
	}
	return b.String(), nil
}

// loggingDirective 渲染 `logging --host=` 一行，指向 syslog（计划任务 6
// "组装顺序"第二段，SPEC §6.5）。
func loggingDirective(ctx model.RenderContext) string {
	return fmt.Sprintf("logging --host=%s --port=%d --level=info\n", ctx.SyslogHost, ctx.SyslogPort)
}

// prologue 渲染序言段：参数内联 + fact 注入 + 注入的 signal/run_hook 函数
// （SPEC §5.1 三段结构的第一段，计划任务 1-3 的产出在此处组合）。
//
// 注入位置在序言段，早于任何用户 hook 的调用（计划任务 3 设计要点）——
// prologue 整体先于 RenderStageSection 产出的 hook 段落出现。
func prologue(ctx model.RenderContext) (string, error) {
	var b strings.Builder
	b.WriteString("%pre\n")

	params, err := InlineParams(ctx.Task.ResolvedParams)
	if err != nil {
		return "", fmt.Errorf("render: 参数内联失败: %w", err)
	}
	b.WriteString(params)
	b.WriteString(InjectFacts(ctx.Facts))
	b.WriteString("# 用户 hook 从这里开始\n")
	b.WriteString(ClientLib(ctx))
	b.WriteString("%end\n")
	return b.String(), nil
}

// AssembleKickstart 把前五个任务的产出按 SPEC §5.1 三段结构与计划任务 6
// 的组装顺序拼成完整的 kickstart 应答文件文本：
//
//	基础指令 → logging 一行 → %packages（来自 Template.AnswerTemplate）→
//	三个 installer 阶段的 hook 段 → firstboot 脚手架段
//
// MVP 只对 RHEL 家族（Rocky 9）调用本函数——其它家族在 renderer.go 的入口
// 分派处直接返回"未实现"错误，不会走到这里（计划任务 8）。
func AssembleKickstart(ctx model.RenderContext) (string, error) {
	var b strings.Builder

	base, err := baseDirectives(ctx)
	if err != nil {
		return "", err
	}
	b.WriteString(base)
	b.WriteString(loggingDirective(ctx))
	b.WriteString(ctx.Template.AnswerTemplate)
	b.WriteString("\n")

	pro, err := prologue(ctx)
	if err != nil {
		return "", err
	}
	b.WriteString(pro)

	for _, stage := range []model.HookStage{
		model.HookStagePre, model.HookStagePostNoChroot, model.HookStagePost,
	} {
		b.WriteString(RenderStageSection(ctx, stage))
	}

	// firstboot 脚手架必须落在自己的 %post ... %end 段落里（SPEC §5.8 的
	// 参考实现正是这样写的）——它需要写文件到目标系统的
	// /var/lib/lober、/etc/systemd/system、/usr/local/sbin，这些路径只有
	// 在某个 %post 段落内、chroot 进目标系统之后才有意义。写成不属于任何
	// 段落的裸露文本，anaconda 会把它当成语法错误或直接忽略，firstboot
	// 机制会在没有任何明显报错的情况下整体失效——这是最容易被忽略的一类
	// bug，因为通过编译与单元断言都不会暴露，只有 golden file 的完整装配
	// 顺序检查才能抓到。
	b.WriteString("%post\n")
	b.WriteString(FirstbootScaffold(ctx))
	b.WriteString("%end\n")

	return b.String(), nil
}
