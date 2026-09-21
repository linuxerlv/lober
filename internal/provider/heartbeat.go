// 本文件实现心跳探测：tcp（默认）与 icmp 两个探测器。对应实现计划任务 6，
// 依据 SPEC §6.6、§7.4。
//
// 拉取的本质局限（写进注释，任务 6 明确要求）：ICMP/TCP 在你最关心的故障
// 里全是正常的——anaconda 卡在依赖解析、hook 死循环、仓库 404 重试时，
// 内核活着、网络通着。它能替代存活判断，替代不了进展判断。进展靠信号加
// syslog（§6.6）。心跳只是「推送断了」时的兜底判断，不是主通道。
package provider

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/lober/lober/internal/core/contract"
)

// maxConcurrentProbes 是批量探测的并发上限（任务 6：避免几百个目标同时
// 拨号耗尽文件描述符）。取值不来自配置——心跳批量探测的规模是「当前非
// 终态任务数」，百级场景下 32 的并发已经足够快，而不做成配置项能避免
// 运维在这个几乎不需要调的参数上误配出资源问题。
const maxConcurrentProbes = 32

// tcpDialTimeout 是单个 TCP 探测的拨号超时。
const tcpDialTimeout = 3 * time.Second

// icmpReadTimeout 是单个 ICMP 探测等待回包的超时。
const icmpReadTimeout = 3 * time.Second

// ---------------------------------------------------------------------------
// tcp 探测器（默认，零权限要求）
// ---------------------------------------------------------------------------

// tcpHeartbeat 是 HeartbeatProviderV1 的 tcp 实现：带超时的拨号
// （任务 6：默认 tcp，零权限要求）。
//
// 探测目标端口从哪里来：contract.ProbeTarget 只有 TaskID 与 IP 两个字段，
// 没有端口——这与 SPEC §6.6「tcp 探测用表单中的业务 IP + 指定端口」的
// 描述有一处契约缺口（详见 factory.go 顶部的契约缺口说明）。本实现的
// 保守处理：构造时固定注入一个端口（默认 22，运维可通过配置覆盖，见
// factory.go 的 NewHeartbeatProvider），对全部目标使用同一个端口——这是
// 「不知道该探测目标关联的具体业务端口时」影响面最小的选择，且不需要
// 修改 contract.ProbeTarget 的字段形状。
type tcpHeartbeat struct {
	port    int
	timeout time.Duration
}

// newTCPHeartbeat 构造一个 tcp 心跳探测器，port 是全部目标统一使用的探测
// 端口。
func newTCPHeartbeat(port int) *tcpHeartbeat {
	return &tcpHeartbeat{port: port, timeout: tcpDialTimeout}
}

// Probe 并发探测全部目标，单个目标的失败不影响其余（任务 6）。
func (h *tcpHeartbeat) Probe(ctx context.Context, targets []contract.ProbeTarget) []contract.ProbeResult {
	return probeConcurrently(ctx, targets, func(ctx context.Context, target contract.ProbeTarget) contract.ProbeResult {
		return h.probeOne(ctx, target)
	})
}

// probeOne 探测单个目标：带超时地拨号，能拨通即视为存活。
func (h *tcpHeartbeat) probeOne(ctx context.Context, target contract.ProbeTarget) contract.ProbeResult {
	addr := net.JoinHostPort(target.IP, fmt.Sprintf("%d", h.port))

	dialer := net.Dialer{Timeout: h.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return contract.ProbeResult{
			TaskID:  target.TaskID,
			Reached: false,
			Detail:  fmt.Sprintf("tcp 拨号 %s 失败: %v", addr, err),
		}
	}
	_ = conn.Close()

	return contract.ProbeResult{TaskID: target.TaskID, Reached: true, Detail: "tcp 拨号成功"}
}

// Ping 直接返回成功——探测器本身没有需要探活的外部依赖（每次探测都是
// 独立的拨号，不维护长连接）。
func (h *tcpHeartbeat) Ping(ctx context.Context) error {
	return nil
}

// 编译期断言：tcpHeartbeat 必须满足 contract.HeartbeatProviderV1。
var _ contract.HeartbeatProviderV1 = (*tcpHeartbeat)(nil)

// ---------------------------------------------------------------------------
// icmp 探测器（需要特权套接字，默认不启用，优雅降级）
// ---------------------------------------------------------------------------

// icmpHeartbeat 是 HeartbeatProviderV1 的 icmp 实现（任务 6）。
//
// ICMP 的取舍决定：默认用 tcp，icmp 实现但优雅降级。理由（写进注释，
// 任务 6 明确要求）：ICMP 需要 root 或 CAP_NET_RAW 或调整内核的
// ping_group_range。心跳只是辅助通道，不值得为它给整个进程提权——
// 提权的影响面是全系统，而收益只是「能 ping 通还没配业务 IP 的机器」。
//
// 无权限时的行为：不 panic、不让整批探测失败，而是对每个目标返回
// 「探测失败」并在 Detail 里写明「需要提权」。这样运维在控制台看到的是
// 「心跳不可用，原因是权限」而不是「所有机器都死了」。
type icmpHeartbeat struct {
	timeout time.Duration
}

// newICMPHeartbeat 构造一个 icmp 心跳探测器。
func newICMPHeartbeat() *icmpHeartbeat {
	return &icmpHeartbeat{timeout: icmpReadTimeout}
}

// Probe 并发探测全部目标。若本进程没有 ICMP 特权套接字权限，每个目标
// 各自独立地在 probeOne 内部发现这一事实并返回降级结果——不在 Probe
// 顶层做一次性权限探测再整批放弃，因为「整批放弃」与「逐个探测都失败」
// 对调用方（ProbeResult 切片长度与内容）应该是同一种可观察结果，没有
// 必要为了「快速失败」这个优化引入第二条返回路径。
func (h *icmpHeartbeat) Probe(ctx context.Context, targets []contract.ProbeTarget) []contract.ProbeResult {
	return probeConcurrently(ctx, targets, func(ctx context.Context, target contract.ProbeTarget) contract.ProbeResult {
		return h.probeOne(ctx, target)
	})
}

// probeOne 对单个目标发一个 ICMP echo 请求。
//
// 无权限时 icmp.ListenPacket 会返回错误（Linux 上通常是
// "socket: operation not permitted"，取决于 ping_group_range 配置）——
// 这里把该错误包装成带「需要提权」字样的失败结果，不 panic、不让调用方
// 的其它目标受影响（每个目标独立调用 ListenPacket，互不共享状态）。
func (h *icmpHeartbeat) probeOne(ctx context.Context, target contract.ProbeTarget) contract.ProbeResult {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return contract.ProbeResult{
			TaskID:  target.TaskID,
			Reached: false,
			Detail: fmt.Sprintf("icmp 探测失败：无法打开 ICMP 套接字（%v）——"+
				"需要提权（root 或 CAP_NET_RAW 或调整内核 ping_group_range），"+
				"心跳只是辅助通道，未提权时默认不可用，可切换为 tcp 探测", err),
		}
	}
	defer conn.Close()

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   int(uint16(len(target.TaskID))), // 用任务 id 长度做一个稳定但轻量的会话标识
			Seq:  1,
			Data: []byte("lober-heartbeat"),
		},
	}
	wb, err := msg.Marshal(nil)
	if err != nil {
		return contract.ProbeResult{
			TaskID:  target.TaskID,
			Reached: false,
			Detail:  fmt.Sprintf("icmp 探测失败：构造报文出错: %v", err),
		}
	}

	dst, err := net.ResolveIPAddr("ip4", target.IP)
	if err != nil {
		return contract.ProbeResult{
			TaskID:  target.TaskID,
			Reached: false,
			Detail:  fmt.Sprintf("icmp 探测失败：目标地址 %q 无法解析: %v", target.IP, err),
		}
	}

	if _, err := conn.WriteTo(wb, dst); err != nil {
		return contract.ProbeResult{
			TaskID:  target.TaskID,
			Reached: false,
			Detail:  fmt.Sprintf("icmp 探测失败：发送报文出错: %v", err),
		}
	}

	if err := conn.SetReadDeadline(time.Now().Add(h.timeout)); err != nil {
		return contract.ProbeResult{
			TaskID:  target.TaskID,
			Reached: false,
			Detail:  fmt.Sprintf("icmp 探测失败：设置读超时出错: %v", err),
		}
	}

	rb := make([]byte, 1500)
	n, _, err := conn.ReadFrom(rb)
	if err != nil {
		return contract.ProbeResult{
			TaskID:  target.TaskID,
			Reached: false,
			Detail:  fmt.Sprintf("icmp 探测超时或出错（%v）：目标可能不可达或丢弃了 ICMP", err),
		}
	}

	parsed, err := icmp.ParseMessage(1, rb[:n]) // 1 = ICMP for IPv4 protocol number
	if err != nil {
		return contract.ProbeResult{
			TaskID:  target.TaskID,
			Reached: false,
			Detail:  fmt.Sprintf("icmp 探测失败：解析回包出错: %v", err),
		}
	}

	if parsed.Type != ipv4.ICMPTypeEchoReply {
		return contract.ProbeResult{
			TaskID:  target.TaskID,
			Reached: false,
			Detail:  fmt.Sprintf("icmp 探测失败：收到非 echo-reply 类型的报文（%v）", parsed.Type),
		}
	}

	return contract.ProbeResult{TaskID: target.TaskID, Reached: true, Detail: "icmp echo-reply 收到"}
}

// Ping 尝试打开一次 ICMP 套接字并立即关闭，用于探活本探测器自身是否可用
// （是否具备提权）。
func (h *icmpHeartbeat) Ping(ctx context.Context) error {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("provider/heartbeat(icmp): 无 ICMP 套接字权限，需要提权（root/CAP_NET_RAW）: %w", err)
	}
	return conn.Close()
}

// 编译期断言：icmpHeartbeat 必须满足 contract.HeartbeatProviderV1。
var _ contract.HeartbeatProviderV1 = (*icmpHeartbeat)(nil)

// ---------------------------------------------------------------------------
// 并发探测辅助：有上限的并发，单目标失败不影响其余
// ---------------------------------------------------------------------------

// probeConcurrently 对 targets 逐个调用 probeOne，并发度不超过
// maxConcurrentProbes（任务 6：避免几百个目标同时拨号耗尽文件描述符）。
// 空目标列表直接返回空结果，不报错、不启动任何 goroutine。
func probeConcurrently(ctx context.Context, targets []contract.ProbeTarget, probeOne func(context.Context, contract.ProbeTarget) contract.ProbeResult) []contract.ProbeResult {
	if len(targets) == 0 {
		return nil
	}

	results := make([]contract.ProbeResult, len(targets))
	sem := make(chan struct{}, maxConcurrentProbes)
	var wg sync.WaitGroup

	for i, target := range targets {
		wg.Add(1)
		sem <- struct{}{} // 达到上限时阻塞，直到有 goroutine 释放名额
		go func(i int, target contract.ProbeTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = probeOne(ctx, target)
		}(i, target)
	}

	wg.Wait()
	return results
}
