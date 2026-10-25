package execution

// 本文件的仓储口只供可信应用或启动组装调用；仓储不认证网页用户或机器令牌。
// 身份、权限或凭据检查的有无及先后，由各消费入口的操作约定负责。
// Boot/Preseed按SN读取开放任务，不先验机器令牌；GetAuthorized先GetTask再验令牌。
// 输入规范化由调用应用负责；排序、空值、错误优先级、重复/版本冲突、锁内复查、原子审计及取消均沿用所指保存契约。
// 只读方法不写业务或审计、不发通知，故无原子写与提交复查；重复读取不保证多次或多页快照。

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type Repository interface {
	// GetTask 的八项保存约定唯一委托 repository.Tasks.GetTask
	// （internal/domain/repository/tasks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetTask(context.Context, string) (model.Task, error)
	// FindOpenTask 的八项保存约定唯一委托 repository.Tasks.FindOpenTask
	// （internal/domain/repository/tasks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	FindOpenTask(context.Context, string) (model.Task, bool, error)
	// ListTasks 的八项保存约定唯一委托 repository.Tasks.ListTasks
	// （internal/domain/repository/tasks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListTasks(context.Context, repository.TaskQuery) (repository.Page[model.Task], error)
	// UpdateTaskExecution 的八项保存约定唯一委托 repository.Tasks.UpdateTaskExecution
	// （internal/domain/repository/tasks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	UpdateTaskExecution(context.Context, string, repository.TaskExpect, model.TaskState, *model.Execution, []repository.NewEvent, repository.Audit) (model.Task, []repository.Appended, error)
}
// Publish 的八项约定唯一委托 ports.Notifier.Publish（internal/application/ports/notification.go），本口只缩小方法集。
type Notifier interface{ Publish(string) }
type Service struct {
	repo     Repository
	key      []byte
	now      func() int64
	notifier Notifier
}

// New 的八项应用约定唯一连接 docs/contracts/machine-execution.md「构造、凭据与授权」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func New(repo Repository, key []byte, now func() int64, notifier Notifier) (*Service, error) {
	if len(key) < 32 || now == nil {
		return nil, errors.New("invalid execution configuration")
	}
	return &Service{repo: repo, key: append([]byte(nil), key...), now: now, notifier: notifier}, nil
}
func (s *Service) commit(ctx context.Context, before, after model.Task, event repository.NewEvent, audit repository.Audit) (model.Task, error) {
	next, _, err := s.repo.UpdateTaskExecution(ctx, before.ID, repository.TaskExpect{State: before.State, Rev: before.Rev}, after.State, after.Execution, []repository.NewEvent{event}, audit)
	if err == nil && s.notifier != nil {
		s.notifier.Publish(next.ID)
	}
	return next, err
}
// Boot 的八项应用约定唯一连接 docs/contracts/machine-execution.md「引导和应答」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (s *Service) Boot(ctx context.Context, sn string, arch catalog.Arch, firmware catalog.Firmware) (model.Task, bool, error) {
	normalized, p := model.NormalizeSN(sn)
	if p != nil {
		return model.Task{}, false, nil
	}
	for range 8 {
		task, found, err := s.repo.FindOpenTask(ctx, normalized)
		if err != nil || !found {
			return task, found, err
		}
		// An installed or running installer must never be booted into installation again.
		if task.State != model.TaskPending && task.State != model.TaskBooting {
			return model.Task{}, false, nil
		}
		at := s.now()
		next, err := boot(task, arch, firmware, at)
		if err != nil {
			return model.Task{}, false, err
		}
		next, err = s.commit(ctx, task, next, repository.NewEvent{At: at, Kind: "machine.boot", Key: attemptKey(next, "boot-"+strconv.Itoa(next.Execution.BootCount)) + "-" + string(next.State), Fields: map[string]string{"arch": string(arch), "firmware": string(firmware)}}, repository.Audit{})
		if errors.Is(err, repository.ErrConflict) {
			return model.Task{}, false, nil
		}
		return next, err == nil && !next.State.Finished(), err
	}
	return model.Task{}, false, repository.ErrConflict
}

// Preseed 按 SN 找可以经 DHCP bootfile 地址下发应答的任务：只有 iPXE 已引导过的
// （booting/installing）才给；pending 的机器上还没有安装器在跑，发过去只会提前推
// 状态机，boot 都进不去；已结束的按 FindOpenTask 的口径本来就不算开放任务。
// Debian 的安装器在 auto 模式下会把 DHCP option 67 的 bootfile 地址当 preseed 下载，
// 并覆盖内核命令行上的 preseed/url，所以网关需要按 SN 在这个阶段给出真实应答（D1）。
// Preseed 的八项应用约定唯一连接 docs/contracts/machine-execution.md「引导和应答」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (s *Service) Preseed(ctx context.Context, sn string) (model.Task, bool, error) {
	normalized, p := model.NormalizeSN(sn)
	if p != nil {
		return model.Task{}, false, nil
	}
	task, found, err := s.repo.FindOpenTask(ctx, normalized)
	if err != nil || !found {
		return model.Task{}, false, err
	}
	if task.State != model.TaskBooting && task.State != model.TaskInstalling {
		return model.Task{}, false, nil
	}
	return task, true, nil
}

// GetAuthorized 的八项应用约定唯一连接 docs/contracts/machine-execution.md「构造、凭据与授权」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (s *Service) GetAuthorized(ctx context.Context, id, token string, finalize bool) (model.Task, error) {
	task, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return model.Task{}, err
	}
	scope := Install
	if finalize {
		scope = Finalize
	}
	if err := s.Authorize(task, token, scope); err != nil {
		return model.Task{}, err
	}
	return task, nil
}
// Answer 的八项应用约定唯一连接 docs/contracts/machine-execution.md「引导和应答」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (s *Service) Answer(ctx context.Context, id, token string) (model.Task, error) {
	for range 8 {
		task, err := s.GetAuthorized(ctx, id, token, false)
		if err != nil {
			return model.Task{}, err
		}
		if task.State == model.TaskInstalling {
			return task, nil
		}
		next := clone(task)
		at := s.now()
		next.State = model.TaskInstalling
		stage(next.Execution, "packages", at, 3600)
		next, err = s.commit(ctx, task, next, repository.NewEvent{At: at, Kind: "machine.installing", Key: attemptKey(next, "installing")}, repository.Audit{})
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		return next, err
	}
	return model.Task{}, repository.ErrConflict
}
func finalSignal(sig Signal) bool {
	return sig.Timing == catalog.TimingFirstboot || sig.Phase == "booted" || sig.Phase == "init-start" || sig.Phase == "init-done" || sig.Phase == "ready"
}
// Signal 的八项应用约定唯一连接 docs/contracts/machine-execution.md「信号与超时扫描」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (s *Service) Signal(ctx context.Context, id, token string, sig Signal) error {
	for range 8 {
		task, err := s.repo.GetTask(ctx, id)
		if err != nil {
			return err
		}
		scope := Install
		if finalSignal(sig) {
			scope = Finalize
		}
		if err := s.authorizeCredential(task, token, scope); err != nil {
			return err
		}
		if scope == Install && task.State.Finished() {
			return ErrCredential
		}
		at := s.now()
		next, duplicate, err := advance(task, sig, at)
		if err != nil || duplicate {
			return err
		}
		event := repository.NewEvent{At: at, Kind: "machine." + sig.Phase, Key: attemptKey(next, signalKey(sig)), Fields: map[string]string{"slot": strconv.Itoa(sig.Slot), "timing": string(sig.Timing), "instance": strconv.Itoa(sig.Instance), "digest": sig.Digest, "rc": strconv.Itoa(sig.RC), "log_base64": base64.StdEncoding.EncodeToString(model.SafeLogTail(sig.Body))}}
		// Event kinds use dot identifiers; phases are canonical HTTP hyphen words.
		switch sig.Phase {
		case "slot-start":
			event.Kind = "slot.start"
		case "slot-end":
			event.Kind = "slot.end"
		case "hook-start":
			event.Kind = "hook.start"
		case "hook-report":
			event.Kind = "hook.report"
		case "init-start":
			event.Kind = "machine.init_start"
		case "init-done":
			event.Kind = "machine.init_done"
		}
		_, err = s.commit(ctx, task, next, event, repository.Audit{})
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		return err
	}
	return repository.ErrConflict
}

// CanCancel 的八项应用约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (*Service) CanCancel(t model.Task) []model.Problem { return CanCancel(t) }
// CanRetry 的八项应用约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (*Service) CanRetry(t model.Task) []model.Problem  { return CanRetry(t) }

// User controls use the exact revision the user reviewed. Conflicts require refresh.
func (s *Service) control(ctx context.Context, task model.Task, audit repository.Audit, retry bool) error {
	if err := repository.CheckAudit(audit); err != nil {
		return err
	}
	latest, err := s.repo.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if latest.Rev != task.Rev || latest.State != task.State {
		return repository.ErrConflict
	}
	problems := CanCancel(latest)
	if retry {
		problems = CanRetry(latest)
	}
	if len(problems) > 0 {
		return &repository.ProblemsError{Kind: repository.ErrRejected, Problems: problems}
	}
	next := clone(latest)
	at := s.now()
	kind := "cancelled"
	if next.Execution == nil {
		next.Execution = runtime(at, 1)
	}
	if retry {
		attempt := next.Execution.Attempt + 1
		next.Execution = runtime(at, attempt)
		next.State = model.TaskPending
		kind = "retry"
	} else {
		next.State = model.TaskCancelled
		next.Execution.Deadline = 0
	}
	_, err = s.commit(ctx, latest, next, repository.NewEvent{At: at, Kind: "machine." + kind, Key: attemptKey(next, kind)}, audit)
	return err
}
// Cancel 的八项应用约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (s *Service) Cancel(ctx context.Context, t model.Task, a repository.Audit) error {
	return s.control(ctx, t, a, false)
}
// Retry 的八项应用约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (s *Service) Retry(ctx context.Context, t model.Task, a repository.Audit) error {
	return s.control(ctx, t, a, true)
}

// Sweep reads durable state on every invocation, including after process restart.
// Sweep 的八项应用约定唯一连接 docs/contracts/machine-execution.md「信号与超时扫描」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (s *Service) Sweep(ctx context.Context) error {
	q := repository.TaskQuery{States: []model.TaskState{model.TaskBooting, model.TaskInstalling, model.TaskRebooting, model.TaskInitializing}, Limit: 100}
	for {
		page, err := s.repo.ListTasks(ctx, q)
		if err != nil {
			return err
		}
		for _, task := range page.Items {
			at := s.now()
			if task.Execution == nil || task.Execution.Deadline == 0 || at < task.Execution.Deadline {
				continue
			}
			next := clone(task)
			next.State = model.TaskFailed
			next.Execution.Deadline = 0
			_, err := s.commit(ctx, task, next, repository.NewEvent{At: at, Kind: "machine.timeout", Key: attemptKey(task, "timeout"), Fields: map[string]string{"state": string(task.State), "stage": task.Execution.Stage}}, repository.Audit{})
			if err != nil && !errors.Is(err, repository.ErrConflict) {
				return err
			}
		}
		if page.Next == "" {
			return nil
		}
		q.Cursor = page.Next
	}
}

// Credentials 属于执行模块，供可信机器协议入口取得当前尝试的两项凭据。
// 输入为原任务，不规范化、不另加大小限制；无Execution时两项均为空字符串。
// 成功始终先安装、后收尾，逐项委托Token；不重新校验状态、有效期或调用者凭据。
// 无错误或原因代码；不读仓储、不写任务，无事务、版本冲突与提交复查。
// 相同任务和服务密钥可重复调用，并发只读；调用期间不得修改任务，无新重试规则。
// 无审计、通知、上下文或超时，不新增取消检查。
func (s *Service) Credentials(task model.Task) (string, string) {
	return s.Token(task, Install), s.Token(task, Finalize)
}
