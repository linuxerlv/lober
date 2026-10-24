package media_test

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
	"testing"
)

type cancelExtraSession struct {
	port.ImportSession
	cancel context.CancelFunc
}

func (s cancelExtraSession) OpenSource(ctx context.Context, src model.Source) (port.MediaView, error) {
	if src.Role != "" {
		s.cancel()
	}
	return s.ImportSession.OpenSource(ctx, src)
}

func Test镜像打开有效额外源时取消仍返回取消(t *testing.T) {
	s, db := importSetup(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "cancel at extra", Family: "linux", Series: "debian"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, item, "", isotest.Image(importFixture("debian")))
	importUpload(t, s, item, "netboot", isotest.Image(map[string][]byte{".disk/info": []byte("Debian GNU/Linux 13.6.0 amd64 NETBOOT"), "linux": []byte("kernel"), "initrd.gz": []byte("initrd")}))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.files = importStepFiles{ImportFiles: s.files, wrap: func(real port.ImportSession) port.ImportSession {
		return cancelExtraSession{ImportSession: real, cancel: cancel}
	}}
	if err := s.Import(ctx, item.ID); !errors.Is(err, context.Canceled) {
		t.Fatal("有效文件的取消被误报为无效额外源", err)
	}
	current, err := db.GetISO(t.Context(), item.ID)
	if err != nil || current.State != model.ISOAcquiring {
		t.Fatal(current, err)
	}
	if mustSource(t, s, item.ID, "").Error != "" {
		t.Fatal("取消被写成导入失败")
	}
}
