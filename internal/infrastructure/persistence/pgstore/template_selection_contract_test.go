package pgstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"gorm.io/gorm"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

func Test模板候选资格只由领域实现(t *testing.T) {
	data, err := os.ReadFile("template_selection.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, duplicate := range []string{"func templateSelectionQuery", "func allowedVersionPairs", "func parameterElements", "h.state = 'deprecated'", "p->>'Offer'"} {
		if strings.Contains(source, duplicate) {
			t.Fatalf("数据库仍独立实现模板资格：%s", duplicate)
		}
	}
	if !strings.Contains(source, "selection.TemplateEligible(") {
		t.Fatal("候选扫描没有调用唯一领域资格规则")
	}
}

// 大批夹具只绕过审批写状态，模板和精确引用仍从正常保存入口写入；本组只测候选读取。
func candidateTemplates(t *testing.T, s *pgstore.Store, n int, hook *model.Hook) []model.Template {
	t.Helper()
	out := make([]model.Template, 0, n)
	for i := 0; i < n; i++ {
		in := sampleTemplate(fmt.Sprintf("batch-%04d", i))
		if hook != nil {
			in = referencing(fullTemplate(in.Name), hook.Key())
		}
		tpl, err := s.CreateTemplate(t.Context(), in)
		must(t, err, "建候选夹具")
		out = append(out, tpl)
	}
	must(t, s.DBForTest().Exec("UPDATE template_versions SET state='approved', approver='bob'").Error, "准备已审批版本")
	for i := range out {
		out[i].State = model.VersionApproved
		out[i].Approver = "bob"
	}
	return out
}

func candidateSelection() *repository.TemplateSelection {
	return &repository.TemplateSelection{ISO: model.ISO{ID: "iso", Name: "Rocky", Family: "linux", Series: "rocky", Version: "9", Arch: "x86_64", State: model.ISOReady}}
}

func assertCandidatePage(t *testing.T, s *pgstore.Store, q repository.TemplateQuery, count int, total int64) repository.Page[repository.TemplateCandidateFacts] {
	t.Helper()
	facts, err := s.ListTemplateCandidates(t.Context(), q)
	checkNumberedPage(t, facts, err, count, total)
	if facts.Items == nil {
		t.Fatal("空Items不能为nil")
	}
	legacy, err := s.ListTemplates(t.Context(), q)
	must(t, err, "兼容Selection入口")
	templates := make([]model.Template, 0, len(facts.Items))
	for _, f := range facts.Items {
		templates = append(templates, f.Template)
		keys := repository.TemplateHookKeys([]model.Template{f.Template})
		for k := range f.Hooks {
			if !slices.Contains(keys, k) {
				t.Fatalf("单项夹带别的模板引用：%v", k)
			}
		}
		expected, err := s.GetHooks(t.Context(), keys)
		must(t, err, "精确事实参考")
		if !reflect.DeepEqual(f.Hooks, expected) {
			t.Fatal("返回组件不是模板自身精确引用")
		}
	}
	if !reflect.DeepEqual(templates, legacy.Items) || facts.Next != legacy.Next || !reflect.DeepEqual(facts.Total, legacy.Total) {
		t.Fatal("新旧Selection入口的版本、顺序、Next或Total不同")
	}
	return facts
}

func Test模板候选跨批精确引用与分页一致(t *testing.T) {
	t.Run("260模板合格257末页17", func(t *testing.T) {
		s := pgtest.OpenStore(t)
		hook := hookOps.to(t, s, hookOps.mustCreate(t, s, "batch-hook"), model.VersionApproved)
		templates := candidateTemplates(t, s, 260, nil)
		// 后4项仅第257项合格；最高审批不合格不能回退版本1。
		bad := templates[257]
		bad.Params = []model.ParamDecl{{Name: "edition", Type: "enum", Offer: "editions"}}
		data, err := jsonBytes(bad.Params)
		must(t, err, "编码参数")
		must(t, s.DBForTest().Exec("UPDATE template_versions SET params=CAST(? AS jsonb) WHERE template_id=?", data, bad.ID).Error, "缺选项")
		bad = templates[258]
		bad.Assembly = map[string][]model.Instance{"pre": {{HookID: hook.ID, HookVersion: 1}}}
		data, err = jsonBytes(bad.Assembly)
		must(t, err, "编码引用")
		must(t, s.DBForTest().Exec("UPDATE template_versions SET assembly=CAST(? AS jsonb) WHERE template_id=?", data, bad.ID).Error, "引用即将废弃组件")
		must(t, s.DeprecateHook(t.Context(), hook.Key(), hook.Rev, anAudit("hook.deprecate")), "废弃引用")
		next, err := s.NewTemplateVersion(t.Context(), templates[259].ID, 1, "alice")
		must(t, err, "开最高版本")
		next.Series = []catalog.Series{"ubuntu"}
		next, err = s.SaveTemplateDraft(t.Context(), next)
		must(t, err, "改最高版本的系")
		must(t, s.DBForTest().Exec("UPDATE template_versions SET state='approved', approver='bob' WHERE template_id=? AND version=2", next.ID).Error, "最高审批不合格")
		for _, n := range []int{1, 13, 14, int(^uint(0)>>1)/20 + 1} {
			count := 20
			if n == 13 {
				count = 17
			}
			if n > 13 {
				count = 0
			}
			page := assertCandidatePage(t, s, repository.TemplateQuery{Selection: candidateSelection(), Page: n, Limit: 20, Series: "rocky"}, count, 257)
			if (n < 13) != (page.Next != "") {
				t.Fatal("Next边界", n, page.Next)
			}
			for i, f := range page.Items {
				if f.Template.ID != templates[(n-1)*20+i].ID {
					t.Fatal("跨256项批次顺序错误")
				}
			}
		}
		// 游标也是先资格后分页，但不带总数；两入口遍历全部页必须完全一致。
		var cursor string
		var all []model.Template
		for {
			q := repository.TemplateQuery{Selection: candidateSelection(), Limit: 20, Cursor: cursor, Series: "rocky"}
			facts, err := s.ListTemplateCandidates(t.Context(), q)
			must(t, err, "候选游标")
			old, err := s.ListTemplates(t.Context(), q)
			must(t, err, "旧Selection游标")
			if facts.Total != nil || old.Total != nil || facts.Next != old.Next || len(facts.Items) != len(old.Items) {
				t.Fatal("游标协议变化")
			}
			for i, f := range facts.Items {
				if !reflect.DeepEqual(f.Template, old.Items[i]) {
					t.Fatal("游标版本/顺序不同")
				}
				all = append(all, f.Template)
			}
			cursor = facts.Next
			if cursor == "" {
				break
			}
			if len(all) > 260 {
				t.Fatal("游标重复")
			}
		}
		if len(all) != 257 {
			t.Fatal(len(all))
		}
	})
	t.Run("第1001精确引用不能漏读", func(t *testing.T) {
		s := pgtest.OpenStore(t)
		templates := candidateTemplates(t, s, 2, nil)
		for i := 0; i < 1002; i++ {
			h, err := s.CreateHook(t.Context(), sampleHook(fmt.Sprintf("exact-%04d", i)))
			must(t, err, "建独立组件")
			j := i % 2
			if templates[j].Assembly == nil {
				templates[j].Assembly = map[string][]model.Instance{}
			}
			templates[j].Assembly["pre"] = append(templates[j].Assembly["pre"], model.Instance{HookID: h.ID, HookVersion: 1})
		}
		must(t, s.DBForTest().Exec("UPDATE hook_versions SET state='approved', approver='bob'").Error, "准备组件审批")
		for _, tpl := range templates {
			body, err := jsonBytes(tpl.Assembly)
			must(t, err, "编码大批引用")
			must(t, s.DBForTest().Exec("UPDATE template_versions SET assembly=CAST(? AS jsonb) WHERE template_id=?", body, tpl.ID).Error, "准备引用正文")
		}
		keys := repository.TemplateHookKeys(templates)
		if len(keys) != 1002 {
			t.Fatal("夹具引用数", len(keys))
		}
		sizes := []int64{}
		callback := "task6_hook_batches"
		must(t, s.DBForTest().Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
			if db.Statement.Table == "hook_versions" {
				sizes = append(sizes, db.RowsAffected)
			}
		}), "注册测试组件批屏障")
		defer s.DBForTest().Callback().Query().Remove(callback)
		assertCandidatePage(t, s, repository.TemplateQuery{Selection: candidateSelection(), Page: 1}, 2, 2)
		if !slices.Contains(sizes, int64(1000)) || !slices.Contains(sizes, int64(2)) {
			t.Fatal("没有按1000读取第二组件批", sizes)
		}
		denied := keys[1000]
		must(t, s.DBForTest().Exec("UPDATE hook_versions SET state='deprecated' WHERE hook_id=? AND version=?", denied.ID, denied.Version).Error, "第1001引用改为拒绝事实")
		got := assertCandidatePage(t, s, repository.TemplateQuery{Selection: candidateSelection(), Page: 1}, 1, 1)
		if slices.Contains(model.HookRefs(got.Items[0].Template), denied) {
			t.Fatal("漏读第二批拒绝事实")
		}
	})
}

func jsonBytes(v any) (string, error) { b, e := json.Marshal(v); return string(b), e }

func Test模板候选读取期间变更仍使用同次事实(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	writer := pgtest.OpenAt(t, dsn)
	hook := hookOps.to(t, s, hookOps.mustCreate(t, s, "snapshot-hook"), model.VersionApproved)
	templates := candidateTemplates(t, s, 260, &hook)
	next, err := s.NewTemplateVersion(t.Context(), templates[259].ID, 1, "alice")
	must(t, err, "准备并发新版本")
	next = templateOps.to(t, s, next, model.VersionReview)
	reached, resume := make(chan struct{}), make(chan struct{})
	once := sync.Once{}
	callback := "task6_snapshot_barrier"
	must(t, s.DBForTest().Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if db.Statement.Table == "template_versions" && db.RowsAffected == 256 {
			once.Do(func() {
				var isolation, readOnly string
				if e := db.Session(&gorm.Session{NewDB: true}).Raw("SHOW transaction_isolation").Row().Scan(&isolation); e != nil {
					db.AddError(e)
					return
				}
				if e := db.Session(&gorm.Session{NewDB: true}).Raw("SHOW transaction_read_only").Row().Scan(&readOnly); e != nil {
					db.AddError(e)
					return
				}
				if isolation != "repeatable read" || readOnly != "on" {
					db.AddError(fmt.Errorf("读取事务=%s/%s", isolation, readOnly))
					return
				}
				close(reached)
				<-resume
			})
		}
	}), "注册首批测试屏障")
	defer s.DBForTest().Callback().Query().Remove(callback)
	type result struct {
		page repository.Page[repository.TemplateCandidateFacts]
		err  error
	}
	done := make(chan result, 1)
	go func() {
		p, e := s.ListTemplateCandidates(t.Context(), repository.TemplateQuery{Selection: candidateSelection(), Page: 13, Limit: 20})
		done <- result{p, e}
	}()
	select {
	case <-reached:
	case r := <-done:
		t.Fatalf("未到首批屏障：%v", r.err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	func() {
		defer close(resume)
		must(t, writer.ApproveTemplate(t.Context(), model.TemplateKey{ID: next.ID, Version: next.Version}, next.Rev, "bob", anAudit("template.approve")), "第二连接审批")
		must(t, writer.RenameTemplate(t.Context(), templates[258].ID, "aaa-moved", anAudit("template.rename")), "第二连接改名")
		must(t, writer.DeprecateHook(t.Context(), hook.Key(), hook.Rev, anAudit("hook.deprecate")), "第二连接废弃")
	}()
	got := <-done
	checkNumberedPage(t, got.page, got.err, 20, 260)
	for i, f := range got.page.Items {
		if f.Template.ID != templates[240+i].ID || f.Template.Version != 1 || f.Template.Name != templates[240+i].Name || f.Hooks[hook.Key()].State != model.VersionApproved {
			t.Fatal("同次事实混入后续变更", f.Template)
		}
	}
	after, err := s.ListTemplateCandidates(t.Context(), repository.TemplateQuery{Selection: candidateSelection(), Page: 1})
	checkNumberedPage(t, after, err, 0, 0)
	current, err := s.GetTemplate(t.Context(), model.TemplateKey{ID: next.ID, Version: next.Version})
	must(t, err, "下一查询新审批")
	if current.State != model.VersionApproved {
		t.Fatal(current)
	}
	renamed, err := s.GetTemplate(t.Context(), model.TemplateKey{ID: templates[258].ID, Version: templates[258].Version})
	must(t, err, "下一查询改名")
	if renamed.Name != "aaa-moved" {
		t.Fatal(renamed)
	}
}

func Test模板候选取消不返回半页或半总数(t *testing.T) {
	s := pgtest.OpenStore(t)
	candidateTemplates(t, s, 260, nil)
	sqlDB, err := s.DBForTest().DB()
	must(t, err, "连接池")
	sqlDB.SetMaxOpenConns(1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queries := 0
	callback := "task6_cancel_barrier"
	must(t, s.DBForTest().Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if db.Statement.Table == "template_versions" {
			queries++
			if queries == 2 {
				cancel()
			}
		}
	}), "注册后批取消屏障")
	p, err := s.ListTemplateCandidates(ctx, repository.TemplateQuery{Selection: candidateSelection(), Page: 1, Limit: 20})
	must(t, s.DBForTest().Callback().Query().Remove(callback), "清理取消屏障")
	if !errors.Is(err, context.Canceled) || p.Items != nil || p.Total != nil || p.Next != "" || queries != 2 {
		t.Fatal("取消交付半页或未在后批发生", p, err, queries)
	}
	var inTransaction bool
	must(t, s.DBForTest().WithContext(t.Context()).Raw("SELECT txid_current_if_assigned() IS NOT NULL").Row().Scan(&inTransaction), "事务结束后复用唯一连接")
	if inTransaction {
		t.Fatal("只读事务仍占用连接")
	}
	assertCandidatePage(t, s, repository.TemplateQuery{Selection: candidateSelection(), Page: 13, Limit: 20}, 20, 260)
	if sqlDB.Stats().InUse != 0 {
		t.Fatal("连接未归还", sqlDB.Stats())
	}
}

func Test模板候选要求Selection且保留参数错误(t *testing.T) {
	s := pgtest.OpenStore(t)
	for _, q := range []repository.TemplateQuery{{}, {Page: -1}, {Selection: candidateSelection(), Page: -1}, {Selection: candidateSelection(), Page: 1, Cursor: "bad"}, {Selection: candidateSelection(), Limit: 501}, {Selection: candidateSelection(), Cursor: repository.EncodeCursor("hooks", "x", "id")}, {Selection: candidateSelection(), Page: int(^uint(0) >> 1), Limit: 500}} {
		_, err := s.ListTemplateCandidates(t.Context(), q)
		if !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(q, err)
		}
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = s.ListTemplateCandidates(canceled, q)
		if !errors.Is(err, repository.ErrInvalid) {
			t.Fatal("参数错误必须先于取消", q, err)
		}
	}
	for _, q := range []repository.TemplateQuery{{Selection: candidateSelection(), Page: 1, Series: "rocky\x00"}, {Selection: candidateSelection(), Page: 1, NamePrefix: "\x00"}, {Selection: candidateSelection(), Page: 1, States: []model.VersionState{"\x00"}}} {
		assertCandidatePage(t, s, q, 0, 0)
	}
}
