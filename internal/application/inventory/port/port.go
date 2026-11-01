// Package port 声明采集模块需要的最小外部能力。
package port

import (
	"context"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// Hosts 只声明采集模块实际使用的方法。
type Hosts interface {
	// GetHost 的八项保存约定唯一委托 repository.Hosts.GetHost
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetHost(ctx context.Context, sn string) (model.Host, error)
	// UpdateHost 的八项保存约定唯一委托 repository.Hosts.UpdateHost
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先CheckAudit，再CheckHosts及记录转换，随后锁行检查存在性、原Rev，再更新及写审计。
	UpdateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error)
}

// Tokens 生成一次性采集令牌。实现要给出 64 位小写十六进制。
type Tokens interface {
	// NewToken 只生成一个新令牌，不查重、不保存、不读时钟；失败原样返回。
	NewToken() (string, error)
}

// OpenTasks 只声明采集模块判断「这台主机有没有打开的任务」需要的那一个方法。
// 采集模块不依赖执行模块；组装点把仓储直接交进来即可，不需要函数字面量。
type OpenTasks interface {
	// FindOpenTask 的八项保存约定唯一委托 repository.Tasks.FindOpenTask
	// （internal/domain/repository/tasks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集。
	FindOpenTask(ctx context.Context, sn string) (model.Task, bool, error)
}
