package main

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/execution"
	executionport "github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/events"
	"github.com/lober/lober/internal/infrastructure/machinekeys"
	"github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/interfaces/console"
	"github.com/lober/lober/internal/interfaces/machine"
)

type machineBundle struct {
	Service *execution.Service
	Handler http.Handler
	Logs    *events.Collector
}
type machineAdapter struct {
	execution.Gateway
	logger *slog.Logger
}

func (a machineAdapter) Signal(ctx context.Context, in execution.SignalInput) error {
	start := time.Now()
	err := a.Gateway.Signal(ctx, in)
	a.logger.Debug("机器信号处理", "operation", "machine.signal", "task", in.TaskID, "duration", time.Since(start), "accepted", err == nil)
	return err
}
func machineOf(st repository.Store, hub ports.Notifier, cfg config, clock ports.Clock, logger *slog.Logger) (*machineBundle, error) {
	if !cfg.Machine.Enabled {
		return &machineBundle{}, nil
	}
	for _, raw := range []string{cfg.Preview.Gateway, cfg.Preview.Artifacts} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("machine.enabled 需要有效的 preview.gateway 和 preview.artifacts HTTP 基地址")
		}
	}
	keyFile := cfg.Machine.KeyFile
	if keyFile == "" {
		keyFile = filepath.Join(cfg.ISO.Staging, "machine-signing.key")
	}
	key, err := machinekeys.LoadOrCreate(keyFile)
	if err != nil {
		return nil, errors.New("无法安全读取机器签名密钥")
	}
	engine, err := execution.New(st, key, func() int64 { return clock() }, hub)
	if err != nil {
		return nil, err
	}
	logs := execution.NewInstallerLogs(st, hub)
	collector := events.New(logs, func() int64 { return clock() })
	files := authoring.NewMachineRenderer(iso.NewBootConfigReader(cfg.ISO.Artifacts))
	renderer := executionport.RenderFunc(func(ctx context.Context, r executionport.RenderRequest) ([]byte, error) {
		return files.Render(ctx, authoring.MachineRenderRequest{Task: r.Task, Kind: r.Kind, Slot: r.Slot, Values: r.Values})
	})
	gateway, err := execution.NewGateway(execution.GatewayDeps{Service: engine, Renderer: renderer, Endpoints: execution.Endpoints{Gateway: cfg.Preview.Gateway, Artifacts: cfg.Preview.Artifacts, SMB: cfg.Preview.SMB, SyslogAddr: cfg.Preview.SyslogAddr}, Observe: execution.NewIPObserver(collector)})
	if err != nil {
		return nil, err
	}
	handler := machine.New(machineAdapter{gateway, logger})
	return &machineBundle{Service: engine, Handler: handler, Logs: collector}, nil
}
func (a *app) Handler() http.Handler {
	frontend := console.WithFrontend(a.Console.Engine)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") && a.Machine != nil && a.Machine.Handler != nil {
			a.Machine.Handler.ServeHTTP(w, r)
			return
		}
		frontend.ServeHTTP(w, r)
	})
}
func (a *app) runMachineRecovery(ctx context.Context, logger *slog.Logger) {
	if a.Machine == nil || a.Machine.Service == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := guardMachine(func() error { return a.Machine.Service.Sweep(ctx) }); err != nil && ctx.Err() == nil {
			logger.Error("机器任务超时恢复失败")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func guardMachine(work func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("machine background panic")
		}
	}()
	return work()
}
func (a *app) runSyslog(ctx context.Context, packet net.PacketConn) {
	body := make([]byte, 65536)
	for {
		n, remote, err := packet.ReadFrom(body)
		if err != nil {
			if ctx.Err() == nil {
				a.Logger.Error("安装器 syslog 监听停止")
			}
			return
		}
		err = guardMachine(func() error { return a.Machine.Logs.Receive(ctx, remote.String(), body[:n]) })
		if err != nil && ctx.Err() == nil {
			a.Logger.Warn("安装器 syslog 归档失败")
		}
	}
}
