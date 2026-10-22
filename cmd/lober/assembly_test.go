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
	"testing"
	"time"

	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/iso"
	"github.com/lober/lober/internal/iso/isotest"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
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
func assemblyAudit(action string) store.Audit {
	return store.Audit{Actor: "operator", Action: action, At: assemblyAt}
}
func waitISO(t *testing.T, st store.ISOs, id string, state model.ISOState) model.ISO {
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
	if err := a.Native.RequestReimport(t.Context(), failed.ID, assemblyAudit("iso.reimport")); err != nil {
		t.Fatal(err)
	}
	restarted := newSourceSupervisor(t.Context(), a.Native, a.Store, a.Deletion, a.Gate, cfg.ISO, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	if err := restarted.Upload(t.Context(), upload.ID, "", 0, bytes.NewReader(otherImage), store.Audit{}); err != nil {
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
	if err := a.Supervisor.Upload(t.Context(), item.ID, "", 0, bytes.NewReader([]byte("data")), store.Audit{}); err != nil {
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
	client := http.Client{Timeout: 30 * time.Second, Transport: http.DefaultTransport.(*http.Transport).Clone()}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var signedIn struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&signedIn)
	if decodeErr == nil {
		_, decodeErr = io.Copy(io.Discard, response.Body)
	}
	response.Body.Close()
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
		if _, err := io.Copy(io.Discard, got.Body); err != nil {
			got.Body.Close()
			t.Fatal(err)
		}
		got.Body.Close()
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
