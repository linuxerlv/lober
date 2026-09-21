// 本文件实现迁移执行器与数据库打开逻辑。对应实现计划任务 13，依据 SPEC
// §2.1-§2.7、§3.6。迁移 SQL 用 go:embed 内嵌进二进制，按文件名排序应用未
// 执行过的文件，每个文件在一个事务里应用并记录版本号。
//
// 本包只做 schema 与迁移，不含任何 Store 实现——contract.Store 的实现是
// M2 的职责，会在本目录下另起文件。
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema/*.sql
var migrationFiles embed.FS

// migrationFilePattern 约束迁移文件必须命名为 NNN_描述.sql——版本号取文件名
// 前三位数字。三位数字加下划线是硬性前缀，不符合的文件会在 applyMigrations
// 里被当作 bug 拒绝，而不是静默跳过（静默跳过会让人以为迁移生效了）。
var migrationFilePattern = regexp.MustCompile(`^(\d{3})_.+\.sql$`)

// Open 打开（或创建）一个 SQLite 数据库文件，设置四个必需的 pragma，并将
// 连接池上限设为 1，然后应用全部未执行过的迁移。
//
// 四个 pragma：
//   - journal_mode=WAL：让扫描循环的读不阻塞信号写（SPEC §3.3 超时扫描与
//     §3.6 信号写入并发发生）。
//   - foreign_keys=ON：SQLite 默认关闭外键约束，必须显式打开。
//   - busy_timeout：吸收 SQLite 单写者下的短暂竞争，而不是让并发写直接
//     以 SQLITE_BUSY 报错退出。
//   - synchronous=NORMAL：WAL 模式下 NORMAL 已能保证提交后不丢数据，
//     FULL 的额外 fsync 对本系统的吞吐没有必要。
//
// 连接池上限设为 1 是刻意的：SQLite 只允许一个写者，放开连接数只会把锁
// 竞争从「连接池排队」变成「SQLITE_BUSY 风暴」。读性能由 WAL 保证，不
// 依赖多连接。
//
// dsn 形如 "file:/path/to/db.sqlite" 或 ":memory:"，由调用方按环境决定。
func Open(dsn string) (*sql.DB, error) {
	pragmaDSN := withPragmas(dsn)
	db, err := sql.Open("sqlite", pragmaDSN)
	if err != nil {
		return nil, fmt.Errorf("sqlite: 打开数据库失败: %w", err)
	}

	// 连接池上限设为 1（设计要点：SQLite 单写者，放开连接数会把锁竞争
	// 变成 SQLITE_BUSY 风暴）。
	db.SetMaxOpenConns(1)

	if err := applyMigrations(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// withPragmas 把四个必需 pragma 追加到 DSN 的查询参数里。modernc.org/sqlite
// 支持在 DSN 里用 _pragma= 声明一组在每次建立底层连接时执行的 PRAGMA 语句，
// 这样连接池扩容或重连时 pragma 不会被遗漏——比"打开后单独 Exec 一次"更
// 可靠，因为后者只对当时那一个连接生效。
func withPragmas(dsn string) string {
	pragmas := []string{
		"journal_mode(WAL)",
		"foreign_keys(1)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	var b strings.Builder
	b.WriteString(dsn)
	for _, p := range pragmas {
		b.WriteString(sep)
		b.WriteString("_pragma=")
		b.WriteString(p)
		sep = "&"
	}
	return b.String()
}

// applyMigrations 按文件名顺序应用 schema/ 目录下未执行过的迁移文件。
//
// schema_version 表本身由 001_init.sql 建出来，不在 Go 代码里单独
// CREATE TABLE 兜底——schema 是数据契约，契约应该只有一处定义（迁移
// 文件），Go 代码里再写一份会导致两处定义漂移。首次打开时 schema_version
// 尚不存在，appliedVersions 对"表不存在"这个特定错误容错为"已应用版本
// 集合为空"，而不是报错。
//
// 流程：
//  1. 读出已应用版本集合（表不存在时视为空集合）。
//  2. 按文件名排序遍历 embed 的 .sql 文件，未应用过的才执行。
//  3. 每个文件的 SQL 与「插入 schema_version 记录」在同一个事务里提交——
//     保证不会出现"表建了但版本没记下"的中间状态（那会导致下次启动重复
//     执行已经跑过的 DDL 并报错）。
func applyMigrations(db *sql.DB) error {
	entries, err := fs.ReadDir(migrationFiles, "schema")
	if err != nil {
		return fmt.Errorf("sqlite: 读取内嵌迁移目录失败: %w", err)
	}

	type migration struct {
		version  int
		filename string
	}
	var migrations []migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationFilePattern.FindStringSubmatch(e.Name())
		if m == nil {
			return fmt.Errorf("sqlite: 迁移文件名 %q 不符合 NNN_描述.sql 格式，版本号无法解析", e.Name())
		}
		version, err := strconv.Atoi(m[1])
		if err != nil {
			return fmt.Errorf("sqlite: 迁移文件 %q 的版本号解析失败: %w", e.Name(), err)
		}
		migrations = append(migrations, migration{version: version, filename: e.Name()})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })

	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		sqlBytes, err := migrationFiles.ReadFile("schema/" + m.filename)
		if err != nil {
			return fmt.Errorf("sqlite: 读取迁移文件 %q 失败: %w", m.filename, err)
		}
		if err := applyOneMigration(db, m.version, string(sqlBytes)); err != nil {
			return fmt.Errorf("sqlite: 应用迁移 %q 失败: %w", m.filename, err)
		}
	}
	return nil
}

// appliedVersions 返回已记录在 schema_version 表中的版本号集合。
//
// schema_version 表本身由 001_init.sql 创建（见 applyMigrations 注释），
// 首次打开一个全新数据库时该表尚不存在，SQLite 返回的错误信息包含
// "no such table"——这种情况视为"已应用版本集合为空"，不是错误，
// 让后续流程正常跑第一个迁移文件把这张表建出来。
func appliedVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_version`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return map[int]bool{}, nil
		}
		return nil, fmt.Errorf("sqlite: 查询 schema_version 失败: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("sqlite: 读取 schema_version 行失败: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: 遍历 schema_version 失败: %w", err)
	}
	return applied, nil
}

// applyOneMigration 在一个事务里执行迁移 SQL 并插入版本记录。事务保证
// "DDL 生效"与"版本号落盘"是原子的——否则进程在两步之间崩溃会导致下次
// 启动重复执行已经跑过的 CREATE TABLE 而报错。
func applyOneMigration(db *sql.DB, version int, sqlText string) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("执行 DDL 失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`,
		version, time.Now().Unix()); err != nil {
		return fmt.Errorf("记录 schema_version 失败: %w", err)
	}
	return tx.Commit()
}
