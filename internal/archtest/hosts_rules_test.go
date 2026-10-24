package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// 每例先检查类型，再走总检查；旧业务只放在非根文件，避免污染允许例。
func hostsTransitionFixture(t *testing.T, file, body string) []violation {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for _, src := range []struct{ name, text string }{
		{"server.go", `package console; import(repo "` + pkgStore + `"; id "` + modulePath + `/internal/application/identity"; h "` + modulePath + `/internal/application/hosts"; p "` + pkgPorts + `"; tp "` + pkgTasksPort + `"); type localReader interface{GetHost()}; type deps struct{LegacyTasks repo.Tasks; Registry h.Registry; Other h.Other; Local localReader; Notifier p.Notifier; Diagnostics tp.TaskDiagnostics}; type server struct{deps deps;principal id.Principal}`},
		{"tasks.go", `package console; func(s *server) controlTask(){s.deps.LegacyTasks.GetTask()}`},
		{"legacy.go", `package console; func(s *server) bridge(){s.controlTask()}; func(s *server) bridge2(){s.bridge()}; func oldResponse(){(&server{}).controlTask()}; var alias=oldResponse`},
		{"errors.go", `package console; func RespondError(){(&server{}).controlTask()}`},
		{file, `package console; import m "` + pkgModel + `"; type HostResponse struct{SN string}; type CsvPlan struct{Total int}; func toHostResponse(h m.Host)HostResponse{return HostResponse{h.SN}}; func toCsvPlan(n int)CsvPlan{return CsvPlan{n}}; func(s *server) hostHandler(){` + body + `}; var _=m.Host{}`},
	} {
		f, err := parser.ParseFile(fset, "internal/interfaces/console/"+src.name, src.text, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	imports := transitionFixtureImporter{
		modulePath + "/internal/application/identity": `package identity;type Principal struct{};func(Principal)Actor(){};func(Principal)Other(){}`,
		pkgStore: `package repository; type Tasks interface{GetTask()}`,
		modulePath + "/internal/application/hosts": `package hosts; type Registry interface{List();Get();Create();Update();Delete();Import();Extra()}; type Other interface{Get()}`,
		pkgPorts:     `package ports; type Notifier interface{Publish()}`,
		pkgTasksPort: `package port; type TaskDiagnostics interface{CanCancel()}`,
		pkgModel:     `package model; type Host struct{SN string}; func(Host)Validate(){}; func NormalizeSN(string){}`,
	}
	if _, err := (&types.Config{Importer: imports}).Check(pkgConsole, fset, files, info); err != nil {
		t.Fatal("反例类型错误", err)
	}
	return typedTransitionFindings("", pkgConsole, fset, files, info)
}

func Test主机入口不能绕回旧业务(t *testing.T) {
	for _, file := range []string{"hosts.go", "csv.go"} {
		for _, tc := range []struct {
			name, body string
			allowed    bool
		}{
			{"认证快照转操作者", `s.principal.Actor()`, true},
			{"身份其他方法不可借用", `s.principal.Other()`, false},
			{"直接仓储", `s.deps.LegacyTasks.GetTask()`, false},
			{"局部别名", `u:=s.deps.LegacyTasks;u.GetTask()`, false},
			{"方法值", `f:=s.deps.LegacyTasks.GetTask;f()`, false},
			{"旧辅助方法值", `f:=s.controlTask;f()`, false},
			{"包级别名", `alias()`, false},
			{"两层旧辅助", `s.bridge2()`, false},
			{"旧响应转发", `oldResponse()`, false},
			{"局部小接口", `s.deps.Local.GetHost()`, false},
			{"主机校验", `m.Host{}.Validate()`, false},
			{"编号规范化", `m.NormalizeSN("sn")`, false},
			{"错误转换夹带业务", `RespondError()`, false},
			{"通知", `s.deps.Notifier.Publish()`, false},
			{"任务诊断", `s.deps.Diagnostics.CanCancel()`, false},
			{"相似应用接口", `s.deps.Other.Get()`, false},
			{"未登记Registry方法", `s.deps.Registry.Extra()`, false},
			{"纯HostResponse转换", `_=toHostResponse(m.Host{SN:"SN-1"})`, true},
			{"纯CsvPlan转换", `_=toCsvPlan(2)`, true},
			{"Registry六方法", `s.deps.Registry.List();s.deps.Registry.Get();s.deps.Registry.Create();s.deps.Registry.Update();s.deps.Registry.Delete();s.deps.Registry.Import()`, true},
		} {
			t.Run(file+"/"+tc.name, func(t *testing.T) {
				found := hostsTransitionFixture(t, file, tc.body)
				if (len(found) == 0) != tc.allowed {
					t.Fatalf("allowed=%v findings=%v", tc.allowed, found)
				}
			})
		}
	}
}

// 第三阶段和此前阶段持续清零；本轮逐项内容核对由镜像迁移测试承接。
func Test主机许可已清零(t *testing.T) {
	for _, entries := range [][]migrationException{migrationExceptions, mediaMigrationExceptions, consoleRuleMigrationExceptions, notificationMigrationExceptions} {
		for _, e := range entries {
			if e.RemoveIn == "1" || e.RemoveIn == "2" || e.RemoveIn == "3" || e.File == "internal/interfaces/console/wizard.go" {
				t.Errorf("旧许可复活: %+v", e)
			}
		}
	}
}
