package execution_test

import (
	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/model"
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
	}{{1000, false, 6400}, {1000, true, 87400}, {math.MaxInt64, false, math.MinInt64 + 5399}, {-1000, false, 4400}} {
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
