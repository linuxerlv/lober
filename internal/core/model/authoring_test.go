package model

import "testing"

// TestHookDigest_MatchesSHA256Sum 验证 digest 等于
// `printf '<正文>' | sha256sum` 的输出。用一个具体正文与预先算好的 64 位
// 十六进制值比对（该值用 Python 的 hashlib.sha256 独立算出，与实现逐字节
// 对比），并验证尾部多一个空格即得到不同 digest（SPEC §6.3，计划任务 7
// 测试要求）。
func TestHookDigest_MatchesSHA256Sum(t *testing.T) {
	body := "#!/bin/bash\necho hello lober\n"
	// 用 printf '%s' "$body" | sha256sum 独立算出（已用 Python hashlib 复核）。
	want := "c8c5128875fd8bc1bc22dcede7b31039a2b5419abb48530cb9084ee1aab8ce32"

	got := HookDigest(body)
	if got != want {
		t.Errorf("HookDigest(%q) = %q, want %q —— "+
			"机器侧 run_hook 用 sha256sum -c 校验下载到的脚本（SPEC §6.3），"+
			"digest 算法一旦与 coreutils sha256sum 的输出不一致，会导致所有 hook 校验失败",
			body, got, want)
	}

	trailingSpace := body + " "
	gotTrailing := HookDigest(trailingSpace)
	if gotTrailing == got {
		t.Errorf("HookDigest 对正文末尾多一个空格的两个字符串返回了相同的 digest —— " +
			"digest 必须是正文字节的裸 sha256、不做任何规范化，" +
			"否则内容寻址的完整性保证就失去了意义（SPEC §2.2）")
	}
}

// TestHookStage_Valid 验证阶段合法性判定拒绝白名单外的值（SPEC §5.2）。
func TestHookStage_Valid(t *testing.T) {
	valid := []HookStage{HookStagePre, HookStagePostNoChroot, HookStagePost, HookStageFirstboot}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("HookStage(%q).Valid() = false, want true —— 这是四个合法阶段之一", s)
		}
	}

	invalid := HookStage("mid-install")
	if invalid.Valid() {
		t.Errorf("HookStage(%q).Valid() = true, want false —— "+
			"阶段不是排序而是能力边界，放错阶段的 hook 根本跑不起来，"+
			"白名单外的值必须被拒绝（SPEC §5.2）", invalid)
	}
}

// TestHook_SupportsFamily 验证 hook 的家族支持判定，含空列表表示不限家族
// （SPEC §2.2，计划任务 7 设计要点）。
func TestHook_SupportsFamily(t *testing.T) {
	unrestricted := Hook{OSFamilies: nil}
	if !unrestricted.SupportsFamily(OSFamilyRHEL) {
		t.Error("OSFamilies 为空的 hook SupportsFamily(rhel) = false, want true —— " +
			"空列表表示不限家族，不是「不支持任何家族」，这条设计要点若判反会让所有通用 hook 失效")
	}

	restricted := Hook{OSFamilies: []OSFamily{OSFamilyRHEL, OSFamilyDebian}}
	if !restricted.SupportsFamily(OSFamilyRHEL) {
		t.Error("restricted.SupportsFamily(rhel) = false, want true —— rhel 在列表中")
	}
	if restricted.SupportsFamily(OSFamilyUbuntu) {
		t.Error("restricted.SupportsFamily(ubuntu) = true, want false —— ubuntu 不在列表中")
	}
}

// TestTemplate_HooksForStage_OrderAndFilter 验证模板取某阶段 hook 时按
// order 升序、且不含其它阶段的条目（SPEC §2.3、§5.2，计划任务 7 设计要点）。
func TestTemplate_HooksForStage_OrderAndFilter(t *testing.T) {
	tmpl := Template{
		HookRefs: []HookRef{
			{HookID: "post-b", Stage: HookStagePost, Order: 20},
			{HookID: "pre-a", Stage: HookStagePre, Order: 10},
			{HookID: "post-a", Stage: HookStagePost, Order: 5},
			{HookID: "pre-b", Stage: HookStagePre, Order: 1},
			{HookID: "firstboot-a", Stage: HookStageFirstboot, Order: 1},
		},
	}

	preHooks := tmpl.HooksForStage(HookStagePre)
	if len(preHooks) != 2 {
		t.Fatalf("HooksForStage(pre) 返回 %d 条，want 2 —— 不应混入其它阶段的条目", len(preHooks))
	}
	if preHooks[0].HookID != "pre-b" || preHooks[1].HookID != "pre-a" {
		t.Errorf("HooksForStage(pre) 顺序 = [%s, %s], want [pre-b, pre-a] —— "+
			"必须按 order 升序排列，排序逻辑收敛在这一处，若渲染器与 preview 各排一遍会不一致",
			preHooks[0].HookID, preHooks[1].HookID)
	}

	postHooks := tmpl.HooksForStage(HookStagePost)
	if len(postHooks) != 2 {
		t.Fatalf("HooksForStage(post) 返回 %d 条，want 2", len(postHooks))
	}
	if postHooks[0].HookID != "post-a" || postHooks[1].HookID != "post-b" {
		t.Errorf("HooksForStage(post) 顺序 = [%s, %s], want [post-a, post-b]",
			postHooks[0].HookID, postHooks[1].HookID)
	}

	empty := tmpl.HooksForStage(HookStagePostNoChroot)
	if len(empty) != 0 {
		t.Errorf("HooksForStage(post-nochroot) 返回 %d 条, want 0 —— 该模板没有此阶段的 hook", len(empty))
	}
}
