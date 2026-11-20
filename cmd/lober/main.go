package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/lober/lober/internal/application/ports"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		slog.Error("lober 启动或运行失败", "error", err)
		os.Exit(1)
	}
}
func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("lober", flag.ContinueOnError)
	flags.SetOutput(output)
	file := flags.String("config", "/etc/lober/lober.yaml", "YAML configuration path")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("只能提供一份配置路径")
	}
	if flags.NArg() == 1 {
		*file = flags.Arg(0)
	}
	cfg, err := loadConfig(*file)
	if err != nil {
		return err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		return errors.New("日志级别不合法")
	}
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	a, cleanup, err := newApp(ctx, cfg, ports.Clock(func() int64 { return time.Now().Unix() }), logger)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := a.Initialise(); err != nil {
		return err
	}
	if err := a.Supervisor.Start(); err != nil {
		return err
	}
	return serve(ctx, a)
}
func serve(ctx context.Context, a *app) error {
	listener, err := net.Listen("tcp", a.Config.Listen)
	if err != nil {
		return err
	}
	return serveListener(ctx, a, listener)
}
func serveListener(ctx context.Context, a *app, listener net.Listener) error {
	defer listener.Close()
	recoveryCtx, stopRecovery := context.WithCancel(ctx)
	defer stopRecovery()
	var packet net.PacketConn
	var stream net.Listener
	var err error
	if a.Machine != nil && a.Machine.Logs != nil && a.Config.Machine.SyslogListen != "" {
		packet, stream, err = listenSyslog(a.Config.Machine.SyslogListen)
		if err != nil {
			return errors.New("无法监听安装器 syslog 地址")
		}
	}
	recoveryDone := make(chan struct{})
	go func() { defer close(recoveryDone); a.runMachineRecovery(recoveryCtx, a.Logger) }()
	defer func() { stopRecovery(); <-recoveryDone }()
	if packet != nil {
		logDone := make(chan struct{})
		streamDone := make(chan struct{})
		go func() { defer close(logDone); a.runSyslog(recoveryCtx, packet) }()
		go func() { defer close(streamDone); a.runSyslogTCP(recoveryCtx, stream) }()
		defer func() {
			stopRecovery()
			packet.Close()
			stream.Close()
			<-logDone
			<-streamDone
		}()
	}
	srv := &http.Server{Addr: a.Config.Listen, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	result := make(chan error, 1)
	go func() {
		a.Logger.Info("控制台开始监听", "address", a.Config.Listen)
		if a.Config.TLS.Cert != "" {
			result <- srv.ServeTLS(listener, a.Config.TLS.Cert, a.Config.TLS.Key)
		} else {
			result <- srv.Serve(listener)
		}
	}()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("HTTP 服务停止：%w", err)
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			srv.Close()
			return err
		}
		return nil
	}
}
