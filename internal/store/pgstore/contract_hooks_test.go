package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/activation"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

// Test契约hook 是 hook 这一组（C8–C12、C22、C23、C25）：名字、版本的生命周期、内置内容与查询由 versioned 的共用用例查，
// 与模板那一组是同一份；这里另查 hook 自己的内容：文本存不下、审批时重跑激活判定、深拷贝。删除被模板引用的 hook
// 草稿要先建模板，留到模板那一组。
func Test契约hook(t *testing.T) {
	runCases(t, hookOps.cases())
	runCases(t, []testCase{
		{"说明、正文与别的文本存不下报 text_not_storable，问题落在那一处", hookUnstorable},
		{"审批：激活判定不通过报 ProblemsError(ErrRejected)，问题与直接调用 activation 的相同，状态不变", hookApproveRejected},
		{"返回值与入参都是拷贝：Params、Impls 是深拷贝", hookCopies},
		{"GetHooks 只放存在的，一个都没取到时是空 map", hookGetHooks},
		{"GetHooks 的键多于一批（batchSize）：分批查，第二批里的照样取到", hookGetHooksManyBatches},
	})
	runAudited(t, hookOps.auditedOps())
}

// sampleHook 是新建 hook 的入参：能通过激活判定，作者是 alice。两份实现一片段一脚本，三个参数各带选项、上下限与
// 默认值，深拷贝要顾到的每一层都有东西。
func sampleHook(name string) model.Hook {
	lo, hi := int64(1), int64(100)
	return model.Hook{
		Name: name, Description: "LVM 分区，系统盘可选", Author: "alice",
		Params: []model.ParamDecl{
			{Name: "disk", Label: "系统盘", Type: "diskpath", Default: "/dev/sda"},
			{Name: "size_gb", Label: "大小（GB）", Type: "int", Min: &lo, Max: &hi, Default: "20"},
			{Name: "fs", Label: "文件系统", Type: "enum", Options: []model.Option{{Value: "xfs", Label: "XFS"}, {Value: "ext4", Label: "ext4"}}, Default: "xfs"},
		},
		TimeoutSec: 600, Required: true,
		Impls: []model.Impl{
			{Family: "linux", Series: []catalog.Series{"rocky", "almalinux"}, Kind: model.ImplFragment,
				Body: "ignoredisk --only-use={{param:disk}}\nautopart --type=lvm\n"},
			{Family: "linux", Series: []catalog.Series{"debian"}, Kind: model.ImplScript, Body: "#!/bin/sh\necho done\n",
				Interpreter: "sh", Timings: []catalog.Timing{"post-target"}},
		},
	}
}

// fullHook 是 sampleHook 再加两个参数，参数声明的每个字段就都有不是零值的了（S5-5）：一个列表参数声明了必填，一个 enum
// 的选项取自 ISO 提供的集合 profiles。它照样能通过激活判定。sampleHook 不带这两个：建任务的夹具用它，Rocky 的 ISO
// 不提供 profiles，必填的参数又没有值，参数求值会报错。
func fullHook(name string) model.Hook {
	h := sampleHook(name)
	h.Params = append(h.Params,
		model.ParamDecl{Name: "pkgs", Label: "软件包", Type: "word", List: true, Required: true},
		model.ParamDecl{Name: "profile", Label: "安装类型", Type: "enum", Offer: "profiles"},
	)
	return h
}

// hookOps 把 Hooks 的方法接进共用用例。
var hookOps = versioned[model.Hook]{
	kind:   "hook",
	idents: pgstore.HookIdentsForTest,
	sample: sampleHook,
	full:   fullHook,
	head: func(h model.Hook) head {
		return head{h.ID, h.Version, h.Name, h.State, h.Author, h.Approver, h.Builtin, h.Rev}
	},
	withHead: func(h model.Hook, x head) model.Hook {
		h.ID, h.Version, h.Name, h.State, h.Author, h.Approver, h.Builtin, h.Rev = x.ID, x.Version, x.Name, x.State, x.Author, x.Approver, x.Builtin, x.Rev
		return h
	},
	edit: func(h model.Hook) model.Hook {
		h.Description += "（改过）"
		return h
	},
	zero: func(h model.Hook) model.Hook {
		h.Description, h.Params, h.TimeoutSec, h.Required, h.Impls = "", nil, 0, false, nil
		return h
	},
	empty: func(h model.Hook) model.Hook {
		h.Params, h.Impls = []model.ParamDecl{}, []model.Impl{}
		return h
	},
	variants: []variant[model.Hook]{
		{"说明", func(h *model.Hook) { h.Description += "（改过）" }},
		{"参数的默认值", func(h *model.Hook) { h.Params[2].Default = "ext4" }},
		{"参数是否列表", func(h *model.Hook) { h.Params[3].List = false }},
		{"参数是否必填", func(h *model.Hook) { h.Params[3].Required = false }},
		{"参数的选项集合", func(h *model.Hook) { h.Params[4].Offer = "editions" }},
		{"超时", func(h *model.Hook) { h.TimeoutSec = 900 }},
		{"是否必需", func(h *model.Hook) { h.Required = false }},
		{"实现的正文", func(h *model.Hook) { h.Impls[0].Body += "clearpart --all\n" }},
		{"实现的时机", func(h *model.Hook) { h.Impls[1].Timings = []catalog.Timing{"firstboot"} }},
	},
	create: func(ctx context.Context, s *pgstore.Store, h model.Hook) (model.Hook, error) {
		return s.CreateHook(ctx, h)
	},
	save: func(ctx context.Context, s *pgstore.Store, h model.Hook) (model.Hook, error) {
		return s.SaveHookDraft(ctx, h)
	},
	newVersion: func(ctx context.Context, s *pgstore.Store, id string, from int, author string) (model.Hook, error) {
		return s.NewHookVersion(ctx, id, from, author)
	},
	submit: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64, a store.Audit) error {
		return s.SubmitHook(ctx, model.HookKey{ID: id, Version: v}, rev, a)
	},
	approve: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64, approver string, a store.Audit) error {
		return s.ApproveHook(ctx, model.HookKey{ID: id, Version: v}, rev, approver, a)
	},
	reject: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64, a store.Audit) error {
		return s.RejectHook(ctx, model.HookKey{ID: id, Version: v}, rev, a)
	},
	deprecate: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64, a store.Audit) error {
		return s.DeprecateHook(ctx, model.HookKey{ID: id, Version: v}, rev, a)
	},
	rename: func(ctx context.Context, s *pgstore.Store, id, name string, a store.Audit) error {
		return s.RenameHook(ctx, id, name, a)
	},
	deleteDraft: func(ctx context.Context, s *pgstore.Store, id string, v int, rev int64) error {
		return s.DeleteHookDraft(ctx, model.HookKey{ID: id, Version: v}, rev)
	},
	get: func(ctx context.Context, s *pgstore.Store, id string, v int) (model.Hook, error) {
		return s.GetHook(ctx, model.HookKey{ID: id, Version: v})
	},
	versions: func(ctx context.Context, s *pgstore.Store, id string) ([]model.Hook, error) {
		return s.ListHookVersions(ctx, id)
	},
	list: func(ctx context.Context, s *pgstore.Store, q listQuery) (store.Page[model.Hook], error) {
		return s.ListHooks(ctx, store.HookQuery{NamePrefix: q.NamePrefix, States: q.States, ApprovedOnly: q.ApprovedOnly, Cursor: q.Cursor, Limit: q.Limit})
	},
	ensureBuiltin: func(ctx context.Context, s *pgstore.Store, h model.Hook) error { return s.EnsureBuiltinHook(ctx, h) },
}

// 能存成 UTF-8 文本的草稿都能存（spec §3 总则），存不下的新建与保存都拒收：每一处一条 text_not_storable，按 hook 整体、
// 参数声明、实现的顺序，Subject 的写法同激活判定。新建时还没有 ID，Subject 的 ID 为空；保存时是「ID@版本」。
func hookUnstorable(t *testing.T, s *pgstore.Store) {
	draft := hookOps.mustCreate(t, s, "Base-1")
	before := authoredSnapshot(t, s)
	for _, c := range []struct {
		name string
		mod  func(h *model.Hook)
		want func(id string) []model.Problem
	}{
		{"说明含 NUL", func(h *model.Hook) { h.Description = "a\x00b" },
			func(id string) []model.Problem { return []model.Problem{notStorable("hook", id, "")} }},
		{"第 2 份实现的正文不是 UTF-8", func(h *model.Hook) { h.Impls[1].Body = "echo \xff\n" },
			func(id string) []model.Problem { return []model.Problem{notStorable("impl", id, "impl/2")} }},
		{"参数的显示名", func(h *model.Hook) { h.Params[1].Label = "大小\x00" },
			func(id string) []model.Problem { return []model.Problem{notStorable("hook", id, "param/2")} }},
		{"选项的显示名", func(h *model.Hook) { h.Params[2].Options[1].Label = "\xff" },
			func(id string) []model.Problem { return []model.Problem{notStorable("hook", id, "param/3")} }},
		{"参数名", func(h *model.Hook) { h.Params[0].Name = "disk\x00" },
			func(id string) []model.Problem { return []model.Problem{notStorable("hook", id, "param/1")} }},
		{"参数的类型", func(h *model.Hook) { h.Params[1].Type = "int\xff" },
			func(id string) []model.Problem { return []model.Problem{notStorable("hook", id, "param/2")} }},
		{"参数的 Offer", func(h *model.Hook) { h.Params[2].Offer = "editions\x00" },
			func(id string) []model.Problem { return []model.Problem{notStorable("hook", id, "param/3")} }},
		{"选项的值", func(h *model.Hook) { h.Params[2].Options[0].Value = "xfs\xff" },
			func(id string) []model.Problem { return []model.Problem{notStorable("hook", id, "param/3")} }},
		{"实现的系与时机", func(h *model.Hook) {
			h.Impls[0].Series = append(h.Impls[0].Series, "\xff")
			h.Impls[1].Timings = []catalog.Timing{"post\x00"}
		}, func(id string) []model.Problem {
			return []model.Problem{notStorable("impl", id, "impl/1"), notStorable("impl", id, "impl/2")}
		}},
		{"实现的族", func(h *model.Hook) { h.Impls[0].Family = "linux\x00" },
			func(id string) []model.Problem { return []model.Problem{notStorable("impl", id, "impl/1")} }},
		{"实现的种类", func(h *model.Hook) { h.Impls[1].Kind = "script\xff" },
			func(id string) []model.Problem { return []model.Problem{notStorable("impl", id, "impl/2")} }},
		{"实现的解释器", func(h *model.Hook) { h.Impls[1].Interpreter = "sh\x00" },
			func(id string) []model.Problem { return []model.Problem{notStorable("impl", id, "impl/2")} }},
		{"几处一起", func(h *model.Hook) { h.Description, h.Params[0].Default, h.Impls[0].Body = "\xff", "\x00", "\x00" },
			func(id string) []model.Problem {
				return []model.Problem{notStorable("hook", id, ""), notStorable("hook", id, "param/1"), notStorable("impl", id, "impl/1")}
			}},
	} {
		in := sampleHook("Base-2")
		c.mod(&in)
		_, err := s.CreateHook(t.Context(), in)
		wantProblems(t, err, store.ErrInvalid, c.want(""), "新建："+c.name)
		in = hookOps.mustGet(t, s, draft.ID, 1) // 每次重读一份：mod 改的是切片里的元素
		c.mod(&in)
		_, err = s.SaveHookDraft(t.Context(), in)
		wantProblems(t, err, store.ErrInvalid, c.want(draft.ID+"@1"), "保存："+c.name)
	}
	// 名字不合法与文本存不下一起时，名字的问题在前，顺序同激活判定
	in := sampleHook("a b")
	in.Impls[1].Body = "\xff"
	want := []model.Problem{nameProblem(t, "a b", model.Subject{Kind: "hook"}), notStorable("impl", "", "impl/2")}
	_, err := s.CreateHook(t.Context(), in)
	wantProblems(t, err, store.ErrInvalid, want, "名字不合法、正文也存不下")
	wantAuthoredUnchanged(t, s, before, "文本存不下时")
}

// 审批在事务里对加锁后读到的版本重跑激活判定与审批人的检查，问题与直接调用第 1 层得到的完全相同：先激活判定，
// 后审批人。被拒时状态与 Rev 都不变，改好之后同一个版本照样能批。
func hookApproveRejected(t *testing.T, s *pgstore.Store) {
	in := sampleHook("Base-1")
	in.Impls = nil               // hook_no_impl
	in.Params[0].Default = "sda" // diskpath 的默认值要以 /dev/ 开头：param_decl_invalid
	h := hookOps.to(t, s, hookOps.mustCreateFrom(t, s, in), model.VersionReview)
	before := authoredSnapshot(t, s)
	for _, c := range []struct {
		approver string
		codes    []model.Code // 用来确认反例造对了
	}{
		{"bob", []model.Code{"param_decl_invalid", "hook_no_impl"}},
		{"Alice", []model.Code{"param_decl_invalid", "hook_no_impl", "approver_is_author"}},
	} {
		want := append(activation.Hook(h), approverProblems(h.Author, c.approver, model.Subject{Kind: "hook", ID: h.ID + "@1"})...)
		wantCodes(t, want, c.codes)
		err := s.ApproveHook(t.Context(), h.Key(), h.Rev, c.approver, anAudit("hook.approve"))
		wantProblems(t, err, store.ErrRejected, want, "审批人 "+c.approver)
	}
	wantAuthoredUnchanged(t, s, before, "审批被拒时")

	must(t, s.RejectHook(t.Context(), h.Key(), h.Rev, anAudit("hook.reject")), "驳回")
	fixed := hookOps.withHead(sampleHook(h.Name), hookOps.head(hookOps.mustGet(t, s, h.ID, 1)))
	fixed, err := s.SaveHookDraft(t.Context(), fixed)
	must(t, err, "改回能通过的内容")
	approved := hookOps.to(t, s, fixed, model.VersionApproved)
	if approved.State != model.VersionApproved || approved.Approver != "bob" {
		t.Fatalf("改好之后应能批准，审批人为 bob，得到 %s、%q", approved.State, approved.Approver)
	}
}

// hookCopies：改了入参或返回值里的切片、指针与它们里面的元素，仓储里的数据都不变（包说明里的约定）。
func hookCopies(t *testing.T, s *pgstore.Store) {
	mutate := func(h model.Hook) {
		h.Params[0].Label = "改了"
		*h.Params[1].Min = 99
		h.Params[2].Options[0].Label = "改了"
		h.Impls[0].Series[0] = "ubuntu"
		h.Impls[1].Timings[0] = "pre"
		h.Impls[1].Body = "改了"
	}
	ctx := t.Context()
	in := fullHook("Base-1")
	got, err := s.CreateHook(ctx, in)
	must(t, err, "新建")
	want := hookOps.withHead(fullHook("Base-1"), hookOps.head(got))
	mutate(in)
	mutate(got)
	wantEqual(t, hookOps.mustGet(t, s, want.ID, 1), want, "改了新建的入参与返回值之后")

	mutate(hookOps.mustGet(t, s, want.ID, 1))
	m, err := s.GetHooks(ctx, []model.HookKey{want.Key()})
	must(t, err, "批量取")
	mutate(m[want.Key()])
	p, err := s.ListHooks(ctx, store.HookQuery{})
	must(t, err, "列出")
	mutate(p.Items[0])
	vs, err := s.ListHookVersions(ctx, want.ID)
	must(t, err, "列出版本")
	mutate(vs[0])
	wantEqual(t, hookOps.mustGet(t, s, want.ID, 1), want, "改了读到的返回值之后")

	in = hookOps.mustGet(t, s, want.ID, 1)
	in.Description = "第二稿"
	saved, err := s.SaveHookDraft(ctx, in)
	must(t, err, "保存")
	want = hookOps.mustGet(t, s, want.ID, 1)
	mutate(in)
	mutate(saved)
	wantEqual(t, hookOps.mustGet(t, s, want.ID, 1), want, "改了保存的入参与返回值之后")

	approved := hookOps.to(t, s, want, model.VersionApproved)
	next, err := s.NewHookVersion(ctx, approved.ID, 1, "carol")
	must(t, err, "开新版本")
	want = hookOps.mustGet(t, s, next.ID, 2)
	mutate(next)
	wantEqual(t, hookOps.mustGet(t, s, next.ID, 2), want, "改了新版本的返回值之后")
	wantEqual(t, hookOps.mustGet(t, s, approved.ID, 1), approved, "改了新版本的返回值之后，来源版本")

	b := hookOps.builtin("builtin-lvm", 1, "lober:LVM")
	must(t, s.EnsureBuiltinHook(ctx, b), "装载内置")
	want = hookOps.mustGet(t, s, b.ID, 1)
	mutate(b)
	wantEqual(t, hookOps.mustGet(t, s, want.ID, 1), want, "改了装载内置的入参之后")
}

func hookGetHooks(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	a := hookOps.mustCreate(t, s, "A")
	b := hookOps.to(t, s, hookOps.mustCreate(t, s, "B"), model.VersionApproved)
	b2, err := s.NewHookVersion(ctx, b.ID, 1, "carol")
	must(t, err, "开新版本")
	got, err := s.GetHooks(ctx, []model.HookKey{a.Key(), {ID: a.ID, Version: 2}, b.Key(), b2.Key(), {ID: "nosuchhook", Version: 1}, a.Key()})
	must(t, err, "批量取")
	wantEqual(t, got, map[model.HookKey]model.Hook{a.Key(): a, b.Key(): b, b2.Key(): b2}, "批量取的结果")
	for _, ks := range [][]model.HookKey{nil, {}, {{ID: "nosuchhook", Version: 1}}} {
		m, err := s.GetHooks(ctx, ks)
		must(t, err, fmt.Sprintf("批量取 %v", ks))
		if m == nil || len(m) != 0 {
			t.Fatalf("批量取 %v：应得到空 map，不是 nil；得到 %#v", ks, m)
		}
	}
	for _, k := range []model.HookKey{{ID: "Bad", Version: 1}, {ID: a.ID, Version: 0}} {
		_, err := s.GetHooks(ctx, []model.HookKey{a.Key(), k})
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("批量取的键 %v 不合格式", k))
	}
}

// 批量取的键数不设上限，多于一条语句查得下的一批（batchSize）时按入参的次序分批查，两批取到的合在一起：a 是第一个键，
// b 是第 batchSize+1 个，落在第二批，中间是 a 不存在的版本。
func hookGetHooksManyBatches(t *testing.T, s *pgstore.Store) {
	a := hookOps.mustCreate(t, s, "A")
	b := hookOps.mustCreate(t, s, "B")
	ks := []model.HookKey{a.Key()}
	for v := 2; len(ks) < pgstore.BatchSizeForTest; v++ {
		ks = append(ks, model.HookKey{ID: a.ID, Version: v}) // a 只有版本 1
	}
	ks = append(ks, b.Key())
	got, err := s.GetHooks(t.Context(), ks)
	must(t, err, fmt.Sprintf("批量取 %d 个键", len(ks)))
	wantEqual(t, got, map[model.HookKey]model.Hook{a.Key(): a, b.Key(): b}, "批量取的结果")
}

// 下面是 hook 与模板共用的部分。仓储契约里 Templates 与 Hooks 一一对应，名字、生命周期、内置内容与查询的规则相同，
// 用例也只写一份，经 versioned 分别跑在两边：规则写两遍就会走偏，用例也一样。

// head 是 hook 与模板版本里由仓储管的那几个字段，两边同名同义。
type head struct {
	ID       string
	Version  int
	Name     string
	State    model.VersionState
	Author   string
	Approver string
	Builtin  bool
	Rev      int64
}

// listQuery 是 HookQuery 与 TemplateQuery 共有的部分。
type listQuery struct {
	NamePrefix   string
	States       []model.VersionState
	ApprovedOnly bool
	Cursor       string
	Limit        int
}

// versioned 把 hook 或模板的方法接成同一组操作，V 是 model.Hook 或 model.Template。
type versioned[V any] struct {
	kind   string // hook 或 template：Subject 的 Kind，也是审计对象的 TargetKind
	idents string // 身份表：分配过的最大版本号 last_version 记在这里，经接口看不到（lastVersion）
	// sample 是新建的入参：内容齐全、能通过激活判定，不带 ID 与版本，作者是 alice。full 同 sample，再把 sample 为了能
	// 审批、能建任务而空着的地方填上（模板插槽里的实例，引用的 hook 不必存在；hook 里必填的列表参数与选项取自 ISO 的
	// enum），内容的每一层、每个字段都有东西：装载内置内容不跑激活判定，内置的入参用它。
	sample, full func(name string) V
	head         func(v V) head
	withHead     func(v V, h head) V
	// edit 改一处内容；zero 把内容都改成零值，集合为 nil；empty 把集合都改成空的，不是 nil。
	edit, zero, empty func(v V) V
	// variants 各改 full 内容里的一处，每个内容字段一个，改在嵌套的最里层：比内容时漏了哪个字段、或者只比到外层，都有
	// 一个查得出来（builtinEnsure）。
	variants []variant[V]

	create        func(ctx context.Context, s *pgstore.Store, v V) (V, error)
	save          func(ctx context.Context, s *pgstore.Store, v V) (V, error)
	newVersion    func(ctx context.Context, s *pgstore.Store, id string, from int, author string) (V, error)
	submit        func(ctx context.Context, s *pgstore.Store, id string, version int, rev int64, a store.Audit) error
	approve       func(ctx context.Context, s *pgstore.Store, id string, version int, rev int64, approver string, a store.Audit) error
	reject        func(ctx context.Context, s *pgstore.Store, id string, version int, rev int64, a store.Audit) error
	deprecate     func(ctx context.Context, s *pgstore.Store, id string, version int, rev int64, a store.Audit) error
	rename        func(ctx context.Context, s *pgstore.Store, id, name string, a store.Audit) error
	deleteDraft   func(ctx context.Context, s *pgstore.Store, id string, version int, rev int64) error
	get           func(ctx context.Context, s *pgstore.Store, id string, version int) (V, error)
	versions      func(ctx context.Context, s *pgstore.Store, id string) ([]V, error)
	list          func(ctx context.Context, s *pgstore.Store, q listQuery) (store.Page[V], error)
	ensureBuiltin func(ctx context.Context, s *pgstore.Store, v V) error
}

// variant 是内容里的一处改动：what 说明改的是哪里，mod 就地改 v，连切片与 map 里的元素一起改，所以每次要给它新造的
// 一份。
type variant[V any] struct {
	what string
	mod  func(v *V)
}

// 下面是用例里常用的几步，出错就让用例失败。

func (k versioned[V]) mustCreateFrom(t *testing.T, s *pgstore.Store, v V) V {
	t.Helper()
	got, err := k.create(t.Context(), s, v)
	must(t, err, fmt.Sprintf("新建 %s %q", k.kind, k.head(v).Name))
	return got
}

func (k versioned[V]) mustCreate(t *testing.T, s *pgstore.Store, name string) V {
	t.Helper()
	return k.mustCreateFrom(t, s, k.sample(name))
}

func (k versioned[V]) mustGet(t *testing.T, s *pgstore.Store, id string, version int) V {
	t.Helper()
	got, err := k.get(t.Context(), s, id, version)
	must(t, err, fmt.Sprintf("读 %s %s@%d", k.kind, id, version))
	return got
}

func (k versioned[V]) mustVersions(t *testing.T, s *pgstore.Store, id string) []V {
	t.Helper()
	got, err := k.versions(t.Context(), s, id)
	must(t, err, fmt.Sprintf("列出 %s %s 的版本", k.kind, id))
	return got
}

// with 返回改了 head 的 v。
func (k versioned[V]) with(v V, mod func(h *head)) V {
	h := k.head(v)
	mod(&h)
	return k.withHead(v, h)
}

// to 把版本 v 沿 draft → review → approved → deprecated 推到 state，审批人是 bob，返回重读的版本。
func (k versioned[V]) to(t *testing.T, s *pgstore.Store, v V, state model.VersionState) V {
	t.Helper()
	ctx := t.Context()
	for {
		h := k.head(v)
		var err error
		switch {
		case h.State == state:
			return v
		case h.State == model.VersionDraft:
			err = k.submit(ctx, s, h.ID, h.Version, h.Rev, anAudit(k.kind+".submit"))
		case h.State == model.VersionReview:
			err = k.approve(ctx, s, h.ID, h.Version, h.Rev, "bob", anAudit(k.kind+".approve"))
		case h.State == model.VersionApproved:
			err = k.deprecate(ctx, s, h.ID, h.Version, h.Rev, anAudit(k.kind+".deprecate"))
		default:
			t.Fatalf("%s %s@%d 已是 %s，推不到 %s", k.kind, h.ID, h.Version, h.State, state)
		}
		must(t, err, fmt.Sprintf("把 %s %s@%d 从 %s 往 %s 推", k.kind, h.ID, h.Version, h.State, state))
		v = k.mustGet(t, s, h.ID, h.Version)
	}
}

// builtin 是装载内置版本的入参：内容同 full，已审批，作者是 lober，没有审批人（出厂即已审批，C25）。
func (k versioned[V]) builtin(id string, version int, name string) V {
	return k.withHead(k.full(name), head{ID: id, Version: version, Name: name, State: model.VersionApproved, Author: "lober", Builtin: true})
}

// currentApproved 从一个身份的版本 vs 里取当前的已审批版本。规则借 model.CurrentApproved：只拿版本号与状态问它，
// 规则只有那一份。
func (k versioned[V]) currentApproved(vs []V) (V, bool) {
	ts := make([]model.Template, len(vs))
	for i, v := range vs {
		h := k.head(v)
		ts[i] = model.Template{Version: h.Version, State: h.State}
	}
	if cur, ok := model.CurrentApproved(ts); ok {
		for _, v := range vs {
			if k.head(v).Version == cur.Version {
				return v, true
			}
		}
	}
	var none V
	return none, false
}

// pages 用查询 q 从头翻到尾，返回每一页；游标由它填。
func (k versioned[V]) pages(t *testing.T, s *pgstore.Store, q listQuery) [][]V {
	t.Helper()
	return allPages(t, func(cursor string) (store.Page[V], error) {
		q.Cursor = cursor
		return k.list(t.Context(), s, q)
	})
}

// listed 是按查询 q 翻完的全部条目，每页一条：翻页时过滤也要对。
func (k versioned[V]) listed(t *testing.T, s *pgstore.Store, q listQuery) []V {
	t.Helper()
	q.Limit = 1
	return slices.Concat(k.pages(t, s, q)...)
}

// names 是 vs 的名字，按 vs 的顺序。
func (k versioned[V]) names(vs []V) []string {
	var out []string
	for _, v := range vs {
		out = append(out, k.head(v).Name)
	}
	return out
}

// authored 是经接口看得见的全部 hook 与模板版本，以及审计，用来断言一次被拒绝或失败的写入什么都没改。hook 与模板
// 两组都看两边：模板那一组的写入也可能动到 hook。主机与 ISO 那几组的写入碰不到 hook 与模板，用的是 contents。
type authored struct {
	Hooks     []model.Hook
	Templates []model.Template
	Audit     []store.AuditEntry
}

func authoredSnapshot(t *testing.T, s *pgstore.Store) authored {
	t.Helper()
	var a authored
	for _, h := range slices.Concat(hookOps.pages(t, s, listQuery{Limit: 500})...) {
		a.Hooks = append(a.Hooks, hookOps.mustVersions(t, s, h.ID)...)
	}
	for _, tpl := range slices.Concat(templateOps.pages(t, s, listQuery{Limit: 500})...) {
		a.Templates = append(a.Templates, templateOps.mustVersions(t, s, tpl.ID)...)
	}
	a.Audit = auditEntries(t, s)
	return a
}

// wantAuthoredUnchanged 要求 hook、模板与审计都与 before 相同：动作没有落库，审计也没有写。
func wantAuthoredUnchanged(t *testing.T, s *pgstore.Store, before authored, what string) {
	t.Helper()
	if after := authoredSnapshot(t, s); !reflect.DeepEqual(after, before) {
		t.Fatalf("%s仓储不应有变化：\n之前 %+v\n之后 %+v", what, before, after)
	}
}

// notStorable 是一处文本存不下（text_not_storable），Subject 落在那一处。
func notStorable(kind, id, path string) model.Problem {
	return model.NewProblem("text_not_storable", model.Subject{Kind: kind, ID: id, Path: path})
}

// nameProblem 是 model.NormalizeName 对 name 报的问题，Subject 换成 subj：NormalizeName 把它留给调用方填。
func nameProblem(t *testing.T, name string, subj model.Subject) model.Problem {
	t.Helper()
	_, _, err := model.NormalizeName(name)
	var p model.Problem
	if !errors.As(err, &p) {
		t.Fatalf("用例的名字 %q 应当不合法，NormalizeName 得到 %v", name, err)
	}
	p.Subject = subj
	return p
}

// reservedNames 是用了保留前缀的名字（spec C12），用户新建、派生、改名时都要报 name_reserved。按 key 比，大小写不同的
// 写法也算；首尾的空白先去掉。
var reservedNames = []string{"lober:Base", " LOBER:base ", "Lober:"}

// reservedProblem 是用户的入口对名字 name 报的 name_reserved：参数是规范的写法，Subject 为 subj。
func reservedProblem(t *testing.T, name string, subj model.Subject) model.Problem {
	t.Helper()
	display, key, err := model.NormalizeName(name)
	if err != nil || !model.NameReserved(key) {
		t.Fatalf("用例的名字 %q 应当合名字规则并用了保留前缀，得到 %q、%v", name, key, err)
	}
	return model.NewProblem("name_reserved", subj, display)
}

// approverProblems 是直接调用 activation.Approver 得到的问题，Subject 填成审批的那个版本：Approver 把它留给调用方填。
func approverProblems(author, approver string, subj model.Subject) []model.Problem {
	ps := activation.Approver(author, approver)
	for i := range ps {
		ps[i].Subject = subj
	}
	return ps
}

// wantCodes 要求 ps 的代码依次是 codes：用来确认反例造对了，否则「问题相同」可能两边都是空的。
func wantCodes(t *testing.T, ps []model.Problem, codes []model.Code) {
	t.Helper()
	got := make([]model.Code, len(ps))
	for i, p := range ps {
		got[i] = p.Code
	}
	if !slices.Equal(got, codes) {
		t.Fatalf("反例没造对：问题的代码是 %v，应为 %v", got, codes)
	}
}

// auditedOp 是一个要审计的方法：prepare 备好记录，返回调用它的函数，规则同审计那一组的 auditedOps。
type auditedOp struct {
	name    string
	prepare func(t *testing.T, s *pgstore.Store) auditedCall
}

// runAudited 查 ops 与审计写在同一个事务里（spec §6.2 第 8 条）。四项检查与审计那一组相同，只是快照换成 hook、模板与
// 审计：审计那一组只覆盖主机与 ISO，它的快照（contents）不看 hook 与模板。四组与组里的各个方法都并行跑，同 eachAudited。
func runAudited(t *testing.T, ops []auditedOp) {
	each := func(t *testing.T, check func(t *testing.T, s *pgstore.Store, call auditedCall)) {
		for _, op := range ops {
			t.Run(op.name, func(t *testing.T) {
				t.Parallel()
				s := pgtest.OpenStore(t)
				check(t, s, op.prepare(t, s))
			})
		}
	}
	t.Run("零值审计报 ErrAuditRequired，什么都不写", func(t *testing.T) {
		t.Parallel()
		each(t, func(t *testing.T, s *pgstore.Store, call auditedCall) {
			before := authoredSnapshot(t, s)
			_, err := call(store.Audit{})
			wantIs(t, err, store.ErrAuditRequired, "零值审计")
			wantAuthoredUnchanged(t, s, before, "零值审计时")
			_, err = call(anAudit("test.retry"))
			must(t, err, "补上审计之后")
		})
	})
	t.Run("审计不合法报 ErrInvalid，什么都不写", func(t *testing.T) {
		t.Parallel()
		each(t, func(t *testing.T, s *pgstore.Store, call auditedCall) {
			before := authoredSnapshot(t, s)
			for _, a := range []store.Audit{{Actor: "alice", Action: "approve", At: at0}, {Action: "hook.approve", At: at0}, {Actor: "alice", Action: "hook.approve"}} {
				_, err := call(a)
				wantIs(t, err, store.ErrInvalid, fmt.Sprintf("审计 %+v", a))
			}
			wantAuthoredUnchanged(t, s, before, "审计不合法时")
			_, err := call(anAudit("test.retry"))
			must(t, err, "改对审计之后")
		})
	})
	t.Run("审计写失败时动作也不落库", func(t *testing.T) {
		t.Parallel()
		each(t, func(t *testing.T, s *pgstore.Store, call auditedCall) {
			before := authoredSnapshot(t, s)
			_, err := call(store.Audit{Actor: pgtest.FaultActor, Action: "test.fault", At: at0})
			if err == nil {
				t.Fatal("审计写失败时应报错")
			}
			for _, sentinel := range sentinels { // 失败要来自审计的写入，而不是动作之前的检查
				if errors.Is(err, sentinel) {
					t.Fatalf("审计写失败时报了 %v：失败应来自审计的写入", err)
				}
			}
			wantAuthoredUnchanged(t, s, before, "审计写失败时")
			_, err = call(anAudit("test.retry")) // 同样的调用换一个操作者就成功：状态与 Rev 都没动
			must(t, err, "换一个操作者之后")
		})
	})
	t.Run("成功时写一条审计，对象由仓储填", func(t *testing.T) {
		t.Parallel()
		each(t, func(t *testing.T, s *pgstore.Store, call auditedCall) {
			before := auditEntries(t, s)
			a := store.Audit{Actor: "张三", Action: "test.audited", Detail: "理由：缺少分区说明", At: at0 + 1}
			targets, err := call(a)
			must(t, err, "调用")
			after := auditEntries(t, s)
			if len(after) != len(before)+len(targets) {
				t.Fatalf("应新写 %d 条审计，审计从 %d 条变成了 %d 条", len(targets), len(before), len(after))
			}
			wantIDsDescending(t, after)
			wantItems(t, after[len(targets):], before, "原有的审计")
			added := slices.Clone(after[:len(targets)])
			slices.Reverse(added)
			for i, e := range added {
				if e.Audit != a || e.TargetKind != targets[i].kind || e.TargetID != targets[i].id {
					t.Fatalf("第 %d 条审计是 %+v，应为 %+v、对象 %s %s", i+1, e, a, targets[i].kind, targets[i].id)
				}
			}
		})
	})
}

// auditedOps 是 hook 或模板里要审计的方法。提交、审批、驳回、废弃针对一个版本，审计的对象是「ID@版本」；改名针对
// 整个身份，对象是 ID。
func (k versioned[V]) auditedOps() []auditedOp {
	step := func(name string, from model.VersionState, do func(t *testing.T, s *pgstore.Store, h head, a store.Audit) error) auditedOp {
		return auditedOp{name, func(t *testing.T, s *pgstore.Store) auditedCall {
			h := k.head(k.to(t, s, k.mustCreate(t, s, "Base-1"), from))
			return func(a store.Audit) ([]target, error) {
				return []target{{k.kind, fmt.Sprintf("%s@%d", h.ID, h.Version)}}, do(t, s, h, a)
			}
		}}
	}
	return []auditedOp{
		step("提交", model.VersionDraft, func(t *testing.T, s *pgstore.Store, h head, a store.Audit) error {
			return k.submit(t.Context(), s, h.ID, h.Version, h.Rev, a)
		}),
		step("审批", model.VersionReview, func(t *testing.T, s *pgstore.Store, h head, a store.Audit) error {
			return k.approve(t.Context(), s, h.ID, h.Version, h.Rev, "bob", a)
		}),
		step("驳回", model.VersionReview, func(t *testing.T, s *pgstore.Store, h head, a store.Audit) error {
			return k.reject(t.Context(), s, h.ID, h.Version, h.Rev, a)
		}),
		step("废弃", model.VersionApproved, func(t *testing.T, s *pgstore.Store, h head, a store.Audit) error {
			return k.deprecate(t.Context(), s, h.ID, h.Version, h.Rev, a)
		}),
		{"改名", func(t *testing.T, s *pgstore.Store) auditedCall {
			id := k.head(k.mustCreate(t, s, "Base-1")).ID
			return func(a store.Audit) ([]target, error) {
				return []target{{k.kind, id}}, k.rename(t.Context(), s, id, "Renamed-1", a)
			}
		}},
	}
}

// cases 是 hook 与模板共用的用例。
func (k versioned[V]) cases() []testCase {
	return []testCase{
		{"新建：身份加版本 1 的草稿，名字按 NormalizeName 规范化", k.createDraft},
		{"新建：名字不合法报 ProblemsError，问题与 NormalizeName 的相同；用了保留前缀报 name_reserved", k.createBadName},
		{"新建：名字大小写不同也算同名，报 DuplicateError，Existing 是已有的身份", k.createDuplicate},
		{"新建：带了由仓储填的字段，或作者为空、存不下，报 ErrInvalid", k.createBadInput},
		{"保存草稿：整条替换内容，Rev 加 1；改成零值、空集合都原样存下", k.saveDraft},
		{"保存草稿：不是草稿的版本报 ErrImmutable", k.saveNotDraft},
		{"保存草稿：Name 与身份现在的名字不同报 ErrInvalid", k.saveName},
		{"保存草稿：状态、作者、审批人、内置标记与库里的不同报 ErrInvalid", k.saveFixed},
		{"保存草稿：Rev 对不上报 ErrConflict，没有的报 ErrNotFound", k.saveConflict},
		{"状态转换：提交、驳回、保存、再提交、审批、废弃，每步 Rev 加 1", k.lifecycle},
		{"状态转换：不合规矩的转换报 ErrConflict，什么都不改", k.badTransitions},
		{"状态转换：Rev 对不上报 ErrConflict，没有的报 ErrNotFound", k.transitionConflict},
		{"审批：审批人是作者或为空报 ProblemsError(ErrRejected)，存不下报 ErrInvalid", k.approveApprover},
		{"开新版本：复制来源的内容，版本号是下一个，作者是给的那一个", k.newVersionCopies},
		{"开新版本：已有草稿或待审批的版本报 ErrConflict", k.newVersionPending},
		{"开新版本：来源不存在报 ErrNotFound，作者为空或存不下报 ErrInvalid", k.newVersionBad},
		{"改名：按 NormalizeName 规范化，每个版本都换成新名字，Rev 不变，旧名字空出来", k.renameIdentity},
		{"改名：不合法或用了保留前缀报 ProblemsError，撞上别的身份报 DuplicateError，没有的报 ErrNotFound", k.renameBad},
		{"删除草稿：删掉最后一个版本时身份一起删掉、名字空出来；删掉的版本号不再发", k.deleteLast},
		{"删除草稿：带着删掉的草稿的键与 Rev 写报 ErrNotFound，写不到新开的版本上", k.deleteStale},
		{"删除草稿：不是草稿报 ErrImmutable，Rev 对不上报 ErrConflict，没有的报 ErrNotFound", k.deleteBad},
		{"内置：不存在时写入，再装一次什么都不做，内容不同报 ErrBuiltinMismatch", k.builtinEnsure},
		{"内置：名字被别的身份占了报 DuplicateError，用户用内置的名字报 name_reserved", k.builtinName},
		{"内置：身份与版本都不能改，报 ErrImmutable", k.builtinImmutable},
		{"内置：入参不是规范的内置已审批版本，或名字不带保留前缀，报 ErrInvalid", k.builtinBadInput},
		{"ID 或版本号不合格式报 ErrInvalid", k.badKey},
		{"列表：每个身份一项，默认给最新的版本，ApprovedOnly 给当前的已审批版本", k.listPerIdentity},
		{"列表：按名字的 key、再按 ID 排，翻页与一次全取相同", k.listPaging},
		{"列表：按名字前缀过滤，只有 ASCII 字母不分大小写，_、% 与反斜杠按原样比", k.listPrefix},
		{"列表：按状态过滤，看的是给出的那一项", k.listStates},
		{"列表：查询不合规则报 ErrInvalid", k.listInvalid},
		{"列表：Limit 为 0 时每页 100 条", k.listDefaultLimit},
	}
}

func (k versioned[V]) createDraft(t *testing.T, s *pgstore.Store) {
	in := k.sample("  Base-1\t")
	got, err := k.create(t.Context(), s, in)
	must(t, err, "新建")
	id := k.head(got).ID
	if !model.ValidID(id) || !model.IsSafeWord(id) {
		t.Fatalf("生成的 ID %q 应当合 model.ValidID，并且是安全词", id)
	}
	want := k.with(in, func(h *head) { h.ID, h.Version, h.Name, h.State, h.Rev = id, 1, "Base-1", model.VersionDraft, 1 })
	wantEqual(t, got, want, "新建的返回值")
	wantEqual(t, k.mustGet(t, s, id, 1), want, "读回来的")
	wantEqual(t, k.mustVersions(t, s, id), []V{want}, "全部版本")
	if other := k.head(k.mustCreate(t, s, "Base-2")); other.ID == id {
		t.Fatalf("两次新建得到同一个 ID %q", id)
	}
}

func (k versioned[V]) createBadName(t *testing.T, s *pgstore.Store) {
	before := authoredSnapshot(t, s)
	for _, name := range []string{"", "  ", "Rocky Base", "中文名", "Ｒocky", strings.Repeat("a", 65), "a\x00b", "\xff"} {
		want := []model.Problem{nameProblem(t, name, model.Subject{Kind: k.kind})} // 还没有 ID，Subject 只有 Kind
		_, err := k.create(t.Context(), s, k.sample(name))
		wantProblems(t, err, store.ErrInvalid, want, fmt.Sprintf("名字 %q", name))
	}
	for _, name := range reservedNames {
		want := []model.Problem{reservedProblem(t, name, model.Subject{Kind: k.kind})}
		_, err := k.create(t.Context(), s, k.sample(name))
		wantProblems(t, err, store.ErrInvalid, want, fmt.Sprintf("名字 %q 用了保留前缀", name))
	}
	wantAuthoredUnchanged(t, s, before, "名字不合法时")
}

func (k versioned[V]) createDuplicate(t *testing.T, s *pgstore.Store) {
	first := k.head(k.mustCreate(t, s, "Base-1"))
	before := authoredSnapshot(t, s)
	for _, name := range []string{"Base-1", "base-1", "BASE-1", " bAsE-1 "} {
		_, err := k.create(t.Context(), s, k.sample(name))
		wantDuplicate(t, err, "name", first.ID, fmt.Sprintf("新建 %q", name))
	}
	wantAuthoredUnchanged(t, s, before, "名字重复时")
}

// 新建的入参不带由仓储填的字段（ID、版本、状态、审批人、内置标记、Rev）：调用方填了，多半是把别的记录当成了新建的
// 入参。作者是审批人检查的依据（C22），空的或存不下的不收。
func (k versioned[V]) createBadInput(t *testing.T, s *pgstore.Store) {
	before := authoredSnapshot(t, s)
	for _, c := range []struct {
		what string
		mod  func(h *head)
	}{
		{"ID", func(h *head) { h.ID = "my-id" }},
		{"版本", func(h *head) { h.Version = 1 }},
		{"状态", func(h *head) { h.State = model.VersionDraft }}, // 填的就是新建时的状态也不行
		{"审批人", func(h *head) { h.Approver = "bob" }},
		{"内置标记", func(h *head) { h.Builtin = true }},
		{"Rev", func(h *head) { h.Rev = 1 }},
		{"空的作者", func(h *head) { h.Author = "" }},
		{"存不下的作者", func(h *head) { h.Author = "a\x00" }},
	} {
		_, err := k.create(t.Context(), s, k.with(k.sample("Base-1"), c.mod))
		wantIs(t, err, store.ErrInvalid, "新建时"+c.what)
	}
	wantAuthoredUnchanged(t, s, before, "入参不对时")
}

// 保存是整条替换内容：把内容改成零值、把集合清成空的，都是正当的写入，不能被当成「没改」丢掉，nil 与空集合也要
// 分得开（写进去什么就读出什么）。
func (k versioned[V]) saveDraft(t *testing.T, s *pgstore.Store) {
	cur := k.mustCreate(t, s, "Base-1")
	for _, c := range []struct {
		what string
		mod  func(v V) V
	}{{"改一处内容", k.edit}, {"改成零值", k.zero}, {"改成空集合", k.empty}, {"改回齐全的内容", func(v V) V { return k.withHead(k.sample("x"), k.head(v)) }}} {
		in := c.mod(cur)
		got, err := k.save(t.Context(), s, in)
		must(t, err, c.what)
		want := k.with(in, func(h *head) { h.Rev++ })
		wantEqual(t, got, want, c.what+"的返回值")
		wantEqual(t, k.mustGet(t, s, k.head(in).ID, 1), want, c.what+"之后读回来的")
		cur = got
	}
}

// 已提交、已审批、已废弃的版本都不能改（C23）：改要开新版本，待审批的先驳回。
func (k versioned[V]) saveNotDraft(t *testing.T, s *pgstore.Store) {
	v := k.mustCreate(t, s, "Base-1")
	for _, state := range []model.VersionState{model.VersionReview, model.VersionApproved, model.VersionDeprecated} {
		v = k.to(t, s, v, state)
		before := authoredSnapshot(t, s)
		_, err := k.save(t.Context(), s, k.edit(v)) // 带着当前的 Rev
		wantIs(t, err, store.ErrImmutable, "保存 "+string(state)+" 的版本")
		wantAuthoredUnchanged(t, s, before, "保存被拒时")
	}
}

// 名字放在身份上，改名用 rename。保存时 Name 要与身份现在的名字一字不差：大小写不同、带空白都不算。
func (k versioned[V]) saveName(t *testing.T, s *pgstore.Store) {
	v := k.mustCreate(t, s, "Base-1")
	id := k.head(v).ID
	before := authoredSnapshot(t, s)
	for _, name := range []string{"Base-2", "base-1", " Base-1", ""} {
		_, err := k.save(t.Context(), s, k.with(k.edit(v), func(h *head) { h.Name = name }))
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("保存时 Name 为 %q", name))
	}
	wantAuthoredUnchanged(t, s, before, "Name 不对时")
	// 改名之后，带旧名字的保存被拒，带新名字的照常保存
	must(t, k.rename(t.Context(), s, id, "Renamed-1", anAudit(k.kind+".rename")), "改名")
	_, err := k.save(t.Context(), s, k.edit(v))
	wantIs(t, err, store.ErrInvalid, "改名之后带着旧名字保存")
	_, err = k.save(t.Context(), s, k.with(k.edit(v), func(h *head) { h.Name = "Renamed-1" }))
	must(t, err, "改名之后带着新名字保存")
}

// 状态只能经状态转换改，作者在开版本时定下（审批人检查的依据，C22），审批人由审批写，内置标记只由装载内置内容写：
// 保存时带来的值与库里的不同，报 ErrInvalid，不悄悄丢掉。
func (k versioned[V]) saveFixed(t *testing.T, s *pgstore.Store) {
	v := k.mustCreate(t, s, "Base-1")
	before := authoredSnapshot(t, s)
	for _, c := range []struct {
		what string
		mod  func(h *head)
	}{
		{"状态改成 review", func(h *head) { h.State = model.VersionReview }},
		{"状态为空", func(h *head) { h.State = "" }},
		{"作者", func(h *head) { h.Author = "carol" }},
		{"审批人", func(h *head) { h.Approver = "bob" }},
		{"内置标记", func(h *head) { h.Builtin = true }},
	} {
		_, err := k.save(t.Context(), s, k.with(k.edit(v), c.mod))
		wantIs(t, err, store.ErrInvalid, "保存时改了"+c.what)
	}
	wantAuthoredUnchanged(t, s, before, "改了不能改的字段时")
}

func (k versioned[V]) saveConflict(t *testing.T, s *pgstore.Store) {
	stale := k.mustCreate(t, s, "Base-1")
	cur, err := k.save(t.Context(), s, k.edit(stale))
	must(t, err, "保存")
	before := authoredSnapshot(t, s)
	for _, rev := range []int64{0, k.head(stale).Rev, k.head(cur).Rev + 1} {
		_, err := k.save(t.Context(), s, k.with(k.edit(cur), func(h *head) { h.Rev = rev }))
		wantIs(t, err, store.ErrConflict, fmt.Sprintf("带着 Rev %d 保存", rev))
	}
	_, err = k.save(t.Context(), s, k.with(cur, func(h *head) { h.ID = "nosuchid" }))
	wantIs(t, err, store.ErrNotFound, "保存不存在的身份")
	_, err = k.save(t.Context(), s, k.with(cur, func(h *head) { h.Version = 2 }))
	wantIs(t, err, store.ErrNotFound, "保存不存在的版本")
	wantAuthoredUnchanged(t, s, before, "保存被拒时")
}

// 一个版本走完它能走的每一步：提交、驳回（理由写在审计里）、驳回之后又是草稿，可以保存、再提交、审批、废弃。每步 Rev
// 加 1，审批写下审批人，废弃之后审批人仍在。
func (k versioned[V]) lifecycle(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	cur := k.mustCreate(t, s, "Base-1")
	h := k.head(cur)
	expect := func(what string, state model.VersionState, approver string) {
		t.Helper()
		cur = k.with(cur, func(h *head) { h.State, h.Approver, h.Rev = state, approver, h.Rev+1 })
		wantEqual(t, k.mustGet(t, s, h.ID, 1), cur, what+"之后")
	}
	rev := func() int64 { return k.head(cur).Rev }
	must(t, k.submit(ctx, s, h.ID, 1, rev(), anAudit(k.kind+".submit")), "提交")
	expect("提交", model.VersionReview, "")
	reason := store.Audit{Actor: "bob", Action: k.kind + ".reject", Detail: "缺少说明", At: at0}
	must(t, k.reject(ctx, s, h.ID, 1, rev(), reason), "驳回")
	expect("驳回", model.VersionDraft, "")
	saved, err := k.save(ctx, s, k.edit(cur))
	must(t, err, "驳回之后保存")
	cur = saved
	must(t, k.submit(ctx, s, h.ID, 1, rev(), anAudit(k.kind+".submit")), "再提交")
	expect("再提交", model.VersionReview, "")
	must(t, k.approve(ctx, s, h.ID, 1, rev(), "bob", anAudit(k.kind+".approve")), "审批")
	expect("审批", model.VersionApproved, "bob")
	must(t, k.deprecate(ctx, s, h.ID, 1, rev(), anAudit(k.kind+".deprecate")), "废弃")
	expect("废弃", model.VersionDeprecated, "bob")
}

// transition 是一种状态转换，from 是它唯一合规矩的起点；do 带着 h 里的键与 Rev 调用它。
type transition struct {
	name string
	from model.VersionState
	do   func(t *testing.T, s *pgstore.Store, h head) error
}

// transitions 是四种状态转换。
func (k versioned[V]) transitions() []transition {
	return []transition{
		{"提交", model.VersionDraft, func(t *testing.T, s *pgstore.Store, h head) error {
			return k.submit(t.Context(), s, h.ID, h.Version, h.Rev, anAudit(k.kind+".submit"))
		}},
		{"审批", model.VersionReview, func(t *testing.T, s *pgstore.Store, h head) error {
			return k.approve(t.Context(), s, h.ID, h.Version, h.Rev, "bob", anAudit(k.kind+".approve"))
		}},
		{"驳回", model.VersionReview, func(t *testing.T, s *pgstore.Store, h head) error {
			return k.reject(t.Context(), s, h.ID, h.Version, h.Rev, anAudit(k.kind+".reject"))
		}},
		{"废弃", model.VersionApproved, func(t *testing.T, s *pgstore.Store, h head) error {
			return k.deprecate(t.Context(), s, h.ID, h.Version, h.Rev, anAudit(k.kind+".deprecate"))
		}},
	}
}

// 只有 draft → review、review → approved、review → draft、approved → deprecated 四步合规矩，其余的带着正确的 Rev 也报
// ErrConflict：调用方重读之后再判断（spec §6.2 第 4 条）。
func (k versioned[V]) badTransitions(t *testing.T, s *pgstore.Store) {
	for i, state := range []model.VersionState{model.VersionDraft, model.VersionReview, model.VersionApproved, model.VersionDeprecated} {
		v := k.to(t, s, k.mustCreate(t, s, fmt.Sprintf("Base-%d", i)), state)
		before := authoredSnapshot(t, s)
		for _, step := range k.transitions() {
			if step.from != state {
				wantIs(t, step.do(t, s, k.head(v)), store.ErrConflict, fmt.Sprintf("从 %s %s", state, step.name))
			}
		}
		wantAuthoredUnchanged(t, s, before, "转换不合规矩时")
	}
}

func (k versioned[V]) transitionConflict(t *testing.T, s *pgstore.Store) {
	for i, step := range k.transitions() {
		h := k.head(k.to(t, s, k.mustCreate(t, s, fmt.Sprintf("Base-%d", i)), step.from))
		before := authoredSnapshot(t, s)
		for _, rev := range []int64{0, h.Rev - 1, h.Rev + 1} {
			stale := h
			stale.Rev = rev
			wantIs(t, step.do(t, s, stale), store.ErrConflict, fmt.Sprintf("%s时带着 Rev %d", step.name, rev))
		}
		for _, missing := range []head{{ID: "nosuchid", Version: 1, Rev: h.Rev}, {ID: h.ID, Version: 2, Rev: h.Rev}} {
			wantIs(t, step.do(t, s, missing), store.ErrNotFound, fmt.Sprintf("%s不存在的 %s@%d", step.name, missing.ID, missing.Version))
		}
		wantAuthoredUnchanged(t, s, before, step.name+"被拒时")
	}
}

// 审批人不能是作者（C22），首尾空白与大小写不同也算本人；没有审批人同样批不了。问题与直接调用 activation.Approver 的
// 相同，Subject 是审批的那个版本。审批人要写进库里，存不下的报 ErrInvalid。被拒时状态与 Rev 都不变。
func (k versioned[V]) approveApprover(t *testing.T, s *pgstore.Store) {
	h := k.head(k.to(t, s, k.mustCreate(t, s, "Base-1"), model.VersionReview))
	subj := model.Subject{Kind: k.kind, ID: h.ID + "@1"}
	before := authoredSnapshot(t, s)
	for _, c := range []struct {
		approver string
		code     model.Code
	}{{"alice", "approver_is_author"}, {" Alice ", "approver_is_author"}, {"ALICE", "approver_is_author"}, {"", "approver_missing"}, {"  ", "approver_missing"}} {
		want := approverProblems(h.Author, c.approver, subj)
		wantCodes(t, want, []model.Code{c.code})
		err := k.approve(t.Context(), s, h.ID, 1, h.Rev, c.approver, anAudit(k.kind+".approve"))
		wantProblems(t, err, store.ErrRejected, want, fmt.Sprintf("审批人 %q", c.approver))
	}
	for _, approver := range []string{"b\x00b", "\xff"} {
		err := k.approve(t.Context(), s, h.ID, 1, h.Rev, approver, anAudit(k.kind+".approve"))
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("审批人 %q", approver))
	}
	wantAuthoredUnchanged(t, s, before, "审批被拒时")
	must(t, k.approve(t.Context(), s, h.ID, 1, h.Rev, "bob", anAudit(k.kind+".approve")), "换成 bob 审批")
}

// 新版本复制来源版本的内容，版本号是分配过的最大版本号加 1，作者是给的那一个，没有审批人，Rev 从 1 起。来源可以是已审批
// 或已废弃的：已有草稿或待审批的版本时开不了新版本（见下一条），所以来源不会是这两种。
func (k versioned[V]) newVersionCopies(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	v1 := k.to(t, s, k.mustCreate(t, s, "Base-1"), model.VersionApproved)
	id := k.head(v1).ID
	v2, err := k.newVersion(ctx, s, id, 1, "carol")
	must(t, err, "从版本 1 开新版本")
	fresh := func(from V, version int) V {
		return k.with(from, func(h *head) {
			h.Version, h.State, h.Author, h.Approver, h.Rev = version, model.VersionDraft, "carol", "", 1
		})
	}
	wantEqual(t, v2, fresh(v1, 2), "版本 2 的返回值")
	wantEqual(t, k.mustGet(t, s, id, 2), v2, "读回来的版本 2")
	wantEqual(t, k.mustGet(t, s, id, 1), v1, "来源版本不变")

	v2, err = k.save(ctx, s, k.edit(v2))
	must(t, err, "改版本 2")
	v2 = k.to(t, s, v2, model.VersionApproved) // carol 是作者，bob 审批
	v1 = k.to(t, s, v1, model.VersionDeprecated)
	v3, err := k.newVersion(ctx, s, id, 1, "carol") // 来源已废弃
	must(t, err, "从废弃的版本 1 开新版本")
	wantEqual(t, v3, fresh(v1, 3), "版本 3 复制的是版本 1 的内容")
	wantEqual(t, k.mustVersions(t, s, id), []V{v1, v2, v3}, "全部版本按版本号升序")
}

// 每个身份同时最多一个未审批的版本（草稿或待审批）。被拒的开新版本不占号：版本号在同一个事务里分配，报 ErrConflict 时
// 一起回滚，分配过的最大版本号不变（S5-4）。
func (k versioned[V]) newVersionPending(t *testing.T, s *pgstore.Store) {
	v1 := k.mustCreate(t, s, "Base-1")
	id := k.head(v1).ID
	check := func(what string, last int) {
		t.Helper()
		before := authoredSnapshot(t, s)
		_, err := k.newVersion(t.Context(), s, id, 1, "carol")
		wantIs(t, err, store.ErrConflict, what)
		wantAuthoredUnchanged(t, s, before, what+"时")
		k.wantLastVersion(t, s, id, last, what+"时")
	}
	check("版本 1 还是草稿", 1)
	v1 = k.to(t, s, v1, model.VersionReview)
	check("版本 1 待审批", 1)
	k.to(t, s, v1, model.VersionApproved)
	v2, err := k.newVersion(t.Context(), s, id, 1, "carol")
	must(t, err, "版本 1 审批之后开新版本")
	check("版本 2 还是草稿", 2)
	k.to(t, s, v2, model.VersionReview)
	check("版本 2 待审批", 2)
}

// 开新版本被拒（ErrNotFound、ErrInvalid）同样不占号，理由同上。
func (k versioned[V]) newVersionBad(t *testing.T, s *pgstore.Store) {
	id := k.head(k.to(t, s, k.mustCreate(t, s, "Base-1"), model.VersionApproved)).ID
	before := authoredSnapshot(t, s)
	_, err := k.newVersion(t.Context(), s, "nosuchid", 1, "carol")
	wantIs(t, err, store.ErrNotFound, "身份不存在")
	_, err = k.newVersion(t.Context(), s, id, 2, "carol")
	wantIs(t, err, store.ErrNotFound, "来源版本不存在")
	for _, author := range []string{"", "c\x00", "\xff"} {
		_, err = k.newVersion(t.Context(), s, id, 1, author)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("作者 %q", author))
	}
	wantAuthoredUnchanged(t, s, before, "开新版本被拒时")
	k.wantLastVersion(t, s, id, 1, "开新版本被拒之后")
}

// wantLastVersion 直接查身份 id 上分配过的最大版本号 last_version，要等于 want。接口只给出开出来的版本，看不到这个计数：
// 被拒的开新版本要是占了号（比如另起一次写入把计数加 1），之后开出的版本号就会跳过几个，经接口要等到再开一次才看得出来。
func (k versioned[V]) wantLastVersion(t *testing.T, s *pgstore.Store, id string, want int, what string) {
	t.Helper()
	var got int
	err := s.DBForTest().WithContext(t.Context()).Raw("SELECT last_version FROM "+k.idents+" WHERE id = ?", id).Row().Scan(&got)
	must(t, err, fmt.Sprintf("查 %s %s 的版本计数", k.kind, id))
	if got != want {
		t.Fatalf("%s：%s %s 分配过的最大版本号是 %d，应为 %d", what, k.kind, id, got, want)
	}
}

// 改名针对整个身份：每个版本的 Name 都换成新名字，版本的 Rev 不变（版本那条记录没改）；旧名字空出来，别的身份能用。
// 只改大小写也是改名，名字的 key 还是自己的，不算撞上。改名也不动版本号的分配，之后开的新版本接着往后编号。
func (k versioned[V]) renameIdentity(t *testing.T, s *pgstore.Store) {
	v1 := k.to(t, s, k.mustCreate(t, s, "Base-1"), model.VersionApproved)
	id := k.head(v1).ID
	v2, err := k.newVersion(t.Context(), s, id, 1, "carol")
	must(t, err, "开新版本")
	for _, c := range []struct{ in, want string }{{"  Renamed-1 ", "Renamed-1"}, {"RENAMED-1", "RENAMED-1"}} {
		must(t, k.rename(t.Context(), s, id, c.in, anAudit(k.kind+".rename")), fmt.Sprintf("改名为 %q", c.in))
		renamed := func(v V) V { return k.with(v, func(h *head) { h.Name = c.want }) }
		wantEqual(t, k.mustVersions(t, s, id), []V{renamed(v1), renamed(v2)}, "改名之后的全部版本")
	}
	v2 = k.to(t, s, k.mustGet(t, s, id, 2), model.VersionApproved)
	if h := k.head(k.newVersionOf(t, s, v2)); h.Version != 3 {
		t.Fatalf("改名之后开的新版本应是版本 3，得到版本 %d", h.Version)
	}
	other := k.head(k.mustCreate(t, s, "base-1")) // 旧名字空出来了
	if other.ID == id {
		t.Fatalf("用旧名字新建的应是另一个身份，得到同一个 ID %q", id)
	}
}

func (k versioned[V]) renameBad(t *testing.T, s *pgstore.Store) {
	id := k.head(k.mustCreate(t, s, "Base-1")).ID
	other := k.head(k.mustCreate(t, s, "Other-1")).ID
	before := authoredSnapshot(t, s)
	for _, name := range []string{"", "Base 2", "中文名"} {
		err := k.rename(t.Context(), s, id, name, anAudit(k.kind+".rename"))
		want := []model.Problem{nameProblem(t, name, model.Subject{Kind: k.kind, ID: id})} // 改名针对整个身份：ID 不带版本
		wantProblems(t, err, store.ErrInvalid, want, fmt.Sprintf("改名为 %q", name))
	}
	for _, name := range reservedNames {
		err := k.rename(t.Context(), s, id, name, anAudit(k.kind+".rename"))
		want := []model.Problem{reservedProblem(t, name, model.Subject{Kind: k.kind, ID: id})}
		wantProblems(t, err, store.ErrInvalid, want, fmt.Sprintf("改名为用了保留前缀的 %q", name))
	}
	for _, name := range []string{"Other-1", "other-1", " OTHER-1 "} {
		wantDuplicate(t, k.rename(t.Context(), s, id, name, anAudit(k.kind+".rename")), "name", other, fmt.Sprintf("改名为 %q", name))
	}
	wantIs(t, k.rename(t.Context(), s, "nosuchid", "Base-9", anAudit(k.kind+".rename")), store.ErrNotFound, "改不存在的身份")
	wantAuthoredUnchanged(t, s, before, "改名被拒时")
}

// 删的是身份的最后一个版本时，身份一起删掉，名字空出来。身份还有别的版本时只删这个草稿；版本号只增不减，下次开新
// 版本时跳过删掉的号（为什么见 deleteStale）。删除草稿不审计（spec §6.2 第 8 条：编辑草稿不写审计）。
func (k versioned[V]) deleteLast(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	only := k.head(k.mustCreate(t, s, "Base-1"))
	audits := auditEntries(t, s)
	must(t, k.deleteDraft(ctx, s, only.ID, 1, only.Rev), "删除唯一的草稿")
	_, err := k.get(ctx, s, only.ID, 1)
	wantIs(t, err, store.ErrNotFound, "读删掉的版本")
	_, err = k.versions(ctx, s, only.ID)
	wantIs(t, err, store.ErrNotFound, "列出删掉的身份的版本")
	wantItems(t, k.listed(t, s, listQuery{}), nil, "身份删掉之后的列表")
	wantItems(t, auditEntries(t, s), audits, "删除草稿不写审计")
	if again := k.head(k.mustCreate(t, s, "base-1")); again.ID == only.ID {
		t.Fatalf("名字空出来之后新建的应是另一个身份，得到同一个 ID %q", only.ID)
	}

	v1 := k.to(t, s, k.mustCreate(t, s, "Base-2"), model.VersionApproved)
	id := k.head(v1).ID
	v2, err := k.newVersion(ctx, s, id, 1, "carol")
	must(t, err, "开新版本")
	must(t, k.deleteDraft(ctx, s, id, 2, k.head(v2).Rev), "删除版本 2 的草稿")
	wantEqual(t, k.mustVersions(t, s, id), []V{v1}, "只剩版本 1")
	again, err := k.newVersion(ctx, s, id, 1, "dave")
	must(t, err, "再开新版本")
	if h := k.head(again); h.Version != 3 || h.Author != "dave" {
		t.Fatalf("再开的新版本应是 dave 的版本 3，得到 %s 的版本 %d", h.Author, h.Version)
	}
}

// 删掉的草稿的键还记在别处：调用方手里的旧副本、派生模板的 DerivedFrom、审计的对象都是「ID@版本」。号再发出去，这些
// 就会对到另一份内容上；新版本的 Rev 同样从 1 起，带着旧副本保存时 Rev 也对得上，乐观锁拦不住，别人新开的草稿被悄悄
// 覆盖。所以删掉的号不再发，带着删掉的草稿的键写，一律报 ErrNotFound。
func (k versioned[V]) deleteStale(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	v1 := k.to(t, s, k.mustCreate(t, s, "Base-1"), model.VersionApproved)
	old := k.newVersionOf(t, s, v1) // 调用方手里留着这份版本 2 的草稿
	h := k.head(old)
	must(t, k.deleteDraft(ctx, s, h.ID, h.Version, h.Rev), "删除版本 2 的草稿")
	k.newVersionOf(t, s, v1) // 另一个人又开了新版本
	before := authoredSnapshot(t, s)
	_, err := k.save(ctx, s, k.edit(old))
	wantIs(t, err, store.ErrNotFound, "带着删掉的草稿保存")
	wantIs(t, k.submit(ctx, s, h.ID, h.Version, h.Rev, anAudit(k.kind+".submit")), store.ErrNotFound, "提交删掉的草稿")
	wantIs(t, k.deleteDraft(ctx, s, h.ID, h.Version, h.Rev), store.ErrNotFound, "再删一次删掉的草稿")
	wantAuthoredUnchanged(t, s, before, "带着删掉的草稿写时")
}

func (k versioned[V]) deleteBad(t *testing.T, s *pgstore.Store) {
	draft := k.head(k.mustCreate(t, s, "Base-1"))
	before := authoredSnapshot(t, s)
	for _, rev := range []int64{0, draft.Rev + 1} {
		wantIs(t, k.deleteDraft(t.Context(), s, draft.ID, 1, rev), store.ErrConflict, fmt.Sprintf("带着 Rev %d 删除", rev))
	}
	wantIs(t, k.deleteDraft(t.Context(), s, "nosuchid", 1, 1), store.ErrNotFound, "删除不存在的身份")
	wantIs(t, k.deleteDraft(t.Context(), s, draft.ID, 2, 1), store.ErrNotFound, "删除不存在的版本")
	wantAuthoredUnchanged(t, s, before, "删除被拒时")
	// 已提交、已审批与已废弃的版本永不删除，只能删草稿
	for i, state := range []model.VersionState{model.VersionReview, model.VersionApproved, model.VersionDeprecated} {
		h := k.head(k.to(t, s, k.mustCreate(t, s, fmt.Sprintf("Base-%d", i+2)), state))
		before := authoredSnapshot(t, s)
		wantIs(t, k.deleteDraft(t.Context(), s, h.ID, 1, h.Rev), store.ErrImmutable, "删除 "+string(state)+" 的版本")
		wantAuthoredUnchanged(t, s, before, "删除被拒时")
	}
}

// 装载内置内容（spec §6.2 第 11 条）：同一个 ID 与版本不存在就写入，Rev 为 1；已存在并且内容相同，什么都不做，入参的
// Rev 不算数；内容不同说明有人改了内置内容却没升版本号，报 ErrBuiltinMismatch。内容的每个字段都要比，嵌套的要比到
// 最里层：最可能被改的是实现的正文这类嵌套的内容，漏比一处，同一个版本号就对应了两份内容（variants 各改一处）。名字
// 放在身份上，同一身份的版本共用一个名字，别的版本带了另一个名字也算不同。装载不写审计。
func (k versioned[V]) builtinEnsure(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	b1 := k.builtin("builtin-base", 1, "lober:Base")
	must(t, k.ensureBuiltin(ctx, s, b1), "装载版本 1")
	want1 := k.with(b1, func(h *head) { h.Rev = 1 })
	wantEqual(t, k.mustVersions(t, s, "builtin-base"), []V{want1}, "装载之后")
	wantItems(t, auditEntries(t, s), nil, "装载内置内容不写审计")

	before := authoredSnapshot(t, s)
	must(t, k.ensureBuiltin(ctx, s, b1), "再装一次")
	must(t, k.ensureBuiltin(ctx, s, k.with(b1, func(h *head) { h.Rev = 7 })), "带着别的 Rev 再装一次")
	wantAuthoredUnchanged(t, s, before, "内容相同再装时")
	for _, c := range []struct {
		what string
		v    V
	}{
		{"作者", k.with(b1, func(h *head) { h.Author = "someone" })},
		{"审批人", k.with(b1, func(h *head) { h.Approver = "bob" })},
		{"名字", k.with(b1, func(h *head) { h.Name = "lober:Other" })},
		{"名字的大小写", k.with(b1, func(h *head) { h.Name = "lober:base" })},
	} {
		wantIs(t, k.ensureBuiltin(ctx, s, c.v), store.ErrBuiltinMismatch, "版本 1 的"+c.what+"不同")
	}
	for _, c := range k.variants {
		v := k.builtin("builtin-base", 1, "lober:Base") // 新造一份：mod 连切片与 map 里的元素一起改
		c.mod(&v)
		wantIs(t, k.ensureBuiltin(ctx, s, v), store.ErrBuiltinMismatch, "版本 1 的"+c.what+"不同")
	}
	wantAuthoredUnchanged(t, s, before, "内容不同时")

	b2 := k.with(k.edit(b1), func(h *head) { h.Version = 2 })
	must(t, k.ensureBuiltin(ctx, s, b2), "装载版本 2")
	want2 := k.with(b2, func(h *head) { h.Rev = 1 })
	wantEqual(t, k.mustVersions(t, s, "builtin-base"), []V{want1, want2}, "两个版本都在")
	// 名字随身份定下（S5-1）：以后的版本换了名，同样报 ErrBuiltinMismatch；要改名只能换一个新 ID
	b3 := k.with(b1, func(h *head) { h.Version, h.Name = 3, "lober:Renamed" })
	wantIs(t, k.ensureBuiltin(ctx, s, b3), store.ErrBuiltinMismatch, "版本 3 带了另一个名字")
}

// 内置与用户建的身份共用一个名字空间（spec §6.2 第 11 条）：名字被别的身份占了，装载报 DuplicateError，Existing 是那个
// 身份。用户的身份用不了保留前缀（C12），占着内置名字的只可能是另一个内置身份；反过来，用户新建与改名时用内置的名字，
// 在名字规则里就报 name_reserved，到不了唯一索引。
func (k versioned[V]) builtinName(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	must(t, k.ensureBuiltin(ctx, s, k.builtin("builtin-base", 1, "lober:Base")), "装载")
	before := authoredSnapshot(t, s)
	wantDuplicate(t, k.ensureBuiltin(ctx, s, k.builtin("builtin-other", 1, "LOBER:BASE")), "name", "builtin-base", "装载撞上别的身份的名字")
	wantAuthoredUnchanged(t, s, before, "装载撞名时")

	user := k.head(k.mustCreate(t, s, "Base-1"))
	before = authoredSnapshot(t, s)
	_, err := k.create(ctx, s, k.sample("lober:BASE"))
	wantProblems(t, err, store.ErrInvalid, []model.Problem{reservedProblem(t, "lober:BASE", model.Subject{Kind: k.kind})},
		"用户新建时用了内置的名字")
	wantProblems(t, k.rename(ctx, s, user.ID, "LOBER:base", anAudit(k.kind+".rename")), store.ErrInvalid,
		[]model.Problem{reservedProblem(t, "LOBER:base", model.Subject{Kind: k.kind, ID: user.ID})}, "用户改名为内置的名字")
	wantAuthoredUnchanged(t, s, before, "用户用了内置的名字时")
}

// 内置内容只读（C25）：内置的身份不能改名、不能开新版本，内置的版本不能保存、删除，也不能转换状态，废弃也不行。要改
// 就先派生（模板），或者等 lober 发布新版本。
func (k versioned[V]) builtinImmutable(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	b := k.builtin("builtin-base", 1, "lober:Base")
	must(t, k.ensureBuiltin(ctx, s, b), "装载")
	b = k.mustGet(t, s, "builtin-base", 1)
	h := k.head(b)
	before := authoredSnapshot(t, s)
	_, err := k.save(ctx, s, k.edit(b))
	wantIs(t, err, store.ErrImmutable, "保存内置的版本")
	_, err = k.newVersion(ctx, s, h.ID, 1, "carol")
	wantIs(t, err, store.ErrImmutable, "给内置的身份开新版本")
	wantIs(t, k.rename(ctx, s, h.ID, "Mine", anAudit(k.kind+".rename")), store.ErrImmutable, "给内置的身份改名")
	wantIs(t, k.deleteDraft(ctx, s, h.ID, 1, h.Rev), store.ErrImmutable, "删除内置的版本")
	for _, step := range k.transitions() {
		wantIs(t, step.do(t, s, h), store.ErrImmutable, step.name+"内置的版本")
	}
	wantAuthoredUnchanged(t, s, before, "改内置内容被拒时")
}

// 内置内容是程序自带的：要是内置的已审批版本，ID 与版本号合格式，名字已经是规范的写法（再规范化一次不变）并且带保留
// 前缀 lober:（C12），文本都存得下。不合这些的是程序的错，报 ErrInvalid，什么都不写。
func (k versioned[V]) builtinBadInput(t *testing.T, s *pgstore.Store) {
	before := authoredSnapshot(t, s)
	for _, c := range []struct {
		what string
		mod  func(h *head)
	}{
		{"不是内置的", func(h *head) { h.Builtin = false }},
		{"是草稿", func(h *head) { h.State = model.VersionDraft }},
		{"待审批", func(h *head) { h.State = model.VersionReview }},
		{"已废弃", func(h *head) { h.State = model.VersionDeprecated }},
		{"ID 为空", func(h *head) { h.ID = "" }},
		{"ID 有大写", func(h *head) { h.ID = "Builtin-Base" }},
		{"版本号为 0", func(h *head) { h.Version = 0 }},
		{"名字带空白", func(h *head) { h.Name = " lober:Base" }},
		{"名字不合法", func(h *head) { h.Name = "lober:Builtin Base" }},
		{"名字不带保留前缀", func(h *head) { h.Name = "Builtin-Base" }},
		{"名字只是长得像保留前缀", func(h *head) { h.Name = "lober-Base" }},
		{"作者存不下", func(h *head) { h.Author = "\xff" }},
	} {
		err := k.ensureBuiltin(t.Context(), s, k.with(k.builtin("builtin-base", 1, "lober:Base"), c.mod))
		wantIs(t, err, store.ErrInvalid, "装载的内置内容"+c.what)
	}
	wantAuthoredUnchanged(t, s, before, "入参不对时")
}

// 收到的 ID 先过 model.ValidID，版本号从 1 起：不合的报 ErrInvalid，不当成找不到。
func (k versioned[V]) badKey(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	v := k.mustCreate(t, s, "Base-1")
	good := k.head(v).ID
	before := authoredSnapshot(t, s)
	for _, key := range []struct {
		id      string
		version int
	}{{"", 1}, {"Bad", 1}, {"a@1", 1}, {"-a", 1}, {"a.b", 1}, {good, 0}, {good, -1}} {
		what := fmt.Sprintf("%q@%d", key.id, key.version)
		h := head{ID: key.id, Version: key.version, Rev: 1}
		_, err := k.get(ctx, s, key.id, key.version)
		wantIs(t, err, store.ErrInvalid, "读 "+what)
		_, err = k.save(ctx, s, k.with(v, func(x *head) { x.ID, x.Version = key.id, key.version }))
		wantIs(t, err, store.ErrInvalid, "保存 "+what)
		wantIs(t, k.deleteDraft(ctx, s, key.id, key.version, 1), store.ErrInvalid, "删除 "+what)
		for _, step := range k.transitions() {
			wantIs(t, step.do(t, s, h), store.ErrInvalid, step.name+" "+what)
		}
		if key.version == 1 { // 下面几个方法只收 ID
			_, err = k.versions(ctx, s, key.id)
			wantIs(t, err, store.ErrInvalid, "列出版本 "+what)
			wantIs(t, k.rename(ctx, s, key.id, "Base-9", anAudit(k.kind+".rename")), store.ErrInvalid, "改名 "+what)
			_, err = k.newVersion(ctx, s, key.id, 1, "carol")
			wantIs(t, err, store.ErrInvalid, "开新版本 "+what)
		} else {
			_, err = k.newVersion(ctx, s, good, key.version, "carol")
			wantIs(t, err, store.ErrInvalid, "从版本 "+what+" 开新版本")
		}
	}
	wantAuthoredUnchanged(t, s, before, "键不合格式时")
}

// newVersionOf 从 v 开一个新版本，作者是 carol。
func (k versioned[V]) newVersionOf(t *testing.T, s *pgstore.Store, v V) V {
	t.Helper()
	h := k.head(v)
	got, err := k.newVersion(t.Context(), s, h.ID, h.Version, "carol")
	must(t, err, fmt.Sprintf("从 %s@%d 开新版本", h.ID, h.Version))
	return got
}

// 每个身份一项。ApprovedOnly 时给当前的已审批版本，规则同 model.CurrentApproved：废弃的不算，最新的已审批版本被废弃后
// 回落到上一个（C26）；新版本审批之前，旧版本照常给出（C23）。没有已审批版本的身份跳过。
func (k versioned[V]) listPerIdentity(t *testing.T, s *pgstore.Store) {
	a1 := k.to(t, s, k.mustCreate(t, s, "A"), model.VersionApproved)
	a2 := k.to(t, s, k.newVersionOf(t, s, a1), model.VersionApproved)
	a3 := k.newVersionOf(t, s, a2) // 最新的是草稿
	b1 := k.to(t, s, k.mustCreate(t, s, "B"), model.VersionApproved)
	b2 := k.to(t, s, k.newVersionOf(t, s, b1), model.VersionDeprecated)
	c1 := k.mustCreate(t, s, "C")
	d1 := k.to(t, s, k.mustCreate(t, s, "D"), model.VersionDeprecated)
	wantEqual(t, k.listed(t, s, listQuery{}), []V{a3, b2, c1, d1}, "默认给最新的版本")
	approved := k.listed(t, s, listQuery{ApprovedOnly: true})
	wantEqual(t, approved, []V{a2, b1}, "ApprovedOnly 给当前的已审批版本")
	var want []V
	for _, v := range []V{a1, b1, c1, d1} {
		if cur, ok := k.currentApproved(k.mustVersions(t, s, k.head(v).ID)); ok {
			want = append(want, cur)
		}
	}
	wantEqual(t, approved, want, "与 model.CurrentApproved 一致")
}

// 按名字的 key（小写）排，而不是按显示的名字：这几个名字按两种排法得到的顺序不同。key 唯一，再按 ID 只是兜底。
func (k versioned[V]) listPaging(t *testing.T, s *pgstore.Store) {
	byName := map[string]V{}
	for _, name := range []string{"b-2", "A-1", "_x", "Z9", "a-10"} {
		byName[name] = k.mustCreate(t, s, name)
	}
	var want []V
	for _, name := range []string{"_x", "A-1", "a-10", "b-2", "Z9"} {
		want = append(want, byName[name])
	}
	whole, err := k.list(t.Context(), s, listQuery{Limit: len(want)})
	must(t, err, "一次全取")
	wantItems(t, whole.Items, want, "一次全取")
	if whole.Next != "" {
		t.Fatalf("恰好取完时 Next 应为空，得到 %q", whole.Next)
	}
	for _, limit := range []int{2, 3} {
		wantPaged(t, k.pages(t, s, listQuery{Limit: limit}), limit, want, fmt.Sprintf("每页 %d 条", limit))
	}
}

// 名字的前缀与名字的 key 比，所以 ASCII 字母不分大小写，与「大小写不同也算同名」一致。名字只收可打印 ASCII，前缀里的
// 别的字符只按字节比：开尔文符号 K（U+212A）按 Unicode 的规则会转成 k，这里不能把它当成 k。
func (k versioned[V]) listPrefix(t *testing.T, s *pgstore.Store) {
	for _, name := range []string{"Net_A", "NetXA", "Net%B", "NetYB", `Net\Z`, "NetAZ", "Nic-1", "K-Tool"} {
		k.mustCreate(t, s, name)
	}
	for _, c := range []struct {
		prefix string
		want   []string
	}{
		{"net_", []string{"Net_A"}},
		{"NET%", []string{"Net%B"}},
		{`net\`, []string{`Net\Z`}},
		{"NET", []string{"Net%B", `Net\Z`, "Net_A", "NetAZ", "NetXA", "NetYB"}},
		{"nic-1", []string{"Nic-1"}}, // 整个名字也是它自己的前缀
		{"k", []string{"K-Tool"}},
		{"K-t", []string{"K-Tool"}},
		{"K-tool", nil}, // 开尔文符号：按 Unicode 的大小写规则会转成 k；下一行的前缀就是它本身
		{"K", nil},
		{"net ", nil},
		{"", []string{"K-Tool", "Net%B", `Net\Z`, "Net_A", "NetAZ", "NetXA", "NetYB", "Nic-1"}},
		// 存不下的前缀谁的名字都不是（store 的包说明）：当作没有，得到空的一页，不报数据库的编码错误
		{"Net\x00", nil},
		{"Net\xff", nil},
		{"\x00", nil},
	} {
		wantItems(t, k.names(k.listed(t, s, listQuery{NamePrefix: c.prefix})), c.want, fmt.Sprintf("前缀 %q", c.prefix))
	}
}

// 状态的过滤看的是给出的那一项：默认是最新的版本，ApprovedOnly 时是当前的已审批版本。
func (k versioned[V]) listStates(t *testing.T, s *pgstore.Store) {
	a := k.mustCreate(t, s, "A")
	b := k.to(t, s, k.mustCreate(t, s, "B"), model.VersionReview)
	c := k.to(t, s, k.mustCreate(t, s, "C"), model.VersionApproved)
	d := k.to(t, s, k.mustCreate(t, s, "D"), model.VersionDeprecated)
	e1 := k.to(t, s, k.mustCreate(t, s, "E"), model.VersionApproved)
	e2 := k.newVersionOf(t, s, e1)
	for _, q := range []struct {
		name string
		q    listQuery
		want []V
	}{
		{"草稿", listQuery{States: []model.VersionState{model.VersionDraft}}, []V{a, e2}},
		{"待审批或已审批", listQuery{States: []model.VersionState{model.VersionReview, model.VersionApproved}}, []V{b, c}},
		{"已废弃", listQuery{States: []model.VersionState{model.VersionDeprecated}}, []V{d}},
		{"ApprovedOnly 再按已审批过滤", listQuery{ApprovedOnly: true, States: []model.VersionState{model.VersionApproved}}, []V{c, e1}},
		{"ApprovedOnly 再按草稿过滤", listQuery{ApprovedOnly: true, States: []model.VersionState{model.VersionDraft}}, nil},
		// 空的切片与 nil 一样不过滤：第 5 层把 JSON 的空数组解出来就是它，当成「状态在空集合里」就一条都不给了
		{"状态给空切片等于不过滤", listQuery{States: []model.VersionState{}}, []V{a, b, c, d, e2}},
		{"ApprovedOnly 时状态给空切片也不过滤", listQuery{ApprovedOnly: true, States: []model.VersionState{}}, []V{c, e1}},
		// 存不下的状态谁都不是（store 的包说明）：当作没有，不报数据库的编码错误
		{"状态含 NUL", listQuery{States: []model.VersionState{"draft\x00"}}, nil},
		{"状态不是 UTF-8", listQuery{States: []model.VersionState{"draft\xff"}}, nil},
	} {
		wantItems(t, k.listed(t, s, q.q), q.want, q.name)
	}
}

func (k versioned[V]) listDefaultLimit(t *testing.T, s *pgstore.Store) {
	for i := range defaultPageSize + 1 {
		k.mustCreate(t, s, fmt.Sprintf("Base-%03d", i))
	}
	wantDefaultLimit(t, slices.Concat(k.pages(t, s, listQuery{Limit: 500})...), func(cursor string) (store.Page[V], error) {
		return k.list(t.Context(), s, listQuery{Cursor: cursor})
	})
}

func (k versioned[V]) listInvalid(t *testing.T, s *pgstore.Store) {
	k.mustCreate(t, s, "Base-1")
	for _, q := range []listQuery{{Limit: 501}, {Limit: -1}, {Cursor: "\xff"}, {Cursor: "这不是游标"}} {
		_, err := k.list(t.Context(), s, q)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("Limit %d、Cursor %q", q.Limit, q.Cursor))
	}
}
