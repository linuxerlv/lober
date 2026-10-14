package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

// The one Wire injector is build-time input. Keep all other excluded files
// forbidden; checkSources still parses and checks this injector's imports.
func assemblyIgnoredFiles(fsys fs.FS, p goPackage) []violation {
	var found []violation
	for _, name := range p.IgnoredGoFiles {
		allowed := false
		if p.ImportPath == pkgAssembly && name == "wire.go" {
			data, err := fs.ReadFile(fsys, path.Join("cmd/lober", name))
			if err == nil && strings.HasPrefix(string(data), "//go:build wireinject\n") {
				f, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
				if err == nil {
					allowed = true
					injector := false
					wireImport := false
					for _, imp := range f.Imports {
						value, _ := strconv.Unquote(imp.Path.Value)
						wireImport = wireImport || value == "github.com/goforj/wire"
					}
					for _, decl := range f.Decls {
						switch d := decl.(type) {
						case *ast.GenDecl:
							allowed = allowed && d.Tok == token.IMPORT
						case *ast.FuncDecl:
							allowed = allowed && d.Name.Name == "newApp" && d.Recv == nil
							injector = injector || d.Name.Name == "newApp"
						default:
							allowed = false
						}
					}
					allowed = allowed && injector && wireImport
				}
			}
		}
		if !allowed {
			found = append(found, violation{rule: ruleIgnored, pkg: p.ImportPath, subject: name})
		}
	}
	return found
}

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
