package archtest

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
)

// 复用总检查已加载的实际符号，补上同包转发路径。直接外部调用仍由原过渡规则检查。
// 函数、方法值和包级函数别名都按对象连接，不能凭函数名字猜是否属于业务。
func wizardHelperFindings(fset *token.FileSet, files []*ast.File, info *types.Info) []violation {
	type node struct {
		body ast.Node
		file string
		name string
	}
	nodes := map[types.Object]node{}
	var roots []types.Object
	for _, f := range files {
		file := filepath.Base(fset.Position(f.Pos()).Filename)
		for _, decl := range f.Decls {
			add := func(id *ast.Ident, body ast.Node) {
				if obj := info.Defs[id]; obj != nil {
					nodes[obj] = node{body, file, declarationName(decl)}
					if file == "wizard.go" {
						roots = append(roots, obj)
					}
				}
			}
			switch d := decl.(type) {
			case *ast.FuncDecl:
				add(d.Name, d)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if s, ok := spec.(*ast.ValueSpec); ok {
						for _, id := range s.Names {
							add(id, s)
						}
					}
				}
			}
		}
	}
	var found []violation
	for _, root := range roots {
		seen := map[types.Object]bool{}
		var visit func(types.Object)
		visit = func(obj types.Object) {
			if seen[obj] {
				return
			}
			seen[obj] = true
			n, ok := nodes[obj]
			if !ok {
				return
			}
			ast.Inspect(n.body, func(a ast.Node) bool {
				id, ok := a.(*ast.Ident)
				if !ok {
					return true
				}
				ref := info.Uses[id]
				if ref == nil {
					return true
				}
				fn, function := ref.(*types.Func)
				localCapability := false
				externalCapability := function && fn.Pkg() != nil && (fn.Pkg().Path() == pkgTasksPort || fn.Pkg().Path() == pkgPorts)
				if function && fn.Pkg() != nil && fn.Pkg().Path() == pkgConsole {
					if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
						_, localCapability = recv.Type().Underlying().(*types.Interface)
					}
				}
				business := function && transitionRestricted(pkgConsole, ref)
				// 问题分类是响应映射的一部分；只允许原函数调用这个精确方法。
				if function && ref.Pkg() != nil && ref.Pkg().Path() == pkgModel && transitionSymbol(ref) == "Code.Category" && n.file == "errors.go" && n.name == "StatusOfProblems" {
					business = false
				}
				if localCapability || externalCapability || business && obj != root {
					found = append(found, violation{rule: ruleMigration, pkg: pkgConsole, subject: ref.Pkg().Path() + "." + transitionSymbol(ref), pos: position(fset, id.Pos()), detail: "向导经 " + nodes[root].name + " 到达旧业务函数 " + n.name})
					return true
				}
				visit(ref)
				return true
			})
		}
		visit(root)
	}
	return found
}
