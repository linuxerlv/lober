// 本文件测试按类型白名单校验的三个校验器：IP、主机名、整数范围。对应实现
// 计划任务 1，依据 SPEC §5.6："form_data 的值会进 ks 脚本，必须按类型白名单
// 校验（IP 格式、hostname 字符集、整数范围）——否则填入 `; rm -rf /` 会真的
// 执行"。
package guard

import (
	"strings"
	"testing"
)

// TestValidIP 锁住合法 IPv4 值通过校验（计划任务 1 "测试要锁住的行为" 第一条）。
func TestValidIP(t *testing.T) {
	cases := []string{
		"10.20.0.15",
		"192.168.1.1",
		"0.0.0.0",
		"255.255.255.255",
	}
	for _, v := range cases {
		if err := ValidateIP(v); err != nil {
			t.Errorf("ValidateIP(%q) 返回错误 %v，want nil —— 这是合法的 IPv4 值，"+
				"校验器过严会挡住正常运维填表", v, err)
		}
	}
}

// TestValidHostname 锁住合法主机名通过校验。
func TestValidHostname(t *testing.T) {
	cases := []string{
		"web-01",
		"a",
		"host123",
		"WEB-01", // 大写字母合法（RFC 1123 标签允许大小写，比较时不敏感）
		strings.Repeat("a", 63),
	}
	for _, v := range cases {
		if err := ValidateHostname(v); err != nil {
			t.Errorf("ValidateHostname(%q) 返回错误 %v，want nil —— 恰好 63 字符或"+
				"常见命名风格应当通过", v, err)
		}
	}
}

// TestValidIntRange 锁住区间内整数通过校验，包括闭区间端点。
func TestValidIntRange(t *testing.T) {
	cases := []struct {
		value    string
		min, max int
	}{
		{"5", 1, 10},
		{"1", 1, 10},  // 下端点
		{"10", 1, 10}, // 上端点
		{"0", 0, 0},
	}
	for _, c := range cases {
		if err := ValidateIntRange(c.value, c.min, c.max); err != nil {
			t.Errorf("ValidateIntRange(%q, %d, %d) 返回错误 %v，want nil —— "+
				"闭区间端点必须通过，否则调用方需要额外做 off-by-one 处理",
				c.value, c.min, c.max, err)
		}
	}
}

// TestIntRangeBoundary 单独锁住整数区间端点的边界行为（计划任务 1 "测试要
// 锁住的行为" 第三条："整数恰好在区间端点通过"）。
func TestIntRangeBoundary(t *testing.T) {
	if err := ValidateIntRange("0", 0, 100); err != nil {
		t.Errorf("下端点 0 应通过，got %v", err)
	}
	if err := ValidateIntRange("100", 0, 100); err != nil {
		t.Errorf("上端点 100 应通过，got %v", err)
	}
	if err := ValidateIntRange("-1", 0, 100); err == nil {
		t.Error("下端点外一位（-1）应被拒绝，got nil error")
	}
	if err := ValidateIntRange("101", 0, 100); err == nil {
		t.Error("上端点外一位（101）应被拒绝，got nil error")
	}
}

// TestHostnameBoundary 单独锁住主机名长度边界（计划任务 1 "测试要锁住的行为"
// 第三条："主机名恰好 63 字符通过、64 字符拒绝"）。
func TestHostnameBoundary(t *testing.T) {
	if err := ValidateHostname(strings.Repeat("a", 63)); err != nil {
		t.Errorf("63 字符主机名应通过（RFC 1123 单标签长度上限），got %v", err)
	}
	if err := ValidateHostname(strings.Repeat("a", 64)); err == nil {
		t.Error("64 字符主机名应被拒绝（超过 RFC 1123 单标签长度上限 63），got nil error")
	}
}

// maliciousInputs 是计划任务 1 要求"必须验证被拒绝的恶意输入"的七类样本，
// 逐类标注失败信息以便断言失败时能指出具体是哪一类没被拒绝。
type maliciousCase struct {
	label string
	value string
}

var maliciousInputs = []maliciousCase{
	{"分号命令分隔符", "10.0.0.1; rm -rf /"},
	{"反引号命令替换", "web`rm -rf /`01"},
	{"$(...) 命令替换", "web$(rm -rf /)01"},
	{"换行注入", "web-01\nrm -rf /"},
	{"kickstart 段落结束标记", "web-01\n%end\n%post"},
	{"kickstart 段落开始标记", "web-01\n%post\nrm -rf /"},
	{"看似合法但含尾部空格", "web-01 "},
}

// TestIPRejectsMaliciousInputs 断言 IP 校验器逐一拒绝七类恶意输入
// （计划任务 1："七类恶意输入逐一被拒绝，每类一个断言"）。
func TestIPRejectsMaliciousInputs(t *testing.T) {
	for _, c := range maliciousInputs {
		t.Run(c.label, func(t *testing.T) {
			if err := ValidateIP(c.value); err == nil {
				t.Errorf("ValidateIP(%q) 未拒绝%s类恶意输入 —— 该值会被内联进 kickstart "+
					"脚本并以 root 身份执行，放行等同于允许任意命令执行", c.value, c.label)
			}
		})
	}
}

// TestHostnameRejectsMaliciousInputs 断言主机名校验器逐一拒绝七类恶意输入。
func TestHostnameRejectsMaliciousInputs(t *testing.T) {
	for _, c := range maliciousInputs {
		t.Run(c.label, func(t *testing.T) {
			if err := ValidateHostname(c.value); err == nil {
				t.Errorf("ValidateHostname(%q) 未拒绝%s类恶意输入 —— 该值会被内联进 "+
					"kickstart 脚本并以 root 身份执行，放行等同于允许任意命令执行", c.value, c.label)
			}
		})
	}
}

// TestIntRangeRejectsMaliciousInputs 断言整数校验器逐一拒绝七类恶意输入
// （其中数值型的恶意输入天然会在 strconv.Atoi 阶段被拒，仍逐条断言以锁住
// 这个前提不会被将来的实现改动破坏）。
func TestIntRangeRejectsMaliciousInputs(t *testing.T) {
	for _, c := range maliciousInputs {
		t.Run(c.label, func(t *testing.T) {
			if err := ValidateIntRange(c.value, 0, 1000); err == nil {
				t.Errorf("ValidateIntRange(%q, 0, 1000) 未拒绝%s类恶意输入 —— 该值会被"+
					"内联进 kickstart 脚本并以 root 身份执行，放行等同于允许任意命令执行",
					c.value, c.label)
			}
		})
	}
}

// TestIPRejectsIPv6AndCIDR 锁住"解析为 IP 后收紧到 IPv4"这条规则：拒绝子网
// 掩码写法、IPv6、任何附加字符（计划任务 1 校验器规则表）。
func TestIPRejectsIPv6AndCIDR(t *testing.T) {
	cases := []string{
		"::1",
		"2001:db8::1",
		"10.0.0.1/24",
		"10.0.0.1:8080",
		"10.0.0",     // 不完整
		"10.0.0.1.1", // 多一段
		"999.0.0.1",  // 超范围段
	}
	for _, v := range cases {
		if err := ValidateIP(v); err == nil {
			t.Errorf("ValidateIP(%q) 应拒绝——不是纯粹的 IPv4 点分十进制写法", v)
		}
	}
}

// TestHostnameRejectsInvalidChars 锁住主机名校验器拒绝下划线、空格、点号
// （计划任务 1："拒绝下划线、空格、换行、点号"）与首尾连字符。
func TestHostnameRejectsInvalidChars(t *testing.T) {
	cases := []string{
		"web_01", // 下划线
		"web 01", // 空格
		"web.01", // 点号（FQDN 由主机名加域名拼出，点号会产生歧义）
		"-web01", // 连字符开头
		"web01-", // 连字符结尾
		"",       // 空串
	}
	for _, v := range cases {
		if err := ValidateHostname(v); err == nil {
			t.Errorf("ValidateHostname(%q) 应拒绝", v)
		}
	}
}

// TestIntRangeRejectsNonDecimal 锁住"解析为十进制整数"这条规则：天然拒绝
// 十六进制、小数、前后空格（计划任务 1 校验器规则表）。
func TestIntRangeRejectsNonDecimal(t *testing.T) {
	cases := []string{
		"0x10", // 十六进制
		"1.5",  // 小数
		" 5",   // 前置空格
		"5 ",   // 尾随空格
	}
	for _, v := range cases {
		if err := ValidateIntRange(v, 0, 100); err == nil {
			t.Errorf("ValidateIntRange(%q, 0, 100) 应拒绝——不是纯十进制整数写法", v)
		}
	}
}

// TestValidatorsReturnBoolNotSanitized 锁住设计要点："校验器不返回清洗后的
// 值，只返回是否合法"——本测试通过编译期签名检查体现：若签名一旦被改成
// 返回 (string, error)，本文件中所有 `err :=` 形式的调用都会编译失败。
// 这里额外用一次显式调用确认返回值只有 error 一种。
func TestValidatorsReturnBoolNotSanitized(t *testing.T) {
	var _ error = ValidateIP("10.0.0.1")
	var _ error = ValidateHostname("web-01")
	var _ error = ValidateIntRange("5", 0, 10)
}
