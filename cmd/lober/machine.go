package main

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/render"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/events"
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
	*execution.Service
	logger *slog.Logger
}

func (a machineAdapter) Credentials(task model.Task) (string, string) {
	return a.Token(task, execution.Install), a.Token(task, execution.Finalize)
}
func (a machineAdapter) Signal(ctx context.Context, id, token string, sig machine.Signal) error {
	start := time.Now()
	err := a.Service.Signal(ctx, id, token, execution.Signal{Phase: sig.Phase, Slot: sig.Slot, Timing: sig.Timing, Instance: sig.Instance, Digest: sig.Digest, RC: sig.RC, Body: sig.Body})
	a.logger.Debug("机器信号处理", "operation", "machine.signal", "task", id, "duration", time.Since(start), "accepted", err == nil)
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
	key, err := machineSigningKey(keyFile)
	if err != nil {
		return nil, errors.New("无法安全读取机器签名密钥")
	}
	engine, err := execution.New(st, key, func() int64 { return clock() }, hub)
	if err != nil {
		return nil, err
	}
	collector := events.New(st, func() int64 { return clock() }, hub)
	renderer := func(r machine.Request) ([]byte, error) {
		fw := catalog.Firmware("")
		if r.Task.Execution != nil {
			fw = r.Task.Execution.Firmware
		}
		original := ""
		if r.Task.Snapshot.Format == "esxi-ks" && r.Kind == catalog.FileAux {
			var err error
			original, err = iso.NewBootConfigReader(cfg.ISO.Artifacts).ReadESXiBootConfig(r.Context, r.Task.Snapshot.ISO)
			if err != nil {
				return nil, errors.New("immutable boot configuration unavailable")
			}
		}
		body, problems, err := render.Render(render.Request{Snapshot: r.Task.Snapshot, Firmware: fw, ESXiBootConfig: original, Values: r.Values, Kind: r.Kind, Slot: r.Slot})
		if err != nil || len(problems) != 0 {
			return nil, errors.New("machine rendering rejected")
		}
		return body, nil
	}
	observer := func(task model.Task, address string) {
		if task.Execution == nil {
			return
		}
		life := int64(5400)
		if task.Execution.ReachedReboot {
			life = 86400
		}
		collector.ObserveIP(task.ID, task.Execution.Attempt, address, task.Execution.IssuedAt+life)
	}
	handler := machine.New(machineAdapter{engine, logger}, renderer, machine.Endpoints{Gateway: cfg.Preview.Gateway, Artifacts: cfg.Preview.Artifacts, SMB: cfg.Preview.SMB, SyslogAddr: cfg.Preview.SyslogAddr}, observer)
	return &machineBundle{Service: engine, Handler: handler, Logs: collector}, nil
}
func machineSigningKey(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("key path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return machineSigningKey(path)
			}
			return nil, err
		}
		_, writeErr := f.Write(key)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return nil, errors.New("key write failed")
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != 32 {
		return nil, errors.New("key must be a private regular 32-byte file")
	}
	return os.ReadFile(path)
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
