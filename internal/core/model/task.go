package model

import "github.com/lober/lober/internal/core/catalog"

// TaskState 是装机任务的状态。字面量会存进数据库，发布后不能改名。状态只进不退（C33），
// 由第 7 层的状态机推进；本层只定下取值，并判断任务结束了没有。
type TaskState string

const (
	TaskPending      TaskState = "pending"      // 等待引导：任务一建成就是这个状态
	TaskBooting      TaskState = "booting"      // 引导中
	TaskInstalling   TaskState = "installing"   // 安装中
	TaskRebooting    TaskState = "rebooting"    // 重启中：安装器已完成，之后的失败不能自动重装（C34）
	TaskInitializing TaskState = "initializing" // 初始化中：新系统起来了，首次开机的脚本在跑
	TaskDone         TaskState = "done"         // 完成
	TaskFailed       TaskState = "failed"       // 失败
	TaskCancelled    TaskState = "cancelled"    // 已取消
)

// Finished 报告任务是否已结束：done、failed、cancelled 三种。一台主机同一时刻最多有一个未结束
// 的任务（C31），结束了才能再建。不认识的状态算未结束：宁可挡住这台主机再建任务，也不让它同时
// 有两个任务在装。
func (s TaskState) Finished() bool {
	switch s {
	case TaskDone, TaskFailed, TaskCancelled:
		return true
	}
	return false
}

// Valid 报告 s 是不是任务的状态，只认原样的写法（T7-2）。第 2 层的 CHECK 约束、第 7 层推进状态之前
// 的检查都用它，不另抄取值表。
func (s TaskState) Valid() bool {
	switch s {
	case TaskPending, TaskBooting, TaskInstalling, TaskRebooting, TaskInitializing, TaskDone, TaskFailed, TaskCancelled:
		return true
	}
	return false
}

// Task 是一个装机任务：一台主机、一个 ISO、一个模板版本，外加建成时固化的快照（C27、C29）。
// 第 2 层原样存它，字段名就是最终的名字；第 7 层会再加 token、引导次数、时机进度这类执行字段。
//
// HostSN、ISOID、TemplateID 与 TemplateVersion 和快照里的重复：第 2 层要按它们查任务（这台主机
// 有没有未结束的任务、这个版本还被哪些任务引用），不必为此解开快照。
type Task struct {
	// ID 经 {{sys:task}} 原样代入模板正文与片段（C18），所以必须是安全词（IsSafeWord），由第 2 层
	// 生成时保证（L-13b）：含空格、引号的 ID 会改变应答文件的结构。快照里不带它，快照构建查不到。
	ID              string
	HostSN          string
	ISOID           string
	TemplateID      string
	TemplateVersion int
	State           TaskState
	Snapshot        Snapshot
	CreatedBy       string
	CreatedAt       int64 // Unix 秒，由调用方传入：层 1 不碰时钟
}

// SnapshotSchema 是本层产出的快照格式版本。任务要保存很久，格式将来变了，渲染靠它认出旧快照，
// 而不是按新格式把它读错。
const SnapshotSchema = 1

// Snapshot 是任务建成时固化的渲染输入（C29），由 snapshot.Build 构建。渲染只读它，所以它必须
// 自足：主机、ISO、模板和 hook 之后怎么改，都不影响已建的任务。第 2 层按 JSON 存它，字段名就是
// 最终的名字。
type Snapshot struct {
	Schema   int
	Family   catalog.Family
	Series   catalog.Series // ISO 的系：每个 hook 取的都是覆盖它的实现（C27）
	Format   catalog.Format
	Host     SnapshotHost
	ISO      SnapshotISO
	Template SnapshotTemplate
	Slots    []SnapshotSlot // 按正文里出现的顺序
}

// SnapshotHost 是建任务那一刻的主机登记。属性整份留下：取自主机属性的参数值已经求进各个参数，
// 这里另记一份原样的，事后查得到这个任务当时是按什么登记装的。
type SnapshotHost struct {
	SN       string
	Arch     catalog.Arch     // 可空：主机没登记
	Firmware catalog.Firmware // 可空，同上
	Attrs    map[string]string
}

// SnapshotISO 是 ISO 记录里渲染要的部分。制品只记相对路径，完整地址由渲染按制品目录布局拼出。
type SnapshotISO struct {
	ID        string
	Name      string
	Version   string
	Arch      catalog.Arch
	Artifacts map[string]string // 角色 → 制品目录下的相对路径
}

// SnapshotTemplate 是模板版本的正文和模板参数的最终取值。渲染照正文展开占位符，插槽里放了
// 什么见 Snapshot.Slots。
type SnapshotTemplate struct {
	ID      string
	Version int
	Name    string
	Body    string
	Params  map[string]SnapshotValue // 键为参数名；没有值的可选参数不在其中
}

// SnapshotSlot 是正文里的一个插槽和放进它的实例。
type SnapshotSlot struct {
	// Seq 是插槽在正文里的序号，从 1 起。插槽名可以用中文，网关下发执行插槽的运行脚本时，
	// 地址里用它作 ASCII 标识。
	Seq       int
	Name      string
	Exec      bool
	Timing    catalog.Timing     // 仅执行插槽
	Instances []SnapshotInstance // 按装配顺序；装配表里没有实例的插槽为空
}

// SnapshotInstance 是插槽里的一个 hook 实例：钉住的 hook 版本、按 ISO 的系选中的实现，以及
// 这个实例的参数取值。
type SnapshotInstance struct {
	HookID      string
	HookVersion int
	HookName    string
	Kind        ImplKind
	Body        string
	// 下面四项只对脚本有意义，片段为零值：片段在渲染时就写进了应答文件，没有执行这一步。
	Interpreter catalog.Interpreter
	TimeoutSec  int                      // 生效的超时：实例的覆盖优先，否则取 hook 的默认值（C11）
	Required    bool                     // 生效的是否必需，规则同上
	Digest      string                   // 正文的 Impl.Digest：网关只对快照里钉住的摘要放行 hook 脚本
	Params      map[string]SnapshotValue // 键为参数名；没有值的可选参数不在其中
}

// SnapshotValue 是一个参数的最终取值，只记渲染要的类型、是否列表和规范形式的各项。不存 Value：
// 存它会把它的内部结构带进数据库。
type SnapshotValue struct {
	Type  ParamType
	List  bool
	Items []string // 规范形式，标量为单元素
}
