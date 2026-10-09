package model

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// 真实世界的写法：公钥由 ssh-keygen 生成（私钥随即删掉），SHA-crypt 散列由 openssl passwd
// 生成。校验规则不能把它们挡在门外。
const (
	keyEd25519  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDNaJi8uw2oGwAug7HWX7cvFNvum8Lk5qwBmH3KVfWx3 ops@lober-test"
	keyECDSA384 = "ecdsa-sha2-nistp384 AAAAE2VjZHNhLXNoYTItbmlzdHAzODQAAAAIbmlzdHAzODQAAABhBIGOkTSFzJWDoOdKGjXYH16L0GycaJYFdxatj31pKmlacP0E9Xjd9J266BfdquMH0edbjT6tnDElpkH+/o/2Kfyda6C63hUBpCsDoZBtJdsQlESX84J6MDEgqLiYtxZq7A== root@pxe"
	// ssh-keygen -C "" 的原样输出：没有注释，行尾留着一个空格。
	keyRSA = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDeniCMki6s4CmJdhOQ/ijbLXlrFOD0s7ua9QSefrVLIaJFbcTN2cmxZgwvXkZSyv2uzqaAehYGieXF9nP3rf9Fu2nG+c/jmvx12evqHZ8ISCnIlH36kgjZoIxcPyWqPaJ/Hj4W7XnPBKAE1M6S4ZOyrC8n3u70zJywUMEWfmJ7xl+6xBt9nQ3Bv8mfxPwuWebtxO+cY7n3ZVQYIkcTMh1zkcvxPJdFDnZO7dBU6vPEoKs8p3kutiRNgQq2KFyg6etFHISWvicwlniPi87PSKUCGvhCIg4xFkflqG2wZ3pi8dFuXzCxksXWxzxtvFJ0SOXZo29jKOlaXiuwWkTMeqSR "

	hashSHA512 = "$6$Xy7.ab/Q$gC3uFAr1KLOtix6ADDQfq8konX8KYhSlksATh5cWsgVsZsQhU150UNCu69FQ9VP.6VTovrqGb4c/.J3OD2DwP."
	hashSHA256 = "$5$8kQ2mZp$NV7u1wpnA84EkGw9EPTTk0whtG.N4PAs5q8A7Vh47V2"
	// yescrypt 比 SHA-crypt 多一段参数：$y$参数$盐$散列。openssl 生成不了它，照 mkpasswd 输出的结构写。
	hashYescrypt = "$y$j9T$F5Jx5fExrKuPp53xLKQ..1$X3DX6M94c7o.9agCG9G317fhZg9SqC.5i5rd.RhAtQ7"
)

// keyEd25519B64 是 keyEd25519 的公钥部分，用来拼各种写错的公钥。
var keyEd25519B64 = strings.Fields(keyEd25519)[1]

// paramTypes 是计划任务 6 类型表里的十种类型。
var paramTypes = []ParamType{"word", "int", "enum", "ip", "cidr", "hostname", "diskpath", "sshkey", "pwhash", "text"}

// sshBlobBytes 按 OpenSSH 公钥的线上格式拼 blob：每个字段是 4 字节大端长度加内容。
func sshBlobBytes(fields ...string) []byte {
	var b []byte
	for _, f := range fields {
		b = binary.BigEndian.AppendUint32(b, uint32(len(f)))
		b = append(b, f...)
	}
	return b
}

func sshBlob(fields ...string) string {
	return base64.StdEncoding.EncodeToString(sshBlobBytes(fields...))
}

// sshKeyOf 给一种算法拼一把字段数正确的公钥（字段数见 RFC 4253、5656、8709 与 OpenSSH 的
// PROTOCOL.u2f）。算法名之后各字段的内容不校验，个数要对。
func sshKeyOf(algo string) string {
	point := strings.Repeat("q", 65)
	rest := map[string][]string{
		"ssh-ed25519":                        {strings.Repeat("k", 32)},
		"ssh-rsa":                            {"\x01\x00\x01", strings.Repeat("n", 256)},
		"ecdsa-sha2-nistp256":                {"nistp256", point},
		"ecdsa-sha2-nistp384":                {"nistp384", point},
		"ecdsa-sha2-nistp521":                {"nistp521", point},
		"sk-ssh-ed25519@openssh.com":         {strings.Repeat("k", 32), "ssh:"},
		"sk-ecdsa-sha2-nistp256@openssh.com": {"nistp256", point, "ssh:"},
	}[algo]
	return algo + " " + sshBlob(append([]string{algo}, rest...)...)
}

func int64p(v int64) *int64 { return &v }

func mustParse(t *testing.T, d ParamDecl, raw string) Value {
	t.Helper()
	v, err := d.Parse(raw)
	if err != nil {
		t.Fatalf("%s（%s）解析 %q 失败：%v", d.Name, d.Type, raw, err)
	}
	return v
}

// asProblem 取出 error 里的 Problem；取不出就判失败。
func asProblem(t *testing.T, err error) Problem {
	t.Helper()
	var p Problem
	if !errors.As(err, &p) {
		t.Fatalf("error 应能经 errors.As 取出 Problem，得到 %T：%v", err, err)
	}
	return p
}

// typeCase 是一种类型（或一种声明写法）的取值样例：ok 是原始输入到规范形式，bad 必须拒收。
type typeCase struct {
	decl ParamDecl
	ok   map[string]string
	bad  []string
}

// typeCases 给每种类型至少三个合法、三个非法样例。受限类型的非法样例里都有能改变结构的
// 字符；TestParse受限类型拒收能改变结构的字符 还会往每个合法样例里逐一夹进安全词以外的字符。
func typeCases() []typeCase {
	hostname253 := strings.Repeat(strings.Repeat("h", 63)+".", 3) + strings.Repeat("h", 61)
	sshOK := map[string]string{
		keyEd25519:  keyEd25519,
		keyECDSA384: keyECDSA384,
		keyRSA:      strings.TrimSpace(keyRSA), // 注释可省
		"ssh-ed25519   " + keyEd25519B64 + "    ops@lober-test": keyEd25519, // 多余的空格压成一个
	}
	for _, algo := range []string{
		"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
		"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com",
	} {
		sshOK[sshKeyOf(algo)] = sshKeyOf(algo)
	}
	withComment := func(comment string) string { return "ssh-ed25519 " + keyEd25519B64 + " " + comment }

	return []typeCase{
		{
			decl: ParamDecl{Name: "pkg", Type: "word"},
			ok: map[string]string{
				"nginx":                  "nginx",
				" kernel-5.14.0+el9\t":   "kernel-5.14.0+el9", // 首尾空白去掉
				"Asia/Shanghai":          "Asia/Shanghai",
				"en_US.UTF-8":            "en_US.UTF-8",
				"ops@lab:a^b=c":          "ops@lab:a^b=c",
				strings.Repeat("w", 128): strings.Repeat("w", 128),
			},
			bad: []string{
				"", " \t", strings.Repeat("w", 129), "vim enhanced", "包名", "ｖｉｍ",
				`a"b`, "a'b", "a\nb", "a<b", "a#b", "a$(id)", "a`id`", `a\b`, "a{{b}}", "a%b", "a&b", "a;b", "a|b", "a*", "~root",
			},
		},
		{
			decl: ParamDecl{Name: "count", Type: "int"},
			ok: map[string]string{
				"42":                   "42",
				"+5":                   "5",
				"007":                  "7",
				"-12":                  "-12",
				"-007":                 "-7",
				"-0":                   "0",
				" 8\r\n":               "8",
				"9223372036854775807":  "9223372036854775807",
				"-9223372036854775808": "-9223372036854775808",
			},
			bad: []string{
				"", "4.0", "1e3", "0x10", "5_000", "+-5", "--5", "１２", "five",
				"9223372036854775808", // 超出 int64
				`5"`, "5\n6", "<5>", "#5", "$(5)", "`5`",
			},
		},
		{
			// 上下限两端都含。
			decl: ParamDecl{Name: "vlan", Type: "int", Min: int64p(1), Max: int64p(4094)},
			ok:   map[string]string{"1": "1", "4094": "4094", "+0100": "100"},
			bad:  []string{"0", "4095", "-1", "$(1)"},
		},
		{
			decl: ParamDecl{Name: "profile", Type: "enum", Options: []Option{{"server", "服务器"}, {"desktop", "桌面"}, {"minimal", "最小化"}}},
			ok:   map[string]string{"server": "server", " minimal ": "minimal", "desktop": "desktop"},
			bad: []string{
				"", "Server", "serverx", "服务器", // 只认选项值，区分大小写，显示名不算
				`server"`, "server\nx", "<server>", "#server", "$(server)", "`server`",
			},
		},
		{
			// 选项取自 ISO：这里只能查是不是安全词，在不在这个 ISO 的集合里由参数求值查（C21）。
			decl: ParamDecl{Name: "edition", Type: "enum", Offer: "editions"},
			ok:   map[string]string{"ServerStandard": "ServerStandard", "ServerDatacenterCore": "ServerDatacenterCore", " Pro ": "Pro"},
			bad:  []string{"", "Server Standard", `Pro"`, "Pro#1", "$(Pro)", "`Pro`"},
		},
		{
			decl: ParamDecl{Name: "gw", Type: "ip"},
			ok: map[string]string{
				"10.0.0.1":        "10.0.0.1",
				" 192.168.1.10 ":  "192.168.1.10",
				"::1":             "::1",
				"2001:DB8::1":     "2001:db8::1",
				"::FFFF:10.0.0.1": "::ffff:10.0.0.1",
				"2001:0db8:0000:0000:0000:0000:0000:0001": "2001:db8::1",
			},
			bad: []string{
				"", "010.0.0.1", "10.0.0.256", "10.0.0", "10.0.0.1/24", "gw.example.com",
				"fe80::1%eth0", // zone 只在本机有意义，写进应答文件到了别的机器上就不对了
				`10.0.0.1"`, "10.0.0.1\n10.0.0.2", "<10.0.0.1>", "10.0.0.1#", "$(10.0.0.1)", "`10.0.0.1`",
			},
		},
		{
			decl: ParamDecl{Name: "addr", Type: "cidr"},
			ok: map[string]string{
				"10.0.0.3/8":     "10.0.0.3/8", // 保留主机位：它说的是「这台机器的地址和前缀」
				"192.168.1.0/24": "192.168.1.0/24",
				"0.0.0.0/0":      "0.0.0.0/0",
				" 10.0.0.1/32 ":  "10.0.0.1/32",
				"2001:DB8::1/64": "2001:db8::1/64",
			},
			bad: []string{
				"", "10.0.0.3", "10.0.0.3/33", "10.0.0.3/08", "010.0.0.3/8", "10.0.0.3/ 8", "fe80::1%eth0/64",
				`10.0.0.3/8"`, "10.0.0.3/8\n10.0.0.4/8", "<10.0.0.3/8>", "10.0.0.3/8#", "$(10.0.0.3/8)", "`10.0.0.3/8`",
			},
		},
		{
			decl: ParamDecl{Name: "host", Type: "hostname"},
			ok: map[string]string{
				"node01":                         "node01",
				"a":                              "a",
				"1-2.a-b":                        "1-2.a-b",
				"PXE-01.Lab.Example":             "pxe-01.lab.example", // 输出小写
				"xn--fiq228c.example":            "xn--fiq228c.example",
				strings.Repeat("h", 63) + ".lab": strings.Repeat("h", 63) + ".lab",
				hostname253:                      hostname253,
			},
			bad: []string{
				"", "-node", "node-", "no_de", "node..lab", ".node", "node.",
				strings.Repeat("h", 64), hostname253 + "h", "node 01", "节点",
				// 开尔文符号（U+212A，长得像 K）转小写就是 k：先转小写再查字符，它会被收成 kube。
				"\xe2\x84\xaaube",
				`node"`, "node\n01", "<node>", "node#1", "$(node)", "`node`",
			},
		},
		{
			decl: ParamDecl{Name: "disk", Type: "diskpath"},
			ok: map[string]string{
				"/dev/sda":                                 "/dev/sda",
				" /dev/nvme0n1 ":                           "/dev/nvme0n1",
				"/dev/disk/by-id/wwn-0x5000c500a1b2c3d4":   "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4",
				"/dev/disk/by-path/pci-0000:00:1f.2-ata-1": "/dev/disk/by-path/pci-0000:00:1f.2-ata-1",
				"/dev/mapper/vg0-root":                     "/dev/mapper/vg0-root",
				"/dev/" + strings.Repeat("d", 123):         "/dev/" + strings.Repeat("d", 123), // 共 128 个字符
			},
			bad: []string{
				"", "sda", "dev/sda", "/devices/sda", "/dev", "/dev/", "/dev/" + strings.Repeat("d", 124),
				// 要指向 /dev 之下一个具体的设备
				"/dev/sda/", "/dev//sda", "/dev/./sda", "/dev/../etc/shadow",
				"/dev/sd a", `/dev/sda"`, "/dev/sda\n/dev/sdb", "/dev/<sda>", "/dev/sda#", "/dev/$(id)", "/dev/`id`",
			},
		},
		{
			decl: ParamDecl{Name: "key", Type: "sshkey"},
			ok:   sshOK,
			bad: []string{
				"", "ssh-ed25519", keyEd25519B64,
				"SSH-ED25519 " + keyEd25519B64,                      // 算法名区分大小写
				"ssh-dss " + sshBlob("ssh-dss", "p", "q", "g", "y"), // 不在算法清单里
				// 声明的算法与公钥里记的对不上，字段数却相符：与合法的 sshKeyOf("ssh-ed25519")、
				// keyECDSA384 相比只差一个算法名，拦得住它们的只有算法名的检查。
				"ssh-ed25519 " + sshBlob("ssh-rsa", strings.Repeat("k", 32)),
				strings.Replace(keyECDSA384, "nistp384", "nistp256", 1),
				"ssh-ed25519 " + sshBlob("ssh-ed25519", "k", "extra"), // 字段数不对
				"ssh-ed25519 " + keyEd25519B64[:40],                   // 截断
				"ssh-ed25519 " + keyEd25519B64 + "==",                 // 不是合法的 base64
				"ssh-ed25519\t" + keyEd25519B64,                       // 只认空格分隔
				withComment("my laptop"),                              // 注释里不能有空格
				withComment(strings.Repeat("c", 129)),                 // 注释是安全词，最长 128 个字符
				// 字段之后还剩几个字节，凑不成下一个字段的长度。
				"ssh-ed25519 " + base64.StdEncoding.EncodeToString(append(sshBlobBytes("ssh-ed25519", strings.Repeat("k", 32)), 0, 0, 0)),
				// base64 解码器会跳过换行，拦换行不能指望它。
				"ssh-ed25519 " + keyEd25519B64[:20] + "\n" + keyEd25519B64[20:],
				keyEd25519 + "\n" + keyEd25519,
				keyEd25519 + `"`, withComment("<ops>"), withComment("#ops"), withComment("$(id)"), withComment("`id`"),
			},
		},
		{
			decl: ParamDecl{Name: "root_pw", Type: "pwhash"},
			ok: map[string]string{
				hashSHA512:              hashSHA512,
				hashSHA256:              hashSHA256,
				hashYescrypt:            hashYescrypt,
				" " + hashSHA256 + "\n": hashSHA256,
			},
			bad: []string{
				"", "lober-test", // 明文
				"$1$abcdefgh$9Kq0eRfGkFZm2i8Ohs4Gc/",                           // MD5-crypt 不在清单里
				"$2b$12$C6UzMDM.H6dfI/f/IKcEeO5aWjvVLs1F0DJ3tM6qQYz1JAx3lJb1y", // bcrypt 也不在
				"$6$", "$6$Xy7.ab/Q", "$6$Xy7.ab/Q$", "$6$$abc", "$y$j9T$abc",
				"$6$rounds=5000$Xy7.ab/Q$abc", // rounds= 带等号，计划只收 [./A-Za-z0-9$]
				hashSHA512 + `"`, "$6$salt$ha\nsh", "$6$salt$ha sh", "$6$salt$<hash>", "$6$salt$hash#", "$6$salt$$(id)", "$6$salt$`id`",
			},
		},
		{
			// text 只经环境变量交给脚本，进不了正文与片段，所以引号、换行、$( 都是它的合法内容。
			decl: ParamDecl{Name: "motd", Type: "text"},
			ok: map[string]string{
				"echo \"hi\" > /tmp/x; $(id) `uname` <a>&amp; # 注释": "echo \"hi\" > /tmp/x; $(id) `uname` <a>&amp; # 注释",
				"  首尾空白原样保留\r\n":                                    "  首尾空白原样保留\r\n",
				strings.Repeat("a", 4096):                           strings.Repeat("a", 4096),
				strings.Repeat("字", 1365) + "a":                     strings.Repeat("字", 1365) + "a", // 正好 4096 字节
			},
			bad: []string{"", "a\x00b", strings.Repeat("a", 4097), strings.Repeat("字", 1366), "\xff\xfe"},
		},
	}
}

func TestParamType只有text不受限(t *testing.T) {
	for _, typ := range paramTypes {
		if got, want := typ.Restricted(), typ != "text"; got != want {
			t.Errorf("%s.Restricted() = %v，应为 %v", typ, got, want)
		}
	}
	// 不认识的类型不能当受限类型：放它进正文，就没有任何校验替结构把关了。
	for _, typ := range []ParamType{"", "Word", "TEXT", "string"} {
		if typ.Restricted() {
			t.Errorf("不认识的类型 %q 不应算受限类型", typ)
		}
	}
}

func TestParse样例覆盖十种类型(t *testing.T) {
	seen := map[ParamType]bool{}
	for _, c := range typeCases() {
		seen[c.decl.Type] = true
		if len(c.ok) < 3 || len(c.bad) < 3 {
			t.Errorf("%s 的样例不够：合法 %d 个、非法 %d 个，各要至少 3 个", c.decl.Name, len(c.ok), len(c.bad))
		}
	}
	for _, typ := range paramTypes {
		if !seen[typ] {
			t.Errorf("类型 %s 没有取值样例", typ)
		}
	}
}

// 上面两个测试遍历的是 paramTypes：类型表少了类型查得出，多了查不出。往 paramTypeTable 里
// 加了类型却没补进 paramTypes，它就不会有样例，param_value_invalid 的格式说明也可能是空串，
// 所以这里双向比对，并要求表里每种类型都登记了格式。
func TestParamType类型表与计划一致(t *testing.T) {
	got := slices.Sorted(maps.Keys(paramTypeTable))
	if want := slices.Sorted(slices.Values(paramTypes)); !slices.Equal(got, want) {
		t.Errorf("paramTypeTable 的类型是 %q，计划是 %q", got, want)
	}
	for _, typ := range got {
		if strings.TrimSpace(paramValueFormats[string(typ)]) == "" {
			t.Errorf("类型 %s 没有登记 param_value_invalid 期望的格式", typ)
		}
	}
}

func TestParse合法取值得到规范形式(t *testing.T) {
	for _, c := range typeCases() {
		for raw, want := range c.ok {
			v, err := c.decl.Parse(raw)
			if err != nil {
				t.Errorf("%s：%q 应合法：%v", c.decl.Name, raw, err)
				continue
			}
			if got := v.Items(); len(got) != 1 || got[0] != want || v.Canonical() != want {
				t.Errorf("%s：%q 的规范形式应为 %q，得到 Items %q、Canonical %q", c.decl.Name, raw, want, got, v.Canonical())
			}
			// 规范形式是不动点：默认值要求原样写成规范形式，这一条保证它总写得出来。
			if again, err := c.decl.Parse(want); err != nil || again.Canonical() != want {
				t.Errorf("%s：规范形式 %q 再解析一次变成了 %q（%v）", c.decl.Name, want, again.Canonical(), err)
			}
			if c.decl.Type.Restricted() && !onlyBodySafe(c.decl.Type, want) {
				t.Errorf("%s：规范形式 %q 含安全词以外的字符", c.decl.Name, want)
			}
		}
	}
}

// onlyBodySafe 判断受限类型的规范形式是否只含安全词的字符；sshkey 另有分隔用的空格，
// pwhash 另有 $（计划要求作者在 shell 里给它加单引号）。
func onlyBodySafe(typ ParamType, s string) bool {
	allowed := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:/@^+=-" // 计划的 [A-Za-z0-9._:/@^+=-]
	switch typ {
	case "sshkey":
		allowed += " "
	case "pwhash":
		allowed += "$"
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return !strings.ContainsRune(allowed, r) })
}

func TestParse拒收非法取值(t *testing.T) {
	for _, c := range typeCases() {
		for _, raw := range c.bad {
			v, err := c.decl.Parse(raw)
			if err == nil {
				t.Errorf("%s：%q 应拒收，得到 %q", c.decl.Name, raw, v.Canonical())
				continue
			}
			p := asProblem(t, err)
			if p.Code != "param_value_invalid" {
				t.Errorf("%s：%q 的代码是 %s，应为 param_value_invalid", c.decl.Name, raw, p.Code)
			}
			// 说明要点出是哪个参数、期望什么格式，操作员才知道怎么改。
			if format := paramValueFormats[string(c.decl.Type)]; !strings.Contains(p.Message, "「"+c.decl.Name+"」") || !strings.Contains(p.Message, format) {
				t.Errorf("%s：说明里应有参数名和 %s 期望的格式 %q：%s", c.decl.Name, c.decl.Type, format, p.Message)
			}
		}
	}
}

// 安全插值的根基（C19）：受限类型的合法取值里夹进一个安全词以外的字符就必须拒收，夹在开头、
// 中间、结尾都一样。字符按安全词的补集取，不靠列举：0x00–0x7F 里 onlyBodySafe 不放行的每一个，
// 再加几个非 ASCII 字符，所以安全词的字符集哪怕被放宽一个字符，这里也会失败。sshkey 的空格、
// pwhash 的 $ 是它们规范形式里的分隔符，不在补集里。首尾的空白本来就会被去掉，所以
// unicode.IsSpace 为真的字符只夹在中间。
func TestParse受限类型拒收能改变结构的字符(t *testing.T) {
	var chars []rune
	for r := range rune(0x80) {
		chars = append(chars, r)
	}
	chars = append(chars,
		0x00a0, 0x0085, 0x2028, // 不换行空格、NEL、行分隔符：都算空白，YAML 1.1 还把后两个当换行
		0x200b, 0xfeff, 0x202e, // 零宽空格、BOM、从右到左覆盖：看不见，最后一个还会打乱显示顺序
		0xff56, 0x212a, // 全角的 ｖ；开尔文符号（U+212A），转小写就是 k
	)
	for _, c := range typeCases() {
		if !c.decl.Type.Restricted() {
			continue
		}
	nextChar:
		for _, r := range chars {
			x := string(r)
			if onlyBodySafe(c.decl.Type, x) {
				continue
			}
			for _, s := range c.ok {
				mid := len(s) / 2
				var tries []string
				switch {
				case !unicode.IsSpace(r):
					tries = []string{x + s, s[:mid] + x + s[mid:], s + x}
				case mid > 0: // 只有一个字符的取值没有中间可夹
					tries = []string{s[:mid] + x + s[mid:]}
				}
				for _, raw := range tries {
					if v, err := c.decl.Parse(raw); err == nil {
						t.Errorf("%s：夹进 %q 的 %q 应拒收，得到 %q", c.decl.Name, x, raw, v.Canonical())
						continue nextChar // 同一个字符报一次就够了
					}
				}
			}
		}
	}
}

// IsSafeWord 是安全词唯一的一份判定：word 等类型的取值、enum 的选项值，以及快照构建对 {{sys:sn}}
// 要代入的 SN，都按它查，别处不另抄字符集。字符按 0x00–0x7F 逐个与计划的 [A-Za-z0-9._:/@^+=-]
// 比对，再加几个非 ASCII 字符；长度为 1–128。
func TestIsSafeWord与计划的字符集和长度一致(t *testing.T) {
	for r := range rune(0x80) {
		s := "a" + string(r) + "b"
		if got, want := IsSafeWord(s), onlyBodySafe("word", s); got != want {
			t.Errorf("IsSafeWord(%q) = %v，应为 %v", s, got, want)
		}
	}
	// 非 ASCII 的三个：不换行空格（U+00A0）、全角的 a（U+FF41）、汉字。
	nonASCII := []string{"a" + string(rune(0x00a0)) + "b", string(rune(0xff41)), "名"}
	for _, s := range append([]string{"", strings.Repeat("w", 129), "VMWARE-42 1A"}, nonASCII...) {
		if IsSafeWord(s) {
			t.Errorf("IsSafeWord(%q) 应为 false", s)
		}
	}
	// 以 - 或 @ 开头的也照收（T6-3 只补了文档）：要不要拒收，留到第 3 层写内置模板之前再定，到时改这里。
	for _, s := range []string{"w", strings.Repeat("w", 128), "VMWARE-42-1A", "Asia/Shanghai", "ops@lab:a^b+c=d_e.f", "--setopt=sslverify=0", "@x"} {
		if !IsSafeWord(s) {
			t.Errorf("IsSafeWord(%q) 应为 true", s)
		}
	}
}

// 计划点名的规范化：同一个值只有一种写法，向导里预填的、快照里存的、正文里渲染的都是它。
func TestParse规范化(t *testing.T) {
	cases := []struct {
		typ       ParamType
		raw, want string
	}{
		{"int", "+5", "5"},
		{"int", "007", "7"},
		{"hostname", "PXE-01.Lab.Example", "pxe-01.lab.example"},
		{"sshkey", "ssh-ed25519   " + keyEd25519B64 + "    ops@lober-test", keyEd25519},
		{"ip", "2001:0DB8::0001", "2001:db8::1"},
	}
	for _, c := range cases {
		if got := mustParse(t, ParamDecl{Name: "p", Type: c.typ}, c.raw).Canonical(); got != c.want {
			t.Errorf("%s：%q 的规范形式应为 %q，得到 %q", c.typ, c.raw, c.want, got)
		}
	}
	// IPv4 各段的前导零有的实现按八进制读（010 是 8），不替人猜，直接拒收。
	for _, raw := range []string{"010.0.0.1", "10.0.0.01"} {
		if _, err := (ParamDecl{Name: "gw", Type: "ip"}).Parse(raw); err == nil {
			t.Errorf("ip：%q 带前导零，应拒收", raw)
		}
	}
}

func TestParse列表逐项校验(t *testing.T) {
	pkgs := ParamDecl{Name: "pkgs", Type: "word", List: true}

	// 每行一项。浏览器提交的 \r\n、空行和每项首尾的空白都不是内容；顺序照原样，不去重。
	v := mustParse(t, pkgs, "vim\r\n\n  curl \n\tgit\nvim\n")
	if got, want := v.Items(), []string{"vim", "curl", "git", "vim"}; !slices.Equal(got, want) {
		t.Errorf("Items = %q，应为 %q", got, want)
	}
	if got := v.Canonical(); got != "vim\ncurl\ngit\nvim" {
		t.Errorf("Canonical = %q，应以换行连接各项", got)
	}

	// 每项各自规范化。
	ports := mustParse(t, ParamDecl{Name: "ports", Type: "int", List: true}, "+80\n0443")
	if got := ports.Items(); !slices.Equal(got, []string{"80", "443"}) {
		t.Errorf("int 列表的 Items = %q，应为 [80 443]", got)
	}

	// 只有一项的列表与标量一样，Items 是单元素。
	if got := mustParse(t, pkgs, "vim").Items(); !slices.Equal(got, []string{"vim"}) {
		t.Errorf("单项列表的 Items = %q，应为 [vim]", got)
	}

	full := strings.Repeat("vim\n", 256)
	if got := len(mustParse(t, pkgs, full).Items()); got != 256 {
		t.Errorf("256 项的列表解析出 %d 项", got)
	}

	// 项数不在 1–256 之内，或者有一项不合法，整个列表都拒收：不悄悄丢掉写错的那一项。
	for _, raw := range []string{
		"", "\n", " \r\n\t\n",
		full + "vim",
		"vim\nvim enhanced\ncurl", "vim\n$(reboot)", "vim\n`id`\n",
	} {
		_, err := pkgs.Parse(raw)
		if err == nil {
			t.Errorf("列表 %q 应拒收", raw)
			continue
		}
		if p := asProblem(t, err); p.Code != "param_value_invalid" {
			t.Errorf("列表 %q 的代码是 %s，应为 param_value_invalid", raw, p.Code)
		}
	}
}

func TestValueItems返回副本(t *testing.T) {
	v := mustParse(t, ParamDecl{Name: "pkgs", Type: "word", List: true}, "vim\ncurl")
	v.Items()[0] = "$(reboot)"
	if got := v.Canonical(); got != "vim\ncurl" {
		t.Errorf("改了 Items 返回的切片，取值跟着变成了 %q", got)
	}
}

// 类型不认识或 text 声明成列表时，取值无从谈起：报声明的问题，而不是让操作员去改取值。
func TestParse声明不成立时报param_decl_invalid(t *testing.T) {
	for _, d := range []ParamDecl{
		{Name: "x"},
		{Name: "x", Type: "string"},
		{Name: "motd", Type: "text", List: true},
	} {
		_, err := d.Parse("abc")
		if err == nil {
			t.Errorf("%+v：应报错", d)
			continue
		}
		if p := asProblem(t, err); p.Code != "param_decl_invalid" {
			t.Errorf("%+v：代码是 %s，应为 param_decl_invalid", d, p.Code)
		}
	}
}

func TestValueSub取cidr的子值(t *testing.T) {
	addr := ParamDecl{Name: "addr", Type: "cidr"}
	// 子值本身也是受限类型的取值：addr 与 netmask 是 ip，prefix 是 int，各自的规范形式不变。
	subType := map[string]ParamType{"addr": "ip", "prefix": "int", "netmask": "ip"}
	cases := []struct{ raw, sub, want string }{
		{"10.0.0.3/8", "addr", "10.0.0.3"},
		{"10.0.0.3/8", "prefix", "8"},
		{"10.0.0.3/8", "netmask", "255.0.0.0"},
		{"192.168.1.77/26", "netmask", "255.255.255.192"},
		{"10.0.0.1/32", "netmask", "255.255.255.255"},
		{"0.0.0.0/0", "netmask", "0.0.0.0"},
		{"2001:db8::1/64", "addr", "2001:db8::1"},
		{"2001:db8::1/64", "prefix", "64"},
	}
	for _, c := range cases {
		sub, err := mustParse(t, addr, c.raw).Sub(c.sub)
		if err != nil || sub.Canonical() != c.want {
			t.Errorf("%s 的 %s 应为 %q，得到 %q（%v）", c.raw, c.sub, c.want, sub.Canonical(), err)
			continue
		}
		if again := mustParse(t, ParamDecl{Name: "sub", Type: subType[c.sub]}, c.want); again.Canonical() != c.want {
			t.Errorf("%s 的 %s 不是 %s 的规范形式：%q", c.raw, c.sub, subType[c.sub], c.want)
		}
	}

	// 列表逐项取，顺序不变。
	nets := mustParse(t, ParamDecl{Name: "nets", Type: "cidr", List: true}, "10.0.0.3/8\n192.168.1.5/24")
	for sub, want := range map[string][]string{
		"addr":    {"10.0.0.3", "192.168.1.5"},
		"prefix":  {"8", "24"},
		"netmask": {"255.0.0.0", "255.255.255.0"},
	} {
		v, err := nets.Sub(sub)
		if err != nil || !slices.Equal(v.Items(), want) {
			t.Errorf("列表的 %s 应为 %q，得到 %q（%v）", sub, want, v.Items(), err)
		}
	}
}

func TestValueSub不适用时报param_accessor_invalid(t *testing.T) {
	cidr := ParamDecl{Name: "addr", Type: "cidr"}
	v4 := mustParse(t, cidr, "10.0.0.3/8")
	addrOnly, err := v4.Sub("addr")
	if err != nil {
		t.Fatalf("取 addr 失败：%v", err)
	}
	cases := map[string]struct {
		v   Value
		sub string
	}{
		// IPv6 没有点分掩码；IPv4 映射地址属于 IPv6，同样没有。
		"IPv6 取 netmask":     {mustParse(t, cidr, "2001:db8::1/64"), "netmask"},
		"IPv4 映射地址取 netmask": {mustParse(t, cidr, "::ffff:10.0.0.3/104"), "netmask"},
		"列表里混了 IPv6":         {mustParse(t, ParamDecl{Name: "nets", Type: "cidr", List: true}, "10.0.0.3/8\n2001:db8::1/64"), "netmask"},
		// 看的是声明的类型，不是取值长得像不像 cidr。
		"ip 没有子值":    {mustParse(t, ParamDecl{Name: "gw", Type: "ip"}, "10.0.0.1"), "addr"},
		"word 没有子值":  {mustParse(t, ParamDecl{Name: "pkg", Type: "word"}, "10.0.0.0/8"), "addr"},
		"int 没有子值":   {mustParse(t, ParamDecl{Name: "count", Type: "int"}, "8"), "prefix"},
		"子值没有子值":     {addrOnly, "addr"},
		"不认识的子值":     {v4, "mask"},
		"子值名区分大小写":   {v4, "Addr"},
		"空的子值名":      {v4, ""},
		"|join 不是子值": {v4, "|join"},
		"零值":         {Value{}, "addr"},
	}
	for name, c := range cases {
		_, err := c.v.Sub(c.sub)
		if err == nil {
			t.Errorf("%s：应报错", name)
			continue
		}
		p := asProblem(t, err)
		if p.Code != "param_accessor_invalid" || !strings.Contains(p.Message, "「"+c.sub+"」") {
			t.Errorf("%s：应报 param_accessor_invalid 并点出子值名 %q，得到 %s：%s", name, c.sub, p.Code, p.Message)
		}
	}
}

func TestParamDeclValidate合法的声明(t *testing.T) {
	for _, d := range []ParamDecl{
		{Name: "pkgs", Label: "软件包", Type: "word", List: true, Default: "vim\ncurl"},
		{Name: "vlan", Label: "VLAN", Type: "int", Min: int64p(1), Max: int64p(4094), Default: "100"},
		{Name: "mtu", Type: "int", Min: int64p(576)},
		{Name: "retries", Type: "int", Max: int64p(10), Default: "-1"},
		{Name: "only_one", Type: "int", Min: int64p(1), Max: int64p(1), Default: "1"},
		{Name: "profile", Type: "enum", Options: []Option{{"server", "服务器"}, {"minimal", "最小化"}}, Default: "minimal"},
		{Name: "edition", Type: "enum", Offer: "editions", Default: "ServerStandard"},
		{Name: "gw", Type: "ip", Default: "2001:db8::1"},
		{Name: "dns", Type: "ip", List: true, Default: "10.0.0.53\n2001:db8::53"},
		{Name: "addr", Type: "cidr", Default: "10.0.0.3/8"},
		{Name: "host", Type: "hostname", Default: "node01.lab"},
		{Name: "disk", Label: "系统盘", Type: "diskpath", Required: true},
		{Name: "keys", Type: "sshkey", List: true, Default: keyEd25519 + "\n" + strings.TrimSpace(keyRSA)},
		{Name: "root_pw", Type: "pwhash", Default: hashSHA512},
		{Name: "motd", Type: "text", Default: "  欢迎\n$(不会被执行)  "},
	} {
		if probs := d.Validate(); len(probs) != 0 {
			t.Errorf("%s 的声明应当合法，得到 %+v", d.Name, probs)
		}
	}
}

func TestParamDeclValidate拒收不合法的声明(t *testing.T) {
	opts := []Option{{"server", "服务器"}, {"minimal", "最小化"}}
	cases := map[string]ParamDecl{
		"名字为空":        {Type: "word"},
		"名字有大写":       {Name: "Disk", Type: "word"},
		"名字以数字开头":     {Name: "1disk", Type: "word"},
		"名字带连字符":      {Name: "disk-path", Type: "word"},
		"名字是中文":       {Name: "磁盘", Type: "word"},
		"类型为空":        {Name: "disk"},
		"类型不认识":       {Name: "disk", Type: "string"},
		"类型大小写不对":     {Name: "disk", Type: "Word"},
		"text 声明成列表":  {Name: "motd", Type: "text", List: true},
		"默认值不合法":      {Name: "disk", Type: "diskpath", Default: "sda"},
		"默认值超出上下限":    {Name: "vlan", Type: "int", Min: int64p(1), Max: int64p(4094), Default: "0"},
		"默认值不在选项里":    {Name: "profile", Type: "enum", Options: opts, Default: "desktop"},
		"列表默认值超过256项": {Name: "pkgs", Type: "word", List: true, Default: strings.TrimSuffix(strings.Repeat("vim\n", 257), "\n")},

		// 默认值要原样就是规范形式：向导里预填的和快照里存的才是同一个串。
		"默认值带正号":        {Name: "count", Type: "int", Default: "+5"},
		"默认值有前导零":       {Name: "count", Type: "int", Default: "007"},
		"默认值有大写字母":      {Name: "host", Type: "hostname", Default: "Node01"},
		"默认值不是规范的IPv6":  {Name: "gw", Type: "ip", Default: "2001:DB8::1"},
		"默认值首尾有空白":      {Name: "pkg", Type: "word", Default: " vim"},
		"列表默认值有空行":      {Name: "pkgs", Type: "word", List: true, Default: "vim\n\ncurl"},
		"列表默认值以换行结尾":    {Name: "pkgs", Type: "word", List: true, Default: "vim\n"},
		"text默认值不是UTF8": {Name: "motd", Type: "text", Default: "\xff"},

		"enum选项值有空格":         {Name: "profile", Type: "enum", Options: []Option{{"server std", "服务器"}}},
		"enum选项值为空":          {Name: "profile", Type: "enum", Options: []Option{{"", "空"}}},
		"enum选项值带引号":         {Name: "profile", Type: "enum", Options: []Option{{`a"b`, "引号"}}},
		"enum选项值重复":          {Name: "profile", Type: "enum", Options: []Option{{"server", "服务器"}, {"server", "又一个"}}},
		"enum同时给了静态选项和ISO集合": {Name: "edition", Type: "enum", Options: opts, Offer: "editions"},
		"enum两样都没给":          {Name: "edition", Type: "enum"},
		"ISO集合名不是标识符":        {Name: "edition", Type: "enum", Offer: "Editions"},
		"非enum给了静态选项":        {Name: "pkg", Type: "word", Options: opts},
		"非enum给了ISO集合":       {Name: "pkg", Type: "word", Offer: "editions"},
		"非int给了下限":           {Name: "pkg", Type: "word", Min: int64p(1)},
		"非int给了上限":           {Name: "pkg", Type: "word", Max: int64p(1)},
		"下限大于上限":             {Name: "vlan", Type: "int", Min: int64p(10), Max: int64p(1)},
	}
	for name, d := range cases {
		probs := d.Validate()
		if len(probs) != 1 || probs[0].Code != "param_decl_invalid" {
			t.Errorf("%s：应恰好报一条 param_decl_invalid，得到 %+v", name, probs)
			continue
		}
		if !strings.Contains(probs[0].Message, "「"+d.Name+"」") {
			t.Errorf("%s：说明里应点出参数名 %q：%s", name, d.Name, probs[0].Message)
		}
	}
}

func TestEnvName(t *testing.T) {
	for param, want := range map[string]string{
		"disk":     "LOBER_PARAM_DISK",
		"ip_addr2": "LOBER_PARAM_IP_ADDR2",
		"a":        "LOBER_PARAM_A",
		// 下划线原样保留，disk_1 与 disk1 不会撞成同一个变量。
		"disk_1": "LOBER_PARAM_DISK_1",
		"disk1":  "LOBER_PARAM_DISK1",
	} {
		if got := EnvName(param); got != want {
			t.Errorf("EnvName(%q) = %q，应为 %q", param, got, want)
		}
	}
}

// 哪种实现的正文要展开只有 Impl.Expanded 一份（C-2）：片段写进应答文件，要展开；脚本原样下发，参数经
// 环境变量传入。种类不认识的按片段算，多要几个必填，好过把空值渲染进应答文件。
func TestImplExpanded(t *testing.T) {
	cases := map[ImplKind]bool{ImplFragment: true, ImplScript: false, "": true, "macro": true, "Script": true}
	for kind, want := range cases {
		if got := (Impl{Kind: kind}).Expanded(); got != want {
			t.Errorf("种类为 %q 的实现 Expanded() = %v，应为 %v", kind, got, want)
		}
	}
}

// 「选项值是互不重复的安全词，显示名过 ValidLabel」只有 validOptions 一份（C-8）：enum 的静态选项与 ISO
// 提供的集合都按它查，同一组选项在两处的结论相同。空的一组本身算合格：enum 的静态选项不能为空，由声明
// 另外查；ISO 提供的空集合等同于没提供。
func Test选项的规则只有一份(t *testing.T) {
	cases := []struct {
		name string
		opts []Option
		ok   bool
	}{
		{"合法", []Option{{"server", "服务器"}, {"minimal", "最小化"}}, true},
		{"空的一组", nil, true},
		{"值不是安全词", []Option{{"server std", "服务器"}}, false},
		{"值为空", []Option{{"", "空"}}, false},
		{"值重复", []Option{{"server", "服务器"}, {"server", "又一个"}}, false},
		{"显示名为空", []Option{{"server", ""}}, false},
		{"显示名含零宽空格", []Option{{"server", "服务" + string(rune(0x200b)) + "器"}}, false},
	}
	for _, c := range cases {
		if got := validOptions(c.opts); got != c.ok {
			t.Errorf("%s：validOptions = %v，应为 %v", c.name, got, c.ok)
		}
		iso := completeISO()
		iso.Offers = map[string][]Option{"editions": c.opts}
		if got := len(iso.Validate()) == 0; got != c.ok {
			t.Errorf("%s：作为 ISO 提供的集合，记录合法 = %v，应为 %v：%v", c.name, got, c.ok, iso.Validate())
		}
		if len(c.opts) == 0 {
			continue
		}
		d := ParamDecl{Name: "profile", Type: "enum", Options: c.opts}
		if got := len(d.Validate()) == 0; got != c.ok {
			t.Errorf("%s：作为 enum 的静态选项，声明合法 = %v，应为 %v：%v", c.name, got, c.ok, d.Validate())
		}
	}
}

// —— 第 1 层修补轮（plans/2026-10-02-layer1-fixups.md 任务 U2），括号里是遗留问题清单的条目 ——

// Valid 只认十种类型的原样写法（T7-2）：第 2 层建表的 CHECK 约束、第 5 层保存时的拒收都用它，
// 不另抄一份类型表。
func TestParamTypeValid只认十种类型(t *testing.T) {
	for _, typ := range paramTypes {
		if !typ.Valid() {
			t.Errorf("%s.Valid() 应为 true", typ)
		}
	}
	for _, typ := range []ParamType{"", "Word", "TEXT", "string", "float", " word", "word "} {
		if typ.Valid() {
			t.Errorf("不认识的类型 %q 的 Valid() 应为 false", typ)
		}
	}
}

// 显示名给人看（T13-10）：参数的可以留空，参数求值时回落到参数名；选项的不能，否则向导的下拉框里
// 这一项没有文字。非空的显示名都要通过 ValidLabel，它的规则由 ident_test 逐条测；这里测 Validate
// 对两种显示名都查了，不合格时恰好报一条 param_decl_invalid。
func TestParamDeclValidate查显示名(t *testing.T) {
	enumLabeled := func(label string) ParamDecl {
		return ParamDecl{Name: "profile", Type: "enum", Options: []Option{{"server", "服务器"}, {"minimal", label}}}
	}
	for name, d := range map[string]ParamDecl{
		"参数显示名留空":   {Name: "swap_mb", Type: "int"},
		"参数显示名用中文":  {Name: "swap_mb", Label: "交换分区（MiB）", Type: "int"},
		"参数显示名64个字": {Name: "swap_mb", Label: strings.Repeat("中", 64), Type: "int"},
		"选项显示名用中文":  enumLabeled("最小化"),
		"选项显示名64个字": enumLabeled(strings.Repeat("中", 64)),
	} {
		if probs := d.Validate(); len(probs) != 0 {
			t.Errorf("%s：声明应当合法，得到 %+v", name, probs)
		}
	}

	// 每一例只有显示名不合格。不可见的字符写成转义，免得编辑器悄悄改掉它们。
	badLabels := map[string]string{
		"含控制字符":     "交换\x07分区",
		"含零宽空格":     "交换\U0000200b分区",
		"开头是空格":     " 交换分区",
		"结尾是空格":     "交换分区 ",
		"有65个字":     strings.Repeat("中", 65),
		"不是合法的UTF8": "交换\xff分区",
	}
	cases := map[string]ParamDecl{"选项显示名为空": enumLabeled("")}
	for what, label := range badLabels {
		cases["参数显示名"+what] = ParamDecl{Name: "swap_mb", Label: label, Type: "int"}
		cases["选项显示名"+what] = enumLabeled(label)
	}
	for name, d := range cases {
		probs := d.Validate()
		if len(probs) != 1 || probs[0].Code != "param_decl_invalid" || !strings.Contains(probs[0].Message, "「"+d.Name+"」") {
			t.Errorf("%s：应恰好报一条点出参数名的 param_decl_invalid，得到 %+v", name, probs)
		}
	}
}

// 公钥主体（base64 解开后的 blob）不超过 16384 字节，即 OpenSSH 的 SSH_MAX_PUBKEY_BYTES（T6-2）。
// 用 ssh-rsa 凑：blob 是算法名、e、n 三个字段，加长 n 凑出总字节数，两例只差 n 的一个字节。
func TestParse公钥主体不超过16384字节(t *testing.T) {
	head := len(sshBlobBytes("ssh-rsa", "\x01\x00\x01", "")) // 三个字段的长度头，加上算法名与 e
	keyOfBlob := func(size int) string {
		blob := sshBlobBytes("ssh-rsa", "\x01\x00\x01", strings.Repeat("n", size-head))
		if len(blob) != size {
			t.Fatalf("测试自己拼的 blob 有 %d 字节，应为 %d", len(blob), size)
		}
		return "ssh-rsa " + base64.StdEncoding.EncodeToString(blob)
	}
	key := ParamDecl{Name: "key", Type: "sshkey"}
	if at := keyOfBlob(16384); mustParse(t, key, at).Canonical() != at {
		t.Error("16384 字节的公钥主体应原样收下")
	}
	// 列表逐项查，一项超限整个列表都拒收。
	for _, d := range []ParamDecl{key, {Name: "keys", Type: "sshkey", List: true}} {
		_, err := d.Parse(keyOfBlob(16385))
		if err == nil {
			t.Errorf("%s：16385 字节的公钥主体应拒收", d.Name)
			continue
		}
		if p := asProblem(t, err); p.Code != "param_value_invalid" {
			t.Errorf("%s：16385 字节的公钥主体报的是 %s，应为 param_value_invalid", d.Name, p.Code)
		}
	}
}

// 口令散列不超过 256 字节（T6-2）：最长的 yescrypt 散列也远小于它，再长的只会是写错了。
func TestParse口令散列不超过256字节(t *testing.T) {
	ofLen := func(n int) string {
		head := "$6$Xy7.ab/Q$"
		return head + strings.Repeat("h", n-len(head))
	}
	d := ParamDecl{Name: "root_pw", Type: "pwhash"}
	if at := ofLen(256); mustParse(t, d, at).Canonical() != at {
		t.Error("256 字节的口令散列应原样收下")
	}
	_, err := d.Parse(ofLen(257))
	if err == nil {
		t.Fatal("257 字节的口令散列应拒收")
	}
	if p := asProblem(t, err); p.Code != "param_value_invalid" {
		t.Errorf("257 字节的口令散列报的是 %s，应为 param_value_invalid", p.Code)
	}
}

// 列表的规范形式（各项以换行连接）合计不超过 32767 字节（T6-2）：脚本经环境变量拿到的就是它，
// Windows 单个环境变量最多 32767 个字符。用 130 项主机名凑，项数远不到 256，拦下它的只能是总长。
func TestParse列表规范形式合计不超过32767字节(t *testing.T) {
	long := strings.Repeat(strings.Repeat("h", 63)+".", 3) + strings.Repeat("h", 61) // 253 个字符，最长的主机名
	itemsOfSize := func(size int) []string {
		n := (size - 1) / (len(long) + 1) // 前面 n 项都用最长的主机名，每项连同换行占 254 字节
		items := append(slices.Repeat([]string{long}, n), strings.Repeat("h", size-n*(len(long)+1)))
		if got := len(strings.Join(items, "\n")); got != size {
			t.Fatalf("测试自己拼的列表合计 %d 字节，应为 %d", got, size)
		}
		return items
	}
	d := ParamDecl{Name: "ntp", Type: "hostname", List: true}
	at := strings.Join(itemsOfSize(32767), "\n")
	if v := mustParse(t, d, at); v.Canonical() != at || len(v.Items()) != 130 {
		t.Errorf("合计 32767 字节的列表应原样收下，得到 %d 项、%d 字节", len(v.Items()), len(v.Canonical()))
	}
	// 数的是规范形式：原样输入里的空行、\r\n 和每项首尾的空白都不算。
	padded := "\n  " + strings.Join(itemsOfSize(32767), " \r\n\n\t") + "\r\n"
	if v := mustParse(t, d, padded); v.Canonical() != at {
		t.Errorf("去掉空行与空白后合计 32767 字节的列表应收下，规范形式却有 %d 字节", len(v.Canonical()))
	}
	_, err := d.Parse(strings.Join(itemsOfSize(32768), "\n"))
	if err == nil {
		t.Fatal("合计 32768 字节的列表应拒收")
	}
	if p := asProblem(t, err); p.Code != "param_value_invalid" {
		t.Errorf("合计 32768 字节的列表报的是 %s，应为 param_value_invalid", p.Code)
	}
}

// int 声明了上下限时，说明里带出具体的范围（T5-4①）：只说「在范围之内」，操作员不知道该填多少。
// 范围是期望格式的一部分，填的不是整数时也带上。措辞取 codes.go 登记的 intRange*，数字原样填入。
func TestParse的int说明带出具体范围(t *testing.T) {
	intFormat := paramValueFormats["int"]
	cases := []struct {
		name   string
		decl   ParamDecl
		bad    []string
		format string   // 说明里期望的格式
		nums   []string // 说明里必须出现的数字
	}{
		{"不限", ParamDecl{Name: "count", Type: "int"}, []string{"abc"}, intFormat, nil},
		{"只有下限", ParamDecl{Name: "mtu", Type: "int", Min: int64p(576)}, []string{"575", "abc"},
			intFormat + fmt.Sprintf(intRangeMin, int64(576)), []string{"576"}},
		{"只有上限", ParamDecl{Name: "offset", Type: "int", Max: int64p(-15)}, []string{"-14", "abc"},
			intFormat + fmt.Sprintf(intRangeMax, int64(-15)), []string{"-15"}},
		{"两端都有", ParamDecl{Name: "vlan", Type: "int", Min: int64p(1), Max: int64p(4094)}, []string{"0", "4095", "abc"},
			intFormat + fmt.Sprintf(intRangeBoth, int64(1), int64(4094)), []string{"1", "4094"}},
	}
	for _, c := range cases {
		for _, raw := range c.bad {
			_, err := c.decl.Parse(raw)
			if err == nil {
				t.Errorf("%s：%q 应拒收", c.name, raw)
				continue
			}
			p := asProblem(t, err)
			if want := NewProblem("param_value_invalid", Subject{}, c.decl.Name, c.format); p != want {
				t.Errorf("%s：%q 报的是 %+v，应为 %+v", c.name, raw, p, want)
			}
			for _, n := range c.nums {
				if !strings.Contains(p.Message, n) {
					t.Errorf("%s：说明里应出现 %s：%s", c.name, n, p.Message)
				}
			}
		}
	}
}

// 参数求值面向操作员，经 ParseNamed 传显示名（T11-8），说明里的称呼才与向导的列头一致。除了称呼，
// 它与 Parse 完全相同：同样的取值得到同样的结果，同样的错误只差那个称呼。
func TestParseNamed在说明里用传入的称呼(t *testing.T) {
	const label = "交换分区（MiB）"
	for _, c := range typeCases() {
		for raw := range c.ok {
			want := mustParse(t, c.decl, raw)
			got, err := c.decl.ParseNamed(label, raw)
			if err != nil || got.Canonical() != want.Canonical() || !slices.Equal(got.Items(), want.Items()) {
				t.Errorf("%s：ParseNamed(%q) 得到 %q（%v），Parse 得到 %q", c.decl.Name, raw, got.Items(), err, want.Items())
			}
		}
		for _, raw := range c.bad {
			_, perr := c.decl.Parse(raw)
			_, err := c.decl.ParseNamed(label, raw)
			if err == nil {
				t.Errorf("%s：ParseNamed(%q) 应拒收", c.decl.Name, raw)
				continue
			}
			want, got := asProblem(t, perr), asProblem(t, err)
			want.Message = strings.Replace(want.Message, "「"+c.decl.Name+"」", "「"+label+"」", 1)
			if got != want {
				t.Errorf("%s：ParseNamed(%q) 报的是 %+v，应为 %+v", c.decl.Name, raw, got, want)
			}
		}
	}

	// 列表里有一项超出范围时也一样：称呼换成传入的，范围照样带出。
	vlans := ParamDecl{Name: "vlans", Type: "int", List: true, Min: int64p(1), Max: int64p(4094)}
	_, err := vlans.ParseNamed(label, "100\n5000")
	if p := asProblem(t, err); !strings.Contains(p.Message, "「"+label+"」") || strings.Contains(p.Message, "vlans") || !strings.Contains(p.Message, "4094") {
		t.Errorf("列表的说明应以 %s 称呼参数并带出范围：%s", label, p.Message)
	}

	// 声明本身不成立是写给作者的，照 codes.go 的约定仍用参数名。
	d := ParamDecl{Name: "size", Label: "大小", Type: "float"}
	_, err = d.ParseNamed(d.Label, "1")
	if p := asProblem(t, err); p != NewProblem("param_decl_invalid", Subject{}, "size") {
		t.Errorf("类型不认识时应报点出参数名的 param_decl_invalid，得到 %+v", p)
	}
}

// OfferMissing 是 iso_offer_missing 唯一的一份条件（L-2）：参数求值与准入判定都调用它，向导里列出的
// 候选与提交时的结果才对得上。空集合也算没提供：一个选项都没有，向导里无从选起。
func TestOfferMissing(t *testing.T) {
	edition := ParamDecl{Name: "edition", Type: "enum", Offer: "editions"}
	editions := []Option{{"ServerStandard", "Standard"}, {"ServerDatacenter", "Datacenter"}}
	cases := []struct {
		name   string
		decl   ParamDecl
		offers map[string][]Option
		want   bool
	}{
		{"ISO 什么集合都没提供", edition, nil, true},
		{"ISO 没提供这个集合", edition, map[string][]Option{"images": editions}, true},
		{"集合名大小写不同不算同一个", edition, map[string][]Option{"Editions": editions}, true},
		{"集合为空", edition, map[string][]Option{"editions": {}}, true},
		{"集合是 nil", edition, map[string][]Option{"editions": nil}, true},
		{"集合有值", edition, map[string][]Option{"editions": editions}, false},
		{"不是 enum", ParamDecl{Name: "edition", Type: "word", Offer: "editions"}, nil, false},
		{"enum 没声明 Offer", ParamDecl{Name: "profile", Type: "enum", Options: editions}, nil, false},
		{"别的类型也没声明 Offer", ParamDecl{Name: "pkg", Type: "word"}, nil, false},
	}
	for _, c := range cases {
		if got := c.decl.OfferMissing(c.offers); got != c.want {
			t.Errorf("%s：OfferMissing = %v，应为 %v", c.name, got, c.want)
		}
	}
}

// MissingOffers 按声明顺序列出缺的集合名，同一个集合只列一次：准入判定为每个缺的集合各报一条。
func TestMissingOffers按声明顺序列出且不重复(t *testing.T) {
	langs := []Option{{"zh-CN", "简体中文"}}
	decls := []ParamDecl{
		{Name: "image", Type: "enum", Offer: "images"},
		{Name: "lang", Type: "enum", Offer: "langs"},
		{Name: "edition", Type: "enum", Offer: "editions"},
		{Name: "pkg", Type: "word"},
		{Name: "image2", Type: "enum", Offer: "images"},
		{Name: "profile", Type: "enum", Options: langs},
		{Name: "keyboard", Type: "enum", Offer: "keyboards"},
	}
	// 按字典序会是 editions、images、keyboards；按声明顺序 images 在前。
	offers := map[string][]Option{"langs": langs, "keyboards": {}}
	if got, want := MissingOffers(decls, offers), []string{"images", "editions", "keyboards"}; !slices.Equal(got, want) {
		t.Errorf("MissingOffers = %q，应为 %q", got, want)
	}
	full := map[string][]Option{"images": langs, "langs": langs, "editions": langs, "keyboards": langs}
	if got := MissingOffers(decls, full); len(got) != 0 {
		t.Errorf("集合都提供了，MissingOffers 应为空，得到 %q", got)
	}
	if got := MissingOffers(nil, nil); len(got) != 0 {
		t.Errorf("没有声明时 MissingOffers 应为空，得到 %q", got)
	}
}
