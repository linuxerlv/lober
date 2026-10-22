package console

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func Test绑定拒绝非法UTF8而不静默替换(t *testing.T) {
	c, w := testContext(t)
	c.Request = httptest.NewRequest("POST", "/", bytes.NewReader([]byte{'{', '"', 'v', '"', ':', '"', 0xff, '"', '}'}))
	var v map[string]string
	if Bind(c, &v) || w.Code != 400 {
		t.Fatal("invalid bytes accepted", v)
	}
}
