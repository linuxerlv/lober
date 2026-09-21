// 本文件实现模板解析器的 MVP 形态：规则表。对应实现计划任务 5，依据
// SPEC §7.1、§7.6。
//
// 契约缺口（详见本文件末尾的说明）：计划要求「规则从配置的文件路径读」，
// 但 M1 的 internal/config.Provider 结构体没有为 resolver 规则表预留文件
// 路径字段（Resolver 字段本身是选择哪个内置实现的字符串，不是路径）。
// 本文件不在 config 包里加字段（契约只读），转而提供 LoadRulesYAML 与
// NewTemplateResolver 两段式构造：工厂（factory.go）目前用「空规则」调用
// NewTemplateResolver，等价于计划里「路径为空时用兜底规则」这一分支；
// 一旦 config 补上路径字段，工厂只需改一行调 LoadRulesYAML(path) 即可接上，
// 不需要改本文件任何签名。
package provider

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/lober/lober/internal/core/contract"
)

// ResolverRule 是规则表的一行：按 os + version + vendor + product 匹配，
// 命中则解析为 TemplateID/TemplateVersion（任务 5、SPEC §7.6）。
//
// 四个匹配字段均为空字符串表示「该维度不限」（用于构造兜底的全匹配规则）；
// 非空字段要求与 HWFacts 对应字段完全相等（不是子串匹配——型号字符串的
// 子串匹配是 by-model 磁盘策略的行为，规则表匹配的是「这台机器该装哪个
// 模板」，运维在规则表里填的应该是精确值，模糊匹配在这里只会增加意外
// 命中的风险）。
type ResolverRule struct {
	OSFamily string
	Version  string
	Vendor   string
	Product  string

	TemplateID      string
	TemplateVersion int
}

// matches 判定该规则是否命中给定的硬件事实。四个维度里非空的必须精确相等，
// 空维度视为通配。
func (r ResolverRule) matches(facts contract.HWFacts) bool {
	if r.OSFamily != "" && r.OSFamily != facts.OSFamily {
		return false
	}
	if r.Vendor != "" && r.Vendor != facts.Vendor {
		return false
	}
	if r.Product != "" && r.Product != facts.Product {
		return false
	}
	// Version 不在 contract.HWFacts 里（HWFacts 只有 OSFamily/Vendor/
	// Product/Arch 四个字段），版本维度按 SPEC §7.6「os + version + vendor +
	// product」的描述理应存在，但 M1 的 HWFacts 没有暴露它——这是与前面
	// 「配置文件路径」同一类的契约缺口。规则表结构体仍保留 Version 字段
	// （运维填写规则时可能已经在用这个维度做区分，删掉字段是破坏性变更），
	// 但匹配逻辑目前不对它做判断，等 HWFacts 补上 Version 字段后，这里加
	// 一行判断即可，不需要改规则表的数据结构或调用方签名。
	return true
}

// templateResolver 是规则表驱动的 TemplateResolver 实现（SPEC §7.6：MVP
// 形态是规则表，按 os + version + vendor + product 匹配，自上而下第一条
// 命中生效）。
//
// 引入脚本引擎的信号（写进注释，任务 5 明确要求）：规则表开始长出嵌套
// 条件，或运维反复要求「这批机器特殊处理」——那时应该考虑 Starlark
// （SPEC §7.2），而不是在这个规则表结构体上继续叠加字段。
type templateResolver struct {
	rules []ResolverRule
}

// defaultFallbackTemplateID 是兜底规则解析到的模板 ID。真实部署应通过非空
// 规则表覆盖这个兜底——它只在配置显式给了空规则表时生效（任务 5：路径为空
// 时用一条「全匹配到默认模板」的兜底规则）。
const defaultFallbackTemplateID = "default"

// NewTemplateResolver 用给定规则表构造一个 TemplateResolver。rules 为空时
// 使用一条全匹配到默认模板的兜底规则（任务 5）。
func NewTemplateResolver(rules []ResolverRule) contract.TemplateResolver {
	if len(rules) == 0 {
		rules = []ResolverRule{
			{TemplateID: defaultFallbackTemplateID, TemplateVersion: 1},
		}
	}
	out := make([]ResolverRule, len(rules))
	copy(out, rules)
	return &templateResolver{rules: out}
}

// Resolve 自上而下遍历规则表，返回第一条命中规则的模板引用（SPEC §7.6）。
// 一条都不匹配时返回明确错误——不回落到默认模板，那会让机器装上不该装的
// 系统（任务 5）。
//
// form 参数目前未参与匹配：SPEC §7.6 描述的匹配维度是 os + version +
// vendor + product，均来自 HWFacts；表单数据（如用户在建任务界面填的
// 备注）不是这四个维度的一部分。接口签名保留 form 是为了与
// contract.TemplateResolver 的方法签名一致（该签名由 M1 定义，本模块不能
// 修改），且为将来可能出现的「按表单选择维度」的规则扩展预留调用点。
func (t *templateResolver) Resolve(ctx context.Context, form map[string]string, facts contract.HWFacts) (contract.TemplateRef, error) {
	for _, rule := range t.rules {
		if rule.matches(facts) {
			return contract.TemplateRef{
				TemplateID:      rule.TemplateID,
				TemplateVersion: rule.TemplateVersion,
			}, nil
		}
	}
	return contract.TemplateRef{}, fmt.Errorf("provider: 规则表中没有任何规则命中 facts=%+v"+
		"——不回落到默认模板，那会让机器装上不该装的系统（SPEC §7.6、计划任务 5）", facts)
}

// resolverRulesYAML 是 LoadRulesYAML 反序列化 YAML 文件时使用的结构，字段名
// 采用 snake_case 以匹配运维手写 YAML 时的习惯（与 config 包的 yaml tag
// 风格一致）。
type resolverRulesYAML struct {
	Rules []struct {
		OSFamily        string `yaml:"os_family"`
		Version         string `yaml:"version"`
		Vendor          string `yaml:"vendor"`
		Product         string `yaml:"product"`
		TemplateID      string `yaml:"template_id"`
		TemplateVersion int    `yaml:"template_version"`
	} `yaml:"rules"`
}

// LoadRulesYAML 把 YAML 文本解析为 []ResolverRule，供将来 config 补上规则表
// 文件路径字段后，工厂直接调用本函数加载。当前没有调用点在生产路径上使用
// 它（factory.go 目前总是传空规则给 NewTemplateResolver），保留是为了不让
// config 补字段这件事牵连本包的公开 API 形状（见文件顶部契约缺口说明）。
func LoadRulesYAML(data []byte) ([]ResolverRule, error) {
	var parsed resolverRulesYAML
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("provider: 解析 resolver 规则表 YAML 失败: %w", err)
	}

	out := make([]ResolverRule, 0, len(parsed.Rules))
	for i, r := range parsed.Rules {
		if r.TemplateID == "" {
			return nil, fmt.Errorf("provider: resolver 规则表第 %d 条缺少 template_id", i)
		}
		out = append(out, ResolverRule{
			OSFamily:        r.OSFamily,
			Version:         r.Version,
			Vendor:          r.Vendor,
			Product:         r.Product,
			TemplateID:      r.TemplateID,
			TemplateVersion: r.TemplateVersion,
		})
	}
	return out, nil
}

// 编译期断言：templateResolver 必须满足 contract.TemplateResolver。
var _ contract.TemplateResolver = (*templateResolver)(nil)
