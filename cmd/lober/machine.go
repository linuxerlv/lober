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
	"syscall"
	"time"

	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/execution"
	executionport "github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/application/inventory"
	domaininv "github.com/lober/lober/internal/domain/inventory"
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
	// 采集服务只造一次：网关要它的三项能力，HTTP 入口要它的 Submit。造两个实例会让
	// 「网关发的令牌」和「上报时查的令牌表」分家，采集一律 401。
	inv := inventoryServiceOf(st, clock)
	gateway, err := execution.NewGateway(execution.GatewayDeps{Service: engine, Renderer: renderer, Endpoints: execution.Endpoints{Gateway: cfg.Preview.Gateway, Artifacts: cfg.Preview.Artifacts, SMB: cfg.Preview.SMB, SyslogAddr: cfg.Preview.SyslogAddr}, Observe: execution.NewIPObserver(collector), Inventory: inventoryBoot(inv)})
	if err != nil {
		return nil, err
	}
	handler := machine.New(machineAdapter{gateway, logger})
	return &machineBundle{Service: engine, Handler: handler, Logs: collector}, nil
}

// inventoryAdapter 把采集服务接到执行网关的接口上。两个应用模块互不依赖，翻译在这里做。
type inventoryAdapter struct{ svc *inventory.Service }

func (a inventoryAdapter) Pending(ctx context.Context, sn string) (bool, error) {
	return a.svc.Pending(ctx, sn)
}

func (a inventoryAdapter) Token(sn string) (string, bool) { return a.svc.Issue(sn) }

func (a inventoryAdapter) Menu(sn, token string) ([]byte, error) { return a.svc.Menu(sn, token) }

// Submit 只把类别翻成执行模块的哨兵错误，不做别的判断。
func (a inventoryAdapter) Submit(ctx context.Context, sn, token string, body []byte) error {
	_, err := a.svc.Submit(ctx, sn, token, body)
	if err == nil {
		return nil
	}
	kind, ok := domaininv.KindOf(err)
	if !ok {
		return err
	}
	switch kind {
	case domaininv.KindUnauthorized:
		return execution.ErrInventoryUnauthorized
	case domaininv.KindUnknownHost:
		return execution.ErrInventoryUnknownHost
	case domaininv.KindBusy:
		return execution.ErrInventoryBusy
	case domaininv.KindRejected:
		return execution.ErrInventoryRejected
	}
	return err
}

// inventoryBoot 决定接不接采集能力：镜像路径没配全时返回 nil，网关据此不给采集菜单，
// 机器照旧从本地盘启动。这条让「没部署采集镜像」成为一个安全状态，而不是错误状态。
func inventoryBoot(inv *inventory.Service) execution.InventoryBoot {
	if inv == nil || !inv.Configured() {
		return nil
	}
	return inventoryAdapter{svc: inv}
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

func listenSyslog(address string) (net.PacketConn, net.Listener, error) {
	return listenSyslogWith(address, net.ListenPacket, net.Listen)
}

func listenSyslogWith(address string, packetListen func(string, string) (net.PacketConn, error), streamListen func(string, string) (net.Listener, error)) (net.PacketConn, net.Listener, error) {
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		packet, err := packetListen("udp", address)
		if err != nil {
			return nil, nil, err
		}
		// 使用 UDP 实际绑定的地址，端口为 0 时两种协议也必须共用同一端口。
		stream, err := streamListen("tcp", packet.LocalAddr().String())
		if err == nil {
			return packet, stream, nil
		}
		packet.Close()
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, nil, err
		}
		// 只重选原本由系统分配的端口，不改变用户指定的固定端口。
		_, port, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			return nil, nil, err
		}
		number, portErr := net.LookupPort("udp", port)
		if portErr != nil || number != 0 {
			return nil, nil, err
		}
		lastErr = err
	}
	return nil, nil, lastErr
}

func (a *app) runSyslogTCP(ctx context.Context, listener net.Listener) {
	err := a.Machine.Logs.ServeTCP(ctx, listener, func(error) {
		a.Logger.Warn("安装器 syslog 归档失败")
	})
	if err != nil && ctx.Err() == nil {
		a.Logger.Error("安装器 TCP syslog 监听停止")
	}
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
