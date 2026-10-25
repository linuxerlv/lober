package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 每个反例先通过类型检查，不能拿无效源码冒充守卫有效。
func authoringTransitionFixture(t *testing.T, file, body string) []violation {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	renderImport := ""
	if strings.Contains(body, "r.Render") {
		renderImport = `; r "` + pkgRender + `"`
	}
	legacy := ""
	if strings.Contains(body, "bridge2") {
		legacy += `func(s *server)bridge2(){s.bridge()};`
	}
	if strings.Contains(body, "bridge") {
		legacy += `func(s *server)bridge(){s.taskStream()};`
	}
	if strings.Contains(body, "alias") || strings.Contains(body, "oldResponse") {
		legacy += `func oldResponse(){(&server{}).taskStream()};var alias=oldResponse;`
	}
	if strings.Contains(body, "RespondError") {
		legacy += `func RespondError(){(&server{}).taskStream()};`
	}
	if legacy != "" || strings.Contains(body, "taskStream") {
		legacy += `func(s *server)taskStream(){s.deps.LegacyTasks.GetTask()};`
	}
	local := `type localReader struct{}`
	if strings.Contains(body, "s.deps.Local") {
		local = `type localReader interface{GetHook()}`
	}
	for _, src := range []struct{ name, text string }{
		{"server.go", `package console; import(repo "` + pkgStore + `"; ap "` + modulePath + `/internal/application/authoring/port"; a "` + modulePath + `/internal/application/authoring"; p "` + pkgPorts + `"; tp "` + pkgTasksPort + `"); ` + local + `; type deps struct{LegacyTasks repo.Tasks; Hooks ap.HookReader; Authoring a.Hooks; Templates a.Templates; Editor a.Editor; Frozen p.FrozenPreview; Notifier p.Notifier; Diagnostics tp.TaskDiagnostics; Other a.Other; Local localReader}; type server struct{deps deps}`},
		{"tasks.go", `package console;` + legacy},
		{"legacy.go", `package console;func pure(v int)int{return v}`},

		{file, `package console; import (p "` + layer1 + `/params"` + renderImport + `); type Clock func()int64; func(s *server) authoringHandler(){` + body + `}; var _=p.FieldKey("")`},
	} {
		f, err := parser.ParseFile(fset, "internal/interfaces/console/"+src.name, src.text, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	imports := transitionFixtureImporter{
		pkgStore: `package repository; type Tasks interface{GetTask()}`,
		modulePath + "/internal/application/authoring/port": `package port; type HookReader interface{GetHook()}`,
		modulePath + "/internal/application/authoring":      `package authoring; type Hooks interface{Get(); EnsureBuiltinHook()}; type Templates interface{Derive()}; type Editor interface{Preview()}; type Other interface{Get()}`,
		pkgPorts:           `package ports; type FrozenPreview interface{RenderFrozen(); Extra()}; type Notifier interface{Publish()}`,
		pkgTasksPort:       `package port; type TaskDiagnostics interface{CanCancel()}`,
		pkgModel:           `package model; type Role string; func(Role) Valid()bool{return true}`,
		pkgRender:          `package render; type Output struct{}; func Render(){}`,
		layer1 + "/params": `package params; type FieldKey string`,
	}
	if _, err := (&types.Config{Importer: imports}).Check(pkgConsole, fset, files, info); err != nil {
		t.Fatal("反例类型错误", err)
	}
	// 本例只注入所需声明；检查器仍扫描这里全部函数、变量和类型。
	return wizardHelperFindings(fset, files, info)
}
func Test编写预览不能绕回旧业务(t *testing.T) {
	for _, file := range []string{"hooks.go", "templates.go", "lifecycle.go", "authoring_helpers.go", "slots.go", "preview.go"} {
		for _, tc := range []struct {
			name, body string
			allowed    bool
		}{
			{"直接仓储", `s.deps.LegacyTasks.GetTask()`, false},
			{"局部别名", `u:=s.deps.LegacyTasks;u.GetTask()`, false},
			{"仓储方法值", `f:=s.deps.LegacyTasks.GetTask;f()`, false},
			{"旧辅助方法值", `f:=s.taskStream;f()`, false},
			{"两层同包转发", `s.bridge2()`, false},
			{"包级别名", `alias()`, false},
			{"旧响应计算", `oldResponse()`, false},
			{"小接口仓储", `s.deps.Hooks.GetHook()`, false},
			{"本地小接口仓储", `s.deps.Local.GetHook()`, false},
			{"领域渲染", `r.Render()`, false},
			{"错误转换夹带业务", `RespondError()`, false},
			{"禁止通知", `s.deps.Notifier.Publish()`, false},
			{"禁止诊断", `s.deps.Diagnostics.CanCancel()`, false},
			{"不放行相似应用接口", `s.deps.Other.Get()`, false},
			{"不放行未登记应用方法", `s.deps.Authoring.EnsureBuiltinHook()`, false},
			{"不放行新增预览方法", `s.deps.Frozen.Extra()`, false},
			{"纯转换", `_=pure(2);_=p.FieldKey("x");_=Clock(func()int64{return 1})`, true},
			{"组件入口", `s.deps.Authoring.Get()`, true},
			{"模板入口", `s.deps.Templates.Derive()`, true},
			{"编辑入口", `s.deps.Editor.Preview()`, true},
			{"冻结预览直调已删除", `s.deps.Frozen.RenderFrozen()`, false},
		} {
			t.Run(file+"/"+tc.name, func(t *testing.T) {
				found := authoringTransitionFixture(t, file, tc.body)
				if (len(found) == 0) != tc.allowed {
					t.Fatal(found)
				}
			})
		}
	}
}
func Test第二阶段与向导许可已清零(t *testing.T) {
	total := 0
	for _, entries := range [][]migrationException{phaseFourBaselineEntries("migrationExceptions", migrationExceptions), phaseFourBaselineEntries("mediaMigrationExceptions", mediaMigrationExceptions), phaseFourBaselineEntries("consoleRuleMigrationExceptions", consoleRuleMigrationExceptions), phaseFourBaselineEntries("notificationMigrationExceptions", notificationMigrationExceptions)} {
		total += len(entries)
		for _, e := range entries {
			if e.RemoveIn == "2" || e.File == "internal/interfaces/console/wizard.go" {
				t.Errorf("旧许可尚存: %+v", e)
			}
		}
	}
	if total != 107 {
		t.Fatalf("固定许可总数=%d，期望107", total)
	}
}

func Test启动组装只委托两个初始化入口(t *testing.T) {
	name := filepath.Join(filepath.Dir(goModPath(t)), "cmd/lober/app.go")
	source, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, decl := range f.Decls {
		if declarationName(decl) == "ensureBuiltin" {
			t.Error("组装点仍公开任意写入重试回调")
		}
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || declarationName(decl) != "(*app).Initialise" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				t.Error("启动组装仍拥有仓储循环")
			case *ast.CallExpr:
				method, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					t.Error("启动初始化存在额外函数调用")
					break
				}
				receiver, ok := method.X.(*ast.SelectorExpr)
				if !ok {
					t.Error("启动初始化必须委托注入入口")
					break
				}
				calls = append(calls, receiver.Sel.Name+"."+method.Sel.Name)
			}
			return true
		})
	}
	if strings.Join(calls, ",") != "Builtin.Initialise,Identity.Initialise" {
		t.Fatal(calls)
	}
}

func Test网页不能使用内置启动入口(t *testing.T) {
	imports := identityFixtureImports(t)
	a := `a "` + modulePath + `/internal/application/authoring"`
	for _, body := range []string{
		`import ` + a + `;func f(b a.Bootstrapper){_=b.Initialise(nil)}`,
		`import ` + a + `;func f(){_=a.NewBootstrapper}`,
	} {
		if got := identityTypedFixture(t, imports, pkgConsole, "hooks.go", body); len(got) == 0 {
			t.Fatal("网页获得了启动入口")
		}
	}
}
