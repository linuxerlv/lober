package repository

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
)

// ISOs 是 ISO 的记录与状态（C2–C5、C26）。获取来源（下载断点、上传分片、本地路径）不在本层，由第 4 层加。
type ISOs interface {
	// CreateISO 建一条获取中的 ISO：入参只填 Name、Family、Series。系必须属于族，Name 是可存文本，否则报
	// ErrInvalid。仓储生成 ID，State 为 acquiring，Rev 为 1。
	CreateISO(ctx context.Context, iso model.ISO, a Audit) (model.ISO, error)
	// UpdateISO 是第 4 层导入用的系统更新，不审计：可以改 Version、Arch、State、SHA256、Artifacts、Offers；
	// 改 Family、Series、Name 报 ErrInvalid。已废弃的 ISO 不再接受系统更新，报 ErrImmutable：废弃是要审计的决定，
	// 不审计的更新不能把它撤掉。目标状态不认识，或者是 deprecated（废弃走 DeprecateISO），报 ErrInvalid；写进来的文本
	// （版本、架构、制品、提供的集合与选项）存不下，同样报 ErrInvalid（CheckISOText）。要置为 ready 时必须通过
	// ISO.Validate，否则报 ProblemsError(ErrInvalid)。SHA256 只能为空串，或是 64 位小写十六进制（SHA-256 摘要
	// 的规范写法，大写的不收），不论置为什么状态都查，否则报 ErrInvalid：C4 按它去重，同一份内容写成大写就成了另一份。
	// SHA256 撞上另一个不是导入失败的 ISO，报 DuplicateError，Existing 是对方的 ID（C4）；对方恰好在这期间导入失败
	// 或被删掉、报不出是谁时，报 ErrConflict，重读之后再更新。
	UpdateISO(ctx context.Context, iso model.ISO) (model.ISO, error)
	// DeprecateISO 只能把 ready 改成 deprecated，其他状态报 ErrConflict。
	DeprecateISO(ctx context.Context, id string, rev int64, a Audit) error
	// DeleteISO 删除一个 ISO；有任务引用报 ErrReferenced。
	DeleteISO(ctx context.Context, id string, rev int64, a Audit) error
	GetISO(ctx context.Context, id string) (model.ISO, error)
	ListISOs(ctx context.Context, q ISOQuery) (Page[model.ISO], error)
}
