package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func wizardTransitionFixture(t *testing.T, body string) []violation {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for _, src := range []struct{ name, text string }{
		{"server.go", `package console; import (repo "` + pkgStore + `"; tp "` + pkgTasksPort + `"; p "` + pkgPorts + `"); type deps struct{LegacyTasks repo.Tasks; TaskCtl TaskControl; Tasks tp.Tasks; Notifier p.Notifier}; type server struct{deps deps}; type TaskControl interface{CanCancel()}`},
		{"tasks.go", `package console; func(s *server) getTask(){s.deps.LegacyTasks.GetTask()}`},
		{"legacy_templates.go", `package console; func toTemplateResponse(){(&server{}).templateHooks()}; func(s *server) templateHooks(){s.getTask()}`},
		{"tasks.go", `package console; func(s *server) taskResponse(){s.deps.TaskCtl.CanCancel()}`},
		{"bridge.go", `package console; func(s *server) bridge(){s.getTask()}; var alias=toTemplateResponse`},
		{"wizard.go", `package console; import p "` + layer1 + `/params"; type Clock func()int64; func(s *server) wizardHosts(){` + body + `}; var _=p.FieldKey("")`},
	} {
		f, e := parser.ParseFile(fset, "internal/interfaces/console/"+src.name, src.text, 0)
		if e != nil {
			t.Fatal(e)
		}
		files = append(files, f)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	imports := transitionFixtureImporter{pkgStore: `package repository; type Tasks interface{GetTask()}`, layer1 + "/params": `package params; type FieldKey string`, pkgTasksPort: `package port; type Tasks interface{CreateCurrentTasks()}`, pkgPorts: `package ports; type Notifier interface{Publish()}`}
	if _, e := (&types.Config{Importer: imports}).Check(pkgConsole, fset, files, info); e != nil {
		t.Fatal("夹具类型错误", e)
	}
	return typedTransitionFindings("", pkgConsole, fset, files, info)
}
func Test向导不能经同包辅助函数绕回旧业务(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"直接仓储", `s.deps.LegacyTasks.GetTask()`, false},
		{"局部仓储别名", `u:=s.deps.LegacyTasks;u.GetTask()`, false},
		{"旧函数", `s.getTask()`, false},
		{"旧函数方法值", `f:=s.getTask;f()`, false},
		{"两层函数转发", `s.bridge()`, false},
		{"模板响应旧规则", `toTemplateResponse()`, false},
		{"模板引用旧辅助函数", `s.templateHooks()`, false},
		{"全局函数值", `alias()`, false},
		{"任务响应旧诊断", `s.taskResponse()`, false},
		{"直接旧诊断", `s.deps.TaskCtl.CanCancel()`, false},
		{"不能改用小接口直写", `s.deps.Tasks.CreateCurrentTasks()`, false},
		{"不能在网页重新通知", `s.deps.Notifier.Publish()`, false},
		{"纯类型转换", `_ = p.FieldKey("x"); _ = Clock(func()int64{return 1})`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found := wizardTransitionFixture(t, tc.body)
			if (len(found) == 0) != tc.allowed {
				t.Fatal(found)
			}
		})
	}
}
