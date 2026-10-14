package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/iso"
	"github.com/lober/lober/internal/store"
)

type sourceSupervisor struct {
	source          *iso.Sources
	store           store.Store
	journal         *deletionJournal
	gate            *registrationGate
	cfg             isoConfig
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

func newSourceSupervisor(ctx context.Context, src *iso.Sources, st store.Store, j *deletionJournal, gate *registrationGate, cfg isoConfig, logger *slog.Logger) *sourceSupervisor {
	child, cancel := context.WithCancel(ctx)
	return &sourceSupervisor{source: src, store: st, journal: j, gate: gate, cfg: cfg, logger: logger, ctx: child, cancel: cancel, wake: make(chan struct{}, 1), slots: make(chan struct{}, cfg.Workers), running: map[string]bool{}, retryAt: map[string]time.Time{}, failures: map[string]int{}}
}
func (s *sourceSupervisor) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *sourceSupervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return context.Canceled
	}
	if s.started {
		return nil
	}
	// Validate recovery reads before acknowledging a running application.
	if _, err := s.source.Recover(s.ctx); err != nil {
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

func (s *sourceSupervisor) guarded(work func() error) (err error) {
	defer func() {
		if recover() != nil {
			// Panic values can contain credentials or user content.
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

func (s *sourceSupervisor) waitRetry(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return false
	case <-timer.C:
		return s.ctx.Err() == nil
	}
}

func (s *sourceSupervisor) runLoop(reconcile func()) {
	ticker := time.NewTicker(time.Duration(s.cfg.PollMS) * time.Millisecond)
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
func (s *sourceSupervisor) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *sourceSupervisor) reconcile() {
	if err := s.journal.RetryAll(s.ctx); err != nil && s.ctx.Err() == nil {
		s.logger.Warn("ISO 删除清理尚未完成")
	}
	work, err := s.source.Recover(s.ctx)
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
		if w.Kind == iso.ResumeFetch {
			src, exists, err := s.store.GetSource(s.ctx, w.ISOID, w.Role)
			if err != nil || !exists || src.Kind == store.SourceUpload {
				continue
			}
		}
		s.launch(w, func() error {
			if w.Kind == iso.ResumeFetch {
				return s.source.FetchOne(s.ctx, w.ISOID, w.Role)
			}
			return s.source.Import(s.ctx, w.ISOID)
		})
	}
}

func (s *sourceSupervisor) launch(w iso.ToResume, work func() error) bool {
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
func (s *sourceSupervisor) Register(ctx context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a store.Audit) error {
	s.gate.RLock()
	defer s.gate.RUnlock()
	if err := s.source.RegisterQueued(ctx, item, kind, addr, role, size, digest, a); err != nil {
		return err
	}
	s.Wake()
	return nil
}
func (s *sourceSupervisor) Upload(ctx context.Context, id, role string, offset int64, r io.Reader, a store.Audit) error {
	err := s.source.Upload(ctx, id, role, offset, r, a)
	if err == nil {
		s.Wake()
	}
	return err
}
func (s *sourceSupervisor) Cancel(ctx context.Context, id string, a store.Audit) error {
	return s.source.Cancel(ctx, id, a)
}
func (s *sourceSupervisor) Retry(ctx context.Context, id string, a store.Audit) error {
	err := s.source.Retry(ctx, id, a)
	if err == nil {
		s.Wake()
	}
	return err
}
func (s *sourceSupervisor) Reimport(ctx context.Context, id string, a store.Audit) error {
	err := s.source.RequestReimport(ctx, id, a)
	if err == nil {
		s.Wake()
	}
	return err
}
func (s *sourceSupervisor) FreeSpace(ctx context.Context) (int64, int64, error) {
	return s.source.FreeSpace(ctx)
}
