package media_test

import (
	"bytes"
	"context"
	"errors"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 屏障在真实保存前暂停，数据库、字节和快照仍由真实实现处理。
type sourceSaveBarrier struct {
	port.Sources
	entered, release chan struct{}
}

func (s *sourceSaveBarrier) SaveSource(ctx context.Context, src model.Source, a repository.Audit) (model.Source, error) {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return model.Source{}, ctx.Err()
	}
	return s.Sources.SaveSource(ctx, src, a)
}

type journalBarrier struct {
	port.DeletionJournal
	entered, release chan struct{}
}

func (j *journalBarrier) Persist(ctx context.Context, s port.DeletionSnapshot) error {
	if err := j.DeletionJournal.Persist(ctx, s); err != nil {
		return err
	}
	close(j.entered)
	select {
	case <-j.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func waitSignal(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(5 * time.Second):
		t.Fatal("屏障未到达")
	}
}
func newDeleteWith(t *testing.T, a *deletionFixture, isos port.ISOs, j port.DeletionJournal) *appmedia.Deletion {
	t.Helper()
	return appmedia.NewDeletion(appmedia.DeletionDeps{ISOs: isos, Sources: a.Store, Journal: j, Files: iso.NewDeletedFiles(a.Dirs), Locks: a.Locks, Gate: a.Gate, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

type observedGate struct {
	port.RegistrationGate
	readAttempt, writeAttempt chan struct{}
}

func (g *observedGate) RLock() { close(g.readAttempt); g.RegistrationGate.RLock() }
func (g *observedGate) Lock()  { close(g.writeAttempt); g.RegistrationGate.Lock() }

func Test删除和补来源按两种先后次序保护真实文件(t *testing.T) {
	for _, registerFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "先登记", false: "先删除"}[registerFirst], func(t *testing.T) {
			a, _ := assembledDeletion(t)
			item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "order", Family: "linux", Series: "debian"}, assemblyAudit("iso.create"))
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(a.Dirs.Staging, item.ID+".netboot")
			if err := os.WriteFile(p, []byte("extra"), 0600); err != nil {
				t.Fatal(err)
			}
			registered, deleted := make(chan error, 1), make(chan error, 1)
			gate := &observedGate{RegistrationGate: a.Gate, readAttempt: make(chan struct{}), writeAttempt: make(chan struct{})}
			makeDeletion := func(j port.DeletionJournal) *appmedia.Deletion {
				return appmedia.NewDeletion(appmedia.DeletionDeps{ISOs: a.Store, Sources: a.Store, Journal: j, Files: iso.NewDeletedFiles(a.Dirs), Locks: a.Locks, Gate: gate, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			}
			if registerFirst {
				barrier := &sourceSaveBarrier{Sources: a.Store, entered: make(chan struct{}), release: make(chan struct{})}
				sources := appmedia.NewSources(appmedia.SourceDeps{ISOs: a.Store, Sources: barrier, Transfer: iso.NewTransfer(a.Dirs), Import: iso.NewImportFiles(a.Dirs), Locks: a.Locks, Gate: gate})
				go func() {
					registered <- sources.Register(t.Context(), item, "upload", "", "netboot", 5, "", assemblyAudit("iso.source.register"))
				}()
				waitSignal(t, barrier.entered)
				go func() {
					deleted <- makeDeletion(a.Journal).Delete(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete"))
				}()
				waitSignal(t, gate.writeAttempt)
				close(barrier.release)
				if err := <-registered; err != nil {
					t.Fatal(err)
				}
			} else {
				barrier := &journalBarrier{DeletionJournal: a.Journal, entered: make(chan struct{}), release: make(chan struct{})}
				deletion := makeDeletion(barrier)
				go func() { deleted <- deletion.Delete(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")) }()
				waitSignal(t, barrier.entered)
				if _, err := a.Store.GetISO(t.Context(), item.ID); err != nil {
					t.Fatal("快照确认前已删除记录", err)
				}
				snapshots, err := a.Journal.List(t.Context())
				if err != nil || len(snapshots) != 1 || len(snapshots[0].Sources) != 0 {
					t.Fatal("删除前快照不完整", snapshots, err)
				}
				audits, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{TargetID: item.ID, Action: "iso.delete"})
				if err != nil || len(audits.Items) != 0 {
					t.Fatal("快照确认前出现删除审计", audits, err)
				}
				go func() {
					registered <- appmedia.NewSources(appmedia.SourceDeps{ISOs: a.Store, Sources: a.Store, Transfer: iso.NewTransfer(a.Dirs), Import: iso.NewImportFiles(a.Dirs), Locks: a.Locks, Gate: gate}).Register(t.Context(), item, "upload", "", "netboot", 5, "", assemblyAudit("iso.source.register"))
				}()
				waitSignal(t, gate.readAttempt)
				close(barrier.release)
				if err := <-registered; !errors.Is(err, repository.ErrNotFound) {
					t.Fatal("删除后仍登记来源", err)
				}
			}
			if err := <-deleted; err != nil {
				t.Fatal(err)
			}
			if _, err := a.Store.GetISO(t.Context(), item.ID); !errors.Is(err, repository.ErrNotFound) {
				t.Fatal(err)
			}
			if _, exists, err := a.Store.GetSource(t.Context(), item.ID, "netboot"); err != nil || exists {
				t.Fatal(exists, err)
			}
			_, fileErr := os.Stat(p)
			if registerFirst && !os.IsNotExist(fileErr) {
				t.Fatal("快照没有覆盖已登记来源", fileErr)
			}
			if !registerFirst && fileErr != nil {
				t.Fatal("删除了未登记的其他字节", fileErr)
			}
			audits, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{TargetID: item.ID, Action: "iso.source.register"})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if registerFirst {
				want = 1
			}
			if len(audits.Items) != want {
				t.Fatal(audits.Items)
			}
			snapshots, err := a.Journal.List(t.Context())
			if err != nil || len(snapshots) != 0 {
				t.Fatal(snapshots, err)
			}
		})
	}
}

type lostDeleteAck struct {
	port.ISOs
	unknown bool
}

func (s *lostDeleteAck) DeleteISO(ctx context.Context, id string, rev int64, a repository.Audit) error {
	if err := s.ISOs.DeleteISO(ctx, id, rev, a); err != nil {
		return err
	}
	s.unknown = true
	return errors.New("commit acknowledgement lost")
}
func (s *lostDeleteAck) GetISO(ctx context.Context, id string) (model.ISO, error) {
	if s.unknown {
		return model.ISO{}, errors.New("read unavailable")
	}
	return s.ISOs.GetISO(ctx, id)
}
func Test删除丢确认保留字节快照并在重启后清理(t *testing.T) {
	a, _ := assembledDeletion(t)
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "lost-ack", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Control.Register(t.Context(), item, "upload", "", "", 4, "", assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	if err := a.Control.Upload(t.Context(), item.ID, "", 0, bytes.NewBufferString("data"), repository.Audit{}); err != nil {
		t.Fatal(err)
	}
	deletion := newDeleteWith(t, a, &lostDeleteAck{ISOs: a.Store}, a.Journal)
	if err := deletion.Delete(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")); err == nil {
		t.Fatal("丢确认未返回错误")
	}
	if _, err := a.Store.GetISO(t.Context(), item.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("没有真实提交", err)
	}
	path := filepath.Join(a.Dirs.Staging, item.ID)
	if b, err := os.ReadFile(path); err != nil || string(b) != "data" {
		t.Fatal("未知结果删除了字节", err)
	}
	snapshots, err := a.Journal.List(t.Context())
	if err != nil || len(snapshots) != 1 {
		t.Fatal(snapshots, err)
	}
	if err := a.newDeletion(a.Journal).RetryAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	snapshots, err = a.Journal.List(t.Context())
	if err != nil || len(snapshots) != 0 {
		t.Fatal(snapshots, err)
	}
	audits, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{TargetID: item.ID, Action: "iso.delete"})
	if err != nil || len(audits.Items) != 1 {
		t.Fatal(audits, err)
	}
}

type deletionReadBarrier struct {
	port.ISOs
	reads   int
	entered chan struct{}
}

func (s *deletionReadBarrier) GetISO(ctx context.Context, id string) (model.ISO, error) {
	s.reads++
	if s.reads == 2 {
		close(s.entered)
	}
	if s.reads == 3 {
		return model.ISO{}, errors.New("confirmation unavailable")
	}
	return s.ISOs.GetISO(ctx, id)
}
func Test等导入锁后必须再次确认删除(t *testing.T) {
	a, _ := assembledDeletion(t)
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "confirm", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Sources.Register(t.Context(), item, "upload", "", "", 4, "", assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	sources, err := a.Store.SourcesOf(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.Dirs.Staging, item.ID)
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Journal.Persist(t.Context(), port.DeletionSnapshot{ISO: item, Sources: sources}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.DeleteISO(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")); err != nil {
		t.Fatal(err)
	}
	unlock := a.Locks.Lock(item.ID, "@import")
	reads := &deletionReadBarrier{ISOs: a.Store, entered: make(chan struct{})}
	deletion := newDeleteWith(t, a, reads, a.Journal)
	done := make(chan error, 1)
	go func() { done <- deletion.RetryAll(t.Context()) }()
	waitSignal(t, reads.entered)
	if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
		t.Fatal(err)
	}
	unlock()
	if err := <-done; err == nil {
		t.Fatal("没有第二次确认")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
		t.Fatal(err)
	}
	snapshots, err := a.Journal.List(t.Context())
	if err != nil || len(snapshots) != 1 {
		t.Fatal(snapshots, err)
	}
	if err := a.Deletion.RetryAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func Test坏快照不会阻止其他删除恢复(t *testing.T) {
	a, _ := assembledDeletion(t)
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "valid-neighbor", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Sources.Register(t.Context(), item, "upload", "", "", 4, "", assemblyAudit("iso.source.register")); err != nil {
		t.Fatal(err)
	}
	sources, err := a.Store.SourcesOf(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.Dirs.Staging, item.ID)
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Journal.Persist(t.Context(), port.DeletionSnapshot{ISO: item, Sources: sources}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.DeleteISO(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(a.Dirs.Staging, "deletions", "aaa-bad.json")
	if err := os.WriteFile(badPath, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	invalid := item
	invalid.ID = "aaa-invalid"
	if err := a.Journal.Persist(t.Context(), port.DeletionSnapshot{ISO: invalid, Sources: sources}); err != nil {
		t.Fatal(err)
	}
	if err := a.Deletion.RetryAll(t.Context()); err == nil {
		t.Fatal("坏日志未报错")
	}
	for _, p := range []string{path, filepath.Join(a.Dirs.Staging, "deletions", item.ID+".json")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatal("有效删除被坏日志阻塞", p, err)
		}
	}
	if _, err := os.Stat(badPath); err != nil {
		t.Fatal("坏日志被吞掉", err)
	}
	audits, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{TargetID: item.ID, Action: "iso.delete"})
	if err != nil || len(audits.Items) != 1 {
		t.Fatal(audits, err)
	}
}

type discardFailure struct{ port.DeletionJournal }

func (discardFailure) Discard(context.Context, string) error {
	return errors.New("directory sync unavailable")
}
func Test已提交删除丢弃日志失败仍成功且可重试(t *testing.T) {
	a, _ := assembledDeletion(t)
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "discard", Family: "linux", Series: "rocky"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	d := a.newDeletion(discardFailure{a.Journal})
	if err := d.Delete(t.Context(), item.ID, item.Rev, assemblyAudit("iso.delete")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.GetISO(t.Context(), item.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
	snapshots, err := a.Journal.List(t.Context())
	if err != nil || len(snapshots) != 1 {
		t.Fatal(snapshots, err)
	}
	if err := a.Deletion.RetryAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshots, err = a.Journal.List(t.Context())
	if err != nil || len(snapshots) != 0 {
		t.Fatal(snapshots, err)
	}
	audits, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{TargetID: item.ID, Action: "iso.delete"})
	if err != nil || len(audits.Items) != 1 {
		t.Fatal(audits, err)
	}
}
