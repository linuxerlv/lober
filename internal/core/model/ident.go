package model

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	snMaxLen      = 64 // 字节数；合法的 SN 只含 ASCII，等于字符数
	nameMaxLen    = 64 // 字节数；合法的名称只含 ASCII，等于字符数
	identMaxLen   = 32
	idMaxLen      = 64 // 字节数；合法的 ID 只含 ASCII，等于字符数
	labelMaxRunes = 64 // 按 rune 计，一个汉字算一个
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

// NormalizeName 规范化模板、hook 的名称（C12）：去掉首尾空白后，必须是 1–64 个可打印 ASCII
// 字符（0x21–0x7E），即英文字母、数字和美式键盘上可见的 32 个符号（T5-3）。中间的空格、中文、
// 全角字符和其他非 ASCII 字符一律拒收：标准库没有 Unicode 规范化（NFC、NFKC），é 的合成与分解
// 两种写法、全角字母、变体选择符这类字符看着一样，key 却不同，会绕过第 2 层按 key 建的唯一约束。
// 只收 ASCII，看着一样的名字就是同一个串。以后要放宽（比如允许单个空格），只改这里和它的测试。
//
// key 是 display 的 ASCII 小写，第 2 层按它做唯一约束，所以「Rocky-Base」与「rocky-base」算同名。
//
// 返回的 error 是 Problem，代码为 name_invalid。Subject 留空：名称属于哪个对象
// 只有调用方知道，由调用方填上。
//
// 保留前缀不在这里拦：用户的名字和内置内容的名字都走这条规则，前缀对两边的要求正好相反（用户不能用，内置的必须带），
// 由调用方拿 key 问 NameReserved。
func NormalizeName(raw string) (display, key string, err error) {
	display = strings.TrimSpace(raw)
	if reason := nameInvalidReason(display); reason != "" {
		return "", "", NewProblem("name_invalid", Subject{}, reason)
	}
	return display, strings.ToLower(display), nil // 走到这里只剩 ASCII，ToLower 只会动 ASCII 字母
}

// ReservedNamePrefix 是只给内置内容用的名字前缀（spec C12）：用户新建、派生和改名时用了它就拒绝（name_reserved），
// 内置内容的名字反过来必须带它。它和正文里系统部件的命名空间 {{lober:…}} 是同一个词，用户一看就知道是系统给的。
// 写成规范化之后的 key 的样子（小写），NameReserved 拿它直接与 key 比。
const ReservedNamePrefix = "lober:"

// NameReserved 判断规范化后的 key（NormalizeName 返回的第二个值）是不是用了保留前缀。比较按 key（小写）做，所以
// LOBER:x 也算；lober-base、lober 这类只是长得像的不算。判定只有这一份（spec §4.2）：仓储的用户入口与内置装载、
// 第 5 层的预检都问它。
func NameReserved(key string) bool {
	return strings.HasPrefix(key, ReservedNamePrefix)
}

// nameInvalidReason 返回名称不合法的原因，取 codes.go 登记的 nameReason* 之一；合法时返回空串。
// 逐个字符查，第一个不合法的字符决定原因；先查字符再查长度，理由同 snInvalidReason。非法的
// UTF-8 字节按 U+FFFD 读出，归入非 ASCII 字符。
func nameInvalidReason(name string) string {
	if name == "" {
		return nameReasonEmpty
	}
	for _, r := range name {
		switch {
		case unicode.IsSpace(r):
			return nameReasonSpace
		case r < 0x21 || r > 0x7e:
			return nameReasonChar
		}
	}
	if len(name) > nameMaxLen {
		return nameReasonTooLong
	}
	return ""
}

// StorableText 判断自由文本能不能原样存下：合法的 UTF-8，且不含 NUL（T13-10）。PostgreSQL 的
// text 与 jsonb 都不收 NUL；encoding/json 又会把非法字节换成 U+FFFD，存进去再取出来就不是原来
// 的字节，按原字节算的摘要也对不上（C35）。NUL 以外的控制字符照存。空串返回 true：空草稿也能存。
func StorableText(s string) bool {
	return utf8.ValidString(s) && strings.IndexByte(s, 0) < 0
}

// ValidLabel 判断参数、选项、状态这类给人看的显示名，可以用中文：存得下（StorableText），
// 1–64 个字（按 rune 计），首尾不是空白，不含控制字符、格式字符、变体选择符和其他默认不显示的
// 字符（Cc、Cf、Variation_Selector、Other_Default_Ignorable_Code_Point）。这些字符看不见，却能让
// 两个看着一样的显示名不同，或在界面上打乱显示。中间的空格照收。空串返回 false：显示名能不能
// 留空由调用方判断。
func ValidLabel(s string) bool {
	if s == "" || !StorableText(s) || utf8.RuneCountInString(s) > labelMaxRunes {
		return false
	}
	first, _ := utf8.DecodeRuneInString(s)
	last, _ := utf8.DecodeLastRuneInString(s)
	return !unicode.IsSpace(first) && !unicode.IsSpace(last) && !strings.ContainsFunc(s, hiddenInLabel)
}

func hiddenInLabel(r rune) bool {
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}

// ValidID 判断 ISO、hook、模板与任务的 ID：^[a-z0-9][a-z0-9-]{0,63}$。仓储用 store.NewID 生成的 26 个
// 字符的小写 base32，和内置内容自带的 builtin-kickstart 这类固定 ID 都合它；仓储收到的 ID 先过它。
//
// 合它的都是安全词（IsSafeWord）：字符集是安全词的子集，长度也在安全词的上限之内，所以任务 ID 经
// {{sys:task}} 原样代入正文，不会改变结构（L-13b）。只收小写，ID 就没有只差大小写的两种写法；不收 @，
// 它是「ID@版本」里的分隔符；不收 . 与 /，放进路径时它们会改变层次；首字符不能是 -，否则放进命令行
// 会被当成选项。
func ValidID(s string) bool {
	if s == "" || len(s) > idMaxLen || s[0] == '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
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
