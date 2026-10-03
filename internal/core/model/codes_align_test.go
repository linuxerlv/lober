package model

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
)

// 本文件把 codes.go 说明里抄下的事实与代码钉在一起（T5-5）：上限、参数类型、算法、保留键、安全词
// 字符、子值，目录里的时机、部件、系统值和 ESXi 的固件限制，以及主机平台的取值、脚本参数的变量名和
// 转义用的系统值（C-9）。能力表注释写明是初值，第 3、7 层会改，第 3 层还可能加参数类型；到时只改了
// 一边，这里就报红。超时范围、插槽名长度与占位符的种类不在本包登记（本包的测试也 import 不了 scan，
// 会成环），在 activation、scan 的测试里对齐。
//
// 说明里的清单按措辞找（如「类型只能是 …」）。措辞改了就找不到，同样报红：先改这里的写法，再核对清单。

// alignText 是代码的说明与下一步模板，中间隔一个换行。要对齐的都写死在模板里，不用填参数。
func alignText(t *testing.T, c Code) string {
	t.Helper()
	spec, ok := codeTable[c]
	if !ok {
		t.Fatalf("%s 没有登记", c)
	}
	return spec.message + "\n" + spec.next
}

// alignFind 按 pattern 找出 text 里每一处要对齐的内容，返回各处的分组；一处都没有就判失败。
func alignFind(t *testing.T, where, text, pattern string) [][]string {
	t.Helper()
	found := regexp.MustCompile(pattern).FindAllStringSubmatch(text, -1)
	if len(found) == 0 {
		t.Fatalf("%s 里按 %s 找不到要对齐的内容；措辞改了的话，先改测试里的写法再核对：\n%s", where, pattern, text)
	}
	return found
}

// alignSplit 拆开说明里的清单，「a、b 或 c」拆成三项。
func alignSplit(list string) []string {
	return strings.Split(strings.ReplaceAll(list, " 或 ", "、"), "、")
}

// alignKeys 是 map 的键，按字典序排好，失败信息因此固定。
func alignKeys[K ~string, V any](m map[K]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	slices.Sort(keys)
	return keys
}

// alignSame 比对说明列出的词与代码里的取值：不论顺序，一个不多、一个不少，也不重复。
func alignSame(t *testing.T, where string, listed, actual []string) {
	t.Helper()
	var extra, missing, dup []string
	seen := make(map[string]bool, len(listed))
	for _, w := range listed {
		switch {
		case seen[w]:
			dup = append(dup, w)
		case !slices.Contains(actual, w):
			extra = append(extra, w)
		}
		seen[w] = true
	}
	for _, w := range actual {
		if !seen[w] {
			missing = append(missing, w)
		}
	}
	if len(extra)+len(missing)+len(dup) > 0 {
		t.Errorf("%s与代码对不上：多了 %q，少了 %q，重复了 %q\n说明里是 %q\n代码里是 %q", where, extra, missing, dup, listed, actual)
	}
}

// alignCount 是说明里个数的中文写法，「三个」的三。
func alignCount(n int) string {
	if words := []string{"零", "一", "两", "三", "四", "五", "六", "七", "八", "九", "十"}; n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n) // 十以上的写法没登记：照数字报出来，到时在上面补上
}

// 上限连同措辞一起找，如「最长 32 个字符」：只找数字的话，同一条说明里另一个上限碰巧等于新值也算数，
// identMaxLen 改成 64 时，param_decl_invalid 就会被旁边的「最多 64 个字」顶过去。
func TestAlign说明里的上限与常量一致(t *testing.T) {
	cases := []struct {
		where, text string
		name        string // 常量名，只用于失败信息
		value       int
		wording     string // 说明里写这个上限的措辞，%d 处是数字
	}{
		{"sn_invalid", alignText(t, "sn_invalid"), "snMaxLen", snMaxLen, "1–%d 个"},
		{"snReasonTooLong", snReasonTooLong, "snMaxLen", snMaxLen, "超过 %d 个字符"},
		{"name_invalid", alignText(t, "name_invalid"), "nameMaxLen", nameMaxLen, "1–%d 个"},
		{"nameReasonTooLong", nameReasonTooLong, "nameMaxLen", nameMaxLen, "超过 %d 个字符"},
		{"param_decl_invalid", alignText(t, "param_decl_invalid"), "identMaxLen", identMaxLen, "最长 %d 个字符"},
		{"param_decl_invalid", alignText(t, "param_decl_invalid"), "labelMaxRunes", labelMaxRunes, "最多 %d 个字"},
		{"attr_key_invalid", alignText(t, "attr_key_invalid"), "identMaxLen", identMaxLen, "最长 %d 个字符"},
		{"param_value_invalid", alignText(t, "param_value_invalid"), "listMaxItems", listMaxItems, "1–%d 项"},
		{"param_value_invalid", alignText(t, "param_value_invalid"), "listMaxBytes", listMaxBytes, "不超过 %d 字节"},
		{"paramValueFormats[word]", paramValueFormats["word"], "safeWordMaxLen", safeWordMaxLen, "1–%d 个"},
		{"paramValueFormats[diskpath]", paramValueFormats["diskpath"], "safeWordMaxLen", safeWordMaxLen, "1–%d 个"},
		{"paramValueFormats[hostname]", paramValueFormats["hostname"], "hostLabelMaxLen", hostLabelMaxLen, "1–%d 个"},
		{"paramValueFormats[hostname]", paramValueFormats["hostname"], "hostnameMaxLen", hostnameMaxLen, "不超过 %d 个字符"},
		{"paramValueFormats[sshkey]", paramValueFormats["sshkey"], "sshKeyBlobMaxBytes", sshKeyBlobMaxBytes, "不超过 %d 字节"},
		{"paramValueFormats[pwhash]", paramValueFormats["pwhash"], "pwhashMaxLen", pwhashMaxLen, "不超过 %d 字节"},
		{"paramValueFormats[text]", paramValueFormats["text"], "textMaxBytes", textMaxBytes, "不超过 %d 字节"},
	}
	for _, c := range cases {
		if want := fmt.Sprintf(c.wording, c.value); !strings.Contains(c.text, want) {
			t.Errorf("%s 里应有「%s」，与 %s = %d 一致；改了一边，另一边要跟着改：\n%s", c.where, want, c.name, c.value, c.text)
		}
	}
}

// 第 3 层加参数类型时两处都得补：param_decl_invalid 不列它，作者照说明找不到新类型；paramValueFormats
// 缺了它，param_value_invalid 的「应为…」后面就是空的。
func TestAlign参数类型清单与类型表一致(t *testing.T) {
	types := alignKeys(paramTypeTable)
	for _, m := range alignFind(t, "param_decl_invalid", alignText(t, "param_decl_invalid"), `类型只能是 ([^\s，；。]+)`) {
		alignSame(t, "param_decl_invalid 的类型清单", alignSplit(m[1]), types)
	}
	alignSame(t, "paramValueFormats 的键", alignKeys(paramValueFormats), types)
}

// sshkey 的格式列出了收的全部算法：这里跟进 OpenSSH 的新算法时，说明得一起补，被拒的人才知道收哪些。
func TestAlignSSH公钥算法与支持的算法一致(t *testing.T) {
	for _, m := range alignFind(t, "paramValueFormats[sshkey]", paramValueFormats["sshkey"], `算法限 ([^\s，；。]+)`) {
		alignSame(t, "sshkey 的算法", alignSplit(m[1]), alignKeys(sshKeyFields))
	}
}

// attr_key_invalid 的说明与下一步各列了一遍保留键，两处都要与 hostReservedAttrs 一致：漏列一个，
// 被拒的人对照说明找不出原因。
func TestAlign保留属性键与hostReservedAttrs一致(t *testing.T) {
	found := alignFind(t, "attr_key_invalid", alignText(t, "attr_key_invalid"), `(?:保留的|不能是) ([^\s，；。]+)`)
	if len(found) != 2 {
		t.Errorf("attr_key_invalid 的说明与下一步应各列一遍保留键，找到 %d 处", len(found))
	}
	for _, m := range found {
		alignSame(t, "attr_key_invalid 的保留键", alignSplit(m[1]), hostReservedAttrs)
	}
}

// alignCatalogVocab 经目录的公开查询（Families → SeriesOf → Format）求各格式支持的时机、必需的部件和
// 能引用的系统值的并集，各按首次出现的顺序：目录没有直接列出全部取值的查询。
func alignCatalogVocab() (timings, parts, values []string) {
	add := func(list *[]string, w string) {
		if !slices.Contains(*list, w) {
			*list = append(*list, w)
		}
	}
	for _, fam := range catalog.Families() {
		for _, s := range catalog.SeriesOf(fam) {
			f := s.Format()
			for _, tm := range f.Timings() {
				add(&timings, string(tm))
			}
			for _, p := range f.RequiredParts() {
				add(&parts, string(p))
			}
			for _, v := range f.SystemValues() {
				add(&values, string(v))
			}
		}
	}
	return timings, parts, values
}

// timing_unknown、part_unknown、sysvalue_unknown 的下一步逐个列出了全部取值，后两条还写了个数。能力表
// 是初值，第 3、7 层真装之后会改：加了时机、部件或系统值，说明就得跟着补。
func TestAlign时机部件系统值与目录一致(t *testing.T) {
	timings, parts, values := alignCatalogVocab()
	cases := []struct {
		code    Code
		pattern string // 第 1 个分组是清单，第 2 个分组（有的话）是写出的个数
		actual  []string
	}{
		{"timing_unknown", `执行时机只能是 (\S+) 之一`, timings},
		{"part_unknown", `系统部件只有 (\S+) (\p{Han}+)个`, parts},
		{"sysvalue_unknown", `系统值只有 (\S+) (\p{Han}+)个`, values},
	}
	for _, c := range cases {
		t.Run(string(c.code), func(t *testing.T) {
			for _, m := range alignFind(t, string(c.code), alignText(t, c.code), c.pattern) {
				alignSame(t, string(c.code)+" 列出的取值", alignSplit(m[1]), c.actual)
				if len(m) > 2 && m[2] != alignCount(len(c.actual)) {
					t.Errorf("%s 写的是「%s个」，目录里有 %d 个", c.code, m[2], len(c.actual))
				}
			}
		})
	}
}

// firmware_mismatch 的下一步提醒「ESXi 只走 UEFI」，系与固件都写显示名，要与 esxi 系的 Firmwares()
// 一致：目录放开或改了这条限制，提醒就成了错的。
func TestAlignESXi只走UEFI与目录一致(t *testing.T) {
	esxi := catalog.Series("esxi")
	var labels []string
	for _, fw := range esxi.Firmwares() {
		labels = append(labels, fw.Label())
	}
	pattern := `（` + regexp.QuoteMeta(esxi.Label()) + ` 只走 ([^\s（）]+)）`
	for _, m := range alignFind(t, "firmware_mismatch", alignText(t, "firmware_mismatch"), pattern) {
		alignSame(t, "firmware_mismatch 说的 ESXi 能用的固件", alignSplit(m[1]), labels)
	}
}

// 安全词的字符抄在三处：sn_unsafe_in_body 的下一步，word、diskpath 的格式。逐个试 0x21–0x7E 里字母、
// 数字以外的字符，夹在两个字母中间试：以后若对首尾另加限制（T6-3 留待第 3 层定），不影响这里比字符集。
func TestAlign安全词字符与IsSafeWord一致(t *testing.T) {
	var accepted []string
	for r := rune(0x21); r <= 0x7e; r++ {
		if !isASCIIAlnum(byte(r)) && IsSafeWord("a"+string(r)+"b") {
			accepted = append(accepted, string(r))
		}
	}
	texts := []struct{ where, text string }{
		{"sn_unsafe_in_body", alignText(t, "sn_unsafe_in_body")},
		{"paramValueFormats[word]", paramValueFormats["word"]},
		{"paramValueFormats[diskpath]", paramValueFormats["diskpath"]},
	}
	for _, c := range texts {
		t.Run(c.where, func(t *testing.T) {
			for _, m := range alignFind(t, c.where, c.text, `字母、数字[和或] (\S+) `) {
				alignSame(t, c.where+" 的安全词字符", strings.Split(m[1], ""), accepted)
			}
		})
	}
}

// pwhash 的格式列出了收的 crypt 前缀，要与 pwhashSchemes 一致：加了方案，说明里得补上。
func TestAlign口令散列前缀与支持的方案一致(t *testing.T) {
	var prefixes []string
	for _, sc := range pwhashSchemes {
		prefixes = append(prefixes, sc.prefix)
	}
	for _, m := range alignFind(t, "paramValueFormats[pwhash]", paramValueFormats["pwhash"], `以 (.+?) 开头`) {
		alignSame(t, "pwhash 的前缀", alignSplit(m[1]), prefixes)
	}
}

// param_accessor_invalid 列出了 cidr 的全部子值，要与 cidrAccessors 一致：加了子值，说明里得补上。
func TestAlignCIDR子值与支持的子值一致(t *testing.T) {
	for _, m := range alignFind(t, "param_accessor_invalid", alignText(t, "param_accessor_invalid"), `cidr 参数有 (\S+) 子值`) {
		alignSame(t, "param_accessor_invalid 的子值", alignSplit(m[1]), alignKeys(cidrAccessors))
	}
}

// alignFirmwares 经目录的公开查询（Families → SeriesOf → Firmwares）求各系能装的固件的并集，按首次出现的
// 顺序。没有固件限制的系能装全部固件，所以并集就是全集。
func alignFirmwares() []string {
	var out []string
	for _, fam := range catalog.Families() {
		for _, s := range catalog.SeriesOf(fam) {
			for _, fw := range s.Firmwares() {
				if !slices.Contains(out, string(fw)) {
					out = append(out, string(fw))
				}
			}
		}
	}
	return out
}

// host_platform_invalid 的下一步列出了架构与固件的合法取值，以及 iPXE 上报值怎样对应（C-9）：目录改了
// 取值或换算，说明得跟着改，被拒的人照着说明才填得对。固件查得到全集；目录没有列出全部架构的公开查询，
// 架构只能查列出的都合法，目录新加了架构而说明没补，这里查不出。每一对 iPXE 换算都按目录核对。
func TestAlign主机平台的取值与目录一致(t *testing.T) {
	text := alignText(t, "host_platform_invalid")
	arches := alignSplit(alignFind(t, "host_platform_invalid", text, `架构只能填 ([^，；。]+)，`)[0][1])
	firmwares := alignSplit(alignFind(t, "host_platform_invalid", text, `固件只能填 ([^，；。]+)，`)[0][1])
	for _, a := range arches {
		// 说明还写了「区分大小写」：大写的写法不能也算合法。
		if !catalog.Arch(a).Valid() || catalog.Arch(strings.ToUpper(a)).Valid() {
			t.Errorf("host_platform_invalid 列出的架构 %q 应是目录里区分大小写的合法取值", a)
		}
	}
	for _, fw := range firmwares {
		if catalog.Firmware(strings.ToUpper(fw)).Valid() {
			t.Errorf("host_platform_invalid 说区分大小写，固件 %q 的大写写法却也合法", fw)
		}
	}
	alignSame(t, "host_platform_invalid 列出的固件", firmwares, alignFirmwares())

	m := alignFind(t, "host_platform_invalid", text, `iPXE 上报的 (\S+) 分别对应 (\S+?)，(\S+) 分别对应 (\S+?)。`)[0]
	pairs := []struct {
		what     string
		from, to []string
		convert  func(string) (string, bool)
	}{
		{"架构", alignSplit(m[1]), alignSplit(m[2]), func(s string) (string, bool) {
			a, ok := catalog.ArchFromIPXE(s)
			return string(a), ok
		}},
		{"固件", alignSplit(m[3]), alignSplit(m[4]), func(s string) (string, bool) {
			fw, ok := catalog.FirmwareFromIPXE(s)
			return string(fw), ok
		}},
	}
	for _, p := range pairs {
		if len(p.from) != len(p.to) {
			t.Errorf("iPXE 的%s上报值 %q 与对应的取值 %q 个数不同", p.what, p.from, p.to)
			continue
		}
		for i, from := range p.from {
			if got, ok := p.convert(from); !ok || got != p.to[i] {
				t.Errorf("说明说 iPXE 上报的%s %q 对应 %q，目录换算成 %q、%v", p.what, from, p.to[i], got, ok)
			}
		}
	}
}

// script_placeholder_not_expanded 的下一步教作者改从环境变量取参数，写出了变量名的规则和一个例子（C-9）：
// 都要与 EnvName 一致，第 3 层就是按它导出变量的；照着错的名字去取，脚本拿到的是空值。
func TestAlign脚本占位符说明里的变量名与EnvName一致(t *testing.T) {
	text := alignText(t, "script_placeholder_not_expanded")
	for _, m := range alignFind(t, "script_placeholder_not_expanded", text, `变量名是 (\S+) 加大写的参数名`) {
		if m[1] != EnvName("") {
			t.Errorf("说明里的变量名前缀是 %q，EnvName 用的是 %q", m[1], EnvName(""))
		}
	}
	for _, m := range alignFind(t, "script_placeholder_not_expanded", text, `\{\{param:([a-z][a-z0-9_]*)\}\} 对应 ([A-Z0-9_]+)`) {
		if got := EnvName(m[1]); got != m[2] {
			t.Errorf("说明说 {{param:%s}} 对应 %s，EnvName 给的是 %s", m[1], m[2], got)
		}
	}
}

// 三条说明教人写 {{sys:lbrace}} 原样输出 {{（C-9）：它要是目录里的系统值，并且每种格式都能用，否则照着
// 写反倒报 sysvalue_unknown 或 sysvalue_not_allowed。
func TestAlign说明里转义用的系统值每种格式都能用(t *testing.T) {
	var formats []catalog.Format
	for _, fam := range catalog.Families() {
		for _, s := range catalog.SeriesOf(fam) {
			if !slices.Contains(formats, s.Format()) {
				formats = append(formats, s.Format())
			}
		}
	}
	for _, c := range []Code{"placeholder_unknown_kind", "placeholder_unclosed", "script_placeholder_not_expanded"} {
		for _, m := range alignFind(t, string(c), alignText(t, c), `\{\{sys:([^{}]+)\}\}`) {
			v := catalog.SystemValue(m[1])
			if !v.Valid() {
				t.Errorf("%s 写的 {{sys:%s}} 不是目录里的系统值", c, m[1])
				continue
			}
			for _, f := range formats {
				if !slices.Contains(f.SystemValues(), v) {
					t.Errorf("%s 写的 {{sys:%s}} 在 %s 格式里不可用", c, m[1], f)
				}
			}
		}
	}
}
