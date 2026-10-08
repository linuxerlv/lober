package pgstore

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// Test契约ISO 是 ISO 这一组（C2、C4、C5、C26）：新建、第 4 层导入用的系统更新、废弃、删除与查询。删除被任务引用的
// ISO 写在这里（isoDeleteReferenced），要先建任务，由任务那一组跑。
func Test契约ISO(t *testing.T) {
	runCases(t, []testCase{
		{"新建：生成的 ID 合规，State 为 acquiring，Rev 为 1", isoCreate},
		{"新建：族与系对不上报 ErrInvalid", isoCreateBadSeries},
		{"新建：名称存不下报 ErrInvalid", isoCreateBadName},
		{"新建：只填 Name、Family、Series，多填了报 ErrInvalid", isoCreateExtraFields},
		{"系统更新：不写审计，Rev 加 1，读回来与写进去的相同", isoUpdate},
		{"系统更新：把字段改回零值能存下", isoUpdateToZero},
		{"系统更新：改 Family、Series、Name 报 ErrInvalid", isoUpdateFixedFields},
		{"系统更新：Rev 对不上报 ErrConflict，没有的报 ErrNotFound", isoUpdateConflict},
		{"系统更新：置为 ready 时 ISO.Validate 的问题原样带出", isoUpdateReadyInvalid},
		{"系统更新：已是 ready 的改成不合法的记录，同样带出 ISO.Validate 的问题", isoUpdateStillReadyInvalid},
		{"系统更新：文本存不下报 ErrInvalid，不论置为什么状态", isoUpdateUnstorable},
		{"系统更新：不能置为 deprecated，状态不认识的报 ErrInvalid", isoUpdateBadState},
		{"系统更新：已废弃的不能再改，报 ErrImmutable", isoUpdateDeprecated},
		{"SHA256 撞上未失败的 ISO 报 DuplicateError，对方导入失败之后就能写", isoSHA256},
		{"只有 ready 的能废弃", isoDeprecate},
		{"删除之后读不到；Rev 对不上报 ErrConflict，没有的报 ErrNotFound", isoDelete},
		{"ID 不合格式报 ErrInvalid", isoBadID},
		{"ListISOs 按名称的字节序、再按 ID，翻页与一次全取相同", isoListPaging},
		{"ListISOs 按族、系、状态过滤", isoListFilters},
		{"ListISOs 的查询不合规则报 ErrInvalid", isoListInvalid},
		{"Artifacts、Offers 是深拷贝", isoCopies},
	})
}

// rockyISO 是新建 Rocky 的 ISO 的入参。
func rockyISO(name string) model.ISO {
	return model.ISO{Name: name, Family: "linux", Series: "rocky"}
}

// windowsISO 是新建 Windows Server 的 ISO 的入参：它提供 editions 这类选项集合。
func windowsISO(name string) model.ISO {
	return model.ISO{Name: name, Family: "windows", Series: "windows-server"}
}

// imported 是 iso 导入之后的样子：记录合法，可以置为 ready；内容的 SHA256 用 n 区分，Rev 原样带回。
func imported(iso model.ISO, state model.ISOState, n int) model.ISO {
	iso.Version, iso.Arch, iso.State, iso.SHA256 = "9.6", "x86_64", state, sha256Of(n)
	iso.Artifacts = map[string]string{"kernel": "rocky/9.6/x86_64/vmlinuz", "initrd": "rocky/9.6/x86_64/initrd.img"}
	return iso
}

func sha256Of(n int) string { return fmt.Sprintf("%064x", n) }

func mustCreateISO(t *testing.T, s *Store, iso model.ISO) model.ISO {
	t.Helper()
	got, err := s.CreateISO(t.Context(), iso, anAudit("iso.create"))
	must(t, err, "新建 ISO "+iso.Name)
	return got
}

func mustUpdateISO(t *testing.T, s *Store, iso model.ISO) model.ISO {
	t.Helper()
	got, err := s.UpdateISO(t.Context(), iso)
	must(t, err, fmt.Sprintf("把 ISO %s 更新为 %s", iso.Name, iso.State))
	return got
}

func mustGetISO(t *testing.T, s *Store, id string) model.ISO {
	t.Helper()
	got, err := s.GetISO(t.Context(), id)
	must(t, err, "读 ISO "+id)
	return got
}

// mustReadyISO 新建一个 ISO 并经系统更新置为 ready，内容的 SHA256 用 n 区分。
func mustReadyISO(t *testing.T, s *Store, iso model.ISO, n int) model.ISO {
	t.Helper()
	return mustUpdateISO(t, s, imported(mustCreateISO(t, s, iso), model.ISOReady, n))
}

// isoPages 用查询 q 从头翻到尾，返回每一页；游标由它填。
func isoPages(t *testing.T, s *Store, q store.ISOQuery) [][]model.ISO {
	t.Helper()
	return allPages(t, func(cursor string) (store.Page[model.ISO], error) {
		q.Cursor = cursor
		return s.ListISOs(t.Context(), q)
	})
}

func isoCreate(t *testing.T, s *Store) {
	in := model.ISO{Name: "Rocky Linux 9.6 安装盘", Family: "linux", Series: "rocky"}
	got, err := s.CreateISO(t.Context(), in, anAudit("iso.create"))
	must(t, err, "新建")
	if !model.ValidID(got.ID) || !model.IsSafeWord(got.ID) {
		t.Fatalf("生成的 ID %q 应当合 model.ValidID，并且是安全词", got.ID)
	}
	want := in
	want.ID, want.State, want.Rev = got.ID, model.ISOAcquiring, 1
	wantEqual(t, got, want, "新建的返回值")
	wantEqual(t, mustGetISO(t, s, got.ID), want, "读回来的")
	// 名称可以重复，ID 各不相同
	if again := mustCreateISO(t, s, in); again.ID == got.ID {
		t.Fatalf("两次新建得到同一个 ID %q", got.ID)
	}
}

func isoCreateBadSeries(t *testing.T, s *Store) {
	before := snapshot(t, s)
	for _, c := range []struct {
		family catalog.Family
		series catalog.Series
	}{
		{"linux", "windows-server"}, // 系属于别的族
		{"windows", "rocky"},
		{"Linux", "rocky"}, // 族与系都只认原样的写法
		{"linux", "Rocky"},
		{"linux", ""},
		{"", "rocky"},
		{"", ""},
	} {
		_, err := s.CreateISO(t.Context(), model.ISO{Name: "x", Family: c.family, Series: c.series}, anAudit("iso.create"))
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("族 %q、系 %q", c.family, c.series))
	}
	wantUnchanged(t, s, before, "族与系对不上时")
}

func isoCreateBadName(t *testing.T, s *Store) {
	before := snapshot(t, s)
	for _, name := range []string{"a\x00b", "\xff"} {
		_, err := s.CreateISO(t.Context(), rockyISO(name), anAudit("iso.create"))
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("名称 %q", name))
	}
	wantUnchanged(t, s, before, "名称存不下时")
}

// 其余字段由仓储与第 4 层的导入填。调用方填了，多半是把别的记录当成了新建的入参。
func isoCreateExtraFields(t *testing.T, s *Store) {
	before := snapshot(t, s)
	for _, c := range []struct {
		field string
		mod   func(iso *model.ISO)
	}{
		{"ID", func(iso *model.ISO) { iso.ID = "my-iso" }},
		{"Version", func(iso *model.ISO) { iso.Version = "9.6" }},
		{"Arch", func(iso *model.ISO) { iso.Arch = "x86_64" }},
		{"State", func(iso *model.ISO) { iso.State = model.ISOAcquiring }}, // 填的就是新建时的状态也不行
		{"SHA256", func(iso *model.ISO) { iso.SHA256 = sha256Of(1) }},
		{"Artifacts", func(iso *model.ISO) { iso.Artifacts = map[string]string{"kernel": "vmlinuz"} }},
		{"Offers", func(iso *model.ISO) { iso.Offers = map[string][]model.Option{"editions": nil} }},
		{"Rev", func(iso *model.ISO) { iso.Rev = 1 }},
	} {
		in := rockyISO("x")
		c.mod(&in)
		_, err := s.CreateISO(t.Context(), in, anAudit("iso.create"))
		wantIs(t, err, store.ErrInvalid, "新建时填了 "+c.field)
	}
	wantUnchanged(t, s, before, "多填了字段时")
}

func isoUpdate(t *testing.T, s *Store) {
	created := mustCreateISO(t, s, rockyISO("Rocky"))
	audits := auditEntries(t, s)

	in := imported(created, model.ISOImporting, 1)
	got, err := s.UpdateISO(t.Context(), in)
	must(t, err, "置为导入中")
	want := in
	want.Rev = created.Rev + 1
	wantEqual(t, got, want, "系统更新的返回值")
	wantEqual(t, mustGetISO(t, s, created.ID), want, "读回来的")

	in = got
	in.State = model.ISOReady
	got, err = s.UpdateISO(t.Context(), in)
	must(t, err, "置为 ready")
	want = in
	want.Rev = created.Rev + 2
	wantEqual(t, got, want, "置为 ready 的返回值")
	wantEqual(t, mustGetISO(t, s, created.ID), want, "置为 ready 之后读回来的")

	wantItems(t, auditEntries(t, s), audits, "系统更新不写审计")
}

// 重新导入时，第 4 层把导入得到的字段清回零值。先清成空的 map 与切片，再清成 nil：写进去什么就读出什么。
func isoUpdateToZero(t *testing.T, s *Store) {
	full := imported(mustCreateISO(t, s, windowsISO("WS")), model.ISOImporting, 1)
	full.Offers = map[string][]model.Option{"editions": {{Value: "ServerStandard", Label: "标准版"}}}
	iso := mustUpdateISO(t, s, full)
	for _, c := range []struct {
		artifacts map[string]string
		offers    map[string][]model.Option
	}{
		{map[string]string{}, map[string][]model.Option{"editions": {}}},
		{nil, nil},
	} {
		in := model.ISO{
			ID: iso.ID, Name: iso.Name, Family: iso.Family, Series: iso.Series, State: model.ISOAcquiring,
			Artifacts: c.artifacts, Offers: c.offers, Rev: iso.Rev,
		}
		got := mustUpdateISO(t, s, in)
		want := in
		want.Rev = iso.Rev + 1
		wantEqual(t, got, want, "改回零值的返回值")
		wantEqual(t, mustGetISO(t, s, iso.ID), want, "改回零值之后读回来的")
		iso = got
	}
}

// 族与系在新建时声明（C2），导入只核对、不改它们；名称也只在新建时给。
func isoUpdateFixedFields(t *testing.T, s *Store) {
	iso := mustCreateISO(t, s, rockyISO("Rocky"))
	before := snapshot(t, s)
	for _, c := range []struct {
		field string
		mod   func(iso *model.ISO)
	}{
		{"Family", func(iso *model.ISO) { iso.Family, iso.Series = "windows", "windows-server" }},
		{"Series", func(iso *model.ISO) { iso.Series = "almalinux" }},
		{"Name", func(iso *model.ISO) { iso.Name = "Alma" }},
	} {
		in := imported(iso, model.ISOImporting, 1)
		c.mod(&in)
		_, err := s.UpdateISO(t.Context(), in)
		wantIs(t, err, store.ErrInvalid, "改 "+c.field)
	}
	wantUnchanged(t, s, before, "改不能改的字段时")
}

func isoUpdateConflict(t *testing.T, s *Store) {
	stale := mustCreateISO(t, s, rockyISO("Rocky"))
	cur := mustUpdateISO(t, s, imported(stale, model.ISOImporting, 1))
	before := snapshot(t, s)
	for _, rev := range []int64{0, stale.Rev, cur.Rev + 1} {
		in := cur
		in.State = model.ISOFailed
		in.Rev = rev
		_, err := s.UpdateISO(t.Context(), in)
		wantIs(t, err, store.ErrConflict, fmt.Sprintf("带着 Rev %d 更新", rev))
	}
	missing := cur
	missing.ID = "nosuchiso"
	_, err := s.UpdateISO(t.Context(), missing)
	wantIs(t, err, store.ErrNotFound, "更新不存在的 ISO")
	wantUnchanged(t, s, before, "更新被拒时")
}

func isoUpdateReadyInvalid(t *testing.T, s *Store) {
	in := mustCreateISO(t, s, windowsISO("WS"))
	before := snapshot(t, s)
	in.State = model.ISOReady
	in.Version = ".."
	in.Offers = map[string][]model.Option{"Editions": {{Value: "Server Standard", Label: ""}}}
	want := in.Validate()
	if len(want) == 0 {
		t.Fatal("用例的记录应当不合法")
	}
	_, err := s.UpdateISO(t.Context(), in)
	wantProblems(t, err, store.ErrInvalid, want, "置为 ready")
	wantUnchanged(t, s, before, "记录不合法时")

	in.Version, in.Arch = "10.0.26100", "x86_64"
	in.Offers = map[string][]model.Option{"editions": {{Value: "ServerStandard", Label: "Windows Server 2025 Standard"}}}
	if got := mustUpdateISO(t, s, in); got.State != model.ISOReady {
		t.Fatalf("记录合法之后应能置为 ready，得到 %s", got.State)
	}
}

// 状态不变、仍为 ready 的系统更新也要过 ISO.Validate。只在转进 ready 时查，ready 的 ISO 就能带着「..」这种版本
// （它是制品目录里的一段路径，L-6），违反 C5。
func isoUpdateStillReadyInvalid(t *testing.T, s *Store) {
	ready := mustReadyISO(t, s, rockyISO("R"), 1)
	before := snapshot(t, s)
	for _, c := range []struct {
		name string
		mod  func(iso *model.ISO)
	}{
		{"版本改成 ..", func(iso *model.ISO) { iso.Version = ".." }},
		{"架构清空", func(iso *model.ISO) { iso.Arch = "" }},
	} {
		in := ready // 带着当前的 Rev，State 仍为 ready
		c.mod(&in)
		want := in.Validate()
		if len(want) == 0 {
			t.Fatal("用例的记录应当不合法")
		}
		_, err := s.UpdateISO(t.Context(), in)
		wantProblems(t, err, store.ErrInvalid, want, c.name)
	}
	wantUnchanged(t, s, before, "记录不合法时")
}

// 系统更新写进来的文本都要存得下（spec §3 总则）：PostgreSQL 的 text 与 jsonb 都不收 NUL，jsonb 还会把非法的
// UTF-8 换成 U+FFFD，读回来的就不是写进去的。置为 ready 时跑的 ISO.Validate 不看制品与 SHA256，其余状态连它也不跑，
// 所以每一处文本都要查，不论置为什么状态。
func isoUpdateUnstorable(t *testing.T, s *Store) {
	iso := mustCreateISO(t, s, windowsISO("WS"))
	before := snapshot(t, s)
	states := []model.ISOState{model.ISOAcquiring, model.ISOImporting, model.ISOFailed, model.ISOReady}
	// valid 是置为 state 的一次合法的导入结果，下面每次只往其中一处放进存不下的字节
	valid := func(state model.ISOState) model.ISO {
		in := imported(iso, state, 1)
		in.Offers = map[string][]model.Option{"editions": {{Value: "ServerStandard", Label: "标准版"}}}
		return in
	}
	for _, c := range []struct {
		field string
		mod   func(iso *model.ISO, bad string)
	}{
		{"版本", func(iso *model.ISO, bad string) { iso.Version += bad }},
		{"架构", func(iso *model.ISO, bad string) { iso.Arch += catalog.Arch(bad) }},
		{"SHA256", func(iso *model.ISO, bad string) { iso.SHA256 += bad }},
		{"制品的角色", func(iso *model.ISO, bad string) { iso.Artifacts["kernel"+bad] = "rocky/9.6/x86_64/vmlinuz" }},
		{"制品的路径", func(iso *model.ISO, bad string) { iso.Artifacts["kernel"] += bad }},
		{"集合名", func(iso *model.ISO, bad string) { iso.Offers["editions"+bad] = iso.Offers["editions"] }},
		{"选项的值", func(iso *model.ISO, bad string) { iso.Offers["editions"][0].Value += bad }},
		{"选项的显示名", func(iso *model.ISO, bad string) { iso.Offers["editions"][0].Label += bad }},
	} {
		for _, state := range states {
			for _, bad := range []string{"\x00", "\xff"} {
				in := valid(state)
				c.mod(&in, bad)
				_, err := s.UpdateISO(t.Context(), in)
				wantIs(t, err, store.ErrInvalid, fmt.Sprintf("%s：含 %q，置为 %s", c.field, bad, state))
			}
		}
	}
	wantUnchanged(t, s, before, "文本存不下时")
	// 去掉那几个字节，同样的记录在每个状态下都能写：上面的拒绝只来自存不下的文本
	for _, state := range states {
		iso = mustUpdateISO(t, s, valid(state))
	}
}

// 废弃要审计，只能走 DeprecateISO。
func isoUpdateBadState(t *testing.T, s *Store) {
	acquiring := mustCreateISO(t, s, rockyISO("A"))
	ready := mustReadyISO(t, s, rockyISO("R"), 1)
	before := snapshot(t, s)
	for _, iso := range []model.ISO{acquiring, ready} {
		for _, state := range []model.ISOState{model.ISODeprecated, "", "Ready", "done"} {
			in := iso
			in.State = state
			_, err := s.UpdateISO(t.Context(), in)
			wantIs(t, err, store.ErrInvalid, fmt.Sprintf("从 %s 经系统更新置为 %q", iso.State, state))
		}
	}
	wantUnchanged(t, s, before, "状态不对时")
}

// 废弃是要审计的决定，不审计的系统更新不能把它撤掉，所以已废弃的 ISO 不再接受系统更新。
func isoUpdateDeprecated(t *testing.T, s *Store) {
	ready := mustReadyISO(t, s, rockyISO("R"), 1)
	must(t, s.DeprecateISO(t.Context(), ready.ID, ready.Rev, anAudit("iso.deprecate")), "废弃")
	deprecated := mustGetISO(t, s, ready.ID)
	before := snapshot(t, s)
	for _, state := range []model.ISOState{model.ISOReady, model.ISOImporting, model.ISOFailed, model.ISOAcquiring} {
		in := deprecated
		in.State = state
		_, err := s.UpdateISO(t.Context(), in)
		wantIs(t, err, store.ErrImmutable, fmt.Sprintf("把已废弃的 ISO 经系统更新置为 %s", state))
	}
	wantUnchanged(t, s, before, "已废弃时")
}

func isoSHA256(t *testing.T, s *Store) {
	// 只有导入失败的 ISO 不占自己的 SHA256，获取中、导入中、ready 与已废弃的都占着（与 pgstore 的部分唯一索引是同一个
	// 条件）。获取中的也算：内容的 SHA256 可能在字节到齐之前就记下了。另一个 ISO 经系统更新写同样的内容，不论置为
	// 导入中还是 ready，都撞上占着的那一个
	acquiring := mustCreateISO(t, s, rockyISO("acquiring"))
	acquiring.SHA256 = sha256Of(1)
	acquiring = mustUpdateISO(t, s, acquiring)
	importing := mustUpdateISO(t, s, imported(mustCreateISO(t, s, rockyISO("importing")), model.ISOImporting, 2))
	ready := mustReadyISO(t, s, rockyISO("ready"), 3)
	deprecated := mustReadyISO(t, s, rockyISO("deprecated"), 4)
	must(t, s.DeprecateISO(t.Context(), deprecated.ID, deprecated.Rev, anAudit("iso.deprecate")), "废弃")
	b := mustCreateISO(t, s, rockyISO("B"))
	before := snapshot(t, s)
	for i, holder := range []model.ISO{acquiring, importing, ready, deprecated} { // 内容依次是 sha256Of(1) 到 sha256Of(4)
		for _, state := range []model.ISOState{model.ISOImporting, model.ISOReady} {
			_, err := s.UpdateISO(t.Context(), imported(b, state, i+1))
			wantDuplicate(t, err, "sha256", holder.ID, fmt.Sprintf("B 置为 %s 时撞上 %s 的 ISO", state, holder.Name))
		}
	}
	wantUnchanged(t, s, before, "SHA256 撞了时")

	// B 自己置为导入失败、记下撞上的内容，可以写：导入失败的不占这个 SHA256（spec §5.1 第 3 步）
	b = mustUpdateISO(t, s, imported(b, model.ISOFailed, 3))
	// 占着这份内容的那一个（ready 的）导入失败之后，B 就能重新导入它；这时轮到对方撞上 B
	a := mustUpdateISO(t, s, imported(ready, model.ISOFailed, 3))
	b = mustUpdateISO(t, s, imported(b, model.ISOImporting, 3))
	_, err := s.UpdateISO(t.Context(), imported(a, model.ISOReady, 3))
	wantDuplicate(t, err, "sha256", b.ID, "撞上导入中的 B")

	// 空的 SHA256 不占位
	for _, name := range []string{"C", "D"} {
		in := imported(mustCreateISO(t, s, rockyISO(name)), model.ISOImporting, 0)
		in.SHA256 = ""
		mustUpdateISO(t, s, in)
	}
}

func isoDeprecate(t *testing.T, s *Store) {
	acquiring := mustCreateISO(t, s, rockyISO("acquiring"))
	importing := mustUpdateISO(t, s, imported(mustCreateISO(t, s, rockyISO("importing")), model.ISOImporting, 1))
	failed := mustUpdateISO(t, s, imported(mustCreateISO(t, s, rockyISO("failed")), model.ISOFailed, 2))
	ready := mustReadyISO(t, s, rockyISO("ready"), 3)
	before := snapshot(t, s)
	for _, iso := range []model.ISO{acquiring, importing, failed} {
		err := s.DeprecateISO(t.Context(), iso.ID, iso.Rev, anAudit("iso.deprecate"))
		wantIs(t, err, store.ErrConflict, fmt.Sprintf("废弃 %s 的 ISO", iso.State))
	}
	wantIs(t, s.DeprecateISO(t.Context(), ready.ID, ready.Rev+1, anAudit("iso.deprecate")), store.ErrConflict, "Rev 对不上")
	wantIs(t, s.DeprecateISO(t.Context(), "nosuchiso", 1, anAudit("iso.deprecate")), store.ErrNotFound, "废弃不存在的")
	wantUnchanged(t, s, before, "废弃被拒时")

	must(t, s.DeprecateISO(t.Context(), ready.ID, ready.Rev, anAudit("iso.deprecate")), "废弃 ready 的 ISO")
	want := ready
	want.State, want.Rev = model.ISODeprecated, ready.Rev+1
	wantEqual(t, mustGetISO(t, s, ready.ID), want, "废弃之后")
	wantIs(t, s.DeprecateISO(t.Context(), ready.ID, want.Rev, anAudit("iso.deprecate")), store.ErrConflict, "再废弃一次")
}

func isoDelete(t *testing.T, s *Store) {
	a := mustCreateISO(t, s, rockyISO("A"))
	b := mustCreateISO(t, s, rockyISO("B"))
	before := snapshot(t, s)
	wantIs(t, s.DeleteISO(t.Context(), a.ID, a.Rev+1, anAudit("iso.delete")), store.ErrConflict, "Rev 对不上")
	wantIs(t, s.DeleteISO(t.Context(), "nosuchiso", 1, anAudit("iso.delete")), store.ErrNotFound, "删除不存在的")
	wantUnchanged(t, s, before, "删除被拒时")

	must(t, s.DeleteISO(t.Context(), a.ID, a.Rev, anAudit("iso.delete")), "删除")
	_, err := s.GetISO(t.Context(), a.ID)
	wantIs(t, err, store.ErrNotFound, "读删掉的 ISO")
	wantEqual(t, mustGetISO(t, s, b.ID), b, "别的 ISO")
	wantIs(t, s.DeleteISO(t.Context(), a.ID, a.Rev, anAudit("iso.delete")), store.ErrNotFound, "再删一次")

	// 已废弃的也能删；删掉之后，它占着的 SHA256 空出来，同样的内容可以由新的 ISO 导入
	r := mustReadyISO(t, s, rockyISO("R"), 7)
	must(t, s.DeprecateISO(t.Context(), r.ID, r.Rev, anAudit("iso.deprecate")), "废弃")
	must(t, s.DeleteISO(t.Context(), r.ID, r.Rev+1, anAudit("iso.delete")), "删除已废弃的")
	mustReadyISO(t, s, rockyISO("R2"), 7)
}

// 被任务引用的 ISO 只能废弃、不能删除（C26）：废弃之后、任务结束之后也删不掉，已建的任务要一直查得到它。没被引用的照常
// 能删。由任务那一组跑（Test契约任务）。
func isoDeleteReferenced(t *testing.T, s *Store) {
	ctx := t.Context()
	f := newFixture(t, s)
	task := f.mustCreate(t, s, mustTaskHosts(t, s, "SN-1")...)[0]
	iso := mustGetISO(t, s, f.iso.ID)
	before := taskSnapshot(t, s)
	wantIs(t, s.DeleteISO(ctx, iso.ID, iso.Rev, anAudit("iso.delete")), store.ErrReferenced, "删除被未结束任务引用的 ISO")
	wantTasksUnchanged(t, s, before, "删除被拒时")
	must(t, s.DeprecateISO(ctx, iso.ID, iso.Rev, anAudit("iso.deprecate")), "废弃被引用的 ISO")
	mustTransition(t, s, task, model.TaskDone)
	iso = mustGetISO(t, s, iso.ID)
	before = taskSnapshot(t, s)
	wantIs(t, s.DeleteISO(ctx, iso.ID, iso.Rev, anAudit("iso.delete")), store.ErrReferenced, "删除已废弃、任务都已结束的 ISO")
	wantTasksUnchanged(t, s, before, "删除被拒时")
	other := mustCreateISO(t, s, rockyISO("Other"))
	must(t, s.DeleteISO(ctx, other.ID, other.Rev, anAudit("iso.delete")), "删除没被引用的 ISO")
}

func isoBadID(t *testing.T, s *Store) {
	mustCreateISO(t, s, rockyISO("A"))
	before := snapshot(t, s)
	for _, id := range []string{"", "Bad", "a@1", "-a", "a.b", "a/b"} {
		_, err := s.GetISO(t.Context(), id)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("GetISO(%q)", id))
		in := imported(rockyISO("A"), model.ISOImporting, 1)
		in.ID, in.Rev = id, 1
		_, err = s.UpdateISO(t.Context(), in)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("UpdateISO(%q)", id))
		wantIs(t, s.DeprecateISO(t.Context(), id, 1, anAudit("iso.deprecate")), store.ErrInvalid, fmt.Sprintf("DeprecateISO(%q)", id))
		wantIs(t, s.DeleteISO(t.Context(), id, 1, anAudit("iso.delete")), store.ErrInvalid, fmt.Sprintf("DeleteISO(%q)", id))
	}
	wantUnchanged(t, s, before, "ID 不合格式时")
}

func isoListPaging(t *testing.T, s *Store) {
	// 名称按字节序：大写在小写之前，中文在 ASCII 之后；同名的再按 ID，所以页的边界会落在同名的几个中间
	var want []model.ISO
	for _, name := range []string{"Rocky 9.6", "Debian 13", "debian 13", "Debian 13", "中文名", "Alma 9", "Debian 13"} {
		want = append(want, mustCreateISO(t, s, rockyISO(name)))
	}
	slices.SortFunc(want, func(a, b model.ISO) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
	})
	whole, err := s.ListISOs(t.Context(), store.ISOQuery{Limit: len(want)})
	must(t, err, "一次全取")
	wantItems(t, whole.Items, want, "一次全取")
	if whole.Next != "" {
		t.Fatalf("恰好取完时 Next 应为空，得到 %q", whole.Next)
	}
	for _, limit := range []int{2, 3} {
		wantPaged(t, isoPages(t, s, store.ISOQuery{Limit: limit}), limit, want, fmt.Sprintf("每页 %d 条", limit))
	}
}

func isoListFilters(t *testing.T, s *Store) {
	// 名称以序号开头，按名称排出来就是建的顺序
	acquiring := mustCreateISO(t, s, rockyISO("1 rocky"))
	ready := mustReadyISO(t, s, rockyISO("2 rocky"), 1)
	alma := mustUpdateISO(t, s, imported(mustCreateISO(t, s, model.ISO{Name: "3 alma", Family: "linux", Series: "almalinux"}), model.ISOImporting, 2))
	server := mustReadyISO(t, s, windowsISO("4 server"), 3)
	desktop := mustUpdateISO(t, s, imported(mustCreateISO(t, s, model.ISO{Name: "5 desktop", Family: "windows", Series: "windows-desktop"}), model.ISOFailed, 4))
	esxi := mustReadyISO(t, s, model.ISO{Name: "6 esxi", Family: "esxi", Series: "esxi"}, 5)
	must(t, s.DeprecateISO(t.Context(), esxi.ID, esxi.Rev, anAudit("iso.deprecate")), "废弃")
	esxi = mustGetISO(t, s, esxi.ID)
	for _, c := range []struct {
		name string
		q    store.ISOQuery
		want []model.ISO
	}{
		{"不过滤", store.ISOQuery{}, []model.ISO{acquiring, ready, alma, server, desktop, esxi}},
		{"按族", store.ISOQuery{Family: "linux"}, []model.ISO{acquiring, ready, alma}},
		{"按系", store.ISOQuery{Series: "rocky"}, []model.ISO{acquiring, ready}},
		{"按一个状态", store.ISOQuery{States: []model.ISOState{model.ISOReady}}, []model.ISO{ready, server}},
		{"按几个状态", store.ISOQuery{States: []model.ISOState{model.ISOFailed, model.ISODeprecated}}, []model.ISO{desktop, esxi}},
		{"族与状态同时给", store.ISOQuery{Family: "linux", States: []model.ISOState{model.ISOReady, model.ISOImporting}}, []model.ISO{ready, alma}},
		{"组合不上的为空", store.ISOQuery{Family: "windows", Series: "rocky"}, nil},
	} {
		c.q.Limit = 1 // 一条一页：翻页时过滤也要对
		wantItems(t, slices.Concat(isoPages(t, s, c.q)...), c.want, c.name)
	}
}

func isoListInvalid(t *testing.T, s *Store) {
	mustCreateISO(t, s, rockyISO("A"))
	for _, q := range []store.ISOQuery{{Limit: 501}, {Limit: -1}, {Cursor: "\xff"}, {Cursor: "这不是游标"}} {
		_, err := s.ListISOs(t.Context(), q)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("Limit %d、Cursor %q", q.Limit, q.Cursor))
	}
}

func isoCopies(t *testing.T, s *Store) {
	iso := mustCreateISO(t, s, windowsISO("WS"))
	importing := func() model.ISO {
		in := imported(iso, model.ISOImporting, 1)
		in.Offers = map[string][]model.Option{"editions": {{Value: "ServerStandard", Label: "标准版"}}}
		return in
	}
	want := importing()
	want.Rev = iso.Rev + 1
	mutate := func(iso model.ISO) {
		iso.Artifacts["kernel"] = "改了"
		iso.Artifacts["extra"] = "多了"
		iso.Offers["editions"][0].Label = "改了"
		iso.Offers["extra"] = nil
	}

	in := importing()
	got, err := s.UpdateISO(t.Context(), in)
	must(t, err, "系统更新")
	mutate(in)
	mutate(got)
	wantEqual(t, mustGetISO(t, s, iso.ID), want, "改了系统更新的入参与返回值之后")

	mutate(mustGetISO(t, s, iso.ID))
	p, err := s.ListISOs(t.Context(), store.ISOQuery{})
	must(t, err, "列出")
	mutate(p.Items[0])
	wantEqual(t, mustGetISO(t, s, iso.ID), want, "改了读到的返回值之后")
}
