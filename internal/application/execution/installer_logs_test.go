package execution_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/execution"
	executionport "github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"reflect"
	"testing"
)

type logRepoProbe struct {
	task             model.Task
	readErr, saveErr error
	entries          []repository.NewEvent
	trace            []string
	ctx              context.Context
	afterRead        func()
}

func (r *logRepoProbe) GetTask(ctx context.Context, id string) (model.Task, error) {
	if ctx != r.ctx || id != "task-one" {
		panic("上下文或任务改变")
	}
	r.trace = append(r.trace, "read")
	task := r.task
	if r.afterRead != nil {
		r.afterRead()
	}
	return task, r.readErr
}
func (r *logRepoProbe) AppendEvents(ctx context.Context, id string, ev []repository.NewEvent) ([]repository.Appended, error) {
	if ctx != r.ctx || id != "task-one" {
		panic("上下文或任务改变")
	}
	r.trace = append(r.trace, "append")
	if r.saveErr != nil {
		return nil, r.saveErr
	}
	r.entries = append(r.entries, ev...)
	return []repository.Appended{{Seq: 1}}, nil
}

type logNotifierProbe struct{ repo *logRepoProbe }

func (n logNotifierProbe) Publish(id string) {
	if id != "task-one" {
		panic("通知任务改变")
	}
	n.repo.trace = append(n.repo.trace, "publish")
}
func Test日志收录按事实判断并在成功后通知(t *testing.T) {
	readFailure := errors.New("read failed")
	saveFailure := errors.New("save failed")
	for _, tc := range []struct {
		name                      string
		state                     model.TaskState
		execution                 *model.Execution
		readErr, saveErr, wantErr error
		want                      []string
	}{
		{"success", model.TaskInstalling, &model.Execution{Attempt: 1}, nil, nil, nil, []string{"read", "append", "publish"}},
		{"done", model.TaskDone, &model.Execution{Attempt: 1}, nil, nil, nil, []string{"read"}},
		{"failed", model.TaskFailed, &model.Execution{Attempt: 1}, nil, nil, nil, []string{"read"}},
		{"cancelled", model.TaskCancelled, &model.Execution{Attempt: 1}, nil, nil, nil, []string{"read"}},
		{"nil", model.TaskInstalling, nil, nil, nil, nil, []string{"read"}},
		{"old", model.TaskInstalling, &model.Execution{Attempt: 2}, nil, nil, nil, []string{"read"}},
		{"missing", model.TaskInstalling, nil, repository.ErrNotFound, nil, nil, []string{"read"}},
		{"wrapped-missing", model.TaskInstalling, nil, fmt.Errorf("wrapped: %w", repository.ErrNotFound), nil, nil, []string{"read"}},
		{"read-error", model.TaskInstalling, nil, readFailure, nil, readFailure, []string{"read"}},
		{"read-cancel", model.TaskInstalling, nil, context.Canceled, nil, context.Canceled, []string{"read"}},
		{"save-error", model.TaskInstalling, &model.Execution{Attempt: 1}, nil, saveFailure, saveFailure, []string{"read", "append"}},
		{"save-missing", model.TaskInstalling, &model.Execution{Attempt: 1}, nil, repository.ErrNotFound, repository.ErrNotFound, []string{"read", "append"}},
		{"save-cancel", model.TaskInstalling, &model.Execution{Attempt: 1}, nil, context.Canceled, context.Canceled, []string{"read", "append"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.WithValue(t.Context(), struct{}{}, "original")
			r := &logRepoProbe{task: model.Task{ID: "task-one", State: tc.state, Execution: tc.execution}, readErr: tc.readErr, saveErr: tc.saveErr, ctx: ctx}
			c := execution.NewInstallerLogs(r, logNotifierProbe{r})
			err := c.ReceiveInstallerLog(ctx, executionport.InstallerLog{TaskID: "task-one", Attempt: 1, SourceIP: "192.0.2.1", At: 1000, Body: []byte("x\x00tail")})
			if err != tc.wantErr || !reflect.DeepEqual(r.trace, tc.want) {
				t.Fatalf("error=%v trace=%v", err, r.trace)
			}
			if len(tc.want) == 3 && len(r.entries) != 1 {
				t.Fatal("成功单包必须只写一个事件", len(r.entries))
			}
			if len(r.entries) > 0 {
				ev := r.entries[0]
				if ev.At != 1000 || ev.Key != "" || ev.Kind != "installer.syslog" || len(ev.Fields) != 2 || ev.Fields["source_ip"] != "192.0.2.1" || ev.Fields["log_base64"] != base64.StdEncoding.EncodeToString([]byte("x\x00tail")) {
					t.Fatal(ev)
				}
			}
		})
	}
}

func Test日志保留读取追加窗口且重复包逐包写入(t *testing.T) {
	r := &logRepoProbe{task: model.Task{ID: "task-one", Rev: 7, State: model.TaskInstalling, Execution: &model.Execution{Attempt: 1}}, ctx: t.Context()}
	r.afterRead = func() { r.task.State = model.TaskCancelled; r.task.Execution = &model.Execution{Attempt: 2} }
	receiver := execution.NewInstallerLogs(r, logNotifierProbe{r})
	body := []byte("original\x00body")
	input := executionport.InstallerLog{TaskID: "task-one", Attempt: 1, SourceIP: "192.0.2.1", At: 123, Body: body}
	if err := receiver.ReceiveInstallerLog(t.Context(), input); err != nil || len(r.entries) != 1 {
		t.Fatal("读取后改变任务不新增原子检查", err)
	}
	body[0] = 'X'
	if r.entries[0].Fields["log_base64"] != base64.StdEncoding.EncodeToString([]byte("original\x00body")) {
		t.Fatal("保留了可变正文")
	}
	r.afterRead = nil
	r.task.State = model.TaskInstalling
	r.task.Execution = &model.Execution{Attempt: 1}
	for range 2 {
		if err := receiver.ReceiveInstallerLog(t.Context(), input); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.entries) != 3 || r.task.Rev != 7 || r.task.Execution.Attempt != 1 || r.task.State != model.TaskInstalling {
		t.Fatal("重复包被合并或任务被修改", r.task, len(r.entries))
	}
	if !reflect.DeepEqual(r.trace, []string{"read", "append", "publish", "read", "append", "publish", "read", "append", "publish"}) {
		t.Fatal(r.trace)
	}
}

func Test日志原旧尝试终态与受限包断言(t *testing.T) {
	r := &logRepoProbe{task: model.Task{ID: "task-one", State: model.TaskInstalling, Execution: &model.Execution{Attempt: 2}}, ctx: t.Context()}
	receiver := execution.NewInstallerLogs(r, nil)
	input := executionport.InstallerLog{TaskID: "task-one", Attempt: 1, SourceIP: "192.0.2.1", At: 1000, Body: []byte("old installer")}
	if err := receiver.ReceiveInstallerLog(t.Context(), input); err != nil || len(r.entries) != 0 {
		t.Fatal("old attempt log attributed", err)
	}
	input.Attempt = 2
	input.Body = model.SafeLogTail(bytes.Repeat([]byte("x"), 8000))
	if err := receiver.ReceiveInstallerLog(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(r.entries[0].Fields["log_base64"])
	if err != nil || len(decoded) != 4096 {
		t.Fatal("log tail unbounded", len(decoded), err)
	}
	r.task.State = model.TaskDone
	if err := receiver.ReceiveInstallerLog(t.Context(), input); err != nil || len(r.entries) != 1 {
		t.Fatal("terminal task log accepted", err)
	}
}
func Test日志上下文取消只由仓储返回决定(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &logRepoProbe{task: model.Task{ID: "task-one", State: model.TaskInstalling, Execution: &model.Execution{Attempt: 1}}, ctx: ctx}
	receiver := execution.NewInstallerLogs(r, logNotifierProbe{r})
	if err := receiver.ReceiveInstallerLog(ctx, executionport.InstallerLog{TaskID: "task-one", Attempt: 1}); err != nil || !reflect.DeepEqual(r.trace, []string{"read", "append", "publish"}) {
		t.Fatal("新增取消检查或成功通知丢失", err, r.trace)
	}
}

func Test日志小接口仅暴露精确能力(t *testing.T) {
	if reflect.TypeFor[executionport.InstallerLogRepository]().NumMethod() != 2 || reflect.TypeFor[executionport.InstallerLogReceiver]().NumMethod() != 1 || reflect.TypeFor[execution.Notifier]().NumMethod() != 1 {
		t.Fatal("日志能力口变宽")
	}
}
