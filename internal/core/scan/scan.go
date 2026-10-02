// Package scan 是正文扫描（spec §4.1）：把模板正文或 hook 片段切成原文与占位符，
// 列出插槽、系统部件、系统值和参数引用，并标出写错的占位符。
//
// 控制台用它做保存与激活校验、给编辑器列出插槽，渲染用它展开占位符。两边必须用同一份，
// 否则会出现编辑器里有这个插槽、渲染时却找不到的情况。扫描不看格式，也不管应答文件本身的
// 语法（C18）：占位符以外的字节原样留在原文段里；某种格式能不能用某个时机、某个系统值，
// 必需哪些部件，由激活判定按格式去查。
package scan

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// Kind 是片段的种类：原文，或四种占位符之一。写错的占位符不单成一种：它留在原文段里，
// 问题另外报出。
type Kind int

const (
	KindText  Kind = iota // 原文
	KindSlot              // 插槽：{{slot:名字}}、{{slot:名字@时机}}
	KindPart              // 系统部件：{{lober:部件}}
	KindSys               // 系统值：{{sys:名字}}
	KindParam             // 参数引用：{{param:名字}}，可带 .子值、|join
)

// kindOf 是占位符种类的写法。
var kindOf = map[string]Kind{
	"slot":  KindSlot,
	"lober": KindPart,
	"sys":   KindSys,
	"param": KindParam,
}

// slotNameMaxRunes 按 rune 计：插槽名给人看，可以用中文，一个汉字算一个。
const slotNameMaxRunes = 32

// Slot 是正文里的一个插槽。名字、类型和时机都只写在占位符里，不在别处另存（C14）。
type Slot struct {
	Name   string
	Exec   bool           // 执行插槽；否则是片段插槽
	Timing catalog.Timing // 仅执行插槽
}

// ParamRef 是一处参数引用：模板正文里指模板参数，片段里指该 hook 的参数。参数是不是列表、
// 子值适不适用于它的类型，扫描时不知道，由激活判定按参数声明去查。
type ParamRef struct {
	Name string
	Sub  string // 子值，如 {{param:addr.netmask}} 的 netmask；没写时为空
	Join bool   // 写了 |join：列表以单个空格连接后内联；没写时逐行重复所在的整行
}

// Segment 是正文的一段：一段原文，或一个占位符。
//
// 行按 \n 分，CRLF 的 \r 留在行尾，算作行尾空白；列按 rune 计。行列都从 1 起，是给编辑器
// 标红用的：按字节算的话，中文正文里标出的位置会错开。
type Segment struct {
	Kind Kind
	Raw  string // 原文；全部段的 Raw 依次拼起来与输入逐字节相同（C18）
	Line int    // 本段第一个字符所在的行
	Col  int    // 本段第一个字符所在的列

	// 下面两项只对占位符有意义，原文段为零值。
	Indent string // 所在行的前导空白（空格与制表符）：片段插槽展开出的每一行都套用它
	Alone  bool   // 独占一行：所在行除了前导空白、它自己和行尾空白，没有别的字符

	// 解析结果，按 Kind 只填其中一项。
	Slot  Slot
	Part  catalog.SystemPart
	Sys   catalog.SystemValue
	Param ParamRef
}

// Result 是扫描结果。四个列表各按出现顺序列出对应种类的每一个占位符，重复的照列：
// 插槽名重复、部件出现两次由模板的激活判定报，它要靠这里去数。
type Result struct {
	Segments []Segment // 按原文顺序；相邻两段不会都是原文
	Slots    []Slot
	Parts    []catalog.SystemPart
	Sys      []catalog.SystemValue
	Params   []ParamRef
}

// Scan 扫描正文 src。占位符有四种：
//   - {{slot:名字}} 是片段插槽，{{slot:名字@时机}} 是执行插槽；
//   - {{lober:部件}} 是系统部件；
//   - {{sys:名字}} 是系统值，其中 {{sys:lbrace}} 输出字面量 {{，用来转义；
//   - {{param:名字}} 是参数引用，还可以写成 名字.子值、名字|join 或 名字.子值|join。
//
// {{ 之后跳过空格，紧跟「小写字母加冒号」的才是 lober 的占位符，其余 {{…}} 原样保留，
// 所以 cloud-init 的 {{ ds.meta_data.local_hostname }} 不受影响；代价是正文里不能直接写
// {{http://…}} 这类字面量，要写就用 {{sys:lbrace}} 转义。占位符从 {{ 起，到同一行里其后
// 的第一个 }} 为止，首尾可以有空格。在 }} 之前先遇到下一个 {{、或者到行尾都没有 }} 的报
// 未闭合：占位符的内容不可能含 {{，外层就截断在内层的 {{ 处，从那里接着扫，同一行后面的
// 占位符不会被吞掉。截断的外层不论种类认不认识都报未闭合；没被截断的，种类不认识先于未闭合。
//
// 有问题时照样返回其余部分的结果，写错的占位符留在原文段里：草稿有错也能保存，编辑器要靠
// 它继续显示插槽列表。问题按出现位置排列，Subject 的 Kind 为 body，行列指向占位符的 {{。
func Scan(src string) (Result, []model.Problem) {
	sc := scanner{src: src, textLine: 1, textCol: 1}
	for start, n := 0, 1; ; n++ {
		end := strings.IndexByte(src[start:], '\n')
		if end < 0 {
			sc.scanLine(n, start, len(src))
			break
		}
		sc.scanLine(n, start, start+end)
		start += end + 1
	}
	sc.flushText(len(src))
	return sc.res, sc.problems
}

// scanner 逐行扫描。还没输出的原文从 textStart 起，textLine、textCol 是它的位置。
type scanner struct {
	src      string
	res      Result
	problems []model.Problem

	textStart, textLine, textCol int
}

// scanLine 扫描第 n 行 src[start:end]，不含 \n。占位符不能跨行，所以逐行找就够了。
func (sc *scanner) scanLine(n, start, end int) {
	line := sc.src[start:end]
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	content := len(strings.TrimRight(line, " \t\r")) // 行尾空白从这里起
	cols := runeCols{line: line, col: 1}
	for i := 0; ; {
		j := strings.Index(line[i:], "{{")
		if j < 0 {
			return
		}
		at := i + j
		word, body, ok := loberPrefix(line, at)
		if !ok {
			// 只跳过一个字节：{{{slot:a}} 里多出来的 { 留作原文，紧跟的占位符照样认出来，
			// 不会原样漏进应答文件。
			i = at + 1
			continue
		}
		// 插槽名、标识、子值里都没有花括号，所以先找下一个 {{：在它之前有 }} 才算闭合，
		// 没有就截断在那里，从内层的 {{ 接着扫。只在截断点之前找 }}：每个占位符都找到行尾
		// 的话，一行写满没闭合的 {{ 时会变成平方级。
		stop, cut := len(line), false
		if k := strings.Index(line[body:], "{{"); k >= 0 {
			stop, cut = body+k, true
		}
		closed := false
		if k := strings.Index(line[body:stop], "}}"); k >= 0 {
			stop, closed, cut = body+k+2, true, false
		}
		i = stop
		seg := Segment{Raw: line[at:stop], Line: n, Col: cols.at(at)}
		subject := model.Subject{Kind: "body", Line: n, Col: seg.Col}
		kind, known := kindOf[word]
		switch {
		case !known && !cut:
			// 种类在冒号前就定了，所以先于闭合检查：把网址写进双花括号的人，看到这一条
			// 才知道要用 {{sys:lbrace}} 转义。
			sc.problems = append(sc.problems, model.NewProblem("placeholder_unknown_kind", subject, word))
			continue
		case !closed:
			// 到行尾都没有 }}，或者被内层的 {{ 截断。截断的外层不论种类都报这一条：同一行
			// 后面还有 {{，作者多半是在写占位符时少了 }}，补上以后种类不对会接着报出来。
			sc.problems = append(sc.problems, model.NewProblem("placeholder_unclosed", subject))
			continue
		}
		seg.Kind = kind
		if code, arg := parse(&seg, strings.TrimRight(line[body:stop-2], " ")); code != "" {
			sc.problems = append(sc.problems, model.NewProblem(code, subject, arg))
			continue
		}
		seg.Indent = line[:indent]
		seg.Alone = at == indent && stop == content
		sc.flushText(start + at)
		sc.add(seg)
		sc.textStart, sc.textLine, sc.textCol = start+stop, n, cols.at(stop)
	}
}

// loberPrefix 认 line[at:] 开头的「{{、可选的空格、小写字母、冒号」，返回种类的写法和冒号
// 之后的位置。不是这个开头的 {{…}} 不归 lober 管。
func loberPrefix(line string, at int) (word string, body int, ok bool) {
	k := at + 2
	for k < len(line) && line[k] == ' ' {
		k++
	}
	w := k
	for w < len(line) && 'a' <= line[w] && line[w] <= 'z' {
		w++
	}
	if w == k || w == len(line) || line[w] != ':' {
		return "", 0, false
	}
	return line[k:w], w + 1, true
}

// parse 解析占位符的内容 body（冒号之后、结尾空格之前的部分），填进 seg；写错时返回问题的
// 代码与参数，没错时代码为空。一个占位符只报一个问题：先查写法，写法对了再查目录里有没有。
func parse(seg *Segment, body string) (model.Code, string) {
	malformed := func() (model.Code, string) { return "placeholder_malformed", seg.Raw }
	switch seg.Kind {
	case KindSlot:
		name, timing, exec := strings.Cut(body, "@")
		if !validSlotName(name) || (exec && !validWord(timing)) {
			return malformed()
		}
		if exec && !catalog.Timing(timing).Valid() {
			return "timing_unknown", timing
		}
		seg.Slot = Slot{Name: name, Exec: exec, Timing: catalog.Timing(timing)}
	case KindPart:
		if !validWord(body) {
			return malformed()
		}
		if !catalog.SystemPart(body).Valid() {
			return "part_unknown", body
		}
		seg.Part = catalog.SystemPart(body)
	case KindSys:
		if !validWord(body) {
			return malformed()
		}
		if !catalog.SystemValue(body).Valid() {
			return "sysvalue_unknown", body
		}
		seg.Sys = catalog.SystemValue(body)
	case KindParam:
		// 参数名与子值都是标识符（model.ValidIdent），|join 只能写在最后。
		ref, join := strings.CutSuffix(body, "|join")
		name, sub, hasSub := strings.Cut(ref, ".")
		if !model.ValidIdent(name) || (hasSub && !model.ValidIdent(sub)) {
			return malformed()
		}
		seg.Param = ParamRef{Name: name, Sub: sub, Join: join}
	}
	return "", ""
}

// validSlotName 认插槽名：1–32 个字母（含中文）、数字、_ 或 -。
func validSlotName(s string) bool {
	n := 0
	for _, r := range s {
		n++
		if n > slotNameMaxRunes || !nameRune(r) {
			return false
		}
	}
	return n > 0
}

// validWord 认时机、部件和系统值的写法，字符同插槽名。这里不按目录的标识收紧，大写、中文
// 都放过去查目录：写法对、目录里却没有的报 *_unknown，说明里列出可选的值，比笼统的
// 「写法不对」好改。带了 . | @ 或空格的，是把子值、|join、时机写错了地方，报写法不对。
func validWord(s string) bool {
	return s != "" && !strings.ContainsFunc(s, func(r rune) bool { return !nameRune(r) })
}

func nameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// runeCols 把行内的字节偏移换算成列号。同一行里查的偏移只增不减，接着上一次往后数，
// 整行只数一遍：一行里写满占位符时也不会变成平方级。
type runeCols struct {
	line     string
	off, col int
}

func (c *runeCols) at(off int) int {
	c.col += utf8.RuneCountInString(c.line[c.off:off])
	c.off = off
	return c.col
}

// flushText 把 textStart 到 upto 之间还没输出的原文输出成一段。写错的占位符也在其中，
// 所以相邻两段不会都是原文。
func (sc *scanner) flushText(upto int) {
	if upto > sc.textStart {
		sc.res.Segments = append(sc.res.Segments, Segment{Kind: KindText, Raw: sc.src[sc.textStart:upto], Line: sc.textLine, Col: sc.textCol})
	}
}

// add 输出一个占位符段，并按种类记进对应的列表。
func (sc *scanner) add(seg Segment) {
	sc.res.Segments = append(sc.res.Segments, seg)
	switch seg.Kind {
	case KindSlot:
		sc.res.Slots = append(sc.res.Slots, seg.Slot)
	case KindPart:
		sc.res.Parts = append(sc.res.Parts, seg.Part)
	case KindSys:
		sc.res.Sys = append(sc.res.Sys, seg.Sys)
	case KindParam:
		sc.res.Params = append(sc.res.Params, seg.Param)
	}
}
