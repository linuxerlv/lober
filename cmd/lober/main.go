package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lober/lober/internal/console"
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
	a, cleanup, err := newApp(ctx, cfg, console.Clock(func() int64 { return time.Now().Unix() }), logger)
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
	srv := &http.Server{Addr: a.Config.Listen, Handler: console.WithFrontend(a.Console.Engine), ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
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
