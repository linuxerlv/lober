// 本文件是进程入口：解析 flag、加载配置、装配、信号驱动的优雅退出。
// 对应实现计划任务 5，依据 SPEC §1.6。
//
// 流程：解析 flag → 设置日志 → 加载配置 → 装配 → 注册信号处理 → 跑启动
// 自检 → 若是校验模式则到此返回 → 起扫描循环 goroutine → 起三个 listener
// 并阻塞。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lober/lober/internal/config"
)

// version/commit/buildTime 由链接期注入（-ldflags），气隙环境里排查问题
// 第一个问题永远是「装的是哪个版本」（计划任务 5「设计要点」）。
//
//	go build -ldflags "-X main.version=v0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run 是 main 的可测试版本：接受参数切片、返回退出码，不直接调用
// os.Exit——测试需要断言退出码而不是让进程真的退出。
func run(args []string) int {
	fs := flag.NewFlagSet("lober", flag.ContinueOnError)
	var (
		configPath  = fs.String("config", "", "配置文件路径（YAML）")
		showVersion = fs.Bool("version", false, "打印版本信息后退出")
		checkOnly   = fs.Bool("check", false, "只加载并校验配置、跑装配与启动自检，不起服务（供 systemd ExecStartPre 使用）")
		logLevel    = fs.String("log-level", "info", "日志级别：debug/info/warn/error")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		fmt.Printf("lober %s (commit=%s, built=%s)\n", version, commit, buildTime)
		return 0
	}

	setupLogging(*logLevel)

	if *configPath == "" {
		slog.Error("必须指定 -config 参数")
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("加载配置失败", slog.Any("error", err))
		return 1
	}

	app, err := buildApp(cfg)
	if err != nil {
		slog.Error("装配失败", slog.Any("error", err))
		return 1
	}
	// *gorm.DB 没有 Close 方法（它是可复制的会话对象，不持有连接），
	// 关闭要落到底层的连接池上，closeDB 负责取出来关。
	defer closeDB(app.DB)

	// 启动自检：制品完整性（任务 2）。发现问题只打日志，不阻止服务启动
	// （docs/contracts/filesystem.md 硬约束 3：一个坏掉的制品集不该拖累
	// 其它制品集与整个装机服务）。
	runPreflight(context.Background(), app)

	if *checkOnly {
		// 校验模式到此为止：配置能解析、能通过校验、装配能成功、启动自检
		// 已经跑过一遍——这是 systemd ExecStartPre 需要的全部信息（配置
		// 写错时在这里失败，比起服务后第一个请求才崩好查，计划任务 5）。
		slog.Info("校验模式：配置与装配均通过，不起服务")
		return 0
	}

	return serve(app, cfg)
}

// loadConfig 打开配置文件、解析、校验。三步分开报错，让运维能立刻分清
// 是「文件打不开」「YAML 语法错」还是「语义危险但语法合法」。
func loadConfig(path string) (*config.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开配置文件失败（path=%s): %w", path, err)
	}
	defer f.Close()

	cfg, err := config.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置校验未通过: %w", err)
	}
	return cfg, nil
}

// serve 起扫描循环与三个 listener，阻塞直到收到 SIGTERM/SIGINT 或某个
// listener 出错，随后走优雅关闭。
//
// 信号处理同时接 SIGTERM（systemd stop）与 SIGINT（终端 Ctrl-C），计划
// 任务 5 明确要求。
func serve(app *App, cfg *config.Config) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	scanDone := runScanLoop(ctx, app.StateMachine, cfg.Scan.Interval)

	if err := runServers(ctx, app, cfg); err != nil {
		slog.Error("服务运行期出错", slog.Any("error", err))
		<-scanDone
		return 1
	}

	<-scanDone
	slog.Info("lober 已优雅退出")
	return 0
}

// setupLogging 按 -log-level 设置全局默认 slog handler，输出到标准错误
// 交给 journald 收，本进程不自己管日志轮转（计划任务 5「设计要点」：
// 那是 systemd 的事）。level 取值不合法时兜底为 info，不让一个拼写错误
// 的 flag 值导致进程启动失败——日志级别配错的后果只是"话多了或话少了"，
// 不是需要拒绝启动的安全边界。
func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}
