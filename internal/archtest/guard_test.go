package archtest

// 守卫自身的反例测试（T1-2）。源码都放在内存里：写成 testdata 下的文件，会被
// make fmt 的 gofmt 目录递归扫到；取了 x_linux.go 这类名字，还会被构建约束
// 静默排除，测试就假绿了。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// pkgScan 只有测试用得到：方向规则不限制 scan。
const pkgScan = layer1 + "/scan"

// finding 是一条违规里测试要断言的部分：违反了哪一条、违规的是什么。
type finding struct {
	rule    rule
	subject string
}

// parseSource 解析内存里的一段源码，位置里的文件名记作 x.go。
func parseSource(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("测试源码解析失败：%v\n%s", err, src)
	}
	return fset, f
}

func checkSource(t *testing.T, pkg, src string) []violation {
	t.Helper()
	fset, f := parseSource(t, src)
	return checkFile(fset, pkg, f)
}

// checkStoreSource 同 checkSource，按存储层的规则查。
func checkStoreSource(t *testing.T, pkg, src string) []violation {
	t.Helper()
	fset, f := parseSource(t, src)
	return checkStoreFile(fset, pkg, f)
}

// importing 拼出只有导入声明的源文件：守卫只解析不编译，导入了不用也没关系。
func importing(paths ...string) string {
	var b strings.Builder
	b.WriteString("package p\n\nimport (\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "\t%q\n", p)
	}
	b.WriteString(")\n")
	return b.String()
}

// withImport 拼出导入一个包、再跟一段代码的源文件。
func withImport(spec, body string) string {
	return "package p\n\nimport " + spec + "\n\n" + body + "\n"
}

// assertFindings 比对违规要点，并要求每条违规信息都写出包、违规的是什么和违反了哪一条。
func assertFindings(t *testing.T, got []violation, pkg string, want []finding) {
	t.Helper()
	var points []finding
	for _, v := range got {
		points = append(points, finding{v.rule, v.subject})
		msg := v.String()
		for _, part := range []string{pkg, v.subject, string(v.rule)} {
			if !strings.Contains(msg, part) {
				t.Errorf("违规信息 %q 没写出 %q", msg, part)
			}
		}
	}
	if !slices.Equal(points, want) {
		t.Errorf("违规要点 = %v，期望 %v", points, want)
	}
}

func Test守卫逐个文件的检查(t *testing.T) {
	// 九个函数各自都会读时钟或依赖时钟，少认一个就留下一条绕法。
	clockNames := []string{"Now", "Since", "Until", "Sleep", "After", "AfterFunc", "Tick", "NewTimer", "NewTicker"}
	allClocks := []finding{{ruleStd, "time"}}
	for _, n := range clockNames {
		allClocks = append(allClocks, finding{ruleClock, "time." + n})
	}
	coreutils := "github.com/lober/lober/internal/coreutils"

	cases := []struct {
		name string
		pkg  string
		src  string
		want []finding
	}{
		{"白名单里的十六个包都不报", pkgModel, importing(
			"cmp", "errors", "fmt", "maps", "slices", "strconv", "strings", "unicode", "unicode/utf8",
			"crypto/sha256", "encoding/base64", "encoding/binary", "encoding/hex", "encoding/json",
			"net/netip", "regexp"), nil},
		{"os 要报", pkgCatalog, importing("os"), []finding{{ruleStd, "os"}}},
		{"io/ioutil 要报", pkgModel, importing("io/ioutil"), []finding{{ruleStd, "io/ioutil"}}},
		{"math/rand 要报", pkgParams, importing("math/rand"), []finding{{ruleStd, "math/rand"}}},
		{"time 要报", pkgSnapshot, importing("time"), []finding{{ruleStd, "time"}}},
		// 放行 net/netip 不等于放行 net 下的其他包。
		{"白名单按整条路径比对", pkgModel, importing("net", "net/http", "net/netip"),
			[]finding{{ruleStd, "net"}, {ruleStd, "net/http"}}},
		{"cgo 的 C 要报", pkgModel, importing("C"), []finding{{ruleCgo, "C"}}},
		{"unsafe 要报", pkgModel, importing("unsafe"), []finding{{ruleStd, "unsafe"}}},

		{"别的模块要报", pkgModel, importing("golang.org/x/text/width"),
			[]finding{{ruleOutside, "golang.org/x/text/width"}}},
		{"前缀相撞的 internal/coreutils 不算层 1", pkgModel, importing(coreutils),
			[]finding{{ruleOutside, coreutils}}},
		// 存储契约依赖层 1，反过来就成了环（spec §6.1）。
		{"层 1 import 存储层要报", pkgModel, importing(pkgStore), []finding{{ruleOutside, pkgStore}}},

		// time 现在不在白名单里，下面几例都先报 import 了 time；时钟检查留给以后放进 time 时用。
		{"别名导入后的 t.Now 与 t.Since 要报", pkgModel,
			withImport(`t "time"`, "func f() { _ = t.Now(); _ = t.Since(t.Time{}) }"),
			[]finding{{ruleStd, "time"}, {ruleClock, "time.Now"}, {ruleClock, "time.Since"}}},
		{"点导入 time 要报", pkgModel, withImport(`. "time"`, "func f() { _ = Now() }"),
			[]finding{{ruleStd, "time"}, {ruleDotTime, "time"}}},
		{"time.Now 当函数值用要报", pkgModel, withImport(`"time"`, "var clock = time.Now"),
			[]finding{{ruleStd, "time"}, {ruleClock, "time.Now"}}},
		{"读时钟的九个函数都要报", pkgModel,
			withImport(`"time"`, "var clocks = []any{time."+strings.Join(clockNames, ", time.")+"}"), allClocks},
		{"注释和字符串里的不报", pkgModel,
			withImport(`"time"`, "// time.Now() 只是注释\nvar d time.Duration\n\nvar s = \"time.Now() time.Since(x)\""),
			[]finding{{ruleStd, "time"}}},
		{"没导入 time 时同名的 Now 不报", pkgModel,
			"package p\n\ntype clock struct{}\n\nfunc (clock) Now() int { return 0 }\n\nfunc f() { var time clock; _ = time.Now() }\n", nil},

		// 层内方向（T1-1 选 b）
		{"catalog 只用标准库不报", pkgCatalog, importing("slices"), nil},
		{"catalog import model 要报", pkgCatalog, importing(pkgModel), []finding{{ruleCatalog, pkgModel}}},
		{"model import catalog 不报", pkgModel, importing(pkgCatalog), nil},
		{"model import scan 要报", pkgModel, importing(pkgCatalog, pkgScan), []finding{{ruleModel, pkgScan}}},
		{"snapshot import params 不报", pkgSnapshot, importing(pkgCatalog, pkgModel, pkgScan, pkgParams), nil},
		{"snapshot import activation 要报", pkgSnapshot, importing(pkgParams, pkgActivation),
			[]finding{{ruleDecision, pkgActivation}}},
		{"activation import scan 不报", pkgActivation, importing(pkgCatalog, pkgModel, pkgScan), nil},
		{"activation import params 要报", pkgActivation, importing(pkgParams), []finding{{ruleDecision, pkgParams}}},
		{"params import scan 不报", pkgParams, importing(pkgModel, pkgScan), nil},
		{"params import snapshot 要报", pkgParams, importing(pkgSnapshot), []finding{{ruleDecision, pkgSnapshot}}},
		{"admission import catalog 与 model 不报", pkgAdmission, importing(pkgCatalog, pkgModel), nil},
		{"admission import params 要报", pkgAdmission, importing(pkgParams), []finding{{ruleDecision, pkgParams}}},
		{"scan 不另作限制", pkgScan, importing(pkgCatalog, pkgModel, pkgAdmission), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, checkSource(t, c.pkg, c.src), c.pkg, c.want)
		})
	}
}

func Test违规信息写出位置(t *testing.T) {
	got := checkSource(t, pkgCatalog, importing("os"))
	if len(got) != 1 {
		t.Fatalf("违规 = %v，期望恰好一条", got)
	}
	if msg := got[0].String(); !strings.Contains(msg, "x.go:4:2") {
		t.Errorf("违规信息 %q 没写出位置 x.go:4:2", msg)
	}
}

func Test层1包的文件都参与构建(t *testing.T) {
	cases := []struct {
		name string
		p    goPackage
		want []finding
	}{
		{"只有 GoFiles 不报", goPackage{ImportPath: pkgModel, GoFiles: []string{"a.go"}}, nil},
		{"cgo 文件要报", goPackage{ImportPath: pkgModel, GoFiles: []string{"a.go"}, CgoFiles: []string{"c.go"}},
			[]finding{{ruleCgo, "c.go"}}},
		{"按族命名被排除的文件要报",
			goPackage{ImportPath: pkgCatalog, GoFiles: []string{"series.go"}, IgnoredGoFiles: []string{"repo_linux.go"}},
			[]finding{{ruleIgnored, "repo_linux.go"}}},
		// 测试文件被排除，就是它在这个平台上静默不跑。
		{"被排除的测试文件也要报",
			goPackage{ImportPath: pkgCatalog, GoFiles: []string{"series.go"}, IgnoredGoFiles: []string{"series_windows_test.go"}},
			[]finding{{ruleIgnored, "series_windows_test.go"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, checkBuildFiles(c.p), c.p.ImportPath, c.want)
		})
	}
}

func Test含源文件的目录都要出现在go_list里(t *testing.T) {
	listed := []goPackage{{ImportPath: layer1}, {ImportPath: pkgModel}, {ImportPath: pkgScan}}
	cases := []struct {
		name   string
		dirs   []string
		listed []goPackage
		want   []finding
	}{
		{"都列出了不报", []string{".", "model", "scan"}, listed, nil},
		{"整包被构建约束排除的目录要报", []string{".", "model", "probeonly", "scan"}, listed,
			[]finding{{ruleListed, "internal/core/probeonly"}}},
		{"internal/core 自己没列出也要报", []string{".", "model"}, listed[1:],
			[]finding{{ruleListed, "internal/core"}}},
		// 只有测试文件的目录不算含源文件，go list 照样会列出它。
		{"列出的包多于目录不报", []string{"model"}, listed, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, unlistedDirs(layer1Dir, c.dirs, c.listed), "", c.want)
		})
	}
}

func Test只收集含非测试源文件的目录(t *testing.T) {
	fsys := fstest.MapFS{
		"doc.go":                  {}, // internal/core 自己也可能有包
		"model/model.go":          {},
		"model/model_test.go":     {},
		"model/_gen/x.go":         {}, // go list 不进 _ 开头的目录
		"onlytest/x_test.go":      {},
		"probeonly/x_linux.go":    {}, // 整包被构建约束排除，正是要查出来的
		"deep/README.md":          {},
		"deep/nested/x.go":        {},
		"scan/scan.go":            {},
		"scan/testdata/sample.go": {}, // go list 也不进 testdata
		".cache/x.go":             {}, // 和 . 开头的目录
	}
	got, err := sourceDirs(fsys)
	if err != nil {
		t.Fatalf("遍历失败：%v", err)
	}
	want := []string{".", "deep/nested", "model", "probeonly", "scan"}
	if !slices.Equal(got, want) {
		t.Errorf("目录 = %v，期望 %v", got, want)
	}
}

// 真实的树是干净的：总检查漏接了哪一项，Test层1依赖守卫 照样通过，所以组合本身也要有反例。
func Test总检查接上了每一项(t *testing.T) {
	fsys := fstest.MapFS{ // 以模块根为根
		"internal/core/model/model.go":   {Data: []byte(importing(pkgScan))},
		"internal/core/model/cgo.go":     {Data: []byte(importing("C"))},
		"internal/core/model/x_linux.go": {Data: []byte(importing("os"))},
		// 被排除的测试文件只报它被排除，不解析：要是解析了，它 import 的 net 会多报一条。
		"internal/core/model/x_windows_test.go": {Data: []byte(importing("net"))},
	}
	pkgs := []goPackage{{
		ImportPath:     pkgModel,
		GoFiles:        []string{"model.go"},
		CgoFiles:       []string{"cgo.go"},
		IgnoredGoFiles: []string{"x_linux.go", "x_windows_test.go"},
	}}
	got := checkLayer1(fsys, []string{"model", "probeonly"}, pkgs)
	// 依次是目录覆盖、文件构成，再是三类文件各自的 import。model.go 按 model 的方向规则报，
	// 说明解析时包名也接对了。
	assertFindings(t, got, "", []finding{
		{ruleListed, "internal/core/probeonly"},
		{ruleCgo, "cgo.go"},
		{ruleIgnored, "x_linux.go"},
		{ruleIgnored, "x_windows_test.go"},
		{ruleModel, pkgScan},
		{ruleCgo, "C"},
		{ruleStd, "os"},
	})
}

// 存储层的两条规则（第 2 层计划任务 2）各有正反例。两条都是白名单，照计划「包与文件」表；第 1 层的标准库
// 白名单不套用到这里。
func Test存储层的依赖规则(t *testing.T) {
	thirdParty := "github.com/google/btree"
	storex := "github.com/lober/lober/internal/storex"
	cases := []struct {
		name string
		pkg  string
		src  string
		want []finding
	}{
		// 一、契约只依赖标准库、层 1 和契约自己。
		{"store 用层 1 白名单以外的标准库不报", pkgStore,
			importing("context", "crypto/rand", "encoding/base32", "sync", pkgModel, pkgActivation, pkgAdmission), nil},
		{"store import 实现要报", pkgStore, importing(pkgPgstore), []finding{{ruleContract, pkgPgstore}}},
		{"store import gorm 要报", pkgStore, importing("gorm.io/gorm"), []finding{{ruleContract, "gorm.io/gorm"}}},
		{"cgo 的 C 不算标准库", pkgStore, importing("C"), []finding{{ruleContract, "C"}}},
		{"前缀相撞的 internal/storex 不算契约", pkgStore, importing(storex), []finding{{ruleContract, storex}}},
		// 新加的包与契约放行的一样多：不依赖第三方模块，也不依赖 pgstore。
		{"存储层新加的包按契约的规则查", pkgStore + "/cache",
			importing("context", "sync", pkgStore, pkgModel, pkgAdmission, thirdParty, "gorm.io/gorm", pkgPgstore),
			[]finding{{ruleContract, thirdParty}, {ruleContract, "gorm.io/gorm"}, {ruleContract, pkgPgstore}}},

		// 二、只有 pgstore 能依赖 gorm 与 pgx v5；存储层别的包，除了契约，它也不依赖。
		{"pgstore import gorm 与 pgx v5 不报", pkgPgstore, importing("database/sql",
			"gorm.io/gorm", "gorm.io/gorm/logger", "gorm.io/driver/postgres",
			"github.com/jackc/pgx/v5", "github.com/jackc/pgx/v5/pgconn", pkgStore, pkgModel), nil},
		{"pgstore import 存储层新加的包要报", pkgPgstore, importing(pkgStore + "/cache"),
			[]finding{{rulePgstore, pkgStore + "/cache"}}},
		// pgx 按整段比对：别的大版本、前缀相撞的路径都不算。
		{"pgstore import 别的第三方模块要报", pkgPgstore,
			importing("github.com/lib/pq", "github.com/jackc/pgx/v4", "github.com/jackc/pgx/v5x", thirdParty),
			[]finding{{rulePgstore, "github.com/lib/pq"}, {rulePgstore, "github.com/jackc/pgx/v4"},
				{rulePgstore, "github.com/jackc/pgx/v5x"}, {rulePgstore, thirdParty}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, checkStoreSource(t, c.pkg, c.src), c.pkg, c.want)
		})
	}
}

// 真实的树是干净的，存储层的总检查漏接了哪一项，Test存储层依赖守卫 照样通过，所以组合也要有反例。
func Test存储层总检查接上了每一项(t *testing.T) {
	fsys := fstest.MapFS{ // 以模块根为根
		"internal/store/store.go": {Data: []byte(importing(pkgPgstore))},
		// gorm 只有 pgstore 能用：pg.go 要是被当成别的包，gorm 也会报。
		"internal/store/pgstore/pg.go":      {Data: []byte(importing("gorm.io/gorm", "github.com/lib/pq"))},
		"internal/store/pgstore/cgo.go":     {Data: []byte(importing("C"))},
		"internal/store/pgstore/x_linux.go": {Data: []byte(importing("github.com/jackc/pgx/v4"))},
		// 测试文件不查：测试 import 的包，生产代码不许用的也不报。
		"internal/store/pgstore/x_windows_test.go": {Data: []byte(importing("github.com/lib/pq"))},
	}
	pkgs := []goPackage{
		{ImportPath: pkgStore, GoFiles: []string{"store.go"}},
		{ImportPath: pkgPgstore, GoFiles: []string{"pg.go"}, CgoFiles: []string{"cgo.go"},
			IgnoredGoFiles: []string{"x_linux.go", "x_windows_test.go"}},
	}
	got := checkStore(fsys, []string{".", "pgstore", "probeonly"}, pkgs)
	// 依次是目录覆盖，再是各包的 import：store.go 按契约的规则报、pg.go 按 pgstore 的规则报，说明包名接对了；
	// cgo 文件与被构建约束排除的文件也解析了，在本平台不编译的文件躲不过守卫。第 1 层对这两类文件本身的要求
	// 不套用到存储层，所以不报它们。
	assertFindings(t, got, "", []finding{
		{ruleListed, "internal/store/probeonly"},
		{ruleContract, pkgPgstore},
		{rulePgstore, "github.com/lib/pq"},
		{rulePgstore, "C"},
		{rulePgstore, "github.com/jackc/pgx/v4"},
	})
}
