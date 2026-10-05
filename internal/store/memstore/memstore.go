// Package memstore 是仓储的内存实现：契约测试的参照实现，第 5 层的 HTTP 测试也用它。
//
// 一把锁让所有操作串行。写操作先在暂存的副本上改，全部成功才把副本换上去，中途出错就丢掉副本：一次写要么全做、
// 要么全不做，审计写失败时动作也一起回滚（spec §6.2 第 8 条）。表里的记录存进去之后不再原地修改，改一条记录就是
// 放进一个新的值，所以副本只要浅拷贝各张表；从调用方收下的、交给调用方的，都是深拷贝。
package memstore

import (
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
	hosts    map[string]model.Host            // 键是 SN
	isos     map[string]model.ISO             // 键是 ID
	hooks    map[string]ident                 // hook 的身份，键是 ID
	hookVers map[model.HookKey]model.Hook     // hook 的版本；名字只在身份上，这里的 Name 为空
	tpls     map[string]ident                 // 模板的身份，键是 ID
	tplVers  map[model.TemplateKey]tplVersion // 模板的版本，连同它引用的 hook 版本
	tasks    map[string]model.Task            // 键是 ID
	// events 的键是任务 ID，值按 Seq 升序，Seq 就是下标加 1。只追加：追加时另起一块，不写进已提交的那块（appendEvents）。
	events map[string][]store.Event
	audits []store.AuditEntry // 只追加，按 ID 升序；ID 就是下标加 1
}

// New 返回一个空的内存仓储。
func New() *Store {
	return &Store{d: &data{
		hosts: map[string]model.Host{}, isos: map[string]model.ISO{},
		hooks: map[string]ident{}, hookVers: map[model.HookKey]model.Hook{},
		tpls: map[string]ident{}, tplVers: map[model.TemplateKey]tplVersion{},
		tasks: map[string]model.Task{}, events: map[string][]store.Event{},
	}}
}

// Close 见 store.Store。内存仓储没有要释放的资源。
func (s *Store) Close() error { return nil }

// clone 是写操作的暂存：各张表浅拷贝一份。审计截掉多余的容量，暂存里追加时就另起一块，不会写进已提交的那块。
func (d *data) clone() *data {
	return &data{
		hosts: maps.Clone(d.hosts), isos: maps.Clone(d.isos),
		hooks: maps.Clone(d.hooks), hookVers: maps.Clone(d.hookVers),
		tpls: maps.Clone(d.tpls), tplVers: maps.Clone(d.tplVers),
		tasks: maps.Clone(d.tasks), events: maps.Clone(d.events),
		audits: slices.Clip(d.audits),
	}
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

// 游标由 store.EncodeCursor 编出、store.DecodeCursor 解开，与 pgstore 用同一份：排序键是上一页最后一条的。

// errorf 返回属于 kind 这一类的错误，写明是怎么回事；调用方用 errors.Is 判断类别。
func errorf(kind error, format string, args ...any) error {
	return fmt.Errorf("%w：%s", kind, fmt.Sprintf(format, args...))
}

// staleRev 是调用方带回的 Rev 与现在的对不上：记录在它读过之后改过了。
func staleRev(what string, have, got int64) error {
	return errorf(store.ErrConflict, "%s 现在的 Rev 是 %d，收到的是 %d：重读之后再判断", what, have, got)
}
