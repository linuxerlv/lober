package main

import (
	"bytes"
	"github.com/lober/lober/internal/application/ports"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestEnabledMachineAssemblyPersistsProtectedKeyAndServesCanonicalRoute(t *testing.T) {
	cfg := assemblyConfig(t)
	cfg.Machine = machineConfig{Enabled: true, KeyFile: filepath.Join(cfg.ISO.Staging, "machine.key")}
	cfg.Preview.Gateway = "http://192.168.56.2:18090"
	cfg.Preview.Artifacts = "http://192.168.56.2:18090/artifacts"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clock := ports.Clock(func() int64 { return assemblyAt })
	a, cleanup, err := newApp(t.Context(), cfg, clock, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Initialise(); err != nil {
		cleanup()
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/boot?sn=UNREGISTERED&arch=x86_64&firmware=bios", nil))
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("#!ipxe")) {
		cleanup()
		t.Fatal("machine gateway not assembled", w.Code, w.Body)
	}
	key, err := os.ReadFile(cfg.Machine.KeyFile)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	info, err := os.Stat(cfg.Machine.KeyFile)
	if err != nil || len(key) != 32 || info.Mode().Perm() != 0600 {
		cleanup()
		t.Fatal("signing key is not protected")
	}
	cleanup()
	b, cleanup, err := newApp(t.Context(), cfg, clock, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	again, err := os.ReadFile(cfg.Machine.KeyFile)
	if err != nil || !bytes.Equal(key, again) || b.Machine == nil {
		t.Fatal("restart changed signing key")
	}
}
func TestEnabledMachineRejectsInvalidEndpointOrUnprotectedKey(t *testing.T) {
	for _, mode := range []string{"missing-endpoint", "query-endpoint", "short-key", "public-key", "symlink-key"} {
		t.Run(mode, func(t *testing.T) {
			cfg := assemblyConfig(t)
			cfg.Machine = machineConfig{Enabled: true, KeyFile: filepath.Join(t.TempDir(), "machine.key")}
			cfg.Preview.Gateway = "http://gateway"
			cfg.Preview.Artifacts = "http://artifacts"
			switch mode {
			case "missing-endpoint":
				cfg.Preview.Gateway = ""
			case "query-endpoint":
				cfg.Preview.Gateway = "http://gateway?token=bad"
			case "short-key":
				if err := os.WriteFile(cfg.Machine.KeyFile, []byte("short"), 0600); err != nil {
					t.Fatal(err)
				}
			case "public-key":
				if err := os.WriteFile(cfg.Machine.KeyFile, bytes.Repeat([]byte("x"), 32), 0644); err != nil {
					t.Fatal(err)
				}
				// WriteFile's mode is filtered by the caller's umask. Make
				// the unsafe fixture explicit even in a private test runner.
				if err := os.Chmod(cfg.Machine.KeyFile, 0644); err != nil {
					t.Fatal(err)
				}
				if info, err := os.Stat(cfg.Machine.KeyFile); err != nil || info.Mode().Perm() != 0644 {
					t.Fatal("public-key fixture is not public", err)
				}
			case "symlink-key":
				target := cfg.Machine.KeyFile + ".target"
				if err := os.WriteFile(target, bytes.Repeat([]byte("x"), 32), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, cfg.Machine.KeyFile); err != nil {
					t.Fatal(err)
				}
			}
			_, cleanup, err := newApp(t.Context(), cfg, ports.Clock(func() int64 { return assemblyAt }), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if cleanup != nil {
				cleanup()
			}
			if err == nil {
				t.Fatal("unsafe machine configuration accepted")
			}
		})
	}
}

func Test机器关闭不读取密钥启用使用原默认路径(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clock := ports.Clock(func() int64 { return assemblyAt })
	disabled, err := machineOf(nil, nil, config{}, clock, logger)
	if err != nil || disabled.Service != nil || disabled.Handler != nil || disabled.Logs != nil {
		t.Fatal("关闭分支改变", err)
	}
	cfg := assemblyConfig(t)
	cfg.Machine = machineConfig{Enabled: true}
	cfg.Preview.Gateway = "http://gateway"
	cfg.Preview.Artifacts = "http://artifacts"
	bundle, err := machineOf(nil, nil, cfg, clock, logger)
	if err != nil || bundle.Service == nil || bundle.Handler == nil || bundle.Logs == nil {
		t.Fatal("默认路径未组装", err)
	}
	info, err := os.Stat(filepath.Join(cfg.ISO.Staging, "machine-signing.key"))
	if err != nil || info.Mode().Perm() != 0600 || info.Size() != 32 {
		t.Fatal("默认密钥路径或权限改变", err)
	}
}
