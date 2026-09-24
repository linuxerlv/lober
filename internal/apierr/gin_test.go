// 本文件锁住 gin 适配层三个函数（OK / Fail / Abort）的行为：成功响应、
// 每个已知 sentinel、*Error、validator.ValidationErrors 的每个 tag、
// 以及未识别错误的安全边界（响应体不含原始错误文本）。这层适配不重写
// internal/apierr 现有的错误码映射表，只是把同一套判定逻辑接到
// gin.Context 上，因此测试只锁行为，不重复验证 apierr_test.go 已经
// 锁住的映射表内容。
package apierr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/lober/lober/internal/core/contract"
)

func init() {
	// 测试环境固定为 TestMode，避免 gin 默认的 debug 模式在每次建 Engine
	// 时往 stdout 打警告，淹掉 go test -v 的输出。SetMode 是包级全局
	// 状态，但只影响日志噪音，不影响响应体内容，测试之间不会互相干扰。
	gin.SetMode(gin.TestMode)
}

// newTestContext 造一个绑定了 httptest.ResponseRecorder 的 *gin.Context，
// 避免每个测试重复写 gin.CreateTestContext 样板代码。
func newTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	return c, rec
}

// decodeGinEnvelope 与 apierr_test.go 的 decodeEnvelope 作用相同，只是
// 复用本文件已有的 rec 变量名习惯，避免跨文件耦合私有 helper。
func decodeGinEnvelope(t *testing.T, rec *httptest.ResponseRecorder) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应体不是合法 JSON：%v，原文：%s", err, rec.Body.String())
	}
	return env
}

// TestOK_EnvelopeShape 锁住 OK 的响应形态：HTTP 200、CodeOK、Message 空串、
// Data 原样携带调用方传入的数据。这与标准库版本 WriteOK 的契约完全一致
// （契约 §3：OK 就是 WriteOK 的 gin 版本）。
func TestOK_EnvelopeShape(t *testing.T) {
	c, rec := newTestContext()
	payload := map[string]string{"taskId": "t-1"}

	OK(c, payload)

	if rec.Code != 200 {
		t.Errorf("HTTP 状态码 = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q——前端拦截器按 JSON 解析响应体，"+
			"Content-Type 不对会导致 fetch 层直接抛异常", got, "application/json; charset=utf-8")
	}

	env := decodeGinEnvelope(t, rec)
	if env.Code != CodeOK {
		t.Errorf("env.Code = %d, want %d", env.Code, CodeOK)
	}
	if env.Message != "" {
		t.Errorf("env.Message = %q, want 空串", env.Message)
	}
	gotData, _ := json.Marshal(env.Data)
	wantData, _ := json.Marshal(payload)
	if string(gotData) != string(wantData) {
		t.Errorf("env.Data = %s, want %s", gotData, wantData)
	}
}

// TestFail_SentinelMapping 逐一验证 Fail 对已知 sentinel error 的处理，
// 断言状态码与业务码都对——这一步验证 Fail 确实复用了 lookupSentinel，
// 而不是自建一份映射表（契约 §3：三者内部复用现有判定逻辑，不重写映射表）。
func TestFail_SentinelMapping(t *testing.T) {
	cases := []struct {
		name       string
		sentinel   error
		wantStatus int
		wantCode   Code
	}{
		{"ErrNotFound", contract.ErrNotFound, 404, CodeNotFound},
		{"ErrConflict", contract.ErrConflict, 409, CodeConflict},
		{"ErrActiveTaskExists", contract.ErrActiveTaskExists, 409, CodeActiveTaskExists},
		{"ErrStillReferenced", contract.ErrStillReferenced, 409, CodeStillReferenced},
		{"ErrNotReinstallable", contract.ErrNotReinstallable, 409, CodeReinstallDenied},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newTestContext()
			// 模拟 handler 实际拿到的错误经过至少一层 wrap，验证 Fail 用
			// errors.Is 判定而不是直接比较错误值。
			wrapped := fmt.Errorf("对 %s 求值时：%w", tc.name, tc.sentinel)

			Fail(c, wrapped)

			if rec.Code != tc.wantStatus {
				t.Errorf("%s: HTTP 状态码 = %d, want %d", tc.name, rec.Code, tc.wantStatus)
			}
			env := decodeGinEnvelope(t, rec)
			if env.Code != tc.wantCode {
				t.Errorf("%s: 业务码 = %d, want %d", tc.name, env.Code, tc.wantCode)
			}
			if env.Message == "" {
				t.Errorf("%s: message 为空", tc.name)
			}
		})
	}
}

// TestFail_BusinessError 验证 Fail 对 handler 主动构造的 *Error 类型按其
// 携带的状态码/业务码/文案原样输出，不做改写。
func TestFail_BusinessError(t *testing.T) {
	c, rec := newTestContext()
	businessErr := New(400, CodeValidationFailed, "磁盘策略选择了 by-model，但未提供型号串")

	Fail(c, businessErr)

	if rec.Code != 400 {
		t.Errorf("HTTP 状态码 = %d, want 400", rec.Code)
	}
	env := decodeGinEnvelope(t, rec)
	if env.Code != CodeValidationFailed {
		t.Errorf("业务码 = %d, want %d", env.Code, CodeValidationFailed)
	}
	if env.Message != "磁盘策略选择了 by-model，但未提供型号串" {
		t.Errorf("message = %q，want 原样保留调用方传入的文案", env.Message)
	}
}

// TestFail_UnrecognizedErrorHidesDetail 锁住安全边界：未识别错误的响应体
// 绝不能包含原始错误文本，完整信息只进 log/slog（契约 §2 的安全边界，
// 也是本任务要求「必须断言响应体不含原始错误文本」的那一条）。
func TestFail_UnrecognizedErrorHidesDetail(t *testing.T) {
	c, rec := newTestContext()

	dsn := "postgres://lober_admin:s3cr3t-p@ss@10.0.0.5:5432/lober?sslmode=disable"
	dbErr := errors.New("dial tcp: connection refused to " + dsn)

	Fail(c, dbErr)

	body := rec.Body.String()
	if containsSubstring(body, "lober_admin") || containsSubstring(body, "s3cr3t-p@ss") ||
		containsSubstring(body, dbErr.Error()) {
		t.Fatalf("响应体泄露了原始错误文本：%s——未识别错误必须回通用文案，"+
			"内部细节只能记进 log/slog", body)
	}

	env := decodeGinEnvelope(t, rec)
	if env.Code != CodeInternal {
		t.Errorf("未识别错误的业务码 = %d, want %d", env.Code, CodeInternal)
	}
	if env.Message == "" {
		t.Errorf("未识别错误的 message 为空")
	}
	if rec.Code != 500 {
		t.Errorf("未识别错误的 HTTP 状态码 = %d, want 500", rec.Code)
	}
}

// ---- validator.ValidationErrors 覆盖 ----

// validationTagFixture 是一个专供测试用的结构体，用标签把任务要求覆盖的
// 八个 tag 全部凑出校验失败：required、min、max、oneof、email、ip、mac、
// hostname。用的是 binding 标签而不是 validate 标签——gin 的
// defaultValidator.lazyinit() 调了 SetTagName("binding")，handler 里
// c.ShouldBindJSON 实际读的是 binding 标签，validate 标签在这个引擎上
// 不会被识别（见本文件 collectValidationErrors 的注释）。字段名故意与
// json tag 不同（Go 字段大写驼峰，json 标签小写+下划线风格），这样才能
// 真正验证 Fail 的文案用的是 json tag 而不是 Go 字段名——如果实现偷懒
// 用了 StructField()，这里会立刻测出来。
type validationTagFixture struct {
	RequiredField string `json:"required_field" binding:"required"`
	MinField      int    `json:"min_field" binding:"min=3"`
	MaxField      int    `json:"max_field" binding:"max=10"`
	OneofField    string `json:"oneof_field" binding:"oneof=a b c"`
	EmailField    string `json:"email_field" binding:"email"`
	IPField       string `json:"ip_field" binding:"ip"`
	MACField      string `json:"mac_field" binding:"mac"`
	HostnameField string `json:"hostname_field" binding:"hostname"`
}

// collectValidationErrors 用 gin binding 实际使用的那个 validator 引擎
// （binding.Validator.Engine()，即 gin.go 的 init() 注册了 json 标签命名
// 函数的那个全局单例）校验一个刻意违反单个 tag 的结构体实例，返回该
// tag 对应的 validator.ValidationErrors。
//
// 不能用 validator.New() 现造一个新引擎——那样拿到的 FieldError.Field()
// 只会是 Go 字段名，因为 RegisterTagNameFunc 是注册在具体的 *validator.
// Validate 实例上的，不是全局默认行为。生产环境里 handler 通过
// c.ShouldBindJSON 触发校验，走的正是 binding.Validator 这个单例，所以
// 测试必须用同一个实例，否则测的是一个生产环境不会发生的路径。
func collectValidationErrors(t *testing.T, fixture validationTagFixture) validator.ValidationErrors {
	t.Helper()
	engine, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		t.Fatalf("binding.Validator.Engine() 不是 *validator.Validate：%T", binding.Validator.Engine())
	}
	err := engine.Struct(fixture)
	if err == nil {
		t.Fatalf("fixture 未触发任何校验失败，测试数据本身有问题")
	}
	ve, ok := err.(validator.ValidationErrors)
	if !ok {
		t.Fatalf("err 不是 validator.ValidationErrors：%T", err)
	}
	return ve
}

// validFixture 返回一个满足全部标签的基线实例，测试逐个字段改成非法值时
// 从这个基线出发，保证每次只有一个字段违反校验，方便断言只产生一条
// FieldError。
func validFixture() validationTagFixture {
	return validationTagFixture{
		RequiredField: "x",
		MinField:      5,
		MaxField:      5,
		OneofField:    "a",
		EmailField:    "a@b.com",
		IPField:       "127.0.0.1",
		MACField:      "01:23:45:67:89:ab",
		HostnameField: "host.example.com",
	}
}

// TestFail_ValidationErrors_EachTag 逐个覆盖任务要求的八个 tag，断言：
//  1. HTTP 422 + CodeValidationFailed（契约 §3）；
//  2. message 是可展示中文，不含 validator 的英文原文格式
//     "Key: '...' Error:Field validation for '...' failed on the '...' tag"；
//  3. message 带上是哪个字段，且用的是 json tag 名（required_field 这种
//     小写下划线风格）而不是 Go 字段名（RequiredField 这种大写驼峰）。
func TestFail_ValidationErrors_EachTag(t *testing.T) {
	cases := []struct {
		tag       string
		jsonField string
		mutate    func(f *validationTagFixture)
	}{
		{"required", "required_field", func(f *validationTagFixture) { f.RequiredField = "" }},
		{"min", "min_field", func(f *validationTagFixture) { f.MinField = 1 }},
		{"max", "max_field", func(f *validationTagFixture) { f.MaxField = 999 }},
		{"oneof", "oneof_field", func(f *validationTagFixture) { f.OneofField = "z" }},
		{"email", "email_field", func(f *validationTagFixture) { f.EmailField = "not-an-email" }},
		{"ip", "ip_field", func(f *validationTagFixture) { f.IPField = "not-an-ip" }},
		{"mac", "mac_field", func(f *validationTagFixture) { f.MACField = "not-a-mac" }},
		{"hostname", "hostname_field", func(f *validationTagFixture) { f.HostnameField = "invalid_hostname_!" }},
	}

	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			fixture := validFixture()
			tc.mutate(&fixture)
			ve := collectValidationErrors(t, fixture)

			c, rec := newTestContext()
			Fail(c, ve)

			if rec.Code != 422 {
				t.Errorf("%s: HTTP 状态码 = %d, want 422", tc.tag, rec.Code)
			}
			env := decodeGinEnvelope(t, rec)
			if env.Code != CodeValidationFailed {
				t.Errorf("%s: 业务码 = %d, want %d", tc.tag, env.Code, CodeValidationFailed)
			}
			if env.Message == "" {
				t.Fatalf("%s: message 为空", tc.tag)
			}
			if containsASCIIValidatorNoise(env.Message) {
				t.Errorf("%s: message = %q，仍包含 validator 的英文原文格式，"+
					"不能把 \"Key: '...' Error:Field validation...\" 直接吐给用户",
					tc.tag, env.Message)
			}
			if !containsSubstring(env.Message, tc.jsonField) {
				t.Errorf("%s: message = %q，缺少字段名 %q（必须用 json tag 名，"+
					"不能用 Go 字段名，用户看到的是 JSON 字段）", tc.tag, env.Message, tc.jsonField)
			}
			goFieldName := ve[0].StructField()
			if containsSubstring(env.Message, goFieldName) {
				t.Errorf("%s: message = %q，包含了 Go 字段名 %q，"+
					"应该用 json tag 名 %q 而不是 Go 字段名",
					tc.tag, env.Message, goFieldName, tc.jsonField)
			}
		})
	}
}

// TestFail_ValidationErrors_MultipleFields 验证一次校验失败涉及多个字段
// 时，message 里能同时看到每个字段各自的 json tag 名，不会因为只处理
// 第一条 FieldError 而丢掉其余字段的信息。
func TestFail_ValidationErrors_MultipleFields(t *testing.T) {
	fixture := validFixture()
	fixture.RequiredField = ""
	fixture.EmailField = "not-an-email"
	ve := collectValidationErrors(t, fixture)
	if len(ve) != 2 {
		t.Fatalf("fixture 应恰好触发两条 FieldError，实际 %d 条：%v", len(ve), ve)
	}

	c, rec := newTestContext()
	Fail(c, ve)

	if rec.Code != 422 {
		t.Errorf("HTTP 状态码 = %d, want 422", rec.Code)
	}
	env := decodeGinEnvelope(t, rec)
	for _, field := range []string{"required_field", "email_field"} {
		if !containsSubstring(env.Message, field) {
			t.Errorf("message = %q，缺少字段 %q", env.Message, field)
		}
	}
}

// containsSubstring 是不引入 strings 包的最小子串判断，供本文件内的
// message 内容断言使用（本文件已经导入了不少包，这里手写一个够用的
// 版本以保持导入列表短小；标准库 strings.Contains 逻辑完全等价）。
func containsSubstring(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// containsASCIIValidatorNoise 检测 message 是否仍包含 validator 默认
// Error() 格式的关键片段（"Key: '" 与 "Error:Field validation for"），
// 这是任务明确要求禁止出现的英文原文标记。
func containsASCIIValidatorNoise(message string) bool {
	return containsSubstring(message, "Key: '") ||
		containsSubstring(message, "Error:Field validation for")
}

// TestAbort_CallsGinAbort 验证 Abort 除了写出与 Fail 相同的响应体之外，
// 确实调用了 c.Abort()，让后续注册的中间件不再执行——这是它与 Fail 唯一
// 的行为差异（契约 §3：Abort 同 Fail，额外调 c.Abort()，供中间件用）。
func TestAbort_CallsGinAbort(t *testing.T) {
	c, rec := newTestContext()

	// gin 的 abortIndex 机制：c.Abort() 之后，Engine 在 handler 链里不会
	// 再调用后续 handler，但在单元测试里我们没有走完整的 Engine.Handle
	// 链，因此改用 c.IsAborted() 直接判定，这是 gin 官方推荐的判定方式，
	// 比手搭一条 handler 链更直接可靠。
	Abort(c, contract.ErrNotFound)

	if !c.IsAborted() {
		t.Errorf("Abort 之后 c.IsAborted() = false, want true——中间件要能感知到\n" +
			"链已经被中断，否则会继续执行本不该执行的后续逻辑")
	}
	if rec.Code != 404 {
		t.Errorf("HTTP 状态码 = %d, want 404", rec.Code)
	}
	env := decodeGinEnvelope(t, rec)
	if env.Code != CodeNotFound {
		t.Errorf("业务码 = %d, want %d", env.Code, CodeNotFound)
	}
}

// TestAbort_UnrecognizedErrorHidesDetail 对 Abort 重复一次 Fail 已经锁住
// 的安全边界断言，确认 Abort 没有绕开 Fail 的判定逻辑另起一套实现
// （避免出现「Fail 安全、Abort 不安全」的分裂）。
func TestAbort_UnrecognizedErrorHidesDetail(t *testing.T) {
	c, rec := newTestContext()
	dsn := "postgres://lober_admin:s3cr3t-p@ss@10.0.0.5:5432/lober"
	dbErr := errors.New("dial tcp: connection refused to " + dsn)

	Abort(c, dbErr)

	body := rec.Body.String()
	if containsSubstring(body, "lober_admin") || containsSubstring(body, dbErr.Error()) {
		t.Fatalf("响应体泄露了原始错误文本：%s", body)
	}
	if !c.IsAborted() {
		t.Errorf("c.IsAborted() = false, want true")
	}
}
