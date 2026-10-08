package store

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/core/activation"
	"github.com/lober/lober/internal/core/admission"
	"github.com/lober/lober/internal/core/model"
)

// 实现调用的纯函数：入参校验、生成 ID、游标的编解码、提取模板的 hook 引用，以及在事务里重跑第 1 层判定的封装。
// 主机与 ISO 的检查在 check_registry.go，hook 与模板的在 check_authoring.go。

// 入参的上限，单位都是字节。
const (
	actorMaxBytes  = 128
	actionMaxBytes = 64 // Audit.Action 与 NewEvent.Kind
	detailMaxBytes = 4096
	fieldsMaxBytes = 64 << 10 // 一条事件全部字段的键与值合计
)

// 分页的条数：Limit 为 0 时取 defaultLimit，最多 maxLimit。
const (
	defaultLimit = 100
	maxLimit     = 500
)

// actionPattern 是 Audit.Action 与 NewEvent.Kind 的写法：点分的两段以上，如 host.import、slot.exit。
var actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// idEncoding 是 RFC 4648 的 base32 换成小写字母，不带填充。
var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewID 生成一个 ID：128 位随机数，编成 26 个字符的小写 base32。它满足 model.ValidID 与 model.IsSafeWord：
// 任务 ID 经 {{sys:task}} 原样代入正文（L-13b）。它不含 -，所以不会和 builtin-kickstart 这类内置内容的固定 ID
// 撞上。
func NewID() string {
	var b [16]byte
	// Go 1.24 起 crypto/rand.Read 不会返回错误：取不到随机数时进程直接崩溃，不会给出可预测的 ID。
	rand.Read(b[:])
	return idEncoding.EncodeToString(b[:])
}

// CheckAudit 查要审计的方法收到的审计：零值报 ErrAuditRequired，表示调用方漏了审计；只要有一个字段不是零值，
// 就按 Audit 各字段的规则查，不合规则报 ErrInvalid。实现在写任何东西之前调用它。
func CheckAudit(a Audit) error {
	if a == (Audit{}) {
		return ErrAuditRequired
	}
	switch {
	case a.Actor == "" || len(a.Actor) > actorMaxBytes || !model.StorableText(a.Actor):
		return invalidf("审计的 Actor 要是 1–%d 字节的可存文本", actorMaxBytes)
	case !validAction(a.Action):
		return invalidf("审计的 Action %q 不合写法：要点分的两段以上，每段以小写字母开头，只含小写字母、数字和"+
			"下划线，不超过 %d 字节", a.Action, actionMaxBytes)
	case len(a.Detail) > detailMaxBytes || !model.StorableText(a.Detail):
		return invalidf("审计的 Detail 要是不超过 %d 字节的可存文本", detailMaxBytes)
	case a.At <= 0:
		return invalidf("审计的 At 要大于 0：Unix 秒，由调用方给")
	}
	return nil
}

// CheckEvents 查要追加的事件，规则见 NewEvent。有一条不合规则就报 ErrInvalid，整批都不写。空的一批合规则：
// 不带事件的推进也调它。
//
// 同一批里 Key 重复不算不合规则，后一条按「Key 已存在」处理（AppendEvents）：机器重发的一批信号里有重复，
// 不能因此把整批都拒掉，否则重发多少次都进不来。
func CheckEvents(es []NewEvent) error {
	for i, e := range es {
		if msg := eventProblem(e); msg != "" {
			return invalidf("第 %d 条事件%s", i+1, msg)
		}
	}
	return nil
}

// eventProblem 说明一条事件哪里不合规则，合规则时返回空串。字段按键的字典序查，报出的总是同一处。
func eventProblem(e NewEvent) string {
	switch {
	case e.At <= 0:
		return "的 At 要大于 0：Unix 秒，由调用方给"
	case !validAction(e.Kind):
		return fmt.Sprintf("的 Kind %q 不合写法，规则同审计的 Action", e.Kind)
	case e.Key != "" && !model.IsSafeWord(e.Key):
		return "的 Key 要么为空，要么是 1–128 字节的安全词"
	}
	size := 0
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		switch {
		case !model.ValidIdent(k):
			return fmt.Sprintf("的字段名 %q 不是标识符", k)
		case !model.StorableText(e.Fields[k]):
			return fmt.Sprintf("的字段 %s 存不下：不是合法的 UTF-8，或含 NUL", k)
		}
		size += len(k) + len(e.Fields[k])
	}
	if size > fieldsMaxBytes {
		return fmt.Sprintf("的字段合计 %d 字节，超过了 %d 字节", size, fieldsMaxBytes)
	}
	return ""
}

// CheckEventQuery 查 ListEvents 的入参，返回实际要取的条数：任务 ID 合格式（CheckID），limit 的规则同 CheckQuery，
// afterSeq 不能为负。不合的报 ErrInvalid。
func CheckEventQuery(taskID string, afterSeq int64, limit int) (int, error) {
	if err := CheckID(taskID); err != nil {
		return 0, err
	}
	n, err := CheckQuery("", limit)
	if err != nil {
		return 0, err
	}
	if afterSeq < 0 {
		return 0, invalidf("afterSeq 不能为负，收到 %d", afterSeq)
	}
	return n, nil
}

// CheckTransition 查任务推进的入参（TransitionTask、TransitionTaskByUser）：任务 ID 合格式（CheckID），目标状态是 model
// 认得的（TaskState.Valid），带的事件合规则（CheckEvents）。不合的报 ErrInvalid，整个推进不生效。一步推进合不合规矩
// 不在这里查，那是第 7 层状态机的事。
func CheckTransition(id string, to model.TaskState, events []NewEvent) error {
	if err := CheckID(id); err != nil {
		return err
	}
	if !to.Valid() {
		return invalidf("任务的目标状态 %q 不认识", to)
	}
	return CheckEvents(events)
}

// CheckQuery 查分页的入参，返回实际要取的条数：limit 为 0 时取 100，1–500 照用，负数或超过 500 报 ErrInvalid。
//
// cursor 是上一页的 Next，空串表示从头取。非空的游标由实现用 DecodeCursor 解开，认不出的报 ErrInvalid；这里只挡掉
// 存不下的串（不是合法的 UTF-8，或含 NUL）：仓储给出的游标都存得下，存不下的连查询参数都当不了。
func CheckQuery(cursor string, limit int) (int, error) {
	switch {
	case !model.StorableText(cursor):
		return 0, invalidf("游标不是仓储给出的：不是合法的 UTF-8，或含 NUL")
	case limit == 0:
		return defaultLimit, nil
	case limit < 0 || limit > maxLimit:
		return 0, invalidf("Limit 要在 0–%d 之间，收到 %d", maxLimit, limit)
	}
	return limit, nil
}

// 游标是上一页最后一条的排序键，连同列表的名字编成 JSON 数组，再做 base64url：调用方看不出里面是什么，也就不会依赖它；
// 改坏的、别的列表给的游标都认得出来，报 ErrInvalid。各列表的排序键见各查询类型的说明，编解码只有这一份，认得什么样的
// 游标不随实现里查询的写法变。

// EncodeCursor 把列表 list 的排序键 key 编成游标。排序键只有字符串与整数。
func EncodeCursor(list string, key ...any) string {
	b, _ := json.Marshal(append([]any{list}, key...)) // 字符串与整数的编码不会失败
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor 把 EncodeCursor 给出的游标解回排序键，key 是各项的指针，个数与类型都要与编的时候相同，否则报 ErrInvalid。
// 解出的串要存得下：仓储给出的游标都是，藏着 NUL 的只能是伪造的，拿去查询还会让 PostgreSQL 报编码错误。排序键为 null
// 的同样只能是伪造的，也报 ErrInvalid：json.Unmarshal 遇到 null 既不报错也不改目标值，收下的话排序键停在初值上，审计与
// 任务列表翻出空的一页、Next 为空，调用方以为翻到头了，别的列表则悄悄从头翻起。
func DecodeCursor(cursor, list string, key ...any) error {
	bad := invalidf("游标 %q 不是这个列表上一页给出的 Next", cursor)
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return bad
	}
	var parts []json.RawMessage
	var name string
	if json.Unmarshal(b, &parts) != nil || len(parts) != len(key)+1 || json.Unmarshal(parts[0], &name) != nil || name != list {
		return bad
	}
	for i, k := range key {
		if string(bytes.TrimSpace(parts[i+1])) == "null" || json.Unmarshal(parts[i+1], k) != nil {
			return bad
		}
		if s, ok := k.(*string); ok && !model.StorableText(*s) {
			return bad
		}
	}
	return nil
}

// CheckID 查收到的 ISO、hook、模板或任务的 ID：过 model.ValidID，不合的报 ErrInvalid，不当成找不到。
func CheckID(id string) error {
	if !model.ValidID(id) {
		return invalidf("ID %q 不合格式", id)
	}
	return nil
}

// CheckVersionKey 查收到的 hook 或模板的版本键：ID 过 CheckID，版本号从 1 起。不合的报 ErrInvalid，不当成找不到。
func CheckVersionKey(id string, version int) error {
	if err := CheckID(id); err != nil {
		return err
	}
	if version < 1 {
		return invalidf("版本号要从 1 起，收到 %d", version)
	}
	return nil
}

// VersionID 是 hook 或模板的一个版本在审计对象、Subject 与报错里的写法：「ID@版本」，与第 1 层判定里的相同。
func VersionID(id string, version int) string {
	return id + "@" + strconv.Itoa(version)
}

// CheckSN 查按 SN 找任务时收到的 SN（FindOpenTask）：要是规范化之后的写法，与 model.NormalizeSN 的结果相同，否则报
// ErrInvalid。登记过的主机只有这种写法，拿别的写法去找，找不到也不说明这台主机没有任务。
func CheckSN(sn string) error {
	if !normalizedSN(sn) {
		return invalidf("SN %q 不是规范化之后的写法：调用方要先经 model.NormalizeSN", sn)
	}
	return nil
}

// CheckNewTasks 是批量建任务的第 1 步：查入参本身，不合格报 ErrInvalid。前提是：
//
//   - 一批至少一个任务。
//   - 每个任务的 ID、State、Rev 都是零值，由仓储填；CreatedAt 大于 0；CreatedBy 是非空的可存文本。
//   - Snapshot.Schema 等于 model.SnapshotSchema；HostSN、ISOID、TemplateID、TemplateVersion 与快照里的一致。
//   - 快照里的每个串都存得下（model.StorableText），嵌套的结构、切片与 map 里的也算，map 的键也算：pgstore 把快照存成
//     jsonb，jsonb 不收 NUL，encoding/json 还会把非法的 UTF-8 换成 U+FFFD，固化下来的就不是写进去的那份（C29）。
//   - ID 的格式对：HostSN 已经规范化（与 model.NormalizeSN 的结果相同），ISOID 与 TemplateID 过
//     model.ValidID，TemplateVersion 从 1 起。
//   - 同一批的 ISO 与模板版本都相同（spec §2.7）。
//   - 批里 SN 不重复。
//
// 前面几条报出第一处不满足的。SN 重复能用现有代码表达，报 ProblemsError(ErrInvalid)：每个重复的 SN 一条
// host_duplicate_in_batch，按它在批里第一次出现的顺序。主机登没登记、ISO 与模板能不能用，要在事务里对加锁
// 后的数据查，见 BatchProblems。
func CheckNewTasks(ts []model.Task) error {
	if len(ts) == 0 {
		return invalidf("一批至少要有一个任务")
	}
	for i, t := range ts {
		if msg := newTaskProblem(t, ts[0]); msg != "" {
			return invalidf("第 %d 个任务%s", i+1, msg)
		}
	}
	return duplicateSNs(ts)
}

// newTaskProblem 说明一个待建任务哪里不合前提，合前提时返回空串。first 是本批的第 1 个任务，同一批的 ISO
// 与模板版本都要和它相同；先查任务自己与快照一致，再和它比。
func newTaskProblem(t, first model.Task) string {
	s := t.Snapshot
	switch {
	case t.ID != "" || t.State != "" || t.Rev != 0:
		return "的 ID、State、Rev 要留空，由仓储填"
	case t.CreatedAt <= 0:
		return "的 CreatedAt 要大于 0：Unix 秒，由调用方给"
	case t.CreatedBy == "" || !model.StorableText(t.CreatedBy):
		return "的 CreatedBy 要是非空的可存文本"
	case s.Schema != model.SnapshotSchema:
		return fmt.Sprintf("的快照格式是 %d，应为 %d", s.Schema, model.SnapshotSchema)
	case t.HostSN != s.Host.SN || t.ISOID != s.ISO.ID || t.TemplateID != s.Template.ID || t.TemplateVersion != s.Template.Version:
		return "的 HostSN、ISOID、TemplateID、TemplateVersion 与快照里的不一致"
	case !storableValue(reflect.ValueOf(s)):
		return "的快照里有存不下的文本：不是合法的 UTF-8，或含 NUL"
	case !normalizedSN(t.HostSN):
		return "的 HostSN 不是规范化之后的 SN"
	case !model.ValidID(t.ISOID) || !model.ValidID(t.TemplateID) || t.TemplateVersion < 1:
		return "的 ISOID、TemplateID 或 TemplateVersion 不合 ID 的格式"
	case t.ISOID != first.ISOID || t.TemplateID != first.TemplateID || t.TemplateVersion != first.TemplateVersion:
		return "的 ISO 或模板版本与第 1 个任务的不同：同一批只用一个 ISO 和一个模板版本"
	}
	return ""
}

// storableValue 报告 v 里的每个串（包括 map 的键）都存得下（model.StorableText）。快照层层嵌套，逐层走一遍：model 以后
// 给它加了字段也照样查到。
func storableValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return model.StorableText(v.String())
	case reflect.Struct:
		for i := range v.NumField() {
			if !storableValue(v.Field(i)) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if !storableValue(v.Index(i)) {
				return false
			}
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			if !storableValue(it.Key()) || !storableValue(it.Value()) {
				return false
			}
		}
	case reflect.Pointer, reflect.Interface:
		return v.IsNil() || storableValue(v.Elem())
	}
	return true
}

// normalizedSN 报告 sn 是不是规范化之后的 SN：登记过的主机只有这种，别的写法永远对不上号。
func normalizedSN(sn string) bool {
	n, err := model.NormalizeSN(sn)
	return err == nil && n == sn
}

// duplicateSNs 为批里每个重复的 SN 报一条 host_duplicate_in_batch，没有重复时返回 nil。
func duplicateSNs(ts []model.Task) error {
	count := make(map[string]int, len(ts))
	for _, t := range ts {
		count[t.HostSN]++
	}
	var ps []model.Problem
	for _, t := range ts {
		if count[t.HostSN] > 1 {
			ps = append(ps, model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: "host", ID: t.HostSN}))
			count[t.HostSN] = 0 // 报过了，后面再遇到它不再报
		}
	}
	if len(ps) == 0 {
		return nil
	}
	return &ProblemsError{Kind: ErrInvalid, Problems: ps}
}

// HookRefs 列出模板装配表引用的 hook 版本，去重，按 ID、版本排序。保存模板的任何一个版本时，实现都用它
// 维护「这个版本引用了哪些 hook 版本」，删 hook 草稿、审批与建任务时都靠这份记录。装配表是 map，排过序，同一个
// 模板每次得到的结果才一样，加锁的顺序也就固定。草稿可以引用不存在的版本（激活时才报 hook_ref_missing），这里
// 照样列出。
func HookRefs(t model.Template) []model.HookKey {
	var refs []model.HookKey
	for _, insts := range t.Assembly {
		for _, in := range insts {
			refs = append(refs, model.HookKey{ID: in.HookID, Version: in.HookVersion})
		}
	}
	slices.SortFunc(refs, func(a, b model.HookKey) int {
		return cmp.Or(strings.Compare(a.ID, b.ID), cmp.Compare(a.Version, b.Version))
	})
	return slices.Compact(refs)
}

// ApproveHookProblems 是审批 hook 时在事务里的重跑：对加锁后读到的版本 h 跑 activation.Hook 与
// activation.Approver（C8–C11、C22）。没有问题时返回 nil，有问题时返回 ProblemsError(ErrRejected)，问题依次是
// 激活判定的和审批人的。Approver 把 Subject 留给调用方填，这里填成审批的那个版本。状态对不对（只有待审批的
// 能批）由调用方先查。
func ApproveHookProblems(h model.Hook, approver string) error {
	ps := activation.Hook(h)
	ps = append(ps, approverProblems(h.Author, approver, model.Subject{Kind: "hook", ID: VersionID(h.ID, h.Version)})...)
	return rejected(ps)
}

// ApproveTemplateProblems 同 ApproveHookProblems，判定的是模板版本 t：跑 activation.Template 与
// activation.Approver（C13–C17、C19、C22、C26）。hooks 是 t 引用的 hook 版本（HookRefs），由调用方在同一个事务里
// 加读锁后取出：引用的版本刚被废弃，就会报 hook_ref_deprecated；缺了的当作不存在，报 hook_ref_missing。
func ApproveTemplateProblems(t model.Template, hooks map[model.HookKey]model.Hook, approver string) error {
	ps := activation.Template(t, hooks)
	ps = append(ps, approverProblems(t.Author, approver, model.Subject{Kind: "template", ID: VersionID(t.ID, t.Version)})...)
	return rejected(ps)
}

// BatchProblems 是批量建任务的第 3 步：对加锁后读到的数据跑 admission.Batch。missing 是批里没登记的 SN（从没登记
// 过，或者在向导里选中之后被删了），准入判定的入参里没有它们、查不出来，这里在 ByHost 里各补一条
// host_not_registered，同一个 SN 只补一条。没有问题时返回 nil，有问题时返回 BatchError。
func BatchProblems(in admission.BatchInput, missing []string) error {
	res := admission.Batch(in)
	for _, sn := range missing {
		p := model.NewProblem("host_not_registered", model.Subject{Kind: "host", ID: sn})
		if !slices.Contains(res.ByHost[sn], p) {
			res.ByHost[sn] = append(res.ByHost[sn], p)
		}
	}
	if passed(res) {
		return nil
	}
	return &BatchError{Result: res}
}

// OpenTaskProblems 是推进到未结束的状态时，主机 sn 已有另一个未结束的任务（C31，比如失败的任务重试时，期间又建了新
// 任务）：返回 ProblemsError(ErrRejected)，问题与准入判定对这台主机报的相同（admission.HostSelectable）。
func OpenTaskProblems(sn string) error {
	return &ProblemsError{Kind: ErrRejected, Problems: admission.HostSelectable(model.Host{SN: sn}, true)}
}

// passed 报告一批的判定结果是否通过：Common 与 ByHost 里全都没有问题。
func passed(r admission.BatchResult) bool {
	if len(r.Common) > 0 {
		return false
	}
	for _, ps := range r.ByHost {
		if len(ps) > 0 {
			return false
		}
	}
	return true
}

// approverProblems 是 activation.Approver 的问题，Subject 填成 s。
func approverProblems(author, approver string, s model.Subject) []model.Problem {
	ps := activation.Approver(author, approver)
	for i := range ps {
		ps[i].Subject = s
	}
	return ps
}

// rejected 把在事务里重跑判定得到的问题包成 ProblemsError(ErrRejected)，没有问题时返回 nil。
func rejected(ps []model.Problem) error {
	if len(ps) == 0 {
		return nil
	}
	return &ProblemsError{Kind: ErrRejected, Problems: ps}
}

func validAction(s string) bool {
	return len(s) <= actionMaxBytes && actionPattern.MatchString(s)
}

// errorf 返回属于 kind 这一类的错误，写明是怎么回事；调用方用 errors.Is 判断类别。
func errorf(kind error, format string, args ...any) error {
	return fmt.Errorf("%w：%s", kind, fmt.Sprintf(format, args...))
}

// invalidf 返回包着 ErrInvalid 的错误，写明哪一条前提不满足。
func invalidf(format string, args ...any) error {
	return errorf(ErrInvalid, format, args...)
}
