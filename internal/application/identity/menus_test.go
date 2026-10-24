package identity

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

// 防止绕过应用入口的身份和输入检查；触碰保存或读取会直接报错。
type menuGuard struct{ t *testing.T }

func (g menuGuard) GetRoleMenus(context.Context, model.Role) (repository.RoleMenuSettings, bool, error) {
	g.t.Fatal("拒绝请求读取菜单")
	return repository.RoleMenuSettings{}, false, nil
}
func (g menuGuard) SaveRoleMenus(context.Context, repository.RoleMenuSettings, repository.Audit) (repository.RoleMenuSettings, error) {
	g.t.Fatal("拒绝请求保存菜单")
	return repository.RoleMenuSettings{}, nil
}

func Test菜单身份和输入检查先于保存(t *testing.T) {
	s, _, _, b := accessFixture()
	p, err := s.Authenticate(t.Context(), b.Put("alice"))
	if err != nil {
		t.Fatal(err)
	}
	other, _, _, _ := accessFixture()
	s.roleMenus = menuGuard{t}
	s.clock = func() int64 { t.Fatal("拒绝请求读取审计时钟"); return 0 }
	for _, tc := range []struct {
		p    Principal
		want error
	}{
		{Principal{}, ports.ErrUnauthenticated}, {Principal{owner: other, user: p.user}, ports.ErrUnauthenticated},
		{Principal{owner: s, user: model.User{Username: "alice", Role: model.RoleViewer}}, ports.ErrForbidden},
		{Principal{owner: s, user: model.User{Username: "alice", Role: model.RoleAdmin, MustChangePassword: true}}, ports.ErrPasswordChangeRequired},
	} {
		if _, e := s.ListRoleMenus(t.Context(), tc.p); !errors.Is(e, tc.want) {
			t.Fatal(e)
		}
		if _, e := s.SaveRoleMenus(t.Context(), tc.p, "bad", SaveMenusCommand{}); !errors.Is(e, tc.want) {
			t.Fatal(e)
		}
	}
	for _, tc := range []struct {
		role model.Role
		cmd  SaveMenusCommand
	}{
		{"bad", SaveMenusCommand{MenuIDs: []string{}}},
		{model.RoleViewer, SaveMenusCommand{}},
		{model.RoleViewer, SaveMenusCommand{MenuIDs: []string{}, Rev: -1}},
		{model.RoleViewer, SaveMenusCommand{MenuIDs: []string{}, Rev: math.MaxInt64}},
		{model.RoleViewer, SaveMenusCommand{MenuIDs: []string{"tasks", "tasks"}}},
		{model.RoleViewer, SaveMenusCommand{MenuIDs: []string{"bad id"}}},
		{model.RoleViewer, SaveMenusCommand{MenuIDs: []string{"users"}}},
		{model.RoleViewer, SaveMenusCommand{MenuIDs: []string{"tasks-group"}}},
		{model.RoleAdmin, SaveMenusCommand{MenuIDs: []string{}}},
	} {
		if _, e := s.SaveRoleMenus(t.Context(), p, tc.role, tc.cmd); !errors.Is(e, ErrBadRequest) {
			t.Fatal(tc, e)
		}
	}
}

func Test菜单列表默认排序与显式空保存(t *testing.T) {
	s, st := accountFixture(t)
	p, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
	out, err := s.ListRoleMenus(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	roles := []model.Role{model.RoleViewer, model.RoleOperator, model.RoleHookAuthor, model.RoleHookApprover, model.RoleAdmin}
	if len(out.Roles) != 5 || len(out.Menus) != 17 {
		t.Fatal("目录或角色数量错误")
	}
	for i, role := range roles {
		if out.Roles[i].Role != role || out.Roles[i].Rev != 0 {
			t.Fatal("角色顺序或默认版本错误")
		}
	}
	want := []string{"tasks", "hosts", "templates", "hooks", "isos", "tools"}
	if !reflect.DeepEqual(out.Roles[0].MenuIDs, want) || !reflect.DeepEqual(out.Roles[0].AvailableMenuIDs, want) {
		t.Fatal(out.Roles[0])
	}
	if out.Menus[0].ID != "tasks-group" || out.Menus[16].ID != "role-menus" || !out.Menus[16].Required {
		t.Fatal("目录顺序错误")
	}
	out.Menus[0].ID = "changed"
	next, err := s.ListRoleMenus(t.Context(), p)
	if err != nil || next.Menus[0].ID != "tasks-group" {
		t.Fatal("结果共享目录", err)
	}
	saved, err := s.SaveRoleMenus(t.Context(), p, model.RoleViewer, SaveMenusCommand{MenuIDs: []string{}})
	if err != nil || saved.Rev != 1 || saved.MenuIDs == nil || len(saved.MenuIDs) != 0 {
		t.Fatal(saved, err)
	}
	next, err = s.ListRoleMenus(t.Context(), p)
	if err != nil || next.Roles[0].Rev != 1 || next.Roles[0].MenuIDs == nil || len(next.Roles[0].MenuIDs) != 0 {
		t.Fatal("显式空被恢复默认", err)
	}
	ids := []string{"tools", "tasks"}
	saved, err = s.SaveRoleMenus(t.Context(), p, model.RoleViewer, SaveMenusCommand{MenuIDs: ids, Rev: 1})
	if err != nil || saved.Rev != 2 || !reflect.DeepEqual(saved.MenuIDs, []string{"tasks", "tools"}) || !reflect.DeepEqual(ids, []string{"tools", "tasks"}) {
		t.Fatal(saved, err)
	}
	for _, rev := range []int64{0, 1} {
		if _, e := s.SaveRoleMenus(t.Context(), p, model.RoleViewer, SaveMenusCommand{MenuIDs: []string{}, Rev: rev}); !errors.Is(e, repository.ErrConflict) {
			t.Fatal(e)
		}
	}
	a := menuAudits(t, st, model.RoleViewer, 2)
	if a[0].Actor != "admin" || a[0].At != 100 || a[0].Detail != `{"before":[],"after":["tasks","tools"]}` || a[1].Detail != `{"before":null,"after":[]}` {
		t.Fatal("菜单审计内容错误")
	}
}

func menuAudits(t *testing.T, st repository.AuditLog, role model.Role, count int) []repository.AuditEntry {
	t.Helper()
	page, err := st.ListAudit(t.Context(), repository.AuditQuery{Action: "role.menus.update", TargetKind: "role", TargetID: string(role)})
	if err != nil || len(page.Items) != count {
		t.Fatalf("菜单审计数量错误，期望%d，实际%d，错误%v", count, len(page.Items), err)
	}
	return page.Items
}

func Test菜单配置不提升接口权限(t *testing.T) {
	s, st := accountFixture(t)
	admin, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
	viewer, _ := accountSeed(t, s, st, "viewer", model.RoleViewer)
	// 旧配置可能含越权页面；读取必须按当前权限重新过滤。
	_, err := st.SaveRoleMenus(t.Context(), repository.RoleMenuSettings{Role: model.RoleViewer, MenuIDs: []string{"users", "tasks"}}, repository.Audit{Actor: "seed", Action: "menu.seed", At: 1})
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.CurrentUser(t.Context(), viewer)
	if err != nil || !reflect.DeepEqual(view.MenuIDs, []string{"tasks"}) {
		t.Fatal(view, err)
	}
	if e := s.Authorize(viewer, AccessRequest{Permission: access.PermUserManage}); !errors.Is(e, ports.ErrForbidden) {
		t.Fatal(e)
	}
	if _, err = s.SaveRoleMenus(t.Context(), admin, model.RoleViewer, SaveMenusCommand{MenuIDs: []string{}, Rev: 1}); err != nil {
		t.Fatal(err)
	}
	if e := s.Authorize(viewer, AccessRequest{Permission: access.PermTaskRead}); e != nil {
		t.Fatal("隐藏菜单撤销接口权限", e)
	}
	if _, err = s.SaveRoleMenus(t.Context(), admin, model.RoleAdmin, SaveMenusCommand{MenuIDs: []string{"role-menus"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ListUsers(t.Context(), admin, UserQuery{}); err != nil {
		t.Fatal("隐藏账号菜单改变管理权限", err)
	}
}

type menuBarrier struct {
	repository.RoleMenus
	entered chan struct{}
	resume  chan struct{}
}

func (g menuBarrier) SaveRoleMenus(ctx context.Context, m repository.RoleMenuSettings, a repository.Audit) (repository.RoleMenuSettings, error) {
	g.entered <- struct{}{}
	<-g.resume
	return g.RoleMenus.SaveRoleMenus(ctx, m, a)
}

type menuAuditFailure struct{ repository.RoleMenus }

func (g menuAuditFailure) SaveRoleMenus(ctx context.Context, m repository.RoleMenuSettings, a repository.Audit) (repository.RoleMenuSettings, error) {
	a.Actor = pgtest.FaultActor
	return g.RoleMenus.SaveRoleMenus(ctx, m, a)
}

func Test身份菜单首次竞争及审计回滚(t *testing.T) {
	s, st := accountFixture(t)
	p, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
	gate := menuBarrier{RoleMenus: st, entered: make(chan struct{}, 2), resume: make(chan struct{})}
	s.roleMenus = gate
	result := make(chan error, 2)
	for range 2 {
		go func() {
			_, e := s.SaveRoleMenus(context.Background(), p, model.RoleViewer, SaveMenusCommand{MenuIDs: []string{"tasks"}})
			result <- e
		}()
	}
	<-gate.entered
	<-gate.entered
	close(gate.resume)
	successes := 0
	for range 2 {
		if e := <-result; e == nil {
			successes++
		} else if !errors.Is(e, repository.ErrConflict) {
			t.Fatal(e)
		}
	}
	if successes != 1 {
		t.Fatal("首次竞争没有恰好一次成功")
	}
	before, found, err := st.GetRoleMenus(t.Context(), model.RoleViewer)
	if err != nil || !found || before.Rev != 1 || !reflect.DeepEqual(before.MenuIDs, []string{"tasks"}) {
		t.Fatal(before, found, err)
	}
	menuAudits(t, st, model.RoleViewer, 1)
	// 用户资料已经提交，后续菜单事务失败不能把先前资料一起回滚。
	s.roleMenus = st
	if _, err = s.UpdateProfile(t.Context(), p, ProfileCommand{DisplayName: "已提交", Rev: p.user.Rev}); err != nil {
		t.Fatal(err)
	}
	s.roleMenus = menuAuditFailure{st}
	for _, role := range []model.Role{model.RoleViewer, model.RoleOperator} {
		rev := int64(0)
		if role == model.RoleViewer {
			rev = 1
		}
		_, e := s.SaveRoleMenus(t.Context(), p, role, SaveMenusCommand{MenuIDs: []string{}, Rev: rev})
		var pgerr *pgconn.PgError
		if !errors.As(e, &pgerr) || pgerr.Code != "P0001" {
			t.Fatalf("未触发真实审计失败：%T", e)
		}
	}
	back, found, err := st.GetRoleMenus(t.Context(), model.RoleViewer)
	if err != nil || !found || !reflect.DeepEqual(back, before) {
		t.Fatal("审计失败改变配置", err)
	}
	_, found, err = st.GetRoleMenus(t.Context(), model.RoleOperator)
	if err != nil || found {
		t.Fatal("首次失败仍留下配置", err)
	}
	menuAudits(t, st, model.RoleViewer, 1)
	menuAudits(t, st, model.RoleOperator, 0)
	if accountBack(t, st, "admin").DisplayName != "已提交" {
		t.Fatal("菜单失败回滚先前资料")
	}
	accountAudit(t, st, "user.profile.update", "admin", 1)
	if _, ok := s.sessions.Lookup(p.token); !ok {
		t.Fatal("菜单失败撤销会话")
	}
}

func Test菜单读取故障及取消不产生写入(t *testing.T) {
	s, st := accountFixture(t)
	p, _ := accountSeed(t, s, st, "admin", model.RoleAdmin)
	fault := errors.New("读取失败")
	s.roleMenus = &accessMenus{err: fault}
	if _, err := s.ListRoleMenus(t.Context(), p); !errors.Is(err, fault) {
		t.Fatal(err)
	}
	s.roleMenus = st
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.ListRoleMenus(ctx, p); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.SaveRoleMenus(ctx, p, model.RoleViewer, SaveMenusCommand{MenuIDs: []string{}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, found, err := st.GetRoleMenus(t.Context(), model.RoleViewer)
	if err != nil || found {
		t.Fatal("取消仍保存配置", err)
	}
	menuAudits(t, st, model.RoleViewer, 0)
}
