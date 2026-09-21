// 本文件锁住计划任务 14 要求的三类行为：成功响应的字段形态与
// Content-Type；五个 Store sentinel error 各自映射到正确的 HTTP 状态码
// 与业务码且 message 非空；未识别错误不泄露内部细节。
package apierr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

// decodeEnvelope 把 httptest.ResponseRecorder 的响应体解成 Envelope，
// 供各测试断言字段值，避免每个测试重复写 json.Unmarshal 样板代码。
func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应体不是合法 JSON：%v，原文：%s", err, rec.Body.String())
	}
	return env
}

// TestWriteOK_EnvelopeShape 锁住成功响应的三个字段形态与 Content-Type
// （计划任务 14「测试要锁住的行为」第一条）。vben 的默认响应拦截器按
// code/data/successCode:0 解包（SPEC §12.5），三个字段名与 successCode
// 语义对不上，前端就无法解包出 data。
func TestWriteOK_EnvelopeShape(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]string{"taskId": "t-1"}

	WriteOK(rec, payload)

	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q，vben 的响应拦截器按 JSON 解析响应体（SPEC §12.5），\n"+
			"非 JSON 的 Content-Type 会导致前端 fetch 层直接抛异常而不是走业务错误处理", got)
	}

	env := decodeEnvelope(t, rec)
	if env.Code != CodeOK {
		t.Errorf("成功响应 code = %d，want %d（vben 拦截器的 successCode 硬编码为 0，"+
			"不是 0 就会被前端当成失败，即使 HTTP 状态码是 200）", env.Code, CodeOK)
	}
	if env.Message != "" {
		t.Errorf("成功响应 message = %q，want 空串——message 是留给失败时展示给运维的通道，"+
			"成功时非空会让 UI 产生误导性提示", env.Message)
	}
	gotData, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("重新序列化 env.Data 失败：%v", err)
	}
	wantData, _ := json.Marshal(payload)
	if string(gotData) != string(wantData) {
		t.Errorf("env.Data 序列化结果 = %s, want %s——data 必须原样携带调用方传入的业务数据",
			gotData, wantData)
	}
}

// TestWriteError_SentinelMapping 逐一验证五个 sentinel error 映射到计划
// 任务 14 指定的 HTTP 状态码与业务码，且 message 非空（UI 直接展示它，
// 计划任务 14「测试要锁住的行为」第二条）。
//
// 契约偏差说明：计划任务 14 原文写「五个 Store sentinel error」，但
// store.go 实际的五个 sentinel 是 ErrNotFound/ErrConflict/
// ErrDuplicateSeq/ErrActiveTaskExists/ErrStillReferenced；其中
// ErrDuplicateSeq 的文档明确写着「这不是错误路径，handler 应回成功」
// （store.go 第 26-29 行），把它映射成 HTTP 错误响应会直接违反它自己的
// 契约语义。而计划给出的错误码表第 1008 项「拒绝重装」在类型上只能对应
// contract.ErrNotReinstallable（定义于 service.go，是 StateMachine.Retry
// 的拒绝信号，不是 Store 方法的返回值）。用 ErrNotReinstallable 顶替
// ErrDuplicateSeq 才能让「五个」这个数量与错误码表的 1004-1008 五项一一
// 对应，这是最保守、最贴合两处文本本意的读法：ErrDuplicateSeq 不产生
// HTTP 错误响应（它从不应该到达这里），ErrNotReinstallable 补齐 1008。
// 详见本任务返回的 contractDeviations。
func TestWriteError_SentinelMapping(t *testing.T) {
	cases := []struct {
		name       string
		sentinel   error
		wantStatus int
		wantCode   Code
	}{
		{
			name:       "ErrNotFound",
			sentinel:   contract.ErrNotFound,
			wantStatus: 404,
			wantCode:   CodeNotFound,
		},
		{
			name:       "ErrConflict",
			sentinel:   contract.ErrConflict,
			wantStatus: 409,
			wantCode:   CodeConflict,
		},
		{
			name:       "ErrActiveTaskExists",
			sentinel:   contract.ErrActiveTaskExists,
			wantStatus: 409,
			wantCode:   CodeActiveTaskExists,
		},
		{
			name:       "ErrStillReferenced",
			sentinel:   contract.ErrStillReferenced,
			wantStatus: 409,
			wantCode:   CodeStillReferenced,
		},
		{
			name:       "ErrNotReinstallable",
			sentinel:   contract.ErrNotReinstallable,
			wantStatus: 409,
			wantCode:   CodeReinstallDenied,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			// 用 fmt.Errorf %w 包一层，模拟 handler 实际拿到的错误常常经过
			// 至少一层 wrap；WriteError 必须用 errors.Is 判定而不是直接
			// 比较错误值，否则一旦调用方 wrap 了 sentinel 就会误判为
			// 未识别错误（本文件另一测试锁住的行为）。
			wrapped := fmt.Errorf("对 %s 求值时：%w", tc.name, tc.sentinel)

			WriteError(rec, wrapped)

			if rec.Code != tc.wantStatus {
				t.Errorf("%s: HTTP 状态码 = %d, want %d——计划任务 14 为这个\n"+
					"sentinel 指定的状态码，handler 与前端路由的错误页判断都依赖它",
					tc.name, rec.Code, tc.wantStatus)
			}
			env := decodeEnvelope(t, rec)
			if env.Code != tc.wantCode {
				t.Errorf("%s: 业务码 = %d, want %d", tc.name, env.Code, tc.wantCode)
			}
			if env.Message == "" {
				t.Errorf("%s: message 为空——UI 直接把 message 展示给运维，"+
					"空文案会让运维看到一片空白，不知道发生了什么", tc.name)
			}
			if env.Data != nil {
				t.Errorf("%s: env.Data = %v, want nil——失败响应不应携带业务数据", tc.name, env.Data)
			}
		})
	}
}

// TestWriteError_ReinstallDeniedMessageIsActionable 单独锁住拒绝重装这一条
// 文案的内容，而不只是非空。计划任务 14 明确要求这条要说明「该失败状态的
// 机器已完成安装，重装会擦除磁盘数据」，不能是干巴巴的「操作被拒绝」——
// 运维看到「拒绝」两个字不会知道为什么，也不知道强行绕过的后果。
func TestWriteError_ReinstallDeniedMessageIsActionable(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteError(rec, contract.ErrNotReinstallable)

	env := decodeEnvelope(t, rec)
	if !strings.Contains(env.Message, "已完成安装") && !strings.Contains(env.Message, "已装好") {
		t.Errorf("message = %q，缺少「机器已完成安装」这层说明——运维需要知道\n"+
			"为什么不能重装，而不只是知道「不能」", env.Message)
	}
	if !strings.Contains(env.Message, "擦除") && !strings.Contains(env.Message, "擦掉") &&
		!strings.Contains(env.Message, "数据") {
		t.Errorf("message = %q，缺少「重装会擦除磁盘数据」这层风险说明——这是运维\n"+
			"决定是否要走人工介入路径的关键信息，SPEC §3.4 的安全约束必须体现在人面文案里",
			env.Message)
	}
}

// TestWriteError_UnrecognizedErrorHidesDetail 锁住计划任务 14 最关键的一条
// 安全行为：数据库错误里可能带连接串与用户名，这些细节只能进服务端日志，
// 绝不能出现在 HTTP 响应体里（计划任务 14「测试要锁住的行为」第三条）。
func TestWriteError_UnrecognizedErrorHidesDetail(t *testing.T) {
	rec := httptest.NewRecorder()

	dsn := "postgres://lober_admin:s3cr3t-p@ss@10.0.0.5:5432/lober?sslmode=disable"
	dbErr := errors.New("dial tcp: connection refused to " + dsn)

	WriteError(rec, dbErr)

	body := rec.Body.String()
	if strings.Contains(body, "lober_admin") || strings.Contains(body, "s3cr3t-p@ss") {
		t.Fatalf("响应体泄露了数据库连接串/用户名：%s——未识别错误必须回通用文案，\n"+
			"内部细节只能记进 log/slog，这是本任务最要紧的一条安全边界", body)
	}

	env := decodeEnvelope(t, rec)
	if env.Code != CodeInternal {
		t.Errorf("未识别错误的业务码 = %d, want %d（内部错误）", env.Code, CodeInternal)
	}
	if env.Message == "" {
		t.Errorf("未识别错误的 message 为空——即使不泄露细节，也要给运维一句\n" +
			"「服务器内部错误，请查看日志」之类的通用提示，不能是空白")
	}
	if strings.Contains(env.Message, dsn) {
		t.Errorf("message 直接包含了原始错误文本：%q", env.Message)
	}
	if rec.Code != 500 {
		t.Errorf("未识别错误的 HTTP 状态码 = %d, want 500", rec.Code)
	}
}

// TestWriteError_UnrecognizedErrorIsLogged 验证未识别错误的原始细节确实被
// 记录到了 slog，而不是被直接丢弃——不泄露到 HTTP 响应不等于可以不留痕迹，
// 运维排障时仍需要在服务端日志里找到真正的错误原因。
func TestWriteError_UnrecognizedErrorIsLogged(t *testing.T) {
	var buf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prevLogger)

	rec := httptest.NewRecorder()
	dbErr := errors.New("dial tcp: connection refused to postgres://lober_admin:s3cr3t-p@ss@10.0.0.5:5432/lober")

	WriteError(rec, dbErr)

	logged := buf.String()
	if !strings.Contains(logged, "lober_admin") {
		t.Errorf("服务端日志里没有找到原始错误细节，日志内容：%q——细节不进 HTTP 响应\n"+
			"不代表可以彻底丢弃，运维排障时唯一的线索就是服务端日志", logged)
	}
}

// TestNew_BusinessError 验证「业务错误」类型可供 handler 主动构造，且走
// 与 sentinel 映射相同的 WriteError 输出函数（计划任务 14 要求的另一个
// 产出：这两条路径必须汇合到同一个函数，不能是两套响应写法）。
func TestNew_BusinessError(t *testing.T) {
	rec := httptest.NewRecorder()

	businessErr := New(400, CodeValidationFailed, "磁盘策略选择了 by-model，但未提供型号串")

	WriteError(rec, businessErr)

	if rec.Code != 400 {
		t.Errorf("HTTP 状态码 = %d, want 400——业务错误类型必须携带调用方指定的状态码，"+
			"不能被 WriteError 强行改写", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Code != CodeValidationFailed {
		t.Errorf("业务码 = %d, want %d", env.Code, CodeValidationFailed)
	}
	if env.Message != "磁盘策略选择了 by-model，但未提供型号串" {
		t.Errorf("message = %q，want 原样保留调用方传入的文案", env.Message)
	}
}

// TestError_ErrorMethod 验证 *Error 实现了标准 error 接口且 Error() 内容
// 可用于服务端日志排查（不是空字符串或与业务无关的占位文本）。
func TestError_ErrorMethod(t *testing.T) {
	err := New(403, CodeForbidden, "无权限执行该操作")
	var _ error = err // 编译期确认满足 error 接口

	if got := err.Error(); !strings.Contains(got, "无权限执行该操作") {
		t.Errorf("Error() = %q，应包含 message 内容，便于服务端日志排查", got)
	}
}
