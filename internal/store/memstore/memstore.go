// Package memstore 是仓储的内存实现：契约测试的参照实现，第 5 层的 HTTP 测试也用它。
//
// 一把锁让所有操作串行。写操作先在暂存的副本上改，全部成功才把副本换上去，中途出错就丢掉副本：一次写要么全做、
// 要么全不做，审计写失败时动作也一起回滚（spec §6.2 第 8 条）。表里的记录存进去之后不再原地修改，改一条记录就是
// 放进一个新的值，所以副本只要浅拷贝各张表；从调用方收下的、交给调用方的，都是深拷贝。
package memstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// Store 是内存仓储，用 New 构造，可以被多个 goroutine 同时使用。
type Store struct {
	mu sync.Mutex
	d  *data // 已提交的记录
	// fault 非空时，Actor 等于它的审计写入失败（storetest.Harness 的失败注入）。只有测试经 export_test.go 设它，
	// New 构造的仓储没有这条路。
	fault string
}

var _ store.Store = (*Store)(nil)

// data 是仓储的全部记录。
type data struct {
	hosts  map[string]model.Host // 键是 SN
	isos   map[string]model.ISO  // 键是 ID
	audits []store.AuditEntry    // 只追加，按 ID 升序；ID 就是下标加 1
}

// New 返回一个空的内存仓储。
func New() *Store {
	return &Store{d: &data{hosts: map[string]model.Host{}, isos: map[string]model.ISO{}}}
}

// Close 见 store.Store。内存仓储没有要释放的资源。
func (s *Store) Close() error { return nil }

// clone 是写操作的暂存：各张表浅拷贝一份。审计截掉多余的容量，暂存里追加时就另起一块，不会写进已提交的那块。
func (d *data) clone() *data {
	return &data{hosts: maps.Clone(d.hosts), isos: maps.Clone(d.isos), audits: slices.Clip(d.audits)}
}

// tx 是一次写操作的暂存。
type tx struct {
	*data
	fault string
}

// write 在暂存的副本上执行 fn：fn 返回 nil 才把副本换上去，否则丢掉，什么都不改。
func (s *Store) write(fn func(t *tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &tx{data: s.d.clone(), fault: s.fault}
	if err := fn(t); err != nil {
		return err
	}
	s.d = t.data
	return nil
}

// errFault 是注入的审计写入失败。它不属于契约的任何一类错误：调用方看到的是一次普通的写入失败。
var errFault = errors.New("memstore：注入的审计写入失败")

// addAudit 在暂存里给对象 kind、id 写一条审计。调用方先写动作本身、后调用它，注入的失败因此落在动作写完之后，
// 整次写入随之丢掉。
func (t *tx) addAudit(a store.Audit, kind, id string) error {
	if t.fault != "" && a.Actor == t.fault {
		return errFault
	}
	t.audits = append(t.audits, store.AuditEntry{ID: int64(len(t.audits)) + 1, Audit: a, TargetKind: kind, TargetID: id})
	return nil
}

// page 从排好序、已跳过游标之前的 items 里取一页，条目用 clone 深拷贝。多看一条才知道还有没有下一页：恰好取完时
// Next 为空，不多给一个空页。Items 不为 nil，第 5 层编成 JSON 是 []。
func page[T any](items []T, limit int, clone func(T) T, cursorOf func(T) string) store.Page[T] {
	var p store.Page[T]
	if len(items) > limit {
		items = items[:limit]
		p.Next = cursorOf(items[limit-1])
	}
	p.Items = make([]T, len(items))
	for i, it := range items {
		p.Items[i] = clone(it)
	}
	return p
}

// 游标是上一页最后一条的排序键，连同列表的名字编成 JSON 数组，再做 base64url：调用方看不出里面是什么，也就不会
// 依赖它；改坏的、别的列表给的游标都认得出来，报 ErrInvalid。

// encodeCursor 把列表 list 的排序键 key 编成游标。
func encodeCursor(list string, key ...any) string {
	b, _ := json.Marshal(append([]any{list}, key...)) // 排序键只有字符串与整数，编码不会失败
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor 把 encodeCursor 给出的游标解回排序键，key 是各项的指针。
func decodeCursor(cursor, list string, key ...any) error {
	bad := errorf(store.ErrInvalid, "游标 %q 不是这个列表上一页给出的 Next", cursor)
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
		if json.Unmarshal(parts[i+1], k) != nil {
			return bad
		}
	}
	return nil
}

// errorf 返回属于 kind 这一类的错误，写明是怎么回事；调用方用 errors.Is 判断类别。
func errorf(kind error, format string, args ...any) error {
	return fmt.Errorf("%w：%s", kind, fmt.Sprintf(format, args...))
}

// badID 是收到的 ID 不合 model.ValidID。
func badID(id string) error {
	return errorf(store.ErrInvalid, "ID %q 不合格式", id)
}

// staleRev 是调用方带回的 Rev 与现在的对不上：记录在它读过之后改过了。
func staleRev(what string, have, got int64) error {
	return errorf(store.ErrConflict, "%s 现在的 Rev 是 %d，收到的是 %d：重读之后再判断", what, have, got)
}

// 下面是还没做的方法，返回 errNotImplemented：hook 与模板由任务 5 实现，任务与事件由任务 6 实现，届时连同这一节
// 一起删掉。

var errNotImplemented = errors.New("memstore：尚未实现")

func (s *Store) CreateHook(context.Context, model.Hook) (model.Hook, error) {
	return model.Hook{}, errNotImplemented
}

func (s *Store) SaveHookDraft(context.Context, model.Hook) (model.Hook, error) {
	return model.Hook{}, errNotImplemented
}

func (s *Store) NewHookVersion(context.Context, string, int, string) (model.Hook, error) {
	return model.Hook{}, errNotImplemented
}

func (s *Store) SubmitHook(context.Context, model.HookKey, int64, store.Audit) error {
	return errNotImplemented
}

func (s *Store) ApproveHook(context.Context, model.HookKey, int64, string, store.Audit) error {
	return errNotImplemented
}

func (s *Store) RejectHook(context.Context, model.HookKey, int64, store.Audit) error {
	return errNotImplemented
}

func (s *Store) DeprecateHook(context.Context, model.HookKey, int64, store.Audit) error {
	return errNotImplemented
}

func (s *Store) RenameHook(context.Context, string, string, store.Audit) error {
	return errNotImplemented
}

func (s *Store) DeleteHookDraft(context.Context, model.HookKey, int64) error {
	return errNotImplemented
}

func (s *Store) GetHook(context.Context, model.HookKey) (model.Hook, error) {
	return model.Hook{}, errNotImplemented
}

func (s *Store) GetHooks(context.Context, []model.HookKey) (map[model.HookKey]model.Hook, error) {
	return nil, errNotImplemented
}

func (s *Store) ListHookVersions(context.Context, string) ([]model.Hook, error) {
	return nil, errNotImplemented
}

func (s *Store) ListHooks(context.Context, store.HookQuery) (store.Page[model.Hook], error) {
	return store.Page[model.Hook]{}, errNotImplemented
}

func (s *Store) EnsureBuiltinHook(context.Context, model.Hook) error {
	return errNotImplemented
}

func (s *Store) CreateTemplate(context.Context, model.Template) (model.Template, error) {
	return model.Template{}, errNotImplemented
}

func (s *Store) SaveTemplateDraft(context.Context, model.Template) (model.Template, error) {
	return model.Template{}, errNotImplemented
}

func (s *Store) NewTemplateVersion(context.Context, string, int, string) (model.Template, error) {
	return model.Template{}, errNotImplemented
}

func (s *Store) DeriveTemplate(context.Context, model.TemplateKey, string, string) (model.Template, error) {
	return model.Template{}, errNotImplemented
}

func (s *Store) SubmitTemplate(context.Context, model.TemplateKey, int64, store.Audit) error {
	return errNotImplemented
}

func (s *Store) ApproveTemplate(context.Context, model.TemplateKey, int64, string, store.Audit) error {
	return errNotImplemented
}

func (s *Store) RejectTemplate(context.Context, model.TemplateKey, int64, store.Audit) error {
	return errNotImplemented
}

func (s *Store) DeprecateTemplate(context.Context, model.TemplateKey, int64, store.Audit) error {
	return errNotImplemented
}

func (s *Store) RenameTemplate(context.Context, string, string, store.Audit) error {
	return errNotImplemented
}

func (s *Store) DeleteTemplateDraft(context.Context, model.TemplateKey, int64) error {
	return errNotImplemented
}

func (s *Store) GetTemplate(context.Context, model.TemplateKey) (model.Template, error) {
	return model.Template{}, errNotImplemented
}

func (s *Store) ListTemplateVersions(context.Context, string) ([]model.Template, error) {
	return nil, errNotImplemented
}

func (s *Store) ListTemplates(context.Context, store.TemplateQuery) (store.Page[model.Template], error) {
	return store.Page[model.Template]{}, errNotImplemented
}

func (s *Store) EnsureBuiltinTemplate(context.Context, model.Template) error {
	return errNotImplemented
}

func (s *Store) CreateTasks(context.Context, []model.Task, store.Audit) ([]model.Task, error) {
	return nil, errNotImplemented
}

func (s *Store) GetTask(context.Context, string) (model.Task, error) {
	return model.Task{}, errNotImplemented
}

func (s *Store) FindOpenTask(context.Context, string) (model.Task, bool, error) {
	return model.Task{}, false, errNotImplemented
}

func (s *Store) ListTasks(context.Context, store.TaskQuery) (store.Page[model.Task], error) {
	return store.Page[model.Task]{}, errNotImplemented
}

func (s *Store) TransitionTask(context.Context, string, store.TaskExpect, model.TaskState, []store.NewEvent) (model.Task, []store.Appended, error) {
	return model.Task{}, nil, errNotImplemented
}

func (s *Store) TransitionTaskByUser(context.Context, string, store.TaskExpect, model.TaskState, []store.NewEvent, store.Audit) (model.Task, []store.Appended, error) {
	return model.Task{}, nil, errNotImplemented
}

func (s *Store) AppendEvents(context.Context, string, []store.NewEvent) ([]store.Appended, error) {
	return nil, errNotImplemented
}

func (s *Store) ListEvents(context.Context, string, int64, int) ([]store.Event, error) {
	return nil, errNotImplemented
}
