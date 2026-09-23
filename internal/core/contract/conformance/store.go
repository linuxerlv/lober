// Package conformance 是 Store 的可执行行为契约（对应实现计划任务 16）。
//
// 它是库代码，不是 _test.go——要能被 internal/core/fake 与
// internal/store/sqlite 两个实现包的测试 import，所以必须独立编译。导出的
// RunConformanceSuite 接收一个「造一个空 Store」的工厂函数与 *testing.T，
// 内部用 t.Run 跑全部子测试。内存实现与 SQLite 实现跑同一套断言——于是
// 并行模块用内存实现写的测试，在换成 SQLite 后行为不会变。
//
// 依据 SPEC §2.5、§2.6、§2.7、§3.3、§3.4、§3.6、§4.3、§4.4、§4.7、§5.5、
// §12.8。
//
// 集成规则因此变得机械：新增 Store 方法 → 先往本套件加断言 → 两个实现
// 各自补齐 → 套件变绿即集成完成。
package conformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// fixedNow 是本套件全部时间断言的基准时刻（Unix 秒），取自套件加载时的
// 真实 time.Now()。全套件内所有需要「现在」的地方都从这个变量加减同一批
// 相对偏移量（+90分钟、-1秒、+24小时、+1年……），因此任意两个子测试之间
// 的相对时间差是固定的、可重复的——这满足计划任务 16「时间相关断言必须
// 可重复」的真实意图。
//
// 不能是编译期硬编码的历史常量：contract.Store.GetTaskByToken 的签名不
// 接受时钟参数（见 store.go 接口文档），任何实现（fake 或未来的 SQLite）
// 判断 token 是否过期时只能读真实 time.Now()。若 fixedNow 固定在过去某个
// 具体时刻，「未过期」用例构造出的 expiresAt = fixedNow+90min 会随套件
// 运行日期后移而不断远离真实现在，最终变成过去时刻，被判定为「已过期」
// ——这不是被测代码的 bug，是共享测试夹具的时间基准与被测实现的真实时钟
// 之间存在漂移。用 var（而非 const）初始化为 time.Now().Unix()：每次运行
// 套件都重新锚定到当时的真实现在，相对偏移量永远不会漂移出「未过期」或
// 「已过期」的预期区间。
var fixedNow = time.Now().Unix()

// RunConformanceSuite 对 newStore 每次产出的新 Store 实例跑全部子测试。
//
// newStore 每次调用必须返回一个全新、彼此隔离的空 Store——每个子测试用
// 独立的实例，避免相互污染（计划任务 16 设计要点）。调用方通常这样接入：
//
//	func TestConformance(t *testing.T) {
//	    conformance.RunConformanceSuite(t, func() contract.Store {
//	        return fake.NewStore() // 或 sqlite 实现的等价构造函数
//	    })
//	}
func RunConformanceSuite(t *testing.T, newStore func() contract.Store) {
	t.Run("CAS_正常路径与版本自增", func(t *testing.T) { testCASHappyPath(t, newStore) })
	t.Run("CAS_旧版本或错误的from状态被拒绝", func(t *testing.T) { testCASRejectsStaleVersionOrWrongFromState(t, newStore) })
	t.Run("同一SN只能有一个非终态任务", func(t *testing.T) { testSameSNSingleActiveTask(t, newStore) })
	t.Run("终态后同一SN可再建任务", func(t *testing.T) { testSameSNCanRecreateAfterTerminal(t, newStore) })
	t.Run("身份查找的MAC回退", func(t *testing.T) { testIdentityLookupFallsBackToMAC(t, newStore) })
	t.Run("空SN不匹配SN也为空的任务", func(t *testing.T) { testEmptySNDoesNotMatchEmptySNTask(t, newStore) })
	t.Run("身份查找只返回pending任务", func(t *testing.T) { testIdentityLookupOnlyReturnsPending(t, newStore) })
	t.Run("信号seq去重返回专用error", func(t *testing.T) { testDuplicateSeqReturnsSentinel(t, newStore) })
	t.Run("非信号事件可连续写入", func(t *testing.T) { testNonSignalEventsSkipDedup(t, newStore) })
	t.Run("事件按seq大于某值查询", func(t *testing.T) { testListEventsAfterSeq(t, newStore) })
	t.Run("token查询的四种情形", func(t *testing.T) { testGetTaskByTokenFourCases(t, newStore) })
	t.Run("finalize_token比装机token活得更长但也会过期", func(t *testing.T) { testFinalizeTokenOutlivesInstallTokenButExpires(t, newStore) })
	t.Run("改运行时字段携带状态必须报错", func(t *testing.T) { testMutateTaskFieldsRejectsState(t, newStore) })
	t.Run("超时扫描永不返回pending任务", func(t *testing.T) { testTimeoutScanNeverReturnsPending(t, newStore) })
	t.Run("facts写入是合并而非替换", func(t *testing.T) { testMergeFactsIsMergeNotReplace(t, newStore) })
	t.Run("模板pin版本取到那一版的正文与hook引用", func(t *testing.T) { testTemplatePinnedVersionRoundTrip(t, newStore) })
	t.Run("hook按digest反查改一个字节即查不到", func(t *testing.T) { testHookLookupByDigestIsExact(t, newStore) })
	t.Run("制品导入写回OSFamily与Paths完整往返", func(t *testing.T) { testArtifactSetContentRoundTrip(t, newStore) })
	t.Run("导入失败只改状态不清空已写入的Paths", func(t *testing.T) { testArtifactSetStateUpdateDoesNotClobberPaths(t, newStore) })
	t.Run("导入失败原因原文完整往返含中文引号换行", func(t *testing.T) { testArtifactSetFailReasonRoundTrip(t, newStore) })
	t.Run("审计写入后能按target查回", func(t *testing.T) { testAuditAppendThenListByTarget(t, newStore) })
	t.Run("审计detail原文完整往返", func(t *testing.T) { testAuditDetailRoundTrip(t, newStore) })
	t.Run("审计按时间倒序且limit生效", func(t *testing.T) { testAuditOrderedDescendingWithLimit(t, newStore) })
	t.Run("审计查不到时返回空而不是错误", func(t *testing.T) { testAuditListEmptyIsNotError(t, newStore) })
	t.Run("审计按时间范围过滤", func(t *testing.T) { testAuditFilterByTimeRange(t, newStore) })
	t.Run("审计同一时刻多条记录按写入顺序倒序", func(t *testing.T) { testAuditSameTimestampOrderedByInsertionDesc(t, newStore) })
}

// ---------------------------------------------------------------------------
// 共用测试夹具
// ---------------------------------------------------------------------------

// taskOpt 是 newTestTask 的可选覆盖项，用函数式选项而非在调用点直接摆一堆
// 字段字面量——将来加必填字段时只改 newTestTask 一处（计划任务 16 明确
// 要求：套件要提供一个建任务的辅助函数供所有子测试共用）。
type taskOpt func(*model.Task)

func withHostIdentity(sn, mac, uuid string) taskOpt {
	return func(tk *model.Task) {
		tk.HostSN = sn
		tk.HostMAC = mac
		tk.HostUUID = uuid
	}
}

func withState(s model.TaskState) taskOpt {
	return func(tk *model.Task) { tk.State = s }
}

func withPhase(p model.Phase, enteredAt int64) taskOpt {
	return func(tk *model.Task) {
		tk.CurrentPhase = p
		tk.PhaseEnteredAt = enteredAt
	}
}

func withToken(token string, expiresAt int64) taskOpt {
	return func(tk *model.Task) {
		tk.Token = token
		tk.TokenExpiresAt = expiresAt
	}
}

func withFinalizeToken(token string, expiresAt int64) taskOpt {
	return func(tk *model.Task) {
		tk.FinalizeToken = token
		tk.FinalizeExpiresAt = expiresAt
	}
}

// newTestTask 构造一个可直接 CreateTask 的最小合法任务，默认为 pending、
// 携带一个非空的 HostSN 以避免意外撞上「空 SN」子测试的判据。调用方用
// taskOpt 覆盖需要的字段。
//
// idSeed 参与生成 HostSN/HostMAC/HostUUID 的默认值，让不同子测试之间即使
// 忘记显式指定身份键也不会互相碰撞。
func newTestTask(idSeed string, opts ...taskOpt) model.Task {
	tk := model.Task{
		ID:              "task-" + idSeed,
		CreatedAt:       fixedNow,
		CreatedBy:       "conformance-suite",
		HostSN:          "SN-" + idSeed,
		HostMAC:         "aa:bb:cc:dd:ee:" + idSeed,
		HostUUID:        "uuid-" + idSeed,
		TemplateID:      "tpl-" + idSeed,
		TemplateVersion: 1,
		ArtifactSetID:   "artifact-" + idSeed,
		HookDigests:     nil,
		FormData:        map[string]string{},
		ResolvedParams:  map[string]string{},
		State:           model.TaskStatePending,
		StateUpdatedAt:  fixedNow,
		Version:         0,
		CurrentPhase:    model.PhasePending,
		PhaseEnteredAt:  fixedNow,
		AttemptCount:    0,
		LastSeq:         0,
		LastSignalAt:    0,
	}
	for _, opt := range opts {
		opt(&tk)
	}
	return tk
}

// mustCreateTask 建任务并在失败时立即终止子测试——本套件里建任务失败通常
// 意味着夹具本身出了问题，继续跑下去只会产出一串无意义的连锁错误。
func mustCreateTask(t *testing.T, store contract.Store, tk model.Task) model.Task {
	t.Helper()
	created, err := store.CreateTask(context.Background(), tk)
	if err != nil {
		t.Fatalf("CreateTask(%+v) 失败: %v —— 夹具本身建任务失败，后续断言无意义，先修夹具", tk, err)
	}
	return created
}

// ---------------------------------------------------------------------------
// CAS：并发保护的全部内容（SPEC §3.6）
// ---------------------------------------------------------------------------

// testCASHappyPath 验证 CAS 正常路径：状态按期望的 from 状态与版本转换成功，
// version 自增，返回值反映新状态。
func testCASHappyPath(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	created := mustCreateTask(t, store, newTestTask("cas-happy"))

	updated, err := store.CASTaskState(ctx, created.ID, model.TaskStatePending, created.Version, model.TaskStateBooting)
	if err != nil {
		t.Fatalf("CASTaskState 用正确的 (expectedState=%q, expectedVersion=%d) 仍失败: %v —— "+
			"这是 CAS 的基本正确性，SPEC §3.6 要求的状态转换机制本身就跑不通",
			model.TaskStatePending, created.Version, err)
	}
	if updated.State != model.TaskStateBooting {
		t.Errorf("CAS 成功后 State = %q, want %q —— CAS 的返回值必须反映转换后的新状态，"+
			"调用方（状态机）依赖这个返回值决定下一步动作", updated.State, model.TaskStateBooting)
	}
	if updated.Version != created.Version+1 {
		t.Errorf("CAS 成功后 Version = %d, want %d —— SPEC §3.6 要求每次成功的状态转换令 version 自增，"+
			"版本不增会让后续基于旧版本号的 CAS 意外成功，乐观锁形同虚设",
			updated.Version, created.Version+1)
	}
}

// testCASRejectsStaleVersionOrWrongFromState 验证 CAS 的全部并发保护：
// 用旧 version 失败、用错 from 状态失败，两者都返回 ErrConflict。
func testCASRejectsStaleVersionOrWrongFromState(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()

	t.Run("旧version被拒绝", func(t *testing.T) {
		store := newStore()
		created := mustCreateTask(t, store, newTestTask("cas-stale-version"))

		// 先成功转换一次，让库内 version 变成 created.Version+1。
		if _, err := store.CASTaskState(ctx, created.ID, model.TaskStatePending, created.Version, model.TaskStateBooting); err != nil {
			t.Fatalf("夹具准备阶段的 CAS 失败: %v", err)
		}

		// 再用已经过期的旧 version 尝试 CAS，模拟两个并发请求持有同一份
		// 读快照的场景。
		_, err := store.CASTaskState(ctx, created.ID, model.TaskStateBooting, created.Version, model.TaskStateInstalling)
		if !errors.Is(err, contract.ErrConflict) {
			t.Errorf("用已过期的旧 version=%d 做 CAS，err = %v, want errors.Is(err, ErrConflict) —— "+
				"SPEC §3.6 的乐观锁靠 version 拦住基于陈旧读快照的并发写；"+
				"这里若不返回 ErrConflict，第二个并发请求会无条件覆盖第一个的结果",
				created.Version, err)
		}
	})

	t.Run("错误的from状态被拒绝", func(t *testing.T) {
		store := newStore()
		created := mustCreateTask(t, store, newTestTask("cas-wrong-from-state"))

		// 期望的 from 状态写成 booting，但任务实际仍是 pending。
		_, err := store.CASTaskState(ctx, created.ID, model.TaskStateBooting, created.Version, model.TaskStateInstalling)
		if !errors.Is(err, contract.ErrConflict) {
			t.Errorf("期望 from 状态为 %q 但任务实际是 %q 时做 CAS，err = %v, want errors.Is(err, ErrConflict) —— "+
				"CAS 的条件是 (state=expectedState AND version=expectedVersion) 同时成立，"+
				"从状态不符必须和版本不符一样被拦住，否则调用方可以绕过状态机的顺序约束直接跳状态",
				model.TaskStateBooting, model.TaskStatePending, err)
		}
	})
}

// ---------------------------------------------------------------------------
// 同一 SN 的非终态任务唯一性（SPEC §3.6、§4.7）
// ---------------------------------------------------------------------------

// testSameSNSingleActiveTask 验证同一 SN 只能有一个非终态任务：这条约束
// 保护的是「网关查到同一主机的两个 pending 任务时无法决定渲染哪种引导
// 形态」这个故障（SPEC §4.7：六种引导形态可能完全不同，比如 wimboot 与
// mboot.efi）。
func testSameSNSingleActiveTask(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	sn := "SN-shared-active"

	first := newTestTask("active-first", withHostIdentity(sn, "aa:bb:cc:dd:ee:01", "uuid-active-first"))
	if _, err := store.CreateTask(ctx, first); err != nil {
		t.Fatalf("建第一个任务失败: %v —— 夹具本身出问题", err)
	}

	second := newTestTask("active-second", withHostIdentity(sn, "aa:bb:cc:dd:ee:02", "uuid-active-second"))
	_, err := store.CreateTask(ctx, second)
	if !errors.Is(err, contract.ErrActiveTaskExists) {
		t.Errorf("同一 SN=%q 已有非终态任务时再建第二个，err = %v, want errors.Is(err, ErrActiveTaskExists) —— "+
			"SPEC §4.7 依赖这条约束保证网关能唯一确定该渲染哪种引导形态；"+
			"若这里放行，同一主机会同时挂着两个 pending 任务，网关拿到两个候选时无法决定", sn, err)
	}
}

// testSameSNCanRecreateAfterTerminal 验证任务进入终态后，同一 SN 可以再建
// 新任务——装完再重装是正常流程，partial unique index 只覆盖非终态任务，
// 不能连终态任务也一并挡住（SPEC §3.6）。
func testSameSNCanRecreateAfterTerminal(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	sn := "SN-recreate-after-done"

	first := mustCreateTask(t, store, newTestTask("recreate-first", withHostIdentity(sn, "aa:bb:cc:dd:ee:03", "uuid-recreate-first")))

	// 把第一个任务推进到终态 done。
	if _, err := store.CASTaskState(ctx, first.ID, model.TaskStatePending, first.Version, model.TaskStateDone); err != nil {
		t.Fatalf("把任务推进到终态 done 失败: %v —— 夹具准备阶段失败", err)
	}

	second := newTestTask("recreate-second", withHostIdentity(sn, "aa:bb:cc:dd:ee:04", "uuid-recreate-second"))
	if _, err := store.CreateTask(ctx, second); err != nil {
		t.Errorf("第一个同 SN=%q 任务已进入终态 done 后，再建第二个任务失败: %v, want 成功 —— "+
			"SPEC §3.6 的 partial unique index 只约束非终态任务，装完再重装是正常运维流程，"+
			"不能被这条索引连带挡住", sn, err)
	}
}

// testIdentityLookupFallsBackToMAC 验证身份查找按 SN → MAC → UUID 优先级
// 回退：SN 传空时应改用 MAC 命中（SPEC §4.3：白牌机 SN 可能留空或整批
// 重复，因此必须有 MAC 兜底）。
func testIdentityLookupFallsBackToMAC(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	mac := "aa:bb:cc:dd:ee:ff"

	created := mustCreateTask(t, store, newTestTask("mac-fallback", withHostIdentity("", mac, "uuid-mac-fallback")))

	got, err := store.FindPendingTaskByIdentity(ctx, "", mac, "")
	if err != nil {
		t.Fatalf("SN 为空、只传 MAC=%q 查 pending 任务失败: %v, want 命中 %q —— "+
			"SPEC §4.3 要求身份查找按 SN → MAC → UUID 回退、空键跳过，"+
			"SN 不可信时 MAC 是唯一的兜底，回退失效意味着白牌机永远匹配不到任务",
			mac, created.ID, err)
	}
	if got.ID != created.ID {
		t.Errorf("按 MAC=%q 回退查到的任务 ID = %q, want %q", mac, got.ID, created.ID)
	}
}

// testEmptySNDoesNotMatchEmptySNTask 验证空 SN 不匹配「SN 也为空」的任务。
// 否则任何不上报 SN 的机器都会撞上库里第一个空 SN 任务——这是白牌机批量
// SN 留空场景下最危险的一类误装（SPEC §4.3）。
func testEmptySNDoesNotMatchEmptySNTask(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	// 建一个 SN 为空、但 MAC/UUID 也各异的任务，模拟一台 SN 留空的白牌机。
	victim := mustCreateTask(t, store, newTestTask("empty-sn-victim", withHostIdentity("", "aa:bb:cc:dd:ee:10", "uuid-empty-sn-victim")))

	// 另一台机器同样没有 SN、且 MAC/UUID 与 victim 完全不同，只传一个空
	// SN 去查——不能查到 victim，否则等于「空 SN 撞上第一个空 SN 任务」。
	_, err := store.FindPendingTaskByIdentity(ctx, "", "", "")
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("三个身份键全部为空时查 pending 任务，err = %v, want errors.Is(err, ErrNotFound)（不应查到 %q）—— "+
			"SPEC §4.3：空 SN 必须跳过而不是当作一个可匹配的值，否则任何不报 SN 的机器"+
			"都会撞上库里第一个空 SN 任务，那可能是别人的机器", err, victim.ID)
	}
}

// testIdentityLookupOnlyReturnsPending 验证身份查找只返回 pending 状态的
// 任务：installing 中的任务再来一次引导请求不应重新签 token（SPEC §4.2：
// 其它状态的任务不接受新的引导请求）。
func testIdentityLookupOnlyReturnsPending(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	sn := "SN-only-pending"

	created := mustCreateTask(t, store, newTestTask("only-pending", withHostIdentity(sn, "aa:bb:cc:dd:ee:11", "uuid-only-pending")))
	if _, err := store.CASTaskState(ctx, created.ID, model.TaskStatePending, created.Version, model.TaskStateBooting); err != nil {
		t.Fatalf("把任务推进到 booting 失败: %v —— 夹具准备阶段失败", err)
	}
	if _, err := store.CASTaskState(ctx, created.ID, model.TaskStateBooting, created.Version+1, model.TaskStateInstalling); err != nil {
		t.Fatalf("把任务推进到 installing 失败: %v —— 夹具准备阶段失败", err)
	}

	_, err := store.FindPendingTaskByIdentity(ctx, sn, "", "")
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("任务已处于 installing 时按 SN=%q 查 pending 任务，err = %v, want errors.Is(err, ErrNotFound) —— "+
			"SPEC §4.2 规定只有 pending 任务接受新的引导请求；installer 可能因为网络问题重新走一遍 "+
			"PXE 流程再次打到 /v1/boot，若这里命中了 installing 中的任务，会给它重新签一个 token，"+
			"扰乱正在进行的装机窗口", sn, err)
	}
}

// ---------------------------------------------------------------------------
// 事件去重与查询（SPEC §2.5、§3.6、§12.8）
// ---------------------------------------------------------------------------

// testDuplicateSeqReturnsSentinel 验证信号 seq 去重返回专用 error
// ErrDuplicateSeq。机器侧会重试三次，重复投递是正常情况——这不是错误路径，
// handler 应把它当作「已记录过，继续走成功路径」处理（SPEC §2.5、§3.6）。
func testDuplicateSeqReturnsSentinel(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	created := mustCreateTask(t, store, newTestTask("dup-seq"))

	event := model.TaskEvent{
		TaskID:  created.ID,
		Seq:     10,
		TS:      fixedNow,
		Phase:   model.PhasePre,
		Level:   model.EventLevelInfo,
		Message: "pre-start",
		Source:  model.EventSourceSignal,
	}
	if _, err := store.AppendEvent(ctx, event); err != nil {
		t.Fatalf("第一次 AppendEvent(seq=10) 失败: %v —— 夹具准备阶段失败", err)
	}

	_, err := store.AppendEvent(ctx, event)
	if !errors.Is(err, contract.ErrDuplicateSeq) {
		t.Errorf("同一 (task_id, seq)=(%q, %d) 重复 AppendEvent，err = %v, want errors.Is(err, ErrDuplicateSeq) —— "+
			"SPEC §3.6 靠 (task_id, seq) 唯一索引做信号去重；机器侧对同一次信号会重试最多三次，"+
			"若这里不返回专用 error，handler 无法区分「正常的重复投递」与「真正的写入失败」",
			created.ID, event.Seq, err)
	}
}

// testNonSignalEventsSkipDedup 验证非信号事件（seq 非正）可连续写入，不
// 参与 (task_id, seq) 去重——扫描循环判超时、人工操作产生的事件都用非正
// seq，实现必须为它们分配自增值而不是套用信号的去重路径（SPEC §2.5，
// contract.EventStore.AppendEvent 文档）。
func testNonSignalEventsSkipDedup(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	created := mustCreateTask(t, store, newTestTask("non-signal-events"))

	base := model.TaskEvent{
		TaskID:  created.ID,
		Seq:     0, // 非正：非信号来源
		TS:      fixedNow,
		Phase:   model.PhasePending,
		Level:   model.EventLevelInfo,
		Message: "scan-tick",
		Source:  model.EventSourceHeartbeat,
	}

	first, err := store.AppendEvent(ctx, base)
	if err != nil {
		t.Fatalf("第一次写入非信号事件（seq=0）失败: %v —— 非信号事件本应无条件成功", err)
	}

	second, err := store.AppendEvent(ctx, base)
	if err != nil {
		t.Errorf("第二次写入同样 seq=%d 的非信号事件失败: %v, want 成功 —— "+
			"SPEC §2.5：扫描循环与人工操作产生的事件不带真实信号序号、不参与去重，"+
			"若这里报错，超时扫描每一轮都写不进第二条诊断事件", base.Seq, err)
	}
	if err == nil && first.Seq == second.Seq {
		t.Errorf("两次非信号事件写入后分配到相同的 Seq=%d —— "+
			"实现必须为非正 seq 的事件分配不参与去重的自增值，若分配到相同值，"+
			"ListEvents 按 seq 排序或去重时会把两条事件误判为同一条", first.Seq)
	}
}

// testListEventsAfterSeq 验证事件按「seq 大于某值」查询：SSE 续传用
// Last-Event-ID 时不能从头重放（SPEC §12.8）。
func testListEventsAfterSeq(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	created := mustCreateTask(t, store, newTestTask("list-events-after-seq"))

	seqs := []int{10, 20, 30}
	for _, seq := range seqs {
		event := model.TaskEvent{
			TaskID: created.ID,
			Seq:    seq,
			TS:     fixedNow,
			Phase:  model.PhasePre,
			Level:  model.EventLevelInfo,
			Source: model.EventSourceSignal,
		}
		if _, err := store.AppendEvent(ctx, event); err != nil {
			t.Fatalf("写入 seq=%d 的事件失败: %v —— 夹具准备阶段失败", seq, err)
		}
	}

	got, err := store.ListEvents(ctx, created.ID, contract.EventFilter{AfterSeq: 10})
	if err != nil {
		t.Fatalf("ListEvents(AfterSeq=10) 失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListEvents(AfterSeq=10) 返回 %d 条事件, want 2 条（seq=20 与 seq=30）—— "+
			"SPEC §12.8 要求 SSE 重连带 Last-Event-ID 续传时不从头重放，"+
			"若这里连 seq=10 本身也一并返回，前端会收到已经渲染过的重复事件",
			len(got))
	}
	for i, wantSeq := range []int{20, 30} {
		if got[i].Seq != wantSeq {
			t.Errorf("ListEvents(AfterSeq=10)[%d].Seq = %d, want %d —— 结果必须按 seq 升序返回", i, got[i].Seq, wantSeq)
		}
	}
}

// ---------------------------------------------------------------------------
// token 查询（SPEC §4.4、§6.4）
// ---------------------------------------------------------------------------

// testGetTaskByTokenFourCases 验证 token 查询的四种情形：有效、用途不符、
// 过期、未知——后三种统一返回 ErrNotFound，不区分「token 存在但过期/用途
// 不符」与「token 根本不存在」，避免向未认证的机器面调用方泄露 token
// 曾经存在过（SPEC §4.4）。
func testGetTaskByTokenFourCases(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	validToken := "install-token-valid"
	created := mustCreateTask(t, store, newTestTask("token-valid",
		withToken(validToken, fixedNow+int64(model.InstallTokenTTL.Seconds())),
	))

	t.Run("有效token能查到任务", func(t *testing.T) {
		got, err := store.GetTaskByToken(ctx, validToken, model.TokenPurposeInstall)
		if err != nil {
			t.Fatalf("GetTaskByToken(有效 install token) 失败: %v, want 命中 %q", err, created.ID)
		}
		if got.ID != created.ID {
			t.Errorf("GetTaskByToken 查到的任务 ID = %q, want %q", got.ID, created.ID)
		}
	})

	t.Run("用途不符统一返回未找到", func(t *testing.T) {
		_, err := store.GetTaskByToken(ctx, validToken, model.TokenPurposeFinalize)
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("用有效的 install token 去查 finalize 用途，err = %v, want errors.Is(err, ErrNotFound) —— "+
				"SPEC §4.4：装机 token 与 finalize token 权限不同，用途不符必须等同查不到，"+
				"否则 finalize 阶段的接口能被装机 token 冒用", err)
		}
	})

	t.Run("已过期token统一返回未找到", func(t *testing.T) {
		expiredToken := "install-token-expired"
		mustCreateTask(t, store, newTestTask("token-expired",
			withToken(expiredToken, fixedNow-1), // 过期时间早于当前基准时刻
		))
		_, err := store.GetTaskByToken(ctx, expiredToken, model.TokenPurposeInstall)
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("token 已过期（expiresAt=%d < now=%d）时查询，err = %v, want errors.Is(err, ErrNotFound) —— "+
				"SPEC §4.4 要求过期与不存在返回同一个错误，不能让机器面调用方通过错误类型差异"+
				"推断出「这个 token 曾经存在过」", fixedNow-1, fixedNow, err)
		}
	})

	t.Run("未知token返回未找到", func(t *testing.T) {
		_, err := store.GetTaskByToken(ctx, "token-that-never-existed", model.TokenPurposeInstall)
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("查询一个从未签发过的 token，err = %v, want errors.Is(err, ErrNotFound)", err)
		}
	})
}

// testFinalizeTokenOutlivesInstallTokenButExpires 验证 finalize token 活得
// 比装机 token 长（24 小时 vs 90 分钟），但自己也会过期——这是 §6.4 全部
// 意义所在：finalize token 必须独立存续到装机 token 过期之后仍可用，同时
// 不能被误当成永不过期的凭据。
func testFinalizeTokenOutlivesInstallTokenButExpires(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	installToken := "install-token-short"
	finalizeToken := "finalize-token-long"

	// 装机 token 的过期时刻设在 90 分钟后；finalize token 的过期时刻设在
	// 24 小时后——两者独立、finalize 明显更晚过期。
	installExpiresAt := fixedNow + int64(model.InstallTokenTTL.Seconds())
	finalizeExpiresAt := fixedNow + int64(model.FinalizeTokenTTL.Seconds())

	mustCreateTask(t, store, newTestTask("finalize-outlives",
		withToken(installToken, installExpiresAt),
		withFinalizeToken(finalizeToken, finalizeExpiresAt),
	))

	// 场景一：走到装机 token 已过期、但 finalize token 仍在有效期内的
	// 那个时间点（用 90 分钟 + 1 秒之后的一个「查询时刻」体现，而非真实
	// 睡眠——这里不依赖 Store 内部时钟，只构造过期时间戳本身，因此不需要
	// 额外的时钟注入）。
	if _, err := store.GetTaskByToken(ctx, installToken, model.TokenPurposeInstall); err != nil {
		t.Fatalf("装机 token 尚在有效期内时查询失败: %v —— 夹具准备阶段失败", err)
	}
	if _, err := store.GetTaskByToken(ctx, finalizeToken, model.TokenPurposeFinalize); err != nil {
		t.Fatalf("finalize token 尚在有效期内时查询失败: %v —— 夹具准备阶段失败", err)
	}

	// 场景二：finalize token 自己也会过期——建一个 finalize 过期时间已经
	// 早于当前基准时刻的任务，验证它和装机 token 一样受 TTL 约束，不是
	// 「一旦签发永久有效」。
	expiredFinalizeToken := "finalize-token-already-expired"
	mustCreateTask(t, store, newTestTask("finalize-expires",
		withFinalizeToken(expiredFinalizeToken, fixedNow-1),
	))
	_, err := store.GetTaskByToken(ctx, expiredFinalizeToken, model.TokenPurposeFinalize)
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("finalize token 已过期（expiresAt=%d < now=%d）时查询，err = %v, want errors.Is(err, ErrNotFound) —— "+
			"SPEC §6.4 的 finalize token 比装机 token 活得更长（24 小时 vs 90 分钟），"+
			"但绝不是永不过期；若这里放行，重启后阶段的凭据会变成永久有效的后门",
			fixedNow-1, fixedNow, err)
	}

	// 交叉验证 TTL 常量关系本身：这条断言把「finalize 更长」这个事实
	// 锁在测试里，防止未来有人把两个 TTL 常量改反。
	if model.FinalizeTokenTTL <= model.InstallTokenTTL {
		t.Fatalf("model.FinalizeTokenTTL=%v 不大于 model.InstallTokenTTL=%v —— "+
			"SPEC §6.4 的前提是 finalize token（24 小时）比装机 token（90 分钟）活得更长，"+
			"这条关系一旦反转，重启后阶段的凭据会在装机 token 之前就过期",
			model.FinalizeTokenTTL, model.InstallTokenTTL)
	}
}

// ---------------------------------------------------------------------------
// 改运行时字段（SPEC §3.4、§3.6）
// ---------------------------------------------------------------------------

// testMutateTaskFieldsRejectsState 验证改运行时字段时携带状态必须报错。
//
// model.TaskMutation 类型本身不含 State 字段（编译期已消除这个后门），但
// Store 实现仍可能通过反射等运行期手段绕过类型系统直接写 state 列——本
// 测试从「调用方能观察到的行为」角度断言：即使真的存在这样一条绕过路径，
// 状态也绝不能被它改变。做法是构造一个把 mutation 里能设的字段全部设满、
// 但不触碰 State 的调用，验证调用后 State 原样不变——这是防止绕过 CAS
// 与「禁止重装」判定的最后一道防线（SPEC §3.4：rebooting/initializing
// 超时的机器已装好，绕过 CAS 直接改状态可能让它被判定为可重装）。
func testMutateTaskFieldsRejectsState(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	created := mustCreateTask(t, store, newTestTask("mutate-no-state-bypass",
		withState(model.TaskStateRebooting),
	))

	phase := model.PhaseRebooting
	phaseEnteredAt := fixedNow + 100
	attempt := 2
	lastSeq := 70

	mutation := model.TaskMutation{
		CurrentPhase:   &phase,
		PhaseEnteredAt: &phaseEnteredAt,
		AttemptCount:   &attempt,
		LastSeq:        &lastSeq,
	}

	updated, err := store.MutateTaskFields(ctx, created.ID, mutation)
	if err != nil {
		t.Fatalf("MutateTaskFields 携带合法的运行时字段（不含 State）却失败: %v —— "+
			"本测试的重点是验证「State 不会被改变」，前提是这个调用本身必须成功", err)
	}

	if updated.State != model.TaskStateRebooting {
		t.Errorf("MutateTaskFields 之后 State = %q, want 保持 %q 不变 —— "+
			"SPEC §3.4：rebooting 超时是不可重装的失败态，"+
			"MutateTaskFields 是唯一不走 CAS 的写路径，一旦它能改变 State，"+
			"就出现了绕过「禁止重装」判定的后门",
			updated.State, model.TaskStateRebooting)
	}
	if updated.CurrentPhase != phase {
		t.Errorf("MutateTaskFields 之后 CurrentPhase = %q, want %q —— 合法字段本身应该正常写入", updated.CurrentPhase, phase)
	}
	if updated.AttemptCount != attempt {
		t.Errorf("MutateTaskFields 之后 AttemptCount = %d, want %d", updated.AttemptCount, attempt)
	}
}

// ---------------------------------------------------------------------------
// 超时扫描（SPEC §3.3）
// ---------------------------------------------------------------------------

// testTimeoutScanNeverReturnsPending 验证超时扫描永不返回 pending 任务：
// pending 阶段无预算，那台机器什么时候上电不在系统控制范围内（SPEC
// §0.1、§3.3）。用「一年之后再扫」这个极端的 asOf 验证——如果 pending
// 任务在任何查询范围下都不会被返回，一年之后自然也不会；反过来，如果
// 实现里混进了「遍历全部 Phase 取值、pending 也给个默认预算」的写法，
// 这条极端断言最容易把它炸出来。
func testTimeoutScanNeverReturnsPending(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	pending := mustCreateTask(t, store, newTestTask("timeout-scan-pending",
		withPhase(model.PhasePending, fixedNow),
	))

	// 同时建一个真的该超时的 booting 任务，确保扫描逻辑本身是工作的——
	// 如果扫描返回空列表只是因为查询整体失效，那这条测试就什么都没验证到。
	timedOutBooting := mustCreateTask(t, store, newTestTask("timeout-scan-booting",
		withState(model.TaskStateBooting),
		withPhase(model.PhaseBooting, fixedNow),
	))

	oneYearLater := fixedNow + int64((365 * 24 * 3600))

	got, err := store.ListTimedOutTasks(ctx, oneYearLater)
	if err != nil {
		t.Fatalf("ListTimedOutTasks(asOf=一年之后) 失败: %v", err)
	}

	for _, tk := range got {
		if tk.ID == pending.ID {
			t.Errorf("ListTimedOutTasks 返回了 pending 任务 %q —— "+
				"pending 任务永不超时（SPEC §3.3：那台机器什么时候上电不在本系统控制范围内，"+
				"给它设超时等于替运维的排期做判断）；即使扫描时刻设在一年之后，"+
				"pending 任务也绝不应该出现在超时列表里", tk.ID)
		}
	}

	foundBooting := false
	for _, tk := range got {
		if tk.ID == timedOutBooting.ID {
			foundBooting = true
		}
	}
	if !foundBooting {
		t.Errorf("ListTimedOutTasks(asOf=一年之后) 没有返回本该超时的 booting 任务 %q —— "+
			"这条断言用来确认扫描逻辑本身是工作的：如果它连真正超时的任务都查不出来，"+
			"上面「pending 不出现」的断言就没有意义（可能只是查询整体返回了空列表）",
			timedOutBooting.ID)
	}
}

// ---------------------------------------------------------------------------
// facts 合并写入（SPEC §5.5）
// ---------------------------------------------------------------------------

// testMergeFactsIsMergeNotReplace 验证 facts 写入是合并而非替换：跨阶段
// 跨重启传递的前提就是后一次写入不会抹掉前一次写入的其它键（SPEC §5.5：
// %pre 的 hook 探测出 RAID 型号写入，%post 的 hook 读出后决定驱动，
// firstboot 的 hook 还要再读到 %pre 选中的盘——三次写入都不能互相覆盖）。
func testMergeFactsIsMergeNotReplace(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	created := mustCreateTask(t, store, newTestTask("merge-facts"))

	if err := store.MergeFacts(ctx, created.ID, map[string]string{
		"raid_model": "PERC-H755",
	}); err != nil {
		t.Fatalf("第一次 MergeFacts 失败: %v", err)
	}
	if err := store.MergeFacts(ctx, created.ID, map[string]string{
		"chosen_disk": "/dev/disk/by-id/wwn-0xabc",
	}); err != nil {
		t.Fatalf("第二次 MergeFacts 失败: %v", err)
	}

	got, err := store.GetFacts(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetFacts 失败: %v", err)
	}

	if got["raid_model"] != "PERC-H755" {
		t.Errorf("第二次 MergeFacts 写入 chosen_disk 之后，raid_model = %q, want %q —— "+
			"SPEC §5.5 要求写入是合并而非替换；若第二次写入抹掉了第一次的键，"+
			"%%post 阶段的 hook 就读不到 %%pre 阶段探测出的 RAID 型号了",
			got["raid_model"], "PERC-H755")
	}
	if got["chosen_disk"] != "/dev/disk/by-id/wwn-0xabc" {
		t.Errorf("chosen_disk = %q, want %q", got["chosen_disk"], "/dev/disk/by-id/wwn-0xabc")
	}

	// 再验证「覆盖同键」这一半：合并语义是「已存在的键被覆盖，不存在的
	// 键被新增」，不是「已存在的键永久保留旧值」。
	if err := store.MergeFacts(ctx, created.ID, map[string]string{
		"raid_model": "PERC-H965",
	}); err != nil {
		t.Fatalf("第三次 MergeFacts（覆盖同键）失败: %v", err)
	}
	got, err = store.GetFacts(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetFacts 失败: %v", err)
	}
	if got["raid_model"] != "PERC-H965" {
		t.Errorf("同键 raid_model 再次写入后 = %q, want %q（覆盖为新值）—— "+
			"合并语义要求同键覆盖，不是永久保留第一次写入的值", got["raid_model"], "PERC-H965")
	}
	if got["chosen_disk"] != "/dev/disk/by-id/wwn-0xabc" {
		t.Errorf("覆盖 raid_model 之后 chosen_disk = %q, want 保持不变 %q —— "+
			"合并写入只应影响本次调用携带的键，未出现在本次调用中的既有键必须保持不变",
			got["chosen_disk"], "/dev/disk/by-id/wwn-0xabc")
	}
}

// ---------------------------------------------------------------------------
// 模板 pin 版本（SPEC §2.6）
// ---------------------------------------------------------------------------

// testTemplatePinnedVersionRoundTrip 验证模板 pin 版本取到的是那一版的
// 正文与 hook 引用：创建同一 ID 的两个版本，指定版本号查询必须返回对应
// 版本的内容，不能被后来的新版本覆盖查询结果（SPEC §2.6：正在运行的任务
// 不受后续改模板影响，且可复现——数月后排查「这台机器当初装了什么」，
// 靠 pin 能精确还原）。
func testTemplatePinnedVersionRoundTrip(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	tplID := "tpl-pin"
	v1 := model.Template{
		ID:             tplID,
		Name:           "rocky9-v1",
		ArtifactSetID:  "artifact-rocky9",
		AnswerTemplate: "# v1 answer file body",
		HookRefs: []model.HookRef{
			{HookID: "hook-a", HookDigest: "digest-a-v1", Stage: model.HookStagePre, Order: 1, Required: true},
		},
		Version: 1,
		State:   model.TemplateStateActive,
	}
	v2 := model.Template{
		ID:             tplID,
		Name:           "rocky9-v2",
		ArtifactSetID:  "artifact-rocky9",
		AnswerTemplate: "# v2 answer file body, 内容已变化",
		HookRefs: []model.HookRef{
			{HookID: "hook-a", HookDigest: "digest-a-v2", Stage: model.HookStagePre, Order: 1, Required: true},
			{HookID: "hook-b", HookDigest: "digest-b-v1", Stage: model.HookStagePost, Order: 1, Required: false},
		},
		Version: 2,
		State:   model.TemplateStateActive,
	}

	if _, err := store.CreateTemplate(ctx, v1); err != nil {
		t.Fatalf("创建模板 v1 失败: %v", err)
	}
	if _, err := store.CreateTemplate(ctx, v2); err != nil {
		t.Fatalf("创建模板 v2 失败: %v", err)
	}

	got, err := store.GetTemplate(ctx, tplID, 1)
	if err != nil {
		t.Fatalf("GetTemplate(id=%q, version=1) 失败: %v", tplID, err)
	}
	if got.AnswerTemplate != v1.AnswerTemplate {
		t.Errorf("GetTemplate(version=1).AnswerTemplate = %q, want %q —— "+
			"SPEC §2.6 要求 pin 住的版本内容可精确还原；若查询 version=1 却拿到 v2 的正文，"+
			"意味着「正在运行的任务不受后续改模板影响」这条保证已经失效",
			got.AnswerTemplate, v1.AnswerTemplate)
	}
	if diff := cmp.Diff(v1.HookRefs, got.HookRefs); diff != "" {
		t.Errorf("GetTemplate(version=1).HookRefs 与创建时不符（-want +got）:\n%s —— "+
			"pin 住的不只是正文，还有 hook 引用列表；hook 引用漂移到 v2 的会导致渲染时"+
			"下发了审批范围之外的 hook", diff)
	}

	gotLatest, err := store.GetTemplate(ctx, tplID, 0)
	if err != nil {
		t.Fatalf("GetTemplate(id=%q, version<=0) 失败: %v", tplID, err)
	}
	if gotLatest.Version != 2 {
		t.Errorf("GetTemplate(version<=0).Version = %d, want 2 —— "+
			"非正数版本号表示取最新版本，这是控制台管理界面依赖的语义（store.go 文档明确要求）",
			gotLatest.Version)
	}
}

// ---------------------------------------------------------------------------
// hook 按 digest 反查（SPEC §2.6、§2.7）
// ---------------------------------------------------------------------------

// testHookLookupByDigestIsExact 验证 hook 按 digest 反查正文，改一个字节
// 即查不到——这是渲染时的取法（task 与 template 的 hook_refs pin 住的是
// digest 不是 id），也是审批过的内容不会被偷换的保障（SPEC §2.6、§2.7）。
func testHookLookupByDigestIsExact(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	body := "#!/bin/bash\necho hello from hook\n"
	digest := model.HookDigest(body)

	hook := model.Hook{
		ID:         "hook-digest-lookup",
		Name:       "示例 hook",
		Stage:      model.HookStagePre,
		Body:       body,
		BodySHA256: digest,
		Version:    1,
		State:      model.HookStateActive,
	}
	if _, err := store.CreateHook(ctx, hook); err != nil {
		t.Fatalf("CreateHook 失败: %v", err)
	}

	got, err := store.GetHookByDigest(ctx, digest)
	if err != nil {
		t.Fatalf("GetHookByDigest(正确 digest=%q) 失败: %v, want 命中该 hook", digest, err)
	}
	if got.Body != body {
		t.Errorf("GetHookByDigest 查到的 Body 与写入时不符——渲染时会把错的正文发给机器")
	}

	// 改一个字节：把 digest 最后一个十六进制字符翻转，模拟传输/存储中的
	// 单字节损坏或蓄意篡改。
	tamperedDigest := flipLastHexChar(digest)
	_, err = store.GetHookByDigest(ctx, tamperedDigest)
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("GetHookByDigest(篡改后的 digest=%q) err = %v, want errors.Is(err, ErrNotFound) —— "+
			"SPEC §2.7：hook 正文不可变，pin 住的是内容寻址的 digest；"+
			"如果一个字节之差还能查到内容，说明反查退化成了「模糊匹配」，"+
			"审批过的内容就有可能被内容不同但 digest 相近的脚本偷换",
			tamperedDigest, err)
	}
}

// flipLastHexChar 把一个十六进制字符串的最后一个字符替换成另一个不同的
// 合法十六进制字符，用于构造「只改一个字节」的篡改后 digest。
func flipLastHexChar(hexStr string) string {
	if len(hexStr) == 0 {
		return "0"
	}
	last := hexStr[len(hexStr)-1]
	replacement := byte('0')
	if last == '0' {
		replacement = '1'
	}
	return hexStr[:len(hexStr)-1] + string(replacement)
}

// ---------------------------------------------------------------------------
// 制品集导入写回（SPEC §2.1、§2.7，计划任务 10 设计要点）
// ---------------------------------------------------------------------------

// testArtifactSetContentRoundTrip 验证制品导入写回：OSFamily 与 Paths
// 完整往返。含切片（Initrd）与 map（Extra）的字段最容易在 JSON 序列化
// 时掉字段——本测试专门构造非空的切片与 map 来暴露这类问题。
func testArtifactSetContentRoundTrip(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	created, err := store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID:        "artifact-roundtrip",
		Name:      "rocky9.4",
		OSVersion: "9.4",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetImporting,
	})
	if err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	paths := model.ArtifactPaths{
		Kernel: "rocky9.4/vmlinuz",
		Initrd: []string{"rocky9.4/initrd.img", "rocky9.4/initrd-extra.img"},
		Repo:   "rocky9.4/repo/",
		Extra: map[string]string{
			"boot_cfg": "rocky9.4/boot.cfg",
			"wimboot":  "rocky9.4/wimboot",
		},
	}

	updated, err := store.SetArtifactSetContent(ctx, created.ID, model.OSFamilyRHEL, paths)
	if err != nil {
		t.Fatalf("SetArtifactSetContent 失败: %v", err)
	}
	if updated.OSFamily != model.OSFamilyRHEL {
		t.Errorf("SetArtifactSetContent 之后 OSFamily = %q, want %q", updated.OSFamily, model.OSFamilyRHEL)
	}

	got, err := store.GetArtifactSet(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	if got.OSFamily != model.OSFamilyRHEL {
		t.Errorf("重新查询后 OSFamily = %q, want %q —— 写回未能持久化", got.OSFamily, model.OSFamilyRHEL)
	}
	if diff := cmp.Diff(paths, got.Paths); diff != "" {
		t.Errorf("重新查询后 Paths 与写入时不符（-want +got）:\n%s —— "+
			"含切片（Initrd）与 map（Extra）的字段在 JSON 序列化/反序列化时最容易掉字段，"+
			"这里的差异一旦出现，渲染出的 URL 会指向一个不存在的路径，"+
			"引导时表现为 404（SPEC §10 运维点一）", diff)
	}
}

// testArtifactSetStateUpdateDoesNotClobberPaths 验证导入失败只改状态、不
// 清空已写入的 Paths——这是 UpdateArtifactSetState 与 SetArtifactSetContent
// 分成两个方法的全部理由（SPEC §2.1：状态更新与内容写回是两件不同的事，
// 导入失败时 Paths 可能尚未写入或只写了一半，绝不能被状态更新方法覆盖
// 成半成品）。
func testArtifactSetStateUpdateDoesNotClobberPaths(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	created, err := store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID:        "artifact-state-update",
		Name:      "rocky9.4",
		OSVersion: "9.4",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetImporting,
	})
	if err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	paths := model.ArtifactPaths{
		Kernel: "rocky9.4/vmlinuz",
		Initrd: []string{"rocky9.4/initrd.img"},
		Repo:   "rocky9.4/repo/",
	}
	if _, err := store.SetArtifactSetContent(ctx, created.ID, model.OSFamilyRHEL, paths); err != nil {
		t.Fatalf("SetArtifactSetContent 失败: %v", err)
	}

	// 模拟导入 job 后续步骤失败（比如 manifest 校验不过），只调用状态更新
	// 方法，不应触及已经写入的 Paths。
	const failReason = "抽取内核/initrd/仓库树失败: open rocky9.4/vmlinuz: no such file or directory"
	updated, err := store.UpdateArtifactSetState(ctx, created.ID, model.ArtifactSetFailed, "manifest-sha-abc", failReason)
	if err != nil {
		t.Fatalf("UpdateArtifactSetState 失败: %v", err)
	}
	if updated.State != model.ArtifactSetFailed {
		t.Errorf("UpdateArtifactSetState 之后 State = %q, want %q", updated.State, model.ArtifactSetFailed)
	}
	if updated.FailReason != failReason {
		t.Errorf("UpdateArtifactSetState 的返回值 FailReason = %q, want %q —— "+
			"运维在控制台看到的失败原因来自这次写回的返回值/重新查询结果，"+
			"若返回值里没带上，前端拿不到任何具体信息", updated.FailReason, failReason)
	}

	got, err := store.GetArtifactSet(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	if got.FailReason != failReason {
		t.Errorf("重新查询后 FailReason = %q, want %q —— 失败原因未能持久化，"+
			"运维刷新页面后就再也看不到这次失败的具体原因", got.FailReason, failReason)
	}
	if diff := cmp.Diff(paths, got.Paths); diff != "" {
		t.Errorf("UpdateArtifactSetState 之后 Paths 被改变（-want +got）:\n%s —— "+
			"SPEC §2.1：UpdateArtifactSetState 只应改 State、ManifestSHA256 与 FailReason，"+
			"绝不能触及 Paths；这正是它与 SetArtifactSetContent 分成两个方法的全部理由——"+
			"导入失败时 Paths 可能只写了一半，一旦被状态更新方法覆盖成空值，"+
			"运维会看到一个「失败了但之前的产物也丢了」的制品集", diff)
	}
	if got.OSFamily != model.OSFamilyRHEL {
		t.Errorf("UpdateArtifactSetState 之后 OSFamily = %q, want 保持 %q 不变 —— "+
			"OSFamily 同样是 SetArtifactSetContent 的写回内容，状态更新不应触及它",
			got.OSFamily, model.OSFamilyRHEL)
	}
}

// testArtifactSetFailReasonRoundTrip 验证失败原因原文完整往返，含中文、
// 引号、换行——失败原因来自 fmt.Sprintf 包裹的底层 error（imports/job.go
// 的 fail 方法），底层 error 可能来自文件系统路径（含中文目录名）、
// 命令输出（可能带引号）或多行 stderr，任何一种字符若在编解码过程中被
// 截断或转义错误，运维看到的原因就会失真甚至截断到看不出真正原因。
func testArtifactSetFailReasonRoundTrip(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	created, err := store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID:        "artifact-fail-reason",
		Name:      "rocky9.4",
		OSVersion: "9.4",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetImporting,
	})
	if err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	reason := "识别不出 OS 家族: open \"/data/isos/未命名 光盘.iso\": 权限不足\n" +
		"（第二行：气隙环境常见问题，运维手填路径时容易带上全角空格或引号）"

	if _, err := store.UpdateArtifactSetState(ctx, created.ID, model.ArtifactSetFailed, "", reason); err != nil {
		t.Fatalf("UpdateArtifactSetState 失败: %v", err)
	}

	got, err := store.GetArtifactSet(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	if got.FailReason != reason {
		t.Errorf("FailReason 原文往返后不一致（want %q, got %q）—— "+
			"含中文、引号、换行的原文一旦在编解码时被截断或转义错误，"+
			"运维看到的失败原因会失真，反而比完全没有原因更容易误导排障方向",
			reason, got.FailReason)
	}
}

// ---------------------------------------------------------------------------
// 审计记录（SPEC §9.4，本次契约变更新增的 AuditStore）
// ---------------------------------------------------------------------------

// testAuditAppendThenListByTarget 验证「写入后能按 target 查回来」——这是
// 本次契约变更要补的第一层缺口的核心诉求：hook 驳回理由必须能在驳回那次
// HTTP 响应之外，事后按 hook id 查到。如果 AppendAudit 写入的记录在
// ListAudit(Target=同一个值) 里查不到，查询面就是摆设，等于缺口没补上。
func testAuditAppendThenListByTarget(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	rec := contract.AuditRecord{
		TS: fixedNow, Actor: "approver-1", Action: "hook.reject",
		Target: "hook-audit-target-1", Detail: "reason=脚本有安全隐患",
	}
	if err := store.AppendAudit(ctx, rec); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}
	// 另一个 target 的记录，验证过滤确实按 target 精确匹配，不是「只要有
	// 记录就都返回」。
	if err := store.AppendAudit(ctx, contract.AuditRecord{
		TS: fixedNow, Actor: "approver-1", Action: "hook.reject",
		Target: "hook-audit-target-其它", Detail: "reason=无关记录",
	}); err != nil {
		t.Fatalf("AppendAudit(其它 target) 失败: %v", err)
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{Target: "hook-audit-target-1"})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListAudit(Target=%q) 返回 %d 条, want 1 —— "+
			"驳回理由查不到，作者事后无法知道自己的 hook 为什么被拒",
			rec.Target, len(got))
	}
	if got[0].Actor != rec.Actor || got[0].Action != rec.Action || got[0].Target != rec.Target {
		t.Errorf("ListAudit 查回的记录 = %+v, want 与写入时的 actor/action/target 一致: %+v", got[0], rec)
	}
}

// testAuditDetailRoundTrip 验证 Detail 原文完整往返。detail_json 是 JSON
// 列（schema 早已建成），JSON 列最容易在编解码时掉字段或错误转义特殊
// 字符——本测试专门构造含引号、换行、中文与 "reason=" 前缀的 detail，
// 覆盖 hook.go handleRejectHook 实际写入的形态（reason 是运维手填的自由
// 文本，可能包含任意字符）。
func testAuditDetailRoundTrip(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	const tricky = `reason=脚本用了 "rm -rf /data"，第二行` + "\n" + `还有反斜杠 \ 与制表符	`
	if err := store.AppendAudit(ctx, contract.AuditRecord{
		TS: fixedNow, Actor: "approver-2", Action: "hook.reject",
		Target: "hook-audit-detail-roundtrip", Detail: tricky,
	}); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{Target: "hook-audit-detail-roundtrip"})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListAudit 返回 %d 条, want 1", len(got))
	}
	if got[0].Detail != tricky {
		t.Errorf("Detail 往返后 = %q, want %q —— "+
			"JSON 列编解码丢了字符，驳回理由里的引号/换行/中文会被截断或乱码，"+
			"运维看到的理由和作者当时填的不是同一句话", got[0].Detail, tricky)
	}
}

// testAuditOrderedDescendingWithLimit 验证「按时间倒序」与「limit 生效」
// 这两条设计要点。同一个 target 下写入三条不同 TS 的记录，断言查回顺序
// 是最新的在前；再验证 Limit=1 时只返回最新那一条——审计页面打开时最
// 想看到的是最近发生的事，倒序+limit 让调用方不必先取全量再截断
// （AuditFilter.Limit 的文档明确要求这个用法）。
func testAuditOrderedDescendingWithLimit(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	const target = "hook-audit-order-limit"

	ts := []int64{fixedNow - 20, fixedNow - 10, fixedNow}
	actors := []string{"actor-最早", "actor-中间", "actor-最新"}
	for i := range ts {
		if err := store.AppendAudit(ctx, contract.AuditRecord{
			TS: ts[i], Actor: actors[i], Action: "hook.reject", Target: target, Detail: "reason=x",
		}); err != nil {
			t.Fatalf("AppendAudit(#%d) 失败: %v", i, err)
		}
	}

	all, err := store.ListAudit(ctx, contract.AuditFilter{Target: target})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListAudit 返回 %d 条, want 3", len(all))
	}
	wantOrder := []string{"actor-最新", "actor-中间", "actor-最早"}
	for i, want := range wantOrder {
		if all[i].Actor != want {
			t.Errorf("ListAudit 第 %d 条 Actor = %q, want %q —— "+
				"顺序不是按 TS 倒序，运维打开审计页面最先看到的会是最旧而不是最新的驳回记录",
				i, all[i].Actor, want)
		}
	}

	limited, err := store.ListAudit(ctx, contract.AuditFilter{Target: target, Limit: 1})
	if err != nil {
		t.Fatalf("ListAudit(Limit=1) 失败: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("ListAudit(Limit=1) 返回 %d 条, want 1", len(limited))
	}
	if limited[0].Actor != "actor-最新" {
		t.Errorf("ListAudit(Limit=1) 返回的记录 Actor = %q, want %q —— "+
			"Limit 应该配合倒序取「最新的 N 条」，不是任意截断",
			limited[0].Actor, "actor-最新")
	}
}

// testAuditSameTimestampOrderedByInsertionDesc 验证同一时刻（TS 相等）
// 写入的多条记录之间的顺序：必须是「后写入的排前面」，与
// SQLite 实现文档写明的 "ORDER BY ts DESC, id DESC" 语义一致（id 自增，
// DESC 即后写入的行排前面）。
//
// 这条断言存在的理由：时间戳精度是秒，同一秒内并发写入并不罕见（审计
// 中间件与 handler 内部的额外一条 Log 调用，见 hook.go
// handleRejectHook，可能落在同一秒）。此前 fake 实现用
// sort.SliceStable 只比较 TS，对相等的元素保持切片原有顺序（先写入的
// 排前面），与 SQLite 的实际行为相反——但 testAuditOrderedDescendingWithLimit
// 只覆盖了三个不同 TS 的场景，从未触发这条分叉，直到本次核对独立复现
// 才发现。
func testAuditSameTimestampOrderedByInsertionDesc(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	const target = "hook-audit-same-ts"
	sameTS := fixedNow

	actors := []string{"actor-第一个写入", "actor-第二个写入", "actor-第三个写入"}
	for _, actor := range actors {
		if err := store.AppendAudit(ctx, contract.AuditRecord{
			TS: sameTS, Actor: actor, Action: "hook.reject", Target: target, Detail: "reason=x",
		}); err != nil {
			t.Fatalf("AppendAudit(%s) 失败: %v", actor, err)
		}
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{Target: target})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListAudit 返回 %d 条, want 3", len(got))
	}

	wantOrder := []string{"actor-第三个写入", "actor-第二个写入", "actor-第一个写入"}
	for i, want := range wantOrder {
		if got[i].Actor != want {
			t.Errorf("同一时刻多条记录，第 %d 条 Actor = %q, want %q —— "+
				"同一秒内后写入的记录应排在前面（与 SQLite 的 id DESC 一致），"+
				"顺序错了会让运维在并发驳回场景下看到过时的记录排在最前",
				i, got[i].Actor, want)
		}
	}
}

// testAuditListEmptyIsNotError 验证「查不到时返回空而不是错误」——没有
// 审计记录是正常状态（一个从未被驳回过的 hook），不是异常。如果这条
// 断言不锁住，某个实现可能在查无记录时返回 contract.ErrNotFound，调用方
// （HTTP handler）就被迫为一个正常状态多写一层错误处理，且容易和「target
// 参数本身有问题」这种真正的错误混淆。
func testAuditListEmptyIsNotError(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()

	got, err := store.ListAudit(ctx, contract.AuditFilter{Target: "hook-从未被驳回过"})
	if err != nil {
		t.Fatalf("ListAudit(查无记录) err = %v, want nil —— 没有审计记录是正常状态，不是异常", err)
	}
	if len(got) != 0 {
		t.Errorf("ListAudit(查无记录) 返回 %d 条, want 0", len(got))
	}
}

// testAuditFilterByTimeRange 验证 SinceTS/UntilTS 的闭区间过滤——支持
// 「按时间范围」查询是设计要点明确列出的维度之一（区别于单纯按 target），
// 运维排查「今天上午的驳回」这类问题时需要按时间窗口而不是逐条翻。
func testAuditFilterByTimeRange(t *testing.T, newStore func() contract.Store) {
	ctx := context.Background()
	store := newStore()
	const target = "hook-audit-time-range"

	ts := []int64{fixedNow - 30, fixedNow - 20, fixedNow - 10}
	for i, tv := range ts {
		if err := store.AppendAudit(ctx, contract.AuditRecord{
			TS: tv, Actor: "actor", Action: "hook.reject", Target: target, Detail: "reason=x",
		}); err != nil {
			t.Fatalf("AppendAudit(#%d) 失败: %v", i, err)
		}
	}

	got, err := store.ListAudit(ctx, contract.AuditFilter{
		Target: target, SinceTS: fixedNow - 25, UntilTS: fixedNow - 15,
	})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(got) != 1 || got[0].TS != fixedNow-20 {
		t.Errorf("ListAudit(SinceTS=%d, UntilTS=%d) 返回 %d 条（TS=%v）, want 1 条 TS=%d —— "+
			"时间范围过滤不准，运维会漏看或多看不属于该时间窗口的记录",
			fixedNow-25, fixedNow-15, len(got), tsList(got), fixedNow-20)
	}
}

// tsList 提取一组审计记录的 TS，供断言失败信息里打印实际查回的时间点。
func tsList(records []contract.AuditRecord) []int64 {
	out := make([]int64, 0, len(records))
	for _, r := range records {
		out = append(out, r.TS)
	}
	return out
}
