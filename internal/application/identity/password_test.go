package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/lober/lober/internal/application/identity/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type passwordBarrier struct {
	port.Passwords
	hashed, resume chan struct{}
	pausePlain     string
}

func (g passwordBarrier) Hash(p string) (string, error) {
	h, e := g.Passwords.Hash(p)
	if g.hashed != nil && p == g.pausePlain {
		close(g.hashed)
		<-g.resume
	}
	return h, e
}

func Test本人改密校验顺序和成功撤销全部会话(t *testing.T) {
	s, st := accountFixture(t)
	p, u := accountSeed(t, s, st, "alice", model.RoleViewer)
	u.MustChangePassword = true
	var err error
	u, err = st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.prepare", At: 1})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Authenticate(t.Context(), p.token)
	if err != nil {
		t.Fatal(err)
	}
	second := s.sessions.Put("alice")
	checks := []struct {
		cmd  PasswordCommand
		want error
	}{{PasswordCommand{"wrong", "old password"}, ports.ErrForbidden}, {PasswordCommand{"old password", "old password"}, ErrBadRequest}, {PasswordCommand{"old password", "x\x00"}, ErrBadRequest}, {PasswordCommand{"", "new"}, ErrBadRequest}, {PasswordCommand{"old password", ""}, ErrBadRequest}}
	for _, tc := range checks {
		if err := s.ChangePassword(t.Context(), p, tc.cmd); !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
	}
	if back := accountBack(t, st, "alice"); back != u {
		t.Fatal("无效请求改变账号")
	}
	accountAudit(t, st, "user.password.change", "alice", 0)
	if err := s.ChangePassword(t.Context(), p, PasswordCommand{"old password", "new password"}); err != nil {
		t.Fatal(err)
	}
	back := accountBack(t, st, "alice")
	if back.Rev != u.Rev+1 || !s.passwords.Check("new password", back.Password) || back.MustChangePassword {
		t.Fatal("新密码未保存")
	}
	for _, token := range []string{p.token, second} {
		if _, ok := s.sessions.Lookup(token); ok {
			t.Fatal("改密后遗留会话")
		}
	}
	entries := accountAudit(t, st, "user.password.change", "alice", 1)
	if entries[0].Actor != "alice" || entries[0].At != 100 {
		t.Fatal("改密审计错误")
	}
}

func Test本人改密锁内复查令牌和账号(t *testing.T) {
	for _, kind := range []string{"logout", "expired", "disabled", "changed", "missing"} {
		t.Run(kind, func(t *testing.T) {
			s, st := accountFixture(t)
			p, u := accountSeed(t, s, st, "alice", model.RoleViewer)
			want := ports.ErrUnauthenticated
			switch kind {
			case "logout":
				s.sessions.Drop(p.token)
			case "expired":
				s.sessions.now = func() int64 { return 200 }
			case "disabled":
				u.Disabled = true
				var e error
				u, e = st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.update", At: 1})
				if e != nil {
					t.Fatal(e)
				}
			case "changed":
				u.DisplayName = "other"
				var e error
				u, e = st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.update", At: 1})
				if e != nil {
					t.Fatal(e)
				}
				want = repository.ErrConflict
			case "missing":
				s.users = &accessUsers{err: repository.ErrNotFound}
				want = repository.ErrNotFound
			}
			if err := s.ChangePassword(t.Context(), p, PasswordCommand{"old password", "new password"}); !errors.Is(err, want) {
				t.Fatal(err)
			}
			if back := accountBack(t, st, "alice"); back != u {
				t.Fatal("失败改密改变账号")
			}
			accountAudit(t, st, "user.password.change", "alice", 0)
		})
	}
}

func Test并发改密不能让旧口令登录补发会话(t *testing.T) {
	for _, kind := range []string{"password", "reset"} {
		t.Run(kind, func(t *testing.T) {
			s, st := accountFixture(t)
			admin, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
			p, u := accountSeed(t, s, st, "alice", model.RoleViewer)
			checked, resume := make(chan struct{}), make(chan struct{})
			// 只暂停首次登录的真实密码核验，后续核验继续真实算法。
			original := s.passwords
			s.passwords = &onceCheckBarrier{Passwords: original, entered: checked, resume: resume}
			type result struct {
				session SessionResult
				err     error
			}
			done := make(chan result, 1)
			go func() {
				r, e := s.Login(context.Background(), LoginCommand{"alice", "old password"})
				done <- result{r, e}
			}()
			<-checked
			var err error
			if kind == "password" {
				err = s.ChangePassword(t.Context(), p, PasswordCommand{"old password", "new password"})
			} else {
				_, err = s.UpdateUser(t.Context(), admin, "alice", UpdateUserCommand{Role: model.RoleViewer, Password: "new password", Rev: u.Rev})
			}
			close(resume)
			if err != nil {
				t.Fatal(err)
			}
			r := <-done
			if !errors.Is(r.err, ErrLoginFailed) || r.session.Token != "" {
				t.Fatal("旧密码登录发布了会话", r.err)
			}
			back := accountBack(t, st, "alice")
			if back.Rev != u.Rev+1 || !original.Check("new password", back.Password) {
				t.Fatal("新密码未提交")
			}
			if _, ok := s.sessions.Lookup(p.token); ok {
				t.Fatal("旧会话未撤销")
			}
			action := "user.password.change"
			if kind == "reset" {
				action = "user.update"
			}
			accountAudit(t, st, action, "alice", 1)
			fresh, e := s.Login(t.Context(), LoginCommand{"alice", "new password"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.Authenticate(t.Context(), fresh.Token); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func Test并发本人改密不能覆盖管理员重置(t *testing.T) {
	s, st := accountFixture(t)
	admin, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
	p, u := accountSeed(t, s, st, "alice", model.RoleViewer)
	hashed, resume := make(chan struct{}), make(chan struct{})
	original := s.passwords
	s.passwords = passwordBarrier{Passwords: original, hashed: hashed, resume: resume, pausePlain: "self replacement"}
	done := make(chan error, 1)
	go func() {
		done <- s.ChangePassword(context.Background(), p, PasswordCommand{"old password", "self replacement"})
	}()
	<-hashed
	_, err := s.UpdateUser(t.Context(), admin, "alice", UpdateUserCommand{Role: model.RoleViewer, Password: "admin replacement", Rev: u.Rev})
	close(resume)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, repository.ErrConflict) {
		t.Fatal("慢改密未返回版本冲突", err)
	}
	back := accountBack(t, st, "alice")
	if back.Rev != u.Rev+1 || !original.Check("admin replacement", back.Password) {
		t.Fatal("本人改密覆盖了管理员重置")
	}
	accountAudit(t, st, "user.password.change", "alice", 0)
	accountAudit(t, st, "user.update", "alice", 1)
	if _, ok := s.sessions.Lookup(p.token); ok {
		t.Fatal("重置后旧令牌仍有效")
	}
}
