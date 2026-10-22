package console

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// 检查真正发给网页的正文，避免只检查转换函数而漏掉某条路由。
func Test用户响应不泄露口令并保留字段(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	check := func(method, path, body, auth string) {
		t.Helper()
		w := httpCall(s, method, path, body, auth)
		if w.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "pbkdf2-sha256$") || strings.Contains(w.Body.String(), "private-hash") {
			t.Fatal("响应泄露散列")
		}
		var value any
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		users := 0
		var walk func(any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				for k, item := range x {
					if strings.EqualFold(k, "password") {
						t.Fatal("响应包含口令字段")
					}
					walk(item)
				}
				if _, ok := x["username"]; ok {
					users++
					for _, key := range []string{"username", "displayName", "role", "permissions", "disabled", "createdAt", "rev", "passwordChangeRequired"} {
						if _, ok := x[key]; !ok {
							t.Errorf("用户响应缺少 %s", key)
						}
					}
					if len(x) != 8 {
						t.Errorf("用户响应出现额外字段：%v", x)
					}
				}
			case []any:
				for _, item := range x {
					walk(item)
				}
			}
		}
		walk(value)
		if users == 0 {
			t.Fatal("响应没有用户，检查空跑")
		}
	}
	check("POST", "/console/v1/auth/login", `{"username":"admin","password":"admin password"}`, "")
	check("GET", "/console/v1/user/info", "", token)
	check("POST", "/console/v1/users", `{"username":"bob","displayName":"鲍勃","role":"operator","password":"initial"}`, token)
	u, err := st.GetUser(t.Context(), "bob")
	if err != nil {
		t.Fatal(err)
	}
	check("PUT", "/console/v1/users/bob", fmt.Sprintf(`{"rev":%d,"displayName":"新名字","role":"viewer","password":"replacement"}`, u.Rev), token)
	check("GET", "/console/v1/users", "", token)
}
