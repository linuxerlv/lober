package archtest

// 守卫自身的反例测试（T1-2）。源码都放在内存里：写成 testdata 下的文件，会被
// make fmt 的 gofmt 目录递归扫到；取了 x_linux.go 这类名字，还会被构建约束
// 静默排除，测试就假绿了。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"reflect"
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
	f, err := parser.ParseFile(fset, "x.go", src, 0)
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

// checkLayer3Source 同 checkSource，按第 3 层的规则查。
func checkLayer3Source(t *testing.T, pkg, src string) []violation {
	t.Helper()
	fset, f := parseSource(t, src)
	return checkLayer3File(fset, pkg, f)
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
	coreutils := "github.com/lober/lober/internal/domainutils"

	cases := []struct {
		name string
		pkg  string
		src  string
		want []finding
	}{
		{"白名单里的二十个包都不报", pkgModel, importing(
			"bytes", "cmp", "errors", "fmt", "maps", "path", "slices", "sort", "strconv", "strings", "unicode", "unicode/utf8",
			"crypto/sha256", "encoding/base64", "encoding/binary", "encoding/hex", "encoding/json",
			"net/netip", "net/url", "regexp"), nil},
		{"os 要报", pkgCatalog, importing("os"), []finding{{ruleStd, "os"}}},
		{"io/ioutil 要报", pkgModel, importing("io/ioutil"), []finding{{ruleStd, "io/ioutil"}}},
		{"math/rand 要报", pkgParams, importing("math/rand"), []finding{{ruleStd, "math/rand"}}},
		{"time 要报", pkgSnapshot, importing("time"), []finding{{ruleStd, "time"}}},
		// 放行 net/netip 不等于放行 net 下的其他包。
		{"白名单按整条路径比对", pkgModel, importing("net", "net/http", "net/netip"),
			[]finding{{ruleStd, "net"}, {ruleStd, "net/http"}}},
		{"cgo 的 C 要报", pkgModel, importing("C"), []finding{{ruleCgo, "C"}}},
		{"unsafe 要报", pkgModel, importing("unsafe"), []finding{{ruleStd, "unsafe"}}},
		// embed 只给内置内容另许，不在共用的白名单里；放行 path 不等于放行随平台变的 path/filepath。
		{"embed 与 path/filepath 要报", pkgModel, importing("embed", "path/filepath"),
			[]finding{{ruleStd, "embed"}, {ruleStd, "path/filepath"}}},

		{"别的模块要报", pkgModel, importing("golang.org/x/text/width"),
			[]finding{{ruleOutside, "golang.org/x/text/width"}}},
		{"前缀相撞的 internal/domainutils 不算层 1", pkgModel, importing(coreutils),
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
		{"都列出了不报", []string{"internal/domain", "internal/domain/model", "internal/domain/scan"}, listed, nil},
		{"整包被构建约束排除的目录要报", []string{"internal/domain", "internal/domain/model", "internal/domain/probeonly", "internal/domain/scan"},
			listed, []finding{{ruleListed, "internal/domain/probeonly"}}},
		{"internal/domain 自己没列出也要报", []string{"internal/domain", "internal/domain/model"}, listed[1:],
			[]finding{{ruleListed, "internal/domain"}}},
		// 只有测试文件的目录不算含源文件，go list 照样会列出它。
		{"列出的包多于目录不报", []string{"internal/domain/model"}, listed, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, unlistedDirs(c.dirs, c.listed), "", c.want)
		})
	}
}

func Test只收集含非测试源文件的目录(t *testing.T) {
	fsys := fstest.MapFS{
		"doc.go":                   {}, // internal/domain 自己也可能有包
		"model/model.go":           {},
		"model/model_test.go":      {},
		"model/_gen/x.go":          {}, // go list 不进 _ 开头的目录
		"onlytest/x_test.go":       {},
		"probeonly/x_linux.go":     {}, // 整包被构建约束排除，正是要查出来的
		"deep/README.md":           {},
		"deep/nested/x.go":         {},
		"builtin/kickstart/ks.cfg": {}, // 内置内容按格式分目录放正文，只放正文的目录不算
		"scan/scan.go":             {},
		"scan/testdata/sample.go":  {}, // go list 也不进 testdata
		".cache/x.go":              {}, // 和 . 开头的目录
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

// 真实的树是干净的：总检查漏接了哪一项，Test全模块依赖守卫 照样通过，所以组合本身也要有反例。
func Test总检查接上了每一项(t *testing.T) {
	fsys := fstest.MapFS{ // 以模块根为根
		"internal/domain/model/model.go":   {Data: []byte(importing(pkgScan))},
		"internal/domain/model/cgo.go":     {Data: []byte(importing("C"))},
		"internal/domain/model/x_linux.go": {Data: []byte(importing("os"))},
		// 被排除的测试文件只报它被排除，不解析：要是解析了，它 import 的 net 会多报一条。
		"internal/domain/model/x_windows_test.go": {Data: []byte(importing("net"))},
	}
	pkgs := []goPackage{{
		ImportPath:     pkgModel,
		GoFiles:        []string{"model.go"},
		CgoFiles:       []string{"cgo.go"},
		IgnoredGoFiles: []string{"x_linux.go", "x_windows_test.go"},
	}}
	got, _ := checkModule(fsys, []string{"internal/domain/model", "internal/domain/probeonly"}, pkgs)
	// 依次是目录覆盖、文件构成，再是三类文件各自的 import。model.go 按 model 的方向规则报，
	// 说明解析时包名也接对了。
	assertFindings(t, got, "", []finding{
		{ruleListed, "internal/domain/probeonly"},
		{ruleCgo, "cgo.go"},
		{ruleIgnored, "x_linux.go"},
		{ruleIgnored, "x_windows_test.go"},
		{ruleModel, pkgScan},
		{ruleCgo, "C"},
		{ruleStd, "os"},
	})
}

// 存储层的三条规则（第 2 层计划任务 2，以及修补计划任务 U4 的 pgtest）各有正反例。三条都是白名单，照计划「包与文件」
// 表；仓储契约使用领域纯标准库白名单。
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
		{"契约只用纯计算标准库", pkgStore,
			importing("context", "math", "reflect", "encoding/base64", pkgModel, pkgActivation, pkgAdmission), nil},
		{"store import 实现要报", pkgStore, importing(pkgPgstore), []finding{{ruleContract, pkgPgstore}}},
		{"store import gorm 要报", pkgStore, importing("gorm.io/gorm"), []finding{{ruleContract, "gorm.io/gorm"}}},
		{"cgo 的 C 不算标准库", pkgStore, importing("C"), []finding{{ruleContract, "C"}}},
		{"前缀相撞的 internal/storex 不算契约", pkgStore, importing(storex), []finding{{ruleContract, storex}}},
		// 新加的包与契约放行的一样多：不依赖第三方模块，也不依赖 pgstore。
		{"存储层新加的包按契约的规则查", pkgStore + "/cache",
			importing("context", "reflect", pkgStore, pkgModel, pkgAdmission, thirdParty, "gorm.io/gorm", pkgPgstore),
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

		// 三、pgtest 是测试夹具：标准库（含 testing）、pgx v5、契约与 pgstore 以外一律不许。按契约的规则查，
		// pgx 与 pgstore 都会被报出来。
		{"pgtest import pgx v5、契约与 pgstore 不报", pkgPgtest, importing("context", "os", "testing",
			pgxV5, pgxV5+"/pgconn", pkgStore, pkgPgstore), nil},
		// 它经 pgstore 起仓储，用不着 gorm；它也不做判定，用不着层 1。
		{"pgtest import gorm 与层 1 要报", pkgPgtest, importing("gorm.io/gorm", "gorm.io/driver/postgres", pkgModel),
			[]finding{{rulePgtest, "gorm.io/gorm"}, {rulePgtest, "gorm.io/driver/postgres"}, {rulePgtest, pkgModel}}},
		{"pgtest import 别的第三方模块与存储层新加的包要报", pkgPgtest,
			importing("C", "github.com/lib/pq", "github.com/jackc/pgx/v4", "github.com/jackc/pgx/v5x", pkgStore+"/cache"),
			[]finding{{rulePgtest, "C"}, {rulePgtest, "github.com/lib/pq"}, {rulePgtest, "github.com/jackc/pgx/v4"},
				{rulePgtest, "github.com/jackc/pgx/v5x"}, {rulePgtest, pkgStore + "/cache"}}},
		// 包名按整条路径比对：pgtest 下面新加的包不沾它的放宽，按契约的规则查。
		{"pgtest 下面新加的包按契约的规则查", pkgPgtest + "/seed", importing(pgxV5, pkgPgstore),
			[]finding{{ruleContract, pgxV5}, {ruleContract, pkgPgstore}}},

		// 四、仓储不读时钟（store 的包说明）。time 是标准库，存储层照常能 import，只禁读时钟的九个函数，九个都认由
		// Test守卫逐个文件的检查 钉住。pgtest 现在就用 time.Minute 给 context.WithTimeout 定时限。
		{"time.Minute 与 time.Duration 不报", pkgPgtest, withImport("(\n\t\"context\"\n\t\"time\"\n)",
			"var d time.Duration\n\nfunc f() { _, _ = context.WithTimeout(context.Background(), time.Minute) }"), nil},
		{"pgstore 读时钟要报", pkgPgstore, withImport(`"time"`, "func f() { _ = time.Now(); time.Sleep(time.Second) }"),
			[]finding{{ruleClock, "time.Now"}, {ruleClock, "time.Sleep"}}},
		{"契约别名导入后读时钟要报", pkgStore, withImport(`t "time"`, "var clock = t.Since"),
			[]finding{{ruleContract, "time"}, {ruleClock, "time.Since"}}},
		{"存储层点导入 time 要报", pkgStore + "/cache", withImport(`. "time"`, "var d Duration"),
			[]finding{{ruleContract, "time"}, {ruleDotTime, "time"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, checkStoreSource(t, c.pkg, c.src), c.pkg, c.want)
		})
	}
}

// 守卫按 go list ./... 列出的包查（goPackage）。pgstore 的测试都在外部测试包 pgstore_test 里，它不单列成包，被构建约束
// 排除的文件要记在被测包的 IgnoredGoFiles 里，守卫才报得出 pgstore_test 里的 x_linux_test.go。这一条是 go 命令的行为，
// 内存里的反例测不到，所以在临时模块里真跑一次 go list：一个包、一个包内测试、两个外部测试文件，其中一个被构建约束排除。
// 哪天 go list 改了报告方式，这里先红。
func Test外部测试包的文件记在被测包里(t *testing.T) {
	root := t.TempDir()
	const mod = "example.com/m"
	for name, src := range map[string]string{
		"go.mod":            "module " + mod + "\n\ngo 1.21\n",
		"p/p.go":            "package p\n",
		"p/in_test.go":      "package p\n",
		"p/x_test.go":       "package p_test\n",
		"p/x_never_test.go": "//go:build never\n\npackage p_test\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := packagesUnder(t, root, mod)
	want := []goPackage{{ImportPath: mod + "/p", GoFiles: []string{"p.go"}, IgnoredGoFiles: []string{"x_never_test.go"},
		TestGoFiles: []string{"in_test.go"}, XTestGoFiles: []string{"x_test.go"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("go list 列出 %+v，应只有被测包、外部测试文件记在它的 XTestGoFiles 里，被排除的记在 IgnoredGoFiles 里：%+v",
			got, want)
	}
	assertFindings(t, ignoredFiles(got[0]), mod+"/p", []finding{{ruleIgnored, "x_never_test.go"}})
}

// 真实的树是干净的，存储层的总检查漏接了哪一项，Test全模块依赖守卫 照样通过，所以组合也要有反例。
func Test存储层总检查接上了每一项(t *testing.T) {
	fsys := fstest.MapFS{ // 以模块根为根
		"internal/domain/repository/repository.go": {Data: []byte(importing(pkgPgstore))},
		// gorm 只有 pgstore 能用：pg.go 要是被当成别的包，gorm 也会报。
		"internal/store/pgstore/pg.go":      {Data: []byte(importing("gorm.io/gorm", "github.com/lib/pq"))},
		"internal/store/pgstore/cgo.go":     {Data: []byte(importing("C"))},
		"internal/store/pgstore/x_linux.go": {Data: []byte(importing("github.com/jackc/pgx/v4"))},
		// 测试文件不查：测试 import 的包，生产代码不许用的也不报。
		"internal/store/pgstore/x_windows_test.go": {Data: []byte(importing("github.com/lib/pq"))},
	}
	pkgs := []goPackage{
		{ImportPath: pkgStore, GoFiles: []string{"repository.go"}},
		{ImportPath: pkgPgstore, GoFiles: []string{"pg.go"}, CgoFiles: []string{"cgo.go"},
			IgnoredGoFiles: []string{"x_linux.go", "x_windows_test.go"}},
	}
	got, _ := checkModule(fsys, []string{"internal/domain/repository", "internal/store/pgstore", "internal/domain/repository/probeonly"}, pkgs)
	// 依次是目录覆盖，再逐个包：先报被构建约束排除的文件（测试文件也算：*_linux_test.go 在 Windows 上静默不跑，契约测试
	// 就零覆盖了），再是 import。repository.go 按契约的规则报、pg.go 按 pgstore 的规则报，说明包名接对了；cgo 文件与被排除的
	// 非测试文件也解析了，在本平台不编译的文件躲不过守卫。cgo 不另报：C 不算标准库，import 那一条已经挡住它。
	assertFindings(t, got, "", []finding{
		{ruleListed, "internal/domain/repository/probeonly"},
		{ruleContract, pkgPgstore},
		{ruleIgnored, "x_linux.go"},
		{ruleIgnored, "x_windows_test.go"},
		{rulePgstore, "github.com/lib/pq"},
		{rulePgstore, "C"},
		{rulePgstore, "github.com/jackc/pgx/v4"},
	})
}

// 第 3 层（渲染与内置内容）只依赖标准库白名单与层 1（spec §6.1），内置内容另许 embed 带正文；两者的非测试代码互不 import。
// 子包按所在那个包的规则查：内置内容按格式分子包时，子包也能用 embed，也能 import 父包取共用的类型。
func Test第3层的依赖规则(t *testing.T) {
	kickstart := pkgBuiltin + "/kickstart"
	menu := pkgRender + "/menu"
	renderer := "github.com/lober/lober/internal/renderer"
	thirdParty := "github.com/google/btree"
	cases := []struct {
		name string
		pkg  string
		src  string
		want []finding
	}{
		// 一、只依赖标准库白名单与层 1。
		{"render import 白名单与层 1 不报", pkgRender,
			importing("bytes", "path", "sort", "strings", "net/url", layer1, pkgCatalog, pkgModel, pkgScan), nil},
		{"render import 存储层要报", pkgRender, importing(pkgStore, pkgPgstore, pkgPgtest),
			[]finding{{ruleLayer3, pkgStore}, {ruleLayer3, pkgPgstore}, {ruleLayer3, pkgPgtest}}},
		{"builtin 的子包 import 存储层要报", kickstart, importing(pkgStore), []finding{{ruleLayer3, pkgStore}}},
		{"render import 第三方模块要报", pkgRender, importing("gorm.io/gorm", pgxV5, thirdParty),
			[]finding{{ruleLayer3, "gorm.io/gorm"}, {ruleLayer3, pgxV5}, {ruleLayer3, thirdParty}}},
		// 前缀相撞的 internal/renderer 不是渲染自己的包。
		{"render import 本模块的其他包要报", pkgRender, importing(pkgGuard, renderer),
			[]finding{{ruleLayer3, pkgGuard}, {ruleLayer3, renderer}}},
		{"白名单以外的标准库要报", pkgRender, importing("os", "net/http", "path/filepath", "time"),
			[]finding{{ruleStd, "os"}, {ruleStd, "net/http"}, {ruleStd, "path/filepath"}, {ruleStd, "time"}}},
		{"cgo 的 C 要报", pkgBuiltin, importing("C"), []finding{{ruleCgo, "C"}}},

		// 二、embed 只给内置内容带正文用。
		{"builtin import embed 不报", pkgBuiltin, importing("embed", pkgModel), nil},
		{"builtin 的子包 import embed 与 builtin 不报", kickstart, importing("embed", pkgBuiltin, pkgModel), nil},
		{"render import embed 要报", pkgRender, importing("embed"), []finding{{ruleStd, "embed"}}},
		{"render 的子包 import embed 要报", menu, importing("embed"), []finding{{ruleStd, "embed"}}},

		// 三、渲染与内置内容互不 import，各自的子包也算在内；同一边的父包与子包之间不限。
		{"render import builtin 与它的子包要报", pkgRender, importing(pkgBuiltin, kickstart),
			[]finding{{ruleLayer3Apart, pkgBuiltin}, {ruleLayer3Apart, kickstart}}},
		{"builtin import render 要报", pkgBuiltin, importing(pkgRender), []finding{{ruleLayer3Apart, pkgRender}}},
		{"builtin 的子包 import render 的子包要报", kickstart, importing(menu), []finding{{ruleLayer3Apart, menu}}},
		{"render 与它的子包互相 import 不报", menu, importing(pkgRender), nil},
		{"builtin import 它的子包不报", pkgBuiltin, importing(kickstart), nil},

		// 四、不读时钟。time 不在白名单里，先报 import 了 time；时钟检查留给以后放进 time 时用。
		{"读时钟要报", pkgRender, withImport(`t "time"`, "func f() { _ = t.Now() }"),
			[]finding{{ruleStd, "time"}, {ruleClock, "time.Now"}}},
		{"点导入 time 要报", pkgBuiltin, withImport(`. "time"`, "var d Duration"),
			[]finding{{ruleStd, "time"}, {ruleDotTime, "time"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, checkLayer3Source(t, c.pkg, c.src), c.pkg, c.want)
		})
	}
}

// 真实的树里还没有第 3 层的包，第 3 层的总检查漏接了哪一项，Test全模块依赖守卫 照样通过，所以组合也要有反例。
func Test第3层总检查接上了每一项(t *testing.T) {
	fsys := fstest.MapFS{ // 以模块根为根
		"internal/builtin/builtin.go": {Data: []byte(importing("embed", pkgModel, pkgRender))},
		// golden 测试在测试文件里，可以 import 渲染（第 3 层计划「包与文件」）：测试文件不查，import 存储层也不报。
		"internal/builtin/golden_test.go": {Data: []byte(importing(pkgRender, pkgStore))},
		"internal/builtin/x_test.go":      {Data: []byte(importing(pkgRender, "gorm.io/gorm"))},
		// 按格式分的子包：embed 与父包不报，越过层边界要报；只放正文的子目录不是包，也不在 dirs 里。
		"internal/builtin/kickstart/embed.go": {Data: []byte(importing("embed", pkgBuiltin, pkgPgstore))},
		"internal/domain/render/render.go":    {Data: []byte(importing(pkgStore, pkgBuiltin, "embed"))},
		"internal/domain/render/cgo.go":       {Data: []byte(importing("C"))},
		"internal/domain/render/x_linux.go":   {Data: []byte(importing("os"))},
	}
	pkgs := []goPackage{
		{ImportPath: pkgBuiltin, GoFiles: []string{"builtin.go"}, TestGoFiles: []string{"golden_test.go"},
			XTestGoFiles: []string{"x_test.go"}},
		{ImportPath: pkgBuiltin + "/kickstart", GoFiles: []string{"embed.go"}},
		{ImportPath: pkgRender, GoFiles: []string{"render.go"}, CgoFiles: []string{"cgo.go"}, IgnoredGoFiles: []string{"x_linux.go"}},
	}
	got, _ := checkModule(fsys, []string{"internal/builtin", "internal/builtin/kickstart", "internal/domain/render",
		"internal/domain/render/probeonly"}, pkgs)
	// 依次是目录覆盖，再逐个包。builtin.go 的 embed 不报、render.go 的要报，说明包名接对了；渲染照第 1 层的样子先报文件构成，
	// cgo 文件与被排除的文件也解析了。
	assertFindings(t, got, "", []finding{
		{ruleListed, "internal/domain/render/probeonly"},
		{ruleLayer3Apart, pkgRender},
		{ruleLayer3, pkgPgstore},
		{ruleCgo, "cgo.go"},
		{ruleIgnored, "x_linux.go"},
		{ruleLayer3, pkgStore},
		{ruleLayer3Apart, pkgBuiltin},
		{ruleStd, "embed"},
		{ruleCgo, "C"},
		{ruleStd, "os"},
	})
}

// 守卫只放测试文件。被构建约束排除的测试文件也要报：deps_linux_test.go 会让守卫在 Windows 上静默不跑。非测试文件本身已经
// 违规，不再解析：fsys 是空的，要是解析了，读不到文件会多报 ruleParse。
func Test守卫自己只放测试文件(t *testing.T) {
	cases := []struct {
		name string
		p    goPackage
		want []finding
	}{
		{"只有测试文件不报", goPackage{ImportPath: pkgGuard, TestGoFiles: []string{"deps_test.go", "guard_test.go"}}, nil},
		{"非测试文件与 cgo 文件要报",
			goPackage{ImportPath: pkgGuard, GoFiles: []string{"helper.go"}, CgoFiles: []string{"cgo.go"}, TestGoFiles: []string{"deps_test.go"}},
			[]finding{{ruleGuard, "helper.go"}, {ruleGuard, "cgo.go"}}},
		{"被排除的测试文件要报",
			goPackage{ImportPath: pkgGuard, TestGoFiles: []string{"guard_test.go"}, IgnoredGoFiles: []string{"deps_linux_test.go"}},
			[]finding{{ruleIgnored, "deps_linux_test.go"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := checkModule(fstest.MapFS{}, nil, []goPackage{c.p})
			assertFindings(t, got, c.p.ImportPath, c.want)
		})
	}
}

// 包放错了目录，或者新的一层（比如装配点 cmd/lober）还没在守卫里登记规则，都归不到一层。按整段比对：前缀相撞的不归那一层，
// 守卫下面的包也不算守卫。归不到一层的包不解析，fsys 是空的，理由同上。
func Test每个包都要归到一层(t *testing.T) {
	var pkgs []goPackage
	var want []finding
	for _, p := range []string{
		modulePath, modulePath + "/internal", modulePath + "/cmd/loberx", modulePath + "/tools/gen",
		modulePath + "/internal/domainutils", modulePath + "/internal/storex", modulePath + "/internal/renderer",
		modulePath + "/internal/builtins", pkgGuard + "/fixture",
	} {
		pkgs = append(pkgs, goPackage{ImportPath: p, GoFiles: []string{"x.go"}})
		want = append(want, finding{ruleLayer, p})
	}
	got, checked := checkModule(fstest.MapFS{}, nil, pkgs)
	assertFindings(t, got, "", want)
	if len(checked) != 0 {
		t.Errorf("每层查的包数 = %v，归不到一层的包不该算进任何一层", checked)
	}
}

// Test全模块依赖守卫 靠每层查了几个包挡住空跑，计数本身也要对：归到一层的包各算一次，归不到的不算。
func Test总检查按层计数(t *testing.T) {
	var pkgs []goPackage
	for _, p := range []string{
		pkgCatalog, pkgModel, pkgStore, pkgPgtest, pkgRender, pkgBuiltin, pkgBuiltin + "/kickstart", pkgGuard,
		modulePath + "/cmd/lober",
	} {
		pkgs = append(pkgs, goPackage{ImportPath: p})
	}
	_, got := checkModule(fstest.MapFS{}, nil, pkgs)
	want := map[layer]int{layerCore: 2, layerStore: 2, layerRender: 1, layerBuiltin: 2, layerGuard: 1, layerAssembly: 1}
	if !maps.Equal(got, want) {
		t.Errorf("每层查的包数 = %v，期望 %v", got, want)
	}
}

// 第三方模块只有 pgstore（gorm 与 pgx v5）与 pgtest（pgx v5）能用（S2-12）。没有另起一张第三方模块的表：每个包都要归到一层，
// 各层的规则都不放行第三方，只有这两处例外，所以这条对全模块成立。每层取一个代表包，经总检查走一遍。
func Test第三方模块只有pgstore与pgtest能用(t *testing.T) {
	gorm, btree := "gorm.io/gorm", "github.com/google/btree"
	cmdLober := modulePath + "/cmd/lober"
	cases := []struct {
		pkg  string
		want []finding
	}{
		{pkgModel, []finding{{ruleOutside, gorm}, {ruleOutside, pgxV5}, {ruleOutside, btree}}},
		{pkgStore, []finding{{ruleContract, gorm}, {ruleContract, pgxV5}, {ruleContract, btree}}},
		{pkgStore + "/cache", []finding{{ruleContract, gorm}, {ruleContract, pgxV5}, {ruleContract, btree}}},
		{pkgPgstore, []finding{{rulePgstore, btree}}},
		{pkgPgtest, []finding{{rulePgtest, gorm}, {rulePgtest, btree}}},
		{pkgRender, []finding{{ruleLayer3, gorm}, {ruleLayer3, pgxV5}, {ruleLayer3, btree}}},
		{pkgBuiltin, []finding{{ruleLayer3, gorm}, {ruleLayer3, pgxV5}, {ruleLayer3, btree}}},
		{pkgBuiltin + "/kickstart", []finding{{ruleLayer3, gorm}, {ruleLayer3, pgxV5}, {ruleLayer3, btree}}},
		// 守卫有非测试文件就违规，归不到一层的包整个违规，都不再看 import。
		{pkgGuard, []finding{{ruleGuard, "x.go"}}},
		{cmdLober, []finding{{ruleAssembly, gorm}, {ruleAssembly, pgxV5}, {ruleAssembly, btree}}},
	}
	for _, c := range cases {
		t.Run(strings.TrimPrefix(c.pkg, modulePath+"/"), func(t *testing.T) {
			dir := strings.TrimPrefix(c.pkg, modulePath+"/")
			fsys := fstest.MapFS{dir + "/x.go": {Data: []byte(importing(gorm, pgxV5, btree))}}
			got, _ := checkModule(fsys, []string{dir}, []goPackage{{ImportPath: c.pkg, GoFiles: []string{"x.go"}}})
			assertFindings(t, got, c.pkg, c.want)
		})
	}
}
