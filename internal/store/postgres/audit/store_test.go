// 本文件测试 audit.Store 对 contract.AuditStore 的实现，覆盖任务描述
// 明确要求的六个场景：倒序、同一秒内后写入排前面、Target 精确匹配、
// 时间区间过滤、SinceTS > UntilTS 返回空、无匹配返回空切片。
package audit

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// newTestStore 建一个独立 schema 的 Store，供各测试用例复用。
func newTestStore(t *testing.T) *Store {
	db := pgtest.NewDB(t, Models()...)
	return New(db)
}

// TestAppendAudit_And_ListAudit_RoundTrips 验证最基本的写入-查询往返：
// 写一条记录，ListAudit 无过滤条件应原样查回，包括 Detail 自由文本的
// 完整往返（含中文、引号——JSON 编码列的典型故障点）。
func TestAppendAudit_And_ListAudit_RoundTrips(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	record := contract.AuditRecord{
		TS:     1735689600,
		Actor:  "carol-author",
		Action: "hook.reject",
		Target: "hook-roundtrip-test",
		Detail: `reason=接口调用没有做超时保护，容易把主机名单请求卡死` + "\n" +
			`第二行含双引号 "quoted" 与反斜杠 \path\to\file`,
	}

	if err := store.AppendAudit(ctx, record); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{Target: record.Target})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListAudit 返回 %d 条，want 1", len(got))
	}
	if got[0] != record {
		t.Errorf("往返后记录不一致：\n写入=%+v\n读回=%+v", record, got[0])
	}
}

// TestListAudit_OrderedByTSDescending 验证按 TS 倒序：写入三条不同 TS
// 的记录，ListAudit 无过滤条件时应按 TS 从大到小返回。
func TestListAudit_OrderedByTSDescending(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const target = "hook-order-test"
	tsList := []int64{100, 300, 200}
	for _, ts := range tsList {
		if err := store.AppendAudit(ctx, contract.AuditRecord{
			TS: ts, Actor: "actor", Action: "action", Target: target, Detail: "detail",
		}); err != nil {
			t.Fatalf("AppendAudit(ts=%d) 失败: %v", ts, err)
		}
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{Target: target})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListAudit 返回 %d 条，want 3", len(got))
	}

	wantOrder := []int64{300, 200, 100}
	for i, want := range wantOrder {
		if got[i].TS != want {
			t.Errorf("第 %d 条 TS = %d, want %d（完整顺序=%v）", i, got[i].TS, want, tsListOf(got))
		}
	}
}

// tsListOf 是测试失败信息的辅助函数，提取一组记录的 TS 序列方便打印。
func tsListOf(records []contract.AuditRecord) []int64 {
	out := make([]int64, 0, len(records))
	for _, r := range records {
		out = append(out, r.TS)
	}
	return out
}

// TestListAudit_SameSecondNewerWritesFirst 验证同一秒内后写入的记录排
// 前面（TS 相同时按 id 倒序兜底）——任务描述明确指出有下游断言依赖这个
// 具体行为，不是"顺序不确定也可以接受"的场景。
func TestListAudit_SameSecondNewerWritesFirst(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const (
		target = "hook-same-second-test"
		ts     = int64(1735689600)
	)

	if err := store.AppendAudit(ctx, contract.AuditRecord{
		TS: ts, Actor: "actor", Action: "action", Target: target, Detail: "first",
	}); err != nil {
		t.Fatalf("第一次 AppendAudit 失败: %v", err)
	}
	if err := store.AppendAudit(ctx, contract.AuditRecord{
		TS: ts, Actor: "actor", Action: "action", Target: target, Detail: "second",
	}); err != nil {
		t.Fatalf("第二次 AppendAudit 失败: %v", err)
	}
	if err := store.AppendAudit(ctx, contract.AuditRecord{
		TS: ts, Actor: "actor", Action: "action", Target: target, Detail: "third",
	}); err != nil {
		t.Fatalf("第三次 AppendAudit 失败: %v", err)
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{Target: target})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListAudit 返回 %d 条，want 3", len(got))
	}

	wantOrder := []string{"third", "second", "first"}
	for i, want := range wantOrder {
		if got[i].Detail != want {
			t.Errorf("第 %d 条 Detail = %q, want %q", i, got[i].Detail, want)
		}
	}
}

// TestListAudit_TargetExactMatchNotSubstring 验证 Target 精确匹配、不做
// 子串匹配：存一条 target="hook-a" 再查 "hook" 应查不到。
func TestListAudit_TargetExactMatchNotSubstring(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.AppendAudit(ctx, contract.AuditRecord{
		TS: 1735689600, Actor: "actor", Action: "action", Target: "hook-a", Detail: "detail",
	}); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{Target: "hook"})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("查 Target=%q（子串）返回 %d 条，want 0（精确匹配不应命中 %q）", "hook", len(got), "hook-a")
	}

	// 确认精确匹配本身可用——不是过滤条件整体失效导致的假阳性。
	gotExact, err := store.ListAudit(ctx, contract.AuditFilter{Target: "hook-a"})
	if err != nil {
		t.Fatalf("ListAudit(精确) 失败: %v", err)
	}
	if len(gotExact) != 1 {
		t.Errorf("精确查 Target=%q 返回 %d 条，want 1", "hook-a", len(gotExact))
	}
}

// TestListAudit_TimeRangeFilter 验证 SinceTS/UntilTS 限定闭区间
// [SinceTS, UntilTS]，边界值本身应被包含。
func TestListAudit_TimeRangeFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const target = "hook-range-test"
	tsList := []int64{100, 200, 300, 400, 500}
	for _, ts := range tsList {
		if err := store.AppendAudit(ctx, contract.AuditRecord{
			TS: ts, Actor: "actor", Action: "action", Target: target, Detail: "detail",
		}); err != nil {
			t.Fatalf("AppendAudit(ts=%d) 失败: %v", ts, err)
		}
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{
		Target: target, SinceTS: 200, UntilTS: 400,
	})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}

	wantTS := map[int64]bool{200: true, 300: true, 400: true}
	if len(got) != len(wantTS) {
		t.Fatalf("ListAudit(区间[200,400]) 返回 %d 条，want %d（实际=%v）",
			len(got), len(wantTS), tsListOf(got))
	}
	for _, r := range got {
		if !wantTS[r.TS] {
			t.Errorf("返回了区间外的 TS=%d", r.TS)
		}
	}
}

// TestListAudit_SinceAfterUntilReturnsEmpty 验证 SinceTS > UntilTS 时
// 返回空结果而不是报错——调用方传参颠倒的正常后果，不是异常。
func TestListAudit_SinceAfterUntilReturnsEmpty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const target = "hook-since-after-until-test"
	if err := store.AppendAudit(ctx, contract.AuditRecord{
		TS: 300, Actor: "actor", Action: "action", Target: target, Detail: "detail",
	}); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{
		Target: target, SinceTS: 500, UntilTS: 100,
	})
	if err != nil {
		t.Fatalf("ListAudit(SinceTS > UntilTS) 应返回空结果而不是报错，实际报错: %v", err)
	}
	if got == nil {
		t.Errorf("ListAudit(SinceTS > UntilTS) 返回了 nil，want 非 nil 的空切片")
	}
	if len(got) != 0 {
		t.Errorf("ListAudit(SinceTS > UntilTS) 返回 %d 条，want 0", len(got))
	}
}

// TestListAudit_NoMatchReturnsEmptySliceNotError 验证查不到匹配记录时
// 返回空切片而非 ErrNotFound——没有审计记录是正常状态（比如一个刚创建、
// 从未被驳回过的 hook）。
func TestListAudit_NoMatchReturnsEmptySliceNotError(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	got, err := store.ListAudit(ctx, contract.AuditFilter{Target: "hook-never-existed"})
	if err != nil {
		t.Fatalf("ListAudit(无匹配) 应返回 nil error，实际报错: %v", err)
	}
	if got == nil {
		t.Errorf("ListAudit(无匹配) 返回了 nil，want 非 nil 的空切片")
	}
	if len(got) != 0 {
		t.Errorf("ListAudit(无匹配) 返回 %d 条，want 0", len(got))
	}
}
