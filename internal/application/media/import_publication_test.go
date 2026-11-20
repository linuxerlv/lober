package media_test

import (
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
	"os"
	"path/filepath"
	"testing"
)

func Test镜像共享树先发布且冲突只撤销本次新树(t *testing.T) {
	s, db := importSetup(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "partial publication", Family: "windows", Series: "windows-server"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, item, "", isotest.Image(importFixture(item.Series)))
	root := s.artifactRoot(item.Series, "10.0", "x86_64")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".lober-owner"), []byte("other-owner\nother-digest\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep"), []byte("other content"), 0644); err != nil {
		t.Fatal(err)
	}
	s.files = importStepFiles{ImportFiles: s.files, wrap: func(real port.ImportSession) port.ImportSession {
		return &importSteps{ImportSession: real, published: func(p port.Publication, err error) {
			if err == nil || !p.MadeSMB || p.MadeArtifacts {
				t.Fatal("发布失败没有保留先发布共享树的事实", p, err)
			}
			if _, err := os.Stat(filepath.Join(s.dirs.SMB, "windows-server", "10.0", "x86_64", "sources", "install.wim")); err != nil {
				t.Fatal("共享树未先发布", err)
			}
		}}
	}}
	if err := s.Import(t.Context(), item.ID); !errors.Is(err, repository.ErrConflict) {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "keep")); err != nil || string(got) != "other content" {
		t.Fatal("其他拥有者的内容被改写", err)
	}
	if _, err := os.Stat(filepath.Join(s.dirs.SMB, "windows-server", "10.0", "x86_64")); !os.IsNotExist(err) {
		t.Fatal("本次共享树没有撤销", err)
	}
	current, err := db.GetISO(t.Context(), item.ID)
	if err != nil || current.State != model.ISOFailed {
		t.Fatal(current, err)
	}
}
