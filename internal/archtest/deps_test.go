// Package archtest 放跨包的架构守卫。守卫检查的是别的包，本包只有测试代码，
// 不会被任何包 import，也不进 go build 的产物。
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

// layer1Dir 是层 1 相对模块根的目录，layer1 是它的导入路径：它自己和它下面的包都算层 1。
const (
	layer1Dir = "internal/core"
	layer1    = "github.com/lober/lober/" + layer1Dir
)

// 层内方向规则点名的包。
const (
	pkgCatalog    = layer1 + "/catalog"
	pkgModel      = layer1 + "/model"
	pkgActivation = layer1 + "/activation"
	pkgParams     = layer1 + "/params"
	pkgAdmission  = layer1 + "/admission"
	pkgSnapshot   = layer1 + "/snapshot"
)

// stdAllowed 是层 1 的非测试源文件可以 import 的标准库包（T1-1 选 C）。层 1 只做判定与
// 变换，不碰文件、网络和时钟（spec §6.1），同样的输入还要得到同样的结果，所以白名单
// 以外的包一律不许，cgo 的 C 与 unsafe 也一样。确实要加一个就改这里，评审时会被看到。
var stdAllowed = map[string]bool{
	"cmp": true, "errors": true, "fmt": true, "maps": true, "slices": true,
	"strconv": true, "strings": true, "unicode": true, "unicode/utf8": true,
	"crypto/sha256": true, "encoding/base64": true, "encoding/binary": true, "encoding/hex": true,
	"encoding/json": true, "net/netip": true, "regexp": true,
}

// clockFuncs 是 time 包里读时钟或依赖时钟的函数。time 现在不在白名单里，import 它就已经
// 违规；这项检查留给以后为了 time.Time 把 time 放进白名单时用。
var clockFuncs = map[string]bool{
	"Now": true, "Since": true, "Until": true, "Sleep": true, "After": true,
	"AfterFunc": true, "Tick": true, "NewTimer": true, "NewTicker": true,
}

// decisionPkgs 是四个判定包，它们之间只允许 snapshot import params。
var decisionPkgs = map[string]bool{pkgActivation: true, pkgParams: true, pkgAdmission: true, pkgSnapshot: true}

// rule 是守卫的一条规则，取值就是违规信息里「违反了哪一条」的说法。
type rule string

const (
	ruleStd      rule = "标准库只许 import 白名单（stdAllowed）里的包：层 1 不碰文件、网络、时钟和随机数"
	ruleOutside  rule = "层 1 只依赖标准库和层 1 自己的包，不 import 别的模块或本模块的其他包"
	ruleCgo      rule = "层 1 不用 cgo"
	ruleCatalog  rule = "catalog 是叶子，不 import 层 1 的任何包"
	ruleModel    rule = "model 在层 1 里只 import catalog"
	ruleDecision rule = "activation、params、admission、snapshot 之间只允许 snapshot import params"
	ruleClock    rule = "层 1 不读时钟，当前时间由调用方传入"
	ruleDotTime  rule = "不点导入 time，否则读时钟的调用认不出来"
	ruleIgnored  rule = "层 1 的文件都参与构建：被构建约束排除的文件在本平台不编译也不测试，按族命名的 x_linux.go 也会被排除"
	ruleParse    rule = "源文件要能解析，守卫才查得了"
	ruleListed   rule = "含非测试 .go 文件的目录都要出现在 go list ./... 里，否则守卫查不到它"
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
type goPackage struct {
	ImportPath     string
	GoFiles        []string
	CgoFiles       []string
	IgnoredGoFiles []string
}

func Test层1依赖守卫(t *testing.T) {
	gomod := goModPath(t)
	root := filepath.Dir(gomod)
	pkgs := layer1Packages(t, root)
	if len(pkgs) == 0 {
		// go list 出错、模块路径改了或 internal/core 被挪走都会走到这里，
		// 没有检查对象还报通过，守卫就形同虚设。
		t.Fatalf("go list 没有列出 %s 下的任何包，守卫没有检查对象", layer1)
	}
	dirs := layer1SourceDirs(t, gomod, root)
	for _, v := range checkLayer1(os.DirFS(root), dirs, pkgs) {
		t.Error(v)
	}
	t.Logf("检查了 %d 个层 1 包、%d 个含源文件的目录", len(pkgs), len(dirs))
}

// checkLayer1 是守卫的全部检查：先查有没有目录漏出 go list，再逐个查包。fsys 以模块根为根，
// dirs 是含非测试源文件的目录（相对 internal/core），pkgs 是 go list 列出的层 1 包。
// 它只吃输入，组合本身也就测得了（guard_test）；新加的检查接在这里，别接在 Test层1依赖守卫 里。
func checkLayer1(fsys fs.FS, dirs []string, pkgs []goPackage) []violation {
	found := unlistedDirs(dirs, pkgs)
	for _, p := range pkgs {
		found = append(found, checkPackage(fsys, p)...)
	}
	return found
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

// layer1Packages 逐个解码 go list -json 的输出：它是首尾相接的多个 JSON 对象，不是数组。
func layer1Packages(t *testing.T, root string) []goPackage {
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
		if inLayer1(p.ImportPath) {
			pkgs = append(pkgs, p)
		}
	}
}

func inLayer1(path string) bool {
	return path == layer1 || strings.HasPrefix(path, layer1+"/")
}

// isStd 沿用 go 命令的判断：路径第一段不含点的是标准库。放行只看白名单，
// 它只用来决定白名单以外的包报哪一条。
func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// layer1SourceDirs 遍历 internal/core，收集含非测试 .go 文件的目录（T1-3）。go list ./...
// 会静默跳过整包被构建约束排除的目录：只有 x_linux.go 的包在 Windows 上就不算包，
// 所以检查对象不能只信 go list。
//
// 这次遍历还让 go test 的结果缓存跟着层 1 的源码走。go test 只按测试进程自己经 os 包
// 打开过的文件和目录判断缓存是否失效，子进程 go list 读过什么它不知道；不走这一遍，
// 新增一个层 1 包或源文件、改了 go.mod 之后，go test 会直接复用上一次的 PASS。
// 跳过的目录 go list 也不进，不用跟踪。
func layer1SourceDirs(t *testing.T, gomod, root string) []string {
	t.Helper()
	if _, err := os.Stat(gomod); err != nil {
		t.Fatalf("读取 go.mod 失败：%v", err)
	}
	dirs, err := sourceDirs(os.DirFS(filepath.Join(root, filepath.FromSlash(layer1Dir))))
	if err != nil {
		t.Fatalf("遍历 %s 失败：%v", layer1Dir, err)
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

// unlistedDirs 返回 dirs（相对 internal/core）里没有出现在 go list 结果里的目录。
// 整包被构建约束排除，或者目录里另有 go.mod，go list ./... 都不报错、只是不列出它。
// 按导入路径比对，不受路径分隔符和盘符大小写的影响。
func unlistedDirs(dirs []string, pkgs []goPackage) []violation {
	listed := map[string]bool{}
	for _, p := range pkgs {
		listed[p.ImportPath] = true
	}
	var found []violation
	for _, d := range dirs {
		if !listed[path.Join(layer1, d)] {
			found = append(found, violation{rule: ruleListed, subject: path.Join(layer1Dir, d),
				detail: "多半是整包被构建约束排除（比如只有 x_linux.go），或者目录里另有 go.mod"})
		}
	}
	return found
}

// checkPackage 先查包的文件构成，再逐个解析非测试源文件。cgo 文件和被构建约束排除的
// 文件本身已经违规，也照样解析，一次报全它们 import 了什么。
//
// 文件从 fsys（以模块根为根）里读；线上是 os.DirFS，读文件照样经过 os 包，测试缓存跟得上。
// 模块里的包，导入路径就是模块路径接上目录，所以目录按导入路径推出，不用 go list 给的绝对路径。
func checkPackage(fsys fs.FS, p goPackage) []violation {
	found := checkBuildFiles(p)
	dir := layer1Dir + strings.TrimPrefix(p.ImportPath, layer1)
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
			found = append(found, checkFile(fset, p.ImportPath, f)...)
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

// checkBuildFiles 要求层 1 的包没有 cgo 文件，也没有被构建约束排除的文件。族标识 linux、
// windows 恰好是 GOOS 名，按族命名的 repo_linux.go 会被 Go 当成平台约束（T1-3）。
// 被排除的测试文件也算：它在这个平台上静默不跑。
func checkBuildFiles(p goPackage) []violation {
	var found []violation
	for _, name := range p.CgoFiles {
		found = append(found, violation{rule: ruleCgo, pkg: p.ImportPath, subject: name})
	}
	for _, name := range p.IgnoredGoFiles {
		found = append(found, violation{rule: ruleIgnored, pkg: p.ImportPath, subject: name})
	}
	return found
}

// checkFile 查一个非测试源文件的 import 和读时钟的写法。只看语法树，
// 所以注释和字符串里的 time.Now 不算。
func checkFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	var found []violation
	report := func(r rule, subject string, pos token.Pos) {
		p := fset.Position(pos)
		found = append(found, violation{rule: r, pkg: pkg, subject: subject, pos: fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)})
	}
	timeNames := map[string]bool{} // time 包在本文件里的名字，别名导入也要认
	for _, imp := range f.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value) // 解析成功的文件里，导入路径一定是合法的字符串字面量
		switch {
		case stdAllowed[importPath]:
		case importPath == "C":
			report(ruleCgo, importPath, imp.Pos())
		case inLayer1(importPath):
			if r := layerDirection(pkg, importPath); r != "" {
				report(r, importPath, imp.Pos())
			}
		case isStd(importPath):
			report(ruleStd, importPath, imp.Pos())
		default:
			report(ruleOutside, importPath, imp.Pos())
		}
		if importPath != "time" {
			continue
		}
		name := "time"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			report(ruleDotTime, importPath, imp.Pos())
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
