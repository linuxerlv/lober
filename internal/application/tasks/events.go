package tasks

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"sync"
)

type eventReader struct{ deps EventDeps }

// NewEventReader 要求显式传入任务、事件和共同通知实例。
func NewEventReader(d EventDeps) EventReader {
	if d.Tasks == nil || d.Events == nil || d.Notifier == nil {
		panic("任务事件读取缺少必要能力")
	}
	return &eventReader{deps: d}
}
func eventCopies(events []Event) []Event {
	out := make([]Event, len(events))
	for i, e := range events {
		out[i] = e
		if e.Fields != nil {
			out[i].Fields = make(map[string]string, len(e.Fields))
			for k, v := range e.Fields {
				out[i].Fields[k] = v
			}
		}
	}
	return out
}
func readEvents(ctx context.Context, d EventDeps, id string, after int64, limit int) ([]Event, error) {
	rows, err := d.Events.ListEvents(ctx, id, after, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Event, len(rows))
	for i, e := range rows {
		out[i] = Event{TaskID: e.TaskID, Seq: e.Seq, At: e.At, Kind: e.Kind, Key: e.Key, Fields: e.Fields}
	}
	return eventCopies(out), nil
}
func (r *eventReader) List(ctx context.Context, actor ports.Actor, id string, after int64, limit int) ([]Event, error) {
	if err := actor.Authorize(access.PermTaskRead); err != nil {
		return nil, err
	}
	return readEvents(ctx, r.deps, id, after, limit)
}
func (r *eventReader) Open(ctx context.Context, actor ports.Actor, id string, after int64) (EventSubscription, error) {
	if err := actor.Authorize(access.PermTaskRead); err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, ErrBadRequest
	}
	if _, err := r.deps.Tasks.GetTask(ctx, id); err != nil {
		return nil, err
	}
	hints, stop := r.deps.Notifier.Subscribe(id)
	s := &eventSubscription{deps: r.deps, id: id, hints: hints, stop: stop}
	initial, err := s.Read(ctx, after)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.initial = initial
	return s, nil
}

type eventSubscription struct {
	deps    EventDeps
	id      string
	initial []Event
	hints   <-chan struct{}
	stop    func()
	once    sync.Once
}

func (s *eventSubscription) Initial() []Event       { return eventCopies(s.initial) }
func (s *eventSubscription) Hints() <-chan struct{} { return s.hints }
func (s *eventSubscription) Read(ctx context.Context, after int64) ([]Event, error) {
	return readEvents(ctx, s.deps, s.id, after, 200)
}
func (s *eventSubscription) Close() { s.once.Do(s.stop) }
