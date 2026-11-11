package execution

// 本文件的仓储口只供可信应用或启动组装调用；仓储不认证网页用户或机器令牌。
// 身份、权限或凭据检查的有无及先后，由各消费入口的操作约定负责。
// Boot/Preseed按SN读取开放任务，不先验机器令牌；GetAuthorized先GetTask再验令牌。
// 输入规范化由调用应用负责；排序、空值、错误优先级、重复/版本冲突、锁内复查、原子审计及取消均沿用所指保存契约。
// 只读方法不写业务或审计、不发通知，故无原子写与提交复查；重复读取不保证多次或多页快照。

import (
	"context"

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

// clock 是三个角色共用的时钟。测试改一处，三个角色看到的时间一起变。
type clock struct {
	now func() int64
}

func (c *clock) Now() int64 { return c.now() }

// core 是三个角色共用的保存路径：仓储、时钟与提交后的通知。
// 拆成独立结构体后，机器协议、用户控制和超时扫描各自只拿到自己需要的部分。
type core struct {
	repo     Repository
	clock    *clock
	notifier Notifier
}

func (c core) commit(ctx context.Context, before, after model.Task, event repository.NewEvent, audit repository.Audit) (model.Task, error) {
	next, _, err := c.repo.UpdateTaskExecution(ctx, before.ID, repository.TaskExpect{State: before.State, Rev: before.Rev}, after.State, after.Execution, []repository.NewEvent{event}, audit)
	if err == nil && c.notifier != nil {
		c.notifier.Publish(next.ID)
	}
	return next, err
}

// Service 把三个角色拼在一起，保留原来的调用形状。
// 新的入口应当只依赖 Machine、Control 或 Sweeper 中自己用得到的那一个。
type Service struct {
	*Machine
	*Control
	*Sweeper
}

// New 的八项应用约定唯一连接 docs/contracts/machine-execution.md「构造、凭据与授权」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func New(repo Repository, key []byte, now func() int64, notifier Notifier) (*Service, error) {
	sharedClock := &clock{now: now}
	credentials, err := newCredentials(key, sharedClock)
	if err != nil {
		return nil, err
	}
	shared := core{repo: repo, clock: sharedClock, notifier: notifier}
	return &Service{
		Machine: &Machine{core: shared, auth: credentials},
		Control: &Control{core: shared},
		Sweeper: &Sweeper{core: shared},
	}, nil
}
