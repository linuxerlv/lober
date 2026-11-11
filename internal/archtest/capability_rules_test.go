package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
)

// symbolCapability 是正式的小能力许可，和带清除阶段的迁移例外分开。
type symbolCapability struct {
	Caller, File, Function, Target, Symbol string
}

const ruleCapability rule = "编号能力只允许登记的调用包、文件、函数与 NewID 符号；其他随机入口不允许"

func checkCapabilities(entries []symbolCapability) []violation {
	converted := make([]migrationException, len(entries))
	for i, entry := range entries {
		converted[i] = migrationException{entry.Caller, entry.File, entry.Function, entry.Target, entry.Symbol, "1"}
	}
	found := checkMigrationExceptions(converted)
	for i := range found {
		found[i].rule = ruleCapability
	}
	return found
}

func capabilityAllows(entries []symbolCapability, use symbolCapability) bool {
	if len(checkCapabilities(entries)) != 0 {
		return false
	}
	for _, entry := range entries {
		if entry == use {
			return true
		}
	}
	return false
}

func declarationName(decl ast.Decl) string {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok {
		return "package-init"
	}
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return fn.Name.Name
	}
	receiver := fn.Recv.List[0].Type
	prefix := ""
	if ptr, ok := receiver.(*ast.StarExpr); ok {
		prefix = "*"
		receiver = ptr.X
	}
	if ident, ok := receiver.(*ast.Ident); ok {
		return "(" + prefix + ident.Name + ")." + fn.Name.Name
	}
	return fn.Name.Name
}

// 唯一转发入口也按真实选择器检查，不能让同包其他文件借用通知实现。
func checkNotificationForwarder(fset *token.FileSet, pkg string, f *ast.File) []violation {
	aliases := map[string]bool{}
	var found []violation
	for _, imp := range f.Imports {
		name, _ := strconv.Unquote(imp.Path.Value)
		if name != pkgNotification {
			continue
		}
		alias := "notification"
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "." || alias == "_" {
			found = append(found, violation{rule: ruleMigration, pkg: pkg, subject: pkgNotification, pos: position(fset, imp.Pos())})
			continue
		}
		aliases[alias] = true
	}
	for _, decl := range f.Decls {
		ast.Inspect(decl, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			name, ok := sel.X.(*ast.Ident)
			if !ok || !aliases[name.Name] || name.Obj != nil {
				return true
			}
			use := migrationException{Caller: pkg, File: fset.Position(sel.Pos()).Filename, Function: declarationName(decl), Target: pkgNotification, Symbol: sel.Sel.Name}
			if !migrationAllows(notificationMigrationExceptions, use) {
				found = append(found, violation{rule: ruleMigration, pkg: pkg, subject: pkgNotification + "." + sel.Sel.Name, pos: position(fset, sel.Pos())})
			}
			return true
		})
	}
	return found
}
