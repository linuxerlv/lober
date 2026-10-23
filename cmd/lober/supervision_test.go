package main

import (
	"testing"
	"time"

	appmedia "github.com/lober/lober/internal/application/media"
)

func awaitJobRelease(t *testing.T, s *sourceSupervisor, key string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		running := s.running[key]
		s.mu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("job resources not released")
		}
		time.Sleep(time.Millisecond)
	}
}

func Test后台工作panic释放资源退避后重试(t *testing.T) {
	a, _ := assembled(t)
	s := a.Supervisor
	w := appmedia.ResumeWork{ISOID: "fault", Kind: appmedia.ResumeFetch}
	key := "fault//fetch"
	if !s.launch(w, func() error { panic("private failure payload") }) {
		t.Fatal("job did not start")
	}
	awaitJobRelease(t, s, key)
	if len(s.slots) != 0 {
		t.Fatal("worker slot leaked")
	}
	s.mu.Lock()
	next := s.retryAt[key]
	failures := s.failures[key]
	s.mu.Unlock()
	if !next.After(time.Now()) || failures != 1 {
		t.Fatal("panic did not schedule backoff")
	}
	if s.launch(w, func() error { return nil }) {
		t.Fatal("panic retried without backoff")
	}
	time.Sleep(time.Until(next) + time.Millisecond)
	if !s.launch(w, func() error { return nil }) {
		t.Fatal("eligible retry not started")
	}
	awaitJobRelease(t, s, key)
	s.mu.Lock()
	_, stillRetrying := s.retryAt[key]
	_, stillFailed := s.failures[key]
	s.mu.Unlock()
	if stillRetrying || stillFailed || len(s.slots) != 0 {
		t.Fatal("successful job kept failed retry state")
	}
}

func Test后台退避沿用原先间隔与上限(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{{0, 100 * time.Millisecond}, {1, 100 * time.Millisecond}, {2, 200 * time.Millisecond}, {9, 25600 * time.Millisecond}, {10, 25600 * time.Millisecond}, {100, 25600 * time.Millisecond}} {
		if got := supervisorBackoff(tc.attempt); got != tc.want {
			t.Fatal(tc.attempt, got, tc.want)
		}
	}
}

func Test后台协调panic退避且取消立即退出(t *testing.T) {
	a, _ := assembled(t)
	s := a.Supervisor
	started := time.Now()
	done := make(chan struct{})
	calls := 0
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runLoop(func() {
			calls++
			if calls == 1 {
				panic("coordination failure")
			}
			close(done)
			s.cancel()
		})
	}()
	select {
	case <-done:
		if time.Since(started) < 90*time.Millisecond {
			t.Fatal("coordinator restarted without backoff")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("coordinator did not restart")
	}
	s.Close()
	// Cancellation must interrupt a much longer retry wait.
	if s.waitRetry(30 * time.Second) {
		t.Fatal("cancelled supervisor kept waiting")
	}
}
