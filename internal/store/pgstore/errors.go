package pgstore

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lober/lober/internal/store"
)

// PostgreSQL 的错误代码，见 PostgreSQL 手册的附录 A。
const (
	codeUnique  = "23505" // unique_violation
	codeForeign = "23503" // foreign_key_violation
	codeCheck   = "23514" // check_violation
)

// duplicateFields 是撞上时翻成 DuplicateError 的唯一约束（主键或唯一索引），值是 DuplicateError.Field。名字要与
// schema.go 的期望值一致，由 Test唯一约束的翻译表与期望值一致 守着。
var duplicateFields = map[string]string{
	"hosts_pkey":             "sn",
	"idx_isos_sha256":        "sha256",
	"idx_hooks_name_key":     "name",
	"idx_templates_name_key": "name",
}

// translate 把一次写入的数据库错误翻成契约的错误。只翻认得的约束，认不出的原样返回，不猜：
//
//   - 23505（唯一约束）：按约束名翻成 DuplicateError。已有的那一个由 holder 查：撞上之后事务已经中止，要在事务外
//     另查；holder 为 nil 或者没查到，原样返回。
//   - 23503（外键）：期望值里的外键。del 为真（删除）时翻成 ErrReferenced：别的记录还引用着它；否则（插入、更新）
//     翻成 ErrNotFound：要引用的记录不存在。
//   - 23514（CHECK）：期望值里的 CHECK，翻成 ErrInvalid。入参在写入之前都查过，撞上的是漏查的前提。
//
// 别的错误（连接断了、失败注入抛的错）都原样返回，它们不属于契约的任何一类。
func translate(err error, del bool, holder func(field string) string) error {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return err
	}
	switch pe.Code {
	case codeUnique:
		field, ok := duplicateFields[pe.ConstraintName]
		if !ok || holder == nil {
			return err
		}
		if existing := holder(field); existing != "" {
			return &store.DuplicateError{Field: field, Existing: existing}
		}
	case codeForeign:
		if !knownConstraint(pe.ConstraintName, true) {
			return err
		}
		if del {
			return errorf(store.ErrReferenced, "还有别的记录经外键 %s 引用它", pe.ConstraintName)
		}
		return errorf(store.ErrNotFound, "外键 %s 要引用的记录不存在", pe.ConstraintName)
	case codeCheck:
		if knownConstraint(pe.ConstraintName, false) {
			return errorf(store.ErrInvalid, "写入的值违反了 %s 表的 CHECK %s", pe.TableName, pe.ConstraintName)
		}
	}
	return err
}

// knownConstraint 报告 name 是不是期望值（tables）里的外键（foreign 为真）或 CHECK。
func knownConstraint(name string, foreign bool) bool {
	for _, t := range tables {
		if foreign {
			for _, f := range t.foreign {
				if f.name == name {
					return true
				}
			}
			continue
		}
		for _, c := range t.checks {
			if c.name == name {
				return true
			}
		}
	}
	return false
}

// errorf 返回属于 kind 这一类的错误，写明是怎么回事；调用方用 errors.Is 判断类别。
func errorf(kind error, format string, args ...any) error {
	return fmt.Errorf("%w：%s", kind, fmt.Sprintf(format, args...))
}

// badID 是收到的 ID 不合 model.ValidID。
func badID(id string) error {
	return errorf(store.ErrInvalid, "ID %q 不合格式", id)
}

// staleRev 是调用方带回的 Rev 与现在的对不上：记录在它读过之后改过了。
func staleRev(what string, have, got int64) error {
	return errorf(store.ErrConflict, "%s 现在的 Rev 是 %d，收到的是 %d：重读之后再判断", what, have, got)
}
