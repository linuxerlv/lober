// 本文件实现组合器 Store：用结构体嵌入把八个聚合包拼成 contract.Store。
// 对应实现计划任务 10，依据设计文档 §4。
//
// 为什么用结构体嵌入而不是手写八个接口的转发方法。嵌入让八个聚合包各自
// 的方法集自动提升到 *Store 上——contract.Store 组合的八个窄接口因此
// 免费得到满足，不需要为每个方法写一遍 `func (s *Store) Xxx(...) { return
// s.task.Xxx(...) }` 这种纯转发代码。八个窄接口一共约 40 个方法，手写
// 转发是纯粹的样板代码，且每次契约加方法都要在这里同步加一行——嵌入把
// 这个同步负担转移给编译器：contract 加方法时，先在 store.go 末尾的
// 接口断言处报错，位置固定，比运行时才发现方法集不匹配更早暴露问题。
//
// 与实现计划字面示例的一处必要偏离：八个聚合包各自把自己的类型命名为
// `Store`（task.Store、event.Store、……），这是本次迁移刻意选的统一
// 命名（每个包对外只暴露一个类型，叫 Store 最直白）。但 Go 的匿名嵌入
// 按「类型名」登记隐式字段名，八个都叫 Store 会导致 `*task.Store` 与
// `*event.Store` 在同一个结构体里都想登记字段名 `Store`，编译期报
// "Store redeclared"——这不是方法名冲突（计划风险提示预期的那种，
// "真撞了就报告，别自己加 wrapper 绕过"），是类型名冲突，在方法集
// 比较之前就已经无法通过编译，八个接口甚至还没来得及检查。
//
// 解法是本地类型别名（`type xxxStore = xxx.Store`）：别名与原类型是
// 同一个类型，不是新类型，不改变方法集也不新增任何转发代码——`aStore`
// 与 `a.Store` 是同一个东西，只是嵌入时登记的字段名从 `Store`变成了
// `xxxStore`，从而八个字段名互不相同，可以同时匿名嵌入。方法提升不受
// 影响：`(*Store).GetTask` 依然是 `task.Store.GetTask` 提升出来的
// 同一个方法，不是新写的 wrapper。已用 go run 的最小复现验证过提升
// 确实生效（复现用完即删，不保留在交付物里）。
package postgres

import (
	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/store/postgres/artifact"
	"github.com/lober/lober/internal/store/postgres/audit"
	"github.com/lober/lober/internal/store/postgres/event"
	"github.com/lober/lober/internal/store/postgres/fact"
	"github.com/lober/lober/internal/store/postgres/hook"
	"github.com/lober/lober/internal/store/postgres/host"
	"github.com/lober/lober/internal/store/postgres/task"
	"github.com/lober/lober/internal/store/postgres/template"
)

// 八个本地类型别名，只用于让匿名嵌入时的隐式字段名互不相同（见文件头
// 注释）。每个别名与右侧类型是同一个类型，Store 的方法集因此原样包含
// 八个聚合包各自导出的全部方法，不多不少。
type (
	taskStore     = task.Store
	eventStore    = event.Store
	hostStore     = host.Store
	hookStore     = hook.Store
	templateStore = template.Store
	artifactStore = artifact.Store
	factStore     = fact.Store
	auditStore    = audit.Store
)

// Store 组合八个聚合包，满足 contract.Store（八个窄接口）与
// contract.TxRunner。
//
// db 与 inTx 都不导出，调用方不应该、也不需要绕过 RunInTx 直接拿到底层
// 连接或探测「当前是否在事务里」。
//
// inTx 是 RunInTx 拒绝嵌套调用的判据（见 tx.go）。这与旧 SQLite 实现
// 用 rawDB == nil 判断的思路不同：GORM 的事务句柄仍是 *gorm.DB，顶层
// 连接与事务句柄是同一个类型，无法像 *sql.DB / *sql.Tx 那样靠类型或
// nil 值区分，因此需要一个显式的布尔标记位。
type Store struct {
	*taskStore
	*eventStore
	*hostStore
	*hookStore
	*templateStore
	*artifactStore
	*factStore
	*auditStore

	db   *gorm.DB
	inTx bool
}

// newWithTxFlag 是 New 与 tx.go 里事务内重新组合共用的构造逻辑，
// inTx 由调用方决定——顶层构造传 false，RunInTx 在事务句柄上重新
// 组合时传 true。八个聚合包各自的 New 都只需要一个 *gorm.DB，因此
// 事务句柄（同样是 *gorm.DB）可以直接传给同一份构造逻辑，不需要为
// 事务场景单写一套。
func newWithTxFlag(db *gorm.DB, inTx bool) *Store {
	return &Store{
		taskStore:     task.New(db),
		eventStore:    event.New(db),
		hostStore:     host.New(db),
		hookStore:     hook.New(db),
		templateStore: template.New(db),
		artifactStore: artifact.New(db),
		factStore:     fact.New(db),
		auditStore:    audit.New(db),
		db:            db,
		inTx:          inTx,
	}
}

// New 用给定的 *gorm.DB 构造顶层 Store（inTx = false）。
//
// 必须把八个嵌入指针全部构造出来——任一为 nil 时，调用落在该嵌入类型
// 方法集里的方法会在运行期 panic（nil pointer dereference），而不是
// 编译期报错，因为 Go 只检查方法集是否满足接口，不检查嵌入指针是否为
// nil。newWithTxFlag 是保证这件事的唯一位置，RunInTx 复用它而不是
// 各自 new 一遍八个聚合。
func New(db *gorm.DB) *Store {
	return newWithTxFlag(db, false)
}

// 编译期断言：*Store 满足 contract.Store 与 contract.TxRunner。放在
// 文件末尾——contract 层新增方法时，这里最先报编译错误，位置固定，
// 比运行时才发现方法集不匹配更容易定位。
var (
	_ contract.Store    = (*Store)(nil)
	_ contract.TxRunner = (*Store)(nil)
)
