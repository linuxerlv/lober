// 本文件是 gRPC 加载器的公开入口（P3-04 第一半任务 4 的被调用侧）：把插件
// 进程的拉起、握手、崩溃重建接到 protocol.go 的协议与 reload.go 的替换逻辑上。
//
// # 熔断与重试直接复用 P2-03 的实现
//
// 计划任务 4 明确要求：**不要为 gRPC 写第二套熔断。** breaker 是按扩展点的失败
// 语义分流的（§7.4），与加载方式无关——一个 IPAM 连续失败五次该熔断，不因为它
// 是 gRPC 插件还是 shell 脚本而不同。因此本文件把 manager 包在 loader 包既有的
// 重试与熔断装饰器里，自己一行熔断逻辑都不写。
package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/lober/lober/internal/provider/loader"
)

// PluginConfig 是 gRPC 加载器的专属参数。
//
// 只有一个字段：插件可执行文件的位置。与 loader.ExecConfig/HTTPConfig 同一性质
// ——共用的超时、重试、熔断参数走 loader.Config，不混进来（那会让「哪些字段对
// 哪种加载器有意义」变得含糊，见 loader.Config 的文档）。
type PluginConfig struct {
	// BinaryPath 必须是一个**指向版本目录里可执行文件的符号链接**
	// （§7.3 第一项的原子替换形态，判定见 resolvePluginBinary）。
	BinaryPath string
}

// pluginName 是 go-plugin 的插件名，主程序与插件侧必须一致。
const pluginName = "provider"

// New 构造一个 gRPC 加载器。
//
// 启动校验（计划任务 4）：符号链接存在且指向一个可执行文件；协议版本能协商成功
// ——后者由 go-plugin 的握手完成，失败即 New 返回错误。**在启动时失败是刻意的**：
// 一个协议版本对不上的插件若被接受，表现是「provider 返回了无意义的结果」，
// 而那类错误极难追溯到版本不匹配这个真正的原因（§7.3 最后一项）。
func New(pluginCfg PluginConfig, sharedCfg loader.Config) (loader.Caller, error) {
	binary, err := resolvePluginBinary(pluginCfg.BinaryPath)
	if err != nil {
		return nil, err
	}

	start := func() (pluginInstance, error) { return startPlugin(binary) }

	first, err := start()
	if err != nil {
		return nil, fmt.Errorf("loader/grpc: 首次拉起插件 %q 失败: %w", binary, err)
	}

	m, err := newManagerWithInstance(first)
	if err != nil {
		first.kill()
		return nil, err
	}
	m.start = start

	// 套上 loader 包既有的重试与熔断（见文件头：不写第二套）。
	return loader.WithRetryAndBreaker(m, sharedCfg), nil
}

// startPlugin 拉起一个插件子进程并完成握手，返回可用的实例。
func startPlugin(binary string) (pluginInstance, error) {
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         map[string]goplugin.Plugin{pluginName: &providerPlugin{}},
		Cmd:             exec.Command(binary),
		// 只允许 gRPC：go-plugin 也支持 net/rpc，但两种协议并存意味着一个只
		// 实现了 net/rpc 的插件会被静默接受，而它的行为与本包的假设不同。
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		// 插件的 stderr 转进本进程的结构化日志。不转的话插件自己的报错会完全
		// 消失——而插件出问题时那几行 stderr 往往是唯一线索。
		Logger: nil,
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("loader/grpc: 与插件握手失败"+
			"（协议版本或 magic cookie 不匹配时会走到这里——版本对不上直接拒绝加载"+
			"是 §7.3 最后一项的硬要求，不拒绝的后果是主程序与插件对同一个方法的"+
			"参数理解不同，表现为「provider 返回了无意义的结果」且极难追溯）: %w", err)
	}

	raw, err := rpcClient.Dispense(pluginName)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("loader/grpc: 插件未提供 %q 服务: %w", pluginName, err)
	}

	inst, ok := raw.(*grpcClient)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("loader/grpc: 插件返回了意料之外的客户端类型 %T", raw)
	}
	return &processInstance{client: client, conn: inst}, nil
}

// processInstance 把「一个 go-plugin 客户端 + 它的 gRPC 连接」包成
// pluginInstance。
type processInstance struct {
	client *goplugin.Client
	conn   *grpcClient

	killOnce sync.Once
}

func (p *processInstance) Call(ctx context.Context, method string, params, result any) error {
	return p.conn.Call(ctx, method, params, result)
}

func (p *processInstance) ping(ctx context.Context) error {
	return p.conn.ping(ctx)
}

// kill 停止插件进程。用 sync.Once 保证幂等——pluginInstance 的接口文档要求
// 幂等，而 reload 的失败路径与正常路径都可能调它。
func (p *processInstance) kill() {
	p.killOnce.Do(func() {
		p.client.Kill()
	})
}

// providerPlugin 是 go-plugin 的 GRPCPlugin 实现。
//
// GRPCServer 只在插件侧被调用，GRPCClient 只在主程序侧被调用——同一个类型服务
// 两端是 go-plugin 的既有形态。
type providerPlugin struct {
	goplugin.NetRPCUnsupportedPlugin

	// Impl 只有插件侧会填（主程序侧构造这个类型时留 nil）。
	Impl PluginServer
}

// GRPCServer 在插件进程里注册服务。
func (p *providerPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	if p.Impl == nil {
		return fmt.Errorf("loader/grpc: 插件侧必须提供 PluginServer 实现")
	}
	s.RegisterService(serviceDesc(p.Impl), p.Impl)
	return nil
}

// GRPCClient 在主程序里构造客户端。
func (p *providerPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &grpcClient{conn: c}, nil
}

// ServePlugin 是插件实现方的入口：写一个 main 调用它即可。
//
// 放在本包而不是让插件作者自己拼 go-plugin 的 ServeConfig：握手配置与插件名
// 必须与主程序侧逐字节一致，让插件作者手写那两个值就是把「版本协商」这道闸门
// 的正确性交给了每一个插件作者。
func ServePlugin(impl PluginServer) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         map[string]goplugin.Plugin{pluginName: &providerPlugin{Impl: impl}},
		GRPCServer:      goplugin.DefaultGRPCServer,
	})
}

// Reload 重新拉起插件并替换当前实例（§7.3）。
//
// 顺序与失败语义见 reload.go 的文件头与 manager.reloadWith。
func (m *manager) Reload(ctx context.Context) error {
	if m.start == nil {
		return fmt.Errorf("loader/grpc: 本 manager 没有 start 函数，无法 reload" +
			"（只有通过 New 构造的实例支持 reload；测试用的 manager 不支持）")
	}
	return m.reloadWith(ctx, m.start)
}

// ensureAlive 在调用前确认插件进程还活着，不活则重建。
//
// go-plugin 的 Client.Exited() 报告子进程是否已退出。插件崩溃后不重建的话，
// 之后每一次调用都会失败直到运维手工 reload——而插件崩溃往往是偶发的（一次
// 空指针），自动重建能让系统自己恢复。
//
// **重建走的是同一条 reloadWith 路径**（含探活闸门）：崩溃重建与运维主动 reload
// 在正确性要求上没有区别，用两条路径会让其中一条的探活被遗漏。
func (m *manager) ensureAlive(ctx context.Context) {
	ref := m.current.Load()
	if ref == nil {
		return
	}
	pi, ok := ref.inst.(*processInstance)
	if !ok || !pi.client.Exited() {
		return
	}
	if err := m.Reload(ctx); err != nil {
		slog.Warn("检测到插件进程已退出，自动重建失败",
			slog.Any("error", err))
	}
}
