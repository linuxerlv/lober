package archtest

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/tools/go/packages"
)

const pkgIdentityApp = modulePath + "/internal/application/identity"
const pkgAuditApp = modulePath + "/internal/application/audit"

const ruleIdentityAudit rule = "身份和审计只能通过已声明的入口使用，不能重建存储、会话或权限业务"

func identityConsoleSymbol(symbol string) bool {
	switch symbol {
	case "Registry", "Principal", "LoginCommand", "SessionResult", "UserView", "Operation", "AccessRequest", "UserQuery", "UserPage", "CreateUserCommand", "UpdateUserCommand", "PasswordCommand", "ProfileCommand", "MenuDefinition", "RoleMenuView", "MenuList", "SaveMenusCommand",
		"ErrLoginFailed", "ErrBadRequest", "ErrSelfDisable", "ErrInitialPasswordUnchanged", "General", "UserInfo", "Logout", "ChangePassword", "LegacySelfUpdate", "KnownPermission",
		"PermAnyUser", "PermHostRead", "PermISORead", "PermHookRead", "PermTemplateRead", "PermTaskRead", "PermHostWrite", "PermTaskCreate", "PermTaskCancel", "PermTaskRetry", "PermHookWrite", "PermTemplateWrite", "PermHookApprove", "PermTemplateApprove", "PermISOWrite", "PermUserManage", "PermAuditRead",
		"Access.Login", "Access.Logout", "Access.CurrentUser", "Access.Authenticate", "Access.Authorize", "Access.StreamAuthorized", "Accounts.ListUsers", "Accounts.CreateUser", "Accounts.UpdateUser", "Accounts.DisableUser", "Accounts.ChangePassword", "Accounts.UpdateProfile", "Menus.ListRoleMenus", "Menus.SaveRoleMenus", "Principal.Actor", "PublicRoles":
		return true
	}
	return false
}

func identityRepositoryMethod(name string) bool {
	switch name {
	case "GetUser", "ListUsers", "CreateUser", "UpdateUser", "GetRoleMenus", "SaveRoleMenus", "ListAudit":
		return true
	}
	return false
}

// 根据类型对象检查包别名、方法值和本地接口；不依赖局部变量的名字。
func identityAuditFindings(caller string, fset *token.FileSet, files []*ast.File, info *types.Info) []violation {
	console := under(caller, pkgConsole)
	assembly := caller == pkgAssembly
	credentials := caller == pkgCredentials
	if !console && !assembly && !credentials {
		return nil
	}
	var found []violation
	for _, f := range files {
		file := filepath.ToSlash(fset.Position(f.Pos()).Filename)
		for _, decl := range f.Decls {
			function := declarationName(decl)
			assemblyAt := func(want string) bool {
				return assembly && strings.HasSuffix(file, "cmd/lober/app.go") && function == want
			}
			report := func(pos token.Pos, subject string) {
				found = append(found, violation{rule: ruleIdentityAudit, pkg: caller, subject: subject, pos: position(fset, pos)})
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					obj := info.Uses[id]
					if obj == nil {
						obj = info.Defs[id]
					}
					if obj == nil || obj.Pkg() == nil {
						return true
					}
					if _, ok := obj.(*types.PkgName); ok {
						return true
					}
					if v, ok := obj.(*types.Var); ok && v.IsField() {
						return true
					}
					target, symbol := obj.Pkg().Path(), transitionSymbol(obj)
					bad := false
					if fn, ok := obj.(*types.Func); ok && identityRepositoryMethod(fn.Name()) {
						// 外部应用入口保留；网页自建的小接口和组装点均不能读写账号存储。
						bad = credentials || assembly || console && target == caller
					}
					if fn, ok := obj.(*types.Func); ok && console && target == caller {
						if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
							if _, ok := recv.Type().Underlying().(*types.Interface); ok {
								switch fn.Name() {
								case "Put", "Lookup", "ExpiresAt", "Drop", "DropUser":
									bad = true
								}
							}
						}
					}
					if console {
						if target == pkgIdentityApp {
							bad = bad || !identityConsoleSymbol(symbol)
						}
						if target == pkgAuditApp {
							switch symbol {
							case "Reader", "Filter", "Query", "Entry", "Page", "ErrBadRequest", "Reader.List", "Reader.ExportCSV":
							default:
								bad = true
							}
						}
						bad = bad || under(target, pkgIdentityPort) || under(target, pkgAuditApp+"/port") || under(target, pkgCredentials)
					}
					if console || assembly {
						// 机器签名密钥是原有独立用途，只保留原文件、函数和 Reader 符号。
						machineRandom := assembly && strings.HasSuffix(file, "cmd/lober/machine.go") && function == "machineSigningKey" && target == "crypto/rand" && symbol == "Reader"
						bad = bad || !machineRandom && (under(target, "crypto/rand") || under(target, "crypto/pbkdf2") || under(target, "crypto/subtle") || under(target, "math/rand"))
					}
					if assembly {
						if target == layer1+"/access" || target == layer1+"/audit" {
							bad = true
						}
						if target == pkgIdentityApp {
							switch symbol {
							case "Service", "Bootstrapper":
							case "NewService", "Deps", "NewSessionBook":
								bad = bad || !assemblyAt("identityServiceOf")
							case "Bootstrapper.Initialise":
								bad = bad || !assemblyAt("(*app).Initialise")
							default:
								bad = true
							}
						}
						if target == pkgCredentials {
							bad = bad || !assemblyAt("identityServiceOf") || (symbol != "NewPasswords" && symbol != "NewTokens")
						}
						if target == pkgAuditApp {
							bad = bad || (symbol != "Reader" && (symbol != "NewReader" || !assemblyAt("auditReaderOf")))
						}
					}
					if bad {
						report(id.Pos(), target+"."+symbol)
					}
				}
				if console || assembly {
					role := func(e ast.Expr) bool {
						typ := info.TypeOf(e)
						if typ == nil {
							return false
						}
						named, ok := types.Unalias(typ).(*types.Named)
						return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == pkgModel && named.Obj().Name() == "Role"
					}
					switch x := n.(type) {
					case *ast.BinaryExpr:
						if role(x.X) || role(x.Y) {
							report(x.Pos(), "model.Role 权限判断")
						}
					case *ast.SwitchStmt:
						if x.Tag != nil && role(x.Tag) {
							report(x.Pos(), "model.Role 权限分支")
						}
					}
				}
				return true
			})
		}
	}
	return found
}

// 反例使用仓库真实类型；导入失败和编译失败都不能当成边界拦截。
type identityActualImporter map[string]*types.Package

func (i identityActualImporter) Import(path string) (*types.Package, error) {
	if p := i[path]; p != nil {
		return p, nil
	}
	return nil, fmt.Errorf("未装载真实依赖 %s", path)
}

func identityFixtureImports(t *testing.T, extra ...string) identityActualImporter {
	t.Helper()
	patterns := []string{pkgIdentityApp, pkgAuditApp, pkgCredentials, pkgConsole, pkgExecution, "os", "time", "crypto/rand", "crypto/pbkdf2", "crypto/sha256"}
	patterns = append(patterns, extra...)
	loaded, err := packages.Load(&packages.Config{Dir: filepath.Dir(goModPath(t)), Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps}, patterns...)
	if err != nil {
		t.Fatal(err)
	}
	imports := identityActualImporter{}
	packages.Visit(loaded, nil, func(p *packages.Package) {
		if len(p.Errors) != 0 || p.Types == nil {
			t.Fatalf("真实依赖装载失败 %s: %v", p.PkgPath, p.Errors)
		}
		imports[p.PkgPath] = p.Types
	})
	return imports
}

func identityTypedFixture(t *testing.T, imports identityActualImporter, caller, file, body string) []violation {
	t.Helper()
	name := strings.TrimPrefix(caller, modulePath+"/") + "/" + file
	source := "package probe;" + body
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	if _, err := (&types.Config{Importer: imports}).Check(caller, fset, []*ast.File{f}, info); err != nil {
		t.Fatal("反例必须先通过真实 Go 类型检查", err)
	}
	t.Log("真实类型检查通过")
	got, _ := checkModule(fstest.MapFS{name: {Data: []byte(source)}}, nil, []goPackage{{ImportPath: caller, GoFiles: []string{file}}})
	return append(got, typedTransitionFindings("", caller, fset, []*ast.File{f}, info)...)
}

func Test身份审计类型边界(t *testing.T) {
	imports := identityFixtureImports(t)
	i := `i "` + pkgIdentityApp + `"`
	r := `r "` + pkgStore + `"`
	p := `p "` + pkgIdentityPort + `"`
	a := `a "` + pkgAuditApp + `"`
	m := `m "` + pkgModel + `"`
	cases := []struct {
		name, caller, body string
		allowed            bool
	}{
		{"网页新文件查用户", pkgConsole, `import ` + r + `;func f(s r.Users){read:=s.GetUser;_,_=read(nil,"x")}`, false},
		{"网页别名查菜单", pkgConsole, `import ` + p + `;type hidden=p.RoleMenus;func f(s hidden){read:=s.GetRoleMenus;_,_,_=read(nil,"")}`, false},
		{"网页本地用户接口", pkgConsole, `type local interface{GetUser()};func f(s local){read:=s.GetUser;read()}`, false},
		{"网页仅持有匿名菜单接口", pkgConsole, `type holder struct{Menus interface{GetRoleMenus()}}`, false},
		{"网页本地会话接口", pkgConsole, `type local interface{Put(string)string};func f(s local){_=s.Put("x")}`, false},
		{"网页点导入用户能力", pkgConsole, `import . "` + pkgIdentityPort + `";var _ Users`, false},
		{"网页构造身份服务", pkgConsole, `import ` + i + `;var build=i.NewService`, false},
		{"网页公开角色无需身份", pkgConsole, `import ` + i + `;func f(){_=i.PublicRoles()}`, true},
		{"网页持有具体服务", pkgConsole, `import ` + i + `;var service *i.Service`, false},
		{"网页构造会话表", pkgConsole, `import ` + i + `;var build=i.NewSessionBook`, false},
		{"网页调用会话方法值", pkgConsole, `import ` + i + `;func f(s *i.SessionBook){put:=s.Put;_=put("x")}`, false},
		{"网页会话方法表达式", pkgConsole, `import ` + i + `;var lookup=(*i.SessionBook).Lookup`, false},
		{"网页启动后门", pkgConsole, `import ` + i + `;func f(b i.Bootstrapper){_=b.Initialise(nil,"x")}`, false},
		{"网页重算角色", pkgConsole, `import ` + m + `;func f(role m.Role)bool{return role==m.RoleAdmin}`, false},
		{"网页角色分支", pkgConsole, `import ` + m + `;func f(role m.Role)bool{switch role{case m.RoleAdmin:return true};return false}`, false},
		{"网页角色方法值", pkgConsole, `import ` + m + `;var valid=m.Role.Valid`, false},
		{"网页重算菜单", pkgConsole, `import d "` + layer1 + `/access";var effective=d.EffectiveMenuIDs`, false},
		{"网页直接读随机数", pkgConsole, `import "crypto/rand";var random=rand.Read`, false},
		{"网页直接推导口令", pkgConsole, `import("crypto/pbkdf2";"crypto/sha256");func f(){_,_=pbkdf2.Key(sha256.New,"x",nil,600000,32)}`, false},
		{"网页构造审计查询", pkgConsole, `import ` + a + `;var build=a.NewReader`, false},
		{"凭据持有通用存储", pkgCredentials, `import ` + r + `;var store r.Store`, false},
		{"凭据持有自己用户能力", pkgCredentials, `import ` + p + `;var store p.Users`, false},
		{"凭据自建用户能力", pkgCredentials, `type holder struct{Users interface{GetUser()}}`, false},
		{"身份应用导入网页", pkgIdentityApp, `import c "` + pkgConsole + `";var _ c.Deps`, false},
		{"审计应用依赖身份具体服务", pkgAuditApp, `import ` + i + `;var _ *i.Service`, false},
		{"身份应用读取文件", pkgIdentityApp, `import "os";var read=os.ReadFile`, false},
		{"审计应用读取时钟", pkgAuditApp, `import "time";var now=time.Now`, false},
		{"领域读取时钟", layer1 + "/access", `import "time";var now=time.Now`, false},
		{"领域读取文件", layer1 + "/audit", `import "os";var read=os.ReadFile`, false},
		{"组装重做用户保存", pkgAssembly, `import ` + r + `;func f(s r.Users){save:=s.CreateUser;_=save}`, false},
		{"组装重做菜单判断", pkgAssembly, `import d "` + layer1 + `/access";var normalize=d.NormalizeMenuIDs`, false},
		{"组装发放会话", pkgAssembly, `import ` + i + `;func f(s *i.SessionBook){_=s.Put("x")}`, false},
		{"执行依赖身份服务", pkgExecution, `import ` + i + `;var _ *i.Service`, false},
		{"正常身份入口", pkgConsole, `import ` + i + `;func f(s i.Registry,p i.Principal){_,_=s.Login(nil,i.LoginCommand{});_=s.Logout(nil,p);_,_=s.CurrentUser(nil,p);_,_=s.Authenticate(nil,"");_=s.Authorize(p,i.AccessRequest{});_=s.StreamAuthorized(nil,"","");_,_=s.ListUsers(nil,p,i.UserQuery{});_,_=s.CreateUser(nil,p,i.CreateUserCommand{});_,_=s.UpdateUser(nil,p,"",i.UpdateUserCommand{});_=s.DisableUser(nil,p,"");_=s.ChangePassword(nil,p,i.PasswordCommand{});_,_=s.UpdateProfile(nil,p,i.ProfileCommand{});_,_=s.ListRoleMenus(nil,p);_,_=s.SaveRoleMenus(nil,p,"",i.SaveMenusCommand{});_=p.Actor()}`, true},
		{"路由权限常量", pkgConsole, `import ` + i + `;const permission=i.PermUserManage;var known=i.KnownPermission;var op=i.UserInfo`, true},
		{"普通结果转换", pkgConsole, `import ` + i + `;func f(u i.UserView)(string,string){return u.Username,string(u.Role)}`, true},
		{"正常审计读取", pkgConsole, `import (` + a + `;p "` + pkgPorts + `");func f(r a.Reader){_,_=r.List(nil,p.Actor{},a.Query{});_,_=r.ExportCSV(nil,p.Actor{},a.Filter{})}`, true},
		{"凭据精确加密能力", pkgCredentials, `import (` + p + `;"crypto/rand");var _ p.Passwords;var _ p.Tokens;var random=rand.Read`, true},
		{"执行只转换审计意图", pkgExecution, `import (` + r + `;p "` + pkgPorts + `");func f(a p.AuditIntent)r.Audit{return r.Audit{Actor:a.Actor,Action:a.Action,Detail:a.Detail,At:a.At}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, tc.caller, "new_boundary.go", tc.body)
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v findings=%v", tc.allowed, got)
			}
		})
	}
}

// 公共能力不能新增仓储返回值、可修改的身份凭据或会话表出口。
func identityPublicMethodErrors(name string, typ types.Type, pointer bool, want map[string]string, qualify func(*types.Package) string) []string {
	if pointer {
		typ = types.NewPointer(typ)
	}
	methods := types.NewMethodSet(typ)
	got := map[string]string{}
	for j := 0; j < methods.Len(); j++ {
		method := methods.At(j).Obj()
		if method.Exported() {
			got[method.Name()] = types.TypeString(method.Type(), qualify)
		}
	}
	var errors []string
	if len(got) != len(want) {
		errors = append(errors, fmt.Sprintf("%s 能力方法数改变: %v", name, got))
	}
	for method, signature := range want {
		if got[method] != signature {
			errors = append(errors, fmt.Sprintf("%s.%s=%s，期望%s", name, method, got[method], signature))
		}
	}
	if !pointer && (name == "Principal" || name == "AuditIntent") {
		errors = append(errors, identityPublicMethodErrors(name, typ, true, want, qualify)...)
	}
	return errors
}

func Test公共契约拒绝仅指针公开方法(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         map[string]string
		allowed      bool
	}{
		{"Principal正常值方法", "package fixture; type Principal struct{}; func (Principal) Actor(){}", map[string]string{"Actor": "func()"}, true},
		{"Principal额外指针方法", "package fixture; type Principal struct{}; func (Principal) Actor(){}; func (*Principal) Extra(){}", map[string]string{"Actor": "func()"}, false},
		{"AuditIntent无方法", "package fixture; type AuditIntent struct{}", map[string]string{}, true},
		{"AuditIntent额外指针方法", "package fixture; type AuditIntent struct{}; func (*AuditIntent) Extra(){}", map[string]string{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "fixture.go", tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			pkg, err := (&types.Config{}).Check("fixture", fset, []*ast.File{f}, nil)
			if err != nil {
				t.Fatal("反例必须先通过真实 Go 类型检查", err)
			}
			name := "Principal"
			if strings.Contains(tc.source, "AuditIntent") {
				name = "AuditIntent"
			}
			got := identityPublicMethodErrors(name, pkg.Scope().Lookup(name).Type(), false, tc.want, func(p *types.Package) string { return p.Name() })
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v errors=%v", tc.allowed, got)
			}
		})
	}
}

func Test身份审计公共能力固定(t *testing.T) {
	imports := identityFixtureImports(t)
	qualify := func(p *types.Package) string { return p.Name() }
	for _, tc := range []struct {
		pkg, name string
		pointer   bool
		want      map[string]string
	}{
		{pkgIdentityApp, "Registry", false, map[string]string{
			"Login":            "func(context.Context, identity.LoginCommand) (identity.SessionResult, error)",
			"Logout":           "func(context.Context, identity.Principal) error",
			"CurrentUser":      "func(context.Context, identity.Principal) (identity.UserView, error)",
			"Authenticate":     "func(context.Context, string) (identity.Principal, error)",
			"Authorize":        "func(identity.Principal, identity.AccessRequest) error",
			"StreamAuthorized": "func(context.Context, string, string) bool",
			"ListUsers":        "func(context.Context, identity.Principal, identity.UserQuery) (identity.UserPage, error)",
			"CreateUser":       "func(context.Context, identity.Principal, identity.CreateUserCommand) (identity.UserView, error)",
			"UpdateUser":       "func(context.Context, identity.Principal, string, identity.UpdateUserCommand) (identity.UserView, error)",
			"DisableUser":      "func(context.Context, identity.Principal, string) error",
			"ChangePassword":   "func(context.Context, identity.Principal, identity.PasswordCommand) error",
			"UpdateProfile":    "func(context.Context, identity.Principal, identity.ProfileCommand) (identity.UserView, error)",
			"ListRoleMenus":    "func(context.Context, identity.Principal) (identity.MenuList, error)",
			"SaveRoleMenus":    "func(context.Context, identity.Principal, model.Role, identity.SaveMenusCommand) (identity.RoleMenuView, error)",
		}},
		{pkgIdentityApp, "Bootstrapper", false, map[string]string{"Initialise": "func(context.Context, string) error"}},
		{pkgAuditApp, "Reader", false, map[string]string{"List": "func(context.Context, ports.Actor, audit.Query) (audit.Page, error)", "ExportCSV": "func(context.Context, ports.Actor, audit.Filter) ([]byte, error)"}},
		{pkgIdentityApp, "Principal", false, map[string]string{"Actor": "func() ports.Actor"}},
		{pkgIdentityApp, "SessionBook", true, map[string]string{"Put": "func(username string) string", "Lookup": "func(token string) (string, bool)", "ExpiresAt": "func(token string) int64", "Drop": "func(token string)", "DropUser": "func(username string)"}},
		{pkgIdentityPort, "Passwords", false, map[string]string{"Hash": "func(string) (string, error)", "Check": "func(string, string) bool", "CheckDummy": "func(string)"}},
		{pkgIdentityPort, "Tokens", false, map[string]string{"NewToken": "func() (string, error)"}},
		{pkgIdentityPort, "Users", false, map[string]string{"GetUser": "func(context.Context, string) (model.User, error)", "ListUsers": "func(context.Context, repository.UserQuery) (repository.Page[model.User], error)", "CreateUser": "func(context.Context, model.User, repository.Audit) (model.User, error)", "UpdateUser": "func(context.Context, model.User, repository.Audit) (model.User, error)"}},
		{pkgIdentityPort, "RoleMenus", false, map[string]string{"GetRoleMenus": "func(context.Context, model.Role) (repository.RoleMenuSettings, bool, error)", "SaveRoleMenus": "func(context.Context, repository.RoleMenuSettings, repository.Audit) (repository.RoleMenuSettings, error)"}},
		{pkgAuditApp + "/port", "AuditLog", false, map[string]string{"ListAudit": "func(context.Context, repository.AuditQuery) (repository.Page[repository.AuditEntry], error)"}},
		{pkgPorts, "AuditIntent", false, map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := imports[tc.pkg].Scope().Lookup(tc.name)
			if obj == nil {
				t.Fatal("能力丢失", tc.name)
			}
			for _, err := range identityPublicMethodErrors(tc.name, obj.Type(), tc.pointer, tc.want, qualify) {
				t.Error(err)
			}
		})
	}
	for _, tc := range []struct {
		pkg, name string
		want      map[string]string
	}{
		{pkgIdentityApp, "Principal", map[string]string{"user": "model.User", "token": "string", "owner": "*identity.Service"}},
		{pkgPorts, "AuditIntent", map[string]string{"Actor": "string", "Action": "string", "Detail": "string", "At": "int64"}},
		{pkgIdentityApp, "Deps", map[string]string{"Users": "port.Users", "RoleMenus": "port.RoleMenus", "Passwords": "port.Passwords", "Sessions": "*identity.SessionBook", "Clock": "ports.Clock"}},
		{pkgIdentityApp, "UserView", map[string]string{"Username": "string", "DisplayName": "string", "Email": "string", "Avatar": "string", "Bio": "string", "Role": "model.Role", "Permissions": "[]string", "MenuIDs": "[]string", "Disabled": "bool", "MustChangePassword": "bool", "CreatedAt": "int64", "Rev": "int64", "MenuRev": "int64"}},
		{pkgIdentityApp, "UserPage", map[string]string{"Items": "[]identity.UserView", "Next": "string", "Total": "*int64"}},
		{pkgIdentityApp, "SessionResult", map[string]string{"Token": "string", "ExpiresAt": "int64", "User": "identity.UserView"}},
		{pkgAuditApp, "Filter", map[string]string{"TargetKind": "string", "TargetID": "string", "Actor": "string", "Action": "string", "From": "int64", "To": "int64"}},
		{pkgAuditApp, "Entry", map[string]string{"ID": "int64", "At": "int64", "Actor": "string", "Action": "string", "Detail": "string", "TargetKind": "string", "TargetID": "string"}},
		{pkgAuditApp, "Page", map[string]string{"Items": "[]audit.Entry", "Next": "string", "Total": "*int64"}},
	} {
		t.Run(tc.name+"字段", func(t *testing.T) {
			obj := imports[tc.pkg].Scope().Lookup(tc.name)
			if obj == nil {
				t.Fatal("类型丢失")
			}
			if _, alias := obj.Type().(*types.Alias); alias {
				t.Fatal("普通数据不能换成仓储别名")
			}
			st, ok := obj.Type().Underlying().(*types.Struct)
			if !ok {
				t.Fatal("必须是普通结构体")
			}
			if st.NumFields() != len(tc.want) {
				t.Fatal("字段数改变", st.NumFields())
			}
			for j := 0; j < st.NumFields(); j++ {
				f := st.Field(j)
				if tc.want[f.Name()] != types.TypeString(f.Type(), qualify) || f.Embedded() {
					t.Errorf("不允许的字段 %v", f)
				}
			}
		})
	}
}

func Test身份审计装配许可精确(t *testing.T) {
	imports := identityFixtureImports(t)
	for _, tc := range []struct {
		name, file, body string
		allowed          bool
	}{
		{"原机器密钥读取", "machine.go", `import "crypto/rand";func machineSigningKey(){_=rand.Reader}`, true},
		{"机器密钥同文件其他函数", "machine.go", `import "crypto/rand";func extra(){_=rand.Reader}`, false},
		{"机器密钥同名函数其他文件", "new.go", `import "crypto/rand";func machineSigningKey(){_=rand.Reader}`, false},
		{"机器密钥原位置其他随机方法", "machine.go", `import "crypto/rand";func machineSigningKey(){_=rand.Read}`, false},
		{"原身份装配", "app.go", `import i "` + pkgIdentityApp + `";func identityServiceOf(){_=i.NewService;_=i.NewSessionBook;_=i.Deps{}}`, true},
		{"身份装配同名其他文件", "new.go", `import i "` + pkgIdentityApp + `";func identityServiceOf(){_=i.NewService}`, false},
		{"身份装配同文件其他函数", "app.go", `import i "` + pkgIdentityApp + `";func extra(){_=i.NewService}`, false},
		{"原初始化委托", "app.go", `import i "` + pkgIdentityApp + `";type app struct{identity i.Bootstrapper};func(a *app)Initialise(){_=a.identity.Initialise(nil,"")}`, true},
		{"初始化同接口其他函数", "app.go", `import i "` + pkgIdentityApp + `";func extra(b i.Bootstrapper){_=b.Initialise(nil,"")}`, false},
		{"原审计装配", "app.go", `import a "` + pkgAuditApp + `";func auditReaderOf(){_=a.NewReader}`, true},
		{"审计装配同名其他文件", "new.go", `import a "` + pkgAuditApp + `";func auditReaderOf(){_=a.NewReader}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgAssembly, tc.file, tc.body)
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v findings=%v", tc.allowed, got)
			}
		})
	}
}

// 摘要来自实施前逐字段确认的107项清单，不从当前许可反算期望。
// 顺序也参与摘要；删错项、挪表、改阶段和改名都不能靠相同数量通过。
func Test身份审计精确删除68项保留39项(t *testing.T) {
	tables := []struct {
		name                       string
		current                    []migrationException
		count                      int
		remainingHash, removedHash string
	}{
		{"migrationExceptions", migrationExceptions, 27, "a28dab617ab35d3974dd809813bdf827338a98185361a92f3e40e210f0f6de8b", "9b6f8d2c8b473ce980de40534af1c873d1699385a813f73df6922bfc97cd44f8"},
		{"mediaMigrationExceptions", mediaMigrationExceptions, 0, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"consoleRuleMigrationExceptions", consoleRuleMigrationExceptions, 11, "04264bbe752a287b4c36354564a36611a1822969bf9a3cf6ca6be6260742d659", "62976f7f13685ac25feada3c8199e08731f3781bb9afa452b8cd64c2dd5c2cce"},
		{"notificationMigrationExceptions", notificationMigrationExceptions, 1, "ba63e2a81c31ee5bf62e0288cdf68e5845af7009edfd247e6b18a119189aeaf6", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	total, removedTotal := 0, 0
	for _, tc := range tables {
		tc.current = phaseFiveBaselineEntries(tc.name, tc.current)
		total += len(tc.current)
		removedTotal += len(phaseFivePlannedRemovals[tc.name])
		if len(tc.current) != tc.count {
			t.Errorf("%s 条数错误", tc.name)
		}
		for _, e := range tc.current {
			if e.RemoveIn != "6" {
				t.Errorf("已迁移阶段仍有许可: %+v", e)
			}
		}
		for _, set := range []struct {
			entries []migrationException
			want    string
		}{{tc.current, tc.remainingHash}, {phaseFivePlannedRemovals[tc.name], tc.removedHash}} {
			var lines []string
			for _, e := range set.entries {
				lines = append(lines, strings.Join([]string{e.Caller, e.File, e.Function, e.Target, e.Symbol, e.RemoveIn}, "\t")+"\n")
			}
			if got := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(lines, "")))); got != set.want {
				t.Errorf("%s 逐字段摘要改变: %s", tc.name, got)
			}
		}
		for _, e := range phaseFivePlannedRemovals[tc.name] {
			if slices.Contains(tc.current, e) || transitionAllows(e) {
				t.Errorf("删除项仍能使用: %+v", e)
			}
		}
	}
	if total != 39 || removedTotal != 68 {
		t.Fatal(total, removedTotal)
	}
}
