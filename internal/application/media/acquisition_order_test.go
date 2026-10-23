package media_test

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func awaitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("等待顺序确认超时")
	}
}
func awaitResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(10 * time.Second):
		t.Fatal("等待任务退出超时")
		return nil
	}
}

type observedSources struct {
	port.Sources
	beforeSave func(model.Source) error
}

func (s *observedSources) SaveSource(ctx context.Context, src model.Source, a repository.Audit) (model.Source, error) {
	if s.beforeSave != nil {
		if e := s.beforeSave(src); e != nil {
			return model.Source{}, e
		}
	}
	return s.Sources.SaveSource(ctx, src, a)
}

type heldTransfer struct {
	port.Transfer
	verified, release         chan struct{}
	published, publishRelease chan struct{}
	closes                    atomic.Int32
}

func (s *heldTransfer) BeginPublish(ctx context.Context, src model.Source) (port.FetchSession, error) {
	f, e := s.Transfer.BeginPublish(ctx, src)
	if e != nil {
		return nil, e
	}
	return &heldSession{FetchSession: f, owner: s}, nil
}

type heldSession struct {
	port.FetchSession
	owner *heldTransfer
}

func (s *heldSession) Verify(ctx context.Context) (port.VerifiedContent, error) {
	v, e := s.FetchSession.Verify(ctx)
	if e != nil {
		return v, e
	}
	close(s.owner.verified)
	select {
	case <-s.owner.release:
		return v, nil
	case <-ctx.Done():
		return v, ctx.Err()
	}
}
func (s *heldSession) Publish(ctx context.Context) error {
	if e := s.FetchSession.Publish(ctx); e != nil {
		return e
	}
	if s.owner.published != nil {
		close(s.owner.published)
		select {
		case <-s.owner.publishRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (s *heldSession) Close() error { s.owner.closes.Add(1); return s.FetchSession.Close() }
func Test镜像取消与最后片发布两方向竞争(t *testing.T) {
	for _, name := range []string{"取消先提交", "改名后取消先提交", "完成先提交"} {
		cancelFirst := name != "完成先提交"
		t.Run(name, func(t *testing.T) {
			db := pgtest.Open(t)
			audit := repository.Audit{Actor: "source-test", Action: "iso.source", At: 1}
			item, e := db.CreateISO(t.Context(), model.ISO{Name: "race", Family: "linux", Series: "rocky"}, audit)
			if e != nil {
				t.Fatal(e)
			}
			dir := t.TempDir()
			transfer := &heldTransfer{Transfer: iso.NewTransfer(iso.Dirs{Staging: dir}), verified: make(chan struct{}), release: make(chan struct{})}
			if name == "改名后取消先提交" {
				transfer.published = make(chan struct{})
				transfer.publishRelease = make(chan struct{})
			}
			sources := &observedSources{Sources: db}
			service := media.NewSources(media.SourceDeps{ISOs: db, Sources: sources, Transfer: transfer, Locks: iso.NewSourceLocks(), Gate: iso.NewRegistrationGate()})
			if e = service.Register(t.Context(), item, "upload", "", "", 4, "", audit); e != nil {
				t.Fatal(e)
			}
			uploaded := make(chan error, 1)
			go func() {
				uploaded <- service.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), repository.Audit{})
			}()
			awaitSignal(t, transfer.verified)
			before, _, e := db.GetSource(t.Context(), item.ID, "")
			if e != nil || before.State != model.SourceFetching || before.Fetched != 4 {
				t.Fatal(before, e)
			}
			if cancelFirst {
				if transfer.published != nil {
					close(transfer.release)
					awaitSignal(t, transfer.published)
				}
				if e = service.Cancel(t.Context(), item.ID, audit); e != nil {
					t.Fatal(e)
				}
				if transfer.published != nil {
					close(transfer.publishRelease)
				} else {
					close(transfer.release)
				}
				if e = awaitResult(t, uploaded); !errors.Is(e, repository.ErrConflict) {
					t.Fatal("旧完成必须被拒绝", e)
				}
				current, _, e := db.GetSource(t.Context(), item.ID, "")
				if e != nil || current.State != model.SourceCancelled || current.Rev != before.Rev+1 {
					t.Fatal(current, e)
				}
				before.State = model.SourceFetched
				if _, e = db.SaveSource(t.Context(), before, repository.Audit{}); !errors.Is(e, repository.ErrConflict) {
					t.Fatal("旧版本覆盖了取消", e)
				}
				if _, e = os.Stat(filepath.Join(dir, item.ID)); !os.IsNotExist(e) {
					t.Fatal("取消后发布了成品", e)
				}
				b, e := os.ReadFile(filepath.Join(dir, item.ID+".part"))
				if e != nil || string(b) != "abcd" {
					t.Fatal(string(b), e)
				}
			} else {
				entered, release := make(chan struct{}), make(chan struct{})
				sources.beforeSave = func(src model.Source) error {
					if src.State == model.SourceCancelled {
						close(entered)
						select {
						case <-release:
						case <-t.Context().Done():
							return t.Context().Err()
						}
					}
					return nil
				}
				cancelled := make(chan error, 1)
				go func() { cancelled <- service.Cancel(t.Context(), item.ID, audit) }()
				awaitSignal(t, entered) // 取消已读到旧版本，但还没有提交。
				close(transfer.release)
				if e = awaitResult(t, uploaded); e != nil {
					t.Fatal(e)
				}
				close(release)
				if e = awaitResult(t, cancelled); e != nil {
					t.Fatal(e)
				}
				current, _, e := db.GetSource(t.Context(), item.ID, "")
				if e != nil || current.State != model.SourceFetched || current.Rev != before.Rev+1 {
					t.Fatal(current, e)
				}
				if e = service.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), repository.Audit{}); e != nil {
					t.Fatal("完成重放失败", e)
				}
				if e = service.Upload(t.Context(), item.ID, "", 1, strings.NewReader("XX"), repository.Audit{}); e == nil {
					t.Fatal("错误重放未拒绝")
				}
				after, _, e := db.GetSource(t.Context(), item.ID, "")
				if e != nil || after != current {
					t.Fatal("重放修改了来源", after, e)
				}
				spans, e := db.RangesOf(t.Context(), item.ID, "")
				if e != nil || len(spans) != 1 || spans[0] != (model.Range{Start: 0, Length: 4}) {
					t.Fatal(spans, e)
				}
				b, e := os.ReadFile(filepath.Join(dir, item.ID))
				if e != nil || string(b) != "abcd" {
					t.Fatal("重放修改了字节", string(b), e)
				}
			}
			if transfer.closes.Load() != 1 {
				t.Fatal("发布会话关闭次数", transfer.closes.Load())
			}
		})
	}
}

type scriptedTransfer struct {
	port.Transfer
	session  *scriptedSession
	beginErr error
}

func (s scriptedTransfer) BeginFetch(context.Context, model.Source) (port.FetchSession, error) {
	if s.beginErr != nil {
		return nil, s.beginErr
	}
	return s.session, nil
}

type scriptedSession struct {
	stage                       string
	resume                      int64
	closed, restored, published int
	stepError                   error
}

var injected = errors.New("受控会话失败")

func (s *scriptedSession) Resume(context.Context) (int64, error) {
	if s.stage == "resume" {
		return 0, injected
	}
	return s.resume, nil
}
func (s *scriptedSession) Open(context.Context) (port.FetchInfo, error) {
	if s.stage == "open" {
		return port.FetchInfo{}, injected
	}
	if s.stage == "reset" {
		return port.FetchInfo{Reset: true, TotalSize: 4}, &port.Fault{Kind: port.URLRejected}
	}
	return port.FetchInfo{Offset: s.resume, TotalSize: 4}, nil
}
func (s *scriptedSession) Step(context.Context) (int64, bool, error) {
	if s.stage == "step" {
		return 3, false, io.ErrUnexpectedEOF
	}
	return 4, true, nil
}
func (s *scriptedSession) Verify(context.Context) (port.VerifiedContent, error) {
	if s.stage == "verify" {
		return port.VerifiedContent{}, injected
	}
	if s.stage == "size" {
		return port.VerifiedContent{Size: 3}, nil
	}
	return port.VerifiedContent{Size: 4, SHA256: "digest"}, nil
}
func (s *scriptedSession) Publish(context.Context) error {
	s.published++
	if s.stage == "publish" {
		return injected
	}
	return nil
}
func (s *scriptedSession) Restore(context.Context) error { s.restored++; return nil }
func (s *scriptedSession) Close() error                  { s.closed++; return nil }
func Test镜像获取会话每条退出路径关闭(t *testing.T) {
	for _, stage := range []string{"begin", "resume", "resume-save", "open", "reset", "open-save", "step", "step-save", "verify", "size", "publish", "final-save", "success"} {
		t.Run(stage, func(t *testing.T) {
			db := pgtest.Open(t)
			audit := repository.Audit{Actor: "source-test", Action: "iso.source", At: 1}
			item, e := db.CreateISO(t.Context(), model.ISO{Name: "close", Family: "linux", Series: "rocky"}, audit)
			if e != nil {
				t.Fatal(e)
			}
			_, e = db.SaveSource(t.Context(), model.Source{ISOID: item.ID, Kind: model.SourceRemote, URL: "http://mirror.example/image", TotalSize: 4, State: model.SourcePending}, audit)
			if e != nil {
				t.Fatal(e)
			}
			session := &scriptedSession{stage: stage}
			if stage == "resume-save" {
				session.resume = 2
			}
			tr := scriptedTransfer{session: session}
			if stage == "begin" {
				tr.beginErr = injected
			}
			sources := &observedSources{Sources: db}
			sources.beforeSave = func(src model.Source) error {
				if stage == "resume-save" && src.Fetched == 2 || stage == "open-save" && src.State == model.SourceFetching && src.Rev == 2 || stage == "step-save" && src.Fetched == 4 && src.State == model.SourceFetching || stage == "final-save" && src.State == model.SourceFetched {
					return injected
				}
				return nil
			}
			service := media.NewSources(media.SourceDeps{ISOs: db, Sources: sources, Transfer: tr, Locks: iso.NewSourceLocks(), Gate: iso.NewRegistrationGate()})
			e = service.FetchOne(t.Context(), item.ID, "")
			if stage == "success" && e != nil || stage != "success" && e == nil {
				t.Fatal(stage, e)
			}
			wantClose := 1
			if stage == "begin" {
				wantClose = 0
			}
			if session.closed != wantClose {
				t.Fatal("关闭次数", session.closed, wantClose)
			}
			if stage == "final-save" && session.restored != 1 {
				t.Fatal("保存失败没有恢复断点")
			}
			if stage != "success" {
				current, _, readErr := db.GetSource(t.Context(), item.ID, "")
				if readErr != nil || current.State != model.SourceFailed {
					t.Fatal("外部失败没有落在原来源", current, readErr)
				}
			}
			if stage == "step" {
				current, _, e := db.GetSource(t.Context(), item.ID, "")
				if e != nil || current.Fetched != 3 || !strings.Contains(current.Error, "unexpected EOF") {
					t.Fatal("读取失败丢失已同步位置或错误", current, e)
				}
			}
		})
	}
}

type noLocalParking struct{ port.Transfer }

func (noLocalParking) ParkRetry(context.Context, model.Source) (port.RetryFiles, error) {
	return nil, errors.New("本地原件不应进入移走字节的流程")
}
func Test镜像重试本地来源不要求托管文件能力(t *testing.T) {
	db := pgtest.Open(t)
	a := repository.Audit{Actor: "source-test", Action: "iso.source", At: 1}
	item, e := db.CreateISO(t.Context(), model.ISO{Name: "local", Family: "linux", Series: "rocky"}, a)
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	path := filepath.Join(root, "original.iso")
	if e = os.WriteFile(path, []byte("abcd"), 0600); e != nil {
		t.Fatal(e)
	}
	service := media.NewSources(media.SourceDeps{ISOs: db, Sources: db, Transfer: noLocalParking{iso.NewTransfer(iso.Dirs{LocalRoots: []string{root}})}, Locks: iso.NewSourceLocks(), Gate: iso.NewRegistrationGate()})
	if e = service.Register(t.Context(), item, "path", path, "", 4, "", a); e != nil {
		t.Fatal(e)
	}
	if e = service.Cancel(t.Context(), item.ID, a); e != nil {
		t.Fatal(e)
	}
	if e = service.Retry(t.Context(), item.ID, a); e != nil {
		t.Fatal(e)
	}
	src, ok, e := db.GetSource(t.Context(), item.ID, "")
	if e != nil || !ok || src.State != model.SourcePending || src.Fetched != 0 {
		t.Fatal(src, e)
	}
	b, e := os.ReadFile(path)
	if e != nil || string(b) != "abcd" {
		t.Fatal(string(b), e)
	}
}
