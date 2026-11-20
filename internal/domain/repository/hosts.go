package repository

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
)

// Hosts 是主机的登记（C6、C7）。SN 是主机的身份，也是它的键；登记过的 SN 都是规范化之后的写法（model.NormalizeSN）。
//
// 收到合法却没规范化的 SN（如 sn-1）时，带整条记录的写入（CreateHost、UpdateHost、ImportHosts，以及
// Tasks.CreateTasks）和 Tasks.FindOpenTask 报 ErrInvalid；其余按 SN 查找的不查规范化，照原样去找，找不到就是没有：
// GetHost 与 DeleteHost 报 ErrNotFound，GetHosts 的结果里没有它，ListTasks 按它过滤得到空页。写入要查，否则 sn-1 会
// 登记成 SN-1 之外的另一台主机（C6）；FindOpenTask 要查，因为网关不能把漏了规范化当成「没有任务」，否则机器会从本地盘
// 启动、任务一直等着。其余的查找当成找不到没有这种后果，统一报 ErrInvalid 反倒会让 GetHosts 因为一个这样的 SN 整批
// 失败。调用方按 SN 调仓储之前要先经 model.NormalizeSN，漏了的话拿到的是找不到，不是入参不合法。
type Hosts interface {
	// CreateHost 登记一台主机，Rev 置 1。SN 不合法（model.NormalizeSN 报出问题）或 Host.Validate 不通过，报
	// ProblemsError(ErrInvalid)；SN 合法却没规范化（与 NormalizeSN 的结果不同），是调用方漏了规范化，model 的校验对它
	// 没有问题可报，报不带问题清单的 ErrInvalid（CheckHosts）。SN 已登记报 DuplicateError。
	CreateHost(ctx context.Context, h model.Host, a Audit) (model.Host, error)
	// UpdateHost 按 SN 整条替换架构、固件与属性，入参的规则同 CreateHost。Rev 对不上报 ErrConflict；成功后 Rev 加 1。
	UpdateHost(ctx context.Context, h model.Host, a Audit) (model.Host, error)
	// ImportHosts 是 CSV 批量导入：已登记的 SN 整条替换，不比 Rev；没登记的新建。一条不合法，整批都不写，
	// ProblemsError(ErrInvalid) 列出每一条的问题（Subject 为 host 与 SN，NormalizeSN 报的两种见 model.Subject）；
	// 批里 SN 重复报 host_duplicate_in_batch。空的一批报 ErrInvalid；批里只要有一台的 SN 合法却没规范化，不论排在
	// 第几，整批报不带问题清单的 ErrInvalid，规则同 CreateHost。每台主机写一条审计。
	ImportHosts(ctx context.Context, hs []model.Host, a Audit) ([]model.Host, error)
	// DeleteHost 删除一台主机；有任务引用报 ErrReferenced。
	DeleteHost(ctx context.Context, sn string, rev int64, a Audit) error
	GetHost(ctx context.Context, sn string) (model.Host, error)
	// GetHosts 按 SN 批量取，结果只放存在的。
	GetHosts(ctx context.Context, sns []string) (map[string]model.Host, error)
	ListHosts(ctx context.Context, q HostQuery) (Page[model.Host], error)
}
