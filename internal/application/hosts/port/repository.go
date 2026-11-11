package port

// 本文件的仓储口只供可信应用或启动组装调用；仓储不认证网页用户或机器令牌。
// 身份、权限或凭据检查的有无及先后，由各消费入口的操作约定负责。
// 输入规范化由调用应用负责；排序、空值、错误优先级、重复/版本冲突、锁内复查、原子审计及取消均沿用所指保存契约。
// 只读方法不写业务或审计、不发通知，故无原子写与提交复查；重复读取不保证多次或多页快照。

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// Hosts 只声明主机应用本阶段实际使用的方法。
type Hosts interface {
	// GetHosts 的八项保存约定唯一委托 repository.Hosts.GetHosts
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetHosts(ctx context.Context, sns []string) (map[string]model.Host, error)
	// ImportHosts 的八项保存约定唯一委托 repository.Hosts.ImportHosts
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先CheckAudit，再CheckHosts（含空批、规范SN和批内重复），再转换记录；审计错误先于主机输入错误。
	// 按hostLockOrder写入后读回Rev，审计与结果保持输入顺序；不比较传入Rev，写入、审计或事务失败整批回滚。
	ImportHosts(ctx context.Context, hs []model.Host, a repository.Audit) ([]model.Host, error)
	// CreateHost 的八项保存约定唯一委托 repository.Hosts.CreateHost
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先CheckAudit，再CheckHosts，再转换记录并插入；审计错误先于主机输入错误。
	// 重复SN在插入时由唯一约束拒绝，成功才同事务写审计。
	CreateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error)
	// UpdateHost 的八项保存约定唯一委托 repository.Hosts.UpdateHost
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先CheckAudit，再CheckHosts及记录转换，随后锁行检查存在性、原Rev，再更新及写审计。
	// 审计错误先于主机输入错误；缺失先于旧版本冲突，不自动换用新Rev。
	UpdateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error)
	// DeleteHost 的八项保存约定唯一委托 repository.Hosts.DeleteHost
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先CheckAudit，再处理不可存SN（ErrNotFound），随后锁行查存在性、原Rev，再查任务引用并删除/写审计。
	// 不在保存口新增SN规范化或正Rev预检查；缺失先于版本，版本先于引用，失败不留删除或审计。
	DeleteHost(ctx context.Context, sn string, rev int64, a repository.Audit) error
	// GetHost 的八项保存约定唯一委托 repository.Hosts.GetHost
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetHost(ctx context.Context, sn string) (model.Host, error)
	// ListHosts 的八项保存约定唯一委托 repository.Hosts.ListHosts
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListHosts(ctx context.Context, q repository.HostQuery) (repository.Page[model.Host], error)
}
