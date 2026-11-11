package execution_test

import (
	"context"
	"github.com/lober/lober/internal/application/execution"
	executionport "github.com/lober/lober/internal/application/execution/port"
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/events"
	"math"
	"testing"
)

type observedIP struct {
	id      string
	attempt int
	address string
	expires int64
}
type recordingObservation struct{ calls []observedIP }

func (s *recordingObservation) ObserveIP(id string, attempt int, address string, expires int64) {
	s.calls = append(s.calls, observedIP{id, attempt, address, expires})
}
func Test机器日志观察保留原有效期(t *testing.T) {
	sink := &recordingObservation{}
	observe := execution.NewIPObserver(sink)
	observe(model.Task{ID: "absent"}, "untouched:42")
	if len(sink.calls) != 0 {
		t.Fatal(sink.calls)
	}
	for _, tc := range []struct {
		issued int64
		reboot bool
		want   int64
	}{{1000, false, 1000 + executionrules.InstallLife}, {1000, true, 87400}, {math.MaxInt64, false, math.MinInt64 + executionrules.InstallLife - 1}, {-1000, false, -1000 + executionrules.InstallLife}} {
		observe(model.Task{ID: "task", Execution: &model.Execution{Attempt: 2, IssuedAt: tc.issued, ReachedReboot: tc.reboot}}, "untouched:42")
		if len(sink.calls) == 0 {
			t.Fatal("未登记")
		}
		got := sink.calls[len(sink.calls)-1]
		if got != (observedIP{"task", 2, "untouched:42", tc.want}) {
			t.Fatal(got)
		}
	}
	if len(sink.calls) != 4 {
		t.Fatal(sink.calls)
	}
}

type observationLogReceiver struct{ calls []executionport.InstallerLog }

func (r *observationLogReceiver) ReceiveInstallerLog(_ context.Context, in executionport.InstallerLog) error {
	r.calls = append(r.calls, in)
	return nil
}
func Test执行观察小接口可直接连接日志收集器(t *testing.T) {
	r := &observationLogReceiver{}
	now := int64(1000)
	c := events.New(r, func() int64 { return now })
	observe := execution.NewIPObserver(c)
	observe(model.Task{ID: "task-one", Execution: &model.Execution{Attempt: 2, IssuedAt: 1000}}, "192.0.2.1:1234")
	if err := c.Receive(t.Context(), "192.0.2.1:514", []byte("installer")); err != nil || len(r.calls) != 1 || r.calls[0].Attempt != 2 {
		t.Fatal("小接口未连接", err)
	}
	now = 1000 + executionrules.InstallLife
	if err := c.Receive(t.Context(), "192.0.2.1:514", []byte("expired")); err != nil || len(r.calls) != 1 {
		t.Fatal("观察有效期改变", err)
	}
}
