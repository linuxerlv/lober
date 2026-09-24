// 本文件测试 Store 的五个 HookStore 方法。对应实现计划任务 9（hook 聚合
// 的前一半），依据 SPEC §2.2、§2.6、§2.7。
package hook

import (
	"context"
	"errors"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// mkHook 构造一个可直接写库的 Hook，body 与 digest 保持一致（CreateHook
// 不像 sqlite 版那样校验这一致性——契约本身没有这条要求，本包照抄契约，
// 不额外加校验，见 store.go 的 CreateHook 文档）。
func mkHook(id string, version int, body string) model.Hook {
	return model.Hook{
		ID:          id,
		Name:        "hook-" + id,
		Stage:       model.HookStagePost,
		OSFamilies:  []model.OSFamily{model.OSFamilyDebian},
		Interpreter: "bash",
		Body:        body,
		BodySHA256:  model.HookDigest(body),
		Version:     version,
		State:       model.HookStateDraft,
	}
}

func TestStore_CreateHook_ThenGetHook_ExactVersion(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	h := mkHook("hook-a", 1, "body-v1")
	if _, err := s.CreateHook(ctx, h); err != nil {
		t.Fatalf("CreateHook() 返回错误: %v", err)
	}

	got, err := s.GetHook(ctx, "hook-a", 1)
	if err != nil {
		t.Fatalf("GetHook(确切版本) 返回错误: %v", err)
	}
	if got.Body != "body-v1" || got.Version != 1 {
		t.Errorf("GetHook() = %+v, want Body=body-v1 Version=1", got)
	}
}

// TestStore_GetHook_VersionZeroTakesLatest 验证 version 传 0 取最新版本
// ——非正数取最新的分支，容易只实现 0 而漏掉负数，本测试与下一条各锁
// 一种非正数输入。
func TestStore_GetHook_VersionZeroTakesLatest(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	mustCreate(t, s, mkHook("hook-b", 1, "body-v1"))
	mustCreate(t, s, mkHook("hook-b", 2, "body-v2"))
	mustCreate(t, s, mkHook("hook-b", 3, "body-v3"))

	got, err := s.GetHook(ctx, "hook-b", 0)
	if err != nil {
		t.Fatalf("GetHook(version=0) 返回错误: %v", err)
	}
	if got.Version != 3 || got.Body != "body-v3" {
		t.Errorf("GetHook(version=0) = %+v, want Version=3 Body=body-v3", got)
	}
}

// TestStore_GetHook_VersionNegativeTakesLatest 验证 version 传 -1 同样
// 取最新版本——「非正数」不等于「0」，负数必须走同一分支。
func TestStore_GetHook_VersionNegativeTakesLatest(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	mustCreate(t, s, mkHook("hook-c", 1, "body-v1"))
	mustCreate(t, s, mkHook("hook-c", 2, "body-v2"))

	got, err := s.GetHook(ctx, "hook-c", -1)
	if err != nil {
		t.Fatalf("GetHook(version=-1) 返回错误: %v", err)
	}
	if got.Version != 2 || got.Body != "body-v2" {
		t.Errorf("GetHook(version=-1) = %+v, want Version=2 Body=body-v2", got)
	}
}

func TestStore_GetHook_IDNotFound(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	_, err := s.GetHook(ctx, "no-such-hook", 0)
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("GetHook(不存在的 id) 返回 %v, want errors.Is(_, contract.ErrNotFound)", err)
	}
}

func TestStore_GetHookByDigest_Found(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	h := mkHook("hook-d", 1, "unique-body-for-digest-test")
	mustCreate(t, s, h)

	got, err := s.GetHookByDigest(ctx, h.BodySHA256)
	if err != nil {
		t.Fatalf("GetHookByDigest(命中) 返回错误: %v", err)
	}
	if got.ID != "hook-d" {
		t.Errorf("GetHookByDigest() = %+v, want ID=hook-d", got)
	}
}

func TestStore_GetHookByDigest_NotFound(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	_, err := s.GetHookByDigest(ctx, model.HookDigest("从未写入过的正文"))
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("GetHookByDigest(不命中) 返回 %v, want errors.Is(_, contract.ErrNotFound)", err)
	}
}

// TestStore_ListHooks_ReturnsAllVersionsOfEachID 验证 ListHooks 返回各
// id 的全部版本，不只是每个 id 的最新版本。
func TestStore_ListHooks_ReturnsAllVersionsOfEachID(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	mustCreate(t, s, mkHook("hook-e", 1, "e-v1"))
	mustCreate(t, s, mkHook("hook-e", 2, "e-v2"))
	mustCreate(t, s, mkHook("hook-f", 1, "f-v1"))

	got, err := s.ListHooks(ctx)
	if err != nil {
		t.Fatalf("ListHooks() 返回错误: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(ListHooks()) = %d, want 3（hook-e 两版 + hook-f 一版）", len(got))
	}

	versionsByID := map[string][]int{}
	for _, h := range got {
		versionsByID[h.ID] = append(versionsByID[h.ID], h.Version)
	}
	if len(versionsByID["hook-e"]) != 2 {
		t.Errorf("hook-e 的版本数 = %d, want 2", len(versionsByID["hook-e"]))
	}
	if len(versionsByID["hook-f"]) != 1 {
		t.Errorf("hook-f 的版本数 = %d, want 1", len(versionsByID["hook-f"]))
	}
}

// TestStore_UpdateHookState_BodyAndDigestUnchanged 验证改状态后 Body 与
// BodySHA256 不变——正文不可变是本方法最核心的约束（计划任务 9「再强调」
// 与接口契约都明确指出这一点）。
func TestStore_UpdateHookState_BodyAndDigestUnchanged(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	h := mkHook("hook-g", 1, "body-must-not-change")
	mustCreate(t, s, h)

	got, err := s.UpdateHookState(ctx, "hook-g", 1, model.HookStateActive, "approver-1", 1700000000)
	if err != nil {
		t.Fatalf("UpdateHookState() 返回错误: %v", err)
	}
	if got.Body != h.Body || got.BodySHA256 != h.BodySHA256 {
		t.Errorf("UpdateHookState() 改动了正文: Body=%q BodySHA256=%q, want 不变", got.Body, got.BodySHA256)
	}
	if got.State != model.HookStateActive || got.Approver != "approver-1" || got.ApprovedAt != 1700000000 {
		t.Errorf("UpdateHookState() = %+v, want State=active Approver=approver-1 ApprovedAt=1700000000", got)
	}
}

// TestStore_UpdateHookState_ApprovedAtRoundTrip 验证 approved_at 从 0
// 改成非 0、再改回 0 的往返——0 必须真的写成 SQL NULL，不能被 GORM 的
// Updates 当零值跳过（计划任务 5 的风险提示：同样的陷阱在本方法也存在）。
func TestStore_UpdateHookState_ApprovedAtRoundTrip(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	mustCreate(t, s, mkHook("hook-h", 1, "body-h"))

	afterSet, err := s.UpdateHookState(ctx, "hook-h", 1, model.HookStateReview, "approver-x", 1700000000)
	if err != nil {
		t.Fatalf("第一次 UpdateHookState()（0→非0）返回错误: %v", err)
	}
	if afterSet.ApprovedAt != 1700000000 {
		t.Fatalf("第一次 UpdateHookState() 后 ApprovedAt = %d, want 1700000000", afterSet.ApprovedAt)
	}

	afterUnset, err := s.UpdateHookState(ctx, "hook-h", 1, model.HookStateDraft, "", 0)
	if err != nil {
		t.Fatalf("第二次 UpdateHookState()（非0→0）返回错误: %v", err)
	}
	if afterUnset.ApprovedAt != 0 {
		t.Errorf("第二次 UpdateHookState() 后 ApprovedAt = %d, want 0（必须真的写成 SQL NULL）", afterUnset.ApprovedAt)
	}

	// 重新从库里查一次，排除"只有返回值对但库里其实没改"的可能。
	reread, err := s.GetHook(ctx, "hook-h", 1)
	if err != nil {
		t.Fatalf("重读验证时 GetHook() 返回错误: %v", err)
	}
	if reread.ApprovedAt != 0 {
		t.Errorf("重读得到 ApprovedAt = %d, want 0", reread.ApprovedAt)
	}
}

func TestStore_UpdateHookState_IDNotFound(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	_, err := s.UpdateHookState(ctx, "no-such-hook", 1, model.HookStateActive, "a", 1)
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("UpdateHookState(不存在) 返回 %v, want errors.Is(_, contract.ErrNotFound)", err)
	}
}

func mustCreate(t *testing.T, s *Store, h model.Hook) {
	t.Helper()
	if _, err := s.CreateHook(context.Background(), h); err != nil {
		t.Fatalf("CreateHook(%q, v%d) 返回错误: %v", h.ID, h.Version, err)
	}
}
