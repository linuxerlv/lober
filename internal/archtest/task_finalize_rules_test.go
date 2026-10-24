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
	"testing/fstest"
)

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
