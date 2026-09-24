// 本文件实现 Open：一次性完成连库、连接池设置、AutoMigrate、索引校验
// 四步，对应实现计划任务 6，依据设计文档 §6、§10。
//
// Config 在本包定义，不动 internal/config——那个包有自己的校验风格与
// 测试，配置的接线（把 lober.yaml 的 postgres 段转成本类型）留给后续
// 任务统一改（任务 6 的硬性约束）。
package postgres

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/lober/lober/internal/store/postgres/artifact"
	"github.com/lober/lober/internal/store/postgres/audit"
	"github.com/lober/lober/internal/store/postgres/event"
	"github.com/lober/lober/internal/store/postgres/fact"
	"github.com/lober/lober/internal/store/postgres/hook"
	"github.com/lober/lober/internal/store/postgres/host"
	"github.com/lober/lober/internal/store/postgres/task"
	"github.com/lober/lober/internal/store/postgres/template"
)

// Config 是 Open 需要的连接参数。字段与 dsn 拼接逐一对应，不用一个
// 现成的连接串参数——结构化字段能在拼 DSN 之前就发现「host 忘填」这类
// 错误，且与 config.go 现有的结构化配置风格一致（设计文档 §10 的
// yaml 示例）。
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string

	// SearchPath 指定建表与查询落在哪个 schema，空值表示用连接的默认
	// search_path（通常是 public）。
	//
	// 有两类用处。一是共享数据库部署：同一个 PG 实例上跑多套 lober 时，
	// 各自一个 schema 比各自一个 database 更容易做备份与权限。二是测试：
	// 装配点的用例要在一个用完即删的独立 schema 上跑完整的 AutoMigrate，
	// 不能把表留在 public 里——那会让测试之间互相看见对方的数据，且失败
	// 表现为随机的顺序相关问题。
	//
	// 注意 Open 不会自动建这个 schema：建 schema 是一次显式的、需要相应
	// 权限的操作，静默创建会掩盖「schema 名拼错」这类配置错误（症状会变成
	// 「库突然是空的」而不是一句明确的报错）。
	SearchPath string
}

// dsn 把 Config 拼接成 gorm.io/driver/postgres 接受的连接串。
func (c Config) dsn() string {
	s := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
	)
	if c.SearchPath != "" {
		s += " search_path=" + c.SearchPath
	}
	return s
}

// AllModels 收集八个聚合包的 Models()，供 Open 的 AutoMigrate 使用。
//
// 导出是因为包外的测试也需要建出同一套表：cmd/lober 的装配点测试要在一个
// 独立 schema 上装配一个完整 App，而它不能走 Open（Open 连的是配置里那个
// 生产库的默认 schema）。除此之外没有生产代码需要它——装配点只调 Open。
func AllModels() []any {
	var models []any
	models = append(models, task.Models()...)
	models = append(models, event.Models()...)
	models = append(models, host.Models()...)
	models = append(models, hook.Models()...)
	models = append(models, template.Models()...)
	models = append(models, artifact.Models()...)
	models = append(models, fact.Models()...)
	models = append(models, audit.Models()...)
	return models
}

// Open 按顺序完成四步：连库 → 设连接池 → AutoMigrate（收集八个聚合包
// 的 Models()）→ verifyIndexes。任何一步失败都返回能定位到具体步骤的
// 错误（用 fmt.Errorf 的 %w 包装一句步骤说明，而不是让底层错误原样
// 冒泡——底层错误消息不会自己说「这是在连库阶段发生的」）。
//
// 连接池参数没有做成 Config 的字段，取的是一个保守的固定值：MaxOpenConns
// 25、MaxIdleConns 5——设计文档没有对连接池大小给出具体数字（§12「真
// 并发首次出现」只说明池子放开后的后果，没有定具体上限），生产环境
// 需要调整时应该在后续任务里把这两个数字提升为可配置项，本任务不
// 引入一个尚无实际取值来源的配置面。
func Open(cfg Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.dsn()), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("postgres.Open: 连接数据库失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("postgres.Open: 取底层 *sql.DB 失败: %w", err)
	}
	const (
		maxOpenConns = 25
		maxIdleConns = 5
	)
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)

	if err := db.AutoMigrate(AllModels()...); err != nil {
		return nil, fmt.Errorf("postgres.Open: AutoMigrate 失败: %w", err)
	}

	if err := verifyIndexes(db); err != nil {
		return nil, fmt.Errorf("postgres.Open: 索引校验失败: %w", err)
	}

	return db, nil
}
