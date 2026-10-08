package store

import (
	"context"

	"github.com/lober/lober/internal/core/model"
)

// Store 是仓储的全部能力，由装配点构造。调用方只声明自己用得到的那几个接口（spec §6.1）。
type Store interface {
	Hosts
	ISOs
	Hooks
	Templates
	Tasks
	Events
	AuditLog
	// Close 释放连接这类资源，之后不能再调用别的方法。
	Close() error
}

// Hosts 是主机的登记（C6、C7）。SN 是主机的身份，也是它的键。
type Hosts interface {
	// CreateHost 登记一台主机，Rev 置 1。SN 必须已经规范化（与 model.NormalizeSN 的结果相同），并且
	// Host.Validate 通过，否则报 ProblemsError(ErrInvalid)；SN 已登记报 DuplicateError。
	CreateHost(ctx context.Context, h model.Host, a Audit) (model.Host, error)
	// UpdateHost 按 SN 整条替换架构、固件与属性。Rev 对不上报 ErrConflict；成功后 Rev 加 1。
	UpdateHost(ctx context.Context, h model.Host, a Audit) (model.Host, error)
	// ImportHosts 是 CSV 批量导入：已登记的 SN 整条替换，不比 Rev；没登记的新建。一条不合法，整批都不写，
	// ProblemsError(ErrInvalid) 列出每一条的问题（Subject 为 host 与 SN）；批里 SN 重复报
	// host_duplicate_in_batch。每台主机写一条审计。
	ImportHosts(ctx context.Context, hs []model.Host, a Audit) ([]model.Host, error)
	// DeleteHost 删除一台主机；有任务引用报 ErrReferenced。
	DeleteHost(ctx context.Context, sn string, rev int64, a Audit) error
	GetHost(ctx context.Context, sn string) (model.Host, error)
	// GetHosts 按 SN 批量取，结果只放存在的。
	GetHosts(ctx context.Context, sns []string) (map[string]model.Host, error)
	ListHosts(ctx context.Context, q HostQuery) (Page[model.Host], error)
}

// ISOs 是 ISO 的记录与状态（C2–C5、C26）。获取来源（下载断点、上传分片、本地路径）不在本层，由第 4 层加。
type ISOs interface {
	// CreateISO 建一条获取中的 ISO：入参只填 Name、Family、Series。系必须属于族，Name 是可存文本，否则报
	// ErrInvalid。仓储生成 ID，State 为 acquiring，Rev 为 1。
	CreateISO(ctx context.Context, iso model.ISO, a Audit) (model.ISO, error)
	// UpdateISO 是第 4 层导入用的系统更新，不审计：可以改 Version、Arch、State、SHA256、Artifacts、Offers；
	// 改 Family、Series、Name 报 ErrInvalid。要置为 ready 时必须通过 ISO.Validate，否则报
	// ProblemsError(ErrInvalid)；不能经它置为 deprecated。SHA256 撞上另一个不是导入失败的 ISO，报
	// DuplicateError，Existing 是对方的 ID（C4）。
	UpdateISO(ctx context.Context, iso model.ISO) (model.ISO, error)
	// DeprecateISO 只能把 ready 改成 deprecated，其他状态报 ErrConflict。
	DeprecateISO(ctx context.Context, id string, rev int64, a Audit) error
	// DeleteISO 删除一个 ISO；有任务引用报 ErrReferenced。
	DeleteISO(ctx context.Context, id string, rev int64, a Audit) error
	GetISO(ctx context.Context, id string) (model.ISO, error)
	ListISOs(ctx context.Context, q ISOQuery) (Page[model.ISO], error)
}

// Hooks 是 hook 的身份与版本（C8–C11、C22、C23、C25、C26）。
//
// 名字放在身份上：每个 hook 一行身份，名字唯一，各版本共用，返回的每个版本 Name 都是身份现在的名字。版本的状态
// draft → review → approved → deprecated，review 可以退回 draft，不合这几步的转换报 ErrConflict。每个身份
// 同时最多一个未审批的版本（草稿或待审批）；已审批与已废弃的版本永不删除，只能删草稿。
type Hooks interface {
	// CreateHook 建一个新身份，加上版本 1 的草稿；入参不带 ID 与版本。名字按 model.NormalizeName 规范化，
	// 不合法或用了保留前缀（name_reserved，C12）报 ProblemsError(ErrInvalid)；key 被别的身份占了报
	// DuplicateError。说明与每份实现的正文都要是可存文本，否则报 ProblemsError(ErrInvalid)，问题是
	// text_not_storable。
	CreateHook(ctx context.Context, h model.Hook) (model.Hook, error)
	// SaveHookDraft 保存草稿。只能改草稿，否则报 ErrImmutable；Name 必须等于身份现在的名字，否则报
	// ErrInvalid（改名用 RenameHook）；Rev 对不上报 ErrConflict。
	SaveHookDraft(ctx context.Context, h model.Hook) (model.Hook, error)
	// NewHookVersion 复制版本 from 的内容，建下一个版本号的草稿，作者为 author。已有草稿或待审批的版本报
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
	// ListHookVersions 返回一个身份的全部版本，按版本号升序。
	ListHookVersions(ctx context.Context, id string) ([]model.Hook, error)
	// ListHooks 每个身份一项，见 HookQuery.ApprovedOnly。
	ListHooks(ctx context.Context, q HookQuery) (Page[model.Hook], error)
	// EnsureBuiltinHook 装载一个内置的 hook 版本（Builtin 为 true，State 为 approved，spec §6.2 第 11 条），
	// 不审计：同一个 ID 与版本不存在就写入；已存在并且内容相同，什么都不做；内容不同报 ErrBuiltinMismatch；
	// 名字被别的身份占了，报 DuplicateError。名字必须带保留前缀 model.ReservedNamePrefix（C12），不带报 ErrInvalid。
	// 名字随身份定下：身份已在库里时，以后的版本不能换名，换了名同样报 ErrBuiltinMismatch；要改名只能换一个新 ID。
	EnsureBuiltinHook(ctx context.Context, h model.Hook) error
}

// Templates 是模板的身份与版本（C12–C17、C22–C26）。各方法与 Hooks 一一对应（CreateTemplate 对 CreateHook，依此
// 类推），没写注释的规则相同；另有四处不同：可以派生（DeriveTemplate）；审批时连带它引用的 hook 版本一起判定；ListTemplates 可以按系过滤；
// 保存模板的任何一个版本时，仓储同时维护这个版本引用了哪些 hook 版本（HookRefs），DeleteHookDraft 与建任务都
// 靠它。
type Templates interface {
	CreateTemplate(ctx context.Context, t model.Template) (model.Template, error)
	SaveTemplateDraft(ctx context.Context, t model.Template) (model.Template, error)
	NewTemplateVersion(ctx context.Context, id string, from int, author string) (model.Template, error)
	// DeriveTemplate 从来源版本 from 派生一个新身份（C24），不审计：来源可以是任何状态。复制族、系、正文、
	// 参数、取值来源与装配表，建新身份的版本 1 草稿，名字为 name（规则同 CreateTemplate），作者为 author，
	// DerivedFrom 记下来源。
	DeriveTemplate(ctx context.Context, from model.TemplateKey, name, author string) (model.Template, error)
	SubmitTemplate(ctx context.Context, k model.TemplateKey, rev int64, a Audit) error
	// ApproveTemplate 同 ApproveHook，判定的是模板：事务里锁住它引用的 hook 版本，对最新数据重跑判定
	// （ApproveTemplateProblems）。引用的 hook 刚被废弃时，会因此报 hook_ref_deprecated（C26）。
	ApproveTemplate(ctx context.Context, k model.TemplateKey, rev int64, approver string, a Audit) error
	RejectTemplate(ctx context.Context, k model.TemplateKey, rev int64, a Audit) error
	DeprecateTemplate(ctx context.Context, k model.TemplateKey, rev int64, a Audit) error
	RenameTemplate(ctx context.Context, id, name string, a Audit) error
	DeleteTemplateDraft(ctx context.Context, k model.TemplateKey, rev int64) error
	GetTemplate(ctx context.Context, k model.TemplateKey) (model.Template, error)
	ListTemplateVersions(ctx context.Context, id string) ([]model.Template, error)
	ListTemplates(ctx context.Context, q TemplateQuery) (Page[model.Template], error)
	EnsureBuiltinTemplate(ctx context.Context, t model.Template) error
}

// Tasks 是装机任务（C27–C31、C33）。仓储保证约束，并保证并发下一次推进只生效一次；一步推进合不合规矩由第 7 层
// 的状态机判断（spec §6.2 第 4 条）。
type Tasks interface {
	// CreateTasks 一次建出一批任务（C29、C30、C31），每个任务写一条审计：
	//  1. 先查入参（CheckNewTasks），不合格报 ErrInvalid。快照里的每个串都要存得下（model.StorableText），存不下的
	//     固化不下来：由 CheckNewTasks 在写之前报出，不等到 PostgreSQL 存不进去。
	//  2. 开事务，按 SN 升序给涉及的主机加写锁，对 ISO、模板版本和它引用的 hook 版本加读锁。拿锁的顺序固定，
	//     两批同时提交时就不会互相等死。
	//  3. 用锁住之后读到的数据重跑准入判定（BatchProblems），没登记的 SN 报 host_not_registered。任何一处有
	//     问题，整批回滚，报 BatchError，不写任务，也不写审计。
	//  4. 全部通过才写：ID 用 NewID 生成，State 为 pending，Rev 为 1。并发下撞上「一台主机一个未结束任务」的
	//     约束时，同样回滚并报 BatchError，问题是 host_has_open_task。
	CreateTasks(ctx context.Context, ts []model.Task, a Audit) ([]model.Task, error)
	GetTask(ctx context.Context, id string) (model.Task, error)
	// FindOpenTask 找这台主机未结束的那个任务（C31 保证最多一个），给第 7 层引导时用；没有时第二个返回值为
	// false。
	FindOpenTask(ctx context.Context, sn string) (model.Task, bool, error)
	ListTasks(ctx context.Context, q TaskQuery) (Page[model.Task], error)
	// TransitionTask 是系统发起的条件推进，不审计：当前的状态与 Rev 都等于 expect 才写入，否则报 ErrConflict；
	// 成功后 Rev 加 1。events 与状态在同一个事务里写，规则同 AppendEvents。目标状态不合法报 ErrInvalid。
	// 推进到未结束的状态时，这台主机已有另一个未结束的任务（比如失败的任务重试时，期间又建了新任务），报
	// ProblemsError(ErrRejected)，问题是 host_has_open_task。
	TransitionTask(ctx context.Context, id string, expect TaskExpect, to model.TaskState, events []NewEvent) (model.Task, []Appended, error)
	// TransitionTaskByUser 是用户发起的推进（取消、重试），要审计，其余与 TransitionTask 相同。
	TransitionTaskByUser(ctx context.Context, id string, expect TaskExpect, to model.TaskState, events []NewEvent, a Audit) (model.Task, []Appended, error)
}

// Events 是任务的追加式日志，不审计。
type Events interface {
	// AppendEvents 给任务追加事件，Seq 由仓储按任务连续分配。Key 已存在的那一条跳过，报告为 Duplicate，
	// 不是错误：第 7 层收到重发的信号时据此去重。任务不存在报 ErrNotFound；事件不合法（CheckEvents）报
	// ErrInvalid，整批不写。
	AppendEvents(ctx context.Context, taskID string, es []NewEvent) ([]Appended, error)
	// ListEvents 返回 Seq 大于 afterSeq 的事件，按 Seq 升序；limit 的规则同分页（CheckQuery）。
	ListEvents(ctx context.Context, taskID string, afterSeq int64, limit int) ([]Event, error)
}

// AuditLog 是审计的查询。审计由要审计的写操作在同一个事务里写，没有单独写审计的方法。
type AuditLog interface {
	ListAudit(ctx context.Context, q AuditQuery) (Page[AuditEntry], error)
}
