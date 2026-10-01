package model

import (
	"errors"
	"strings"
	"testing"
)

// problemCode 取出规范化函数返回的 Problem 的代码；error 不是 Problem 时直接判失败。
func problemCode(t *testing.T, err error) Code {
	t.Helper()
	var p Problem
	if !errors.As(err, &p) {
		t.Fatalf("error 应能经 errors.As 取出 Problem，得到 %T：%v", err, err)
	}
	return p.Code
}

func TestNormalizeSN规范化(t *testing.T) {
	cases := map[string]string{
		"  abc-123\t":            "ABC-123",
		"abc 123":                "ABC 123", // 中间允许单个空格
		"Z9!~":                   "Z9!~",    // 0x21–0x7E 都是可打印字符
		"a1":                     "A1",      // 两个不同的字符就不算占位值
		strings.Repeat("ab", 32): strings.Repeat("AB", 32),
	}
	for raw, want := range cases {
		got, err := NormalizeSN(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeSN(%q) = %q, %v；应为 %q", raw, got, err, want)
		}
	}
}

func TestNormalizeSN拒收不合法(t *testing.T) {
	for _, raw := range []string{
		"", "   ", "\t\n",
		strings.Repeat("A", 65),
		"AB  12",           // 连续空格
		"AB\t12", "AB\n12", // 内部的空白控制字符
		"AB\x0012", "AB\x7f12", "序列号12", "ＡＢ12",
		// ſ（U+017F）、ı（U+0131）转大写后是 S、I：先转大写再查字符的话，
		// 它们会被收成另一台机器的 SN-1、ID-1。
		"ſn-1", "ıd-1",
	} {
		got, err := NormalizeSN(raw)
		if err == nil {
			t.Errorf("NormalizeSN(%q) 应被拒收，得到 %q", raw, got)
			continue
		}
		if c := problemCode(t, err); c != "sn_invalid" {
			t.Errorf("NormalizeSN(%q) 的代码是 %s，应为 sn_invalid", raw, c)
		}
	}
}

func TestNormalizeSN拒收占位值(t *testing.T) {
	placeholders := []string{
		"TO BE FILLED BY O.E.M.", "DEFAULT STRING", "SYSTEM SERIAL NUMBER",
		"CHASSIS SERIAL NUMBER", "SERIAL NUMBER", "NOT SPECIFIED", "NOT APPLICABLE",
		"N/A", "NONE", "INVALID", "OEM", "0123456789", "123456789",
	}
	var raws []string
	for _, p := range placeholders {
		// 厂商写法大小写不一，首尾常带空格：规范化之后才比较。
		raws = append(raws, p, " "+strings.ToLower(p)+"  ")
	}
	raws = append(raws, "00000000", "xxxxxxxx", "XXXXXXXX", "--", "  ffffffffffff ")
	// 只有一个字符的也算：VirtualBox 的默认 SN 就是 0，一批虚拟机都是它。
	raws = append(raws, "0", "x", "A")
	for _, raw := range raws {
		got, err := NormalizeSN(raw)
		if err == nil {
			t.Errorf("NormalizeSN(%q) 是占位值，应被拒收，得到 %q", raw, got)
			continue
		}
		if c := problemCode(t, err); c != "sn_placeholder" {
			t.Errorf("NormalizeSN(%q) 的代码是 %s，应为 sn_placeholder", raw, c)
		}
	}
	// 只是含有占位值或重复字符，本身不是占位值。
	for _, raw := range []string{"N/A-01", "OEM1", "0000 0000", "00000001"} {
		if _, err := NormalizeSN(raw); err != nil {
			t.Errorf("NormalizeSN(%q) 不是占位值，不应被拒收：%v", raw, err)
		}
	}
}

func TestNormalizeName规范化(t *testing.T) {
	cases := []struct{ raw, display, key string }{
		{"  Linux   基线  ", "Linux 基线", "linux 基线"},
		{"a\t\r\nb", "a b", "a b"},
		{"中文　名称", "中文 名称", "中文 名称"}, // 全角空格也是空白
		{strings.Repeat("中", 64), strings.Repeat("中", 64), strings.Repeat("中", 64)},
	}
	for _, c := range cases {
		display, key, err := NormalizeName(c.raw)
		if err != nil || display != c.display || key != c.key {
			t.Errorf("NormalizeName(%q) = %q, %q, %v；应为 %q, %q", c.raw, display, key, err, c.display, c.key)
		}
	}
}

// 第 2 层按 key 做唯一约束，大小写不同的两个名字必须撞在一起（C12）。
func TestNormalizeName大小写不同得到同一个key(t *testing.T) {
	_, k1, err1 := NormalizeName("Linux 基线")
	_, k2, err2 := NormalizeName(" linux  基线")
	if err1 != nil || err2 != nil || k1 != k2 {
		t.Errorf("两个名字应得到同一个 key：%q（%v）与 %q（%v）", k1, err1, k2, err2)
	}
}

func TestNormalizeName拒收不合法(t *testing.T) {
	for _, raw := range []string{
		"", " \t\n ",
		strings.Repeat("中", 65), // 按 rune 计，不按字节
		"名称\x00", "a\x1bb", "a\x7fb", "a\u0080b",
		"a\xe2\x80\x8bb", "\xef\xbb\xbf名称", // U+200B、U+FEFF：不可见的格式字符会让看着一样的名字 key 不同
		"a\xffb", // 不是合法的 UTF-8
	} {
		display, key, err := NormalizeName(raw)
		if err == nil {
			t.Errorf("NormalizeName(%q) 应被拒收，得到 %q, %q", raw, display, key)
			continue
		}
		if c := problemCode(t, err); c != "name_invalid" {
			t.Errorf("NormalizeName(%q) 的代码是 %s，应为 name_invalid", raw, c)
		}
	}
}

func TestValidIdent(t *testing.T) {
	for _, s := range []string{"a", "disk", "ip_addr", "nic2", "a_", "a" + strings.Repeat("b", 31)} {
		if !ValidIdent(s) {
			t.Errorf("ValidIdent(%q) 应为 true", s)
		}
	}
	for _, s := range []string{
		"", "1a", "_a", "Disk", "dIsk", "a-b", "a.b", "a b", "a\n", "磁盘",
		"a" + strings.Repeat("b", 32),
	} {
		if ValidIdent(s) {
			t.Errorf("ValidIdent(%q) 应为 false", s)
		}
	}
}
