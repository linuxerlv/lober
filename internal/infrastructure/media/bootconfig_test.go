package iso

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"os"
	"path/filepath"
	"testing"
)

func TestESXi配置读取原文且不能逃离制品根(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "esxi", "7.0", "x86_64", "install")
	if err := os.MkdirAll(repo, 0750); err != nil {
		t.Fatal(err)
	}
	raw := "title=original\nmodules=a --- nested/b\nunknown=preserved\n"
	if err := os.WriteFile(filepath.Join(repo, "boot.cfg"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	reader := NewBootConfigReader(base)
	snapshot := model.SnapshotISO{Artifacts: map[string]string{"repo": "esxi/7.0/x86_64/install/"}}
	got, err := reader.ReadESXiBootConfig(t.Context(), snapshot)
	if err != nil || got != raw {
		t.Fatal(got, err)
	}
	snapshot.Artifacts["repo"] = "../outside"
	if _, err := reader.ReadESXiBootConfig(t.Context(), snapshot); err == nil {
		t.Fatal("traversal accepted")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "escaped")); err != nil {
		t.Fatal(err)
	}
	snapshot.Artifacts["repo"] = "escaped"
	if _, err := reader.ReadESXiBootConfig(t.Context(), snapshot); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestESXi配置已取消不返回内容(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := NewBootConfigReader(t.TempDir()).ReadESXiBootConfig(ctx, model.SnapshotISO{Artifacts: map[string]string{"repo": "esxi"}})
	if got != "" || !errors.Is(err, context.Canceled) {
		t.Fatal(got, err)
	}
}
