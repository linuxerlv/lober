// 本包为 internal/store/postgres 下八个聚合子包的测试提供数据库夹具。
//
// 为什么是真实包而不是 _test.go：计划原稿把这个夹具写成
// internal/store/postgres/testdb_test.go（仅本包测试可见），但八个聚合
// 子包（task/event/host/hook/template/artifact/fact/audit）各自的测试
// 都需要建一个独立 schema 的数据库句柄——_test.go 里的符号只在同一个
// 包内的测试文件之间可见，不能被其他包 import。改成真实包是本次实现
// 相对计划的唯一偏离，已获主会话批准。
//
// 为什么每个测试用独立 schema 而不是独立 database：PostgreSQL 的
// CREATE DATABASE 不能在事务里执行、开销也明显更大；CREATE SCHEMA 配合
// search_path 足够便宜，隔离性对测试夹具的需求同样足够（设计文档 §9）。
package pgtest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// dsnEnvVar 是测试夹具读取 DSN 的环境变量名。写成常量而不是每处内联
// 字符串，是因为错误信息、测试断言与实现三处都要用到同一个名字，
// 打错一处就会出现"错误信息提示的变量名跟实际读取的不一致"这种
// 令人困惑的 bug。
const dsnEnvVar = "LOBER_TEST_PG_DSN"

// dsnFromEnv 从环境变量读取测试用的 PostgreSQL DSN。
//
// 单独拆成一个返回 error 的函数，而不是把判断逻辑直接写在 NewDB 里
// 调 t.Fatal：t.Fatal 会终止调用它的那个测试，无法在"验证 NewDB 对
// 缺失 DSN 的处理是否正确"这个场景里被安全地观察到。拆出来之后，
// "环境变量为空时的行为"这一契约点本身变得可测——pgtest 自己的测试
// 断言 dsnFromEnv 在缺失时返回 error，而不必真的触发一次会终止测试
// 进程的 t.Fatal。
func dsnFromEnv() (string, error) {
	dsn := os.Getenv(dsnEnvVar)
	if dsn == "" {
		return "", fmt.Errorf(
			"环境变量 %s 未设置。八个聚合子包的测试都需要一个可达的 "+
				"PostgreSQL 实例，且该实例上的账号需要 CREATE SCHEMA / "+
				"DROP SCHEMA 权限（每个测试建一个独立 schema 做隔离，"+
				"结束时删除）。请设置 %s 为形如 "+
				"postgres://user:pass@host:5432/dbname?sslmode=disable "+
				"的连接串后重新运行测试",
			dsnEnvVar, dsnEnvVar,
		)
	}
	return dsn, nil
}

// newSchemaName 生成一个大概率不与并行跑的其他测试冲突的 schema 名。
//
// 八个聚合包的测试会同时跑在同一个数据库上（计划任务 1 的接口契约），
// 名字必须既能定位到是哪个测试建的（前缀含 t.Name()，方便排查残留
// schema 时知道该找哪个测试），又不会在并行场景撞车（后缀带随机数）。
//
// PostgreSQL 的 identifier 只允许字母、数字、下划线（不加引号的情况
// 下），且大小写不敏感地折叠成小写——t.Name() 里子测试常见的 "/" 会
// 被折叠函数替换成 "_"。
func newSchemaName(t *testing.T) string {
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		// crypto/rand 读失败在正常运行的系统上几乎不会发生（本机没有
		// 可用的随机数源），发生时说明环境本身已经不正常，直接 Fatal
		// 比返回一个可能重复的 schema 名更安全。
		t.Fatalf("生成随机 schema 后缀失败: %v", err)
	}

	sanitized := sanitizeForIdentifier(t.Name())
	return fmt.Sprintf("pgtest_%s_%s", sanitized, hex.EncodeToString(randBytes))
}

// sanitizeForIdentifier 把测试名里 PostgreSQL identifier 不允许的字符
// （不加引号时）替换成下划线，并截断到一个安全长度。
//
// PostgreSQL 的 identifier 上限是 63 字节，超长会被静默截断而不是报错
// ——两个前缀相同、只在超长部分不同的测试名会被截成同一个 schema 名，
// 造成难以察觉的冲突。这里主动截断到远低于上限的长度，把随机后缀留出
// 充足空间，冲突概率交给随机后缀承担而不是依赖不做任何处理的静默截断。
func sanitizeForIdentifier(name string) string {
	const maxLen = 40
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name) && len(out) < maxLen; i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c-'A'+'a')
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "test"
	}
	return string(out)
}

// NewDB 建一个独立 schema，对 models 跑 AutoMigrate，返回连到该 schema
// 的句柄。t.Cleanup 里 DROP SCHEMA ... CASCADE。
//
// DSN 未设置或连不上时 t.Fatal，不 t.Skip——skip 会让 make check 在
// store 层零覆盖的情况下变绿，比明确报错危险得多（计划任务 1 的
// 接口契约）。
func NewDB(t *testing.T, models ...any) *gorm.DB {
	dsn, err := dsnFromEnv()
	if err != nil {
		t.Fatal(err)
	}

	// 先用不带 search_path 的连接建 schema：CREATE SCHEMA 本身不依赖
	// search_path，用同一个连接池顺手做，不需要单独开一条连接。
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("连接 PostgreSQL 失败（DSN 取自环境变量 %s）: %v —— "+
			"请确认目标实例可达、DSN 格式正确", dsnEnvVar, err)
	}
	// adminDB 的池上限必须压到 1，理由是连接总量而不是 search_path。
	//
	// go test 默认按 CPU 核数并行跑包（本项目有 9 个包在用这个夹具），每个
	// 包内的用例又各自调一次 NewDB。GORM 默认不限池上限，adminDB 会随查询
	// 长出多条连接，于是并行峰值轻易超过 PostgreSQL 的 max_connections
	// （默认 100，其中 3 个还留给超级用户）——实测表现为
	// `SQLSTATE 53300: remaining connection slots are reserved for roles
	// with the SUPERUSER attribute`，且失败的包每次都不一样，看起来像随机
	// 故障而不是资源耗尽。
	//
	// adminDB 只跑 CREATE SCHEMA 与 DROP SCHEMA 两条语句，一条连接绰绰有余。
	// 下面那个 dbWithSchema 同样限 1（但那是为了 search_path 不漂移，理由
	// 不同，见该处注释）。
	if adminSQL, sqlErr := adminDB.DB(); sqlErr == nil {
		adminSQL.SetMaxOpenConns(1)
		adminSQL.SetMaxIdleConns(1)
	}

	schemaName := newSchemaName(t)
	if err := adminDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error; err != nil {
		t.Fatalf("CREATE SCHEMA %q 失败: %v —— 请确认 DSN 对应的账号"+
			"具有建 schema 的权限", schemaName, err)
	}

	t.Cleanup(func() {
		// DROP SCHEMA CASCADE 会连同该 schema 下的全部表、索引一起删除
		// ——测试夹具的清理本就应该是"这个测试建的东西全部消失"，不需要
		// 逐个删表。清理失败只记录，不让它掩盖测试本身的失败结果（这是
		// t.Cleanup 里常见的做法：清理错误如果用 t.Fatal 会覆盖掉真正
		// 的测试失败原因）。
		if err := adminDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error; err != nil {
			t.Logf("清理 schema %q 失败: %v（不影响本次测试结果，但该 "+
				"schema 可能残留，需要人工清理）", schemaName, err)
		}
		if sqlDB, err := adminDB.DB(); err == nil {
			sqlDB.Close()
		}
	})

	// search_path 只放目标 schema 一个，不追加 public——追加 public 会
	// 让"不小心漏了 schema 前缀的查询"意外命中 public 上同名的表（如果
	// 恰好存在），把本该在隔离失效时立刻暴露的 bug 悄悄掩盖过去。
	dbWithSchema, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("以独立 schema %q 重新打开连接失败: %v", schemaName, err)
	}
	if err := dbWithSchema.Exec(fmt.Sprintf(`SET search_path TO "%s"`, schemaName)).Error; err != nil {
		t.Fatalf("设置 search_path 到 %q 失败: %v", schemaName, err)
	}

	sqlDB, err := dbWithSchema.DB()
	if err != nil {
		t.Fatalf("取底层 *sql.DB 失败: %v", err)
	}
	// 连接池必须只有一条连接：PostgreSQL 的 search_path 是连接级别的
	// 会话状态，不是随 DSN 传递的属性。池子里如果有第二条连接，GORM
	// 后续的查询可能被路由到一条从未执行过 SET search_path 的连接上，
	// 静默落回 public——这正是本包最需要防住的那种"看起来正常、实际
	// 隔离失效"的失败模式（TestNewDB_SearchPathIsIsolatedSchema 断言
	// 的就是这一点，但断言只抓当次查询，池子里潜伏的第二条连接不会
	// 每次都被抽到，必须从根上限制池大小消除这个可能性）。
	sqlDB.SetMaxOpenConns(1)

	t.Cleanup(func() {
		sqlDB.Close()
	})

	if len(models) > 0 {
		if err := dbWithSchema.AutoMigrate(models...); err != nil {
			t.Fatalf("AutoMigrate 失败: %v", err)
		}
	}

	return dbWithSchema
}

// NewSchema 建一个用完即删的空 schema，返回连接串与 schema 名，**不建任何
// 表、不返回句柄**。
//
// 与 NewDB 的分工：NewDB 面向「我要一个能直接用的 Store」——它自己开连接、
// 设 search_path、跑 AutoMigrate。NewSchema 面向「被测代码自己会开连接」
// 的场景，典型是 cmd/lober 的装配点测试：那里要走 postgres.Open 完整的
// 连库 → AutoMigrate → 索引校验路径（这正是它要验证的东西），因此不能
// 拿一个别人开好的句柄，只需要一个干净的 schema 名连同凭据。
//
// 两者共用同一套 schema 命名与 DROP SCHEMA CASCADE 清理逻辑，隔离强度
// 一致。调用方拿到 schema 名后应通过被测代码的配置项（如
// postgres.Config.SearchPath）把它传进去。
func NewSchema(t *testing.T) (dsn, schema string) {
	t.Helper()

	dsn, err := dsnFromEnv()
	if err != nil {
		t.Fatal(err)
	}

	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("连接 PostgreSQL 失败（DSN 取自环境变量 %s）: %v —— "+
			"请确认目标实例可达、DSN 格式正确", dsnEnvVar, err)
	}
	// 池上限压到 1，理由与 NewDB 里的 adminDB 完全相同（并行测试的连接
	// 总量会打满 max_connections，见那处的长注释）。
	if adminSQL, sqlErr := adminDB.DB(); sqlErr == nil {
		adminSQL.SetMaxOpenConns(1)
		adminSQL.SetMaxIdleConns(1)
	}

	schema = newSchemaName(t)
	if err := adminDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error; err != nil {
		t.Fatalf("CREATE SCHEMA %q 失败: %v —— 请确认 DSN 对应的账号"+
			"具有建 schema 的权限", schema, err)
	}

	t.Cleanup(func() {
		if err := adminDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema)).Error; err != nil {
			t.Logf("清理 schema %q 失败: %v（不影响本次测试结果，但该 "+
				"schema 可能残留，需要人工清理）", schema, err)
		}
		if sqlDB, err := adminDB.DB(); err == nil {
			sqlDB.Close()
		}
	})

	return dsn, schema
}
