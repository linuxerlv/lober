// 本文件实现 exec 加载器：把契约方法调用转成子进程 stdio 上的 JSON 请求/
// 响应（SPEC §7.2：「exec/http：子进程/webhook，JSON 进 JSON 出」）。
//
// 选 exec 而非要求外部程序常驻：气隙环境下换一个 shell 脚本是重新覆盖
// 一个文件这么简单的事，不需要进程管理（计划任务动机、§7.2 选型判据）。
// 每次 Call 都拉起一个新子进程，写入一条 Request 到其 stdin 并关闭
// stdin（让脚本能用 `read` 或语言内建的 stdin-EOF 判定读完一整条输入），
// 读取其 stdout 的一条 Response，子进程退出后按退出码与解码结果判定
// 成功与否。
package loader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// ExecConfig 是构造 ExecLoader 需要的专属参数（与两种加载器共用的
// loader.Config 分开，见该类型文档）。
type ExecConfig struct {
	// Command 是要执行的可执行文件路径（或 PATH 可解析的程序名）。必须
	// 非空——见 NewExecLoader 的启动校验：选了 exec 类型却没给 command，
	// 必须在构造期拒绝，不能等到运行期第一次调用才发现（任务要求的测试
	// 用例「配置里选了 exec 但没给 command → 启动校验拒绝」）。
	Command string
	// Args 是传给 Command 的固定参数列表（可选）。
	Args []string
}

// ExecLoader 是 Caller 的子进程实现。
type ExecLoader struct {
	execCfg ExecConfig
	cfg     Config
	breaker *circuitBreaker

	// onProcessStarted 是仅供测试使用的钩子：子进程 Start 成功后回调，
	// 参数是子进程 PID。生产路径上始终为 nil（不产生任何行为差异）——
	// 存在的唯一理由是让 exec_test.go 的失败诊断信息里能带上具体 PID，
	// 判定逻辑本身不依赖它（见 callOnce 文档：Wait() 返回本身就是终止
	// 证明）。
	onProcessStarted func(pid int)
}

// NewExecLoader 构造一个 ExecLoader。cmd.Command 为空时返回错误——这是
// 启动校验，不是运行期才暴露的错误（任务明确要求的测试点）。
func NewExecLoader(cmd ExecConfig, cfg Config) (*ExecLoader, error) {
	if cmd.Command == "" {
		return nil, fmt.Errorf("loader: exec 加载器缺少 command 参数" +
			"（配置里选了 exec 类型但没给要执行的命令，启动时拒绝，" +
			"不能等到第一次调用时才在运行期发现）")
	}
	cfg = cfg.withDefaults()
	return &ExecLoader{
		execCfg: cmd,
		cfg:     cfg,
		breaker: newCircuitBreaker(cfg.BreakerThreshold),
	}, nil
}

// Call 实现 Caller。熔断打开时直接返回熔断错误，不执行任何子进程操作；
// 否则按 cfg 的重试策略执行一到多次子进程调用，最终结果（成功或耗尽
// 重试后的失败）记录进熔断器的连续失败计数。
func (l *ExecLoader) Call(ctx context.Context, method string, params any, result any) error {
	if !l.breaker.Allow() {
		return fmt.Errorf("loader: exec 加载器（command=%s）已熔断，不再发起调用"+
			"（连续失败达到阈值，需人工核查子进程脚本或依赖的外部系统后重启进程恢复）",
			l.execCfg.Command)
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("loader: 序列化请求参数失败: %w", err)
	}

	err = callWithRetry(ctx, l.cfg, func(ctx context.Context) error {
		return l.callOnce(ctx, method, paramsJSON, result)
	})
	l.breaker.RecordResult(err)
	return err
}

// callOnce 发起一次子进程调用：拉起进程、写 Request 到 stdin、等待退出、
// 解码 stdout 的 Response。
//
// 超时处理：用 context.WithTimeout 派生一个带 l.cfg.Timeout 截止时间的子
// context 传给 exec.CommandContext——超时或外层 ctx 被取消时，
// exec.CommandContext 会向子进程发 Kill（Windows 上是 TerminateProcess，
// 见包文档），不是仅仅放弃等待：僵死子进程会累积，必须真的杀掉（任务
// 明确要求）。cmd.Wait() 返回后再判断是否命中了超时，命中时用 ErrTimeout
// 包装，供调用方与测试用 errors.Is 判定。
//
// cmd.Wait() 返回后（无论走哪条分支）子进程一定已经终止：这是 Go 标准库
// os/exec 包文档明确保证的契约——Wait() 只有在底层 OS 进程真正退出后
// 才会返回，不是"发个信号就假装完事"。因此本函数返回这件事本身就是
// 「进程已终止」的直接证据，测试不需要再额外查询进程表来二次确认
// （exec_test.go 的 TestExecLoader_TimeoutKillsProcess 文档详述了为什么
// 这比查进程表更可靠，以及为什么放弃了查进程表这个方向）。
func (l *ExecLoader) callOnce(ctx context.Context, method string, paramsJSON json.RawMessage, result any) error {
	callCtx, cancel := context.WithTimeout(ctx, l.cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(callCtx, l.execCfg.Command, l.execCfg.Args...)

	reqJSON, err := json.Marshal(Request{Method: method, Params: paramsJSON})
	if err != nil {
		return fmt.Errorf("loader: 序列化 Request 信封失败: %w", err)
	}
	cmd.Stdin = bytes.NewReader(reqJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("loader: 启动子进程 %s 失败: %w", l.execCfg.Command, err)
	}
	if l.onProcessStarted != nil {
		l.onProcessStarted(cmd.Process.Pid)
	}

	runErr := cmd.Wait()

	if callCtx.Err() != nil {
		// 超时或外层 ctx 取消触发了 CommandContext 的自动 Kill——
		// exec.CommandContext 的文档保证进程会被杀掉，这里不需要再手动
		// 调 cmd.Process.Kill()，只需要把这次失败归类成超时错误。
		return fmt.Errorf("loader: 子进程 %s 执行超时（超过 %v）: %w",
			l.execCfg.Command, l.cfg.Timeout, ErrTimeout)
	}

	if runErr != nil {
		return fmt.Errorf("loader: 子进程 %s 执行失败: %w（stderr: %s）",
			l.execCfg.Command, runErr, stderr.String())
	}

	var resp Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return fmt.Errorf("loader: 解析子进程 %s 的 stdout 失败: %w（stdout: %s）",
			l.execCfg.Command, err, stdout.String())
	}

	if resp.Error != "" {
		return fmt.Errorf("loader: 子进程 %s 返回业务错误: %s", l.execCfg.Command, resp.Error)
	}

	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("loader: 解析子进程 %s 的 result 字段失败: %w", l.execCfg.Command, err)
		}
	}

	return nil
}

// 编译期断言：ExecLoader 必须满足 Caller。
var _ Caller = (*ExecLoader)(nil)
