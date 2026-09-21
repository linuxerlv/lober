// 本文件提供最基础的测试 HTTP 请求构造与 JSON 解码 helper，供全部资源的
// 测试文件共用，避免每个 _test.go 各写一份。
package console

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newJSONRequest 构造一个 httptest 可用的 *http.Request。body 为 nil 时
// 不设置请求体与 Content-Type；否则序列化为 JSON 并设置
// Content-Type: application/json。
func newJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("序列化请求体失败: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// RemoteAddr 默认设为一个非 loopback 地址，模拟「请求不经可信代理直连
	// 而来」的默认场景；需要模拟 nginx 场景的测试自行覆盖为 127.0.0.1。
	req.RemoteAddr = "203.0.113.1:12345"
	return req
}

// decodeJSON 把 data 解码进 out，解码失败直接 t.Fatalf——测试里调用这个
// helper 的前提通常是已经断言过 HTTP 状态码，body 不是合法 JSON 属于
// 测试本身写错而不是被测代码的正常分支。
func decodeJSON(t *testing.T, data []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("解析响应 JSON 失败: %v，body=%s", err, string(data))
	}
}
