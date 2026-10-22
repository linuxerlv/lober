package pgstore_test

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
)

// Test契约模板 是模板这一组（C12–C17、C22–C26）：名字、版本的生命周期、内置内容与查询由 versioned 的共用用例查，与
// hook 那一组是同一份；这里另查模板自己的：文本存不下、派生、审批时连同引用的 hook 版本一起重跑激活判定、按系过滤、
// 引用关系（删 hook 草稿），以及深拷贝。
func Test契约模板(t *testing.T) {
	runCases(t, templateOps.cases())
	runCases(t, []testCase{
		{"正文与别的文本存不下报 text_not_storable，问题落在那一处", templateUnstorable},
		{"DerivedFrom 只由派生写：新建时带了、保存时改了都报 ErrInvalid", templateDerivedFromFixed},
		{"派生：复制来源的全部内容，记下来源；来源什么状态都可以；不审计", templateDerive},
		{"派生：名字照样要唯一、合规则、不用保留前缀，来源不存在报 ErrNotFound，作者不合法报 ErrInvalid", templateDeriveBad},
		{"审批：激活判定不通过报 ProblemsError(ErrRejected)，问题与直接调用 activation 的相同，状态不变", templateApproveRejected},
		{"审批：对引用的 hook 版本重跑判定，刚被废弃的报 hook_ref_deprecated", templateApproveHookRefs},
		{"ListTemplates 按系过滤，看的是给出的那一项", templateListSeries},
		{"引用：模板的任一版本引用了 hook 草稿，草稿就删不掉；不再引用之后就能删", templateRefs},
		{"引用的 hook 版本多于一批：保存时第二批的引用照样记下，审批与建任务时第二批的照样取得出来", hookRefsManyBatches},
		{"hook 与模板的名字各管各的", templateNamespaces},
		{"返回值与入参都是拷贝：Series、Params、Bindings、Assembly、DerivedFrom 是深拷贝", templateCopies},
	})
	runAudited(t, templateOps.auditedOps())
}

// kickstartBody 是一份能通过激活判定的 kickstart 正文：三个必需部件各一次，一个片段插槽，引用模板参数 hostname。
const kickstartBody = "text\n{{lober:syslog}}\n{{slot:分区}}\nnetwork --hostname={{param:hostname}}\n%post\n{{lober:firstboot}}\n{{lober:cleanup}}\n%end\n"

// sampleTemplate 是新建模板的入参：能通过激活判定，作者是 alice。插槽里没有实例，共用用例不必先备 hook；空的插槽
// 读回来也要是空的，不是 nil。
func sampleTemplate(name string) model.Template {
	return model.Template{
		Name: name, Author: "alice", Family: "linux", Series: []catalog.Series{"rocky", "almalinux"},
		Body:     kickstartBody,
		Params:   []model.ParamDecl{{Name: "hostname", Label: "主机名", Type: "hostname"}},
		Bindings: map[string]model.Binding{"hostname": {Source: model.BindingAttr, Attr: "hostname"}},
		Assembly: map[string][]model.Instance{"分区": {}},
	}
}

// fullTemplate 是 sampleTemplate 再往插槽里放一个 hook 实例：实例覆盖了超时与是否必需，带一条取值来源，写死的值在向导里
// 可改（Editable，S5-5）。被引用的 hook 版本不必存在：草稿可以引用不存在的版本，激活时才报 hook_ref_missing。
func fullTemplate(name string) model.Template {
	t := sampleTemplate(name)
	sec, required := 300, false
	t.Assembly = map[string][]model.Instance{"分区": {{
		HookID: "lvm", HookVersion: 1, TimeoutSec: &sec, Required: &required,
		Bindings: map[string]model.Binding{"disk": {Source: model.BindingFixed, Value: "/dev/vda", Editable: true}},
	}}}
	return t
}

// templateOps 把 Templates 的方法接进共用用例。
var templateOps = versioned[model.Template]{
	kind:   "template",
	idents: pgstore.TemplateIdentsForTest,
	sample: sampleTemplate,
	full:   fullTemplate,
	head: func(t model.Template) head {
		return head{t.ID, t.Version, t.Name, t.State, t.Author, t.Approver, t.Builtin, t.Rev}
	},
	withHead: func(t model.Template, x head) model.Template {
		t.ID, t.Version, t.Name, t.State, t.Author, t.Approver, t.Builtin, t.Rev = x.ID, x.Version, x.Name, x.State, x.Author, x.Approver, x.Builtin, x.Rev
		return t
	},
	edit: func(t model.Template) model.Template {
		t.Body += "# 改过\n"
		return t
	},
	zero: func(t model.Template) model.Template {
		t.Family, t.Series, t.Body, t.Params, t.Bindings, t.Assembly = "", nil, "", nil, nil, nil
		return t
	},
	empty: func(t model.Template) model.Template {
		t.Series, t.Params, t.Bindings, t.Assembly = []catalog.Series{}, []model.ParamDecl{}, map[string]model.Binding{}, map[string][]model.Instance{}
		return t
	},
	variants: []variant[model.Template]{
		{"正文", func(t *model.Template) { t.Body += "# 改过\n" }},
		{"族", func(t *model.Template) { t.Family = "windows" }},
		{"系", func(t *model.Template) { t.Series = t.Series[:1] }},
		{"参数的显示名", func(t *model.Template) { t.Params[0].Label = "机器名" }},
		{"模板参数的取值来源", func(t *model.Template) {
			t.Bindings["hostname"] = model.Binding{Source: model.BindingAttr, Attr: "fqdn"}
		}},
		{"实例引用的 hook 版本", func(t *model.Template) { t.Assembly["分区"][0].HookVersion = 2 }},
		{"实例覆盖的超时", func(t *model.Template) { *t.Assembly["分区"][0].TimeoutSec = 600 }},
		{"实例的取值来源", func(t *model.Template) { // 引用的 hook 版本没变：只比引用关系查不出来
			t.Assembly["分区"][0].Bindings["disk"] = model.Binding{Source: model.BindingFixed, Value: "/dev/vdb", Editable: true}
		}},
		{"实例的取值来源可不可改", func(t *model.Template) {
			b := t.Assembly["分区"][0].Bindings["disk"]
			b.Editable = false
			t.Assembly["分区"][0].Bindings["disk"] = b
		}},
	},
	create: func(ctx context.Context, s *pgstore.Store, t model.Template) (model.Template, error) {
		return s.CreateTemplate(ctx, t)
	},
	save: func(ctx context.Context, s *pgstore.Store, t model.Template) (model.Template, error) {
		return s.SaveTemplateDraft(ctx, t)
	},
	newVersion: func(ctx context.Context, s *pgstore.Store, id string, from int, author string) (model.Template, error) {
		return s.NewTemplateVersion(ctx, id, from, author)
	},
	submit: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64, a repository.Audit) error {
		return s.SubmitTemplate(ctx, model.TemplateKey{ID: id, Version: v}, rev, a)
	},
	approve: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64, approver string, a repository.Audit) error {
		return s.ApproveTemplate(ctx, model.TemplateKey{ID: id, Version: v}, rev, approver, a)
	},
	reject: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64, a repository.Audit) error {
		return s.RejectTemplate(ctx, model.TemplateKey{ID: id, Version: v}, rev, a)
	},
	deprecate: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64, a repository.Audit) error {
		return s.DeprecateTemplate(ctx, model.TemplateKey{ID: id, Version: v}, rev, a)
	},
	rename: func(ctx context.Context, s *pgstore.Store, id, name string, a repository.Audit) error {
		return s.RenameTemplate(ctx, id, name, a)
	},
	deleteDraft: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64) error {
		return s.DeleteTemplateDraft(ctx, model.TemplateKey{ID: id, Version: v}, rev)
	},
	get: func(ctx context.Context, s *pgstore.Store, id string, v int) (model.Template, error) {
		return s.GetTemplate(ctx, model.TemplateKey{ID: id, Version: v})
	},
	versions: func(ctx context.Context, s *pgstore.Store, id string) ([]model.Template, error) {
		return s.ListTemplateVersions(ctx, id)
	},
	list: func(ctx context.Context, s *pgstore.Store, q listQuery) (repository.Page[model.Template], error) {
		return s.ListTemplates(ctx, repository.TemplateQuery{NamePrefix: q.NamePrefix, States: q.States, ApprovedOnly: q.ApprovedOnly, Cursor: q.Cursor, Limit: q.Limit})
	},
	ensureBuiltin: func(ctx context.Context, s *pgstore.Store, t model.Template) error {
		return s.EnsureBuiltinTemplate(ctx, t)
	},
}

// 规则同 hook：每一处一条 text_not_storable，Subject 的写法同激活判定。顺序是模板整体（正文、族与系）、参数声明、模板
// 参数的取值来源（按参数名），再是装配表：按插槽名，插槽名本身、其中的实例按顺序、实例的取值来源按参数名。
func templateUnstorable(t *testing.T, s *pgstore.Store) {
	draft := templateOps.mustCreateFrom(t, s, fullTemplate("Base-1"))
	before := authoredSnapshot(t, s)
	slot := func(id, path string) model.Problem { return notStorable("slot", id, path) }
	for _, c := range []struct {
		name string
		mod  func(t *model.Template)
		want func(id string) []model.Problem
	}{
		{"正文含 NUL", func(t *model.Template) { t.Body += "\x00" },
			func(id string) []model.Problem { return []model.Problem{notStorable("template", id, "")} }},
		{"系不是 UTF-8", func(t *model.Template) { t.Series = append(t.Series, "\xff") },
			func(id string) []model.Problem { return []model.Problem{notStorable("template", id, "")} }},
		{"族", func(t *model.Template) { t.Family = "linux\x00" },
			func(id string) []model.Problem { return []model.Problem{notStorable("template", id, "")} }},
		{"参数的默认值", func(t *model.Template) { t.Params[0].Default = "a\x00" },
			func(id string) []model.Problem { return []model.Problem{notStorable("template", id, "param/1")} }},
		{"模板参数的取值来源", func(t *model.Template) {
			t.Bindings["hostname"] = model.Binding{Source: model.BindingAttr, Attr: "\xff"}
		}, func(id string) []model.Problem {
			return []model.Problem{notStorable("template", id, model.TemplateFieldKey("hostname"))}
		}},
		{"模板参数取值来源的参数名", func(t *model.Template) {
			t.Bindings["host\xff"] = model.Binding{Source: model.BindingDefault}
		}, func(id string) []model.Problem {
			return []model.Problem{notStorable("template", id, model.TemplateFieldKey("host\xff"))}
		}},
		{"模板参数取值来源的 Source", func(t *model.Template) {
			t.Bindings["hostname"] = model.Binding{Source: "attr\x00", Attr: "hostname"}
		}, func(id string) []model.Problem {
			return []model.Problem{notStorable("template", id, model.TemplateFieldKey("hostname"))}
		}},
		{"插槽名", func(t *model.Template) { t.Assembly["装后\x00"] = nil },
			func(id string) []model.Problem { return []model.Problem{slot(id, "装后\x00")} }},
		{"实例引用的 hook ID", func(t *model.Template) { t.Assembly["分区"][0].HookID = "lvm\xff" },
			func(id string) []model.Problem { return []model.Problem{slot(id, model.InstancePath("分区", 1))} }},
		{"实例的取值来源", func(t *model.Template) {
			t.Assembly["分区"][0].Bindings["disk"] = model.Binding{Source: model.BindingFixed, Value: "/dev/\x00"}
		}, func(id string) []model.Problem {
			return []model.Problem{slot(id, model.InstanceFieldKey("分区", 1, "disk"))}
		}},
		{"实例取值来源的参数名", func(t *model.Template) {
			t.Assembly["分区"][0].Bindings["disk\x00"] = model.Binding{Source: model.BindingDefault}
		}, func(id string) []model.Problem {
			return []model.Problem{slot(id, model.InstanceFieldKey("分区", 1, "disk\x00"))}
		}},
		{"几处一起", func(t *model.Template) {
			t.Body, t.Params[0].Label = "\xff", "\x00"
			t.Assembly["分区"][0].Bindings["disk"] = model.Binding{Source: model.BindingFixed, Value: "\xff"}
		}, func(id string) []model.Problem {
			return []model.Problem{notStorable("template", id, ""), notStorable("template", id, "param/1"), slot(id, model.InstanceFieldKey("分区", 1, "disk"))}
		}},
	} {
		in := fullTemplate("Base-2")
		c.mod(&in)
		_, err := s.CreateTemplate(t.Context(), in)
		wantProblems(t, err, repository.ErrInvalid, c.want(""), "新建："+c.name)
		in = templateOps.mustGet(t, s, draft.ID, 1) // 每次重读一份：mod 改的是 map 与切片里的元素
		c.mod(&in)
		_, err = s.SaveTemplateDraft(t.Context(), in)
		wantProblems(t, err, repository.ErrInvalid, c.want(draft.ID+"@1"), "保存："+c.name)
	}
	in := fullTemplate("a b")
	in.Body = "\x00"
	want := []model.Problem{nameProblem(t, "a b", model.Subject{Kind: "template"}), notStorable("template", "", "")}
	_, err := s.CreateTemplate(t.Context(), in)
	wantProblems(t, err, repository.ErrInvalid, want, "名字不合法、正文也存不下")
	wantAuthoredUnchanged(t, s, before, "文本存不下时")
}

// DerivedFrom 记的是派生的来源（C24），只由 DeriveTemplate 写、由开新版本照抄：新建时带了它，等于没经派生就声称有来源；
// 保存时改了它，来源的提示与差异就对不上了。
func templateDerivedFromFixed(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	src := templateOps.mustCreate(t, s, "Src")
	srcKey := model.TemplateKey{ID: src.ID, Version: 1}
	plain := templateOps.mustCreate(t, s, "Plain")
	derived, err := s.DeriveTemplate(ctx, srcKey, "Derived", "carol")
	must(t, err, "派生")
	before := authoredSnapshot(t, s)
	in := sampleTemplate("Base-1")
	in.DerivedFrom = &srcKey
	_, err = s.CreateTemplate(ctx, in)
	wantIs(t, err, repository.ErrInvalid, "新建时带了 DerivedFrom")
	plain.DerivedFrom = &srcKey
	_, err = s.SaveTemplateDraft(ctx, plain)
	wantIs(t, err, repository.ErrInvalid, "给新建的模板补上 DerivedFrom")
	for _, from := range []*model.TemplateKey{nil, {ID: src.ID, Version: 2}, {ID: plain.ID, Version: 1}} {
		in := derived
		in.DerivedFrom = from
		_, err = s.SaveTemplateDraft(ctx, in)
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("把派生模板的 DerivedFrom 改成 %v", from))
	}
	wantAuthoredUnchanged(t, s, before, "改 DerivedFrom 时")
}

// 派生复制来源的族、系、正文、参数、取值来源与装配表，建一个新身份的版本 1 草稿，作者是给的那一个，DerivedFrom 记下
// 来源（C24）。来源是草稿、待审批、已审批、已废弃或内置的都可以；之后来源改了，派生出的不受影响。派生不审计。开新版本
// 时 DerivedFrom 照抄：来源出了新版本，派生模板的每个版本都要能比出差异。
func templateDerive(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	must(t, s.EnsureBuiltinTemplate(ctx, templateOps.with(fullTemplate("lober:KS"), func(h *head) {
		h.ID, h.Version, h.State, h.Author, h.Builtin = "builtin-ks", 1, model.VersionApproved, "lober", true
	})), "装载内置模板")
	lvm := hookOps.to(t, s, hookOps.mustCreate(t, s, "LVM"), model.VersionApproved) // 来源要审批，引用的 hook 就得是已审批的
	sources := []model.Template{templateOps.mustGet(t, s, "builtin-ks", 1)}
	for i, state := range []model.VersionState{model.VersionDraft, model.VersionReview, model.VersionApproved, model.VersionDeprecated} {
		src := templateOps.mustCreateFrom(t, s, referencing(fullTemplate(fmt.Sprintf("Src-%d", i)), lvm.Key()))
		sources = append(sources, templateOps.to(t, s, src, state))
	}
	audits := auditEntries(t, s)
	for i, src := range sources {
		from := model.TemplateKey{ID: src.ID, Version: src.Version}
		name := fmt.Sprintf("Derived-%d", i)
		got, err := s.DeriveTemplate(ctx, from, " "+name+" ", "carol")
		must(t, err, fmt.Sprintf("从 %s 的 %s 派生", src.State, src.Name))
		want := model.Template{
			ID: got.ID, Version: 1, Name: name, State: model.VersionDraft, Author: "carol", Rev: 1, DerivedFrom: &from,
			Family: src.Family, Series: src.Series, Body: src.Body, Params: src.Params, Bindings: src.Bindings, Assembly: src.Assembly,
		}
		if !model.ValidID(got.ID) || got.ID == src.ID {
			t.Fatalf("派生出的应是一个新身份，ID 合 model.ValidID，得到 %q", got.ID)
		}
		wantEqual(t, got, want, "派生的返回值")
		wantEqual(t, templateOps.mustGet(t, s, got.ID, 1), want, "读回来的")
	}
	wantItems(t, auditEntries(t, s), audits, "派生不写审计")

	// 来源之后改了，派生出的不受影响：内容在派生时已整份复制过来
	derived, err := s.DeriveTemplate(ctx, model.TemplateKey{ID: sources[1].ID, Version: 1}, "Derived-Again", "carol")
	must(t, err, "从草稿再派生一次")
	_, err = s.SaveTemplateDraft(ctx, templateOps.edit(sources[1]))
	must(t, err, "改来源草稿")
	wantEqual(t, templateOps.mustGet(t, s, derived.ID, 1), derived, "来源改了之后的派生模板")

	// 开新版本照抄 DerivedFrom；派生出的身份从版本 1 起编号，新版本是版本 2
	approved := templateOps.to(t, s, derived, model.VersionApproved)
	next := templateOps.newVersionOf(t, s, approved)
	wantEqual(t, next.DerivedFrom, approved.DerivedFrom, "新版本的 DerivedFrom")
	wantEqual(t, next.Version, 2, "派生出的模板开的新版本")
	// 装载的内置模板、派生出的与新开的版本都引用着 hook 版本，三条写入路径都要记下引用
	wantRefsRecorded(t, s, "装载内置模板、派生与开新版本之后")
}

// referencing 把 fullTemplate 里那个实例改成引用 k。
func referencing(t model.Template, k model.HookKey) model.Template {
	t.Assembly["分区"][0].HookID, t.Assembly["分区"][0].HookVersion = k.ID, k.Version
	return t
}

func templateDeriveBad(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	src := templateOps.mustCreate(t, s, "Src")
	from := model.TemplateKey{ID: src.ID, Version: 1}
	before := authoredSnapshot(t, s)
	for _, name := range []string{"src", " SRC "} {
		_, err := s.DeriveTemplate(ctx, from, name, "carol")
		wantDuplicate(t, err, "name", src.ID, fmt.Sprintf("派生时用了已有的名字 %q", name))
	}
	for _, name := range []string{"", "Derived 1", "派生"} {
		_, err := s.DeriveTemplate(ctx, from, name, "carol")
		wantProblems(t, err, repository.ErrInvalid, []model.Problem{nameProblem(t, name, model.Subject{Kind: "template"})}, fmt.Sprintf("派生时名字为 %q", name))
	}
	// 派生是用户的路径：用了保留前缀报 name_reserved，新身份还没有 ID（C12）
	for _, name := range reservedNames {
		_, err := s.DeriveTemplate(ctx, from, name, "carol")
		wantProblems(t, err, repository.ErrInvalid, []model.Problem{reservedProblem(t, name, model.Subject{Kind: "template"})},
			fmt.Sprintf("派生时名字用了保留前缀 %q", name))
	}
	for _, author := range []string{"", "\xff"} {
		_, err := s.DeriveTemplate(ctx, from, "Derived", author)
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("派生时作者为 %q", author))
	}
	_, err := s.DeriveTemplate(ctx, model.TemplateKey{ID: src.ID, Version: 2}, "Derived", "carol")
	wantIs(t, err, repository.ErrNotFound, "来源版本不存在")
	_, err = s.DeriveTemplate(ctx, model.TemplateKey{ID: "nosuchid", Version: 1}, "Derived", "carol")
	wantIs(t, err, repository.ErrNotFound, "来源身份不存在")
	for _, bad := range []model.TemplateKey{{ID: "Bad", Version: 1}, {ID: src.ID, Version: 0}} {
		_, err = s.DeriveTemplate(ctx, bad, "Derived", "carol")
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("来源的键 %v 不合格式", bad))
	}
	wantAuthoredUnchanged(t, s, before, "派生被拒时")
}

// 同 hook：审批在事务里对最新数据重跑激活判定与审批人的检查，问题与直接调用第 1 层得到的完全相同，被拒时状态不变。
func templateApproveRejected(t *testing.T, s *pgstore.Store) {
	in := sampleTemplate("Base-1")
	in.Body = strings.Replace(kickstartBody, "{{lober:cleanup}}\n", "", 1) // part_missing
	tpl := templateOps.to(t, s, templateOps.mustCreateFrom(t, s, in), model.VersionReview)
	before := authoredSnapshot(t, s)
	for _, c := range []struct {
		approver string
		codes    []model.Code
	}{
		{"bob", []model.Code{"part_missing"}},
		{"Alice", []model.Code{"part_missing", "approver_is_author"}},
	} {
		want := append(activation.Template(tpl, map[model.HookKey]model.Hook{}), approverProblems(tpl.Author, c.approver, model.Subject{Kind: "template", ID: tpl.ID + "@1"})...)
		wantCodes(t, want, c.codes)
		err := s.ApproveTemplate(t.Context(), model.TemplateKey{ID: tpl.ID, Version: 1}, tpl.Rev, c.approver, anAudit("template.approve"))
		wantProblems(t, err, repository.ErrRejected, want, "审批人 "+c.approver)
	}
	wantAuthoredUnchanged(t, s, before, "审批被拒时")
}

// 模板引用的是 hook 的具体版本（C23）。审批时在同一个事务里取出（pgstore 加读锁）这些版本，对它们的最新状态重跑判定：
// 模板提交之后、审批之前 hook 版本被废弃，批不了，报 hook_ref_deprecated（C26）；没审批的、不存在的同样拦下。
func templateApproveHookRefs(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	lvm := hookOps.to(t, s, hookOps.mustCreate(t, s, "LVM"), model.VersionApproved)
	draftHook := hookOps.mustCreate(t, s, "NIC")
	in := sampleTemplate("Base-1")
	in.Assembly = map[string][]model.Instance{"分区": {{HookID: lvm.ID, HookVersion: 1}}}
	tpl := templateOps.to(t, s, templateOps.mustCreateFrom(t, s, in), model.VersionReview)
	key := model.TemplateKey{ID: tpl.ID, Version: 1}
	must(t, s.DeprecateHook(ctx, lvm.Key(), lvm.Rev, anAudit("hook.deprecate")), "提交之后废弃引用的 hook")
	hooks := map[model.HookKey]model.Hook{lvm.Key(): hookOps.mustGet(t, s, lvm.ID, 1)}
	want := activation.Template(tpl, hooks)
	wantCodes(t, want, []model.Code{"hook_ref_deprecated"})
	before := authoredSnapshot(t, s)
	wantProblems(t, s.ApproveTemplate(ctx, key, tpl.Rev, "bob", anAudit("template.approve")), repository.ErrRejected, want, "引用的 hook 刚被废弃")
	wantAuthoredUnchanged(t, s, before, "审批被拒时")

	// 驳回，改成引用一个不存在的版本与一个草稿版本
	must(t, s.RejectTemplate(ctx, key, tpl.Rev, anAudit("template.reject")), "驳回")
	in = templateOps.mustGet(t, s, tpl.ID, 1)
	in.Assembly = map[string][]model.Instance{"分区": {{HookID: lvm.ID, HookVersion: 2}, {HookID: draftHook.ID, HookVersion: 1}}}
	in, err := s.SaveTemplateDraft(ctx, in)
	must(t, err, "改装配表")
	in = templateOps.to(t, s, in, model.VersionReview)
	want = activation.Template(in, map[model.HookKey]model.Hook{draftHook.Key(): draftHook})
	wantCodes(t, want, []model.Code{"hook_ref_missing", "hook_ref_unapproved"})
	wantProblems(t, s.ApproveTemplate(ctx, key, in.Rev, "bob", anAudit("template.approve")), repository.ErrRejected, want, "引用不存在与没审批的版本")

	// 引用的版本都审批了，模板就能批
	must(t, s.RejectTemplate(ctx, key, in.Rev, anAudit("template.reject")), "再驳回")
	nic := hookOps.to(t, s, draftHook, model.VersionApproved)
	in = templateOps.mustGet(t, s, tpl.ID, 1)
	in.Assembly = map[string][]model.Instance{"分区": {{HookID: nic.ID, HookVersion: 1}}}
	in, err = s.SaveTemplateDraft(ctx, in)
	must(t, err, "改成引用已审批的版本")
	if got := templateOps.to(t, s, in, model.VersionApproved); got.State != model.VersionApproved {
		t.Fatalf("引用的版本都已审批，模板应能批准，得到 %s", got.State)
	}
}

// 按系过滤只列勾了这个系的，看的是给出的那一项：默认是最新的版本，ApprovedOnly 时是当前的已审批版本。
func templateListSeries(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	withSeries := func(name string, series ...catalog.Series) model.Template {
		in := sampleTemplate(name)
		in.Series = series
		return templateOps.mustCreateFrom(t, s, in)
	}
	alma := withSeries("Alma-Only", "almalinux")
	both := withSeries("Both", "rocky", "almalinux")
	rocky := withSeries("Rocky-Only", "rocky")
	none := withSeries("None") // 草稿可以先不勾系
	// 版本 1 勾 rocky 并已审批，版本 2 的草稿改勾 almalinux
	moved1 := templateOps.to(t, s, withSeries("Moved", "rocky"), model.VersionApproved)
	moved2 := templateOps.newVersionOf(t, s, moved1)
	moved2.Series = []catalog.Series{"almalinux"}
	moved2, err := s.SaveTemplateDraft(ctx, moved2)
	must(t, err, "改勾的系")
	for _, c := range []struct {
		name string
		q    repository.TemplateQuery
		want []model.Template
	}{
		{"almalinux", repository.TemplateQuery{Series: "almalinux"}, []model.Template{alma, both, moved2}},
		{"rocky", repository.TemplateQuery{Series: "rocky"}, []model.Template{both, rocky}},
		{"不过滤", repository.TemplateQuery{}, []model.Template{alma, both, moved2, none, rocky}},
		{"没有模板勾的系", repository.TemplateQuery{Series: "debian"}, nil},
		{"不认识的系", repository.TemplateQuery{Series: "Rocky"}, nil},
		{"ApprovedOnly 时看当前的已审批版本", repository.TemplateQuery{Series: "rocky", ApprovedOnly: true}, []model.Template{moved1}},
		{"ApprovedOnly 时的 almalinux", repository.TemplateQuery{Series: "almalinux", ApprovedOnly: true}, nil},
		{"与名字前缀同时给", repository.TemplateQuery{Series: "rocky", NamePrefix: "ROCKY"}, []model.Template{rocky}},
		// 存不下的系谁都没勾（store 的包说明）：当作没有，得到空的一页，不报数据库的编码错误
		{"系含 NUL", repository.TemplateQuery{Series: "rocky\x00"}, nil},
		{"系不是 UTF-8", repository.TemplateQuery{Series: "rocky\xff"}, nil},
	} {
		c.q.Limit = 1 // 一条一页：翻页时过滤也要对
		got := slices.Concat(allPages(t, func(cursor string) (repository.Page[model.Template], error) {
			c.q.Cursor = cursor
			return s.ListTemplates(ctx, c.q)
		})...)
		wantItems(t, got, c.want, c.name)
	}
}

// 保存模板的任何一个版本时，仓储同时记下它引用了哪些 hook 版本（repository.HookRefs）。模板的任一版本引用了某个 hook 草稿，
// 包括模板的草稿与待审批的版本，这个草稿就删不掉（ErrReferenced，C26）；模板改掉装配表、或者模板草稿删了之后，就能删。
// 只引用了同一个 hook 的别的版本，不挡这个版本。派生出的版本 1 也是一次保存：它照抄来源的装配表，来源引用的 hook 草稿
// 它也引用。
func templateRefs(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	refTo := func(name string, ks ...model.HookKey) model.Template {
		in := sampleTemplate(name)
		for _, k := range ks {
			in.Assembly["分区"] = append(in.Assembly["分区"], model.Instance{HookID: k.ID, HookVersion: k.Version})
		}
		return templateOps.mustCreateFrom(t, s, in)
	}
	deleteHook := func(h model.Hook) error { return s.DeleteHookDraft(ctx, h.Key(), h.Rev) }

	lvm := hookOps.mustCreate(t, s, "LVM")
	user := refTo("Base-1", lvm.Key())
	before := authoredSnapshot(t, s)
	wantIs(t, deleteHook(lvm), repository.ErrReferenced, "被模板草稿引用")
	wantAuthoredUnchanged(t, s, before, "删除被拒时")
	user = templateOps.to(t, s, user, model.VersionReview)
	wantIs(t, deleteHook(lvm), repository.ErrReferenced, "被待审批的模板引用")
	must(t, s.RejectTemplate(ctx, model.TemplateKey{ID: user.ID, Version: 1}, user.Rev, anAudit("template.reject")), "驳回")
	user = templateOps.mustGet(t, s, user.ID, 1)
	user.Assembly = map[string][]model.Instance{"分区": {}}
	_, err := s.SaveTemplateDraft(ctx, user)
	must(t, err, "改掉装配表")
	must(t, deleteHook(lvm), "模板不再引用之后删除")

	// 模板的草稿删掉之后，它的引用也一起没了
	nic := hookOps.mustCreate(t, s, "NIC")
	other := refTo("Base-2", nic.Key())
	wantIs(t, deleteHook(nic), repository.ErrReferenced, "被另一个模板草稿引用")
	must(t, s.DeleteTemplateDraft(ctx, model.TemplateKey{ID: other.ID, Version: 1}, other.Rev), "删除模板草稿")
	must(t, deleteHook(nic), "模板草稿删掉之后删除")

	// 模板的版本 1 已审批、引用 disk@1，版本 2 的草稿引用 disk@2：disk@2 删不掉；只引用 disk@1 的模板不挡 disk@2
	disk1 := hookOps.to(t, s, hookOps.mustCreate(t, s, "Disk"), model.VersionApproved)
	disk2 := hookOps.newVersionOf(t, s, disk1)
	v1 := templateOps.to(t, s, refTo("Base-3", disk1.Key()), model.VersionApproved)
	refTo("Base-4", model.HookKey{ID: disk1.ID, Version: 1})
	v2 := templateOps.newVersionOf(t, s, v1)
	wantRefsRecorded(t, s, "开新版本之后") // 版本 2 照抄版本 1 的装配表，也引用 disk@1
	v2.Assembly = map[string][]model.Instance{"分区": {{HookID: disk2.ID, HookVersion: 2}}}
	v2, err = s.SaveTemplateDraft(ctx, v2)
	must(t, err, "模板版本 2 改成引用 disk@2")
	wantIs(t, deleteHook(disk2), repository.ErrReferenced, "被模板的版本 2 引用")
	must(t, s.DeleteTemplateDraft(ctx, model.TemplateKey{ID: v2.ID, Version: 2}, v2.Rev), "删除模板版本 2")
	must(t, deleteHook(disk2), "只剩引用 disk@1 的模板时删除 disk@2")

	// 从引用 raid 草稿的模板草稿派生：来源改掉装配表之后，派生出的草稿仍挡着 raid；它也改掉之后，raid 才删得掉
	raid := hookOps.mustCreate(t, s, "RAID")
	src := refTo("Base-5", raid.Key())
	derived, err := s.DeriveTemplate(ctx, model.TemplateKey{ID: src.ID, Version: 1}, "Derived-1", "carol")
	must(t, err, "从引用 raid 草稿的模板派生")
	src.Assembly = map[string][]model.Instance{"分区": {}}
	_, err = s.SaveTemplateDraft(ctx, src)
	must(t, err, "来源改掉装配表")
	wantIs(t, deleteHook(raid), repository.ErrReferenced, "来源不再引用，派生出的草稿仍引用")
	derived.Assembly = map[string][]model.Instance{"分区": {}}
	_, err = s.SaveTemplateDraft(ctx, derived)
	must(t, err, "派生出的草稿也改掉装配表")
	must(t, deleteHook(raid), "都不再引用之后删除")
}

// hookRef 是 template_hook_refs 的一行：模板版本 Template 引用了 hook 版本 Hook。
type hookRef struct {
	TemplateID      string
	TemplateVersion int
	HookID          string
	HookVersion     int
}

// wantRefsRecorded 直接查 template_hook_refs（接口看不到这张表）：它的行要恰好是库里每个模板版本按 repository.HookRefs 算出的
// 引用，只记 HookID 合 model.ValidID 的（见 refRows）。经接口只有 DeleteHookDraft 读这张表，它碰得到的只有 hook 草稿；开新
// 版本与装载内置模板记下的引用指向的都是已审批的 hook 版本，漏记了经接口查不出来（S5-8）。
func wantRefsRecorded(t *testing.T, s *pgstore.Store, what string) {
	t.Helper()
	var want []hookRef
	for _, tpl := range authoredSnapshot(t, s).Templates {
		for _, k := range repository.HookRefs(tpl) {
			if model.ValidID(k.ID) {
				want = append(want, hookRef{tpl.ID, tpl.Version, k.ID, k.Version})
			}
		}
	}
	var got []hookRef
	err := s.DBForTest().WithContext(t.Context()).Raw(`SELECT template_id, template_version, hook_id, hook_version FROM template_hook_refs`).
		Scan(&got).Error
	must(t, err, "读 template_hook_refs")
	byKey := func(a, b hookRef) int {
		return cmp.Or(strings.Compare(a.TemplateID, b.TemplateID), cmp.Compare(a.TemplateVersion, b.TemplateVersion),
			strings.Compare(a.HookID, b.HookID), cmp.Compare(a.HookVersion, b.HookVersion))
	}
	slices.SortFunc(want, byKey)
	slices.SortFunc(got, byKey)
	wantItems(t, got, want, what+"：template_hook_refs 的行与各模板版本的 HookRefs")
}

// hook 与模板各有自己的名字空间（各自的身份表上各有一个唯一索引）：同一个名字可以同时是一个 hook 和一个模板。
func templateNamespaces(t *testing.T, s *pgstore.Store) {
	h := hookOps.mustCreate(t, s, "Same-Name")
	tpl := templateOps.mustCreate(t, s, "same-name")
	other := hookOps.mustCreate(t, s, "Other")
	must(t, s.RenameHook(t.Context(), other.ID, "Tpl-Name", anAudit("hook.rename")), "hook 改名")
	must(t, s.RenameTemplate(t.Context(), tpl.ID, "TPL-NAME", anAudit("template.rename")), "模板改成与 hook 相同的名字")
	wantEqual(t, hookOps.names(hookOps.listed(t, s, listQuery{})), []string{"Same-Name", "Tpl-Name"}, "hook 的名字")
	wantEqual(t, templateOps.names(templateOps.listed(t, s, listQuery{})), []string{"TPL-NAME"}, "模板的名字")
	if h.ID == tpl.ID {
		t.Fatalf("hook 与模板得到同一个 ID %q", h.ID)
	}
}

// templateCopies：改了入参或返回值里的切片、map、指针与它们里面的元素，仓储里的数据都不变。
func templateCopies(t *testing.T, s *pgstore.Store) {
	mutate := func(tpl model.Template) {
		tpl.Series[0] = "ubuntu"
		tpl.Params[0].Label = "改了"
		tpl.Bindings["hostname"] = model.Binding{Source: model.BindingFixed, Value: "改了"}
		in := &tpl.Assembly["分区"][0]
		in.HookVersion = 99
		*in.TimeoutSec = 1
		*in.Required = true
		in.Bindings["disk"] = model.Binding{Source: model.BindingDefault}
		tpl.Assembly["多了"] = nil
		if tpl.DerivedFrom != nil {
			tpl.DerivedFrom.Version = 99
		}
	}
	ctx := t.Context()
	in := fullTemplate("Base-1")
	got, err := s.CreateTemplate(ctx, in)
	must(t, err, "新建")
	want := templateOps.withHead(fullTemplate("Base-1"), templateOps.head(got))
	mutate(in)
	mutate(got)
	wantEqual(t, templateOps.mustGet(t, s, want.ID, 1), want, "改了新建的入参与返回值之后")

	mutate(templateOps.mustGet(t, s, want.ID, 1))
	p, err := s.ListTemplates(ctx, repository.TemplateQuery{})
	must(t, err, "列出")
	mutate(p.Items[0])
	vs, err := s.ListTemplateVersions(ctx, want.ID)
	must(t, err, "列出版本")
	mutate(vs[0])
	wantEqual(t, templateOps.mustGet(t, s, want.ID, 1), want, "改了读到的返回值之后")

	in = templateOps.mustGet(t, s, want.ID, 1)
	in.Body = "第二稿"
	saved, err := s.SaveTemplateDraft(ctx, in)
	must(t, err, "保存")
	want = templateOps.mustGet(t, s, want.ID, 1)
	mutate(in)
	mutate(saved)
	wantEqual(t, templateOps.mustGet(t, s, want.ID, 1), want, "改了保存的入参与返回值之后")

	derived, err := s.DeriveTemplate(ctx, model.TemplateKey{ID: want.ID, Version: 1}, "Derived", "carol")
	must(t, err, "派生")
	wantDerived := templateOps.mustGet(t, s, derived.ID, 1)
	mutate(derived)
	wantEqual(t, templateOps.mustGet(t, s, derived.ID, 1), wantDerived, "改了派生的返回值之后")
	mutate(templateOps.mustGet(t, s, derived.ID, 1))
	wantEqual(t, templateOps.mustGet(t, s, derived.ID, 1), wantDerived, "改了读到的派生模板之后")
	wantEqual(t, templateOps.mustGet(t, s, want.ID, 1), want, "改了派生模板之后，来源")

	lvm := hookOps.to(t, s, hookOps.mustCreate(t, s, "LVM"), model.VersionApproved)
	approved := templateOps.to(t, s, templateOps.mustCreateFrom(t, s, referencing(fullTemplate("Base-2"), lvm.Key())), model.VersionApproved)
	next, err := s.NewTemplateVersion(ctx, approved.ID, 1, "carol")
	must(t, err, "开新版本")
	wantNext := templateOps.mustGet(t, s, next.ID, 2)
	mutate(next)
	wantEqual(t, templateOps.mustGet(t, s, next.ID, 2), wantNext, "改了新版本的返回值之后")
	wantEqual(t, templateOps.mustGet(t, s, approved.ID, 1), approved, "改了新版本的返回值之后，来源版本")

	b := templateOps.builtin("builtin-ks", 1, "lober:KS") // 内容同 fullTemplate，插槽里有实例
	must(t, s.EnsureBuiltinTemplate(ctx, b), "装载内置")
	wantBuiltin := templateOps.mustGet(t, s, b.ID, 1)
	mutate(b)
	wantEqual(t, templateOps.mustGet(t, s, b.ID, 1), wantBuiltin, "改了装载内置的入参之后")
}

// TEMPLATES-PLACEHOLDER
