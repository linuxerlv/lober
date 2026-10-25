package machine

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/execution"
	executionrules "github.com/lober/lober/internal/domain/execution"
)

type fixtureGateway struct {
	received  executionrules.Signal
	boot      execution.BootInput
	installer execution.InstallerAnswerInput
	file      execution.FileInput
	signal    execution.SignalInput
	result    execution.BootResult
	body      []byte
	err       error
}

func (f *fixtureGateway) BootMenu(_ context.Context, in execution.BootInput) (execution.BootResult, error) {
	f.boot = in
	return f.result, f.err
}
func (f *fixtureGateway) InstallerAnswer(_ context.Context, in execution.InstallerAnswerInput) (execution.BootResult, error) {
	f.installer = in
	return f.result, f.err
}
func (f *fixtureGateway) File(_ context.Context, in execution.FileInput) ([]byte, error) {
	f.file = in
	return f.body, f.err
}
func (f *fixtureGateway) Signal(_ context.Context, in execution.SignalInput) error {
	f.signal = in
	f.received = in.Signal
	return f.err
}
func gatewayFixture(t *testing.T) (*fixtureGateway, http.Handler) {
	t.Helper()
	f := &fixtureGateway{}
	return f, New(f)
}
func call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}
func TestBoundedBinarySignalAndStrictQuery(t *testing.T) {
	f, h := gatewayFixture(t)
	url := "/v1/signal/task-one/install-token/hook-report?slot=2&timing=firstboot&instance=1&rc=7&digest=" + strings.Repeat("a", 64)
	w := call(h, "POST", url, "a\x00b")
	if w.Code != 200 || string(f.received.Body) != "a\x00b" || f.received.RC != 7 {
		t.Fatal("binary signal", w.Code, w.Body)
	}
	for _, query := range []string{"?slot=0&timing=firstboot&rc=0", "?slot=2&timing=firstboot&rc=0&rc=1", "?unknown=secret", "?slot=2&timing=firstboot&instance=1&rc=not-number"} {
		if w := call(h, "POST", "/v1/signal/task-one/install-token/hook-report"+query, ""); w.Code == 200 {
			t.Fatal("bad query accepted", query)
		}
	}
	if w := call(h, "POST", url, strings.Repeat("x", 4097)); w.Code != 413 {
		t.Fatal("unbounded body", w.Code)
	}
	if w := call(h, "GET", url, ""); w.Code != 405 {
		t.Fatal("GET mutates signal", w.Code)
	}
}
func Test机器HTTP只解析输出(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "http.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			switch s.Sel.Name {
			case "Snapshot", "Execution", "Credentials", "GetAuthorized", "Answer", "Preseed", "Sum256":
				t.Errorf("HTTP仍编排任务选择、摘要或推进: %s %s", s.Sel.Name, fset.Position(s.Pos()))
			}
		}
		return true
	})
}

func Test机器HTTP按来源类别映射而不猜原因(t *testing.T) {
	entries := []struct {
		path, method, agent string
		fallback            int
	}{{"/v1/boot?sn=SN-ONE&arch=x86_64&firmware=bios", "GET", "", 503}, {"/v1/boot?sn=SN-ONE&arch=x86_64&firmware=bios", "GET", "debian-installer", 503}, {"/v1/answer/task-one/token/ks.cfg", "GET", "", 403}, {"/v1/signal/task-one/token/installed", "POST", "", 409}}
	kinds := []struct {
		kind   execution.GatewayFailureKind
		status int
	}{{execution.LookupUnavailable, 503}, {execution.RenderRejected, 422}, {execution.Unauthorized, 403}, {execution.FileNotFound, 404}, {execution.AnswerConflict, 409}, {execution.SignalConflict, 409}, {"unknown", 0}}
	for _, entry := range entries {
		for _, tc := range kinds {
			status := tc.status
			if status == 0 {
				status = entry.fallback
			}
			f := &fixtureGateway{err: &execution.GatewayError{Kind: tc.kind, Cause: execution.ErrCredential}}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(entry.method, entry.path, nil)
			r.Header.Set("User-Agent", entry.agent)
			New(f).ServeHTTP(w, r)
			if w.Code != status || w.Body.String() != http.StatusText(status)+"\n" {
				t.Fatal("按底层错误猜状态或泄露原因", entry, tc, w.Code, w.Body.String())
			}
		}
		f := &fixtureGateway{err: errors.New("secret unclassified")}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(entry.method, entry.path, nil)
		r.Header.Set("User-Agent", entry.agent)
		New(f).ServeHTTP(w, r)
		if w.Code != entry.fallback || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("入口兜底改变", w.Code, w.Body.String())
		}
	}
}
func Test机器HTTP保留输入输出(t *testing.T) {
	f, h := gatewayFixture(t)
	f.result = execution.BootResult{Found: true, Body: []byte("body")}
	r := httptest.NewRequest("GET", "/v1/boot?sn=Sn-One&arch=i386&firmware=pcbios", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if f.boot.SN != "Sn-One" || f.boot.Arch != "x86_64" || f.boot.Firmware != "bios" || w.Body.String() != "body" {
		t.Fatal("普通菜单解析或输出改变", f.boot, w.Body.String())
	}
	for _, agent := range []string{"", "unknown", "Debian-installer", "debian-other"} {
		f.boot = execution.BootInput{}
		r := httptest.NewRequest("GET", "/v1/boot?sn=SN-ONE&arch=x86_64&firmware=bios", nil)
		r.Header.Set("User-Agent", agent)
		h.ServeHTTP(httptest.NewRecorder(), r)
		if f.boot.SN != "SN-ONE" {
			t.Fatal("UA识别扩大", agent)
		}
	}
	r = httptest.NewRequest("GET", "/v1/boot?sn=SN-ONE&arch=x86_64&firmware=bios", nil)
	r.RemoteAddr = "192.0.2.1:12"
	r.Header.Set("User-Agent", " debian-installer/1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if f.installer.SN != "SN-ONE" || f.installer.RemoteAddr != r.RemoteAddr {
		t.Fatal("安装器转交字段改变", f.installer)
	}
	f.result = execution.BootResult{Body: []byte("must not leak")}
	w = call(h, "GET", "/v1/boot?sn=SN-ONE&arch=x86_64&firmware=bios", "")
	if w.Body.String() != "#!ipxe\nexit || sanboot --no-describe --drive 0x80\n" {
		t.Fatal("无任务本地盘正文改变", w.Body.String())
	}
	f.body = []byte("answer")
	r = httptest.NewRequest("GET", "/v1/answer/task-one/token/scripts/2.sh", nil)
	r.RemoteAddr = "192.0.2.1:12"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if f.file.TaskID != "task-one" || f.file.Token != "token" || f.file.Name != "scripts/2.sh" || f.file.RemoteAddr != r.RemoteAddr || w.Body.String() != "answer" {
		t.Fatal("文件转交字段改变", f.file, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatal("响应头改变", w.Header())
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"POST", "/v1/boot", 405}, {"GET", "/v1/boot?sn=a&sn=b&arch=x86_64&firmware=bios", 400}, {"GET", "/v1/boot?sn=a&arch=bad&firmware=bios", 400}, {"GET", "/v1/boot?sn=a&arch=x86_64&firmware=bad", 400}, {"GET", "/v1/boot?sn=a&arch=x86_64&firmware=bios&unknown=x", 400}, {"GET", "/v1/answer/task-one/token/ks.cfg?q=1", 400}, {"POST", "/v1/answer/task-one/token/ks.cfg", 405}, {"GET", "/v1/answer/task-one/token/../ks.cfg", 404}, {"POST", "/v1/signal/task-one/token/installed/extra", 404}, {"GET", "/v1/nope/task-one/token/x", 404}} {
		if w := call(h, tc.method, tc.path, ""); w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
}
