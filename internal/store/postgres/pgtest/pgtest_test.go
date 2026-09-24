// 本文件测试 pgtest 包自身：NewDB 建的 schema 是否真的生效（search_path
// 落在新建的 schema 而不是静默落在 public），以及环境变量缺失时是否
// Fatal 而不是 Skip（计划任务 1 的硬性要求，见任务描述）。
package pgtest

import (
	"strings"
	"testing"
)

// TestNewDB_SearchPathIsIsolatedSchema 验证 NewDB 返回的句柄上
// current_schema() 等于刚建的那个 schema 名——这是隔离是否真的生效的
// 唯一直接证据。如果 search_path 设置失败或被忽略，句柄会静默落在
// public 上，症状是所有并行测试共享同一张表、随机互相冲突。
func TestNewDB_SearchPathIsIsolatedSchema(t *testing.T) {
	db := NewDB(t)

	var current string
	if err := db.Raw("SELECT current_schema()").Scan(&current).Error; err != nil {
		t.Fatalf("查询 current_schema() 失败: %v", err)
	}

	if current == "public" {
		t.Fatalf("current_schema() = %q，落在 public 上——search_path 未生效，"+
			"所有并行测试会共享同一张表，隔离失效", current)
	}

	if current == "" {
		t.Fatalf("current_schema() 返回空字符串，NewDB 未能建立可用的 schema")
	}
}

// probeModel 是本包自测专用的极简 model，用于验证 AutoMigrate 真的把
// 表建到了独立 schema 里，不是验证 model 映射正确性（那是各聚合包
// 自己的职责）。
type probeModel struct {
	ID uint `gorm:"primaryKey"`
}

// TestNewDB_AutoMigratesGivenModels 验证传入的 model 真的被 AutoMigrate
// 建出表——若 NewDB 忘记调用 AutoMigrate 或调用时机不对（比如在
// 设置 search_path 之前迁移，迁移会静默落在 public），本测试能抓住。
func TestNewDB_AutoMigratesGivenModels(t *testing.T) {
	db := NewDB(t, &probeModel{})

	if !db.Migrator().HasTable(&probeModel{}) {
		t.Fatalf("AutoMigrate 后 HasTable(probeModel) = false，表未在当前 " +
			"schema（即 NewDB 建的独立 schema）中建出")
	}
}

// TestDSNFromEnv_MissingReturnsError 验证「环境变量为空时" NewDB 会
// Fatal」这条契约里，判断逻辑本身是可测的——把 t.Fatal 之前的判断
// 拆成一个返回 error 的函数（dsnFromEnv），使得"未设时应该出错"这个
// 分支能在不真正调用 t.Fatal（会终止测试进程）的情况下被断言到。
//
// 这不是对 NewDB 的端到端测试（那需要真的观察一次 t.Fatal，Go 的
// testing 包不提供安全的手段在子测试外捕获它），而是对 NewDB 内部
// 依赖的纯函数的直接测试——只要 dsnFromEnv 在环境变量缺失时返回
// error，NewDB 对该 error 调 t.Fatal 的那一行本身足够简单，不需要
// 再单独验证。
func TestDSNFromEnv_MissingReturnsError(t *testing.T) {
	t.Setenv(dsnEnvVar, "")

	if _, err := dsnFromEnv(); err == nil {
		t.Fatalf("dsnFromEnv() 在环境变量 %s 为空时未返回 error——"+
			"NewDB 依赖这个 error 来判断是否应该 Fatal；如果这里静默"+
			"放行，未设 DSN 时测试会在后续某个不相关的地方以更难懂的"+
			"方式失败，而不是在最早的地方给出明确指引", dsnEnvVar)
	}
}

// TestDSNFromEnv_MissingErrorMentionsWhatIsNeeded 验证错误信息写明了
// 需要什么——环境变量名本身。可达的 PG 与建/删 schema 权限这两项无法
// 在缺少 DSN 的分支里验证（那是连接失败分支的责任，见
// TestDSNFromEnv_UnreachablePGFails 之类的连接期错误，本函数只覆盖
// "根本没设"这一种情况），但环境变量名是这个分支唯一能给出的、最
// 直接可核实的线索。
func TestDSNFromEnv_MissingErrorMentionsWhatIsNeeded(t *testing.T) {
	t.Setenv(dsnEnvVar, "")

	_, err := dsnFromEnv()
	if err == nil {
		t.Fatalf("dsnFromEnv() 未返回 error，无法核实错误信息内容")
	}

	if !strings.Contains(err.Error(), dsnEnvVar) {
		t.Fatalf("错误信息 %q 未提及环境变量名 %s——失败信息必须写明"+
			"需要设置哪个环境变量，否则调用方（跑测试的人）不知道该改什么",
			err.Error(), dsnEnvVar)
	}
}
