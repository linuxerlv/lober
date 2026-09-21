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
func RenderStageSection(ctx model.RenderContext, stage model.HookStage) string {
	header, ok := hookStageSections[stage]
	if !ok {
		return ""
	}

	refs := hooksForStage(ctx.Hooks, stage)
	if len(refs) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	for _, ref := range refs {
		b.WriteString(runHookCall(ref))
	}
	b.WriteString("%end\n")
	return b.String()
}

// hooksForStage 从完整的 hook 引用列表中筛出属于某阶段的条目，按 Order
// 升序排列。不假设调用方已经排好序——即使 M1 的 Template.HooksForStage
// 已经排过一遍，RenderContext.Hooks 的构造方在两者之间可能引入乱序（例如
// 合并了多个来源），渲染器自己的输出顺序不应依赖外部调用约定，否则同一份
// 输入换一个调用方就可能产出不同的 golden file。
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
func runHookCall(ref model.HookRef) string {
	return fmt.Sprintf("run_hook '%s' '%t' '%d'\n", ref.HookDigest, ref.Required, defaultHookTimeoutSec)
}

// rockyRootSSHKeyPlaceholder 是 MVP 阶段 root 免密登录注入的 SSH 公钥占位
// 值（对应实现计划任务 6"基础指令里的两条要点"之一："root 不设可复用密码，
// 注入 SSH 公钥并关闭密码登录"）。
//
// 契约缺口：SPEC §1.5 与本计划都要求注入 SSH 公钥，但 model.RenderContext
// / model.Task / model.Template / model.FactKeysV1 的 24 个 fact 键中都
// 没有任何字段承载"要注入的 SSH 公钥文本"。这不是可以从现有字段间接推导
// 的值（不像磁盘 by-id 那样能从 Facts[LOBER_DISK_PRIMARY] 拿到）——它是
// 一个纯粹缺失的输入。
//
// 保守处理：不静默省略 SSH 公钥指令（省略会让机器装完后 root 完全无法
// 登录，比"用一个占位公钥"更危险——运维连不上任何刚装好的机器）；也不去
// 触碰 internal/config（那是冻结契约，禁止修改）。因此用一个包级变量
// （不是 const——保留被测试或未来装配点覆盖的可能性）承载一个明显不可用
// 的占位公钥，值本身以 "REPLACE-ME" 开头，任何人拿它去连接真实机器都会
// 立刻失败并意识到需要配置真实公钥。
//
// 正解需要契约新增一处 SSH 公钥来源——最自然的位置是
// internal/config.Gateway（新增字段 RootSSHPublicKey）或
// model.RenderContext（新增字段），由 M9 装配时把配置值传下来，
// 到时只需把下面 baseDirectives 里对本变量的引用换成 ctx 上的新字段。
var rockyRootSSHKeyPlaceholder = "ssh-ed25519 REPLACE-ME-lober-has-no-contract-field-for-this-key placeholder@lober"

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
func baseDirectives(ctx model.RenderContext) string {
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
	fmt.Fprintf(&b, "sshkey --username=root \"%s\"\n", rockyRootSSHKeyPlaceholder)
	return b.String()
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

	b.WriteString(baseDirectives(ctx))
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
