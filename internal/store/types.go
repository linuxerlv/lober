package store

import (
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// Audit 是调用方给的一条审计：路由表声明审计动作，中间件按当前用户和动作生成它，处理函数原样交给仓储的
// 写操作（spec §6.3）。零值表示没给审计。字段的规则由 CheckAudit 查。
type Audit struct {
	// Actor 是操作者，1–128 字节的可存文本（model.StorableText）。用户与会话在第 5 层，这里只是一个字符串。
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
	// TargetKind 是审计的对象，由仓储填：host、iso、hook、template、task 之一。
	TargetKind string
	// TargetID 是对象的标识，由仓储填：主机是 SN，ISO 与任务是 ID，hook 与模板的版本是「ID@版本」，
	// 改名这类针对整个身份的操作是 ID。
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
}

// 下面是各查询类型。它们都带 Cursor 与 Limit，规则由 CheckQuery 查：Cursor 是上一页的 Next，空串表示从头取；
// Limit 为 0 时取 100，1–500 照用，超过 500 报 ErrInvalid。过滤条件为零值表示不按它过滤；切片的过滤条件（States）
// 为 nil 与为空切片相同，都表示不按它过滤，不是「状态在空集合里」：第 5 层把 JSON 里的空数组解出来就是空切片。
//
// 顺序固定，翻页才不会漏也不会重：主机按 SN；ISO、hook、模板按名字的 key，再按 ID；任务按 CreatedAt 倒序，
// 再按 ID；审计按 ID 倒序。事件不分页，由 ListEvents 按 Seq 续读。

// HostQuery 查主机。
type HostQuery struct {
	SNPrefix string
	// AttrKey 与 AttrValue 要么都给，要么都不给；给了就要求这个属性的值完全相等。
	AttrKey, AttrValue string
	Cursor             string
	Limit              int
}

// ISOQuery 查 ISO。
type ISOQuery struct {
	Family catalog.Family
	Series catalog.Series
	States []model.ISOState
	Cursor string
	Limit  int
}

// HookQuery 查 hook，每个身份一项。
type HookQuery struct {
	NamePrefix string
	States     []model.VersionState
	// ApprovedOnly 时每个身份给当前的已审批版本（与 model.CurrentApproved 同一规则），没有就跳过；
	// 否则给最新的版本。
	ApprovedOnly bool
	Cursor       string
	Limit        int
}

// TemplateQuery 查模板，与 HookQuery 相同，另可按系过滤。
type TemplateQuery struct {
	NamePrefix   string
	States       []model.VersionState
	ApprovedOnly bool
	Series       catalog.Series // 只列勾了这个系的
	Cursor       string
	Limit        int
}

// TaskQuery 查任务。
type TaskQuery struct {
	HostSN                 string
	States                 []model.TaskState
	CreatedFrom, CreatedTo int64 // CreatedAt 的范围，含两端
	Cursor                 string
	Limit                  int
}

// AuditQuery 查审计。各过滤条件都是完全相等。
type AuditQuery struct {
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
