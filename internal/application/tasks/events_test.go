package tasks

import (
	"context"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"reflect"
	"sync"
	"testing"
)

type eventProbe struct {
	calls         []string
	ctx           context.Context
	id            string
	after         int64
	limit         int
	rows          []repository.Event
	err, errorGet error
	hints         chan struct{}
	onSubscribe   func()
}

func (p *eventProbe) GetTask(ctx context.Context, id string) (model.Task, error) {
	p.calls = append(p.calls, "get")
	p.ctx = ctx
	p.id = id
	return model.Task{}, p.errorGet
}
func (p *eventProbe) ListEvents(ctx context.Context, id string, after int64, limit int) ([]repository.Event, error) {
	p.calls = append(p.calls, fmt.Sprint("list:", limit))
	p.ctx = ctx
	p.id = id
	p.after = after
	p.limit = limit
	return p.rows, p.err
}
func (p *eventProbe) Publish(string) {}
func (p *eventProbe) Subscribe(id string) (<-chan struct{}, func()) {
	p.calls = append(p.calls, "subscribe")
	p.id = id
	if p.onSubscribe != nil {
		p.onSubscribe()
	}
	return p.hints, func() { p.calls = append(p.calls, "close") }
}
func eventFixture() (*eventProbe, EventReader) {
	p := &eventProbe{hints: make(chan struct{}, 1)}
	return p, NewEventReader(EventDeps{Tasks: p, Events: p, Notifier: p})
}
func Test任务事件直接查询不额外查任务(t *testing.T) {
	p, r := eventFixture()
	p.err = repository.ErrInvalid
	_, err := r.List(t.Context(), operator, "raw", -1, 501)
	if !errors.Is(err, p.err) || !reflect.DeepEqual(p.calls, []string{"list:501"}) || p.id != "raw" || p.after != -1 {
		t.Fatal(err, p.calls, p.id, p.after)
	}
	p.calls = nil
	_, err = r.List(t.Context(), ports.Actor{}, "raw", -1, 501)
	if !errors.Is(err, ports.ErrUnauthenticated) || len(p.calls) != 0 {
		t.Fatal(err, p.calls)
	}
}
func Test事件订阅先订阅再首读且失败退订(t *testing.T) {
	p, r := eventFixture()
	p.err = errors.New("read")
	_, err := r.Open(t.Context(), operator, "task", 0)
	want := []string{"get", "subscribe", "list:200", "close"}
	if !errors.Is(err, p.err) || !reflect.DeepEqual(p.calls, want) {
		t.Fatal(err, p.calls)
	}
	p, r = eventFixture()
	p.onSubscribe = func() { p.rows = []repository.Event{{Seq: 1}}; p.hints <- struct{}{} }
	s, err := r.Open(t.Context(), operator, "task", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Initial()) != 1 {
		t.Fatal(s.Initial())
	}
	select {
	case <-s.Hints():
	default:
		t.Fatal("订阅与首读之间的提示丢失")
	}
}
func Test事件订阅保留字段副本与上下文(t *testing.T) {
	p, r := eventFixture()
	p.rows = []repository.Event{{TaskID: "task", Seq: 7, At: 3, Kind: "kind", Key: "key", Fields: map[string]string{"a": "one"}}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s, err := r.Open(ctx, operator, "task", 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.ctx != ctx || p.after != 2 || p.limit != 200 {
		t.Fatal(p)
	}
	a := s.Initial()
	a[0].Fields["a"] = "changed"
	if p.rows[0].Fields["a"] != "one" || s.Initial()[0].Fields["a"] != "one" {
		t.Fatal("共享字段")
	}
	a, err = s.Read(ctx, 7)
	if err != nil || p.ctx != ctx || p.after != 7 || p.limit != 200 || a[0].TaskID != "task" || a[0].Kind != "kind" || a[0].Key != "key" || a[0].At != 3 {
		t.Fatal(a, err, p)
	}
	a[0].Fields["a"] = "other"
	if p.rows[0].Fields["a"] != "one" {
		t.Fatal("后读共享字段")
	}
	p.rows = nil
	a, err = s.Read(ctx, 7)
	if err != nil || a == nil {
		t.Fatal(a, err)
	}
	p.err = errors.New("late")
	if _, err = s.Read(ctx, 7); !errors.Is(err, p.err) {
		t.Fatal(err)
	}
	s.Close()
	s.Close()
	if p.calls[len(p.calls)-1] != "close" {
		t.Fatal(p.calls)
	}
	count := 0
	for _, c := range p.calls {
		if c == "close" {
			count++
		}
	}
	if count != 1 {
		t.Fatal(p.calls)
	}
}
func Test事件订阅参数权限与任务错误顺序(t *testing.T) {
	p, r := eventFixture()
	if _, err := r.Open(t.Context(), ports.Actor{}, "id", -1); !errors.Is(err, ports.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err := r.Open(t.Context(), operator, "id", -1); !errors.Is(err, ErrBadRequest) {
		t.Fatal(err)
	}
	if len(p.calls) != 0 {
		t.Fatal(p.calls)
	}
	p.errorGet = repository.ErrNotFound
	if _, err := r.Open(t.Context(), operator, "id", 0); !errors.Is(err, p.errorGet) || !reflect.DeepEqual(p.calls, []string{"get"}) {
		t.Fatal(err, p.calls)
	}
}
func Test事件读取空数组及构造必需依赖(t *testing.T) {
	_, r := eventFixture()
	a, err := r.List(t.Context(), operator, "id", 0, 0)
	if err != nil || a == nil {
		t.Fatal(a, err)
	}
	p := &eventProbe{}
	for _, d := range []EventDeps{{}, {Tasks: p, Events: p}, {Tasks: p, Notifier: p}, {Events: p, Notifier: p}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("缺少依赖未拒绝")
				}
			}()
			NewEventReader(d)
		}()
	}
}
func Test事件订阅并发重复关闭(t *testing.T) {
	p, r := eventFixture()
	s, err := r.Open(t.Context(), operator, "id", 0)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(s.Close)
	}
	wg.Wait()
	if !reflect.DeepEqual(p.calls, []string{"get", "subscribe", "list:200", "close"}) {
		t.Fatal(p.calls)
	}
}
