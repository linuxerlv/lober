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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// layer1 是层 1 的导入路径，它自己和它下面的包都算层 1。
const layer1 = "github.com/lober/lober/internal/core"

// forbiddenStd 是层 1 不许 import 的标准库包：层 1 只做判定与变换，
// 不碰文件、网络、数据库和子进程（spec §6.1）。
var forbiddenStd = map[string]bool{
	"os":           true,
	"os/exec":      true,
	"net":          true,
	"net/http":     true,
	"database/sql": true,
	"syscall":      true,
}

// goPackage 是 go list -json 输出里守卫用到的字段。
type goPackage struct {
	ImportPath     string
	Dir            string
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
	trackForTestCache(t, gomod, root)
	for _, p := range pkgs {
		for _, v := range checkPackage(root, p) {
			t.Error(v)
		}
	}
	t.Logf("检查了 %d 个层 1 包", len(pkgs))
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

// isStd 沿用 go 命令的判断：路径第一段不含点的是标准库。
func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// trackForTestCache 让 go test 的结果缓存跟着层 1 的源码走。go test 只按测试进程
// 自己经 os 包打开过的文件和目录判断缓存是否失效，子进程 go list 读过什么它不知道；
// 不走这一遍，新增一个层 1 包或源文件、改了 go.mod 之后，go test 会直接复用上一次的
// PASS。已有源文件的改动不用管：checkPackage 解析时已经打开过它们。
func trackForTestCache(t *testing.T, gomod, root string) {
	t.Helper()
	if _, err := os.Stat(gomod); err != nil {
		t.Fatalf("读取 go.mod 失败：%v", err)
	}
	walk := func(_ string, _ fs.DirEntry, err error) error { return err }
	if err := filepath.WalkDir(filepath.Join(root, "internal", "core"), walk); err != nil {
		t.Fatalf("遍历 internal/core 失败：%v", err)
	}
}

// checkPackage 逐个解析包里的非测试源文件。构建约束排除掉的文件也查：
// 只在 Linux 上编译的文件，在 Windows 上跑守卫时同样要守规矩。
func checkPackage(root string, p goPackage) []string {
	var found []string
	fset := token.NewFileSet()
	for _, list := range [][]string{p.GoFiles, p.CgoFiles, p.IgnoredGoFiles} {
		for _, name := range list {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				found = append(found, fmt.Sprintf("%s 的 %s 解析失败，守卫无法检查：%v", p.ImportPath, name, err))
				continue
			}
			found = append(found, checkFile(fset, root, p.ImportPath, f)...)
		}
	}
	return found
}

func checkFile(fset *token.FileSet, root, pkg string, f *ast.File) []string {
	where := func(pos token.Pos) string {
		p := fset.Position(pos)
		name := p.Filename
		if rel, err := filepath.Rel(root, name); err == nil {
			name = filepath.ToSlash(rel)
		}
		return fmt.Sprintf("%s:%d:%d", name, p.Line, p.Column)
	}
	var found []string
	timeNames := map[string]bool{} // time 包在本文件里的名字，别名导入也要认
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value) // 解析成功的文件里，导入路径一定是合法的字符串字面量
		switch {
		case forbiddenStd[path]:
			found = append(found, fmt.Sprintf("%s import 了 %s（%s）：层 1 不碰文件、网络、数据库和子进程", pkg, path, where(imp.Pos())))
		case !isStd(path) && !inLayer1(path):
			found = append(found, fmt.Sprintf("%s import 了层 1 以外的包 %s（%s）：层 1 只依赖标准库和它自己", pkg, path, where(imp.Pos())))
		}
		if path != "time" {
			continue
		}
		name := "time"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			found = append(found, fmt.Sprintf("%s 点导入了 time（%s）：这样写的 Now() 守卫认不出来，改用普通导入", pkg, where(imp.Pos())))
		}
		timeNames[name] = true
	}
	if len(timeNames) == 0 {
		return found
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Now" {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && timeNames[x.Name] {
			found = append(found, fmt.Sprintf("%s 调用了 time.Now（%s）：当前时间要由调用方传入", pkg, where(sel.Pos())))
		}
		return true
	})
	return found
}
