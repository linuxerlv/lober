package console

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func testContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/console/v1/x", nil)
	return c, w
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func Test成功响应包上code与data(t *testing.T) {
	c, w := testContext(t)
	Ok(c, map[string]string{"sn": "SN-1"})
	got := decodeEnvelope(t, w)
	if w.Code != 200 || got["code"] != float64(0) || got["message"] != "" || got["data"].(map[string]any)["sn"] != "SN-1" {
		t.Fatalf("unexpected envelope: %d %s", w.Code, w.Body.String())
	}
}

func Test没有数据时成功响应的data是null(t *testing.T) {
	c, w := testContext(t)
	Ok(c, nil)
	if got := decodeEnvelope(t, w); got["data"] != nil {
		t.Fatal(got)
	}
}

func Test失败响应停止后续处理(t *testing.T) {
	c, w := testContext(t)
	Fail(c, 403, nil, "denied")
	if !c.IsAborted() || w.Code != 403 || decodeEnvelope(t, w)["code"] != float64(403) {
		t.Fatal(w.Body.String())
	}
}
