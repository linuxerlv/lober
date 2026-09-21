// 本文件测试并发准入控制 AdmissionController（计划 phase4-01 任务 1）。
// 依据 SPEC §10 失败模式 11、§3.3。
//
// 核心断言：超限时不消耗 attempt、不置 failed，任务原样留在 pending；
// 计数口径只算 booting 与 installing；进程重启后（用预置了在装任务的库
// 模拟）计数依然正确——这三条是本任务与「用内存计数器」这个错误实现方式
// 的分界线。
package statemachine

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// mustCreateTaskInState 创建一个直接处于指定状态的任务，供准入测试构造
// 「当前有 N 台在装」的库前提。不经过 StartBoot 等状态机方法——准入测试
// 只关心 AdmissionController.Check 读到的计数是否正确，不关心任务是怎么
// 走到这个状态的（那是其它测试文件的职责）。
func mustCreateTaskInState(t *testing.T, store *fake.Store, taskID string, state model.TaskState) model.Task {
	t.Helper()
	created, err := store.CreateTask(context.Background(), model.Task{
		ID:           taskID,
		State:        model.TaskStatePending,
		CurrentPhase: model.PhasePending,
	})
	if err != nil {
		t.Fatalf("CreateTask(%s) 失败: %v", taskID, err)
	}
	if state == model.TaskStatePending {
		return created
	}
	updated, err := store.CASTaskState(context.Background(), taskID, model.TaskStatePending, created.Version, state)
	if err != nil {
		t.Fatalf("CASTaskState(%s, pending -> %s) 失败: %v", taskID, state, err)
	}
	return updated
}

// TestAdmissionController_UnderLimitAdmits 验证未达上限时正常放行。
func TestAdmissionController_UnderLimitAdmits(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	a := NewAdmissionController(store)
	a.SetLimit(3)

	mustCreateTaskInState(t, store, "task-booting-1", model.TaskStateBooting)

	decision, err := a.Check(ctx)
	if err != nil {
		t.Fatalf("Check 返回了错误: %v", err)
	}
	if !decision.Admitted {
		t.Errorf("当前在装 1 台、上限 3 台，Check.Admitted = false, want true —— " +
			"未达上限时必须正常放行（计划任务 1「测试要锁住的行为」第一条）")
	}
}

// TestAdmissionController_AtLimitRejectsWithoutFailingTask 验证达到上限
// 时拒绝，且这条拒绝绝不能通过消耗 attempt 或置 failed 来表达——那是为
// 「重试必然同样失败」的场景设计的语义，而超限是「稍后重试就会成功」，
// 两者的处理后果完全相反：若误用 failed 表达排队，运维会看到一堆
// 「因为并发限流」而失败的任务，而这批任务本该在几分钟后自然装上，不需要
// 任何人工介入。
func TestAdmissionController_AtLimitRejectsWithoutFailingTask(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	a := NewAdmissionController(store)
	a.SetLimit(2)

	mustCreateTaskInState(t, store, "task-booting-1", model.TaskStateBooting)
	mustCreateTaskInState(t, store, "task-installing-1", model.TaskStateInstalling)
	pending := mustCreateTaskInState(t, store, "task-pending-1", model.TaskStatePending)

	decision, err := a.Check(ctx)
	if err != nil {
		t.Fatalf("Check 返回了错误: %v", err)
	}
	if decision.Admitted {
		t.Fatalf("当前在装 2 台、上限 2 台，Check.Admitted = true, want false —— " +
			"已达上限，新的引导请求应该排队而不是放行")
	}
	if decision.InFlight != 2 || decision.Limit != 2 {
		t.Errorf("Check 返回 InFlight=%d Limit=%d, want InFlight=2 Limit=2", decision.InFlight, decision.Limit)
	}

	// 拒绝本身必须是纯读操作：不改动任何任务的字段。用 GetTask 重新读取
	// pending 任务，确认 attempt 与 state 都原封未动。
	after, err := store.GetTask(ctx, pending.ID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if after.AttemptCount != 0 {
		t.Errorf("Check 拒绝之后 pending 任务的 AttemptCount = %d, want 0 —— "+
			"超限不是失败，不能消耗 attempt（计划任务 1：消耗 attempt 的后果是三轮排队"+
			"之后任务被判失败，而它本该只是等一会儿）", after.AttemptCount)
	}
	if after.State != model.TaskStatePending {
		t.Errorf("Check 拒绝之后任务状态 = %q, want %q —— "+
			"超限的任务必须原样留在 pending，不能被置 failed", after.State, model.TaskStatePending)
	}
	if after.Version != pending.Version {
		t.Errorf("Check 拒绝之后任务 Version = %d, want %d（未变）—— "+
			"Check 是纯读操作，不应该触发任何一次 CAS 或 mutation", after.Version, pending.Version)
	}
}

// TestAdmissionController_OnlyCountsBootingAndInstalling 验证计数口径：
// 只算 booting 与 installing，不算 rebooting/initializing（那两个阶段不
// 占带宽，机器在重启或跑初始化脚本）、不算 done/failed（终态）、也不算
// pending（还没开始占带宽）。
func TestAdmissionController_OnlyCountsBootingAndInstalling(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	a := NewAdmissionController(store)
	a.SetLimit(1)

	// 五个不占带宽的状态：pending、rebooting、initializing、done、failed。
	// 全部存在时 InFlight 应该仍是 0——若实现错误地把它们也计入，本测试的
	// 上限 1 会被立刻打满，Admitted 会变成 false，从而暴露错误。
	mustCreateTaskInState(t, store, "task-pending", model.TaskStatePending)
	mustCreateTaskInState(t, store, "task-rebooting", model.TaskStateRebooting)
	mustCreateTaskInState(t, store, "task-initializing", model.TaskStateInitializing)
	mustCreateTaskInState(t, store, "task-done", model.TaskStateDone)
	mustCreateTaskInState(t, store, "task-failed", model.TaskStateFailed)

	decision, err := a.Check(ctx)
	if err != nil {
		t.Fatalf("Check 返回了错误: %v", err)
	}
	if decision.InFlight != 0 {
		t.Errorf("五个 pending/rebooting/initializing/done/failed 任务存在时 InFlight = %d, want 0 —— "+
			"计数口径必须只算 booting 与 installing，rebooting/initializing 不占带宽"+
			"（机器在重启或跑初始化脚本），done/failed 是终态", decision.InFlight)
	}
	if !decision.Admitted {
		t.Errorf("InFlight 应为 0（上限 1），Check.Admitted = false, want true")
	}

	// 现在加一个 booting 与一个 installing，InFlight 应变为 2。
	mustCreateTaskInState(t, store, "task-booting", model.TaskStateBooting)
	mustCreateTaskInState(t, store, "task-installing", model.TaskStateInstalling)

	decision2, err := a.Check(ctx)
	if err != nil {
		t.Fatalf("Check 返回了错误: %v", err)
	}
	if decision2.InFlight != 2 {
		t.Errorf("加入一个 booting 与一个 installing 之后 InFlight = %d, want 2", decision2.InFlight)
	}
}

// TestAdmissionController_CountIsCorrectAfterRestart 验证「进程重启后
// 计数正确」——这是计划任务 1 明确要求的行为，也是「不要用内存计数器」
// 这条约束的直接体现：本测试完全不经过任何会累积内存状态的调用路径，
// 直接对一个预先写好数据的 fake.Store 构造一个全新的 AdmissionController，
// 模拟「进程刚重启，内存里什么都没有，但库里已经有正在装的机器」这个
// 场景。若实现使用了内存计数器（不查库），本测试会因为新
// AdmissionController 从零开始而看不到任何 in-flight 任务，从而错误地
// 放行。
func TestAdmissionController_CountIsCorrectAfterRestart(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()

	// 第一个 AdmissionController 实例模拟「重启前」——制造出两台在装的
	// 任务。
	before := NewAdmissionController(store)
	before.SetLimit(2)
	mustCreateTaskInState(t, store, "task-before-1", model.TaskStateBooting)
	mustCreateTaskInState(t, store, "task-before-2", model.TaskStateInstalling)

	// 第二个 AdmissionController 实例（全新的进程内状态）模拟「重启后」
	// ——但共享同一个 store，就像重启后重新连接同一个 SQLite 文件。
	after := NewAdmissionController(store)
	after.SetLimit(2)

	decision, err := after.Check(ctx)
	if err != nil {
		t.Fatalf("Check 返回了错误: %v", err)
	}
	if decision.InFlight != 2 {
		t.Errorf("「重启后」的全新 AdmissionController 读到 InFlight = %d, want 2 —— "+
			"计数必须来自查库而不是进程内存，否则重启后会错误地放行一批新机器，"+
			"造成真正的拥塞（计划任务 1「一条要避免的实现方式」）", decision.InFlight)
	}
	if decision.Admitted {
		t.Errorf("「重启后」InFlight=2 已达上限 2，Check.Admitted = true, want false")
	}
}

// TestAdmissionController_LimitZeroMeansUnlimited 验证上限设为 0 时表示
// 不限（向后兼容 Phase 1 的行为——Phase 1 没有准入控制，本功能默认不应
// 改变现有部署在未显式配置上限时的行为）。
func TestAdmissionController_LimitZeroMeansUnlimited(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	a := NewAdmissionController(store)
	a.SetLimit(0)

	for i := 0; i < 50; i++ {
		mustCreateTaskInState(t, store, "task-booting-many-"+string(rune('a'+i)), model.TaskStateBooting)
	}

	decision, err := a.Check(ctx)
	if err != nil {
		t.Fatalf("Check 返回了错误: %v", err)
	}
	if !decision.Admitted {
		t.Errorf("SetLimit(0) 时即使有 50 台在装，Check.Admitted 也应为 true" +
			"（0 表示不限，向后兼容 Phase 1 的行为），got false")
	}
}

// TestAdmissionController_DefaultLimitIsPositive 验证包级默认值本身是
// 一个正数（即默认状态下准入控制是生效的、有一个保守的默认上限），而
// 不是默认 0（默认不限）。计划任务 1：「上限的默认值要给一个保守的数并
// 说明怎么调」，若默认是 0，新部署在没有显式配置时就完全不受保护，与
// 「保守默认」的要求相反。
func TestAdmissionController_DefaultLimitIsPositive(t *testing.T) {
	if defaultAdmissionLimit <= 0 {
		t.Fatalf("defaultAdmissionLimit = %d, want > 0 —— 默认值必须是一个保守的正数上限，"+
			"不能默认不限（计划任务 1：「上限的默认值要给一个保守的数并说明怎么调」）",
			defaultAdmissionLimit)
	}

	ctx := context.Background()
	store := fake.NewStore()
	a := NewAdmissionController(store)

	// 未调用 SetLimit 时应该落到包级默认值：造出正好 defaultAdmissionLimit
	// 台在装任务，此时应该刚好打满（Admitted=false）；少一台则应该放行。
	for i := 0; i < defaultAdmissionLimit; i++ {
		mustCreateTaskInState(t, store, "task-default-"+string(rune('a'+i)), model.TaskStateBooting)
	}
	decision, err := a.Check(ctx)
	if err != nil {
		t.Fatalf("Check 返回了错误: %v", err)
	}
	if decision.Limit != defaultAdmissionLimit {
		t.Errorf("NewAdmissionController 未调用 SetLimit 时 Check 返回的 Limit = %d, "+
			"want 等于包级默认值 %d —— 未注入时应该落到默认值，不是零值 0"+
			"（0 有「不限」的特殊含义，不能与「未配置」混淆）", decision.Limit, defaultAdmissionLimit)
	}
	if decision.Admitted {
		t.Errorf("已有 %d 台在装（等于默认上限），Check.Admitted = true, want false", defaultAdmissionLimit)
	}
}

// TestAdmissionController_SetLimitOverridesDefault 验证注入口：SetLimit
// 能覆盖默认值。config 包已冻结、没有准入上限这个字段（契约缺口，见任务
// 返回值 contractGaps），因此本模块必须留一个不依赖 config 包改动的注入
// 口，供装配点（cmd/lober）在未来 config 补上这个字段后接入。
func TestAdmissionController_SetLimitOverridesDefault(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	a := NewAdmissionController(store)
	a.SetLimit(1)

	mustCreateTaskInState(t, store, "task-booting-1", model.TaskStateBooting)

	decision, err := a.Check(ctx)
	if err != nil {
		t.Fatalf("Check 返回了错误: %v", err)
	}
	if decision.Admitted {
		t.Errorf("SetLimit(1) 之后已有 1 台在装，Check.Admitted = true, want false —— 注入的上限没有生效")
	}
}
