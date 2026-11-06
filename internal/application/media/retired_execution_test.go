package media

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type retiredSourceFixture struct {
	port.ISOs
	port.Sources
	item   model.ISO
	state  model.SourceState
	writes int
}

func (f *retiredSourceFixture) GetISO(context.Context, string) (model.ISO, error) { return f.item, nil }
func (f *retiredSourceFixture) SourcesOf(context.Context, string) ([]model.Source, error) {
	return []model.Source{{ISOID: f.item.ID, State: f.state}}, nil
}
func (f *retiredSourceFixture) ListUnfinishedSources(context.Context) ([]model.Source, error) {
	return []model.Source{{ISOID: f.item.ID, State: f.state}}, nil
}
func (f *retiredSourceFixture) ListISOs(context.Context, repository.ISOQuery) (repository.Page[model.ISO], error) {
	return repository.Page[model.ISO]{Items: []model.ISO{f.item}}, nil
}
func (f *retiredSourceFixture) SaveSourceAndAcquire(_ context.Context, s model.Source, _ int64, _ repository.Audit) (model.Source, error) {
	f.writes++
	return s, nil
}

func TestRetiredISORejectsRetryAndReimport(t *testing.T) {
	for _, series := range []catalog.Series{"sles15", "leap15", "sles16", "leap16", "openeuler", "opencloudos"} {
		f := &retiredSourceFixture{item: model.ISO{ID: "old", Family: "linux", Series: series, State: model.ISOFailed, Rev: 1}, state: model.SourceFetched}
		s := NewSources(SourceDeps{ISOs: f, Sources: f})
		audit := repository.Audit{Actor: "admin", Action: "iso.retry", At: 1}
		if err := s.Retry(t.Context(), "old", audit); !errors.Is(err, repository.ErrInvalid) {
			t.Errorf("%s 重试错误=%v", series, err)
		}
		if err := s.RequestReimport(t.Context(), "old", audit); !errors.Is(err, repository.ErrInvalid) {
			t.Errorf("%s 重新导入错误=%v", series, err)
		}
		if f.writes != 0 {
			t.Errorf("%s 写入了 %d 次", series, f.writes)
		}
	}
}
func TestRecoverOmitsRetiredISOWork(t *testing.T) {
	for _, state := range []model.SourceState{model.SourcePending, model.SourceFetched} {
		f := &retiredSourceFixture{item: model.ISO{ID: "old", Family: "linux", Series: "sles16", State: model.ISOAcquiring}, state: state}
		s := NewSources(SourceDeps{ISOs: f, Sources: f})
		work, err := s.Recover(t.Context())
		if err != nil || len(work) != 0 {
			t.Fatalf("停用镜像被恢复：%v，%v", work, err)
		}
	}
}

func TestRetiredISORejectsImportAndSourceRegistration(t *testing.T) {
	f := &retiredSourceFixture{item: model.ISO{ID: "old", Family: "linux", Series: "leap16", State: model.ISOAcquiring, Rev: 1}, state: model.SourceFetched}
	s := NewSources(SourceDeps{ISOs: f, Sources: f, Locks: retiredSourceLocks{}, Gate: &sync.RWMutex{}})
	if err := s.Import(t.Context(), "old"); !errors.Is(err, repository.ErrInvalid) {
		t.Fatalf("旧镜像仍能进入导入：%v", err)
	}
	audit := repository.Audit{Actor: "admin", Action: "iso.source.register", At: 1}
	if err := s.Register(t.Context(), f.item, "upload", "", "", 100, "", audit); !errors.Is(err, repository.ErrInvalid) {
		t.Fatalf("旧镜像仍能登记来源：%v", err)
	}
}

type retiredSourceLocks struct{ port.SourceLocks }

func (retiredSourceLocks) Lock(string, string) func() { return func() {} }
