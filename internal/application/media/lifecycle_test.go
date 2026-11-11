package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func Test关闭后不能重新启动(t *testing.T) {
	s := NewSupervisor(t.Context(), SupervisorDeps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Workers: 1})
	s.Close()
	s.Close()
	if err := s.Start(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func Test删除缺少依赖在组装时拒绝(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("缺少依赖却能构造删除应用")
		}
	}()
	NewDeletion(DeletionDeps{})
}

type waitingWork struct {
	entered chan struct{}
	calls   atomic.Int32
}

func (w *waitingWork) Recover(ctx context.Context) ([]ResumeWork, error) {
	w.calls.Add(1)
	close(w.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func (*waitingWork) FetchOne(context.Context, string, string) error { return nil }
func (*waitingWork) Import(context.Context, string) error           { return nil }
func Test关闭能取消正在启动的恢复读取(t *testing.T) {
	work := &waitingWork{entered: make(chan struct{})}
	s := NewSupervisor(t.Context(), SupervisorDeps{Work: work, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	started := make(chan error, 1)
	go func() { started <- s.Start() }()
	<-work.entered
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("关闭没有取消启动读取")
	}
	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func Test关闭等待作业且拒绝再占名额(t *testing.T) {
	var logs bytes.Buffer
	s := NewSupervisor(t.Context(), SupervisorDeps{Workers: 1, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	entered, release := make(chan struct{}), make(chan struct{})
	work := ResumeWork{ISOID: "one", Kind: ResumeFetch}
	if !s.launch(work, func() error { close(entered); <-release; panic("secret-token") }) {
		t.Fatal("未启动")
	}
	<-entered
	if s.launch(work, func() error { return nil }) || s.launch(ResumeWork{ISOID: "two", Kind: ResumeImport}, func() error { return nil }) {
		t.Fatal("重复工作或超过名额")
	}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	<-s.ctx.Done()
	if s.launch(ResumeWork{ISOID: "three", Kind: ResumeFetch}, func() error { return nil }) {
		t.Fatal("关闭后启动作业")
	}
	select {
	case <-closed:
		t.Fatal("未等待作业")
	default:
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("没有结束")
	}
	s.Close()
	if len(s.slots) != 0 || len(s.running) != 0 {
		t.Fatal("名额或工作标记泄漏")
	}
	if strings.Contains(logs.String(), "secret-token") || !strings.Contains(logs.String(), "后台任务异常") {
		t.Fatal("异常日志未脱敏", logs.String())
	}
}
