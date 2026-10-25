package console

import (
	"encoding/base64"
	"encoding/json"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/credentials"
	"strings"
	"sync"
	"testing"
)

const knownPasswordHash = "pbkdf2-sha256$600000$AAECAwQFBgcICQoLDA0ODw$lqWQTC4IyNpCMF28xdfPGOrSY21J9ZUmtgbyZpYoFHM"

func Test口令散列格式随机盐与独立向量(t *testing.T) {
	a, err := credentials.NewPasswords().Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	b, err := credentials.NewPasswords().Hash("correct horse")
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
	if !credentials.NewPasswords().Check("correct horse", knownPasswordHash) || !credentials.NewPasswords().Check("correct horse", a) {
		t.Fatal("independent PBKDF2 vector not accepted")
	}
	for _, bad := range []string{"", "correct hors", "CORRECT HORSE", "correct horse "} {
		if credentials.NewPasswords().Check(bad, a) {
			t.Fatal("wrong password accepted")
		}
	}
}
func Test口令散列拒绝坏编码和无界工作量(t *testing.T) {
	for _, bad := range []string{"", "md5$600000$abc$def", "pbkdf2-sha256$0$a$b", "pbkdf2-sha256$-1$a$b", "pbkdf2-sha256$999999999$AAECAwQFBgcICQoLDA0ODw$x", strings.Replace(knownPasswordHash, "600000", "1", 1), strings.Replace(knownPasswordHash, "AAECAwQFBgcICQoLDA0ODw", "!!!", 1), knownPasswordHash + "$extra"} {
		if credentials.NewPasswords().Check("correct horse", bad) {
			t.Fatal("malformed hash accepted")
		}
	}
	for _, bad := range []string{"", "x\x00", "x\xff"} {
		if _, err := credentials.NewPasswords().Hash(bad); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
}

// 仅复用测试准备的结果，仍由生产函数用真实随机盐和600000轮计算生成。
// 每个登录请求照常校验口令；随机盐与独立向量测试不经过这里。
var seedPasswordHashes = struct {
	sync.Mutex
	values map[string]string
}{values: make(map[string]string)}

func seedPasswordHash(t *testing.T, pw string) string {
	t.Helper()
	seedPasswordHashes.Lock()
	defer seedPasswordHashes.Unlock()
	if hash, ok := seedPasswordHashes.values[pw]; ok {
		return hash
	}
	hash, err := credentials.NewPasswords().Hash(pw)
	if err != nil {
		t.Fatal(err)
	}
	seedPasswordHashes.values[pw] = hash
	return hash
}

func seedAuthUser(t *testing.T, st repository.Users, name string, role model.Role, pw string) model.User {
	t.Helper()
	hash := seedPasswordHash(t, pw)
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

// 测试账号可以复用同一完整散列，真实登录和随机盐测试仍走生产算法。
func Test测试用户重复准备复用完整强度散列(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	const password = "seed fixture reuse"
	var users [4]model.User
	t.Run("同时准备", func(t *testing.T) {
		for i, name := range []string{"seeda", "seedb", "seedc", "seedd"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				users[i] = seedAuthUser(t, st, name, model.RoleViewer, password)
			})
		}
	})
	for _, u := range users {
		if u.Password != users[0].Password {
			t.Fatal("同一口令的测试准备重复生成散列")
		}
	}
	if !strings.HasPrefix(users[0].Password, "pbkdf2-sha256$600000$") || !credentials.NewPasswords().Check(password, users[0].Password) {
		t.Fatal("复用值不是完整强度的真实散列")
	}
	other := seedAuthUser(t, st, "seedother", model.RoleViewer, password+" ")
	if other.Password == users[0].Password || !credentials.NewPasswords().Check(password+" ", other.Password) {
		t.Fatal("不同原文口令共用了错误散列")
	}
	first := loginHTTP(t, s, users[0].Username, password)
	second := loginHTTP(t, s, users[1].Username, password)
	body, _ := json.Marshal(map[string]string{"currentPassword": password, "newPassword": "replacement seed password"})
	if w := httpCall(s, "POST", "/console/v1/auth/password", string(body), first.Token); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := httpCall(s, "GET", "/console/v1/user/info", "", second.Token); w.Code != 200 {
		t.Fatal("另一个账号的会话受到改密影响", w.Code)
	}
	oldLogin, _ := json.Marshal(map[string]string{"username": users[0].Username, "password": password})
	if w := httpCall(s, "POST", "/console/v1/auth/login", string(oldLogin), ""); w.Code != 401 {
		t.Fatal("改密后仍接受旧口令", w.Code)
	}
	loginHTTP(t, s, users[0].Username, "replacement seed password")
	loginHTTP(t, s, users[1].Username, password)
}
