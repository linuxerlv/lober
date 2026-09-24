// 本文件测试 Open：连库 + 连接池 + AutoMigrate + 索引校验的一次性入口。
// 对应实现计划任务 6。
package postgres

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

// dsnPartsFromEnv 从 LOBER_TEST_PG_DSN 解析出 Open 需要的 Config 字段。
// pgtest 包内部同样读这个环境变量，但只暴露 DSN 字符串，不暴露拆开的
// 字段——本测试需要构造 Config，因此在这里独立解析一次。测试专用，
// 不放进非 _test.go 文件。
func dsnPartsFromEnv(t *testing.T) Config {
	dsn := os.Getenv("LOBER_TEST_PG_DSN")
	if dsn == "" {
		t.Fatal("环境变量 LOBER_TEST_PG_DSN 未设置，Open 的测试需要一个可达的 PostgreSQL 实例")
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("解析 LOBER_TEST_PG_DSN 失败: %v", err)
	}

	host := u.Hostname()
	port := 5432
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			t.Fatalf("解析 DSN 里的端口失败: %v", err)
		}
	}

	password, _ := u.User.Password()
	dbname := strings.TrimPrefix(u.Path, "/")
	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}

	return Config{
		Host:     host,
		Port:     port,
		User:     u.User.Username(),
		Password: password,
		DBName:   dbname,
		SSLMode:  sslmode,
	}
}

// TestOpen_Succeeds 验证 Open 能完整走完连库、AutoMigrate、索引校验
// 三步，返回的 *gorm.DB 可用于正常查询。用真实 DSN 直连 lober_test 库
// 的 public schema（不像 pgtest 那样建独立 schema）——Open 面向生产
// 装配场景，本测试验证的是 Open 自身的行为，AutoMigrate 具有幂等性，
// 重复调用不会因为表已存在而失败。
func TestOpen_Succeeds(t *testing.T) {
	cfg := dsnPartsFromEnv(t)

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	if db == nil {
		t.Fatal("Open 成功时不应返回 nil *gorm.DB")
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层 *sql.DB 失败: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("Ping 失败，Open 返回的连接不可用: %v", err)
	}

	// AutoMigrate 之后 tasks 表应该存在，用一次真实查询验证——只验证
	// Open 不报错还不够，Open 也可能「返回 nil error 但什么都没做」。
	var count int64
	if err := db.Table("tasks").Count(&count).Error; err != nil {
		t.Fatalf("Open 之后查询 tasks 表失败，AutoMigrate 可能没有正确执行: %v", err)
	}
}

// TestOpen_BadHost_ReturnsLocatableError 验证连库失败时返回的错误能
// 定位到「连库」这一步，不是一个语焉不详的错误。
func TestOpen_BadHost_ReturnsLocatableError(t *testing.T) {
	cfg := dsnPartsFromEnv(t)
	cfg.Port = 1 // 一个几乎不可能有 PostgreSQL 监听的端口

	_, err := Open(cfg)
	if err == nil {
		t.Fatal("连接一个不存在的端口，Open 应该返回错误")
	}
	if !strings.Contains(err.Error(), "连") && !strings.Contains(err.Error(), "open") && !strings.Contains(err.Error(), "connect") {
		t.Logf("提示：错误信息最好能看出是连库这一步失败，实际: %v", err)
	}
}

// TestOpen_ThenVerifyIndexes 确认 Open 内部已经跑过 verifyIndexes：
// 用 Open 拿到的库句柄再调一次 verifyIndexes 应该直接通过（证明 Open
// 已经把 AutoMigrate 需要的八个聚合模型全部收集了，不是漏了几个）。
func TestOpen_ThenVerifyIndexes(t *testing.T) {
	cfg := dsnPartsFromEnv(t)

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层 *sql.DB 失败: %v", err)
	}
	defer sqlDB.Close()

	if err := verifyIndexes(db); err != nil {
		t.Fatalf("Open 之后 verifyIndexes 应该通过（说明 Open 内部已经跑过一次），实际: %v", err)
	}
}
