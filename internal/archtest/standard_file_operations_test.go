package archtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type wholeBranchFileProbe struct {
	name, caller, body, marker string
	want                       rule
}

// 保留独立审查原74格输入，函数体、调用包、名字和标记不变。
func wholeBranchFileProbes() []wholeBranchFileProbe {
	out := []wholeBranchFileProbe{
		{"machine-iofs", pkgMachine, `import "io/fs";func extra(f fs.FS)([]byte,error){return fs.ReadFile(f,"business.json")}`, "ReadFile", ruleMachine},
		{"machine-embed", pkgMachine, `import "embed";func extra(f embed.FS)([]byte,error){return f.ReadFile("business.json")}`, "ReadFile", ruleMachine},
	}
	callers := []string{pkgConsole, pkgConsole + "/extra", pkgMachine, pkgTasks, pkgTasksPort, modulePath + "/internal/application/hosts", modulePath + "/internal/application/hosts/port", modulePath + "/internal/application/authoring", modulePath + "/internal/application/authoring/port", fixtureMedia, fixtureMediaPort, pkgIdentityApp, pkgIdentityPort, pkgAuditApp, pkgAuditApp + "/port", pkgPorts, pkgExecution, pkgExecutionPort}
	wrappers := []struct{ name, body, marker string }{
		{"filepath-glob", `import "path/filepath";func extra()([]string,error){return filepath.Glob("business/*.json")}`, "Glob"},
		{"filepath-walk", `import "path/filepath";func extra()error{return filepath.Walk("business",nil)}`, "Walk"},
		{"zip-open", `import "archive/zip";func extra()(*zip.ReadCloser,error){return zip.OpenReader("business.zip")}`, "OpenReader"},
		{"template-files", `import "text/template";func extra()(*template.Template,error){return template.ParseFiles("business.txt")}`, "ParseFiles"},
	}
	for _, caller := range callers {
		for _, wrapper := range wrappers {
			want := ruleApplication
			if under(caller, pkgConsole) {
				want = ruleMigration
			}
			if under(caller, pkgMachine) {
				want = ruleMachine
			}
			out = append(out, wholeBranchFileProbe{wrapper.name + "/" + caller, caller, wrapper.body, wrapper.marker, want})
		}
	}
	return out
}

func expandedStandardFileProbes() []wholeBranchFileProbe {
	wrappers := []struct{ name, body, marker string }{
		{"filepath-WalkDir", `import "path/filepath";func extra(){_ = filepath.WalkDir}`, "WalkDir"},
		{"filepath-EvalSymlinks", `import "path/filepath";func extra(){_ = filepath.EvalSymlinks}`, "EvalSymlinks"},
		{"filepath-Abs", `import "path/filepath";func extra(){_ = filepath.Abs}`, "Abs"},
		{"text/template-method-ParseFiles", `import "text/template";func extra(t *template.Template){_ = t.ParseFiles}`, "ParseFiles"},
		{"text/template-expression-ParseFiles", `import "text/template";var extra=(*template.Template).ParseFiles`, "ParseFiles"},
		{"text/template-ParseGlob", `import "text/template";func extra(){_ = template.ParseGlob}`, "ParseGlob"},
		{"text/template-method-ParseGlob", `import "text/template";func extra(t *template.Template){_ = t.ParseGlob}`, "ParseGlob"},
		{"text/template-expression-ParseGlob", `import "text/template";var extra=(*template.Template).ParseGlob`, "ParseGlob"},
		{"text/template-ParseFS", `import "text/template";func extra(){_ = template.ParseFS}`, "ParseFS"},
		{"text/template-method-ParseFS", `import "text/template";func extra(t *template.Template){_ = t.ParseFS}`, "ParseFS"},
		{"text/template-expression-ParseFS", `import "text/template";var extra=(*template.Template).ParseFS`, "ParseFS"},
		{"html/template-ParseFiles", `import "html/template";func extra(){_ = template.ParseFiles}`, "ParseFiles"},
		{"html/template-method-ParseFiles", `import "html/template";func extra(t *template.Template){_ = t.ParseFiles}`, "ParseFiles"},
		{"html/template-expression-ParseFiles", `import "html/template";var extra=(*template.Template).ParseFiles`, "ParseFiles"},
		{"html/template-ParseGlob", `import "html/template";func extra(){_ = template.ParseGlob}`, "ParseGlob"},
		{"html/template-method-ParseGlob", `import "html/template";func extra(t *template.Template){_ = t.ParseGlob}`, "ParseGlob"},
		{"html/template-expression-ParseGlob", `import "html/template";var extra=(*template.Template).ParseGlob`, "ParseGlob"},
		{"html/template-ParseFS", `import "html/template";func extra(){_ = template.ParseFS}`, "ParseFS"},
		{"html/template-method-ParseFS", `import "html/template";func extra(t *template.Template){_ = t.ParseFS}`, "ParseFS"},
		{"html/template-expression-ParseFS", `import "html/template";var extra=(*template.Template).ParseFS`, "ParseFS"},
		{"debug/elf-Open", `import "debug/elf";func extra(){_ = elf.Open}`, "Open"},
		{"debug/macho-Open", `import "debug/macho";func extra(){_ = macho.Open}`, "Open"},
		{"debug/pe-Open", `import "debug/pe";func extra(){_ = pe.Open}`, "Open"},
		{"debug/plan9obj-Open", `import "debug/plan9obj";func extra(){_ = plan9obj.Open}`, "Open"},
		{"debug/macho-OpenFat", `import "debug/macho";func extra(){_ = macho.OpenFat}`, "OpenFat"},
		{"plugin-Open", `import "plugin";func extra(){_ = plugin.Open}`, "Open"},
		{"crypto/tls-LoadX509KeyPair", `import "crypto/tls";func extra(){_ = tls.LoadX509KeyPair}`, "LoadX509KeyPair"},
		{"crypto/x509-SystemCertPool", `import "crypto/x509";func extra(){_ = x509.SystemCertPool}`, "SystemCertPool"},
		{"go/parser-ParseDir", `import "go/parser";func extra(){_ = parser.ParseDir}`, "ParseDir"},
		{"go/parser-ParseFile", `import "go/parser";func extra(){_ = parser.ParseFile}`, "ParseFile"},
		{"go/parser-ParseExprFrom", `import "go/parser";func extra(){_ = parser.ParseExprFrom}`, "ParseExprFrom"},
		{"go/build-Import", `import "go/build";func extra(){_ = build.Import}`, "Import"},
		{"go/build-ImportDir", `import "go/build";func extra(){_ = build.ImportDir}`, "ImportDir"},
		{"go/importer-Default", `import "go/importer";func extra(){_ = importer.Default}`, "Default"},
		{"go/importer-ForCompiler", `import "go/importer";func extra(){_ = importer.ForCompiler}`, "ForCompiler"},
		{"go/build-context-Import", `import "go/build";func extra(c *build.Context){_ = c.Import}`, "Import"},
		{"go/build-context-ImportDir", `import "go/build";func extra(c *build.Context){_ = c.ImportDir}`, "ImportDir"},
		{"go/build-context-MatchFile", `import "go/build";func extra(c *build.Context){_ = c.MatchFile}`, "MatchFile"},
		{"go/build-context-OpenFile", `import "go/build";func extra(c *build.Context){_ = c.OpenFile}`, "OpenFile"},
		{"go/build-context-ReadDir", `import "go/build";func extra(c *build.Context){_ = c.ReadDir}`, "ReadDir"},
		{"go/build-context-IsDir", `import "go/build";func extra(c *build.Context){_ = c.IsDir}`, "IsDir"},
		{"filepath-alias-local", `import p "path/filepath";func extra(){read:=p.Glob;_,_=read("business/*.json")}`, "Glob"},
		{"filepath-global-value", `import "path/filepath";var extra=filepath.WalkDir`, "WalkDir"},
		{"filepath-function-field", `import "path/filepath";func extra(){r:=struct{Read func(string)([]string,error)}{Read:filepath.Glob};_,_=r.Read("business/*.json")}`, "Glob"},
		{"template-function-field", `import "text/template";func extra(){r:=struct{Read func(...string)(*template.Template,error)}{Read:template.ParseFiles};_,_=r.Read("business.txt")}`, "ParseFiles"},
		{"io/fs-ReadFile", `import "io/fs";func extra(){_ = fs.ReadFile}`, "ReadFile"},
		{"io/fs-ReadDir", `import "io/fs";func extra(){_ = fs.ReadDir}`, "ReadDir"},
		{"io/fs-Glob", `import "io/fs";func extra(){_ = fs.Glob}`, "Glob"},
		{"io/fs-WalkDir", `import "io/fs";func extra(){_ = fs.WalkDir}`, "WalkDir"},
		{"io/fs-Sub", `import "io/fs";func extra(){_ = fs.Sub}`, "Sub"},
		{"io/fs-interface-open", `import "io/fs";func extra(f fs.FS){_ = f.Open}`, "Open"},
		{"io/fs-interface-readfile", `import "io/fs";func extra(f fs.ReadFileFS){_ = f.ReadFile}`, "ReadFile}"},
		{"embed-method-ReadFile", `import "embed";func extra(f embed.FS){_ = f.ReadFile}`, "ReadFile"},
		{"embed-method-ReadDir", `import "embed";func extra(f embed.FS){_ = f.ReadDir}`, "ReadDir"},
		{"embed-method-Open", `import "embed";func extra(f embed.FS){_ = f.Open}`, "Open"},
	}
	callers := []string{pkgConsole, pkgMachine, pkgTasks}
	var out []wholeBranchFileProbe
	for _, caller := range callers {
		for _, wrapper := range wrappers {
			want := ruleApplication
			if under(caller, pkgConsole) {
				want = ruleMigration
			}
			if under(caller, pkgMachine) {
				want = ruleMachine
			}
			out = append(out, wholeBranchFileProbe{"expanded/" + wrapper.name + "/" + caller, caller, wrapper.body, wrapper.marker, want})
		}
	}
	return out
}

// 只查看本条输入的准确位置；其他文件的拒绝不能替这条反例过关。
func assertStandardFileRejection(t *testing.T, got []violation, tc wholeBranchFileProbe, file, source string) {
	t.Helper()
	for _, v := range got {
		if v.rule == ruleParse {
			t.Fatal("编译或装载失败不是边界拒绝", v)
		}
	}
	var own []violation
	for _, v := range got {
		if v.pkg == tc.caller && strings.Contains(filepath.ToSlash(v.pos), filepath.ToSlash(file)+":") {
			own = append(own, v)
		}
	}
	closureRejectSource(t, own, tc.want, file, source, tc.marker)
}

func standardFileImports(t *testing.T) identityActualImporter {
	return identityFixtureImports(t, "io/fs", "embed", "path/filepath", "archive/zip", "text/template", "html/template", "debug/elf", "debug/macho", "debug/pe", "debug/plan9obj", "plugin", "crypto/tls", "crypto/x509", "go/parser", "go/token", "go/build", "go/importer", "io", "bytes", "crypto/hmac", "crypto/sha256")
}

func TestWholeBranchStandardFileReadersMustBeRejected(t *testing.T) {
	imports := standardFileImports(t)
	original := wholeBranchFileProbes()
	if len(original) != 74 {
		t.Fatal("原74格不得删减", len(original))
	}
	type sourceCase struct {
		Name, Caller, Body, Marker string
		Rule                       rule
	}
	corpus := make([]sourceCase, len(original))
	for i, tc := range original {
		corpus[i] = sourceCase{tc.name, tc.caller, tc.body, tc.marker, tc.want}
	}
	encoded, err := json.Marshal(corpus)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("original74-json " + string(encoded))
	for _, tc := range append(original, expandedStandardFileProbes()...) {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, tc.caller, "wholebranch_extra.go", tc.body)
			assertStandardFileRejection(t, got, tc, "wholebranch_extra.go", "package probe;"+tc.body)
		})
	}
	pureCallers := []string{pkgConsole, pkgConsole + "/extra", pkgMachine, pkgTasks, pkgTasksPort, modulePath + "/internal/application/hosts", modulePath + "/internal/application/hosts/port", modulePath + "/internal/application/authoring", modulePath + "/internal/application/authoring/port", pkgPorts, pkgExecution}
	for _, caller := range pureCallers {
		t.Run("pure/path/"+caller, func(t *testing.T) {
			body := `import "path/filepath";func extra(s string){_=filepath.Join("given",s);_=filepath.Clean(s);_=filepath.Base(s);_=filepath.Dir(s);_=filepath.Ext(s);_,_=filepath.Rel("given",s);_,_=filepath.Match("*.txt",s);_=filepath.IsAbs(s);_=filepath.ToSlash(s)}`
			if got := identityTypedFixture(t, imports, caller, "pure_path.go", body); len(got) != 0 {
				t.Fatal("纯路径处理被误拒", got)
			}
		})
	}
	memoryCallers := append(pureCallers, pkgIdentityApp, pkgIdentityPort, pkgAuditApp, pkgAuditApp+"/port", fixtureMedia, fixtureMediaPort)
	for _, caller := range memoryCallers {
		for _, pkg := range []string{"text/template", "html/template"} {
			t.Run("pure/"+pkg+"/"+caller, func(t *testing.T) {
				body := `import("` + pkg + `";"io");func extra(w io.Writer){t:=template.New("given");_,_=t.Parse("given {{.}}");_=t.Execute(w,"value")}`
				if got := identityTypedFixture(t, imports, caller, "memory_template.go", body); len(got) != 0 {
					t.Fatal("内存模板被误拒", got)
				}
			})
		}
	}
	for _, pkg := range []string{"archive/zip", "debug/elf", "debug/macho", "debug/pe", "debug/plan9obj"} {
		t.Run("pure/reader/"+pkg, func(t *testing.T) {
			alias := filepath.Base(pkg)
			factory := "NewFile"
			args := "r"
			if pkg == "archive/zip" {
				factory = "NewReader"
				args = "r,n"
			}
			body := `import("` + pkg + `";"io");func extra(r io.ReaderAt,n int64){_,_=` + alias + `.` + factory + `(` + args + `)}`
			if got := identityTypedFixture(t, imports, pkgConsole, "memory_reader.go", body); len(got) != 0 {
				t.Fatal("给定Reader处理被误拒", got)
			}
		})
	}
	body := `import("crypto/hmac";"crypto/sha256");func extra(key,data []byte)[]byte{m:=hmac.New(sha256.New,key);_,_=m.Write(data);return m.Sum(nil)}`
	if got := identityTypedFixture(t, imports, pkgExecution, "pure_hmac.go", body); len(got) != 0 {
		t.Fatal("纯注入密钥HMAC被误拒", got)
	}
	body = `import "go/parser";func extra(){_,_=parser.ParseExpr("1+2")}`
	if got := identityTypedFixture(t, imports, pkgConsole, "memory_go_expression.go", body); len(got) != 0 {
		t.Fatal("内存表达式被误拒", got)
	}
}

func TestWholeBranchFileReadersReachActualModuleGuard(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module "+modulePath+"\n\ngo 1.27.1\n")
	write("internal/interfaces/console/base.go", "package console")
	write("internal/interfaces/machine/base.go", "package machine")
	write("internal/application/hosts/base.go", "package hosts")
	check := func() []violation {
		t.Helper()
		pkgs := packagesUnder(t, root, modulePath)
		got, _ := checkModule(os.DirFS(root), moduleSourceDirs(t, filepath.Join(root, "go.mod"), root), pkgs)
		return append(got, checkTypedTransitions(root, pkgs)...)
	}
	if got := check(); len(got) != 0 {
		t.Fatal("合法实际模块基线", got)
	}
	cases := append(wholeBranchFileProbes(), expandedStandardFileProbes()...)
	for i, tc := range cases {
		name := strings.TrimPrefix(tc.caller, modulePath+"/") + fmt.Sprintf("/wholebranch_%d.go", i)
		body := strings.ReplaceAll(tc.body, "extra", fmt.Sprintf("wholeBranchFileRead%d", i))
		write(name, "package "+filepath.Base(filepath.Dir(name))+";"+body)
	}
	got := check()
	for _, v := range got {
		if v.rule == ruleParse {
			t.Fatal("实际模块必须先成功装载", v)
		}
	}
	t.Log("actual module types loaded before rejection assertions")
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := strings.TrimPrefix(tc.caller, modulePath+"/") + fmt.Sprintf("/wholebranch_%d.go", i)
			body := strings.ReplaceAll(tc.body, "extra", fmt.Sprintf("wholeBranchFileRead%d", i))
			assertStandardFileRejection(t, got, tc, name, "package "+filepath.Base(filepath.Dir(name))+";"+body)
		})
	}
}
