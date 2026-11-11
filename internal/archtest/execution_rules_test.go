package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// 检查真实生产语法树；旧规则即使换文件也不能留在应用层。
func Test纯执行规则只归领域(t *testing.T) {
	root := filepath.Join(filepath.Dir(goModPath(t)), "internal/application/execution")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, entry.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
				switch fn.Name.Name {
				case "boot", "advance", "clone", "runtime", "stage", "preComplete", "fingerprint", "seen", "slotsComplete":
					t.Errorf("应用仍拥有纯规则 %s: %s", fn.Name.Name, fset.Position(fn.Pos()))
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if a, ok := n.(*ast.AssignStmt); ok {
				for _, lhs := range a.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok {
						switch sel.Sel.Name {
						case "State", "Deadline", "Execution", "Signals", "BootCount", "TimingRank", "ReachedReboot", "InitDone":
							t.Errorf("应用仍写执行状态 %s: %s", sel.Sel.Name, fset.Position(sel.Pos()))
						}
					}
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("没有检查生产文件")
	}
}

func TestExecutionDomainHasOnlyDeclaredPureDependencies(t *testing.T) {
	for _, imp := range []string{pkgModel, pkgCatalog, "crypto/sha256", "encoding/json", "maps", "errors"} {
		if got := layer1Rule(pkgExecutionRules, imp); got != "" {
			t.Fatal(imp, got)
		}
	}
	for _, imp := range []string{pkgStore, pkgExecution, pkgExecutionPort, pkgMachine, pkgEvents, pkgSnapshot, "time", "os", "context"} {
		got := layer1Rule(pkgExecutionRules, imp)
		// context在旧通用纯标准库表中，本包另有精确限制。
		if got == "" {
			t.Fatal("领域纯执行依赖被加宽", imp)
		}
	}
	if layerOf(pkgExecutionRules) != layerCore {
		t.Fatal("新增领域包未归类")
	}
}

// 检查真实类型对象，避免只改函数名或把赋值换成指针写入绕过职责检查。
func TestExecutionApplicationDoesNotWriteActualDomainObjects(t *testing.T) {
	loaded, err := packages.Load(&packages.Config{Dir: filepath.Dir(goModPath(t)), Mode: packages.NeedName | packages.NeedDeps | packages.NeedImports | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo}, pkgExecution)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatal("未检查实际执行包")
	}
	packages.Visit(loaded, nil, func(p *packages.Package) {
		if len(p.Errors) > 0 {
			t.Fatalf("实际对象加载失败 %s: %v", p.PkgPath, p.Errors)
		}
	})
	p := loaded[0]
	if p.TypesInfo == nil || len(p.Syntax) == 0 {
		t.Fatal("没有实际生产语法树")
	}
	isRuleObject := func(typ types.Type) bool {
		if ptr, ok := typ.(*types.Pointer); ok {
			typ = ptr.Elem()
		}
		named, ok := typ.(*types.Named)
		if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != pkgModel {
			return false
		}
		return named.Obj().Name() == "Task" || named.Obj().Name() == "Execution"
	}
	for _, file := range p.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			var targets []ast.Expr
			switch node := n.(type) {
			case *ast.AssignStmt:
				targets = node.Lhs
			case *ast.IncDecStmt:
				targets = []ast.Expr{node.X}
			}
			for _, target := range targets {
				if sel, ok := target.(*ast.SelectorExpr); ok && isRuleObject(p.TypesInfo.TypeOf(sel.X)) {
					t.Errorf("应用写实际领域字段 %s: %s", sel.Sel.Name, p.Fset.Position(sel.Pos()))
				}
				if star, ok := target.(*ast.StarExpr); ok && isRuleObject(p.TypesInfo.TypeOf(star.X)) {
					t.Errorf("应用覆盖实际领域对象: %s", p.Fset.Position(star.Pos()))
				}
			}
			return true
		})
	}
}
