package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type retiredUploadFixture struct{ *retiredSourceFixture }

func (f *retiredUploadFixture) GetSource(context.Context, string, string) (model.Source, bool, error) {
	return model.Source{ISOID: f.item.ID, Kind: model.SourceUpload, State: f.state, TotalSize: 4}, true, nil
}

type observedRetiredUpload struct {
	port.Transfer
	opened int
}

func (f *observedRetiredUpload) BeginUpload(context.Context, model.Source, int64, io.Reader) (port.UploadSession, error) {
	f.opened++
	return nil, errors.New("不应进入文件上传")
}

func TestRetiredISORejectsExistingUploadSources(t *testing.T) {
	for _, series := range []catalog.Series{"sles15", "leap15", "sles16", "leap16", "openeuler", "opencloudos"} {
		for _, state := range []model.SourceState{model.SourcePending, model.SourceFetching, model.SourceFetched} {
			for _, throughRegistry := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/registry=%t", series, state, throughRegistry), func(t *testing.T) {
					f := &retiredUploadFixture{retiredSourceFixture: &retiredSourceFixture{item: model.ISO{ID: "old", Family: "linux", Series: series, State: model.ISOAcquiring, Rev: 1}, state: state}}
					transfer := &observedRetiredUpload{}
					sources := NewSources(SourceDeps{ISOs: f, Sources: f, Transfer: transfer, Locks: retiredSourceLocks{}})
					body := strings.NewReader("data")
					var err error
					if throughRegistry {
						registry := &registry{deps: Deps{ISOs: f, Sources: f, Control: NewControl(sources, nil)}}
						err = registry.Upload(t.Context(), admin, UploadRequest{ID: "old", End: 3, Total: 4, ContentLength: 4, Body: body})
					} else {
						err = sources.Upload(t.Context(), "old", "", 0, body, repository.Audit{})
					}
					if !errors.Is(err, repository.ErrInvalid) {
						t.Errorf("停用镜像上传错误=%v", err)
					}
					if transfer.opened != 0 || body.Len() != 4 {
						t.Errorf("拒绝前已触及上传内容：打开%d次，剩余%d字节", transfer.opened, body.Len())
					}
				})
			}
		}
	}
}
