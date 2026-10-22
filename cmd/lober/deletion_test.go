package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test删除快照写失败保留数据及清理失败可重启重试(t *testing.T) {
	a, cfg := assembled(t)
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "cleanup-failure", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	image := rockyImage()
	if err := a.Supervisor.Register(t.Context(), item, "upload", "", "", int64(len(image)), "", assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	if err := a.Supervisor.Upload(t.Context(), item.ID, "", 0, bytes.NewReader(image), repository.Audit{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Native.Import(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	item, err = a.Store.GetISO(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{TargetKind: "iso", TargetID: item.ID, Action: "iso.delete"})
	if err != nil {
		t.Fatal(err)
	}
	actualDir := a.Deletion.dir
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	a.Deletion.dir = blocked
	if err := a.Deletion.Delete(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")); err == nil {
		t.Fatal("snapshot write failure accepted")
	}
	a.Deletion.dir = actualDir
	if _, err := a.Store.GetISO(t.Context(), item.ID); err != nil {
		t.Fatal("snapshot failure removed row", err)
	}
	after, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{TargetKind: "iso", TargetID: item.ID, Action: "iso.delete"})
	if err != nil || len(before.Items) != len(after.Items) {
		t.Fatal("snapshot failure audited", err)
	}
	tree := filepath.Join(cfg.ISO.Artifacts, "rocky", "9.6", "x86_64")
	saved := tree + ".saved"
	if err := os.Rename(tree, saved); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep")
	if err := os.WriteFile(sentinel, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, tree); err != nil {
		t.Fatal(err)
	}
	if err := a.Deletion.Delete(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.GetISO(t.Context(), item.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("delete not committed", err)
	}
	if _, err := os.Stat(filepath.Join(actualDir, item.ID+".json")); err != nil {
		t.Fatal("failed cleanup lost durable journal", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("cleanup escaped root", err)
	}
	if err := os.Remove(tree); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, tree); err != nil {
		t.Fatal(err)
	}
	restarted, err := newDeletionJournal(a.Store, a.Native, a.Gate, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RetryAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatal("managed bytes remained", err)
	}
	if _, err := os.Stat(filepath.Join(actualDir, item.ID+".json")); !os.IsNotExist(err) {
		t.Fatal("completed journal remained", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("outside bytes changed", err)
	}
}

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
	reader := bootConfigReader{root: base}
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
