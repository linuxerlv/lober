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
				if obj == nil && (under(caller, pkgISO) || under(caller, layer1+"/media")) {
					// 匿名接口的声明没有 Uses；也不能把仓储藏在尚未调用的字段里。
					if fn, ok := info.Defs[id].(*types.Func); ok && mediaRepositoryMethod(fn.Name()) {
						obj = fn
					}
				}
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
	if under(caller, pkgISO) || caller == pkgAssembly {
		found = append(found, mediaStateFindings(caller, fset, files, info)...)
	}
	found = append(found, interfaceCapabilityFindings(caller, fset, files, info)...)
	found = append(found, identityAuditFindings(caller, fset, files, info)...)
	found = append(found, taskFinalFindings(caller, fset, files, info)...)
	// 同一真实对象与位置可能被两条补充路径发现，只报告一次，不改变原精确断言。
	unique := found[:0]
	seen := map[string]bool{}
	for _, v := range found {
		key := string(v.rule) + "\x00" + v.pkg + "\x00" + v.subject + "\x00" + v.pos
		if !seen[key] {
			seen[key] = true
			unique = append(unique, v)
		}
	}
	return unique
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
	if (caller == modulePath+"/internal/application/media" || caller == modulePath+"/internal/application/media/port") && target == "io/fs" {
		// 一个是能力返回的缺失事实，一个是应用自身递归的停止标记；均不读文件。
		return obj.Name() != "ErrNotExist" && obj.Name() != "SkipAll"
	}
	if under(caller, layer1+"/media") {
		if _, ok := obj.(*types.Func); ok && mediaRepositoryMethod(obj.Name()) {
			return true
		}
	}
	if under(caller, layer1+"/media") && target == pkgStore {
		// 镜像规则只复用这三个纯校验符号；仓储类型和方法仍全部拒绝。
		switch transitionSymbol(obj) {
		case "CheckID", "CheckSource", "ErrInvalid":
			return caller != layer1+"/media"
		}
		return true
	}
	_, function := obj.(*types.Func)
	if under(caller, pkgConsole) {
		// 新文件也不能借本地接口查镜像；身份和任务接口仍按原阶段规则检查。
		if function && target == caller && mediaRepositoryMethod(obj.Name()) {
			if recv := obj.Type().(*types.Signature).Recv(); recv != nil {
				if _, ok := recv.Type().Underlying().(*types.Interface); ok {
					return true
				}
			}
		}
		if target == modulePath+"/internal/application/media/port" {
			return true
		}
		if target == modulePath+"/internal/application/media" && function {
			return !migratedApplicationMethod(obj.(*types.Func))
		}
		return target == pkgStore || under(target, pkgBuiltin) || under(target, pkgRender) || target == pkgNotification || target == layer1+"/access" || function && under(target, layer1) || target == pkgConsole && strings.HasPrefix(transitionSymbol(obj), "ISOSourceState.")
	}
	if under(caller, pkgISO) {
		if _, ok := obj.(*types.TypeName); ok {
			methods := types.NewMethodSet(obj.Type())
			for i := 0; i < methods.Len(); i++ {
				if mediaRepositoryMethod(methods.At(i).Obj().Name()) {
					return true
				}
			}
		}
		if target == modulePath+"/internal/application/media/port" {
			switch strings.Split(transitionSymbol(obj), ".")[0] {
			case "ISOs", "Sources", "SourceControl", "Deletion", "Waker":
				return true
			}
		}
		// 小接口也不能重新声明同名仓储能力；不会因换了声明包而放行。
		if function && mediaRepositoryMethod(obj.Name()) {
			return true
		}
		// 普通架构值检查可用于解码事实；镜像识别、状态、导入和删除规则仍由应用调用。
		if target == layer1+"/catalog" && transitionSymbol(obj) == "Arch.Valid" {
			return false
		}
		return target == pkgStore || function && under(target, layer1)
	}
	if caller == pkgAssembly && function {
		if mediaRepositoryMethod(obj.Name()) {
			return true
		}
		if target == modulePath+"/internal/application/media" {
			switch transitionSymbol(obj) {
			case "NewRegistry", "NewSources", "NewControl", "NewDeletion", "NewSupervisor", "Supervisor.Start", "Supervisor.Close":
				return false
			}
			return true
		}
		if target == modulePath+"/internal/application/media/port" || target == layer1+"/media" {
			return true
		}
		if target == pkgModel && (strings.HasPrefix(transitionSymbol(obj), "ISO.") || strings.HasPrefix(transitionSymbol(obj), "Source.") || strings.HasPrefix(transitionSymbol(obj), "ISOState.") || strings.HasPrefix(transitionSymbol(obj), "SourceState.")) {
			return true
		}
	}
	return false
}

func mediaRepositoryMethod(name string) bool {
	switch name {
	case "CreateISO", "UpdateISO", "DeprecateISO", "DeleteISO", "GetISO", "ListISOs", "SaveSource", "SaveSourceAndAcquire", "GetSource", "SourcesOf", "ListUnfinishedSources", "SaveRange", "RangesOf", "DeleteSource":
		return true
	}
	return false
}

// 文件适配器可以读取持久化事实，但不能产生业务状态；组装入口连状态判定也不承担。
func mediaStateFindings(caller string, fset *token.FileSet, files []*ast.File, info *types.Info) []violation {
	// 按字段真实类型识别；指针别名和复制的结构体仍保留这两个状态类型。
	stateType := func(t types.Type) bool {
		if t == nil {
			return false
		}
		n, ok := types.Unalias(t).(*types.Named)
		return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkgModel && (n.Obj().Name() == "ISOState" || n.Obj().Name() == "SourceState")
	}
	state := func(e ast.Expr) bool {
		s, ok := ast.Unparen(e).(*ast.SelectorExpr)
		return ok && stateType(info.TypeOf(s))
	}
	var found []violation
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			bad := false
			switch x := n.(type) {
			case *ast.SelectorExpr:
				bad = caller == pkgAssembly && state(x)
			case *ast.AssignStmt:
				for _, lhs := range x.Lhs {
					bad = bad || state(lhs)
				}
			case *ast.UnaryExpr:
				bad = x.Op == token.AND && state(x.X)
			case *ast.CompositeLit:
				if t := info.TypeOf(x); t != nil {
					if fields, ok := t.Underlying().(*types.Struct); ok {
						for i, e := range x.Elts {
							if kv, ok := e.(*ast.KeyValueExpr); ok {
								if id, ok := kv.Key.(*ast.Ident); ok {
									for j := 0; j < fields.NumFields(); j++ {
										field := fields.Field(j)
										bad = bad || field.Name() == id.Name && stateType(field.Type())
									}
								}
							} else if i < fields.NumFields() {
								bad = bad || stateType(fields.Field(i).Type())
							}
						}
					}
				}
			}
			if bad {
				found = append(found, violation{rule: ruleMigration, pkg: caller, subject: "镜像业务状态", pos: position(fset, n.Pos()), detail: "状态由镜像应用处理"})
			}
			return true
		})
	}
	return found
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
