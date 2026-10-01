package model

import (
	"encoding/base64"
	"encoding/binary"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParamType 是参数的类型：word、int、enum、ip、cidr、hostname、diskpath、sshkey、pwhash、text，
// 只认这十种原样写法。类型决定取值怎样校验、规范化和取子值，也决定它能不能进正文与片段：
// 正文和片段没有转义语法，系统也不解析各格式的语法，所以只放行取值不会改变结构的受限类型（C19）。
type ParamType string

// Option 是 enum 的一个静态选项：Value 是安全词，原样进正文；Label 是给人看的显示名。
type Option struct{ Value, Label string }

// ParamDecl 是 hook 或模板里的一个参数声明。第 2 层原样存它，字段名就是最终的名字。
type ParamDecl struct {
	Name  string // 标识符（ValidIdent）；同一个 hook 或模板内不重名，由激活判定查
	Label string // 显示名，可以用中文
	Type  ParamType
	List  bool // 列表 1–256 项；text 不能是列表
	// Required 只对脚本生效：被正文或片段引用的参数一律必填（C20），由参数求值判断。
	Required bool
	Default  string   // 规范形式的默认值，空表示没有；列表以换行分隔
	Options  []Option // enum 的静态选项，与 Offer 二选一
	Min, Max *int64   // int 的上下限，含两端；nil 表示不限
	Offer    string   // enum 的选项取自 ISO 提供的哪个集合（如 editions，C21），与 Options 二选一
}

// Value 是通过了类型校验的取值。它只能由 Parse 或 Sub 得到，所以拿到它就说明每一项都已是
// 规范形式。字段不导出：快照存的是类型、是否列表和 Items，不把 Value 的内部结构带进数据库。
type Value struct {
	typ   ParamType
	items []string
}

const (
	safeWordMaxLen  = 128
	listMaxItems    = 256
	textMaxBytes    = 4096
	hostnameMaxLen  = 253
	hostLabelMaxLen = 63
)

// paramTypeRow 是类型表的一行：能否进正文与片段，以及单项的校验。parse 收到的单项已去掉
// 首尾空白（text 除外），合法时返回它的规范形式。
type paramTypeRow struct {
	restricted bool
	parse      func(d ParamDecl, s string) (string, bool)
}

// paramTypeTable 照计划任务 6 的类型表。受限类型的规范形式只含安全词的字符，sshkey 另有
// 分隔用的单个空格，pwhash 另有 $。放宽任何一个类型之前，先看计划该任务的「风险」一节。
var paramTypeTable = map[ParamType]paramTypeRow{
	"word":     {true, parseWord},
	"int":      {true, parseInt},
	"enum":     {true, parseEnum},
	"ip":       {true, parseIP},
	"cidr":     {true, parseCIDR},
	"hostname": {true, parseHostname},
	"diskpath": {true, parseDiskPath},
	"sshkey":   {true, parseSSHKey},
	"pwhash":   {true, parsePWHash},
	"text":     {false, parseText},
}

// Restricted 报告 t 的取值能不能进正文与片段（C19）。text 只能经环境变量进脚本；
// 不认识的类型也不算：放它进正文，就没有任何校验替结构把关了。
func (t ParamType) Restricted() bool {
	return paramTypeTable[t].restricted
}

// Validate 校验声明本身：名字、类型、是否列表、选项、上下限和默认值。各种毛病共用
// param_decl_invalid 的同一条说明，所以最多报一条。Subject 留空：声明属于哪个 hook 或
// 模板只有调用方知道，由调用方填上。名字在 hook 或模板内是否重复，要看全部声明，不在这里查。
func (d ParamDecl) Validate() []Problem {
	if ValidIdent(d.Name) && d.typeOK() && d.optionsOK() && d.boundsOK() && d.defaultOK() {
		return nil
	}
	return []Problem{NewProblem("param_decl_invalid", Subject{}, d.Name)}
}

// typeOK：类型认识，且 text 没声明成列表。列表按换行分项，而 text 本身就能含换行。
func (d ParamDecl) typeOK() bool {
	_, known := paramTypeTable[d.Type]
	return known && !(d.List && d.Type == "text")
}

// optionsOK：enum 的选项恰好来自一处，静态选项或 ISO 提供的集合；别的类型两样都不能有。
// 静态选项值要是互不相同的安全词：它原样进正文，重复了操作员也分不清选的是哪个。
func (d ParamDecl) optionsOK() bool {
	switch {
	case d.Type != "enum":
		return len(d.Options) == 0 && d.Offer == ""
	case d.Offer != "":
		return len(d.Options) == 0 && ValidIdent(d.Offer)
	case len(d.Options) == 0:
		return false
	}
	seen := make(map[string]bool, len(d.Options))
	for _, o := range d.Options {
		if !isSafeWord(o.Value) || seen[o.Value] {
			return false
		}
		seen[o.Value] = true
	}
	return true
}

// boundsOK：上下限只给 int，下限不大于上限。
func (d ParamDecl) boundsOK() bool {
	if d.Type != "int" {
		return d.Min == nil && d.Max == nil
	}
	return d.Min == nil || d.Max == nil || *d.Min <= *d.Max
}

// defaultOK：默认值要能通过本声明的校验，而且原样就是规范形式。这样向导里预填的、
// 快照里存的是同一个串，不会一处是 +5、一处是 5。
func (d ParamDecl) defaultOK() bool {
	if d.Default == "" {
		return true
	}
	v, err := d.Parse(d.Default)
	return err == nil && v.Canonical() == d.Default
}

// Parse 按声明校验并规范化一个取值。标量去掉首尾空白再校验；列表的 raw 每行一项，空行和
// 每项首尾的空白（含 \r）都去掉，剩下的 1–256 项逐项校验，一项不合法整个列表都拒收。
// text 原样收，不去空白。空串表示没有值，不是取值，一律拒收；没有值时怎么办由参数求值定。
//
// error 是 Problem，代码为 param_value_invalid，说明里写着这个类型期望的格式。类型不认识或
// text 声明成列表时，取值无从谈起，改报 param_decl_invalid。Subject 都留空，由调用方填上。
func (d ParamDecl) Parse(raw string) (Value, error) {
	if !d.typeOK() {
		return Value{}, NewProblem("param_decl_invalid", Subject{}, d.Name)
	}
	items, ok := d.parseItems(paramTypeTable[d.Type].parse, raw)
	if !ok {
		return Value{}, NewProblem("param_value_invalid", Subject{}, d.Name, paramValueFormats[string(d.Type)])
	}
	return Value{typ: d.Type, items: items}, nil
}

func (d ParamDecl) parseItems(parse func(ParamDecl, string) (string, bool), raw string) ([]string, bool) {
	if !d.List {
		if d.Type != "text" {
			raw = strings.TrimSpace(raw)
		}
		item, ok := parse(d, raw)
		if !ok {
			return nil, false
		}
		return []string{item}, true
	}
	var items []string
	for line := range strings.Lines(raw) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		item, ok := parse(d, line)
		if !ok || len(items) == listMaxItems {
			return nil, false
		}
		items = append(items, item)
	}
	return items, len(items) > 0
}

// Items 返回各项的规范形式，标量为单元素。返回的是副本，调用方改了不影响取值。
func (v Value) Items() []string {
	return slices.Clone(v.items)
}

// Canonical 是取值的规范形式，列表以换行连接。脚本经 EnvName 的环境变量拿到的就是它。
func (v Value) Canonical() string {
	return strings.Join(v.items, "\n")
}

// cidrAccessor 是 cidr 的一个子值：子值的类型，以及从一项里取出它的办法。
type cidrAccessor struct {
	typ ParamType
	get func(p netip.Prefix) (string, bool)
}

// cidrAccessors 是 {{param:名字.子值}} 能取的全部子值，只有 cidr 有。
var cidrAccessors = map[string]cidrAccessor{
	"addr":    {"ip", func(p netip.Prefix) (string, bool) { return p.Addr().String(), true }},
	"prefix":  {"int", func(p netip.Prefix) (string, bool) { return strconv.Itoa(p.Bits()), true }},
	"netmask": {"ip", ipv4Netmask},
}

// ipv4Netmask 只对 IPv4 有：IPv6 没有点分掩码的写法，配置里只写前缀长度。
// IPv4 映射的 IPv6 地址（::ffff:a.b.c.d）也属于 IPv6。
func ipv4Netmask(p netip.Prefix) (string, bool) {
	if !p.Addr().Is4() {
		return "", false
	}
	var mask [4]byte
	binary.BigEndian.PutUint32(mask[:], ^uint32(0)<<uint(32-p.Bits()))
	return netip.AddrFrom4(mask).String(), true
}

// Sub 取子值，列表逐项取，结果仍是列表。只有 cidr 有子值：addr 与 netmask 是 ip，prefix 是
// int，netmask 只适用于 IPv4。看的是声明的类型，不看取值长得像不像 cidr。不适用时 error 是
// Problem，代码为 param_accessor_invalid，Subject 留空，由调用方填上。
func (v Value) Sub(name string) (Value, error) {
	acc, ok := cidrAccessors[name]
	if !ok || v.typ != "cidr" {
		return Value{}, NewProblem("param_accessor_invalid", Subject{}, name)
	}
	out := Value{typ: acc.typ, items: make([]string, 0, len(v.items))}
	for _, item := range v.items {
		// 各项是 Parse 产出的规范形式，解析不会出错。万一出了错，get 对无效的前缀也只会
		// 返回无意义的值而不会 panic，这里一并按不适用处理。
		p, err := netip.ParsePrefix(item)
		sub, ok := acc.get(p)
		if err != nil || !ok {
			return Value{}, NewProblem("param_accessor_invalid", Subject{}, name)
		}
		out.items = append(out.items, sub)
	}
	return out, nil
}

// EnvName 是脚本拿参数用的环境变量名：disk → LOBER_PARAM_DISK。第 3 层按它导出变量，
// hook 的激活判定按它核对脚本里的引用，两处都只认这个函数。参数名是 ValidIdent 过的标识符，
// 转成大写后仍与参数名一一对应。
func EnvName(param string) string {
	return "LOBER_PARAM_" + strings.ToUpper(param)
}

// isSafeWord 判断安全词：1–128 个 [A-Za-z0-9._:/@^+=-]。它不含引号、空白、$、#、%、<>&、
// 反斜杠和花括号，放进七种格式里带引号的位置、XML 文本、kickstart 与 preseed 的指令值、
// shell 参数，都不会改变结构，也拼不出占位符或 Jinja 表达式。放宽任何一个字符，都要先在
// 七种格式的文本、引号和 shell 位置里逐一确认。
//
// 结构不变不等于语义不变：YAML 裸值不能以 @ 开头，以 : 结尾会被读成映射键，这些由作者
// 按格式留意（计划任务 6 的「安全词」）。
func isSafeWord(s string) bool {
	if s == "" || len(s) > safeWordMaxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !isASCIIAlnum(c) && strings.IndexByte("._:/@^+=-", c) < 0 {
			return false
		}
	}
	return true
}

func isASCIIAlnum(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}

func parseWord(_ ParamDecl, s string) (string, bool) {
	return s, isSafeWord(s)
}

// parseInt 收十进制 int64，可带正负号和前导零，规范形式两者都不带。
func parseInt(d ParamDecl, s string) (string, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || (d.Min != nil && n < *d.Min) || (d.Max != nil && n > *d.Max) {
		return "", false
	}
	return strconv.FormatInt(n, 10), true
}

// parseEnum 只认选项值，区分大小写。选项取自 ISO 时这里只查是不是安全词，在不在该 ISO
// 的集合里要对照具体的 ISO，由参数求值查（C21）。
func parseEnum(d ParamDecl, s string) (string, bool) {
	if !isSafeWord(s) {
		return "", false
	}
	if len(d.Options) == 0 {
		return s, d.Offer != ""
	}
	for _, o := range d.Options {
		if o.Value == s {
			return s, true
		}
	}
	return "", false
}

// parseIP 不收 zone：它只在本机有意义，写进应答文件到了别的机器上就不对了。IPv4 各段的
// 前导零由 netip 拒收：有的实现按八进制读（010 是 8），不替人猜。
func parseIP(_ ParamDecl, s string) (string, bool) {
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return "", false
	}
	return a.String(), true
}

// parseCIDR 保留主机位：10.0.0.3/8 说的是「这台机器的地址和前缀」，不是网段。
func parseCIDR(_ ParamDecl, s string) (string, bool) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return "", false
	}
	return p.String(), true
}

// parseHostname 按 RFC 1123 查。必须先查字符再转小写：开尔文符号（U+212A）转小写是 k，
// 先转的话它会被收成另一个主机名。
func parseHostname(_ ParamDecl, s string) (string, bool) {
	if s == "" || len(s) > hostnameMaxLen {
		return "", false
	}
	for label := range strings.SplitSeq(s, ".") {
		if label == "" || len(label) > hostLabelMaxLen || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for i := 0; i < len(label); i++ {
			if c := label[i]; !isASCIIAlnum(c) && c != '-' {
				return "", false
			}
		}
	}
	return strings.ToLower(s), true // 走到这里只剩 ASCII，ToLower 只会动 ASCII 字母
}

// parseDiskPath 要求 /dev/ 之下一个具体的设备：不收空段、. 和 ..，免得 /dev/../etc/shadow
// 这类路径借着 /dev/ 前缀混进分区指令。
func parseDiskPath(_ ParamDecl, s string) (string, bool) {
	rest, ok := strings.CutPrefix(s, "/dev/")
	if !ok || !isSafeWord(s) {
		return "", false
	}
	for seg := range strings.SplitSeq(rest, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return s, true
}

// sshKeyFields 是各算法公钥 blob 的字段数，第一个字段总是算法名（RFC 4253、5656、8709 与
// OpenSSH 的 PROTOCOL.u2f）。键就是 sshkey 收的全部算法。
var sshKeyFields = map[string]int{
	"ssh-ed25519":                        2, // 算法名、公钥
	"ssh-rsa":                            3, // 算法名、e、n
	"ecdsa-sha2-nistp256":                3, // 算法名、曲线名、公钥点
	"ecdsa-sha2-nistp384":                3,
	"ecdsa-sha2-nistp521":                3,
	"sk-ssh-ed25519@openssh.com":         3, // 算法名、公钥、application
	"sk-ecdsa-sha2-nistp256@openssh.com": 4, // 算法名、曲线名、公钥点、application
}

// parseSSHKey 收「算法 base64[ 注释]」，多余的空格压成一个；只认空格分隔，注释是安全词。
// base64 要能解开，解出的 blob 要与算法对得上，截断或拼错的公钥不必等装完机登不上才发现。
func parseSSHKey(_ ParamDecl, s string) (string, bool) {
	var fields []string
	for f := range strings.SplitSeq(s, " ") {
		if f == "" {
			continue
		}
		if len(fields) == 3 {
			return "", false
		}
		fields = append(fields, f)
	}
	if len(fields) < 2 || (len(fields) == 3 && !isSafeWord(fields[2])) {
		return "", false
	}
	algo, key := fields[0], fields[1]
	// base64 解码器会跳过换行，所以先自己查字符，不指望它来拦。
	if _, ok := sshKeyFields[algo]; !ok || strings.ContainsFunc(key, notBase64Char) {
		return "", false
	}
	blob, err := base64.StdEncoding.Strict().DecodeString(key)
	if err != nil || !sshKeyBlobOK(algo, blob) {
		return "", false
	}
	return strings.Join(fields, " "), true
}

func notBase64Char(r rune) bool {
	return r > 0x7f || (!isASCIIAlnum(byte(r)) && r != '+' && r != '/' && r != '=')
}

// sshKeyBlobOK 拆开公钥 blob：若干个「4 字节大端长度 + 内容」的字段首尾相接、恰好用完，
// 第一个字段是算法名，字段数与该算法相符。
func sshKeyBlobOK(algo string, blob []byte) bool {
	n := 0
	for len(blob) > 0 {
		if len(blob) < 4 {
			return false
		}
		size := uint64(binary.BigEndian.Uint32(blob))
		blob = blob[4:]
		if size > uint64(len(blob)) || (n == 0 && string(blob[:size]) != algo) {
			return false
		}
		blob = blob[size:]
		n++
	}
	return n == sshKeyFields[algo]
}

// pwhashSchemes 是 pwhash 收的 crypt 方案：前缀，以及去掉前缀后以 $ 分隔的段数。
// SHA-crypt 是盐与散列，yescrypt 多一段参数。
var pwhashSchemes = []struct {
	prefix string
	parts  int
}{
	{"$6$", 2},
	{"$5$", 2},
	{"$y$", 3},
}

// parsePWHash 只看 crypt 的写法，不验散列本身。每段非空、只含 [./A-Za-z0-9]：带 rounds= 的
// SHA-crypt 写法有等号，不收。
func parsePWHash(_ ParamDecl, s string) (string, bool) {
	for _, sc := range pwhashSchemes {
		rest, ok := strings.CutPrefix(s, sc.prefix)
		if !ok {
			continue
		}
		parts := strings.Split(rest, "$")
		if len(parts) != sc.parts {
			return "", false
		}
		for _, p := range parts {
			if p == "" || strings.ContainsFunc(p, notCryptChar) {
				return "", false
			}
		}
		return s, true
	}
	return "", false
}

func notCryptChar(r rune) bool {
	return r > 0x7f || (!isASCIIAlnum(byte(r)) && r != '.' && r != '/')
}

// parseText 收任意 UTF-8，原样保留。不含 NUL：环境变量的值装不下它。
func parseText(_ ParamDecl, s string) (string, bool) {
	if s == "" || len(s) > textMaxBytes || !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 {
		return "", false
	}
	return s, true
}
