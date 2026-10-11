// Package archtest 放跨包的架构守卫，检查整个模块的包。本包只有测试代码，
// 不会被任何包 import，也不进 go build 的产物；这一条守卫也查（ruleGuard）。
package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// modulePath 是本模块的路径：模块里的包，导入路径就是它接上相对模块根的目录。
const modulePath = "github.com/lober/lober"

// layer1Dir 是层 1 相对模块根的目录，layer1 是它的导入路径：它自己和它下面的包都算层 1。
const (
	layer1Dir = "internal/core"
	layer1    = modulePath + "/" + layer1Dir
)

// storeDir 是存储层（第 2 层）相对模块根的目录：它自己和它下面的包都算存储层。下面是存储层规则点名的三个包，
// pkgStore 既是存储层的根，也是契约包；pkgPgtest 是给别的层的测试用的 PostgreSQL 夹具。
const (
	storeDir   = "internal/store"
	pkgStore   = modulePath + "/" + storeDir
	pkgPgstore = pkgStore + "/pgstore"
	pkgPgtest  = pkgPgstore + "/pgtest"
)

// pkgRender 与 pkgBuiltin 是第 3 层的两个包：它们自己和它们下面的包分别算渲染、内置内容。内置内容按格式分子目录放正文，
// 子目录里有 .go 文件时就是内置内容的子包，按内置内容的规则查；只放正文的子目录不是包。
const (
	pkgRender  = modulePath + "/internal/render"
	pkgBuiltin = modulePath + "/internal/builtin"
)

// pkgGuard 是守卫自己，也就是本包。只有它自己算守卫，它下面的包不算。
const pkgGuard = modulePath + "/internal/archtest"

// sourceRoots 是守卫遍历的目录（moduleSourceDirs），相对模块根：internal/ 放各层，cmd/ 留给装配点，现在还没有。web/ 已由
// go.mod 的 ignore 排除在模块之外，它的 node_modules 很大，不遍历。
var sourceRoots = []string{"internal", "cmd"}

// layer 是模块里的一层，取值就是日志里的叫法。每个包按导入路径归到一层（layerOf），各层查的东西不同（checkModule）。
type layer string

const (
	layerCore    layer = "层 1"
	layerStore   layer = "存储层"
	layerRender  layer = "渲染"
	layerBuiltin layer = "内置内容"
	layerGuard   layer = "守卫"
)

// layers 是 layer 的全部取值，按日志里的先后排。
var layers = []layer{layerCore, layerStore, layerRender, layerBuiltin, layerGuard}

// minChecked 是 Test全模块依赖守卫 每层至少要查到的包数。删了某层的包就改这里；渲染与内置内容的包加进来以后，
// 补上它们的下限——忘了补会由「查到了包的层都登记了下限」那一条报出来。
var minChecked = map[layer]int{layerCore: 7, layerStore: 3, layerRender: 1, layerGuard: 1}

// minDirs 是 moduleSourceDirs 至少要遍历到的目录数。目录遍历没有别的防线：sourceRoots 少了一项、或者
// checkModule 收到空的 dirs，ruleListed（整包被构建约束排除的目录）与 go test 的缓存跟踪都会静默失效，
// 正是计划「风险」一节点名的「新增包后复用旧的 PASS」。
const minDirs = 10

// pgxV5 是 pgx v5 的模块路径：它和它下面的包算 pgx v5。
const pgxV5 = "github.com/jackc/pgx/v5"

// 层内方向规则点名的包。
const (
	pkgCatalog    = layer1 + "/catalog"
	pkgModel      = layer1 + "/model"
	pkgActivation = layer1 + "/activation"
	pkgParams     = layer1 + "/params"
	pkgAdmission  = layer1 + "/admission"
	pkgSnapshot   = layer1 + "/snapshot"
)

// stdAllowed 是层 1 与第 3 层的非测试源文件可以 import 的标准库包（T1-1 选 C）。这两层只做判定、
// 变换与渲染，不碰文件、网络和时钟（spec §6.1），同样的输入还要得到同样的结果，所以白名单
// 以外的包一律不许，cgo 的 C 与 unsafe 也一样。确实要加一个就改这里，评审时会被看到。
// net/url 是第 3 层计划任务 2 为 catalog 拼机器面地址加的：只用它转义路径段，不发请求。
// bytes、sort、path 是任务 3 为渲染加的，都只在内存里算；path 只拼斜杠分隔的路径，随平台变的
// path/filepath 不放。embed 不在这里：只有内置内容要带正文，在 layer3Rule 里另许。
var stdAllowed = map[string]bool{
	"bytes": true, "cmp": true, "errors": true, "fmt": true, "maps": true, "path": true, "slices": true, "sort": true,
	"strconv": true, "strings": true, "unicode": true, "unicode/utf8": true,
	"crypto/sha256": true, "encoding/base64": true, "encoding/binary": true, "encoding/hex": true,
	"encoding/json": true, "net/netip": true, "net/url": true, "regexp": true,
}

// clockFuncs 是 time 包里读时钟或依赖时钟的函数，层 1、存储层与第 3 层都查（checkClock）。层 1 与
// 第 3 层的白名单里没有 time，import 它就已经违规，这项检查留给以后为了 time.Time 把 time 放进
// 白名单时用；存储层放行标准库，time.Duration、time.Minute 照用，读时钟的只有这九个函数。
var clockFuncs = map[string]bool{
	"Now": true, "Since": true, "Until": true, "Sleep": true, "After": true,
	"AfterFunc": true, "Tick": true, "NewTimer": true, "NewTicker": true,
}

// decisionPkgs 是四个判定包，它们之间只允许 snapshot import params。
var decisionPkgs = map[string]bool{pkgActivation: true, pkgParams: true, pkgAdmission: true, pkgSnapshot: true}

// rule 是守卫的一条规则，取值就是违规信息里「违反了哪一条」的说法。
type rule string

const (
	ruleStd      rule = "标准库只许 import 白名单（stdAllowed）里的包：层 1 与第 3 层不碰文件、网络、时钟和随机数，内置内容另许 embed"
	ruleOutside  rule = "层 1 只依赖标准库和层 1 自己的包，不 import 别的模块或本模块的其他包"
	ruleCgo      rule = "层 1 与第 3 层不用 cgo"
	ruleCatalog  rule = "catalog 是叶子，不 import 层 1 的任何包"
	ruleModel    rule = "model 在层 1 里只 import catalog"
	ruleDecision rule = "activation、params、admission、snapshot 之间只允许 snapshot import params"
	ruleClock    rule = "层 1、存储层与第 3 层不读时钟，当前时间由调用方传入"
	ruleDotTime  rule = "不点导入 time，否则读时钟的调用认不出来"
	ruleIgnored  rule = "各层的文件都参与构建，测试文件也一样：被构建约束排除的文件在本平台不编译也不测试，按族命名的 x_linux.go 也会被排除"
	ruleParse    rule = "源文件要能解析，守卫才查得了"
	ruleListed   rule = "含非测试 .go 文件的目录都要出现在 go list ./... 里，否则守卫查不到它"

	// 存储层的三条规则：前两条是第 2 层计划任务 2 的，第三条是修补计划任务 U4 的，都照计划「包与文件」表写成白名单。
	ruleContract rule = "store 只依赖标准库、internal/core/... 和 internal/store：契约不依赖 pgstore，" +
		"也不依赖第三方模块；存储层新加的包也按这一条查"
	rulePgstore rule = "pgstore 只依赖标准库、internal/core/...、internal/store，以及 gorm.io/... 与 github.com/jackc/pgx/v5/...：" +
		"第三方模块只有这两家"
	rulePgtest rule = "pgtest 只依赖标准库、internal/store、internal/store/pgstore 与 github.com/jackc/pgx/v5/...：" +
		"它是测试夹具，经 pgstore 起仓储，不直接用 gorm，也不依赖层 1"

	// 第 3 层的两条规则，照第 3 层计划「包与文件」表。
	ruleLayer3 rule = "渲染与内置内容只依赖标准库与层 1（spec §6.1）：不 import 存储层与本模块的其他包，" +
		"也不依赖第三方模块"
	ruleLayer3Apart rule = "渲染与内置内容的非测试代码互不 import：内置内容的 golden 测试在测试文件里，可以 import 渲染"

	ruleLayer rule = "每个包都要归到一层：internal/core、internal/store、internal/render、internal/builtin，" +
		"以及守卫自己 internal/archtest。归不到的，要么是包放错了目录，要么是新的一层（比如 cmd/lober）还没在守卫里登记规则"
	ruleGuard rule = "守卫 internal/archtest 只放测试文件：有了非测试文件，它就成了能被 import、会进 go build 的包"
)

// violation 是守卫查出的一处违规。
type violation struct {
	rule    rule
	pkg     string // 违规的包；目录没进 go list 时为空
	subject string // 违规的 import 路径、函数、文件或目录
	pos     string // 文件:行:列；整个文件或目录的违规为空
	detail  string // 补充说明，比如解析错误
}

func (v violation) String() string {
	var b strings.Builder
	if v.pkg != "" {
		b.WriteString(v.pkg + "：")
	}
	b.WriteString(v.subject)
	if v.pos != "" {
		b.WriteString("（" + v.pos + "）")
	} else {
		b.WriteString(" ")
	}
	b.WriteString("违反「" + string(v.rule) + "」")
	if v.detail != "" {
		b.WriteString("：" + v.detail)
	}
	return b.String()
}

// goPackage 是 go list -json 输出里守卫用到的字段。
//
// go list ./... 不带 -test 时，外部测试包（比如 pgstore 的测试都属于 pgstore_test）不单列成一个包：它的文件记在被测包的
// XTestGoFiles 里，与包内测试的 TestGoFiles 一样不查 import；被构建约束排除的文件，不论包名是什么，都记在被测包的
// IgnoredGoFiles 里，照样报出来。所以守卫按包名判断规则时只见得到 pgstore，见不到 pgstore_test。Test外部测试包的文件记在被测包里
// 钉住了这一点。
//
// TestGoFiles 与 XTestGoFiles 守卫不读，列在这里是为了让反例把测试文件摆进包里：哪天守卫开始查测试文件，
// Test第3层总检查接上了每一项 先红。
type goPackage struct {
	ImportPath     string
	GoFiles        []string
	CgoFiles       []string
	IgnoredGoFiles []string
	TestGoFiles    []string
	XTestGoFiles   []string
}

// Test全模块依赖守卫 按层查 go list ./... 列出的每个包（checkModule）：层 1 只依赖标准库白名单与层 1 自己；存储层照三条
// 白名单，第三方模块只有 pgstore 与 pgtest 能用；渲染与内置内容只依赖标准库白名单与层 1；守卫自己只放测试文件；归不到
// 任何一层的包报违规。层 1、存储层与第 3 层都不许读时钟。
func Test全模块依赖守卫(t *testing.T) {
	gomod := goModPath(t)
	root := filepath.Dir(gomod)
	pkgs := packagesUnder(t, root, modulePath)
	dirs := moduleSourceDirs(t, gomod, root)
	found, checked := checkModule(os.DirFS(root), dirs, pkgs)
	for _, v := range found {
		t.Error(v)
	}
	// go list 出错、模块路径改了、包被挪走，或者按层分派漏了一层，都会变成什么都没查还报通过，守卫就形同虚设。
	var counts []string
	for _, l := range layers {
		if checked[l] < minChecked[l] {
			t.Errorf("%s 只查了 %d 个包，至少应有 %d 个，守卫的检查对象少了", l, checked[l], minChecked[l])
		}
		counts = append(counts, fmt.Sprintf("%s %d 个", l, checked[l]))
	}
	// 查到了包却没登记下限，这一层就没有防空跑的底线：渲染与内置内容的包加进来时，逼着补 minChecked。
	for _, l := range layers {
		if _, ok := minChecked[l]; !ok && checked[l] > 0 {
			t.Errorf("%s 查到了 %d 个包，但 minChecked 里没有它的下限：补上，否则这一层的包被挪走也没人报", l, checked[l])
		}
	}
	// layers 漏了一层，那一层的下限与日志计数都会悄悄消失，上面两条也就查不到它。checked 的键来自 layerOf，
	// 所以拿它比对就能发现 layers 没跟上。
	for l := range checked {
		if !slices.Contains(layers, l) {
			t.Errorf("layerOf 给出了 %q，但 layers 里没有它：新加一层要同时登记进 layers", l)
		}
	}
	// 目录遍历没有别的防线，见 minDirs。
	if len(dirs) < minDirs {
		t.Errorf("只遍历到 %d 个含源文件的目录，至少应有 %d 个：sourceRoots 少了一项，或者 dirs 传空了", len(dirs), minDirs)
	}
	t.Logf("检查了 %d 个包（%s）、%d 个含源文件的目录", len(pkgs), strings.Join(counts, "，"), len(dirs))
}

// checkModule 是守卫的全部检查：先查有没有目录漏出 go list，再按层逐个查包，返回违规与每层查了几个包。fsys 以模块根为根，
// dirs 是含非测试源文件的目录（相对模块根），pkgs 是 go list 列出的包。它只吃输入，组合本身也就测得了（guard_test）；
// 新加的检查与新的一层都接在这里，别接在 Test全模块依赖守卫 里。
//
// 层 1 与第 3 层先查文件构成（checkBuildFiles），再逐个查非测试源文件的 import 与读时钟的写法。
//
// 存储层被构建约束排除的文件同样报出来（ignoredFiles），测试文件也算：pgstore 里多一个 *_linux_test.go，或者给
// 测试文件加一个 //go:build，PostgreSQL 契约测试在 Windows 上就静默不跑，make check 照样是绿的。被排除的非测试
// 文件照样解析，import 的包照样按规则查。cgo 不另报：C 不算标准库，import 那一条已经挡住它。
//
// 守卫只查文件构成（checkGuardFiles）。归不到一层的包整个报 ruleLayer，不再解析它的文件，也不算进任何一层。
func checkModule(fsys fs.FS, dirs []string, pkgs []goPackage) ([]violation, map[layer]int) {
	found := unlistedDirs(dirs, pkgs)
	checked := map[layer]int{}
	for _, p := range pkgs {
		l := layerOf(p.ImportPath)
		switch l {
		case layerCore:
			found = append(found, checkPackage(fsys, p, checkFile)...)
		case layerStore:
			found = append(found, ignoredFiles(p)...)
			found = append(found, checkSources(fsys, p, checkStoreFile)...)
		case layerRender, layerBuiltin:
			found = append(found, checkPackage(fsys, p, checkLayer3File)...)
		case layerGuard:
			found = append(found, checkGuardFiles(p)...)
		default:
			found = append(found, violation{rule: ruleLayer, subject: p.ImportPath})
			continue
		}
		checked[l]++
	}
	return found, checked
}

// layerOf 返回包 pkg 归哪一层，归不到时返回空串。按整段比对，internal/coreutils 不归层 1；守卫只认本包自己。
func layerOf(pkg string) layer {
	switch {
	case inLayer1(pkg):
		return layerCore
	case under(pkg, pkgStore):
		return layerStore
	case under(pkg, pkgRender):
		return layerRender
	case under(pkg, pkgBuiltin):
		return layerBuiltin
	case pkg == pkgGuard:
		return layerGuard
	}
	return ""
}

// goModPath 用 go env GOMOD 定位模块，测试从哪个目录跑、仓库放在哪都一样。
func goModPath(t *testing.T) string {
	t.Helper()
	gomod := strings.TrimSpace(string(runGo(t, "", "env", "GOMOD")))
	if gomod == "" || gomod == os.DevNull {
		t.Fatalf("go env GOMOD 输出 %q：不在模块里，找不到要检查的包", gomod)
	}
	return gomod
}

func runGo(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("go %s 失败：%v\n%s", strings.Join(args, " "), err, exit.Stderr)
		}
		t.Fatalf("go %s 失败：%v", strings.Join(args, " "), err)
	}
	return out
}

// packagesUnder 逐个解码 go list -json 的输出，留下 base 和它下面的包。输出是首尾相接的多个 JSON 对象，
// 不是数组。
func packagesUnder(t *testing.T, root, base string) []goPackage {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(runGo(t, root, "list", "-json", "./...")))
	var pkgs []goPackage
	for {
		var p goPackage
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			return pkgs
		}
		if err != nil {
			t.Fatalf("解码 go list 的输出失败：%v", err)
		}
		if under(p.ImportPath, base) {
			pkgs = append(pkgs, p)
		}
	}
}

// under 报告 path 是不是 base 自己或它下面的包。按整段比对，internal/coreutils 不在 internal/core 下面。
func under(path, base string) bool {
	return path == base || strings.HasPrefix(path, base+"/")
}

func inLayer1(path string) bool {
	return under(path, layer1)
}

// isStd 沿用 go 命令的判断：路径第一段不含点的是标准库，cgo 的 C 也算在内。层 1 与第 3 层放行只看白名单，
// 它只用来决定白名单以外的包报哪一条；存储层不套用白名单，按它放行标准库（C 除外）。
func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// moduleSourceDirs 遍历 sourceRoots，收集含非测试 .go 文件的目录（T1-3），斜杠分隔、相对模块根。go list ./...
// 会静默跳过整包被构建约束排除的目录：只有 x_linux.go 的包在 Windows 上就不算包，
// 所以检查对象不能只信 go list。
//
// 这次遍历还让 go test 的结果缓存跟着源码走。go test 只按测试进程自己经 os 包
// 打开过、Stat 过的文件和目录判断缓存是否失效，子进程 go list 读过什么它不知道；不走这一遍，
// 新增一个包或源文件、改了 go.mod 之后，go test 会直接复用上一次的 PASS。还没有的目录（现在的 cmd/）
// 也 Stat 一次，它一出现缓存就失效。跳过的目录 go list 也不进，不用跟踪。sourceRoots 以外新出现的包，
// go list 照样列出、照样报 ruleLayer，只是缓存不知道它出现了，要等别的输入变了才重跑。
func moduleSourceDirs(t *testing.T, gomod, root string) []string {
	t.Helper()
	if _, err := os.Stat(gomod); err != nil {
		t.Fatalf("读取 go.mod 失败：%v", err)
	}
	var dirs []string
	for _, r := range sourceRoots {
		dir := filepath.Join(root, filepath.FromSlash(r))
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		sub, err := sourceDirs(os.DirFS(dir))
		if err != nil {
			t.Fatalf("遍历 %s 失败：%v", r, err)
		}
		for _, d := range sub {
			dirs = append(dirs, path.Join(r, d))
		}
	}
	return dirs
}

// sourceDirs 返回 fsys 里含非测试 .go 文件的目录，斜杠分隔、按字典序排列，根目录是 "."。
// 和 go list ./... 一样跳过 testdata 以及 . 和 _ 开头的目录。
func sourceDirs(fsys fs.FS) ([]string, error) {
	dirs := map[string]bool{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		switch {
		case d.IsDir():
			if p != "." && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return fs.SkipDir
			}
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
			dirs[path.Dir(p)] = true
		}
		return nil
	})
	return slices.Sorted(maps.Keys(dirs)), err
}

// unlistedDirs 返回 dirs（相对模块根）里没有出现在 go list 结果里的目录。
// 整包被构建约束排除，或者目录里另有 go.mod，go list ./... 都不报错、只是不列出它。
// 按导入路径比对，不受路径分隔符和盘符大小写的影响。
func unlistedDirs(dirs []string, pkgs []goPackage) []violation {
	listed := map[string]bool{}
	for _, p := range pkgs {
		listed[p.ImportPath] = true
	}
	var found []violation
	for _, d := range dirs {
		if !listed[path.Join(modulePath, d)] {
			found = append(found, violation{rule: ruleListed, subject: d,
				detail: "多半是整包被构建约束排除（比如只有 x_linux.go），或者目录里另有 go.mod"})
		}
	}
	return found
}

// fileCheck 查一个非测试源文件，pkg 是它所在的包。各层各有一个：checkFile、checkStoreFile、checkLayer3File。
type fileCheck func(fset *token.FileSet, pkg string, f *ast.File) []violation

// checkPackage 查层 1 或第 3 层的一个包：先查包的文件构成，再用 check 逐个查非测试源文件。cgo 文件和被构建约束
// 排除的文件本身已经违规，也照样解析，一次报全它们 import 了什么。
func checkPackage(fsys fs.FS, p goPackage, check fileCheck) []violation {
	return append(checkBuildFiles(p), checkSources(fsys, p, check)...)
}

// checkSources 逐个解析包的非测试源文件，交给 check 查；cgo 文件和被构建约束排除的文件也在内。
//
// 文件从 fsys（以模块根为根）里读；线上是 os.DirFS，读文件照样经过 os 包，测试缓存跟得上。
// 模块里的包，导入路径就是模块路径接上目录，所以目录按导入路径推出，不用 go list 给的绝对路径。
func checkSources(fsys fs.FS, p goPackage, check fileCheck) []violation {
	var found []violation
	dir := strings.TrimPrefix(p.ImportPath, modulePath+"/")
	fset := token.NewFileSet()
	for _, list := range [][]string{p.GoFiles, p.CgoFiles, p.IgnoredGoFiles} {
		for _, name := range list {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parseFile(fsys, fset, path.Join(dir, name))
			if err != nil {
				found = append(found, violation{rule: ruleParse, pkg: p.ImportPath, subject: name, detail: err.Error()})
				continue
			}
			found = append(found, check(fset, p.ImportPath, f)...)
		}
	}
	return found
}

// parseFile 从 fsys 读出 name 并解析。位置里的文件名就是 name，即相对模块根的斜杠路径；
// 读不出来和解析不了一样报 ruleParse。
func parseFile(fsys fs.FS, fset *token.FileSet, name string) (*ast.File, error) {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	return parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
}

// checkBuildFiles 要求层 1 与第 3 层的包没有 cgo 文件，也没有被构建约束排除的文件（ignoredFiles）。族标识 linux、
// windows 恰好是 GOOS 名，按族命名的 repo_linux.go 会被 Go 当成平台约束（T1-3）。
func checkBuildFiles(p goPackage) []violation {
	var found []violation
	for _, name := range p.CgoFiles {
		found = append(found, violation{rule: ruleCgo, pkg: p.ImportPath, subject: name})
	}
	return append(found, ignoredFiles(p)...)
}

// ignoredFiles 把包里被构建约束排除的文件逐个报出来，各层都查。被排除的测试文件也算：它在这个平台上静默
// 不跑。
func ignoredFiles(p goPackage) []violation {
	var found []violation
	for _, name := range p.IgnoredGoFiles {
		found = append(found, violation{rule: ruleIgnored, pkg: p.ImportPath, subject: name})
	}
	return found
}

// position 是违规信息里的位置：文件:行:列。
func position(fset *token.FileSet, pos token.Pos) string {
	p := fset.Position(pos)
	return fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)
}

// checkFile 查层 1 一个非测试源文件的 import（layer1Rule）和读时钟的写法（checkClock）。
func checkFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return append(checkImports(fset, pkg, f, layer1Rule), checkClock(fset, pkg, f)...)
}

// checkImports 把文件的每个 import 交给 ruleFor 判，ruleFor 返回空串的不报。三层的单文件检查都由它查 import，
// 各层只有 ruleFor 不同。
func checkImports(fset *token.FileSet, pkg string, f *ast.File, ruleFor func(pkg, imp string) rule) []violation {
	var found []violation
	for _, imp := range f.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value) // 解析成功的文件里，导入路径一定是合法的字符串字面量
		if r := ruleFor(pkg, importPath); r != "" {
			found = append(found, violation{rule: r, pkg: pkg, subject: importPath, pos: position(fset, imp.Pos())})
		}
	}
	return found
}

// checkClock 查一个文件里读时钟的写法，层 1、存储层与第 3 层共用：time 包（别名导入也认）的 clockFuncs 九个函数，
// 调用和当函数值用都报；点导入 time 之后这些调用认不出来，点导入本身就报。只看语法树，所以注释和字符串里的
// time.Now 不算，没导入 time 时同名的 Now 也不算。time.Duration、time.Minute 这类类型与常量不读时钟，照常放行。
func checkClock(fset *token.FileSet, pkg string, f *ast.File) []violation {
	var found []violation
	report := func(r rule, subject string, pos token.Pos) {
		found = append(found, violation{rule: r, pkg: pkg, subject: subject, pos: position(fset, pos)})
	}
	timeNames := map[string]bool{} // time 包在本文件里的名字，别名导入也要认
	for _, imp := range f.Imports {
		if importPath, _ := strconv.Unquote(imp.Path.Value); importPath != "time" {
			continue
		}
		name := "time"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			report(ruleDotTime, "time", imp.Pos())
		}
		timeNames[name] = true
	}
	if len(timeNames) == 0 {
		return found
	}
	ast.Inspect(f, func(n ast.Node) bool {
		// 只认选择器，调用和当函数值用都是这个形状。
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !clockFuncs[sel.Sel.Name] {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && timeNames[x.Name] {
			report(ruleClock, "time."+sel.Sel.Name, sel.Pos())
		}
		return true
	})
	return found
}

// layer1Rule 返回层 1 的包 pkg import imp 违反的规则，不违反时返回空串：标准库按白名单，cgo 的 C 单独报，
// 层 1 里按方向规则（layerDirection），此外一律不许。
func layer1Rule(pkg, imp string) rule {
	switch {
	case stdAllowed[imp]:
		return ""
	case imp == "C":
		return ruleCgo
	case inLayer1(imp):
		return layerDirection(pkg, imp)
	case isStd(imp):
		return ruleStd
	}
	return ruleOutside
}

// layerDirection 返回 pkg import 层 1 的 imp 违反的方向规则，不违反时返回空串。
// 只守关键不变式（T1-1 选 b），scan 与以后新加的包不另作限制；编译器只拦得住
// 成环的依赖，admission 去 import params 这类越级依赖要靠这里。
func layerDirection(pkg, imp string) rule {
	switch {
	case pkg == pkgCatalog:
		return ruleCatalog
	case pkg == pkgModel && imp != pkgCatalog:
		return ruleModel
	case decisionPkgs[pkg] && decisionPkgs[imp] && (pkg != pkgSnapshot || imp != pkgParams):
		return ruleDecision
	}
	return ""
}

// checkStoreFile 查存储层一个非测试源文件的 import（storeRule）和读时钟的写法（checkClock）：仓储不读时钟是
// store 包说明里的全局约定。
func checkStoreFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return append(checkImports(fset, pkg, f, storeRule), checkClock(fset, pkg, f)...)
}

// storeRule 返回存储层的包 pkg import imp 违反的规则，不违反时返回空串。三条规则都是白名单，照计划「包与文件」表：
// 标准库、层 1 与契约包 internal/store 谁都能用，pgstore 另可用 gorm 与 pgx v5，此外一律不许，所以契约不依赖实现。
// pgtest 单列一条：它要 import pgx 与 pgstore，按契约的规则查会报违规；它也不用层 1。三个包以外、存储层新加的包
// 按契约的规则查，要放宽就先改这里。pgstore 的外部测试包 pgstore_test 不会走到这里：go list 不把它列成包（见 goPackage），
// 测试文件也不查 import，它 import pgtest 不算违规。
//
// 第三方模块只有这里放行的两处（S2-12）：每个包都要归到一层，别的层的规则都不放行第三方，所以「只有 pgstore 能用
// gorm 与 pgx、pgtest 能用 pgx」对全模块成立，不另起一张第三方模块的表。
//
// 第 1 层的标准库白名单不套用到这里：仓储要用 context、sync、crypto/rand 这类包，夹具要用 testing。cgo 的 C 不是
// 标准库，照样要报。
func storeRule(pkg, imp string) rule {
	std := isStd(imp) && imp != "C"
	if pkg == pkgPgtest {
		if std || imp == pkgStore || imp == pkgPgstore || under(imp, pgxV5) {
			return ""
		}
		return rulePgtest
	}
	if std || inLayer1(imp) || imp == pkgStore {
		return ""
	}
	switch pkg {
	case pkgPgstore:
		if pgDriver(imp) {
			return ""
		}
		return rulePgstore
	}
	return ruleContract
}

// pgDriver 报告 imp 是不是 pgstore 能用的第三方包：gorm.io 下的包，以及 pgx v5 和它下面的包。按整段比对，
// github.com/jackc/pgx/v4 与 github.com/jackc/pgx/v5x 都不算。
func pgDriver(imp string) bool {
	return strings.HasPrefix(imp, "gorm.io/") || under(imp, pgxV5)
}

// checkLayer3File 查渲染或内置内容一个非测试源文件的 import（layer3Rule）和读时钟的写法（checkClock）。
func checkLayer3File(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return append(checkImports(fset, pkg, f, layer3Rule), checkClock(fset, pkg, f)...)
}

// layer3Rule 返回渲染或内置内容的包 pkg import imp 违反的规则，不违反时返回空串，照第 3 层计划「包与文件」表：
// 标准库按层 1 的白名单，内置内容（含子包）另许 embed 带正文；层 1 都能用；同一边的父包与子包之间互相 import
// 不限（内置内容按格式分的子包可以 import 父包取共用的类型）。渲染与内置内容之间不许，存储层、本模块的其他包与
// 第三方模块也不许。内置内容的 golden 测试 import 渲染不归这里管：测试文件不查 import。
func layer3Rule(pkg, imp string) rule {
	own, other := pkgRender, pkgBuiltin
	if under(pkg, pkgBuiltin) {
		own, other = pkgBuiltin, pkgRender
	}
	switch {
	case stdAllowed[imp], inLayer1(imp), under(imp, own):
		return ""
	case imp == "embed" && own == pkgBuiltin:
		return ""
	case imp == "C":
		return ruleCgo
	case under(imp, other):
		return ruleLayer3Apart
	case isStd(imp):
		return ruleStd
	}
	return ruleLayer3
}

// checkGuardFiles 要求守卫只有测试文件：非测试文件与 cgo 文件逐个报 ruleGuard，被构建约束排除的文件照样报
// （ignoredFiles），deps_linux_test.go 会让守卫在 Windows 上静默不跑。非测试文件本身已经违规，不再解析。
func checkGuardFiles(p goPackage) []violation {
	var found []violation
	for _, name := range slices.Concat(p.GoFiles, p.CgoFiles) {
		found = append(found, violation{rule: ruleGuard, pkg: p.ImportPath, subject: name})
	}
	return append(found, ignoredFiles(p)...)
}
