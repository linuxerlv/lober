package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// 内存反例也先做真实类型检查；夹具本身不成立时直接报错，不能拿装载失败冒充拦截成功。
type transitionFixtureImporter map[string]string

func (sources transitionFixtureImporter) Import(name string) (*types.Package, error) {
	source, ok := sources[name]
	if !ok {
		return nil, fmt.Errorf("夹具未定义导入 %s", name)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name+".go", source, 0)
	if err != nil {
		return nil, err
	}
	return (&types.Config{Importer: sources}).Check(name, fset, []*ast.File{f}, nil)
}

func fixtureTransitionUses(t *testing.T, caller, file, source string) []violation {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	imports := transitionFixtureImporter{
		pkgStore:        "package repository; type Users interface { GetUser() }; type ISOs interface { GetISO() }; type Store interface { ISOs }",
		pkgNotification: "package notification; func NewHub() int { return 1 }",
	}
	if _, err := (&types.Config{Importer: imports}).Check(caller, fset, []*ast.File{f}, info); err != nil {
		t.Fatal("反例的类型检查失败", err)
	}
	return typedTransitionFindings("", caller, fset, []*ast.File{f}, info)
}

func Test过渡符号许可不能被同包新函数借用(t *testing.T) {
	for _, tc := range []struct {
		name, file, function, body string
		allowed                    bool
	}{
		{"原函数", "auth.go", "login", "s.users.GetUser()", true},
		{"原函数局部别名", "auth.go", "login", "users := s.users; users.GetUser()", true},
		{"原函数方法值", "auth.go", "login", "read := s.users.GetUser; read()", true},
		{"新函数", "auth.go", "steal", "s.users.GetUser()", false},
		{"新函数局部别名", "auth.go", "steal", "users := s.users; users.GetUser()", false},
		{"新函数方法值", "auth.go", "steal", "read := s.users.GetUser; read()", false},
		{"新文件同名函数", "new.go", "login", "s.users.GetUser()", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 类型声明单独放到允许声明的位置；待检查函数仍保留自己的真实文件身份。
			source := "package console; import repo \"" + pkgStore + "\"; type server struct { users repo.Users }; func (s *server) " + tc.function + "(){" + tc.body + "}"
			found := fixtureTransitionUses(t, pkgConsole, "internal/interfaces/console/"+tc.file, source)
			var methodUses []violation
			for _, v := range found {
				if v.subject == pkgStore+".Users.GetUser" {
					methodUses = append(methodUses, v)
				}
			}
			if (len(methodUses) == 0) != tc.allowed {
				t.Fatal(tc.name, methodUses)
			}
		})
	}
}

func Test镜像过渡方法和唯一通知转发必须精确(t *testing.T) {
	for _, tc := range []struct {
		caller, file, source string
		allowed              bool
	}{
		{pkgISO, "internal/infrastructure/media/delete.go", "package iso; import repo \"" + pkgStore + "\"; type Sources struct{store repo.Store}; func(s *Sources) confirmDeleted(){read:=s.store.GetISO;read()}", true},
		{pkgISO, "internal/infrastructure/media/sources.go", "package iso; import repo \"" + pkgStore + "\"; type Sources struct{store repo.Store}; func(s *Sources) register(){s.store.GetISO()}", false},
		{pkgISO, "internal/infrastructure/media/new.go", "package iso; import repo \"" + pkgStore + "\"; type Sources struct{store repo.Store}; func(s *Sources) confirmDeleted(){read:=s.store.GetISO;read()}", false},
		{pkgConsole, "internal/interfaces/console/notify.go", "package console; import n \"" + pkgNotification + "\"; func NewHub()int{return n.NewHub()}", true},
		{pkgConsole, "internal/interfaces/console/notify.go", "package console; import n \"" + pkgNotification + "\"; func steal()int{return n.NewHub()}", false},
		{pkgConsole, "internal/interfaces/console/new.go", "package console; import n \"" + pkgNotification + "\"; var steal=n.NewHub", false},
	} {
		got := fixtureTransitionUses(t, tc.caller, tc.file, tc.source)
		var restricted []violation
		for _, v := range got {
			if strings.HasSuffix(v.subject, ".ISOs.GetISO") || strings.HasSuffix(v.subject, ".NewHub") {
				restricted = append(restricted, v)
			}
		}
		if (len(restricted) == 0) != tc.allowed {
			t.Fatal(tc.file, restricted)
		}
	}
}

func Test应用边界不接受其他服务或伪装接口包(t *testing.T) {
	for _, tc := range []struct {
		caller, imp string
		allowed     bool
	}{
		{pkgTasks, pkgPorts, true}, {pkgTasks, pkgTasksPort, true}, {pkgTasks, pkgModel, true},
		{pkgTasksPort, pkgPorts, true}, {pkgTasksPort, pkgStore, true},
		{pkgTasks, modulePath + "/internal/application/media", false},
		{pkgTasks, modulePath + "/internal/application/media/port", false},
		{pkgTasks, pkgTasksPort + "s", false}, {pkgTasks, pkgTasksPort + "/service", false},
		{pkgTasksPort, pkgTasks, false}, {pkgTasks, pkgConsole, false}, {pkgTasks, pkgPgstore, false},
		{pkgTasks, gin, false}, {pkgPorts, pkgPgstore, false},
	} {
		dir := strings.TrimPrefix(tc.caller, modulePath+"/")
		got, counts := checkModule(fstest.MapFS{dir + "/x.go": {Data: []byte(importing(tc.imp))}}, nil, []goPackage{{ImportPath: tc.caller, GoFiles: []string{"x.go"}}})
		if (len(got) == 0) != tc.allowed || counts[layerApplication] != 1 {
			t.Fatal(tc, got, counts)
		}
	}
}

func Test真实总检查拒绝新文件仓储方法值及类型错误(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module "+modulePath+"\n\ngo 1.27.1\n")
	write("internal/domain/repository/users.go", "package repository; type Users interface{ GetUser() }")
	write("internal/interfaces/console/server.go", "package console; import repo \""+pkgStore+"\"; type server struct { users repo.Users }")
	write("internal/interfaces/console/auth.go", "package console; func(s *server) login(){s.users.GetUser()}")
	write("internal/infrastructure/media/iso.go", "package iso")
	check := func() []violation {
		t.Helper()
		pkgs := packagesUnder(t, root, modulePath)
		found, _ := checkModule(os.DirFS(root), moduleSourceDirs(t, filepath.Join(root, "go.mod"), root), pkgs)
		return append(found, checkTypedTransitions(root, pkgs)...)
	}
	if got := check(); len(got) != 0 {
		t.Fatal("合法真实基线失败", got)
	}
	write("internal/interfaces/console/new.go", "package console; func(s *server) steal(){users:=s.users; read:=users.GetUser; read()}")
	got := check()
	if len(got) != 1 || got[0].rule != ruleMigration || got[0].subject != pkgStore+".Users.GetUser" {
		t.Fatal("真实新文件绕过守卫", got)
	}
	write("internal/interfaces/console/new.go", "package console; func steal(){undefinedCall()}")
	got = check()
	failed := false
	for _, v := range got {
		failed = failed || v.rule == ruleParse
	}
	if !failed {
		t.Fatal("类型错误被当成没有违规", got)
	}
	// 原网页文件恢复后，在另一个已登记的新应用包中制造类型错误。
	// 不能只检查存在旧调用的两个根包。
	write("internal/interfaces/console/new.go", "package console")
	write("internal/application/tasks/new.go", "package tasks; func invalid(){undefinedCall()}")
	got = check()
	failed = false
	for _, v := range got {
		failed = failed || v.rule == ruleParse && v.pkg == pkgTasks
	}
	if !failed {
		t.Fatal("新增应用包的类型错误漏出检查", got)
	}
}

func Test编号能力拒绝新调用方和其他随机入口(t *testing.T) {
	for _, pkg := range []string{pkgPgstore, pkgPgtest} {
		for _, imp := range []string{"crypto/rand", "math/rand", "math/rand/v2"} {
			if got := checkStoreSource(t, pkg, importing(imp)); len(got) == 0 {
				t.Fatal("数据库绕过编号能力", pkg, imp)
			}
		}
	}
	fset, f := parseSource(t, withImport(`"`+pkgIdentity+`"`, "func newID(){ _ = identity.NewID() }"))
	if got := checkAssemblyFile(fset, pkgAssembly, f); len(got) == 0 {
		t.Fatal("组装新函数借用了编号许可")
	}
	if len(identityCapabilities) != 6 {
		t.Fatal("编号能力只登记六个已有位置", len(identityCapabilities))
	}
	for _, entry := range identityCapabilities {
		if entry.Target != pkgIdentity || entry.Symbol != "NewID" {
			t.Fatal("编号许可变成了其他能力", entry)
		}
	}
}
