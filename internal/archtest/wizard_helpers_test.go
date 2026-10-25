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
	var found []violation
	for _, f := range files {
		file := filepath.Base(fset.Position(f.Pos()).Filename)
		for _, decl := range f.Decls {
			name := declarationName(decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				ref := info.Uses[id]
				if ref == nil {
					if fn, ok := info.Defs[id].(*types.Func); ok {
						ref = fn
					}
				}
				fn, function := ref.(*types.Func)
				if !function || fn.Pkg() == nil {
					return true
				}
				localCapability := false
				if fn.Pkg().Path() == pkgConsole {
					if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
						_, localCapability = recv.Type().Underlying().(*types.Interface)
						localCapability = localCapability && unapprovedInterfaceMethod(fn)
					}
				}
				externalCapability := under(fn.Pkg().Path(), modulePath+"/internal/application") && !migratedApplicationMethod(fn)
				business := transitionRestricted(pkgConsole, ref)
				if fn.Pkg().Path() == pkgModel && transitionSymbol(ref) == "Code.Category" && file == "errors.go" && name == "StatusOfProblems" {
					business = false
				}
				if localCapability || externalCapability || business {
					found = append(found, violation{rule: ruleMigration, pkg: pkgConsole, subject: ref.Pkg().Path() + "." + transitionSymbol(ref), pos: position(fset, id.Pos()), detail: "全部声明检查发现旧业务能力：" + name})
				}
				return true
			})
		}
	}
	return found
}

// 只登记已交付的入口与方法。共同能力包新增方法不会自动获得许可。
func migratedApplicationMethod(fn *types.Func) bool {
	target, symbol := fn.Pkg().Path(), transitionSymbol(fn)
	if target == modulePath+"/internal/application/identity" {
		return identityConsoleSymbol(symbol)
	}
	if target == pkgAuditApp {
		return symbol == "Reader.List" || symbol == "Reader.ExportCSV"
	}
	if target == pkgPorts {
		return symbol == "DescribeFailure" || symbol == "ProblemClass"
	}
	if target == modulePath+"/internal/application/media" {
		switch symbol {
		case "Registry.List", "Registry.Get", "Registry.Create", "Registry.RegisterSource", "Registry.Progress", "Registry.Upload", "Registry.Cancel", "Registry.Retry", "Registry.Reimport", "Registry.Deprecate", "Registry.Delete":
			return true
		}
	}
	if target == modulePath+"/internal/application/hosts" {
		switch symbol {
		case "Registry.List", "Registry.Get", "Registry.Create", "Registry.Update", "Registry.Delete", "Registry.Import":
			return true
		}
	}
	if target == pkgTasks {
		switch symbol {
		case "EventReader.List", "EventReader.Open", "EventSubscription.Initial", "EventSubscription.Hints", "EventSubscription.Read", "EventSubscription.Close", "Controller.Cancel", "Controller.Retry", "Reader.List", "Reader.Get", "Reader.Preview", "Wizard.Hosts", "Wizard.ISOs", "Wizard.Templates", "Wizard.Params", "Wizard.Submit":
			return true
		}
	}
	if target == modulePath+"/internal/application/authoring" {
		switch symbol {
		case "PublicCatalog", "Hooks.List", "Hooks.Versions", "Hooks.Get", "Hooks.Create", "Hooks.SaveDraft", "Hooks.NewVersion", "Hooks.DeleteDraft", "Hooks.Rename", "Hooks.Submit", "Hooks.Approve", "Hooks.Reject", "Hooks.Deprecate", "Hooks.Validate",
			"Templates.List", "Templates.Versions", "Templates.Get", "Templates.Create", "Templates.SaveDraft", "Templates.NewVersion", "Templates.DeleteDraft", "Templates.Rename", "Templates.Submit", "Templates.Approve", "Templates.Reject", "Templates.Deprecate", "Templates.Validate", "Templates.Derive",
			"Editor.Slots", "Editor.Candidates", "Editor.SourceDiff", "Editor.PreviewParams", "Editor.Preview":
			return true
		}
	}
	return false
}
