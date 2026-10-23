package media

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/lober/lober/internal/domain/model"
)

// SupervisorDeps 只允许读取待办、执行工作和重试删除；不持有整个仓储。
type SupervisorDeps struct {
	Work interface {
		Recover(context.Context) ([]ResumeWork, error)
		FetchOne(context.Context, string, string) error
		Import(context.Context, string) error
	}
	Sources interface {
		GetSource(context.Context, string, string) (model.Source, bool, error)
	}
	Deletion     interface{ RetryAll(context.Context) error }
	Logger       *slog.Logger
	Workers      int
	PollInterval time.Duration
}
type Supervisor struct {
	deps            SupervisorDeps
	logger          *slog.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	wake            chan struct{}
	slots           chan struct{}
	mu              sync.Mutex
	running         map[string]bool
	retryAt         map[string]time.Time
	failures        map[string]int
	started, closed bool
	wg              sync.WaitGroup
}

func NewSupervisor(ctx context.Context, d SupervisorDeps) *Supervisor {
	if d.Workers == 0 {
		d.Workers = 2
	}
	if d.PollInterval == 0 {
		d.PollInterval = time.Second
	}
	if d.Workers < 1 || d.PollInterval < time.Millisecond {
		panic("invalid media supervisor configuration")
	}
	child, cancel := context.WithCancel(ctx)
	return &Supervisor{deps: d, logger: d.Logger, ctx: child, cancel: cancel, wake: make(chan struct{}, 1), slots: make(chan struct{}, d.Workers), running: map[string]bool{}, retryAt: map[string]time.Time{}, failures: map[string]int{}}
}
func (s *Supervisor) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Supervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return context.Canceled
	}
	if s.started {
		return nil
	}
	// 恢复读取成功后，才能确认应用已经启动。
	if _, err := s.deps.Work.Recover(s.ctx); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	s.started = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runLoop(s.reconcile)
	}()
	return nil
}

func (s *Supervisor) guarded(work func() error) (err error) {
	defer func() {
		if recover() != nil {
			// 异常内容可能带有凭据或用户输入，日志只写固定提示。
			s.logger.Error("ISO 后台任务异常，将按退避恢复")
			err = errors.New("background worker panic")
		}
	}()
	return work()
}

func supervisorBackoff(attempt int) time.Duration {
	if attempt > 9 {
		attempt = 9
	}
	return min(100*time.Millisecond*time.Duration(1<<max(attempt-1, 0)), 30*time.Second)
}

func (s *Supervisor) waitRetry(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return false
	case <-timer.C:
		return s.ctx.Err() == nil
	}
}

func (s *Supervisor) runLoop(reconcile func()) {
	ticker := time.NewTicker(s.deps.PollInterval)
	defer ticker.Stop()
	failures := 0
	for s.ctx.Err() == nil {
		if err := s.guarded(func() error { reconcile(); return nil }); err != nil {
			failures++
			if !s.waitRetry(supervisorBackoff(failures)) {
				return
			}
			continue
		}
		failures = 0
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
	}
}
func (s *Supervisor) Close() {
	// 先取消，启动中的恢复读取才能退出并归还锁。
	s.cancel()
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *Supervisor) reconcile() {
	if err := s.deps.Deletion.RetryAll(s.ctx); err != nil && s.ctx.Err() == nil {
		s.logger.Warn("ISO 删除清理尚未完成")
	}
	work, err := s.deps.Work.Recover(s.ctx)
	if err != nil {
		if s.ctx.Err() == nil {
			s.logger.Warn("ISO 恢复读取失败")
		}
		return
	}
	for _, w := range work {
		if s.ctx.Err() != nil {
			return
		}
		if w.Kind == ResumeFetch {
			src, exists, err := s.deps.Sources.GetSource(s.ctx, w.ISOID, w.Role)
			if err != nil || !exists || src.Kind == model.SourceUpload {
				continue
			}
		}
		s.launch(w, func() error {
			if w.Kind == ResumeFetch {
				return s.deps.Work.FetchOne(s.ctx, w.ISOID, w.Role)
			}
			return s.deps.Work.Import(s.ctx, w.ISOID)
		})
	}
}

func (s *Supervisor) launch(w ResumeWork, work func() error) bool {
	key := w.ISOID + "/" + w.Role + "/" + string(w.Kind)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil || s.running[key] || time.Now().Before(s.retryAt[key]) {
		return false
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return false
	}
	s.running[key] = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		err := s.guarded(work)
		if err != nil && s.ctx.Err() == nil {
			s.logger.Warn("ISO 后台工作结束", "iso", w.ISOID, "operation", string(w.Kind))
		}
		<-s.slots
		s.mu.Lock()
		if err != nil && s.ctx.Err() == nil {
			s.failures[key]++
			s.retryAt[key] = time.Now().Add(supervisorBackoff(s.failures[key]))
		} else {
			delete(s.failures, key)
			delete(s.retryAt, key)
		}
		delete(s.running, key)
		s.mu.Unlock()
		if err == nil {
			s.Wake()
		}
	}()
	return true
}
