package media

import (
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"reflect"
	"testing"
)

func Test删除快照纯校验保留未识别状态(t *testing.T) {
	item := model.ISO{ID: "test", Family: "linux", Series: "rocky", State: model.ISOAcquiring, Rev: 1}
	before := item
	if err := CheckDeletion(item, []model.Source{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, item) || item.Version != "" || item.Arch != "" {
		t.Fatal("临时值写回了对象")
	}
	good := model.Source{ISOID: item.ID, Kind: model.SourceUpload, State: model.SourcePending, TotalSize: 4}
	for _, tc := range []struct {
		name    string
		item    model.ISO
		sources []model.Source
	}{
		{"nil来源", item, nil},
		{"其他镜像", item, []model.Source{{ISOID: "other", Kind: model.SourceUpload, State: model.SourcePending}}},
		{"重复角色", item, []model.Source{good, good}},
		{"来源路径角色", item, []model.Source{{ISOID: item.ID, Role: "../other", Kind: model.SourceUpload, State: model.SourcePending}}},
		{"来源计数", item, []model.Source{{ISOID: item.ID, Kind: model.SourceUpload, State: model.SourcePending, Fetched: -1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckDeletion(tc.item, tc.sources); !errors.Is(err, repository.ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
	for _, id := range []string{"../outside", "UPPER"} {
		bad := item
		bad.ID = id
		if err := CheckDeletion(bad, []model.Source{}); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	bad := item
	bad.Version = "../outside"
	if err := CheckDeletion(bad, []model.Source{}); !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
}
