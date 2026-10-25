package events

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	executionport "github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/model"
	"strings"
	"testing"
)

type logReceiver struct {
	inputs    []executionport.InstallerLog
	ctx       context.Context
	err       error
	onReceive func()
}

func (r *logReceiver) ReceiveInstallerLog(ctx context.Context, in executionport.InstallerLog) error {
	r.ctx = ctx
	in.Body = append([]byte(nil), in.Body...)
	r.inputs = append(r.inputs, in)
	if r.onReceive != nil {
		r.onReceive()
	}
	return r.err
}

var _ executionport.IPObservationSink = (*Collector)(nil)

func TestSyslogNeedsAuthenticatedIPLeaseAndRedactsCredentials(t *testing.T) {
	receiver := &logReceiver{}
	now := int64(1000)
	c := New(receiver, func() int64 { return now })
	body := []byte("installer fetch http://gw/v1/answer/task-one/" + strings.Repeat("a", 64) + "/ks.cfg\x00tail")
	if err := c.Receive(t.Context(), "192.168.56.101:514", body); err != nil || len(receiver.inputs) != 0 {
		t.Fatal("unattributed syslog persisted", err)
	}
	c.ObserveIP("task-one", 1, "192.168.56.101:39400", 1100)
	if err := c.Receive(t.Context(), "192.168.56.101:514", body); err != nil {
		t.Fatal(err)
	}
	if len(receiver.inputs) != 1 {
		t.Fatal("authenticated syslog missing")
	}
	decoded := receiver.inputs[0].Body
	if strings.Contains(string(decoded), strings.Repeat("a", 64)) || !strings.Contains(string(decoded), "[REDACTED]") {
		t.Fatal("credential leaked")
	}
	if !strings.Contains(string(decoded), "\x00tail") {
		t.Fatal("binary log corrupted")
	}
	now = 1100
	if err := c.Receive(t.Context(), "192.168.56.101:514", body); err != nil || len(receiver.inputs) != 1 {
		t.Fatal("expired mapping accepted", err)
	}
}
func Test日志传输先截断再脱敏且保持捕获时间(t *testing.T) {
	now := int64(1000)
	calls := 0
	receiver := &logReceiver{onReceive: func() { now = 1500 }}
	c := New(receiver, func() int64 { calls++; return now })
	c.ObserveIP("task-one", 2, "[::ffff:192.0.2.1]:1", 2000)
	calls = 0
	body := append(bytes.Repeat([]byte("x"), 65530), []byte("\x00TAIL!Z")...)
	if len(body) != 65537 {
		t.Fatal(len(body))
	}
	ctx := context.WithValue(t.Context(), struct{}{}, "original")
	if err := c.Receive(ctx, "192.0.2.1:514", body); err != nil {
		t.Fatal(err)
	}
	in := receiver.inputs[0]
	if !bytes.Equal(in.Body, model.SafeLogTail(body[:65536])) || bytes.HasSuffix(in.Body, []byte("Z")) || in.At != 1000 || calls != 1 || in.SourceIP != "192.0.2.1" || in.TaskID != "task-one" || in.Attempt != 2 || receiver.ctx != ctx {
		t.Fatal("字节或捕获时间改变", calls, in)
	}
	// 凭据的前缀不能因只取尾部而被切掉，必须先脱敏再取尾部。
	secret := strings.Repeat("a", 64)
	body = []byte("/v1/answer/task-one/" + secret + "/" + strings.Repeat("x", 4050))
	receiver.onReceive = nil
	if err := c.Receive(ctx, "[::ffff:192.0.2.1]:514", body); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(receiver.inputs[1].Body, []byte(strings.Repeat("a", 20))) || !bytes.Equal(receiver.inputs[1].Body, model.SafeLogTail(body)) {
		t.Fatal("log tail unbounded or credential leaked")
	}
	now = 2000
	before := len(receiver.inputs)
	if err := c.Receive(ctx, "192.0.2.1:514", []byte("expired")); err != nil || len(receiver.inputs) != before {
		t.Fatal("expired mapping accepted", err)
	}
}
func Test日志租约容量覆盖无效地址与错误原样传递(t *testing.T) {
	now := int64(1000)
	receiver := &logReceiver{}
	c := New(receiver, func() int64 { return now })
	for _, address := range []string{"invalid", "hostname:514", "192.0.2.1", "[:::]:9"} {
		c.ObserveIP("task-one", 1, address, 2000)
		if err := c.Receive(t.Context(), address, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		id      string
		attempt int
		expires int64
	}{{"invalid/id", 1, 2000}, {"task-one", 0, 2000}, {"task-one", 1, 1000}} {
		c.ObserveIP(tc.id, tc.attempt, "192.0.2.1:9", tc.expires)
	}
	if len(c.leases) != 0 || len(receiver.inputs) != 0 {
		t.Fatal("非法租约被接受")
	}
	for i := 0; i < 4096; i++ {
		c.ObserveIP("task-one", 1, fmt.Sprintf("10.%d.%d.%d:9", i/65536, (i/256)%256, i%256), 2000)
	}
	c.ObserveIP("task-two", 2, "192.0.2.1:9", 2000)
	c.ObserveIP("task-two", 2, "10.0.0.0:9", 2000)
	if len(c.leases) != 4096 {
		t.Fatal("容量改变", len(c.leases))
	}
	if err := c.Receive(t.Context(), "192.0.2.1:514", []byte("unbound")); err != nil || len(receiver.inputs) != 0 {
		t.Fatal("新地址超过容量仍登记", err)
	}
	sentinel := errors.New("receiver failed")
	receiver.err = sentinel
	if err := c.Receive(t.Context(), "10.0.0.0:514", []byte("x")); err != sentinel || receiver.inputs[0].TaskID != "task-two" || receiver.inputs[0].Attempt != 2 {
		t.Fatal("覆盖或错误改变", err)
	}
	now = 2000
	c.ObserveIP("task-one", 3, "192.0.2.1:9", 3000)
	if len(c.leases) != 1 {
		t.Fatal("到期地址未清理", len(c.leases))
	}
}
