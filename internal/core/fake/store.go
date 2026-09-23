// 本文件实现内存 Store：contract.Store 与 contract.TxRunner 的测试夹具。
// 对应实现计划任务 17，依据 SPEC §2.5、§2.6、§2.7、§3.3、§3.4、§3.6、§4.3、
// §4.4、§4.7、§5.5、§12.8。
//
// 存在的唯一理由：M3-M8 六个模块的测试都用它，不需要等 M2 的 SQLite 实现
// 就能开工——这是整个并行方案成立的关键（计划任务 17）。它必须通过
// internal/core/contract/conformance 的可执行行为契约套件，且在换成 SQLite
// 实现后，依赖它写的测试行为不应改变。
//
// 设计要点（计划任务 17）：
//   - 所有返回值必须深拷贝：返回内部指针会让一个测试里的修改渗进另一个
//     测试，这类 bug 极难定位。
//   - 身份查找与 partial unique index 的语义要手工模拟：遍历时跳过空键、
//     只匹配 pending、判重时只看非终态任务——这些在 SQL 里由索引保证，
//     内存实现要显式写。
//   - 改运行时字段时若携带状态就返回错误，与将来的 SQLite 实现行为一致。
//     model.TaskMutation 类型本身不含 State 字段，因此本实现天然不存在
//     写 state 列的路径，不需要额外的运行期校验。
//   - 事务运行在内存实现里退化为直接执行（单进程单锁），但仍必须拒绝
//     嵌套调用，保证行为与真实 SQLite 实现一致。
//   - 非信号事件用递减的负数序号，与信号的正序号天然不冲突。
package fake

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// Store 是内存实现，一把读写锁保护全部状态——状态机测试会并发 CAS，
// 读写锁让并发读（如超时扫描的列表查询）不必互相阻塞，写操作（CAS、
// MutateTaskFields、AppendEvent 等）互斥。
type Store struct {
	mu sync.RWMutex

	tasks    map[string]model.Task // key: task id
	hosts    map[string]model.Host // key: host id（本实现取 SN 非空则用 SN，否则用 MAC，否则用 UUID）
	artifact map[string]model.ArtifactSet
	// hooks 以 "id@version" 为主键，与 SQLite 实现的 (id, version) 复合主键
	// 语义对齐（计划任务 13：hooks 的主键是 (id, version)）。
	hooks map[string]model.Hook
	// hooksByDigest 是 digest 到 hook 的反查索引，key 为完整 digest 字符串。
	hooksByDigest map[string]model.Hook
	// templates 以 "id@version" 为主键，与 hooks 同理。
	templates map[string]model.Template
	// events 按 task id 分桶，桶内切片按写入顺序天然递增，ListEvents 遍历
	// 时按 Seq 排序返回（AppendEvent 保证同一任务内 Seq 严格递增分配，因此
	// 写入顺序即 Seq 顺序，遍历无需额外排序）。
	events map[string][]model.TaskEvent
	// facts 按 task id 分桶，桶内是该任务全部 facts 的合并结果。
	facts map[string]map[string]string
	// audit 是全部审计记录，按写入顺序追加（append-only，与真实的
	// audit_log 表一样没有更新或删除）。ListAudit 遍历这个切片按 Filter
	// 筛选、按 TS 倒序返回——记录数量级是人面操作次数，规模远小于任务/
	// 事件表，遍历代价可忽略（与 HookStore.ListHooks 用 map 遍历代价相同
	// 的取舍）。
	audit []contract.AuditRecord

	// nextHostID 给没有可用身份键的主机分配一个回退 id（现实中不会发生，
	// Host 的三个查找键理论上不会同时为空，这里只是防御性兜底）。
	nextHostID int

	// nextNonSignalSeq 是下一个待分配的非信号事件序号，按任务分桶、递减。
	// 递减而非递增：信号序号天然是正数且递增（model.LookupSignal 表），
	// 非信号事件用递减的负数序号，两个序号空间永不相交，且都保持「新事件
	// 序号在数轴上更远离 0」这一单调性质，ListEvents 按 Seq 排序时行为
	// 直观。
	nextNonSignalSeq map[string]int

	// inTx 标记当前是否已处于一次 RunInTx 调用内部，用于拒绝嵌套调用
	// （contract.TxRunner 明确要求）。单独用一把锁保护而不是复用 mu，
	// 避免事务内部对 mu 的正常读写操作与这个标记的检查互相死锁。
	txMu sync.Mutex
	inTx bool
}

// NewStore 返回一个空的内存 Store，各类实体的 map 均已初始化为空集合
// （而不是 nil map，避免调用方读到 nil map 上的读操作 panic）。
func NewStore() *Store {
	return &Store{
		tasks:            make(map[string]model.Task),
		hosts:            make(map[string]model.Host),
		artifact:         make(map[string]model.ArtifactSet),
		hooks:            make(map[string]model.Hook),
		hooksByDigest:    make(map[string]model.Hook),
		templates:        make(map[string]model.Template),
		events:           make(map[string][]model.TaskEvent),
		facts:            make(map[string]map[string]string),
		nextNonSignalSeq: make(map[string]int),
	}
}

// 编译期断言：*Store 必须同时满足组合 Store 接口与事务运行接口
// （计划任务 17「实现流程」第四步明确要求）。
var (
	_ contract.Store    = (*Store)(nil)
	_ contract.TxRunner = (*Store)(nil)
)

// errNestedTx 是 RunInTx 拒绝嵌套调用时返回的错误。不导出为 contract 层的
// sentinel error——嵌套调用是调用方的编程错误（不是像 ErrConflict 那样的
// 正常业务分支），调用方不需要用 errors.Is 判定它、只需要知道调用失败了。
var errNestedTx = errors.New("fake: RunInTx 不支持嵌套调用")

// ---------------------------------------------------------------------------
// 深拷贝辅助函数（计划任务 17 设计要点：所有返回值必须深拷贝）
// ---------------------------------------------------------------------------

// copyStringSlice 返回 s 的一份独立拷贝；s 为 nil 时返回 nil（不把 nil 变成
// 空切片，保持与调用方原始语义一致，便于 go-cmp 之类的比较不因 nil/空切片
// 的差异误报）。
func copyStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// copyStringMap 返回 m 的一份独立拷贝；m 为 nil 时返回 nil。
func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// copyFailureDetail 返回 *model.FailureDetail 的一份独立拷贝；d 为 nil 时
// 返回 nil。FailureDetail 本身全是值类型字段，浅拷贝指向的结构体即为深拷贝。
func copyFailureDetail(d *model.FailureDetail) *model.FailureDetail {
	if d == nil {
		return nil
	}
	out := *d
	return &out
}

// copyTask 返回 model.Task 的一份独立拷贝，逐个处理含切片/map/指针的字段
// （HookDigests、FormData、ResolvedParams、FailureDetail）——这四个字段是
// CreateTask/GetTask 等方法在库内存储与对外返回时必须深拷贝的部分，其余
// 字段全是值类型，结构体本身的赋值已经是深拷贝。
func copyTask(t model.Task) model.Task {
	out := t
	out.HookDigests = copyStringSlice(t.HookDigests)
	out.FormData = copyStringMap(t.FormData)
	out.ResolvedParams = copyStringMap(t.ResolvedParams)
	out.FailureDetail = copyFailureDetail(t.FailureDetail)
	return out
}

// copyArtifactPaths 返回 model.ArtifactPaths 的一份独立拷贝，Initrd 切片与
// Extra map 是唯一需要显式处理的引用类型字段。
func copyArtifactPaths(p model.ArtifactPaths) model.ArtifactPaths {
	out := p
	out.Initrd = copyStringSlice(p.Initrd)
	out.Extra = copyStringMap(p.Extra)
	return out
}

// copyArtifactSet 返回 model.ArtifactSet 的一份独立拷贝。
func copyArtifactSet(a model.ArtifactSet) model.ArtifactSet {
	out := a
	out.Paths = copyArtifactPaths(a.Paths)
	return out
}

// copyHookRefSlice 返回 []model.HookRef 的一份独立拷贝，每个元素的
// ParamsDefaults map 也要逐个拷贝。
func copyHookRefSlice(refs []model.HookRef) []model.HookRef {
	if refs == nil {
		return nil
	}
	out := make([]model.HookRef, len(refs))
	for i, r := range refs {
		out[i] = r
		out[i].ParamsDefaults = copyStringMap(r.ParamsDefaults)
	}
	return out
}

// copyHook 返回 model.Hook 的一份独立拷贝，OSFamilies 切片与
// Requires.Facts/Capabilities 切片是需要显式处理的引用类型字段。
func copyHook(h model.Hook) model.Hook {
	out := h
	out.OSFamilies = make([]model.OSFamily, len(h.OSFamilies))
	copy(out.OSFamilies, h.OSFamilies)
	out.Requires.Facts = copyStringSlice(h.Requires.Facts)
	out.Requires.Capabilities = copyStringSlice(h.Requires.Capabilities)
	return out
}

// copyTemplate 返回 model.Template 的一份独立拷贝，HookRefs 与
// RequiredFormFields 切片是需要显式处理的引用类型字段。
func copyTemplate(tpl model.Template) model.Template {
	out := tpl
	out.HookRefs = copyHookRefSlice(tpl.HookRefs)
	out.RequiredFormFields = copyStringSlice(tpl.RequiredFormFields)
	return out
}

// copyHost 返回 model.Host 的一份独立拷贝。Host 目前全是值类型字段，直接
// 赋值即深拷贝，本函数只是为了让调用点风格与其它实体一致、并为将来
// Host 加引用类型字段时留一个显式改动点。
func copyHost(h model.Host) model.Host {
	return h
}

// copyTaskEvent 返回 model.TaskEvent 的一份独立拷贝。TaskEvent 目前全是
// 值类型字段。
func copyTaskEvent(e model.TaskEvent) model.TaskEvent {
	return e
}

// hookKey 与 templateKey 构造 hooks/templates map 的复合主键
// "id@version"，与计划任务 13 的 SQLite schema（hooks 主键 (id, version)）
// 语义对齐。
func hookKey(id string, version int) string {
	return id + "@" + strconv.Itoa(version)
}

func templateKey(id string, version int) string {
	return id + "@" + strconv.Itoa(version)
}

// identityMatches 判定给定的 (querySN, queryMAC, queryUUID) 是否命中
// (candidateSN, candidateMAC, candidateUUID)，按 SN → MAC → UUID 优先级
// 回退、空键跳过（SPEC §4.3）。
//
// TaskStore.FindPendingTaskByIdentity 与 HostStore.FindHostByIdentity 必须
// 共享同一套判据（contract 作者的下游说明明确要求：哪怕复制一份也要逐字节
// 一致），这是「建任务时选的主机」与「引导时匹配的主机」判据一致的唯一
// 保障——本函数就是这唯一的一份实现，两个 Find 方法都调用它。
//
// 判据：依次尝试 SN、MAC、UUID 三个键——只要 query 侧该键非空，就要求
// candidate 侧同一键也非空且相等才算命中该键；一旦某一层的 query 键非空
// 但候选对应键为空或不相等，直接判定为不命中该候选（不再降级尝试下一层
// 键，因为「query 给出了 SN 但候选 SN 不符」不代表应该转而比较 MAC）。
//
// 只有当 query 侧某键为空时才跳过该层、尝试下一层键。三个 query 键全部为
// 空时，任何候选都不命中——这是 testEmptySNDoesNotMatchEmptySNTask 锁住的
// 行为：空 SN 不能撞上库里第一个空 SN 的候选。
func identityMatches(querySN, queryMAC, queryUUID string, candidateSN, candidateMAC, candidateUUID string) bool {
	if querySN != "" {
		return candidateSN != "" && candidateSN == querySN
	}
	if queryMAC != "" {
		return candidateMAC != "" && candidateMAC == queryMAC
	}
	if queryUUID != "" {
		return candidateUUID != "" && candidateUUID == queryUUID
	}
	return false
}

// ---------------------------------------------------------------------------
// TaskStore
// ---------------------------------------------------------------------------

// CreateTask 落库一条新任务。若该主机（按 task.HostSN）已存在非终态任务，
// 返回 contract.ErrActiveTaskExists（§3.6 的 partial unique index 在应用层
// 的体现）。
//
// 判重只看非终态任务、且只按 HostSN 判重——与 SQLite 实现计划里
// 「tasks 上按 SN 的 partial unique index，条件是状态不在终态集合内」完全
// 对齐（计划任务 13）。注意这条判重刻意只比较 HostSN 本身（不经过
// identityMatches 的空键跳过逻辑）：空 SN 的任务之间不应互相冲突（一批
// SN 留空的白牌机各自建任务时不能被这条唯一性约束互相拦住），这与
// FindPendingTaskByIdentity 的「空 SN 不匹配」是同一个方向的两种体现——
// 前者防止「空 SN 建任务互相打架」，后者防止「空 SN 引导查询误配对」。
func (s *Store) CreateTask(ctx context.Context, task model.Task) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if task.HostSN != "" {
		for _, existing := range s.tasks {
			if existing.HostSN == task.HostSN && !existing.State.IsTerminal() {
				return model.Task{}, contract.ErrActiveTaskExists
			}
		}
	}

	s.tasks[task.ID] = copyTask(task)
	return copyTask(task), nil
}

// GetTask 按 id 查任务。查不到返回 contract.ErrNotFound。
func (s *Store) GetTask(ctx context.Context, taskID string) (model.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tk, ok := s.tasks[taskID]
	if !ok {
		return model.Task{}, contract.ErrNotFound
	}
	return copyTask(tk), nil
}

// FindPendingTaskByIdentity 按主机身份查 pending 任务，判据是 SN → MAC →
// UUID 优先级回退（§4.3），空键跳过，只匹配 state=pending 的任务。
func (s *Store) FindPendingTaskByIdentity(ctx context.Context, sn, mac, uuid string) (model.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, tk := range s.tasks {
		if tk.State != model.TaskStatePending {
			continue
		}
		if identityMatches(sn, mac, uuid, tk.HostSN, tk.HostMAC, tk.HostUUID) {
			return copyTask(tk), nil
		}
	}
	return model.Task{}, contract.ErrNotFound
}

// ListTasks 按过滤条件列出任务。
func (s *Store) ListTasks(ctx context.Context, filter contract.TaskFilter) ([]model.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Task, 0, len(s.tasks))
	for _, tk := range s.tasks {
		if filter.State != "" && tk.State != filter.State {
			continue
		}
		if filter.HostSN != "" && tk.HostSN != filter.HostSN {
			continue
		}
		out = append(out, copyTask(tk))
	}
	return out, nil
}

// CASTaskState 是唯一允许改变 task.state 的方法（§3.6）。
func (s *Store) CASTaskState(ctx context.Context, taskID string, expectedState model.TaskState, expectedVersion int, newState model.TaskState) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tk, ok := s.tasks[taskID]
	if !ok {
		return model.Task{}, contract.ErrNotFound
	}
	if tk.State != expectedState || tk.Version != expectedVersion {
		return model.Task{}, contract.ErrConflict
	}

	tk.State = newState
	tk.Version++
	s.tasks[taskID] = tk
	return copyTask(tk), nil
}

// MutateTaskFields 更新不涉及状态转换的运行时字段。model.TaskMutation
// 类型本身不含 State 字段，因此本方法天然不存在写 state 列的路径——这条
// 约束在类型层已经做了一半，本实现只需要不主动破坏它（即不要在这里加一个
// 反射之类的手段去推断并写 state），不需要额外的运行期校验。
func (s *Store) MutateTaskFields(ctx context.Context, taskID string, mutation model.TaskMutation) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tk, ok := s.tasks[taskID]
	if !ok {
		return model.Task{}, contract.ErrNotFound
	}

	if mutation.CurrentPhase != nil {
		tk.CurrentPhase = *mutation.CurrentPhase
	}
	if mutation.PhaseEnteredAt != nil {
		tk.PhaseEnteredAt = *mutation.PhaseEnteredAt
	}
	if mutation.AttemptCount != nil {
		tk.AttemptCount = *mutation.AttemptCount
	}
	if mutation.LastSeq != nil {
		tk.LastSeq = *mutation.LastSeq
	}
	if mutation.LastSignalAt != nil {
		tk.LastSignalAt = *mutation.LastSignalAt
	}
	if mutation.Token != nil {
		tk.Token = *mutation.Token
	}
	if mutation.TokenExpiresAt != nil {
		tk.TokenExpiresAt = *mutation.TokenExpiresAt
	}
	if mutation.FinalizeToken != nil {
		tk.FinalizeToken = *mutation.FinalizeToken
	}
	if mutation.FinalizeExpiresAt != nil {
		tk.FinalizeExpiresAt = *mutation.FinalizeExpiresAt
	}
	if mutation.ResolvedParams != nil {
		tk.ResolvedParams = copyStringMap(*mutation.ResolvedParams)
	}
	if mutation.FailureReason != nil {
		tk.FailureReason = *mutation.FailureReason
	}
	if mutation.FailureDetail != nil {
		tk.FailureDetail = copyFailureDetail(mutation.FailureDetail)
	}

	s.tasks[taskID] = tk
	return copyTask(tk), nil
}

// ListTimedOutTasks 返回超时扫描应判失败的任务：当前阶段的
// phase_entered_at + 该阶段预算 < asOf 的非终态任务。
//
// 以 model.BudgetedPhases() 为查询范围（排除无预算的阶段，即 pending），
// 不遍历全部 Phase 取值再跳过 pending——这是 model 包作者的下游说明明确
// 要求的写法，避免 Phase 新增取值时漏改。
func (s *Store) ListTimedOutTasks(ctx context.Context, asOf int64) ([]model.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []model.Task
	for _, tk := range s.tasks {
		if tk.State.IsTerminal() {
			continue
		}
		// model.PhaseBudget 对无预算的阶段（目前只有 pending）返回
		// ok=false——用这个返回值本身判断是否跳过，而不是先构造一份
		// BudgetedPhases() 的集合再查表，两者等价但少一层间接。
		budget, ok := model.PhaseBudget(tk.CurrentPhase)
		if !ok {
			continue
		}
		if tk.PhaseEnteredAt+int64(budget.Seconds()) < asOf {
			out = append(out, copyTask(tk))
		}
	}
	return out, nil
}

// GetTaskByToken 按 token 值与用途查任务。token 不存在、已过期、或与请求
// 的 purpose 不符，统一返回 contract.ErrNotFound（§4.4：不泄露「token 存在
// 但过期了」这一区分）。
func (s *Store) GetTaskByToken(ctx context.Context, token string, purpose model.TokenPurpose) (model.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if token == "" {
		return model.Task{}, contract.ErrNotFound
	}

	// 过期判据用真实 time.Now()：GetTaskByToken 的签名不接受任何时钟/时间
	// 参数（见 contract.TaskStore 文档），§4.4 的 TTL 是真实墙钟时间窗口，
	// 这与将来 SQLite 实现里 `WHERE expires_at > strftime('%s','now')` 的
	// 行为一致——两个实现必须对「过期」给出相同判据，否则 conformance
	// 机制保证的「内存实现测试换成 SQLite 后行为不变」就是空话。
	//
	// conformance 套件的 fixedNow 已改为在套件加载时锚定真实 time.Now()
	// （见 conformance/store.go），不再是编译期硬编码的历史常量，因此不会
	// 随运行日期推移而与这里的真实时钟产生漂移。
	now := time.Now().Unix()

	for _, tk := range s.tasks {
		switch purpose {
		case model.TokenPurposeInstall:
			if tk.Token == token {
				if tk.TokenExpiresAt < now {
					return model.Task{}, contract.ErrNotFound
				}
				return copyTask(tk), nil
			}
		case model.TokenPurposeFinalize:
			if tk.FinalizeToken == token {
				if tk.FinalizeExpiresAt < now {
					return model.Task{}, contract.ErrNotFound
				}
				return copyTask(tk), nil
			}
		}
	}
	return model.Task{}, contract.ErrNotFound
}

// ---------------------------------------------------------------------------
// HostStore
// ---------------------------------------------------------------------------

// hostKey 为一个 model.Host 选取内存 map 的主键：三个查找键按 SN → MAC →
// UUID 优先级取第一个非空值。真实部署中三者不会同时为空（§4.3 末条要求
// Arch 与 Firmware 必填，隐含主机记录本身必须可被识别），这里的空兜底只是
// 防御性处理，不代表一个合法使用场景。
func hostKey(h model.Host) string {
	if h.SN != "" {
		return "sn:" + h.SN
	}
	if h.MAC != "" {
		return "mac:" + h.MAC
	}
	if h.UUID != "" {
		return "uuid:" + h.UUID
	}
	return ""
}

// UpsertHost 按 model.Host 的三个查找键落库或更新一条主机记录。
func (s *Store) UpsertHost(ctx context.Context, host model.Host) (model.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := hostKey(host)
	if key == "" {
		s.nextHostID++
		key = "fallback:" + strconv.Itoa(s.nextHostID)
	}
	s.hosts[key] = copyHost(host)
	return copyHost(host), nil
}

// GetHost 按 id 查主机；本实现取 hostKey(host) 的值作为 id。查不到返回
// contract.ErrNotFound。
func (s *Store) GetHost(ctx context.Context, hostID string) (model.Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	h, ok := s.hosts[hostID]
	if !ok {
		return model.Host{}, contract.ErrNotFound
	}
	return copyHost(h), nil
}

// FindHostByIdentity 按身份查主机，与 TaskStore.FindPendingTaskByIdentity
// 共享同一套 identityMatches 判据（SN → MAC → UUID 回退，空键跳过），保证
// 「建任务时选的主机」与「引导时匹配的主机」判据一致。
func (s *Store) FindHostByIdentity(ctx context.Context, sn, mac, uuid string) (model.Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, h := range s.hosts {
		if identityMatches(sn, mac, uuid, h.SN, h.MAC, h.UUID) {
			return copyHost(h), nil
		}
	}
	return model.Host{}, contract.ErrNotFound
}

// ListHosts 列出全部已发现主机。文本过滤由调用方在 Go 里做（本接口不接受
// 过滤参数）。
func (s *Store) ListHosts(ctx context.Context) ([]model.Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Host, 0, len(s.hosts))
	for _, h := range s.hosts {
		out = append(out, copyHost(h))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// ArtifactSetStore
// ---------------------------------------------------------------------------

// CreateArtifactSet 登记一个新制品集。
func (s *Store) CreateArtifactSet(ctx context.Context, set model.ArtifactSet) (model.ArtifactSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.artifact[set.ID] = copyArtifactSet(set)
	return copyArtifactSet(set), nil
}

// GetArtifactSet 按 id 查制品集。查不到返回 contract.ErrNotFound。
func (s *Store) GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a, ok := s.artifact[id]
	if !ok {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	return copyArtifactSet(a), nil
}

// ListArtifactSets 列出全部制品集。
func (s *Store) ListArtifactSets(ctx context.Context) ([]model.ArtifactSet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.ArtifactSet, 0, len(s.artifact))
	for _, a := range s.artifact {
		out = append(out, copyArtifactSet(a))
	}
	return out, nil
}

// UpdateArtifactSetState 只改 State、ManifestSHA256 与 FailReason，不触及
// Paths 与 OSFamily——导入失败路径调用本方法，失败时 Paths 可能尚未写入或
// 只写了一半，绝不能被本方法覆盖成半成品（计划任务 10 设计要点）。
//
// reason 是本次契约变更新增的第四个参数（见 contract.ArtifactSetStore 的
// 文档）：导入失败时把具体原因一并写入，供运维在控制台看到「为什么失败」
// 而不只是一个没有信息量的 "failed"。
func (s *Store) UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256, reason string) (model.ArtifactSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.artifact[id]
	if !ok {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	a.State = state
	a.ManifestSHA256 = manifestSHA256
	a.FailReason = reason
	s.artifact[id] = a
	return copyArtifactSet(a), nil
}

// SetArtifactSetContent 写回导入完成后才知道的内容：OSFamily 识别结果与
// Paths。只改这两项，不触及 State/ManifestSHA256。
func (s *Store) SetArtifactSetContent(ctx context.Context, id string, osFamily model.OSFamily, paths model.ArtifactPaths) (model.ArtifactSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.artifact[id]
	if !ok {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	a.OSFamily = osFamily
	a.Paths = copyArtifactPaths(paths)
	s.artifact[id] = a
	return copyArtifactSet(a), nil
}

// DeleteArtifactSet 删除制品集。若仍被某个 active 状态的 template 引用，
// 返回 contract.ErrStillReferenced（§2.7：只能 deprecate，不能删）。
func (s *Store) DeleteArtifactSet(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.artifact[id]; !ok {
		return contract.ErrNotFound
	}
	for _, tpl := range s.templates {
		if tpl.State == model.TemplateStateActive && tpl.ArtifactSetID == id {
			return contract.ErrStillReferenced
		}
	}
	delete(s.artifact, id)
	return nil
}

// ---------------------------------------------------------------------------
// HookStore
// ---------------------------------------------------------------------------

// CreateHook 创建一个新 hook 版本，同时维护 (id, version) 主索引与 digest
// 反查索引。
func (s *Store) CreateHook(ctx context.Context, hook model.Hook) (model.Hook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.hooks[hookKey(hook.ID, hook.Version)] = copyHook(hook)
	if hook.BodySHA256 != "" {
		s.hooksByDigest[hook.BodySHA256] = copyHook(hook)
	}
	return copyHook(hook), nil
}

// GetHook 按 (id, version) 查 hook。version 非正数表示取该 id 下最新版本。
// 查不到返回 contract.ErrNotFound。
func (s *Store) GetHook(ctx context.Context, id string, version int) (model.Hook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if version > 0 {
		h, ok := s.hooks[hookKey(id, version)]
		if !ok {
			return model.Hook{}, contract.ErrNotFound
		}
		return copyHook(h), nil
	}

	// 非正数：取该 id 下最新（Version 最大）的版本。
	var latest model.Hook
	found := false
	for _, h := range s.hooks {
		if h.ID != id {
			continue
		}
		if !found || h.Version > latest.Version {
			latest = h
			found = true
		}
	}
	if !found {
		return model.Hook{}, contract.ErrNotFound
	}
	return copyHook(latest), nil
}

// GetHookByDigest 按内容寻址 digest 反查 hook 正文。查不到返回
// contract.ErrNotFound。
func (s *Store) GetHookByDigest(ctx context.Context, digest string) (model.Hook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	h, ok := s.hooksByDigest[digest]
	if !ok {
		return model.Hook{}, contract.ErrNotFound
	}
	return copyHook(h), nil
}

// ListHooks 列出全部 hook（不区分版本，返回各 id 的全部版本）。
func (s *Store) ListHooks(ctx context.Context) ([]model.Hook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Hook, 0, len(s.hooks))
	for _, h := range s.hooks {
		out = append(out, copyHook(h))
	}
	return out, nil
}

// UpdateHookState 更新 hook 的审批状态。正文不可变——本方法不接受修改
// Body，因此不必更新 hooksByDigest 索引（digest 由 Body 算出，Body 没变
// digest 也不会变）。
func (s *Store) UpdateHookState(ctx context.Context, id string, version int, state model.HookState, approver string, approvedAt int64) (model.Hook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := hookKey(id, version)
	h, ok := s.hooks[key]
	if !ok {
		return model.Hook{}, contract.ErrNotFound
	}
	h.State = state
	h.Approver = approver
	h.ApprovedAt = approvedAt
	s.hooks[key] = h
	if h.BodySHA256 != "" {
		s.hooksByDigest[h.BodySHA256] = h
	}
	return copyHook(h), nil
}

// ---------------------------------------------------------------------------
// TemplateStore
// ---------------------------------------------------------------------------

// CreateTemplate 创建一个新模板版本。
func (s *Store) CreateTemplate(ctx context.Context, tpl model.Template) (model.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.templates[templateKey(tpl.ID, tpl.Version)] = copyTemplate(tpl)
	return copyTemplate(tpl), nil
}

// GetTemplate 按 (id, version) 查模板。version 非正数表示取该 id 下最新
// 版本。查不到返回 contract.ErrNotFound。
func (s *Store) GetTemplate(ctx context.Context, id string, version int) (model.Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if version > 0 {
		tpl, ok := s.templates[templateKey(id, version)]
		if !ok {
			return model.Template{}, contract.ErrNotFound
		}
		return copyTemplate(tpl), nil
	}

	var latest model.Template
	found := false
	for _, tpl := range s.templates {
		if tpl.ID != id {
			continue
		}
		if !found || tpl.Version > latest.Version {
			latest = tpl
			found = true
		}
	}
	if !found {
		return model.Template{}, contract.ErrNotFound
	}
	return copyTemplate(latest), nil
}

// ListTemplates 列出全部模板（不区分版本）。
func (s *Store) ListTemplates(ctx context.Context) ([]model.Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Template, 0, len(s.templates))
	for _, tpl := range s.templates {
		out = append(out, copyTemplate(tpl))
	}
	return out, nil
}

// UpdateTemplateState 更新模板状态。
func (s *Store) UpdateTemplateState(ctx context.Context, id string, version int, state model.TemplateState) (model.Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := templateKey(id, version)
	tpl, ok := s.templates[key]
	if !ok {
		return model.Template{}, contract.ErrNotFound
	}
	tpl.State = state
	s.templates[key] = tpl
	return copyTemplate(tpl), nil
}

// DeleteTemplate 删除模板。若存在引用该模板的非终态 task，返回
// contract.ErrStillReferenced（§2.7）。
func (s *Store) DeleteTemplate(ctx context.Context, id string, version int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := templateKey(id, version)
	if _, ok := s.templates[key]; !ok {
		return contract.ErrNotFound
	}
	for _, tk := range s.tasks {
		if tk.State.IsTerminal() {
			continue
		}
		if tk.TemplateID == id && tk.TemplateVersion == version {
			return contract.ErrStillReferenced
		}
	}
	delete(s.templates, key)
	return nil
}

// ---------------------------------------------------------------------------
// EventStore
// ---------------------------------------------------------------------------

// AppendEvent 追加一条事件。
//
// event.Seq 为正数时代表信号来源事件：(task_id, seq) 已存在时返回
// contract.ErrDuplicateSeq——这不是错误路径，是机器侧重试造成的正常重复
// 投递。
//
// event.Seq 非正数（零或负）表示非信号来源的事件：由本方法分配一个不参与
// 去重的自增负数序号（计划任务 17 设计要点：非信号事件用递减的负数序号，
// 与信号的正序号天然不冲突），调用方传入的 Seq 值在此情况下被忽略——这一
// 分支必须走一条真正不查重的写路径，不能为了复用代码把它也过一遍
// (task_id, seq) 唯一性检查（contract 作者的下游说明明确要求：否则扫描
// 循环连续两次调用同一个负数 seq 会互相冲突，而两次调用传入的 Seq 本来就
// 都是占位值，不代表任何真实序号）。
func (s *Store) AppendEvent(ctx context.Context, event model.TaskEvent) (model.TaskEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if event.Seq > 0 {
		for _, existing := range s.events[event.TaskID] {
			if existing.Seq == event.Seq {
				return model.TaskEvent{}, contract.ErrDuplicateSeq
			}
		}
		s.events[event.TaskID] = append(s.events[event.TaskID], copyTaskEvent(event))
		return copyTaskEvent(event), nil
	}

	// 非信号事件：分配一个不参与去重、递减的负数序号，不检查是否已存在
	// 相同 Seq——传入的 Seq 是占位值，真正的序号由这里分配。
	s.nextNonSignalSeq[event.TaskID]--
	assigned := event
	assigned.Seq = s.nextNonSignalSeq[event.TaskID]
	s.events[event.TaskID] = append(s.events[event.TaskID], copyTaskEvent(assigned))
	return copyTaskEvent(assigned), nil
}

// ListEvents 按任务与过滤条件列出事件，按 Seq 升序返回。
//
// 桶内元素按写入顺序追加，但写入顺序不等于 Seq 升序——非信号事件的负数
// Seq 与信号的正数 Seq 可能交替写入，因此这里必须显式按 Seq 排序，不能
// 依赖切片本身的顺序。
func (s *Store) ListEvents(ctx context.Context, taskID string, filter contract.EventFilter) ([]model.TaskEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bucket := s.events[taskID]
	out := make([]model.TaskEvent, 0, len(bucket))
	for _, e := range bucket {
		if filter.AfterSeq != 0 && e.Seq <= filter.AfterSeq {
			continue
		}
		out = append(out, copyTaskEvent(e))
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Seq > out[j].Seq; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// FactStore
// ---------------------------------------------------------------------------

// MergeFacts 把给定键值对合并写入该任务的 fact 集合：已存在的键被覆盖，
// 不存在的键被新增，未出现在本次调用中的既有键保持不变（§5.5）。
func (s *Store) MergeFacts(ctx context.Context, taskID string, facts map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.facts[taskID]
	if existing == nil {
		existing = make(map[string]string, len(facts))
	}
	for k, v := range facts {
		existing[k] = v
	}
	s.facts[taskID] = existing
	return nil
}

// GetFacts 返回该任务当前的全部 facts（早先写入的合并结果）。
func (s *Store) GetFacts(ctx context.Context, taskID string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return copyStringMap(s.facts[taskID]), nil
}

// ---------------------------------------------------------------------------
// AuditStore
// ---------------------------------------------------------------------------

// AppendAudit 追加一条审计记录（append-only）。
func (s *Store) AppendAudit(ctx context.Context, record contract.AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.audit = append(s.audit, record)
	return nil
}

// ListAudit 按过滤条件列出审计记录，按 TS 倒序返回。查不到匹配记录时
// 返回空切片而非错误（contract.AuditStore.ListAudit 文档明确要求：没有
// 审计记录是正常状态）。
func (s *Store) ListAudit(ctx context.Context, filter contract.AuditFilter) ([]contract.AuditRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 反向遍历 s.audit（从最后写入的开始）：s.audit 本身按写入顺序追加，
	// 反向遍历后 out 里的相对顺序就是「后写入的排前面」。这一步是让下面
	// 的稳定排序在同一时刻多条记录之间也能正确倒转顺序的前提——见下方
	// 排序那行的说明。
	out := make([]contract.AuditRecord, 0, len(s.audit))
	for i := len(s.audit) - 1; i >= 0; i-- {
		rec := s.audit[i]
		if filter.Target != "" && rec.Target != filter.Target {
			continue
		}
		if filter.SinceTS != 0 && rec.TS < filter.SinceTS {
			continue
		}
		if filter.UntilTS != 0 && rec.TS > filter.UntilTS {
			continue
		}
		out = append(out, rec)
	}

	// 按 TS 倒序：最新的在前（§9.4 的审计用途是事后追责）。out 此时已经是
	// 反向写入顺序（见上面的反向遍历），sort.SliceStable 只按 TS 比较、
	// 对相等的元素保持 out 当前的相对顺序不变——两者合起来，同一时刻多条
	// 记录之间的顺序就是「后写入的排前面」，与 SQLite 实现的
	// "ORDER BY ts DESC, id DESC" 一致（sqlite/audit.go：id 自增，DESC
	// 即后写入的行排前面）。若改成正向遍历 s.audit 再排序，稳定排序会让
	// 同一时刻的记录保持「先写入的排前面」，与 SQLite 的行为相反——这正是
	// 本函数曾经的实现方式，直到一条覆盖同秒多条记录的测试才把它证伪。
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS > out[j].TS })

	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// TxRunner：内存实现里退化为直接执行，但仍拒绝嵌套调用
// ---------------------------------------------------------------------------

// RunInTx 在内存实现里退化为直接执行 fn（单进程单锁，没有中途失败需要
// 回滚的持久化）。仍必须拒绝嵌套调用（fn 内部再次调用 RunInTx）——真实
// SQLite 实现下嵌套会在单写者模型上死锁，内存实现必须表现出同样的拒绝
// 行为，否则依赖它写测试的模块换成 SQLite 后会观察到不同的行为
// （contract.TxRunner 文档明确要求）。
func (s *Store) RunInTx(ctx context.Context, fn func(ctx context.Context, store contract.Store) error) error {
	s.txMu.Lock()
	if s.inTx {
		s.txMu.Unlock()
		return errNestedTx
	}
	s.inTx = true
	s.txMu.Unlock()

	defer func() {
		s.txMu.Lock()
		s.inTx = false
		s.txMu.Unlock()
	}()

	return fn(ctx, s)
}
