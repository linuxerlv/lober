package repository

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
)

// Hooks 是 hook 的身份与版本（C8–C11、C22、C23、C25、C26）。
//
// 名字放在身份上：每个 hook 一行身份，名字唯一，各版本共用，返回的每个版本 Name 都是身份现在的名字。版本的状态
// draft → review → approved → deprecated，review 可以退回 draft，不合这几步的转换报 ErrConflict。每个身份
// 同时最多一个未审批的版本（草稿或待审批）；已审批与已废弃的版本永不删除，只能删草稿。
type Hooks interface {
	// CreateHook 建一个新身份，加上版本 1 的草稿；入参不带 ID 与版本。名字按 model.NormalizeName 规范化，
	// 不合法或用了保留前缀（name_reserved，C12）报 ProblemsError(ErrInvalid)；key 被别的身份占了报
	// DuplicateError，占着它的身份恰好在这期间改了名或被删掉、报不出是谁时，报 ErrConflict，重读之后再写。说明与
	// 每份实现的正文都要是可存文本，否则报 ProblemsError(ErrInvalid)，问题是 text_not_storable。
	CreateHook(ctx context.Context, h model.Hook) (model.Hook, error)
	// SaveHookDraft 保存草稿。只能改草稿，否则报 ErrImmutable；Name 必须等于身份现在的名字，否则报
	// ErrInvalid（改名用 RenameHook）；Rev 对不上报 ErrConflict。
	SaveHookDraft(ctx context.Context, h model.Hook) (model.Hook, error)
	// NewHookVersion 复制版本 from 的内容，建下一个版本号的草稿，作者为 author。新版本号是这个身份分配过的最大版本号
	// 加 1，不是现有版本里最大的加 1：删掉的草稿用过的号不再发，那个草稿的键可能还记在调用方手里的旧副本、派生模板的
	// DerivedFrom 与审计里，号再发出去，它们就会对到另一份内容上。被拒的开新版本不占号。已有草稿或待审批的版本报
	// ErrConflict；内置的报 ErrImmutable。
	NewHookVersion(ctx context.Context, id string, from int, author string) (model.Hook, error)
	// SubmitHook：draft → review。
	SubmitHook(ctx context.Context, k model.HookKey, rev int64, a Audit) error
	// ApproveHook：review → approved，Approver 置为 approver。事务里对最新数据重跑激活判定与审批人的检查
	// （ApproveHookProblems），有问题报 ProblemsError(ErrRejected)，状态不变。
	ApproveHook(ctx context.Context, k model.HookKey, rev int64, approver string, a Audit) error
	// RejectHook：review → draft，理由写在审计的 Detail 里。
	RejectHook(ctx context.Context, k model.HookKey, rev int64, a Audit) error
	// DeprecateHook：approved → deprecated。
	DeprecateHook(ctx context.Context, k model.HookKey, rev int64, a Audit) error
	// RenameHook 给身份改名，规则同 CreateHook；内置的报 ErrImmutable。
	RenameHook(ctx context.Context, id, name string, a Audit) error
	// DeleteHookDraft 只能删草稿；被任何模板版本引用（包括草稿）报 ErrReferenced。删的是这个身份的最后一个
	// 版本时，身份一起删掉，名字随之空出来。
	DeleteHookDraft(ctx context.Context, k model.HookKey, rev int64) error
	GetHook(ctx context.Context, k model.HookKey) (model.Hook, error)
	// GetHooks 按版本批量取，结果只放存在的。
	GetHooks(ctx context.Context, ks []model.HookKey) (map[model.HookKey]model.Hook, error)
	// ListHookVersions 返回一个身份的全部版本，按版本号升序；身份不存在报 ErrNotFound。
	ListHookVersions(ctx context.Context, id string) ([]model.Hook, error)
	// ListHooks 每个身份一项，见 HookQuery.ApprovedOnly。
	ListHooks(ctx context.Context, q HookQuery) (Page[model.Hook], error)
	// EnsureBuiltinHook 装载一个内置的 hook 版本（Builtin 为 true，State 为 approved，spec §6.2 第 11 条），
	// 不审计：同一个 ID 与版本不存在就写入；已存在并且内容相同，什么都不做；内容不同报 ErrBuiltinMismatch；
	// 名字被别的身份占了，报 DuplicateError。名字必须带保留前缀 model.ReservedNamePrefix（C12），不带报 ErrInvalid。
	// 名字随身份定下：身份已在库里时，以后的版本不能换名，换了名同样报 ErrBuiltinMismatch；要改名只能换一个新 ID。
	// 两次同时装载同一个版本时，后到的一次等先到的提交，内容相同同样什么都不做。占着名字的身份或要装载的身份恰好在
	// 这期间变了时报 ErrConflict（规则同 CreateHook），由装配点重试；装配点把它当作装载失败，程序就会拒绝启动。
	EnsureBuiltinHook(ctx context.Context, h model.Hook) error
}
