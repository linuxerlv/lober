// 本文件定义 hook 与模板类型：Hook、HookStage、Template、HookRef，以及
// hook digest 计算。对应实现计划任务 7，依据 SPEC §2.2、§2.3、§2.6、§2.7、
// §5.2、§6.3。
package model

import (
	"crypto/sha256"
	"encoding/hex"
)

// HookStage 是 hook 能挂载的四个执行环境（§5.2）。阶段不是排序，是能力
// 边界：装机过程中有几个能力完全不同的执行环境，hook 放错阶段不是顺序错
// 了，是根本跑不起来。
type HookStage string

const (
	HookStagePre          HookStage = "pre"
	HookStagePostNoChroot HookStage = "post-nochroot"
	HookStagePost         HookStage = "post"
	HookStageFirstboot    HookStage = "firstboot"
)

// validHookStages 是 HookStage 的白名单。它来自用户输入（注册 hook 时填），
// 必须白名单校验——阶段不是排序而是能力边界，放错阶段的 hook 根本跑不起来
// （§5.2，计划任务 7 设计要点）。
var validHookStages = map[HookStage]bool{
	HookStagePre:          true,
	HookStagePostNoChroot: true,
	HookStagePost:         true,
	HookStageFirstboot:    true,
}

// Valid 判定该 HookStage 是否为四个合法取值之一。
func (s HookStage) Valid() bool {
	return validHookStages[s]
}

// HookState 是 hook 的审批生命周期状态（§2.2）。
type HookState string

const (
	HookStateDraft      HookState = "draft"
	HookStateReview     HookState = "review"
	HookStateActive     HookState = "active"
	HookStateDeprecated HookState = "deprecated"
)

// HookRequires 把运行时意外提前为注册时错误（§2.2、§7.5）：注册与激活时
// 校验 fact 键是否存在、所需 capability 的 provider 是否已配置、
// contract_version 是否匹配。
type HookRequires struct {
	ContractVersion string // 版本约束表达式，如 ">=1"
	Facts           []string
	Capabilities    []string
}

// Hook 是用户注册的一段机器侧脚本（§2.2）。hook 表中只存用户 hook——系统
// 必需的机器侧动作（遥测、firstboot 脚手架、凭据清理）由渲染器生成，不作为
// hook 数据存在（§5.1）。
type Hook struct {
	ID          string
	Name        string
	Description string

	Stage       HookStage
	OSFamilies  []OSFamily // 支持哪些家族；为空表示不限家族，不是「不支持任何家族」
	Interpreter string     // bash | powershell | ash(esxi)

	Body       string
	BodySHA256 string // 内容寻址 digest，由 Body 计算得出（HookDigest）

	ParamsSchema string // JSON Schema 文本，声明需要哪些参数
	Requires     HookRequires

	Required   bool
	TimeoutSec int
	Version    int
	State      HookState
	Author     string
	Approver   string
	ApprovedAt int64
}

// SupportsFamily 判定该 hook 是否支持给定 OS 家族。OSFamilies 为空表示不
// 限家族（返回 true），这是刻意的设计——空列表不等于「不支持任何家族」
// （§2.2、计划任务 7 设计要点）。
func (h Hook) SupportsFamily(family OSFamily) bool {
	if len(h.OSFamilies) == 0 {
		return true
	}
	for _, f := range h.OSFamilies {
		if f == family {
			return true
		}
	}
	return false
}

// HookDigest 计算 hook 正文的内容寻址 digest。
//
// 必须与 coreutils sha256sum 的输出一致：正文字节的裸 sha256 十六进制
// 小写——不加盐、不加前缀、不做任何规范化。机器侧的 run_hook 用
// `sha256sum -c` 校验下载到的脚本（§6.3），算法对不上会导致所有 hook 都
// 校验失败。
func HookDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// HookRef 是模板对某个 hook 版本的引用（§2.3）。pin 住的是 digest 不是
// id——这是 §2.7 的浮动引用防护：hook 正文不可变，修改即新版本新 digest，
// 模板 pin 的是 digest，故审批过的内容不会被偷换。
type HookRef struct {
	HookID         string
	HookDigest     string
	Stage          HookStage
	Order          int
	Required       bool
	ParamsDefaults map[string]string
}

// TemplateState 是模板的生命周期状态。
type TemplateState string

const (
	TemplateStateDraft      TemplateState = "draft"
	TemplateStateActive     TemplateState = "active"
	TemplateStateDeprecated TemplateState = "deprecated"
)

// DiskPolicy 是模板级的磁盘选择策略（§2.3）。
type DiskPolicy string

const (
	DiskPolicyFirst    DiskPolicy = "first"
	DiskPolicyLargest  DiskPolicy = "largest"
	DiskPolicyByModel  DiskPolicy = "by-model"
	DiskPolicyRAID1SSD DiskPolicy = "raid1-ssd"
	DiskPolicyByHook   DiskPolicy = "by-hook"
)

// OnFailurePolicy 是模板级的失败后处理策略（§2.3）：这是运维策略而非渲染
// 器该定的事——调试期用 shell（留住机器供人工登录），生产用 reboot 加上报。
type OnFailurePolicy string

const (
	OnFailureReboot      OnFailurePolicy = "reboot"
	OnFailureHalt        OnFailurePolicy = "halt"
	OnFailureShell       OnFailurePolicy = "shell"
	OnFailureKeepNetwork OnFailurePolicy = "keep-network"
)

// Template 是组装单元，必须版本化（§2.3）。
type Template struct {
	ID   string
	Name string

	ArtifactSetID  string
	AnswerTemplate string // ks.cfg / user-data / preseed.cfg / unattend.xml 等模板文本
	HookRefs       []HookRef

	KernelArgs         string
	DiskPolicy         DiskPolicy
	OnFailure          OnFailurePolicy
	RequiredFormFields []string

	Version int
	State   TemplateState
}

// HooksForStage 返回该模板在某阶段的 hook 引用，按 Order 升序排列，且只含
// 该阶段的条目。排序收敛在这一处，渲染器直接消费结果不再自己排——否则
// 渲染器与控制台的 preview 端点会各排一遍，迟早不一致（计划任务 7 设计要点）。
//
// 返回新分配的切片，不共享或修改 Template.HookRefs 的底层数组。
func (t Template) HooksForStage(stage HookStage) []HookRef {
	matched := make([]HookRef, 0, len(t.HookRefs))
	for _, ref := range t.HookRefs {
		if ref.Stage == stage {
			matched = append(matched, ref)
		}
	}
	for i := 1; i < len(matched); i++ {
		for j := i; j > 0 && matched[j-1].Order > matched[j].Order; j-- {
			matched[j-1], matched[j] = matched[j], matched[j-1]
		}
	}
	return matched
}
