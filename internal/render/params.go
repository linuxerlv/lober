// Package render 实现 contract.Renderer：输入 model.RenderContext，输出
// iPXE 菜单与应答文件文本。纯函数——不查库、不碰时钟、不做 IO（SPEC §5.1）。
//
// 本文件负责两件事（对应实现计划任务 2，依据 SPEC §5.3、§5.4）：
//
//  1. 参数内联：把 RenderContext.Task.ResolvedParams 按键名选对应的类型
//     校验器逐一校验后，内联为 shell 变量赋值（渲染时直接内联，机器无需
//     下载参数文件）。
//  2. fact 命名空间注入：无条件注入 model.FactKeysV1 的全部键，不管 hook
//     用不用。
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/render/guard"
)

// paramValidator 是某个表单键对应的校验函数：接收该键的字符串值，合法则
// 返回 nil，否则返回描述性错误。
type paramValidator func(value string) error

// ipValidator 把 guard.ValidateIP 适配成 paramValidator 签名。
func ipValidator(value string) error {
	return guard.ValidateIP(value)
}

// hostnameValidator 把 guard.ValidateHostname 适配成 paramValidator 签名。
func hostnameValidator(value string) error {
	return guard.ValidateHostname(value)
}

// roleValidator 校验 role 字段：SPEC §2.4/§8.3 把 role 列为 form_data 的
// 固定示例键之一，但没有给出它的取值集合或格式规则（这是 SPEC 自身的缺口，
// 类似机器面契约文档「约定 5」明确记录的那种缺口）。保守处理：既不能放行
// 任意字符串（那等于没有校验），也不能编造一个未经确认的枚举列表——因此
// 复用主机名校验器的字符集规则（字母数字与连字符，不以连字符首尾，长度
// 上限 63）：这是本表单键集合里最严格、最不可能意外拒绝合法业务角色名
// （如 "web-server"、"db-primary"）的规则，同时仍然彻底排除任何 shell
// 元字符。若将来 SPEC 或 M6 给出 role 的正式枚举表，应改为对照该表校验，
// 并在 contractGaps 中已记录此处的临时决定。
func roleValidator(value string) error {
	return guard.ValidateHostname(value)
}

// knownParams 是键名到校验器的显式白名单表（计划任务 2 设计要点：
// "键名到校验器的映射是一张显式的表，未知键一律拒绝"）。
//
// 收录依据：SPEC §2.4、§8.3 明确列出的 form_data 固定字段
// （hostname、biz_ip、role）与 §5.3 kickstart 示例中出现的
// LOBER_BIZ_IP/DISK_PRIMARY 对应的原始表单键。biz_gw 与 biz_mask 未在
// SPEC 的 form_data 示例中出现（它们作为 LOBER_BIZ_GW/LOBER_BIZ_MASK
// 出现在 fact 命名空间里，是否也是表单直填键或纯粹的 provider 决策产物，
// SPEC 没有说明）——本表暂不收录，属于 contractGaps 记录的缺口之一；
// 一旦 M6/M7 确认这两个键会出现在 ResolvedParams 里，只需在此表新增
// 两行即可，不需要改动本文件的其它逻辑。
var knownParams = map[string]paramValidator{
	"hostname": hostnameValidator,
	"biz_ip":   ipValidator,
	"role":     roleValidator,
}

// InlineParams 把 params 按 knownParams 白名单逐一校验并渲染为
// shell 变量赋值文本（SPEC §5.3），每行一个 `KEY='value'\n`。
//
// 未知键（不在 knownParams 表中的键名）一律拒绝并整体返回错误——不在表里
// 的键说明有人加了新表单字段却没想过它的合法取值范围，正是该拦住的时刻
// （计划任务 2 设计要点）。任一值未通过对应校验器同样整体返回错误、不渲染
// 任何内容（guard 包"走拒绝路径，不走转义路径"的原则在这里延续）。
//
// 变量名按键名的大写形式加 shell 惯例前缀不做处理——键本身就是最终变量名
// （调用方传入的键应已是 LOBER_ 风格或渲染器约定的大写形式，本函数不做
// 大小写转换，以保持"传入什么键就输出什么键"的可预测性）。
//
// 输出顺序按键名排序，保证同一输入两次渲染结果逐字节相同（golden file 与
// 幂等渲染的前提，计划任务 2 设计要点）。
func InlineParams(params map[string]string) (string, error) {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		validator, ok := knownParams[key]
		if !ok {
			return "", fmt.Errorf("render: 表单键 %q 不在已知校验器白名单中"+
				"——新增表单字段必须先在 knownParams 中声明对应的类型校验器，"+
				"否则该值会未经校验直接进入以 root 身份执行的 kickstart 脚本", key)
		}
		value := params[key]
		if err := validator(value); err != nil {
			return "", fmt.Errorf("render: 表单键 %q 的值未通过校验: %w", key, err)
		}
		b.WriteString(shellAssignment(key, value))
	}
	return b.String(), nil
}

// shellAssignment 渲染一行 `KEY='value'\n`，值用单引号包裹并转义其中的
// 单引号（即使调用方已经校验过值，两道防护的成本接近于零——计划任务 2
// 设计要点）。单引号转义采用 shell 惯用写法：先闭合引号、插入一个转义过的
// 单引号、再重新打开引号，写作四个字符一组：单引号、反斜杠、单引号、单引号。
func shellAssignment(key, value string) string {
	escaped := strings.ReplaceAll(value, "'", `'\''`)
	return key + "='" + escaped + "'\n"
}

// InjectFacts 无条件注入 model.FactKeysV1 的全部键（SPEC §5.4："服务端无
// 条件注入一大包事实，不管 hook 用不用"）。
//
// facts 中缺失的键渲染为空值而不是跳过（计划任务 2 设计要点）：hook 里
// `$LOBER_RACK` 未定义与为空是两种不同的调试体验——未定义时 `set -u` 会
// 让脚本直接退出且错误信息指向 hook 自己，而为空只是条件判断走了另一支，
// 后者好查得多。
//
// 输出顺序固定为 model.FactKeysV1() 的声明顺序（该函数本身返回稳定顺序的
// 切片），不按字母排序——facts 表按语义分组排列（见 model/facts.go 的分组
// 注释），保持这个分组对阅读渲染输出的人更友好。
//
// fact 的值不经过 guard 校验器：facts 命名空间的值来自服务端自己的决策
// 与 discovery 采集结果（§5.4"宁可多注入，也别让 hook 去发明自己的取数
// 方式"），不是用户可自由填写的表单值，不属于 SPEC §5.6 点名的注入风险
// 对象。但仍然复用 shellAssignment 做单引号转义，防止某个 fact 值恰好
// 含单引号时打断脚本语法。
func InjectFacts(facts map[string]string) string {
	var b strings.Builder
	for _, key := range model.FactKeysV1() {
		value := facts[key] // 缺失时 map 零值即空字符串，天然满足"缺值渲染为空"
		b.WriteString(shellAssignment(key, value))
	}
	return b.String()
}
