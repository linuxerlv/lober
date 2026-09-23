package model

import "testing"

// TestBaseURLs_JoinFourCombinations 验证 URL 拼接对基址与路径各自带/不带
// 斜杠的四种组合都产出正确结果（计划任务 9 测试要求）。
func TestBaseURLs_JoinFourCombinations(t *testing.T) {
	cases := []struct {
		name string
		base string
		seg  string
		want string
	}{
		{"base无尾斜杠_seg无头斜杠", "http://gw", "v1/boot", "http://gw/v1/boot"},
		{"base有尾斜杠_seg无头斜杠", "http://gw/", "v1/boot", "http://gw/v1/boot"},
		{"base无尾斜杠_seg有头斜杠", "http://gw", "/v1/boot", "http://gw/v1/boot"},
		{"base有尾斜杠_seg有头斜杠", "http://gw/", "/v1/boot", "http://gw/v1/boot"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := BaseURLs{GatewayBase: c.base}
			got := b.GatewayURL(c.seg)
			if got != c.want {
				t.Errorf("GatewayURL(%q) with base=%q = %q, want %q —— "+
					"拼接函数必须规范化重复斜杠而不产出双斜杠或缺失斜杠",
					c.seg, c.base, got, c.want)
			}
		})
	}
}

// TestBaseURLs_TrailingSlashPreserved 单独锁住结尾斜杠被保留这条行为。
//
// Ubuntu 的 ds=nocloud-net;s=<url> 要求 URL 是目录且结尾带 /（SPEC §4.4），
// 拼接函数吃掉它会导致 cloud-init 取不到应答文件——这个故障的现场表现是
// 「机器装到一半停住」，极难定位到一个斜杠。单独一条测试因为它对应一个
// 真实且极难排查的故障（计划任务 9 明确要求）。
func TestBaseURLs_TrailingSlashPreserved(t *testing.T) {
	b := BaseURLs{GatewayBase: "http://gw"}
	got := b.GatewayURL("v1/answer/42/tok123/")
	want := "http://gw/v1/answer/42/tok123/"
	if got != want {
		t.Errorf("GatewayURL 拼接结果 = %q, want %q —— "+
			"结尾斜杠被吃掉会导致 Ubuntu 的 ds=nocloud-net;s=<url> 取不到应答文件目录，"+
			"现场表现是机器装到一半停住，极难定位到是这一个斜杠的问题（SPEC §4.4）",
			got, want)
	}

	gotNoTrailing := b.GatewayURL("v1/boot")
	wantNoTrailing := "http://gw/v1/boot"
	if gotNoTrailing != wantNoTrailing {
		t.Errorf("GatewayURL(无结尾斜杠输入) = %q, want %q —— "+
			"不应凭空给不需要结尾斜杠的路径加上斜杠", gotNoTrailing, wantNoTrailing)
	}
}

// TestBaseURLs_SchemeFromConfig 验证 scheme 完全来自配置（传 https:// 进去
// 得到 https:// 出来），SPEC §1.5 要求代码里不出现字面 http://。
func TestBaseURLs_SchemeFromConfig(t *testing.T) {
	b := BaseURLs{GatewayBase: "https://gw.internal", ArtifactBase: "https://ng.internal"}

	gotGW := b.GatewayURL("v1/boot")
	if gotGW != "https://gw.internal/v1/boot" {
		t.Errorf("GatewayURL 用 https 基址拼出 %q，want 保留 https:// scheme —— "+
			"scheme 必须完全来自配置：代码若硬编码 http:// 就把 URL 形态变成代码决定"+
			"（SPEC §1.5；人面已上 HTTPS，机器面永久明文，见 docs/tls-与安全启动-范围决定.md）",
			gotGW)
	}

	gotArtifact := b.ArtifactURL("rocky9.4/vmlinuz")
	if gotArtifact != "https://ng.internal/artifacts/rocky9.4/vmlinuz" {
		t.Errorf("ArtifactURL 用 https 基址拼出 %q，want 保留 https:// scheme 并带 /artifacts/ 前缀（§1.4）",
			gotArtifact)
	}
}

// TestNormalizeMAC_ThreeFormatsProduceSameResult 验证 MAC 归一化对大写
// 冒号、小写连字符、无分隔三种输入产出同一结果（SPEC §4.3，计划任务 9
// 测试要求）。
func TestNormalizeMAC_ThreeFormatsProduceSameResult(t *testing.T) {
	want := "aa:bb:cc:dd:ee:ff"
	inputs := []string{
		"AA:BB:CC:DD:EE:FF", // 大写冒号（discovery 导入可能的格式）
		"aa-bb-cc-dd-ee-ff", // 小写连字符
		"aabbccddeeff",      // 无分隔
	}
	for _, raw := range inputs {
		got := NormalizeMAC(raw)
		if got != want {
			t.Errorf("NormalizeMAC(%q) = %q, want %q —— "+
				"三种常见输入格式必须归一化为同一结果，否则同一台机器会因为"+
				"discovery 与引导时上报格式不同而在身份匹配时对不上（SPEC §4.3）",
				raw, got, want)
		}
	}
}

// TestNormalizeMAC_GarbageReturnsEmpty 验证对垃圾输入返回空串——这是安全
// 方向：空 MAC 在 §4.3 的双因子校验里会被当作对不上，宁可多一次人工介入，
// 不要错装（计划任务 9 测试要求）。
func TestNormalizeMAC_GarbageReturnsEmpty(t *testing.T) {
	garbageInputs := []string{
		"",
		"not-a-mac",
		"aa:bb:cc:dd:ee",       // 太短
		"aa:bb:cc:dd:ee:ff:gg", // 太长
		"gg:hh:ii:jj:kk:ll",    // 非十六进制字符
	}
	for _, raw := range garbageInputs {
		got := NormalizeMAC(raw)
		if got != "" {
			t.Errorf("NormalizeMAC(%q) = %q, want \"\" —— "+
				"垃圾输入必须返回空串而不是猜测性地拼出一个看起来合法的值，"+
				"空 MAC 在双因子校验里被当作对不上是刻意的安全方向（SPEC §4.3）",
				raw, got)
		}
	}
}

// TestAnswerFileName_SixOSFamilies 验证六个 OS 家族的应答文件名，特别是
// suse 不等于 ks.cfg（SPEC §5.1、附录 C；计划任务 9 明确强调的危险点）。
func TestAnswerFileName_SixOSFamilies(t *testing.T) {
	cases := []struct {
		family OSFamily
		want   string
	}{
		{OSFamilyRHEL, "ks.cfg"},
		{OSFamilyUbuntu, "user-data"},
		{OSFamilyDebian, "preseed.cfg"},
		{OSFamilySUSE, "autoinst.xml"},
		{OSFamilyWindows, "unattend.xml"},
		{OSFamilyESXi, "boot.cfg"},
	}
	for _, c := range cases {
		got, ok := AnswerFileName(c.family)
		if !ok {
			t.Fatalf("AnswerFileName(%q) 查不到文件名，want %q", c.family, c.want)
		}
		if got != c.want {
			t.Errorf("AnswerFileName(%q) = %q, want %q —— "+
				"应答文件名错了会导致 installer 拿到的文件名与内核参数指向的不一致，"+
				"装机在取文件那一步直接失败", c.family, got, c.want)
		}
	}

	if got, _ := AnswerFileName(OSFamilySUSE); got == "ks.cfg" {
		t.Fatal("AnswerFileName(suse) 返回了 ks.cfg —— " +
			"SUSE 用 AutoYaST 不是 kickstart，引导参数是 autoyast= 而非 inst.ks=，" +
			"格式是独立的 XML，语法完全不同，产出的文件安装器根本不认")
	}
}

// TestAnswerFileName_UnknownFamilyRejected 验证未知家族查不到文件名。
func TestAnswerFileName_UnknownFamilyRejected(t *testing.T) {
	if _, ok := AnswerFileName(OSFamily("plan9")); ok {
		t.Error("AnswerFileName(未知家族) 返回 ok=true，want false —— 未知家族不应被静默接受")
	}
}

// TestRenderContext_AnswerFileName 验证 RenderContext 的便捷方法与直接调用
// AnswerFileName(Artifact.OSFamily) 结果一致。
func TestRenderContext_AnswerFileName(t *testing.T) {
	ctx := RenderContext{Artifact: ArtifactSet{OSFamily: OSFamilySUSE}}
	got, ok := ctx.AnswerFileName()
	if !ok || got != "autoinst.xml" {
		t.Errorf("RenderContext{Artifact.OSFamily=suse}.AnswerFileName() = (%q, %v), want (\"autoinst.xml\", true)",
			got, ok)
	}
}

// TestRenderContext_RootSSHPublicKeysField 锁住本轮新增字段的存在与直接
// 赋值可用——RenderContext 需要一个独立字段承载"要注入的 SSH 公钥文本"
// （SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码登录），不塞进
// Facts——fact 命名空间是面向 hook 的对外 API（§5.4：一旦有数十个 hook 在
// 用就不能改名，只增不删），把渲染器内部要用的东西塞进去会永久绑住。
func TestRenderContext_RootSSHPublicKeysField(t *testing.T) {
	ctx := RenderContext{
		RootSSHPublicKeys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
		},
	}
	if len(ctx.RootSSHPublicKeys) != 1 {
		t.Fatalf("RenderContext.RootSSHPublicKeys 长度 = %d, want 1", len(ctx.RootSSHPublicKeys))
	}
	if ctx.RootSSHPublicKeys[0] != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober" {
		t.Errorf("RenderContext.RootSSHPublicKeys[0] = %q，字段值与赋值不一致", ctx.RootSSHPublicKeys[0])
	}
}
