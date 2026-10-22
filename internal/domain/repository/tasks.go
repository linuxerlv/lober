package repository

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
)

// Tasks 是装机任务（C27–C31、C33）。仓储保证约束，并保证并发下一次推进只生效一次；一步推进合不合规矩由第 7 层
// 的状态机判断（spec §6.2 第 4 条）。
type Tasks interface {
	// CreateTasks 一次建出一批任务（C29、C30、C31），每个任务写一条审计：
	//  1. 先查入参（CheckNewTasks），不合格报 ErrInvalid。快照里的每个串都要存得下（model.StorableText），存不下的
	//     固化不下来：由 CheckNewTasks 在写之前报出，不等到 PostgreSQL 存不进去。
	//  2. 开事务，按 SN 升序给涉及的主机加写锁，对 ISO、模板版本和它引用的 hook 版本加读锁。拿锁的顺序固定，
	//     两批同时提交时就不会互相等死。ISO 或模板版本不存在报 ErrNotFound：按键找不到，准入无从判起。
	//  3. 用锁住之后读到的数据重跑准入判定（BatchProblems），没登记的 SN 报 host_not_registered。判定用的是任务里
	//     钉住的模板版本（TemplateID、TemplateVersion），不另取当前的已审批版本：已被新版本取代、但仍是已审批的
	//     旧版本照样通过。向导需要当前版本（C23）时使用 CurrentTaskCreator。任何一处有
	//     问题，整批回滚，报 BatchError，不写任务，也不写审计。
	//  4. 全部通过才写：ID 用 NewID 生成，State 为 pending，Rev 为 1。并发下撞上「一台主机一个未结束任务」的
	//     约束时，同样回滚并报 BatchError，问题是 host_has_open_task。
	CreateTasks(ctx context.Context, ts []model.Task, a Audit) ([]model.Task, error)
	GetTask(ctx context.Context, id string) (model.Task, error)
	// FindOpenTask 找这台主机未结束的那个任务（C31 保证最多一个），给第 7 层引导时用；没有时第二个返回值为
	// false。sn 要是规范化之后的写法（与 model.NormalizeSN 的结果相同），否则报 ErrInvalid（CheckSN），不当成没有
	// 任务：按 SN 查找的方法里只有它查。网关不能把漏了规范化当成「没有任务」，否则机器会从本地盘启动、任务一直等着
	// （见 Hosts 的说明）。
	FindOpenTask(ctx context.Context, sn string) (model.Task, bool, error)
	ListTasks(ctx context.Context, q TaskQuery) (Page[model.Task], error)
	// TransitionTask 是系统发起的条件推进，不审计：当前的状态与 Rev 都等于 expect 才写入，否则报 ErrConflict；
	// 成功后 Rev 加 1。events 与状态在同一个事务里写，规则同 AppendEvents。目标状态不合法报 ErrInvalid。
	// 推进到未结束的状态时，这台主机已有另一个未结束的任务（比如失败的任务重试时，期间又建了新任务），报
	// ProblemsError(ErrRejected)，问题是 host_has_open_task。
	TransitionTask(ctx context.Context, id string, expect TaskExpect, to model.TaskState, events []NewEvent) (model.Task, []Appended, error)
	// TransitionTaskByUser 是用户发起的推进（取消、重试），要审计，其余与 TransitionTask 相同。
	TransitionTaskByUser(ctx context.Context, id string, expect TaskExpect, to model.TaskState, events []NewEvent, a Audit) (model.Task, []Appended, error)
}
