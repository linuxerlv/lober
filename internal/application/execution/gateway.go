package execution

import (
	"context"
	"errors"
	"reflect"

	"github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/catalog"
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
)

type gateway struct{ deps GatewayDeps }

// NewGateway 的八项构造约定唯一连接 docs/contracts/machine-execution.md「机器交付入口」本方法行及共同约定。
func NewGateway(deps GatewayDeps) (Gateway, error) {
	if deps.Service == nil || nilRenderer(deps.Renderer) {
		return nil, errors.New("invalid machine gateway configuration")
	}
	return &gateway{deps: deps}, nil
}
func nilRenderer(r port.FrozenRenderer) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
func gatewayFailure(kind GatewayFailureKind, err error) error {
	return &GatewayError{Kind: kind, Cause: err}
}
func (g *gateway) values(task model.Task) map[catalog.SystemValue]string {
	install, final := g.deps.Service.Credentials(task)
	e := g.deps.Endpoints
	return map[catalog.SystemValue]string{"gateway": e.Gateway, "artifacts": e.Artifacts, "smb": e.SMB, "syslog-addr": e.SyslogAddr, "task": task.ID, "sn": task.HostSN, "token": install, "finalize-token": final}
}

// BootMenu 的八项应用约定唯一连接 Gateway.BootMenu 方法旁引用的唯一来源。
//
// 顺序：先按 SN 找打开的任务。找到了就照旧给安装菜单。找不到时再看这台机器该不该采集——
// 采集只对「登记过、没任务、有人点过更新配置」的主机开放，其余照旧从本地盘启动。
// 采集检查排在任务检查之后：有任务时绝不进采集，否则装到一半的机器会被拉去跑采集。
func (g *gateway) BootMenu(ctx context.Context, in BootInput) (BootResult, error) {
	task, found, err := g.deps.Service.Boot(ctx, in.SN, in.Arch, in.Firmware)
	if err != nil {
		return BootResult{}, gatewayFailure(LookupUnavailable, err)
	}
	if !found {
		return g.inventoryMenu(ctx, in.SN)
	}
	body, err := g.deps.Renderer.Render(ctx, port.RenderRequest{Task: task, Kind: catalog.FileMenu, Values: g.values(task)})
	if err != nil {
		return BootResult{}, gatewayFailure(RenderRejected, err)
	}
	return BootResult{Body: body, Found: true}, nil
}

// inventoryMenu 在没有打开的任务时决定给不给采集菜单。给不出就返回 Found=false，
// 机器按既有约定从本地盘启动。
func (g *gateway) inventoryMenu(ctx context.Context, sn string) (BootResult, error) {
	boot := g.deps.Inventory
	if boot == nil {
		return BootResult{}, nil
	}
	pending, err := boot.Pending(ctx, sn)
	if err != nil {
		return BootResult{}, gatewayFailure(LookupUnavailable, err)
	}
	if !pending {
		return BootResult{}, nil
	}
	token, ok := boot.Token(sn)
	if !ok {
		// 令牌发不出来（随机源不可用）时不给菜单：给了也没有可用的上报凭据，
		// 机器会跑完采集却传不上来。
		return BootResult{}, nil
	}
	body, err := boot.Menu(sn, token)
	if err != nil {
		return BootResult{}, gatewayFailure(RenderRejected, err)
	}
	if len(body) == 0 {
		return BootResult{}, nil
	}
	return BootResult{Body: body, Found: true}, nil
}

// InventoryReport 的八项应用约定唯一连接 docs/contracts/machine-execution.md「采集上报入口」。
//
// 不观察机器地址：采集时机器用的是装机网络的地址，对业务没有意义，进来只会污染
// syslog 的任务归属表。
func (g *gateway) InventoryReport(ctx context.Context, in InventoryInput) error {
	boot := g.deps.Inventory
	if boot == nil {
		return gatewayFailure(NotSupported, ErrInventoryUnsupported)
	}
	err := boot.Submit(ctx, in.SN, in.Token, in.Body)
	if err == nil {
		return nil
	}
	// 分类只在这里做一次：采集模块的错误不暴露给 HTTP 层，免得接口层依赖另一个应用模块。
	switch {
	case errors.Is(err, ErrInventoryUnauthorized):
		return gatewayFailure(Unauthorized, err)
	case errors.Is(err, ErrInventoryUnknownHost):
		return gatewayFailure(FileNotFound, err)
	case errors.Is(err, ErrInventoryBusy):
		return gatewayFailure(InventoryBusy, err)
	case errors.Is(err, ErrInventoryRejected):
		return gatewayFailure(ReportRejected, err)
	default:
		return gatewayFailure(LookupUnavailable, err)
	}
}

// InstallerAnswer 的八项应用约定唯一连接 Gateway.InstallerAnswer 方法旁引用的唯一来源。
func (g *gateway) InstallerAnswer(ctx context.Context, in InstallerAnswerInput) (BootResult, error) {
	task, found, err := g.deps.Service.Preseed(ctx, in.SN)
	if err != nil {
		return BootResult{}, gatewayFailure(LookupUnavailable, err)
	}
	if !found || !executionrules.InstallerAnswerAvailable(task) {
		return BootResult{}, nil
	}
	body, err := g.deps.Renderer.Render(ctx, port.RenderRequest{Task: task, Kind: catalog.FileAnswer, Values: g.values(task)})
	if err != nil {
		return BootResult{}, gatewayFailure(RenderRejected, err)
	}
	install, _ := g.deps.Service.Credentials(task)
	if _, err := g.deps.Service.Answer(ctx, task.ID, install); err != nil {
		return BootResult{}, gatewayFailure(AnswerConflict, err)
	}
	if g.deps.Observe != nil {
		g.deps.Observe(task, in.RemoteAddr)
	}
	return BootResult{Body: body, Found: true}, nil
}

// File 的八项应用约定唯一连接 Gateway.File 方法旁引用的唯一来源。
func (g *gateway) File(ctx context.Context, in FileInput) ([]byte, error) {
	task, err := g.deps.Service.GetAuthorized(ctx, in.TaskID, in.Token, false)
	if err != nil {
		return nil, gatewayFailure(Unauthorized, err)
	}
	if g.deps.Observe != nil {
		g.deps.Observe(task, in.RemoteAddr)
	}
	file, err := executionrules.SelectFile(task.Snapshot, in.Name)
	if err != nil {
		if errors.Is(err, executionrules.ErrFileNotFound) {
			return nil, gatewayFailure(FileNotFound, err)
		}
		return nil, gatewayFailure(RenderRejected, err)
	}
	body := file.Body
	if !file.Hook {
		body, err = g.deps.Renderer.Render(ctx, port.RenderRequest{Task: task, Kind: file.Kind, Slot: file.Slot, Values: g.values(task)})
		if err != nil {
			return nil, gatewayFailure(RenderRejected, err)
		}
	}
	if file.Kind == catalog.FileAnswer {
		if _, err := g.deps.Service.Answer(ctx, in.TaskID, in.Token); err != nil {
			return nil, gatewayFailure(AnswerConflict, err)
		}
	}
	return body, nil
}

// Signal 的八项应用约定唯一连接 Gateway.Signal 方法旁引用的唯一来源。
func (g *gateway) Signal(ctx context.Context, in SignalInput) error {
	if g.deps.Observe != nil {
		if task, err := g.deps.Service.GetAuthorized(ctx, in.TaskID, in.Token, executionrules.FinalSignal(in.Signal)); err == nil {
			g.deps.Observe(task, in.RemoteAddr)
		}
	}
	if err := g.deps.Service.Signal(ctx, in.TaskID, in.Token, in.Signal); err != nil {
		return gatewayFailure(SignalConflict, err)
	}
	return nil
}
