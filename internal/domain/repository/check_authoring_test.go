package repository_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// hook 与模板的入参与版本状态的检查：pgstore 调用的就是这一份，契约测试再在 pgstore 上跑一遍。

// wantPlainInvalid 要求 err 是不带问题的 ErrInvalid：调用方或程序自己的错，没有面向操作员的原因代码可报。
func wantPlainInvalid(t *testing.T, err error, what string) {
	t.Helper()
	var pe *repository.ProblemsError
	if !errors.Is(err, repository.ErrInvalid) || errors.As(err, &pe) {
		t.Errorf("%s：应报不带问题的 ErrInvalid，得到 %v", what, err)
	}
}

// notStorableAt 是一条 text_not_storable，落在 s 上。
func notStorableAt(s model.Subject) model.Problem {
	return model.NewProblem("text_not_storable", s)
}

// nameProblem 是 model.NormalizeName 对 raw 报的问题，Subject 换成 s。
func nameProblem(t *testing.T, raw string, s model.Subject) model.Problem {
	t.Helper()
	_, _, err := model.NormalizeName(raw)
	var p model.Problem
	if !errors.As(err, &p) {
		t.Fatalf("反例没造对：名字 %q 应过不了 NormalizeName，得到 %v", raw, err)
	}
	p.Subject = s
	return p
}

func TestNormalizeName(t *testing.T) {
	subj := model.Subject{Kind: "template", ID: "tpl-1"}
	if display, key, ps := repository.NormalizeName("  Rocky-Base  ", subj); display != "Rocky-Base" || key != "rocky-base" || ps != nil {
		t.Errorf("规范化得到 %q、%q、%v，应为 Rocky-Base、rocky-base、没有问题", display, key, ps)
	}
	// 不合法时问题与 model 的相同，只是 Subject 指向调用方给的对象
	for _, raw := range []string{"", "Rocky Base", "中文", strings.Repeat("a", 65), "lober:Rocky Base"} {
		display, key, ps := repository.NormalizeName(raw, subj)
		if want := []model.Problem{nameProblem(t, raw, subj)}; display != "" || key != "" || !reflect.DeepEqual(ps, want) {
			t.Errorf("名字 %q 得到 %q、%q、%v，应为两个空串与 %v", raw, display, key, ps, want)
		}
	}
	// 这是用户的入口：用了保留前缀（按 key 比，大小写不同也算）报 name_reserved，参数是规范的写法
	for _, raw := range []string{"lober:Base", " LOBER:Base ", "Lober:"} {
		display, key, ps := repository.NormalizeName(raw, subj)
		if want := []model.Problem{reservedProblem(strings.TrimSpace(raw), subj)}; display != "" || key != "" || !reflect.DeepEqual(ps, want) {
			t.Errorf("名字 %q 得到 %q、%q、%v，应为两个空串与 %v", raw, display, key, ps, want)
		}
	}
	// 只是长得像的不算
	for _, raw := range []string{"lober-base", "lober", "Rocky-lober:x"} {
		if display, _, ps := repository.NormalizeName(raw, subj); display != raw || ps != nil {
			t.Errorf("名字 %q 没用保留前缀，应原样收下，得到 %q、%v", raw, display, ps)
		}
	}
}

// reservedProblem 是用户的入口对名字 display（规范的写法）报的 name_reserved，Subject 为 s。
func reservedProblem(display string, s model.Subject) model.Problem {
	return model.NewProblem("name_reserved", s, display)
}

// 前缀与名字的 key 一样只转 ASCII 字母：开尔文符号 K（U+212A）按 Unicode 转成 k，就会匹配上本不该匹配的名字。
func TestNameKeyPrefix(t *testing.T) {
	const kelvin = "K" // 开尔文符号，看着像 K
	for in, want := range map[string]string{"": "", "Rocky-B": "rocky-b", "rocky": "rocky", kelvin: kelvin, "Ä" + kelvin + "B_1": "Ä" + kelvin + "b_1"} {
		if got := repository.NameKeyPrefix(in); got != want {
			t.Errorf("NameKeyPrefix(%q) 得到 %q，应为 %q", in, got, want)
		}
	}
}

func TestCheckAuthor与CheckApprover(t *testing.T) {
	for _, s := range []string{"alice", "张三"} {
		wantErr(t, repository.CheckAuthor(s), nil)
		wantErr(t, repository.CheckApprover(s), nil)
	}
	wantInvalid(t, repository.CheckAuthor(""), "作者为空：审批人不能是作者（C22），没有作者就无从查起")
	wantErr(t, repository.CheckApprover(""), nil) // 空的审批人由审批时重跑的判定报 approver_missing
	for _, bad := range []string{"al\x00ice", "al\xffice"} {
		wantInvalid(t, repository.CheckAuthor(bad), fmt.Sprintf("作者 %q", bad))
		wantInvalid(t, repository.CheckApprover(bad), fmt.Sprintf("审批人 %q", bad))
	}
}

func TestHookMeta与TemplateMeta(t *testing.T) {
	want := repository.VersionMeta{Name: "LVM", State: model.VersionApproved, Author: "alice", Approver: "bob", Builtin: true}
	h := model.Hook{ID: "lvm", Version: 3, Name: "LVM", State: model.VersionApproved, Author: "alice", Approver: "bob", Builtin: true, Rev: 7}
	tpl := model.Template{ID: "base", Version: 3, Name: "LVM", State: model.VersionApproved, Author: "alice", Approver: "bob", Builtin: true, Rev: 7}
	if got := repository.HookMeta(h); got != want {
		t.Errorf("HookMeta 得到 %+v，应为 %+v", got, want)
	}
	if got := repository.TemplateMeta(tpl); got != want {
		t.Errorf("TemplateMeta 得到 %+v，应为 %+v", got, want)
	}
}

// newHook 是新建 hook 的合规则入参：只带内容与作者，名字带着首尾空白，由仓储规范化。
func newHook() model.Hook {
	h := reviewHook()
	h.ID, h.Version, h.State, h.Name, h.Description = "", 0, "", "  LVM-Partition ", "分区"
	return h
}

// newTemplate 是新建模板的合规则入参，规则同 newHook。
func newTemplate() model.Template {
	tpl := reviewTemplate()
	tpl.ID, tpl.Version, tpl.State, tpl.Name = "", 0, "", " RHEL-Baseline"
	return tpl
}

func TestCheckNewHook(t *testing.T) {
	if name, key, err := repository.CheckNewHook(newHook()); err != nil || name != "LVM-Partition" || key != "lvm-partition" {
		t.Fatalf("应通过并给出规范的名字，得到 %q、%q、%v", name, key, err)
	}
	for _, c := range []struct {
		name string
		mod  func(h *model.Hook)
	}{
		// 只带内容与作者：其余由仓储填
		{"带了 ID", func(h *model.Hook) { h.ID = "lvm" }},
		{"带了版本", func(h *model.Hook) { h.Version = 1 }},
		{"带了状态", func(h *model.Hook) { h.State = model.VersionDraft }},
		{"带了审批人", func(h *model.Hook) { h.Approver = "bob" }},
		{"标成内置", func(h *model.Hook) { h.Builtin = true }},
		{"带了 Rev", func(h *model.Hook) { h.Rev = 1 }},
		{"没有作者", func(h *model.Hook) { h.Author = "" }},
		{"作者存不下", func(h *model.Hook) { h.Author = "a\x00" }},
	} {
		h := newHook()
		c.mod(&h)
		_, _, err := repository.CheckNewHook(h)
		wantPlainInvalid(t, err, c.name)
	}
	// 名字与文本的问题一起报，先名字、后文本；还没有 ID，Subject 不带 ID
	h := newHook()
	h.Name, h.Description, h.Impls[0].Body = "LVM Partition", "分区\x00", "\xff"
	want := []model.Problem{nameProblem(t, h.Name, model.Subject{Kind: "hook"}),
		notStorableAt(model.Subject{Kind: "hook"}), notStorableAt(model.Subject{Kind: "impl", Path: "impl/1"})}
	_, _, err := repository.CheckNewHook(h)
	wantProblemsErr(t, err, repository.ErrInvalid, want)
	// 用户新建时用了保留前缀：name_reserved 与文本的问题一起报，先名字、后文本
	h = newHook()
	h.Name, h.Description = " LOBER:LVM ", "分区\x00"
	want = []model.Problem{reservedProblem("LOBER:LVM", model.Subject{Kind: "hook"}), notStorableAt(model.Subject{Kind: "hook"})}
	_, _, err = repository.CheckNewHook(h)
	wantProblemsErr(t, err, repository.ErrInvalid, want)
}

func TestCheckNewTemplate(t *testing.T) {
	if name, key, err := repository.CheckNewTemplate(newTemplate()); err != nil || name != "RHEL-Baseline" || key != "rhel-baseline" {
		t.Fatalf("应通过并给出规范的名字，得到 %q、%q、%v", name, key, err)
	}
	for _, c := range []struct {
		name string
		mod  func(tpl *model.Template)
	}{
		{"带了 ID", func(tpl *model.Template) { tpl.ID = "base" }},
		{"带了版本", func(tpl *model.Template) { tpl.Version = 1 }},
		{"带了状态", func(tpl *model.Template) { tpl.State = model.VersionDraft }},
		{"带了审批人", func(tpl *model.Template) { tpl.Approver = "bob" }},
		{"标成内置", func(tpl *model.Template) { tpl.Builtin = true }},
		{"带了派生来源：派生走 DeriveTemplate", func(tpl *model.Template) { tpl.DerivedFrom = &model.TemplateKey{ID: "base", Version: 1} }},
		{"带了 Rev", func(tpl *model.Template) { tpl.Rev = 1 }},
		{"没有作者", func(tpl *model.Template) { tpl.Author = "" }},
		{"作者存不下", func(tpl *model.Template) { tpl.Author = "\xff" }},
	} {
		tpl := newTemplate()
		c.mod(&tpl)
		_, _, err := repository.CheckNewTemplate(tpl)
		wantPlainInvalid(t, err, c.name)
	}
	tpl := newTemplate()
	tpl.Name, tpl.Body = "RHEL Baseline", "text\x00"
	want := []model.Problem{nameProblem(t, tpl.Name, model.Subject{Kind: "template"}), notStorableAt(model.Subject{Kind: "template"})}
	_, _, err := repository.CheckNewTemplate(tpl)
	wantProblemsErr(t, err, repository.ErrInvalid, want)
	tpl = newTemplate()
	tpl.Name = "lober:Kickstart"
	_, _, err = repository.CheckNewTemplate(tpl)
	wantProblemsErr(t, err, repository.ErrInvalid, []model.Problem{reservedProblem("lober:Kickstart", model.Subject{Kind: "template"})})
}

func TestCheckHookDraft与CheckTemplateDraft(t *testing.T) {
	h, tpl := reviewHook(), reviewTemplate()
	h.State, tpl.State = model.VersionDraft, model.VersionDraft
	wantErr(t, repository.CheckHookDraft(h), nil)
	wantErr(t, repository.CheckTemplateDraft(tpl), nil)
	// 键不合格式报不带问题的 ErrInvalid，不当成找不到
	for _, k := range []model.HookKey{{ID: "LVM", Version: 3}, {ID: "lvm", Version: 0}} {
		bad, badTpl := h, tpl
		bad.ID, bad.Version, badTpl.ID, badTpl.Version = k.ID, k.Version, k.ID, k.Version
		wantPlainInvalid(t, repository.CheckHookDraft(bad), fmt.Sprintf("hook 的键 %+v", k))
		wantPlainInvalid(t, repository.CheckTemplateDraft(badTpl), fmt.Sprintf("模板的键 %+v", k))
	}
	// 存不下的文本按位置报，Subject 指向这个版本
	h.Params[0].Label = "系统盘\xff"
	wantProblemsErr(t, repository.CheckHookDraft(h), repository.ErrInvalid,
		[]model.Problem{notStorableAt(model.Subject{Kind: "hook", ID: "lvm@3", Path: "param/1"})})
	tpl.Bindings = map[string]model.Binding{"hostname": {Source: "fixed", Value: "a\x00"}}
	wantProblemsErr(t, repository.CheckTemplateDraft(tpl), repository.ErrInvalid,
		[]model.Problem{notStorableAt(model.Subject{Kind: "template", ID: "rhel-base@2", Path: model.TemplateFieldKey("hostname")})})
}

// 版本的状态转换：draft → review → approved → deprecated，review 可以退回 draft。内置的只读（C25）。
func TestCheckStep(t *testing.T) {
	review := repository.VersionMeta{Name: "LVM", State: model.VersionReview, Author: "alice"}
	wantErr(t, repository.CheckStep("hook lvm@3", review, model.VersionReview), nil)
	for _, from := range []model.VersionState{model.VersionDraft, model.VersionApproved, model.VersionDeprecated} {
		if err := repository.CheckStep("hook lvm@3", review, from); !errors.Is(err, repository.ErrConflict) {
			t.Errorf("待审批的版本按从 %s 转出应报 ErrConflict，得到 %v", from, err)
		}
	}
	builtin := repository.VersionMeta{Name: "LVM", State: model.VersionApproved, Builtin: true}
	if err := repository.CheckStep("hook builtin-lvm@1", builtin, model.VersionApproved); !errors.Is(err, repository.ErrImmutable) {
		t.Errorf("内置的版本不能转换（包括废弃），应报 ErrImmutable，得到 %v", err)
	}
}

// 只有草稿能改、能删（C23），内置的只读（C25）。
func TestCheckDraft(t *testing.T) {
	wantErr(t, repository.CheckDraft("模板 base@2", repository.VersionMeta{State: model.VersionDraft}), nil)
	for _, m := range []repository.VersionMeta{
		{State: model.VersionReview}, {State: model.VersionApproved}, {State: model.VersionDeprecated},
		{State: model.VersionDraft, Builtin: true},
	} {
		if err := repository.CheckDraft("模板 base@2", m); !errors.Is(err, repository.ErrImmutable) {
			t.Errorf("%+v 应报 ErrImmutable，得到 %v", m, err)
		}
	}
}

// 保存草稿时，由仓储管的字段要原样带回：改了不悄悄丢掉，报 ErrInvalid。名字改用 Rename。
func TestCheckSameMeta(t *testing.T) {
	old := repository.VersionMeta{Name: "LVM", State: model.VersionDraft, Author: "alice"}
	wantErr(t, repository.CheckSameMeta("hook lvm@3", old, old), nil)
	for _, c := range []struct {
		name string
		mod  func(m *repository.VersionMeta)
	}{
		{"改了名字", func(m *repository.VersionMeta) { m.Name = "lvm" }},
		{"改了状态", func(m *repository.VersionMeta) { m.State = model.VersionReview }},
		{"改了作者", func(m *repository.VersionMeta) { m.Author = "bob" }},
		{"带了审批人", func(m *repository.VersionMeta) { m.Approver = "bob" }},
		{"标成内置", func(m *repository.VersionMeta) { m.Builtin = true }},
	} {
		in := old
		c.mod(&in)
		wantInvalid(t, repository.CheckSameMeta("hook lvm@3", old, in), c.name)
	}
}

// 派生来源只由派生写（C24），比的是指向的键，不是指针本身。
func TestCheckSameDerivedFrom(t *testing.T) {
	key := func(id string, v int) *model.TemplateKey { return &model.TemplateKey{ID: id, Version: v} }
	wantErr(t, repository.CheckSameDerivedFrom("模板 d@1", nil, nil), nil)
	wantErr(t, repository.CheckSameDerivedFrom("模板 d@1", key("base", 2), key("base", 2)), nil)
	for _, c := range []struct {
		name    string
		old, in *model.TemplateKey
	}{
		{"去掉了", key("base", 2), nil},
		{"加上了", nil, key("base", 2)},
		{"改了版本", key("base", 2), key("base", 3)},
		{"改了来源", key("base", 2), key("other", 2)},
	} {
		wantInvalid(t, repository.CheckSameDerivedFrom("模板 d@1", c.old, c.in), c.name)
	}
}

// builtinHook 是程序自带的一个内置 hook 版本：内置的已审批版本，名字是带保留前缀的规范写法。builtinTemplate 同。
func builtinHook() model.Hook {
	h := reviewHook()
	h.ID, h.Version, h.Name, h.State, h.Author, h.Approver, h.Builtin = "builtin-lvm", 1, "lober:LVM", model.VersionApproved, "lober", "lober", true
	return h
}

func builtinTemplate() model.Template {
	tpl := reviewTemplate()
	tpl.ID, tpl.Version, tpl.Name, tpl.State, tpl.Author, tpl.Approver, tpl.Builtin = "builtin-kickstart", 1, "lober:Kickstart",
		model.VersionApproved, "lober", "lober", true
	return tpl
}

// 装载内置内容（spec §6.2 第 11 条）：不合规则的是程序的错，报不带问题的 ErrInvalid；名字与文本的问题同新建时一样逐条
// 报，Subject 指向这个版本。名字必须带保留前缀（C12），与用户的入口正好相反。
func TestCheckBuiltinHook与CheckBuiltinTemplate(t *testing.T) {
	if key, err := repository.CheckBuiltinHook(builtinHook()); err != nil || key != "lober:lvm" {
		t.Fatalf("内置 hook 应通过并给出名字的 key，得到 %q、%v", key, err)
	}
	if key, err := repository.CheckBuiltinTemplate(builtinTemplate()); err != nil || key != "lober:kickstart" {
		t.Fatalf("内置模板应通过并给出名字的 key，得到 %q、%v", key, err)
	}
	// 前缀按 key 比，大写的写法也算带了
	upper := builtinHook()
	upper.Name = "LOBER:LVM"
	if key, err := repository.CheckBuiltinHook(upper); err != nil || key != "lober:lvm" {
		t.Errorf("大写前缀的内置 hook 应通过，得到 %q、%v", key, err)
	}
	for _, c := range []struct {
		name string
		mod  func(h *model.Hook)
	}{
		{"不是内置的", func(h *model.Hook) { h.Builtin = false }},
		{"不是已审批的", func(h *model.Hook) { h.State = model.VersionReview }},
		{"版本号不到 1", func(h *model.Hook) { h.Version = 0 }},
		{"ID 不合格式", func(h *model.Hook) { h.ID = "Builtin-LVM" }},
		{"作者存不下", func(h *model.Hook) { h.Author = "\xff" }},
		{"审批人存不下", func(h *model.Hook) { h.Approver = "a\x00" }},
		{"名字不是规范的写法：再规范化一次会变，库里的与程序自带的就比不上", func(h *model.Hook) { h.Name = " lober:LVM" }},
		{"名字不带保留前缀", func(h *model.Hook) { h.Name = "Builtin-LVM" }},
		{"名字只是长得像保留前缀", func(h *model.Hook) { h.Name = "lober-LVM" }},
	} {
		h := builtinHook()
		c.mod(&h)
		_, err := repository.CheckBuiltinHook(h)
		wantPlainInvalid(t, err, "内置 hook "+c.name)
	}
	// 模板与 hook 共用这几条，只看接上了；模板另有一条：内置模板不是派生来的
	for _, c := range []struct {
		name string
		mod  func(tpl *model.Template)
	}{
		{"不是内置的", func(tpl *model.Template) { tpl.Builtin = false }},
		{"名字不是规范的写法", func(tpl *model.Template) { tpl.Name = "lober:Kickstart " }},
		{"名字不带保留前缀", func(tpl *model.Template) { tpl.Name = "Builtin-Kickstart" }},
		{"带了派生来源", func(tpl *model.Template) { tpl.DerivedFrom = &model.TemplateKey{ID: "base", Version: 1} }},
	} {
		tpl := builtinTemplate()
		c.mod(&tpl)
		_, err := repository.CheckBuiltinTemplate(tpl)
		wantPlainInvalid(t, err, "内置模板"+c.name)
	}

	h := builtinHook()
	h.Name, h.Description = "lober:Builtin LVM", "\x00"
	_, err := repository.CheckBuiltinHook(h)
	subj := model.Subject{Kind: "hook", ID: "builtin-lvm@1"}
	wantProblemsErr(t, err, repository.ErrInvalid, []model.Problem{nameProblem(t, h.Name, subj), notStorableAt(subj)})
	tpl := builtinTemplate()
	tpl.Name, tpl.Body = "lober:Builtin Kickstart", "\xff"
	_, err = repository.CheckBuiltinTemplate(tpl)
	subj = model.Subject{Kind: "template", ID: "builtin-kickstart@1"}
	wantProblemsErr(t, err, repository.ErrInvalid, []model.Problem{nameProblem(t, tpl.Name, subj), notStorableAt(subj)})
}

// 存不下的文本每一处一条 text_not_storable，Subject 的写法同激活判定：说明落在 hook 整体上，参数声明与实现按位置；族、
// 系、种类这些照目录取值的字段也查。
func TestHookTextProblems按位置报出(t *testing.T) {
	h := reviewHook()
	if ps := repository.HookTextProblems(h, "lvm@3"); len(ps) != 0 {
		t.Fatalf("存得下的 hook 不应有问题，得到 %v", ps)
	}
	h.Description = "分区\x00"
	h.Params = []model.ParamDecl{
		{Name: "disk", Label: "系统盘"},
		{Name: "size", Label: "大小\xff"},
		{Name: "fs", Type: "enum", Options: []model.Option{{Value: "xfs", Label: "\x00"}}},
	}
	h.Impls = []model.Impl{
		{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment, Body: "autopart\n"},
		{Family: "linux", Series: []catalog.Series{"rocky\x00"}, Kind: model.ImplFragment},
		{Family: "linux", Kind: model.ImplScript, Interpreter: "sh\xff"},
		{Family: "linux", Kind: model.ImplScript, Timings: []catalog.Timing{"post\x00"}},
	}
	whole := model.Subject{Kind: "hook", ID: "lvm@3"}
	at := func(s model.Subject, path string) model.Subject { s.Path = path; return s }
	impl := model.Subject{Kind: "impl", ID: "lvm@3"}
	want := []model.Problem{
		notStorableAt(whole), notStorableAt(at(whole, "param/2")), notStorableAt(at(whole, "param/3")),
		notStorableAt(at(impl, "impl/2")), notStorableAt(at(impl, "impl/3")), notStorableAt(at(impl, "impl/4")),
	}
	if got := repository.HookTextProblems(h, "lvm@3"); !reflect.DeepEqual(got, want) {
		t.Errorf("问题 = %v\n期望 %v", got, want)
	}
}

// 规则同 hook：正文、族与系落在模板整体上，参数声明按位置，取值来源按参数名；装配表按插槽名的字节序，插槽名本身落在插槽
// 上，实例按顺序落在 <插槽名>/<序号>，实例的取值来源落在 <插槽名>/<序号>/<参数名>。map 按键排过序，每次报得一样。
func TestTemplateTextProblems按位置报出(t *testing.T) {
	tpl := reviewTemplate()
	if ps := repository.TemplateTextProblems(tpl, "rhel-base@2"); len(ps) != 0 {
		t.Fatalf("存得下的模板不应有问题，得到 %v", ps)
	}
	tpl.Series = []catalog.Series{"rocky", "alma\xff"}
	tpl.Params = []model.ParamDecl{{Name: "hostname", Default: "\xff"}}
	tpl.Bindings = map[string]model.Binding{"b": {Source: "fixed", Value: "\x00"}, "a": {Source: "attr", Attr: "\xff"}, "ok": {Source: "fixed", Value: "1"}}
	tpl.Assembly = map[string][]model.Instance{
		"分区": {{HookID: "lvm", HookVersion: 3}, {HookID: "lvm\x00", HookVersion: 1, Bindings: map[string]model.Binding{
			"disk": {Source: "fixed", Value: "\xff"}, "fs": {Source: "fixed", Value: "xfs"}}}},
		"\xff": {{HookID: "nic", HookVersion: 1}},
	}
	whole := model.Subject{Kind: "template", ID: "rhel-base@2"}
	at := func(s model.Subject, path string) model.Subject { s.Path = path; return s }
	slot := model.Subject{Kind: "slot", ID: "rhel-base@2"}
	want := []model.Problem{
		notStorableAt(whole), notStorableAt(at(whole, "param/1")),
		notStorableAt(at(whole, model.TemplateFieldKey("a"))), notStorableAt(at(whole, model.TemplateFieldKey("b"))),
		notStorableAt(at(slot, model.InstancePath("分区", 2))), notStorableAt(at(slot, model.InstanceFieldKey("分区", 2, "disk"))),
		notStorableAt(at(slot, "\xff")),
	}
	for range 10 { // map 每次遍历的顺序不同：多跑几次，结果都要一样
		if got := repository.TemplateTextProblems(tpl, "rhel-base@2"); !reflect.DeepEqual(got, want) {
			t.Fatalf("问题 = %v\n期望 %v", got, want)
		}
	}
}
