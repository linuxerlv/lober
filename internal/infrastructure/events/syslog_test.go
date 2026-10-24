package events

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/execution"
	executionport "github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type memoryRepo struct {
	task    model.Task
	entries []repository.NewEvent
}

func (r *memoryRepo) GetTask(context.Context, string) (model.Task, error) { return r.task, nil }
func (r *memoryRepo) AppendEvents(_ context.Context, _ string, events []repository.NewEvent) ([]repository.Appended, error) {
	r.entries = append(r.entries, events...)
	return []repository.Appended{{Seq: 1}}, nil
}
func TestSyslogNeedsAuthenticatedIPLeaseAndRedactsCredentials(t *testing.T) {
	repo := &memoryRepo{task: model.Task{ID: "task-one", State: model.TaskInstalling, Execution: &model.Execution{Attempt: 1}}}
	now := int64(1000)
	c := New(repo, func() int64 { return now }, nil)
	body := []byte("installer fetch http://gw/v1/answer/task-one/" + strings.Repeat("a", 64) + "/ks.cfg\x00tail")
	if err := c.Receive(t.Context(), "192.168.56.101:514", body); err != nil || len(repo.entries) != 0 {
		t.Fatal("unattributed syslog persisted", err)
	}
	c.ObserveIP("task-one", 1, "192.168.56.101:39400", 1100)
	if err := c.Receive(t.Context(), "192.168.56.101:514", body); err != nil {
		t.Fatal(err)
	}
	if len(repo.entries) != 1 {
		t.Fatal("authenticated syslog missing")
	}
	decoded, err := base64.StdEncoding.DecodeString(repo.entries[0].Fields["log_base64"])
	if err != nil || strings.Contains(string(decoded), strings.Repeat("a", 64)) || !strings.Contains(string(decoded), "[REDACTED]") {
		t.Fatal("credential leaked", err)
	}
	if !strings.Contains(string(decoded), "\x00tail") {
		t.Fatal("binary log corrupted")
	}
	now = 1100
	if err := c.Receive(t.Context(), "192.168.56.101:514", body); err != nil || len(repo.entries) != 1 {
		t.Fatal("expired mapping accepted", err)
	}
}
func TestSyslogRejectsOldAttemptsAndBoundedPackets(t *testing.T) {
	repo := &memoryRepo{task: model.Task{ID: "task-one", State: model.TaskInstalling, Execution: &model.Execution{Attempt: 2}}}
	c := New(repo, func() int64 { return 1000 }, nil)
	c.ObserveIP("task-one", 1, "192.168.56.101:1", 2000)
	if err := c.Receive(t.Context(), "192.168.56.101:514", []byte("old installer")); err != nil || len(repo.entries) != 0 {
		t.Fatal("old attempt log attributed", err)
	}
	c.ObserveIP("task-one", 2, "192.168.56.101:1", 2000)
	if err := c.Receive(t.Context(), "192.168.56.101:514", []byte(strings.Repeat("x", 8000))); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(repo.entries[0].Fields["log_base64"])
	if err != nil || len(decoded) != 4096 {
		t.Fatal("log tail unbounded", len(decoded), err)
	}
	repo.task.State = model.TaskDone
	if err := c.Receive(t.Context(), "192.168.56.101:514", []byte("finished")); err != nil || len(repo.entries) != 1 {
		t.Fatal("terminal task log accepted", err)
	}
}

var _ executionport.IPObservationSink = (*Collector)(nil)

func Test执行观察小接口可直接连接日志收集器(t *testing.T) {
	repo := &memoryRepo{task: model.Task{ID: "task-one", State: model.TaskInstalling, Execution: &model.Execution{Attempt: 2, IssuedAt: 1000}}}
	now := int64(1000)
	c := New(repo, func() int64 { return now }, nil)
	observe := execution.NewIPObserver(c)
	observe(repo.task, "192.0.2.1:1234")
	if e := c.Receive(t.Context(), "192.0.2.1:514", []byte("installer")); e != nil || len(repo.entries) != 1 {
		t.Fatal("小接口未连接", e)
	}
	now = 6400
	if e := c.Receive(t.Context(), "192.0.2.1:514", []byte("expired")); e != nil || len(repo.entries) != 1 {
		t.Fatal("观察有效期改变", e)
	}
}
