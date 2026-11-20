package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
	"testing/fstest"
)

func Test凭据实现只接受精确密码能力(t *testing.T) {
	pkg := modulePath + "/internal/infrastructure/credentials"
	for _, imp := range []string{modulePath + "/internal/application/identity/port", modulePath + "/internal/application/identity", modulePath + "/internal/application/identity/port/hidden", pkgPorts, pkgStore, pkgConsole} {
		source := "package credentials; import dep \"" + imp + "\"; var _ dep.Passwords"
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = (&types.Config{Importer: transitionFixtureImporter{imp: "package dep; type Passwords interface { Hash(string)(string,error); Check(string,string)bool; CheckDummy(string) }"}}).Check(pkg, fset, []*ast.File{f}, nil); err != nil {
			t.Fatal("反例必须先通过类型检查", err)
		}
		got, counts := checkModule(fstest.MapFS{"internal/infrastructure/credentials/x.go": {Data: []byte(source)}}, nil, []goPackage{{ImportPath: pkg, GoFiles: []string{"x.go"}}})
		allowed := strings.HasSuffix(imp, "/identity/port")
		if (len(got) == 0) != allowed || len(counts) != 1 {
			t.Errorf("凭据能力边界错误 %s: %v %v", imp, got, counts)
		}
	}
	// 同一个接口包也包含账号存储能力，密码实现不能借导入许可使用它。
	for _, tc := range []struct {
		alias, use string
		allowed    bool
	}{
		{"cap", "cap.Passwords", true},
		{"cap", "cap.Tokens", true},
		{"cap", "cap.Users", false},
		{"cap", "cap.RoleMenus", false},
		{".", "Users", false},
	} {
		imp := modulePath + "/internal/application/identity/port"
		source := "package credentials; import " + tc.alias + " \"" + imp + "\"; var _ " + tc.use
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		imports := transitionFixtureImporter{imp: "package port; type Passwords interface { Hash(string)(string,error); Check(string,string)bool; CheckDummy(string) }; type Tokens interface { NewToken()(string,error) }; type Users interface { GetUser() }; type RoleMenus interface { GetRoleMenus() }"}
		if _, err = (&types.Config{Importer: imports}).Check(pkg, fset, []*ast.File{f}, nil); err != nil {
			t.Fatal("反例必须先通过类型检查", err)
		}
		got, _ := checkModule(fstest.MapFS{"internal/infrastructure/credentials/x.go": {Data: []byte(source)}}, nil, []goPackage{{ImportPath: pkg, GoFiles: []string{"x.go"}}})
		if (len(got) == 0) != tc.allowed {
			t.Errorf("符号能力边界错误 %s: %v", tc.use, got)
		}
	}
}
