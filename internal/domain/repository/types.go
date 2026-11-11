package repository

import (
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// Audit 是应用交给业务写操作的保存参数；操作者来自已认证身份，动作由操作契约确定。
// 仓储在业务事务内一起保存它；查询由 application/audit 负责，没有独立写审计入口。
// 零值表示没给审计。字段的规则由 CheckAudit 查。
type Audit struct {
	// Actor 是操作者，1–128 字节的可存文本（model.StorableText）。用户与会话由身份应用管理，这里只是一个字符串。
	Actor string
	// Action 是动作，如 host.import：点分的两段以上，每段以小写字母开头，只含小写字母、数字和下划线，
	// 不超过 64 字节。
	Action string
	// Detail 是补充说明，可空，不超过 4096 字节的可存文本。驳回审批的理由写在这里。
	Detail string
	At     int64 // Unix 秒，大于 0
}

// AuditEntry 是存下来的一条审计。批量的操作每个对象一条，所以一次调用会存下几条同样的 Audit。
type AuditEntry struct {
	ID int64 // 仓储分配，递增
	Audit
	// TargetKind 是审计的对象，由仓储填：host、iso、hook、template、task、user 或 role。
	TargetKind string
	// TargetID 是对象的标识，由仓储填：主机是 SN，ISO 与任务是 ID，hook 与模板的版本是「ID@版本」，
	// 改名这类针对整个身份的操作是 ID；用户是用户名，角色菜单是角色名。
	TargetID string
}

// NewEvent 是要追加给任务的一条事件。Kind 与 Fields 的含义由第 7 层定，仓储不解释它们。字段的规则由
// CheckEvents 查。
type NewEvent struct {
	At   int64  // Unix 秒，大于 0
	Kind string // 写法同 Audit.Action，如 slot.exit
	// Key 可空；非空时是 1–128 字节的安全词（model.IsSafeWord），同一个任务里唯一，用来去重：第 7 层收到
	// 重发的信号时，同一个 Key 只落一条。
	Key string
	// Fields 的键是标识符（model.ValidIdent），值是可存文本，全部键与值合计不超过 64 KiB。
	Fields map[string]string
}

// Event 是存下来的一条事件。
type Event struct {
	TaskID string
	Seq    int64 // 仓储分配：每个任务从 1 起、连续
	NewEvent
}

// Appended 是追加一条事件的结果。Key 已存在时 Duplicate 为 true，Seq 是原来那一条的，这一条不再写入。
type Appended struct {
	Seq       int64
	Duplicate bool
}

// Page 是分页查询的一页。Next 是不透明的游标，原样放进下一次查询的 Cursor；空串表示没有下一页。
type Page[T any] struct {
	Items []T
	Next  string
	Total *int64 // Present only for numbered pagination, including zero matches.
}

// 下面是各查询类型。Page > 0 时使用页码分页，Limit 为 0 时取 20；Page = 0 时保留游标分页。
// 页码与非空 Cursor 不可混用，规则由 CheckPageQuery 查。Cursor 是上一页的 Next，空串表示从头取；
// 游标模式 Limit 为 0 时取 100，1–500 照用，超过 500 报 ErrInvalid。过滤条件为零值表示不按它过滤；切片的过滤条件（States）
// 为 nil 与为空切片相同，都表示不按它过滤，不是「状态在空集合里」：第 5 层把 JSON 里的空数组解出来就是空切片。
//
// 顺序固定，翻页才不会漏也不会重。字符串一律按字节序比较，不随数据库的排序规则变（pgstore 用 COLLATE "C"）：
// 主机按 SN；ISO 按 Name 再按 ID，ISO 的名字不规范化、没有 key，所以区分大小写，与 hook、模板不同；hook 与模板
// 按名字的 key（model.NormalizeName），再按 ID；任务按 CreatedAt 倒序，同一时刻按 ID 升序；审计按 ID 倒序。事件
// 不分页，由 ListEvents 按 Seq 升序续读。

// HostQuery 查主机。
type HostQuery struct {
	Page     int // Zero preserves cursor pagination; positive values select a numbered page.
	SNPrefix string
	// AttrKey 与 AttrValue 要么都给，要么都不给；给了就要求这个属性的值完全相等。
	AttrKey, AttrValue string
	Cursor             string
	Limit              int
}

// ISOQuery 查 ISO。
type ISOQuery struct {
	Page   int // Zero preserves cursor pagination; positive values select a numbered page.
	Family catalog.Family
	Series catalog.Series
	States []model.ISOState
	Cursor string
	Limit  int
}

// HookQuery 查 hook，默认每个身份一项。AllVersions 按版本列出，每个身份按版本号升序。
type HookQuery struct {
	AllVersions bool // Return every version instead of one selected version per identity.
	Page        int  // Zero preserves cursor pagination; positive values select a numbered page.
	NamePrefix  string
	States      []model.VersionState
	// ApprovedOnly 时每个身份给当前的已审批版本（与 model.CurrentApproved 同一规则），没有就跳过；
	// 否则给最新的版本。
	ApprovedOnly bool
	Cursor       string
	Limit        int
}

// TemplateQuery 查模板，与 HookQuery 相同，另可按系过滤。
type TemplateQuery struct {
	Selection    *TemplateSelection // Optional wizard eligibility, applied before counting and paging.
	Page         int                // Zero preserves cursor pagination; positive values select a numbered page.
	NamePrefix   string
	States       []model.VersionState
	ApprovedOnly bool
	Series       catalog.Series // 只列勾了这个系的
	Cursor       string
	Limit        int
}

// TemplateCandidateFacts 是同次只读查询取得的一个模板与其精确引用；Hooks不含其他模板的组件。
// 应用只读这份结果，不能将候选当成提交保证；普通输入输出字段随ListTemplateCandidates追溯。
type TemplateCandidateFacts struct {
	Template model.Template
	Hooks    map[model.HookKey]model.Hook
}

// TemplateSelection supplies immutable facts needed to select wizard candidates.
// Eligibility matches admission.TemplateUsable plus the current builtin manifest.
type TemplateSelection struct {
	ISO              model.ISO
	BuiltinTemplates []model.TemplateKey
	BuiltinHooks     []model.HookKey
}

// TaskQuery 查任务。
type TaskQuery struct {
	Page                   int // Zero preserves cursor pagination; positive values select a numbered page.
	HostSN                 string
	States                 []model.TaskState
	CreatedFrom, CreatedTo int64 // CreatedAt 的范围，含两端
	Cursor                 string
	Limit                  int
}

// AuditQuery 查审计。各过滤条件都是完全相等。
type AuditQuery struct {
	Page                 int // Zero preserves cursor pagination; positive values select a numbered page.
	TargetKind, TargetID string
	Actor, Action        string
	From, To             int64 // At 的范围，含两端
	Cursor               string
	Limit                int
}

// TaskExpect 是条件推进的期望值：任务当前的状态与 Rev 都等于它们才写入（C33）。
type TaskExpect struct {
	State model.TaskState
	Rev   int64
}
