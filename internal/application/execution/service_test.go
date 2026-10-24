package execution_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/snapshot"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

type simultaneousBootRepo struct {
	execution.Repository
	arrived sync.WaitGroup
	reads   atomic.Int32
	release chan struct{}
}

func (r *simultaneousBootRepo) FindOpenTask(ctx context.Context, sn string) (model.Task, bool, error) {
	task, found, err := r.Repository.FindOpenTask(ctx, sn)
	if r.reads.Add(1) <= 2 {
		r.arrived.Done()
		<-r.release
	}
	return task, found, err
}
func TestConcurrentBootCASConflictDoesNotIssueSecondCredential(t *testing.T) {
	st := pgtest.OpenStore(t)
	fixture(t, st)
	var now atomic.Int64
	now.Store(1000)
	repo := &simultaneousBootRepo{Repository: st, release: make(chan struct{})}
	repo.arrived.Add(2)
	s, err := execution.New(repo, []byte(strings.Repeat("k", 32)), now.Load, nil)
	must(t, err)
	type result struct {
		found bool
		err   error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			_, found, err := s.Boot(t.Context(), "EXECUTION-GUEST", "x86_64", "bios")
			results <- result{found, err}
		}()
	}
	repo.arrived.Wait()
	close(repo.release)
	winners := 0
	for range 2 {
		r := <-results
		must(t, r.err)
		if r.found {
			winners++
		}
	}
	if winners != 1 {
		t.Fatal("concurrent request re-read issued another credential", winners)
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, st *pgstore.Store) model.Task {
	t.Helper()
	ctx := t.Context()
	audit := repository.Audit{Actor: "tester", Action: "task.create", At: 1000}
	host, err := st.CreateHost(ctx, model.Host{SN: "EXECUTION-GUEST"}, audit)
	must(t, err)
	iso, err := st.CreateISO(ctx, model.ISO{Name: "execution-fixture", Family: "linux", Series: "rocky"}, audit)
	must(t, err)
	iso.Arch = "x86_64"
	iso.Version = "9.8"
	iso.State = model.ISOReady
	iso.SHA256 = strings.Repeat("1", 64)
	iso.Artifacts = map[string]string{"kernel": "fixture/linux", "initrd": "fixture/initrd"}
	iso, err = st.UpdateISO(ctx, iso)
	must(t, err)
	tpl := model.Template{ID: "builtin-execution-fixture", Version: 1, Name: "lober:execution-fixture", Builtin: true, State: model.VersionApproved, Family: "linux", Series: []catalog.Series{"rocky"}, Body: "{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n"}
	must(t, st.EnsureBuiltinTemplate(ctx, tpl))
	snap, problems := snapshot.Build(params.Input{Template: tpl, ISO: iso, Host: host})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	tasks, err := st.CreateTasks(ctx, []model.Task{{HostSN: host.SN, ISOID: iso.ID, TemplateID: tpl.ID, TemplateVersion: 1, Snapshot: snap, CreatedBy: "tester", CreatedAt: 1000}}, audit)
	must(t, err)
	return tasks[0]
}
func service(t *testing.T, st *pgstore.Store, now *atomic.Int64) *execution.Service {
	t.Helper()
	s, err := execution.New(st, []byte(strings.Repeat("k", 32)), now.Load, nil)
	must(t, err)
	return s
}
func boot(t *testing.T, s *execution.Service) model.Task {
	t.Helper()
	task, found, err := s.Boot(t.Context(), "execution-guest", "x86_64", "bios")
	must(t, err)
	if !found {
		t.Fatal("registered task did not boot")
	}
	return task
}

func TestDurableLifecycleConcurrentDuplicatesAndPostRebootRetry(t *testing.T) {
	dsn := pgtest.Schema(t)
	st := pgtest.OpenAt(t, dsn)
	created := fixture(t, st)
	var now atomic.Int64
	now.Store(1000)
	s := service(t, st, &now)
	task := boot(t, s)
	install, final := s.Token(task, execution.Install), s.Token(task, execution.Finalize)
	_, err := s.Answer(t.Context(), task.ID, install)
	must(t, err)
	must(t, s.Signal(t.Context(), task.ID, install, execution.Signal{Phase: "installed"}))
	// An acknowledged request may lose its HTTP response. Retrying the exact
	// installer boundary must succeed without reopening file access.
	must(t, s.Signal(t.Context(), task.ID, install, execution.Signal{Phase: "installed"}))
	must(t, st.Close())
	st = pgtest.OpenAt(t, dsn)
	s = service(t, st, &now)
	must(t, s.Signal(t.Context(), task.ID, final, execution.Signal{Phase: "booted"}))
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Go(func() { errs <- s.Signal(t.Context(), task.ID, final, execution.Signal{Phase: "init-start"}) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	events, err := st.ListEvents(t.Context(), task.ID, 0, 100)
	must(t, err)
	starts := 0
	for _, event := range events {
		if event.Kind == "machine.init_start" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatal("duplicate signal wrote multiple events", starts)
	}
	must(t, s.Signal(t.Context(), task.ID, final, execution.Signal{Phase: "init-done"}))
	must(t, s.Signal(t.Context(), task.ID, final, execution.Signal{Phase: "ready"}))
	must(t, s.Signal(t.Context(), task.ID, final, execution.Signal{Phase: "ready"}))
	task, err = st.GetTask(t.Context(), task.ID)
	must(t, err)
	if task.State != model.TaskDone || !task.Execution.ReachedReboot || task.Snapshot.Template.Body != created.Snapshot.Template.Body {
		t.Fatal("lifecycle or immutable snapshot lost")
	}
	err = s.Retry(t.Context(), task, repository.Audit{Actor: "tester", Action: "task.retry", At: 1000})
	if !errors.Is(err, repository.ErrRejected) {
		t.Fatal("rebooted task retry accepted", err)
	}
	if _, found, err := s.Boot(t.Context(), task.HostSN, "x86_64", "bios"); err != nil || found {
		t.Fatal("completed task reinstalled", err)
	}
}
func TestPreseedServesBootedInstallerOnly(t *testing.T) {
	st := pgtest.OpenStore(t)
	fixture(t, st)
	var now atomic.Int64
	now.Store(1000)
	s := service(t, st, &now)
	ctx := t.Context()
	// pending：机器还没 PXE 引导过，先把应答发给它只会提前推状态机。
	if _, found, err := s.Preseed(ctx, "EXECUTION-GUEST"); err != nil || found {
		t.Fatalf("pending task served before boot: %v %v", found, err)
	}
	if _, found, err := s.Preseed(ctx, "EXECUTION-NONE"); err != nil || found {
		t.Fatalf("unregistered SN served: %v %v", found, err)
	}
	task := boot(t, s)
	if _, found, err := s.Preseed(ctx, "EXECUTION-GUEST"); err != nil || !found {
		t.Fatalf("booting task not served: %v %v", found, err)
	}
	if _, err := s.Answer(ctx, task.ID, s.Token(task, execution.Install)); err != nil {
		t.Fatal(err)
	}
	// d-i 可能多次重取 DHCP filename 的 preseed；installing 阶段保持可发。
	if _, found, err := s.Preseed(ctx, "EXECUTION-GUEST"); err != nil || !found {
		t.Fatalf("installing task not refetchable: %v %v", found, err)
	}
	must(t, s.Signal(ctx, task.ID, s.Token(task, execution.Install), execution.Signal{Phase: "installed"}))
	final := s.Token(task, execution.Finalize)
	for _, phase := range []string{"booted", "init-start", "init-done", "ready"} {
		must(t, s.Signal(ctx, task.ID, final, execution.Signal{Phase: phase}))
	}
	if _, found, err := s.Preseed(ctx, "EXECUTION-GUEST"); err != nil || found {
		t.Fatalf("finished task served: %v %v", found, err)
	}
}

func TestTimeoutRestartRetryRevokesOldCredentialAndAuditIsAtomic(t *testing.T) {
	st := pgtest.OpenStore(t)
	fixture(t, st)
	var now atomic.Int64
	now.Store(1000)
	s := service(t, st, &now)
	task := boot(t, s)
	old := s.Token(task, execution.Install)
	now.Store(1900)
	s = service(t, st, &now)
	must(t, s.Sweep(t.Context()))
	task, err := st.GetTask(t.Context(), task.ID)
	must(t, err)
	if task.State != model.TaskFailed {
		t.Fatal("restart forgot timeout")
	}
	err = s.Retry(t.Context(), task, repository.Audit{Actor: pgtest.FaultActor, Action: "task.retry", At: 1600})
	if err == nil {
		t.Fatal("audit fault did not abort retry")
	}
	unchanged, err := st.GetTask(t.Context(), task.ID)
	must(t, err)
	if unchanged.Rev != task.Rev || unchanged.Execution.Attempt != 1 {
		t.Fatal("failed audit altered attempt")
	}
	must(t, s.Retry(t.Context(), task, repository.Audit{Actor: "tester", Action: "task.retry", At: 1600}))
	now.Store(10000)
	task = boot(t, s)
	if task.Execution.Attempt != 2 || s.Token(task, execution.Install) == old {
		t.Fatal("retry did not rotate credential")
	}
	if _, err := s.Answer(t.Context(), task.ID, old); err == nil {
		t.Fatal("old token accepted")
	}
	_, err = s.Answer(t.Context(), task.ID, s.Token(task, execution.Install))
	must(t, err)
	task, err = st.GetTask(t.Context(), task.ID)
	must(t, err)
	must(t, s.Cancel(t.Context(), task, repository.Audit{Actor: "tester", Action: "task.cancel", At: 10000}))
	if _, err := s.Answer(t.Context(), task.ID, s.Token(task, execution.Install)); err == nil {
		t.Fatal("cancelled task supplied answer")
	}
}

func Test机器凭据保持安装与收尾顺序(t *testing.T) {
	s, err := execution.New(nil, []byte(strings.Repeat("k", 32)), func() int64 { return 1000 }, nil)
	must(t, err)
	for _, task := range []model.Task{{ID: "task"}, {ID: "task", Execution: &model.Execution{Attempt: 2, IssuedAt: 1000}}} {
		a, b := s.Credentials(task)
		if a != s.Token(task, execution.Install) || b != s.Token(task, execution.Finalize) {
			t.Fatal("凭据顺序改变")
		}
	}
}
