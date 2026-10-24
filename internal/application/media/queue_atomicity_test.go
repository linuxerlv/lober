package media_test

import (
	"bytes"
	appmedia "github.com/lober/lober/internal/application/media"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/media"
)

func failedQueueISO(t *testing.T, a *deletionFixture, series string) model.ISO {
	t.Helper()
	item, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "queue-" + series, Family: "linux", Series: catalog.Series(series)}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Sources.Register(t.Context(), item, "upload", "", "", 4, "", assemblyAudit("iso.source")); err != nil {
		t.Fatal(err)
	}
	if err := a.Sources.Upload(t.Context(), item.ID, "", 0, bytes.NewReader([]byte("data")), repository.Audit{}); err != nil {
		t.Fatal(err)
	}
	item.State = model.ISOFailed
	item, err = a.Store.UpdateISO(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func rejectQueuedState(t *testing.T, cfg deletionConfig) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(t.Context(), cfg.PG.DSN)
	if err != nil {
		t.Fatal("test connection failed")
	}
	t.Cleanup(func() { c.Close(t.Context()) })
	_, err = c.Exec(t.Context(), `CREATE FUNCTION reject_queue() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.state = 'acquiring' THEN RAISE EXCEPTION 'injected queue failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER reject_queue BEFORE UPDATE ON isos FOR EACH ROW EXECUTE FUNCTION reject_queue();`)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func Test补来源调度失败必须回滚登记和审计(t *testing.T) {
	a, cfg := assembledDeletion(t)
	item := failedQueueISO(t, a, "debian")
	c := rejectQueuedState(t, cfg)
	audit := assemblyAudit("iso.extra.queue")
	if err := a.Control.Register(t.Context(), item, "upload", "", "netboot", 4, "", audit); err == nil {
		t.Fatal("failed queue accepted")
	}
	_, exists, err := a.Store.GetSource(t.Context(), item.ID, "netboot")
	if err != nil || exists {
		t.Fatal("failed scheduling committed extra source", exists, err)
	}
	unchanged, err := a.Store.GetISO(t.Context(), item.ID)
	if err != nil || unchanged.State != model.ISOFailed || unchanged.Rev != item.Rev {
		t.Fatal("排队失败改变了镜像", unchanged, err)
	}
	entries, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{Action: audit.Action, TargetID: item.ID})
	if err != nil || len(entries.Items) != 0 {
		t.Fatal("failed scheduling committed audit", entries, err)
	}
	if _, err := c.Exec(t.Context(), `DROP TRIGGER reject_queue ON isos`); err != nil {
		t.Fatal(err)
	}
	if err := a.Control.Register(t.Context(), item, "upload", "", "netboot", 4, "", audit); err != nil {
		t.Fatal(err)
	}
	queued, err := a.Store.GetISO(t.Context(), item.ID)
	if err != nil || queued.State != model.ISOAcquiring || queued.Rev != item.Rev+1 {
		t.Fatal("来源与排队没有一起保存", queued, err)
	}
	entries, err = a.Store.ListAudit(t.Context(), repository.AuditQuery{Action: audit.Action, TargetID: item.ID})
	if err != nil || len(entries.Items) != 1 {
		t.Fatal("成功排队应恰好记录一条审计", entries, err)
	}
	restarted := appmedia.NewSources(appmedia.SourceDeps{ISOs: a.Store, Sources: a.Store, Transfer: iso.NewTransfer(cfg.ISO), Import: iso.NewImportFiles(cfg.ISO), Locks: iso.NewSourceLocks(), Gate: iso.NewRegistrationGate()})
	work, err := restarted.Recover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range work {
		if w.ISOID == item.ID && w.Role == "netboot" && w.Kind == appmedia.ResumeFetch {
			found = true
		}
	}
	if !found {
		t.Fatal("extra source not durable after restart", work)
	}
}

func Test重新导入调度失败必须回滚审计和来源修订(t *testing.T) {
	a, cfg := assembledDeletion(t)
	item := failedQueueISO(t, a, "rocky")
	rejectQueuedState(t, cfg)
	before, _, err := a.Store.GetSource(t.Context(), item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	audit := assemblyAudit("iso.reimport.queue")
	if err := a.Sources.RequestReimport(t.Context(), item.ID, audit); err == nil {
		t.Fatal("failed queue accepted")
	}
	after, _, err := a.Store.GetSource(t.Context(), item.ID, "")
	if err != nil || after.Rev != before.Rev {
		t.Fatal("failed queue changed source revision", before.Rev, after.Rev, err)
	}
	unchanged, err := a.Store.GetISO(t.Context(), item.ID)
	if err != nil || unchanged.State != model.ISOFailed || unchanged.Rev != item.Rev {
		t.Fatal("重新导入失败改变了镜像", unchanged, err)
	}
	entries, err := a.Store.ListAudit(t.Context(), repository.AuditQuery{Action: audit.Action, TargetID: item.ID})
	if err != nil || len(entries.Items) != 0 {
		t.Fatal("failed queue committed audit", entries, err)
	}
}
