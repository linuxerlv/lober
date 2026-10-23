package media_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 包装真实文件能力，只在指定边界放入受控故障。
type importStepFiles struct {
	port.ImportFiles
	wrap func(port.ImportSession) port.ImportSession
}

func (f importStepFiles) Open(ctx context.Context, item model.ISO) (port.ImportSession, error) {
	s, err := f.ImportFiles.Open(ctx, item)
	if err != nil {
		return nil, err
	}
	return f.wrap(s), nil
}

type importSteps struct {
	port.ImportSession
	begin     func() error
	prepared  func()
	verify    func()
	published func(port.Publication, error)
	view      func(port.MediaView) port.MediaView
}

func (s *importSteps) OpenSource(ctx context.Context, src model.Source) (port.MediaView, error) {
	v, err := s.ImportSession.OpenSource(ctx, src)
	if err == nil && s.view != nil {
		v = s.view(v)
	}
	return v, err
}
func (s *importSteps) BeginPrepare(ctx context.Context, p port.ExtractionPlan) error {
	if s.begin != nil {
		if err := s.begin(); err != nil {
			return err
		}
	}
	return s.ImportSession.BeginPrepare(ctx, p)
}
func (s *importSteps) Prepare(ctx context.Context, p port.ExtractionPlan) (map[string]string, error) {
	v, err := s.ImportSession.Prepare(ctx, p)
	if err == nil && s.prepared != nil {
		s.prepared()
	}
	return v, err
}
func (s *importSteps) Verify(ctx context.Context) error {
	if s.verify != nil {
		s.verify()
	}
	return s.ImportSession.Verify(ctx)
}
func (s *importSteps) Publish(ctx context.Context) (port.Publication, error) {
	p, err := s.ImportSession.Publish(ctx)
	if s.published != nil {
		s.published(p, err)
	}
	return p, err
}

type countedMedia struct {
	port.MediaView
	entries *int
}

func (v countedMedia) Entries(ctx context.Context, name string) ([]port.Entry, error) {
	*v.entries++
	return v.MediaView.Entries(ctx, name)
}

func Test镜像临时目录失败先于Debian检查(t *testing.T) {
	s, db := importSetup(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "prepare first", Family: "linux", Series: "debian"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	files := importFixture("debian")
	delete(files, "pool/main/c/curl/curl.deb")
	importUpload(t, s, item, "", isotest.Image(files))
	importUpload(t, s, item, "netboot", isotest.Image(map[string][]byte{".disk/info": []byte("Debian GNU/Linux 13.6.0 amd64 NETBOOT"), "linux": []byte("kernel"), "initrd.gz": []byte("initrd")}))
	sentinel := errors.New("temporary root unavailable")
	entries, prepared := 0, false
	s.files = importStepFiles{ImportFiles: s.files, wrap: func(real port.ImportSession) port.ImportSession {
		return &importSteps{ImportSession: real, begin: func() error { return sentinel }, prepared: func() { prepared = true }, view: func(v port.MediaView) port.MediaView { return countedMedia{MediaView: v, entries: &entries} }}
	}}
	if err := s.Import(t.Context(), item.ID); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if entries != 0 || prepared {
		t.Fatalf("临时目录失败后仍扫描或复制: %d %v", entries, prepared)
	}
}

func Test镜像Debian检查失败不复制(t *testing.T) {
	s, db := importSetup(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "repo before copy", Family: "linux", Series: "debian"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	files := importFixture("debian")
	delete(files, "pool/main/c/curl/curl.deb")
	importUpload(t, s, item, "", isotest.Image(files))
	importUpload(t, s, item, "netboot", isotest.Image(map[string][]byte{".disk/info": []byte("Debian GNU/Linux 13.6.0 amd64 NETBOOT"), "linux": []byte("kernel"), "initrd.gz": []byte("initrd")}))
	begun, prepared := false, false
	s.files = importStepFiles{ImportFiles: s.files, wrap: func(real port.ImportSession) port.ImportSession {
		return &importSteps{ImportSession: real, begin: func() error { begun = true; return nil }, prepared: func() { prepared = true }}
	}}
	var problem model.Problem
	if err := s.Import(t.Context(), item.ID); !errors.As(err, &problem) || problem.Code != "iso_repo_incomplete" {
		t.Fatal(err)
	}
	if !begun || prepared {
		t.Fatalf("仓库失败前后顺序不对: %v %v", begun, prepared)
	}
	if _, err := os.Stat(s.artifactRoot(item.Series, "13.6.0", "x86_64") + ".tmp-" + item.ID); !os.IsNotExist(err) {
		t.Fatal("临时目录未清理", err)
	}
}

func Test镜像多源完成复制后才复核(t *testing.T) {
	s, db := importSetup(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "verify after consumers", Family: "linux", Series: "debian"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, item, "", isotest.Image(importFixture("debian")))
	importUpload(t, s, item, "netboot", isotest.Image(map[string][]byte{".disk/info": []byte("Debian GNU/Linux 13.6.0 amd64 NETBOOT"), "linux": []byte("original-kernel"), "initrd.gz": []byte("initrd")}))
	copied := false
	s.files = importStepFiles{ImportFiles: s.files, wrap: func(real port.ImportSession) port.ImportSession {
		return &importSteps{ImportSession: real, prepared: func() { copied = true }, verify: func() {
			if !copied {
				t.Fatal("复制前复核")
			}
			tmp := s.artifactRoot(item.Series, "13.6.0", "x86_64") + ".tmp-" + item.ID
			got, err := os.ReadFile(filepath.Join(tmp, "linux"))
			if err != nil || string(got) != "original-kernel" {
				t.Fatal(string(got), err)
			}
			source := s.finalPath(item.ID, "netboot")
			raw, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			at := bytes.Index(raw, []byte("original-kernel"))
			if at < 0 {
				t.Fatal("缺少原内容")
			}
			f, err := os.OpenFile(source, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteAt([]byte("modified-kernel"), int64(at))
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}}
	}}
	var problem model.Problem
	if err := s.Import(t.Context(), item.ID); !errors.As(err, &problem) || problem.Code != "iso_content_mismatch" || problem.Subject.Path != "source.netboot" {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.artifactRoot(item.Series, "13.6.0", "x86_64")); !os.IsNotExist(err) {
		t.Fatal("改变的来源被发布", err)
	}
}

type unknownImportStore struct {
	repository.Store
	committed       bool
	reconciliations int
}

func (s *unknownImportStore) UpdateISO(ctx context.Context, item model.ISO) (model.ISO, error) {
	got, err := s.Store.UpdateISO(ctx, item)
	if err == nil && item.State == model.ISOReady {
		s.committed = true
		return model.ISO{}, errors.New("commit acknowledgment unavailable")
	}
	return got, err
}
func (s *unknownImportStore) GetISO(ctx context.Context, id string) (model.ISO, error) {
	if s.committed {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second {
			s.reconciliations++
		}
		return model.ISO{}, errors.New("outcome unknown")
	}
	return s.Store.GetISO(ctx, id)
}
func Test镜像导入提交未知不删已发布内容(t *testing.T) {
	s, db := importSetup(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "unknown outcome", Family: "windows", Series: "windows-server"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, item, "", isotest.Image(importFixture(item.Series)))
	uncertain := &unknownImportStore{Store: db}
	s.store = uncertain
	if err := s.Import(t.Context(), item.ID); err == nil {
		t.Fatal("未知结果被当成功")
	}
	if uncertain.reconciliations != 2 {
		t.Fatal("最终确认与回滚确认必须各自使用五秒读取", uncertain.reconciliations)
	}
	current, err := db.GetISO(t.Context(), item.ID)
	if err != nil || current.State != model.ISOReady {
		t.Fatal(current, err)
	}
	for _, file := range []string{filepath.Join(s.dirs.Artifacts, current.Artifacts["boot-wim"]), filepath.Join(s.dirs.SMB, "windows-server", "10.0", "x86_64", "sources", "install.wim")} {
		if st, err := os.Stat(file); err != nil || st.Size() == 0 {
			t.Fatal("已提交内容丢失", err)
		}
	}
}

func Test镜像导入中废弃不能被失败标记覆盖(t *testing.T) {
	s, db := importSetup(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "deprecate during import", Family: "linux", Series: "ubuntu"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, item, "", isotest.Image(importFixture(item.Series)))
	s.files = importStepFiles{ImportFiles: s.files, wrap: func(real port.ImportSession) port.ImportSession {
		return &importSteps{ImportSession: real, published: func(p port.Publication, err error) {
			if err != nil {
				t.Fatal(err)
			}
			current, err := db.GetISO(t.Context(), item.ID)
			if err != nil {
				t.Fatal(err)
			}
			current.State = model.ISOReady
			current.Artifacts = p.Artifacts
			current, err = db.UpdateISO(t.Context(), current)
			if err != nil {
				t.Fatal(err)
			}
			if err = db.DeprecateISO(t.Context(), current.ID, current.Rev, importAudit()); err != nil {
				t.Fatal(err)
			}
		}}
	}}
	if err := s.Import(t.Context(), item.ID); !errors.Is(err, repository.ErrImmutable) && !errors.Is(err, repository.ErrConflict) {
		t.Fatal(err)
	}
	current, err := db.GetISO(t.Context(), item.ID)
	if err != nil || current.State != model.ISODeprecated {
		t.Fatal(current, err)
	}
	if mustSource(t, s, item.ID, "").Error != "" {
		t.Fatal("废弃镜像被写入导入失败")
	}
	if _, err := os.Stat(s.artifactRoot(item.Series, current.Version, current.Arch)); err != nil {
		t.Fatal("废弃内容被回滚", err)
	}
}
