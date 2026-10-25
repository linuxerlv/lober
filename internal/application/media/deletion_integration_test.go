package media_test

import (
	"bytes"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"os"
	"path/filepath"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test删除快照写失败保留数据及清理失败可重启重试(t *testing.T) {
	a, cfg := assembledDeletion(t)
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "cleanup-failure", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	image := rockyImage()
	if err := a.Control.Register(t.Context(), item, "upload", "", "", int64(len(image)), "", assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	if err := a.Control.Upload(t.Context(), item.ID, "", 0, bytes.NewReader(image), repository.Audit{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sources.Import(t.Context(), item.ID); err != nil {
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
	actualDir := filepath.Join(cfg.ISO.Staging, "deletions")
	savedJournal := actualDir + ".saved"
	if err := os.Rename(actualDir, savedJournal); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actualDir, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Deletion.Delete(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")); err == nil {
		t.Fatal("snapshot write failure accepted")
	}
	if err := os.Remove(actualDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(savedJournal, actualDir); err != nil {
		t.Fatal(err)
	}
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
	after, err = a.Store.ListAudit(t.Context(), repository.AuditQuery{TargetKind: "iso", TargetID: item.ID, Action: "iso.delete"})
	if err != nil || len(after.Items) != len(before.Items)+1 {
		t.Fatal("清理失败仍须保留已提交的删除审计", after, err)
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
	restarted := a.newDeletion(a.Journal)
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
	afterRetry, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{TargetKind: "iso", TargetID: item.ID, Action: "iso.delete"})
	if err != nil || len(afterRetry.Items) != len(after.Items) {
		t.Fatal("重试清理不能重复记录删除审计", afterRetry, err)
	}
}

func Test持久化删除快照先于删除及重启重试(t *testing.T) {
	a, cfg := assembledDeletion(t)
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "delete-upload", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Control.Register(t.Context(), item, "upload", "", "", 4, "", assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	if err := a.Control.Upload(t.Context(), item.ID, "", 0, bytes.NewReader([]byte("data")), repository.Audit{}); err != nil {
		t.Fatal(err)
	}
	sources, err := a.Store.SourcesOf(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := port.DeletionSnapshot{ISO: item, Sources: sources}
	if err := a.Journal.Persist(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	if err := a.Deletion.RetryAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.ISO.Staging, item.ID)); err != nil {
		t.Fatal("cleanup ran before committed deletion", err)
	}
	// Simulate the crash boundary: journal persisted, DB deletion committed.
	if err := a.Journal.Persist(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.DeleteISO(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")); err != nil {
		t.Fatal(err)
	}
	restarted := a.newDeletion(a.Journal)
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
