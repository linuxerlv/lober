package execution

import (
	"context"

	"github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/catalog"
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
)

type BootInput struct {
	SN       string
	Arch     catalog.Arch
	Firmware catalog.Firmware
}
type InstallerAnswerInput struct{ SN, RemoteAddr string }
type FileInput struct{ TaskID, Token, Name, RemoteAddr string }
type SignalInput struct {
	TaskID, Token, RemoteAddr string
	Signal                    executionrules.Signal
}
type BootResult struct {
	Body  []byte
	Found bool
}

type Gateway interface {
	// BootMenu 的八项应用约定唯一连接 docs/contracts/machine-execution.md「机器交付入口」本方法行及共同约定。
	BootMenu(context.Context, BootInput) (BootResult, error)
	// InstallerAnswer 的八项应用约定唯一连接 docs/contracts/machine-execution.md「机器交付入口」本方法行及共同约定。
	InstallerAnswer(context.Context, InstallerAnswerInput) (BootResult, error)
	// File 的八项应用约定唯一连接 docs/contracts/machine-execution.md「机器交付入口」本方法行及共同约定。
	File(context.Context, FileInput) ([]byte, error)
	// Signal 的八项应用约定唯一连接 docs/contracts/machine-execution.md「机器交付入口」本方法行及共同约定。
	Signal(context.Context, SignalInput) error
	// InventoryReport 接收采集镜像的硬件上报。没接采集能力时一律 NotFound。
	InventoryReport(context.Context, InventoryInput) error
}

// InventoryInput 是一次采集上报：SN、令牌与正文。
type InventoryInput struct {
	SN, Token  string
	Body       []byte
	RemoteAddr string
}
type Endpoints struct{ Gateway, Artifacts, SMB, SyslogAddr string }

// InventoryBoot 是采集能力的全部接口，由组装点接到采集模块上。
//
// 执行模块不直接依赖采集模块：两者都是应用模块，互相依赖会让架构守卫第一层就报错。
// 这里声明成接口，组装点交进来的实现是采集服务本身。
//
// 全部方法为空时网关不给采集菜单，机器照旧从本地盘启动——这是没启用采集时的正常状态。
type InventoryBoot interface {
	// Pending 报告这台主机此刻该不该进采集镜像。它自己会查登记、请求标记与进行中超时。
	Pending(context.Context, string) (bool, error)
	// Token 为这次采集发一个一次性令牌。
	Token(string) (string, bool)
	// Menu 产出采集菜单的 iPXE 文本。
	Menu(sn, token string) ([]byte, error)
	// Submit 接收一份上报的正文，成功时返回零值错误。错误分类由采集模块定义。
	Submit(context.Context, string, string, []byte) error
}

// MachineService 是网关真正用到的机器协议能力。
// 网关不需要取消、重试或超时扫描，所以这里只声明自己用得到的六个方法。
type MachineService interface {
	Boot(context.Context, string, catalog.Arch, catalog.Firmware) (model.Task, bool, error)
	Preseed(context.Context, string) (model.Task, bool, error)
	Answer(context.Context, string, string) (model.Task, error)
	Signal(context.Context, string, string, executionrules.Signal) error
	GetAuthorized(context.Context, string, string, bool) (model.Task, error)
	Credentials(model.Task) (string, string)
}

var _ MachineService = (*Machine)(nil)

type GatewayDeps struct {
	Service   MachineService
	Renderer  port.FrozenRenderer
	Endpoints Endpoints
	Observe   func(model.Task, string)
	// Inventory 可空：没接采集能力时，引导一律按「没有采集」处理，上报路由返回 404。
	Inventory InventoryBoot
}
type GatewayFailureKind string

const (
	LookupUnavailable GatewayFailureKind = "lookup-unavailable"
	RenderRejected    GatewayFailureKind = "render-rejected"
	Unauthorized      GatewayFailureKind = "unauthorized"
	FileNotFound      GatewayFailureKind = "file-not-found"
	AnswerConflict    GatewayFailureKind = "answer-conflict"
	SignalConflict    GatewayFailureKind = "signal-conflict"
	// ReportRejected 是采集上报被拒：值不可信。与 Unauthorized 分开，调用方按 422 回。
	ReportRejected GatewayFailureKind = "report-rejected"
	// NotSupported 是没接采集能力。调用方回 404，与「路径不存在」同一个响应。
	NotSupported GatewayFailureKind = "not-supported"
	// InventoryBusy 是主机此刻有打开的任务，不接受采集。
	InventoryBusy GatewayFailureKind = "inventory-busy"
)

type GatewayError struct {
	Kind  GatewayFailureKind
	Cause error
}

// Error 的八项纯错误约定唯一连接 docs/contracts/machine-execution.md「机器交付错误」。
func (e *GatewayError) Error() string { return string(e.Kind) }

// Unwrap 的八项纯错误约定唯一连接 docs/contracts/machine-execution.md「机器交付错误」。
func (e *GatewayError) Unwrap() error { return e.Cause }
