// Package archtest 是依赖方向守卫（对应实现计划任务 19）。
//
// 并行开发最容易出的错是「某模块直接 import 了另一个模块的实现」——编译能
// 过、测试能跑，但集成时两个模块就焊在一起了，再也拆不开。本包用一组可执行
// 断言在 CI 里拦住它，而不是只靠评审者记住架构图。
//
// 依据 SPEC §1.1（分包）与实现计划「架构总览」三条规则：
//  1. model 不 import 任何项目内包；
//  2. contract 只 import model（不约束它的子包——conformance 会 import
//     contract，这是允许的）；
//  3. 各实现模块（store/statemachine/render/gateway/console/provider/
//     imports）互不 import；
//
// 加一条反向确认：装配点 cmd/lober 确实在装配全部模块（装配点不存在时该条
// t.Skip，等 M9 创建它）。
package archtest

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// modulePath 是本项目的 module 声明（go.mod 第一行），项目内 import 的判定
// 依据：以这个前缀开头的 import path 才是「项目内」，否则是第三方或标准库，
// 依赖方向守卫不关心它们。
const modulePath = "github.com/lober/lober"

// pkgInfo 对应 `go list -json` 输出里本守卫关心的字段子集。
//
// 三种 import 都要看（计划任务 19 步骤 1 明确要求「含测试 import」）：
//   - Imports：包正文的 import
//   - TestImports：包内 _test.go（package X）的 import
//   - XTestImports：包外部测试（package X_test）的 import
//
// 少看任何一种都会漏判——例如某模块的实现代码规规矩矩只 import contract，
// 但它的白盒测试为了造夹具偷偷 import 了另一个模块的具体类型，编译测试都能
// 过，同样会把两个模块焊死，只是要等到跑测试才会显形（而 go build ./... 不会
// 触发编译测试文件，看不出这类违规）。
type pkgInfo struct {
	ImportPath   string
	Imports      []string
	TestImports  []string
	XTestImports []string
}

// loadPackages 对整个项目跑一次 `go list -json ./...`，解析出每个包的三种
// import 列表。
//
// 用一次调用覆盖全部包，而不是每个包各跑一次 `go list`——项目包数量会随
// M2-M9 逐步增长到几十个，逐包调用子进程的开销会线性放大，一次 JSON 流式
// 输出则是常数次子进程创建。
func loadPackages(t *testing.T) []pkgInfo {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -json ./... 执行失败: %v —— 依赖方向守卫本身依赖 go list 的输出，"+
			"跑不起来这条守卫就完全失去意义，必须先修好环境", err)
	}

	// go list -json ./... 对多个包的输出是「多个 JSON 对象背靠背拼接」，
	// 不是一个 JSON 数组，需要用 Decoder 逐个解码。
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []pkgInfo
	for dec.More() {
		var p pkgInfo
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("解析 go list -json 输出失败: %v —— 输出格式与预期不符，"+
				"可能是 Go 版本升级改了字段，需要人工核对", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// repoRoot 返回本仓库根目录。archtest 包本身就在仓库内（internal/archtest），
// 用 go list 反查 module 根，不硬编码路径——测试要能在任何 checkout 位置跑
// 通，不能假设仓库一定克隆在某个固定绝对路径下。
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD 执行失败: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	dir := strings.TrimSuffix(gomod, "\\go.mod")
	dir = strings.TrimSuffix(dir, "/go.mod")
	return dir
}

// internalImports 从三种 import 列表里筛出「项目内」的那部分（以 modulePath
// 为前缀），去重后按字典序排列。
//
// 只看项目内 import：标准库与第三方依赖（gorm.io/gorm、go-cmp 等）
// 不受本守卫约束，混进结果只会让断言失败信息淹没在噪声里。
func internalImports(p pkgInfo) []string {
	seen := make(map[string]bool)
	var out []string
	for _, list := range [][]string{p.Imports, p.TestImports, p.XTestImports} {
		for _, imp := range list {
			if !strings.HasPrefix(imp, modulePath) {
				continue
			}
			if imp == p.ImportPath {
				continue // 包内测试 import 自己的正文，不算「依赖别人」
			}
			if seen[imp] {
				continue
			}
			seen[imp] = true
			out = append(out, imp)
		}
	}
	sort.Strings(out)
	return out
}

// findPkg 按 import path 精确查找一个包，找不到时返回 nil——调用方据此
// 决定是 t.Fatal（包理应存在）还是 t.Skip（包可能还没被创建，比如装配点）。
func findPkg(pkgs []pkgInfo, importPath string) *pkgInfo {
	for i := range pkgs {
		if pkgs[i].ImportPath == importPath {
			return &pkgs[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 规则一：model 的项目内依赖必须为空（架构总览表第一行）
// ---------------------------------------------------------------------------

// TestModelHasNoInternalDeps 锁住「model 是依赖图的根」这条架构前提。
//
// 为什么这条重要：model 一旦引用了别的项目内包，全部下游模块（M2-M9 都直接
// 或间接依赖 model）都会被那个包的依赖链污染——比如 model 不小心 import 了
// internal/config，则任何只想用 model.Task 类型的模块都被迫连带引入 YAML
// 解析依赖，独立编译测试的前提就没了。这不是「风格问题」，是并行开发能不能
// 成立的地基。
func TestModelHasNoInternalDeps(t *testing.T) {
	pkgs := loadPackages(t)
	const modelPkg = modulePath + "/internal/core/model"

	p := findPkg(pkgs, modelPkg)
	if p == nil {
		t.Fatalf("go list ./... 找不到 %s —— model 包应该始终存在，找不到说明 loadPackages "+
			"或仓库结构本身有问题，不是「暂未创建」能解释的", modelPkg)
	}

	if got := internalImports(*p); len(got) != 0 {
		t.Errorf("model 的项目内依赖 = %v, want 空 —— SPEC §1.1 与实现计划「架构总览」"+
			"明确要求 model 是依赖图的根：它引用了 %v 之后，"+
			"所有依赖 model 的模块都会被这些包的依赖链间接污染，"+
			"并行开发『各模块只通过 model/contract 通信』的前提就不成立了", got, got)
	}
}

// ---------------------------------------------------------------------------
// 规则二：contract 自身的项目内依赖只能是 model（不约束它的子包）
// ---------------------------------------------------------------------------

// TestContractDependsOnlyOnModel 只锁 contract 包本身（import path 精确等于
// internal/core/contract），不锁它的子包。
//
// 为什么这条重要：contract 是「跨模块接口层」，一旦它引用了某个实现细节
// （哪怕只是引用了 config 包），所有实现 contract 接口的模块就被迫连带依赖
// 那个实现细节，「接口层带上实现依赖，模块就无法独立编译测试」（架构总览
// 表第二行）。
//
// 为什么不约束子包：internal/core/contract/conformance 的存在意义就是
// import contract 来跑可执行行为断言，这是计划任务 19 设计要点明确允许的
// 例外——如果误把这条规则写成「路径前缀匹配」而不是「路径精确匹配」，
// conformance 自己都会被这条守卫误判为违规，跑不起来。
func TestContractDependsOnlyOnModel(t *testing.T) {
	pkgs := loadPackages(t)
	const (
		contractPkg = modulePath + "/internal/core/contract"
		modelPkg    = modulePath + "/internal/core/model"
	)

	p := findPkg(pkgs, contractPkg)
	if p == nil {
		t.Fatalf("go list ./... 找不到 %s —— contract 包应该始终存在", contractPkg)
	}

	got := internalImports(*p)
	want := []string{modelPkg}
	if !equalStrings(got, want) {
		t.Errorf("contract 的项目内依赖 = %v, want %v —— 实现计划「架构总览」明确要求"+
			"contract 只 import model：接口层一旦带上除 model 以外的项目内依赖，"+
			"所有依赖 contract 接口编程的模块（M2-M9 全部模块）都会被那个依赖间接拖累，"+
			"『模块只通过 contract 通信、互不 import 实现包』的隔离就形同虚设", got, want)
	}
}

// equalStrings 比较两个已排序的字符串切片是否完全相等，用于「依赖集合
// 恰好等于某个精确集合」这类断言（不能用长度+子集来代替，多一个少一个都
// 要报错，且要在错误信息里把完整实际值打出来，不能只说「不匹配」）。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// 规则三：按包路径前缀归到各模块，模块之间互不 import
// ---------------------------------------------------------------------------

// moduleGroups 是「模块名 → 该模块下所有包路径前缀」的白名单，对应实现
// 计划索引文档「模块清单」表里 M2-M8 七个包路径列（store/postgres、
// statemachine、render、gateway、console、provider、imports）。
//
// 之所以是「模块名 → 多个前缀」而不是「包路径 → 模块名」的反向映射：
// 一个模块可能拆成多个子包（例如 render 拆出 render/guard 放注入防护，
// console 未来会拆出 console/ui 放 M8 的 WebUI 装配），子包必须跟主包
// 归为同一个模块，否则「render 的子包 import render」会被误判成「跨模块
// import」。
//
// 不含 internal/core/{model,contract,fake}、internal/apierr、
// internal/config——这些是地基层，供全部模块自由引用，不参与「模块互斥」
// 这条规则（架构总览表只约束 model/contract 自身，地基层内部的引用关系
// 由前两条规则单独锁住）。也不含 internal/archtest（本包自己）与
// cmd/lober（装配点，规则五单独处理，且装配点的职责就是看见所有实现）。
var moduleGroups = map[string][]string{
	"store":        {modulePath + "/internal/store"},
	"statemachine": {modulePath + "/internal/statemachine"},
	"render":       {modulePath + "/internal/render"},
	"gateway":      {modulePath + "/internal/gateway"},
	"console":      {modulePath + "/internal/console"},
	"provider":     {modulePath + "/internal/provider"},
	"imports":      {modulePath + "/internal/imports"},
}

// moduleOf 返回 importPath 所属的模块名；不属于任何已知模块（地基层、
// archtest 自己、cmd/lober、或尚未创建的包）时返回空字符串与 false。
//
// 前缀匹配且要求「精确等于前缀，或以前缀+'/'开头」——不能用裸的
// strings.HasPrefix，否则 internal/store 会误吃到未来某个假设存在的
// internal/storefront 之类同前缀但语义无关的包（当前项目没有这种包，
// 但守卫规则本身不应该依赖「恰好没有」这种偶然性）。
func moduleOf(importPath string) (name string, ok bool) {
	for mod, prefixes := range moduleGroups {
		for _, prefix := range prefixes {
			if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
				return mod, true
			}
		}
	}
	return "", false
}

// TestModulesDoNotImportEachOther 断言 M2-M8 七个模块两两之间不存在任何
// 项目内 import（含测试 import）。
//
// 为什么这条重要：这是实现计划反复强调的并行开发唯一致命风险——「两个
// 模块直接互相 import，编译能过、测试能跑，但集成时就焊在一起，再也拆不
// 开，并行开发失去意义」。例如 gateway 为了拿到 statemachine 的某个具体
// 类型方便起见直接 import 了 internal/statemachine，而不是通过
// contract.StateMachine 接口编程——这种违规不会体现在任何单元测试失败上，
// 只会在下一次想要单独重构 statemachine 内部实现时突然发现 gateway 的
// 编译也跟着炸。
func TestModulesDoNotImportEachOther(t *testing.T) {
	pkgs := loadPackages(t)

	for _, p := range pkgs {
		fromMod, ok := moduleOf(p.ImportPath)
		if !ok {
			continue // 不是七个受限模块之一（地基层/archtest/cmd 等），跳过
		}
		for _, imp := range internalImports(p) {
			toMod, ok := moduleOf(imp)
			if !ok {
				continue // 依赖的是地基层包（model/contract/apierr/config 等），允许
			}
			if toMod == fromMod {
				continue // 同一模块内部的子包互相引用，允许
			}
			t.Errorf("包 %s（属于模块 %q）import 了 %s（属于模块 %q）—— "+
				"实现计划「架构总览」明确禁止各实现模块互相 import：一旦这条成立，"+
				"两个模块就被焊在一起，任何一方想独立重构内部实现都会牵连另一方，"+
				"『M2-M8 完全并行、集成时零接口返工』的前提就被破坏了。"+
				"跨模块通信必须改成通过 internal/core/contract 的接口编程",
				p.ImportPath, fromMod, imp, toMod)
		}
	}
}

// ---------------------------------------------------------------------------
// 规则五（反向确认）：装配点 cmd/lober 确实在装配
// ---------------------------------------------------------------------------

// TestAssemblyPointWiresModules 反向确认 cmd/lober 存在时确实 import 了
// 「不止一个」M2-M8 模块——只 import 一个模块的 cmd/lober 不是「装配」，
// 是「转发」，说明其余模块还没接进来，装配没有真正完成。
//
// 为什么这条重要：前面几条规则只能防止「模块之间焊死」，但不能防止另一种
// 反向错误——把全部实现都写成通过接口编程、互不 import，结果 cmd/lober
// 忘了真正接入某个模块的具体实现，编译能过（因为接口层已经自洽），但跑
// 起来的二进制缺胳膊少腿。这条断言把「装配点确实看见了多个模块」变成
// 可执行检查，而不是只靠人工核对 wire.go 有没有漏写一行。
//
// 装配点尚不存在时（M1 阶段，cmd/lober 还没被 M9 创建）本条 t.Skip，不算
// 失败——计划任务 19 步骤 5 与验证小节明确要求「装配点还不存在时跳过该
// 条，M9 才会创建它」。
func TestAssemblyPointWiresModules(t *testing.T) {
	pkgs := loadPackages(t)
	const cmdPkg = modulePath + "/cmd/lober"

	p := findPkg(pkgs, cmdPkg)
	if p == nil {
		t.Skip("cmd/lober 尚不存在——这是 M1 阶段的预期状态，装配点由 M9 创建，" +
			"此刻跳过本条反向确认，等 M9 完成后这条测试会自动生效")
	}

	seen := make(map[string]bool)
	for _, imp := range internalImports(*p) {
		if mod, ok := moduleOf(imp); ok {
			seen[mod] = true
		}
	}
	if len(seen) < 2 {
		var got []string
		for mod := range seen {
			got = append(got, mod)
		}
		sort.Strings(got)
		t.Errorf("cmd/lober 的项目内依赖里只覆盖了 %d 个已知模块（%v），want 至少 2 个 —— "+
			"装配点存在的唯一理由就是『看见所有实现』（架构总览「cmd/lober（M9）是唯一"+
			"豁免的包」），只 import 一个模块看起来更像转发或半成品装配，"+
			"不是实现计划要求的『组装全部模块』（模块清单表 M9 行）", len(seen), got)
	}
}
