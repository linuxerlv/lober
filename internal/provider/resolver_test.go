package provider

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

func TestResolver_SingleRuleMatch(t *testing.T) {
	r := NewTemplateResolver([]ResolverRule{
		{OSFamily: "rhel", TemplateID: "tpl-rhel", TemplateVersion: 3},
	})
	ref, err := r.Resolve(context.Background(), nil, contract.HWFacts{OSFamily: "rhel"})
	if err != nil {
		t.Fatalf("单条规则命中应成功: %v", err)
	}
	if ref.TemplateID != "tpl-rhel" || ref.TemplateVersion != 3 {
		t.Fatalf("got %+v, want TemplateID=tpl-rhel TemplateVersion=3", ref)
	}
}

func TestResolver_FirstMatchWinsWhenMultipleMatch(t *testing.T) {
	// 两条规则都能匹配 rhel：第一条（全字段通配的兜底）排在前面，
	// 第二条（明确指定 rhel 的规则）排在后面。自上而下第一条命中生效，
	// 所以应该拿到第一条的结果，即便第二条「看起来更精确」。
	r := NewTemplateResolver([]ResolverRule{
		{TemplateID: "tpl-first", TemplateVersion: 1},
		{OSFamily: "rhel", TemplateID: "tpl-second", TemplateVersion: 2},
	})
	ref, err := r.Resolve(context.Background(), nil, contract.HWFacts{OSFamily: "rhel"})
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	if ref.TemplateID != "tpl-first" {
		t.Fatalf("多条规则都命中时必须取第一条，got %s，want tpl-first——"+
			"若取到 tpl-second 说明「自上而下第一条命中生效」被破坏，"+
			"运维调整规则顺序将不会产生预期效果", ref.TemplateID)
	}
}

func TestResolver_ZeroMatchReturnsError(t *testing.T) {
	r := NewTemplateResolver([]ResolverRule{
		{OSFamily: "windows", TemplateID: "tpl-win", TemplateVersion: 1},
	})
	_, err := r.Resolve(context.Background(), nil, contract.HWFacts{OSFamily: "rhel"})
	if err == nil {
		t.Fatal("一条都不匹配时必须返回错误，不能回落到某个默认模板——" +
			"那会让机器装上不该装的系统（SPEC §7.6）")
	}
}

func TestResolver_EmptyRulesUsesFallback(t *testing.T) {
	r := NewTemplateResolver(nil)
	ref, err := r.Resolve(context.Background(), nil, contract.HWFacts{OSFamily: "anything", Vendor: "anything"})
	if err != nil {
		t.Fatalf("空配置应使用兜底规则而不是报错: %v", err)
	}
	if ref.TemplateID == "" {
		t.Fatal("兜底规则解析出的 TemplateID 不应为空")
	}
}

func TestResolver_MatchesOnVendorAndProduct(t *testing.T) {
	r := NewTemplateResolver([]ResolverRule{
		{Vendor: "Dell", Product: "R740", TemplateID: "tpl-dell-r740", TemplateVersion: 1},
	})
	ref, err := r.Resolve(context.Background(), nil, contract.HWFacts{Vendor: "Dell", Product: "R740"})
	if err != nil {
		t.Fatalf("vendor+product 精确匹配应成功: %v", err)
	}
	if ref.TemplateID != "tpl-dell-r740" {
		t.Fatalf("got %s, want tpl-dell-r740", ref.TemplateID)
	}

	_, err = r.Resolve(context.Background(), nil, contract.HWFacts{Vendor: "Dell", Product: "R750"})
	if err == nil {
		t.Fatal("product 不匹配时不应命中该规则")
	}
}

func TestLoadRulesYAML_ParsesRulesInOrder(t *testing.T) {
	yamlText := `
rules:
  - os_family: rhel
    template_id: tpl-rhel
    template_version: 2
  - template_id: tpl-default
    template_version: 1
`
	rules, err := LoadRulesYAML([]byte(yamlText))
	if err != nil {
		t.Fatalf("解析合法 YAML 不应出错: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("应解析出 2 条规则，got %d", len(rules))
	}
	if rules[0].OSFamily != "rhel" || rules[0].TemplateID != "tpl-rhel" {
		t.Fatalf("第一条规则解析不对: %+v", rules[0])
	}
	if rules[1].TemplateID != "tpl-default" {
		t.Fatalf("第二条规则解析不对: %+v", rules[1])
	}
}

func TestLoadRulesYAML_MissingTemplateIDReturnsError(t *testing.T) {
	yamlText := `
rules:
  - os_family: rhel
`
	_, err := LoadRulesYAML([]byte(yamlText))
	if err == nil {
		t.Fatal("缺少 template_id 的规则应在加载时报错，不能留一个空 ID 混进规则表")
	}
}
