package pgstore_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
)

// Test契约主机 是主机这一组（C6、C7）：登记、修改、CSV 导入、删除与查询。每台主机写一条审计由审计那一组查；删除被
// 任务引用的主机写在这里（hostDeleteReferenced），要先建任务，由任务那一组跑。
func Test契约主机(t *testing.T) {
	runCases(t, []testCase{
		{"登记后读回来与写进去的相同，Rev 为 1", hostCreateReadBack},
		{"登记的记录不合法报 ProblemsError，问题与 model 的校验相同", hostCreateInvalid},
		{"登记时 SN 合法却没规范化报 ErrInvalid", hostCreateNotNormalized},
		{"SN 已登记报 DuplicateError，Existing 是那个 SN", hostCreateDuplicate},
		{"修改是整条替换，Rev 加 1", hostUpdateReplaces},
		{"把架构、固件、属性改成空值能存下", hostUpdateToZero},
		{"修改时 Rev 对不上报 ErrConflict，没登记的报 ErrNotFound", hostUpdateConflict},
		{"修改成不合法的记录报 ProblemsError", hostUpdateInvalid},
		{"修改时 SN 合法却没规范化报 ErrInvalid，不当成没登记的主机", hostUpdateNotNormalized},
		{"导入时已登记的整条替换、不比 Rev，没登记的新建", hostImportMixed},
		{"导入时一条不合法整批不写，逐条列出问题", hostImportInvalid},
		{"导入时有一台的 SN 合法却没规范化，整批报 ErrInvalid、不写", hostImportNotNormalized},
		{"导入时批内 SN 重复报 host_duplicate_in_batch", hostImportDuplicate},
		{"导入空的一批报 ErrInvalid", hostImportEmpty},
		{"一次导入多于一批的主机：分批写入、读回与审计，每一台都对", hostImportManyBatches},
		{"删除之后读不到；Rev 对不上报 ErrConflict，没登记的报 ErrNotFound", hostDelete},
		{"GetHosts 只放存在的，一台都没取到时是空 map", hostGetHosts},
		{"ListHosts 按 SN 的字节序，翻三页以上与一次全取相同", hostListPaging},
		{"ListHosts 的 Limit 为 0 时每页 100 条", hostListDefaultLimit},
		{"ListHosts 按 SN 前缀过滤，_、% 与反斜杠按原样比", hostListPrefix},
		{"ListHosts 按属性过滤，值要完全相等", hostListAttr},
		{"ListHosts 的查询不合规则报 ErrInvalid", hostListInvalid},
		{"存不下的 SN 与过滤条件当作没有：取、删报 ErrNotFound，列表为空，不报数据库的编码错误", hostUnstorableKeys},
		{"返回值与入参都是拷贝", hostCopies},
	})
}

// fullHost 是各字段都填了的一台合法主机。
func fullHost(sn string) model.Host {
	return model.Host{SN: sn, Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{"ip": "10.0.0.3/24", "rack": "A1"}}
}

func mustCreateHost(t *testing.T, s *pgstore.Store, h model.Host) model.Host {
	t.Helper()
	got, err := s.CreateHost(t.Context(), h, anAudit("host.create"))
	must(t, err, "登记主机 "+h.SN)
	return got
}

func mustGetHost(t *testing.T, s *pgstore.Store, sn string) model.Host {
	t.Helper()
	got, err := s.GetHost(t.Context(), sn)
	must(t, err, "读主机 "+sn)
	return got
}

func mustImport(t *testing.T, s *pgstore.Store, hs []model.Host) []model.Host {
	t.Helper()
	got, err := s.ImportHosts(t.Context(), hs, anAudit("host.import"))
	must(t, err, "导入主机")
	return got
}

// hostModelProblems 是 model 对一台主机的校验：先 NormalizeSN，再 Host.Validate。仓储报的问题要与它完全相同。
func hostModelProblems(h model.Host) []model.Problem {
	var ps []model.Problem
	if _, err := model.NormalizeSN(h.SN); err != nil {
		var p model.Problem
		errors.As(err, &p)
		ps = append(ps, p)
	}
	return append(ps, h.Validate()...)
}

// hostPages 用查询 q 从头翻到尾，返回每一页；游标由它填。
func hostPages(t *testing.T, s *pgstore.Store, q store.HostQuery) [][]model.Host {
	t.Helper()
	return allPages(t, func(cursor string) (store.Page[model.Host], error) {
		q.Cursor = cursor
		return s.ListHosts(t.Context(), q)
	})
}

func snsOf(hs []model.Host) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.SN)
	}
	return out
}

func hostCreateReadBack(t *testing.T, s *pgstore.Store) {
	for _, in := range []model.Host{
		fullHost("SN-1"),
		{SN: "SN-2"},                             // 只有 SN：属性是 nil，读回来也是 nil
		{SN: "SN-3", Attrs: map[string]string{}}, // 空的属性读回来也是空的，不是 nil
		{SN: "SN-4", Attrs: map[string]string{"note": "机房 3 楼，\"靠窗\"\t第二排\\"}, Rev: 7}, // 入参的 Rev 不算数
	} {
		got, err := s.CreateHost(t.Context(), in, anAudit("host.create"))
		must(t, err, "登记 "+in.SN)
		want := in
		want.Rev = 1
		wantEqual(t, got, want, "登记 "+in.SN+" 的返回值")
		wantEqual(t, mustGetHost(t, s, in.SN), want, "读回来的 "+in.SN)
	}
}

func hostCreateInvalid(t *testing.T, s *pgstore.Store) {
	before := snapshot(t, s)
	for _, c := range []struct {
		name string
		h    model.Host
	}{
		{"SN 为空", model.Host{}},
		{"SN 中间有连续的空格", model.Host{SN: "SN  1"}},
		{"SN 是占位值", model.Host{SN: "TO BE FILLED BY O.E.M."}},
		{"SN 是没规范化的占位值", model.Host{SN: "To Be Filled By O.E.M."}},
		{"架构不合法", model.Host{SN: "SN-1", Arch: "X86_64"}},
		{"固件不合法", model.Host{SN: "SN-1", Firmware: "efi"}},
		{"属性键不是标识符", model.Host{SN: "SN-1", Attrs: map[string]string{"Rack": "A1"}}},
		{"属性键是保留键", model.Host{SN: "SN-1", Attrs: map[string]string{"arch": "x86_64"}}},
		{"属性值存不下", model.Host{SN: "SN-1", Attrs: map[string]string{"note": "a\x00b"}}},
		{"几处一起不合法", model.Host{Arch: "amd64", Firmware: "pcbios", Attrs: map[string]string{"Rack": "A1", "note": "\xff"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := hostModelProblems(c.h)
			if len(want) == 0 {
				t.Fatal("用例的记录应当不合法")
			}
			_, err := s.CreateHost(t.Context(), c.h, anAudit("host.create"))
			wantProblems(t, err, store.ErrInvalid, want, "登记")
		})
	}
	wantUnchanged(t, s, before, "记录不合法时")
}

// SN 合法却没规范化，是调用方漏了规范化：model 的校验对它没有问题可报，所以只要求 ErrInvalid。
func hostCreateNotNormalized(t *testing.T, s *pgstore.Store) {
	before := snapshot(t, s)
	for _, sn := range []string{"sn-1", " SN-1", "SN-1\n"} {
		_, err := s.CreateHost(t.Context(), model.Host{SN: sn}, anAudit("host.create"))
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("登记 SN %q", sn))
	}
	wantUnchanged(t, s, before, "SN 没规范化时")
}

func hostCreateDuplicate(t *testing.T, s *pgstore.Store) {
	mustCreateHost(t, s, fullHost("SN-1"))
	before := snapshot(t, s)
	_, err := s.CreateHost(t.Context(), model.Host{SN: "SN-1", Arch: "aarch64"}, anAudit("host.create"))
	wantDuplicate(t, err, "sn", "SN-1", "再登记一次")
	wantUnchanged(t, s, before, "SN 重复时")
}

func hostUpdateReplaces(t *testing.T, s *pgstore.Store) {
	created := mustCreateHost(t, s, fullHost("SN-1"))
	in := model.Host{SN: "SN-1", Arch: "aarch64", Firmware: "bios", Attrs: map[string]string{"hostname": "node-1"}, Rev: created.Rev}
	got, err := s.UpdateHost(t.Context(), in, anAudit("host.update"))
	must(t, err, "修改")
	want := in
	want.Rev = created.Rev + 1
	wantEqual(t, got, want, "修改的返回值")
	wantEqual(t, mustGetHost(t, s, "SN-1"), want, "读回来的")

	in = got
	in.Attrs = map[string]string{"hostname": "node-2"}
	got, err = s.UpdateHost(t.Context(), in, anAudit("host.update"))
	must(t, err, "再改一次")
	want = in
	want.Rev = created.Rev + 2
	wantEqual(t, got, want, "再改一次的返回值")
	wantEqual(t, mustGetHost(t, s, "SN-1"), want, "再改一次之后读回来的")
}

// 清空属性、把固件改回空都是正当的写入，不能被当成「没改」丢掉。先改成空的属性，再改成 nil：写进去什么就读出什么。
func hostUpdateToZero(t *testing.T, s *pgstore.Store) {
	h := mustCreateHost(t, s, fullHost("SN-1"))
	for _, attrs := range []map[string]string{{}, nil} {
		in := model.Host{SN: "SN-1", Attrs: attrs, Rev: h.Rev}
		got, err := s.UpdateHost(t.Context(), in, anAudit("host.update"))
		must(t, err, "改成零值")
		want := in
		want.Rev = h.Rev + 1
		wantEqual(t, got, want, "改成零值的返回值")
		wantEqual(t, mustGetHost(t, s, "SN-1"), want, "改成零值之后读回来的")
		h = got
	}
}

func hostUpdateConflict(t *testing.T, s *pgstore.Store) {
	stale := mustCreateHost(t, s, fullHost("SN-1"))
	in := stale
	in.Arch = "aarch64"
	cur, err := s.UpdateHost(t.Context(), in, anAudit("host.update"))
	must(t, err, "修改")
	before := snapshot(t, s)
	for _, rev := range []int64{0, stale.Rev, cur.Rev + 1} {
		in := cur
		in.Firmware = "bios"
		in.Rev = rev
		_, err := s.UpdateHost(t.Context(), in, anAudit("host.update"))
		wantIs(t, err, store.ErrConflict, fmt.Sprintf("带着 Rev %d 修改", rev))
	}
	_, err = s.UpdateHost(t.Context(), model.Host{SN: "SN-2", Rev: 1}, anAudit("host.update"))
	wantIs(t, err, store.ErrNotFound, "修改没登记的主机")
	wantUnchanged(t, s, before, "修改被拒时")
}

func hostUpdateInvalid(t *testing.T, s *pgstore.Store) {
	in := mustCreateHost(t, s, fullHost("SN-1"))
	before := snapshot(t, s)
	in.Firmware = "EFI"
	in.Attrs = map[string]string{"sn": "SN-1"}
	_, err := s.UpdateHost(t.Context(), in, anAudit("host.update"))
	wantProblems(t, err, store.ErrInvalid, hostModelProblems(in), "修改成不合法的记录")
	wantUnchanged(t, s, before, "记录不合法时")
}

// 修改按 SN 找主机。SN 没规范化是调用方漏了规范化，报 ErrInvalid；不能拿它去查、报成 ErrNotFound：这台主机其实
// 登记着。
func hostUpdateNotNormalized(t *testing.T, s *pgstore.Store) {
	h := mustCreateHost(t, s, fullHost("SN-1"))
	before := snapshot(t, s)
	for _, sn := range []string{"sn-1", " SN-1", "SN-1\n"} {
		in := h // 带着当前的 Rev
		in.SN, in.Arch = sn, "aarch64"
		_, err := s.UpdateHost(t.Context(), in, anAudit("host.update"))
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("修改 SN %q", sn))
	}
	wantUnchanged(t, s, before, "SN 没规范化时")
}

func hostImportMixed(t *testing.T, s *pgstore.Store) {
	old := mustCreateHost(t, s, fullHost("SN-1"))
	got, err := s.ImportHosts(t.Context(), []model.Host{
		{SN: "SN-2", Arch: "x86_64"},
		{SN: "SN-1", Attrs: map[string]string{"rack": "B9"}, Rev: old.Rev + 98}, // 已登记的不比 Rev
		{SN: "SN-3"},
	}, anAudit("host.import"))
	must(t, err, "导入")
	want := []model.Host{
		{SN: "SN-2", Arch: "x86_64", Rev: 1},
		{SN: "SN-1", Attrs: map[string]string{"rack": "B9"}, Rev: old.Rev + 1}, // 整条替换：原来登记的架构与固件也清掉了
		{SN: "SN-3", Rev: 1},
	}
	wantEqual(t, got, want, "导入的返回值，顺序同入参")
	read, err := s.GetHosts(t.Context(), []string{"SN-1", "SN-2", "SN-3"})
	must(t, err, "读回")
	wantEqual(t, read, map[string]model.Host{"SN-1": want[1], "SN-2": want[0], "SN-3": want[2]}, "读回来的")
}

func hostImportInvalid(t *testing.T, s *pgstore.Store) {
	mustCreateHost(t, s, fullHost("SN-1"))
	before := snapshot(t, s)
	in := []model.Host{
		{SN: "SN-2"}, // 这一台本身合法，也不写
		{SN: "SN-1", Arch: "X86_64"},
		{SN: "SN-3", Attrs: map[string]string{"Rack": "A1", "sn": "SN-3"}},
		{SN: "NONE"},
	}
	var want []model.Problem
	for _, h := range in {
		want = append(want, hostModelProblems(h)...)
	}
	_, err := s.ImportHosts(t.Context(), in, anAudit("host.import"))
	wantProblems(t, err, store.ErrInvalid, want, "导入有不合法记录的一批")
	wantUnchanged(t, s, before, "一条不合法时")
}

// CSV 导入最容易收到没规范化的 SN。收下来，「sn-1」就成了和「SN-1」不同的另一台主机，同一台机器有了两条登记（C6）。
// 所以一批里只要有一台没规范化，不论它排在第几、是不是已登记的那台的另一种写法，整批都报 ErrInvalid，什么都不写。
func hostImportNotNormalized(t *testing.T, s *pgstore.Store) {
	mustCreateHost(t, s, fullHost("SN-1"))
	before := snapshot(t, s)
	for _, hs := range [][]model.Host{
		{{SN: "SN-2"}, {SN: "sn-1"}}, // 已登记的 SN-1 换了一种写法
		{{SN: " SN-1"}, {SN: "SN-2"}},
		{{SN: "SN-1"}, {SN: "sn-2"}}, // 没登记过的
		{{SN: "SN-1"}, {SN: " SN-2"}, {SN: "SN-3"}},
		{{SN: "SN-2"}, {SN: "SN-3\n"}},
	} {
		_, err := s.ImportHosts(t.Context(), hs, anAudit("host.import"))
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("导入 %q", snsOf(hs)))
	}
	wantUnchanged(t, s, before, "一台的 SN 没规范化时")
}

func hostImportDuplicate(t *testing.T, s *pgstore.Store) {
	before := snapshot(t, s)
	dup := func(sn string) model.Problem {
		return model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: "host", ID: sn})
	}
	// 每个重复的 SN 报一条，按它第一次出现的顺序
	in := []model.Host{{SN: "SN-1"}, {SN: "SN-2"}, {SN: "SN-1"}, {SN: "SN-3"}, {SN: "SN-2"}, {SN: "SN-1"}}
	_, err := s.ImportHosts(t.Context(), in, anAudit("host.import"))
	wantProblems(t, err, store.ErrInvalid, []model.Problem{dup("SN-1"), dup("SN-2")}, "批内 SN 重复")
	// 与记录本身的问题一起时，按入参的顺序：每台先报重复（只在这个 SN 第一次出现时报），再报它自己的问题
	bad := model.Host{SN: "SN-1", Firmware: "efi"}
	_, err = s.ImportHosts(t.Context(), []model.Host{{SN: "SN-1"}, {SN: "SN-2"}, bad}, anAudit("host.import"))
	wantProblems(t, err, store.ErrInvalid, append([]model.Problem{dup("SN-1")}, bad.Validate()...), "重复与不合法一起")
	wantUnchanged(t, s, before, "批内 SN 重复时")
}

func hostImportEmpty(t *testing.T, s *pgstore.Store) {
	before := snapshot(t, s)
	for _, hs := range [][]model.Host{nil, {}} {
		_, err := s.ImportHosts(t.Context(), hs, anAudit("host.import"))
		wantIs(t, err, store.ErrInvalid, "导入空的一批")
	}
	wantUnchanged(t, s, before, "空的一批")
}

// CSV 一次可能导入几千台，多于一条语句写得下的一批（batchSize）：写入、读回 Rev 与写审计都分批做，这里要走到第三批。入参打乱
// 了顺序：写入按加锁的次序分批，读回按入参的顺序分批，两边的批不重合。每三台里有一台已经登记，导入时整条替换、Rev 加 1；其余
// 新建，Rev 为 1。返回值按入参的顺序，每台都要对；GetHosts 一次全部取到；每台恰好一条审计，按入参的顺序写。
func hostImportManyBatches(t *testing.T, s *pgstore.Store) {
	n := 2*pgstore.BatchSizeForTest + 1
	var old, in []model.Host
	for i := range n {
		sn := fmt.Sprintf("SN-%05d", i)
		if i%3 == 0 {
			old = append(old, model.Host{SN: sn, Arch: "aarch64"})
		}
		in = append(in, model.Host{SN: sn, Attrs: map[string]string{"rack": fmt.Sprintf("R%d", i)}})
	}
	_, err := s.ImportHosts(t.Context(), old, anAudit("host.preload"))
	must(t, err, "先登记三分之一")
	r := rand.New(rand.NewPCG(7, 3)) // 种子固定，每次打乱得都一样
	r.Shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
	registered := map[string]bool{}
	for _, h := range old {
		registered[h.SN] = true
	}
	want := make([]model.Host, len(in))
	wantBySN := make(map[string]model.Host, len(in))
	for i, h := range in {
		h.Rev = 1
		if registered[h.SN] {
			h.Rev = 2 // 整条替换：原来登记的架构也清掉了
		}
		want[i], wantBySN[h.SN] = h, h
	}

	got := mustImport(t, s, in)
	if len(got) != n {
		t.Fatalf("导入 %d 台，返回了 %d 台", n, len(got))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("返回值的第 %d 台是 %+v，应为 %+v", i+1, got[i], want[i])
		}
	}
	read, err := s.GetHosts(t.Context(), append(snsOf(in), "SN-MISSING"))
	must(t, err, "一次取回全部")
	if len(read) != n {
		t.Fatalf("GetHosts 应取到 %d 台，取到 %d 台", n, len(read))
	}
	for sn, h := range wantBySN {
		if !reflect.DeepEqual(read[sn], h) {
			t.Fatalf("GetHosts 取到的 %s 是 %+v，应为 %+v", sn, read[sn], h)
		}
	}
	entries := slices.Concat(auditPages(t, s, store.AuditQuery{Action: "host.import", Limit: 500})...)
	var targets []string
	for _, e := range slices.Backward(entries) { // 新的在前，倒过来就是写入的顺序
		if e.TargetKind != "host" {
			t.Fatalf("导入的审计 %+v：对象应是主机", e)
		}
		targets = append(targets, e.TargetID)
	}
	if !slices.Equal(targets, snsOf(in)) {
		t.Fatalf("导入 %d 台应按入参的顺序每台写一条审计，写了 %d 条", n, len(targets))
	}
}

func hostDelete(t *testing.T, s *pgstore.Store) {
	h := mustCreateHost(t, s, fullHost("SN-1"))
	other := mustCreateHost(t, s, fullHost("SN-2"))
	before := snapshot(t, s)
	for _, rev := range []int64{0, h.Rev + 1} {
		err := s.DeleteHost(t.Context(), "SN-1", rev, anAudit("host.delete"))
		wantIs(t, err, store.ErrConflict, fmt.Sprintf("带着 Rev %d 删除", rev))
	}
	wantIs(t, s.DeleteHost(t.Context(), "SN-9", 1, anAudit("host.delete")), store.ErrNotFound, "删除没登记的")
	wantUnchanged(t, s, before, "删除被拒时")

	must(t, s.DeleteHost(t.Context(), "SN-1", h.Rev, anAudit("host.delete")), "删除")
	_, err := s.GetHost(t.Context(), "SN-1")
	wantIs(t, err, store.ErrNotFound, "读删掉的主机")
	wantEqual(t, mustGetHost(t, s, "SN-2"), other, "别的主机")
	wantIs(t, s.DeleteHost(t.Context(), "SN-1", h.Rev, anAudit("host.delete")), store.ErrNotFound, "再删一次")
	// 删掉之后，同一个 SN 能重新登记，Rev 从 1 起
	wantEqual(t, mustCreateHost(t, s, model.Host{SN: "SN-1"}).Rev, 1, "重新登记的 Rev")
}

// 主机被任务引用之后不能删（C26）：已建的任务要一直查得到它的主机，任务结束了也一样。没被引用的主机照常能删。由任务那一组
// 跑（Test契约任务）。
func hostDeleteReferenced(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1", "SN-2")
	task := f.mustCreate(t, s, hs[0])[0]
	before := taskSnapshot(t, s)
	wantIs(t, s.DeleteHost(ctx, "SN-1", hs[0].Rev, anAudit("host.delete")), store.ErrReferenced, "删除有未结束任务的主机")
	wantTasksUnchanged(t, s, before, "删除被拒时")
	mustTransition(t, s, task, model.TaskDone)
	before = taskSnapshot(t, s)
	wantIs(t, s.DeleteHost(ctx, "SN-1", hs[0].Rev, anAudit("host.delete")), store.ErrReferenced, "删除任务都已结束的主机")
	wantTasksUnchanged(t, s, before, "删除被拒时")
	must(t, s.DeleteHost(ctx, "SN-2", hs[1].Rev, anAudit("host.delete")), "删除没被引用的主机")
}

func hostGetHosts(t *testing.T, s *pgstore.Store) {
	a := mustCreateHost(t, s, fullHost("SN-1"))
	b := mustCreateHost(t, s, model.Host{SN: "SN-2"})
	got, err := s.GetHosts(t.Context(), []string{"SN-1", "SN-404", "SN-2", "SN-1"})
	must(t, err, "批量取")
	wantEqual(t, got, map[string]model.Host{"SN-1": a, "SN-2": b}, "批量取的结果")
	// 一台都没取到时是空 map，不是 nil（见包说明）：不取，或者要的都没登记
	for _, sns := range [][]string{nil, {}, {"SN-404"}} {
		m, err := s.GetHosts(t.Context(), sns)
		must(t, err, fmt.Sprintf("批量取 %#v", sns))
		if m == nil || len(m) != 0 {
			t.Fatalf("批量取 %#v：应得到空 map，不是 nil；得到 %#v", sns, m)
		}
	}
	_, err = s.GetHost(t.Context(), "SN-404")
	wantIs(t, err, store.ErrNotFound, "取没登记的主机")
}

func hostListPaging(t *testing.T, s *pgstore.Store) {
	// 这几个 SN 按字节序排，与常见的语言排序规则（忽略标点、不分大小写）排出来不同。顺序以字节序为准，不随
	// 数据库的排序规则变（契约见 store.types.go）
	var in []model.Host
	for _, sn := range []string{"SN_1", "SN-9", "SN 1", "A-1", "SN.2", "SN-10", "SN1"} {
		in = append(in, model.Host{SN: sn})
	}
	want := mustImport(t, s, in)
	slices.SortFunc(want, func(a, b model.Host) int { return strings.Compare(a.SN, b.SN) })

	whole, err := s.ListHosts(t.Context(), store.HostQuery{Limit: len(want)})
	must(t, err, "一次全取")
	wantItems(t, whole.Items, want, "一次全取")
	if whole.Next != "" {
		t.Fatalf("恰好取完时 Next 应为空，得到 %q", whole.Next)
	}
	for _, limit := range []int{2, 3} {
		wantPaged(t, hostPages(t, s, store.HostQuery{Limit: limit}), limit, want, fmt.Sprintf("每页 %d 条", limit))
	}
}

func hostListDefaultLimit(t *testing.T, s *pgstore.Store) {
	var in []model.Host
	for i := range 101 {
		in = append(in, model.Host{SN: fmt.Sprintf("SN-%03d", i)})
	}
	mustImport(t, s, in)
	first, err := s.ListHosts(t.Context(), store.HostQuery{})
	must(t, err, "取第 1 页")
	if len(first.Items) != 100 || first.Next == "" {
		t.Fatalf("Limit 为 0 时应取 100 条并给出下一页，得到 %d 条，Next %q", len(first.Items), first.Next)
	}
	rest, err := s.ListHosts(t.Context(), store.HostQuery{Cursor: first.Next})
	must(t, err, "取第 2 页")
	if !slices.Equal(snsOf(rest.Items), []string{"SN-100"}) || rest.Next != "" {
		t.Fatalf("第 2 页应只有 SN-100，得到 %v，Next %q", snsOf(rest.Items), rest.Next)
	}
}

func hostListPrefix(t *testing.T, s *pgstore.Store) {
	var in []model.Host
	for _, sn := range []string{"SN_A1", "SNXA1", "SN%B", "SNYB", `SN\Z`, "SNAZ", "SN-1", "AB-1"} {
		in = append(in, model.Host{SN: sn})
	}
	mustImport(t, s, in)
	for _, c := range []struct {
		prefix string
		want   []string
	}{
		// _、% 与 \ 都按原样比，不当通配符或转义符
		{"SN_", []string{"SN_A1"}},
		{"SN%", []string{"SN%B"}},
		{`SN\`, []string{`SN\Z`}},
		{"SN", []string{"SN%B", "SN-1", "SNAZ", "SNXA1", "SNYB", `SN\Z`, "SN_A1"}},
		{"SN_A1", []string{"SN_A1"}}, // 整个 SN 也是它自己的前缀
		{"sn", nil},                  // 区分大小写：登记过的 SN 都是规范化之后的大写
		{"", []string{"AB-1", "SN%B", "SN-1", "SNAZ", "SNXA1", "SNYB", `SN\Z`, "SN_A1"}},
	} {
		got := slices.Concat(hostPages(t, s, store.HostQuery{SNPrefix: c.prefix, Limit: 2})...)
		wantItems(t, snsOf(got), c.want, fmt.Sprintf("前缀 %q", c.prefix))
	}
}

func hostListAttr(t *testing.T, s *pgstore.Store) {
	mustImport(t, s, []model.Host{
		{SN: "SN-1", Attrs: map[string]string{"rack": "A1", "role": "web"}},
		{SN: "SN-2", Attrs: map[string]string{"rack": "A10"}},
		{SN: "SN-3", Attrs: map[string]string{"rack": "a1"}},
		{SN: "SN-4", Attrs: map[string]string{"rack": "A1 "}},
		{SN: "SN-5", Attrs: map[string]string{"role": "A1"}}, // 别的键上的同一个值不算
		{SN: "SN-6"},
		{SN: "SN-7", Attrs: map[string]string{"rack": "A1"}},
		{SN: "SN-8", Attrs: map[string]string{"rack": `B"2\`}},
	})
	for _, c := range []struct {
		name string
		q    store.HostQuery
		want []string
	}{
		{"值完全相等", store.HostQuery{AttrKey: "rack", AttrValue: "A1"}, []string{"SN-1", "SN-7"}},
		{"值里的引号与反斜杠按原样比", store.HostQuery{AttrKey: "rack", AttrValue: `B"2\`}, []string{"SN-8"}},
		{"与 SN 前缀同时给", store.HostQuery{SNPrefix: "SN-7", AttrKey: "rack", AttrValue: "A1"}, []string{"SN-7"}},
		{"谁都没有这个属性", store.HostQuery{AttrKey: "nope", AttrValue: "A1"}, nil},
	} {
		c.q.Limit = 1 // 一条一页：翻页时过滤也要对
		wantItems(t, snsOf(slices.Concat(hostPages(t, s, c.q)...)), c.want, c.name)
	}
	for _, q := range []store.HostQuery{{AttrKey: "rack"}, {AttrValue: "A1"}} {
		_, err := s.ListHosts(t.Context(), q)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("属性条件只给了一半：键 %q、值 %q", q.AttrKey, q.AttrValue))
	}
}

func hostListInvalid(t *testing.T, s *pgstore.Store) {
	mustCreateHost(t, s, fullHost("SN-1"))
	for _, q := range []store.HostQuery{{Limit: 501}, {Limit: -1}, {Cursor: "\xff"}, {Cursor: "这不是游标"}} {
		_, err := s.ListHosts(t.Context(), q)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("Limit %d、Cursor %q", q.Limit, q.Cursor))
	}
}

// 存不下的串（含 NUL，或不是合法的 UTF-8）不可能登记过，PostgreSQL 的 text 不收它们：按它取、删报 ErrNotFound，批量取时不放它，
// 按它过滤得到空的一页，都当作没有，不把数据库的编码错误交给调用方（store 的包说明）。另有一台的属性值恰好是 U+FFFD：非法的
// UTF-8 经 encoding/json 就成了它，按 0xFF 过滤不能把它选中。
func hostUnstorableKeys(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	h := mustCreateHost(t, s, fullHost("SN-1"))
	replacement := string(utf8.RuneError) // U+FFFD
	replaced := mustCreateHost(t, s, model.Host{SN: "SN-2", Attrs: map[string]string{"rack": replacement}})
	before := snapshot(t, s)
	for _, bad := range []string{"\x00", "\xff"} {
		for _, sn := range []string{"SN-1" + bad, bad} {
			_, err := s.GetHost(ctx, sn)
			wantIs(t, err, store.ErrNotFound, fmt.Sprintf("GetHost(%q)", sn))
			wantIs(t, s.DeleteHost(ctx, sn, h.Rev, anAudit("host.delete")), store.ErrNotFound, fmt.Sprintf("DeleteHost(%q)", sn))
			got, err := s.GetHosts(ctx, []string{sn, "SN-1"})
			must(t, err, fmt.Sprintf("GetHosts 带着 %q", sn))
			wantEqual(t, got, map[string]model.Host{"SN-1": h}, fmt.Sprintf("GetHosts 带着 %q", sn))
		}
		for _, q := range []store.HostQuery{
			{SNPrefix: "SN" + bad}, {SNPrefix: bad},
			{AttrKey: "rack", AttrValue: bad}, {AttrKey: "rack", AttrValue: "A1" + bad}, {AttrKey: "rack" + bad, AttrValue: "A1"},
		} {
			wantItems(t, slices.Concat(hostPages(t, s, q)...), nil, fmt.Sprintf("ListHosts(%#v)", q))
		}
	}
	wantUnchanged(t, s, before, "按存不下的键取、删与过滤之后")
	// 属性值是 U+FFFD 的那一台照样按它自己的值选得中：上面选不中它，不是因为它没存下
	wantItems(t, slices.Concat(hostPages(t, s, store.HostQuery{AttrKey: "rack", AttrValue: replacement})...), []model.Host{replaced},
		"按 U+FFFD 过滤")
}

func hostCopies(t *testing.T, s *pgstore.Store) {
	want := fullHost("SN-1")
	want.Rev = 1
	mutate := func(h model.Host) {
		h.Attrs["rack"] = "改了"
		h.Attrs["extra"] = "多了"
	}

	in := fullHost("SN-1")
	got, err := s.CreateHost(t.Context(), in, anAudit("host.create"))
	must(t, err, "登记")
	mutate(in)
	mutate(got)
	wantEqual(t, mustGetHost(t, s, "SN-1"), want, "改了登记的入参与返回值之后")

	mutate(mustGetHost(t, s, "SN-1"))
	m, err := s.GetHosts(t.Context(), []string{"SN-1"})
	must(t, err, "批量取")
	mutate(m["SN-1"])
	p, err := s.ListHosts(t.Context(), store.HostQuery{})
	must(t, err, "列出")
	mutate(p.Items[0])
	wantEqual(t, mustGetHost(t, s, "SN-1"), want, "改了读到的返回值之后")

	in = fullHost("SN-1")
	in.Rev = 1
	got, err = s.UpdateHost(t.Context(), in, anAudit("host.update"))
	must(t, err, "修改")
	mutate(in)
	mutate(got)
	want.Rev = 2
	wantEqual(t, mustGetHost(t, s, "SN-1"), want, "改了修改的入参与返回值之后")

	ins := []model.Host{fullHost("SN-1")}
	imported, err := s.ImportHosts(t.Context(), ins, anAudit("host.import"))
	must(t, err, "导入")
	mutate(ins[0])
	mutate(imported[0])
	want.Rev = 3
	wantEqual(t, mustGetHost(t, s, "SN-1"), want, "改了导入的入参与返回值之后")
}
