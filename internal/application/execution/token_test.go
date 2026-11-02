package execution

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"reflect"
	"strings"
	"testing"
)

func TestCredentialsBindTaskAttemptScopeAndExpiry(t *testing.T) {
	s, err := New(nil, []byte(strings.Repeat("k", 32)), func() int64 { return 1100 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	task := credentialTask()
	install, final := s.Token(task, Install), s.Token(task, Finalize)
	if install != "11541f7c22068cd1fc1fc0c47d3554cebab6749381322b430c8a921e2c3ad0a1" || final != "fe9c5b0328380801fff6cd58fada0d8c7a519aeae0e618d403efacbb51cf5f1e" {
		t.Fatal("原签名文本或编码改变", install, final)
	}
	if install == final {
		t.Fatal("scope credentials coincide")
	}
	if err := s.Authorize(task, install, Install); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(task, final, Install); err == nil {
		t.Fatal("finalize credential can download installation files")
	}
	task.ID = "other"
	if s.Authorize(task, install, Install) == nil {
		t.Fatal("cross-task token accepted")
	}
	task.ID = "task-one"
	task.Execution.Attempt++
	if s.Authorize(task, install, Install) == nil {
		t.Fatal("old attempt accepted")
	}
	task.Execution.Attempt = 1
	// 安装寿命 4 小时：IssuedAt 1000 之后 14400 秒整才失效，差一秒仍可用。
	// 这一步只看凭据寿命，先把阶段期限清零，免得被 InstallAccessAllowed 先行拦下。
	task.Execution.Deadline = 0
	s.Machine.clock.now = func() int64 { return 1000 + 14400 }
	if s.Authorize(task, install, Install) == nil {
		t.Fatal("expired install accepted")
	}
	s.Machine.clock.now = func() int64 { return 1000 + 14400 - 1 }
	if s.Authorize(task, install, Install) != nil {
		t.Fatal("install rejected one second before its four-hour lifetime ends")
	}
	s.Machine.clock.now = func() int64 { return 6400 }
	if s.Authorize(task, final, Finalize) != nil {
		t.Fatal("finalize expires with install")
	}
	s.Machine.clock.now = func() int64 { return 87400 }
	if s.Authorize(task, final, Finalize) == nil {
		t.Fatal("expired finalize accepted")
	}
	s.Machine.clock.now = func() int64 { return 1100 }
	task.State = model.TaskRebooting
	if s.Authorize(task, install, Install) == nil {
		t.Fatal("post-reboot file access accepted")
	}
	if _, err := New(nil, []byte("weak"), s.Machine.clock.now, nil); err == nil {
		t.Fatal("short signing key accepted")
	}
}

func credentialTask() model.Task {
	return model.Task{ID: "task-one", State: model.TaskInstalling, Execution: &model.Execution{Attempt: 1, IssuedAt: 1000, Deadline: 6400}}
}

func TestAuthorizationKeepsOriginalClockCallsAndOrder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		scope     Scope
		deadline  int64
		state     model.TaskState
		token     string
		wantCalls int
		wantErr   bool
	}{
		{"install with deadline", Install, 6400, model.TaskInstalling, "", 2, false},
		{"install without deadline", Install, 0, model.TaskInstalling, "", 1, false},
		{"install negative deadline", Install, -1, model.TaskInstalling, "", 1, false},
		{"finalize ignores deadline", Finalize, 1100, model.TaskDone, "", 1, false},
		{"wrong signature before deadline", Install, 6400, model.TaskInstalling, strings.Repeat("0", 64), 1, true},
		{"malformed before time", Install, 6400, model.TaskInstalling, "bad", 0, true},
		{"finished still checks original deadline", Install, 6400, model.TaskDone, "", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s, _ := New(nil, []byte(strings.Repeat("k", 32)), func() int64 { calls++; return 1100 }, nil)
			task := credentialTask()
			task.State = tc.state
			task.Execution.Deadline = tc.deadline
			token := tc.token
			if token == "" {
				token = s.Token(task, tc.scope)
			}
			err := s.Authorize(task, token, tc.scope)
			if (err != nil) != tc.wantErr || calls != tc.wantCalls {
				t.Fatal(err, calls)
			}
		})
	}
	calls := 0
	s, _ := New(nil, []byte(strings.Repeat("k", 32)), func() int64 { calls++; return 1100 }, nil)
	if s.Authorize(model.Task{}, strings.Repeat("0", 64), Install) != ErrCredential || calls != 0 {
		t.Fatal("无记录读取时钟")
	}
	task := credentialTask()
	token := s.Token(task, Install)
	s.Machine.clock.now = func() int64 {
		calls++
		if calls == 1 {
			return 1100
		}
		return 6400
	}
	if s.Authorize(task, token, Install) != ErrCredential || calls != 2 {
		t.Fatal("额外期限时钟位置改变", calls)
	}
}

type clockRepo struct {
	Repository
	task               model.Task
	readErr, commitErr error
	reads, writes      int
	queries            []repository.TaskQuery
	pages              []repository.Page[model.Task]
	events             []repository.NewEvent
	expects            []repository.TaskExpect
}

func (r *clockRepo) GetTask(context.Context, string) (model.Task, error) {
	r.reads++
	return r.task, r.readErr
}
func (r *clockRepo) FindOpenTask(context.Context, string) (model.Task, bool, error) {
	r.reads++
	return r.task, true, r.readErr
}
func (r *clockRepo) UpdateTaskExecution(_ context.Context, _ string, e repository.TaskExpect, state model.TaskState, run *model.Execution, events []repository.NewEvent, _ repository.Audit) (model.Task, []repository.Appended, error) {
	r.writes++
	r.expects = append(r.expects, e)
	r.events = append(r.events, events...)
	if r.commitErr != nil {
		return model.Task{}, nil, r.commitErr
	}
	r.task.State = state
	r.task.Execution = run
	r.task.Rev++
	return r.task, nil, nil
}
func (r *clockRepo) ListTasks(_ context.Context, q repository.TaskQuery) (repository.Page[model.Task], error) {
	r.queries = append(r.queries, q)
	if r.readErr != nil {
		return repository.Page[model.Task]{}, r.readErr
	}
	p := r.pages[0]
	r.pages = r.pages[1:]
	return p, nil
}

type clockNotify struct{ ids []string }

func (n *clockNotify) Publish(id string) { n.ids = append(n.ids, id) }

func TestAnswerAndControlKeepClockAndRevisionBoundaries(t *testing.T) {
	r := &clockRepo{task: credentialTask()}
	n := &clockNotify{}
	calls := 0
	s, _ := New(r, []byte(strings.Repeat("k", 32)), func() int64 { calls++; return 1100 }, n)
	token := s.Token(r.task, Install)
	if _, err := s.Answer(t.Context(), r.task.ID, token); err != nil || calls != 2 || r.writes != 0 || len(n.ids) != 0 {
		t.Fatal("已安装读取了推进时钟", err, calls, r.writes)
	}
	r.task.State = model.TaskBooting
	calls = 0
	if _, err := s.Answer(t.Context(), r.task.ID, token); err != nil || calls != 3 || r.writes != 1 || len(n.ids) != 1 || r.events[0].Key != "attempt-1-installing" {
		t.Fatal(err, calls, r.events)
	}
	audit := repository.Audit{Actor: "operator", Action: "task.cancel", At: 1100}
	// 旧版本先报冲突；诊断失败不取推进时间。
	old := r.task
	old.Rev--
	calls = 0
	if err := s.Cancel(t.Context(), old, audit); !errors.Is(err, repository.ErrConflict) || calls != 0 {
		t.Fatal(err, calls)
	}
	r.task.State = model.TaskDone
	calls = 0
	if err := s.Cancel(t.Context(), r.task, audit); !errors.Is(err, repository.ErrRejected) || calls != 0 {
		t.Fatal(err, calls)
	}
	r.task.State = model.TaskInstalling
	calls = 0
	latest := r.task
	if err := s.Cancel(t.Context(), latest, audit); err != nil || calls != 1 || r.task.State != model.TaskCancelled || r.expects[len(r.expects)-1].Rev != latest.Rev {
		t.Fatal(err, calls, r.task)
	}
	calls = 0
	if err := s.Cancel(t.Context(), latest, audit); !errors.Is(err, repository.ErrConflict) || calls != 0 {
		t.Fatal("旧输入被自动刷新", err, calls)
	}
}

func TestBootConflictAndEightRoundLimitsStayUnchanged(t *testing.T) {
	r := &clockRepo{task: credentialTask(), commitErr: repository.ErrConflict}
	r.task.State = model.TaskPending
	r.task.Execution = nil
	r.task.Snapshot = model.Snapshot{Series: "rocky", ISO: model.SnapshotISO{Arch: "x86_64"}}
	n := &clockNotify{}
	s, _ := New(r, []byte(strings.Repeat("k", 32)), func() int64 { return 1100 }, n)
	if _, found, err := s.Boot(t.Context(), "SN-ONE", "x86_64", "bios"); err != nil || found || r.writes != 1 || r.reads != 1 || len(n.ids) != 0 {
		t.Fatal(found, err, r.reads, r.writes)
	}
	r.task = credentialTask()
	r.task.State = model.TaskBooting
	r.reads = 0
	r.writes = 0
	if _, err := s.Answer(t.Context(), r.task.ID, s.Token(r.task, Install)); !errors.Is(err, repository.ErrConflict) || r.reads != 8 || r.writes != 8 {
		t.Fatal(err, r.reads, r.writes)
	}
	r.task.State = model.TaskInstalling
	r.task.Snapshot.Slots = nil
	r.task.Execution.Signals = map[string]string{}
	r.reads = 0
	r.writes = 0
	if err := s.Signal(t.Context(), r.task.ID, s.Token(r.task, Install), Signal{Phase: "installed"}); !errors.Is(err, repository.ErrConflict) || r.reads != 8 || r.writes != 8 || len(n.ids) != 0 {
		t.Fatal(err, r.reads, r.writes)
	}
}

func TestSweepKeepsPagesStatesAndOnlySwallowsConflicts(t *testing.T) {
	task := credentialTask()
	task.Execution.Deadline = 1100
	r := &clockRepo{task: task, commitErr: repository.ErrConflict, pages: []repository.Page[model.Task]{{Items: []model.Task{task}, Next: "next"}, {Items: []model.Task{{ID: "empty"}}}}}
	calls := 0
	s, _ := New(r, []byte(strings.Repeat("k", 32)), func() int64 { calls++; return 1100 }, nil)
	if err := s.Sweep(t.Context()); err != nil || calls != 2 || r.writes != 1 || len(r.queries) != 2 || r.queries[0].Limit != 100 || r.queries[1].Cursor != "next" {
		t.Fatal(err, calls, r.queries)
	}
	if !reflect.DeepEqual(r.queries[0].States, []model.TaskState{model.TaskBooting, model.TaskInstalling, model.TaskRebooting, model.TaskInitializing}) {
		t.Fatal(r.queries)
	}
	failed := errors.New("save failed")
	r.commitErr = failed
	r.pages = []repository.Page[model.Task]{{Items: []model.Task{task}}}
	if err := s.Sweep(t.Context()); err != failed {
		t.Fatal(err)
	}
}
