package store

import (
	"errors"
	"maps"
	"slices"
	"strconv"

	"github.com/lober/lober/internal/core/model"
)

// hook 与模板的入参与版本状态的检查，不用查库就能定下，所以放在契约包、不随存储的写法走（计划「包与文件」）。hook 与
// 模板的身份、版本与名字规则一一对应（Templates 的说明），检查也一一对应；要查库里数据的那一半（名字被谁占着、版本被谁
// 引用）由实现在事务里查。

// VersionMeta 是 hook 或模板的一个版本里由仓储管的字段，hook 与模板同名同义：名字（在身份上，各版本共用）、状态、
// 作者、审批人与内置标记。状态只经状态转换改，作者在开版本时定下，审批人由审批写，内置标记只由装载内置内容写；保存
// 草稿时要原样带回（CheckSameMeta）。
type VersionMeta struct {
	Name     string
	State    model.VersionState
	Author   string
	Approver string
	Builtin  bool
}

// HookMeta 取 hook 版本 h 里由仓储管的字段。
func HookMeta(h model.Hook) VersionMeta {
	return VersionMeta{Name: h.Name, State: h.State, Author: h.Author, Approver: h.Approver, Builtin: h.Builtin}
}

// TemplateMeta 取模板版本 t 里由仓储管的字段。
func TemplateMeta(t model.Template) VersionMeta {
	return VersionMeta{Name: t.Name, State: t.State, Author: t.Author, Approver: t.Approver, Builtin: t.Builtin}
}

// 名字的规范化有两个入口（spec C12、§6.2 第 11 条）：用户的入口 NormalizeName 拦下保留前缀，内置装载的入口
// normalizeBuiltinName 反过来要求必须带它。两边的名字规则与前缀判定都只问 model（model.NormalizeName、
// model.NameReserved），这里只决定用了前缀算对还是算错。

// NormalizeName 是用户的入口：新建、派生与改名（CheckNewHook、CheckNewTemplate、DeriveTemplate、RenameHook、
// RenameTemplate）都经它，第 5 层的预检也该调它，报出的原因才与仓储的相同。按 model.NormalizeName 规范化 hook 或模板的
// 名字（C12），返回规范的写法 display 与名字的 key，大小写不同也算同名，唯一约束按 key。名字不合法时返回 model 给的
// name_invalid；合法但用了保留前缀（model.NameReserved）时返回 name_reserved，参数是 display。两种问题的 Subject 都是
// subj，返回问题时 display 与 key 为空。
func NormalizeName(raw string, subj model.Subject) (display, key string, ps []model.Problem) {
	display, key, err := model.NormalizeName(raw)
	var p model.Problem
	switch {
	case errors.As(err, &p):
		p.Subject = subj
		return "", "", []model.Problem{p}
	case model.NameReserved(key):
		return "", "", []model.Problem{model.NewProblem("name_reserved", subj, display)}
	}
	return display, key, nil
}

// normalizeBuiltinName 是内置装载的入口（checkBuiltin），返回名字的 key。名字不合法时与用户的入口一样返回 name_invalid，
// Subject 为 subj，由调用方与文本的问题一起报；名字不是规范的写法（再规范化一次会变，库里存的与程序自带的就比不上），
// 或者没带保留前缀，是程序自己的错，报不带问题的 ErrInvalid。
func normalizeBuiltinName(raw string, subj model.Subject) (string, []model.Problem, error) {
	display, key, err := model.NormalizeName(raw)
	var p model.Problem
	switch {
	case errors.As(err, &p):
		p.Subject = subj
		return "", []model.Problem{p}, nil
	case display != raw:
		return "", nil, invalidf("内置内容的名字 %q 不是规范的写法 %q", raw, display)
	case !model.NameReserved(key):
		return "", nil, invalidf("内置内容的名字 %q 要以保留前缀 %s 开头（spec C12）", raw, model.ReservedNamePrefix)
	}
	return key, nil, nil
}

// NameKeyPrefix 把列表的 NamePrefix（HookQuery、TemplateQuery）转成与名字的 key 比的前缀：只把 ASCII 的大写字母转成
// 小写，名字的 key 就是这样来的（model.NormalizeName），所以与「大小写不同也算同名」一致。strings.ToLower 按 Unicode
// 的规则转，会把开尔文符号 K（U+212A）转成 k，前缀就会匹配上本不该匹配的名字。名字只含可打印 ASCII，别的字符原样
// 按字节比。
func NameKeyPrefix(prefix string) string {
	b := []byte(prefix)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// CheckAuthor 查新版本的作者：审批人不能是作者（C22），没有作者这条就无从查起；作者要写进库里，还得存得下。不合的
// 报 ErrInvalid。
func CheckAuthor(author string) error {
	if author == "" || !model.StorableText(author) {
		return invalidf("作者要是非空的可存文本，收到 %q", author)
	}
	return nil
}

// CheckApprover 查审批人存得下，不合的报 ErrInvalid。空的或就是作者的，由事务里重跑的判定报出
// （ApproveHookProblems、ApproveTemplateProblems）。
func CheckApprover(approver string) error {
	if !model.StorableText(approver) {
		return invalidf("审批人 %q 存不下：不是合法的 UTF-8，或含 NUL", approver)
	}
	return nil
}

// CheckNewHook 查 CreateHook 的入参，都过了返回名字的规范写法与 key：
//
//   - 只带内容与作者：ID、版本、状态、审批人、内置标记与 Rev 由仓储填，填了报 ErrInvalid。
//   - 作者合 CheckAuthor。
//   - 名字不合法或用了保留前缀（NormalizeName；还没有 ID，Subject 不带 ID），说明或实现里有存不下的文本
//     （HookTextProblems），问题一起收进 ProblemsError(ErrInvalid)，先名字、后文本。
func CheckNewHook(h model.Hook) (name, key string, err error) {
	if h.ID != "" || h.Version != 0 || h.State != "" || h.Approver != "" || h.Builtin || h.Rev != 0 {
		return "", "", invalidf("新建 hook 只带内容与作者：ID、版本、状态、审批人、内置标记与 Rev 由仓储填")
	}
	if err := CheckAuthor(h.Author); err != nil {
		return "", "", err
	}
	name, key, ps := NormalizeName(h.Name, model.Subject{Kind: "hook"})
	if ps = append(ps, HookTextProblems(h, "")...); len(ps) > 0 {
		return "", "", &ProblemsError{Kind: ErrInvalid, Problems: ps}
	}
	return name, key, nil
}

// CheckNewTemplate 同 CheckNewHook，查的是 CreateTemplate 的入参（文本见 TemplateTextProblems）。派生要记来源，只能走
// DeriveTemplate，所以入参也不能带 DerivedFrom。
func CheckNewTemplate(t model.Template) (name, key string, err error) {
	if t.ID != "" || t.Version != 0 || t.State != "" || t.Approver != "" || t.Builtin || t.DerivedFrom != nil || t.Rev != 0 {
		return "", "", invalidf("新建模板只带内容与作者：ID、版本、状态、审批人、内置标记、派生来源与 Rev 由仓储填，" +
			"派生走 DeriveTemplate")
	}
	if err := CheckAuthor(t.Author); err != nil {
		return "", "", err
	}
	name, key, ps := NormalizeName(t.Name, model.Subject{Kind: "template"})
	if ps = append(ps, TemplateTextProblems(t, "")...); len(ps) > 0 {
		return "", "", &ProblemsError{Kind: ErrInvalid, Problems: ps}
	}
	return name, key, nil
}

// CheckHookDraft 是 SaveHookDraft 动数据之前的检查：版本键合格式（CheckVersionKey）；内容里的文本都存得下
// （HookTextProblems），存不下的收进 ProblemsError(ErrInvalid)。版本是不是草稿、由仓储管的字段改没改，要对读到的版本
// 查（CheckDraft、CheckSameMeta）。
func CheckHookDraft(h model.Hook) error {
	if err := CheckVersionKey(h.ID, h.Version); err != nil {
		return err
	}
	if ps := HookTextProblems(h, VersionID(h.ID, h.Version)); len(ps) > 0 {
		return &ProblemsError{Kind: ErrInvalid, Problems: ps}
	}
	return nil
}

// CheckTemplateDraft 同 CheckHookDraft，查的是 SaveTemplateDraft 的入参（文本见 TemplateTextProblems）。派生来源改没改
// 也要对读到的版本查（CheckSameDerivedFrom）。
func CheckTemplateDraft(t model.Template) error {
	if err := CheckVersionKey(t.ID, t.Version); err != nil {
		return err
	}
	if ps := TemplateTextProblems(t, VersionID(t.ID, t.Version)); len(ps) > 0 {
		return &ProblemsError{Kind: ErrInvalid, Problems: ps}
	}
	return nil
}

// CheckStep 查版本 what 能不能从 from 转出：内置的只读（C25），报 ErrImmutable；状态不是 from 是不合规矩的转换，报
// ErrConflict，调用方重读之后再判断。what 是报错里的称呼，如「hook lvm@3」。
func CheckStep(what string, m VersionMeta, from model.VersionState) error {
	switch {
	case m.Builtin:
		return errorf(ErrImmutable, "%s 是内置的，只读", what)
	case m.State != from:
		return errorf(ErrConflict, "%s 现为 %s，这一步要从 %s 转出", what, m.State, from)
	}
	return nil
}

// CheckDraft 查版本 what 是不是能改、能删的草稿：内置的只读（C25），已提交、已审批与已废弃的都不能改（C23），都报
// ErrImmutable。
func CheckDraft(what string, m VersionMeta) error {
	switch {
	case m.Builtin:
		return errorf(ErrImmutable, "%s 是内置的，只读", what)
	case m.State != model.VersionDraft:
		return errorf(ErrImmutable, "%s 现为 %s，只有草稿能改、能删", what, m.State)
	}
	return nil
}

// CheckSameMeta 查保存草稿时带来的、由仓储管的字段 in 与库里的 old 相同：名字改用 Rename，状态只经状态转换改，作者在
// 开版本时定下，审批人由审批写，内置标记只由装载内置内容写。不同的报 ErrInvalid，不悄悄丢掉。
func CheckSameMeta(what string, old, in VersionMeta) error {
	switch {
	case in.Name != old.Name:
		return invalidf("%s 的名字是 %q，收到的是 %q：改名要另走 Rename", what, old.Name, in.Name)
	case in != old:
		return invalidf("%s 的状态、作者、审批人与内置标记只由仓储写，保存时要原样带回", what)
	}
	return nil
}

// CheckSameDerivedFrom 查保存模板草稿时带来的派生来源 in 与库里的 old 相同：派生来源只由派生写（C24），不同的报
// ErrInvalid，理由同 CheckSameMeta。
func CheckSameDerivedFrom(what string, old, in *model.TemplateKey) error {
	if (old == nil) != (in == nil) || old != nil && *old != *in {
		return invalidf("%s 的派生来源只由派生写，保存时要原样带回", what)
	}
	return nil
}

// CheckBuiltinHook 查 EnsureBuiltinHook 装载的版本 h，都过了返回名字的 key。h 要是内置的已审批版本，键合格式，作者与
// 审批人存得下，名字已经是规范的写法（再规范化一次不变，库里存的与程序自带的才比得上）并且带保留前缀
// model.ReservedNamePrefix（C12，与用户的入口正好相反）：不合这些的是程序的错，报 ErrInvalid。名字不合法、内容里有存
// 不下的文本，问题一起收进 ProblemsError(ErrInvalid)，Subject 指向这个版本。
func CheckBuiltinHook(h model.Hook) (key string, err error) {
	id := VersionID(h.ID, h.Version)
	key, ps, err := checkBuiltin(h.ID, h.Version, HookMeta(h), model.Subject{Kind: "hook", ID: id})
	if err != nil {
		return "", err
	}
	if ps = append(ps, HookTextProblems(h, id)...); len(ps) > 0 {
		return "", &ProblemsError{Kind: ErrInvalid, Problems: ps}
	}
	return key, nil
}

// CheckBuiltinTemplate 同 CheckBuiltinHook，查的是 EnsureBuiltinTemplate 装载的模板版本 t；内置模板不是派生来的，带了
// 派生来源报 ErrInvalid。
func CheckBuiltinTemplate(t model.Template) (key string, err error) {
	id := VersionID(t.ID, t.Version)
	key, ps, err := checkBuiltin(t.ID, t.Version, TemplateMeta(t), model.Subject{Kind: "template", ID: id})
	if err != nil {
		return "", err
	}
	if t.DerivedFrom != nil {
		return "", invalidf("内置模板 %s 不是派生来的，不能带派生来源", id)
	}
	if ps = append(ps, TemplateTextProblems(t, id)...); len(ps) > 0 {
		return "", &ProblemsError{Kind: ErrInvalid, Problems: ps}
	}
	return key, nil
}

// checkBuiltin 是两种内置内容共有的检查，规则见 CheckBuiltinHook。返回名字的 key，以及名字不合法时的问题（Subject 为
// subj），由调用方与文本的问题一起报。
func checkBuiltin(id string, version int, m VersionMeta, subj model.Subject) (string, []model.Problem, error) {
	if err := CheckVersionKey(id, version); err != nil {
		return "", nil, err
	}
	switch {
	case !m.Builtin || m.State != model.VersionApproved:
		return "", nil, invalidf("装载的内置内容要是内置的已审批版本，收到内置为 %v、状态为 %s", m.Builtin, m.State)
	case !storable(m.Author, m.Approver):
		return "", nil, invalidf("内置内容的作者或审批人存不下：不是合法的 UTF-8，或含 NUL")
	}
	return normalizeBuiltinName(m.Name, subj)
}

// HookTextProblems 是 hook 版本 h 里存不下的文本（model.StorableText，spec §3 总则），每一处一条 text_not_storable：
// 说明落在 hook 整体上，参数声明与实现按位置，Subject 的写法同激活判定（model.Subject 的说明），id 是「ID@版本」，新建
// 时为空。PostgreSQL 的 text 与 jsonb 都不收 NUL，encoding/json 还会把非法的 UTF-8 换成 U+FFFD，读回来的就不是写进去
// 的，所以族、系、种类这些照目录取值的字段也查：草稿里它们可以是任何存得下的串，激活时才查对不对。
func HookTextProblems(h model.Hook, id string) []model.Problem {
	whole := model.Subject{Kind: "hook", ID: id}
	var ps []model.Problem
	if !model.StorableText(h.Description) {
		ps = append(ps, notStorable(whole))
	}
	ps = append(ps, declTextProblems(h.Params, whole)...)
	for i, impl := range h.Impls {
		if !storable(impl.Body, string(impl.Family), string(impl.Kind), string(impl.Interpreter)) ||
			!storableAll(impl.Series) || !storableAll(impl.Timings) {
			ps = append(ps, notStorable(model.Subject{Kind: "impl", ID: id, Path: "impl/" + strconv.Itoa(i+1)}))
		}
	}
	return ps
}

// TemplateTextProblems 是模板版本 t 里存不下的文本，规则同 HookTextProblems：正文、族与系落在模板整体上；参数声明按
// 位置；模板参数的取值来源按参数名，Path 为 tpl/<参数名>；装配表按插槽名，插槽名本身落在插槽上，实例按顺序落在
// <插槽名>/<序号>，实例的取值来源落在 <插槽名>/<序号>/<参数名>。map 按键排过序，同一份模板每次报得一样。
func TemplateTextProblems(t model.Template, id string) []model.Problem {
	whole := model.Subject{Kind: "template", ID: id}
	var ps []model.Problem
	if !storable(t.Body, string(t.Family)) || !storableAll(t.Series) {
		ps = append(ps, notStorable(whole))
	}
	ps = append(ps, declTextProblems(t.Params, whole)...)
	for _, name := range slices.Sorted(maps.Keys(t.Bindings)) {
		if !bindingStorable(name, t.Bindings[name]) {
			s := whole
			s.Path = model.TemplateFieldKey(name)
			ps = append(ps, notStorable(s))
		}
	}
	for _, slot := range slices.Sorted(maps.Keys(t.Assembly)) {
		at := func(path string) model.Subject { return model.Subject{Kind: "slot", ID: id, Path: path} }
		if !model.StorableText(slot) {
			ps = append(ps, notStorable(at(slot)))
		}
		for i, in := range t.Assembly[slot] {
			if !model.StorableText(in.HookID) {
				ps = append(ps, notStorable(at(model.InstancePath(slot, i+1))))
			}
			for _, name := range slices.Sorted(maps.Keys(in.Bindings)) {
				if !bindingStorable(name, in.Bindings[name]) {
					ps = append(ps, notStorable(at(model.InstanceFieldKey(slot, i+1, name))))
				}
			}
		}
	}
	return ps
}

// declTextProblems 是参数声明里存不下的文本，每个声明一条，Path 为 param/<序号>，其余同 whole。
func declTextProblems(decls []model.ParamDecl, whole model.Subject) []model.Problem {
	var ps []model.Problem
	for i, d := range decls {
		ok := storable(d.Name, d.Label, string(d.Type), d.Default, d.Offer)
		for _, o := range d.Options {
			ok = ok && storable(o.Value, o.Label)
		}
		if !ok {
			s := whole
			s.Path = "param/" + strconv.Itoa(i+1)
			ps = append(ps, notStorable(s))
		}
	}
	return ps
}

func bindingStorable(name string, b model.Binding) bool {
	return storable(name, string(b.Source), b.Value, b.Attr)
}

func notStorable(s model.Subject) model.Problem {
	return model.NewProblem("text_not_storable", s)
}

// storable 报告 ss 是否都存得下（model.StorableText）。
func storable(ss ...string) bool {
	return !slices.ContainsFunc(ss, func(s string) bool { return !model.StorableText(s) })
}

// storableAll 同 storable，收的是 catalog 里那些以字符串为底的类型。
func storableAll[S ~string](ss []S) bool {
	return !slices.ContainsFunc(ss, func(s S) bool { return !model.StorableText(string(s)) })
}
