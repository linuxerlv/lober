// Package activation 是激活判定（spec §4.1）：判断 hook 或模板的一个版本在审批前是否完备。
// 入参由调用方凑齐，这里不去别处查；结果为空表示可以批准，否则逐条列出原因。控制台的审批
// 与 hook 选择器都调它，所以两边不会一处放行、一处拦下。
package activation

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/scan"
)

// 脚本超时的上下限，单位秒（6 小时）。0 不表示「不限」：执行时机超时要靠超时扫描发现
// （spec §5.4），不设超时的脚本一卡住，任务就停在那个时机上。
const (
	timeoutMinSec = 1
	timeoutMaxSec = 21600
)

// timeoutOK 报告脚本超时是否在范围内。hook 给的默认值与模板按实例的覆盖用同一个范围（C11）。
func timeoutOK(sec int) bool {
	return timeoutMinSec <= sec && sec <= timeoutMaxSec
}

// versionID 是 hook 或模板的一个版本在 Subject 里的 ID：「ID@版本」。
func versionID(id string, version int) string {
	return id + "@" + strconv.Itoa(version)
}

// Hook 判断 hook 版本 h 能不能批准（C8–C11，以及 C19 在片段上的部分）。
//
// 问题的顺序固定：先 hook 整体（名称、说明，参数声明按位置，有没有实现），再按实现的顺序逐份列出，
// 超时最后；一份实现里先列不涉及正文位置的，再列正文的问题，按位置排。
//
// Subject 按位置指向（T9-1）：hook 整体的问题 Kind 为 hook，ID 为「hook ID@版本」，Path 为空；第 n 个
// 参数声明的问题 Path 为 param/<n>；第 n 份实现的问题 Kind 为 impl，Path 为 impl/<n>，涉及正文位置时
// 另带行列。n 都从 1 起，重名的声明、勾了同一组系的两份实现因此分得开；系与参数名只出现在说明里。
func Hook(h model.Hook) []model.Problem {
	id := versionID(h.ID, h.Version)
	hookSubj := model.Subject{Kind: "hook", ID: id}
	var out []model.Problem
	if _, _, err := model.NormalizeName(h.Name); err != nil {
		out = append(out, withSubject(err, hookSubj))
	}
	// 说明与正文一样要原样存下（T13-10）。
	if !model.StorableText(h.Description) {
		out = append(out, model.NewProblem("text_not_storable", hookSubj))
	}
	params, ps := checkDecls(h.Params, hookSubj, expandedRefs(h.Impls))
	out = append(out, ps...)
	if len(h.Impls) == 0 {
		out = append(out, model.NewProblem("hook_no_impl", hookSubj))
	}
	covered := map[catalog.Series]bool{} // 前面的实现已经覆盖的系
	hasScript := false
	for i, impl := range h.Impls {
		subj := model.Subject{Kind: "impl", ID: id, Path: "impl/" + strconv.Itoa(i+1)}
		out = append(out, checkImpl(impl, params, subj, covered)...)
		hasScript = hasScript || impl.Kind == model.ImplScript
	}
	// 超时只约束脚本（C11）：片段在渲染时就写进了应答文件，没有执行这一步。
	if hasScript && !timeoutOK(h.TimeoutSec) {
		out = append(out, model.NewProblem("timeout_invalid", hookSubj, h.TimeoutSec))
	}
	return out
}

// paramInfo 是一个参数名对应的声明。ok 为 false 表示声明本身不合法、已经报过：引用它的地方
// 不再按类型查，免得一个写错的声明在正文里每处引用都再报一遍。
type paramInfo struct {
	decl model.ParamDecl
	ok   bool
}

// expandedRefs 并起 hook 各份要展开的实现对参数的引用（见 scan.Refs），参数的默认值要取得出其中的子值
// （L-4）。哪些实现的正文要展开，与模板的实例、参数求值是同一条规则（model.Impl.Expanded，C-2）：脚本
// 经环境变量拿整个值、不取子值，不算；种类不认识的按片段算。
func expandedRefs(impls []model.Impl) scan.Refs {
	refs := scan.Refs{}
	for _, impl := range impls {
		if impl.Expanded() {
			res, _ := scan.Scan(impl.Body)
			refs.Merge(res.Refs())
		}
	}
	return refs
}

// checkDecls 逐个校验参数声明，并查名字有没有重复。重名的取第一个作引用的对象，后面的
// 报 param_decl_invalid。问题的 Path 为 param/<序号>，序号是声明的位置，从 1 起（T9-1）。
//
// refs 是对各参数的引用，合法声明的默认值要取得出其中的子值（L-4）。hook 传它自己的片段的：默认值
// 不合用，错在声明上。模板与实例的默认值随取值来源查（checkBindings），传 nil。
func checkDecls(decls []model.ParamDecl, subj model.Subject, refs scan.Refs) (map[string]paramInfo, []model.Problem) {
	params := make(map[string]paramInfo, len(decls))
	var out []model.Problem
	for i, d := range decls {
		s := subj
		s.Path = "param/" + strconv.Itoa(i+1)
		ps := d.Validate()
		_, dup := params[d.Name]
		if dup && len(ps) == 0 {
			ps = []model.Problem{model.NewProblem("param_decl_invalid", s, d.Name)}
		}
		for _, p := range ps {
			p.Subject = s
			out = append(out, p)
		}
		if dup {
			continue
		}
		params[d.Name] = paramInfo{decl: d, ok: len(ps) == 0}
		if subs := refs[d.Name]; len(ps) == 0 && d.Default != "" && len(subs) > 0 {
			out = append(out, valueProblems(d, d.Default, subs, s)...)
		}
	}
	return params, out
}

// checkImpl 查一份实现。covered 记着前面的实现覆盖了哪些系，本份查完后把自己的系加进去。
func checkImpl(impl model.Impl, params map[string]paramInfo, subj model.Subject, covered map[catalog.Series]bool) []model.Problem {
	var out []model.Problem
	seriesOK := seriesValid(impl.Family, impl.Series)
	if !seriesOK {
		out = append(out, model.NewProblem("impl_series_invalid", subj))
	}
	// 一个系最多出现在一份实现里（C8），否则渲染时不知道取哪一份。不认识的、不属于本实现族的
	// 系已经算作族或系不对，不再拿来比：另一份实现照样覆盖它是对的，报重叠会让作者去改那份
	// 本来正确的实现（T9-6①）。
	for i, s := range impl.Series {
		if !s.Valid() || s.Family() != impl.Family || slices.Contains(impl.Series[:i], s) {
			continue
		}
		if covered[s] {
			out = append(out, model.NewProblem("impl_series_overlap", subj, s))
		}
		covered[s] = true
	}
	var head, body []model.Problem
	switch impl.Kind {
	case model.ImplFragment:
		head, body = checkFragment(impl, seriesOK, params, subj)
	case model.ImplScript:
		head, body = checkScript(impl, seriesOK, params, subj)
	default:
		// 实现只有片段与脚本两种（C8）。种类不认识的只会来自直接调接口或库里的坏数据，正文按
		// 哪一种的规则查都无从说起（T7-2）。
		head = []model.Problem{model.NewProblem("impl_kind_unknown", subj, impl.Kind)}
	}
	// 正文要原样存下（T13-10），与种类无关。说的是整份正文，不带位置，排在正文的问题最前。
	if !model.StorableText(impl.Body) {
		body = append(body, model.NewProblem("text_not_storable", subj))
	}
	slices.SortStableFunc(body, byPosition)
	out = append(out, head...)
	return append(out, body...)
}

// seriesValid：族合法，系非空、各不相同，且都属于该族。hook 的实现（C8）与模板（C13）共用。
// 同一个系勾两次只会来自直接调接口，界面上做不到；也按系不对拦下，不替调用方去重：第 2 层会
// 原样存下这份列表。
func seriesValid(f catalog.Family, series []catalog.Series) bool {
	if !f.Valid() || len(series) == 0 {
		return false
	}
	for i, s := range series {
		if s.Family() != f || slices.Contains(series[:i], s) {
			return false
		}
	}
	return true
}

// checkFragment 查片段，分别返回不涉及正文位置的问题与正文的问题，后者由调用方按位置排。系不对时
// 推不出格式，跟格式有关的检查（同格式、系统值）都跳过，免得同一个错误换个说法再报一遍；其余检查
// 照做，作者一次能看到更多要改的地方。
//
// 正文可以为空，这是有意的放行（T9-4②）：给不需要做任何事的系当空操作实现，模板勾了这个系才不会
// 报 impl_missing_for_series。脚本不行，见 checkScript。
func checkFragment(impl model.Impl, seriesOK bool, params map[string]paramInfo, subj model.Subject) ([]model.Problem, []model.Problem) {
	var head []model.Problem
	var format catalog.Format // 推不出时为空
	if seriesOK {
		format, head = commonFormat(impl.Series, "fragment_mixed_formats", subj)
	}
	res, body := scanBody(impl.Body, subj)
	rules := newBodyRules(format, params)
	for _, seg := range res.Segments {
		s := at(subj, seg.Line, seg.Col)
		switch seg.Kind {
		case scan.KindSlot, scan.KindPart:
			// hook 只有一层（C8）：插槽与系统部件只属于模板正文。
			body = append(body, model.NewProblem("fragment_forbidden_placeholder", s, seg.Raw))
		default:
			body = append(body, rules.check(seg, s)...)
		}
	}
	return head, body
}

// 下面到 valueProblems 为止是模板与 hook 共用的检查：正文与片段里的参数引用、列表规则、系统值，以及
// 取值取不取得出引用要的子值。正文与片段只在插槽和系统部件上不同，那部分由 checkFragment 与
// checkTemplateBody 各自查。

// commonFormat 是一组合法的系共同的格式。不同格式时报 mixed（片段与模板各有自己的代码），返回
// 空：推不出格式，跟格式有关的检查都要跳过。
func commonFormat(series []catalog.Series, mixed model.Code, subj model.Subject) (catalog.Format, []model.Problem) {
	formats := formatsOf(series)
	if len(formats) != 1 {
		return "", []model.Problem{model.NewProblem(mixed, subj, joinFormats(formats))}
	}
	return formats[0], nil
}

// scanBody 扫描正文或片段。扫描只知道正文，不知道它属于哪个对象：问题的代码、说明与行列原样
// 保留，只改指向 subj。
func scanBody(body string, subj model.Subject) (scan.Result, []model.Problem) {
	res, ps := scan.Scan(body)
	for i, p := range ps {
		ps[i].Subject = at(subj, p.Subject.Line, p.Subject.Col)
	}
	return res, ps
}

// byPosition 按正文位置排问题，给 slices.SortStableFunc 用：扫描的问题与逐段查出的问题各自
// 有序，合起来要再排一次，同一位置的保持原来的先后。
func byPosition(a, b model.Problem) int {
	return cmp.Or(cmp.Compare(a.Subject.Line, b.Subject.Line), cmp.Compare(a.Subject.Col, b.Subject.Col))
}

// bodyRules 查正文里的系统值与参数引用：系统值在该格式可用；参数已声明（C9）、是受限类型（C19），
// 子值与 |join 用得对；逐行重复的列表参数所在的行没有冲突。一份正文用一个，按片段的顺序逐段交给它。
type bodyRules struct {
	format catalog.Format // 推不出时为空，不查系统值
	params map[string]paramInfo
	// 逐行重复把整行按列表项各抄一遍，所以按行记：这一行里第一个逐行重复的列表参数，以及这一行
	// 里有没有插槽、系统部件（只有模板正文里会有）。
	repeated   map[int]string
	structural map[int]bool
	// conflicts 记着哪一行已经为哪个列表参数报过 list_repeat_conflict：每行每个参数只报一次，
	// 同一个参数在冲突的行里再出现、后面再跟插槽或部件，都还是同一个冲突（T9-6④）。
	conflicts map[lineParam]bool
}

type lineParam struct {
	line int
	name string
}

func newBodyRules(format catalog.Format, params map[string]paramInfo) *bodyRules {
	return &bodyRules{format: format, params: params,
		repeated: map[int]string{}, structural: map[int]bool{}, conflicts: map[lineParam]bool{}}
}

// conflict 报第 line 行里列表参数 name 的 list_repeat_conflict，这一行已为它报过的不再报。
func (r *bodyRules) conflict(line int, name string, s model.Subject) []model.Problem {
	k := lineParam{line, name}
	if r.conflicts[k] {
		return nil
	}
	r.conflicts[k] = true
	return []model.Problem{model.NewProblem("list_repeat_conflict", s, name)}
}

// check 查一段系统值或参数引用，别的种类返回空。
func (r *bodyRules) check(seg scan.Segment, s model.Subject) []model.Problem {
	switch seg.Kind {
	case scan.KindSys:
		if r.format != "" && !slices.Contains(r.format.SystemValues(), seg.Sys) {
			return []model.Problem{model.NewProblem("sysvalue_not_allowed", s, seg.Sys, r.format)}
		}
	case scan.KindParam:
		return r.param(seg, s)
	}
	return nil
}

// placeholder 记下插槽或系统部件所在的行。它们只能展开一次，不能随所在的行逐行重复：这一行里
// 已经有逐行重复的列表参数时报 list_repeat_conflict，参数是那个列表参数。
func (r *bodyRules) placeholder(seg scan.Segment, s model.Subject) []model.Problem {
	r.structural[seg.Line] = true
	if name, ok := r.repeated[seg.Line]; ok {
		return r.conflict(seg.Line, name, s)
	}
	return nil
}

// param 查一处参数引用：声明过（C9），是受限类型（C19），子值与 |join 用得对，逐行重复不冲突。
func (r *bodyRules) param(seg scan.Segment, s model.Subject) []model.Problem {
	ref := seg.Param
	info, declared := r.params[ref.Name]
	switch {
	case !declared:
		return []model.Problem{model.NewProblem("param_undeclared", s, ref.Name)}
	case !info.ok:
		return nil
	case !info.decl.Type.Restricted():
		return []model.Problem{model.NewProblem("param_text_in_fragment", s, ref.Name)}
	}
	var out []model.Problem
	if ref.Sub != "" && !subOK(info.decl, ref.Sub) {
		out = append(out, model.NewProblem("param_accessor_invalid", s, ref.Sub))
	}
	if ref.Join && !info.decl.List {
		out = append(out, model.NewProblem("param_accessor_invalid", s, "|join"))
	}
	// 同一行里两个不同的列表参数，没法说清第几份抄本代入哪一项；同一个列表参数出现几次、取几个
	// 子值，每份抄本代入的都是同一项，不冲突。
	if info.decl.List && !ref.Join {
		first, seen := r.repeated[seg.Line]
		if !seen {
			r.repeated[seg.Line] = ref.Name
		}
		if (seen && first != ref.Name) || r.structural[seg.Line] {
			out = append(out, r.conflict(seg.Line, ref.Name, s)...)
		}
	}
	return out
}

// subOK 拿一个 IPv4 的 cidr 样例按声明解析，再取子值：哪些类型有哪些子值只在 model 里
// 登记一处，这里不另抄一份。样例对别的类型要么解析不了，要么解析成该类型的值、取子值时被
// 拒，结果都对。netmask 只适用于 IPv4，取值是不是 IPv6 要看具体的值，见 valueProblems。
func subOK(d model.ParamDecl, sub string) bool {
	v, err := model.ParamDecl{Name: d.Name, Type: d.Type}.Parse("192.0.2.1/24")
	if err != nil {
		return false
	}
	_, err = v.Sub(sub)
	return err == nil
}

// valueProblems 查 raw 能不能作为参数 d 的取值：通过类型校验（C19），并取得出 subs 里的每个子值
// （L-4）。按类型就取不了的子值已在引用处报过（subOK），这里跳过，不连带再报一遍；剩下的只取决于
// 取值本身，比如 IPv6 的 cidr 取不出 netmask。有一个取不出就报，与参数求值一致。问题指向 s。
func valueProblems(d model.ParamDecl, raw string, subs []string, s model.Subject) []model.Problem {
	v, err := d.Parse(raw)
	if err != nil {
		return []model.Problem{withSubject(err, s)}
	}
	for _, sub := range subs {
		if !subOK(d, sub) {
			continue
		}
		if _, err := v.Sub(sub); err != nil {
			return []model.Problem{withSubject(err, s)}
		}
	}
	return nil
}

// checkScript 查脚本，分别返回不涉及正文位置的问题与正文的问题，后者由调用方按位置排：写完整了
// （C10），声明的时机和解释器在覆盖的每个系里都可用（C8、C10），正文里字面出现的参数变量都已声明
// （C9），也没有写 lober 的占位符（L-1）。系不对时不按系查能力，理由同 checkFragment。
//
// 同一个时机写了两次只查一次，这是有意的放行（T9-4①）：不像重复勾的系那样拦下，第 5 层可以在接口
// 边界去重。
func checkScript(impl model.Impl, seriesOK bool, params map[string]paramInfo, subj model.Subject) ([]model.Problem, []model.Problem) {
	var head []model.Problem
	if strings.TrimSpace(impl.Body) == "" || impl.Interpreter == "" || len(impl.Timings) == 0 {
		head = append(head, model.NewProblem("script_incomplete", subj))
	}
	if seriesOK {
		for _, s := range impl.Series {
			f := s.Format()
			for i, t := range impl.Timings {
				switch {
				case slices.Contains(impl.Timings[:i], t): // 重复声明的时机只查一次
				case !slices.Contains(f.Timings(), t):
					head = append(head, model.NewProblem("script_timing_unsupported", subj, s, t))
				case impl.Interpreter != "" && !slices.Contains(f.Interpreters(t), impl.Interpreter):
					head = append(head, model.NewProblem("script_interpreter_unavailable", subj, s, t, impl.Interpreter))
				}
			}
		}
	}
	return head, append(checkEnvRefs(impl.Body, params, subj), checkScriptPlaceholders(impl.Body, subj)...)
}

// checkScriptPlaceholders 找出脚本正文里 lober 的占位符（L-1）：脚本按原样下发、不展开占位符，写在
// 里面的 {{param:disk}} 永远不会被替换，作者以为拿到了参数，到装机时才出错。slot、lober、sys、param
// 四种都报，写错的同种类也报，每处一条，参数是占位符原文，行列指向它的 {{。
//
// 哪些 {{ 开了一个占位符，以扫描为准；扫描报的问题本身不带出：脚本不展开占位符，{{http://…}} 这类
// 写法在脚本里没有毛病，种类不是那四种的照样放行，Jinja、Go 模板也不受影响。
func checkScriptPlaceholders(body string, subj model.Subject) []model.Problem {
	res, ps := scan.Scan(body)
	var out []model.Problem
	for _, seg := range res.Segments {
		if seg.Kind != scan.KindText {
			out = append(out, model.NewProblem("script_placeholder_not_expanded", at(subj, seg.Line, seg.Col), seg.Raw))
		}
	}
	// 写错的占位符留在原文段里，扫描只给出它的问题与 {{ 的行列，原文要从正文里取。扫描的问题按位置
	// 排好，c 只往前走。
	c := cursor{body: body, line: 1, col: 1}
	for _, p := range ps {
		line, col := p.Subject.Line, p.Subject.Col
		if raw, ok := scriptPlaceholderRaw(body[c.seek(line, col):]); ok {
			out = append(out, model.NewProblem("script_placeholder_not_expanded", at(subj, line, col), raw))
		}
	}
	slices.SortStableFunc(out, byPosition)
	return out
}

// scriptKinds 是 lober 占位符的四种种类，与扫描认的相同：写错的占位符扫描不给出种类，只好照它的
// 写法（{{、可选的空格、种类、冒号）再认一次。
var scriptKinds = []string{"slot:", "lober:", "sys:", "param:"}

// scriptPlaceholderRaw 认扫描报了问题的那个占位符，rest 从它的 {{ 起。种类不是那四种的返回 false；
// 是的话返回它的原文，规则同扫描：到同一行里其后的第一个 }} 为止，在那之前先遇到下一个 {{ 或行尾的，
// 截断在那里并去掉末尾的空白。原文只写进说明，哪里有占位符仍以扫描为准。
func scriptPlaceholderRaw(rest string) (string, bool) {
	head, ok := strings.CutPrefix(rest, "{{")
	word := strings.TrimLeft(head, " ")
	if !ok || !slices.ContainsFunc(scriptKinds, func(k string) bool { return strings.HasPrefix(word, k) }) {
		return "", false
	}
	end := len(rest)
	if k := strings.Index(head, "{{"); k >= 0 {
		end = 2 + k
	}
	if k := strings.IndexByte(rest[:end], '\n'); k >= 0 {
		end = k
	}
	if k := strings.Index(rest[2:end], "}}"); k >= 0 {
		return rest[:2+k+2], true
	}
	return strings.TrimRight(rest[:end], " \t\r"), true
}

// cursor 把扫描给的行列换回正文里的字节偏移，行列的算法同 position。它只往前走：问题按位置排好，
// 整份正文最多走一遍，一行里写满写错的占位符时也不会变成平方级。
type cursor struct {
	body           string
	off, line, col int
}

func (c *cursor) seek(line, col int) int {
	for c.line < line {
		c.off += strings.IndexByte(c.body[c.off:], '\n') + 1
		c.line, c.col = c.line+1, 1
	}
	for c.col < col {
		_, size := utf8.DecodeRuneInString(c.body[c.off:])
		c.off += size
		c.col++
	}
	return c.off
}

// envPrefix 是脚本参数变量的前缀，取自 model.EnvName：第 3 层按它导出变量，这里按它认引用，
// 两处只认这一个函数。
var envPrefix = model.EnvName("")

// checkEnvRefs 找出脚本正文里字面出现、却没有对应参数的 LOBER_PARAM_* 变量，每处报一次，
// 带上变量名开头的行列。动态拼出来的变量名查不到，这是刻意的取舍，写在代码的说明里。
// 前缀之前紧挨着标识符字符的（MY_LOBER_PARAM_X）是别的变量，前缀之后没有名字的不是变量。
func checkEnvRefs(body string, params map[string]paramInfo, subj model.Subject) []model.Problem {
	declared := make(map[string]bool, len(params))
	for name := range params {
		declared[model.EnvName(name)] = true
	}
	var out []model.Problem
	for i := 0; ; {
		j := strings.Index(body[i:], envPrefix)
		if j < 0 {
			return out
		}
		start := i + j
		end := start + len(envPrefix)
		for end < len(body) && isEnvNameByte(body[end]) {
			end++
		}
		i = end
		if (start > 0 && isEnvNameByte(body[start-1])) || end == start+len(envPrefix) {
			continue
		}
		if name := body[start:end]; !declared[name] {
			line, col := position(body, start)
			out = append(out, model.NewProblem("param_undeclared", at(subj, line, col), name))
		}
	}
}

// isEnvNameByte 认 shell 变量名的字符。小写也算：环境变量区分大小写，LOBER_PARAM_disk 与
// EnvName 给出的 LOBER_PARAM_DISK 是两个变量，脚本拿不到值。
func isEnvNameByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_'
}

// position 把正文里的字节偏移换算成行列，规则同 scan：行按 \n 分，列按 rune 计，都从 1 起。
func position(body string, off int) (line, col int) {
	before := body[:off]
	lineStart := strings.LastIndexByte(before, '\n') + 1
	return strings.Count(before, "\n") + 1, utf8.RuneCountInString(before[lineStart:]) + 1
}

// withSubject 取出规范化函数返回的 Problem，换上调用方知道的 Subject。
func withSubject(err error, s model.Subject) model.Problem {
	var p model.Problem
	errors.As(err, &p) // model 的规范化函数约定 error 就是 Problem
	p.Subject = s
	return p
}

// at 是 s 加上正文位置。
func at(s model.Subject, line, col int) model.Subject {
	s.Line, s.Col = line, col
	return s
}

// formatsOf 按首次出现的顺序列出 series 用到的格式。
func formatsOf(series []catalog.Series) []catalog.Format {
	var out []catalog.Format
	for _, s := range series {
		if f := s.Format(); !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// joinFormats 按代码表对 fragment_mixed_formats 与 template_mixed_formats 的约定，用顿号连接格式。
func joinFormats(formats []catalog.Format) string {
	parts := make([]string, len(formats))
	for i, f := range formats {
		parts[i] = string(f)
	}
	return strings.Join(parts, "、")
}
