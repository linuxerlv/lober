// 本文件测试 Store 的 MergeFacts / GetFacts，对应实现计划任务 7 后半段
// 的红灯步骤。用 pgtest.NewDB 拿独立 schema，AutoMigrate 已建好
// task_facts 表。
package fact

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// TestMergeFacts_FirstWrite 覆盖首次写入：空任务上写入若干键，GetFacts
// 必须原样取回。
func TestMergeFacts_FirstWrite(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()
	const taskID = "task-1"

	in := map[string]string{
		"LOBER_SN":  "SN001",
		"LOBER_MAC": "aa:bb:cc:dd:ee:ff",
	}
	if err := s.MergeFacts(ctx, taskID, in); err != nil {
		t.Fatalf("MergeFacts 首次写入失败: %v", err)
	}

	got, err := s.GetFacts(ctx, taskID)
	if err != nil {
		t.Fatalf("GetFacts 失败: %v", err)
	}
	if diff := cmp.Diff(in, got); diff != "" {
		t.Errorf("首次写入后 GetFacts 结果与写入值不符 (-want +got):\n%s", diff)
	}
}

// TestMergeFacts_SecondWriteDifferentKey 覆盖第二次写入不同键：验证旧键
// 仍在——这是 merge 语义（而非 replace）的核心断言。
func TestMergeFacts_SecondWriteDifferentKey(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()
	const taskID = "task-2"

	if err := s.MergeFacts(ctx, taskID, map[string]string{"LOBER_SN": "SN001"}); err != nil {
		t.Fatalf("第一次 MergeFacts 失败: %v", err)
	}
	if err := s.MergeFacts(ctx, taskID, map[string]string{"LOBER_MAC": "aa:bb:cc:dd:ee:ff"}); err != nil {
		t.Fatalf("第二次 MergeFacts 失败: %v", err)
	}

	got, err := s.GetFacts(ctx, taskID)
	if err != nil {
		t.Fatalf("GetFacts 失败: %v", err)
	}
	want := map[string]string{
		"LOBER_SN":  "SN001",
		"LOBER_MAC": "aa:bb:cc:dd:ee:ff",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("第二次写入不同键后，旧键未保留 (-want +got):\n%s", diff)
	}
}

// TestMergeFacts_SecondWriteSameKey 覆盖第二次写入同键：验证被覆盖而非
// 追加或报错。
func TestMergeFacts_SecondWriteSameKey(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()
	const taskID = "task-3"

	if err := s.MergeFacts(ctx, taskID, map[string]string{"LOBER_SN": "SN001"}); err != nil {
		t.Fatalf("第一次 MergeFacts 失败: %v", err)
	}
	if err := s.MergeFacts(ctx, taskID, map[string]string{"LOBER_SN": "SN002"}); err != nil {
		t.Fatalf("第二次 MergeFacts（覆盖同键）失败: %v", err)
	}

	got, err := s.GetFacts(ctx, taskID)
	if err != nil {
		t.Fatalf("GetFacts 失败: %v", err)
	}
	want := map[string]string{"LOBER_SN": "SN002"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("同键第二次写入未覆盖旧值 (-want +got):\n%s", diff)
	}
}

// TestMergeFacts_EmptyMapIsNoop 覆盖空 map 输入：不报错，也不清空既有
// facts——空 map 表示「本次不合并任何键」，不是 replace 语义下的
// 「清空全部」。
func TestMergeFacts_EmptyMapIsNoop(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()
	const taskID = "task-4"

	if err := s.MergeFacts(ctx, taskID, map[string]string{"LOBER_SN": "SN001"}); err != nil {
		t.Fatalf("第一次 MergeFacts 失败: %v", err)
	}
	if err := s.MergeFacts(ctx, taskID, map[string]string{}); err != nil {
		t.Fatalf("空 map 的 MergeFacts 应成功，实际报错: %v", err)
	}
	if err := s.MergeFacts(ctx, taskID, nil); err != nil {
		t.Fatalf("nil map 的 MergeFacts 应成功，实际报错: %v", err)
	}

	got, err := s.GetFacts(ctx, taskID)
	if err != nil {
		t.Fatalf("GetFacts 失败: %v", err)
	}
	want := map[string]string{"LOBER_SN": "SN001"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("空/nil map 写入后既有 facts 被改变 (-want +got):\n%s", diff)
	}
}

// TestGetFacts_NoFactsReturnsEmptyMap 覆盖无 fact 的任务：GetFacts 必须
// 返回空 map（非 nil、非错误）——facts 没有「集合不存在」这个错误态。
func TestGetFacts_NoFactsReturnsEmptyMap(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	got, err := s.GetFacts(ctx, "task-never-written")
	if err != nil {
		t.Fatalf("GetFacts 对无 fact 任务应成功，实际报错: %v", err)
	}
	if got == nil {
		t.Fatal("GetFacts 对无 fact 任务返回了 nil，期望非 nil 的空 map")
	}
	if len(got) != 0 {
		t.Fatalf("GetFacts 对无 fact 任务返回了非空结果: %v", got)
	}
}

// TestMergeFacts_DoesNotAffectOtherTask 验证合并写入按 task_id 隔离：
// 写入 task A 不影响 task B 的 facts——这不是契约明确列出的用例，但
// 复合主键 (task_id, key) 的隔离性是整个方法正确性的前提，不测等于
// 白测。
func TestMergeFacts_DoesNotAffectOtherTask(t *testing.T) {
	db := pgtest.NewDB(t, Models()...)
	s := New(db)
	ctx := context.Background()

	if err := s.MergeFacts(ctx, "task-a", map[string]string{"LOBER_SN": "SN-A"}); err != nil {
		t.Fatalf("写入 task-a 失败: %v", err)
	}
	if err := s.MergeFacts(ctx, "task-b", map[string]string{"LOBER_SN": "SN-B"}); err != nil {
		t.Fatalf("写入 task-b 失败: %v", err)
	}

	gotA, err := s.GetFacts(ctx, "task-a")
	if err != nil {
		t.Fatalf("GetFacts(task-a) 失败: %v", err)
	}
	if diff := cmp.Diff(map[string]string{"LOBER_SN": "SN-A"}, gotA); diff != "" {
		t.Errorf("task-a 的 facts 被污染 (-want +got):\n%s", diff)
	}

	gotB, err := s.GetFacts(ctx, "task-b")
	if err != nil {
		t.Fatalf("GetFacts(task-b) 失败: %v", err)
	}
	if diff := cmp.Diff(map[string]string{"LOBER_SN": "SN-B"}, gotB); diff != "" {
		t.Errorf("task-b 的 facts 被污染 (-want +got):\n%s", diff)
	}
}
