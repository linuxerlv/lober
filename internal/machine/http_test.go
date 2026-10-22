package machine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

type fixtureEngine struct {
	task     model.Task
	received Signal
}

func (f *fixtureEngine) Boot(context.Context, string, catalog.Arch, catalog.Firmware) (model.Task, bool, error) {
	return f.task, f.task.ID != "", nil
}
func (f *fixtureEngine) Preseed(_ context.Context, sn string) (model.Task, bool, error) {
	if f.task.ID == "" || sn != f.task.HostSN {
		return model.Task{}, false, nil
	}
	return f.task, true, nil
}
func (f *fixtureEngine) GetAuthorized(_ context.Context, id, token string, final bool) (model.Task, error) {
	if id != f.task.ID || token != "install-token" || final {
		return model.Task{}, errors.New("credential")
	}
	return f.task, nil
}
func (f *fixtureEngine) Answer(ctx context.Context, id, token string) (model.Task, error) {
	return f.GetAuthorized(ctx, id, token, false)
}
func (f *fixtureEngine) Signal(_ context.Context, id, token string, sig Signal) error {
	if id != f.task.ID || token != "install-token" {
		return errors.New("credential")
	}
	f.received = sig
	return nil
}
func (f *fixtureEngine) Credentials(model.Task) (string, string) {
	return "install-token", "final-token"
}
func gatewayFixture(t *testing.T) (*fixtureEngine, http.Handler) {
	t.Helper()
	body := "echo frozen\n"
	digest := sha256.Sum256([]byte(body))
	f := &fixtureEngine{task: model.Task{ID: "task-one", HostSN: "SN-ONE", Snapshot: model.Snapshot{Format: "kickstart", Slots: []model.SnapshotSlot{{Seq: 2, Name: "init", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{{Digest: hex.EncodeToString(digest[:]), Body: body}}}}}}}
	return f, New(f, func(r Request) ([]byte, error) { return []byte(string(r.Kind) + ":" + r.Slot), nil }, Endpoints{Gateway: "http://gateway", Artifacts: "http://artifacts"})
}
func call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}
func TestCanonicalFrozenFileRouting(t *testing.T) {
	f, h := gatewayFixture(t)
	for path, want := range map[string]string{"ks.cfg": "answer:", "scripts/2.sh": "script:init", "hooks/" + f.task.Snapshot.Slots[0].Instances[0].Digest: "echo frozen\n"} {
		w := call(h, "GET", "/v1/answer/task-one/install-token/"+path, "")
		if w.Code != 200 || w.Body.String() != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("credential response cacheable")
		}
	}
	for _, path := range []string{"../ks.cfg", "scripts/02.sh", "scripts/1.sh", "hooks/" + strings.Repeat("b", 64), "meta-data", "ks.cfg/extra"} {
		if w := call(h, "GET", "/v1/answer/task-one/install-token/"+path, ""); w.Code == 200 {
			t.Fatal("unknown file accepted", path)
		}
	}
	w := call(h, "GET", "/v1/answer/task-one/final-token/ks.cfg", "")
	if w.Code == 200 || strings.Contains(w.Body.String(), "token") {
		t.Fatal("credential rejection unsafe")
	}
	f.task.Snapshot.Slots[0].Instances[0].Body = "tampered"
	if w := call(h, "GET", "/v1/answer/task-one/install-token/hooks/"+f.task.Snapshot.Slots[0].Instances[0].Digest, ""); w.Code == 200 {
		t.Fatal("digest mismatch served")
	}
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
