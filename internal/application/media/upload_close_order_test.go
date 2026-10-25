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
	"reflect"
	"strings"
	"testing"
)

type closeOrderTransfer struct {
	port.Transfer
	order    *[]string
	closeErr error
	closes   int
}
type closeOrderUpload struct {
	port.UploadSession
	owner *closeOrderTransfer
}

func (s *closeOrderTransfer) BeginUpload(ctx context.Context, src model.Source, offset int64, r io.Reader) (port.UploadSession, error) {
	u, e := s.Transfer.BeginUpload(ctx, src, offset, r)
	if e != nil {
		return nil, e
	}
	return &closeOrderUpload{u, s}, nil
}
func (s *closeOrderTransfer) BeginPublish(ctx context.Context, src model.Source) (port.FetchSession, error) {
	*s.order = append(*s.order, "publish")
	return s.Transfer.BeginPublish(ctx, src)
}
func (s *closeOrderUpload) Write(ctx context.Context) error {
	*s.owner.order = append(*s.owner.order, "write")
	return s.UploadSession.Write(ctx)
}
func (s *closeOrderUpload) Close() error {
	s.owner.closes++
	*s.owner.order = append(*s.owner.order, "close")
	return errors.Join(s.UploadSession.Close(), s.owner.closeErr)
}

type closeOrderSources struct {
	port.Sources
	order *[]string
}

func (s closeOrderSources) SaveRange(ctx context.Context, id, role string, start, n int64) error {
	*s.order = append(*s.order, "range")
	return s.Sources.SaveRange(ctx, id, role, start, n)
}
func Test镜像最后片关闭失败发生在确认区间之后(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "关闭成功"
		if fail {
			name = "关闭失败"
		}
		t.Run(name, func(t *testing.T) {
			db := pgtest.Open(t)
			a := repository.Audit{Actor: "source-test", Action: "iso.source", At: 1}
			item, e := db.CreateISO(t.Context(), model.ISO{Name: "close-order", Family: "linux", Series: "rocky"}, a)
			if e != nil {
				t.Fatal(e)
			}
			var order []string
			tr := &closeOrderTransfer{Transfer: iso.NewTransfer(iso.Dirs{Staging: t.TempDir()}), order: &order}
			if fail {
				tr.closeErr = injected
			}
			service := media.NewSources(media.SourceDeps{ISOs: db, Sources: closeOrderSources{db, &order}, Transfer: tr, Locks: iso.NewSourceLocks(), Gate: iso.NewRegistrationGate()})
			if e = service.Register(t.Context(), item, "upload", "", "", 4, "", a); e != nil {
				t.Fatal(e)
			}
			e = service.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), repository.Audit{})
			want := []string{"write", "range", "close", "publish"}
			state := model.SourceFetched
			if fail {
				want = want[:3]
				state = model.SourceFetching
				if !errors.Is(e, injected) {
					t.Fatal("未返回关闭错误", e)
				}
			} else if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(order, want) || tr.closes != 1 {
				t.Fatal("顺序或关闭次数错误", order, tr.closes)
			}
			spans, e := db.RangesOf(t.Context(), item.ID, "")
			if e != nil || len(spans) != 1 || spans[0] != (model.Range{Start: 0, Length: 4}) {
				t.Fatal("关闭前应已保存区间", spans, e)
			}
			src, _, e := db.GetSource(t.Context(), item.ID, "")
			if e != nil || src.State != state || src.Fetched != 4 {
				t.Fatal(src, e)
			}
		})
	}
}
