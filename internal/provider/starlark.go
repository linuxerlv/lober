// 本文件实现 contract.TemplateResolver 的 Starlark 后端（P3-04 第二半），
// 依据 SPEC §7.6。
//
// # 开工前提未满足，这一点要先说清楚
//
// §7.6 的引入信号是「规则表开始长出嵌套条件」，而本仓库的规则表**是平的、
// 且在生产路径上根本没被使用**（internal/provider/resolver.go 的
// LoadRulesYAML 注释：factory.go 总是传空规则）。P3-04 的开工判断因此是
// 「推迟」——本实现是在使用者知情后按指令做的，不是有依据地做的。
// 完整判断见 docs/phase3-grpc-starlark-判断.md，包括「将来确认不需要时就删掉，
// 沉没成本不是保留的理由」这一条。
//
// # 为什么 Starlark 而不是别的脚本语言
//
// Starlark 是刻意设计成**可嵌入、确定性、无环境访问**的 Python 方言（Bazel
// 的配置语言）。这三点正好对应 contract.TemplateResolver 的硬约束：
//
//	纯决策、不做 IO、行为完全确定 —— 这套系统会擦盘，决策函数若行为不完全
//	确定则故障无法复现（那份接口文档的原话）。
//
// 换成 Lua 或 JavaScript 要自己把宿主能力一项项关掉，而「漏关一项」不会报错，
// 只会在某天让一个决策变得不可复现。Starlark 的默认状态就是关着的。
//
// # 本实现额外关掉与限制的东西
//
//   - `print` 被拒绝（默认的 print 会往外写，那是副作用）
//   - `load()` 拒绝加载任何模块（脚本必须自包含）
//   - 求值步数有上限（死循环脚本不能挂住建任务的同步路径）
//   - 返回值形状严格校验（空 template_id 会让建任务去查一个不存在的模板，
//     而那时的错误信息指向「模板不存在」，把排查引向错误方向）
package provider

import (
	"context"
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/lober/lober/internal/core/contract"
)

// starlarkResolveFuncName 是脚本里必须定义的入口函数名。
const starlarkResolveFuncName = "resolve"

// starlarkMaxSteps 是单次决策的求值步数上限。
//
// 决策发生在建任务的同步路径上（§7.4 阻塞语义）。没有上限的话，一个死循环
// 脚本会把那个 HTTP 请求永久挂住，而更糟的是它占着一个 goroutine 与一个数据库
// 连接——几次之后整个控制台不可用。
//
// 一百万步对一个「几十条 if 的选模板逻辑」是极宽裕的（那种脚本通常几百步以内
// 就跑完），同时又能在一秒量级内中断一个死循环。取值宽松是刻意的：宁可放过一个
// 写得很绕的合法脚本，也不要让运维遇到「脚本没问题但被莫名中断」。
const starlarkMaxSteps = 1_000_000

// starlarkTemplateResolver 是 contract.TemplateResolver 的 Starlark 实现。
//
// 持有的是**已编译的程序**而不是源码文本：编译一次、每次决策只做求值，省掉
// 重复解析；同时让语法错误在构造期（即启动时）就暴露，而不是等运维建第一个
// 任务时。
type starlarkTemplateResolver struct {
	program *starlark.Program

	// globals 是脚本顶层执行一次之后的全局环境，每次 Resolve 从里面取
	// resolve 函数。
	//
	// **顶层只执行一次**，之后每次决策各自新建 Thread 去调 resolve。这样脚本
	// 顶层若有昂贵的常量表构造，不会在每次建任务时重跑；而 Starlark 的值在
	// 顶层执行结束后被冻结（starlark.StringDict 上的 Freeze），因此多次并发
	// 决策共享这份环境是安全的——冻结之后任何写入都会报错，不会出现「上一次
	// 决策改了某个全局变量、影响下一次」这种最难查的不确定性。
	globals starlark.StringDict
}

// NewStarlarkTemplateResolver 编译脚本并返回一个 TemplateResolver。
//
// 在构造期（而不是首次决策时）做三件事：编译、执行顶层、确认 resolve 函数
// 存在。三者任一失败即返回错误——配置错误应当在启动时暴露。
func NewStarlarkTemplateResolver(script string) (contract.TemplateResolver, error) {
	// 语法选项全部用默认值（即 Starlark 规范的核心子集），不开任何实验特性：
	// 决策脚本是运维写的，语言表面越小越好，而实验特性将来可能改语义。
	opts := &syntax.FileOptions{}

	thread := &starlark.Thread{
		Name: "lober-resolver-load",
		// Load 拒绝一切模块加载：脚本必须自包含。允许 load() 意味着决策依赖
		// 脚本之外的文件，而那让「同一份脚本同样的输入」不再保证同样的结果。
		Load: func(_ *starlark.Thread, module string) (starlark.StringDict, error) {
			return nil, fmt.Errorf("provider: 决策脚本不允许 load(%q)——脚本必须自包含。"+
				"允许加载外部模块意味着决策依赖脚本之外的文件，"+
				"「同一份脚本 + 同样的输入 → 同样的结果」这条保证就不成立了", module)
		},
		// Print 拒绝：默认的 print 往 stderr 写，那是副作用。纯决策函数不该有
		// 任何可观察的外部效果。
		Print: func(_ *starlark.Thread, msg string) {
			// 无法在这里返回 error（Print 的签名不允许），因此改为在求值时
			// 通过 predeclared 覆盖 print 为一个直接报错的内置函数，见下面的
			// starlarkPredeclared。本回调保留为空是兜底：万一某条路径绕过了
			// 覆盖，也不产生任何输出。
			_ = msg
		},
	}

	// 返回三个值：解析出的语法树、编译后的程序、错误。语法树这里用不到——
	// 我们只需要能被求值的 Program。
	_, program, err := starlark.SourceProgramOptions(
		opts, "resolver.star", script, starlarkPredeclared().Has)
	if err != nil {
		return nil, fmt.Errorf("provider: 决策脚本编译失败: %w", err)
	}

	initialized, err := program.Init(thread, starlarkPredeclared())
	if err != nil {
		return nil, fmt.Errorf("provider: 决策脚本顶层执行失败: %w", err)
	}
	// 冻结：之后任何写入都会报错，多次并发决策共享这份环境是安全的。
	initialized.Freeze()

	fn, ok := initialized[starlarkResolveFuncName]
	if !ok {
		return nil, fmt.Errorf("provider: 决策脚本里没有定义 %s 函数——"+
			"这类错误应当在启动时暴露，不是等运维建第一个任务时才发现",
			starlarkResolveFuncName)
	}
	if _, isCallable := fn.(starlark.Callable); !isCallable {
		return nil, fmt.Errorf("provider: 决策脚本的 %s 不是函数（是 %s）",
			starlarkResolveFuncName, fn.Type())
	}

	return &starlarkTemplateResolver{program: program, globals: initialized}, nil
}

// starlarkPredeclared 返回预声明环境。
//
// 刻意只覆盖 print（把它变成一个直接报错的函数），不添加任何能力。Starlark 的
// 默认内置集合本身就不含时间、随机数、文件与网络——这正是选它而不选 Lua 或
// JavaScript 的理由（那两者要自己一项项关掉，而漏关一项不会报错）。
//
// print 之所以要显式拦：它是默认内置里唯一有外部可观察效果的东西。允许它意味着
// 决策脚本可以往服务端日志里写任意内容，而脚本作者是运维、不是经过审计的代码。
func starlarkPredeclared() starlark.StringDict {
	return starlark.StringDict{
		"print": starlark.NewBuiltin("print", func(
			_ *starlark.Thread, _ *starlark.Builtin,
			_ starlark.Tuple, _ []starlark.Tuple,
		) (starlark.Value, error) {
			return nil, fmt.Errorf("provider: 决策脚本不允许调用 print——" +
				"纯决策函数不该有任何可观察的外部效果；" +
				"要排查脚本行为请用 dry-run 端点，不要往服务端日志里写")
		}),
	}
}

// Resolve 用脚本决策模板。
//
// 每次调用新建一个 Thread：Thread 承载求步数计数与调用栈，复用会让上一次决策的
// 步数累加到下一次，最终表现为「跑了几十次之后每次决策都超步数上限」——一种
// 只在长期运行后出现、且与输入无关的故障。
func (r *starlarkTemplateResolver) Resolve(
	ctx context.Context,
	form map[string]string,
	facts contract.HWFacts,
) (contract.TemplateRef, error) {
	thread := &starlark.Thread{Name: "lober-resolve"}
	thread.SetMaxExecutionSteps(starlarkMaxSteps)

	// ctx 取消时中断求值。决策在同步路径上，客户端断开或请求超时之后没有必要
	// 继续算——那只是白占 CPU。
	//
	// Cancel 是并发安全的（starlark 文档明确说明），因此可以从另一个 goroutine
	// 调。done 用来在正常返回后收掉这个 goroutine，不泄漏。
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			thread.Cancel("上下文已取消")
		case <-done:
		}
	}()

	fn := r.globals[starlarkResolveFuncName]
	result, err := starlark.Call(thread, fn, starlark.Tuple{
		starlarkStringDict(form),
		starlarkStringDict(factsToMap(facts)),
	}, nil)
	if err != nil {
		return contract.TemplateRef{}, fmt.Errorf("provider: 决策脚本求值失败: %w", err)
	}

	return templateRefFromStarlark(result)
}

// factsToMap 把 HWFacts 摊成脚本可读的 map。
//
// **键名是对外约定**，与 SPEC §5.4 的 fact 命名空间同一性质：一旦有运维写的
// 脚本在用就不能改名。四个键与 contract.HWFacts 的字段一一对应，用下划线风格
// （Starlark 是 Python 方言，snake_case 是那边的惯例）。
func factsToMap(f contract.HWFacts) map[string]string {
	return map[string]string{
		"os_family": f.OSFamily,
		"vendor":    f.Vendor,
		"product":   f.Product,
		"arch":      f.Arch,
	}
}

// starlarkStringDict 把 Go 的 map[string]string 转成 Starlark 的 dict。
//
// 转换后立刻 Freeze：脚本改不了入参。不冻结的话脚本可以改 facts 再传给别处，
// 而那种「输入被就地修改」的写法会让脚本行为依赖执行顺序。
func starlarkStringDict(m map[string]string) *starlark.Dict {
	d := starlark.NewDict(len(m))
	for k, v := range m {
		// SetKey 对一个新建的 dict 不会失败（键是字符串、dict 未冻结），
		// 错误忽略是安全的；返回 error 会让调用点为一个不可能的分支加一层判断。
		_ = d.SetKey(starlark.String(k), starlark.String(v))
	}
	d.Freeze()
	return d
}

// templateRefFromStarlark 把脚本返回值转成 TemplateRef 并严格校验形状。
//
// 严格校验的理由：一个空的 template_id 会让建任务去查一个不存在的模板，而那时
// 的错误信息是「模板不存在」——把排查引向「谁删了模板」而不是「决策脚本写错了」。
// 在这里拦住，错误信息才指向真正的原因。
func templateRefFromStarlark(v starlark.Value) (contract.TemplateRef, error) {
	dict, ok := v.(*starlark.Dict)
	if !ok {
		return contract.TemplateRef{}, fmt.Errorf(
			"provider: 决策脚本的 %s 返回了 %s，want dict（形如 "+
				`{"template_id": "tpl-x", "template_version": 1}）`,
			starlarkResolveFuncName, v.Type())
	}

	id, err := starlarkDictString(dict, "template_id")
	if err != nil {
		return contract.TemplateRef{}, err
	}
	if id == "" {
		return contract.TemplateRef{}, fmt.Errorf(
			"provider: 决策脚本返回的 template_id 是空字符串——" +
				"空 id 会让建任务去查一个不存在的模板，那时的错误信息指向" +
				"「模板不存在」，会把排查引向错误方向")
	}

	version, err := starlarkDictInt(dict, "template_version")
	if err != nil {
		return contract.TemplateRef{}, err
	}
	if version <= 0 {
		return contract.TemplateRef{}, fmt.Errorf(
			"provider: 决策脚本返回的 template_version = %d，必须是正整数"+
				"（模板版本从 1 起，0 与负数在存储层查不到任何记录）", version)
	}

	return contract.TemplateRef{TemplateID: id, TemplateVersion: version}, nil
}

// starlarkDictString 从 dict 里取一个字符串字段。
func starlarkDictString(d *starlark.Dict, key string) (string, error) {
	v, found, err := d.Get(starlark.String(key))
	if err != nil || !found {
		return "", fmt.Errorf("provider: 决策脚本返回的 dict 里缺 %q 字段", key)
	}
	s, ok := starlark.AsString(v)
	if !ok {
		return "", fmt.Errorf("provider: 决策脚本返回的 %q 是 %s，want string", key, v.Type())
	}
	return s, nil
}

// starlarkDictInt 从 dict 里取一个整数字段。
//
// 用 starlark.AsInt32 而不是 AsInt[int]：版本号是小整数，而 AsInt32 对超出
// 范围的值会明确报错，不会静默截断——静默截断出来的版本号会指向另一个真实存在
// 的模板版本，那是最坏的一种失败。
func starlarkDictInt(d *starlark.Dict, key string) (int, error) {
	v, found, err := d.Get(starlark.String(key))
	if err != nil || !found {
		return 0, fmt.Errorf("provider: 决策脚本返回的 dict 里缺 %q 字段", key)
	}
	i, ok := v.(starlark.Int)
	if !ok {
		return 0, fmt.Errorf("provider: 决策脚本返回的 %q 是 %s，want int", key, v.Type())
	}
	n, err := starlark.AsInt32(i)
	if err != nil {
		return 0, fmt.Errorf("provider: 决策脚本返回的 %q 超出 int32 范围: %w", key, err)
	}
	return n, nil
}

var _ contract.TemplateResolver = (*starlarkTemplateResolver)(nil)
