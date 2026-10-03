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

// usKeyboardSymbols 是美式键盘上可见的 32 个符号，照键盘逐个抄下，不按码位推出来：名称规则
// 说的就是它们（T5-3）。
const usKeyboardSymbols = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

func TestNormalizeName收英文字母数字与32个可见符号(t *testing.T) {
	if len(usKeyboardSymbols) != 32 {
		t.Fatalf("测试自己抄的符号应有 32 个，抄了 %d 个", len(usKeyboardSymbols))
	}
	type nameCase struct{ raw, key string }
	cases := []nameCase{
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789",
			"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz0123456789"},
		{usKeyboardSymbols, usKeyboardSymbols},
	}
	for _, c := range usKeyboardSymbols {
		sym := string(c)
		cases = append(cases, nameCase{sym, sym}, nameCase{"Rocky" + sym + "9", "rocky" + sym + "9"})
	}
	for _, c := range cases {
		display, key, err := NormalizeName(c.raw)
		if err != nil || display != c.raw || key != c.key {
			t.Errorf("NormalizeName(%q) = %q, %q, %v；应为 %q, %q", c.raw, display, key, err, c.raw, c.key)
		}
	}
}

// 上面的符号是照键盘抄的；这里按码位把 ASCII 的 128 个字符各夹进名字中间试一遍，两边合起来
// 才说明规则不多不少：只有 0x21–0x7E 收得下。
func TestNormalizeName只收0x21到0x7E(t *testing.T) {
	for r := range rune(0x80) {
		raw := "a" + string(r) + "b"
		_, _, err := NormalizeName(raw)
		if want := 0x21 <= r && r <= 0x7e; (err == nil) != want {
			t.Errorf("NormalizeName(%q)：收下应为 %v，得到 err = %v", raw, want, err)
		}
	}
}

// 首尾的空白去掉，其余原样保留，大小写只在 key 里统一。长度在去掉空白之后数。
func TestNormalizeName去掉首尾空白(t *testing.T) {
	a64 := strings.Repeat("a", 64)
	cases := []struct{ raw, display, key string }{
		{"  Rocky-Base\t\r\n", "Rocky-Base", "rocky-base"},
		{"\U00003000RHEL_9\U00003000", "RHEL_9", "rhel_9"}, // 全角空格也是空白
		{" " + a64 + " ", a64, a64},
	}
	for _, c := range cases {
		display, key, err := NormalizeName(c.raw)
		if err != nil || display != c.display || key != c.key {
			t.Errorf("NormalizeName(%q) = %q, %q, %v；应为 %q, %q", c.raw, display, key, err, c.display, c.key)
		}
	}
}

// 第 2 层按 key 做唯一约束，大小写不同的名字必须撞在一起（C12）；display 保留原来的大小写。
func TestNormalizeName大小写不同得到同一个key(t *testing.T) {
	for _, raw := range []string{"Rocky-Base", "rocky-base", " ROCKY-BASE "} {
		display, key, err := NormalizeName(raw)
		if err != nil || display != strings.TrimSpace(raw) || key != "rocky-base" {
			t.Errorf("NormalizeName(%q) = %q, %q, %v；key 应为 rocky-base，display 保留大小写", raw, display, key, err)
		}
	}
}

// 名称只收可打印 ASCII（T5-3）。标准库没有 NFC、NFKC：下面这些写法看着与别的名字一样或几乎
// 一样，key 却不同，收下来就能绕过同名约束；空格、中文按用户的决定一并拒收。说明里的原因要选对，
// 作者才知道改哪里。不可见的字符一律写成转义，免得编辑器悄悄改掉它们。
func TestNormalizeName拒收空白中文与不可见字符(t *testing.T) {
	cases := []struct{ name, raw, reason string }{
		{"空串", "", nameReasonEmpty},
		{"只有空白", " \t\r\n\U00003000", nameReasonEmpty},
		{"中间有空格", "Rocky Base", nameReasonSpace},
		{"中间有制表符", "Rocky\tBase", nameReasonSpace},
		{"中间有全角空格", "Rocky\U00003000Base", nameReasonSpace},
		{"中间有不换行空格", "Rocky\U000000a0Base", nameReasonSpace},
		{"中文", "最小模板", nameReasonChar},
		{"中英混写", "RHEL基线", nameReasonChar},
		{"全角字母 Ａ", "\U0000ff21BC", nameReasonChar},
		{"合成写法的 é", "caf\U000000e9", nameReasonChar},
		{"分解写法的 é", "cafe\U00000301", nameReasonChar},
		{"变体选择符 U+FE0F", "base\U0000fe0f", nameReasonChar},
		{"组合字形连接符 U+034F", "ba\U0000034fse", nameReasonChar},
		{"韩文填充符 U+3164", "base\U00003164", nameReasonChar},
		{"零宽空格 U+200B", "ba\U0000200bse", nameReasonChar},
		{"BOM", "\U0000feffbase", nameReasonChar},
		{"开尔文符号，转小写就是 k", "\U0000212aube", nameReasonChar},
		{"NUL", "base\x00", nameReasonChar},
		{"ESC", "a\x1bb", nameReasonChar},
		{"DEL", "a\x7fb", nameReasonChar},
		{"C1 控制字符", "a\U00000080b", nameReasonChar},
		{"非法的 UTF-8", "a\xffb", nameReasonChar},
		{"65 个字符", strings.Repeat("a", 65), nameReasonTooLong},
	}
	for _, c := range cases {
		display, key, err := NormalizeName(c.raw)
		if err == nil {
			t.Errorf("%s：NormalizeName(%q) 应被拒收，得到 %q, %q", c.name, c.raw, display, key)
			continue
		}
		var p Problem
		if !errors.As(err, &p) {
			t.Fatalf("%s：error 应能经 errors.As 取出 Problem，得到 %T：%v", c.name, err, err)
		}
		// Subject 留空，由调用方填上；原因取 codes.go 登记的那一条。
		if want := NewProblem("name_invalid", Subject{}, c.reason); p != want {
			t.Errorf("%s：NormalizeName(%q) 报的是 %+v，应为 %+v", c.name, c.raw, p, want)
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

// 仓储生成的 ID 是 26 个字符的小写 base32，可能以数字开头；内置内容带的是固定 ID。单个字符与
// 64 个字符是长度的两端。合法的 ID 也都得是安全词：任务 ID 经 {{sys:task}} 原样进正文（L-13b）。
func TestValidID收仓储生成的与内置的ID(t *testing.T) {
	for _, s := range []string{
		"k5xq2z7m4rvbt6nwy3hpdjcgfa", "7aq4m2xk5rzt3wb6nyhgpdjcfe",
		"builtin-kickstart",
		"a", "7",
		strings.Repeat("a", 64),
	} {
		if !ValidID(s) {
			t.Errorf("ValidID(%q) 应为 true", s)
		}
		if !IsSafeWord(s) {
			t.Errorf("合法的 ID %q 应当也是安全词", s)
		}
	}
}

// 每一例只犯一条规则。
func TestValidID拒收(t *testing.T) {
	cases := []struct{ name, s string }{
		{"空串", ""},
		{"大写", "Builtin-kickstart"},
		{"下划线", "builtin_kickstart"},
		{"@ 是「ID@版本」里的分隔符", "rocky-base@4"},
		{"点", "rocky.base"},
		{"斜杠", "rocky/base"},
		{"空格", "rocky base"},
		{"以 - 开头，放进命令行会被当成选项", "-builtin"},
		{"65 个字符", strings.Repeat("a", 65)},
		{"中文", "模板"},
		{"全角字母", "\U0000ff42uiltin"},
		{"结尾是换行", "builtin\n"},
		{"NUL", "a\x00b"},
	}
	for _, c := range cases {
		if ValidID(c.s) {
			t.Errorf("%s：ValidID(%q) 应为 false", c.name, c.s)
		}
	}
}

// 上面是挑出来的例子；这里按码位把 ASCII 的 128 个字符分别放在开头和末尾各试一遍，两边合起来才说明
// 字符集不多不少：开头只收小写字母与数字，其余位置另收 -。
func TestValidID逐个字符(t *testing.T) {
	for r := range rune(0x80) {
		c := string(r)
		alnum := 'a' <= r && r <= 'z' || '0' <= r && r <= '9'
		if got := ValidID(c + "a"); got != alnum {
			t.Errorf("ValidID(%q) = %v，应为 %v", c+"a", got, alnum)
		}
		if got, want := ValidID("a"+c), alnum || r == '-'; got != want {
			t.Errorf("ValidID(%q) = %v，应为 %v", "a"+c, got, want)
		}
	}
}

// 能存成 UTF-8 文本的都能存（T13-10）。PostgreSQL 的 text 与 jsonb 不收 NUL，encoding/json 又会
// 把非法字节换成 U+FFFD，存进去再取出来就不是原来的字节；NUL 以外的控制字符、换行都存得下。
func TestStorableText(t *testing.T) {
	for _, s := range []string{
		"", // 空草稿也能存
		"#!/bin/sh\r\necho 你好\n\tdone\n",
		"\x01\x1b\x7f\U00000085",
		"\U0000feff带 BOM 的正文", "\U0001f600", "\U0000fffd",
	} {
		if !StorableText(s) {
			t.Errorf("StorableText(%q) 应为 true", s)
		}
	}
	for _, s := range []string{
		"\x00", "a\x00b", "结尾是 NUL\x00",
		"\xff", "a\xffb",
		"\xe4\xb8",           // 截断的「中」
		"\xed\xa0\x80",       // UTF-8 编码的代理项 U+D800
		"\xc0\x80",           // NUL 的超长编码（Java 的修订版 UTF-8 这样写 NUL）
		"\xff\xfe#\x00!\x00", // UTF-16LE 的「#!」：按文件上传的 .ps1、.cmd 常是这样
	} {
		if StorableText(s) {
			t.Errorf("StorableText(%q) 应为 false", s)
		}
	}
}

// 显示名可以用中文，中间可以有空格（T13-10）；64 个字以内都收。
func TestValidLabel收看得见的显示名(t *testing.T) {
	for _, s := range []string{
		"交换分区（MiB）", "NTP 服务器", "Windows Server 2025 Datacenter", "a",
		"\U00002764", "\U0001f600", // 不带变体选择符的 emoji 本身看得见
		"中间\U00003000有全角空格",
		strings.Repeat("中", 64),
	} {
		if !ValidLabel(s) {
			t.Errorf("ValidLabel(%q) 应为 true", s)
		}
	}
}

// 显示名要存得下、看得见、首尾干净：不可见的字符会让两个看着一样的显示名不同，或在界面上
// 打乱显示。空串也不收，显示名能不能留空由调用方判断。每一例只犯一条规则。
func TestValidLabel拒收不可见字符首尾空白与超长(t *testing.T) {
	cases := []struct{ name, s string }{
		{"空串", ""},
		{"开头是空格", " 交换分区"},
		{"结尾是空格", "交换分区 "},
		{"开头是全角空格", "\U00003000交换分区"},
		{"结尾是换行", "交换分区\n"},
		{"65 个字", strings.Repeat("中", 65)},
		{"变体选择符 VS16", "\U00002764\U0000fe0f"},
		{"表意文字变体选择符", "葛\U000e0100"},
		{"零宽空格", "交换\U0000200b分区"},
		{"零宽连接符", "交换\U0000200d分区"},
		{"BOM", "\U0000feff交换分区"},
		{"从右到左覆盖", "交换\U0000202e分区"},
		{"软连字符", "交换\U000000ad分区"},
		{"组合字形连接符 U+034F", "交换\U0000034f分区"},
		{"韩文填充符 U+3164", "交换\U00003164分区"},
		{"响铃", "交换\x07分区"},
		{"制表符", "交换\t分区"},
		{"DEL", "交换\x7f分区"},
		{"NEL", "交换\U00000085分区"},
		{"NUL", "交换\x00分区"},
		{"非法的 UTF-8", "交换\xff分区"},
	}
	for _, c := range cases {
		if ValidLabel(c.s) {
			t.Errorf("%s：ValidLabel(%q) 应为 false", c.name, c.s)
		}
	}
}
