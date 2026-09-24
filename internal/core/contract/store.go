// 本文件定义 Store 契约：窄接口族、查询过滤器、sentinel error。对应实现
// 计划任务 10，依据 SPEC §2.6、§2.7、§3.3、§3.4、§3.6、§4.2、§4.3、§4.4、
// §5.5、§9.4、§12.8。
//
// 拆成窄接口族而不是一个大接口：渲染器只需要模板查询，超时扫描只需要任务
// 查询——窄依赖让每个模块的测试替身只实现自己用到的那几个方法。
//
// AuditStore 是本次契约变更新增的成员（见 docs/real-machine/05-审计持久化.md）：
// audit_log 表在 M1 就已建好，但此前没有任何窄接口把它纳入 Store——
// 审计记录因此只能写进 log/slog，
// 写了就查不回来。补上本接口后，两层缺口（只写不查、写的地方绕开了
// Store 抽象）都在契约层面消除，具体落地见 AuditStore 类型文档。
package contract

import (
	"context"
	"errors"

	"github.com/lober/lober/internal/core/model"
)

// 五个 sentinel error，调用方用 errors.Is 判定而非比较字符串。
var (
	// ErrNotFound 表示查不到。token 查询时，token 存在但已过期、或用途不符，
	// 都返回本错误——不泄露「token 存在但过期了」这一区分本身（§4.4）。
	ErrNotFound = errors.New("contract: 未找到")

	// ErrConflict 表示 CAS 失败：期望的 state 或 version 与库中当前值不符。
	// 调用方必须重读后自行决定是否重试，绝不能无条件覆盖（§3.6）。
	ErrConflict = errors.New("contract: 状态或版本冲突（CAS 失败）")

	// ErrDuplicateSeq 表示信号去重命中 (task_id, seq) 唯一索引。这不是错误
	// 路径，是机器侧重试造成的正常重复投递——handler 应回成功，不是把它当
	// 异常处理（§2.5、§3.6）。
	ErrDuplicateSeq = errors.New("contract: 信号序号重复（正常去重，非错误路径）")

	// ErrActiveTaskExists 表示同一主机已有非终态任务（§3.6 的 partial unique
	// index）。没有它，网关查到同一主机的两个 pending 任务时无法决定渲染
	// 哪种引导形态（§4.7）。
	ErrActiveTaskExists = errors.New("contract: 该主机已有非终态任务")

	// ErrStillReferenced 表示引用完整性拦截（§2.7）：被 active template 引用
	// 的 artifact_set / hook 只能 deprecate，不能删除；存在非终态 task 的
	// template 不能删除。
	ErrStillReferenced = errors.New("contract: 仍被引用，不能删除")
)

// TaskFilter 是任务列表查询的过滤条件，用结构体而非一长串参数，便于将来
// 加字段不改签名（计划任务 10 设计要点）。零值表示不过滤该字段。
//
// 文本搜索（q）刻意不进本结构：主机数是百级，控制台 handler 取回后在 Go
// 里过滤即可；给 Store 加查询参数要连带改 conformance 套件与两个实现，
// 换来的只是几百行数据的遍历节省。
type TaskFilter struct {
	State  model.TaskState // 零值表示不按状态过滤
	HostSN string          // 零值表示不按 SN 过滤

	// FailureReason 非空时只返回该失败原因的任务，供失败分布报表下钻用
	// （P4-04 任务 3：看到「precheck 失败 12 次」之后要能点进去看是哪 12 个
	// 任务、是不是同一个人建的）。不可下钻的失败分布只是一个数字。
	FailureReason model.FailureReason

	// CreatedSinceTS/CreatedUntilTS 非零时限定 CreatedAt 的**闭区间**
	// [CreatedSinceTS, CreatedUntilTS]，供装机统计报表按时间段取数。
	//
	// 闭区间这一点与 AuditFilter.SinceTS/UntilTS 刻意保持一致：两处是运维
	// 在两个界面上看同一个时间窗口，包含性不同会让边界上的记录在一处出现、
	// 另一处消失，而那种差异极难被当成 bug 报出来。
	//
	// 两者都非零且 CreatedSinceTS > CreatedUntilTS 时闭区间本身为空，实现
	// 返回空结果而不是报错（与 AuditFilter 同一处置）。
	CreatedSinceTS int64
	CreatedUntilTS int64

	// Limit 限定返回条数，语义是「按 CreatedAt 倒序排好之后取前 Limit 条」，
	// 即最新的若干条——不是「任取 Limit 条再排序」。报表与 CSV 导出的行数
	// 上限靠它做规模保护（P4-04 任务 4）。
	//
	// **零值不是「不限制」**，而是取 M1 的默认上限 1000（这是 ListTasks 一
	// 直以来的行为，本字段只是把它变成可被调用方覆盖的参数）。显式传大于
	// 1000 的值是允许的：CSV 导出需要一次取出远超 1000 行，那是调用方明确
	// 知情的选择，而不是忘了分页。
	//
	// 与 AuditFilter.Limit 的零值语义**刻意不同**（那边零值是真的不限制）：
	// 两处的既有行为本就不同，统一它们会改掉其中一处已有调用方依赖的行为。
	Limit int
}

// EventFilter 是事件查询的过滤条件。
type EventFilter struct {
	// AfterSeq 非零时只返回 Seq 大于此值的事件，供 SSE 用 Last-Event-ID
	// 续传（§12.8），不从头重放。
	AfterSeq int
}

// AuditFilter 是审计记录查询的过滤条件，与 TaskFilter/EventFilter 同一种
// 风格：结构体而非一长串参数，加字段不改签名（store.go 既有约定）。零值
// 表示不过滤该维度。
type AuditFilter struct {
	// Target 非空时只返回 Target 完全等于该值的记录（如某个 hook id）——
	// 这是「查某条 hook 的驳回理由」这个用例需要的最小维度，不做子串匹配。
	Target string

	// Actor 非空时只返回该操作人的记录，**精确匹配不做子串**（P4-04 任务 2
	// 的四项查询条件之一）。用例是人员变动或权限审查时回答「某个人做了
	// 什么」。不做子串是因为查 "bob" 时把 "bobby" 的操作一并带出来，在追责
	// 场景下是不可接受的误差。
	Actor string

	// Action 非空时只返回该动作类型的记录，同样精确匹配。用例是「把全部
	// task.create 拉出来逐条核对」——§3.2 承认建任务即授权是残留风险，
	// 事后复核靠的就是这一维。
	Action string

	// SinceTS/UntilTS 非零时限定 TS 的闭区间 [SinceTS, UntilTS]。两者
	// 都非零且 SinceTS > UntilTS 时实现应返回空结果而不是报错——这是
	// 调用方传参颠倒的正常后果，不是需要中断请求的异常。
	SinceTS int64
	UntilTS int64

	// Limit 非零时限定返回条数，按 TS 倒序取最新的 Limit 条（§9.4 的
	// 审计用途是「事后追责」，最近发生的记录价值最高，倒序+limit 让
	// 调用方不必先取全量再在应用层截断）。零值表示不限制条数。
	Limit int
}

// TaskStore 是状态机唯一的任务持久化出口。
type TaskStore interface {
	// CreateTask 落库一条新任务，初始状态必须是 pending。
	//
	// 若该主机（按 task.HostSN）已存在非终态任务，返回 ErrActiveTaskExists
	// （§3.6 的 partial unique index 在应用层的体现；实现若用数据库唯一索引
	// 兜底，命中冲突时同样映射为本错误）。
	CreateTask(ctx context.Context, task model.Task) (model.Task, error)

	// GetTask 按 id 查任务。查不到返回 ErrNotFound。
	GetTask(ctx context.Context, taskID string) (model.Task, error)

	// FindPendingTaskByIdentity 按主机身份查 pending 任务，判据是 SN → MAC →
	// UUID 优先级回退（§4.3），空键跳过。只匹配 state=pending 的任务——其它
	// 状态的任务不接受新的引导请求（§4.2），查不到统一返回 ErrNotFound，
	// 网关据此回 no_task、不建任务不记状态（§0.1）。
	//
	// sn、mac、uuid 三者均可能为空串；实现必须按优先级依次尝试非空的键，
	// 全部为空则直接返回 ErrNotFound。
	FindPendingTaskByIdentity(ctx context.Context, sn, mac, uuid string) (model.Task, error)

	// ListTasks 按过滤条件列出任务。
	ListTasks(ctx context.Context, filter TaskFilter) ([]model.Task, error)

	// CASTaskState 是唯一允许改变 task.state 的方法（§3.6）。以
	// (taskID, expectedState, expectedVersion) 为条件：
	//   UPDATE tasks SET state=newState, version=version+1
	//   WHERE id=taskID AND state=expectedState AND version=expectedVersion
	// 不匹配（状态或版本已变）返回 ErrConflict，调用方须重读后自行决定是否
	// 重试。成功时返回更新后的任务（含自增后的 version）。
	CASTaskState(ctx context.Context, taskID string, expectedState model.TaskState, expectedVersion int, newState model.TaskState) (model.Task, error)

	// MutateTaskFields 更新不涉及状态转换的运行时字段（信号水位、attempt、
	// token、决策参数等，见 model.TaskMutation）。
	//
	// TaskMutation 类型本身不含 State 字段，从类型层面消除绕过 CAS 改状态的
	// 后门；但 Go 允许通过反射等手段绕过类型系统，因此实现仍必须保证不新增
	// 任何能设置 state 列的调用路径——这条约束在类型层已经做了一半，Store
	// 实现只需要不主动破坏它，不需要再加运行时校验。
	MutateTaskFields(ctx context.Context, taskID string, mutation model.TaskMutation) (model.Task, error)

	// ListTimedOutTasks 返回超时扫描应判失败的任务：当前阶段的
	// phase_entered_at + 该阶段预算 < asOf 的非终态任务。
	//
	// 实现必须以 model.BudgetedPhases() 为查询范围（排除无预算的阶段，即
	// pending），不能遍历全部 Phase 取值再跳过 pending——后者在 Phase 新增
	// 取值时容易漏改（model 包作者的下游说明）。
	ListTimedOutTasks(ctx context.Context, asOf int64) ([]model.Task, error)

	// GetTaskByToken 按 token 值与用途查任务。token 不存在、已过期、或
	// 与请求的 purpose 不符，统一返回 ErrNotFound——不区分「不存在」与
	// 「过期/用途不符」这三种情况，避免向未认证的机器面调用方泄露 token
	// 曾经存在过（§4.4）。
	GetTaskByToken(ctx context.Context, token string, purpose model.TokenPurpose) (model.Task, error)
}

// HostStore 承载已发现主机清单（§4.3）。
type HostStore interface {
	// UpsertHost 按 model.Host 的三个查找键落库或更新一条主机记录（discovery
	// 同步或人工录入）。
	UpsertHost(ctx context.Context, host model.Host) (model.Host, error)

	// GetHost 按 id 查主机；id 的取法由实现决定（本接口不规定主机主键形态，
	// 只规定查找语义）。查不到返回 ErrNotFound。
	GetHost(ctx context.Context, hostID string) (model.Host, error)

	// FindHostByIdentity 按身份查主机，判据与 TaskStore.FindPendingTaskByIdentity
	// 使用同一套 SN → MAC → UUID 回退优先级（空键跳过），保证「建任务时选的
	// 主机」与「引导时匹配的主机」判据一致（计划任务 10 设计要点）。
	// 查不到返回 ErrNotFound。
	FindHostByIdentity(ctx context.Context, sn, mac, uuid string) (model.Host, error)

	// ListHosts 列出全部已发现主机，供建任务界面选取 SN（§3.2、§4.3：SN
	// 必须从清单选取，不接受自由文本）。文本过滤由调用方在 Go 里做。
	ListHosts(ctx context.Context) ([]model.Host, error)
}

// ArtifactSetStore 承载 ISO 导入产物（§2.1）。
//
// 状态更新与内容写回分成两个方法：导入失败时只改状态、不该碰已写入的
// Paths——Paths 是识别与抽取完成后才知道的产物，登记时运维只给名字与本地
// 路径（计划任务 10 设计要点）。
type ArtifactSetStore interface {
	// CreateArtifactSet 登记一个新制品集，初始状态通常是 importing。
	CreateArtifactSet(ctx context.Context, set model.ArtifactSet) (model.ArtifactSet, error)

	// GetArtifactSet 按 id 查制品集。查不到返回 ErrNotFound。
	GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error)

	// ListArtifactSets 列出全部制品集，供选集时按 (os_family, os_version,
	// arch, firmware) 四元组过滤（过滤逻辑在调用方）。
	ListArtifactSets(ctx context.Context) ([]model.ArtifactSet, error)

	// UpdateArtifactSetState 只改 State、ManifestSHA256 与 FailReason，不触及
	// Paths。导入失败路径调用本方法——失败时 Paths 可能尚未写入或只写了一
	// 半，绝不能被本方法覆盖成半成品。
	//
	// reason 是本次契约变更新增的第四个参数（原签名只有三个参数，见本方法
	// 的历史版本）：导入失败时运维只能在 /artifact-sets 列表里看到一个
	// "failed"，不知道是路径打不开、识别不出家族、抽取失败还是 manifest
	// 生成失败——这四种失败的处置完全不同，但此前全都长一样（t01 记录的
	// 契约缺口，见 internal/imports/job.go 文件末尾曾经的契约缺口说明）。
	// 成功路径调用本方法时 reason 传空字符串——空字符串是「没有失败原因」
	// 的正常表达，不是一个需要特殊处理的哨兵值。
	UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256, reason string) (model.ArtifactSet, error)

	// SetArtifactSetContent 写回导入完成后才知道的内容：OSFamily 识别结果与
	// Paths。导入 job 成功路径调用本方法。
	SetArtifactSetContent(ctx context.Context, id string, osFamily model.OSFamily, paths model.ArtifactPaths) (model.ArtifactSet, error)

	// DeleteArtifactSet 删除制品集。若仍被某个 active 状态的 template 引用，
	// 返回 ErrStillReferenced（§2.7：只能 deprecate，不能删）。
	DeleteArtifactSet(ctx context.Context, id string) error
}

// HookStore 承载用户注册的 hook（§2.2）。
type HookStore interface {
	// CreateHook 创建一个新 hook 版本。
	CreateHook(ctx context.Context, hook model.Hook) (model.Hook, error)

	// GetHook 按 (id, version) 查 hook。version 非正数表示取该 id 下最新
	// 版本（与 TemplateStore.GetTemplate 的版本参数语义一致）。
	// 查不到返回 ErrNotFound。
	GetHook(ctx context.Context, id string, version int) (model.Hook, error)

	// GetHookByDigest 按内容寻址 digest 反查 hook 正文——这是渲染时的取法：
	// task 与 template 的 hook_refs pin 住的是 digest 不是 id（§2.6），渲染
	// 时只有 digest 可用。查不到返回 ErrNotFound。
	GetHookByDigest(ctx context.Context, digest string) (model.Hook, error)

	// ListHooks 列出全部 hook（不区分版本，返回各 id 的全部版本），供控制台
	// 管理界面使用。
	ListHooks(ctx context.Context) ([]model.Hook, error)

	// UpdateHookState 更新 hook 的审批状态（draft → review → active →
	// deprecated，§9.4）。正文不可变——本方法不接受修改 Body。
	UpdateHookState(ctx context.Context, id string, version int, state model.HookState, approver string, approvedAt int64) (model.Hook, error)
}

// TemplateStore 承载装机模板（§2.3）。
type TemplateStore interface {
	// CreateTemplate 创建一个新模板版本。
	CreateTemplate(ctx context.Context, tpl model.Template) (model.Template, error)

	// GetTemplate 按 (id, version) 查模板。version 非正数表示取该 id 下最新
	// 版本——这是取 pin 住的那一版与取最新版的统一入口：task 创建时传具体
	// 版本号锁定 pin，控制台管理界面传非正数取最新（计划任务 10 设计要点）。
	// 查不到返回 ErrNotFound。
	GetTemplate(ctx context.Context, id string, version int) (model.Template, error)

	// ListTemplates 列出全部模板（不区分版本）。
	ListTemplates(ctx context.Context) ([]model.Template, error)

	// UpdateTemplateState 更新模板状态。
	UpdateTemplateState(ctx context.Context, id string, version int, state model.TemplateState) (model.Template, error)

	// DeleteTemplate 删除模板。若存在引用该模板的非终态 task，返回
	// ErrStillReferenced（§2.7）。
	DeleteTemplate(ctx context.Context, id string, version int) error
}

// EventStore 承载 append-only 的任务事件表（§2.5）。
type EventStore interface {
	// AppendEvent 追加一条事件。
	//
	// event.Seq 为正数时代表信号来源事件，(task_id, seq) 已存在时返回
	// ErrDuplicateSeq——这不是错误路径，是机器侧重试造成的正常重复投递，
	// 调用方应把它当作「已记录过，继续走成功路径」处理。
	//
	// event.Seq 非正数（零或负）表示非信号来源的事件（扫描循环判超时、
	// 人工操作等），由实现分配一个不参与去重的自增序号并写入返回值，
	// 调用方传入的 Seq 值在此情况下被忽略。
	AppendEvent(ctx context.Context, event model.TaskEvent) (model.TaskEvent, error)

	// ListEvents 按任务与过滤条件列出事件，按 Seq 升序返回。
	// filter.AfterSeq 用于 SSE 的 Last-Event-ID 续传（§12.8）。
	ListEvents(ctx context.Context, taskID string, filter EventFilter) ([]model.TaskEvent, error)
}

// AuditRecord 是一条落库的审计记录，字段与 schema 的 audit_log 表逐一
// 对应（id 除外——id 是存储层的自增主键，不进本类型：调用方按 TS 倒序
// 与 Filter 取数据，不需要按 id 寻址单条记录，见 AuditStore.ListAudit
// 的文档）。
//
// 本类型定义在 contract 包而不是 internal/core/model：它不是业务实体，
// 是审计这个横切关注点自己的形态，且 model 包已冻结、不接受因本次契约
// 变更新增类型（本次改动的文件清单不含 internal/core/model/**）。这与
// TaskFilter/EventFilter 同样定义在 contract 包是同一个理由。
type AuditRecord struct {
	// TS 是写入时刻（Unix 秒），由调用方传入（与 AuditLogger.Log 不接受
	// 时钟参数、由 AuditStore 实现自行决定落盘时间同理——具体取哪个时钟
	// 由实现决定，本类型只声明字段含义）。
	TS int64

	// Actor 是操作人标识（用户名），与 AuditLogger.Log 的 actor 参数
	// 同义。
	Actor string

	// Action 是「资源.动作」形式的固定字符串（如 hook.reject），与
	// AuditLogger.Log 的 action 参数同义。
	Action string

	// Target 是被操作对象的标识（如 hook id），与 AuditLogger.Log 的
	// target 参数同义。AuditFilter.Target 按这个字段做精确匹配。
	Target string

	// Detail 是补充说明的原文，与 AuditLogger.Log 的 detail 参数同义。
	// 落库时 JSON 编码进 detail_json 列（schema 早已建成该列为 JSON
	// 文本列），取回时原样解码——detail 本身是自由文本（如
	// "reason=xxx"），不是一个要拆字段的结构体，JSON 编码只是复用既有
	// 列类型的手段，不代表 detail 本身有内部结构。
	Detail string
}

// AuditStore 承载人面操作审计记录的持久化与查询（§9.4）。
//
// 与 AuditLogger（service.go）的关系：AuditLogger 是审计中间件
// （console.auditMiddleware）看到的写入面——一个不关心「写到哪」的
// fire-and-forget 接口，语义类似日志器。AuditStore 是本次契约变更新增
// 的查询面：装配点（cmd/lober）现在提供的 AuditLogger 实现内部持有一个
// AuditStore，Log 调用委托给 AppendAudit；ListAudit 供 HTTP 查询端点
// （GET /hooks/{id}/audit）使用。两者不合并成一个接口——写路径的调用方
// （中间件、hook.go 的 reject handler）只需要 Log 这一个方法，合并会让
// 那些调用点被迫依赖一个查询能力它们根本不使用（窄接口族的一贯原则）。
type AuditStore interface {
	// AppendAudit 追加一条审计记录（append-only，没有更新或删除——审计
	// 记录本身不可变，篡改留痕比篡改内容更严重，因此本接口不提供任何
	// 修改已写入记录的方法）。
	AppendAudit(ctx context.Context, record AuditRecord) error

	// ListAudit 按过滤条件列出审计记录，按 TS 倒序（最新的在前——§9.4
	// 的审计用途是事后追责，运维打开页面最想看到的是最近发生的事）。
	//
	// 查不到匹配记录时返回空切片而非 ErrNotFound：没有审计记录是正常
	// 状态（比如一个刚创建、从未被驳回过的 hook），不是异常，调用方
	// （HTTP handler）不需要为这条查询专门处理一个错误分支。
	ListAudit(ctx context.Context, filter AuditFilter) ([]AuditRecord, error)
}

// FactStore 承载跨阶段跨重启传递的 facts KV（§5.5）。
type FactStore interface {
	// MergeFacts 把给定键值对合并写入该任务的 fact 集合：已存在的键被覆盖，
	// 不存在的键被新增，未出现在本次调用中的既有键保持不变。写入是合并而
	// 非替换——不这样做会导致后一个 hook 的写入抹掉前一个 hook 的结果，
	// 而这正是 facts KV 存在的理由（§5.5）。
	MergeFacts(ctx context.Context, taskID string, facts map[string]string) error

	// GetFacts 返回该任务当前的全部 facts（早先写入的合并结果）。
	GetFacts(ctx context.Context, taskID string) (map[string]string, error)
}

// Store 组合全部窄接口，供装配点（cmd/lober）使用。
//
// 各模块应依赖上面的窄接口，不要依赖本组合接口——渲染器只需要
// TemplateStore/HookStore/ArtifactSetStore，超时扫描只需要 TaskStore，
// 依赖窄接口才能让测试替身只实现用到的那几个方法（计划任务 10 设计要点）。
type Store interface {
	TaskStore
	HostStore
	ArtifactSetStore
	HookStore
	TemplateStore
	EventStore
	FactStore
	AuditStore
}

// TxRunner 是事务运行接口：在单个数据库事务里执行 fn，fn 内部对同一事务
// 可见的 Store 做多次操作，fn 返回错误则整个事务回滚。
//
// 实现必须拒绝嵌套调用（fn 内部再次调用 RunInTx）。原因取决于底层引擎，
// 但两种都不可接受：
//
//   - 单写者引擎（如 SQLite，连接池上限 1）下，嵌套事务会等待自己持有的
//     连接，直接死锁，表现为整个进程卡住无响应。
//   - PostgreSQL 这类放开连接池的引擎下不会死锁，后果更隐蔽：嵌套调用会
//     静默开出一个**独立**事务，外层回滚时内层已经提交，原子性失效且不
//     报任何错。建任务要原子写入 task 与首条 event，这种静默失效会留下
//     「有任务没有事件」或反之的残缺数据。
//
// 拒绝的具体形式（返回 error 还是 panic）由实现自行决定，但绝不能让嵌套
// 调用静默地跑起来——无论是排队等待还是另开一个事务。
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context, store Store) error) error
}
