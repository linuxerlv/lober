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

// ISOs 只提供镜像模块使用的六项记录能力，写入和审计仍由仓储共同提交。
type ISOs interface {
	// CreateISO 的八项保存约定唯一委托 repository.ISOs.CreateISO
	// （internal/domain/repository/isos.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	CreateISO(context.Context, model.ISO, repository.Audit) (model.ISO, error)
	// UpdateISO 的八项保存约定唯一委托 repository.ISOs.UpdateISO
	// （internal/domain/repository/isos.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	UpdateISO(context.Context, model.ISO) (model.ISO, error)
	// DeprecateISO 的八项保存约定唯一委托 repository.ISOs.DeprecateISO
	// （internal/domain/repository/isos.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	DeprecateISO(context.Context, string, int64, repository.Audit) error
	// DeleteISO 的八项保存约定唯一委托 repository.ISOs.DeleteISO
	// （internal/domain/repository/isos.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	DeleteISO(context.Context, string, int64, repository.Audit) error
	// GetISO 的八项保存约定唯一委托 repository.ISOs.GetISO
	// （internal/domain/repository/isos.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetISO(context.Context, string) (model.ISO, error)
	// ListISOs 的八项保存约定唯一委托 repository.ISOs.ListISOs
	// （internal/domain/repository/isos.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListISOs(context.Context, repository.ISOQuery) (repository.Page[model.ISO], error)
}

// Sources 不包含当前流程未使用的单条删除能力。
type Sources interface {
	// SaveSource 的八项保存约定唯一委托 repository.ISOSources.SaveSource
	// （internal/domain/repository/sources.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 输入检查唯一沿用repository.CheckSource，随后检查可空审计；父镜像先于来源锁。
	// 父缺失ErrNotFound、废弃ErrImmutable；新建要求Rev=0/pending/零进度/空错误，返回Rev=1。
	// 更新先比Rev，再比不可变来源字段、状态转换与进度限制；旧版ErrConflict，不自动重试。
	// 终态回pending时区间、Fetched和Error同事务清零，其他保存与可选审计同事务；边界详见media.md第7节。
	SaveSource(context.Context, model.Source, repository.Audit) (model.Source, error)
	// SaveSourceAndAcquire 的八项保存约定唯一委托 repository.ISOSources.SaveSourceAndAcquire
	// （internal/domain/repository/sources.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先检查正isoRev和必填审计，再沿用SaveSource检查；父锁内查失败镜像原Rev及已完成主来源。
	// 补额外源及重新导入主源的齐备条件沿用media.md第3、6、7节；来源、镜像acquiring及审计同事务。
	// 成功返回保存来源，旧版本/状态竞争冲突，不自动重试。
	SaveSourceAndAcquire(context.Context, model.Source, int64, repository.Audit) (model.Source, error)
	// GetSource 的八项保存约定唯一委托 repository.ISOSources.GetSource
	// （internal/domain/repository/sources.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先CheckSourceKey；缺失返回零值、false、nil，不因父镜像缺失单独报404；读取错误原样返回。
	GetSource(context.Context, string, string) (model.Source, bool, error)
	// SourcesOf 的八项保存约定唯一委托 repository.ISOSources.SourcesOf
	// （internal/domain/repository/sources.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先CheckID，按Role字节序返回非nil数组；父不存在也返回空数组，不额外查镜像。
	SourcesOf(context.Context, string) ([]model.Source, error)
	// ListUnfinishedSources 的八项保存约定唯一委托 repository.ISOSources.ListUnfinishedSources
	// （internal/domain/repository/sources.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 无筛选输入；只列pending/fetching，按ISOID再Role字节序，空为非nil数组，不查父状态。
	ListUnfinishedSources(context.Context) ([]model.Source, error)
	// SaveRange 的八项保存约定唯一委托 repository.ISOSources.SaveRange
	// （internal/domain/repository/sources.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先CheckSourceKey再CheckRange；行锁内先存在性、fetching状态，再检查已知总长边界。
	// 区间、合并Fetched与Rev同事务；重传同起点保留较长范围，重复也按保存流程递增Rev，不审计或通知。
	SaveRange(context.Context, string, string, int64, int64) error
	// RangesOf 的八项保存约定唯一委托 repository.ISOSources.RangesOf
	// （internal/domain/repository/sources.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	// 先CheckSourceKey；按起点升序并合并重叠及相邻区间，空为非nil数组，不额外查来源存在性。
	RangesOf(context.Context, string, string) ([]model.Range, error)
}
