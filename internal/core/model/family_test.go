package model

import "testing"

// TestFamily_Valid 验证族类型的合法性校验：三个已知值合法，空串与拼错的值
// 不合法（计划任务 3 设计要点：族没有合理默认值，猜一个会让 ISO 落到错的
// 分组里，未知值不能静默接受）。
func TestFamily_Valid(t *testing.T) {
	cases := []struct {
		family Family
		want   bool
	}{
		{FamilyLinux, true},
		{FamilyWindows, true},
		{FamilyESXi, true},
		{Family(""), false}, // 空串不合法——不存在，不是「未指定」
		{Family("Linux"), false},
		{Family("winodws"), false}, // 拼错
	}
	for _, c := range cases {
		if got := c.family.Valid(); got != c.want {
			t.Errorf("Family(%q).Valid() = %v, want %v", c.family, got, c.want)
		}
	}
}

// TestFamily_ValidRejectsOSFamilyValue 验证把 OSFamily 的字面值当 Family
// 传入会被拒绝——两者底层都是 string，类型系统不会替我们挡住这个错误
// （团队 lead 指出的风险点：不能让 Family 与 OSFamily 在类型上可互换）。
func TestFamily_ValidRejectsOSFamilyValue(t *testing.T) {
	// "rhel"/"debian"/"ubuntu"/"suse" 是 OSFamily 的合法值，但不是 Family
	// 的合法值——族只有三个值（linux/windows/esxi），"rhel" 从未出现在其中。
	osFamilyValuesMisusedAsFamily := []OSFamily{
		OSFamilyRHEL, OSFamilyDebian, OSFamilyUbuntu, OSFamilySUSE,
	}
	for _, osf := range osFamilyValuesMisusedAsFamily {
		misused := Family(osf) // 显式转换才能编译通过，模拟调用方传错类型的场景
		if misused.Valid() {
			t.Errorf("Family(%q).Valid() = true, want false —— "+
				"这是 OSFamily 的值而不是 Family 的值，误传必须被 Valid 挡住", osf)
		}
	}

	// "windows" 与 "esxi" 是两个类型的共同取值，这两个值在 Family 下应当合法
	// （因为它们恰好也是合法的族名），但这不代表类型可互换——只是取值集合
	// 有交集，OSFamilyWindows 与 FamilyWindows 底层字符串相同是设计使然。
	if !Family(OSFamilyWindows).Valid() {
		t.Error(`Family(OSFamilyWindows).Valid() = false, want true —— ` +
			`"windows" 恰好同时是合法的族名与合法的家族名，这是取值交集不是类型混淆`)
	}
}

// TestFamilyOf 验证六个已知家族到三个族的映射，以及未知家族返回 ok=false
// （计划任务 3 契约：rhel/debian/ubuntu/suse → linux，windows → windows，
// esxi → esxi）。
func TestFamilyOf(t *testing.T) {
	cases := []struct {
		os     OSFamily
		want   Family
		wantOK bool
	}{
		{OSFamilyRHEL, FamilyLinux, true},
		{OSFamilyDebian, FamilyLinux, true},
		{OSFamilyUbuntu, FamilyLinux, true},
		{OSFamilySUSE, FamilyLinux, true},
		{OSFamilyWindows, FamilyWindows, true},
		{OSFamilyESXi, FamilyESXi, true},
		{OSFamily("plan9"), Family(""), false},
	}
	for _, c := range cases {
		got, ok := FamilyOf(c.os)
		if ok != c.wantOK || got != c.want {
			t.Errorf("FamilyOf(%q) = (%q, %v), want (%q, %v)", c.os, got, ok, c.want, c.wantOK)
		}
	}
}

// TestFamilyAllows 验证族与家族的归属判定：合法族×允许家族为真，合法族×
// 不允许家族为假（linux 族不允许 windows 家族），任一非法值都返回假
// （计划任务 3 契约：族不能顶替家族，linux 族内四家是四种不同文件种类）。
func TestFamilyAllows(t *testing.T) {
	cases := []struct {
		name   string
		family Family
		os     OSFamily
		want   bool
	}{
		{"linux 族允许 rhel", FamilyLinux, OSFamilyRHEL, true},
		{"linux 族允许 debian", FamilyLinux, OSFamilyDebian, true},
		{"linux 族允许 ubuntu", FamilyLinux, OSFamilyUbuntu, true},
		{"linux 族允许 suse", FamilyLinux, OSFamilySUSE, true},
		{"windows 族允许 windows", FamilyWindows, OSFamilyWindows, true},
		{"esxi 族允许 esxi", FamilyESXi, OSFamilyESXi, true},

		// 族不能顶替家族：linux 族内四家是四种不同的应答文件格式（kickstart
		// 行式加 %section、preseed 是 debconf 键值、autoinstall 是 YAML、
		// autoyast 是 XML），没有任何写法能让同一段文本同时合法。
		{"linux 族不允许 windows", FamilyLinux, OSFamilyWindows, false},
		{"linux 族不允许 esxi", FamilyLinux, OSFamilyESXi, false},
		{"windows 族不允许 rhel", FamilyWindows, OSFamilyRHEL, false},
		{"esxi 族不允许 rhel", FamilyESXi, OSFamilyRHEL, false},

		{"非法族", Family("plan9"), OSFamilyRHEL, false},
		{"非法家族", FamilyLinux, OSFamily("plan9"), false},
		{"两者皆非法", Family(""), OSFamily(""), false},
	}
	for _, c := range cases {
		if got := FamilyAllows(c.family, c.os); got != c.want {
			t.Errorf("%s: FamilyAllows(%q, %q) = %v, want %v", c.name, c.family, c.os, got, c.want)
		}
	}
}

// TestFamily_OSFamilies_Content 验证每个族返回的家族集合内容正确
// （计划任务 3 契约：linux → 四家，windows/esxi 各一家）。
func TestFamily_OSFamilies_Content(t *testing.T) {
	cases := []struct {
		family Family
		want   []OSFamily
	}{
		{FamilyLinux, []OSFamily{OSFamilyRHEL, OSFamilyDebian, OSFamilyUbuntu, OSFamilySUSE}},
		{FamilyWindows, []OSFamily{OSFamilyWindows}},
		{FamilyESXi, []OSFamily{OSFamilyESXi}},
		{Family("plan9"), nil}, // 非法族返回空
	}
	for _, c := range cases {
		got := c.family.OSFamilies()
		if len(got) != len(c.want) {
			t.Errorf("Family(%q).OSFamilies() = %v (len=%d), want %v (len=%d)",
				c.family, got, len(got), c.want, len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Family(%q).OSFamilies()[%d] = %q, want %q —— "+
					"顺序必须稳定，供 UI 的 tab 顺序与错误文案使用",
					c.family, i, got[i], c.want[i])
			}
		}
	}
}

// TestFamily_OSFamilies_OrderStable 验证同一族多次调用返回相同顺序
// （计划任务 3 契约：顺序稳定，不稳定会让 golden 与快照测试随机失败）。
//
// 只调一次断言内容不足以抓住「顺序不稳定」这类 bug（例如内部用 map 迭代
// 拼装切片就会在多次调用间随机排序），所以这里反复调用多次并逐一比较。
func TestFamily_OSFamilies_OrderStable(t *testing.T) {
	const iterations = 20
	first := FamilyLinux.OSFamilies()
	for i := 0; i < iterations; i++ {
		again := FamilyLinux.OSFamilies()
		if len(again) != len(first) {
			t.Fatalf("第 %d 次调用长度 = %d, 首次 = %d，顺序不可能稳定", i, len(again), len(first))
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("第 %d 次调用第 %d 项 = %q，首次同位置 = %q —— "+
					"OSFamilies 的顺序必须在多次调用之间保持一致", i, j, again[j], first[j])
			}
		}
	}
}

// TestFamily_OSFamilies_ReturnsFreshSlice 验证返回的切片是独立分配的，
// 调用方修改它不会影响下一次调用的结果（计划任务 3 契约：返回新分配的
// 切片，调用方增删不影响内部状态）。
//
// 只调一次拿到切片再检查内容，不足以抓住「共享底层数组」这类 bug——必须
// 真的修改返回值之后再调用一次，验证内部状态没被污染。
func TestFamily_OSFamilies_ReturnsFreshSlice(t *testing.T) {
	got := FamilyLinux.OSFamilies()
	if len(got) == 0 {
		t.Fatal("FamilyLinux.OSFamilies() 返回空切片，无法验证隔离性")
	}

	// 篡改返回的切片：改内容、并 append 扩容触及底层数组。
	original := got[0]
	got[0] = OSFamily("tampered")
	got = append(got, OSFamily("extra-injected"))

	again := FamilyLinux.OSFamilies()
	if len(again) != 4 {
		t.Fatalf("篡改返回值后再次调用，长度 = %d, want 4 —— "+
			"说明内部状态被污染（返回的切片与内部共享底层数组）", len(again))
	}
	if again[0] != original {
		t.Errorf("篡改返回值后再次调用，第 0 项 = %q, want %q（原始值）—— "+
			"说明返回的切片与内部状态共享底层数组，不是独立分配的", again[0], original)
	}
}
