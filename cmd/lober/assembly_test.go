package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/snapshot"
	"github.com/lober/lober/internal/infrastructure/media"
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
	if err := a.Supervisor.Register(t.Context(), item, "path", path, "", int64(len(image)), hex.EncodeToString(sum[:]), assemblyAudit("iso.source.register")); err != nil {
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
	if err := a.Supervisor.source.RequestReimport(t.Context(), failed.ID, assemblyAudit("iso.reimport")); err != nil {
		t.Fatal(err)
	}
	queued, err := a.Store.GetISO(t.Context(), failed.ID)
	if err != nil || queued.State != model.ISOAcquiring || queued.Rev != failed.Rev+1 {
		t.Fatal("重启前必须已保存排队事实", queued, err)
	}
	restarted := newSourceSupervisor(t.Context(), a.Supervisor.source, a.Native, a.Store, a.Deletion, a.Gate, cfg.ISO, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	if err := restarted.Register(t.Context(), upload, "upload", "", "", int64(len(otherImage)), hex.EncodeToString(otherSum[:]), assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	// Remove the first platform before publishing another ISO on that platform.
	if err := a.Deletion.Delete(t.Context(), failed.ID, waitISO(t, a.Store, failed.ID, model.ISOReady).Rev, assemblyAudit("iso.delete")); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Upload(t.Context(), upload.ID, "", 0, bytes.NewReader(otherImage), repository.Audit{}); err != nil {
		t.Fatal(err)
	}
	waitISO(t, a.Store, upload.ID, model.ISOReady)
}
func Test持久化删除快照先于删除及重启重试(t *testing.T) {
	a, cfg := assembled(t)
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "delete-upload", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Supervisor.Register(t.Context(), item, "upload", "", "", 4, "", assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	if err := a.Supervisor.Upload(t.Context(), item.ID, "", 0, bytes.NewReader([]byte("data")), repository.Audit{}); err != nil {
		t.Fatal(err)
	}
	sources, err := a.Store.SourcesOf(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := iso.DeletionSnapshot{ISO: item, Sources: sources}
	if err := a.Deletion.persist(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := a.Deletion.RetryAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.ISO.Staging, item.ID)); err != nil {
		t.Fatal("cleanup ran before committed deletion", err)
	}
	// Simulate the crash boundary: journal persisted, DB deletion committed.
	if err := a.Deletion.persist(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.DeleteISO(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")); err != nil {
		t.Fatal(err)
	}
	restarted, err := newDeletionJournal(a.Store, a.Native, a.Gate, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RetryAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.ISO.Staging, item.ID)); !os.IsNotExist(err) {
		t.Fatal("bytes remained", err)
	}
	if err := restarted.RetryAll(t.Context()); err != nil {
		t.Fatal("cleanup is not idempotent", err)
	}
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
