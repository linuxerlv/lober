// 本文件提供一个把重试与熔断套在任意 Caller 外层的组合器。
//
// 存在的理由：exec.go 与 http.go 各自在自己的 Call 里内联了「熔断判定 →
// callWithRetry → 记录结果」这三步（那是它们被写出来时唯一的两种加载器，内联
// 是合理的）。P3-04 加第三种加载器时需要同一套行为，而计划任务 4 明确要求
// **不要为 gRPC 写第二套熔断**——breaker 是按扩展点的失败语义分流的（§7.4），
// 与加载方式无关：一个 IPAM 连续失败五次该熔断，不因为它背后是 gRPC 插件还是
// shell 脚本而不同。
//
// 因此把那三步抽成一个装饰器，让 grpc 加载器直接套用**同一个** circuitBreaker
// 与 callWithRetry 实现。exec.go 与 http.go 保持原样不动——改它们需要重跑那两
// 套已经绿的测试，而本次改动的目的只是让第三种加载器能复用，不是重构前两种。
package loader

import (
	"context"
	"fmt"
)

// decorated 是套了重试与熔断的 Caller。
type decorated struct {
	inner   Caller
	cfg     Config
	breaker *circuitBreaker
}

// WithRetryAndBreaker 用 cfg 的重试与熔断参数包装 inner。
//
// 行为与 ExecLoader.Call / HTTPLoader.Call 的三步逐一对应：
//
//  1. 熔断打开时直接返回错误，不发起任何调用
//  2. 按 cfg 的重试策略执行一到多次
//  3. 最终结果（成功或耗尽重试后的失败）记录进熔断器的连续失败计数
//
// inner 为 nil 时返回一个总是报错的 Caller 而不是 panic：装配点传了 nil 说明
// 上游构造失败却没被检查，让它在第一次调用时给出一句明确的错误，比在某个
// goroutine 里 panic 掉整个进程好——后者会把「一个 provider 没配好」放大成
// 「整个服务挂了」。
func WithRetryAndBreaker(inner Caller, cfg Config) Caller {
	if inner == nil {
		return nilCaller{}
	}
	return &decorated{
		inner:   inner,
		cfg:     cfg.withDefaults(),
		breaker: newCircuitBreaker(cfg.BreakerThreshold),
	}
}

func (d *decorated) Call(ctx context.Context, method string, params any, result any) error {
	if !d.breaker.Allow() {
		return fmt.Errorf("loader: 该 provider 已熔断，不再发起调用（方法 %q）"+
			"——连续失败达到阈值，需人工核查插件/脚本或它依赖的外部系统后重启进程恢复",
			method)
	}

	err := callWithRetry(ctx, d.cfg, func(ctx context.Context) error {
		return d.inner.Call(ctx, method, params, result)
	})
	d.breaker.RecordResult(err)
	return err
}

// nilCaller 是 inner 为 nil 时的替身，见 WithRetryAndBreaker 的文档。
type nilCaller struct{}

func (nilCaller) Call(context.Context, string, any, any) error {
	return fmt.Errorf("loader: 该 provider 的底层 Caller 为 nil——" +
		"装配时上游构造失败却没被检查，请查启动日志里更早的那条错误")
}

var (
	_ Caller = (*decorated)(nil)
	_ Caller = nilCaller{}
)
