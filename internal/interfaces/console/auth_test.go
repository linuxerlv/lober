package console

import (
	"encoding/base64"
	"encoding/json"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"strings"
	"testing"
)

const knownPasswordHash = "pbkdf2-sha256$600000$AAECAwQFBgcICQoLDA0ODw$lqWQTC4IyNpCMF28xdfPGOrSY21J9ZUmtgbyZpYoFHM"

func Test口令散列格式随机盐与独立向量(t *testing.T) {
	a, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(a, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" || a == b {
		t.Fatal("invalid password encoding")
	}
	salt, e1 := base64.RawURLEncoding.DecodeString(parts[2])
	key, e2 := base64.RawURLEncoding.DecodeString(parts[3])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(key) != 32 {
		t.Fatal("salt/key lengths")
	}
	if !CheckPassword("correct horse", knownPasswordHash) || !CheckPassword("correct horse", a) {
		t.Fatal("independent PBKDF2 vector not accepted")
	}
	for _, bad := range []string{"", "correct hors", "CORRECT HORSE", "correct horse "} {
		if CheckPassword(bad, a) {
			t.Fatal("wrong password accepted")
		}
	}
}
func Test口令散列拒绝坏编码和无界工作量(t *testing.T) {
	for _, bad := range []string{"", "md5$600000$abc$def", "pbkdf2-sha256$0$a$b", "pbkdf2-sha256$-1$a$b", "pbkdf2-sha256$999999999$AAECAwQFBgcICQoLDA0ODw$x", strings.Replace(knownPasswordHash, "600000", "1", 1), strings.Replace(knownPasswordHash, "AAECAwQFBgcICQoLDA0ODw", "!!!", 1), knownPasswordHash + "$extra"} {
		if CheckPassword("correct horse", bad) {
			t.Fatal("malformed hash accepted")
		}
	}
	for _, bad := range []string{"", "x\x00", "x\xff"} {
		if _, err := HashPassword(bad); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
}
func seedAuthUser(t *testing.T, st repository.Users, name string, role model.Role, pw string) model.User {
	t.Helper()
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(t.Context(), model.User{Username: name, DisplayName: name, Role: role, Password: hash, CreatedAt: fixedAt}, repository.Audit{Actor: "seed", Action: "user.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func loginHTTP(t *testing.T, s *Server, name, pw string) sessionResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": name, "password": pw})
	w := httpCall(s, "POST", "/console/v1/auth/login", string(body), "")
	if w.Code != 200 {
		t.Fatalf("login %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Data sessionResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}
func Test登录退出与到期走真实HTTP(t *testing.T) {
	s, st, now := newHTTPServer(t)
	seedAuthUser(t, st, "alice", model.RoleOperator, "correct horse")
	login := loginHTTP(t, s, "alice", "correct horse")
	if len(login.Token) != 64 || login.ExpiresAt != fixedAt+12*3600 || login.User.Username != "alice" || len(login.User.Permissions) != 9 {
		t.Fatal("wrong session metadata")
	}
	w := httpCall(s, "GET", "/console/v1/user/info", "", login.Token)
	if w.Code != 200 || strings.Contains(w.Body.String(), "pbkdf2") || strings.Contains(w.Body.String(), "password\"") {
		t.Fatal(w.Body.String())
	}
	w = httpCall(s, "POST", "/console/v1/auth/logout", "", login.Token)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = httpCall(s, "GET", "/console/v1/user/info", "", login.Token); w.Code != 401 {
		t.Fatal("logged-out token accepted")
	}
	login = loginHTTP(t, s, "alice", "correct horse")
	now.Store(fixedAt + 12*3600)
	if w = httpCall(s, "GET", "/console/v1/user/info", "", login.Token); w.Code != 401 {
		t.Fatal("expired token accepted")
	}
}
func Test登录失败说明一致与请求前提(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	seedAuthUser(t, st, "alice", model.RoleViewer, "pw")
	u := seedAuthUser(t, st, "disabled", model.RoleViewer, "pw")
	u.Disabled = true
	_, err := st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.update", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	message := ""
	for _, body := range []string{`{"username":"alice","password":"wrong"}`, `{"username":"nobody","password":"pw"}`, `{"username":"disabled","password":"pw"}`} {
		w := httpCall(s, "POST", "/console/v1/auth/login", body, "")
		if w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
		var env struct{ Message string }
		json.Unmarshal(w.Body.Bytes(), &env)
		if message == "" {
			message = env.Message
		}
		if message == "" || env.Message != message {
			t.Fatal("account enumeration message")
		}
	}
	for _, body := range []string{`{}`, `{"username":"alice"}`, `{"username":"","password":"pw"}`, `{`} {
		if w := httpCall(s, "POST", "/console/v1/auth/login", body, ""); w.Code != 400 {
			t.Fatal("missing/malformed input accepted")
		}
	}
}
