package identity

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/credentials"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

// 真实数据库保留保存校验、版本冲突、最后管理员与审计回滚；不在替身里重写它们。
func accountFixture(t *testing.T) (*Service, repository.Store) {
	t.Helper()
	st := pgtest.Open(t)
	clock := func() int64 { return 100 }
	book := NewSessionBook(100, clock, credentials.NewTokens())
	return NewService(Deps{Users: st, RoleMenus: st, Passwords: credentials.NewPasswords(), Sessions: book, Clock: clock}), st
}

func accountSeed(t *testing.T, s *Service, st repository.Users, name string, role model.Role) (Principal, model.User) {
	t.Helper()
	hash, err := s.passwords.Hash("old password")
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(t.Context(), model.User{Username: name, Role: role, Password: hash, CreatedAt: 1}, repository.Audit{Actor: "seed", Action: "user.create", At: 1})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(t.Context(), s.sessions.Put(name))
	if err != nil {
		t.Fatal(err)
	}
	return p, u
}

func accountBack(t *testing.T, st repository.Users, name string) model.User {
	t.Helper()
	u, err := st.GetUser(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func accountAudit(t *testing.T, st repository.AuditLog, action, target string, n int) []repository.AuditEntry {
	t.Helper()
	page, err := st.ListAudit(t.Context(), repository.AuditQuery{Action: action, TargetKind: "user", TargetID: target})
	if err != nil || len(page.Items) != n {
		t.Fatalf("审计条数期望 %d，实际 %d，错误 %v", n, len(page.Items), err)
	}
	return page.Items
}

func Test账号拒绝先于仓储散列和审计时钟(t *testing.T) {
	s, u, _, b := accessFixture()
	p, err := s.Authenticate(t.Context(), b.Put("alice"))
	if err != nil {
		t.Fatal(err)
	}
	other, _, _, _ := accessFixture()
	forced := p
	forced.user.MustChangePassword = true
	forced.user.Role = model.RoleViewer
	viewer := p
	viewer.user.Role = model.RoleViewer
	actions := []struct {
		name   string
		call   func(Principal) error
		forced error
	}{
		{"列表", func(p Principal) error { _, e := s.ListUsers(t.Context(), p, UserQuery{}); return e }, ports.ErrPasswordChangeRequired},
		{"创建", func(p Principal) error { _, e := s.CreateUser(t.Context(), p, CreateUserCommand{}); return e }, ports.ErrPasswordChangeRequired},
		{"更新", func(p Principal) error { _, e := s.UpdateUser(t.Context(), p, "alice", UpdateUserCommand{}); return e }, ports.ErrForbidden},
		{"禁用", func(p Principal) error { return s.DisableUser(t.Context(), p, "alice") }, ports.ErrPasswordChangeRequired},
		{"改密", func(p Principal) error {
			return s.ChangePassword(t.Context(), p, PasswordCommand{CurrentPassword: "wrong", NewPassword: "new"})
		}, ports.ErrForbidden},
		{"资料", func(p Principal) error { _, e := s.UpdateProfile(t.Context(), p, ProfileCommand{}); return e }, ports.ErrPasswordChangeRequired},
	}
	s.clock = func() int64 { t.Fatal("拒绝请求读取审计时钟"); return 0 }
	s.passwords = accessPasswords{hash: func(string) (string, error) { t.Fatal("拒绝请求计算散列"); return "", nil }}
	before := u.reads
	for _, a := range actions {
		t.Run(a.name, func(t *testing.T) {
			for _, bad := range []Principal{{}, {owner: other, user: p.user}} {
				if e := a.call(bad); !errors.Is(e, ports.ErrUnauthenticated) {
					t.Fatal(e)
				}
			}
			if e := a.call(forced); !errors.Is(e, a.forced) {
				t.Fatal(e)
			}
		})
	}
	for _, a := range actions[:4] {
		if e := a.call(viewer); !errors.Is(e, ports.ErrForbidden) {
			t.Fatal(e)
		}
	}
	if u.reads != before {
		t.Fatal("拒绝请求访问了用户仓储")
	}
}

func Test账号基础检查及创建列表结果(t *testing.T) {
	s, st := accountFixture(t)
	admin, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
	calls := 0
	s.clock = func() int64 { calls++; return int64(100 + calls) }
	s.roleMenus = &accessMenus{err: errors.New("不应查询菜单")}
	for _, cmd := range []CreateUserCommand{{Username: "bad name", Role: model.RoleViewer, Password: "new"}, {Username: "bob", Role: "bad", Password: "new"}, {Username: "bob", Role: model.RoleViewer}, {Username: "bob", Role: model.RoleViewer, Password: "x\x00"}} {
		if _, err := s.CreateUser(t.Context(), admin, cmd); !errors.Is(err, ErrBadRequest) {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatal("基础拒绝读取时钟")
	}
	out, err := s.CreateUser(t.Context(), admin, CreateUserCommand{Username: "bob", DisplayName: "新用户", Role: model.RoleViewer, Password: "new password"})
	if err != nil || out.Rev != 1 || out.MenuIDs == nil || len(out.MenuIDs) != 0 || out.MenuRev != 0 || out.CreatedAt != 101 {
		t.Fatal(out, err)
	}
	row := accountBack(t, st, "bob")
	if !s.passwords.Check("new password", row.Password) || row.MustChangePassword {
		t.Fatal("创建凭据错误")
	}
	audit := accountAudit(t, st, "user.create", "bob", 1)[0]
	if audit.Actor != "admin" || audit.At != 102 || calls != 2 {
		t.Fatal("创建与审计时钟顺序改变")
	}
	page, err := s.ListUsers(t.Context(), admin, UserQuery{Page: 1, Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Username != "admin" || page.Total == nil || *page.Total != 2 || page.Next == "" {
		t.Fatal(page, err)
	}
	next, err := s.ListUsers(t.Context(), admin, UserQuery{Cursor: page.Next, Limit: 1})
	if err != nil || len(next.Items) != 1 || next.Items[0].Username != "bob" || next.Items[0].MenuIDs == nil || next.Items[0].MenuRev != 0 {
		t.Fatal(next, err)
	}
	if _, err = s.ListUsers(t.Context(), admin, UserQuery{Page: 1, Cursor: "bad"}); !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
}

func Test账号更新保留请求版本和错误顺序(t *testing.T) {
	s, st := accountFixture(t)
	admin, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
	_, u := accountSeed(t, s, st, "bob", model.RoleViewer)
	for _, cmd := range []UpdateUserCommand{{Role: model.RoleAdmin, Disabled: true}, {Role: "bad", Rev: 1, Disabled: true}, {Username: "other", Role: model.RoleAdmin, Rev: 1, Disabled: true}} {
		if _, err := s.UpdateUser(t.Context(), admin, "admin", cmd); !errors.Is(err, ErrBadRequest) {
			t.Fatal(err)
		}
	}
	if _, err := s.UpdateUser(t.Context(), admin, "admin", UpdateUserCommand{Role: model.RoleAdmin, Rev: 1, Disabled: true}); !errors.Is(err, ErrSelfDisable) {
		t.Fatal(err)
	}
	if err := s.DisableUser(t.Context(), admin, "admin"); !errors.Is(err, ErrSelfDisable) {
		t.Fatal(err)
	}
	u.DisplayName = "newer"
	u, err := st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.update", At: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateUser(t.Context(), admin, "bob", UpdateUserCommand{Role: model.RoleViewer, Rev: u.Rev - 1, Password: "replacement"}); !errors.Is(err, repository.ErrConflict) {
		t.Fatal(err)
	}
	if back := accountBack(t, st, "bob"); back != u {
		t.Fatal("陈旧版本覆盖账号")
	}
	u.MustChangePassword = true
	u, err = st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.update", At: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"", "old password"} {
		if _, err := s.UpdateUser(t.Context(), admin, "bob", UpdateUserCommand{Role: model.RoleViewer, Rev: u.Rev, Password: password}); !errors.Is(err, ErrInitialPasswordUnchanged) {
			t.Fatal(err)
		}
	}
	out, err := s.UpdateUser(t.Context(), admin, "bob", UpdateUserCommand{Role: model.RoleOperator, Rev: u.Rev, Password: "replacement"})
	if err != nil || out.MustChangePassword || out.MenuIDs == nil || len(out.MenuIDs) != 0 || out.MenuRev != 0 {
		t.Fatal(out, err)
	}
}

// 只改审计触发条件，账号写入及事务回滚仍由真实数据库处理。
type accountAuditFailure struct{ repository.Users }

func (f accountAuditFailure) CreateUser(ctx context.Context, u model.User, a repository.Audit) (model.User, error) {
	a.Actor = pgtest.FaultActor
	return f.Users.CreateUser(ctx, u, a)
}
func (f accountAuditFailure) UpdateUser(ctx context.Context, u model.User, a repository.Audit) (model.User, error) {
	a.Actor = pgtest.FaultActor
	return f.Users.UpdateUser(ctx, u, a)
}

func Test身份写入审计失败不撤销会话(t *testing.T) {
	for _, kind := range []string{"reset", "disable", "password", "profile", "create"} {
		t.Run(kind, func(t *testing.T) {
			s, st := accountFixture(t)
			actor, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
			target, before := accountSeed(t, s, st, "bob", model.RoleViewer)
			s.users = accountAuditFailure{Users: st}
			var err error
			action := "user.update"
			switch kind {
			case "reset":
				_, err = s.UpdateUser(t.Context(), actor, "bob", UpdateUserCommand{Role: model.RoleViewer, Password: "new password", Rev: before.Rev})
			case "disable":
				action = "user.disable"
				err = s.DisableUser(t.Context(), actor, "bob")
			case "password":
				action = "user.password.change"
				target = actor
				before = accountBack(t, st, actor.user.Username)
				err = s.ChangePassword(t.Context(), actor, PasswordCommand{"old password", "new password"})
			case "profile":
				action = "user.profile.update"
				target = actor
				before = accountBack(t, st, actor.user.Username)
				_, err = s.UpdateProfile(t.Context(), actor, ProfileCommand{DisplayName: "changed", Rev: before.Rev})
			case "create":
				action = "user.create"
				_, err = s.CreateUser(t.Context(), actor, CreateUserCommand{Username: "newbie", Role: model.RoleViewer, Password: "new password"})
			}
			if err == nil {
				t.Fatal("注入审计失败却保存成功")
			}
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "P0001" {
				t.Fatalf("未触发真实审计事务故障：%T", err)
			}
			if back := accountBack(t, st, before.Username); back != before {
				t.Fatal("审计失败仍改变用户或版本")
			}
			if _, ok := s.sessions.Lookup(target.token); !ok {
				t.Fatal("审计失败撤销原会话")
			}
			if _, err := s.Authenticate(t.Context(), target.token); err != nil {
				t.Fatal("原令牌不可认证", err)
			}
			if kind == "create" {
				if _, err := st.GetUser(t.Context(), "newbie"); !errors.Is(err, repository.ErrNotFound) {
					t.Fatal(err)
				}
				accountAudit(t, st, action, "newbie", 0)
			} else {
				accountAudit(t, st, action, before.Username, 0)
			}
		})
	}
}

func Test仅改角色下次请求生效但不清令牌(t *testing.T) {
	s, st := accountFixture(t)
	admin, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
	p, u := accountSeed(t, s, st, "bob", model.RoleOperator)
	out, err := s.UpdateUser(t.Context(), admin, "bob", UpdateUserCommand{Role: model.RoleViewer, Rev: u.Rev})
	if err != nil || out.Rev != u.Rev+1 {
		t.Fatal(out, err)
	}
	next, err := s.Authenticate(t.Context(), p.token)
	if err != nil || next.Actor().Role != model.RoleViewer {
		t.Fatal("原会话未获得新角色", err)
	}
	back := accountBack(t, st, "bob")
	if back.Password != u.Password || back.Disabled || back.Rev != u.Rev+1 || back.Role != model.RoleViewer {
		t.Fatal("角色变更保存范围错误")
	}
	accountAudit(t, st, "user.update", "bob", 1)
	if err := s.DisableUser(t.Context(), admin, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.sessions.Lookup(p.token); ok {
		t.Fatal("禁用未清令牌")
	}
	back = accountBack(t, st, "bob")
	if !back.Disabled || back.Rev != u.Rev+2 {
		t.Fatal("禁用未保存")
	}
	accountAudit(t, st, "user.disable", "bob", 1)
}

func Test强制改密管理员本人旧更新仍可用(t *testing.T) {
	s, st := accountFixture(t)
	p, u := accountSeed(t, s, st, "admin", model.RoleAdmin)
	u.MustChangePassword = true
	u, err := st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.prepare", At: 1})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Authenticate(t.Context(), p.token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateUser(t.Context(), p, "other", UpdateUserCommand{}); !errors.Is(err, ports.ErrPasswordChangeRequired) {
		t.Fatal("其他目标绕过强制改密", err)
	}
	out, err := s.UpdateUser(t.Context(), p, "admin", UpdateUserCommand{Role: model.RoleAdmin, Password: "replacement", Rev: u.Rev})
	if err != nil || out.MustChangePassword || out.Rev != u.Rev+1 {
		t.Fatal(out, err)
	}
	back := accountBack(t, st, "admin")
	if !s.passwords.Check("replacement", back.Password) {
		t.Fatal("本人旧更新未替换初始密码")
	}
	if _, ok := s.sessions.Lookup(p.token); ok {
		t.Fatal("本人旧更新未清会话")
	}
	accountAudit(t, st, "user.update", "admin", 1)
}

type accountUpdateBarrier struct {
	repository.Users
	entered chan struct{}
	resume  chan struct{}
}

func (g accountUpdateBarrier) UpdateUser(ctx context.Context, u model.User, a repository.Audit) (model.User, error) {
	g.entered <- struct{}{}
	<-g.resume
	return g.Users.UpdateUser(ctx, u, a)
}

func Test身份跨实例管理员移除仍由数据库保护(t *testing.T) {
	s, st := accountFixture(t)
	a, ua := accountSeed(t, s, st, "admin_a", model.RoleAdmin)
	_, ub := accountSeed(t, s, st, "admin_b", model.RoleAdmin)
	other := NewService(Deps{Users: st, RoleMenus: st, Passwords: s.passwords, Sessions: NewSessionBook(100, s.clock, credentials.NewTokens()), Clock: s.clock})
	b, err := other.Authenticate(t.Context(), other.sessions.Put("admin_b"))
	if err != nil {
		t.Fatal(err)
	}
	// 两个服务拥有不同的协调锁，先让两次移除都走到真实保存入口。
	gate := accountUpdateBarrier{Users: st, entered: make(chan struct{}, 2), resume: make(chan struct{})}
	s.users = gate
	other.users = gate
	results := make(chan error, 2)
	go func() {
		_, e := s.UpdateUser(context.Background(), a, "admin_a", UpdateUserCommand{Role: model.RoleViewer, Rev: ua.Rev})
		results <- e
	}()
	go func() {
		_, e := other.UpdateUser(context.Background(), b, "admin_b", UpdateUserCommand{Role: model.RoleViewer, Rev: ub.Rev})
		results <- e
	}()
	<-gate.entered
	<-gate.entered
	close(gate.resume)
	success := 0
	for range 2 {
		if e := <-results; e == nil {
			success++
		} else if !errors.Is(e, repository.ErrLastAdmin) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("同时移除未保留管理员")
	}
	enabled := 0
	for _, u := range []model.User{ua, ub} {
		back := accountBack(t, st, u.Username)
		n := 0
		if back.Role == model.RoleAdmin {
			enabled++
			if back.Rev != u.Rev {
				t.Fatal("失败移除改变版本")
			}
		} else {
			n = 1
			if back.Rev != u.Rev+1 || back.Role != model.RoleViewer {
				t.Fatal("成功移除事实错误")
			}
		}
		accountAudit(t, st, "user.update", u.Username, n)
	}
	if enabled != 1 {
		t.Fatal("数据库没有恰好保留一位管理员")
	}
	for _, tc := range []struct {
		s *Service
		p Principal
	}{{s, a}, {other, b}} {
		next, e := tc.s.Authenticate(t.Context(), tc.p.token)
		if e != nil || !reflect.DeepEqual(next.user, accountBack(t, st, tc.p.user.Username)) {
			t.Fatal("角色修改错误撤销会话或认证未刷新", e)
		}
	}
}
