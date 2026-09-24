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
}

// dsn 把 Config 拼接成 gorm.io/driver/postgres 接受的连接串。
func (c Config) dsn() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
	)
}

// allModels 收集八个聚合包的 Models()，供 AutoMigrate 使用。schemacheck_test.go
// 里的 allModelsForTest 是这份清单在测试侧的独立副本（测试要在本文件
// 写出之前先跑起来，见该文件的注释），两处字面上重复一次是 TDD 红灯
// 步骤本身要求的顺序代价，不是可以消除的重复。
func allModels() []any {
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

	if err := db.AutoMigrate(allModels()...); err != nil {
		return nil, fmt.Errorf("postgres.Open: AutoMigrate 失败: %w", err)
	}

	if err := verifyIndexes(db); err != nil {
		return nil, fmt.Errorf("postgres.Open: 索引校验失败: %w", err)
	}

	return db, nil
}
