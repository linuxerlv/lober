package media_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

// 只在额外来源边界放入故障；文件、摘要和数据库仍用真实实现。
type extraMetadataSession struct {
	port.ImportSession
	at, reached, closed  int
	digested             bool
	failure, openFailure error
	cancel               context.CancelFunc
}

func (s *extraMetadataSession) OpenSource(ctx context.Context, src model.Source) (port.MediaView, error) {
	if src.Role != "" && s.openFailure != nil {
		s.reached++
		return nil, s.openFailure
	}
	v, err := s.ImportSession.OpenSource(ctx, src)
	if err != nil || src.Role == "" {
		return v, err
	}
	return &extraMetadataView{MediaView: v, session: s}, nil
}

func (s *extraMetadataSession) Close() error {
	s.closed++
	return s.ImportSession.Close()
}

type extraMetadataView struct {
	port.MediaView
	session *extraMetadataSession
	reads   int
}

func (v *extraMetadataView) Digest(ctx context.Context) (string, error) {
	digest, err := v.MediaView.Digest(ctx)
	v.session.digested = err == nil
	return digest, err
}

func (v *extraMetadataView) Text(ctx context.Context, name string, limit int64) (string, error) {
	if name == ".disk/info" {
		v.reads++
		if v.reads == v.session.at {
			v.session.reached++
			if v.session.cancel != nil {
				v.session.cancel()
				return v.MediaView.Text(ctx, name, limit)
			}
			return "", v.session.failure
		}
	}
	return v.MediaView.Text(ctx, name, limit)
}

func extraMetadataSetup(t *testing.T, extra []byte) (*acquisitionFixture, repository.Store, model.ISO, []model.Source) {
	t.Helper()
	s, db := importSetup(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "extra metadata", Family: "linux", Series: "debian"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, item, "", isotest.Image(importFixture("debian")))
	importUpload(t, s, item, "netboot", extra)
	item, err = db.GetISO(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := db.SourcesOf(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != model.ISOAcquiring || len(sources) != 2 {
		t.Fatal("取消测试的导入起点不合法", item, sources)
	}
	return s, db, item, sources
}

func extraMetadataImage(info string) []byte {
	return isotest.Image(map[string][]byte{".disk/info": []byte(info), "linux": []byte("kernel"), "initrd.gz": []byte("initrd")})
}

func wrapExtraMetadata(s *acquisitionFixture, boundary *extraMetadataSession) {
	s.files = importStepFiles{ImportFiles: s.files, wrap: func(real port.ImportSession) port.ImportSession {
		boundary.ImportSession = real
		return boundary
	}}
}

func assertExtraImportUnchanged(t *testing.T, ctx context.Context, db repository.Store, before model.ISO, sources []model.Source, boundary *extraMetadataSession) {
	t.Helper()
	readCtx := context.WithoutCancel(ctx)
	current, err := db.GetISO(readCtx, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, before) {
		t.Errorf("取消后镜像记录改变（包括状态和Rev）：原%+v，现%+v", before, current)
	}
	currentSources, err := db.SourcesOf(readCtx, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(currentSources, sources) {
		t.Errorf("取消后来源记录改变（包括Error和Rev）：原%+v，现%+v", sources, currentSources)
	}
	if boundary.reached != 1 || boundary.closed != 1 {
		t.Errorf("指定边界应到达一次且会话应关闭一次：到达%d，关闭%d", boundary.reached, boundary.closed)
	}
}

// 原审计六例在这里正式回归：两次读取各注入取消/超时，再各用真实父取消委托真实Text。
// 同时检查包装错误不能被拆掉，防止错误上下文丢失。
func Test镜像额外源读取取消超时类型与状态保持(t *testing.T) {
	for _, boundary := range []struct {
		name string
		at   int
	}{
		{"识别网络介质", 1}, {"再次核对网络介质", 2},
	} {
		for _, failure := range []struct {
			name   string
			err    error
			parent bool
		}{
			{"能力取消", context.Canceled, false},
			{"能力超时", context.DeadlineExceeded, false},
			{"包装取消", fmt.Errorf("读取额外源: %w", context.Canceled), false},
			{"包装超时", fmt.Errorf("读取额外源: %w", context.DeadlineExceeded), false},
			{"真实父取消", context.Canceled, true},
		} {
			t.Run(boundary.name+"/"+failure.name, func(t *testing.T) {
				s, db, before, sources := extraMetadataSetup(t, extraMetadataImage("Debian GNU/Linux 13.6.0 amd64 NETBOOT"))
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				step := &extraMetadataSession{at: boundary.at, failure: failure.err}
				if failure.parent {
					step.cancel = cancel
				}
				wrapExtraMetadata(s, step)
				err := s.Import(ctx, before.ID)
				if !step.digested {
					t.Fatal("未完成真实额外源摘要，不能证明元信息读取边界", err)
				}
				if !errors.Is(err, failure.err) {
					t.Errorf("取消或超时类型及包装丢失：期望%v，得到%T: %v", failure.err, err, err)
				}
				if failure.parent && !errors.Is(ctx.Err(), context.Canceled) {
					t.Error("指定边界没有真正取消父上下文")
				}
				assertExtraImportUnchanged(t, ctx, db, before, sources, step)
			})
		}
	}
}

func Test镜像额外源普通错误仍按原分类(t *testing.T) {
	for _, tc := range []struct {
		name    string
		extra   []byte
		at      int
		failure error
	}{
		{"坏介质", []byte("not an ISO image"), 0, nil},
		{"版本不符", extraMetadataImage("Debian GNU/Linux 12.1.0 amd64 NETBOOT"), 0, nil},
		{"不是网络介质", extraMetadataImage("Debian GNU/Linux 13.6.0 amd64 NETINST"), 0, nil},
		{"第一次读取普通错误", extraMetadataImage("Debian GNU/Linux 13.6.0 amd64 NETBOOT"), 1, errors.New("metadata unavailable")},
		{"第二次读取普通错误", extraMetadataImage("Debian GNU/Linux 13.6.0 amd64 NETBOOT"), 2, errors.New("metadata unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, before, _ := extraMetadataSetup(t, tc.extra)
			step := &extraMetadataSession{at: tc.at, failure: tc.failure}
			wrapExtraMetadata(s, step)
			err := s.Import(t.Context(), before.ID)
			var problem model.Problem
			if !errors.As(err, &problem) || problem.Code != "iso_extra_source_invalid" || problem.Subject.ID != before.ID || problem.Subject.Path != "source.netboot" {
				t.Fatalf("普通错误分类改变：%v", err)
			}
			current, readErr := db.GetISO(t.Context(), before.ID)
			if readErr != nil || current.State != model.ISOFailed || current.Rev != before.Rev+1 {
				t.Fatal("普通错误没有按原行为记录失败", current, readErr)
			}
			if mustSource(t, s, before.ID, "").Error != err.Error() {
				t.Error("普通错误没有保存到主来源")
			}
			if step.closed != 1 || tc.at > 0 && (step.reached != 1 || !step.digested) {
				t.Fatalf("普通错误的读取或关闭边界不对：%+v", step)
			}
		})
	}
}
