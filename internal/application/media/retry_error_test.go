package media_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

type failedRetryParking struct {
	port.Transfer
	err   error
	calls int
}

func (p *failedRetryParking) ParkRetry(context.Context, model.Source) (port.RetryFiles, error) {
	p.calls++
	return nil, p.err
}

func Test镜像重试保留文件恢复失败(t *testing.T) {
	for _, state := range []model.SourceState{model.SourceFailed, model.SourceCancelled} {
		t.Run(string(state), func(t *testing.T) {
			db := pgtest.Open(t)
			audit := repository.Audit{Actor: "retry-test", Action: "iso.source", At: 1}
			item, err := db.CreateISO(t.Context(), model.ISO{Name: "retry errors", Family: "linux", Series: "rocky"}, audit)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			restoreCause := errors.New("restore rename failed")
			restoreFailure := &os.LinkError{Op: "rename", Old: "backup", New: "part", Err: restoreCause}
			transfer := &failedRetryParking{Transfer: iso.NewTransfer(iso.Dirs{Staging: dir}), err: errors.Join(&port.Fault{Kind: port.InvalidFile}, restoreFailure)}
			service := media.NewSources(media.SourceDeps{ISOs: db, Sources: db, Transfer: transfer, Locks: iso.NewSourceLocks(), Gate: iso.NewRegistrationGate()})
			if err = service.Register(t.Context(), item, "upload", "", "", 4, "", audit); err != nil {
				t.Fatal(err)
			}
			if err = service.Upload(t.Context(), item.ID, "", 0, strings.NewReader("ab"), audit); err != nil {
				t.Fatal(err)
			}
			before, _, err := db.GetSource(t.Context(), item.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			before.State = state
			if state == model.SourceFailed {
				before.Error = "interrupted"
			}
			before, err = db.SaveSource(t.Context(), before, audit)
			if err != nil {
				t.Fatal(err)
			}
			retryAudit := repository.Audit{Actor: "retry-test", Action: "iso.retry.recovery", At: 2}
			entries, err := db.ListAudit(t.Context(), repository.AuditQuery{Action: retryAudit.Action})
			if err != nil || len(entries.Items) != 0 {
				t.Fatal(entries, err)
			}
			err = service.Retry(t.Context(), item.ID, retryAudit)
			if !errors.Is(err, repository.ErrInvalid) || !errors.Is(err, restoreCause) || !errors.Is(err, restoreFailure) {
				t.Errorf("文件错误与恢复原因必须同时可识别: %v", err)
			}
			var link *os.LinkError
			if !errors.As(err, &link) || link != restoreFailure {
				t.Errorf("恢复错误类型丢失: %v", err)
			}
			after, _, err := db.GetSource(t.Context(), item.ID, "")
			if err != nil || after != before || transfer.calls != 1 {
				t.Fatal(after, before, transfer.calls, err)
			}
			entries, err = db.ListAudit(t.Context(), repository.AuditQuery{Action: retryAudit.Action})
			if err != nil || len(entries.Items) != 0 {
				t.Fatal("失败重试提交了审计", entries, err)
			}
			ranges, err := db.RangesOf(t.Context(), item.ID, "")
			if err != nil || len(ranges) != 1 || ranges[0] != (model.Range{Start: 0, Length: 2}) {
				t.Fatal(ranges, err)
			}
			bytes, err := os.ReadFile(filepath.Join(dir, item.ID+".part"))
			if err != nil || string(bytes) != "ab" {
				t.Fatal(string(bytes), err)
			}
		})
	}
}
