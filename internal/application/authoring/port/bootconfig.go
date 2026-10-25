package port

// 本文件的仓储口只供可信应用或启动组装调用；仓储不认证网页用户或机器令牌。
// 身份、权限或凭据检查的有无及先后，由各消费入口的操作约定负责。
// 输入规范化由调用应用负责；排序、空值、错误优先级、重复/版本冲突、锁内复查、原子审计及取消均沿用所指保存契约。
// 只读方法不写业务或审计、不发通知，故无原子写与提交复查；重复读取不保证多次或多页快照。

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
)

type BootConfig interface {
	// ReadESXiBootConfig 的八项约定唯一连接 docs/contracts/authoring-preview.md「内部只读与目录能力」。
	// 只读冻结镜像的不可变原配置，不选择最新镜像；错误及上下文沿用文件读取。
	ReadESXiBootConfig(context.Context, model.SnapshotISO) (string, error)
}
type HostReader interface {
	// GetHost 的八项保存约定唯一委托 repository.Hosts.GetHost
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetHost(ctx context.Context, sn string) (model.Host, error)
}
type ISOReader interface {
	// GetISO 的八项保存约定唯一委托 repository.ISOs.GetISO
	// （internal/domain/repository/isos.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetISO(ctx context.Context, id string) (model.ISO, error)
}
