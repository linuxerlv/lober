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
)

// 位置从夹具的明确标记推导，不从检查器的结果反推。
func closureReject(t *testing.T, got []violation, want rule, file, body, marker string) {
	t.Helper()
	closureRejectSource(t, got, want, file, "package probe;"+body, marker)
}

func closureRejectSource(t *testing.T, got []violation, want rule, file, source, marker string) {
	t.Helper()
	at := strings.Index(source, marker)
	if at < 0 {
		t.Fatal("反例缺少位置标记", marker)
	}
	before := source[:at]
	line := strings.Count(before, "\n") + 1
	col := at - strings.LastIndex(before, "\n")
	suffix := fmt.Sprintf("%s:%d:%d", file, line, col)
	for _, v := range got {
		if v.rule == ruleParse {
			t.Fatal("编译或装载失败不算拒绝", v)
		}
		if v.rule == want && strings.HasSuffix(filepath.ToSlash(v.pos), suffix) {
			t.Log("准确拒绝", v)
			return
		}
	}
	t.Fatalf("漏拦或位置不准，要求 rule=%s position=%s，实际=%v", want, suffix, got)
}

func Test接口全文件与自建能力不得绕过(t *testing.T) {
	imports := identityFixtureImports(t, "context", pkgModel, "net/http")
	prefix := `import("context";m "` + pkgModel + `");`
	for _, tc := range []struct{ name, file, body, marker string }{
		{"未调用共同通知能力", "notification_unused.go", `import p "` + pkgPorts + `";type holder struct{Notify p.Notifier}`, "Notifier"},
		{"未调用共同选择锁", "selection_unused.go", `import p "` + pkgPorts + `";type holder struct{Lock p.SelectionGuard}`, "SelectionGuard"},
		{"未调用共同渲染能力", "preview_unused.go", `import p "` + pkgPorts + `";type holder struct{Preview p.FrozenPreview}`, "FrozenPreview"},
		{"应用方法别名再绑定", "rebound.go", `import("context";h "` + modulePath + `/internal/application/hosts";p "` + pkgPorts + `";m "` + pkgModel + `");var read=h.Registry.Get;func fallback(h.Registry,context.Context,p.Actor,string)(m.Host,error){return m.Host{},nil};func init(){read=fallback}`, "read"},
		{"原审计GetHost", "host_extra.go", prefix + `type finalAuditHostReader interface{GetHost(context.Context,string)(m.Host,error)};func finalAuditHostBypass(ctx context.Context,r finalAuditHostReader)(m.Host,error){return r.GetHost(ctx,"SN-1")}`, "GetHost"},
		{"改名文件未调用声明", "renamed.go", prefix + `type reader interface{Lookup(context.Context,string)(m.Host,error)}`, "Lookup"},
		{"未调用匿名接口", "unused.go", prefix + `type holder struct{Data interface{Lookup(context.Context,string)(m.Host,error)}}`, "Lookup"},
		{"匿名嵌入", "embedded.go", prefix + `type reader interface{Lookup(context.Context,string)(m.Host,error)};type holder interface{reader}`, "Lookup"},
		{"方法值", "method_value.go", prefix + `type reader interface{Lookup(context.Context,string)(m.Host,error)};func f(ctx context.Context,r reader){read:=r.Lookup;_,_=read(ctx,"x")}`, "Lookup"},
		{"包级方法值", "global.go", prefix + `type reader interface{Lookup(context.Context,string)(m.Host,error)};var load=reader.Lookup`, "Lookup"},
		{"字符串事实能力字段", "string_field.go", `import "context";type holder struct{Lookup func(context.Context,string)(string,error)}`, "Lookup"},
		{"函数能力字段", "field.go", prefix + `type holder struct{Lookup func(context.Context,string)(m.Host,error)}`, "Lookup"},
		{"包级函数能力", "global_func.go", prefix + `var lookup func(context.Context,string)(m.Host,error)`, "lookup"},
		{"两层辅助", "helpers.go", prefix + `type reader interface{Lookup(context.Context,string)(m.Host,error)};func one(r reader){_,_=r.Lookup(nil,"x")};func two(r reader){one(r)}`, "Lookup"},
		{"routes接线", "routes.go", prefix + `type reader interface{Lookup(context.Context,string)(m.Host,error)};var input reader;var routes=[]func(){func(){_,_=input.Lookup(nil,"x")}}`, "Lookup"},
		{"init接线", "init.go", prefix + `type reader interface{Lookup(context.Context,string)(m.Host,error)};var input reader;func init(){_,_=input.Lookup(nil,"x")}`, "Lookup"},
		{"Read同名不能取事实", "read.go", prefix + `type reader interface{Read(context.Context,string)(m.Host,error)}`, "Read"},
		{"Flush同名不能取事实", "flush.go", prefix + `type reader interface{Flush(context.Context,string)(m.Host,error)}`, "Flush"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgConsole, tc.file, tc.body)
			closureReject(t, got, ruleMigration, tc.file, tc.body, tc.marker)
		})
	}
}

func Test接口传输输出可用但客户端取事实被拒绝(t *testing.T) {
	imports := identityFixtureImports(t, "context", "net/http", "io", "time", pkgTasks, pkgPorts, pkgModel)
	for _, tc := range []struct {
		name, body, marker string
		allowed            bool
	}{
		{"标准输出", `import("io";"net/http");func f(w http.ResponseWriter,r io.Reader,c io.Closer){_,_=io.Copy(w,r);_=c.Close();w.WriteHeader(200);w.(http.Flusher).Flush()}`, "", true},
		{"传输字节别名", `type octet=byte;type reader interface{Read([]octet)(int,error)}`, "", true},
		{"精确本地传输接口", `import("context";"time");type transport interface{Read([]byte)(int,error);Write([]byte)(int,error);Close()error;Flush()};type ctx interface{Deadline()(time.Time,bool);Done()<-chan struct{};Err()error;Value(any)any};type failure interface{Error()string;Unwrap()error};func f(c context.Context){_=c.Err()}`, "", true},
		{"带错误的纯DTO函数字段", `import m "` + pkgModel + `";type view struct{SN string};type maps struct{Map func(m.Host)(view,error)}`, "", true},
		{"新文件纯DTO", `import m "` + pkgModel + `";type dto struct{SN string};func convert(h m.Host)dto{return dto{SN:h.SN}}`, "", true},
		{"真正应用方法值", `import("context";h "` + modulePath + `/internal/application/hosts";p "` + pkgPorts + `");func f(r h.Registry,ctx context.Context,p p.Actor){read:=r.Get;_,_=read(ctx,p,"SN-1")}`, "", true},
		{"真正应用包级函数值", `import h "` + modulePath + `/internal/application/hosts";var read=h.Registry.Get`, "", true},
		{"真正应用调用", `import("context";h "` + modulePath + `/internal/application/hosts";p "` + pkgPorts + `");func f(r h.Registry,ctx context.Context,p p.Actor){_,_=r.Get(ctx,p,"SN-1")}`, "", true},
		{"五秒补读取消退订", `import("context";"time";"net/http";a "` + pkgTasks + `");func stream(ctx context.Context,s a.EventSubscription,w http.ResponseWriter){defer s.Close();_=s.Initial();tick:=time.NewTicker(5*time.Second);defer tick.Stop();for{select{case<-ctx.Done():return;case<-s.Hints():case<-tick.C:};_,_=s.Read(ctx,0);_,_=w.Write(nil);w.(http.Flusher).Flush()}}`, "", true},
		{"文件取业务事实", `import "os";var read=os.ReadFile`, "ReadFile", false},
		{"客户端函数值", `import "net/http";var fetch=http.Get`, "Get", false},
		{"客户端方法值", `import "net/http";func f(c *http.Client){_=c.Get}`, "Get", false},
		{"客户端方法表达式", `import "net/http";var fetch=(*http.Client).Do`, "Do", false},
		{"伪Read输出业务", `import m "` + pkgModel + `";type reader interface{Read(string)(m.Host,error)}`, "Read", false},
		{"伪Flush输出业务", `import m "` + pkgModel + `";type writer interface{Flush()(m.Task,error)}`, "Flush", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgConsole, "transport_extra.go", tc.body)
			if tc.allowed {
				if len(got) != 0 {
					t.Fatal(got)
				}
				return
			}
			closureReject(t, got, ruleMigration, "transport_extra.go", tc.body, tc.marker)
		})
	}
}

func Test机器接口不得读取仓储(t *testing.T) {
	imports := identityFixtureImports(t, "context", pkgStore, pkgModel, pkgExecutionPort)
	for _, tc := range []struct{ name, body, marker string }{
		{"真实仓储别名", `import r "` + pkgStore + `";type hidden=r.Tasks;var _ hidden`, "Tasks"},
		{"未调用本地能力", `import("context";m "` + pkgModel + `");type reader interface{Lookup(context.Context,string)(m.Task,error)}`, "Lookup"},
		{"函数能力字段", `import("context";m "` + pkgModel + `");type deps struct{Lookup func(context.Context,string)(m.Host,error)}`, "Lookup"},
		{"应用port", `import p "` + pkgExecutionPort + `";var _ p.InstallerLogRepository`, "InstallerLogRepository"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgMachine, "machine_extra.go", tc.body)
			closureReject(t, got, ruleMachine, "machine_extra.go", tc.body, tc.marker)
		})
	}
	body := `import("context";e "` + pkgExecution + `");func f(g e.Gateway,c context.Context){_,_=g.BootMenu(c,e.BootInput{})}`
	if got := identityTypedFixture(t, imports, pkgMachine, "thin.go", body); len(got) != 0 {
		t.Fatal("机器薄委托被误拒", got)
	}
}

func Test所有应用外部操作都受检查(t *testing.T) {
	imports := identityFixtureImports(t, "net/http", "os/exec", "syscall", "io/ioutil", "io/fs", "math/rand/v2", "database/sql")
	for _, caller := range []string{pkgTasks, pkgTasksPort, modulePath + "/internal/application/hosts", modulePath + "/internal/application/hosts/port", modulePath + "/internal/application/authoring", modulePath + "/internal/application/authoring/port", fixtureMedia, fixtureMediaPort, pkgIdentityApp, pkgIdentityPort, pkgAuditApp, pkgAuditApp + "/port", pkgPorts, pkgExecution, pkgExecutionPort} {
		for _, body := range []string{`import "os";var read=os.ReadFile`, `import "net";var dial=net.Dial`, `import "os/exec";var run=exec.Command`, `import "net/http";var fetch=http.Get`, `import "database/sql";var open=sql.Open`, `import "syscall";var read=syscall.Open`, `import "io/ioutil";var read=ioutil.ReadFile`, `import "crypto/rand";var random=rand.Read`, `import "math/rand/v2";var random=rand.Uint64`} {
			t.Run(caller+body, func(t *testing.T) {
				got := identityTypedFixture(t, imports, caller, "external.go", body)
				found := false
				for _, v := range got {
					if v.rule == ruleParse {
						t.Fatal(v)
					}
					found = found || v.rule == ruleApplication && strings.Contains(v.pos, "external.go:")
				}
				if !found {
					t.Fatal("应用外部操作漏拦", got)
				}
			})
		}
	}
	for _, body := range []string{`import "time";var now=time.Now`, `import "time";var now=time.Since`} {
		if got := identityTypedFixture(t, imports, pkgExecution, "clock.go", body); len(got) == 0 {
			t.Fatal("执行应用自行读时钟")
		}
	}
}

func Test执行与日志职责不能回到适配器(t *testing.T) {
	imports := identityFixtureImports(t, pkgExecutionPort, pkgExecutionRules, pkgStore, pkgPorts)
	for _, tc := range []struct{ name, caller, body, marker string }{
		{"机器重算凭据期限", pkgMachine, `import m "` + pkgModel + `";func deadline(t m.Task)int64{return t.Execution.IssuedAt+5400}`, "Task"},
		{"日志不能持有任务事实", pkgEvents, `import m "` + pkgModel + `";var fact m.Task`, "Task"},
		{"机器领域摘要", pkgMachine, `import e "` + pkgExecutionRules + `";var digest=e.SelectFile`, "SelectFile"},
		{"机器领域状态", pkgMachine, `import e "` + pkgExecutionRules + `";var cancel=e.CanCancel`, "CanCancel"},
		{"日志仓储", pkgEvents, `import r "` + pkgStore + `";var _ r.Tasks`, "Tasks"},
		{"日志通知", pkgEvents, `import p "` + pkgPorts + `";var _ p.Notifier`, "Notifier"},
		{"日志执行仓储", pkgEvents, `import p "` + pkgExecutionPort + `";var _ p.InstallerLogRepository`, "InstallerLogRepository"},
		{"机器重建状态判断", pkgMachine, `import m "` + pkgModel + `";func f(t m.Task)bool{return t.State==m.TaskDone}`, "t.State=="},
		{"日志重建状态判断", pkgEvents, `import m "` + pkgModel + `";func f(t m.Task)bool{return t.State==m.TaskDone}`, "t.State=="},
		{"日志自建能力", pkgEvents, `import("context";m "` + pkgModel + `");type repository interface{Lookup(context.Context,string)(m.Task,error)}`, "Lookup"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, tc.caller, "roles.go", tc.body)
			closureReject(t, got, ruleMachine, "roles.go", tc.body, tc.marker)
		})
	}
}

// 临时模块也走发现、导入检查与真正全包类型装载，不能只验证局部函数。
func Test最终边界接入真实全模块路径(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		name = filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module "+modulePath+"\n\ngo 1.27.1\n")
	write("internal/domain/model/model.go", `package model;type Host struct{SN string};type Task struct{ID string}`)
	write("internal/interfaces/console/base.go", `package console`)
	write("internal/interfaces/machine/base.go", `package machine`)
	write("internal/infrastructure/events/base.go", `package events`)
	write("internal/application/execution/base.go", `package execution`)
	write("internal/application/hosts/base.go", `package hosts`)
	write("internal/application/authoring/base.go", `package authoring;func NewHooks(){}`)
	write("internal/application/ports/base.go", `package ports;type Notifier interface{Publish(string)};type SelectionGuard interface{Acquire()};type FrozenPreview interface{RenderFrozen()}`)
	check := func() []violation {
		t.Helper()
		pkgs := packagesUnder(t, root, modulePath)
		got, _ := checkModule(os.DirFS(root), moduleSourceDirs(t, filepath.Join(root, "go.mod"), root), pkgs)
		return append(got, checkTypedTransitions(root, pkgs)...)
	}
	if got := check(); len(got) != 0 {
		t.Fatal("合法新模块基线", got)
	}
	cases := []struct {
		file, body, marker string
		want               rule
	}{
		{"internal/interfaces/console/extra/entry.go", `import a "` + modulePath + `/internal/application/authoring";var build=a.NewHooks`, "NewHooks", ruleMigration},
		{"internal/interfaces/console/capabilities.go", `import p "` + pkgPorts + `";type unused struct{Notify p.Notifier;Lock p.SelectionGuard;Preview p.FrozenPreview}`, "Notifier", ruleMigration},
		{"internal/interfaces/console/renamed.go", `import("context";m "` + pkgModel + `");type reader interface{GetHost(context.Context,string)(m.Host,error)}`, "GetHost", ruleMigration},
		{"internal/interfaces/machine/renamed.go", `import("context";m "` + pkgModel + `");type reader interface{Lookup(context.Context,string)(m.Host,error)}`, "Lookup", ruleMachine},
		{"internal/infrastructure/events/renamed.go", `import("context";m "` + pkgModel + `");type reader interface{Lookup(context.Context,string)(m.Task,error)}`, "Lookup", ruleMachine},
		{"internal/application/execution/external.go", `import "net/http";var get=http.Get`, "net/http", ruleApplication},
		{"internal/application/hosts/external.go", `import "os";var read=os.ReadFile`, "os", ruleApplication},
		{"internal/application/authoring/external.go", `import "database/sql";var open=sql.Open`, "database/sql", ruleApplication},
		{"internal/interfaces/console/review_dial.go", `import("context";"net/http");func reviewFetchDirect(ctx context.Context){_,_=http.DefaultTransport.(*http.Transport).DialContext(ctx,"tcp","127.0.0.1:8080")}`, "DialContext", ruleMigration},
		{"internal/interfaces/console/review_dial_value.go", `import("context";"net/http");func reviewFetchValue(ctx context.Context){dial:=http.DefaultTransport.(*http.Transport).DialContext;_,_=dial(ctx,"tcp","127.0.0.1:8080")}`, "DialContext", ruleMigration},
		{"internal/interfaces/machine/review_dial.go", `import("context";"net/http");func reviewFetchDirect(ctx context.Context){_,_=http.DefaultTransport.(*http.Transport).DialContext(ctx,"tcp","127.0.0.1:8080")}`, "DialContext", ruleMachine},
		{"internal/interfaces/machine/review_dial_value.go", `import("context";"net/http");func reviewFetchValue(ctx context.Context){dial:=http.DefaultTransport.(*http.Transport).DialContext;_,_=dial(ctx,"tcp","127.0.0.1:8080")}`, "DialContext", ruleMachine},
		{"internal/interfaces/console/review_notify.go", `type reviewEmitterDeps struct{Notify func(string)}`, "Notify", ruleMigration},
		{"internal/infrastructure/events/review_notify.go", `type reviewEmitterDeps struct{Notify func(string)}`, "Notify", ruleMachine},
		{"internal/interfaces/console/review_notify_interface.go", `type reviewSender interface{Deliver(string)}`, "Deliver", ruleMigration},
		{"internal/infrastructure/events/review_notify_call.go", `type reviewCalledNotifyDeps struct{Notify func(string)};func reviewSend(d reviewCalledNotifyDeps){d.Notify("task-1")}`, `Notify("task-1")`, ruleMachine},
	}
	for _, tc := range cases {
		write(tc.file, "package "+filepath.Base(filepath.Dir(tc.file))+";"+tc.body)
	}
	got := check()
	for _, v := range got {
		if v.rule == ruleParse {
			t.Fatal("真实全模块反例必须成功装载", v)
		}
	}
	t.Log("真实全模块类型装载通过")
	for _, tc := range cases {
		marker := tc.marker
		if tc.want == ruleApplication {
			marker = "\"" + marker + "\""
		}
		t.Run(tc.file, func(t *testing.T) {
			closureRejectSource(t, got, tc.want, tc.file, "package "+filepath.Base(filepath.Dir(tc.file))+";"+tc.body, marker)
		})
	}
}

// 真实类型载入不会把普通DTO转换误当成能力。
func Test接口正常类型声明经过真实类型检查(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "dto.go", `package console;type view struct{Name string};func f(s string)view{return view{Name:s}}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	if _, err := (&types.Config{}).Check(pkgConsole, fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	if got := typedTransitionFindings("", pkgConsole, fset, []*ast.File{f}, info); len(got) != 0 {
		t.Fatal(got)
	}
}

func Test内置页面资源读取只开放精确操作(t *testing.T) {
	imports := identityFixtureImports(t, "io/fs", "embed")
	for _, tc := range []struct {
		name, file, body, marker string
		allowed                  bool
	}{
		{"嵌入资源子目录", "ui.go", `import("embed";"io/fs");var frontendFiles embed.FS;func WithFrontend(){_,_=fs.Sub(frontendFiles,"ui/dist")}`, "", true},
		{"页面正文", "ui.go", `import "io/fs";func frontendHandler(files fs.FS){_,_=fs.ReadFile(files,"index.html")}`, "", true},
		{"页面函数不能借机列目录", "ui.go", `import "io/fs";func frontendHandler(files fs.FS){_,_=fs.ReadDir(files,".")}`, "ReadDir", false},
		{"其他文件不能借函数名读文件", "ui_extra.go", `import "io/fs";func frontendHandler(files fs.FS){_,_=fs.ReadFile(files,"x")}`, "ReadFile", false},
		{"同文件其他函数不能读文件", "ui.go", `import "io/fs";func extra(files fs.FS){_,_=fs.ReadFile(files,"x")}`, "ReadFile", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgConsole, tc.file, tc.body)
			if tc.allowed {
				if len(got) != 0 {
					t.Fatal(got)
				}
				return
			}
			closureReject(t, got, ruleMigration, tc.file, tc.body, tc.marker)
		})
	}
}

func Test控制台子包也只可调用登记入口(t *testing.T) {
	imports := identityFixtureImports(t)
	body := `import a "` + modulePath + `/internal/application/authoring";var build=a.NewHooks`
	got := identityTypedFixture(t, imports, pkgConsole+"/extra", "entry.go", body)
	closureReject(t, got, ruleMigration, "entry.go", body, "NewHooks")
	body = `import("context";h "` + modulePath + `/internal/application/hosts";p "` + pkgPorts + `");func f(r h.Registry,ctx context.Context,p p.Actor){_,_=r.Get(ctx,p,"SN-1")}`
	if got := identityTypedFixture(t, imports, pkgConsole+"/extra", "entry.go", body); len(got) != 0 {
		t.Fatal("子包的真实应用调用被误拒", got)
	}
}

// I1使用已装载的标准库字段；不执行拨号，也不以编译失败代替边界拒绝。
func Test审查HTTP函数字段不得主动联网(t *testing.T) {
	imports := identityFixtureImports(t, "context", "net/http", "io")
	transport := imports["net/http"].Scope().Lookup("Transport")
	field, _, _ := types.LookupFieldOrMethod(types.NewPointer(transport.Type()), true, imports["net/http"], "DialContext")
	variable, ok := field.(*types.Var)
	if !ok || !variable.IsField() {
		t.Fatalf("最小输入必须命中真实函数字段，实际=%T", field)
	}
	if _, ok := variable.Type().Underlying().(*types.Signature); !ok {
		t.Fatal("DialContext必须是可调用字段")
	}
	t.Log("真实标准库来源：net/http.Transport.DialContext，*types.Var函数字段")
	for _, caller := range []string{pkgConsole, pkgConsole + "/extra", pkgMachine} {
		want := ruleMigration
		if caller == pkgMachine {
			want = ruleMachine
		}
		for _, tc := range []struct{ name, body string }{
			{"直接调用", `import("context";"net/http");func fetch(ctx context.Context){_,_=http.DefaultTransport.(*http.Transport).DialContext(ctx,"tcp","127.0.0.1:8080")}`},
			{"局部函数值", `import("context";"net/http");func fetch(ctx context.Context){dial:=http.DefaultTransport.(*http.Transport).DialContext;_,_=dial(ctx,"tcp","127.0.0.1:8080")}`},
			{"连接后传输事实", `import("context";"net/http";"io");func fetch(ctx context.Context){conn,_:=http.DefaultTransport.(*http.Transport).DialContext(ctx,"tcp","127.0.0.1:8080");var stream io.ReadWriteCloser=conn;_,_=stream.Write([]byte("GET / HTTP/1.0\r\n\r\n"));_,_=io.ReadAll(stream);_=stream.Close()}`},
			{"匿名嵌入来源", `import("context";"net/http");type transport struct{*http.Transport};func fetch(ctx context.Context,t transport){dial:=t.DialContext;_,_=dial(ctx,"tcp","127.0.0.1:8080")}`},
		} {
			t.Run(caller+"/"+tc.name, func(t *testing.T) {
				got := identityTypedFixture(t, imports, caller, "dial_field.go", tc.body)
				closureReject(t, got, want, "dial_field.go", tc.body, "DialContext")
			})
		}
	}
}

// I2的名字任意变化，Publish/Subscribe的真实能力形状仍属于通知职责。
func Test审查本地通知函数字段和接口按角色拒绝(t *testing.T) {
	imports := identityFixtureImports(t, pkgPorts)
	notifier := imports[pkgPorts].Scope().Lookup("Notifier").Type().Underlying().(*types.Interface)
	for i := 0; i < notifier.NumMethods(); i++ {
		t.Log("真实通知契约", notifier.Method(i).Name(), types.TypeString(notifier.Method(i).Type(), nil))
	}
	for _, caller := range []string{pkgConsole, pkgEvents, pkgMachine} {
		want := ruleMigration
		if caller != pkgConsole {
			want = ruleMachine
		}
		for _, tc := range []struct{ name, body, marker string }{
			{"未调用声明", `type deps struct{Notify func(string)}`, "Notify"},
			{"直接调用", `type deps struct{Notify func(string)};func send(d deps){d.Notify("task-1")}`, `Notify("task-1")`},
			{"局部函数值", `type deps struct{Notify func(string)};func send(d deps){emit:=d.Notify;emit("task-1")}`, "Notify;"},
			{"再次改名", `type deps struct{Dispatch func(string)}`, "Dispatch"},
			{"命名函数类型", `type emitter func(string);type deps struct{Dispatch emitter}`, "Dispatch"},
			{"同形本地接口", `type sender interface{Deliver(string)}`, "Deliver"},
			{"订阅能力改名", `type deps struct{Watch func(string)(<-chan struct{},func())}`, "Watch"},
			{"包级函数值声明", `var dispatch func(string)`, "dispatch"},
		} {
			t.Run(caller+"/"+tc.name, func(t *testing.T) {
				got := identityTypedFixture(t, imports, caller, "notification_field.go", tc.body)
				closureReject(t, got, want, "notification_field.go", tc.body, tc.marker)
			})
		}
	}
}

func Test审查函数字段修复保留纯转换时钟与传输(t *testing.T) {
	imports := identityFixtureImports(t, "context", pkgExecutionPort)
	for _, caller := range []string{pkgConsole, pkgMachine, pkgEvents} {
		for _, body := range []string{
			`type clockDeps struct{Now func()int64};func at(d clockDeps)int64{return d.Now()}`,
			`type view struct{Text string};type conversions struct{ToView func(string)view}`,
			`type transport interface{Read([]byte)(int,error);Write([]byte)(int,error);Close()error;Flush()};type fields struct{Read func([]byte)(int,error);Write func([]byte)(int,error);Close func()error;Flush func()}`,
		} {
			t.Run(caller+body, func(t *testing.T) {
				if got := identityTypedFixture(t, imports, caller, "callable_positive.go", body); len(got) != 0 {
					t.Fatal("合法转换、时钟或传输被误拒", got)
				}
			})
		}
	}
	body := `import("context";p "` + pkgExecutionPort + `");func receive(ctx context.Context,r p.InstallerLogReceiver){accept:=r.ReceiveInstallerLog;_=accept(ctx,p.InstallerLog{})}`
	if got := identityTypedFixture(t, imports, pkgEvents, "receiver_positive.go", body); len(got) != 0 {
		t.Fatal("精确日志接收入口被误拒", got)
	}
}
