// 本文件把 SQLite 约束冲突翻译成 contract 的 sentinel error。对应实现
// 计划任务 2，依据 SPEC §2.5、§3.6。
//
// # 与计划原文的偏差（契约缺口，详见文件末尾长注释）
//
// 计划要求「按索引名区分，不要按错误文本猜」，理由是索引名不会随驱动版本
// 升级或错误文本措辞变化而漂移。但 modernc.org/sqlite v1.59.0 的
// *sqlite.Error 不暴露索引名：它的 Error() 返回的字符串来自 SQLite 的
// sqlite3_errmsg()，对 UNIQUE 冲突给出的是「表名.列名[, 表名.列名...]」
// （已用探针程序实测确认，见下方长注释），驱动本身没有任何 API 把冲突
// 关联回具体的索引对象。本文件改为匹配「表名+列名组合」，这是同一驱动
// 版本下唯一可行、且同样不依赖错误文本措辞的稳定信号——索引名与列名一样
// 都来自 schema（数据契约），列名比索引名更不容易被顺手改掉。
package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	sqlitedrv "modernc.org/sqlite"

	"github.com/lober/lober/internal/core/contract"
)

// translateConstraintErr 把 SQLite 驱动返回的原始错误翻译成 contract 的
// sentinel error。ctxLabel 是调用点标签，用于「无法识别时原样包装返回」
// 分支的错误信息（计划任务 2 设计要点：翻译函数接收原始错误与一个上下文
// 标签，无法识别时原样包装返回而不是吞掉）。
//
// 三种已知情形：
//   - UNIQUE 冲突落在 tasks.host_sn（idx_tasks_sn_active）→ ErrActiveTaskExists
//   - UNIQUE 冲突落在 task_events.(task_id, seq)（idx_task_events_task_seq）→ ErrDuplicateSeq
//   - sql.ErrNoRows → ErrNotFound
//
// 未识别的错误原样用 %w 包装返回而不是吞掉——未知的数据库错误应该冒泡成
// 500 并进服务端日志（计划任务 2 设计要点）。err 为 nil 时返回 nil。
func translateConstraintErr(err error, ctxLabel string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return contract.ErrNotFound
	}

	var sqliteErr *sqlitedrv.Error
	if errors.As(err, &sqliteErr) {
		if isUniqueViolationOnColumns(sqliteErr, "tasks", "host_sn") {
			return contract.ErrActiveTaskExists
		}
		if isUniqueViolationOnColumns(sqliteErr, "task_events", "task_id", "seq") {
			return contract.ErrDuplicateSeq
		}
	}

	return fmt.Errorf("sqlite: %s: %w", ctxLabel, err)
}

// sqliteConstraintUnique 是 SQLite 的扩展错误码
// SQLITE_CONSTRAINT_UNIQUE（modernc.org/sqlite/lib 里的常量值）。不直接
// import 该内部 lib 包（它是驱动的实现细节子包，不是稳定 API），本文件
// 把数值抄成命名常量，注释写明来源，将来驱动升级若改了数值会被下面的
// 测试立刻炸出来。
const sqliteConstraintUnique = 2067

// isUniqueViolationOnColumns 判定该 SQLite 错误是否为「落在给定表的给定
// 列组合上」的 UNIQUE 约束冲突。
//
// 先用扩展错误码粗筛，排除同为 SQLITE_CONSTRAINT 大类但性质不同的冲突
// （如 NOT NULL、CHECK）；再解析错误消息里 "UNIQUE constraint failed: "
// 之后的表名.列名列表，要求列名集合与调用方给定的集合完全一致（不是
// 包含关系）——「完全一致」避免了 tasks 表将来新增另一个刚好也含
// host_sn 的索引时被误判成同一个索引的冲突。
func isUniqueViolationOnColumns(e *sqlitedrv.Error, table string, columns ...string) bool {
	if e.Code() != sqliteConstraintUnique {
		return false
	}

	gotTable, gotColumns, ok := parseUniqueConstraintColumns(e.Error())
	if !ok || gotTable != table {
		return false
	}
	if len(gotColumns) != len(columns) {
		return false
	}
	want := make(map[string]bool, len(columns))
	for _, c := range columns {
		want[c] = true
	}
	for _, c := range gotColumns {
		if !want[c] {
			return false
		}
	}
	return true
}

// parseUniqueConstraintColumns 从 SQLite 的 UNIQUE 冲突消息中解析出表名与
// 列名列表。消息形如：
//
//	constraint failed: UNIQUE constraint failed: tasks.host_sn (2067)
//	constraint failed: UNIQUE constraint failed: task_events.task_id, task_events.seq (2067)
//
// 即冒号后跟一个或多个逗号分隔的 "表名.列名" 词元，且同一条消息里表名
// 恒定（同一张表上的复合唯一索引）。解析失败（消息格式与预期不符，比如
// 驱动未来版本改了措辞）时 ok=false，调用方应视为「无法识别」，交给上层
// 原样包装返回，不能默默匹配成某个 sentinel error。
func parseUniqueConstraintColumns(msg string) (table string, columns []string, ok bool) {
	const marker = "UNIQUE constraint failed: "
	idx := strings.Index(msg, marker)
	if idx < 0 {
		return "", nil, false
	}
	rest := msg[idx+len(marker):]

	// 词元之间用 ", " 分隔（复合索引会有多个「表名.列名」词元），必须先
	// 按 ", " 切分再处理错误码后缀——不能先找「第一个空格」来切后缀，
	// 因为多列消息里第一个空格出现在第一个逗号之后（词元分隔符本身含
	// 空格），比末尾的 " (2067)" 更早出现，会把词元列表切成两半。
	tokens := strings.Split(rest, ", ")
	if len(tokens) == 0 || tokens[0] == "" {
		return "", nil, false
	}

	// 错误码后缀 " (2067)" 只出现在最后一个词元末尾，从最后一个词元里
	// 单独剥掉。
	last := tokens[len(tokens)-1]
	if sp := strings.IndexByte(last, ' '); sp >= 0 {
		tokens[len(tokens)-1] = last[:sp]
	}

	for _, tok := range tokens {
		dot := strings.IndexByte(tok, '.')
		if dot < 0 {
			return "", nil, false
		}
		t := tok[:dot]
		col := tok[dot+1:]
		if table == "" {
			table = t
		} else if table != t {
			// 同一条消息里出现了不同的表名——不符合「复合唯一索引落在
			// 同一张表上」的假设，视为无法识别。
			return "", nil, false
		}
		columns = append(columns, col)
	}
	return table, columns, true
}
