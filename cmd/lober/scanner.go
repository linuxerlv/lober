// 本文件实现超时扫描循环，独立于请求路径。对应实现计划任务 3，依据
// SPEC §3.3。
//
// 不用 in-process timer——进程重启定时器全丢，任务的超时判定必须在重启
// 后继续有效（依据数据库里的 phase_entered_at + budget，不依赖任何进程
// 内存状态）。启动时立即扫一轮，不等第一个间隔：进程重启期间积压的超时
// 要马上判出来。
package main

import (
	"context"
	"log/slog"
	"time"
)

// timeoutScanner 是 runScanLoop 依赖的最窄接口：只需要状态机的一个方法
// （用一个单方法的窄接口声明，测试不必搭出整个状态机，计划任务 3
// 「设计要点」）。
type timeoutScanner interface {
	ScanTimeouts(ctx context.Context, asOf int64) (failedCount int, err error)
}

// runScanLoop 起一个后台 goroutine，按 interval 周期调用
// scanner.ScanTimeouts；ctx 取消时循环退出并关闭返回的 channel（供调用方
// 等待循环真正结束，避免优雅关闭时扫描循环的 goroutine 泄漏）。
//
// interval 非正数时兜底为 30 秒（SPEC §3.3 的默认值；config.Parse 已经
// 在配置层填充过默认值，这里的兜底是双重保险，避免测试或未来的构造路径
// 传入零值时循环忙等）。
func runScanLoop(ctx context.Context, sm timeoutScanner, interval time.Duration) <-chan struct{} {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanOnce(ctx, sm)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("超时扫描循环已停止")
				return
			case <-ticker.C:
				scanOnce(ctx, sm)
			}
		}
	}()
	return done
}

// scanOnce 执行一轮扫描。出错只记日志、不退出循环——数据库瞬时不可用之后
// 超时判定必须自己恢复，循环退出的后果是静默停摆到下次重启，而没人会
// 注意到扫描循环不见了（计划任务 3「设计要点」）。
func scanOnce(ctx context.Context, sm timeoutScanner) {
	failed, err := sm.ScanTimeouts(ctx, time.Now().Unix())
	if err != nil {
		slog.Error("超时扫描本轮出错", slog.Any("error", err))
		return
	}
	if failed > 0 {
		slog.Info("超时扫描本轮完成", slog.Int("failed_count", failed))
	}
}
