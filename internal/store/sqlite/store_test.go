// 本文件是 conformance 套件的入口。对应实现计划任务 1、11，依据 SPEC
// §2.5、§2.6、§2.7、§3.3、§3.4、§3.6、§4.3、§4.4、§4.7、§5.5、§12.8。
//
// 本文件不另写断言——RunConformanceSuite 就是本模块的验收标准
// （计划文档「验收标准已经存在」）。
package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/contract/conformance"
)

// newConformanceStore 给 conformance 套件的每个子测试造一个全新、彼此隔离
// 的 Store 实例。
//
// 用测试临时目录下的文件库而不是内存库：内存库在连接池（本包 Open 设为
// 上限 1）下的行为与文件库不同，测出来的不算数（计划任务 1 设计要点）。
// 每次调用用 t.TempDir() 的一个新子路径，保证套件的子测试之间不共享任何
// 状态——「同一 SN 只能有一个非终态任务」这类断言依赖数据库是空的。
//
// 开库走本包的 Open 函数而不是自己 sql.Open：WAL、外键、忙等超时、连接池
// 上限 1 这几个 pragma 是行为的一部分，绕过它们等于测了另一个东西
// （计划任务 1 设计要点）。
func newConformanceStore(t *testing.T) contract.Store {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "conformance.db")
	db, err := Open("file:" + dbPath)
	if err != nil {
		t.Fatalf("Open(%q) 失败: %v —— conformance 套件依赖一个可用的空数据库，"+
			"打开失败说明迁移或 pragma 设置本身有问题，不是被测的 Store 方法的问题", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("关闭测试数据库失败: %v", err)
		}
	})

	return New(db)
}

// TestConformance 是本模块的验收标准：M1 的 RunConformanceSuite 在 SQLite
// 实现上跑一遍全部子测试。
func TestConformance(t *testing.T) {
	conformance.RunConformanceSuite(t, func() contract.Store {
		return newConformanceStore(t)
	})
}
