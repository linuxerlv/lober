package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// 只观察空库查询和同步并发起点；保存、唯一约束和审计仍使用真实数据库。
type bootstrapUsers struct {
	repository.Users
	t       *testing.T
	entered chan struct{}
	resume  chan struct{}
}

func (g bootstrapUsers) ListUsers(ctx context.Context, q repository.UserQuery) (repository.Page[model.User], error) {
	if q.Limit != 1 || q.Page != 0 || q.Cursor != "" {
		g.t.Fatal("初始化未先查询最多一个账号")
	}
	page, err := g.Users.ListUsers(ctx, q)
	if g.entered != nil {
		g.entered <- struct{}{}
		<-g.resume
	}
	return page, err
}

func Test身份初始化仅空库写入且不泄露种子口令(t *testing.T) {
	s, st := accountFixture(t)
	s.users = bootstrapUsers{Users: st, t: t}
	calls := 0
	s.clock = func() int64 { calls++; return int64(200 + calls) }
	if err := s.Initialise(t.Context(), ""); err == nil || err.Error() != "空库初始化必须显式配置 seedAdmin" {
		t.Fatal("空种子错误改变")
	}
	if calls != 0 {
		t.Fatal("空种子读取时钟")
	}
	seed := "task4-initial-seed"
	if err := s.Initialise(t.Context(), seed); err != nil {
		t.Fatal(err)
	}
	u := accountBack(t, st, "admin")
	if u.Username != "admin" || u.DisplayName != "管理员" || u.Role != model.RoleAdmin || !u.MustChangePassword || u.Disabled || u.CreatedAt != 201 || u.Rev != 1 || calls != 2 {
		t.Fatal("初始账号事实错误")
	}
	if u.Password == seed || !s.passwords.Check(seed, u.Password) {
		t.Fatal("种子未正确散列")
	}
	a := accountAudit(t, st, "user.create", "admin", 1)[0]
	if a.Actor != "bootstrap" || a.At != 202 || strings.Contains(a.Detail, seed) || strings.Contains(a.Detail, u.Password) {
		t.Fatal("初始化审计错误或泄露凭据")
	}
	token := s.sessions.Put("admin")
	s.passwords = accessPasswords{hash: func(string) (string, error) { t.Fatal("已有用户仍散列种子"); return "", nil }}
	s.clock = func() int64 { t.Fatal("已有用户仍读审计时钟"); return 0 }
	for _, ignored := range []string{"", "changed", "bad\x00"} {
		if err := s.Initialise(t.Context(), ignored); err != nil {
			t.Fatal(err)
		}
	}
	if accountBack(t, st, "admin") != u {
		t.Fatal("重复初始化改变账号")
	}
	if _, ok := s.sessions.Lookup(token); !ok {
		t.Fatal("重复初始化撤销会话")
	}
	accountAudit(t, st, "user.create", "admin", 1)
}

func Test初始化已有非管理员也跳过种子(t *testing.T) {
	s, st := accountFixture(t)
	_, u := accountSeed(t, s, st, "viewer", model.RoleViewer)
	s.passwords = accessPasswords{hash: func(string) (string, error) { t.Fatal("非空库仍散列种子"); return "", nil }}
	if err := s.Initialise(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if accountBack(t, st, "viewer") != u {
		t.Fatal("初始化改变已有用户")
	}
	if _, err := st.GetUser(t.Context(), "admin"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
}

func Test初始化并发首次保存遵守唯一约束(t *testing.T) {
	s, st := accountFixture(t)
	gate := bootstrapUsers{Users: st, t: t, entered: make(chan struct{}, 2), resume: make(chan struct{})}
	s.users = gate
	other := NewService(Deps{Users: gate, RoleMenus: st, Passwords: s.passwords, Sessions: s.sessions, Clock: s.clock})
	result := make(chan error, 2)
	go func() { result <- s.Initialise(context.Background(), "seed password") }()
	go func() { result <- other.Initialise(context.Background(), "seed password") }()
	<-gate.entered
	<-gate.entered
	close(gate.resume)
	success := 0
	for range 2 {
		if err := <-result; err == nil {
			success++
		} else {
			var duplicate *repository.DuplicateError
			if !errors.As(err, &duplicate) || duplicate.Field != repository.DuplicateFieldUsername || duplicate.Existing != "admin" {
				t.Fatalf("并发初始化错误改变：%T", err)
			}
		}
	}
	if success != 1 {
		t.Fatal("并发初始化未恰好一次成功")
	}
	if accountBack(t, st, "admin").Rev != 1 {
		t.Fatal("竞争覆盖初始账号")
	}
	accountAudit(t, st, "user.create", "admin", 1)
}

func Test初始化失败和取消不留下账号(t *testing.T) {
	for _, kind := range []string{"hash", "invalid", "audit", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			s, st := accountFixture(t)
			fault := errors.New("散列失败")
			seed := "seed password"
			ctx := t.Context()
			want := fault
			switch kind {
			case "hash":
				s.passwords = accessPasswords{hash: func(string) (string, error) { return "", fault }}
			case "invalid":
				seed = "bad\x00"
				want = repository.ErrInvalid
			case "audit":
				s.users = accountAuditFailure{st}
				want = nil
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			}
			err := s.Initialise(ctx, seed)
			if err == nil || want != nil && !errors.Is(err, want) {
				t.Fatal("初始化错误丢失", err)
			}
			if _, err = st.GetUser(t.Context(), "admin"); !errors.Is(err, repository.ErrNotFound) {
				t.Fatal(err)
			}
			accountAudit(t, st, "user.create", "admin", 0)
		})
	}
}
