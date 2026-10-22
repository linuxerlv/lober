package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
)

const (
	pkgIdentity         = modulePath + "/internal/infrastructure/identity"
	ruleIdentity   rule = "随机编号只依赖 crypto/rand 和 encoding/base32"
	ruleDomainLeaf rule = "权限与选择规则只依赖 model，不依赖仓储契约或其他流程"
)

// 编号是没有业务依赖的叶子能力；只有这六个实际写入位置可以使用。
var identityCapabilities = []symbolCapability{
	{pkgPgstore, "internal/infrastructure/persistence/pgstore/hooks.go", "(*Store).CreateHook", pkgIdentity, "NewID"},
	{pkgPgstore, "internal/infrastructure/persistence/pgstore/isos.go", "(*Store).CreateISO", pkgIdentity, "NewID"},
	{pkgPgstore, "internal/infrastructure/persistence/pgstore/templates.go", "(*Store).CreateTemplate", pkgIdentity, "NewID"},
	{pkgPgstore, "internal/infrastructure/persistence/pgstore/templates.go", "(*Store).DeriveTemplate", pkgIdentity, "NewID"},
	{pkgPgstore, "internal/infrastructure/persistence/pgstore/tasks.go", "(*Store).createTasks", pkgIdentity, "NewID"},
	{pkgPgtest, "internal/infrastructure/persistence/pgstore/pgtest/pgtest.go", "newSchema", pkgIdentity, "NewID"},
}

func checkIdentityFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return checkImports(fset, pkg, f, func(_, imp string) rule {
		if imp == "crypto/rand" || imp == "encoding/base32" {
			return ""
		}
		return ruleIdentity
	})
}

// 导入许可只是第一步，每一次符号访问仍要匹配文件和函数。
func checkIdentityUses(fset *token.FileSet, pkg string, f *ast.File) []violation {
	var found []violation
	aliases := map[string]bool{}
	report := func(subject string, pos token.Pos) {
		found = append(found, violation{rule: ruleCapability, pkg: pkg, subject: subject, pos: position(fset, pos)})
	}
	for _, imp := range f.Imports {
		name, _ := strconv.Unquote(imp.Path.Value)
		if name != pkgIdentity {
			continue
		}
		alias := "identity"
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "." || alias == "_" {
			report(pkgIdentity, imp.Pos())
			continue
		}
		aliases[alias] = true
	}
	if len(aliases) == 0 {
		return found
	}
	found = append(found, checkCapabilities(identityCapabilities)...)
	for _, decl := range f.Decls {
		function := "package-init"
		if fn, ok := decl.(*ast.FuncDecl); ok {
			function = fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				receiver := fn.Recv.List[0].Type
				prefix := ""
				if ptr, ok := receiver.(*ast.StarExpr); ok {
					prefix = "*"
					receiver = ptr.X
				}
				if ident, ok := receiver.(*ast.Ident); ok {
					function = "(" + prefix + ident.Name + ")." + function
				}
			}
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			name, ok := sel.X.(*ast.Ident)
			if !ok || !aliases[name.Name] || name.Obj != nil {
				return true
			}
			use := symbolCapability{Caller: pkg, File: fset.Position(sel.Pos()).Filename, Function: function, Target: pkgIdentity, Symbol: sel.Sel.Name}
			if !capabilityAllows(identityCapabilities, use) {
				report(pkgIdentity+"."+sel.Sel.Name, sel.Pos())
			}
			return true
		})
	}
	return found
}
