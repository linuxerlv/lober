package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/hosts"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	tasksport "github.com/lober/lober/internal/application/tasks/port"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/snapshot"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"github.com/lober/lober/internal/interfaces/console"
	"strings"
)

const assemblyAt int64 = 1790000000

func assemblyConfig(t *testing.T) config {
	t.Helper()
	root := t.TempDir()
	return config{Listen: "127.0.0.1:0", PG: pgConfig{DSN: pgtest.Schema(t), MaxConns: 4}, SeedAdmin: "initial password", SessionTTLHours: 12, ISO: isoConfig{Staging: filepath.Join(root, "staging"), Artifacts: filepath.Join(root, "artifacts"), SMB: filepath.Join(root, "smb"), Tools: filepath.Join(root, "tools"), LocalRoots: []string{root}, Workers: 2, PollMS: 20}}
}
func assembled(t *testing.T) (*app, config) {
	t.Helper()
	cfg := assemblyConfig(t)
	a, closeApp, err := newApp(t.Context(), cfg, console.Clock(func() int64 { return assemblyAt }), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal("assembly failed")
	}
	t.Cleanup(closeApp)
	if err := a.Initialise(); err != nil {
		t.Fatal(err)
	}
	return a, cfg
}
func rockyImage() []byte {
	return isotest.Image(map[string][]byte{
		".treeinfo":              []byte("[general]\nname=Rocky Linux\nversion=9.6\narch=x86_64\npackagedir=media/Packages\n"),
		"images/pxeboot/vmlinuz": []byte("kernel"), "images/pxeboot/initrd.img": []byte("initrd"), "images/install.img": []byte("stage2"), "media/BaseOS/repodata/repomd.xml": []byte("base"), "media/AppStream/repodata/repomd.xml": []byte("app"), "media/Packages/sample.rpm": []byte("rpm"),
	})
}
func assemblyAudit(action string) repository.Audit {
	return repository.Audit{Actor: "operator", Action: action, At: assemblyAt}
}
func waitISO(t *testing.T, st repository.ISOs, id string, state model.ISOState) model.ISO {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		item, err := st.GetISO(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if item.State == state {
			return item
		}
		if item.State == model.ISOFailed && state != model.ISOFailed {
			t.Fatal("background import failed")
		}
		if time.Now().After(deadline) {
			t.Fatal("background work timed out", item.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func Test装配初始管理员强制改密及内置内容(t *testing.T) {
	a, cfg := assembled(t)
	user, err := a.Store.GetUser(t.Context(), "admin")
	if err != nil || !user.MustChangePassword || user.CreatedAt != assemblyAt {
		t.Fatal("bootstrap missing", err)
	}
	tpl, err := a.Store.GetTemplate(t.Context(), model.TemplateKey{ID: "builtin-kickstart", Version: 3})
	if err != nil || !tpl.Builtin {
		t.Fatal("builtin missing", err)
	}
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		a.Console.Engine.ServeHTTP(w, r)
		return w
	}
	login := request("POST", "/console/v1/auth/login", `{"username":"admin","password":"`+cfg.SeedAdmin+`"}`, "")
	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if json.Unmarshal(login.Body.Bytes(), &result) != nil || login.Code != 200 || result.Data.Token == "" {
		t.Fatal("bootstrap login failed")
	}
	if w := request("GET", "/console/v1/isos", "", result.Data.Token); w.Code != 403 {
		t.Fatal("initial password bypassed", w.Code)
	}
	changed := request("PUT", "/console/v1/users/admin", `{"role":"admin","password":"changed password","rev":1}`, result.Data.Token)
	if changed.Code != 200 {
		t.Fatal(changed.Code, changed.Body.String())
	}
	if err := a.Initialise(); err != nil {
		t.Fatal(err)
	}
	after, err := a.Store.GetUser(t.Context(), "admin")
	if err != nil || after.MustChangePassword || after.Password == user.Password {
		t.Fatal("restart reseeded existing user", err)
	}
}
func Test装配后台获取导入及启动恢复(t *testing.T) {
	a, cfg := assembled(t)
	image := rockyImage()
	path := filepath.Join(cfg.ISO.LocalRoots[0], "rocky.iso")
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "real-local", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Control.Register(t.Context(), item, "path", path, "", int64(len(image)), hex.EncodeToString(sum[:]), assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	if err := a.Supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	if err := a.Supervisor.Start(); err != nil {
		t.Fatal("重复启动应成功", err)
	}
	ready := waitISO(t, a.Store, item.ID, model.ISOReady)
	if ready.Version != "9.6" || len(ready.Artifacts) == 0 {
		t.Fatal(ready)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("local original lost", err)
	}
	// Stop workers, persist a manual reimport, then use a new supervisor instance.
	a.Supervisor.Close()
	failed := ready
	failed.State = model.ISOFailed
	failed, err = a.Store.UpdateISO(t.Context(), failed)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Sources.RequestReimport(t.Context(), failed.ID, assemblyAudit("iso.reimport")); err != nil {
		t.Fatal(err)
	}
	queued, err := a.Store.GetISO(t.Context(), failed.ID)
	if err != nil || queued.State != model.ISOAcquiring || queued.Rev != failed.Rev+1 {
		t.Fatal("重启前必须已保存排队事实", queued, err)
	}
	restarted, _ := supervisorOf(t.Context(), a.Sources, a.Store, a.Deletion, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	restartedControl := appmedia.NewControl(a.Sources, restarted)
	defer restarted.Close()
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	waitISO(t, a.Store, failed.ID, model.ISOReady)
	// Recovery skips uploads awaiting client chunks; completion then imports.
	upload, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "real-upload", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	otherImage := append([]byte{}, image...)
	otherImage[7] = 1
	otherSum := sha256.Sum256(otherImage)
	if err := restartedControl.Register(t.Context(), upload, "upload", "", "", int64(len(otherImage)), hex.EncodeToString(otherSum[:]), assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	// Remove the first platform before publishing another ISO on that platform.
	if err := a.Deletion.Delete(t.Context(), failed.ID, waitISO(t, a.Store, failed.ID, model.ISOReady).Rev, assemblyAudit("iso.delete")); err != nil {
		t.Fatal(err)
	}
	if err := restartedControl.Upload(t.Context(), upload.ID, "", 0, bytes.NewReader(otherImage), repository.Audit{}); err != nil {
		t.Fatal(err)
	}
	waitISO(t, a.Store, upload.ID, model.ISOReady)
}
func TestHTTP真实监听登录及取消退出(t *testing.T) {
	a, cfg := assembled(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- serveListener(ctx, a, listener) }()
	request, err := http.NewRequestWithContext(t.Context(), "POST", "http://"+listener.Addr().String()+"/console/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"`+cfg.SeedAdmin+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	// Real PBKDF2 runs under the race detector while other packages are also
	// consuming CPU. The socket test must allow that work to finish.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 30 * time.Second}
	// 每个响应先读到结尾，再关闭，避免旧连接还在后台回收时触发额外拨号。
	readResponse := func(body io.ReadCloser) []byte {
		t.Helper()
		data, readErr := io.ReadAll(body)
		closeErr := body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		return data
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var signedIn struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeErr := json.NewDecoder(bytes.NewReader(readResponse(response.Body))).Decode(&signedIn)
	if response.StatusCode != 200 {
		t.Fatal("real listener login failed", response.StatusCode)
	}
	if decodeErr != nil || signedIn.Data.Token == "" {
		t.Fatal("login response did not contain a session", decodeErr)
	}
	for _, token := range []string{"", signedIn.Data.Token} {
		info, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+listener.Addr().String()+"/console/v1/user/info", nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			info.Header.Set("Authorization", "Bearer "+token)
		}
		got, err := client.Do(info)
		if err != nil {
			t.Fatal(err)
		}
		readResponse(got.Body)
		want := http.StatusUnauthorized
		if token != "" {
			want = http.StatusOK
		}
		if got.StatusCode != want {
			t.Fatalf("protected socket request returned %d, want %d", got.StatusCode, want)
		}
	}
	// Release this client's keep-alive connections before testing server shutdown.
	// Otherwise a speculative replacement connection can still be StateNew;
	// net/http deliberately waits five seconds before treating it as idle.
	client.CloseIdleConnections()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP service did not stop")
	}
}

func Test组件和模板应用同时组装(t *testing.T) {
	a, cfg := assembled(t)
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		a.Console.Engine.ServeHTTP(w, req)
		return w
	}
	login := request("POST", "/console/v1/auth/login", `{"username":"admin","password":"`+cfg.SeedAdmin+`"}`, "")
	var auth struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &auth); err != nil || login.Code != 200 || auth.Data.Token == "" {
		t.Fatal(login.Code, err)
	}
	token := auth.Data.Token
	changed := request("PUT", "/console/v1/users/admin", `{"role":"admin","password":"changed password","rev":1}`, token)
	if changed.Code != 200 {
		t.Fatal(changed.Code, changed.Body.String())
	}
	// 改密会退出旧会话，后续操作先用新密码登录。
	login = request("POST", "/console/v1/auth/login", `{"username":"admin","password":"changed password"}`, "")
	if err := json.Unmarshal(login.Body.Bytes(), &auth); err != nil || login.Code != 200 || auth.Data.Token == "" {
		t.Fatal(login.Code, err)
	}
	token = auth.Data.Token
	for _, kind := range []string{"hooks", "templates"} {
		w := request("POST", "/console/v1/"+kind, `{"name":"assembled-draft"}`, token)
		var got struct {
			Data struct {
				Author   string            `json:"author"`
				State    string            `json:"state"`
				Problems []json.RawMessage `json:"problems"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 {
			t.Fatal(kind, w.Code, err, w.Body.String())
		}
		if got.Data.Author != "admin" || got.Data.State != "draft" || len(got.Data.Problems) == 0 {
			t.Fatal(kind, w.Body.String())
		}
	}
}

func Test预览组装共用地址且不提前读取配置(t *testing.T) {
	cfg := config{ISO: isoConfig{Artifacts: filepath.Join(t.TempDir(), "missing")}, Preview: previewConfig{Gateway: "https://gateway.test", Artifacts: "https://artifacts.test", SMB: `\\smb.test\lober`, SyslogAddr: "udp://syslog.test:514"}}
	renderer := previewRendererOf(cfg)
	st := pgtest.OpenStore(t)
	tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: "assembly-preview", Author: "admin", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "# {{sys:task}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n"})
	if err != nil {
		t.Fatal(err)
	}
	editor := authoringEditorOf(st, renderer)
	result, err := editor.Preview(t.Context(), ports.Actor{Username: "reader", Role: model.RoleViewer}, authoring.PreviewRequest{Key: model.TemplateKey{ID: tpl.ID, Version: tpl.Version}})
	if err != nil || len(result.Files) != 2 || !strings.Contains(result.Files[0].Body, "https://gateway.test") || !strings.Contains(result.Files[1].Body, "# sample-task") {
		t.Fatal(result, err)
	}
	item := model.ISO{ID: "iso", Series: "rocky", Family: "linux", State: model.ISOReady, Version: "9", Arch: "x86_64", Artifacts: map[string]string{"kernel": "kernel", "initrd": "initrd", "stage2": "stage2", "repo": "repo"}}
	frozen, ps := snapshot.Build(params.Input{Template: tpl, ISO: item, Host: model.Host{SN: "SN-1", Arch: "x86_64"}})
	if len(ps) > 0 {
		t.Fatal(ps)
	}
	files, _, ps, err := renderer.RenderFrozen(t.Context(), frozen, catalog.FirmwareUEFI, "task-old")
	if err != nil || len(ps) > 0 || !strings.Contains(files[0].Body, "https://gateway.test") || !strings.Contains(files[1].Body, "# task-old") {
		t.Fatal(files, ps, err)
	}
}

// 只提供主机应用需要的五个方法；其他Store能力被调用就会失败。
type hostRegistryAssemblyStore struct {
	repository.Store
	calls            []string
	contexts         []context.Context
	audits           []repository.Audit
	created, updated model.Host
	deletedSN        string
	deletedRev       int64
}

func (s *hostRegistryAssemblyStore) record(ctx context.Context, action string) {
	s.calls = append(s.calls, action)
	s.contexts = append(s.contexts, ctx)
}
func (s *hostRegistryAssemblyStore) ListHosts(ctx context.Context, q repository.HostQuery) (repository.Page[model.Host], error) {
	s.record(ctx, "list")
	return repository.Page[model.Host]{Items: []model.Host{{SN: "ASSEMBLY-1", Rev: 1}}}, nil
}
func (s *hostRegistryAssemblyStore) GetHost(ctx context.Context, sn string) (model.Host, error) {
	s.record(ctx, "get")
	return model.Host{SN: sn, Rev: 1}, nil
}
func (s *hostRegistryAssemblyStore) CreateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error) {
	s.record(ctx, "create")
	s.created = h
	s.audits = append(s.audits, a)
	h.Rev = 1
	return h, nil
}
func (s *hostRegistryAssemblyStore) UpdateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error) {
	s.record(ctx, "update")
	s.updated = h
	s.audits = append(s.audits, a)
	h.Rev = 2
	return h, nil
}
func (s *hostRegistryAssemblyStore) DeleteHost(ctx context.Context, sn string, rev int64, a repository.Audit) error {
	s.record(ctx, "delete")
	s.deletedSN, s.deletedRev = sn, rev
	s.audits = append(s.audits, a)
	return nil
}

func Test主机应用组装传递仓储和时钟(t *testing.T) {
	st := &hostRegistryAssemblyStore{}
	clockCalls := 0
	registry := hostRegistryOf(st, func() int64 { clockCalls++; return assemblyAt + int64(clockCalls) })
	if clockCalls != 0 || len(st.calls) != 0 {
		t.Fatal("组装时提前访问依赖")
	}
	ctx := t.Context()
	actor := ports.Actor{Username: "operator", Role: model.RoleOperator}
	page, err := registry.List(ctx, actor, hosts.ListRequest{})
	if err != nil || len(page.Items) != 1 || page.Items[0].SN != "ASSEMBLY-1" {
		t.Fatal(page, err)
	}
	if _, err := registry.Get(ctx, actor, "ASSEMBLY-1"); err != nil {
		t.Fatal(err)
	}
	if clockCalls != 0 {
		t.Fatal("查询读取了时钟", clockCalls)
	}
	if _, err := registry.Create(ctx, actor, model.Host{SN: " assembly-1 "}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Update(ctx, actor, hosts.UpdateRequest{PathSN: "ASSEMBLY-1", Host: model.Host{Rev: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Delete(ctx, actor, hosts.DeleteRequest{SN: "ASSEMBLY-1", Rev: 2}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st.calls, []string{"list", "get", "create", "update", "delete"}) || clockCalls != 3 {
		t.Fatal(st.calls, clockCalls)
	}
	if st.created.SN != "ASSEMBLY-1" || st.updated.SN != "ASSEMBLY-1" || st.updated.Rev != 1 || st.deletedSN != "ASSEMBLY-1" || st.deletedRev != 2 {
		t.Fatal(st.created, st.updated, st.deletedSN, st.deletedRev)
	}
	for _, got := range st.contexts {
		if got != ctx {
			t.Fatal("请求上下文被替换")
		}
	}
	if !reflect.DeepEqual(st.audits, []repository.Audit{
		{Actor: "operator", Action: "host.create", At: assemblyAt + 1},
		{Actor: "operator", Action: "host.update", At: assemblyAt + 2},
		{Actor: "operator", Action: "host.delete", At: assemblyAt + 3},
	}) {
		t.Fatal(st.audits)
	}
}

func Test身份接线共用会话与凭据协调(t *testing.T) {
	if _, ok := reflect.TypeOf(console.Deps{}).FieldByName("Identity"); !ok {
		t.Fatal("正式网页尚未接入身份契约")
	}
	cfg := assemblyConfig(t)
	cfg.Machine = machineConfig{Enabled: true, KeyFile: filepath.Join(cfg.ISO.Staging, "machine.key")}
	cfg.Preview.Gateway = "http://machine.example.test:18090"
	cfg.Preview.Artifacts = "http://machine.example.test:18090/artifacts"
	a, cleanup, err := newApp(t.Context(), cfg, console.Clock(func() int64 { return assemblyAt }), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err = a.Initialise(); err != nil {
		t.Fatal(err)
	}
	field := reflect.ValueOf(a).Elem().FieldByName("Identity")
	if !field.IsValid() || field.IsNil() {
		t.Fatal("启动初始化尚未共用身份服务")
	}
	webService := reflect.ValueOf(a.Console).Elem().FieldByName("server").Elem().FieldByName("deps").FieldByName("Identity")
	if field.Elem().Pointer() != webService.Elem().Pointer() {
		t.Fatal("启动与网页创建了不同身份服务")
	}
	control := taskBackendOf(a.Machine)
	for _, call := range []func(context.Context, model.Task, ports.AuditIntent) error{control.Cancel, control.Retry} {
		if err := call(t.Context(), model.Task{ID: "missing"}, ports.AuditIntent{Actor: "admin", Action: "task.cancel", At: assemblyAt}); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal("正式任务控制没有交给原执行服务", err)
		}
	}
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		a.Console.Engine.ServeHTTP(w, r)
		return w
	}
	login := func(password string) string {
		w := request("POST", "/console/v1/auth/login", `{"username":"admin","password":"`+password+`"}`, "")
		var result struct {
			Data struct {
				Token string `json:"token"`
			} `json:"data"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.Token == "" {
			t.Fatal("真实登录失败", w.Code)
		}
		return result.Data.Token
	}
	old := login(cfg.SeedAdmin)
	if w := request("PUT", "/console/v1/users/admin", `{"role":"admin","password":"changed password","rev":1}`, old); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("GET", "/console/v1/user/info", "", old); w.Code != 401 {
		t.Fatal("改密没有撤销原令牌", w.Code)
	}
	fresh := login("changed password")
	if w := request("GET", "/console/v1/user/info", "", fresh); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if err := a.Initialise(); err != nil {
		t.Fatal(err)
	}
}

// 只实现读取口，其他总仓储方法一旦误用就会失败。
type taskReadAssemblyStore struct {
	repository.Store
	calls []string
	task  model.Task
}

func (s *taskReadAssemblyStore) GetTask(_ context.Context, id string) (model.Task, error) {
	s.calls = append(s.calls, "get:"+id)
	return s.task, nil
}
func (s *taskReadAssemblyStore) ListTasks(_ context.Context, _ repository.TaskQuery) (repository.Page[model.Task], error) {
	s.calls = append(s.calls, "list")
	return repository.Page[model.Task]{Items: []model.Task{s.task}}, nil
}

type taskReadAssemblyRenderer struct {
	calls    int
	snapshot model.Snapshot
	id       string
	firmware catalog.Firmware
}

func (r *taskReadAssemblyRenderer) RenderFrozen(_ context.Context, s model.Snapshot, f catalog.Firmware, id string) ([]ports.PreviewFile, []string, []model.Problem, error) {
	r.calls++
	r.snapshot = s
	r.id = id
	r.firmware = f
	return []ports.PreviewFile{{Name: "proof"}}, []string{"credential"}, nil, nil
}

type taskReadAssemblyControl struct {
	tasksport.Control
	calls []string
}

func (c *taskReadAssemblyControl) CanCancel(t model.Task) []model.Problem {
	c.calls = append(c.calls, "cancel:"+t.ID)
	return []model.Problem{model.NewProblem("host_has_open_task", model.Subject{})}
}
func (c *taskReadAssemblyControl) CanRetry(t model.Task) []model.Problem {
	c.calls = append(c.calls, "retry:"+t.ID)
	return nil
}
func Test任务读取和预览显式组装(t *testing.T) {
	st := &taskReadAssemblyStore{task: model.Task{ID: "stored", Snapshot: model.Snapshot{Template: model.SnapshotTemplate{Body: "saved"}, Host: model.SnapshotHost{Firmware: catalog.FirmwareUEFI}}}}
	ctl := &taskReadAssemblyControl{}
	renderer := &taskReadAssemblyRenderer{}
	reader := taskReaderOf(st, ctl, renderer)
	actor := ports.Actor{Username: "viewer", Role: model.RoleViewer}
	if len(st.calls) != 0 || len(ctl.calls) != 0 || renderer.calls != 0 {
		t.Fatal("组装提前调用依赖")
	}
	page, e := reader.List(t.Context(), actor, tasks.ListRequest{})
	if e != nil || len(page.Items) != 1 || len(page.Items[0].CanCancel) != 1 {
		t.Fatal(page, e)
	}
	item, e := reader.Get(t.Context(), actor, "raw")
	if e != nil || item.Task.ID != "stored" {
		t.Fatal(item, e)
	}
	preview, e := reader.Preview(t.Context(), actor, "lookup", tasks.PreviewRequest{})
	if e != nil || len(preview.Files) != 1 || renderer.calls != 1 || renderer.id != "stored" || !reflect.DeepEqual(renderer.snapshot, st.task.Snapshot) || renderer.firmware != catalog.FirmwareUEFI {
		t.Fatal(preview, e, renderer)
	}
	if !reflect.DeepEqual(st.calls, []string{"list", "get:raw", "get:lookup"}) || !reflect.DeepEqual(ctl.calls, []string{"cancel:stored", "retry:stored", "cancel:stored", "retry:stored"}) {
		t.Fatal(st.calls, ctl.calls)
	}
	// 使用真实Wire生成的服务验证网页没有遗漏读取与预览依赖。
	a, _ := assembled(t)
	r := httptest.NewRequest("POST", "/console/v1/auth/login", strings.NewReader(`{"username":"admin","password":"initial password"}`))
	w := httptest.NewRecorder()
	a.Console.Engine.ServeHTTP(w, r)
	var login struct {
		Data struct {
			Token string `json:"token"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &login) != nil || login.Data.Token == "" {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("PUT", "/console/v1/users/admin", strings.NewReader(`{"role":"admin","password":"changed password","rev":1}`))
	r.Header.Set("Authorization", "Bearer "+login.Data.Token)
	w = httptest.NewRecorder()
	a.Console.Engine.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "/console/v1/auth/login", strings.NewReader(`{"username":"admin","password":"changed password"}`))
	w = httptest.NewRecorder()
	a.Console.Engine.ServeHTTP(w, r)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &login) != nil || login.Data.Token == "" {
		t.Fatal(w.Code)
	}
	for _, path := range []string{"/console/v1/tasks", "/console/v1/tasks/missing", "/console/v1/tasks/missing/preview"} {
		method := "GET"
		body := ""
		want := 404
		if path == "/console/v1/tasks" {
			want = 200
		}
		if strings.HasSuffix(path, "preview") {
			method = "POST"
			body = `{"firmware":"bad"}`
			want = 400
		}
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+login.Data.Token)
		w = httptest.NewRecorder()
		a.Console.Engine.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}

// 后端已提交的提示仍保留，应用成功重读后还要补发。
type taskControlAssemblyNotifier struct{ publishes []string }

func (n *taskControlAssemblyNotifier) Publish(id string)                        { n.publishes = append(n.publishes, id) }
func (*taskControlAssemblyNotifier) Subscribe(string) (<-chan struct{}, func()) { panic("不订阅") }

type taskControlAssemblyBackend struct {
	taskReadAssemblyControl
	notifier ports.Notifier
	got      model.Task
	audit    ports.AuditIntent
}

func (b *taskControlAssemblyBackend) Cancel(_ context.Context, t model.Task, a ports.AuditIntent) error {
	b.got = t
	b.audit = a
	b.notifier.Publish(t.ID)
	return nil
}
func (b *taskControlAssemblyBackend) Retry(c context.Context, t model.Task, a ports.AuditIntent) error {
	return b.Cancel(c, t, a)
}
func Test任务控制显式共用后端时钟和通知(t *testing.T) {
	for _, retry := range []bool{false, true} {
		st := &taskReadAssemblyStore{task: model.Task{ID: "stored", Rev: 9, State: model.TaskFailed}}
		n := &taskControlAssemblyNotifier{}
		b := &taskControlAssemblyBackend{notifier: n}
		c := taskControllerOf(st, b, func() int64 { return 43 }, n)
		op := c.Cancel
		action := "task.cancel"
		if retry {
			op = c.Retry
			action = "task.retry"
		}
		got, err := op(t.Context(), ports.Actor{Username: "operator", Role: model.RoleOperator}, "raw")
		if err != nil || got.Task.ID != "stored" || len(got.CanCancel) != 1 || !reflect.DeepEqual(b.got, st.task) || b.audit != (ports.AuditIntent{Actor: "operator", Action: action, At: 43}) || !reflect.DeepEqual(st.calls, []string{"get:raw", "get:stored"}) || !reflect.DeepEqual(b.calls, []string{"cancel:stored", "retry:stored"}) || !reflect.DeepEqual(n.publishes, []string{"stored", "stored"}) {
			t.Fatal(got, err, b.audit, st.calls, b.calls, n.publishes)
		}
	}
}
func Test任务控制未启用明确不可用(t *testing.T) {
	st := &taskReadAssemblyStore{task: model.Task{ID: "stored"}}
	n := &taskControlAssemblyNotifier{}
	c := taskControllerOf(st, taskBackendOf(&machineBundle{}), func() int64 { return 44 }, n)
	for _, op := range []func(context.Context, ports.Actor, string) (tasks.TaskChoice, error){c.Cancel, c.Retry} {
		if _, err := op(t.Context(), ports.Actor{Username: "admin", Role: model.RoleAdmin}, "raw"); !errors.Is(err, tasks.ErrControlUnavailable) {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(st.calls, []string{"get:raw", "get:raw"}) || len(n.publishes) != 0 {
		t.Fatal(st.calls, n.publishes)
	}
}
