package execution_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/catalog"
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/interfaces/machine"
)

type gatewayRepo struct {
	execution.Repository
	task                             model.Task
	install, final                   string
	service                          *execution.Service
	reads, finds, writes             int
	readError, findError, writeError error
	onRead                           func(int) error
	order                            []string
}

func (f *gatewayRepo) GetTask(_ context.Context, id string) (model.Task, error) {
	f.reads++
	f.order = append(f.order, "get")
	if f.onRead != nil {
		if err := f.onRead(f.reads); err != nil {
			return model.Task{}, err
		}
	}
	if f.readError != nil {
		return model.Task{}, f.readError
	}
	if id != f.task.ID {
		return model.Task{}, repository.ErrNotFound
	}
	return f.task, nil
}
func (f *gatewayRepo) FindOpenTask(_ context.Context, sn string) (model.Task, bool, error) {
	f.finds++
	f.order = append(f.order, "find")
	if f.findError != nil {
		return model.Task{}, false, f.findError
	}
	return f.task, f.task.ID != "" && sn == f.task.HostSN, nil
}
func (f *gatewayRepo) UpdateTaskExecution(_ context.Context, id string, expect repository.TaskExpect, state model.TaskState, run *model.Execution, _ []repository.NewEvent, _ repository.Audit) (model.Task, []repository.Appended, error) {
	f.writes++
	f.order = append(f.order, "commit")
	if f.writeError != nil {
		return model.Task{}, nil, f.writeError
	}
	if id != f.task.ID || expect.Rev != f.task.Rev || expect.State != f.task.State {
		return model.Task{}, nil, repository.ErrConflict
	}
	f.task.State = state
	f.task.Execution = run
	f.task.Rev++
	return f.task, nil, nil
}
func (f *gatewayRepo) gateway(t *testing.T, render port.FrozenRenderer, observe func(model.Task, string)) execution.Gateway {
	t.Helper()
	g, err := execution.NewGateway(execution.GatewayDeps{Service: f.service, Renderer: render, Endpoints: execution.Endpoints{Gateway: "http://gateway", Artifacts: "http://artifacts", SMB: "smb", SyslogAddr: "syslog"}, Observe: observe})
	must(t, err)
	return g
}
func (f *gatewayRepo) handler(t *testing.T, render port.FrozenRenderer, observe func(model.Task, string)) http.Handler {
	return machine.New(f.gateway(t, render, observe))
}
func gatewayFixture(t *testing.T) (*gatewayRepo, http.Handler) {
	t.Helper()
	body := "echo frozen\n"
	digest := sha256.Sum256([]byte(body))
	f := &gatewayRepo{task: model.Task{ID: "task-one", HostSN: "SN-ONE", State: model.TaskBooting, Execution: &model.Execution{Attempt: 1, IssuedAt: 1000, Deadline: 1900, Signals: map[string]string{}}, Snapshot: model.Snapshot{Format: "kickstart", Series: "rocky", ISO: model.SnapshotISO{Arch: "x86_64"}, Slots: []model.SnapshotSlot{{Seq: 2, Name: "init", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{{Digest: hex.EncodeToString(digest[:]), Body: body}}}}}}}
	var err error
	f.service, err = execution.New(f, []byte(strings.Repeat("k", 32)), func() int64 { return 1000 }, nil)
	must(t, err)
	f.install, f.final = f.service.Credentials(f.task)
	return f, f.handler(t, port.RenderFunc(func(_ context.Context, r port.RenderRequest) ([]byte, error) {
		return []byte(string(r.Kind) + ":" + r.Slot), nil
	}), nil)
}
func call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}
func TestCanonicalFrozenFileRouting(t *testing.T) {
	f, h := gatewayFixture(t)
	for path, want := range map[string]string{"ks.cfg": "answer:", "scripts/2.sh": "script:init", "hooks/" + f.task.Snapshot.Slots[0].Instances[0].Digest: "echo frozen\n"} {
		w := call(h, "GET", "/v1/answer/task-one/"+f.install+"/"+path, "")
		if w.Code != 200 || w.Body.String() != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("credential response cacheable")
		}
	}
	for _, path := range []string{"../ks.cfg", "scripts/02.sh", "scripts/1.sh", "hooks/" + strings.Repeat("b", 64), "meta-data", "ks.cfg/extra"} {
		if w := call(h, "GET", "/v1/answer/task-one/"+f.install+"/"+path, ""); w.Code == 200 {
			t.Fatal("unknown file accepted", path)
		}
	}
	w := call(h, "GET", "/v1/answer/task-one/"+f.final+"/ks.cfg", "")
	if w.Code == 200 || strings.Contains(w.Body.String(), "token") {
		t.Fatal("credential rejection unsafe")
	}
	f.task.Snapshot.Slots[0].Instances[0].Body = "tampered"
	if w := call(h, "GET", "/v1/answer/task-one/"+f.install+"/hooks/"+f.task.Snapshot.Slots[0].Instances[0].Digest, ""); w.Code == 200 {
		t.Fatal("digest mismatch served")
	}
}
func TestBootWithoutTaskReturnsLocalBootAndCanonicalPlatform(t *testing.T) {
	f, h := gatewayFixture(t)
	f.task = model.Task{}
	w := call(h, "GET", "/v1/boot?sn=SN-ONE&arch=i386&firmware=pcbios", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "sanboot") {
		t.Fatal("missing local boot", w.Code, w.Body)
	}
	w = call(h, "GET", "/v1/boot?sn=SN-ONE&arch=unknown&firmware=bios", "")
	if w.Code == 200 {
		t.Fatal("invalid architecture accepted")
	}
}

// D1：Debian 安装器在 auto 模式下把 DHCP option 67 的 bootfile 地址当 preseed
// 下载并覆盖内核命令行；网关对 debian-installer 客户端在 /v1/boot 上直接发
// preseed 应答，其余客户端保持 iPXE 菜单。
func TestBootServesPreseedAnswerToDebianInstallerAgent(t *testing.T) {
	f, h := gatewayFixture(t)
	f.task.Snapshot.Format = "preseed"
	callUA := func(agent, sn string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/boot?sn="+sn+"&arch=x86_64&firmware=bios", nil)
		if agent != "" {
			req.Header.Set("User-Agent", agent)
		}
		h.ServeHTTP(w, req)
		return w
	}
	if w := callUA("iPXE/1.21.1+git-20220113", "SN-ONE"); w.Code != 200 || !strings.Contains(w.Body.String(), "menu:") {
		t.Fatal("iPXE agent lost the menu", w.Code, w.Body)
	}
	if w := callUA("", "SN-ONE"); w.Code != 200 || !strings.Contains(w.Body.String(), "menu:") {
		t.Fatal("unknown agent must keep the iPXE menu behavior", w.Code, w.Body)
	}
	if w := callUA("debian-installer", "SN-ONE"); w.Code != 200 || !strings.Contains(w.Body.String(), "answer:") {
		t.Fatal("debian-installer agent did not get the preseed answer", w.Code, w.Body)
	}
	// 不给安装：无任务、别的 SN、非 preseed 格式都保持本地盘引导。
	f.task.Snapshot.Format = "kickstart"
	if w := callUA("debian-installer", "SN-ONE"); w.Code != 200 || !strings.Contains(w.Body.String(), "sanboot") {
		t.Fatal("non-preseed format must not install", w.Code, w.Body)
	}
	f.task.Snapshot.Format = "preseed"
	if w := callUA("debian-installer", "SN-OTHER"); w.Code != 200 || !strings.Contains(w.Body.String(), "sanboot") {
		t.Fatal("unknown SN must not install", w.Code, w.Body)
	}
	f.task = model.Task{}
	if w := callUA("debian-installer", "SN-ONE"); w.Code != 200 || !strings.Contains(w.Body.String(), "sanboot") {
		t.Fatal("no task must not install", w.Code, w.Body)
	}
}

func Test机器观察只在凭据检查通过后调用(t *testing.T) {
	f, _ := gatewayFixture(t)
	calls := 0
	h := f.handler(t, port.RenderFunc(func(context.Context, port.RenderRequest) ([]byte, error) { return []byte("answer"), nil }), func(task model.Task, address string) {
		calls++
		if task.ID != f.task.ID || address != "192.0.2.1:1234" {
			t.Fatal("观察字段改变")
		}
	})
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/v1/answer/task-one/bad/ks.cfg", 0},
		{"/v1/answer/task-one/" + f.install + "/ks.cfg", 1},
		{"/v1/answer/task-one/bad/scripts/2.sh", 1},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("X-Forwarded-For", "198.51.100.9")
		h.ServeHTTP(httptest.NewRecorder(), r)
		if calls != tc.want {
			t.Fatal("未认证请求产生观察", calls, tc.want)
		}
	}
}

func Test机器入口错误来源与状态保持(t *testing.T) {
	secret := errors.New("secret cause")
	for _, tc := range []struct {
		name, path  string
		want        int
		kind        execution.GatewayFailureKind
		configure   func(*gatewayRepo)
		renderError bool
	}{
		{name: "file初次读库", path: "file", want: 403, kind: execution.Unauthorized, configure: func(f *gatewayRepo) { f.readError = secret }},
		{name: "Preseed读错", path: "installer", want: 503, kind: execution.LookupUnavailable, configure: func(f *gatewayRepo) { f.findError = secret }},
		{name: "Boot读错", path: "boot", want: 503, kind: execution.LookupUnavailable, configure: func(f *gatewayRepo) { f.findError = secret }},
		{name: "最终Answer读错", path: "file", want: 409, kind: execution.AnswerConflict, configure: func(f *gatewayRepo) {
			f.onRead = func(n int) error {
				if n == 2 {
					return secret
				}
				return nil
			}
		}},
		{name: "Installer最终Answer失败", path: "installer", want: 409, kind: execution.AnswerConflict, configure: func(f *gatewayRepo) { f.readError = secret }},
		{name: "渲染失败", path: "file", want: 422, kind: execution.RenderRejected, renderError: true},
		{name: "未知文件", path: "missing", want: 404, kind: execution.FileNotFound},
		{name: "Signal读错", path: "signal", want: 409, kind: execution.SignalConflict, configure: func(f *gatewayRepo) { f.readError = secret }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, transport := range []bool{false, true} {
				f, _ := gatewayFixture(t)
				f.task.Snapshot.Format = "preseed"
				if tc.configure != nil {
					tc.configure(f)
				}
				renderer := port.RenderFunc(func(context.Context, port.RenderRequest) ([]byte, error) {
					if tc.renderError {
						return nil, secret
					}
					return []byte("secret body"), nil
				})
				g := f.gateway(t, renderer, nil)
				if transport {
					path := "/v1/answer/task-one/" + f.install + "/preseed.cfg"
					method := "GET"
					agent := ""
					switch tc.path {
					case "boot":
						path = "/v1/boot?sn=SN-ONE&arch=x86_64&firmware=bios"
					case "installer":
						path = "/v1/boot?sn=SN-ONE&arch=x86_64&firmware=bios"
						agent = "debian-installer"
					case "missing":
						path = "/v1/answer/task-one/" + f.install + "/unknown"
					case "signal":
						path = "/v1/signal/task-one/" + f.install + "/installed"
						method = "POST"
					}
					w := httptest.NewRecorder()
					r := httptest.NewRequest(method, path, nil)
					r.Header.Set("User-Agent", agent)
					machine.New(g).ServeHTTP(w, r)
					if w.Code != tc.want || w.Body.String() != http.StatusText(tc.want)+"\n" {
						t.Fatalf("状态/正文改变: %d %q", w.Code, w.Body.String())
					}
				} else {
					var err error
					switch tc.path {
					case "boot":
						_, err = g.BootMenu(t.Context(), execution.BootInput{SN: "SN-ONE", Arch: "x86_64", Firmware: "bios"})
					case "installer":
						_, err = g.InstallerAnswer(t.Context(), execution.InstallerAnswerInput{SN: "SN-ONE"})
					case "signal":
						err = g.Signal(t.Context(), execution.SignalInput{TaskID: f.task.ID, Token: f.install, Signal: executionrules.Signal{Phase: "installed"}})
					default:
						name := "preseed.cfg"
						if tc.path == "missing" {
							name = "unknown"
						}
						_, err = g.File(t.Context(), execution.FileInput{TaskID: f.task.ID, Token: f.install, Name: name})
					}
					var failure *execution.GatewayError
					if !errors.As(err, &failure) || failure.Kind != tc.kind {
						t.Fatalf("来源分类改变: %v", err)
					}
					if tc.path != "missing" && !errors.Is(err, secret) {
						t.Fatal("原错误链丢失", err)
					}
				}
			}
		})
	}
}

func Test机器应答渲染后取消不泄出正文(t *testing.T) {
	for _, installer := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "installer"}[installer], func(t *testing.T) {
			f, _ := gatewayFixture(t)
			f.task.Snapshot.Format = "preseed"
			observed := 0
			h := f.handler(t, port.RenderFunc(func(ctx context.Context, _ port.RenderRequest) ([]byte, error) {
				must(t, f.service.Cancel(ctx, f.task, repository.Audit{Actor: "tester", Action: "task.cancel", At: 1000}))
				return []byte("private rendered answer"), nil
			}), func(model.Task, string) { observed++ })
			path := "/v1/answer/task-one/" + f.install + "/preseed.cfg"
			if installer {
				path = "/v1/boot?sn=SN-ONE&arch=x86_64&firmware=bios"
			}
			r := httptest.NewRequest("GET", path, nil)
			if installer {
				r.Header.Set("User-Agent", "debian-installer")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 409 || w.Body.String() != "Conflict\n" || f.task.State != model.TaskCancelled {
				t.Fatal("取消未胜出或正文泄出", w.Code, w.Body.String(), f.task.State)
			}
			want := 1
			if installer {
				want = 0
			}
			if observed != want {
				t.Fatal("取消后观察位置改变", observed, want)
			}
		})
	}
}

func Test机器入口保留观察时机与双重验证(t *testing.T) {
	for _, name := range []string{"file未知文件已观察", "file摘要失败已观察", "file渲染失败已观察", "installer渲染失败不观察", "installerAnswer失败不观察", "signal观察验证失败仍处理", "signal观察成功最终失败", "signal观察nil不额外查库"} {
		t.Run(name, func(t *testing.T) {
			f, _ := gatewayFixture(t)
			observed := 0
			observer := func(task model.Task, remote string) {
				observed++
				f.order = append(f.order, "observe")
				if task.ID != f.task.ID || remote != "192.0.2.1:1234" {
					t.Fatal("观察内容改变")
				}
			}
			render := port.RenderFunc(func(context.Context, port.RenderRequest) ([]byte, error) {
				f.order = append(f.order, "render")
				if strings.Contains(name, "渲染失败") {
					return nil, errors.New("render")
				}
				return []byte("answer"), nil
			})
			if name == "signal观察nil不额外查库" {
				observer = nil
			}
			g := f.gateway(t, render, observer)
			var err error
			switch {
			case strings.HasPrefix(name, "file"):
				file := "unknown"
				if strings.Contains(name, "渲染") {
					file = "ks.cfg"
				}
				if strings.Contains(name, "摘要") {
					file = "hooks/" + f.task.Snapshot.Slots[0].Instances[0].Digest
					f.task.Snapshot.Slots[0].Instances[0].Body = "tampered"
				}
				_, err = g.File(t.Context(), execution.FileInput{TaskID: f.task.ID, Token: f.install, Name: file, RemoteAddr: "192.0.2.1:1234"})
				if err == nil || observed != 1 {
					t.Fatal("文件失败前未观察", err, observed)
				}
			case strings.HasPrefix(name, "installer"):
				f.task.Snapshot.Format = "preseed"
				if strings.Contains(name, "Answer") {
					f.readError = errors.New("answer read")
				}
				_, err = g.InstallerAnswer(t.Context(), execution.InstallerAnswerInput{SN: f.task.HostSN, RemoteAddr: "192.0.2.1:1234"})
				if err == nil || observed != 0 {
					t.Fatal("安装器失败仍观察", err, observed)
				}
			default:
				f.task.State = model.TaskInstalling
				f.task.Execution.Stage = "packages"
				if strings.Contains(name, "验证失败") {
					f.onRead = func(n int) error {
						if n == 1 {
							return errors.New("observation read")
						}
						return nil
					}
				}
				if strings.Contains(name, "最终失败") {
					f.onRead = func(n int) error {
						if n == 2 {
							return errors.New("signal read")
						}
						return nil
					}
				}
				err = g.Signal(t.Context(), execution.SignalInput{TaskID: f.task.ID, Token: f.install, RemoteAddr: "192.0.2.1:1234", Signal: executionrules.Signal{Phase: "installed"}})
				wantReads := 2
				if observer == nil {
					wantReads = 1
				}
				if f.reads != wantReads {
					t.Fatal("观察与Signal验证合并或nil多读", f.reads, wantReads)
				}
				if strings.Contains(name, "最终失败") {
					if err == nil || observed != 1 || f.writes != 0 {
						t.Fatal("最终验证未独立执行", err, observed, f.writes)
					}
				} else if err != nil || f.task.State != model.TaskRebooting || observed != 0 {
					t.Fatal("观察失败阻止了Signal", err, f.task.State, observed)
				}
			}
		})
	}
}

func Test机器交付顺序与冻结值(t *testing.T) {
	for _, installer := range []bool{false, true} {
		f, _ := gatewayFixture(t)
		f.task.Snapshot.Format = "preseed"
		render := port.RenderFunc(func(ctx context.Context, r port.RenderRequest) ([]byte, error) {
			f.order = append(f.order, "render")
			if r.Task.ID != f.task.ID || r.Values["token"] != f.install || r.Values["finalize-token"] != f.final || r.Values["gateway"] != "http://gateway" || r.Values["artifacts"] != "http://artifacts" || r.Values["smb"] != "smb" || r.Values["syslog-addr"] != "syslog" || r.Values["task"] != f.task.ID || r.Values["sn"] != f.task.HostSN {
				t.Fatal("冻结渲染值改变", r.Values)
			}
			if !installer && f.writes != 1 {
				t.Fatal("Boot在提交前渲染")
			}
			return []byte("frozen"), nil
		})
		g := f.gateway(t, render, func(model.Task, string) { f.order = append(f.order, "observe") })
		var result execution.BootResult
		var err error
		want := "find,commit,render"
		if installer {
			result, err = g.InstallerAnswer(t.Context(), execution.InstallerAnswerInput{SN: f.task.HostSN})
			want = "find,render,get,commit,observe"
		} else {
			result, err = g.BootMenu(t.Context(), execution.BootInput{SN: f.task.HostSN, Arch: "x86_64", Firmware: "bios"})
		}
		must(t, err)
		if !result.Found || string(result.Body) != "frozen" || strings.Join(f.order, ",") != want {
			t.Fatal("顺序改变", result, f.order, want)
		}
	}
}

func Test机器交付构造拒绝缺必需依赖(t *testing.T) {
	f, _ := gatewayFixture(t)
	render := port.RenderFunc(func(context.Context, port.RenderRequest) ([]byte, error) { return nil, nil })
	for _, deps := range []execution.GatewayDeps{{}, {Renderer: render}, {Service: f.service}, {Service: f.service, Renderer: port.RenderFunc(nil)}} {
		if g, err := execution.NewGateway(deps); err == nil || g != nil {
			t.Fatal("缺失依赖仍构造成功")
		}
	}
	if _, err := execution.NewGateway(execution.GatewayDeps{Service: f.service, Renderer: render}); err != nil {
		t.Fatal("nil观察或空地址被新增限制", err)
	}
}

func Test冻结组件完整摘要检查不提前交付(t *testing.T) {
	f, _ := gatewayFixture(t)
	original := f.task.Snapshot.Slots[0].Instances[0]
	f.task.Snapshot.Slots = append(f.task.Snapshot.Slots, model.SnapshotSlot{Exec: true, Instances: []model.SnapshotInstance{{Digest: original.Digest, Body: "later corrupt"}}})
	rendered := 0
	g := f.gateway(t, port.RenderFunc(func(context.Context, port.RenderRequest) ([]byte, error) { rendered++; return nil, nil }), nil)
	body, err := g.File(t.Context(), execution.FileInput{TaskID: f.task.ID, Token: f.install, Name: "hooks/" + original.Digest})
	var failure *execution.GatewayError
	if body != nil || !errors.As(err, &failure) || failure.Kind != execution.RenderRejected || !errors.Is(err, executionrules.ErrFrozenContent) || rendered != 0 || f.writes != 0 {
		t.Fatal("提前交付匹配正文", body, err, rendered, f.writes)
	}
	empty := sha256.Sum256(nil)
	f.task.Snapshot.Slots = []model.SnapshotSlot{{Exec: true, Instances: []model.SnapshotInstance{{Digest: hex.EncodeToString(empty[:])}}}}
	body, err = g.File(t.Context(), execution.FileInput{TaskID: f.task.ID, Token: f.install, Name: "hooks/" + hex.EncodeToString(empty[:])})
	if err != nil || len(body) != 0 || rendered != 0 || f.writes != 0 {
		t.Fatal("空组件被误判为待渲染", body, err, rendered, f.writes)
	}
}
func Test渲染端口与错误链原样委托(t *testing.T) {
	type marker struct{}
	ctx := context.WithValue(t.Context(), marker{}, "value")
	cause := errors.New("private cause")
	render := port.RenderFunc(func(actual context.Context, r port.RenderRequest) ([]byte, error) {
		if actual != ctx || r.Slot != "slot" || r.Values["task"] != "task" {
			t.Fatal("渲染端口改了输入")
		}
		return []byte("body"), cause
	})
	body, err := render.Render(ctx, port.RenderRequest{Slot: "slot", Values: map[catalog.SystemValue]string{"task": "task"}})
	if string(body) != "body" || err != cause {
		t.Fatal("渲染端口改了结果")
	}
	failure := &execution.GatewayError{Kind: execution.AnswerConflict, Cause: cause}
	if failure.Error() != string(execution.AnswerConflict) || failure.Unwrap() != cause || !errors.Is(failure, cause) {
		t.Fatal("错误链或固定类别改变")
	}
}

func Test信号重复安装边界观察失败仍独立接受(t *testing.T) {
	f, _ := gatewayFixture(t)
	f.task.State = model.TaskInstalling
	f.task.Execution.Stage = "packages"
	observed := 0
	g := f.gateway(t, port.RenderFunc(func(context.Context, port.RenderRequest) ([]byte, error) {
		t.Fatal("信号不应渲染")
		return nil, nil
	}), func(model.Task, string) { observed++ })
	in := execution.SignalInput{TaskID: f.task.ID, Token: f.install, Signal: executionrules.Signal{Phase: "installed"}}
	must(t, g.Signal(t.Context(), in))
	if observed != 1 || f.task.State != model.TaskRebooting {
		t.Fatal("首次安装边界未接受", observed, f.task.State)
	}
	observed = 0
	f.reads = 0
	f.writes = 0
	must(t, g.Signal(t.Context(), in))
	if observed != 0 || f.reads != 2 || f.writes != 0 {
		t.Fatal("重复安装边界合并了不同验证", observed, f.reads, f.writes)
	}
	must(t, g.Signal(t.Context(), execution.SignalInput{TaskID: f.task.ID, Token: f.final, Signal: executionrules.Signal{Phase: "booted"}}))
	if observed != 1 {
		t.Fatal("收尾观察未使用收尾凭据", observed)
	}
}
