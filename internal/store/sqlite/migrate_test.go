// 本文件测试迁移执行器与两条安全索引锁住的行为。对应实现计划任务 13，
// 依据 SPEC §2.1-§2.7、§3.6、§4.7。
package sqlite

import (
	"database/sql"
	"testing"
)

// openTestDB 打开一个每个测试独立的内存数据库并应用迁移。用 :memory: 而
// 非临时文件——测试不需要跨进程持久化，:memory: 更快且不留残留文件。
//
// 注意：cache=shared 是必需的——纯 :memory: 在 SetMaxOpenConns(1) 下，
// 一旦唯一连接被占用（比如一个未提交的事务持有它），后续查询会等在
// 连接池上而不是报错，可能拖慢测试。cache=shared 让同一 DSN 的多个连接
// 指向同一份内存数据库，行为更接近真实文件数据库。
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("Open() 失败: %v，测试无法继续：迁移执行器本身是被测对象，打开即失败说明 DDL 或 pragma 设置有语法错误", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestOpen_CreatesAllTenTables 锁住：迁移执行后十张表都建出来了
// （计划任务 13「测试要锁住的行为」第一条）。
//
// 不锁住这条，任何一张表的 CREATE TABLE 语句手误漏写都不会在打开阶段
// 报错，而是等到 M2 的 Store 实现第一次查询该表时才报「no such table」，
// 那时排查成本已经从「读 schema 文件」变成「跨包调试」。
func TestOpen_CreatesAllTenTables(t *testing.T) {
	db := openTestDB(t)

	wantTables := []string{
		"schema_version", "hosts", "artifact_sets", "hooks", "templates",
		"template_hook_refs", "tasks", "task_events", "task_facts", "audit_log",
	}
	for _, table := range wantTables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("表 %q 未建出来（err=%v）：SPEC §2.1-§2.7 要求的十张表之一缺失，下游 Store 实现会在运行期才发现", table, err)
		}
	}
}

// TestOpen_MigrationIsIdempotent 锁住：迁移可重复执行，第二次是空操作
// （计划任务 13「测试要锁住的行为」第二条）。
//
// 不锁住这条，服务重启（生产环境的常态，不是异常路径）会在第二次调用
// Open 时因为「表已存在」而直接崩溃退出，那是比任何业务 bug 都更早触发
// 的启动失败。
func TestOpen_MigrationIsIdempotent(t *testing.T) {
	db := openTestDB(t)

	// 第二次在同一个已迁移的数据库上调用 applyMigrations，模拟服务重启
	// 重新打开同一个数据库文件的场景。
	if err := applyMigrations(db); err != nil {
		t.Fatalf("重复应用迁移失败: %v；服务重启时会再次调用 Open()/applyMigrations，"+
			"若第二次不是空操作，进程会在启动阶段直接崩溃退出", err)
	}

	// schema_version 里 001 版本只应有一条记录，不是两条——证明第二次
	// 迁移真的没有重新执行 INSERT，而不是执行了但恰好没报错。
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_version WHERE version = 1`).Scan(&count); err != nil {
		t.Fatalf("查询 schema_version 失败: %v", err)
	}
	if count != 1 {
		t.Errorf("schema_version 里版本 1 的记录数 = %d，want 1；"+
			"这条断言的意义是区分「重复迁移真的是空操作」与「重复迁移执行了但恰好没报错」", count)
	}
}

// TestTasks_SecondActiveTaskForSameSN_Rejected 锁住：同一 SN 的第二个
// 非终态任务被拒绝（计划任务 13「测试要锁住的行为」第三条，安全索引 1）。
//
// 这条测试保护的是 SPEC §3.6、§4.7 约束 2 描述的故障：网关查到同一主机
// 的两个 pending 任务时无法决定渲染哪种引导形态——而两种形态可能是
// wimboot 与 mboot.efi 这种完全不同的东西。partial unique index 在数据库
// 层面把这个不变量做成硬约束，不依赖应用层每一处写路径都记得检查。
func TestTasks_SecondActiveTaskForSameSN_Rejected(t *testing.T) {
	db := openTestDB(t)

	insertTask := insertMinimalTaskFunc(db)

	if err := insertTask("task-1", "SN-0001", "pending"); err != nil {
		t.Fatalf("插入第一个 pending 任务失败: %v", err)
	}

	err := insertTask("task-2", "SN-0001", "booting")
	if err == nil {
		t.Errorf("同一 SN=SN-0001 的第二个非终态任务（booting）插入成功，want 被唯一索引拒绝；" +
			"没有这条约束，网关会在同一主机上看到两个非终态任务，无法决定渲染哪种引导形态（SPEC §4.7 约束 2）")
	}
}

// TestTasks_TwoTasksWithEmptySN_BothAllowed 锁住：host_sn 为空的两个任务
// 互不阻塞（安全索引 1 的 WHERE 条件里 host_sn != ” 那一半）。
//
// 白牌机 SN 可能留空，这些机器只能靠 MAC/UUID 识别（SPEC §4.3）。如果
// partial unique index 的条件漏掉 host_sn != ”，两台 SN 均为空的机器会
// 被数据库误判为"同一台机器的重复任务"而互相阻塞建任务——这个回归很容易
// 在"顺手把索引条件简化"时引入，因为空字符串本身在直觉上不像一个需要
// 特殊排除的值。
func TestTasks_TwoTasksWithEmptySN_BothAllowed(t *testing.T) {
	db := openTestDB(t)
	insertTask := insertMinimalTaskFunc(db)

	if err := insertTask("task-1", "", "pending"); err != nil {
		t.Fatalf("插入第一个空 SN 任务失败: %v", err)
	}
	if err := insertTask("task-2", "", "pending"); err != nil {
		t.Errorf("插入第二个空 SN 任务失败: %v，want 成功；两台 SN 均留空、"+
			"靠 MAC/UUID 识别的机器不应因为 host_sn 的空字符串重合而被误判为重复任务", err)
	}
}

// TestTasks_NewTaskAfterTerminal_Allowed 锁住：同一 SN 的任务进入终态
// （done/failed）后，可以为它再建一个新任务（安全索引 1 的另一半——不是
// "同 SN 只能有一个任务"，是"同 SN 只能有一个非终态任务"）。
//
// 不锁住这条会导致对安全索引的误实现：如果有人把 partial index 的条件
// 写错（比如漏了 WHERE 或条件写反），会变成"装完的机器再也不能建新任务"，
// 这是比"重复任务"更容易被忽略、但同样致命的回归——生产机器装完一次后
// 永久无法重装。
func TestTasks_NewTaskAfterTerminal_Allowed(t *testing.T) {
	db := openTestDB(t)

	insertTask := insertMinimalTaskFunc(db)

	if err := insertTask("task-1", "SN-0002", "pending"); err != nil {
		t.Fatalf("插入第一个 pending 任务失败: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET state = 'done' WHERE id = 'task-1'`); err != nil {
		t.Fatalf("把第一个任务置为 done 失败: %v", err)
	}

	if err := insertTask("task-2", "SN-0002", "pending"); err != nil {
		t.Errorf("第一个任务进入终态 done 后，为同一 SN=SN-0002 建第二个任务失败: %v，"+
			"want 成功；终态任务不应占用安全索引 1，否则装完的机器永久无法重装", err)
	}
}

// TestTaskEvents_DuplicateTaskIDSeq_Rejected 锁住：重复的 (task_id, seq)
// 被拒绝（计划任务 13「测试要锁住的行为」第四条，安全索引 2）。
//
// 这是信号去重机制的基础（SPEC §2.5、§3.6）：机器侧网络抖动或客户端重发
// 会让同一个信号（同一个 seq）被网关收到两次，唯一索引在数据库层面
// 保证第二次写入被拒绝，State 机（M3）据此判断"只记事件不改状态"还是
// "静默忽略"，这条判断的前提是数据库不会真的存下两条相同 seq 的事件行。
func TestTaskEvents_DuplicateTaskIDSeq_Rejected(t *testing.T) {
	db := openTestDB(t)

	insertTask := insertMinimalTaskFunc(db)
	if err := insertTask("task-1", "SN-0003", "pending"); err != nil {
		t.Fatalf("插入任务失败: %v", err)
	}

	insertEvent := func(seq int) error {
		_, err := db.Exec(`INSERT INTO task_events (task_id, seq, ts, phase, level, source)
			VALUES (?, ?, 0, 'pending', 'info', 'signal')`, "task-1", seq)
		return err
	}

	if err := insertEvent(1); err != nil {
		t.Fatalf("插入第一条 seq=1 的事件失败: %v", err)
	}

	if err := insertEvent(1); err == nil {
		t.Errorf("重复插入 task_id=task-1, seq=1 的事件成功，want 被唯一索引拒绝；" +
			"没有这条约束，信号重发会在事件表里留下重复行，State 机无法用 seq 判断" +
			"「已经处理过」还是「第一次收到」（SPEC §3.6）")
	}
}

// insertMinimalTaskFunc 返回一个便捷函数，插入一行满足 tasks 表 NOT NULL
// 约束的最小任务记录，供多个测试复用，避免每个测试各写一遍完整的
// INSERT 列表（那样每次表结构改动要同步改所有测试）。
func insertMinimalTaskFunc(db *sql.DB) func(id, hostSN, state string) error {
	return func(id, hostSN, state string) error {
		_, err := db.Exec(`INSERT INTO tasks (
			id, created_at, host_sn, template_id, template_version,
			artifact_set_id, state, state_updated_at, current_phase, phase_entered_at
		) VALUES (?, 0, ?, 'tpl-1', 1, 'art-1', ?, 0, 'pending', 0)`, id, hostSN, state)
		return err
	}
}
