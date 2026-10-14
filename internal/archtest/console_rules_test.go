package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
)

func checkConsoleFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return checkHTTPImports(fset, pkg, f, false)
}
func checkAssemblyFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return checkHTTPImports(fset, pkg, f, true)
}
func checkHTTPImports(fset *token.FileSet, pkg string, f *ast.File, assembly bool) []violation {
	var found []violation
	for _, imp := range f.Imports {
		name, _ := strconv.Unquote(imp.Path.Value)
		allowed := isStd(name) && name != "C" && name != "unsafe" || under(name, gin)
		r := ruleConsole
		if assembly {
			r = ruleAssembly
			allowed = allowed || under(name, modulePath) || name == yamlV3 || name == "github.com/goforj/wire"
		} else {
			allowed = allowed || inLayer1(name) || name == pkgStore || under(name, pkgRender) || under(name, pkgBuiltin) || under(name, pkgConsole)
		}
		if !allowed {
			found = append(found, violation{rule: r, pkg: pkg, subject: name, pos: position(fset, imp.Pos())})
		}
	}
	return found
}
