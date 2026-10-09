package pgstore_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
)

// hookRefsManyBatches 覆盖模板引用的 hook 版本多于一批（batchSize）时的三处分批，都按 store.HookRefs 的顺序切：保存模板时
// 往 template_hook_refs 写引用（insertRefs），审批模板时给引用的 hook 版本加读锁并取出（ApproveTemplate），建任务时同样
// 加锁取出（admit）。要走到第二批，模板就得引用 batchSize 个以上不同的 hook 版本，所以夹具大，三条路径共用一份。
// 它挂在 Test契约模板 的 runCases 里，跟别的用例共用并发位，不另占一个。
//
// 三处都只证到「第二批里的引用取得出来」。加锁那一半（第二批也带 FOR SHARE）另有用例覆盖：
// Test审批模板等引用的hook版本提交之后再判定、Test建任务等锁住的记录提交之后再重跑准入。那两例只用一个 hook，
// 走的是同一个循环、每批同一个锁子句。

// tinyHook 是能通过激活判定、又能放进 sampleTemplate 那个片段插槽的最小 hook：一份空的片段实现，覆盖 sampleTemplate 勾的
// 两个系，没有参数，实例也就不必给取值来源。片段的正文可以为空，是有意放行的空操作实现（activation 的 checkFragment）。
func tinyHook(name string) model.Hook {
	return model.Hook{Name: name, Author: "alice", Impls: []model.Impl{
		{Family: "linux", Series: []catalog.Series{"rocky", "almalinux"}, Kind: model.ImplFragment},
	}}
}

// 建 batchSize+1 个 hook，模板的插槽里放 batchSize+1 个实例，各引用一个 hook 的版本 1。引用按 ID 排（store.HookRefs），ID
// 最大的那个 last 排在第 batchSize+1 位，落在第二批；三处各让 last 带出一个只有第二批走到了才对的结果：
//
//   - 保存：last 还是草稿时删它，要报 ErrReferenced。只写了第一批的引用，就删得掉。
//   - 审批：last 审批之后，模板要能批。只取了第一批，last 不在判定的入参里，报 hook_ref_missing。
//   - 建任务：先建得上；last 废弃之后，再建要报 template_blocked_by_deprecated（C26）。只取了第一批，准入看不到 last，
//     当它没废弃，任务照样建上。
func hookRefsManyBatches(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	n := pgstore.BatchSizeForTest + 1
	hooks := make([]model.Hook, n)
	for i := range hooks {
		hooks[i] = hookOps.mustCreateFrom(t, s, tinyHook(fmt.Sprintf("H-%04d", i)))
	}
	last := slices.MaxFunc(hooks, func(a, b model.Hook) int { return strings.Compare(a.ID, b.ID) })
	in := sampleTemplate("Many-Refs")
	for _, h := range hooks { // 实例按建 hook 的顺序放：分批按的是 HookRefs 排过的顺序，与实例的顺序无关
		in.Assembly["分区"] = append(in.Assembly["分区"], model.Instance{HookID: h.ID, HookVersion: h.Version})
	}
	tpl := templateOps.mustCreateFrom(t, s, in)
	refs := store.HookRefs(tpl)
	if len(refs) != n || refs[pgstore.BatchSizeForTest] != last.Key() {
		t.Fatalf("夹具不对：模板应引用 %d 个 hook 版本，%s 排在第 %d 位；得到 %d 个", n, store.VersionID(last.ID, 1), n, len(refs))
	}

	// 保存
	wantIs(t, s.DeleteHookDraft(ctx, last.Key(), last.Rev), store.ErrReferenced, "删除被模板引用、引用排在第二批的 hook 草稿")

	// 审批
	for _, h := range hooks {
		hookOps.to(t, s, h, model.VersionApproved)
	}
	tpl = templateOps.to(t, s, tpl, model.VersionReview)
	must(t, s.ApproveTemplate(ctx, model.TemplateKey{ID: tpl.ID, Version: tpl.Version}, tpl.Rev, "bob", anAudit("template.approve")),
		fmt.Sprintf("审批引用了 %d 个已审批 hook 版本的模板", n))
	tpl = templateOps.mustGet(t, s, tpl.ID, tpl.Version)

	// 建任务
	approved, err := s.GetHooks(ctx, refs)
	must(t, err, "读模板引用的 hook 版本")
	f := fixture{iso: mustReadyISO(t, s, rockyISO("Rocky 9.6"), 1), tpl: tpl, hooks: approved}
	hs := mustTaskHosts(t, s, "SN-1", "SN-2")
	f.mustCreate(t, s, hs[0])
	batch := f.newTasks(t, hs[1])
	hookOps.to(t, s, approved[last.Key()], model.VersionDeprecated)
	wantRejected(t, s, batch, []model.Code{"template_blocked_by_deprecated"})
}
