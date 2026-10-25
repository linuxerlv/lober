package archtest

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
)

// 文件包装同样会取得外部事实；按实际标准库对象判断，不按调用方变量名判断。
func standardFileOperation(obj types.Object) bool {
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	if _, callable := interfaceCallableSignature(obj); !callable {
		return false
	}
	target, symbol := obj.Pkg().Path(), transitionSymbol(obj)
	switch target {
	case "path/filepath":
		switch symbol {
		case "Glob", "Walk", "WalkDir", "EvalSymlinks", "Abs":
			return true
		}
	case "archive/zip":
		return symbol == "OpenReader"
	case "text/template", "html/template":
		switch symbol {
		case "ParseFiles", "ParseGlob", "ParseFS", "Template.ParseFiles", "Template.ParseGlob", "Template.ParseFS":
			return true
		}
	case "debug/elf", "debug/pe", "debug/plan9obj":
		return symbol == "Open"
	case "debug/macho":
		return symbol == "Open" || symbol == "OpenFat"
	case "crypto/tls":
		return symbol == "LoadX509KeyPair"
	case "crypto/x509":
		return symbol == "SystemCertPool"
	case "go/parser":
		return symbol == "ParseDir" || symbol == "ParseFile" || symbol == "ParseExprFrom"
	case "go/build":
		// 这些是纯路径处理；其余构造、读取与Context回调可触及本机文件。
		switch symbol {
		case "IsLocalImport", "Context.JoinPath", "Context.SplitPathList", "Context.IsAbsPath":
			return false
		}
		return true
	case "go/importer", "plugin":
		return true
	case "io/fs":
		switch symbol {
		case "ValidPath", "FormatDirEntry", "FormatFileInfo":
			return false
		}
		return true
	case "embed":
		return true
	}
	return false
}

func consoleResourceEntry(caller string, fset *token.FileSet, file *ast.File, decl ast.Decl) bool {
	return caller == pkgConsole && strings.HasSuffix(filepath.ToSlash(fset.Position(file.Pos()).Filename), "internal/interfaces/console/ui.go") && (declarationName(decl) == "WithFrontend" || declarationName(decl) == "frontendHandler")
}

func consoleResourceOperation(obj types.Object) bool {
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	target, symbol := obj.Pkg().Path(), transitionSymbol(obj)
	return target == "io/fs" && (symbol == "Sub" || symbol == "ReadFile") || target == "embed" && (symbol == "FS.Open" || symbol == "FS.ReadFile")
}

// 原全模块类型检查的共同入口，应用和接口都不能靠标准库包装绕过文件端口。
func standardOperationFindings(caller string, fset *token.FileSet, files []*ast.File, info *types.Info) []violation {
	var r rule
	switch {
	case under(caller, pkgConsole):
		r = ruleMigration
	case under(caller, pkgMachine), under(caller, pkgEvents):
		r = ruleMachine
	case applicationModule(caller) != "" || caller == pkgExecution || caller == pkgExecutionPort:
		r = ruleApplication
	default:
		return nil
	}
	var found []violation
	for _, file := range files {
		for _, decl := range file.Decls {
			resource := consoleResourceEntry(caller, fset, file, decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				obj := info.ObjectOf(id)
				if !standardFileOperation(obj) || resource && consoleResourceOperation(obj) {
					return true
				}
				found = append(found, violation{rule: r, pkg: caller, subject: obj.Pkg().Path() + "." + transitionSymbol(obj), pos: position(fset, id.Pos()), detail: "标准库文件包装也须通过声明的外部能力"})
				return true
			})
		}
	}
	return found
}
