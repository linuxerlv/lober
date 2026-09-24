// 本文件定义 gRPC 插件的协议与握手（P3-04 第一半任务 1），依据 SPEC §7.2、§7.3。
//
// # 为什么是 hashicorp/go-plugin
//
// §7.2 指名了它。它提供进程管理、握手、双向通信与崩溃检测——自己实现这些是
// 几周的工作。§0.2 同时排除了 Go 的原生 plugin 构建模式（需重启、无崩溃隔离、
// Windows 不支持），所以「独立子进程 + gRPC」是剩下的唯一形态。
//
// # 为什么载荷是 JSON 而不是 protobuf
//
// 本仓库没有 protoc 工具链（已用 command -v 核对），而更重要的是：既有的
// loader.Caller 抽象本身就是「方法名 + JSON 参数 → JSON 结果」这一种形状
// （见 loader/protocol.go 的 Caller 文档：它刻意不按七个契约接口各写一个方法，
// 传输层完全不需要知道那七个接口长什么样）。
//
// 因此这里定义**一个通用的 gRPC 服务**，签名与 Caller 逐一对应：
//
//	Call(method string, params_json bytes) → (result_json bytes, error)
//
// 收益是直接的：新增第八个 provider 类型时，这一层一行代码都不用改——与
// exec/http 两种加载器保持同一个性质。代价是失去 protobuf 的字段级类型检查，
// 但那层检查在 exec/http 上本来就没有，三种加载方式的强度一致反而更可预期。
//
// 载荷编码用一个注册进 grpc-go 的 raw codec（见 rawCodec）：**传输层仍然是
// 真正的 gRPC**（HTTP/2 帧、流、状态码、go-plugin 的崩溃检测都照常），只是
// 消息体不经 protobuf 序列化。
//
// # 协议版本协商是硬要求
//
// §7.3 最后一项：**版本对不上直接拒绝加载。** 不拒绝的后果是插件与主程序对同一
// 个方法的参数理解不同——而那类错误的表现是「provider 返回了无意义的结果」，
// 极难追溯到版本不匹配这个真正的原因。go-plugin 的 HandshakeConfig 做的正是
// 这件事，MagicCookie 还额外保证「被误当插件执行的普通程序」不会被接受。
package grpc

import (
	"context"
	"encoding/json"
	"fmt"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"

	"github.com/lober/lober/internal/provider/loader"
)

// ProtocolVersion 是 lober provider 插件协议的版本号。
//
// **任何对「方法名集合、参数形状、结果形状」的不兼容改动都必须递增它。**
// 递增之后旧插件会在握手阶段被拒绝加载——那正是想要的：宁可插件加载失败（运维
// 立刻看到），也不要主程序与插件对同一个方法的参数理解不同（表现为 provider
// 返回无意义结果，极难追溯）。
const ProtocolVersion = 1

// magicCookieKey/magicCookieValue 是 go-plugin 的「这确实是一个 lober 插件」
// 握手口令。
//
// 它不是安全机制（值是公开的），作用是让「把一个普通可执行文件配成插件路径」
// 这种配置错误在握手阶段就以明确的错误退出，而不是让 go-plugin 去解析那个程序
// 的 stdout 输出、最终报一个含义不明的协议错误。
const (
	magicCookieKey   = "LOBER_PROVIDER_PLUGIN"
	magicCookieValue = "b7f3c1e2-lober-provider-v1"
)

// Handshake 是主程序与插件共用的握手配置。插件实现方必须用同一份值。
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  ProtocolVersion,
	MagicCookieKey:   magicCookieKey,
	MagicCookieValue: magicCookieValue,
}

// rawCodecName 是注册进 grpc-go 的编解码器名称。
//
// 不叫 "json"：那个名字可能与其它库注册的 json codec 冲突（grpc-go 的 codec
// 注册表是全局的，同名会互相覆盖，而覆盖不报错）。带项目前缀能避免这类难查的
// 冲突。
const rawCodecName = "lober-raw"

// rawCodec 是一个直通编解码器：消息本身就是 []byte，不做任何转换。
//
// 存在的理由见文件头——载荷是 JSON 字节，由上层的 loader 协议决定形状，gRPC
// 这一层只负责搬运。Marshal/Unmarshal 都只接受 *[]byte / []byte，其它类型直接
// 报错而不是尝试兜底：兜底会让一个类型用错的调用点在运行期产出空消息，而空消息
// 在对端表现为「参数全是零值」——又是一类难追溯的错误。
type rawCodec struct{}

func (rawCodec) Name() string { return rawCodecName }

func (rawCodec) Marshal(v any) ([]byte, error) {
	b, ok := v.(*[]byte)
	if !ok {
		return nil, fmt.Errorf("loader/grpc: rawCodec.Marshal 收到 %T, want *[]byte", v)
	}
	return *b, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	b, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("loader/grpc: rawCodec.Unmarshal 收到 %T, want *[]byte", v)
	}
	// 必须复制：grpc-go 复用接收缓冲区，直接持有 data 会在下一条消息到达时
	// 被覆盖——症状是「偶尔收到上一次调用的参数」，只在高并发下出现。
	out := make([]byte, len(data))
	copy(out, data)
	*b = out
	return nil
}

func init() {
	encoding.RegisterCodec(rawCodec{})
}

// serviceName/methodName 是 gRPC 的服务与方法全名。
//
// 手写而不是 protoc 生成：形态就是这两个字符串加一个 ServiceDesc，生成器对这个
// 规模没有价值，而引入 protoc 会给气隙环境的构建链多一个必须预装的工具。
const (
	serviceName = "lober.provider.v1.Provider"
	methodCall  = "/" + serviceName + "/Call"
	methodPing  = "/" + serviceName + "/Ping"
)

// PluginServer 是插件实现方要实现的接口。
//
// 与 loader.Caller 的差异只有一处：Ping。加载器侧需要它做健康检查（§7.3 第三项
// 的探活），而 Caller 是纯业务调用抽象，不该被塞进一个只有 gRPC 加载器才需要的
// 方法。
type PluginServer interface {
	// Call 执行一次 provider 调用。paramsJSON 是上层协议的请求体，返回值是
	// 响应体，形状见 loader/protocol.go。
	Call(ctx context.Context, method string, paramsJSON []byte) (resultJSON []byte, err error)

	// Ping 供健康检查用。返回 error 即视为不健康。
	//
	// 它必须是廉价且不依赖外部系统的：探活的用途是判断「插件进程活着且能应答」，
	// 若 Ping 里去连一次 IPAM，那么 IPAM 短暂不可用会被误判成插件坏了，于是
	// reload 被拒绝——而那次 reload 可能正是为了修 IPAM 的对接问题。
	Ping(ctx context.Context) error
}

// serviceDesc 是手写的 gRPC 服务描述，供插件侧注册。
//
// 两个方法的 handler 都走 raw 载荷：dec 解出 []byte，交给实现，再把结果作为
// []byte 返回。
func serviceDesc(impl PluginServer) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: serviceName,
		HandlerType: (*PluginServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "Call",
				Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
					var payload []byte
					if err := dec(&payload); err != nil {
						return nil, err
					}
					var req loader.Request
					if err := json.Unmarshal(payload, &req); err != nil {
						return nil, fmt.Errorf("loader/grpc: 解析请求信封失败: %w", err)
					}
					result, err := impl.Call(ctx, req.Method, req.Params)
					if err != nil {
						return nil, err
					}
					return &result, nil
				},
			},
			{
				MethodName: "Ping",
				Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
					var ignored []byte
					if err := dec(&ignored); err != nil {
						return nil, err
					}
					if err := impl.Ping(ctx); err != nil {
						return nil, err
					}
					empty := []byte{}
					return &empty, nil
				},
			},
		},
	}
}

// grpcClient 是主程序侧的客户端，实现 loader.Caller 与探活。
type grpcClient struct {
	conn *grpc.ClientConn
}

// Call 实现 loader.Caller：把方法名与参数打成信封发给插件，再把响应解回
// result。
//
// 复用 loader 包的 EncodeRequest/DecodeResponse，让三种加载器的**线上协议完全
// 一致**——于是一个 provider 实现方可以先写成 exec 脚本，需要长连接时改成 gRPC
// 插件而不用改协议理解。
func (c *grpcClient) Call(ctx context.Context, method string, params any, result any) error {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("loader/grpc: 序列化方法 %q 的参数失败: %w", method, err)
	}
	payload, err := json.Marshal(loader.Request{Method: method, Params: paramsJSON})
	if err != nil {
		return fmt.Errorf("loader/grpc: 序列化方法 %q 的请求信封失败: %w", method, err)
	}

	var respBody []byte
	if err := c.conn.Invoke(ctx, methodCall, &payload, &respBody,
		grpc.CallContentSubtype(rawCodecName)); err != nil {
		return fmt.Errorf("loader/grpc: 调用插件方法 %q 失败: %w", method, err)
	}

	var resp loader.Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return fmt.Errorf("loader/grpc: 解析方法 %q 的响应信封失败: %w", method, err)
	}
	if resp.Error != "" {
		return fmt.Errorf("loader/grpc: 插件方法 %q 返回错误: %s", method, resp.Error)
	}
	// result 为 nil 表示调用方不关心返回值（Release/Deregister 那类只关心
	// 成功与否的方法），此时即使 Result 非空也不解码——与 exec/http 一致。
	if result == nil || len(resp.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Result, result); err != nil {
		return fmt.Errorf("loader/grpc: 解码方法 %q 的返回值失败: %w", method, err)
	}
	return nil
}

// ping 向插件发一次探活。
func (c *grpcClient) ping(ctx context.Context) error {
	empty := []byte{}
	var out []byte
	if err := c.conn.Invoke(ctx, methodPing, &empty, &out,
		grpc.CallContentSubtype(rawCodecName)); err != nil {
		return fmt.Errorf("loader/grpc: 插件探活失败: %w", err)
	}
	return nil
}

var _ loader.Caller = (*grpcClient)(nil)
