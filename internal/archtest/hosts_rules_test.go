package archtest

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"
)

// 每例先检查类型，再走总检查；旧业务只放在非根文件，避免污染允许例。
func hostsTransitionFixture(t *testing.T, file, body string) []violation {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for _, src := range []struct{ name, text string }{
		{"server.go", `package console; import(repo "` + pkgStore + `"; h "` + modulePath + `/internal/application/hosts"; p "` + pkgPorts + `"; tp "` + pkgTasksPort + `"); type localReader interface{GetHost()}; type deps struct{Users repo.Users; Registry h.Registry; Other h.Other; Local localReader; Notifier p.Notifier; Diagnostics tp.TaskDiagnostics}; type server struct{deps deps}`},
		{"auth.go", `package console; func(s *server) login(){s.deps.Users.GetUser()}`},
		{"legacy.go", `package console; func(s *server) bridge(){s.login()}; func(s *server) bridge2(){s.bridge()}; func oldResponse(){(&server{}).login()}; var alias=oldResponse`},
		{"errors.go", `package console; func RespondError(){(&server{}).login()}`},
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
		pkgStore: `package repository; type Users interface{GetUser()}`,
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
			{"直接仓储", `s.deps.Users.GetUser()`, false},
			{"局部别名", `u:=s.deps.Users;u.GetUser()`, false},
			{"方法值", `f:=s.deps.Users.GetUser;f()`, false},
			{"旧辅助方法值", `f:=s.login;f()`, false},
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

// 用起点逐项内容的摘要核对，补回精确删除项后必须等于原清单。
// 这样既固定真实变量，也能拒绝在其他阶段偷偷增删或改名。
func Test主机许可已清零(t *testing.T) {
	total := 0
	for _, tc := range []struct {
		name     string
		entries  []migrationException
		count    int
		baseline string
		removed  []migrationException
	}{
		{"migrationExceptions", migrationExceptions, 64, "918b4747e6458b9d190ca2403744f80ba8a8fd564df1b174b3ee4fb2837be4b4", []migrationException{
			{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).createHost", pkgStore, "Hosts.CreateHost", "3"},
			{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).deleteHost", pkgStore, "Hosts.DeleteHost", "3"},
			{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).getHost", pkgStore, "Hosts.GetHost", "3"},
			{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).importHosts", pkgStore, "Hosts.GetHosts", "3"},
			{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).importHosts", pkgStore, "Hosts.ImportHosts", "3"},
			{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).listHosts", pkgStore, "HostQuery", "3"},
			{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).listHosts", pkgStore, "Hosts.ListHosts", "3"},
			{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).listHosts", pkgStore, "Page", "3"},
			{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).updateHost", pkgStore, "Hosts.UpdateHost", "3"},
			{pkgConsole, "internal/interfaces/console/server.go", "package-init", pkgStore, "Hosts", "3"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*exactChunkReader).Read", pkgStore, "ErrInvalid", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).createISO", pkgStore, "CheckNewISO", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).createISO", pkgStore, "ISOs.CreateISO", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).deprecateISO", pkgStore, "ISOs.DeprecateISO", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).getISO", pkgStore, "ISOs.GetISO", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).isoProgress", pkgConsole, "ISOSourceState.SourcesOf", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).isoProgress", pkgStore, "ISOs.GetISO", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).listISOs", pkgStore, "ISOQuery", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).listISOs", pkgStore, "ISOs.ListISOs", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).listISOs", pkgStore, "Page", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).registerISOSource", pkgStore, "ISOs.GetISO", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).sourceOperation", pkgStore, "Audit", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).sourceProgress", pkgStore, "ErrConflict", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).sourceProgress", pkgStore, "ErrNotFound", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).sourceProgress", pkgConsole, "ISOSourceState.GetSource", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).sourceProgress", pkgConsole, "ISOSourceState.RangesOf", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).uploadChunk", pkgStore, "CheckSourceKey", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).uploadChunk", pkgStore, "ErrConflict", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).uploadChunk", pkgStore, "ErrNotFound", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).uploadChunk", pkgConsole, "ISOSourceState.GetSource", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "checkISOSource", pkgStore, "CheckSource", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "checkISOSource", pkgStore, "ErrInvalid", "4"},
			{pkgConsole, "internal/interfaces/console/isosource.go", "package-init", pkgStore, "Audit", "4"},
			{pkgConsole, "internal/interfaces/console/server.go", "package-init", pkgStore, "ISOs", "4"},
		}},
		{"mediaMigrationExceptions", mediaMigrationExceptions, 51, "4e93eaf8a62876bfa694042b348843f41709e729f45b4354708802cfe5df19db", []migrationException{
			{pkgISO, "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{pkgISO, "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{pkgISO, "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/local.go", "(*Sources).fetchLocal", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/local.go", "(*Sources).fetchLocal", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/recover.go", "allFetched", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
			{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOQuery", "4"},
			{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOSources.ListUnfinishedSources", "4"},
			{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOs.ListISOs", "4"},
			{pkgISO, "internal/infrastructure/media/remote.go", "parseContentRange", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{pkgISO, "internal/infrastructure/media/remote.go", "rangeNumber", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{pkgISO, "internal/infrastructure/media/remote.go", "(*Sources).fetchRemote", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/remote.go", "(*Sources).fetchRemote", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/remote.go", "(*Sources).fetchRemote", "github.com/lober/lober/internal/domain/repository", "ProblemsError", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "checkSourceKey", "github.com/lober/lober/internal/domain/repository", "CheckSourceKey", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).resumeOffset", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).resumeOffset", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).resumeOffset", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).restorePublication", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ErrRejected", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ProblemsError", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).failFetch", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).failFetch", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).writeFetch", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).writeFetch", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Register", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RegisterQueued", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "CheckSource", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOSources.GetSource", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSourceAndAcquire", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).retrySource", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).retrySource", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).retrySource", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Reimport", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSourceAndAcquire", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Progress", "github.com/lober/lober/internal/domain/repository", "ISOSources.RangesOf", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Progress", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Progress", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{pkgISO, "internal/infrastructure/media/upload.go", "compareRange", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ISOSources.RangesOf", "4"},
			{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveRange", "4"},
			{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
		}},
		{"consoleRuleMigrationExceptions", consoleRuleMigrationExceptions, 42, "e0e7825d6c0d7450d4a9fb8b9bfe883e90f8fa43107494b48260afe800631708", []migrationException{
			{pkgConsole, "internal/interfaces/console/csv.go", "parseHostCSV", "github.com/lober/lober/internal/domain/model", "Host.Validate", "3"},
			{pkgConsole, "internal/interfaces/console/csv.go", "parseHostCSV", "github.com/lober/lober/internal/domain/model", "HostReservedAttrs", "3"},
			{pkgConsole, "internal/interfaces/console/csv.go", "parseHostCSV", "github.com/lober/lober/internal/domain/model", "NewProblem", "3"},
			{pkgConsole, "internal/interfaces/console/csv.go", "parseHostCSV", "github.com/lober/lober/internal/domain/model", "NormalizeSN", "3"},
			{pkgConsole, "internal/interfaces/console/csv.go", "parseHostCSV", "github.com/lober/lober/internal/domain/model", "ValidAttrKey", "3"},
			{pkgConsole, "internal/interfaces/console/hosts.go", "normalizedHostSN", "github.com/lober/lober/internal/domain/model", "NormalizeSN", "3"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).listISOs", "github.com/lober/lober/internal/domain/catalog", "Family.Valid", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).listISOs", "github.com/lober/lober/internal/domain/catalog", "Series.Valid", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).listISOs", "github.com/lober/lober/internal/domain/model", "ISOState.Valid", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "checkISOSource", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
			{pkgConsole, "internal/interfaces/console/isos.go", "(*server).checkSourceSpace", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
		}},
		{"notificationMigrationExceptions", notificationMigrationExceptions, 1, "ba63e2a81c31ee5bf62e0288cdf68e5845af7009edfd247e6b18a119189aeaf6", []migrationException{}},
	} {
		if len(tc.entries) != tc.count {
			t.Errorf("%s=%d want %d", tc.name, len(tc.entries), tc.count)
		}
		total += len(tc.entries)
		expectedRemoved := 0
		if tc.name == "mediaMigrationExceptions" {
			expectedRemoved = 74
		}
		if tc.name == "migrationExceptions" {
			expectedRemoved = 34
		}
		if tc.name == "consoleRuleMigrationExceptions" {
			expectedRemoved = 11
		}
		if len(tc.removed) != expectedRemoved {
			t.Fatalf("%s removed=%d", tc.name, len(tc.removed))
		}
		var lines []string
		for _, e := range tc.entries {
			if e.RemoveIn == "1" || e.RemoveIn == "2" || e.RemoveIn == "3" || e.File == "internal/interfaces/console/wizard.go" {
				t.Errorf("old permit: %+v", e)
			}
			lines = append(lines, strings.Join([]string{e.Caller, e.File, e.Function, e.Target, e.Symbol, e.RemoveIn}, "\t")+"\n")
		}
		for _, e := range tc.removed {
			if !(tc.name == "mediaMigrationExceptions" && e.RemoveIn == "4" && e.Caller == pkgISO || e.RemoveIn == "3" && (e.File == "internal/interfaces/console/hosts.go" || e.File == "internal/interfaces/console/csv.go" || e.File == "internal/interfaces/console/server.go") || e.RemoveIn == "4" && (e.File == "internal/interfaces/console/isos.go" || e.File == "internal/interfaces/console/isosource.go" || e.File == "internal/interfaces/console/server.go")) {
				t.Fatalf("unexpected removal: %+v", e)
			}
			lines = append(lines, strings.Join([]string{e.Caller, e.File, e.Function, e.Target, e.Symbol, e.RemoveIn}, "\t")+"\n")
		}
		slices.Sort(lines)
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(lines, "")))); got != tc.baseline {
			t.Errorf("%s changed beyond the exact phase-three removals: %s", tc.name, got)
		}
	}
	if total != 158 {
		t.Fatalf("total=%d want 158", total)
	}
}
