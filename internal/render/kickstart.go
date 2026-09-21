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
