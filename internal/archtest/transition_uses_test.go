package archtest

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// 总检查读取实际类型；局部变量、方法值和函数转发仍能认出原来的仓储方法。
// 装载失败或类型错误都必须失败，不能把没有类型信息当成没有违规。
func checkTypedTransitions(root string, listed []goPackage) []violation {
	var patterns []string
	for _, pkg := range listed {
		// 每个已发现的生产包都装载，新增包和子目录的类型错误也不能漏掉。
		if len(pkg.GoFiles)+len(pkg.CgoFiles) > 0 {
			patterns = append(patterns, pkg.ImportPath)
		}
	}
	if len(patterns) == 0 {
		return []violation{{rule: ruleMigration, subject: "过渡实现", detail: "没有找到要检查的生产包"}}
	}
	loaded, err := packages.Load(&packages.Config{
		Dir:  root,
		Mode: packages.NeedDeps | packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
	}, patterns...)
	if err != nil {
		return []violation{{rule: ruleParse, subject: "过渡符号类型装载", detail: err.Error()}}
	}
	var found []violation
	seen := map[string]bool{}
	packages.Visit(loaded, nil, func(pkg *packages.Package) {
		for _, err := range pkg.Errors {
			found = append(found, violation{rule: ruleParse, pkg: pkg.PkgPath, subject: "过渡符号类型装载", detail: err.Error()})
		}
	})
	for _, pkg := range loaded {
		seen[pkg.PkgPath] = true
		if pkg.TypesInfo == nil || len(pkg.Syntax) != len(pkg.CompiledGoFiles) || len(pkg.Syntax) == 0 {
			found = append(found, violation{rule: ruleParse, pkg: pkg.PkgPath, subject: "过渡符号类型装载", detail: "缺少源码或完整类型信息"})
			continue
		}
		found = append(found, typedTransitionFindings(root, pkg.PkgPath, pkg.Fset, pkg.Syntax, pkg.TypesInfo)...)
	}
	for _, pkg := range patterns {
		if !seen[pkg] {
			found = append(found, violation{rule: ruleParse, pkg: pkg, subject: "过渡符号类型装载", detail: "已列出的包没有检查结果"})
		}
	}
	return found
}

func typedTransitionFindings(root, caller string, fset *token.FileSet, files []*ast.File, info *types.Info) []violation {
	var found []violation
	for _, file := range files {
		for _, decl := range file.Decls {
			ast.Inspect(decl, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				obj := info.Uses[id]
				if !transitionRestricted(caller, obj) {
					return true
				}
				name := fset.Position(id.Pos()).Filename
				if filepath.IsAbs(name) {
					relative, err := filepath.Rel(root, name)
					if err != nil {
						found = append(found, violation{rule: ruleParse, pkg: caller, subject: name, detail: err.Error()})
						return true
					}
					name = relative
				}
				use := migrationException{Caller: caller, File: filepath.ToSlash(name), Function: declarationName(decl), Target: obj.Pkg().Path(), Symbol: transitionSymbol(obj)}
				if !transitionAllows(use) {
					found = append(found, violation{rule: ruleMigration, pkg: caller, subject: use.Target + "." + use.Symbol, pos: position(fset, id.Pos()), detail: fmt.Sprintf("%s:%s 未登记", use.File, use.Function)})
				}
				return true
			})
		}
	}
	if caller == pkgConsole {
		found = append(found, wizardHelperFindings(fset, files, info)...)
	}
	return found
}

func transitionRestricted(caller string, obj types.Object) bool {
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	if _, ok := obj.(*types.PkgName); ok {
		return false
	}
	if field, ok := obj.(*types.Var); ok && field.IsField() {
		return false
	}
	target := obj.Pkg().Path()
	_, function := obj.(*types.Func)
	if under(caller, pkgConsole) {
		return target == pkgStore || under(target, pkgBuiltin) || under(target, pkgRender) || target == pkgNotification || target == layer1+"/access" || function && under(target, layer1) || target == pkgConsole && strings.HasPrefix(transitionSymbol(obj), "ISOSourceState.")
	}
	if under(caller, pkgISO) {
		return target == pkgStore || function && under(target, layer1)
	}
	return false
}

func transitionSymbol(obj types.Object) string {
	if fn, ok := obj.(*types.Func); ok {
		if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
			t := recv.Type()
			if ptr, ok := t.(*types.Pointer); ok {
				t = ptr.Elem()
			}
			if named, ok := t.(*types.Named); ok {
				return named.Obj().Name() + "." + obj.Name()
			}
		}
	}
	return obj.Name()
}

func transitionAllows(use migrationException) bool {
	for _, entries := range [][]migrationException{migrationExceptions, mediaMigrationExceptions, consoleRuleMigrationExceptions, notificationMigrationExceptions} {
		if migrationAllows(entries, use) {
			return true
		}
	}
	return false
}
