package media_test

import (
	"github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"testing"
)

func Test镜像恢复只列持久化来源(t *testing.T) {
	db := pgtest.Open(t)
	audit := repository.Audit{Actor: "source-test", Action: "iso.source", At: 1}
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "source", Family: "linux", Series: "rocky"}, audit)
	if err != nil {
		t.Fatal(err)
	}
	src, err := db.SaveSource(t.Context(), model.Source{ISOID: item.ID, Kind: model.SourceUpload, TotalSize: 4, State: model.SourcePending}, audit)
	if err != nil {
		t.Fatal(err)
	}
	// 恢复只读记录，不应调用任何文件或网络能力。
	var transfer port.Transfer
	service := media.NewSources(media.SourceDeps{ISOs: db, Sources: db, Transfer: transfer, Locks: iso.NewSourceLocks(), Gate: iso.NewRegistrationGate()})
	todo, err := service.Recover(t.Context())
	if err != nil || len(todo) != 1 || todo[0] != (media.ResumeWork{ISOID: item.ID, Kind: media.ResumeFetch}) {
		t.Fatalf("恢复清单：%+v %v", todo, err)
	}
	current, ok, err := db.GetSource(t.Context(), item.ID, "")
	if err != nil || !ok || current != src {
		t.Fatalf("恢复修改了来源：%+v %v", current, err)
	}
}
