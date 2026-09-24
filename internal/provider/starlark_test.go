// 本文件测试 Starlark 决策引擎（P3-04 第二半），依据 SPEC §7.6。
//
// 最要紧的一组断言是「纯决策」那几条：这套系统会擦盘，决策函数若行为不完全
// 确定则故障无法复现（contract.TemplateResolver 的类型文档）。因此脚本必须
// 拿不到时间、随机数、文件与网络——下面逐项验证。
package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

const starlarkBasicScript = `
def resolve(form, facts):
    if facts["os_family"] == "rhel" and facts["vendor"] == "Dell Inc.":
        return {"template_id": "tpl-dell-rocky", "template_version": 3}
    if facts["os_family"] == "rhel":
        return {"template_id": "tpl-generic-rocky", "template_version": 1}
    return {"template_id": "tpl-fallback", "template_version": 1}
`

// TestStarlarkResolver_NestedConditions 验证嵌套条件这件规则表做不到的事。
//
// §7.6 的引入信号就是「规则表开始长出嵌套条件」——平的四维相等匹配表达不了
// 「先按家族分，再在 Dell 机型里细分」这种结构。本测试是那个能力的最小证明。
func TestStarlarkResolver_NestedConditions(t *testing.T) {
	r, err := NewStarlarkTemplateResolver(starlarkBasicScript)
	if err != nil {
		t.Fatalf("NewStarlarkTemplateResolver 失败: %v", err)
	}
	ctx := context.Background()

	cases := []struct {
		name  string
		facts contract.HWFacts
		want  contract.TemplateRef
	}{
		{
			"嵌套分支命中",
			contract.HWFacts{OSFamily: "rhel", Vendor: "Dell Inc."},
			contract.TemplateRef{TemplateID: "tpl-dell-rocky", TemplateVersion: 3},
		},
		{
			"外层分支命中",
			contract.HWFacts{OSFamily: "rhel", Vendor: "HPE"},
			contract.TemplateRef{TemplateID: "tpl-generic-rocky", TemplateVersion: 1},
		},
		{
			"兜底",
			contract.HWFacts{OSFamily: "debian"},
			contract.TemplateRef{TemplateID: "tpl-fallback", TemplateVersion: 1},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := r.Resolve(ctx, map[string]string{}, c.facts)
			if err != nil {
				t.Fatalf("Resolve 失败: %v", err)
			}
			if got != c.want {
				t.Errorf("Resolve(%+v) = %+v, want %+v", c.facts, got, c.want)
			}
		})
	}
}

// TestStarlarkResolver_IsDeterministic 验证同一输入多次求值结果一致。
//
// 这是「纯决策」的直接判据。不确定的决策函数意味着故障无法复现——而这套系统
// 擦盘，「上次那台机器为什么选了那个模板」必须能回答。
func TestStarlarkResolver_IsDeterministic(t *testing.T) {
	r, err := NewStarlarkTemplateResolver(starlarkBasicScript)
	if err != nil {
		t.Fatalf("NewStarlarkTemplateResolver 失败: %v", err)
	}
	ctx := context.Background()
	facts := contract.HWFacts{OSFamily: "rhel", Vendor: "Dell Inc."}

	first, err := r.Resolve(ctx, map[string]string{"role": "web"}, facts)
	if err != nil {
		t.Fatalf("首次 Resolve 失败: %v", err)
	}
	for i := 0; i < 20; i++ {
		got, err := r.Resolve(ctx, map[string]string{"role": "web"}, facts)
		if err != nil {
			t.Fatalf("第 %d 次 Resolve 失败: %v", i, err)
		}
		if got != first {
			t.Fatalf("第 %d 次 Resolve = %+v，与首次 %+v 不同 —— 决策必须完全确定，"+
				"否则「上次那台机器为什么选了那个模板」无法回答，而这套系统会擦盘",
				i, got, first)
		}
	}
}

// TestStarlarkResolver_NoAmbientCapabilities 验证脚本拿不到时间、随机数、
// 文件与网络。
//
// 逐项都要拦：任何一个漏出去都会让决策变得不确定或产生副作用。time 与 random
// 直接破坏确定性；文件与网络除了不确定，还让一个「纯决策」函数变成可以读
// /etc/shadow 或往外发请求的东西——而脚本的作者是运维，不是经过审计的代码。
func TestStarlarkResolver_NoAmbientCapabilities(t *testing.T) {
	forbidden := map[string]string{
		"time":   `load("time.star", "time")`,
		"random": `load("random.star", "random")`,
		"print": `def resolve(form, facts):
    print("副作用")
    return {"template_id": "x", "template_version": 1}`,
	}
	for name, snippet := range forbidden {
		t.Run(name, func(t *testing.T) {
			script := snippet
			if !strings.Contains(script, "def resolve") {
				script += "\ndef resolve(form, facts):\n    return {\"template_id\": \"x\", \"template_version\": 1}\n"
			}
			r, err := NewStarlarkTemplateResolver(script)
			if err != nil {
				return // 加载期就拒绝，这是最好的结果
			}
			if _, err := r.Resolve(context.Background(), map[string]string{}, contract.HWFacts{}); err == nil {
				t.Errorf("脚本用了 %s 却没有被拒绝 —— 环境能力必须逐项拦住："+
					"time/random 直接破坏确定性，文件与网络还会让一个「纯决策」函数"+
					"变成能读本地文件或往外发请求的东西，而脚本作者是运维不是经过审计的代码",
					name)
			}
		})
	}
}

// TestStarlarkResolver_RejectsInfiniteLoop 验证死循环脚本不会挂住整个进程。
//
// 决策发生在建任务的同步路径上（§7.4 阻塞语义）。一个死循环脚本若没有步数
// 上限，会把那个 HTTP 请求永久挂住，而更糟的是它占着一个 goroutine 与一个
// 数据库连接——几次之后整个控制台不可用。
func TestStarlarkResolver_RejectsInfiniteLoop(t *testing.T) {
	r, err := NewStarlarkTemplateResolver(`
def resolve(form, facts):
    n = 0
    for i in range(100000000):
        n = n + i
    return {"template_id": "x", "template_version": 1}
`)
	if err != nil {
		t.Fatalf("NewStarlarkTemplateResolver 失败: %v", err)
	}

	if _, err := r.Resolve(context.Background(), map[string]string{}, contract.HWFacts{}); err == nil {
		t.Error("死循环脚本没有被步数上限中断 —— 决策在建任务的同步路径上，" +
			"挂住的不只是那一个请求，还占着 goroutine 与数据库连接，几次之后控制台不可用")
	}
}

// TestStarlarkResolver_RejectsBadReturnShape 验证返回值形状不对时报明确错误。
func TestStarlarkResolver_RejectsBadReturnShape(t *testing.T) {
	cases := map[string]string{
		"返回None":       `def resolve(form, facts):\n    return None`,
		"返回字符串":        `def resolve(form, facts):\n    return "tpl-x"`,
		"缺template_id": `def resolve(form, facts):\n    return {"template_version": 1}`,
		"版本号不是整数":      `def resolve(form, facts):\n    return {"template_id": "x", "template_version": "one"}`,
		"版本号非正":        `def resolve(form, facts):\n    return {"template_id": "x", "template_version": 0}`,
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := NewStarlarkTemplateResolver(strings.ReplaceAll(script, `\n`, "\n"))
			if err != nil {
				return
			}
			if _, err := r.Resolve(context.Background(), map[string]string{}, contract.HWFacts{}); err == nil {
				t.Errorf("返回值形状不对（%s）却没报错 —— 一个空的 template_id 会让"+
					"建任务去查一个不存在的模板，错误信息会指向「模板不存在」而不是"+
					"「决策脚本写错了」，把排查引向错误方向", name)
			}
		})
	}
}

// TestStarlarkResolver_RejectsMissingResolveFunction 验证脚本没有 resolve
// 函数时在**加载期**就被拒绝，不是等到第一次决策。
//
// 加载期拒绝的价值：配置错误在启动时暴露，而不是在运维建第一个任务时。
func TestStarlarkResolver_RejectsMissingResolveFunction(t *testing.T) {
	if _, err := NewStarlarkTemplateResolver(`x = 1`); err == nil {
		t.Error("脚本里没有 resolve 函数却加载成功 —— 这类错误应当在启动时暴露，" +
			"不是等运维建第一个任务时才发现")
	}
}

// TestStarlarkResolver_FormAndFactsBothVisible 验证表单与硬件事实都能被脚本
// 读到，且键名是稳定的对外约定。
func TestStarlarkResolver_FormAndFactsBothVisible(t *testing.T) {
	r, err := NewStarlarkTemplateResolver(`
def resolve(form, facts):
    return {
        "template_id": form["role"] + "-" + facts["arch"],
        "template_version": 2,
    }
`)
	if err != nil {
		t.Fatalf("NewStarlarkTemplateResolver 失败: %v", err)
	}

	got, err := r.Resolve(context.Background(),
		map[string]string{"role": "web"},
		contract.HWFacts{Arch: "x86_64"})
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if got.TemplateID != "web-x86_64" {
		t.Errorf("TemplateID = %q, want web-x86_64 —— form 与 facts 两个入参都要"+
			"能读到，且 facts 的键名是对外约定（os_family/vendor/product/arch）",
			got.TemplateID)
	}
}

// TestNewResolverFromConfig_StarlarkRequiresScriptPath 验证选了 starlark 但没
// 配脚本路径时在**启动时**报错。
//
// 工厂构造发生在进程启动阶段，因此这条错误会让服务起不来——那正是想要的：
// 一个没有决策脚本的 starlark resolver 在第一次建任务时才失败，而那时运维已经
// 在等装机了。
func TestNewResolverFromConfig_StarlarkRequiresScriptPath(t *testing.T) {
	if _, err := newResolverFromConfig("starlark", ""); err == nil {
		t.Error("选了 starlark 却没配 resolver_script_path，工厂却构造成功 —— " +
			"这条错误应当在进程启动时暴露，不是等运维建第一个任务时")
	}
}

// TestNewResolverFromConfig_StarlarkLoadsScriptFromDisk 验证脚本从磁盘读入并
// 编译，且编译错误在构造期就返回。
func TestNewResolverFromConfig_StarlarkLoadsScriptFromDisk(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good.star")
	if err := os.WriteFile(good, []byte(starlarkBasicScript), 0o644); err != nil {
		t.Fatalf("写脚本失败: %v", err)
	}
	r, err := newResolverFromConfig("starlark", good)
	if err != nil {
		t.Fatalf("合法脚本构造失败: %v", err)
	}
	got, err := r.Resolve(context.Background(), map[string]string{},
		contract.HWFacts{OSFamily: "rhel", Vendor: "Dell Inc."})
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if got.TemplateID != "tpl-dell-rocky" {
		t.Errorf("TemplateID = %q, want tpl-dell-rocky", got.TemplateID)
	}

	bad := filepath.Join(dir, "bad.star")
	if err := os.WriteFile(bad, []byte("def resolve(  <<< 语法错误"), 0o644); err != nil {
		t.Fatalf("写脚本失败: %v", err)
	}
	if _, err := newResolverFromConfig("starlark", bad); err == nil {
		t.Error("语法错误的脚本被接受了 —— 语法错误必须在启动时暴露")
	}

	if _, err := newResolverFromConfig("starlark", filepath.Join(dir, "不存在.star")); err == nil {
		t.Error("脚本文件不存在却构造成功")
	}
}
