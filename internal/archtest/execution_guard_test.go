package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/packages"
	"path/filepath"
	"testing"
)

func TestMachineAndExecutionStayBehindDeclaredInterfaces(t *testing.T) {
	for _, pkg := range []string{pkgExecution, pkgMachine, pkgEvents} {
		if layerOf(pkg) == "" {
			t.Errorf("unclassified execution package %s", pkg)
		}
		for _, dependency := range []string{pkgConsole, pkgRender, pkgPgstore, pkgMachine, pkgExecution, pkgEvents, "github.com/gin-gonic/gin"} {
			if dependency == pkg {
				continue
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "probe.go", "package probe\nimport _ \""+dependency+"\"", 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(checkMachineFile(fset, pkg, file)) == 0 && !(pkg == pkgMachine && dependency == pkgExecution) {
				t.Errorf("%s may import %s", pkg, dependency)
			}
		}
		for _, dependency := range []string{"context", "net/http", pkgStore, pkgModel, pkgCatalog} {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "probe.go", "package probe\nimport _ \""+dependency+"\"", 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(checkMachineFile(fset, pkg, file)) != 0 {
				t.Errorf("declared contract dependency rejected: %s", dependency)
			}
		}
	}
}

func Test共同审计意图只开放给执行包(t *testing.T) {
	for _, pkg := range []string{pkgExecution, pkgExecution + "/hidden", pkgMachine, pkgEvents} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "probe.go", "package probe;import _ \""+pkgPorts+"\"", 0)
		if err != nil {
			t.Fatal(err)
		}
		allowed := len(checkMachineFile(fset, pkg, f)) == 0
		if allowed != (pkg == pkgExecution) {
			t.Fatal("共同能力开放范围改变", pkg, allowed)
		}
	}
}

func Test机器只新增执行应用精确依赖(t *testing.T) {
	for _, dependency := range []string{pkgExecution, pkgExecution + "/hidden", pkgExecution + "/port", pkgConsole, pkgEvents} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "probe.go", "package probe;import _ \""+dependency+"\"", 0)
		if err != nil {
			t.Fatal(err)
		}
		if (len(checkMachineFile(fset, pkgMachine, f)) == 0) != (dependency == pkgExecution) {
			t.Fatal(dependency)
		}
	}
}

// 真正装载生产类型后查调用；旧Collector可正常编译，但不能继续读写任务或通知。
func Test日志收集器只处理地址租约与字节(t *testing.T) {
	loaded, err := packages.Load(&packages.Config{Dir: filepath.Dir(goModPath(t)), Mode: packages.NeedName | packages.NeedDeps | packages.NeedImports | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo}, pkgEvents)
	if err != nil || len(loaded) != 1 {
		t.Fatal("未装载日志生产包", err)
	}
	packages.Visit(loaded, nil, func(p *packages.Package) {
		if len(p.Errors) > 0 {
			t.Fatalf("生产类型装载失败 %s: %v", p.PkgPath, p.Errors)
		}
	})
	p := loaded[0]
	if p.TypesInfo == nil || len(p.Syntax) == 0 {
		t.Fatal("未检查生产类型")
	}
	for _, f := range p.Syntax {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			obj, ok := p.TypesInfo.Uses[sel.Sel].(*types.Func)
			if !ok || obj.Pkg() == nil {
				return true
			}
			if obj.Pkg().Path() != pkgEvents && obj.Pkg().Path() != pkgStore && obj.Pkg().Path() != pkgExecutionPort {
				return true
			}
			switch obj.Name() {
			case "GetTask", "AppendEvents", "Publish":
				t.Errorf("Collector仍拥有业务调用 %s: %s", obj.Name(), p.Fset.Position(sel.Pos()))
			}
			return true
		})
	}
}
