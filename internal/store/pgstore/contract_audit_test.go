package pgstore

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// Test契约审计 是审计这一组（spec §6.2 第 8 条、§7.2 第 2 条）：要审计的方法与审计写在同一个事务里，以及审计的查询。
// 这里覆盖主机与 ISO 的方法；hook、模板与任务的方法由它们各自的分组覆盖。
func Test契约审计(t *testing.T) {
	t.Run("零值审计报 ErrAuditRequired，什么都不写", func(t *testing.T) {
		eachAudited(t, func(t *testing.T, s *Store, call auditedCall) {
			before := snapshot(t, s)
			_, err := call(store.Audit{})
			wantIs(t, err, store.ErrAuditRequired, "零值审计")
			wantUnchanged(t, s, before, "零值审计时")
			_, err = call(anAudit("test.retry"))
			must(t, err, "补上审计之后")
		})
	})
	t.Run("审计不合法报 ErrInvalid，什么都不写", func(t *testing.T) {
		eachAudited(t, func(t *testing.T, s *Store, call auditedCall) {
			before := snapshot(t, s)
			for _, a := range []store.Audit{
				{Actor: "alice", Action: "import", At: at0}, // Action 不到两段
				{Action: "host.import", At: at0},            // 没有操作者
				{Actor: "alice", Action: "host.import"},     // 没有时间
			} {
				_, err := call(a)
				wantIs(t, err, store.ErrInvalid, fmt.Sprintf("审计 %+v", a))
			}
			wantUnchanged(t, s, before, "审计不合法时")
			_, err := call(anAudit("test.retry"))
			must(t, err, "改对审计之后")
		})
	})
	t.Run("审计写失败时动作也不落库", func(t *testing.T) {
		eachAudited(t, func(t *testing.T, s *Store, call auditedCall) {
			before := snapshot(t, s)
			_, err := call(store.Audit{Actor: faultActor, Action: "test.fault", At: at0})
			if err == nil {
				t.Fatal("审计写失败时应报错")
			}
			// 失败要来自审计的写入，而不是动作之前的检查：契约里的错误类别一个都不应是
			for _, sentinel := range sentinels {
				if errors.Is(err, sentinel) {
					t.Fatalf("审计写失败时报了 %v：失败应来自审计的写入", err)
				}
			}
			wantUnchanged(t, s, before, "审计写失败时")
			// 同样的调用换一个操作者就成功：失败只来自审计
			_, err = call(anAudit("test.retry"))
			must(t, err, "换一个操作者之后")
		})
	})
	t.Run("成功时每个对象写一条审计，对象由仓储填", func(t *testing.T) {
		eachAudited(t, func(t *testing.T, s *Store, call auditedCall) {
			before := auditEntries(t, s)
			a := store.Audit{Actor: "张三", Action: "test.audited", Detail: "理由：换机房", At: at0 + 1}
			targets, err := call(a)
			must(t, err, "调用")
			after := auditEntries(t, s)
			if len(after) != len(before)+len(targets) {
				t.Fatalf("应新写 %d 条审计，审计从 %d 条变成了 %d 条", len(targets), len(before), len(after))
			}
			wantIDsDescending(t, after)
			wantItems(t, after[len(targets):], before, "原有的审计")
			added := slices.Clone(after[:len(targets)])
			slices.Reverse(added) // 按写入的顺序：一批里的对象按入参的顺序
			for i, e := range added {
				if e.Audit != a || e.TargetKind != targets[i].kind || e.TargetID != targets[i].id {
					t.Fatalf("第 %d 条审计是 %+v，应为 %+v、对象 %s %s", i+1, e, a, targets[i].kind, targets[i].id)
				}
			}
		})
	})
	runCases(t, []testCase{
		{"ListAudit 新的在前，各种过滤都对", auditListFilters},
		{"ListAudit 翻页与一次全取相同，翻页中途写的不插进后面的页", auditListPaging},
		{"ListAudit 的查询不合规则报 ErrInvalid", auditListInvalid},
	})
}

// target 是一条审计的对象。
type target struct{ kind, id string }

// auditedCall 带着审计 a 调用一个要审计的方法。成功时返回应写下审计的对象，按写入的顺序。
type auditedCall func(a store.Audit) ([]target, error)

// auditedOps 是本组覆盖的要审计的方法。prepare 在仓储里备好方法要的记录，返回调用它的函数；同一个函数可以调用
// 多次，被拒绝或失败的调用什么都不改，之后照样能成功。
var auditedOps = []struct {
	name    string
	prepare func(t *testing.T, s *Store) auditedCall
}{
	{"CreateHost", func(t *testing.T, s *Store) auditedCall {
		return func(a store.Audit) ([]target, error) {
			_, err := s.CreateHost(t.Context(), fullHost("SN-NEW"), a)
			return []target{{"host", "SN-NEW"}}, err
		}
	}},
	{"UpdateHost", func(t *testing.T, s *Store) auditedCall {
		h := mustCreateHost(t, s, fullHost("SN-1"))
		h.Arch = "aarch64"
		return func(a store.Audit) ([]target, error) {
			_, err := s.UpdateHost(t.Context(), h, a)
			return []target{{"host", "SN-1"}}, err
		}
	}},
	{"ImportHosts", func(t *testing.T, s *Store) auditedCall {
		mustCreateHost(t, s, fullHost("SN-1"))
		return func(a store.Audit) ([]target, error) {
			_, err := s.ImportHosts(t.Context(), []model.Host{{SN: "SN-2"}, {SN: "SN-1"}}, a)
			return []target{{"host", "SN-2"}, {"host", "SN-1"}}, err
		}
	}},
	{"DeleteHost", func(t *testing.T, s *Store) auditedCall {
		h := mustCreateHost(t, s, fullHost("SN-1"))
		return func(a store.Audit) ([]target, error) {
			return []target{{"host", "SN-1"}}, s.DeleteHost(t.Context(), "SN-1", h.Rev, a)
		}
	}},
	{"CreateISO", func(t *testing.T, s *Store) auditedCall {
		return func(a store.Audit) ([]target, error) {
			iso, err := s.CreateISO(t.Context(), rockyISO("Rocky"), a)
			return []target{{"iso", iso.ID}}, err
		}
	}},
	{"DeprecateISO", func(t *testing.T, s *Store) auditedCall {
		iso := mustReadyISO(t, s, rockyISO("Rocky"), 1)
		return func(a store.Audit) ([]target, error) {
			return []target{{"iso", iso.ID}}, s.DeprecateISO(t.Context(), iso.ID, iso.Rev, a)
		}
	}},
	{"DeleteISO", func(t *testing.T, s *Store) auditedCall {
		iso := mustCreateISO(t, s, rockyISO("Rocky"))
		return func(a store.Audit) ([]target, error) {
			return []target{{"iso", iso.ID}}, s.DeleteISO(t.Context(), iso.ID, iso.Rev, a)
		}
	}},
}

// eachAudited 给每个要审计的方法 openContract 一个仓储、备好记录，再交给 check。
func eachAudited(t *testing.T, check func(t *testing.T, s *Store, call auditedCall)) {
	for _, op := range auditedOps {
		t.Run(op.name, func(t *testing.T) {
			s := openContract(t)
			check(t, s, op.prepare(t, s))
		})
	}
}

// auditPages 用查询 q 从头翻到尾，返回每一页；游标由它填。
func auditPages(t *testing.T, s *Store, q store.AuditQuery) [][]store.AuditEntry {
	t.Helper()
	return allPages(t, func(cursor string) (store.Page[store.AuditEntry], error) {
		q.Cursor = cursor
		return s.ListAudit(t.Context(), q)
	})
}

// auditEntries 是全部审计，新的在前。
func auditEntries(t *testing.T, s *Store) []store.AuditEntry {
	t.Helper()
	return slices.Concat(auditPages(t, s, store.AuditQuery{Limit: 500})...)
}

// wantIDsDescending 要求审计的 ID 严格递减：新的在前，ID 递增分配。ID 不一定连续，回滚的写入可能占掉几个号。
func wantIDsDescending(t *testing.T, es []store.AuditEntry) {
	t.Helper()
	for i := 1; i < len(es); i++ {
		if es[i].ID >= es[i-1].ID {
			t.Fatalf("审计应按 ID 倒序，第 %d 条的 ID 是 %d，第 %d 条的是 %d", i, es[i-1].ID, i+1, es[i].ID)
		}
	}
}

func auditListFilters(t *testing.T, s *Store) {
	ctx := t.Context()
	by := func(actor, action string, at int64) store.Audit {
		return store.Audit{Actor: actor, Action: action, At: at}
	}
	written := []store.AuditEntry{ // 下面依次写下的审计，ID 由仓储填
		{Audit: by("alice", "host.create", 100), TargetKind: "host", TargetID: "SN-1"},
		{Audit: by("bob", "host.create", 200), TargetKind: "host", TargetID: "SN-2"},
		{Audit: by("alice", "host.update", 300), TargetKind: "host", TargetID: "SN-1"},
		{Audit: by("bob", "iso.create", 400), TargetKind: "iso"}, // ID 建出来才知道
		{Audit: by("carol", "host.import", 500), TargetKind: "host", TargetID: "SN-2"},
		{Audit: by("carol", "host.import", 500), TargetKind: "host", TargetID: "SN-3"},
		{Audit: by("alice", "iso.deprecate", 600), TargetKind: "iso"},
		{Audit: store.Audit{Actor: "bob", Action: "host.delete", Detail: "退役", At: 700}, TargetKind: "host", TargetID: "SN-3"},
		// 下面两条的操作者、动作与对象，同上面的只差在前缀或大小写：过滤是完全相等，按上面的值过滤时它们都不算
		{Audit: by("bobby", "host.created", 800), TargetKind: "host", TargetID: "SN-10"},
		{Audit: by("Bob", "host.update", 900), TargetKind: "host", TargetID: "SN-10"},
		// 最后写的这条，At 比前面的都小：审计按 ID 倒序，它仍排在最前；时间范围照样按 At 选中它
		{Audit: by("alice", "host.delete", 50), TargetKind: "host", TargetID: "SN-10"},
	}
	written[4].Detail, written[5].Detail = "批量", "批量"

	h1, err := s.CreateHost(ctx, model.Host{SN: "SN-1"}, written[0].Audit)
	must(t, err, "登记 SN-1")
	_, err = s.CreateHost(ctx, model.Host{SN: "SN-2"}, written[1].Audit)
	must(t, err, "登记 SN-2")
	h1.Arch = "x86_64"
	_, err = s.UpdateHost(ctx, h1, written[2].Audit)
	must(t, err, "修改 SN-1")
	iso, err := s.CreateISO(ctx, rockyISO("Rocky"), written[3].Audit)
	must(t, err, "新建 ISO")
	written[3].TargetID, written[6].TargetID = iso.ID, iso.ID
	imp, err := s.ImportHosts(ctx, []model.Host{{SN: "SN-2"}, {SN: "SN-3"}}, written[4].Audit)
	must(t, err, "导入")
	iso = mustUpdateISO(t, s, imported(iso, model.ISOReady, 1)) // 系统更新不写审计
	must(t, s.DeprecateISO(ctx, iso.ID, iso.Rev, written[6].Audit), "废弃 ISO")
	must(t, s.DeleteHost(ctx, "SN-3", imp[1].Rev, written[7].Audit), "删除 SN-3")
	h10, err := s.CreateHost(ctx, model.Host{SN: "SN-10"}, written[8].Audit)
	must(t, err, "登记 SN-10")
	h10.Arch = "x86_64"
	h10, err = s.UpdateHost(ctx, h10, written[9].Audit)
	must(t, err, "修改 SN-10")
	must(t, s.DeleteHost(ctx, "SN-10", h10.Rev, written[10].Audit), "删除 SN-10")

	all := auditEntries(t, s)
	wantIDsDescending(t, all)
	stored := slices.Clone(all) // 按写入的顺序，带着仓储分配的 ID
	slices.Reverse(stored)
	withoutIDs := slices.Clone(stored)
	for i := range withoutIDs {
		withoutIDs[i].ID = 0
	}
	wantEqual(t, withoutIDs, written, "按写入的顺序，审计与对象都要对")

	for _, c := range []struct {
		name string
		q    store.AuditQuery
		want []int // written 里的下标
	}{
		{"按对象的类别", store.AuditQuery{TargetKind: "iso"}, []int{3, 6}},
		{"按对象", store.AuditQuery{TargetKind: "host", TargetID: "SN-1"}, []int{0, 2}}, // SN-10 的不算
		{"只给对象的标识", store.AuditQuery{TargetID: "SN-3"}, []int{5, 7}},
		{"按操作者", store.AuditQuery{Actor: "bob"}, []int{1, 3, 7}}, // bobby 与 Bob 的不算
		{"操作者区分大小写", store.AuditQuery{Actor: "Bob"}, []int{9}},
		{"按动作", store.AuditQuery{Action: "host.create"}, []int{0, 1}}, // host.created 的不算
		{"时间范围含两端", store.AuditQuery{From: 300, To: 500}, []int{2, 3, 4, 5}},
		{"只给起点", store.AuditQuery{From: 600}, []int{6, 7, 8, 9}},
		{"只给终点：后写的那条 At 最小，仍按 ID 排在最前", store.AuditQuery{To: 200}, []int{0, 1, 10}},
		{"几个条件同时给", store.AuditQuery{Actor: "alice", TargetKind: "host"}, []int{0, 2, 10}},
		{"没有匹配的", store.AuditQuery{Actor: "dave"}, nil},
		// 完全相等：只给前缀、大小写不同、带 LIKE 的通配符，都匹配不上
		{"操作者只给前缀", store.AuditQuery{Actor: "bo"}, nil},
		{"动作只给前缀", store.AuditQuery{Action: "host"}, nil},
		{"对象的标识大小写不同", store.AuditQuery{TargetID: "sn-1"}, nil},
		{"操作者里的 _ 按原样比", store.AuditQuery{Actor: "b_b"}, nil},
		{"对象的标识里的 % 按原样比", store.AuditQuery{TargetID: "SN-%"}, nil},
	} {
		var want []store.AuditEntry
		for _, i := range slices.Backward(c.want) { // 新的在前
			want = append(want, stored[i])
		}
		c.q.Limit = 1 // 一条一页：翻页时过滤也要对
		wantItems(t, slices.Concat(auditPages(t, s, c.q)...), want, c.name)
	}
}

func auditListPaging(t *testing.T, s *Store) {
	var in []model.Host
	for i := range 7 {
		in = append(in, model.Host{SN: fmt.Sprintf("SN-%d", i)})
	}
	mustImport(t, s, in) // 每台一条，共 7 条
	whole, err := s.ListAudit(t.Context(), store.AuditQuery{Limit: 7})
	must(t, err, "一次全取")
	if len(whole.Items) != 7 || whole.Next != "" {
		t.Fatalf("应一次取完 7 条，Next 为空；得到 %d 条，Next %q", len(whole.Items), whole.Next)
	}
	for _, limit := range []int{2, 3} {
		wantPaged(t, auditPages(t, s, store.AuditQuery{Limit: limit}), limit, whole.Items, fmt.Sprintf("每页 %d 条", limit))
	}

	// 翻到一半又写了新的审计：新的在前，不会插进后面的页里，后面的页照旧。它的 At 比已有的都小，顺序也只看 ID
	first, err := s.ListAudit(t.Context(), store.AuditQuery{Limit: 3})
	must(t, err, "取第 1 页")
	_, err = s.CreateHost(t.Context(), model.Host{SN: "SN-NEW"}, store.Audit{Actor: "alice", Action: "host.create", At: at0 - 1})
	must(t, err, "翻页中途登记 SN-NEW")
	rest := slices.Concat(pagesFrom(t, first.Next, func(cursor string) (store.Page[store.AuditEntry], error) {
		return s.ListAudit(t.Context(), store.AuditQuery{Cursor: cursor, Limit: 3})
	})...)
	wantItems(t, slices.Concat(first.Items, rest), whole.Items, "翻页中途写了新审计")
}

func auditListInvalid(t *testing.T, s *Store) {
	mustCreateHost(t, s, fullHost("SN-1"))
	for _, q := range []store.AuditQuery{{Limit: 501}, {Limit: -1}, {Cursor: "\xff"}, {Cursor: "这不是游标"}} {
		_, err := s.ListAudit(t.Context(), q)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("Limit %d、Cursor %q", q.Limit, q.Cursor))
	}
}
