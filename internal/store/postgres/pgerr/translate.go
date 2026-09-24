// 本文件把 PostgreSQL/GORM 错误翻译成 contract 的 sentinel error，对应
// 实现计划任务 2，替代旧实现 internal/store/sqlite/errors.go 那 150 行
// 「解析错误文本猜列名」的方案——modernc.org/sqlite 驱动不暴露索引名，
// PG 上 *pgconn.PgError.ConstraintName 直接给索引名，本该能按索引名精确
// 映射（原实现计划想要但做不到的事）。
//
// 四个分支：nil、gorm.ErrRecordNotFound、23505 唯一冲突按索引名分派、
// 未识别错误原样包装。第三个分支是本文件存在的理由——它把「哪个索引冲突了」
// 这个数据库事实翻译成业务语义，调用方因此不需要知道任何索引名。
package pgerr

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
)

// codeUniqueViolation 是 PostgreSQL 的 SQLSTATE 错误码，unique_violation
// 的字面值，来源：PostgreSQL 文档 Appendix A.1（Error Codes），Class 23
// — Integrity Constraint Violation。抄成命名常量而不是在判断处直接写
// 字符串字面量，是为了在唯一出现的地方写清楚来源（计划任务 2 风险提示：
// 「23505 是 unique_violation 的字面码值，抄成常量并在注释里写明来源」）。
const codeUniqueViolation = "23505"

// IdxTasksSNActive / IdxTaskEventsTaskSeq 是本包依赖的两个唯一索引名，
// 导出成常量供任务 3（tasks 表）、任务 4（task_events 表）的 GORM index
// tag 引用同一份字面值——两处各写一遍字符串是任务 2 风险提示明确指出的
// 隐患（「pgerr 依赖的两个索引名与任务 3、4 的 tag 里写的必须字面一致」），
// 引用同一个常量比字符串对齐更安全。
const (
	// IdxTasksSNActive 对应 001_init.sql 里 tasks(host_sn) 的 partial
	// unique index，冲突时翻译成 contract.ErrActiveTaskExists。
	IdxTasksSNActive = "idx_tasks_sn_active"

	// IdxTaskEventsTaskSeq 对应 001_init.sql 里 task_events(task_id, seq)
	// 的 unique index，冲突时翻译成 contract.ErrDuplicateSeq。
	IdxTaskEventsTaskSeq = "idx_task_events_task_seq"
)

// Translate 把 PostgreSQL/GORM 返回的原始错误翻译成 contract 的
// sentinel error。ctxLabel 是调用点标签，用于「无法识别时原样包装返回」
// 分支的错误信息（与旧实现 sqlite/errors.go 的 translateConstraintErr
// 同一套约定）。
//
// err 为 nil 时返回 nil。已识别的两类分别翻译成对应 sentinel；未识别的
// 错误用 %w 包装 ctxLabel 后返回而不是吞掉——未知的数据库错误应该冒泡成
// 500 并进服务端日志，调用方要能用 errors.Is/errors.As 取出原错误。
func Translate(err error, ctxLabel string) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return contract.ErrNotFound
	}

	// 按 ConstraintName 分派，而不是解析错误文本里的表名列名——这正是旧
	// SQLite 实现做不到、只能退而匹配「表名+列名集合」的那一步（见
	// sqlite/errors.go 顶部的契约缺口说明）。索引名来自 schema，是数据
	// 契约的一部分，比错误消息的措辞稳定得多。
	//
	// 只认已知的两个索引名：其它索引（比如主键）的唯一冲突落到兜底分支
	// 冒泡成 500，而不是被硬塞进某个业务 sentinel——把未知冲突映射成
	// 已知业务错误会让调用方以为自己在处理一个正常业务分支。
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == codeUniqueViolation {
		switch pgErr.ConstraintName {
		case IdxTasksSNActive:
			return contract.ErrActiveTaskExists
		case IdxTaskEventsTaskSeq:
			return contract.ErrDuplicateSeq
		}
	}

	return fmt.Errorf("pgerr: %s: %w", ctxLabel, err)
}
