package main

import (
	"context"
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
	started, closed bool
	wg              sync.WaitGroup
}

func newSourceSupervisor(ctx context.Context, src *iso.Sources, st store.Store, j *deletionJournal, gate *registrationGate, cfg isoConfig, logger *slog.Logger) *sourceSupervisor {
	child, cancel := context.WithCancel(ctx)
	return &sourceSupervisor{source: src, store: st, journal: j, gate: gate, cfg: cfg, logger: logger, ctx: child, cancel: cancel, wake: make(chan struct{}, 1), slots: make(chan struct{}, cfg.Workers), running: map[string]bool{}}
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
		ticker := time.NewTicker(time.Duration(s.cfg.PollMS) * time.Millisecond)
		defer ticker.Stop()
		for {
			if s.ctx.Err() != nil {
				return
			}
			s.reconcile()
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
			case <-ticker.C:
			}
		}
	}()
	return nil
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
		key := w.ISOID + "/" + w.Role + "/" + string(w.Kind)
		s.mu.Lock()
		if s.closed || s.running[key] {
			s.mu.Unlock()
			continue
		}
		select {
		case s.slots <- struct{}{}:
		default:
			s.mu.Unlock()
			continue
		}
		s.running[key] = true
		s.wg.Add(1)
		s.mu.Unlock()
		go func(w iso.ToResume, key string) {
			defer s.wg.Done()
			defer func() { <-s.slots; s.mu.Lock(); delete(s.running, key); s.mu.Unlock(); s.Wake() }()
			var err error
			if w.Kind == iso.ResumeFetch {
				err = s.source.FetchOne(s.ctx, w.ISOID, w.Role)
			} else {
				err = s.source.Import(s.ctx, w.ISOID)
			}
			if err != nil && s.ctx.Err() == nil {
				s.logger.Warn("ISO 后台工作结束", "iso", w.ISOID, "operation", string(w.Kind))
			}
		}(w, key)
	}
}
func (s *sourceSupervisor) Register(ctx context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a store.Audit) error {
	s.gate.RLock()
	defer s.gate.RUnlock()
	if err := s.source.Register(ctx, item, kind, addr, role, size, digest, a); err != nil {
		return err
	}
	// A newly supplied extra source must also be fetchable after a restart.
	current, err := s.store.GetISO(ctx, item.ID)
	if err != nil {
		return err
	}
	if current.State == model.ISOFailed {
		current.State = model.ISOAcquiring
		if _, err = s.store.UpdateISO(ctx, current); err != nil {
			return err
		}
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
