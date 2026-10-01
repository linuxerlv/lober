package model

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	snMaxLen     = 64 // 字节数；合法的 SN 只含 ASCII，等于字符数
	nameMaxRunes = 64 // 按 rune 计，一个汉字算一个
	identMaxLen  = 32
)

// snPlaceholders 是主板与机箱出厂时常见的占位 SN，以规范化后的形式登记。
// 它们在一批机器上完全相同，收下来就会让不同的机器撞成同一个身份（C6）。
var snPlaceholders = map[string]bool{
	"TO BE FILLED BY O.E.M.": true,
	"DEFAULT STRING":         true,
	"SYSTEM SERIAL NUMBER":   true,
	"CHASSIS SERIAL NUMBER":  true,
	"SERIAL NUMBER":          true,
	"NOT SPECIFIED":          true,
	"NOT APPLICABLE":         true,
	"N/A":                    true,
	"NONE":                   true,
	"INVALID":                true,
	"OEM":                    true,
	"0123456789":             true,
	"123456789":              true,
}

// NormalizeSN 规范化主机的 SN（C6）：去掉首尾空白，校验字符与长度，把 ASCII 字母
// 转成大写，再比对占位值。必须先校验再转大写：ſ（U+017F）、ı（U+0131）转大写后是
// S、I，先转的话「ſn-1」会被收成另一台机器的「SN-1」。
// 返回的 error 是 Problem，代码为 sn_invalid 或 sn_placeholder。
func NormalizeSN(raw string) (string, error) {
	sn := strings.TrimSpace(raw)
	if reason := snInvalidReason(sn); reason != "" {
		return "", NewProblem("sn_invalid", Subject{Kind: "host"}, reason)
	}
	sn = strings.ToUpper(sn) // 走到这里只剩可打印 ASCII，ToUpper 只会动 ASCII 字母
	if snPlaceholders[sn] || repeatsOneByte(sn) {
		return "", NewProblem("sn_placeholder", Subject{Kind: "host", ID: sn}, sn)
	}
	return sn, nil
}

// snInvalidReason 返回 SN 不合法的原因，取 codes.go 登记的 snReason* 之一；合法时返回空串。
// 先查字符再查长度：含非 ASCII 字符时，字节数超长的原因会误导人。
// 连续空格不压缩而是拒收：SN 是身份，不替操作员猜哪种写法才对。
func snInvalidReason(sn string) string {
	if sn == "" {
		return snReasonEmpty
	}
	for i := 0; i < len(sn); i++ {
		switch c := sn[i]; {
		case c == ' ' && sn[i-1] == ' ': // 首尾空白已去掉，所以空格前一定还有字符
			return snReasonSpaces
		case c != ' ' && (c < 0x21 || c > 0x7e):
			return snReasonChar
		}
	}
	if len(sn) > snMaxLen {
		return snReasonTooLong
	}
	return ""
}

// repeatsOneByte 认出 00000000、XXXXXXXX 这类由同一个字符组成的占位值。只有一个
// 字符的也算：VirtualBox 的默认 SN 就是 0，收下来一批虚拟机就撞成同一个身份。
func repeatsOneByte(sn string) bool {
	return sn != "" && strings.Count(sn, sn[:1]) == len(sn)
}

// NormalizeName 规范化给人看的名称（C12）：去掉首尾空白，内部连续空白压成一个空格。
// key 是 display 的小写形式，第 2 层按它做唯一约束，所以「Linux 基线」与
// 「linux 基线」算同名。
//
// 返回的 error 是 Problem，代码为 name_invalid。Subject 留空：名称属于哪个对象
// 只有调用方知道，由调用方填上。
func NormalizeName(raw string) (display, key string, err error) {
	if !utf8.ValidString(raw) {
		return "", "", nameInvalid(nameReasonNotUTF8)
	}
	display = strings.Join(strings.Fields(raw), " ")
	switch {
	case display == "":
		return "", "", nameInvalid(nameReasonEmpty)
	case strings.ContainsFunc(display, invisibleRune):
		return "", "", nameInvalid(nameReasonInvisible)
	case utf8.RuneCountInString(display) > nameMaxRunes:
		return "", "", nameInvalid(nameReasonTooLong)
	}
	return display, strings.ToLower(display), nil
}

// nameInvalid 的 reason 取 codes.go 登记的 nameReason* 之一。
func nameInvalid(reason string) error {
	return NewProblem("name_invalid", Subject{}, reason)
}

// invisibleRune 除了控制字符，还拦零宽空格、BOM 这类格式字符：它们看不见，
// 却会让两个看着一模一样的名字得到不同的 key，绕过同名约束。
// 空白在这之前已被压成空格，所以换行、制表符不会走到这里。
func invisibleRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// ValidIdent 判断参数名、主机属性键这类标识符：^[a-z][a-z0-9_]{0,31}$。
// 它们会进环境变量名、字段键和数据库，所以只收小写 ASCII。
func ValidIdent(s string) bool {
	if len(s) == 0 || len(s) > identMaxLen || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
