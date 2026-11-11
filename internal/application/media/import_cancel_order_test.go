package media_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
	"testing"
)

type cancelExtraSession struct {
	port.ImportSession
	cancel context.CancelFunc
}

func Test镜像额外源打开取消类型保持(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
	}{
		{"取消", context.Canceled},
		{"超时", context.DeadlineExceeded},
		{"包装取消", fmt.Errorf("打开额外源: %w", context.Canceled)},
		{"包装超时", fmt.Errorf("打开额外源: %w", context.DeadlineExceeded)},
	} {
		t.Run(failure.name, func(t *testing.T) {
			s, db, before, sources := extraMetadataSetup(t, extraMetadataImage("Debian GNU/Linux 13.6.0 amd64 NETBOOT"))
			step := &extraMetadataSession{openFailure: failure.err}
			wrapExtraMetadata(s, step)
			err := s.Import(t.Context(), before.ID)
			if !errors.Is(err, failure.err) {
				t.Errorf("打开额外源的取消或超时类型及包装丢失：期望%v，得到%T: %v", failure.err, err, err)
			}
			assertExtraImportUnchanged(t, t.Context(), db, before, sources, step)
		})
	}
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
