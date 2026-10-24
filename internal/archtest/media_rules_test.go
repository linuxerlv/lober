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
	"testing/fstest"
)

const fixtureMedia = modulePath + "/internal/application/media"
const fixtureMediaPort = fixtureMedia + "/port"

// 所有正反例先通过 Go 类型检查，再调用生产源码所用的同一检查函数。
func mediaTypedFixture(t *testing.T, caller string, sources map[string]string) []violation {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for name, source := range sources {
		f, err := parser.ParseFile(fset, strings.TrimPrefix(caller, modulePath+"/")+"/"+name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	imports := transitionFixtureImporter{
		"time":              `package time;type Time struct{}`,
		"context":           `package context;import "time";type Context interface{Deadline()(time.Time,bool);Done()<-chan struct{};Err()error;Value(any)any}`,
		"io/fs":             `package fs;var ErrNotExist error;var SkipAll error;type FS interface{Open()};func ReadFile(){};func WalkDir(){}`,
		pkgStore:            `package repository; type ISOs interface{GetISO();UpdateISO()}; type ISOSources interface{SaveSource()}; type Tasks interface{GetTask()}; type Store interface{ISOs;ISOSources;Tasks}; func CheckID(){};func CheckSource(){};var ErrInvalid error`,
		fixtureMediaPort:    `package port; type ISOs interface{GetISO();UpdateISO()}; type Sources interface{SaveSource()};type SourceControl interface{Register();Upload()};type Deletion interface{Delete()};type Transfer interface{BeginFetch()};type FetchSession interface{Open();Close()};type ImportSession interface{OpenSource();Publish()};type DeletionJournal interface{Persist();List();Discard()};type Entry struct{Size int64};type Fault struct{};func(*Fault)Error()string{return "file"}`,
		fixtureMedia:        `package media;type Registry interface{List();Get();Create();RegisterSource();Progress();Upload();Cancel();Retry();Reimport();Deprecate();Delete();Extra()};type Sources struct{};func(*Sources)Recover(){};func(*Sources)Import(){};func NewSources()*Sources{return nil};type Supervisor struct{};func NewSupervisor()*Supervisor{return nil};func(*Supervisor)Start(){};func(*Supervisor)Close(){};func(*Supervisor)Wake(){}`,
		pkgModel:            `package model;type ISOState string;type SourceState string;type TaskState string;type ISO struct{State ISOState};func(ISO)Validate(){};type Source struct{State SourceState};type Task struct{State TaskState};const ISOReady ISOState="ready"`,
		layer1 + "/media":   `package media;type EditionFact struct{Name string};func CheckImport(){};func CheckDeletion(){}`,
		layer1 + "/catalog": `package catalog;type Arch string;func(Arch)Valid()bool{return true}`,
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	if _, err := (&types.Config{Importer: imports}).Check(caller, fset, files, info); err != nil {
		t.Fatal("夹具必须先通过类型检查", err)
	}
	return typedTransitionFindings("", caller, fset, files, info)
}

func Test镜像文件实现拒绝借能力接口查库(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"仓储类型", `import r "` + pkgStore + `";var _ r.Store`, false},
		{"自己的仓储接口", `import p "` + fixtureMediaPort + `";var _ p.ISOs`, false},
		{"类型别名", `import p "` + fixtureMediaPort + `";type hidden=p.ISOs;var _ hidden`, false},
		{"点导入", `import . "` + fixtureMediaPort + `";var _ Sources`, false},
		{"来源控制", `import p "` + fixtureMediaPort + `";var _ p.SourceControl`, false},
		{"删除流程", `import p "` + fixtureMediaPort + `";var _ p.Deletion`, false},
		{"方法值", `import p "` + fixtureMediaPort + `";func load(p p.ISOs){read:=p.GetISO;read()}`, false},
		{"包级方法值", `import p "` + fixtureMediaPort + `";var read=p.ISOs.GetISO`, false},
		{"同包转发", `import p "` + fixtureMediaPort + `";func helper(p p.ISOs){p.GetISO()};func load(p p.ISOs){helper(p)}`, false},
		{"本地小接口", `type reader interface{GetISO()};func load(p reader){f:=p.GetISO;f()}`, false},
		{"仅持有匿名仓储", `type holder struct{Store interface{GetISO()}}`, false},
		{"结构体状态赋值", `import m "` + pkgModel + `";func change(p *m.ISO){p.State=m.ISOReady}`, false},
		{"指针类型别名写状态", `import m "` + pkgModel + `";type pointer = *m.ISO;func change(p pointer){p.State="ready"}`, false},
		{"命名指针写来源状态", `import m "` + pkgModel + `";type pointer *m.Source;func change(p pointer){p.State="fetched"}`, false},
		{"括号左侧写状态", `import m "` + pkgModel + `";func change(p *m.ISO){(p.State)="ready"}`, false},
		{"括号取地址写状态", `import m "` + pkgModel + `";func change(p *m.ISO){s:=&(p.State);*s="ready"}`, false},
		{"复制结构类型写状态", `import m "` + pkgModel + `";type copied m.ISO;func change(p *copied){p.State="ready"}`, false},
		{"复制结构字面量写状态", `import m "` + pkgModel + `";type copied m.Source;var ready=copied{State:"fetched"}`, false},
		{"复制结构位置字面量写状态", `import m "` + pkgModel + `";type copied m.Source;var ready=copied{"fetched"}`, false},
		{"复合字面量状态", `import m "` + pkgModel + `";var ready=m.ISO{State:m.ISOReady}`, false},
		{"导入业务规则", `import m "` + layer1 + `/media";var rule=m.CheckImport`, false},
		{"文件会话事实", `import(p "` + fixtureMediaPort + `";m "` + layer1 + `/media");var _ p.Transfer;var _ p.FetchSession;var _ p.ImportSession;var _ p.DeletionJournal;var _=m.EditionFact{Name:"x"};func facts(e p.Entry)int64{return e.Size}`, true},
		{"读取状态", `import m "` + pkgModel + `";func read(p m.ISO)bool{return p.State==m.ISOReady}`, true},
		{"纯架构值检查", `import c "` + layer1 + `/catalog";func fact(a c.Arch)bool{return a.Valid()}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mediaTypedFixture(t, pkgISO, map[string]string{"extra.go": "package iso;" + tc.body})
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v findings=%v", tc.allowed, got)
			}
		})
	}
}

func Test镜像网页入口追踪同包旧业务(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"正常十一方法", `s.registry.List();s.registry.Get();s.registry.Create();s.registry.RegisterSource();s.registry.Progress();s.registry.Upload();s.registry.Cancel();s.registry.Retry();s.registry.Reimport();s.registry.Deprecate();s.registry.Delete()`, true},
		{"相同接口新增方法", `s.registry.Extra()`, false},
		{"同包两层方法值", `fn:=s.bridge;fn()`, false},
		{"包级函数值", `oldAlias(s)`, false},
		{"泛型辅助转发", `oldGeneric(s,42)`, false},
		{"局部小接口", `s.local.GetISO()`, false},
		{"旧Sources", `s.old.Recover()`, false},
		{"纯响应转换", `_=pure(42)`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mediaTypedFixture(t, pkgConsole, map[string]string{
				"server.go": `package console;import(r "` + pkgStore + `";m "` + fixtureMedia + `");type local interface{GetISO()};type server struct{tasks r.Tasks;registry m.Registry;local local;old *m.Sources}`,
				"tasks.go":  `package console;func(s *server)getTask(){s.tasks.GetTask()}`,
				"helper.go": `package console;func(s *server)bridge(){s.getTask()};func old(s *server){s.bridge()};var oldAlias=old;func pure(n int)int{return n};func oldGeneric[T any](s *server,v T){s.getTask()}`,
				"isos.go":   `package console;func(s *server)handler(){` + tc.body + `}`,
			})
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v findings=%v", tc.allowed, got)
			}
		})
	}
}

func Test镜像组装只构造启停不处理业务(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"构造启停", `import m "` + fixtureMedia + `";func setup(){_=m.NewSources();s:=m.NewSupervisor();s.Start();defer s.Close()}`, true},
		{"传递仓储", `import r "` + pkgStore + `";type deps struct{Store r.Store};func setup(s r.Store)deps{return deps{s}}`, true},
		{"查镜像方法值", `import r "` + pkgStore + `";func run(s r.Store){f:=s.GetISO;f()}`, false},
		{"本地接口查镜像", `type reader interface{GetISO()};func run(s reader){s.GetISO()}`, false},
		{"来源持久化", `import p "` + fixtureMediaPort + `";func run(s p.Sources){s.SaveSource()}`, false},
		{"恢复流程", `import m "` + fixtureMedia + `";func run(s *m.Sources){s.Recover()}`, false},
		{"状态更新", `import m "` + pkgModel + `";func run(s *m.ISO){s.State="ready"}`, false},
		{"指针别名判断状态", `import m "` + pkgModel + `";type pointer = *m.ISO;func run(s pointer)bool{return s.State=="ready"}`, false},
		{"复制结构判断状态", `import m "` + pkgModel + `";type copied m.Source;func run(s copied)bool{return s.State=="fetched"}`, false},
		{"后续任务状态保持原范围", `import m "` + pkgModel + `";func run(s *m.Task)bool{s.State="running";return s.State=="running"}`, true},
		{"状态判断", `import m "` + pkgModel + `";func run(s m.ISO)bool{return s.State=="ready"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mediaTypedFixture(t, pkgAssembly, map[string]string{"extra.go": "package main;" + tc.body})
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v findings=%v", tc.allowed, got)
			}
		})
	}
}

func Test镜像应用不能直接读文件或跨层(t *testing.T) {
	for _, imp := range []string{"os", "net/http", "os/exec", pkgISO, fixtureMedia + "/port/hidden", modulePath + "/internal/application/hosts"} {
		// 导入方向样例也先检查类型，导入器为每个依赖提供有效空包。
		source := withImport("_ \""+imp+"\"", "")
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = (&types.Config{Importer: transitionFixtureImporter{imp: "package dependency"}}).Check(fixtureMedia, fset, []*ast.File{f}, nil); err != nil {
			t.Fatal("导入样例类型错误", err)
		}
		got, _ := checkModule(fstest.MapFS{"internal/application/media/x.go": {Data: []byte(source)}}, nil, []goPackage{{ImportPath: fixtureMedia, GoFiles: []string{"x.go"}}})
		if len(got) == 0 {
			t.Errorf("镜像应用放过 %s", imp)
		}
	}
}

func Test镜像领域不能读取文件(t *testing.T) {
	source := `package media;import fs "io/fs";func read(f fs.FS){_,_=fs.ReadFile(f,"image")}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	imports := transitionFixtureImporter{"io/fs": `package fs;type FS interface{Open(string)(File,error)};type File interface{};func ReadFile(FS,string)([]byte,error){return nil,nil}`}
	caller := layer1 + "/media"
	if _, err = (&types.Config{Importer: imports}).Check(caller, fset, []*ast.File{f}, nil); err != nil {
		t.Fatal("领域样例类型错误", err)
	}
	got, _ := checkModule(fstest.MapFS{"internal/domain/media/x.go": {Data: []byte(source)}}, nil, []goPackage{{ImportPath: caller, GoFiles: []string{"x.go"}}})
	if len(got) == 0 {
		t.Fatal("领域放过文件读取")
	}
}

func Test镜像应用只接受两个文件错误值(t *testing.T) {
	for _, tc := range []struct {
		body    string
		allowed bool
	}{
		{`var missing=fs.ErrNotExist`, true},
		{`var stop=fs.SkipAll`, true},
		{`var read=fs.ReadFile`, false},
		{`var walk=fs.WalkDir`, false},
		{`var source fs.FS`, false},
	} {
		got := mediaTypedFixture(t, fixtureMedia, map[string]string{"extra.go": `package media;import fs "io/fs";` + tc.body})
		if (len(got) == 0) != tc.allowed {
			t.Fatalf("%s allowed=%v findings=%v", tc.body, tc.allowed, got)
		}
	}
}

func Test镜像新网页文件不能调用内部能力(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"自己的仓储接口", `import p "` + fixtureMediaPort + `";func f(s p.ISOs){s.GetISO()}`, false},
		{"内部来源控制", `import p "` + fixtureMediaPort + `";var _ p.SourceControl`, false},
		{"内部恢复流程", `import m "` + fixtureMedia + `";func f(s *m.Sources){s.Recover()}`, false},
		{"本地接口最小方法值", `type reader interface{GetISO()};func newMediaHandler(r reader){read:=r.GetISO;read()}`, false},
		{"本地接口最小直接调用", `type reader interface{GetISO()};func newMediaHandler(r reader){r.GetISO()}`, false},
		{"本地接口真实签名直接调用", `import("context";m "` + pkgModel + `");type reader interface{GetISO(context.Context,string)(m.ISO,error)};func newMediaHandler(ctx context.Context,r reader)(m.ISO,error){return r.GetISO(ctx,"image")}`, false},
		{"本地接口真实签名方法值", `import("context";m "` + pkgModel + `");type reader interface{GetISO(context.Context,string)(m.ISO,error)};func newMediaHandler(ctx context.Context,r reader)(m.ISO,error){read:=r.GetISO;return read(ctx,"image")}`, false},
		{"后续任务接口正常调用", `import("context";m "` + pkgModel + `");type reader interface{GetTask(context.Context,string)(m.Task,error)};func newTaskHandler(ctx context.Context,r reader)(m.Task,error){if _,err:=r.GetTask(ctx,"task");err!=nil{return m.Task{},err};read:=r.GetTask;return read(ctx,"task")}`, true},
		{"已迁移身份本地仓储接口仍拒绝", `type reader interface{GetUser()};func newUserHandler(r reader){r.GetUser();read:=r.GetUser;read()}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mediaTypedFixture(t, pkgConsole, map[string]string{"new.go": "package console;" + tc.body})
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v findings=%v", tc.allowed, got)
			}
		})
	}
	t.Run("已迁移身份Registry正常调用", func(t *testing.T) {
		body := `import i "` + pkgIdentityApp + `";func newUserHandler(r i.Registry){p,_:=r.Authenticate(nil,"token");_,_=r.CurrentUser(nil,p);read:=r.CurrentUser;_,_=read(nil,p)}`
		got := identityTypedFixture(t, identityFixtureImports(t), pkgConsole, "new.go", body)
		if len(got) != 0 {
			t.Fatal("真实身份入口不应被镜像边界拦截", got)
		}
	})
}

// 起点是 dev 48f6c2c1f1ee4aebdd0349868a097891e5c9ad13。补回这154项后必须逐字等于起点。
func Test镜像迁移精确删除154项并保持后两阶段不变(t *testing.T) {
	total, removedTotal := 0, 0
	phases := map[string]int{}
	for _, tc := range []struct {
		name     string
		entries  []migrationException
		count    int
		baseline string
		removed  []migrationException
	}{
		{"migrationExceptions", migrationExceptions, 64, "6196c008c4430e233c854b0a891446e2b6605f43f97b2cd0fe3555752324590f", []migrationException{
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*exactChunkReader).Read", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).createISO", "github.com/lober/lober/internal/domain/repository", "CheckNewISO", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).createISO", "github.com/lober/lober/internal/domain/repository", "ISOs.CreateISO", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).deprecateISO", "github.com/lober/lober/internal/domain/repository", "ISOs.DeprecateISO", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).getISO", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).isoProgress", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).isoProgress", "github.com/lober/lober/internal/interfaces/console", "ISOSourceState.SourcesOf", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).listISOs", "github.com/lober/lober/internal/domain/repository", "ISOQuery", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).listISOs", "github.com/lober/lober/internal/domain/repository", "ISOs.ListISOs", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).listISOs", "github.com/lober/lober/internal/domain/repository", "Page", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).registerISOSource", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).sourceOperation", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).sourceProgress", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).sourceProgress", "github.com/lober/lober/internal/domain/repository", "ErrNotFound", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).sourceProgress", "github.com/lober/lober/internal/interfaces/console", "ISOSourceState.GetSource", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).sourceProgress", "github.com/lober/lober/internal/interfaces/console", "ISOSourceState.RangesOf", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).uploadChunk", "github.com/lober/lober/internal/domain/repository", "CheckSourceKey", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).uploadChunk", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).uploadChunk", "github.com/lober/lober/internal/domain/repository", "ErrNotFound", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).uploadChunk", "github.com/lober/lober/internal/interfaces/console", "ISOSourceState.GetSource", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "checkISOSource", "github.com/lober/lober/internal/domain/repository", "CheckSource", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "checkISOSource", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isosource.go", "package-init", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/server.go", "package-init", "github.com/lober/lober/internal/domain/repository", "ISOs", "4"},
		}},
		{"mediaMigrationExceptions", mediaMigrationExceptions, 0, "4e93eaf8a62876bfa694042b348843f41709e729f45b4354708802cfe5df19db", []migrationException{
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/model", "ISO.Validate", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "CheckID", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "CheckSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/delete.go", "(*Sources).confirmDeleted", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/delete.go", "(*Sources).confirmDeleted", "github.com/lober/lober/internal/domain/repository", "ErrNotFound", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/delete.go", "(*Sources).confirmDeleted", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/delete.go", "cleanupOwnedTree", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/extract.go", "(*Sources).extract", "github.com/lober/lober/internal/domain/catalog", "RepoPaths", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "Arch.Valid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "FormatMedia", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "Formats", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "SeriesByEdition", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "SeriesMedia", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "SeriesOfFormat", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/model", "IsSafeWord", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/model", "ValidLabel", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/identify.go", "identifyProblem", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/catalog", "RequiredRoles", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/model", "ISO.Validate", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "CheckID", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ErrImmutable", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ISOs.UpdateISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/model", "StorableText", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "ISOSources.GetSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "ISOs.UpdateISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).openMedia", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "(*Sources).readImportOutcome", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "importProblem", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/import.go", "publishImportTree", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/iso.go", "New", "github.com/lober/lober/internal/domain/repository", "Store", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/iso.go", "package-init", "github.com/lober/lober/internal/domain/repository", "Store", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/local.go", "(*Sources).fetchLocal", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/local.go", "(*Sources).fetchLocal", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/netboot.go", "identifyNetboot", "github.com/lober/lober/internal/domain/catalog", "Arch.Valid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOQuery", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOSources.ListUnfinishedSources", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOs.ListISOs", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/recover.go", "allFetched", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/remote.go", "(*Sources).fetchRemote", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/remote.go", "(*Sources).fetchRemote", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/remote.go", "(*Sources).fetchRemote", "github.com/lober/lober/internal/domain/repository", "ProblemsError", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/remote.go", "parseContentRange", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/remote.go", "rangeNumber", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).failFetch", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).failFetch", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ErrRejected", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ProblemsError", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).getSource", "github.com/lober/lober/internal/domain/repository", "ErrNotFound", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).getSource", "github.com/lober/lober/internal/domain/repository", "ISOSources.GetSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).restorePublication", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).resumeOffset", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).resumeOffset", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).resumeOffset", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).writeFetch", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "(*Sources).writeFetch", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "checkSourceKey", "github.com/lober/lober/internal/domain/repository", "CheckSourceKey", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "sourceRejected", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "sourceRejected", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/source.go", "sourceRejected", "github.com/lober/lober/internal/domain/repository", "ProblemsError", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Progress", "github.com/lober/lober/internal/domain/repository", "ISOSources.RangesOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Progress", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Progress", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Register", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).RegisterQueued", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Reimport", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSourceAndAcquire", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "CheckSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOSources.GetSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSourceAndAcquire", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).retrySource", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).retrySource", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/sources.go", "(*Sources).retrySource", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/suse.go", "identifySUSELayout", "github.com/lober/lober/internal/domain/catalog", "Arch.Valid", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/suse.go", "identifySUSELayout", "github.com/lober/lober/internal/domain/catalog", "RepoPaths", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ISOSources.RangesOf", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveRange", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/upload.go", "compareRange", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
			{"github.com/lober/lober/internal/infrastructure/media", "internal/infrastructure/media/windows.go", "(*Sources).extractWindows", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
		}},
		{"consoleRuleMigrationExceptions", consoleRuleMigrationExceptions, 42, "258e968b25d41520233c8f43af192c2fe31d41fc125d7f56fd78a13999252cff", []migrationException{
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).checkSourceSpace", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).listISOs", "github.com/lober/lober/internal/domain/catalog", "Family.Valid", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).listISOs", "github.com/lober/lober/internal/domain/catalog", "Series.Valid", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "(*server).listISOs", "github.com/lober/lober/internal/domain/model", "ISOState.Valid", "4"},
			{"github.com/lober/lober/internal/interfaces/console", "internal/interfaces/console/isos.go", "checkISOSource", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
		}},
		{"notificationMigrationExceptions", notificationMigrationExceptions, 1, "ba63e2a81c31ee5bf62e0288cdf68e5845af7009edfd247e6b18a119189aeaf6", []migrationException{}},
	} {
		tc.entries = phaseFourBaselineEntries(tc.name, tc.entries)
		if len(tc.entries) != tc.count {
			t.Errorf("%s=%d want %d", tc.name, len(tc.entries), tc.count)
		}
		total += len(tc.entries)
		removedTotal += len(tc.removed)
		var lines []string
		seen := map[migrationException]bool{}
		for _, e := range tc.entries {
			if e.RemoveIn != "5" && e.RemoveIn != "6" || e.File == "internal/interfaces/console/wizard.go" {
				t.Errorf("已迁移许可复活: %+v", e)
			}
			phases[e.RemoveIn]++
		}
		for _, e := range tc.removed {
			if e.RemoveIn != "4" {
				t.Fatalf("删除了别的阶段: %+v", e)
			}
		}
		for _, e := range append(append([]migrationException{}, tc.entries...), tc.removed...) {
			if seen[e] {
				t.Fatalf("重复许可: %+v", e)
			}
			seen[e] = true
			lines = append(lines, strings.Join([]string{e.Caller, e.File, e.Function, e.Target, e.Symbol, e.RemoveIn}, "\t")+"\n")
		}
		slices.Sort(lines)
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(lines, "")))); got != tc.baseline {
			t.Errorf("%s 超出第四阶段精确删除集: %s", tc.name, got)
		}
	}
	if total != 107 || removedTotal != 154 || phases["5"] != 65 || phases["6"] != 42 {
		t.Fatal(total, removedTotal, phases)
	}
}
