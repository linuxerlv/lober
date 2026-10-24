package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

const ruleTaskFinal rule = "任务接口与组装只转换和委托，不能重建仓储、控制、通知、渲染或状态规则"

func taskFinalFindings(caller string, fset *token.FileSet, files []*ast.File, info *types.Info) []violation {
	console, assembly := under(caller, pkgConsole), caller == pkgAssembly
	if !console && !assembly {
		return nil
	}
	policy := taskPolicyChecker(files, info)
	var found []violation
	for _, f := range files {
		logging := map[*ast.Ident]bool{}
		if assembly && strings.HasSuffix(filepath.ToSlash(fset.Position(f.Pos()).Filename), "cmd/lober/logging.go") {
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && taskLoggingDelegate(fn, info) {
					ast.Inspect(fn, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok {
							logging[id] = true
						}
						return true
					})
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			report := func(subject string) {
				found = append(found, violation{rule: ruleTaskFinal, pkg: caller, subject: subject, pos: position(fset, n.Pos())})
			}
			if id, ok := n.(*ast.Ident); ok {
				obj := info.ObjectOf(id)
				if taskBoundaryObject(obj, console, assembly) && !logging[id] {
					report(obj.Pkg().Path() + "." + obj.Name())
				}
			}
			write := false
			switch x := n.(type) {
			case *ast.BinaryExpr:
				if policy(x.X) || policy(x.Y) {
					report("任务状态或固件判断")
				}
			case *ast.SwitchStmt:
				if policy(x.Tag) {
					report("任务状态或固件分支")
				}
			case *ast.CallExpr:
				// EqualFold等返回布尔值的函数也不能替代直接比较。
				if typ := info.TypeOf(x); typ != nil && types.Identical(typ, types.Typ[types.Bool]) && policy(x) {
					report("任务状态或固件判断")
				}
			case *ast.AssignStmt:
				for _, lhs := range x.Lhs {
					write = write || taskStateLocation(lhs, info)
				}
			case *ast.UnaryExpr:
				write = x.Op == token.AND && taskStateLocation(x.X, info)
			case *ast.CompositeLit:
				// 复制的Task结构仍保留原字段对象；普通网页输出结构不属于Task。
				if typ := info.TypeOf(x); typ != nil {
					if fields, ok := typ.Underlying().(*types.Struct); ok {
						for i, elt := range x.Elts {
							if kv, ok := elt.(*ast.KeyValueExpr); ok {
								if id, ok := kv.Key.(*ast.Ident); ok {
									for j := 0; j < fields.NumFields(); j++ {
										field := fields.Field(j)
										write = write || field.Name() == id.Name && taskStateField(field)
									}
								}
							} else if i < fields.NumFields() {
								write = write || taskStateField(fields.Field(i))
							}
						}
					}
				}
			}
			if write {
				report("任务状态写入或可写地址")
			}
			return true
		})
	}
	return found
}

// 只追同包返回值、局部赋值和容器内容；容器长度与类型描述不继承元素来源。
func taskPolicyChecker(files []*ast.File, info *types.Info) func(ast.Expr) bool {
	origins := map[types.Object][]ast.Expr{}
	elementOrigins := map[types.Object][]ast.Expr{}
	functions := map[types.Object]*ast.FuncDecl{}
	returns := map[ast.Node][]ast.Expr{}
	remember := func(node ast.Node, body *ast.BlockStmt, results *ast.FieldList) {
		ast.Inspect(body, func(n ast.Node) bool {
			if _, nested := n.(*ast.FuncLit); nested {
				return false
			}
			if ret, ok := n.(*ast.ReturnStmt); ok {
				returns[node] = append(returns[node], ret.Results...)
				if len(ret.Results) == 0 && results != nil {
					for _, field := range results.List {
						for _, id := range field.Names {
							returns[node] = append(returns[node], id)
						}
					}
				}
			}
			return true
		})
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				functions[info.Defs[fn.Name]] = fn
				remember(fn, fn.Body, fn.Type.Results)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				remember(x, x.Body, x.Type.Results)
			case *ast.RangeStmt:
				if id, ok := x.Value.(*ast.Ident); ok {
					elementOrigins[info.ObjectOf(id)] = append(elementOrigins[info.ObjectOf(id)], x.X)
				}
			case *ast.AssignStmt:
				if len(x.Lhs) == len(x.Rhs) {
					for i, lhs := range x.Lhs {
						if id, ok := lhs.(*ast.Ident); ok {
							origins[info.ObjectOf(id)] = append(origins[info.ObjectOf(id)], x.Rhs[i])
						}
						if index, ok := lhs.(*ast.IndexExpr); ok {
							if id, ok := index.X.(*ast.Ident); ok {
								origins[info.ObjectOf(id)] = append(origins[info.ObjectOf(id)], x.Rhs[i])
							}
						}
					}
				}
			case *ast.ValueSpec:
				if len(x.Names) == len(x.Values) {
					for i, id := range x.Names {
						origins[info.Defs[id]] = append(origins[info.Defs[id]], x.Values[i])
					}
				}
			}
			return true
		})
	}
	type target struct {
		node ast.Node
		sig  *types.Signature
		expr ast.Expr
	}
	var callable func(ast.Expr, map[types.Object]bool) []target
	callable = func(e ast.Expr, seen map[types.Object]bool) []target {
		if fn, ok := ast.Unparen(e).(*ast.FuncLit); ok {
			if sig, ok := info.TypeOf(fn).(*types.Signature); ok {
				return []target{{fn, sig, e}}
			}
		}
		if conversion, ok := ast.Unparen(e).(*ast.CallExpr); ok && info.Types[conversion.Fun].IsType() && len(conversion.Args) == 1 {
			return callable(conversion.Args[0], seen)
		}
		var obj types.Object
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			obj = info.ObjectOf(x)
		case *ast.SelectorExpr:
			obj = info.Uses[x.Sel]
		}
		if obj == nil || seen[obj] {
			return nil
		}
		if fn := functions[obj]; fn != nil {
			return []target{{fn, info.Defs[fn.Name].Type().(*types.Signature), e}}
		}
		seen[obj] = true
		defer delete(seen, obj)
		var out []target
		for _, source := range origins[obj] {
			out = append(out, callable(source, seen)...)
		}
		return out
	}
	var visit func(ast.Expr, bool, map[types.Object]ast.Expr, map[types.Object]bool, map[ast.Node]bool) bool
	visit = func(e ast.Expr, elements bool, bindings map[types.Object]ast.Expr, seen map[types.Object]bool, calls map[ast.Node]bool) bool {
		if e == nil {
			return false
		}
		if taskPolicyType(info.TypeOf(e)) {
			return true
		}
		if !elements {
			if typ := info.TypeOf(e); typ != nil {
				if _, scalar := typ.Underlying().(*types.Basic); !scalar {
					return false
				}
			}
		}
		next := func(value ast.Expr, contents bool) bool { return visit(value, contents, bindings, seen, calls) }
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			obj := info.ObjectOf(x)
			if obj == nil || seen[obj] {
				return false
			}
			seen[obj] = true
			defer delete(seen, obj)
			if value := bindings[obj]; value != nil {
				return next(value, elements)
			}
			for _, value := range origins[obj] {
				if next(value, elements) {
					return true
				}
			}
			for _, container := range elementOrigins[obj] {
				if next(container, true) {
					return true
				}
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok {
				if builtin, ok := info.Uses[id].(*types.Builtin); ok {
					switch builtin.Name() {
					case "len", "cap":
						if len(x.Args) == 1 {
							if typ := info.TypeOf(x.Args[0]); typ != nil {
								if basic, ok := typ.Underlying().(*types.Basic); ok && basic.Info()&types.IsString != 0 {
									return next(x.Args[0], false)
								}
							}
						}
						return false
					case "append":
						if elements {
							for i, arg := range x.Args {
								if next(arg, i == 0 || x.Ellipsis.IsValid()) {
									return true
								}
							}
						}
						return false
					}
				}
			}
			if info.Types[x.Fun].IsType() {
				for _, arg := range x.Args {
					if next(arg, false) || next(arg, true) {
						return true
					}
				}
				return false
			}
			local := callable(x.Fun, map[types.Object]bool{})
			if len(local) > 0 {
				for _, target := range local {
					fn := target.node
					if calls[fn] {
						continue
					}
					bound := map[types.Object]ast.Expr{}
					for obj, value := range bindings {
						bound[obj] = value
					}
					sig := target.sig
					offset := 0
					if sel, ok := target.expr.(*ast.SelectorExpr); ok && info.Selections[sel] != nil && info.Selections[sel].Kind() == types.MethodExpr {
						offset = 1
					}
					for i := 0; i < sig.Params().Len() && i+offset < len(x.Args); i++ {
						bound[sig.Params().At(i)] = x.Args[i+offset]
					}
					if sig.Recv() != nil {
						if sel, ok := target.expr.(*ast.SelectorExpr); ok {
							if offset == 1 && len(x.Args) > 0 {
								bound[sig.Recv()] = x.Args[0]
							} else {
								bound[sig.Recv()] = sel.X
							}
						}
					}
					calls[fn] = true
					for _, value := range returns[fn] {
						if visit(value, elements, bound, seen, calls) {
							delete(calls, fn)
							return true
						}
					}
					delete(calls, fn)
				}
				return false
			}
			if !elements {
				for _, arg := range x.Args {
					if next(arg, false) || next(arg, true) {
						return true
					}
				}
			}
		case *ast.IndexExpr:
			return next(x.X, true) || next(x.X, false) || next(x.Index, false)
		case *ast.CompositeLit:
			if elements {
				for _, value := range x.Elts {
					if kv, ok := value.(*ast.KeyValueExpr); ok {
						if next(kv.Key, false) || next(kv.Value, false) || next(kv.Value, true) {
							return true
						}
					} else if next(value, false) || next(value, true) {
						return true
					}
				}
			}
		case *ast.TypeAssertExpr:
			return next(x.X, true) || next(x.X, false)
		case *ast.SliceExpr:
			return elements && next(x.X, true)
		case *ast.StarExpr:
			return next(x.X, elements)
		case *ast.UnaryExpr:
			return next(x.X, elements)
		}
		return false
	}
	return func(e ast.Expr) bool {
		return visit(e, false, map[types.Object]ast.Expr{}, map[types.Object]bool{}, map[ast.Node]bool{})
	}
}

func taskPolicyType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	typ = types.Unalias(typ)
	if ptr, ok := typ.(*types.Pointer); ok {
		return taskPolicyType(ptr.Elem())
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == pkgModel && named.Obj().Name() == "TaskState" || named.Obj().Pkg().Path() == pkgCatalog && named.Obj().Name() == "Firmware"
}

func taskStateField(field *types.Var) bool {
	return field.IsField() && field.Pkg() != nil && field.Pkg().Path() == pkgModel && field.Name() == "State" && taskPolicyType(field.Type())
}

func taskStateLocation(e ast.Expr, info *types.Info) bool {
	if sel, ok := ast.Unparen(e).(*ast.SelectorExpr); ok {
		if field, ok := info.Uses[sel.Sel].(*types.Var); ok {
			return taskStateField(field)
		}
	}
	if ptr, ok := ast.Unparen(e).(*ast.StarExpr); ok {
		return taskPolicyType(info.TypeOf(ptr))
	}
	return false
}

// 使用真实函数对象；别名、方法值、嵌入方法和新文件中的声明都经过同一规则。
func taskBoundaryObject(obj types.Object, console, assembly bool) bool {
	if obj == nil || obj.Pkg() == nil || obj.Type() == nil {
		return false
	}
	sig, function := obj.Type().Underlying().(*types.Signature)
	if !function {
		return false
	}
	if _, ok := obj.(*types.TypeName); ok {
		return false
	}
	target, name := obj.Pkg().Path(), obj.Name()
	// 原类型检查已逐符号拒绝网页访问真实仓储方法；这里只补尚未覆盖的旁路。
	if _, method := obj.(*types.Func); console && method && target == pkgStore {
		return false
	}
	if target == pkgTasks {
		if fn, ok := obj.(*types.Func); ok && migratedApplicationMethod(fn) {
			return assembly
		}
	}
	if target == pkgTasksPort {
		return true
	}
	switch name {
	case "GetTask", "ListTasks", "ListEvents", "FindOpenTask", "CanCancel", "CanRetry", "Publish", "Subscribe", "RenderFrozen", "CreateTasks", "CreateCurrentTasks", "TransitionTask", "TransitionTaskByUser", "UpdateTaskExecution":
		return true
	case "NewHub":
		return console
	case "EnsureBuiltinHook", "EnsureBuiltinTemplate":
		return assembly
	case "Cancel", "Retry":
		if assembly && target == pkgExecution {
			return true
		}
		for i := 0; i < sig.Params().Len(); i++ {
			if taskControlRecord(sig.Params().At(i).Type()) {
				return true
			}
		}
	}
	return false
}

// 指针和别名拆开后，对照真实Task的完整结构；同名的无关类型不会被当成任务。
func taskControlRecord(typ types.Type) bool {
	for typ != nil {
		typ = types.Unalias(typ)
		if ptr, ok := typ.(*types.Pointer); ok {
			typ = ptr.Elem()
			continue
		}
		break
	}
	if typ == nil {
		return false
	}
	fields, ok := typ.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < fields.NumFields(); i++ {
		pkg := fields.Field(i).Pkg()
		if pkg == nil || pkg.Path() != pkgModel {
			continue
		}
		if original := pkg.Scope().Lookup("Task"); original != nil && types.Identical(fields, original.Type().Underlying()) {
			return true
		}
	}
	return false
}

// 四个既有计时装饰只能原样转交、记录耗时、原样返回。文件名本身不提供许可。
func taskLoggingDelegate(fn *ast.FuncDecl, info *types.Info) bool {
	ops := map[string]string{"CreateTasks": "task.create", "CreateCurrentTasks": "task.create", "TransitionTask": "task.transition", "UpdateTaskExecution": "task.execution"}
	op, ok := ops[fn.Name.Name]
	if !ok || declarationName(fn) != "(*loggingStore)."+fn.Name.Name || fn.Body == nil || len(fn.Body.List) != 4 || fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 {
		return false
	}
	object, ok := info.Defs[fn.Name].(*types.Func)
	if !ok {
		return false
	}
	sig := object.Type().(*types.Signature)
	receiver := info.Defs[fn.Recv.List[0].Names[0]]
	same := func(e ast.Expr, obj types.Object) bool {
		id, ok := e.(*ast.Ident)
		return ok && obj != nil && info.Uses[id] == obj
	}
	start, ok := fn.Body.List[0].(*ast.AssignStmt)
	if !ok || start.Tok != token.DEFINE || len(start.Lhs) != 1 || len(start.Rhs) != 1 {
		return false
	}
	startID, ok := start.Lhs[0].(*ast.Ident)
	if !ok {
		return false
	}
	now, ok := start.Rhs[0].(*ast.CallExpr)
	if !ok || len(now.Args) != 0 {
		return false
	}
	nowSel, ok := now.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	nowObj := info.Uses[nowSel.Sel]
	if nowObj == nil || nowObj.Pkg() == nil || nowObj.Pkg().Path() != "time" || nowObj.Name() != "Now" {
		return false
	}
	delegated, ok := fn.Body.List[1].(*ast.AssignStmt)
	if !ok || delegated.Tok != token.DEFINE || len(delegated.Rhs) != 1 || len(delegated.Lhs) != sig.Results().Len() {
		return false
	}
	call, ok := delegated.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) != sig.Params().Len() || call.Ellipsis.IsValid() {
		return false
	}
	method, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || method.Sel.Name != fn.Name.Name {
		return false
	}
	called := info.Uses[method.Sel]
	if called == nil || called.Pkg() == nil || called.Pkg().Path() != pkgStore {
		return false
	}
	store, ok := method.X.(*ast.SelectorExpr)
	if !ok || store.Sel.Name != "Store" || !same(store.X, receiver) {
		return false
	}
	field, ok := info.Uses[store.Sel].(*types.Var)
	if !ok || !field.Embedded() {
		return false
	}
	contract, ok := types.Unalias(field.Type()).(*types.Named)
	if !ok || contract.Obj().Pkg() == nil || contract.Obj().Pkg().Path() != pkgStore || contract.Obj().Name() != "Store" {
		return false
	}
	for i, arg := range call.Args {
		if !same(arg, sig.Params().At(i)) {
			return false
		}
	}
	results := make([]types.Object, len(delegated.Lhs))
	for i, lhs := range delegated.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			return false
		}
		results[i] = info.Defs[id]
		if results[i] == nil {
			return false
		}
	}
	statement, ok := fn.Body.List[2].(*ast.ExprStmt)
	if !ok {
		return false
	}
	log, ok := statement.X.(*ast.CallExpr)
	if !ok || len(log.Args) != 3 {
		return false
	}
	logSel, ok := log.Fun.(*ast.SelectorExpr)
	if !ok || logSel.Sel.Name != "log" || !same(logSel.X, receiver) {
		return false
	}
	label, ok := log.Args[0].(*ast.BasicLit)
	if !ok || label.Kind != token.STRING {
		return false
	}
	text, err := strconv.Unquote(label.Value)
	if err != nil || text != op || !same(log.Args[1], info.Defs[startID]) || !same(log.Args[2], results[len(results)-1]) {
		return false
	}
	returned, ok := fn.Body.List[3].(*ast.ReturnStmt)
	if !ok || len(returned.Results) != len(results) {
		return false
	}
	for i, result := range returned.Results {
		if !same(result, results[i]) {
			return false
		}
	}
	return true
}

func Test任务终态边界拒绝仓储和后端旁路(t *testing.T) {
	imports := identityFixtureImports(t, pkgTasksPort, pkgNotification, pkgRender, pkgBuiltin, "net/http", "math/rand/v2", "log/slog")
	for _, tc := range []struct {
		name, caller, body string
		allowed            bool
	}{
		{"完整签名本地接口方法值", pkgConsole, `import("context";"` + pkgModel + `");type leaked interface{GetTask(context.Context,string)(model.Task,error)};func bypass(ctx context.Context,r leaked){call:=r.GetTask;_,_=call(ctx,"task-id")}`, false},
		{"本地接口嵌入", pkgConsole, `import("context";m "` + pkgModel + `");type leaked interface{GetTask(context.Context,string)(m.Task,error)};type embedded interface{leaked};func f(r embedded){_=r.GetTask}`, false},
		{"别名及指针结构嵌入", pkgConsole, `import p "` + pkgTasksPort + `";type alias=p.TaskGetter;type wrapped struct{alias};func f(r *wrapped){_=r.GetTask}`, false},
		{"新文件未调用接口声明", pkgConsole, `import("context";m "` + pkgModel + `");type holder struct{R interface{GetTask(context.Context,string)(m.Task,error)}}`, false},
		{"同包新实现", pkgConsole, `import("context";m "` + pkgModel + `");type local struct{};func(local)GetTask(context.Context,string)(m.Task,error){return m.Task{},nil}`, false},
		{"辅助函数转发", pkgConsole, `import("context";m "` + pkgModel + `");type leaked interface{GetTask(context.Context,string)(m.Task,error)};func helper(r leaked){_,_=r.GetTask(nil,"id")};func f(r leaked){helper(r)}`, false},
		{"本地列表接口", pkgConsole, `import("context";m "` + pkgModel + `";r "` + pkgStore + `");type leaked interface{ListTasks(context.Context,r.TaskQuery)(r.Page[m.Task],error)};func f(r leaked){_=r.ListTasks}`, false},
		{"本地事件接口", pkgConsole, `import("context";r "` + pkgStore + `");type leaked interface{ListEvents(context.Context,string,int64,int)([]r.Event,error)};func f(r leaked){_=r.ListEvents}`, false},
		{"后端控制", pkgConsole, `import p "` + pkgTasksPort + `";func f(c p.Control){_=c.Cancel;_=c.Retry}`, false},
		{"本地后端控制", pkgConsole, `import("context";m "` + pkgModel + `";p "` + pkgPorts + `");type leaked interface{Cancel(context.Context,m.Task,p.AuditIntent)error};func f(c leaked){_=c.Cancel}`, false},
		{"审查I3指针控制直接调用", pkgConsole, `import("context";m "` + pkgModel + `";p "` + pkgPorts + `");type leaked interface{Cancel(context.Context,*m.Task,p.AuditIntent)error};func bypass(ctx context.Context,c leaked,task *m.Task,audit p.AuditIntent)error{return c.Cancel(ctx,task,audit)}`, false},
		{"审查I3指针重试方法值", pkgConsole, `import("context";m "` + pkgModel + `";p "` + pkgPorts + `");type leaked interface{Retry(context.Context,*m.Task,p.AuditIntent)error};func bypass(ctx context.Context,c leaked,task *m.Task,audit p.AuditIntent)error{retry:=c.Retry;return retry(ctx,task,audit)}`, false},
		{"审查I3指针别名嵌入接口", pkgConsole, `import("context";m "` + pkgModel + `";p "` + pkgPorts + `");type taskAlias = *m.Task;type leaked interface{Cancel(context.Context,taskAlias,p.AuditIntent)error};type embedded interface{leaked};func bypass(ctx context.Context,c embedded,task taskAlias,audit p.AuditIntent)error{cancel:=c.Cancel;return cancel(ctx,task,audit)}`, false},
		{"审查I3复制任务值控制", pkgConsole, `import("context";m "` + pkgModel + `";p "` + pkgPorts + `");type copied m.Task;type leaked interface{Retry(context.Context,copied,p.AuditIntent)error};func bypass(ctx context.Context,c leaked,task copied,audit p.AuditIntent)error{return c.Retry(ctx,task,audit)}`, false},
		{"审查I3复制任务指针控制", pkgConsole, `import("context";m "` + pkgModel + `";p "` + pkgPorts + `");type copied m.Task;type leaked interface{Cancel(context.Context,*copied,p.AuditIntent)error};func bypass(ctx context.Context,c leaked,task *copied,audit p.AuditIntent)error{return c.Cancel(ctx,task,audit)}`, false},
		{"无关同名结构不当成任务", pkgConsole, `import "context";type Task struct{Label string};type local interface{Cancel(context.Context,*Task)error};func f(c local,t *Task)error{return c.Cancel(nil,t)}`, true},
		{"本地诊断", pkgConsole, `import m "` + pkgModel + `";type leaked interface{CanRetry(m.Task)[]m.Problem};func f(c leaked){_=c.CanRetry}`, false},
		{"通知构造别名", pkgConsole, `import n "` + pkgNotification + `";var build=n.NewHub`, false},
		{"通知发布方法值", pkgConsole, `import p "` + pkgPorts + `";func f(n p.Notifier){_=n.Publish}`, false},
		{"本地通知发布", pkgConsole, `type local interface{Publish(string)};func f(n local){_=n.Publish}`, false},
		{"直接状态比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{return t.State==m.TaskDone}`, false},
		{"指针别名状态分支", pkgConsole, `import m "` + pkgModel + `";type alias=m.Task;func f(t *alias)bool{switch t.State{case m.TaskDone:return true};return false}`, false},
		{"复制结构状态分支", pkgConsole, `import m "` + pkgModel + `";type copied m.Task;func f(t copied)bool{return t.State=="done"}`, false},
		{"字符串转换后状态比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{s:=string(t.State);return s=="done"}`, false},
		{"审查I1状态辅助返回比较", pkgConsole, `import m "` + pkgModel + `";func stateText(t m.Task)string{return string(t.State)};func byHelper(t m.Task)bool{return stateText(t)=="done"}`, false},
		{"审查I1状态容器元素比较", pkgConsole, `import m "` + pkgModel + `";func byContainer(t m.Task)bool{values:=[]string{string(t.State)};return values[0]=="done"}`, false},
		{"审查I1固件辅助返回比较", pkgConsole, `import m "` + pkgModel + `";func firmwareText(t m.Task)string{return string(t.Snapshot.Host.Firmware)};func byHelper(t m.Task)bool{return firmwareText(t)=="uefi"}`, false},
		{"审查I1固件容器元素比较", pkgConsole, `import m "` + pkgModel + `";func byContainer(t m.Task)bool{values:=[]string{string(t.Snapshot.Host.Firmware)};return values[0]=="uefi"}`, false},
		{"状态容器辅助返回元素比较", pkgConsole, `import m "` + pkgModel + `";func stateTexts(t m.Task)[]string{return []string{string(t.State)}};func f(t m.Task)bool{return stateTexts(t)[0]=="done"}`, false},
		{"状态映射元素比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{values:=map[string]string{"state":string(t.State)};return values["state"]=="done"}`, false},
		{"状态追加容器元素比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{var values []string;values=append(values,string(t.State));return values[0]=="done"}`, false},
		{"审查I1状态容器Join比较", pkgConsole, `import("strings";m "` + pkgModel + `");func f(t m.Task)bool{values:=[]string{string(t.State)};return strings.Join(values,"")=="done"}`, false},
		{"审查I1固件容器Join比较", pkgConsole, `import("strings";m "` + pkgModel + `");func f(t m.Task)bool{values:=[]string{string(t.Snapshot.Host.Firmware)};return strings.Join(values,"")=="uefi"}`, false},
		{"纯状态Join输出", pkgConsole, `import("strings";m "` + pkgModel + `");func f(t m.Task)string{return strings.Join([]string{string(t.State)},"")}`, true},
		{"纯固件Join输出", pkgConsole, `import("strings";m "` + pkgModel + `");func f(t m.Task)string{return strings.Join([]string{string(t.Snapshot.Host.Firmware)},"")}`, true},
		{"审查I1状态字符串索引比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{return string(t.State)[0]=='d'}`, false},
		{"审查I1固件字符串索引比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{return string(t.Snapshot.Host.Firmware)[0]=='u'}`, false},
		{"审查I1状态函数文字返回比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{text:=func(task m.Task)string{return string(task.State)};return text(t)=="done"}`, false},
		{"审查I1固件函数文字返回比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{text:=func(task m.Task)string{return string(task.Snapshot.Host.Firmware)};return text(t)=="uefi"}`, false},
		{"审查I1状态range取值比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{for _,v:=range []string{string(t.State)}{return v=="done"};return false}`, false},
		{"审查I1固件range取值比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{for _,v:=range []string{string(t.Snapshot.Host.Firmware)}{return v=="uefi"};return false}`, false},
		{"审查I1状态容器转换比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{return string([]byte(string(t.State)))=="done"}`, false},
		{"审查I1固件容器转换比较", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{return string([]byte(string(t.Snapshot.Host.Firmware)))=="uefi"}`, false},
		{"明确接口转换保留状态", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{return any(string(t.State)).(string)=="done"}`, false},
		{"明确接口转换保留固件", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{return any(string(t.Snapshot.Host.Firmware)).(string)=="uefi"}`, false},
		{"函数文字纯状态输出", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)string{text:=func(task m.Task)string{return string(task.State)};return text(t)}`, true},
		{"range纯状态输出", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)string{for _,v:=range []string{string(t.State)}{return v};return ""}`, true},
		{"函数文字常量不沾状态", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{label:=func(s string)string{return "ready"};return label(string(t.State))=="ready"}`, true},
		{"纯状态容器长度", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)bool{values:=[]string{string(t.State)};return len(values)>0}`, true},
		{"辅助函数仅输出状态文本", pkgConsole, `import m "` + pkgModel + `";func stateText(t m.Task)string{return string(t.State)};func f(t m.Task)string{return stateText(t)}`, true},
		{"辅助函数返回普通字段", pkgConsole, `import m "` + pkgModel + `";func name(t m.Task)string{return t.ID};func f(t m.Task)bool{return name(t)=="example"}`, true},
		{"辅助函数返回常量不沾输入状态", pkgConsole, `import m "` + pkgModel + `";func label(s string)string{return "ready"};func f(t m.Task)bool{return label(string(t.State))=="ready"}`, true},
		{"固件比较", pkgConsole, `import c "` + pkgCatalog + `";func f(v c.Firmware)bool{return v==c.FirmwareUEFI}`, false},
		{"固件空值回退", pkgConsole, `import c "` + pkgCatalog + `";func f(v *c.Firmware){if *v==""{*v=c.FirmwareUEFI}}`, false},
		{"状态赋值", pkgConsole, `import m "` + pkgModel + `";func f(t *m.Task){t.State=m.TaskCancelled}`, false},
		{"组装直接任务查询", pkgAssembly, `import p "` + pkgTasksPort + `";func f(r p.TaskGetter){_=r.GetTask}`, false},
		{"组装应用查询", pkgAssembly, `import a "` + pkgTasks + `";func f(r a.Reader){_=r.Get}`, false},
		{"组装应用控制", pkgAssembly, `import a "` + pkgTasks + `";func f(c a.Controller){_=c.Retry}`, false},
		{"组装后端控制", pkgAssembly, `import p "` + pkgTasksPort + `";func f(c p.Control){_=c.Cancel}`, false},
		{"组装渲染能力", pkgAssembly, `import p "` + pkgPorts + `";func f(r p.FrozenPreview){_=r.RenderFrozen}`, false},
		{"组装本地渲染", pkgAssembly, `import("context";"` + pkgModel + `";"` + pkgCatalog + `";p "` + pkgPorts + `");type local interface{RenderFrozen(context.Context,model.Snapshot,catalog.Firmware,string)([]p.PreviewFile,[]string,[]model.Problem,error)};func f(r local){_=r.RenderFrozen}`, false},
		{"组装通知", pkgAssembly, `import p "` + pkgPorts + `";func f(n p.Notifier){_=n.Publish}`, false},
		{"组装状态赋值", pkgAssembly, `import m "` + pkgModel + `";type copied m.Task;func f(t *copied){t.State=m.TaskPending}`, false},
		{"组装复制结构状态初始化", pkgAssembly, `import m "` + pkgModel + `";type copied m.Task;func f()copied{return copied{State:m.TaskPending}}`, false},
		{"组装取状态地址", pkgAssembly, `import m "` + pkgModel + `";func f(t *m.Task)*m.TaskState{return &t.State}`, false},
		{"组装内置重试", pkgAssembly, `import r "` + pkgStore + `";func f(s r.Store){_=s.EnsureBuiltinHook;_=s.EnsureBuiltinTemplate}`, false},
		{"日志类型隐藏查询", pkgAssembly, `import("context";r "` + pkgStore + `");type loggingStore struct{r.Store};func(l *loggingStore)hidden(ctx context.Context){read:=l.Store.GetTask;_,_=read(ctx,"id")}`, false},
		{"日志辅助隐藏状态判断", pkgAssembly, `import m "` + pkgModel + `";type loggingStore struct{};func(*loggingStore)log(t m.Task)bool{return t.State==m.TaskDone}`, false},
		{"组装原生命周期", pkgAssembly, `import e "` + pkgExecution + `";func f(s *e.Service){_=s.Sweep}`, true},
		{"真实网页读入口", pkgConsole, `import a "` + pkgTasks + `";func f(r a.Reader){_=r.List;_=r.Get;_=r.Preview}`, true},
		{"真实网页控制入口", pkgConsole, `import a "` + pkgTasks + `";func f(c a.Controller){_=c.Cancel;_=c.Retry}`, true},
		{"真实网页事件入口", pkgConsole, `import a "` + pkgTasks + `";func f(r a.EventReader,s a.EventSubscription){_=r.List;_=r.Open;_=s.Initial;_=s.Hints;_=s.Read;_=s.Close}`, true},
		{"纯字段转换", pkgConsole, `import(m "` + pkgModel + `";c "` + pkgCatalog + `");type dto struct{State m.TaskState;Firmware c.Firmware};func f(t m.Task)dto{return dto{State:t.State,Firmware:t.Snapshot.Host.Firmware}}`, true},
		{"状态转输出文本", pkgConsole, `import m "` + pkgModel + `";func f(t m.Task)string{return string(t.State)}`, true},
		{"构造应用", pkgAssembly, `import a "` + pkgTasks + `";func f(){_=a.NewReader;_=a.NewController;_=a.NewEventReader;_=a.NewWizard;_=a.NewUnavailableControl}`, true},
		{"只持有能力供构造", pkgAssembly, `import(p "` + pkgTasksPort + `";a "` + pkgTasks + `";r "` + pkgStore + `");func f(s r.Store,c p.Control)a.Reader{return a.NewReader(a.ReaderDeps{Tasks:s,Diagnostics:c})}`, true},
		{"发布清单字段", pkgAssembly, `import(b "` + pkgBuiltin + `";m "` + pkgModel + `");func f()[]m.TemplateKey{var out []m.TemplateKey;for _,t:=range b.Templates(){out=append(out,m.TemplateKey{ID:t.ID,Version:t.Version})};return out}`, true},
		{"公开枚举列表转换", pkgConsole, `import a "` + modulePath + `/internal/application/authoring";func f()[]string{var values []string;for _,v:=range a.PublicCatalog().Firmwares{values=append(values,string(v))};if len(values)>0{return values};return nil}`, true},
		{"枚举类型描述", pkgConsole, `import("reflect";c "` + pkgCatalog + `");func f()bool{return reflect.TypeOf(c.Firmware(""))!=nil}`, true},
		{"无关普通比较", pkgAssembly, `func f(s string)bool{return s=="ready"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, tc.caller, "task_extra.go", tc.body)
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v findings=%v", tc.allowed, got)
			}
		})
	}
}

func Test任务应用只使用本模块契约(t *testing.T) {
	imports := identityFixtureImports(t, pkgPgstore, pkgNotification, "net/http", "math/rand/v2", "database/sql", "os/exec", "syscall", "io/fs")
	for _, caller := range []string{pkgTasks, pkgTasksPort} {
		for _, body := range []string{
			`import a "` + modulePath + `/internal/application/authoring";var _ a.Hooks`,
			`import e "` + pkgExecution + `";var _ *e.Service`,
			`import g "` + gin + `";var _ *g.Context`,
			`import p "` + pkgPgstore + `";var _ *p.Store`,
			`import n "` + pkgNotification + `";var _=n.NewHub`,
			`import "os";var _=os.ReadFile`,
			`import "net/http";var _=http.Get`,
			`import "crypto/rand";var _=rand.Read`,
			`import "math/rand/v2";var _=rand.Uint64`,
			`import "database/sql";var _=sql.Open`,
			`import "os/exec";var _=exec.Command`,
			`import "syscall";var _=syscall.Open`,
			`import "io/fs";func readTaskFile(files fs.FS)([]byte,error){return fs.ReadFile(files,"tasks.json")}`,
		} {
			t.Run(caller+body, func(t *testing.T) {
				if got := identityTypedFixture(t, imports, caller, "task_extra.go", body); len(got) == 0 {
					t.Fatal("任务应用越过能力接口")
				}
			})
		}
	}
}

func Test终态零许可且历史不能复活调用(t *testing.T) {
	for name, current := range phaseSixCurrentTables() {
		t.Run(name, func(t *testing.T) {
			if len(current) != 0 {
				t.Fatalf("当前许可必须逐表为0: %d", len(current))
			}
		})
	}
	// 复用原始摘要和实际类型调用的完整断言，不能只检查表项数量。
	t.Run("原始39项摘要", Test第六阶段历史保持39项原始摘要)
	t.Run("删除39项不能复活", Test第六阶段迁移精确删除39项当前四表全零)
	t.Run("原位置真实调用", Test错误枚举任务读取与控制历史不能放行已删除的真实类型调用)
	t.Run("身份审计68项历史", Test身份审计精确删除68项保留39项)
	t.Run("镜像154项历史", Test镜像迁移精确删除154项并保持后两阶段不变)
	t.Run("六个编号精确位置", Test编号正式能力必须精确到实际调用)
	t.Run("编号能力数量与其他入口", Test编号能力拒绝新调用方和其他随机入口)
	t.Run("值和指针公开方法", Test身份审计公共能力固定)
	t.Run("额外指针方法拒绝", Test公共契约拒绝仅指针公开方法)
}

func Test任务终态规则接入真实全模块扫描(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		name = filepath.Join(root, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(name, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("go.mod", "module "+modulePath+"\n\ngo 1.27.1\n")
	write("internal/domain/model/task.go", `package model;type TaskState string;type Task struct{State TaskState}`)
	write("internal/interfaces/console/base.go", "package console")
	write("cmd/lober/base.go", "package main;func main(){}")
	write("internal/application/tasks/base.go", "package tasks")
	check := func() []violation {
		t.Helper()
		pkgs := packagesUnder(t, root, modulePath)
		dirs := moduleSourceDirs(t, filepath.Join(root, "go.mod"), root)
		got, _ := checkModule(os.DirFS(root), dirs, pkgs)
		return append(got, checkTypedTransitions(root, pkgs)...)
	}
	if got := check(); len(got) != 0 {
		t.Fatal("合法基线失败", got)
	}
	write("internal/interfaces/console/extra.go", `package console;import("context";"`+pkgModel+`");type leaked interface{GetTask(context.Context,string)(model.Task,error)};func bypass(ctx context.Context,r leaked){call:=r.GetTask;_,_=call(ctx,"task-id")}`)
	write("cmd/lober/extra.go", `package main;import m "`+pkgModel+`";type copied m.Task;func helper(t *copied){t.State="failed"}`)
	write("internal/application/tasks/extra.go", `package tasks;import "os";var read=os.ReadFile`)
	write("internal/interfaces/console/policy.go", `package console;import m "`+pkgModel+`";func stateText(t m.Task)string{return string(t.State)};func byHelper(t m.Task)bool{return stateText(t)=="done"};func byContainer(t m.Task)bool{values:=[]string{string(t.State)};return values[0]=="done"}`)
	write("internal/application/ports/audit.go", `package ports;type AuditIntent struct{}`)
	write("internal/interfaces/console/control.go", `package console;import("context";m "`+pkgModel+`";p "`+pkgPorts+`");type control interface{Cancel(context.Context,*m.Task,p.AuditIntent)error};func byControl(ctx context.Context,c control,t *m.Task,a p.AuditIntent)error{return c.Cancel(ctx,t,a)}`)
	write("internal/application/tasks/readfile.go", `package tasks;import "io/fs";func readTaskFile(files fs.FS)([]byte,error){return fs.ReadFile(files,"tasks.json")}`)
	write("internal/application/tasks/port/readfile.go", `package port;import "io/fs";func readTaskFile(files fs.FS)([]byte,error){return fs.ReadFile(files,"tasks.json")}`)
	got := check()
	for _, v := range got {
		if v.rule == ruleParse {
			t.Fatal("违规源码必须先成功装载", v)
		}
	}
	for _, file := range []string{"internal/interfaces/console/extra.go", "cmd/lober/extra.go", "internal/application/tasks/extra.go", "internal/interfaces/console/policy.go", "internal/interfaces/console/control.go", "internal/application/tasks/readfile.go", "internal/application/tasks/port/readfile.go"} {
		found := false
		for _, v := range got {
			found = found || strings.Contains(v.pos, file)
		}
		if !found {
			t.Error("真实扫描漏掉新增文件", file, got)
		}
	}
}

func Test机器密钥精确包接入完整检查(t *testing.T) {
	pkg := modulePath + "/internal/infrastructure/machinekeys"
	dir := "internal/infrastructure/machinekeys"
	for _, imp := range []string{"crypto/rand", "errors", "io", "os", "path/filepath", pkgStore, pkgConsole, pkgExecution, pkgPorts, "sync", "time", "net/http"} {
		got, counts := checkModule(fstest.MapFS{dir + "/key.go": {Data: []byte(importing(imp))}}, nil, []goPackage{{ImportPath: pkg, GoFiles: []string{"key.go"}}})
		allowed := imp == "crypto/rand" || imp == "errors" || imp == "io" || imp == "os" || imp == "path/filepath"
		if len(counts) != 1 || (len(got) == 0) != allowed {
			t.Fatal("密钥精确包未按完整规则检查", imp, got, counts)
		}
	}
}
func Test机器组装仅构造转换与委托(t *testing.T) {
	root := filepath.Dir(goModPath(t))
	b, err := os.ReadFile(filepath.Join(root, "cmd/lober/machine.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"render.Render", "machineSigningKey", ".Token(", ".ObserveIP(", ".ReachedReboot", ".IssuedAt", ".Execution.Firmware"} {
		if strings.Contains(string(b), forbidden) {
			t.Error("组装仍拥有业务实现", forbidden)
		}
	}
}
func Test执行观察只开放精确能力包(t *testing.T) {
	for _, pkg := range []string{pkgExecution, pkgExecution + "/port", pkgEvents, pkgMachine} {
		for _, imp := range []string{pkgExecution + "/port", pkgExecution + "/port/hidden", pkgExecution + "/hidden", pkgStore, pkgModel, "os"} {
			fset := token.NewFileSet()
			f, e := parser.ParseFile(fset, "probe.go", importing(imp), 0)
			if e != nil {
				t.Fatal(e)
			}
			got := checkMachineFile(fset, pkg, f)
			allowed := imp == pkgModel || pkg != pkgExecution+"/port" && (imp == "os" || imp == pkgStore) || imp == pkgExecution+"/port" && (pkg == pkgExecution || pkg == pkgEvents)
			if (len(got) == 0) != allowed {
				t.Fatal(pkg, imp, got)
			}
		}
	}
}

// 类型来源判断能识别别名、方法值和同包自建接口，文件名或局部变量名不提供例外。
func machineAssemblyFindings(caller string, fset *token.FileSet, files []*ast.File, info *types.Info) []violation {
	if caller != pkgAssembly {
		return nil
	}
	var found []violation
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := info.Uses[id]
			if obj == nil {
				obj = info.Defs[id]
			}
			if obj == nil || obj.Pkg() == nil {
				return true
			}
			target, name := obj.Pkg().Path(), obj.Name()
			bad := target == pkgRender
			if fn, ok := obj.(*types.Func); ok {
				bad = bad || target == pkgExecution && name == "Token" || fn.Name() == "ObserveIP" || fn.Name() == "ReadESXiBootConfig"
			}
			if field, ok := obj.(*types.Var); ok && field.IsField() && target == pkgModel {
				switch name {
				case "Attempt", "IssuedAt", "ReachedReboot", "Firmware":
					bad = true
				}
			}
			if bad {
				found = append(found, violation{rule: rule("机器组装只构造、普通字段转换和委托，不渲染、推算观察策略或生成凭据"), pkg: caller, subject: target + "." + name, pos: position(fset, id.Pos())})
			}
			return true
		})
	}
	return found
}
func Test机器组装不能借别名或小接口重做业务(t *testing.T) {
	imports := identityFixtureImports(t, pkgRender, pkgEvents, pkgMachine, modulePath+"/internal/application/authoring/port", modulePath+"/internal/application/authoring", pkgMachineKeys)
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"渲染方法别名", `import r "` + pkgRender + `";var render=r.Render`, false},
		{"令牌方法值", `import e "` + pkgExecution + `";func f(s *e.Service){_=s.Token}`, false},
		{"观察方法表达式", `import e "` + pkgEvents + `";var observe=(*e.Collector).ObserveIP`, false},
		{"自建观察接口", `type local interface{ObserveIP(string,int,string,int64)};func f(s local){_=s.ObserveIP}`, false},
		{"原配置能力方法", `import p "` + modulePath + `/internal/application/authoring/port";func f(s p.BootConfig){_=s.ReadESXiBootConfig}`, false},
		{"观察有效期", `import m "` + pkgModel + `";func f(t m.Task)int64{return t.Execution.IssuedAt+5400}`, false},
		{"重启分支", `import m "` + pkgModel + `";func f(t m.Task)bool{return t.Execution.ReachedReboot}`, false},
		{"固件判断", `import m "` + pkgModel + `";func f(t m.Task)string{return string(t.Execution.Firmware)}`, false},
		{"按编号观察", `import m "` + pkgModel + `";func f(t m.Task)int{return t.Execution.Attempt}`, false},
		{"凭据委托", `import(e "` + pkgExecution + `";m "` + pkgModel + `");func f(s *e.Service,t m.Task)(string,string){return s.Credentials(t)}`, true},
		{"普通请求转换", `import(a "` + modulePath + `/internal/application/authoring";m "` + pkgMachine + `");func f(r m.Request)a.MachineRenderRequest{return a.MachineRenderRequest{Task:r.Task,Kind:r.Kind,Slot:r.Slot,Values:r.Values}}`, true},
		{"密钥入口", `import k "` + pkgMachineKeys + `";func f(){_=k.LoadOrCreate}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgAssembly, "machine_extra.go", tc.body)
			if (len(got) == 0) != tc.allowed {
				t.Fatal(tc.allowed, got)
			}
		})
	}
}

func Test任务计时日志只允许原样委托(t *testing.T) {
	imports := identityFixtureImports(t, "log/slog")
	prefix := `import("context";"time";r "` + pkgStore + `";m "` + pkgModel + `");type loggingStore struct{r.Store};func(*loggingStore)log(string,time.Time,error){};`
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"原方法参数和结果原样转交", `func(l *loggingStore)CreateCurrentTasks(ctx context.Context,t []m.Task,a r.Audit)([]m.Task,error){start:=time.Now();got,err:=l.Store.CreateCurrentTasks(ctx,t,a);l.log("task.create",start,err);return got,err}`, true},
		{"换文件不能借用", `func(l *loggingStore)CreateCurrentTasks(ctx context.Context,t []m.Task,a r.Audit)([]m.Task,error){start:=time.Now();got,err:=l.Store.CreateCurrentTasks(ctx,t,a);l.log("task.create",start,err);return got,err}`, false},
		{"同名伪仓储类型不能借用", `func(l *loggingStore)CreateCurrentTasks(ctx context.Context,t []m.Task,a r.Audit)([]m.Task,error){start:=time.Now();got,err:=l.Store.CreateCurrentTasks(ctx,t,a);l.log("task.create",start,err);return got,err}`, false},
		{"改写转交参数", `func(l *loggingStore)CreateCurrentTasks(ctx context.Context,t []m.Task,a r.Audit)([]m.Task,error){start:=time.Now();got,err:=l.Store.CreateCurrentTasks(ctx,nil,a);l.log("task.create",start,err);return got,err}`, false},
		{"改写返回结果", `func(l *loggingStore)CreateCurrentTasks(ctx context.Context,t []m.Task,a r.Audit)([]m.Task,error){start:=time.Now();got,err:=l.Store.CreateCurrentTasks(ctx,t,a);l.log("task.create",start,err);return got,nil}`, false},
		{"辅助函数新增写入", `func helper(s r.Store,ctx context.Context,t []m.Task,a r.Audit){_,_=s.CreateCurrentTasks(ctx,t,a)}`, false},
		{"改名方法不能借用", `func(l *loggingStore)hidden(ctx context.Context,t []m.Task,a r.Audit)([]m.Task,error){start:=time.Now();got,err:=l.Store.CreateCurrentTasks(ctx,t,a);l.log("task.create",start,err);return got,err}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, source := "logging.go", prefix
			if tc.name == "换文件不能借用" {
				file = "logging_extra.go"
			}
			if tc.name == "同名伪仓储类型不能借用" {
				source = `import("context";"time";r "` + pkgStore + `";m "` + pkgModel + `");type Store interface{CreateCurrentTasks(context.Context,[]m.Task,r.Audit)([]m.Task,error)};type loggingStore struct{Store};func(*loggingStore)log(string,time.Time,error){};`
			}
			got := identityTypedFixture(t, imports, pkgAssembly, file, source+tc.body)
			if (len(got) == 0) != tc.allowed {
				t.Fatal(tc.allowed, got)
			}
		})
	}
}
