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
func (g *gateway) BootMenu(ctx context.Context, in BootInput) (BootResult, error) {
	task, found, err := g.deps.Service.Boot(ctx, in.SN, in.Arch, in.Firmware)
	if err != nil {
		return BootResult{}, gatewayFailure(LookupUnavailable, err)
	}
	if !found {
		return BootResult{}, nil
	}
	body, err := g.deps.Renderer.Render(ctx, port.RenderRequest{Task: task, Kind: catalog.FileMenu, Values: g.values(task)})
	if err != nil {
		return BootResult{}, gatewayFailure(RenderRejected, err)
	}
	return BootResult{Body: body, Found: true}, nil
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
