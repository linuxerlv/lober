// Package loader 实现 SPEC §7.2 描述的 exec / http 两种加载器：把七个
// provider 契约接口（internal/core/contract/provider.go）的调用转换成
// 子进程 stdio 或 HTTP POST 的 JSON 请求/响应，对调用方透明——调用方拿到
// 的仍然是满足某个 contract 接口的对象，不知道背后是子进程还是 webhook
// （§7.2：「加载器是接口的另一种实现，对调用方透明」）。
//
// 本文件定义两种加载器共用的部分：
//  1. JSON 请求/响应信封（Request/Response），method 字段区分调用的是
//     哪个契约方法（如 "ipam.allocate"），params/result 各自是该方法的
//     参数/返回值的 JSON 表示；
//  2. Caller 接口——exec.go 与 http.go 各自的加载器类型都实现这一个方法，
//     供上层的 provider 适配器（本包之外，由 factory.go 装配）统一调用，
//     不区分具体是哪种加载方式；
//  3. 超时与重试的公共参数（Config）与重试执行逻辑（callWithRetry）——
//     两种加载器的「超时后杀掉子进程/放弃 HTTP 请求」各自实现，但「失败
//     后按指数退避重试几次」这段逻辑完全一致，抽到这里避免两处漂移。
//
// 不做 grpc 与 starlark（§7.2 判据：有真逻辑的集成才需要 grpc，纯决策不
// 需要外部化，见 docs/phase2-loader-需求依据.md 第 2、4 节）——这是 Phase 3
// 范围，本任务不做。
package loader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Request 是加载器发给外部程序/服务的 JSON 请求信封。Method 标识本次调用
// 对应哪个契约接口方法（约定形如 "<provider 小写名>.<方法名小写>"，例如
// "ipam.allocate"、"dns.register"），Params 是该方法参数的 JSON 编码结果
// （由适配器在调用 Caller.Call 前用 json.Marshal 产出，见 Caller 文档）。
//
// 用统一信封而不是让每个契约方法各自定义单独的请求类型：exec/http 两种
// 加载器的传输层代码（子进程 stdio 编解码、HTTP body 编解码）只需要认识
// 一种信封结构，不需要为七个契约接口的十几个方法各写一份传输代码——
// 方法专属的参数结构留在 Params 字段内部，由适配器负责编解码，传输层
// 本身对 Params 的具体内容不敏感（json.RawMessage）。
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Response 是外部程序/服务返回的 JSON 响应信封。Result 是成功时的返回值
// JSON 编码（由适配器负责解码进具体类型），Error 非空表示本次调用失败——
// 约定与 Go 的 (T, error) 惯例对齐：Result 与 Error 不应同时非空，但解码方
// 只信任 Error 非空即失败，不校验两者互斥（外部程序是不受控输入，防御性
// 校验交给调用方按需加）。
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Caller 是 exec 与 http 两种加载器共同实现的核心方法：发出一次调用并拿到
// 解码后的结果。
//
// method 与 params 的含义见 Request 文档；result 是指向具体返回值类型的
// 指针（调用方传入 &AllocResult{} 之类），Call 内部负责把 Response.Result
// 解码进它。result 为 nil 表示调用方不关心返回值（如 Release/Deregister
// 这类只关心成功与否的方法），此时即使 Response.Result 非空也不解码。
//
// 之所以是「一个通用方法」而不是「七个契约接口各自的具体方法」：Caller
// 本身不实现任何契约接口（IPAMProviderV1 等），它是传输层的抽象——具体的
// 契约接口适配（把 Allocate(ctx, req) 转成 Call(ctx, "ipam.allocate", req,
// &result)）是本包之外的装配职责（factory.go），这样 exec.go/http.go
// 的实现完全不需要知道七个契约接口分别长什么样，新增第八个 provider 类型
// 时也不需要改动传输层代码。
type Caller interface {
	Call(ctx context.Context, method string, params any, result any) error
}

// Config 是 exec 与 http 加载器共用的超时、重试、熔断参数。
//
// 两种加载器各自的专属参数（exec 的 Command/Args，http 的 URL）不放在这里
// ——那是各自 New 函数的专属入参，混进共用 Config 会让「哪些字段对哪种
// 加载器有意义」变得含糊。
type Config struct {
	// Timeout 是单次调用（含重试的每一次尝试）的超时。子进程超时后必须被
	// 杀掉（见 exec.go），HTTP 请求超时后由 context 取消。零值表示使用
	// DefaultTimeout。
	Timeout time.Duration

	// MaxRetries 是失败后的重试次数（不含首次尝试）。0 表示不重试，失败
	// 立即返回。
	MaxRetries int

	// RetryBackoff 是重试的初始退避间隔，指数退避（每次翻倍）。零值表示
	// 使用 DefaultRetryBackoff。
	RetryBackoff time.Duration

	// BreakerThreshold 是连续失败多少次后熔断（标记 provider 不可用，
	// 之后的调用不再发起，直接返回熔断错误——见 breaker.go）。0 表示使用
	// DefaultBreakerThreshold；传入负数等价于禁用熔断（永不熔断），供
	// 测试或明确不想要熔断行为的场景使用。
	BreakerThreshold int
}

// 默认值。DefaultTimeout 10 秒对齐 SPEC §7.2 示例配置里 dns exec 的
// `timeout: 10s`。DefaultRetryBackoff 200ms 与 DefaultBreakerThreshold 3
// 是本包在 SPEC 未给出具体数值时按「重试给外部系统一个短暂抖动恢复的
// 机会，但不能无限重试拖慢建任务」这一原则选的保守值——阻塞类 provider
// （IPAM 等）的失败会直接卡住建任务，重试次数与退避都不宜过大。
const (
	DefaultTimeout          = 10 * time.Second
	DefaultRetryBackoff     = 200 * time.Millisecond
	DefaultBreakerThreshold = 3
)

// withDefaults 返回把零值字段填上默认值后的 Config 副本。BreakerThreshold
// 为负数时保留原值（表示「显式禁用熔断」，与「未设置」的零值区分）。
func (c Config) withDefaults() Config {
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = DefaultRetryBackoff
	}
	if c.BreakerThreshold == 0 {
		c.BreakerThreshold = DefaultBreakerThreshold
	}
	return c
}

// ErrTimeout 是加载器调用超时的哨兵错误，两种加载器的超时都包装此错误
// （errors.Is 可判定），供适配器/调用方区分「超时」与「外部程序返回业务
// 错误」这两类失败——例如熔断计数可能想对两者一视同仓（都算一次失败），
// 但更上层的告警文案可能想区分措辞。
var ErrTimeout = errors.New("loader: 调用超时")

// callAttempt 是单次尝试的函数签名，由 exec.go/http.go 的 Call 实现内部
// 传给 callWithRetry——把「怎么发出这一次尝试」（子进程 or HTTP）与
// 「失败了要不要重试、重试前等多久」两件事分开，重试逻辑本身不关心具体
// 传输方式。
type callAttempt func(ctx context.Context) error

// callWithRetry 执行 attempt，失败后按 cfg 的退避策略重试，直到成功、
// 达到最大重试次数、或 ctx 被取消。
//
// 重试之间的等待本身也受 ctx 约束（用 time.NewTimer 配合 ctx.Done()
// 而不是裸 time.Sleep）：调用方的 ctx 若在退避等待期间被取消（如整个
// HTTP 请求或建任务流程超时），必须立即放弃重试并返回，不能让重试机制
// 本身变成一个忽略上层取消信号的黑洞。
//
// ctx 超时（context.DeadlineExceeded）与本包的 ErrTimeout 是两个不同的
// 错误来源：ctx 超时来自调用方给的整体截止时间，ErrTimeout 来自单次尝试
// 内部判定自己超过了 cfg.Timeout（子进程/HTTP 请求本身的超时）。两者都
// 会导致 attempt 返回错误进而触发重试逻辑，callWithRetry 不需要区分。
func callWithRetry(ctx context.Context, cfg Config, attempt callAttempt) error {
	backoff := cfg.RetryBackoff
	var lastErr error

	for i := 0; i <= cfg.MaxRetries; i++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("loader: 上下文已取消，放弃重试（上一次尝试的错误: %w）: %w", lastErr, err)
			}
			return err
		}

		lastErr = attempt(ctx)
		if lastErr == nil {
			return nil
		}

		if i == cfg.MaxRetries {
			break
		}

		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("loader: 等待重试期间上下文被取消（上一次尝试的错误: %w）: %w", lastErr, ctx.Err())
		}
		backoff *= 2
	}

	return fmt.Errorf("loader: 重试 %d 次后仍失败: %w", cfg.MaxRetries, lastErr)
}
