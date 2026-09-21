package main

import (
	"context"
	"errors"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// stubTaskParamsResolver 是 contract.TaskParamsResolver 的可编程测试
// 替身，模拟 internal/provider/resolve.go 的真实产出形态（fact 命名空间
// 键 + 两个 bookkeeping 键），不需要搭出真实的 provider 依赖链。
type stubTaskParamsResolver struct {
	resolved map[string]string
	err      error
}

func (s stubTaskParamsResolver) Resolve(ctx context.Context, formData map[string]string, facts map[string]string) (map[string]string, error) {
	return s.resolved, s.err
}

// TestResolvedParamsKeyFixup_TranslatesFactKeysToInlineParamsWhitelist 是
// 本任务在实测 e2e 阶段 8（取应答文件）时发现的真实契约裂缝的回归测试：
// internal/provider/resolve.go 产出的键名体系（LOBER_HOSTNAME、
// LOBER_BIZ_IP 等 fact 命名空间键，外加 template_id/template_version 两个
// bookkeeping 键）与 internal/render/params.go 的 InlineParams 白名单
// （裸键名 hostname/biz_ip/role）完全不对齐——不修复的后果是任何走真实
// 建任务路径产出的任务，第一次请求应答文件就会 500（"表单键 ... 不在
// 已知校验器白名单中"），这在 e2e 脚本里 100% 复现，不是理论上的边界
// 情形。
func TestResolvedParamsKeyFixup_TranslatesFactKeysToInlineParamsWhitelist(t *testing.T) {
	stub := stubTaskParamsResolver{
		resolved: map[string]string{
			model.FactHostname: "e2e-host-01",
			model.FactBizIP:    "10.0.0.5",
			model.FactBizGW:    "10.0.0.1",
			model.FactBizMask:  "255.255.255.0",
			"template_id":      "tpl-1",
			"template_version": "1",
		},
	}
	fixup := resolvedParamsKeyFixup{inner: stub}

	got, err := fixup.Resolve(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}

	want := map[string]string{
		"hostname": "e2e-host-01",
		"biz_ip":   "10.0.0.5",
	}
	if len(got) != len(want) {
		t.Fatalf("结果键数量 = %d, want %d，got=%v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("键 %q = %q, want %q", k, got[k], v)
		}
	}

	// 确认 template_id/template_version/biz_gw/biz_mask 这四个
	// InlineParams 白名单认不出的键确实被剔除——留下任何一个都会让
	// render/params.go 的 InlineParams 整体报错拒绝渲染。
	for _, leftoverKey := range []string{"template_id", "template_version", model.FactBizGW, model.FactBizMask} {
		if _, ok := got[leftoverKey]; ok {
			t.Errorf("键 %q 应被剔除，但仍出现在结果中: %v", leftoverKey, got)
		}
	}
}

// TestResolvedParamsKeyFixup_EmptyValueSkipped 断言值为空字符串的键不会
// 被翻译进结果——render/params.go 的白名单校验器对空字符串可能因具体
// 校验规则不同而行为不一致（如 hostnameValidator 可能拒绝空串），保留
// 一个空值键不如干脆不写这个键，效果对渲染器而言等价（未提供该参数），
// 但不会引入一条"键存在但值可能不合法"的额外失败模式。
func TestResolvedParamsKeyFixup_EmptyValueSkipped(t *testing.T) {
	stub := stubTaskParamsResolver{
		resolved: map[string]string{
			model.FactHostname: "",
			model.FactBizIP:    "10.0.0.5",
		},
	}
	fixup := resolvedParamsKeyFixup{inner: stub}

	got, err := fixup.Resolve(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if _, ok := got["hostname"]; ok {
		t.Errorf("空字符串的 hostname 不应出现在结果中，got=%v", got)
	}
	if got["biz_ip"] != "10.0.0.5" {
		t.Errorf("biz_ip = %q, want 10.0.0.5", got["biz_ip"])
	}
}

// TestResolvedParamsKeyFixup_PassesThroughError 断言底层 Resolve 本身
// 返回错误（如 IPAM 分配失败）时原样透传，不做任何改写——这条修复只
// 处理成功返回值的键名翻译，不应该影响错误路径的语义（§7.4 的阻塞
// 语义必须原样保留：失败即建任务失败）。
func TestResolvedParamsKeyFixup_PassesThroughError(t *testing.T) {
	wantErr := errors.New("boom: 模拟 IPAM 分配失败")
	stub := stubTaskParamsResolver{err: wantErr}
	fixup := resolvedParamsKeyFixup{inner: stub}

	_, err := fixup.Resolve(context.Background(), nil, nil)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want 原样透传 %v", err, wantErr)
	}
}
