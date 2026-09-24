// 本文件测试 event.Store（contract.EventStore 的 PG 实现），对应实现
// 计划任务 7 的前一半。
//
// 阻塞说明（同一段落写进 StructuredOutput 的 blockers）：本包 import
// internal/store/postgres/pgtest 建测试数据库句柄，但 pgtest 依赖
// gorm.io/driver/postgres——该依赖当前不在 go.mod/go.sum 里（任务交付
// 说明称三个直接依赖"已就位"，实测不成立，见下方验证）：
//
//	$ /d/golang/bin/go build ./internal/store/postgres/pgtest/...
//	no required module provides package gorm.io/driver/postgres; to add it:
//		go get gorm.io/driver/postgres
//
// 硬性约束第 4 条明确"缺依赖就报告，不要自己 go get"，本文件遵照该约束：
// 不新增 import 去绕过缺口，也不用字符串匹配错误文本这种绕过
// pgerr.Translate 的本地检测手段去让 AppendEvent 的重复序号分支"看起来"
// 通过验证——那样产出的行为会与 pgerr 包尚未支持 23505/ConstraintName
// 判定的现状不一致，且与 pgerr.Translate 自身的阻塞说明（同样因为缺这
// 个依赖而留空 23505 分支）矛盾。本文件的测试在依赖补齐、
// go build ./internal/store/postgres/... 能通过之后应该可以直接跑，
// 不需要改代码；补依赖前，本文件本身也无法编译（同一个阻塞点）。
package event

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgerr"
	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// TestIndexNameMatchesPgerrConstant 锁住 row.go 里字面量索引名与
// pgerr.IdxTaskEventsTaskSeq 的一致性。Go 的 struct tag 是字面常量，
// 不能在 tag 里引用包外常量拼接，所以 row.go 必须重复写一遍字符串；
// 这条测试是那个重复不漂移的唯一保险（计划任务 2 风险提示明确点出的
// 隐患：pgerr 依赖的索引名与聚合包 tag 里写的必须字面一致）。
func TestIndexNameMatchesPgerrConstant(t *testing.T) {
	const literalInRowGo = "idx_task_events_task_seq"
	if literalInRowGo != pgerr.IdxTaskEventsTaskSeq {
		t.Errorf("row.go 里的索引名字面量 %q 与 pgerr.IdxTaskEventsTaskSeq %q 不一致",
			literalInRowGo, pgerr.IdxTaskEventsTaskSeq)
	}
}

// newTestStore 建一个独立 schema 的 Store，供各测试用例复用。
func newTestStore(t *testing.T) *Store {
	db := pgtest.NewDB(t, Models()...)
	return New(db)
}

// sampleEvent 构造一个字段全部填充的信号事件，seq 由调用方指定。
func sampleEvent(taskID string, seq int) model.TaskEvent {
	return model.TaskEvent{
		TaskID:     taskID,
		Seq:        seq,
		TS:         1_700_000_000,
		Phase:      model.PhasePackages,
		Level:      model.EventLevelInfo,
		Message:    "安装 nginx 完成",
		RC:         0,
		HookDigest: "sha256:deadbeef",
		Source:     model.EventSourceSignal,
	}
}

// TestAppendEvent_Normal 验证正常追加：返回值与写入值一致（Seq > 0 的
// 分支不改写调用方传入的 Seq），且能通过 ListEvents 读回。
func TestAppendEvent_Normal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleEvent("task-1", 1)
	got, err := s.AppendEvent(ctx, in)
	if err != nil {
		t.Fatalf("AppendEvent(正常追加) 失败: %v", err)
	}
	if diff := cmp.Diff(in, got); diff != "" {
		t.Errorf("AppendEvent(正常追加) 返回值与输入不符（-want +got）:\n%s", diff)
	}

	list, err := s.ListEvents(ctx, "task-1", contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListEvents 返回 %d 条，want 1", len(list))
	}
	if diff := cmp.Diff(in, list[0]); diff != "" {
		t.Errorf("ListEvents 读回的记录与写入值不符（-want +got）:\n%s", diff)
	}
}

// TestAppendEvent_DuplicateSeq_ReturnsErrDuplicateSeq 验证同 (task_id, seq)
// 第二次追加返回 contract.ErrDuplicateSeq——这不是错误路径，是机器侧重试
// 造成的正常重复投递。
//
// 阻塞：本测试依赖 pgerr.Translate 尚未实现的 23505/ConstraintName 分支
// （见 store.go 的 AppendEvent 文档、pgerr/translate.go 文件头注释）。
// 依赖补齐、pgerr 补上该分支之前，本测试会 FAIL——这是如实反映当前状态，
// 不是本测试写错；store.go 的实现已经是最终形态，无需改动。
func TestAppendEvent_DuplicateSeq_ReturnsErrDuplicateSeq(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := sampleEvent("task-dup", 5)
	if _, err := s.AppendEvent(ctx, first); err != nil {
		t.Fatalf("第一次 AppendEvent 失败: %v", err)
	}

	second := sampleEvent("task-dup", 5)
	second.Message = "同一序号的第二次投递（机器侧重试）"
	_, err := s.AppendEvent(ctx, second)
	if !errors.Is(err, contract.ErrDuplicateSeq) {
		t.Fatalf("AppendEvent(同 task_id+seq 第二次) = %v, want errors.Is(_, contract.ErrDuplicateSeq) = true", err)
	}
}

// TestAppendEvent_ZeroSeq_AllocatesDistinctSeqEachTime 验证 Seq <= 0（本
// 例用 0）连续两次追加都成功，且分配到不同的序号——非信号事件不参与
// (task_id, seq) 去重，但序号本身仍必须唯一才能落进同一张表。
func TestAppendEvent_ZeroSeq_AllocatesDistinctSeqEachTime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	base := sampleEvent("task-zero", 0)
	base.Source = model.EventSourceHeartbeat

	first, err := s.AppendEvent(ctx, base)
	if err != nil {
		t.Fatalf("第一次 AppendEvent(Seq=0) 失败: %v", err)
	}
	second, err := s.AppendEvent(ctx, base)
	if err != nil {
		t.Fatalf("第二次 AppendEvent(Seq=0) 失败: %v", err)
	}

	if first.Seq == 0 || second.Seq == 0 {
		t.Fatalf("分配后的 Seq 不应为 0（占位值不落盘），got first=%d second=%d", first.Seq, second.Seq)
	}
	if first.Seq == second.Seq {
		t.Fatalf("两次 Seq=0 的追加分配到相同序号 %d，want 不同", first.Seq)
	}
	if first.Seq >= 0 || second.Seq >= 0 {
		t.Errorf("非信号事件分配的序号应为负数（与信号序号空间不相交），got first=%d second=%d", first.Seq, second.Seq)
	}
}

// TestAppendEvent_ZeroSeq_IgnoresCallerSeqInReturnValue 验证调用方传入的
// Seq 值在非信号分支被忽略：即便调用方填了一个负数占位值，返回值的 Seq
// 也是实现分配的那个，不是调用方传入的原值。
func TestAppendEvent_ZeroSeq_IgnoresCallerSeqInReturnValue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleEvent("task-ignore-seq", -999)
	got, err := s.AppendEvent(ctx, in)
	if err != nil {
		t.Fatalf("AppendEvent(Seq=-999) 失败: %v", err)
	}
	if got.Seq == -999 {
		t.Errorf("AppendEvent 应忽略调用方传入的 Seq 并分配新序号，got Seq=%d 与调用方原值相同", got.Seq)
	}
}

// TestListEvents_OrderedByInsertion 验证多条事件按**插入顺序**返回，
// 而不是按 Seq 数值排序。
//
// # 为什么不是「按 Seq 升序」
//
// contract.EventStore.ListEvents 的文档写的是「按 Seq 升序返回」，但旧
// SQLite 实现（sqlite/event.go:130）用的是 ORDER BY id ASC，即插入顺序。
// 本次迁移的目标是「只换实现不改行为」，因此保留 id ASC 并在这里把真实
// 行为钉死，而不是照文档字面改成 seq ASC。
//
// 为什么 id ASC 才是对的：非信号事件（Seq <= 0）分配到的是**递减的负数**
// 序号（-1、-2、-3……，见 appendNonSignalEvent）。若按 seq 数值升序，最近
// 写入的那条非信号事件（负得最多）会排到整个事件流的最前面，时间线整体
// 倒置——而这张表是 append-only 的事件日志，控制台任务详情页与 SSE 流都
// 要求时间顺序。两个序号空间（正数信号 / 负数非信号）本来就不构成一条
// 可用于排序的单调数轴，id 才是。
//
// conformance 套件分辨不出这两种排序：它按 10、20、30 递增追加信号事件，
// 此时插入顺序与 seq 顺序恰好相同。本用例故意乱序追加（3、1、2）才能把
// 差异暴露出来。
func TestListEvents_OrderedByInsertion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	insertOrder := []int{3, 1, 2}
	for _, seq := range insertOrder {
		if _, err := s.AppendEvent(ctx, sampleEvent("task-order", seq)); err != nil {
			t.Fatalf("AppendEvent(seq=%d) 失败: %v", seq, err)
		}
	}

	list, err := s.ListEvents(ctx, "task-order", contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(list) != len(insertOrder) {
		t.Fatalf("ListEvents 返回 %d 条，want %d", len(list), len(insertOrder))
	}
	for i, e := range list {
		if e.Seq != insertOrder[i] {
			t.Errorf("ListEvents[%d].Seq = %d, want %d（插入顺序，非 seq 数值序）",
				i, e.Seq, insertOrder[i])
		}
	}
}

// TestListEvents_NonSignalEventStaysChronological 验证非信号事件（分配到
// 负数序号）出现在它**实际写入的位置**，而不是因为序号是负数就被排到最
// 前面。这是 TestListEvents_OrderedByInsertion 注释里那条理由的可执行
// 版本：一旦有人把排序改成 seq ASC，本用例会立刻失败。
func TestListEvents_NonSignalEventStaysChronological(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 先写一条信号事件（seq=1），再写一条非信号事件（分配到 -1），
	// 最后再写一条信号事件（seq=2）。
	if _, err := s.AppendEvent(ctx, sampleEvent("task-chrono", 1)); err != nil {
		t.Fatalf("AppendEvent(seq=1) 失败: %v", err)
	}
	nonSignal, err := s.AppendEvent(ctx, sampleEvent("task-chrono", 0))
	if err != nil {
		t.Fatalf("AppendEvent(seq=0，非信号) 失败: %v", err)
	}
	if nonSignal.Seq > 0 {
		t.Fatalf("非信号事件应分配到非正数序号，got Seq=%d", nonSignal.Seq)
	}
	if _, err := s.AppendEvent(ctx, sampleEvent("task-chrono", 2)); err != nil {
		t.Fatalf("AppendEvent(seq=2) 失败: %v", err)
	}

	list, err := s.ListEvents(ctx, "task-chrono", contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ListEvents 返回 %d 条，want 3", len(list))
	}
	// 非信号事件写在第二位，就应该读回在第二位。
	if list[1].Seq != nonSignal.Seq {
		t.Errorf("非信号事件应出现在第 2 位（写入位置），实际第 2 位 Seq=%d，非信号事件 Seq=%d —— "+
			"若排序被改成 seq ASC，负数序号会被排到最前面，事件流时间线倒置",
			list[1].Seq, nonSignal.Seq)
	}
}

// TestListEvents_AfterSeqFilter 验证 filter.AfterSeq 非零时只返回 Seq
// 大于该值的事件（SSE 的 Last-Event-ID 续传语义）。
func TestListEvents_AfterSeqFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, seq := range []int{1, 2, 3, 4} {
		if _, err := s.AppendEvent(ctx, sampleEvent("task-afterseq", seq)); err != nil {
			t.Fatalf("AppendEvent(seq=%d) 失败: %v", seq, err)
		}
	}

	list, err := s.ListEvents(ctx, "task-afterseq", contract.EventFilter{AfterSeq: 2})
	if err != nil {
		t.Fatalf("ListEvents(AfterSeq=2) 失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListEvents(AfterSeq=2) 返回 %d 条，want 2（seq 3、4）", len(list))
	}
	for _, e := range list {
		if e.Seq <= 2 {
			t.Errorf("ListEvents(AfterSeq=2) 返回了 Seq=%d，want 全部 > 2", e.Seq)
		}
	}
}

// TestListEvents_EmptyResult_ReturnsEmptySliceNotNil 验证查不到匹配记录
// 时返回空切片而非 nil——调用方（HTTP handler）不需要为这条查询专门
// 处理一个 nil 分支（与 contract.AuditStore.ListAudit 同一约定）。
func TestListEvents_EmptyResult_ReturnsEmptySliceNotNil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	list, err := s.ListEvents(ctx, "task-does-not-exist", contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents(不存在的 task_id) 失败: %v", err)
	}
	if list == nil {
		t.Error("ListEvents(无匹配记录) 返回 nil，want 空切片（非 nil）")
	}
	if len(list) != 0 {
		t.Errorf("ListEvents(无匹配记录) 返回 %d 条，want 0", len(list))
	}
}

// TestModels_IDColumnIsBigserial 验证 AutoMigrate 为 task_events.id 生成
// 的 column_default 含 nextval（即 bigserial 背后的序列默认值），而不是
// 普通 bigint 没有默认值——conformance/store.go:1173 的断言依赖 id 单调
// 递增，这条测试是"不假设、去查 information_schema.columns 确认"这一
// 要求本身（计划任务 7「测试必须覆盖」明确要求）。
func TestModels_IDColumnIsBigserial(t *testing.T) {
	s := newTestStore(t)

	var columnDefault string
	row := s.db.Raw(
		`SELECT column_default FROM information_schema.columns WHERE table_name = ? AND column_name = ?`,
		"task_events", "id",
	).Row()
	if err := row.Scan(&columnDefault); err != nil {
		t.Fatalf("查询 information_schema.columns 失败: %v", err)
	}

	if !containsSubstring(columnDefault, "nextval") {
		t.Errorf("task_events.id 的 column_default = %q，want 包含 nextval（bigserial 的标志），"+
			"说明 AutoMigrate 生成的是普通 bigint 而不是 bigserial", columnDefault)
	}
}

// containsSubstring 是测试内部的小工具，避免直接在断言里写
// strings.Contains 影响可读性（与 pgerr/translate_test.go 的
// containsCtxLabel 同一约定）。
func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
