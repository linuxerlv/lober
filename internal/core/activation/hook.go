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

// Hook 判断 hook 版本 h 能不能批准（C8–C11，以及 C19 在片段上的部分）。
//
// 问题的顺序固定：先 hook 整体（名称、参数声明、有没有实现），再按实现的顺序逐份列出，超时
// 最后；一份实现里正文的问题按位置排。hook 整体的问题 Subject 的 Kind 为 hook，ID 为
// 「hook ID@版本」，参数声明的问题 Path 为参数名；实现的问题 Kind 为 impl，Path 为它覆盖的系
// （逗号分隔），涉及正文位置时带上行列。
func Hook(h model.Hook) []model.Problem {
	id := h.ID + "@" + strconv.Itoa(h.Version)
	hookSubj := model.Subject{Kind: "hook", ID: id}
	var out []model.Problem
	if _, _, err := model.NormalizeName(h.Name); err != nil {
		out = append(out, withSubject(err, hookSubj))
	}
	params, ps := checkDecls(h.Params, hookSubj)
	out = append(out, ps...)
	if len(h.Impls) == 0 {
		out = append(out, model.NewProblem("hook_no_impl", hookSubj))
	}
	covered := map[catalog.Series]bool{} // 前面的实现已经覆盖的系
	hasScript := false
	for _, impl := range h.Impls {
		subj := model.Subject{Kind: "impl", ID: id, Path: joinSeries(impl.Series)}
		out = append(out, checkImpl(impl, params, subj, covered)...)
		hasScript = hasScript || impl.Kind == model.ImplScript
	}
	// 超时只约束脚本（C11）：片段在渲染时就写进了应答文件，没有执行这一步。
	if hasScript && (h.TimeoutSec < timeoutMinSec || h.TimeoutSec > timeoutMaxSec) {
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

// checkDecls 逐个校验参数声明，并查名字有没有重复。重名的取第一个作引用的对象，后面的
// 报 param_decl_invalid。问题的 Path 为参数名。
func checkDecls(decls []model.ParamDecl, subj model.Subject) (map[string]paramInfo, []model.Problem) {
	params := make(map[string]paramInfo, len(decls))
	var out []model.Problem
	for _, d := range decls {
		s := subj
		s.Path = d.Name
		ps := d.Validate()
		_, dup := params[d.Name]
		if dup && len(ps) == 0 {
			ps = []model.Problem{model.NewProblem("param_decl_invalid", s, d.Name)}
		}
		for _, p := range ps {
			p.Subject = s
			out = append(out, p)
		}
		if !dup {
			params[d.Name] = paramInfo{decl: d, ok: len(ps) == 0}
		}
	}
	return params, out
}

// checkImpl 查一份实现。covered 记着前面的实现覆盖了哪些系，本份查完后把自己的系加进去。
func checkImpl(impl model.Impl, params map[string]paramInfo, subj model.Subject, covered map[catalog.Series]bool) []model.Problem {
	var out []model.Problem
	seriesOK := implSeriesOK(impl)
	if !seriesOK {
		out = append(out, model.NewProblem("impl_series_invalid", subj))
	}
	// 一个系最多出现在一份实现里（C8），否则渲染时不知道取哪一份。不认识的系已经算作
	// 族或系不对，不再拿来比。
	for i, s := range impl.Series {
		if !s.Valid() || slices.Contains(impl.Series[:i], s) {
			continue
		}
		if covered[s] {
			out = append(out, model.NewProblem("impl_series_overlap", subj, s))
		}
		covered[s] = true
	}
	switch impl.Kind {
	case model.ImplFragment:
		out = append(out, checkFragment(impl, seriesOK, params, subj)...)
	case model.ImplScript:
		out = append(out, checkScript(impl, seriesOK, params, subj)...)
	default:
		// 实现只有片段与脚本两种（C8），代码表里没有专门的代码；种类不认识的当作没写完的
		// 脚本拦下，不能让它通过审批。
		out = append(out, model.NewProblem("script_incomplete", subj))
	}
	return out
}

// implSeriesOK：族合法，系非空、各不相同，且都属于该族（C8）。同一个系勾两次只会来自直接
// 调接口，界面上做不到；也按系不对拦下，不替调用方去重：第 2 层会原样存下这份列表。
func implSeriesOK(impl model.Impl) bool {
	if !impl.Family.Valid() || len(impl.Series) == 0 {
		return false
	}
	for i, s := range impl.Series {
		if s.Family() != impl.Family || slices.Contains(impl.Series[:i], s) {
			return false
		}
	}
	return true
}

// checkFragment 查片段。系不对时推不出格式，跟格式有关的检查（同格式、系统值）都跳过，
// 免得同一个错误换个说法再报一遍；其余检查照做，作者一次能看到更多要改的地方。
func checkFragment(impl model.Impl, seriesOK bool, params map[string]paramInfo, subj model.Subject) []model.Problem {
	var out []model.Problem
	var format catalog.Format // 推不出时为空
	if seriesOK {
		formats := formatsOf(impl.Series)
		if len(formats) == 1 {
			format = formats[0]
		} else {
			out = append(out, model.NewProblem("fragment_mixed_formats", subj, joinFormats(formats)))
		}
	}
	res, scanned := scan.Scan(impl.Body)
	body := make([]model.Problem, 0, len(scanned))
	for _, p := range scanned {
		// 扫描只知道正文，不知道它属于哪份实现：代码、说明与行列原样保留，只改指向。
		p.Subject = at(subj, p.Subject.Line, p.Subject.Col)
		body = append(body, p)
	}
	repeated := map[int]string{} // 行号 → 这一行里第一个逐行重复的列表参数
	for _, seg := range res.Segments {
		s := at(subj, seg.Line, seg.Col)
		switch seg.Kind {
		case scan.KindSlot, scan.KindPart:
			// hook 只有一层（C8）：插槽与系统部件只属于模板正文。
			body = append(body, model.NewProblem("fragment_forbidden_placeholder", s, seg.Raw))
		case scan.KindSys:
			if format != "" && !slices.Contains(format.SystemValues(), seg.Sys) {
				body = append(body, model.NewProblem("sysvalue_not_allowed", s, seg.Sys, format))
			}
		case scan.KindParam:
			body = append(body, checkParamRef(seg, params, s, repeated)...)
		}
	}
	slices.SortStableFunc(body, func(a, b model.Problem) int {
		return cmp.Or(cmp.Compare(a.Subject.Line, b.Subject.Line), cmp.Compare(a.Subject.Col, b.Subject.Col))
	})
	return append(out, body...)
}

// checkParamRef 查正文里的一处参数引用：声明过（C9），是受限类型（C19），子值与 |join 用得对。
// repeated 记着每一行里第一个逐行重复的列表参数。
func checkParamRef(seg scan.Segment, params map[string]paramInfo, s model.Subject, repeated map[int]string) []model.Problem {
	ref := seg.Param
	info, declared := params[ref.Name]
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
	// 逐行重复把整行按列表项各抄一遍。同一行里两个不同的列表参数，没法说清第几份抄本代入
	// 哪一项；同一个列表参数出现几次、取几个子值，每份抄本代入的都是同一项，不冲突。
	if info.decl.List && !ref.Join {
		if first, ok := repeated[seg.Line]; !ok {
			repeated[seg.Line] = ref.Name
		} else if first != ref.Name {
			out = append(out, model.NewProblem("list_repeat_conflict", s, ref.Name))
		}
	}
	return out
}

// subOK 拿一个 IPv4 的 cidr 样例按声明解析，再取子值：哪些类型有哪些子值只在 model 里
// 登记一处，这里不另抄一份。样例对别的类型要么解析不了，要么解析成该类型的值、取子值时被
// 拒，结果都对。netmask 只适用于 IPv4，取值是不是 IPv6 要到参数求值时才知道。
func subOK(d model.ParamDecl, sub string) bool {
	v, err := model.ParamDecl{Name: d.Name, Type: d.Type}.Parse("192.0.2.1/24")
	if err != nil {
		return false
	}
	_, err = v.Sub(sub)
	return err == nil
}

// checkScript 查脚本：写完整了（C10），声明的时机和解释器在覆盖的每个系里都可用（C8、C10），
// 正文里字面出现的参数变量都已声明（C9）。系不对时不按系查能力，理由同 checkFragment。
func checkScript(impl model.Impl, seriesOK bool, params map[string]paramInfo, subj model.Subject) []model.Problem {
	var out []model.Problem
	if strings.TrimSpace(impl.Body) == "" || impl.Interpreter == "" || len(impl.Timings) == 0 {
		out = append(out, model.NewProblem("script_incomplete", subj))
	}
	if seriesOK {
		for _, s := range impl.Series {
			f := s.Format()
			for i, t := range impl.Timings {
				switch {
				case slices.Contains(impl.Timings[:i], t): // 重复声明的时机只查一次
				case !slices.Contains(f.Timings(), t):
					out = append(out, model.NewProblem("script_timing_unsupported", subj, s, t))
				case impl.Interpreter != "" && !slices.Contains(f.Interpreters(t), impl.Interpreter):
					out = append(out, model.NewProblem("script_interpreter_unavailable", subj, s, t, impl.Interpreter))
				}
			}
		}
	}
	return append(out, checkEnvRefs(impl.Body, params, subj)...)
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

// joinSeries 是实现的 Path：它覆盖的系按勾选顺序以逗号连接。实现没有自己的 ID，界面靠它
// 认出是哪一份。
func joinSeries(series []catalog.Series) string {
	parts := make([]string, len(series))
	for i, s := range series {
		parts[i] = string(s)
	}
	return strings.Join(parts, ",")
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

// joinFormats 按代码表对 fragment_mixed_formats 的约定，用顿号连接格式。
func joinFormats(formats []catalog.Format) string {
	parts := make([]string, len(formats))
	for i, f := range formats {
		parts[i] = string(f)
	}
	return strings.Join(parts, "、")
}
