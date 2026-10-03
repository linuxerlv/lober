package store_test

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/activation"
	"github.com/lober/lober/internal/core/admission"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// wantErr 断言 err 的类别：want 为 nil 时要求通过，否则要求 errors.Is(err, want)。
func wantErr(t *testing.T, err, want error) {
	t.Helper()
	switch {
	case want == nil && err != nil:
		t.Fatalf("应通过，得到 %v", err)
	case want != nil && !errors.Is(err, want):
		t.Fatalf("应报 %v，得到 %v", want, err)
	}
}

func codesOf(ps []model.Problem) []model.Code {
	var out []model.Code
	for _, p := range ps {
		out = append(out, p.Code)
	}
	return out
}

// goodAudit 是合规则的审计，反例都在它上面只改一处。
func goodAudit() store.Audit {
	return store.Audit{Actor: "alice", Action: "host.import", Detail: "导入 3 台", At: 1_790_000_000}
}

func TestCheckAudit零值表示没给审计(t *testing.T) {
	wantErr(t, store.CheckAudit(store.Audit{}), store.ErrAuditRequired)
	// 有一个字段不是零值，就是给了审计、只是给错了：漏声明审计与审计写错是两回事，要分得开。
	wantErr(t, store.CheckAudit(store.Audit{Detail: "理由"}), store.ErrInvalid)
}

func TestCheckAudit各字段的规则(t *testing.T) {
	cases := []struct {
		name string
		mod  func(a *store.Audit)
		want error
	}{
		{"合规则", func(*store.Audit) {}, nil},
		{"Actor 可以用中文", func(a *store.Audit) { a.Actor = "张三" }, nil},
		{"Actor 恰好 128 字节", func(a *store.Audit) { a.Actor = strings.Repeat("a", 128) }, nil},
		{"Actor 超过 128 字节", func(a *store.Audit) { a.Actor = strings.Repeat("a", 129) }, store.ErrInvalid},
		{"Actor 为空", func(a *store.Audit) { a.Actor = "" }, store.ErrInvalid},
		{"Actor 含 NUL", func(a *store.Audit) { a.Actor = "al\x00ice" }, store.ErrInvalid},
		{"Actor 不是 UTF-8", func(a *store.Audit) { a.Actor = "al\xffice" }, store.ErrInvalid},
		{"Detail 可空", func(a *store.Audit) { a.Detail = "" }, nil},
		{"Detail 恰好 4096 字节", func(a *store.Audit) { a.Detail = strings.Repeat("a", 4096) }, nil},
		{"Detail 超过 4096 字节", func(a *store.Audit) { a.Detail = strings.Repeat("a", 4097) }, store.ErrInvalid},
		{"Detail 含 NUL", func(a *store.Audit) { a.Detail = "理由\x00" }, store.ErrInvalid},
		{"At 为 0", func(a *store.Audit) { a.At = 0 }, store.ErrInvalid},
		{"At 为负", func(a *store.Audit) { a.At = -1 }, store.ErrInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := goodAudit()
			c.mod(&a)
			wantErr(t, store.CheckAudit(a), c.want)
		})
	}
}

func TestCheckAuditAction的写法(t *testing.T) {
	longest := "a." + strings.Repeat("b", 62) // 恰好 64 字节
	for _, action := range []string{"host.import", "a.b", "task.retry.manual", "hook_v2.approve_1", longest} {
		a := goodAudit()
		a.Action = action
		if err := store.CheckAudit(a); err != nil {
			t.Errorf("Action %q 应通过，得到 %v", action, err)
		}
	}
	bad := []string{
		"", "host", // 不到两段
		"Host.import", "host.Import", // 大写
		"1host.import", "host.1import", "_host.import", "host._import", // 段首不是小写字母
		"host..import", ".host.import", "host.import.", // 空段
		"host-x.import", " host.import", "host.import ", // 段里有别的字符
		"host.import\n", // 正则的 $ 不能放过末尾的换行
		longest + "b",   // 65 字节
	}
	for _, action := range bad {
		a := goodAudit()
		a.Action = action
		if err := store.CheckAudit(a); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("Action %q 应报 ErrInvalid，得到 %v", action, err)
		}
	}
}

// goodEvent 是合规则的事件，反例都在它上面只改一处。
func goodEvent() store.NewEvent {
	return store.NewEvent{
		At: 1_790_000_000, Kind: "slot.exit", Key: "sig-7",
		Fields: map[string]string{"slot": "装后配置", "code": "0", "tail": ""},
	}
}

func TestCheckEvents各字段的规则(t *testing.T) {
	cases := []struct {
		name string
		mod  func(e *store.NewEvent)
		want error
	}{
		{"合规则", func(*store.NewEvent) {}, nil},
		{"Key 可空", func(e *store.NewEvent) { e.Key = "" }, nil},
		{"Key 恰好 128 字节", func(e *store.NewEvent) { e.Key = strings.Repeat("k", 128) }, nil},
		{"Key 超过 128 字节", func(e *store.NewEvent) { e.Key = strings.Repeat("k", 129) }, store.ErrInvalid},
		{"Key 含空格", func(e *store.NewEvent) { e.Key = "sig 7" }, store.ErrInvalid},
		{"Key 含花括号", func(e *store.NewEvent) { e.Key = "{{sig}}" }, store.ErrInvalid},
		{"Kind 只有一段", func(e *store.NewEvent) { e.Kind = "exit" }, store.ErrInvalid},
		{"Kind 有大写", func(e *store.NewEvent) { e.Kind = "slot.Exit" }, store.ErrInvalid},
		{"Kind 超过 64 字节", func(e *store.NewEvent) { e.Kind = "a." + strings.Repeat("b", 63) }, store.ErrInvalid},
		{"At 为 0", func(e *store.NewEvent) { e.At = 0 }, store.ErrInvalid},
		{"Fields 可空", func(e *store.NewEvent) { e.Fields = nil }, nil},
		{"字段名不是标识符", func(e *store.NewEvent) { e.Fields["Exit-Code"] = "0" }, store.ErrInvalid},
		{"字段名为空", func(e *store.NewEvent) { e.Fields[""] = "0" }, store.ErrInvalid},
		{"字段值含 NUL", func(e *store.NewEvent) { e.Fields["tail"] = "a\x00b" }, store.ErrInvalid},
		{"字段值不是 UTF-8", func(e *store.NewEvent) { e.Fields["tail"] = "\xff" }, store.ErrInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := goodEvent()
			c.mod(&e)
			wantErr(t, store.CheckEvents([]store.NewEvent{e}), c.want)
		})
	}
}

// 64 KiB 是一条事件全部字段的合计，键也算在内：两个字段各自都不大，加起来超了也不行。
func TestCheckEvents字段合计不超过64KiB(t *testing.T) {
	half := strings.Repeat("x", 32*1024-1) // 加上 1 字节的键，恰好 32 KiB
	fields := map[string]string{"a": half, "b": half}
	e := store.NewEvent{At: 1, Kind: "log.tail", Fields: fields}
	wantErr(t, store.CheckEvents([]store.NewEvent{e}), nil)
	fields["b"] += "x"
	wantErr(t, store.CheckEvents([]store.NewEvent{e}), store.ErrInvalid)
}

func TestCheckEvents一条不合规则整批都报错(t *testing.T) {
	wantErr(t, store.CheckEvents(nil), nil) // 不带事件的推进也要查，空的一批合规则
	bad := goodEvent()
	bad.At = 0
	wantErr(t, store.CheckEvents([]store.NewEvent{goodEvent(), bad}), store.ErrInvalid)
	// 同一批里 Key 重复不算不合规则：后一条按 Key 已存在处理（AppendEvents），不能因为它把整批信号都拒掉。
	wantErr(t, store.CheckEvents([]store.NewEvent{goodEvent(), goodEvent()}), nil)
}

func TestCheckQuery的条数(t *testing.T) {
	cases := []struct {
		limit int
		want  int // 0 表示应报 ErrInvalid
	}{
		{0, 100}, {1, 1}, {500, 500}, {501, 0}, {-1, 0},
	}
	for _, c := range cases {
		got, err := store.CheckQuery("", c.limit)
		switch {
		case c.want == 0 && !errors.Is(err, store.ErrInvalid):
			t.Errorf("Limit %d 应报 ErrInvalid，得到 %d、%v", c.limit, got, err)
		case c.want != 0 && (err != nil || got != c.want):
			t.Errorf("Limit %d 应取 %d 条，得到 %d、%v", c.limit, c.want, got, err)
		}
	}
}

// 游标对调用方不透明，由各实现自己编码；共用的检查只挡掉存不下的串。
func TestCheckQuery的游标(t *testing.T) {
	if n, err := store.CheckQuery("opaque-cursor", 0); err != nil || n != 100 {
		t.Errorf("存得下的游标应放过，得到 %d、%v", n, err)
	}
	for _, cursor := range []string{"a\x00b", "\xff"} {
		if _, err := store.CheckQuery(cursor, 10); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("游标 %q 应报 ErrInvalid，得到 %v", cursor, err)
		}
	}
}

// newTask 是一个合规则的待建任务：快照里的主机、ISO 与模板版本和任务字段一致。快照的其余部分这里不看。
func newTask(sn string) model.Task {
	return model.Task{
		HostSN: sn, ISOID: "iso-rocky96", TemplateID: "baseline", TemplateVersion: 4,
		CreatedBy: "alice", CreatedAt: 1_790_000_000,
		Snapshot: model.Snapshot{
			Schema:   model.SnapshotSchema,
			Host:     model.SnapshotHost{SN: sn},
			ISO:      model.SnapshotISO{ID: "iso-rocky96"},
			Template: model.SnapshotTemplate{ID: "baseline", Version: 4},
		},
	}
}

func TestCheckNewTasks合规则的一批通过(t *testing.T) {
	wantErr(t, store.CheckNewTasks([]model.Task{newTask("SN-1")}), nil) // 单任务的批，反例也在它上面造
	wantErr(t, store.CheckNewTasks([]model.Task{newTask("SN-1"), newTask("SN-2")}), nil)
}

func TestCheckNewTasks每条前提各一个反例(t *testing.T) {
	wantErr(t, store.CheckNewTasks(nil), store.ErrInvalid) // 一个任务都没有

	// shape 是造反例的一种批：n 个任务，mod 只用在下标为 at 的那几个上，其余合规则。
	type shape struct {
		name string
		n    int
		at   []int
	}
	single := shape{"单任务的批", 1, []int{0}}
	first := shape{"只改第 1 个", 2, []int{0}}
	second := shape{"只改第 2 个", 2, []int{1}}
	both := shape{"两个都改", 2, []int{0, 1}}
	// 逐任务的前提要查每一个任务，第 1 个也不例外：它是同批比较的基准，自己同样要查；单任务的批（给一台主机建任务，
	// 最常见的情形）里只有它。
	perTask := []shape{single, first, second}
	// ID 格式的三条改的是同一批要相同的字段：两个任务里只改一个，同批比较也会报 ErrInvalid，测不出这一条，所以两个
	// 都改。这时第 2 个任务也报，第 1 个任务查没查，只有单任务的批测得出。
	idFormat := []shape{single, both}
	// 同一批要相同的三条只改第 2 个：单任务的批里没有可比的。
	sameBatch := []shape{second}

	cases := []struct {
		name   string
		mod    func(t *model.Task)
		shapes []shape
	}{
		{"ID 不为空", func(t *model.Task) { t.ID = "abc" }, perTask},
		{"State 不为空", func(t *model.Task) { t.State = model.TaskPending }, perTask},
		{"Rev 不为 0", func(t *model.Task) { t.Rev = 1 }, perTask},
		{"CreatedAt 为 0", func(t *model.Task) { t.CreatedAt = 0 }, perTask},
		{"CreatedAt 为负", func(t *model.Task) { t.CreatedAt = -1 }, perTask},
		{"CreatedBy 为空", func(t *model.Task) { t.CreatedBy = "" }, perTask},
		{"CreatedBy 存不下", func(t *model.Task) { t.CreatedBy = "al\x00ice" }, perTask},
		{"快照格式不对", func(t *model.Task) { t.Snapshot.Schema = model.SnapshotSchema + 1 }, perTask},
		{"HostSN 与快照不一致", func(t *model.Task) { t.Snapshot.Host.SN = "SN-9" }, perTask},
		{"ISOID 与快照不一致", func(t *model.Task) { t.Snapshot.ISO.ID = "iso-other" }, perTask},
		{"TemplateID 与快照不一致", func(t *model.Task) { t.Snapshot.Template.ID = "other" }, perTask},
		{"TemplateVersion 与快照不一致", func(t *model.Task) { t.Snapshot.Template.Version = 5 }, perTask},
		{"同一批的 ISO 不同", func(t *model.Task) { t.ISOID, t.Snapshot.ISO.ID = "iso-other", "iso-other" }, sameBatch},
		{"同一批的模板不同", func(t *model.Task) { t.TemplateID, t.Snapshot.Template.ID = "other", "other" }, sameBatch},
		{"同一批的模板版本不同", func(t *model.Task) { t.TemplateVersion, t.Snapshot.Template.Version = 5, 5 }, sameBatch},
		{"HostSN 没有规范化", func(t *model.Task) { t.HostSN = strings.ToLower(t.HostSN); t.Snapshot.Host.SN = t.HostSN }, perTask},
		{"ISOID 不合 ID 的格式", func(t *model.Task) { t.ISOID, t.Snapshot.ISO.ID = "ISO 1", "ISO 1" }, idFormat},
		{"TemplateID 不合 ID 的格式", func(t *model.Task) { t.TemplateID, t.Snapshot.Template.ID = "tpl@4", "tpl@4" }, idFormat},
		{"TemplateVersion 不到 1", func(t *model.Task) { t.TemplateVersion, t.Snapshot.Template.Version = 0, 0 }, idFormat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, s := range c.shapes {
				t.Run(s.name, func(t *testing.T) {
					ts := []model.Task{newTask("SN-1"), newTask("SN-2")}[:s.n]
					for _, i := range s.at {
						c.mod(&ts[i])
					}
					wantErr(t, store.CheckNewTasks(ts), store.ErrInvalid)
				})
			}
		})
	}
}

// SN 重复能用现有代码表达：每个重复的 SN 报一条 host_duplicate_in_batch，按它在批里第一次出现的顺序。
func TestCheckNewTasks批里SN重复(t *testing.T) {
	ts := []model.Task{newTask("SN-2"), newTask("SN-1"), newTask("SN-1"), newTask("SN-3"), newTask("SN-2")}
	err := store.CheckNewTasks(ts)
	var pe *store.ProblemsError
	if !errors.Is(err, store.ErrInvalid) || !errors.As(err, &pe) {
		t.Fatalf("应报 ProblemsError(ErrInvalid)，得到 %v", err)
	}
	want := []model.Problem{
		model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: "host", ID: "SN-2"}),
		model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: "host", ID: "SN-1"}),
	}
	if !reflect.DeepEqual(pe.Problems, want) {
		t.Errorf("问题 = %v\n期望 %v", pe.Problems, want)
	}
}

func TestHookRefs去重并按ID与版本排序(t *testing.T) {
	tpl := model.Template{Assembly: map[string][]model.Instance{
		"装后配置": {{HookID: "nic", HookVersion: 2}, {HookID: "lvm", HookVersion: 10}},
		"分区":   {{HookID: "lvm", HookVersion: 2}, {HookID: "nic", HookVersion: 2}},
		"初始化":  {{HookID: "lvm", HookVersion: 10}},
		"空插槽":  nil,
	}}
	// 版本按数字比，lvm@2 在 lvm@10 前面。
	want := []model.HookKey{{ID: "lvm", Version: 2}, {ID: "lvm", Version: 10}, {ID: "nic", Version: 2}}
	// 装配表是 map，每次遍历的顺序不同：多跑几次，结果都要一样。
	for range 20 {
		if got := store.HookRefs(tpl); !slices.Equal(got, want) {
			t.Fatalf("引用 = %v，期望 %v", got, want)
		}
	}
	if got := store.HookRefs(model.Template{}); len(got) != 0 {
		t.Errorf("没有装配表时应为空，得到 %v", got)
	}
}

// reviewHook 是一个待审批、能通过激活判定的片段 hook，作者是 alice。
func reviewHook() model.Hook {
	return model.Hook{
		ID: "lvm", Version: 3, Name: "LVM-Partition", State: model.VersionReview, Author: "alice",
		Params: []model.ParamDecl{{Name: "disk", Label: "系统盘", Type: "diskpath", Default: "/dev/sda"}},
		Impls: []model.Impl{{
			Family: "linux", Series: []catalog.Series{"rocky", "almalinux"}, Kind: model.ImplFragment,
			Body: "ignoredisk --only-use={{param:disk}}\nautopart --type=lvm\n",
		}},
	}
}

// reviewTemplate 是一个待审批、能通过激活判定的 kickstart 模板：分区插槽放 lvm@3，作者是 alice。
func reviewTemplate() model.Template {
	return model.Template{
		ID: "rhel-base", Version: 2, Name: "RHEL-Baseline", State: model.VersionReview, Author: "alice",
		Family: "linux", Series: []catalog.Series{"rocky", "almalinux"},
		Body:     "text\n{{lober:syslog}}\n{{slot:分区}}\n%post\n{{lober:firstboot}}\n{{lober:cleanup}}\n%end\n",
		Assembly: map[string][]model.Instance{"分区": {{HookID: "lvm", HookVersion: 3}}},
	}
}

// approvedHooks 是 reviewTemplate 引用的 hook 版本，已审批。
func approvedHooks() map[model.HookKey]model.Hook {
	h := reviewHook()
	h.State, h.Approver = model.VersionApproved, "bob"
	return map[model.HookKey]model.Hook{h.Key(): h}
}

// approverAt 是直接调用 activation.Approver 得到的问题，Subject 指向审批的版本：Approver 把它留给调用方填。
func approverAt(author, approver string, s model.Subject) []model.Problem {
	ps := activation.Approver(author, approver)
	for i := range ps {
		ps[i].Subject = s
	}
	return ps
}

// wantRejected 断言 err 是 ProblemsError(ErrRejected)，问题依次就是 want。
func wantRejected(t *testing.T, err error, want []model.Problem) {
	t.Helper()
	var pe *store.ProblemsError
	if !errors.Is(err, store.ErrRejected) || !errors.As(err, &pe) {
		t.Fatalf("应报 ProblemsError(ErrRejected)，得到 %v", err)
	}
	if !reflect.DeepEqual(pe.Problems, want) {
		t.Errorf("问题 = %v\n期望 %v", pe.Problems, want)
	}
}

func TestApproveHookProblems(t *testing.T) {
	if err := store.ApproveHookProblems(reviewHook(), "bob"); err != nil {
		t.Fatalf("应通过，得到 %v", err)
	}
	// 不通过时，问题就是直接调用第 1 层得到的：先激活判定，再审批人。两种判定都要跑，一种通过了不能跳过另一种：
	// 审批人合法，没有实现的 hook 照样批不了；激活判定通过，作者照样不能自己批。大小写不同也算作者本人。
	noImpl := func(h *model.Hook) { h.Impls = nil }
	cases := []struct {
		name     string
		mod      func(h *model.Hook)
		approver string
		codes    []model.Code // 用来确认反例造对了
	}{
		{"只有激活判定不通过", noImpl, "bob", []model.Code{"hook_no_impl"}},
		{"只有审批人不通过", func(*model.Hook) {}, "Alice", []model.Code{"approver_is_author"}},
		{"两种都不通过", noImpl, "Alice", []model.Code{"hook_no_impl", "approver_is_author"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := reviewHook()
			c.mod(&h)
			want := append(activation.Hook(h), approverAt("alice", c.approver, model.Subject{Kind: "hook", ID: "lvm@3"})...)
			if codes := codesOf(want); !slices.Equal(codes, c.codes) {
				t.Fatalf("反例没造对：%v", want)
			}
			wantRejected(t, store.ApproveHookProblems(h, c.approver), want)
		})
	}
}

func TestApproveTemplateProblems(t *testing.T) {
	if err := store.ApproveTemplateProblems(reviewTemplate(), approvedHooks(), "bob"); err != nil {
		t.Fatalf("应通过，得到 %v", err)
	}
	// 同 TestApproveHookProblems。激活判定的反例是引用的 hook 版本在审批前刚被废弃（C26）：审批人合法时，兜住它的
	// 正是事务里的这次重跑。
	deprecateLVM := func(hooks map[model.HookKey]model.Hook) {
		h := hooks[model.HookKey{ID: "lvm", Version: 3}]
		h.State = model.VersionDeprecated
		hooks[h.Key()] = h
	}
	cases := []struct {
		name     string
		mod      func(hooks map[model.HookKey]model.Hook)
		approver string
		codes    []model.Code // 用来确认反例造对了
	}{
		{"只有激活判定不通过", deprecateLVM, "bob", []model.Code{"hook_ref_deprecated"}},
		{"只有审批人不通过", func(map[model.HookKey]model.Hook) {}, "", []model.Code{"approver_missing"}},
		{"两种都不通过", deprecateLVM, "", []model.Code{"hook_ref_deprecated", "approver_missing"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tpl, hooks := reviewTemplate(), approvedHooks()
			c.mod(hooks)
			want := append(activation.Template(tpl, hooks), approverAt("alice", c.approver, model.Subject{Kind: "template", ID: "rhel-base@2"})...)
			if codes := codesOf(want); !slices.Equal(codes, c.codes) {
				t.Fatalf("反例没造对：%v", want)
			}
			wantRejected(t, store.ApproveTemplateProblems(tpl, hooks, c.approver), want)
		})
	}
}

// batchInput 是一批能通过准入判定的输入：两台主机（一台登记了平台），一个导入完成的 ISO，已审批的模板与它
// 引用的已审批 hook。
func batchInput() admission.BatchInput {
	tpl := reviewTemplate()
	tpl.State, tpl.Approver = model.VersionApproved, "bob"
	return admission.BatchInput{
		Hosts:    []model.Host{{SN: "SN-1", Arch: "x86_64", Firmware: "uefi"}, {SN: "SN-2"}},
		OpenTask: map[string]bool{},
		ISO: model.ISO{ID: "iso-rocky96", Name: "Rocky Linux 9.6", Family: "linux", Series: "rocky",
			Version: "9.6", Arch: "x86_64", State: model.ISOReady},
		Template: tpl,
		Hooks:    approvedHooks(),
	}
}

func TestBatchProblems(t *testing.T) {
	if err := store.BatchProblems(batchInput(), nil); err != nil {
		t.Fatalf("应通过，得到 %v", err)
	}
	cases := []struct {
		name    string
		mod     func(in *admission.BatchInput)
		missing []string
		// batchCodes 是直接调用准入判定报出的问题代码，写法见 codesByPlace，用来确认反例造对了。
		batchCodes map[string][]model.Code
	}{
		// 准入判定的入参里没有没登记的主机，它查不出来，全靠这里补报；同一个 SN 只报一条。
		{"只有主机没登记", func(*admission.BatchInput) {}, []string{"SN-9", "SN-9"}, nil},
		{"准入判定的问题与没登记的主机一起报", func(in *admission.BatchInput) {
			in.ISO.State = model.ISODeprecated
			in.OpenTask["SN-1"] = true
		}, []string{"SN-8", "SN-9"}, map[string][]model.Code{"": {"iso_not_ready"}, "SN-1": {"host_has_open_task"}}},
		// 主机都登记了，只有准入判定不通过：ISO 刚被废弃（C26），或者主机刚有了未结束的任务（C31）。事务里兜底的
		// 正是这一层，没有要补报的主机也要重跑；Common 与 ByHost 只要有一处有问题，整批都不建（C30）。
		{"主机都登记、只有 Common 有问题", func(in *admission.BatchInput) { in.ISO.State = model.ISODeprecated },
			nil, map[string][]model.Code{"": {"iso_not_ready"}}},
		{"主机都登记、只有 ByHost 有问题", func(in *admission.BatchInput) { in.OpenTask["SN-1"] = true },
			nil, map[string][]model.Code{"SN-1": {"host_has_open_task"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := batchInput()
			c.mod(&in)
			// 结果就是直接调用准入判定得到的，外加每个没登记的 SN 一条 host_not_registered。
			want := admission.Batch(in)
			if got := codesByPlace(want); !maps.EqualFunc(got, c.batchCodes, slices.Equal[[]model.Code]) {
				t.Fatalf("反例没造对：准入判定报的是 %v", got)
			}
			for _, sn := range c.missing {
				want.ByHost[sn] = []model.Problem{model.NewProblem("host_not_registered", model.Subject{Kind: "host", ID: sn})}
			}
			err := store.BatchProblems(in, c.missing)
			var be *store.BatchError
			if !errors.Is(err, store.ErrRejected) || !errors.As(err, &be) {
				t.Fatalf("应报 BatchError(ErrRejected)，得到 %v", err)
			}
			if !reflect.DeepEqual(be.Result, want) {
				t.Errorf("结果 = %+v\n期望 %+v", be.Result, want)
			}
		})
	}
}

// codesByPlace 列出一批判定结果里各处的问题代码：键为空串的是 Common，其余是 SN；没有问题的不列。
func codesByPlace(r admission.BatchResult) map[string][]model.Code {
	out := map[string][]model.Code{}
	if len(r.Common) > 0 {
		out[""] = codesOf(r.Common)
	}
	for sn, ps := range r.ByHost {
		if len(ps) > 0 {
			out[sn] = codesOf(ps)
		}
	}
	return out
}

func TestNewID生成的ID互不相同且合规则(t *testing.T) {
	const lowerBase32 = "abcdefghijklmnopqrstuvwxyz234567"
	seen := make(map[string]bool, 1000)
	for range 1000 {
		id := store.NewID()
		// Trim 掉字母表里的字符后为空，说明每个字符都在小写 base32 里，也就不含 -。
		if len(id) != 26 || strings.Trim(id, lowerBase32) != "" || !model.ValidID(id) || !model.IsSafeWord(id) {
			t.Fatalf("ID %q 不合规则：应为 26 个字符的小写 base32", id)
		}
		if seen[id] {
			t.Fatalf("ID %q 重复了", id)
		}
		seen[id] = true
	}
}

func Test带类型的错误认得出类别与细节(t *testing.T) {
	dup := fmt.Errorf("登记主机：%w", &store.DuplicateError{Field: "sn", Existing: "SN-1"})
	var de *store.DuplicateError
	if !errors.Is(dup, store.ErrDuplicate) || !errors.As(dup, &de) || de.Field != "sn" || de.Existing != "SN-1" {
		t.Errorf("认不出 DuplicateError：%v", dup)
	}
	if !strings.Contains(dup.Error(), "SN-1") {
		t.Errorf("%q 没写出已有的那一个", dup.Error())
	}

	invalid := error(&store.ProblemsError{Kind: store.ErrInvalid, Problems: []model.Problem{
		model.NewProblem("text_not_storable", model.Subject{Kind: "iso", ID: "iso-1", Path: "name"}),
	}})
	if !errors.Is(invalid, store.ErrInvalid) || errors.Is(invalid, store.ErrRejected) {
		t.Errorf("ProblemsError 的类别应只认 Kind：%v", invalid)
	}
	if !strings.Contains(invalid.Error(), "text_not_storable") {
		t.Errorf("%q 没写出问题", invalid.Error())
	}

	// BatchError 的文字先写 Common，再按 SN 的顺序写各台：同一个结果每次写出来都一样，日志才对得上。
	batch := error(&store.BatchError{Result: admission.BatchResult{
		Common: []model.Problem{model.NewProblem("iso_import_failed", model.Subject{Kind: "iso", ID: "iso-1"})},
		ByHost: map[string][]model.Problem{
			"SN-2": {model.NewProblem("host_has_open_task", model.Subject{Kind: "host", ID: "SN-2"})},
			"SN-1": {model.NewProblem("host_not_registered", model.Subject{Kind: "host", ID: "SN-1"})},
			"SN-3": {},
		},
	}})
	if !errors.Is(batch, store.ErrRejected) {
		t.Errorf("BatchError 应包着 ErrRejected：%v", batch)
	}
	msg := batch.Error()
	common, sn1, sn2 := strings.Index(msg, "iso_import_failed"), strings.Index(msg, "SN-1"), strings.Index(msg, "SN-2")
	if common < 0 || sn1 < common || sn2 < sn1 {
		t.Errorf("%q 应依次写出 Common、SN-1、SN-2 的问题", msg)
	}
}
