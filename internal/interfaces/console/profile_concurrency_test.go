package console

import (
	"context"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type profileMenuFailure struct {
	repository.RoleMenus
	reads int
}

func (f *profileMenuFailure) GetRoleMenus(context.Context, model.Role) (repository.RoleMenuSettings, bool, error) {
	f.reads++
	return repository.RoleMenuSettings{}, false, errors.New("测试：保存后的菜单读取失败")
}

func Test资料已提交后菜单读取失败保留事实(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, before := seedHTTPUser(t, s, st, "profilefailure", model.RoleViewer)
	menus := &profileMenuFailure{RoleMenus: st}
	s.server.deps.RoleMenus = menus
	body := fmt.Sprintf(`{"displayName":"已提交","email":"profile@example.com","avatar":"","bio":"保留资料","rev":%d}`, before.Rev)
	w := httpCall(s, "PUT", "/console/v1/user/profile", body, token)
	if w.Code != 500 || menus.reads != 1 {
		t.Fatalf("响应 %d，菜单读取 %d 次", w.Code, menus.reads)
	}
	after, err := st.GetUser(t.Context(), before.Username)
	if err != nil {
		t.Fatal(err)
	}
	if after.Rev != before.Rev+1 || after.DisplayName != "已提交" || after.Email != "profile@example.com" || after.Bio != "保留资料" || after.Avatar != "" {
		t.Fatal("菜单故障抹掉了已提交的资料")
	}
	if after.Password != before.Password || after.Role != before.Role || after.Disabled != before.Disabled || after.MustChangePassword != before.MustChangePassword || after.CreatedAt != before.CreatedAt {
		t.Fatal("资料保存改动了账号安全字段")
	}
	entries, err := st.ListAudit(t.Context(), repository.AuditQuery{Action: "user.profile.update", TargetKind: "user", TargetID: before.Username})
	if err != nil || len(entries.Items) != 1 {
		t.Fatalf("资料审计数 %d，错误 %v", len(entries.Items), err)
	}
	if entries.Items[0].Actor != before.Username || entries.Items[0].At != fixedAt {
		t.Fatal("资料审计身份或时间改变")
	}
	s.server.deps.RoleMenus = st
	info := responseData[userResponse](t, httpCall(s, "GET", "/console/v1/user/info", "", token))
	if info.Rev != after.Rev || info.DisplayName != after.DisplayName {
		t.Fatal("原会话未读到已提交资料")
	}
	if w := httpCall(s, "PUT", "/console/v1/user/profile", body, token); w.Code != 409 {
		t.Fatal("重试旧版本未返回冲突", w.Code)
	}
	entries, err = st.ListAudit(t.Context(), repository.AuditQuery{Action: "user.profile.update", TargetKind: "user", TargetID: before.Username})
	if err != nil || len(entries.Items) != 1 {
		t.Fatal("失败重试增加了审计")
	}
}

type profileBodyGate struct {
	io.Reader
	once            sync.Once
	entered, resume chan struct{}
}

func (g *profileBodyGate) Read(p []byte) (int, error) {
	g.once.Do(func() { close(g.entered); <-g.resume })
	return g.Reader.Read(p)
}

func Test慢资料请求不能用猜测新修订号恢复旧权限或凭据(t *testing.T) {
	for _, kind := range []string{"reset", "disable", "demote"} {
		t.Run(kind, func(t *testing.T) {
			s, st, _ := newHTTPServer(t)
			admin := adminHTTP(t, s, st)
			victim := seedAuthUser(t, st, "victim", model.RoleAdmin, "old password")
			token := loginHTTP(t, s, "victim", "old password").Token
			gate := &profileBodyGate{Reader: strings.NewReader(fmt.Sprintf(`{"displayName":"stale","rev":%d}`, victim.Rev+1)), entered: make(chan struct{}), resume: make(chan struct{})}
			req := httptest.NewRequest("PUT", "/console/v1/user/profile", gate)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { w := httptest.NewRecorder(); s.Engine.ServeHTTP(w, req); done <- w }()
			select {
			case <-gate.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("profile did not reach body gate")
			}
			role := model.RoleAdmin
			if kind == "demote" {
				role = model.RoleViewer
			}
			password := ""
			if kind == "reset" {
				password = "replacement password"
			}
			w := httpCall(s, "PUT", "/console/v1/users/victim", fmt.Sprintf(`{"role":%q,"disabled":%t,"password":%q,"rev":%d}`, role, kind == "disable", password, victim.Rev), admin)
			close(gate.resume)
			if w.Code != 200 {
				t.Fatal("admin update failed", w.Code)
			}
			response := <-done
			if response.Code != 409 && response.Code != 401 {
				t.Fatal("stale profile update accepted", response.Code)
			}
			back, err := st.GetUser(t.Context(), "victim")
			if err != nil {
				t.Fatal(err)
			}
			if back.Role != role || back.Disabled != (kind == "disable") || back.Rev != victim.Rev+1 {
				t.Fatal("profile overwrote security fields")
			}
			if kind == "reset" && !CheckPassword(password, back.Password) {
				t.Fatal("profile restored old password")
			}
		})
	}
}
