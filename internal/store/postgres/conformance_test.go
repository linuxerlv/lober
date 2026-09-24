// 本文件是 conformance 套件在 PostgreSQL 实现上的入口，对应实现计划任务 11。
//
// 本文件不另写断言——RunConformanceSuite 就是本模块的验收标准，它与
// internal/core/fake 的入口跑的是同一套子测试。两个实现跑同一套断言，是
// 「M3-M8 用内存实现写的测试换成真实实现后行为不变」这个保证的唯一依据，
// 也是本次数据层迁移「只换实现不改行为」的安全网：套件里的断言一行都不改，
// 全绿才说明 GORM + PostgreSQL 的行为与旧 SQLite 实现一致。
package postgres

import (
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/contract/conformance"
	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// newConformanceStore 给 conformance 套件的每个子测试造一个全新、彼此隔离
// 的 Store 实例。
//
// 每个子测试用独立 schema：套件里「同一 SN 只能有一个非终态任务」「终态后
// 同一 SN 可再建任务」这类断言依赖数据库是空的，共享 schema 会让前一个子
// 测试留下的任务把后一个的前提破坏掉，且失败表现为随机顺序相关，极难定位。
//
// 走 pgtest.NewDB 而不是 Open：Open 连的是默认 schema（生产装配路径），
// 测试需要的是一次性隔离 schema。两者建出的表结构同源（都来自 allModels），
// 差别只在 schema 归属与生命周期。
func newConformanceStore(t *testing.T) contract.Store {
	t.Helper()
	return New(pgtest.NewDB(t, allModels()...))
}

// TestConformance 是本模块的验收标准：M1 的 RunConformanceSuite 在
// PostgreSQL 实现上跑一遍全部子测试。
func TestConformance(t *testing.T) {
	conformance.RunConformanceSuite(t, func() contract.Store {
		return newConformanceStore(t)
	})
}
