package identity

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/lober/lober/internal/application/identity/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type accessUsers struct {
	repository.Users
	mu    sync.Mutex
	user  model.User
	err   error
	reads int
}

func (u *accessUsers) GetUser(context.Context, string) (model.User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reads++
	return u.user, u.err
}

type accessMenus struct {
	repository.RoleMenus
	ids   []string
	found bool
	err   error
}

func (m *accessMenus) GetRoleMenus(context.Context, model.Role) (repository.RoleMenuSettings, bool, error) {
	return repository.RoleMenuSettings{MenuIDs: m.ids, Rev: 7}, m.found, m.err
}

type accessPasswords struct {
	check func(string, string) bool
	dummy func(string)
	hash  func(string) (string, error)
}

func (p accessPasswords) Hash(s string) (string, error) {
	if p.hash != nil {
		return p.hash(s)
	}
	return "hash:" + s, nil
}
func (p accessPasswords) Check(s, h string) bool {
	if p.check != nil {
		return p.check(s, h)
	}
	return h == "hash:"+s
}
func (p accessPasswords) CheckDummy(s string) {
	if p.dummy != nil {
		p.dummy(s)
	}
}

type accessTokens struct {
	n     int
	value string
	err   error
}

func (p *accessTokens) NewToken() (string, error) {
	p.n++
	if p.err != nil || p.value != "" {
		return p.value, p.err
	}
	return fmt.Sprintf("%064x", p.n), nil
}
func accessFixture() (*Service, *accessUsers, *accessMenus, *SessionBook) {
	users := &accessUsers{user: model.User{Username: "alice", Role: model.RoleAdmin, Password: "hash:old", Rev: 3, CreatedAt: 1}}
	menus := &accessMenus{}
	book := NewSessionBook(10, func() int64 { return 100 }, &accessTokens{})
	s := NewService(Deps{Users: users, RoleMenus: menus, Passwords: accessPasswords{}, Sessions: book, Clock: func() int64 { return 100 }})
	return s, users, menus, book
}

func Test身份登录凭据复核后才发布会话(t *testing.T) {
	s, users, _, book := accessFixture()
	checked, resume := make(chan struct{}), make(chan struct{})
	s.passwords = accessPasswords{check: func(p, h string) bool { close(checked); <-resume; return p == "old" && h == "hash:old" }}
	result := make(chan error, 1)
	go func() {
		_, err := s.Login(context.Background(), LoginCommand{Username: "alice", Password: "old"})
		result <- err
	}()
	<-checked
	// 模拟后续账号操作：提交新密码和撤销旧会话必须共用此写锁。
	s.credentials.Lock()
	users.mu.Lock()
	users.user.Password = "hash:new"
	users.user.Rev++
	users.mu.Unlock()
	book.DropUser("alice")
	s.credentials.Unlock()
	close(resume)
	if err := <-result; !errors.Is(err, ErrLoginFailed) {
		t.Fatal("旧口令在重置后发布会话", err)
	}
	if len(book.byToken) != 0 {
		t.Fatal("失败登录留下会话")
	}
}

func Test身份会话期限与随机令牌契约(t *testing.T) {
	now, calls := int64(100), 0
	clock := func() int64 { calls++; return now }
	b := NewSessionBook(0, clock, &accessTokens{})
	if calls != 0 {
		t.Fatal("构造读了时钟")
	}
	if _, ok := b.Lookup("missing"); ok || calls != 0 {
		t.Fatal("缺失令牌读了时钟")
	}
	a := b.Put("alice")
	if b.ExpiresAt(a) != 43300 || len(a) != 64 {
		t.Fatal("默认期限或令牌错误")
	}
	now = 43299
	if _, ok := b.Lookup(a); !ok {
		t.Fatal("提前过期")
	}
	now = 43300
	if _, ok := b.Lookup(a); ok {
		t.Fatal("到期边界仍有效")
	}
	for _, n := range []int64{-1, math.MaxInt64 - 1} {
		now = n
		b = NewSessionBook(10, clock, &accessTokens{})
		if b.ExpiresAt(b.Put("alice")) != math.MaxInt64 {
			t.Fatal("期限未饱和")
		}
	}
	for _, bad := range []string{"bad", strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		t.Run(bad[:1], func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("坏令牌未拒绝")
				}
			}()
			NewSessionBook(1, clock, &accessTokens{value: bad}).Put("alice")
		})
	}
	t.Run("随机源失败", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("随机源失败未拒绝")
			}
		}()
		NewSessionBook(1, clock, &accessTokens{err: errors.New("random")}).Put("alice")
	})
}

func Test身份认证保留错误与令牌副作用(t *testing.T) {
	for _, tc := range []struct {
		name     string
		role     model.Role
		disabled bool
		err      error
		drop     bool
		want     error
	}{
		{"缺失", model.RoleAdmin, false, repository.ErrNotFound, true, ports.ErrUnauthenticated},
		{"禁用", model.RoleAdmin, true, nil, true, ports.ErrUnauthenticated},
		{"无效角色", "unknown", false, nil, false, ports.ErrUnauthenticated},
		{"仓储故障", model.RoleAdmin, false, repository.ErrConflict, false, repository.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, u, _, b := accessFixture()
			tok := b.Put("alice")
			u.user.Role = tc.role
			u.user.Disabled = tc.disabled
			u.err = tc.err
			_, err := s.Authenticate(t.Context(), tok)
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			_, ok := b.Lookup(tok)
			if ok == tc.drop {
				t.Fatal("令牌删除副作用错误")
			}
		})
	}
}

func Test身份快照归属与菜单副本(t *testing.T) {
	s, u, m, b := accessFixture()
	tok := b.Put("alice")
	p, err := s.Authenticate(t.Context(), tok)
	if err != nil {
		t.Fatal(err)
	}
	other, _, _, _ := accessFixture()
	for _, bad := range []Principal{{}, p} {
		if !errors.Is(other.Authorize(bad, AccessRequest{Permission: access.PermAnyUser}), ports.ErrUnauthenticated) {
			t.Fatal("接受伪造归属")
		}
		if _, err := other.CurrentUser(t.Context(), bad); !errors.Is(err, ports.ErrUnauthenticated) {
			t.Fatal(err)
		}
		if err := other.Logout(t.Context(), bad); !errors.Is(err, ports.ErrUnauthenticated) {
			t.Fatal(err)
		}
	}
	u.user.DisplayName = "changed"
	first, err := s.CurrentUser(t.Context(), p)
	if err != nil || first.DisplayName != "" || len(first.Permissions) != 16 {
		t.Fatal(first, err)
	}
	first.Permissions[0] = "bad"
	first.MenuIDs[0] = "bad"
	second, _ := s.CurrentUser(t.Context(), p)
	if second.Permissions[0] != "host:read" || second.MenuIDs[0] != "tasks" {
		t.Fatal("结果共享可变切片")
	}
	m.found = true
	m.ids = []string{"users", "tasks"}
	view, _ := s.CurrentUser(t.Context(), p)
	if !reflect.DeepEqual(view.MenuIDs, []string{"tasks", "users", "role-menus"}) || view.MenuRev != 7 {
		t.Fatal(view)
	}
	view.MenuIDs[0] = "bad"
	if m.ids[0] != "users" {
		t.Fatal("菜单泄露存储切片")
	}
	if p.Actor().Username != "alice" {
		t.Fatal("操作者错误")
	}
	if err := s.Logout(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Lookup(tok); ok {
		t.Fatal("退出未撤销")
	}
}

func Test身份登录失败顺序与菜单故障(t *testing.T) {
	s, u, m, b := accessFixture()
	dummy := 0
	checks := 0
	s.passwords = accessPasswords{check: func(p, h string) bool { checks++; return true }, dummy: func(p string) { dummy++ }}
	u.err = repository.ErrNotFound
	if _, err := s.Login(t.Context(), LoginCommand{"alice", "old"}); !errors.Is(err, ErrLoginFailed) || dummy != 1 || checks != 0 {
		t.Fatal(err, dummy, checks)
	}
	u.err = nil
	u.user.Disabled = true
	if _, err := s.Login(t.Context(), LoginCommand{"alice", "old"}); !errors.Is(err, ErrLoginFailed) || checks != 1 {
		t.Fatal("禁用绕过口令校验", err)
	}
	u.user.Disabled = false
	m.err = repository.ErrConflict
	if _, err := s.Login(t.Context(), LoginCommand{"alice", "old"}); !errors.Is(err, repository.ErrConflict) || len(b.byToken) != 0 {
		t.Fatal("菜单失败发布会话", err)
	}
	m.err = nil
	r, err := s.Login(t.Context(), LoginCommand{"alice", "old"})
	if err != nil || r.ExpiresAt != 110 || r.User.Username != "alice" {
		t.Fatal(r, err)
	}
	if _, ok := b.Lookup(r.Token); !ok {
		t.Fatal("未使用注入会话表")
	}
	for _, cmd := range []LoginCommand{{"", "old"}, {"alice", ""}} {
		before := u.reads
		if _, err := s.Login(t.Context(), cmd); !errors.Is(err, ErrBadRequest) || u.reads != before {
			t.Fatal("必填检查顺序错误", err)
		}
	}
}

func Test身份事件流复查不删除异常账号令牌(t *testing.T) {
	s, u, _, b := accessFixture()
	tok := b.Put("alice")
	if !s.StreamAuthorized(t.Context(), tok, access.PermTaskRead) {
		t.Fatal("有效事件流拒绝")
	}
	for _, tc := range []model.User{{Username: "alice", Role: model.RoleAdmin, Disabled: true}, {Username: "alice", Role: model.RoleAdmin, MustChangePassword: true}, {Username: "alice", Role: "unknown"}} {
		u.user = tc
		if s.StreamAuthorized(t.Context(), tok, access.PermTaskRead) {
			t.Fatal("异常事件流通过")
		}
		if _, ok := b.Lookup(tok); !ok {
			t.Fatal("流复查额外删除会话")
		}
	}
	u.err = repository.ErrNotFound
	if s.StreamAuthorized(t.Context(), tok, access.PermTaskRead) {
		t.Fatal("缺失账号通过")
	}
	if _, ok := b.Lookup(tok); !ok {
		t.Fatal("流复查删除缺失账号令牌")
	}
}

type pausedAccessMenus struct {
	repository.RoleMenus
	entered, resume chan struct{}
}

func (m pausedAccessMenus) GetRoleMenus(context.Context, model.Role) (repository.RoleMenuSettings, bool, error) {
	close(m.entered)
	<-m.resume
	return repository.RoleMenuSettings{}, false, nil
}

func Test身份发布会话全程持有凭据读锁(t *testing.T) {
	s, u, _, b := accessFixture()
	entered, resume := make(chan struct{}), make(chan struct{})
	s.roleMenus = pausedAccessMenus{entered: entered, resume: resume}
	result := make(chan SessionResult, 1)
	failure := make(chan error, 1)
	go func() {
		r, err := s.Login(context.Background(), LoginCommand{"alice", "old"})
		result <- r
		failure <- err
	}()
	<-entered
	if s.credentials.TryLock() {
		s.credentials.Unlock()
		close(resume)
		t.Fatal("菜单读取前释放了凭据锁")
	}
	done := make(chan struct{})
	go func() {
		s.credentials.Lock()
		defer s.credentials.Unlock()
		u.mu.Lock()
		u.user.Password = "hash:new"
		u.mu.Unlock()
		b.DropUser("alice")
		close(done)
	}()
	close(resume)
	r := <-result
	if err := <-failure; err != nil {
		t.Fatal(err)
	}
	<-done
	if _, ok := b.Lookup(r.Token); ok {
		t.Fatal("重置完成后仍保留旧口令会话")
	}
}

func Test身份非法新口令不调用计算能力(t *testing.T) {
	s, _, _, _ := accessFixture()
	called := 0
	s.passwords = accessPasswords{hash: func(string) (string, error) { called++; return "", port.ErrInvalidPassword }}
	for _, bad := range []string{"", "x\x00", "x\xff"} {
		if _, err := s.hashPassword(bad); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if called != 0 {
		t.Fatal("非法口令进入计算能力")
	}
	if _, err := s.hashPassword("valid"); !errors.Is(err, repository.ErrInvalid) || called != 1 {
		t.Fatal("未转换能力错误", err)
	}
	fault := errors.New("计算失败")
	s.passwords = accessPasswords{hash: func(string) (string, error) { return "", fault }}
	if _, err := s.hashPassword("valid"); !errors.Is(err, fault) {
		t.Fatal("错误树丢失")
	}
}

func Test身份会话清理隔离与构造校验(t *testing.T) {
	now := int64(100)
	b := NewSessionBook(10, func() int64 { return now }, &accessTokens{})
	old := b.Put("alice")
	now = 105
	alice := b.Put("alice")
	bob := b.Put("bob")
	now = 110
	fresh := b.Put("alice")
	if b.ExpiresAt(old) != 0 {
		t.Fatal("发布时没清理到期会话")
	}
	b.DropUser("alice")
	for _, tok := range []string{alice, fresh} {
		if _, ok := b.Lookup(tok); ok {
			t.Fatal("用户撤销未覆盖全部会话")
		}
	}
	if name, ok := b.Lookup(bob); !ok || name != "bob" {
		t.Fatal("撤销波及其他账号")
	}
	for _, tc := range []struct {
		ttl    int64
		clock  ports.Clock
		tokens port.Tokens
	}{{-1, func() int64 { return 0 }, &accessTokens{}}, {1, nil, &accessTokens{}}, {1, func() int64 { return 0 }, nil}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("非法会话构造成功")
				}
			}()
			NewSessionBook(tc.ttl, tc.clock, tc.tokens)
		}()
	}
	s, _, _, _ := accessFixture()
	valid := Deps{Users: s.users, RoleMenus: s.roleMenus, Passwords: s.passwords, Sessions: s.sessions, Clock: s.clock}
	for i := 0; i < 5; i++ {
		d := valid
		switch i {
		case 0:
			d.Users = nil
		case 1:
			d.RoleMenus = nil
		case 2:
			d.Passwords = nil
		case 3:
			d.Sessions = nil
		case 4:
			d.Clock = nil
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Error("缺失依赖未拒绝", i)
				}
			}()
			NewService(d)
		}()
	}
}

func Test身份授权使用快照且强制改密优先(t *testing.T) {
	s, u, _, b := accessFixture()
	u.user.MustChangePassword = true
	p, err := s.Authenticate(t.Context(), b.Put("alice"))
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []Operation{UserInfo, Logout, ChangePassword, LegacySelfUpdate} {
		if err := s.Authorize(p, AccessRequest{Permission: access.PermAnyUser, Operation: op, TargetUsername: "alice"}); err != nil {
			t.Fatal(op, err)
		}
	}
	if !errors.Is(s.Authorize(p, AccessRequest{Permission: "unknown"}), ports.ErrPasswordChangeRequired) {
		t.Fatal("错误优先级变化")
	}
	if !errors.Is(s.Authorize(p, AccessRequest{Permission: access.PermUserManage, Operation: LegacySelfUpdate, TargetUsername: "bob"}), ports.ErrPasswordChangeRequired) {
		t.Fatal("例外允许改其他人")
	}
	u.user.MustChangePassword = false
	if !errors.Is(s.Authorize(p, AccessRequest{Permission: access.PermTaskRead}), ports.ErrPasswordChangeRequired) {
		t.Fatal("快照被仓储后续改写")
	}
	for _, p := range append(access.AllPerms(), access.PermAnyUser) {
		if !KnownPermission(p) {
			t.Fatal("合法权限未识别", p)
		}
	}
	if KnownPermission("unknown") || KnownPermission("") {
		t.Fatal("未知权限被识别")
	}
}
