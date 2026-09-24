// 本文件测试 verifyIndexes：AutoMigrate 之后校验四个唯一索引没有静默
// 退化。对应实现计划任务 6，是全计划风险最高的部分——校验器自己没被
// 验证过就等于没有，因此必须真的构造一个退化的索引来验证校验器会报错
// （不能只测「正常情况下校验通过」这一条正向路径）。
package postgres

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// 本文件与 store_test.go / tx_test.go 都直接用 AllModels()：测试环境与
// Open 的生产路径必须迁移同一份表集合，否则校验器测出的「通过」不能代表
// Open 场景下也通过。红灯步骤曾在本文件独立声明过一份重复清单（当时
// open.go 还不存在），后来收敛成一个转发函数，现在 AllModels 已导出，
// 连转发也不需要了。

// TestVerifyIndexes_HappyPath 验证正常 AutoMigrate 之后四个索引都通过
// 校验——这是校验器最基本的正向路径，但只有这一条远远不够（见下面的
// 退化测试）。
func TestVerifyIndexes_HappyPath(t *testing.T) {
	db := pgtest.NewDB(t, AllModels()...)

	if err := verifyIndexes(db); err != nil {
		t.Fatalf("正常 AutoMigrate 之后 verifyIndexes 应该通过，实际返回错误: %v", err)
	}
}

// TestVerifyIndexes_DegradedWhereClause_ReturnsError 是本任务最关键的
// 一条测试：手工 DROP 掉 idx_tasks_sn_active，再建一个同名但无 WHERE 的
// 唯一索引（模拟 GORM tag 转义写错导致 WHERE 被截断或丢掉的场景），
// 断言 verifyIndexes 必须报错，且错误信息里能看到是哪个索引、期望与
// 实际的 WHERE 各是什么——这是「校验器本身被验证过」的证据。
//
// 不写这条测试等于没有校验器：索引退化不会让任何业务测试失败（症状是
// 「装完的机器再也不能重装」），只有专门针对校验器的这条负向测试才能
// 在 CI 里抓住「校验器写松了」这个问题本身。
func TestVerifyIndexes_DegradedWhereClause_ReturnsError(t *testing.T) {
	db := pgtest.NewDB(t, AllModels()...)

	if err := db.Exec(`DROP INDEX idx_tasks_sn_active`).Error; err != nil {
		t.Fatalf("DROP INDEX 失败: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_tasks_sn_active ON tasks (host_sn)`).Error; err != nil {
		t.Fatalf("重建退化索引失败: %v", err)
	}

	err := verifyIndexes(db)
	if err == nil {
		t.Fatal("退化成无 WHERE 的索引后 verifyIndexes 应该报错，实际返回 nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "idx_tasks_sn_active") {
		t.Errorf("错误信息应指出是哪个索引出问题，实际: %q", msg)
	}
	// 错误信息应同时给出期望与实际的 WHERE，运维靠这个判断退化的具体
	// 形态（截断了一半，还是整条丢了）。实际此处是「完全没有 WHERE」，
	// 错误信息里应该能看出「实际 WHERE 为空」这一事实。
	if !strings.Contains(msg, "WHERE") && !strings.Contains(msg, "where") {
		t.Errorf("错误信息应提及 WHERE 条件的期望值，实际: %q", msg)
	}
}

// TestVerifyIndexes_MissingIndex_ReturnsError 覆盖索引整个不存在的
// 情形（比 WHERE 退化更彻底的失效）。
func TestVerifyIndexes_MissingIndex_ReturnsError(t *testing.T) {
	db := pgtest.NewDB(t, AllModels()...)

	if err := db.Exec(`DROP INDEX idx_tasks_token`).Error; err != nil {
		t.Fatalf("DROP INDEX 失败: %v", err)
	}

	err := verifyIndexes(db)
	if err == nil {
		t.Fatal("索引缺失时 verifyIndexes 应该报错，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "idx_tasks_token") {
		t.Errorf("错误信息应指出缺失的索引名，实际: %q", err.Error())
	}
}

// TestVerifyIndexes_NotUnique_ReturnsError 覆盖索引存在、WHERE 也对，
// 但不是 unique 的退化情形——同样会让「同一主机两个非终态任务」这类
// 业务约束失效，但退化方式与 WHERE 被截断不同，需要单独覆盖。
func TestVerifyIndexes_NotUnique_ReturnsError(t *testing.T) {
	db := pgtest.NewDB(t, AllModels()...)

	if err := db.Exec(`DROP INDEX idx_tasks_finalize_token`).Error; err != nil {
		t.Fatalf("DROP INDEX 失败: %v", err)
	}
	if err := db.Exec(`CREATE INDEX idx_tasks_finalize_token ON tasks (finalize_token) WHERE finalize_token != ''`).Error; err != nil {
		t.Fatalf("重建非唯一索引失败: %v", err)
	}

	err := verifyIndexes(db)
	if err == nil {
		t.Fatal("索引退化成非唯一后 verifyIndexes 应该报错，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "idx_tasks_finalize_token") {
		t.Errorf("错误信息应指出是哪个索引，实际: %q", err.Error())
	}
}
