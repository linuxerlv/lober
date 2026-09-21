// Package guard 实现按类型白名单校验表单值，是渲染器的注入防护（对应实现
// 计划任务 1，依据 SPEC §5.6）。
//
// form_data 的值最终会被内联进 kickstart 脚本并以 root 身份执行，因此本包
// 只提供"拒绝"路径，不提供"清洗"路径：校验器不返回清洗后的值，只返回是否
// 合法。返回清洗值会让调用方产生"用哪个值"的选择，而选错就是一次 root 执行
// 漏洞。
//
// 独立成子包的理由：M6（控制台）也需要用它做服务端强校验（SPEC §12.7：
// 前端校验只是体验，不是防线）。子包让 M6 不必 import 整个渲染器。
package guard

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ValidateIP 校验 v 是否为合法的 IPv4 点分十进制地址。
//
// 解析为 net.IP 后收紧到 IPv4——这一步拒绝子网掩码写法、IPv6、CIDR 写法、
// 端口号拼接等任何附加字符。装机网是 IPv4 内网，放宽到 IPv6 没有收益，
// 只会扩大攻击面。
func ValidateIP(v string) error {
	ip := net.ParseIP(v)
	if ip == nil {
		return fmt.Errorf("guard: %q 不是合法的 IP 地址", v)
	}
	if ip.To4() == nil {
		return fmt.Errorf("guard: %q 不是 IPv4 地址（装机网只用 IPv4 内网）", v)
	}
	// net.ParseIP 对形如 "10.0.0.1 "（含尾部空格）或附带其它字符的输入会
	// 直接解析失败，已经在上面被拒绝；这里额外用 String() 往返比较，防御
	// net.ParseIP 未来版本变得更宽松（例如接受前导零 "010.0.0.1"）的情况——
	// 若字符串表示与输入不完全一致，说明输入里含有解析器容忍但语义上可疑
	// 的多余内容。
	if ip.String() != v {
		return fmt.Errorf("guard: %q 不是规范的 IPv4 点分十进制写法", v)
	}
	return nil
}

// hostnameMaxLen 是 RFC 1123 单标签长度上限。
const hostnameMaxLen = 63

// ValidateHostname 校验 v 是否为合法的 RFC 1123 单标签主机名：字母数字与
// 连字符，不以连字符开头或结尾，长度上限 63。
//
// 拒绝下划线、空格、换行、点号。点号被拒的理由：FQDN 由主机名加域名拼出，
// 让用户填点号会产生歧义（用户是想填子域还是把整个 FQDN 当主机名？两种
// 理解都可能，不如直接拒绝，逼用户填单标签）。
func ValidateHostname(v string) error {
	if v == "" {
		return fmt.Errorf("guard: 主机名不能为空")
	}
	if len(v) > hostnameMaxLen {
		return fmt.Errorf("guard: 主机名 %q 长度 %d 超过 RFC 1123 单标签上限 %d",
			v, len(v), hostnameMaxLen)
	}
	if strings.HasPrefix(v, "-") || strings.HasSuffix(v, "-") {
		return fmt.Errorf("guard: 主机名 %q 不能以连字符开头或结尾", v)
	}
	for _, c := range v {
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '-' {
			return fmt.Errorf("guard: 主机名 %q 含非法字符 %q"+
				"（只允许字母数字与连字符，拒绝下划线/空格/换行/点号）", v, c)
		}
	}
	return nil
}

// ValidateIntRange 校验 v 解析为十进制整数后是否落在闭区间 [min, max] 内。
//
// strconv.Atoi 天然拒绝十六进制、小数、前后空格——本函数不做额外的字符集
// 预处理，直接依赖标准库的严格解析行为。
func ValidateIntRange(v string, min, max int) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("guard: %q 不是合法的十进制整数: %w", v, err)
	}
	if n < min || n > max {
		return fmt.Errorf("guard: %d 不在允许区间 [%d, %d] 内", n, min, max)
	}
	return nil
}
