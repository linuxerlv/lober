package pgstore

// 契约测试（spec §7.2 第 2 条）：仓储契约的用例按聚合分在 contract_*_test.go 里，每个文件一个以 Test契约 开头的入口，
// 由 go test 直接发现。这个文件放各组共用的部分。
//
// 用例经 store 的接口观察仓储。接口看不到的只有三处，用例直接查列：任务的事件计数 last_seq、身份上分配过的最大版本号
// last_version、模板版本的引用表 template_hook_refs。每个用例 openContract 一个全新的空仓储，互不影响。记录用
// reflect.DeepEqual 比较，nil 与空的 map、切片算不同：写进去什么，就要读出什么。列表的顺序按字节序比较，不随数据库的
// 排序规则变。
//
// 装查询结果的切片与 map 不是写进去的值，一律不为 nil：一页没有条目时 Items 是长度为 0 的切片，GetHosts 一台都
// 没取到时是空 map。第 5 层编成 JSON 就是 [] 与 {}；nil 会编成 null，与 OpenAPI 描述的数组、对象对不上（前端的
// 类型由它生成，spec §6.2 第 10 条）。

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// testCase 是一个用例，跑在一个全新的空仓储上。
type testCase struct {
	name string
	run  func(t *testing.T, s *Store)
}

// runCases 把每个用例作为子测试依次跑，各自 openContract 一个仓储。
func runCases(t *testing.T, cases []testCase) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, openContract(t)) })
	}
}

// at0 是用例里审计的时间。仓储不读时钟，时间都由调用方给，用例也就不依赖时钟。
const at0 int64 = 1_790_000_000

// anAudit 是只关心「给了审计」时用的一条合规则的审计。
func anAudit(action string) store.Audit {
	return store.Audit{Actor: "alice", Action: action, At: at0}
}

// sentinels 是契约的全部哨兵错误。
var sentinels = []error{
	store.ErrNotFound, store.ErrConflict, store.ErrDuplicate, store.ErrImmutable, store.ErrReferenced,
	store.ErrAuditRequired, store.ErrInvalid, store.ErrRejected, store.ErrBuiltinMismatch,
}

func must(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s：%v", what, err)
	}
}

// wantIs 要求 err 属于 want 这一类（errors.Is）。
func wantIs(t *testing.T, err, want error, what string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s：应报 %v，得到 %v", what, want, err)
	}
}

// wantProblems 要求 err 是包着 kind 的 ProblemsError，问题与 want 完全相同，顺序也相同。
func wantProblems(t *testing.T, err, kind error, want []model.Problem, what string) {
	t.Helper()
	var pe *store.ProblemsError
	switch {
	case !errors.As(err, &pe):
		t.Fatalf("%s：应报 ProblemsError(%v)，得到 %v", what, kind, err)
	case pe.Kind != kind:
		t.Fatalf("%s：ProblemsError 包着 %v，应为 %v", what, pe.Kind, kind)
	case !reflect.DeepEqual(pe.Problems, want):
		t.Fatalf("%s：问题是\n%v\n应为\n%v", what, pe.Problems, want)
	}
}

// wantDuplicate 要求 err 是 DuplicateError：撞上的键是 field，已有的那一个是 existing。
func wantDuplicate(t *testing.T, err error, field, existing, what string) {
	t.Helper()
	var de *store.DuplicateError
	switch {
	case !errors.As(err, &de):
		t.Fatalf("%s：应报 DuplicateError，得到 %v", what, err)
	case de.Field != field || de.Existing != existing:
		t.Fatalf("%s：撞上的是 %s %q，应为 %s %q", what, de.Field, de.Existing, field, existing)
	}
}

func wantEqual[T any](t *testing.T, got, want T, what string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s：\n得到 %+v\n应为 %+v", what, got, want)
	}
}

// wantItems 比较列表的结果。空的时候 nil 与长度为 0 的切片都算对：传进来的多是用例自己拼接或提取出来的切片
// （slices.Concat、snsOf），是不是 nil 由用例的写法决定，与仓储无关。仓储给的 Items 不为 nil，由 pagesFrom 查。
func wantItems[T any](t *testing.T, got, want []T, what string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	wantEqual(t, got, want, what)
}

// maxPages 是翻页最多翻的页数：游标出了错、Next 一直不为空时，用例失败，而不是卡死。
const maxPages = 1000

// pagesFrom 从游标 cursor 翻到尾，返回每一页的条目；list 用给定的游标取一页。每一页的 Items 都不能是 nil，没有
// 条目的一页也是长度为 0 的切片（见包说明）：经它翻页的用例都顺带查了这一条。
func pagesFrom[T any](t *testing.T, cursor string, list func(cursor string) (store.Page[T], error)) [][]T {
	t.Helper()
	var pages [][]T
	for {
		p, err := list(cursor)
		must(t, err, "翻页")
		if p.Items == nil {
			t.Fatalf("翻页：第 %d 页的 Items 是 nil，没有条目时应是长度为 0 的切片（第 5 层编成 JSON 是 []，不是 null）", len(pages)+1)
		}
		pages = append(pages, p.Items)
		if p.Next == "" {
			return pages
		}
		if len(pages) == maxPages {
			t.Fatalf("翻了 %d 页还没到头", maxPages)
		}
		cursor = p.Next
	}
}

// allPages 从头翻到尾。
func allPages[T any](t *testing.T, list func(cursor string) (store.Page[T], error)) [][]T {
	t.Helper()
	return pagesFrom(t, "", list)
}

// wantPaged 断言按 limit 翻出的 pages：除了最后一页，每页恰好 limit 条；条目恰好是 limit 的整数倍时，最后一页也是
// 满的，Next 为空，不多给一个空页；拼起来与 want 相同。
func wantPaged[T any](t *testing.T, pages [][]T, limit int, want []T, what string) {
	t.Helper()
	if n := max(1, (len(want)+limit-1)/limit); len(pages) != n {
		t.Fatalf("%s：%d 条按每页 %d 条应翻 %d 页，翻了 %d 页", what, len(want), limit, n, len(pages))
	}
	for i, p := range pages[:len(pages)-1] {
		if len(p) != limit {
			t.Fatalf("%s：第 %d 页有 %d 条，应为 %d 条", what, i+1, len(p), limit)
		}
	}
	wantItems(t, slices.Concat(pages...), want, what)
}

// contents 是经接口看得见的全部记录，用来断言一次被拒绝或失败的写入什么都没改。
type contents struct {
	Hosts []model.Host
	ISOs  []model.ISO
	Audit []store.AuditEntry
}

func snapshot(t *testing.T, s *Store) contents {
	t.Helper()
	return contents{
		Hosts: slices.Concat(hostPages(t, s, store.HostQuery{Limit: 500})...),
		ISOs:  slices.Concat(isoPages(t, s, store.ISOQuery{Limit: 500})...),
		Audit: auditEntries(t, s),
	}
}

// wantUnchanged 要求仓储里的记录与 before 相同：动作没有落库，审计也没有写。
func wantUnchanged(t *testing.T, s *Store, before contents, what string) {
	t.Helper()
	if after := snapshot(t, s); !reflect.DeepEqual(after, before) {
		t.Fatalf("%s仓储不应有变化：\n之前 %+v\n之后 %+v", what, before, after)
	}
}
