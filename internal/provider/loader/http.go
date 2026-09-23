// 本文件实现 http 加载器：把契约方法调用转成一次 HTTP POST（SPEC §7.2：
// 「exec/http：子进程/webhook，JSON 进 JSON 出」，示例配置里 notify 用
// `type: http, url: ...`）。
//
// 请求体是 Request 信封的 JSON 编码，响应体期望是 Response 信封的 JSON
// 编码。2xx 状态码视为传输层成功（再看 Response.Error 是否非空判定业务
// 成败），非 2xx 视为传输层失败（不尝试解析 body 为 Response，因为非 2xx
// 响应体的格式不受本协议约束，可能是任意的错误页面或反代错误信息）。
package loader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// HTTPConfig 是构造 HTTPLoader 需要的专属参数。
type HTTPConfig struct {
	// URL 是接收 POST 请求的目标地址。必须非空——与 exec 的 Command 同理，
	// 启动期校验，不留到运行期才发现。
	URL string

	// Client 是发起请求用的 HTTP 客户端，nil 时使用 http.DefaultClient。
	// 暴露此字段主要供测试注入指向 httptest.Server 的客户端配置（如自定义
	// Transport），生产路径通常留空即可——每次调用的超时通过 ctx 派生的
	// deadline 控制（见 callOnce），不依赖 Client.Timeout 这个全局值，
	// 因为不同调用可能来自不同 loader.Config 的 Timeout 设置，共用一个
	// http.Client 时 Client.Timeout 是错误的控制点。
	Client *http.Client
}

// HTTPLoader 是 Caller 的 HTTP POST 实现。
type HTTPLoader struct {
	httpCfg HTTPConfig
	cfg     Config
	breaker *circuitBreaker
	client  *http.Client
}

// NewHTTPLoader 构造一个 HTTPLoader。httpCfg.URL 为空时返回错误（启动
// 校验）。
func NewHTTPLoader(httpCfg HTTPConfig, cfg Config) (*HTTPLoader, error) {
	if httpCfg.URL == "" {
		return nil, fmt.Errorf("loader: http 加载器缺少 url 参数" +
			"（配置里选了 http 类型但没给目标地址，启动时拒绝，" +
			"不能等到第一次调用时才在运行期发现）")
	}
	cfg = cfg.withDefaults()

	client := httpCfg.Client
	if client == nil {
		client = http.DefaultClient
	}

	return &HTTPLoader{
		httpCfg: httpCfg,
		cfg:     cfg,
		breaker: newCircuitBreaker(cfg.BreakerThreshold),
		client:  client,
	}, nil
}

// Call 实现 Caller。行为与 ExecLoader.Call 对称：熔断打开时直接短路，
// 否则按 cfg 的重试策略执行一到多次 HTTP 请求。
func (l *HTTPLoader) Call(ctx context.Context, method string, params any, result any) error {
	if !l.breaker.Allow() {
		return fmt.Errorf("loader: http 加载器（url=%s）已熔断，不再发起调用"+
			"（连续失败达到阈值，需人工核查目标服务后重启进程恢复）", l.httpCfg.URL)
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

// callOnce 发起一次 HTTP POST：请求体是 Request 信封，用 ctx 派生的
// deadline 控制超时。
//
// 超时判定：ctx 超时会导致 http.Client.Do 返回一个包裹了
// context.DeadlineExceeded 的 *url.Error（net/http 的标准行为），本函数
// 用 errors.Is 判定后归类为 ErrTimeout，与 exec 加载器保持一致的超时
// 错误类型，供上层统一用 errors.Is(err, ErrTimeout) 判断，不需要区分
// 「这次超时来自子进程还是 HTTP 请求」。
func (l *HTTPLoader) callOnce(ctx context.Context, method string, paramsJSON json.RawMessage, result any) error {
	callCtx, cancel := context.WithTimeout(ctx, l.cfg.Timeout)
	defer cancel()

	reqJSON, err := json.Marshal(Request{Method: method, Params: paramsJSON})
	if err != nil {
		return fmt.Errorf("loader: 序列化 Request 信封失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, l.httpCfg.URL, bytes.NewReader(reqJSON))
	if err != nil {
		return fmt.Errorf("loader: 构造 HTTP 请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := l.client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("loader: HTTP 请求 %s 超时（超过 %v）: %w",
				l.httpCfg.URL, l.cfg.Timeout, ErrTimeout)
		}
		return fmt.Errorf("loader: HTTP 请求 %s 失败: %w", l.httpCfg.URL, err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("loader: 读取 HTTP 响应体失败: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("loader: HTTP 请求 %s 返回非 2xx 状态码 %d（body: %s）",
			l.httpCfg.URL, httpResp.StatusCode, truncateForError(body))
	}

	var resp Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("loader: 解析 HTTP 响应体失败: %w（body: %s）", err, truncateForError(body))
	}

	if resp.Error != "" {
		return fmt.Errorf("loader: HTTP 请求 %s 返回业务错误: %s", l.httpCfg.URL, resp.Error)
	}

	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("loader: 解析 HTTP 响应 result 字段失败: %w", err)
		}
	}

	return nil
}

// truncateForErrorMaxLen 是错误信息里回显的响应体最大长度——外部服务的
// 错误页面可能是几十 KB 的 HTML，原样塞进错误信息会淹没日志。
const truncateForErrorMaxLen = 500

// truncateForError 截断 body 用于错误信息展示，超长时追加省略标记。
func truncateForError(body []byte) string {
	if len(body) <= truncateForErrorMaxLen {
		return string(body)
	}
	return string(body[:truncateForErrorMaxLen]) + "...(截断)"
}

// 编译期断言：HTTPLoader 必须满足 Caller。
var _ Caller = (*HTTPLoader)(nil)
