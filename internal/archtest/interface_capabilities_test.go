package archtest

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// 名字相同不代表能力相同。这里连参数与返回类型一起核对。
func consoleTransportMethod(fn *types.Func) bool {
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Variadic() || sig.TypeParams().Len() != 0 {
		return false
	}
	match := func(params, results []types.Type) bool {
		if sig.Params().Len() != len(params) || sig.Results().Len() != len(results) {
			return false
		}
		for i, t := range params {
			if !types.Identical(sig.Params().At(i).Type(), t) {
				return false
			}
		}
		for i, t := range results {
			if !types.Identical(sig.Results().At(i).Type(), t) {
				return false
			}
		}
		return true
	}
	errType := types.Universe.Lookup("error").Type()
	switch fn.Name() {
	case "Read", "Write":
		return match([]types.Type{types.NewSlice(types.Typ[types.Byte])}, []types.Type{types.Typ[types.Int], errType})
	case "Close", "Err":
		return match(nil, []types.Type{errType})
	case "Flush":
		return match(nil, nil)
	case "Done":
		return match(nil, []types.Type{types.NewChan(types.RecvOnly, types.NewStruct(nil, nil))})
	case "Value":
		empty := types.NewInterfaceType(nil, nil).Complete()
		return match([]types.Type{empty}, []types.Type{empty})
	case "Error":
		return match(nil, []types.Type{types.Typ[types.String]})
	case "Unwrap":
		return match(nil, []types.Type{errType}) || match(nil, []types.Type{types.NewSlice(errType)})
	case "WriteHeader":
		return match([]types.Type{types.Typ[types.Int]}, nil)
	case "Deadline":
		return sig.Params().Len() == 0 && sig.Results().Len() == 2 && standardNamedType(sig.Results().At(0).Type(), "time", "Time") && types.Identical(sig.Results().At(1).Type(), types.Typ[types.Bool])
	case "Header":
		return sig.Params().Len() == 0 && sig.Results().Len() == 1 && standardNamedType(sig.Results().At(0).Type(), "net/http", "Header")
	case "ServeHTTP":
		if sig.Params().Len() != 2 || sig.Results().Len() != 0 || !standardNamedType(sig.Params().At(0).Type(), "net/http", "ResponseWriter") {
			return false
		}
		ptr, ok := types.Unalias(sig.Params().At(1).Type()).(*types.Pointer)
		return ok && standardNamedType(ptr.Elem(), "net/http", "Request")
	}
	return false
}

func standardNamedType(t types.Type, path, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != path || n.Obj().Name() != name {
		return false
	}
	original := n.Obj().Pkg().Scope().Lookup(name)
	return original != nil && types.Identical(t, original.Type())
}

func serverHTTPFunction(fn *types.Func) bool {
	if fn.Pkg() == nil || fn.Pkg().Path() != "net/http" {
		return false
	}
	switch transitionSymbol(fn) {
	case "Error", "StatusText", "SetCookie", "Redirect", "NotFound", "ServeContent", "MaxBytesReader", "DetectContentType", "HandlerFunc.ServeHTTP", "Handler.ServeHTTP", "Request.Context", "Request.UserAgent", "Request.Cookie", "Request.Cookies", "Request.FormFile", "Request.ParseMultipartForm", "Request.ParseForm", "Request.FormValue", "Request.PostFormValue", "Header.Get", "Header.Set", "Header.Add", "Header.Del", "Header.Values", "ResponseWriter.Header", "ResponseWriter.Write", "ResponseWriter.WriteHeader", "Flusher.Flush":
		return true
	}
	return false
}

func interfaceCapabilityFindings(caller string, fset *token.FileSet, files []*ast.File, info *types.Info) []violation {
	console, machine, events := under(caller, pkgConsole), under(caller, pkgMachine), under(caller, pkgEvents)
	if !console && !machine && !events {
		return nil
	}
	r := ruleMigration
	if machine || events {
		r = ruleMachine
	}
	approved := applicationFunctionValues(files, info)
	policy, localDecision := taskPolicyChecker(files, info)
	var found []violation
	for _, f := range files {
		for _, decl := range f.Decls {
			// 内置页面只在这两个真实资源入口读文件；其他文件没有文件读取许可。
			ui := consoleResourceEntry(caller, fset, f, decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				if machine || events {
					bad := false
					switch x := n.(type) {
					case *ast.BinaryExpr:
						bad = policy(x.X) || policy(x.Y)
					case *ast.SwitchStmt:
						bad = policy(x.Tag)
					case *ast.CallExpr:
						bad = localDecision(x)
					case *ast.AssignStmt:
						for _, lhs := range x.Lhs {
							bad = bad || taskStateLocation(lhs, info)
						}
					case *ast.UnaryExpr:
						bad = x.Op == token.AND && taskStateLocation(x.X, info)
					}
					if bad {
						found = append(found, violation{rule: r, pkg: caller, subject: "适配器中的业务状态判断或写入", pos: position(fset, n.Pos())})
					}
				}
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				obj := info.ObjectOf(id)
				if obj == nil || obj.Pkg() == nil {
					return true
				}
				if _, ok := obj.(*types.PkgName); ok {
					return true
				}
				target, symbol := obj.Pkg().Path(), transitionSymbol(obj)
				bad := false
				fn, function := obj.(*types.Func)
				_, callable := interfaceCallableSignature(obj)
				// 两个薄适配器只接收网关或日志输入；原始业务记录不能成为本地重算规则的入口。
				if (machine || events) && target == pkgModel {
					if _, record := obj.(*types.TypeName); record {
						bad = true
					}
				}
				if function && target == caller {
					if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
						if _, local := recv.Type().Underlying().(*types.Interface); local {
							bad = unapprovedInterfaceMethod(fn)
							if machine || events {
								bad = !consoleTransportMethod(fn)
							}
						}
					}
				}
				// 函数字段、命名函数类型和包级函数变量也能藏仓储。普通单纯值转换不因此被拒绝。
				switch obj.(type) {
				case *types.Var, *types.TypeName:
					if target == caller && !approved[obj] {
						if sig, ok := obj.Type().Underlying().(*types.Signature); ok {
							bad = bad || businessFunctionSignature(sig)
							if v, ok := obj.(*types.Var); ok && (v.IsField() || v.Parent() == v.Pkg().Scope()) {
								bad = bad || contextErrorSignature(sig)
							}
						}
					}
				}
				if (console || machine) && callable && applicationExternalImport(target) && target != "net/http" && target != "net/url" && target != "net/netip" {
					bad = true
				}
				if (console || machine) && callable && target == "net/http" {
					// 标准库的函数字段也是主动能力；只有精确的服务器方法允许调用。
					bad = bad || !function || !serverHTTPFunction(fn) && !consoleTransportMethod(fn)
				}
				if console {
					if function && under(target, modulePath+"/internal/application") && !migratedApplicationMethod(fn) {
						bad = true
					}
					if target == pkgPorts {
						if _, named := obj.(*types.TypeName); named {
							if methods, capability := obj.Type().Underlying().(*types.Interface); capability && methods.NumMethods() > 0 {
								bad = true
							}
						}
					}
					if target == "io/fs" || target == "embed" {
						if function {
							bad = bad || !ui || !consoleResourceOperation(obj)
						}
					}
					if strings.HasPrefix(target, modulePath+"/internal/application/") && strings.HasSuffix(target, "/port") {
						bad = true
					}
				}
				if machine {
					if target == pkgStore || strings.HasPrefix(target, modulePath+"/internal/application/") && target != pkgExecution {
						bad = true
					}
					if target == pkgExecution {
						switch symbol {
						case "Gateway", "BootInput", "InstallerAnswerInput", "FileInput", "SignalInput", "BootResult", "GatewayError", "GatewayFailureKind", "LookupUnavailable", "RenderRejected", "Unauthorized", "FileNotFound", "AnswerConflict", "SignalConflict", "ReportRejected", "NotSupported", "InventoryBusy", "Gateway.BootMenu", "Gateway.InstallerAnswer", "Gateway.File", "Gateway.Signal", "Gateway.InventoryReport", "InventoryInput", "GatewayError.Error", "GatewayError.Unwrap":
						default:
							bad = true
						}
					}
					if under(target, layer1) && function {
						allowed := target == pkgModel && (symbol == "ValidID" || symbol == "IsSafeWord") || target == pkgCatalog && (symbol == "Arch.Valid" || symbol == "Firmware.Valid" || symbol == "Timing.Valid" || symbol == "ArchFromIPXE" || symbol == "FirmwareFromIPXE")
						bad = bad || !allowed
					}
					if under(target, pkgExecutionRules) {
						bad = bad || symbol != "Signal"
					}
				}
				if events {
					if under(target, modulePath+"/internal/application") {
						switch {
						case target == pkgExecutionPort && (symbol == "InstallerLog" || symbol == "InstallerLogReceiver" || symbol == "IPObservationSink" || symbol == "InstallerLogReceiver.ReceiveInstallerLog" || symbol == "IPObservationSink.ObserveIP"):
						default:
							bad = true
						}
					}
					if under(target, layer1) {
						if target != pkgModel {
							bad = true
						} else if function {
							bad = bad || symbol != "ValidID" && symbol != "SafeLogTail"
						}
					}
				}
				// 字段对象是数据来源；只有函数类型字段才是能力。不能把日志包的输入字段误当外部操作。
				if field, ok := obj.(*types.Var); ok && field.IsField() {
					if _, callable := field.Type().Underlying().(*types.Signature); !callable {
						bad = false
					}
				}
				if bad {
					found = append(found, violation{rule: r, pkg: caller, subject: target + "." + symbol, pos: position(fset, id.Pos()), detail: "接口只转换并委托已登记入口，不自建业务能力"})
				}
				return true
			})
		}
	}
	return found
}

// 通知与取用领域事实都属于业务能力；纯DTO映射和注入时钟不因此被拒绝。
func businessFunctionSignature(sig *types.Signature) bool {
	if notificationFunctionSignature(sig) {
		return true
	}
	fact, contextual, fallible := false, false, false
	resultFact := false
	var inspect func(types.Type)
	seen := map[types.Type]bool{}
	inspect = func(t types.Type) {
		if t == nil || seen[t] {
			return
		}
		seen[t] = true
		t = types.Unalias(t)
		switch v := t.(type) {
		case *types.Named:
			if p := v.Obj().Pkg(); p != nil {
				fact = fact || under(p.Path(), layer1) && p.Path() != pkgCatalog
				contextual = contextual || p.Path() == "context" && v.Obj().Name() == "Context"
			}
			for i := 0; i < v.TypeArgs().Len(); i++ {
				inspect(v.TypeArgs().At(i))
			}
		case *types.Pointer:
			inspect(v.Elem())
		case *types.Slice:
			inspect(v.Elem())
		case *types.Array:
			inspect(v.Elem())
		case *types.Map:
			inspect(v.Key())
			inspect(v.Elem())
		}
	}
	for i := 0; i < sig.Params().Len(); i++ {
		inspect(sig.Params().At(i).Type())
	}
	paramFact := fact
	fact = false
	seen = map[types.Type]bool{}
	for i := 0; i < sig.Results().Len(); i++ {
		t := sig.Results().At(i).Type()
		inspect(t)
		fallible = fallible || types.Identical(t, types.Universe.Lookup("error").Type())
	}
	resultFact = fact
	return contextual && (paramFact || resultFact) || resultFact && fallible
}

// 赋值来源必须是已登记的真实应用方法；仅有相似的函数签名不获得许可。
func applicationFunctionValues(files []*ast.File, info *types.Info) map[types.Object]bool {
	bindings := map[types.Object][]ast.Expr{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			bind := func(lhs ast.Expr, rhs ast.Expr) {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					return
				}
				if obj := info.ObjectOf(id); obj != nil {
					bindings[obj] = append(bindings[obj], rhs)
				}
			}
			switch x := n.(type) {
			case *ast.ValueSpec:
				if len(x.Names) == len(x.Values) {
					for i, id := range x.Names {
						bind(id, x.Values[i])
					}
				}
			case *ast.AssignStmt:
				if len(x.Lhs) == len(x.Rhs) {
					for i, id := range x.Lhs {
						bind(id, x.Rhs[i])
					}
				}
			}
			return true
		})
	}
	approved := map[types.Object]bool{}
	visiting := map[types.Object]bool{}
	var origin func(types.Object) bool
	origin = func(obj types.Object) bool {
		if obj == nil {
			return false
		}
		if fn, ok := obj.(*types.Func); ok {
			return fn.Pkg() != nil && (migratedApplicationMethod(fn) || adapterEntryMethod(fn))
		}
		if yes, checked := approved[obj]; checked {
			return yes
		}
		if visiting[obj] || len(bindings[obj]) == 0 {
			return false
		}
		visiting[obj] = true
		valid := true
		for _, e := range bindings[obj] {
			var ref types.Object
			switch x := ast.Unparen(e).(type) {
			case *ast.Ident:
				ref = info.ObjectOf(x)
			case *ast.SelectorExpr:
				ref = info.ObjectOf(x.Sel)
			}
			valid = origin(ref) && valid
		}
		delete(visiting, obj)
		approved[obj] = valid
		return valid
	}
	for obj := range bindings {
		origin(obj)
	}
	return approved
}

func contextErrorSignature(sig *types.Signature) bool {
	contextual, fallible := false, false
	for i := 0; i < sig.Params().Len(); i++ {
		if n, ok := types.Unalias(sig.Params().At(i).Type()).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "context" && n.Obj().Name() == "Context" {
			contextual = true
		}
	}
	for i := 0; i < sig.Results().Len(); i++ {
		fallible = fallible || types.Identical(sig.Results().At(i).Type(), types.Universe.Lookup("error").Type())
	}
	return contextual && fallible
}

// 普通本地DTO的方法不会因同名被当成仓储；真实事实、通知和已知存储能力仍受限。
func unapprovedInterfaceMethod(fn *types.Func) bool {
	if consoleTransportMethod(fn) {
		return false
	}
	sig := fn.Type().(*types.Signature)
	if businessFunctionSignature(sig) || contextErrorSignature(sig) && sig.Results().Len() > 1 || taskBoundaryObject(fn, true, false) || mediaRepositoryMethod(fn.Name()) || identityRepositoryMethod(fn.Name()) {
		return true
	}
	switch fn.Name() {
	case "GetHost", "ListHosts", "CreateHost", "UpdateHost", "DeleteHost", "GetHook", "ListHooks", "GetTemplate", "ListTemplates", "Put", "Lookup", "ExpiresAt", "Drop", "DropUser":
		return true
	}
	return false
}

func adapterEntryMethod(fn *types.Func) bool {
	if fn.Pkg() == nil {
		return false
	}
	symbol := transitionSymbol(fn)
	if fn.Pkg().Path() == pkgExecution {
		switch symbol {
		case "Gateway.BootMenu", "Gateway.InstallerAnswer", "Gateway.File", "Gateway.Signal":
			return true
		}
	}
	if fn.Pkg().Path() == pkgExecutionPort {
		return symbol == "InstallerLogReceiver.ReceiveInstallerLog" || symbol == "IPObservationSink.ObserveIP"
	}
	return false
}

// 类型声明的函数类型可用于转换；只有真实函数和函数值才是外部可调用来源。
func interfaceCallableSignature(obj types.Object) (*types.Signature, bool) {
	switch obj.(type) {
	case *types.Func, *types.Var:
		if obj.Type() != nil {
			sig, ok := obj.Type().Underlying().(*types.Signature)
			return sig, ok
		}
	}
	return nil, false
}

// 对照Notifier的发布和订阅形状，不使用本地字段或方法的名字判断职责。
func notificationFunctionSignature(sig *types.Signature) bool {
	if sig == nil || sig.Variadic() || sig.TypeParams().Len() != 0 || sig.Params().Len() != 1 || !types.Identical(sig.Params().At(0).Type().Underlying(), types.Typ[types.String]) {
		return false
	}
	if sig.Results().Len() == 0 {
		return true
	}
	if sig.Results().Len() != 2 {
		return false
	}
	hint, ok := sig.Results().At(0).Type().Underlying().(*types.Chan)
	if !ok || hint.Dir() != types.RecvOnly || !types.Identical(hint.Elem().Underlying(), types.NewStruct(nil, nil)) {
		return false
	}
	unsubscribe, ok := sig.Results().At(1).Type().Underlying().(*types.Signature)
	return ok && !unsubscribe.Variadic() && unsubscribe.TypeParams().Len() == 0 && unsubscribe.Params().Len() == 0 && unsubscribe.Results().Len() == 0
}
