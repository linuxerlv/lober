package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test资料仅修改四项且菜单失败不回滚(t *testing.T) {
	s, st := accountFixture(t)
	p, before := accountSeed(t, s, st, "alice", model.RoleViewer)
	s.roleMenus = &accessMenus{err: errors.New("保存后菜单故障")}
	cmd := ProfileCommand{DisplayName: "已提交", Email: "alice@example.com", Avatar: "", Bio: "个人资料", Rev: before.Rev}
	if _, err := s.UpdateProfile(t.Context(), p, cmd); err == nil {
		t.Fatal("菜单故障被吞掉")
	}
	back := accountBack(t, st, "alice")
	expected := before
	expected.DisplayName = cmd.DisplayName
	expected.Email = cmd.Email
	expected.Avatar = cmd.Avatar
	expected.Bio = cmd.Bio
	expected.Rev++
	if back != expected {
		t.Fatal("资料提交范围错误")
	}
	a := accountAudit(t, st, "user.profile.update", "alice", 1)[0]
	if a.Actor != "alice" || a.At != 100 {
		t.Fatal("资料审计错误")
	}
	if _, ok := s.sessions.Lookup(p.token); !ok {
		t.Fatal("资料更新撤销会话")
	}
	if _, err := s.UpdateProfile(t.Context(), p, cmd); !errors.Is(err, repository.ErrConflict) {
		t.Fatal(err)
	}
	accountAudit(t, st, "user.profile.update", "alice", 1)
	s.roleMenus = st
	next, err := s.Authenticate(t.Context(), p.token)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Rev = back.Rev
	cmd.Bio = "第二次"
	out, err := s.UpdateProfile(t.Context(), next, cmd)
	if err != nil || out.Rev != back.Rev+1 || out.Bio != "第二次" || len(out.MenuIDs) == 0 {
		t.Fatal(out, err)
	}
}

func Test资料检查先于快照版本冲突(t *testing.T) {
	s, u, _, b := accessFixture()
	p, err := s.Authenticate(t.Context(), b.Put("alice"))
	if err != nil {
		t.Fatal(err)
	}
	before := u.reads
	s.clock = func() int64 { t.Fatal("无效资料读审计时钟"); return 0 }
	if _, err := s.UpdateProfile(t.Context(), p, ProfileCommand{Rev: p.user.Rev + 1, Email: "invalid"}); !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.UpdateProfile(t.Context(), p, ProfileCommand{Rev: p.user.Rev + 1}); !errors.Is(err, repository.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.UpdateProfile(t.Context(), p, ProfileCommand{}); !errors.Is(err, ErrBadRequest) {
		t.Fatal(err)
	}
	if u.reads != before {
		t.Fatal("资料请求额外认证用户")
	}
}

func Test慢资料请求不能用猜测新修订号恢复旧权限或凭据(t *testing.T) {
	for _, kind := range []string{"reset", "disable", "demote"} {
		t.Run(kind, func(t *testing.T) {
			s, st := accountFixture(t)
			admin, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
			p, u := accountSeed(t, s, st, "victim", model.RoleAdmin)
			// 认证快照已经取得；请求在交给资料入口前暂停，管理员先提交安全字段。
			entered, resume := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			go func() {
				close(entered)
				<-resume
				_, e := s.UpdateProfile(context.Background(), p, ProfileCommand{DisplayName: "stale", Rev: u.Rev + 1})
				done <- e
			}()
			<-entered
			cmd := UpdateUserCommand{Role: model.RoleAdmin, Rev: u.Rev}
			switch kind {
			case "reset":
				cmd.Password = "replacement"
			case "disable":
				cmd.Disabled = true
			case "demote":
				cmd.Role = model.RoleViewer
			}
			_, err := s.UpdateUser(t.Context(), admin, "victim", cmd)
			close(resume)
			if err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, repository.ErrConflict) {
				t.Fatal("慢资料请求被接受", err)
			}
			back := accountBack(t, st, "victim")
			if back.Rev != u.Rev+1 || back.Role != cmd.Role || back.Disabled != cmd.Disabled || back.DisplayName != "" {
				t.Fatal("慢资料覆盖安全字段")
			}
			if kind == "reset" {
				if !s.passwords.Check("replacement", back.Password) {
					t.Fatal("慢资料恢复旧密码")
				}
			} else if back.Password != u.Password {
				t.Fatal("密码意外变化")
			}
			accountAudit(t, st, "user.profile.update", "victim", 0)
			accountAudit(t, st, "user.update", "victim", 1)
			_, ok := s.sessions.Lookup(p.token)
			if ok != (kind == "demote") {
				t.Fatal("会话撤销规则改变")
			}
		})
	}
}
