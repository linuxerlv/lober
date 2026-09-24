// 本文件实现心跳探测循环的生产装配：按配置的间隔周期性查询非终态且已知
// 探测地址的任务，交给 provider 层的 HeartbeatProviderV1 批量探测，记录
// 结果但不改任务状态。对应 SPEC §6.6、§7.4，任务 t14-heartbeat。
//
// 与超时扫描循环（scanner.go）同样的纪律：不依赖进程内存状态、启动时立即
// 探测一轮、ctx 取消后干净退出。心跳与超时扫描是两条独立的后台循环，各自
// 的失败互不影响——探测失败只记日志，绝不能让一次网络抖动把正在装的机器
// 标成失败（§7.4 的状态转换表里根本没有「心跳失败」这个触发源）。
//
// 拉取的本质局限（§6.6 明确要求写进注释）：心跳是辅助通道，不是主通道。
// 机器主动推送信号后，拉取不再承担状态推进，只在「推送断了」时兜底判断
// 「机器是还活着还是真死了」。ICMP/TCP 在最值得关心的故障模式里全是正常
// 的——anaconda 卡在依赖解析、hook 死循环、仓库 404 重试时，内核活着、
// 网络通着，拉取探测不到任何异常。它能替代存活判断，替代不了进展判断；
// 进展判断靠信号加 syslog。
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// heartbeatProbeableStates 是心跳探测覆盖的任务状态集合（§6.6：拉取范围
// 是 booting/installing/rebooting/initializing）。pending 还没到需要探活
// 的阶段（机器甚至没开始装），done/failed 是终态、探测无意义。
var heartbeatProbeableStates = []model.TaskState{
	model.TaskStateBooting,
	model.TaskStateInstalling,
	model.TaskStateRebooting,
	model.TaskStateInitializing,
}

// heartbeatTaskLister 是 runHeartbeatLoop 依赖的最窄接口：只需要按状态
// 列出任务这一个方法（与 scanner.go 的 timeoutScanner 同一种设计——测试
// 不必搭出整个 Store 实现）。*postgres.Store 天然满足，contract.Store 组合
// 接口的这一个方法即可，装配点直接传 app.Store。
type heartbeatTaskLister interface {
	ListTasks(ctx context.Context, filter contract.TaskFilter) ([]model.Task, error)
}

// runHeartbeatLoop 起一个后台 goroutine，按 interval 周期查询可探测状态的
// 任务、抽取已知探测地址、交给 prober 批量探测；ctx 取消时循环退出并关闭
// 返回的 channel（供调用方等待循环真正结束，避免优雅关闭时 goroutine
// 泄漏，与 runScanLoop 同一纪律）。
//
// interval 非正数时兜底为 60 秒（SPEC §6.6 的默认值，config.applyDefaults
// 已经在配置层填充过默认值，这里的兜底是双重保险）。
func runHeartbeatLoop(ctx context.Context, lister heartbeatTaskLister, prober contract.HeartbeatProviderV1, interval time.Duration) <-chan struct{} {
	if interval <= 0 {
		interval = 60 * time.Second
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		heartbeatOnce(ctx, lister, prober)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("心跳探测循环已停止")
				return
			case <-ticker.C:
				heartbeatOnce(ctx, lister, prober)
			}
		}
	}()
	return done
}

// heartbeatOnce 执行一轮心跳探测：逐个可探测状态查询任务、汇总成探测
// 目标列表、调用 prober.Probe、记录结果。
//
// 查询出错只记日志、不中断本轮剩余状态的查询——某一个状态的 ListTasks
// 瞬时失败不该让其它状态的探测也一起放弃（与 scanOnce「出错只记日志、不
// 退出循环」同一种「失败隔离」思路，但粒度更细：这里隔离到单个状态而不是
// 整轮）。
func heartbeatOnce(ctx context.Context, lister heartbeatTaskLister, prober contract.HeartbeatProviderV1) {
	var targets []contract.ProbeTarget
	for _, state := range heartbeatProbeableStates {
		tasks, err := lister.ListTasks(ctx, contract.TaskFilter{State: state})
		if err != nil {
			slog.Error("心跳探测查询任务失败", slog.String("state", string(state)), slog.Any("error", err))
			continue
		}
		for _, task := range tasks {
			// 读裸键 "biz_ip"：resolve.go 写回 ResolvedParams 时用的是
			// InlineParams 白名单认得的裸键，不是 fact 命名空间常量
			// model.FactBizIP（LOBER_BIZ_IP 属于注入给机器侧 hook 的
			// Facts 命名空间，与 ResolvedParams 是两个不同的键空间）。
			ip := task.ResolvedParams["biz_ip"]
			if ip == "" {
				// 没有已知探测地址的任务不进目标列表——喂一个空字符串 IP
				// 给探测器没有意义，且会污染探测结果的可观测性（任务
				// 要点：只探测已知可探测地址的任务）。
				continue
			}
			targets = append(targets, contract.ProbeTarget{TaskID: task.ID, IP: ip})
		}
	}

	if len(targets) == 0 {
		// 没有任何可探测任务时不调用 Probe——空目标列表没有必要发起一次
		// 空调用，上面的 ListTasks 查询本身已经证明本轮确实执行过。
		return
	}

	results := prober.Probe(ctx, targets)
	logHeartbeatResults(results)
}

// logHeartbeatResults 记录探测结果，仅此而已——探测失败绝不改任务状态
// （§7.4 的状态转换表没有心跳失败这个触发源）。这一条搞错的后果是一次
// 网络抖动就把正在装的机器错误地标成失败，因此本函数是本文件里唯一
// 触碰探测结果的地方，且刻意不持有任何能改状态机的依赖。
func logHeartbeatResults(results []contract.ProbeResult) {
	for _, r := range results {
		if r.Reached {
			slog.Debug("心跳探测存活", slog.String("task_id", r.TaskID), slog.String("detail", r.Detail))
			continue
		}
		// 未探测到不是错误级别——参见文件头注释：这在装机中途是常态（内核
		// 活着、网络通着，只是还没到能响应探测的阶段），不该刷成 Error 级别
		// 淹没真正需要运维关注的日志。
		slog.Info("心跳探测未确认存活", slog.String("task_id", r.TaskID), slog.String("detail", r.Detail))
	}
}
