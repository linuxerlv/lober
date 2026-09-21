// 本文件测试参数内联与 fact 注入。对应实现计划任务 2，依据 SPEC §5.3、§5.4。
//
// 测试函数名统一以 TestParams 为前缀，对齐计划任务 2 给出的验证命令
// `go test ./internal/render/ -run TestParams -v`。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestParams_InjectFacts_AllKeysPresent 锁住"全部 fact 键都出现在输出里，
// 缺值的赋成空"（计划任务 2 测试要求第一条）。
func TestParams_InjectFacts_AllKeysPresent(t *testing.T) {
	// 只提供部分 fact 值，其余键应仍出现且赋空值。
	facts := map[string]string{
		model.FactTaskID: "42",
		model.FactSN:     "ABC123",
	}
	out := InjectFacts(facts)

	for _, key := range model.FactKeysV1() {
		if !strings.Contains(out, key+"=") {
			t.Errorf("InjectFacts 输出缺少键 %s —— SPEC §5.4 要求无条件注入全部 "+
				"fact 键，不管 hook 用不用；缺失会让某个 hook 在用到它时得到"+
				"「未定义变量」而不是「空值」的完全不同调试体验", key)
		}
	}

	// 缺失的键（例如 LOBER_RACK 未提供）必须渲染为空值，不能整行跳过。
	if !strings.Contains(out, model.FactRack+"=''\n") {
		t.Errorf("InjectFacts 输出中 %s 应渲染为空值赋值，got:\n%s", model.FactRack, out)
	}

	// 提供的值应正确出现。
	if !strings.Contains(out, model.FactTaskID+"='42'\n") {
		t.Errorf("InjectFacts 输出中 %s 应为 '42'，got:\n%s", model.FactTaskID, out)
	}
}

// TestParams_InjectFacts_StableOrder 锁住"输出顺序稳定（渲染两次结果相同）"
// （计划任务 2 测试要求第四条）。
func TestParams_InjectFacts_StableOrder(t *testing.T) {
	facts := map[string]string{model.FactHostname: "web-01"}
	first := InjectFacts(facts)
	second := InjectFacts(facts)
	if first != second {
		t.Errorf("InjectFacts 两次渲染结果不同，golden file 测试将无法稳定通过\n"+
			"第一次:\n%s\n第二次:\n%s", first, second)
	}
}

// TestParams_InlineParams_KnownKeysPass 锁住"每类参数走了对应的校验器"——
// 用合法值逐类验证（计划任务 2 测试要求第三条的正向部分）。
func TestParams_InlineParams_KnownKeysPass(t *testing.T) {
	params := map[string]string{
		"hostname": "web-01",
		"biz_ip":   "10.20.0.15",
		"role":     "web-server",
	}
	out, err := InlineParams(params)
	if err != nil {
		t.Fatalf("InlineParams(%v) 返回错误 %v，want nil —— 三个键都是合法值", params, err)
	}
	for k, v := range params {
		want := k + "='" + v + "'\n"
		if !strings.Contains(out, want) {
			t.Errorf("InlineParams 输出应含 %q，got:\n%s", want, out)
		}
	}
}

// TestParams_InlineParams_UnknownKeyRejected 锁住"未知的表单键被拒绝"
// （计划任务 2 测试要求第二条）。
func TestParams_InlineParams_UnknownKeyRejected(t *testing.T) {
	params := map[string]string{
		"totally_unknown_field": "whatever",
	}
	_, err := InlineParams(params)
	if err == nil {
		t.Error("InlineParams 对未知键 totally_unknown_field 应返回错误，got nil —— " +
			"未在白名单声明校验器的键意味着没人想过它的合法取值范围，必须拒绝")
	}
}

// TestParams_InlineParams_InvalidIPRejected 用一个非法 IP 验证 IP 类字段
// 被拒绝（计划任务 2 测试要求第三条的反向部分："用一个非法 IP 验证 IP 类
// 字段被拒"）。
func TestParams_InlineParams_InvalidIPRejected(t *testing.T) {
	params := map[string]string{"biz_ip": "10.0.0.1; rm -rf /"}
	_, err := InlineParams(params)
	if err == nil {
		t.Error("InlineParams 对非法 biz_ip 值应返回错误，got nil —— " +
			"该值会被内联进以 root 身份执行的 kickstart 脚本")
	}
}

// TestParams_InlineParams_InvalidHostnameRejected 验证 hostname 字段走的是
// 主机名校验器而不是别的规则（例如误配成 IP 校验器会让所有主机名都被拒）。
func TestParams_InlineParams_InvalidHostnameRejected(t *testing.T) {
	params := map[string]string{"hostname": "web_01"} // 下划线非法
	_, err := InlineParams(params)
	if err == nil {
		t.Error("InlineParams 对含下划线的 hostname 值应返回错误，got nil")
	}
}

// TestParams_InlineParams_StableOrder 锁住"输出顺序稳定（渲染两次结果相同）"
// （计划任务 2 测试要求第四条），且顺序按键名排序。
func TestParams_InlineParams_StableOrder(t *testing.T) {
	params := map[string]string{
		"role":     "web-server",
		"hostname": "web-01",
		"biz_ip":   "10.20.0.15",
	}
	first, err1 := InlineParams(params)
	second, err2 := InlineParams(params)
	if err1 != nil || err2 != nil {
		t.Fatalf("InlineParams 返回意外错误: %v / %v", err1, err2)
	}
	if first != second {
		t.Errorf("InlineParams 两次渲染结果不同:\n第一次:\n%s\n第二次:\n%s", first, second)
	}

	// 按键名排序：biz_ip < hostname < role
	idxBizIP := strings.Index(first, "biz_ip=")
	idxHostname := strings.Index(first, "hostname=")
	idxRole := strings.Index(first, "role=")
	if !(idxBizIP < idxHostname && idxHostname < idxRole) {
		t.Errorf("InlineParams 输出未按键名排序，got:\n%s", first)
	}
}

// TestParams_InlineParams_EmptyMap 验证空参数表渲染出空字符串，不报错——
// 建任务时若没有任何额外表单字段（只有系统固定字段走 fact 注入），不应
// 视为错误。
func TestParams_InlineParams_EmptyMap(t *testing.T) {
	out, err := InlineParams(map[string]string{})
	if err != nil {
		t.Fatalf("InlineParams(空表) 返回错误 %v，want nil", err)
	}
	if out != "" {
		t.Errorf("InlineParams(空表) = %q, want 空字符串", out)
	}
}

// TestParams_ShellAssignment_EscapesSingleQuote 验证值中的单引号被转义，
// 即使该值已通过校验器（计划任务 2 设计要点："即使已经校验过，两道防护的
// 成本接近于零"）。guard.ValidateHostname 已经拒绝单引号，因此本测试直接
// 测试 shellAssignment 这个内部转义函数本身，覆盖"校验器之外还有转义"这道
// 独立防线。
func TestParams_ShellAssignment_EscapesSingleQuote(t *testing.T) {
	got := shellAssignment("X", "a'b")
	want := "X='a'\\''b'\n"
	if got != want {
		t.Errorf("shellAssignment 单引号转义 = %q, want %q", got, want)
	}
}
