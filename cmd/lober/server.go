// 本文件实现两个 HTTP listener 加一个 syslog UDP listener 的生命周期。
// 对应实现计划任务 4，依据 SPEC §1.1、§1.6。
//
// 网关与控制台必须是同一个二进制的两个 listener，不是两个进程——状态机
// 必须有唯一所有者，两个进程会立即撞上状态转换竞态与 reconcile 循环
// 重复执行（SPEC §1.1）。两个 listener 各绑各自的具体 IP（配置已有通配
// 地址守卫，见 config.Validate），syslog 绑配置里的 UDP 地址。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/lober/lober/internal/config"
)

// readHeaderTimeout 是两个 HTTP server 的读头部超时。设了它但不设写
// 超时——SSE 是长连接（SPEC §12.8），设写超时会在装机中途掐断运维正在
// 看的日志流（计划任务 4「设计要点」）。
const readHeaderTimeout = 10 * time.Second

// shutdownGrace 是收到停止信号后，等在途请求结束的宽限期上限。
const shutdownGrace = 15 * time.Second

// runServers 起网关、控制台两个 HTTP listener 与一个 syslog UDP
// listener，阻塞直到 ctx 取消或某个 listener 报错，随后走优雅关闭。
//
// 先逐个 Listen，任一失败就关掉已开的并返回错误——端口被占时必须立刻
// 失败退出，不能让系统半开着跑（只有控制台起来、网关没起来的状态下，
// 运维会以为一切正常而机器全装不了，计划任务 4「设计要点」）。
func runServers(ctx context.Context, app *App, cfg *config.Config) error {
	gwListener, err := net.Listen("tcp", cfg.Gateway.Listen)
	if err != nil {
		return fmt.Errorf("cmd/lober: 网关监听失败（addr=%s): %w", cfg.Gateway.Listen, err)
	}

	consoleListener, err := net.Listen("tcp", cfg.Console.Listen)
	if err != nil {
		gwListener.Close()
		return fmt.Errorf("cmd/lober: 控制台监听失败（addr=%s): %w", cfg.Console.Listen, err)
	}

	// 三个都成功后才各起 goroutine 开始 Serve。
	gwHTTPServer := &http.Server{Handler: app.GatewayServer, ReadHeaderTimeout: readHeaderTimeout}
	consoleMux := http.NewServeMux()
	consoleMux.Handle("/console/v1/", app.ConsoleServer)
	consoleMux.Handle("/", app.UIHandler)
	consoleHTTPServer := &http.Server{Handler: consoleMux, ReadHeaderTimeout: readHeaderTimeout}

	errCh := make(chan error, 3)
	go func() { errCh <- gwHTTPServer.Serve(gwListener) }()
	go func() { errCh <- consoleHTTPServer.Serve(consoleListener) }()

	// syslog：契约缺口（记入 contractGaps）。internal/gateway.Server.
	// ListenAndServeSyslog 自己内部完成 net.ListenUDP 与阻塞读循环，不
	// 接受调用方注入已经 Listen 好的连接，不返回「已开始监听」的信号，也
	// 没有暴露任何可用于唤醒阻塞读调用的取消机制（它的 for 循环只在
	// ReadFromUDP 报错时才可能退出，没有 select ctx.Done() 分支，也没有
	// 导出的、可被外部关闭的连接引用）。
	//
	// 这使得任务 4「先逐个 Listen，任一失败就关掉已开的并返回错误」这条
	// 约束对 syslog 这一路无法做到与另外两个 HTTP listener 同等程度的
	// 「同步、Serve 之前」检查：曾经尝试过在这里先 net.ListenUDP 一次
	// 占位、随后 Close 掉再让 ListenAndServeSyslog 重新绑定同一地址，
	// 但这引入了一个真实的时序竞态——Close 后操作系统释放端口不是
	// 瞬时完成的（尤其在 Windows 上），ListenAndServeSyslog 的重新绑定
	// 可能在端口尚未真正释放时发生而静默失败或延迟生效，这比契约缺口
	// 本身更危险（错误从「显式记录的能力边界」退化成「偶发的、难以
	// 复现的启动竞态」）。因此本实现选择不做这次占位 Listen：syslog 的
	// 绑定失败通过 errCh（与两个 HTTP listener 的 Serve 错误同一个
	// channel）异步上报，牺牲的是「syslog 端口冲突时保证先于 HTTP
	// listener 开始 Serve 之前失败」这一时序保证，换来的是不引入双重
	// bind 竞态。
	//
	// 另一半契约缺口（优雅关闭无法覆盖 syslog）依旧存在：ctx 取消后，
	// 网关与控制台两个 HTTP listener 能被 Shutdown 优雅收尾，但 syslog
	// 的 goroutine 会一直阻塞在 ReadFromUDP 直到进程整体退出（操作系统
	// 回收 socket）。正解方向是给 ListenAndServeSyslog 加一个 context
	// 参数或让它接受一个已经 Listen 好的 *net.UDPConn，两处都不是本
	// 装配点能在不改 internal/gateway 的前提下解决的。
	if cfg.Gateway.SyslogListen != "" {
		syslogAddr := cfg.Gateway.SyslogListen
		go func() { errCh <- app.GatewayServer.ListenAndServeSyslog(syslogAddr) }()
	}

	slog.Info("lober 已启动",
		slog.String("gateway_listen", cfg.Gateway.Listen),
		slog.String("console_listen", cfg.Console.Listen),
		slog.String("syslog_listen", cfg.Gateway.SyslogListen),
	)

	select {
	case <-ctx.Done():
		return shutdownServers(gwHTTPServer, consoleHTTPServer)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = shutdownServers(gwHTTPServer, consoleHTTPServer)
			return fmt.Errorf("cmd/lober: 服务运行期出错: %w", err)
		}
		return shutdownServers(gwHTTPServer, consoleHTTPServer)
	}
}

// shutdownServers 优雅关闭两个 HTTP server：停止接受新连接、等在途请求
// 结束（带超时）。syslog 的 UDP 读循环无法被这样优雅唤醒，见 runServers
// 里的契约缺口说明——它会随进程退出被操作系统回收连接。
func shutdownServers(gw, console *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	var errs []error
	if err := gw.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("网关优雅关闭失败: %w", err))
	}
	if err := console.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("控制台优雅关闭失败: %w", err))
	}
	return errors.Join(errs...)
}
