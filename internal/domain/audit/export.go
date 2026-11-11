package audit

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidRange 的零值表示不限定该端；时间不能为负，开始不能晚于非零结束。
func ValidRange(from, to int64) bool { return from >= 0 && to >= 0 && (to == 0 || from <= to) }

// SpreadsheetText 只观察首个有效字符，不删除原文；危险前缀前加一个制表符。
func SpreadsheetText(raw string) string {
	meaningful := strings.TrimLeftFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
	first, _ := utf8.DecodeRuneInString(meaningful)
	if strings.ContainsRune("=+-@＝＋－＠", first) || len(raw) > 0 && strings.ContainsRune("\t\r\n", rune(raw[0])) {
		return "\t" + raw
	}
	return raw
}
